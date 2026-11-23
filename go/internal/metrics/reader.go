// Package metrics reads Raspberry Pi health metrics from Linux procfs and sysfs.
//
// CPU and network values are rates, so they are computed from the difference
// between two successive kernel counter readings.
package metrics

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const topProcesses = 5

// Snapshot is one reading of every metric shown on the dashboard.
type Snapshot struct {
	CPUUsage                         *float64                  `json:"cpu_usage"`
	LoadOne                          *float64                  `json:"load_one"`
	LoadFive                         *float64                  `json:"load_five"`
	LoadFifteen                      *float64                  `json:"load_fifteen"`
	MemoryTotalBytes                 *int64                    `json:"memory_total_bytes"`
	MemoryUsedBytes                  *int64                    `json:"memory_used_bytes"`
	MemoryUsage                      *float64                  `json:"memory_usage"`
	SwapTotalBytes                   *int64                    `json:"swap_total_bytes"`
	SwapUsedBytes                    *int64                    `json:"swap_used_bytes"`
	SwapUsage                        *float64                  `json:"swap_usage"`
	NetworkReceivedBytesPerSecond    *int64                    `json:"network_received_bytes_per_second"`
	NetworkTransmittedBytesPerSecond *int64                    `json:"network_transmitted_bytes_per_second"`
	NetworkInterfaces                map[string]InterfaceRates `json:"network_interfaces"`
	TopCPUProcesses                  []Process                 `json:"top_cpu_processes"`
	TopMemoryProcesses               []Process                 `json:"top_memory_processes"`
	LoggedInUsers                    int                       `json:"logged_in_users"`
	TemperatureCelsius               *float64                  `json:"temperature_celsius"`
	CPUFrequencyHz                   *int64                    `json:"cpu_frequency_hz"`
	CPUMaxFrequencyHz                *int64                    `json:"cpu_max_frequency_hz"`
	ThrottlingFlags                  *int64                    `json:"throttling_flags"`
	Filesystems                      []Filesystem              `json:"filesystems"`
	Databases                        []Database                `json:"databases"`
}

// InterfaceRates holds the per-second traffic of one network interface.
type InterfaceRates struct {
	ReceivedBytesPerSecond    int64 `json:"received_bytes_per_second"`
	TransmittedBytesPerSecond int64 `json:"transmitted_bytes_per_second"`
}

// Process is one entry of the top CPU or memory consumers.
type Process struct {
	PID         int      `json:"pid"`
	Name        string   `json:"name"`
	CPUUsage    *float64 `json:"cpu_usage"`
	MemoryBytes int64    `json:"memory_bytes"`
	MemoryUsage float64  `json:"memory_usage"`
}

// Filesystem is the capacity of one mounted physical filesystem.
type Filesystem struct {
	Source         string  `json:"source"`
	Type           string  `json:"type"`
	Mountpoint     string  `json:"mountpoint"`
	TotalBytes     int64   `json:"total_bytes"`
	UsedBytes      int64   `json:"used_bytes"`
	AvailableBytes int64   `json:"available_bytes"`
	Usage          float64 `json:"usage"`
}

// Hardware holds Raspberry Pi clock and firmware throttling values.
type Hardware struct {
	CPUFrequencyHz    *int64
	CPUMaxFrequencyHz *int64
	ThrottlingFlags   *int64
}

// Counters are the raw kernel counters kept between two readings.
type Counters struct {
	CPU       cpuCounters
	Network   map[string]networkCounters
	Processes map[int]processCounters
}

type cpuCounters struct{ total, idle int64 }

type networkCounters struct{ received, transmitted int64 }

type processCounters struct {
	name        string
	ticks       int64
	memoryBytes int64
}

// Reader reads metrics from a procfs root. The function fields are the
// sources that do not come from procfs; tests replace them with fakes.
type Reader struct {
	ProcRoot      string
	PageSize      int64
	Filesystems   func() ([]Filesystem, error)
	LoggedInUsers func() int
	Temperature   func() *float64
	Hardware      func() Hardware
	// Databases is optional; it reports watched SQLite files.
	Databases func() []Database
}

// Paths configures where a Reader looks for its Linux sources.
type Paths struct {
	ProcRoot         string
	ThermalPath      string
	FrequencyPath    string
	MaxFrequencyPath string
	ThrottlingPath   string
}

