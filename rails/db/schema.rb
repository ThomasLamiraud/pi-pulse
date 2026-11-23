# This file is auto-generated from the current state of the database. Instead
# of editing this file, please use the migrations feature of Active Record to
# incrementally modify your database, and then regenerate this schema definition.
#
# This file is the source Rails uses to define your schema when running `bin/rails
# db:schema:load`. When creating a new database, `bin/rails db:schema:load` tends to
# be faster and is potentially less error prone than running all of your
# migrations from scratch. Old migrations may fail to apply correctly if those
# migrations use external dependencies or application code.
#
# It's strongly recommended that you check this file into your version control system.

ActiveRecord::Schema[8.1].define(version: 2026_08_03_215000) do
  create_table "metric_samples", force: :cascade do |t|
    t.integer "cpu_frequency_hz"
    t.integer "cpu_max_frequency_hz"
    t.float "cpu_usage"
    t.datetime "created_at", null: false
    t.text "error_message"
    t.json "filesystems", default: [], null: false
    t.float "load_fifteen"
    t.float "load_five"
    t.float "load_one"
    t.integer "logged_in_users", default: 0, null: false
    t.integer "memory_total_bytes"
    t.integer "memory_used_bytes"
    t.json "network_interfaces", default: {}, null: false
    t.integer "network_received_bytes_per_second"
    t.integer "network_transmitted_bytes_per_second"
    t.datetime "sampled_at", null: false
    t.string "status", default: "ok", null: false
    t.integer "swap_total_bytes"
    t.integer "swap_used_bytes"
    t.float "temperature_celsius"
    t.integer "throttling_flags"
    t.json "top_cpu_processes", default: [], null: false
    t.json "top_memory_processes", default: [], null: false
    t.datetime "updated_at", null: false
    t.index ["sampled_at"], name: "index_metric_samples_on_sampled_at"
  end
end
