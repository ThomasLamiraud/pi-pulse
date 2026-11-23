---
name: PiPulse
overview: Build PiPulse, a separate, lightweight Rails 8 + Hotwire application for one Raspberry Pi, showing 2–5 second live metrics and retaining 24 hours of history. Rails/Hotwire is appropriate at this scale; collection will run outside request handling so dashboard responsiveness and metric sampling remain independent.
todos:
  - id: scaffold-app
    content: Scaffold the separate Rails app and configure its Pi deployment/runtime defaults
    status: completed
  - id: collect-metrics
    content: Implement metric parsing, delta calculations, persistence, retention, and collector service
    status: completed
  - id: build-dashboard
    content: Build the Hotwire dashboard, live Turbo updates, and Stimulus charts
    status: completed
  - id: test-deploy
    content: Add tests, systemd services, documentation, and Raspberry Pi verification
    status: completed
isProject: false
---

# PiPulse

## Architecture
- Create a separate Rails application and repository named `pi-pulse`, with the Rails namespace `PiPulse`, using SQLite, import maps, Turbo, Stimulus, and the Rails defaults suited to a single Pi.
- Keep the dashboard LAN-accessible without application login, bind only to the private network, and document the resulting trust-boundary assumption.
- Implement a Linux-only collector that reads `/proc/stat`, `/proc/loadavg`, `/proc/meminfo`, and `/proc/net/dev`; collect filesystem capacity through a safe system API/library. Compute CPU and network rates from successive samples rather than treating cumulative counters as instantaneous values.
- Run collection every 5 seconds in a dedicated long-running process managed by `systemd`, separate from Puma. Persist samples to SQLite, prune raw samples older than 24 hours, and recover cleanly across restarts or counter resets.

## Data and UI
- Add a compact `MetricSample` model/migration covering CPU utilization and load averages, memory/swap, per-interface network receive/transmit rates, and per-mounted-filesystem capacity usage.
- Build a single dashboard route/controller with current summary cards and 24-hour charts. Use Turbo Streams over Action Cable for live card/sample updates and a small Stimulus controller with Chart.js for incremental charts—Hotwire remains responsible for navigation and server-driven updates.
- Handle unavailable interfaces, virtual filesystems, disconnected disks, and collection failures without taking down the page; show sample freshness and collector status.

## Operations and verification
- Add `systemd` units for Puma and the collector, environment configuration, database preparation, boot startup, and LAN-only deployment guidance.
- Unit-test `/proc` parsers and delta calculations with fixtures, model retention behavior, dashboard rendering, and a collector-to-Turbo integration path.
- Verify on Linux/Raspberry Pi OS, including CPU/memory overhead, database growth over 24 hours, reconnect behavior, and responsive rendering on desktop/mobile.