// DefaultPaths returns the standard Raspberry Pi OS locations, overridable
// with the same environment variables as the Rails version.
func DefaultPaths() Paths {
	return Paths{
		ProcRoot:         env("PROC_ROOT", "/proc"),
		ThermalPath:      env("THERMAL_PATH", "/sys/class/thermal/thermal_zone0/temp"),
		FrequencyPath:    env("CPU_FREQUENCY_PATH", "/sys/devices/system/cpu/cpu0/cpufreq/scaling_cur_freq"),
		MaxFrequencyPath: env("CPU_MAX_FREQUENCY_PATH", "/sys/devices/system/cpu/cpu0/cpufreq/cpuinfo_max_freq"),
		ThrottlingPath:   env("THROTTLING_PATH", "/sys/devices/platform/soc/soc:firmware/get_throttled"),
	}
}

// NewReader returns a Reader that uses the real host sources and reports
// the given databases.
func NewReader(paths Paths, databases *DatabaseReader) *Reader {
	return &Reader{
		Databases:     databases.Read,
		ProcRoot:      paths.ProcRoot,
		PageSize:      int64(os.Getpagesize()),
		Filesystems:   func() ([]Filesystem, error) { return ReadFilesystems(filepath.Join(paths.ProcRoot, "mounts")) },
		LoggedInUsers: CountLoggedInUsers,
		Temperature:   func() *float64 { return readTemperature(paths.ThermalPath) },
		Hardware: func() Hardware {
			return Hardware{
				CPUFrequencyHz:    scale(readInteger(paths.FrequencyPath), 1000),
				CPUMaxFrequencyHz: scale(readInteger(paths.MaxFrequencyPath), 1000),
				ThrottlingFlags:   readInteger(paths.ThrottlingPath),
			}
		},
	}
}

// Read takes one reading. previous and elapsed come from the prior reading;
// pass nil and 0 for the first one, which then has no CPU or network rates.
func (r *Reader) Read(previous *Counters, elapsed time.Duration) (Snapshot, Counters, error) {
	cpu, err := r.cpuCounters()
	if err != nil {
		return Snapshot{}, Counters{}, err
	}
	network, err := r.networkCounters()
	if err != nil {
		return Snapshot{}, Counters{}, err
	}
	loads, err := r.loadAverages()
	if err != nil {
		return Snapshot{}, Counters{}, err
	}
	memory, err := r.memory()
	if err != nil {
		return Snapshot{}, Counters{}, err
	}
	filesystems, err := r.Filesystems()
	if err != nil {
		return Snapshot{}, Counters{}, err
	}
	processes := r.processCounters()
	hardware := r.Hardware()

	var prevCPU *cpuCounters
	var prevNetwork map[string]networkCounters
	var prevProcesses map[int]processCounters
	if previous != nil {
		prevCPU = &previous.CPU
		prevNetwork = previous.Network
		prevProcesses = previous.Processes
	}

	snapshot := Snapshot{
		CPUUsage:           cpuUsage(cpu, prevCPU),
		LoadOne:            &loads[0],
		LoadFive:           &loads[1],
		LoadFifteen:        &loads[2],
		MemoryTotalBytes:   &memory.total,
		MemoryUsedBytes:    &memory.used,
		MemoryUsage:        percentage(memory.used, memory.total),
		SwapTotalBytes:     &memory.swapTotal,
		SwapUsedBytes:      &memory.swapUsed,
		SwapUsage:          percentage(memory.swapUsed, memory.swapTotal),
		LoggedInUsers:      r.LoggedInUsers(),
		TemperatureCelsius: r.Temperature(),
		CPUFrequencyHz:     hardware.CPUFrequencyHz,
		CPUMaxFrequencyHz:  hardware.CPUMaxFrequencyHz,
		ThrottlingFlags:    hardware.ThrottlingFlags,
		Filesystems:        filesystems,
	}
	if snapshot.Filesystems == nil {
		snapshot.Filesystems = []Filesystem{}
	}
	snapshot.Databases = []Database{}
	if r.Databases != nil {
		snapshot.Databases = r.Databases()
	}
	snapshot.applyNetworkRates(network, prevNetwork, elapsed)
	snapshot.applyProcesses(processes, prevProcesses, cpu, prevCPU, memory.total)

	return snapshot, Counters{CPU: cpu, Network: network, Processes: processes}, nil
}

