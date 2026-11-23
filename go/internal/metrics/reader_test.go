package metrics

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixtureReader(fixture string) *Reader {
	temperature := 52.4
	frequency := int64(800_000_000)
	flags := int64(0)
	return &Reader{
		ProcRoot: filepath.Join("testdata", fixture),
		PageSize: 4096,
		Filesystems: func() ([]Filesystem, error) {
			return []Filesystem{{
				Source: "/dev/root", Type: "ext4", Mountpoint: "/",
				TotalBytes: 1_000_000, UsedBytes: 500_000, AvailableBytes: 500_000, Usage: 50,
			}}, nil
		},
		LoggedInUsers: func() int { return 2 },
		Temperature:   func() *float64 { return &temperature },
		Hardware: func() Hardware {
			return Hardware{CPUFrequencyHz: &frequency, CPUMaxFrequencyHz: &frequency, ThrottlingFlags: &flags}
		},
	}
}

func TestReadsMemoryLoadNetworkAndFilesystems(t *testing.T) {
	snapshot, _, err := fixtureReader("proc").Read(nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	if snapshot.CPUUsage != nil {
		t.Errorf("CPU usage = %v, want nil on the first reading", *snapshot.CPUUsage)
	}
	assertEqual(t, "load one", *snapshot.LoadOne, 0.42)
	assertEqual(t, "memory total", *snapshot.MemoryTotalBytes, int64(1_024_000_000))
	assertEqual(t, "memory used", *snapshot.MemoryUsedBytes, int64(614_400_000))
	assertEqual(t, "memory usage", *snapshot.MemoryUsage, 60.0)
	assertEqual(t, "swap usage", *snapshot.SwapUsage, 25.0)
	assertEqual(t, "network received", *snapshot.NetworkReceivedBytesPerSecond, int64(0))
	assertEqual(t, "filesystem source", snapshot.Filesystems[0].Source, "/dev/root")
	if _, found := snapshot.NetworkInterfaces["lo"]; found {
		t.Error("loopback interface should be ignored")
	}
	assertEqual(t, "logged in users", snapshot.LoggedInUsers, 2)
	assertEqual(t, "temperature", *snapshot.TemperatureCelsius, 52.4)
	assertEqual(t, "CPU frequency", *snapshot.CPUFrequencyHz, int64(800_000_000))
	assertEqual(t, "throttling flags", *snapshot.ThrottlingFlags, int64(0))
	assertEqual(t, "top memory process", snapshot.TopMemoryProcesses[0].Name, "puma")
	assertEqual(t, "top memory bytes", snapshot.TopMemoryProcesses[0].MemoryBytes, int64(25*4096))
}

func TestCalculatesRatesFromDeltas(t *testing.T) {
	_, counters, err := fixtureReader("proc").Read(nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := fixtureReader("proc_next").Read(&counters, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	assertEqual(t, "CPU usage", *snapshot.CPUUsage, 60.0)
	assertEqual(t, "received", *snapshot.NetworkReceivedBytesPerSecond, int64(200))
	assertEqual(t, "transmitted", *snapshot.NetworkTransmittedBytesPerSecond, int64(120))
	assertEqual(t, "eth0 received", snapshot.NetworkInterfaces["eth0"].ReceivedBytesPerSecond, int64(200))
	assertEqual(t, "top CPU process", *snapshot.TopCPUProcesses[0].CPUUsage, 50.0)
}

func TestExplainsWhenProcfsIsUnavailable(t *testing.T) {
	_, _, err := fixtureReader("missing").Read(nil, 0)
	if err == nil || !strings.Contains(err.Error(), "requires Linux procfs") {
		t.Fatalf("error = %v, want a procfs explanation", err)
	}
}

func TestParsesProcessNamesWithParentheses(t *testing.T) {
	directory := t.TempDir()
	write(t, filepath.Join(directory, "stat"), "7 (my (odd) app) S 1 1 1 0 -1 0 0 0 0 0 3 4 0 0 20 0 1 0")
	write(t, filepath.Join(directory, "statm"), "10 2")

	counter, ok := (&Reader{PageSize: 4096}).processCounter(directory)
	if !ok {
		t.Fatal("process was not parsed")
	}
	assertEqual(t, "name", counter.name, "my (odd) app")
	assertEqual(t, "ticks", counter.ticks, int64(7))
	assertEqual(t, "memory", counter.memoryBytes, int64(8192))
}

func TestReadsHexadecimalThrottlingFlags(t *testing.T) {
	path := filepath.Join(t.TempDir(), "get_throttled")
	write(t, path, "0x50005\n")
	assertEqual(t, "flags", *readInteger(path), int64(0x50005))
}

func TestUnescapesMountPaths(t *testing.T) {
	assertEqual(t, "mountpoint", unescapeMount(`/media/my\040disk`), "/media/my disk")
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertEqual[T comparable](t *testing.T, name string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}
