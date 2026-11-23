require "test_helper"

module SystemMetrics
  class CollectorTest < ActiveSupport::TestCase
    class FakeReader
      attr_reader :calls

      def initialize(result: nil, error: nil)
        @result = result
        @error = error
        @calls = []
      end

      def read(**arguments)
        @calls << arguments
        raise @error if @error

        @result.deep_dup
      end
    end

    setup do
      MetricSample.delete_all
    end

    test "persists a successful sample and prunes expired data" do
      MetricSample.create!(sampled_at: 25.hours.ago, status: "ok")
      reader = FakeReader.new(result: successful_result)
      monotonic_times = [ 10.0, 15.0 ]
      collector = Collector.new(
        reader:,
        monotonic_clock: -> { monotonic_times.shift },
        wall_clock: -> { Time.zone.local(2026, 8, 3, 12) }
      )

      sample = collector.sample!
      collector.sample!

      assert_equal "ok", sample.status
      assert_equal 35.5, sample.cpu_usage
      assert_equal 52.4, sample.temperature_celsius
      assert_equal 800_000_000, sample.cpu_frequency_hz
      assert_nil reader.calls.first[:previous]
      assert_equal successful_result[:counters], reader.calls.second[:previous]
      assert_equal 5.0, reader.calls.second[:elapsed]
      assert_equal 2, MetricSample.count
    end

    test "records collection failures without crashing the loop" do
      reader = FakeReader.new(error: Errno::ENOENT.new("/proc/stat"))
      collector = Collector.new(
        reader:,
        monotonic_clock: -> { 10.0 },
        wall_clock: -> { Time.zone.local(2026, 8, 3, 12) }
      )

      sample = collector.sample!

      assert_equal "error", sample.status
      assert_match "ENOENT", sample.error_message
    end

    private

    def successful_result
      {
        cpu_usage: 35.5,
        load_one: 0.2,
        load_five: 0.1,
        load_fifteen: 0.05,
        memory_used_bytes: 500,
        memory_total_bytes: 1_000,
        swap_used_bytes: 0,
        swap_total_bytes: 0,
        temperature_celsius: 52.4,
        cpu_frequency_hz: 800_000_000,
        cpu_max_frequency_hz: 800_000_000,
        throttling_flags: 0,
        logged_in_users: 1,
        top_cpu_processes: [],
        top_memory_processes: [],
        network_received_bytes_per_second: 10,
        network_transmitted_bytes_per_second: 5,
        network_interfaces: {},
        filesystems: [],
        counters: { cpu: { total: 100, idle: 60 }, network: {} }
      }
    end
  end
end
