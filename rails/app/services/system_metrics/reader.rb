require "etc"
require "open3"

module SystemMetrics
  class Reader
    def initialize(
      proc_root: ENV.fetch("PROC_ROOT", "/proc"),
      filesystem_reader: FilesystemReader.new(root: ENV["FILESYSTEM_ROOT"]),
      login_reader: LoginReader.new(path: ENV["UTMP_PATH"]),
      thermal_reader: ThermalReader.new(path: ENV["THERMAL_PATH"]),
      hardware_reader: HardwareReader.new(
        frequency_path: ENV["CPU_FREQUENCY_PATH"],
        max_frequency_path: ENV["CPU_MAX_FREQUENCY_PATH"],
        throttling_path: ENV["THROTTLING_PATH"]
      )
    )
      @proc_root = Pathname(proc_root)
      @filesystem_reader = filesystem_reader
      @login_reader = login_reader
      @thermal_reader = thermal_reader
      @hardware_reader = hardware_reader
    end

    def read(previous: nil, elapsed: nil)
      cpu = cpu_counters
      network = network_counters
      memory = memory_usage
      processes = process_counters

      {
        cpu_usage: cpu_usage(cpu, previous&.dig(:cpu)),
        **load_averages,
        **memory,
        **network_rates(network, previous&.dig(:network), elapsed),
        **process_metrics(processes, previous&.dig(:processes), cpu, previous&.dig(:cpu), memory[:memory_total_bytes]),
        logged_in_users: @login_reader.count,
        temperature_celsius: @thermal_reader.read,
        **@hardware_reader.read,
        filesystems: @filesystem_reader.read,
        counters: { cpu: cpu, network: network, processes: processes }
      }
    end

    private

    def cpu_counters
      line = read_proc("stat").each_line.find { |entry| entry.start_with?("cpu ") }
      raise "Missing aggregate CPU counters in /proc/stat" unless line

      values = line.split.drop(1).first(8).map { Integer(_1, 10) }
      {
        total: values.sum,
        idle: values.fetch(3, 0) + values.fetch(4, 0)
      }
    end

    def cpu_usage(current, previous)
      return unless previous

      total_delta = current[:total] - previous[:total]
      idle_delta = current[:idle] - previous[:idle]
      return if total_delta <= 0

      ((total_delta - [ idle_delta, 0 ].max).to_f / total_delta * 100).clamp(0, 100).round(2)
    end

    def load_averages
      values = read_proc("loadavg").split.first(3).map { Float(_1) }
      {
        load_one: values[0],
        load_five: values[1],
        load_fifteen: values[2]
      }
    end

    def memory_usage
      values = read_proc("meminfo").each_line.to_h do |line|
        name, amount = line.split(":", 2)
        [ name, amount.to_s[/\d+/].to_i * 1024 ]
      end

      total = values.fetch("MemTotal")
      available = values["MemAvailable"] ||
        values.fetch("MemFree", 0) + values.fetch("Buffers", 0) + values.fetch("Cached", 0)
      swap_total = values.fetch("SwapTotal", 0)

      {
        memory_total_bytes: total,
        memory_used_bytes: [ total - available, 0 ].max,
        swap_total_bytes: swap_total,
        swap_used_bytes: [ swap_total - values.fetch("SwapFree", 0), 0 ].max
      }
    end

    def process_counters
      @proc_root.children.filter_map do |directory|
        next unless directory.basename.to_s.match?(/\A\d+\z/)

        process_counter(directory)
      end.to_h
    rescue Errno::ENOENT
      {}
    end

    def process_counter(directory)
      stat = (directory / "stat").read
      match = stat.strip.match(/\A(\d+) \((.*)\) \S (.*)\z/)
      return unless match

      fields = match[3].split
      pid = Integer(match[1], 10)
      [
        pid,
        {
          name: match[2],
          ticks: Integer(fields.fetch(10), 10) + Integer(fields.fetch(11), 10),
          memory_bytes: Integer((directory / "statm").read.split.fetch(1), 10) * Etc.sysconf(Etc::SC_PAGESIZE)
        }
      ]
    rescue Errno::EACCES, Errno::ENOENT, IndexError, ArgumentError
      nil
    end

    def process_metrics(current, previous, cpu, previous_cpu, memory_total)
      total_delta = previous_cpu && cpu[:total] - previous_cpu[:total]
      processes = current.map do |pid, values|
        old = previous&.fetch(pid, nil)
        tick_delta = old && old[:name] == values[:name] ? values[:ticks] - old[:ticks] : nil
        cpu_usage = if !tick_delta.nil? && tick_delta >= 0 && total_delta.to_i.positive?
          (tick_delta.to_f / total_delta * 100).clamp(0, 100).round(2)
        end

        {
          "pid" => pid,
          "name" => values[:name],
          "cpu_usage" => cpu_usage,
          "memory_bytes" => values[:memory_bytes],
          "memory_usage" => (values[:memory_bytes].to_f / memory_total * 100).round(2)
        }
      end

      {
        top_cpu_processes: processes.select { _1["cpu_usage"] }.max_by(5) { _1["cpu_usage"] },
        top_memory_processes: processes.max_by(5) { _1["memory_bytes"] }
      }
    end

    def network_counters
      read_proc("net/dev").each_line.drop(2).filter_map do |line|
        interface, values = line.strip.split(":", 2)
        next if interface == "lo"

        fields = values.split.map { Integer(_1, 10) }
        [ interface, { received: fields.fetch(0), transmitted: fields.fetch(8) } ]
      end.to_h
    end

    def network_rates(current, previous, elapsed)
      interfaces = current.to_h do |interface, counters|
        old = previous&.fetch(interface, nil)
        received = rate(counters[:received], old&.dig(:received), elapsed)
        transmitted = rate(counters[:transmitted], old&.dig(:transmitted), elapsed)

        [
          interface,
          {
            "received_bytes_per_second" => received,
            "transmitted_bytes_per_second" => transmitted
          }
        ]
      end

      {
        network_received_bytes_per_second: interfaces.sum { |_name, rates| rates["received_bytes_per_second"] },
        network_transmitted_bytes_per_second: interfaces.sum { |_name, rates| rates["transmitted_bytes_per_second"] },
        network_interfaces: interfaces
      }
    end

    def rate(current, previous, elapsed)
      return 0 unless previous && elapsed.to_f.positive?

      delta = current - previous
      return 0 if delta.negative?

      (delta / elapsed).round
    end

    def read_proc(relative_path)
      (@proc_root / relative_path).read
    rescue Errno::ENOENT
      raise "PiPulse requires Linux procfs; #{@proc_root / relative_path} was not found"
    end

    class FilesystemReader
      IGNORED_TYPES = %w[
        tmpfs devtmpfs squashfs overlay proc sysfs cgroup cgroup2
        debugfs tracefs securityfs pstore configfs fusectl
      ].freeze

      def initialize(root: nil)
        @root = root
      end

      def read
        command = [ "df", "-PT", "-B1" ]
        command << @root if @root
        stdout, stderr, status = Open3.capture3(
          { "LANG" => "C" },
          *command
        )
        raise "Unable to read filesystems: #{stderr.strip}" unless status.success?

        stdout.each_line.drop(1).filter_map do |line|
          source, type, total, used, available, percentage, mountpoint = line.split(/\s+/, 7)
          next if IGNORED_TYPES.include?(type)

          mountpoint = mountpoint.to_s.strip
          mountpoint = "/" if @root && mountpoint == @root

          {
            "source" => source,
            "type" => type,
            "mountpoint" => mountpoint,
            "total_bytes" => total.to_i,
            "used_bytes" => used.to_i,
            "available_bytes" => available.to_i,
            "usage" => percentage.to_s.delete("%").to_f
          }
        end
      end
    end

    class LoginReader
      def initialize(path: nil)
        @path = path
      end

      def count
        return 0 if @path && !File.exist?(@path)

        command = [ "who" ]
        command << @path if @path
        stdout, _stderr, status = Open3.capture3(*command)
        return 0 unless status.success?

        stdout.each_line.filter_map { _1.split.first }.uniq.count
      end
    end

    class ThermalReader
      DEFAULT_PATH = "/sys/class/thermal/thermal_zone0/temp"

      def initialize(path: nil)
        @path = Pathname(path || DEFAULT_PATH)
      end

      def read
        return unless @path.file?

        (Float(@path.read.strip) / 1_000).round(1)
      rescue ArgumentError, Errno::EACCES, Errno::ENOENT
        nil
      end
    end

    class HardwareReader
      DEFAULT_FREQUENCY_PATH = "/sys/devices/system/cpu/cpu0/cpufreq/scaling_cur_freq"
      DEFAULT_MAX_FREQUENCY_PATH = "/sys/devices/system/cpu/cpu0/cpufreq/cpuinfo_max_freq"
      DEFAULT_THROTTLING_PATH = "/sys/devices/platform/soc/soc:firmware/get_throttled"

      def initialize(frequency_path: nil, max_frequency_path: nil, throttling_path: nil)
        @frequency_path = Pathname(frequency_path || DEFAULT_FREQUENCY_PATH)
        @max_frequency_path = Pathname(max_frequency_path || DEFAULT_MAX_FREQUENCY_PATH)
        @throttling_path = Pathname(throttling_path || DEFAULT_THROTTLING_PATH)
      end

      def read
        {
          cpu_frequency_hz: read_integer(@frequency_path)&.*(1_000),
          cpu_max_frequency_hz: read_integer(@max_frequency_path)&.*(1_000),
          throttling_flags: read_integer(@throttling_path)
        }
      end

      private

      def read_integer(path)
        return unless path.file?

        Integer(path.read.strip, 0)
      rescue ArgumentError, Errno::EACCES, Errno::ENOENT
        nil
      end
    end
  end
end
