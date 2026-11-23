class MetricSample < ApplicationRecord
  RETENTION = 24.hours
  MAX_ERROR_LENGTH = 500
  DEFAULT_COLLECTION_INTERVAL = 15
  FRESHNESS_INTERVALS = 3
  UNDER_VOLTAGE_FLAG = 1 << 0
  FREQUENCY_CAPPED_FLAG = 1 << 1
  THROTTLED_FLAG = 1 << 2
  SOFT_TEMPERATURE_LIMIT_FLAG = 1 << 3
  HISTORICAL_THROTTLING_MASK = 0xF0000

  validates :sampled_at, :status, presence: true
  validates :status, inclusion: { in: %w[ok error] }

  scope :chronological, -> { order(:sampled_at) }
  scope :recent, -> { where(sampled_at: RETENTION.ago..) }

  after_create_commit :broadcast_dashboard_update

  def self.history_points(max_points: 360)
    samples = recent.chronological
    count = samples.count
    return samples.to_a if count <= max_points

    stride = (count.to_f / max_points).ceil
    points = samples.where("id % ? = 0", stride).limit(max_points).to_a
    latest = samples.last
    points << latest if latest && points.last&.id != latest.id
    points
  end

  def self.prune!
    where(sampled_at: ...RETENTION.ago).delete_all
  end

  def self.collection_interval
    ENV.fetch("METRIC_INTERVAL", DEFAULT_COLLECTION_INTERVAL).to_f.clamp(1, 60)
  end

  def self.freshness_threshold
    collection_interval * FRESHNESS_INTERVALS
  end

  def fresh?
    sampled_at >= self.class.freshness_threshold.seconds.ago
  end

  def memory_usage
    percentage(memory_used_bytes, memory_total_bytes)
  end

  def swap_usage
    percentage(swap_used_bytes, swap_total_bytes)
  end

  def chart_payload
    {
      sampled_at: sampled_at.iso8601,
      cpu_usage: cpu_usage,
      memory_usage: memory_usage,
      swap_usage: swap_usage,
      temperature_celsius: temperature_celsius,
      load_one: load_one,
      load_five: load_five,
      network_received: network_received_bytes_per_second,
      network_transmitted: network_transmitted_bytes_per_second
    }
  end

  def under_voltage?
    flag_set?(UNDER_VOLTAGE_FLAG)
  end

  def frequency_capped?
    flag_set?(FREQUENCY_CAPPED_FLAG)
  end

  def throttled?
    flag_set?(THROTTLED_FLAG)
  end

  def soft_temperature_limit?
    flag_set?(SOFT_TEMPERATURE_LIMIT_FLAG)
  end

  def past_hardware_issue?
    flag_set?(HISTORICAL_THROTTLING_MASK)
  end

  private

  def flag_set?(mask)
    (throttling_flags.to_i & mask).positive?
  end

  def percentage(used, total)
    return if used.nil? || total.to_i.zero?

    (used.to_f / total * 100).round(2)
  end

  def broadcast_dashboard_update
    broadcast_replace_to(
      "metrics",
      target: "dashboard_metrics",
      partial: "dashboard/metrics",
      locals: { sample: self }
    )
    broadcast_append_to(
      "metrics",
      target: "live_samples",
      partial: "dashboard/live_sample",
      locals: { sample: self }
    )
  rescue StandardError => error
    Rails.logger.error("Metric broadcast failed: #{error.class}: #{error.message}")
  end
end
