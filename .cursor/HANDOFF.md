# PiPulse implementation handoff

## Product decisions

- Product name: **PiPulse**
- Framework: Rails 8.1 with Hotwire, Stimulus, import maps, and Chart.js
- Deployment: Rails and the collector run on one Raspberry Pi
- Access: trusted local network without application authentication
- Sampling: every 5 seconds
- Retention: 24 hours of raw samples
- Storage: SQLite

## Architecture

- `bin/collector` runs as a dedicated long-lived process, separate from Puma.
- `SystemMetrics::Reader` reads Linux `/proc` counters and GNU `df`.
- CPU utilization and network throughput are calculated from counter deltas.
- `SystemMetrics::Collector` persists `MetricSample` records and prunes expired data.
- Model callbacks publish card and chart updates through Turbo Streams and Solid Cable.
- Historical dashboard queries downsample records to approximately 360 chart points.
- Browser-side freshness tracking marks the collector stale after 15 seconds.

## Main implementation paths

- `app/services/system_metrics/reader.rb`
- `app/services/system_metrics/collector.rb`
- `app/models/metric_sample.rb`
- `app/controllers/dashboard_controller.rb`
- `app/views/dashboard/`
- `app/javascript/controllers/dashboard_chart_controller.js`
- `app/javascript/controllers/freshness_controller.js`
- `deploy/pipulse-web.service`
- `deploy/pipulse-collector.service`

## Verification completed

- Rails test suite: 10 tests, 42 assertions, no failures
- RuboCop: no offenses
- Brakeman: no warnings
- Bundler and import-map vulnerability audits: no vulnerabilities
- Production database preparation and asset compilation
- Development HTTP dashboard smoke test
- Linux parser behavior tested with `/proc` fixtures

Physical Raspberry Pi validation still requires deployment to the target device.

## Operational notes

- The collector is Linux-only and intentionally reports a procfs error on macOS.
- PiPulse has no login; port 3000 must remain restricted to the trusted LAN.
- Production setup and systemd installation are documented in `README.md`.
- The completed implementation plan is stored in `.cursor/plans/pipulse.plan.md`.
