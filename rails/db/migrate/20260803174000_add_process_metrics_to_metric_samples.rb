class AddProcessMetricsToMetricSamples < ActiveRecord::Migration[8.1]
  def change
    add_column :metric_samples, :logged_in_users, :integer, default: 0, null: false
    add_column :metric_samples, :top_cpu_processes, :json, default: [], null: false
    add_column :metric_samples, :top_memory_processes, :json, default: [], null: false
  end
end
