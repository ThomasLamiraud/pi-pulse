class AddHardwareHealthToMetricSamples < ActiveRecord::Migration[8.1]
  def change
    add_column :metric_samples, :cpu_frequency_hz, :integer
    add_column :metric_samples, :cpu_max_frequency_hz, :integer
    add_column :metric_samples, :throttling_flags, :integer
  end
end