func (r *Reader) readProc(name string) (string, error) {
	path := filepath.Join(r.ProcRoot, name)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("PiPulse requires Linux procfs; %s was not found", path)
	}
	return string(data), err
}

func (r *Reader) cpuCounters() (cpuCounters, error) {
	stat, err := r.readProc("stat")
	if err != nil {
		return cpuCounters{}, err
	}
	for _, line := range strings.Split(stat, "\n") {
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)[1:]
		if len(fields) > 8 {
			fields = fields[:8]
		}
		var counters cpuCounters
		for index, field := range fields {
			value, err := strconv.ParseInt(field, 10, 64)
			if err != nil {
				return cpuCounters{}, fmt.Errorf("invalid CPU counter %q: %w", field, err)
			}
			counters.total += value
			if index == 3 || index == 4 { // idle and iowait
				counters.idle += value
			}
		}
		return counters, nil
	}
	return cpuCounters{}, errors.New("missing aggregate CPU counters in /proc/stat")
}

func cpuUsage(current cpuCounters, previous *cpuCounters) *float64 {
	if previous == nil {
		return nil
	}
	totalDelta := current.total - previous.total
	idleDelta := max(current.idle-previous.idle, 0)
	if totalDelta <= 0 {
		return nil
	}
	usage := round(clamp(float64(totalDelta-idleDelta)/float64(totalDelta)*100, 0, 100), 2)
	return &usage
}

func (r *Reader) loadAverages() ([3]float64, error) {
	data, err := r.readProc("loadavg")
	if err != nil {
		return [3]float64{}, err
	}
	fields := strings.Fields(data)
	if len(fields) < 3 {
		return [3]float64{}, errors.New("unexpected /proc/loadavg format")
	}
	var loads [3]float64
	for index := range loads {
		if loads[index], err = strconv.ParseFloat(fields[index], 64); err != nil {
			return [3]float64{}, fmt.Errorf("invalid load average %q: %w", fields[index], err)
		}
	}
	return loads, nil
}

type memoryUsage struct{ total, used, swapTotal, swapUsed int64 }

func (r *Reader) memory() (memoryUsage, error) {
	data, err := r.readProc("meminfo")
	if err != nil {
		return memoryUsage{}, err
	}
	values := map[string]int64{}
	for _, line := range strings.Split(data, "\n") {
		name, amount, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		fields := strings.Fields(amount)
		if len(fields) == 0 {
			continue
		}
		kilobytes, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			continue
		}
		values[name] = kilobytes * 1024
	}

	total, ok := values["MemTotal"]
	if !ok {
		return memoryUsage{}, errors.New("missing MemTotal in /proc/meminfo")
	}
	available, ok := values["MemAvailable"]
	if !ok {
		available = values["MemFree"] + values["Buffers"] + values["Cached"]
	}
	swapTotal := values["SwapTotal"]
	return memoryUsage{
		total:     total,
		used:      max(total-available, 0),
		swapTotal: swapTotal,
		swapUsed:  max(swapTotal-values["SwapFree"], 0),
	}, nil
}

func (r *Reader) networkCounters() (map[string]networkCounters, error) {
	data, err := r.readProc("net/dev")
	if err != nil {
		return nil, err
	}
	counters := map[string]networkCounters{}
	lines := strings.Split(data, "\n")
	for _, line := range lines[min(2, len(lines)):] {
		name, values, found := strings.Cut(strings.TrimSpace(line), ":")
		if !found || name == "lo" {
			continue
		}
		fields := strings.Fields(values)
		if len(fields) < 9 {
			continue
		}
		received, errReceived := strconv.ParseInt(fields[0], 10, 64)
		transmitted, errTransmitted := strconv.ParseInt(fields[8], 10, 64)
		if errReceived != nil || errTransmitted != nil {
			continue
		}
		counters[name] = networkCounters{received: received, transmitted: transmitted}
	}
	return counters, nil
}

func (s *Snapshot) applyNetworkRates(current, previous map[string]networkCounters, elapsed time.Duration) {
	var totalReceived, totalTransmitted int64
	s.NetworkInterfaces = map[string]InterfaceRates{}
	for name, counters := range current {
		old, found := previous[name]
		rates := InterfaceRates{}
		if found {
			rates.ReceivedBytesPerSecond = rate(counters.received, old.received, elapsed)
			rates.TransmittedBytesPerSecond = rate(counters.transmitted, old.transmitted, elapsed)
		}
		s.NetworkInterfaces[name] = rates
		totalReceived += rates.ReceivedBytesPerSecond
		totalTransmitted += rates.TransmittedBytesPerSecond
	}
	s.NetworkReceivedBytesPerSecond = &totalReceived
	s.NetworkTransmittedBytesPerSecond = &totalTransmitted
}

