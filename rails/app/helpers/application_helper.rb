module ApplicationHelper
  def metric_percentage(value)
    value.nil? ? "—" : "#{number_with_precision(value, precision: 1, strip_insignificant_zeros: true)}%"
  end

  def metric_bytes(value)
    value.nil? ? "—" : number_to_human_size(value, precision: 3)
  end

  def metric_rate(value)
    value.nil? ? "—" : "#{number_to_human_size(value, precision: 3)}/s"
  end

  def metric_number(value, precision: 2)
    value.nil? ? "—" : number_with_precision(value, precision:, strip_insignificant_zeros: true)
  end

  def metric_temperature(value)
    value.nil? ? "—" : "#{metric_number(value, precision: 1)}°C"
  end

  def metric_frequency(value)
    value.nil? ? "—" : "#{metric_number(value.to_f / 1_000_000, precision: 0)} MHz"
  end

  def hardware_health(sample)
    return [ "Hardware status unavailable", "unknown" ] if sample&.throttling_flags.nil?
    return [ "Undervoltage detected", "warning" ] if sample.under_voltage?
    return [ "CPU is throttled", "warning" ] if sample.throttled?
    return [ "Temperature limit active", "warning" ] if sample.soft_temperature_limit?
    return [ "CPU frequency is capped", "warning" ] if sample.frequency_capped?
    return [ "Past power or thermal issue", "history" ] if sample.past_hardware_issue?

    [ "Power and clocks stable", "stable" ]
  end

  def collector_state(sample)
    return [ "Waiting for collector", "waiting" ] unless sample
    return [ "Collector error", "error" ] if sample.status == "error"
    return [ "Data is stale", "stale" ] unless sample.fresh?

    [ "Live", "live" ]
  end
end
