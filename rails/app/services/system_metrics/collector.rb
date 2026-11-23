module SystemMetrics
  class Collector
    PRUNE_INTERVAL = 10.minutes

    def initialize(
      reader: Reader.new,
      monotonic_clock: -> { Process.clock_gettime(Process::CLOCK_MONOTONIC) },
      wall_clock: -> { Time.current }
    )
      @reader = reader
      @monotonic_clock = monotonic_clock
      @wall_clock = wall_clock
      @previous_counters = nil
      @previous_at = nil
      @last_pruned_at = nil
    end

    def sample!
      now = @monotonic_clock.call
      result = @reader.read(
        previous: @previous_counters,
        elapsed: @previous_at && now - @previous_at
      )
      counters = result.delete(:counters)

      sample = MetricSample.create!(
        **result,
        sampled_at: @wall_clock.call,
        status: "ok"
      )

      @previous_counters = counters
      @previous_at = now
      prune_if_due(now)
      sample
    rescue StandardError => error
      record_failure(error)
    end

    private

    def prune_if_due(now)
      return if @last_pruned_at && now - @last_pruned_at < PRUNE_INTERVAL

      MetricSample.prune!
      @last_pruned_at = now
    end

    def record_failure(error)
      Rails.logger.error("Metric collection failed: #{error.class}: #{error.message}")
      MetricSample.create!(
        sampled_at: @wall_clock.call,
        status: "error",
        error_message: "#{error.class}: #{error.message}".truncate(MetricSample::MAX_ERROR_LENGTH)
      )
    end
  end
end