func rate(current, previous int64, elapsed time.Duration) int64 {
	delta := current - previous
	if elapsed <= 0 || delta < 0 {
		return 0
	}
	return int64(math.Round(float64(delta) / elapsed.Seconds()))
}

func (r *Reader) processCounters() map[int]processCounters {
	counters := map[int]processCounters{}
	entries, err := os.ReadDir(r.ProcRoot)
	if err != nil {
		return counters
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		if counter, ok := r.processCounter(filepath.Join(r.ProcRoot, entry.Name())); ok {
			counters[pid] = counter
		}
	}
	return counters
}

// processCounter reads utime+stime and resident memory for one process. The
// name sits between the first "(" and the last ")" because it may contain
// spaces or parentheses itself.
func (r *Reader) processCounter(directory string) (processCounters, bool) {
	stat, err := os.ReadFile(filepath.Join(directory, "stat"))
	if err != nil {
		return processCounters{}, false
	}
	text := strings.TrimSpace(string(stat))
	open, closing := strings.Index(text, "("), strings.LastIndex(text, ")")
	if open < 0 || closing < open || closing+2 > len(text) {
		return processCounters{}, false
	}
	fields := strings.Fields(text[closing+1:])
	if len(fields) < 13 {
		return processCounters{}, false
	}
	utime, errUser := strconv.ParseInt(fields[11], 10, 64)
	stime, errSystem := strconv.ParseInt(fields[12], 10, 64)
	if errUser != nil || errSystem != nil {
		return processCounters{}, false
	}

	statm, err := os.ReadFile(filepath.Join(directory, "statm"))
	if err != nil {
		return processCounters{}, false
	}
	statmFields := strings.Fields(string(statm))
	if len(statmFields) < 2 {
		return processCounters{}, false
	}
	residentPages, err := strconv.ParseInt(statmFields[1], 10, 64)
	if err != nil {
		return processCounters{}, false
	}

	return processCounters{
		name:        text[open+1 : closing],
		ticks:       utime + stime,
		memoryBytes: residentPages * r.PageSize,
	}, true
}

func (s *Snapshot) applyProcesses(current, previous map[int]processCounters, cpu cpuCounters, previousCPU *cpuCounters, memoryTotal int64) {
	var totalDelta int64
	if previousCPU != nil {
		totalDelta = cpu.total - previousCPU.total
	}

	processes := make([]Process, 0, len(current))
	for pid, values := range current {
		process := Process{PID: pid, Name: values.name, MemoryBytes: values.memoryBytes}
		if memoryTotal > 0 {
			process.MemoryUsage = round(float64(values.memoryBytes)/float64(memoryTotal)*100, 2)
		}
		if old, found := previous[pid]; found && old.name == values.name && totalDelta > 0 {
			if tickDelta := values.ticks - old.ticks; tickDelta >= 0 {
				usage := round(clamp(float64(tickDelta)/float64(totalDelta)*100, 0, 100), 2)
				process.CPUUsage = &usage
			}
		}
		processes = append(processes, process)
	}

	byCPU := make([]Process, 0, len(processes))
	for _, process := range processes {
		if process.CPUUsage != nil {
			byCPU = append(byCPU, process)
		}
	}
	sort.Slice(byCPU, func(i, j int) bool {
		if *byCPU[i].CPUUsage != *byCPU[j].CPUUsage {
			return *byCPU[i].CPUUsage > *byCPU[j].CPUUsage
		}
		return byCPU[i].PID < byCPU[j].PID
	})
	sort.Slice(processes, func(i, j int) bool {
		if processes[i].MemoryBytes != processes[j].MemoryBytes {
			return processes[i].MemoryBytes > processes[j].MemoryBytes
		}
		return processes[i].PID < processes[j].PID
	})

	s.TopCPUProcesses = byCPU[:min(topProcesses, len(byCPU))]
	s.TopMemoryProcesses = processes[:min(topProcesses, len(processes))]
}

