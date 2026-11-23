require "test_helper"

class MetricSampleTest < ActiveSupport::TestCase
  test "calculates memory and swap percentages" do
    sample = MetricSample.new(
      memory_used_bytes: 3,
      memory_total_bytes: 4,
      swap_used_bytes: 1,
      swap_total_bytes: 2
    )

    assert_equal 75.0, sample.memory_usage
    assert_equal 50.0, sample.swap_usage
  end

  test "includes swap usage in chart payload" do
    sample = MetricSample.new(
      sampled_at: Time.zone.parse("2026-08-03 12:00:00"),
      swap_used_bytes: 1,
      swap_total_bytes: 4
    )

    assert_equal 25.0, sample.chart_payload[:swap_usage]
  end

  test "reports whether a sample is fresh" do
    assert metric_samples(:current).fresh?
    assert_not metric_samples(:expired).fresh?
  end

  test "decodes current and historical hardware flags" do
    sample = MetricSample.new(throttling_flags: (1 << 0) | (1 << 2) | (1 << 18))

    assert sample.under_voltage?
    assert sample.throttled?
    assert sample.past_hardware_issue?
    assert_not sample.frequency_capped?
    assert_not sample.soft_temperature_limit?
  end

  test "prunes samples beyond retention" do
    expired_id = metric_samples(:expired).id

    assert_difference("MetricSample.count", -1) { MetricSample.prune! }
    assert_not MetricSample.exists?(expired_id)
  end

  test "requires a known status" do
    sample = MetricSample.new(sampled_at: Time.current, status: "unknown")

    assert_not sample.valid?
    assert_includes sample.errors[:status], "is not included in the list"
  end
end
