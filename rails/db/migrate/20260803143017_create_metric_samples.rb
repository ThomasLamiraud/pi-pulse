class CreateMetricSamples < ActiveRecord::Migration[8.1]
  def change
    create_table :metric_samples do |t|
      t.datetime :sampled_at, null: false
      t.string :status, null: false, default: "ok"
      t.float :cpu_usage
      t.float :load_one
      t.float :load_five
      t.float :load_fifteen
      t.integer :memory_used_bytes
      t.integer :memory_total_bytes
      t.integer :swap_used_bytes
      t.integer :swap_total_bytes
      t.integer :network_received_bytes_per_second
      t.integer :network_transmitted_bytes_per_second
      t.json :network_interfaces, null: false, default: {}
      t.json :filesystems, null: false, default: []
      t.text :error_message

      t.timestamps
    end

    add_index :metric_samples, :sampled_at
  end
end
