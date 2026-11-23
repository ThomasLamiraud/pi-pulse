class AddTemperatureToMetricSamples < ActiveRecord::Migration[8.1]
  def change
    add_column :metric_samples, :temperature_celsius, :float
  end
end