// ignoredFilesystemTypes are virtual filesystems that df users rarely care
// about. Filesystems reporting zero blocks are skipped as well.
var ignoredFilesystemTypes = map[string]bool{
	"tmpfs": true, "devtmpfs": true, "squashfs": true, "overlay": true, "proc": true,
	"sysfs": true, "cgroup": true, "cgroup2": true, "debugfs": true, "tracefs": true,
	"securityfs": true, "pstore": true, "configfs": true, "fusectl": true, "autofs": true,
	"devpts": true, "mqueue": true, "hugetlbfs": true, "bpf": true, "ramfs": true,
	"binfmt_misc": true, "rpc_pipefs": true, "nsfs": true, "efivarfs": true,
}

// ReadFilesystems lists mounted physical filesystems with their capacity,
// like `df -PT`. Bind mounts of the same device are reported once, under
// their shortest mountpoint.
func ReadFilesystems(mountsPath string) ([]Filesystem, error) {
	file, err := os.Open(mountsPath)
	if err != nil {
		return nil, fmt.Errorf("unable to read filesystems: %w", err)
	}
	defer file.Close()

	bySource := map[string]Filesystem{}
	var order []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 || ignoredFilesystemTypes[fields[2]] {
			continue
		}
		source, mountpoint, kind := unescapeMount(fields[0]), unescapeMount(fields[1]), fields[2]

		var stat syscall.Statfs_t
		if err := syscall.Statfs(mountpoint, &stat); err != nil || stat.Blocks == 0 {
			continue
		}
		blockSize := int64(stat.Bsize)
		total := int64(stat.Blocks) * blockSize
		used := (int64(stat.Blocks) - int64(stat.Bfree)) * blockSize
		available := int64(stat.Bavail) * blockSize
		usage := 0.0
		if used+available > 0 {
			usage = math.Ceil(float64(used) * 100 / float64(used+available))
		}

		filesystem := Filesystem{
			Source: source, Type: kind, Mountpoint: mountpoint,
			TotalBytes: total, UsedBytes: used, AvailableBytes: available, Usage: usage,
		}
		existing, seen := bySource[source]
		if !seen {
			order = append(order, source)
		}
		if !seen || len(mountpoint) < len(existing.Mountpoint) {
			bySource[source] = filesystem
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("unable to read filesystems: %w", err)
	}

	filesystems := make([]Filesystem, 0, len(order))
	for _, source := range order {
		filesystems = append(filesystems, bySource[source])
	}
	return filesystems, nil
}

// unescapeMount decodes the octal escapes (\040 for a space) used in
// /proc/mounts.
func unescapeMount(value string) string {
	if !strings.Contains(value, `\`) {
		return value
	}
	var builder strings.Builder
	for index := 0; index < len(value); index++ {
		if value[index] == '\\' && index+3 < len(value) {
			if code, err := strconv.ParseUint(value[index+1:index+4], 8, 8); err == nil {
				builder.WriteByte(byte(code))
				index += 3
				continue
			}
		}
		builder.WriteByte(value[index])
	}
	return builder.String()
}

// CountLoggedInUsers returns the number of distinct users reported by `who`,
// or 0 when it is unavailable.
func CountLoggedInUsers() int {
	output, err := exec.Command("who").Output()
	if err != nil {
		return 0
	}
	users := map[string]bool{}
	for _, line := range strings.Split(string(output), "\n") {
		if fields := strings.Fields(line); len(fields) > 0 {
			users[fields[0]] = true
		}
	}
	return len(users)
}

func readTemperature(path string) *float64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	millidegrees, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
	if err != nil {
		return nil
	}
	celsius := round(millidegrees/1000, 1)
	return &celsius
}

// readInteger parses decimal or 0x-prefixed hexadecimal values such as the
// firmware's get_throttled output.
func readInteger(path string) *int64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	value, err := strconv.ParseInt(strings.TrimSpace(string(data)), 0, 64)
	if err != nil {
		return nil
	}
	return &value
}

func scale(value *int64, factor int64) *int64 {
	if value == nil {
		return nil
	}
	scaled := *value * factor
	return &scaled
}

func percentage(used, total int64) *float64 {
	if total == 0 {
		return nil
	}
	value := round(float64(used)/float64(total)*100, 2)
	return &value
}

func round(value float64, places int) float64 {
	factor := math.Pow(10, float64(places))
	return math.Round(value*factor) / factor
}

func clamp(value, low, high float64) float64 {
	return math.Max(low, math.Min(high, value))
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
