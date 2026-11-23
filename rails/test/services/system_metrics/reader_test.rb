require "test_helper"

module SystemMetrics
  class ReaderTest < ActiveSupport::TestCase
    FakeFilesystemReader = Struct.new(:filesystems) do
      def read
        filesystems
      end
    end
    FakeLoginReader = Struct.new(:count)
    FakeThermalReader = Struct.new(:temperature) do
      def read
        temperature
      end
    end
    FakeHardwareReader = Struct.new(:metrics) do
      def read
        metrics
      end
    end

    test "reads memory, load, network and filesystems from Linux sources" do
      reader = build_reader("proc")
      result = reader.read

      assert_nil result[:cpu_usage]
      assert_equal 0.42, result[:load_one]
      assert_equal 1_024_000_000, result[:memory_total_bytes]
      assert_equal 614_400_000, result[:memory_used_bytes]
      assert_equal 0, result[:network_received_bytes_per_second]
      assert_equal "/dev/root", result[:filesystems].first["source"]
      assert_not result[:network_interfaces].key?("lo")
      assert_equal 2, result[:logged_in_users]
      assert_equal 52.4, result[:temperature_celsius]
      assert_equal 800_000_000, result[:cpu_frequency_hz]
      assert_equal 0, result[:throttling_flags]
      assert_equal "puma", result[:top_memory_processes].first["name"]
    end

    test "calculates CPU utilization and per-second network rates from deltas" do
      first = build_reader("proc").read
      second = build_reader("proc_next").read(previous: first[:counters], elapsed: 5)

      assert_equal 60.0, second[:cpu_usage]
      assert_equal 200, second[:network_received_bytes_per_second]
      assert_equal 120, second[:network_transmitted_bytes_per_second]
      assert_equal 200, second[:network_interfaces].dig("eth0", "received_bytes_per_second")
      assert_equal 50.0, second[:top_cpu_processes].first["cpu_usage"]
    end

    test "explains when procfs is unavailable" do
      reader = Reader.new(
        proc_root: Rails.root.join("test/fixtures/missing"),
        filesystem_reader: FakeFilesystemReader.new([]),
        login_reader: FakeLoginReader.new(0),
        thermal_reader: FakeThermalReader.new(nil),
        hardware_reader: FakeHardwareReader.new({})
      )

      error = assert_raises(RuntimeError) { reader.read }
      assert_match "requires Linux procfs", error.message
    end

    private

    def build_reader(fixture)
      Reader.new(
        proc_root: Rails.root.join("test/fixtures", fixture),
        filesystem_reader: FakeFilesystemReader.new(
          [
            {
              "source" => "/dev/root",
              "type" => "ext4",
              "mountpoint" => "/",
              "total_bytes" => 1_000_000,
              "used_bytes" => 500_000,
              "available_bytes" => 500_000,
              "usage" => 50.0
            }
          ]
        ),
        login_reader: FakeLoginReader.new(2),
        thermal_reader: FakeThermalReader.new(52.4),
        hardware_reader: FakeHardwareReader.new({
          cpu_frequency_hz: 800_000_000,
          cpu_max_frequency_hz: 800_000_000,
          throttling_flags: 0
        })
      )
    end
  end
end
