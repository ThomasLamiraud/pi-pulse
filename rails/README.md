# PiPulse (Rails)

> This is the original Rails implementation. A lighter [Go version](../go/README.md)
> (~13 MB of RAM instead of ~300–400 MB) lives next to it and is recommended for
> small boards or Pis that also run Pi-hole. Run every command below from this
> `rails/` folder.

PiPulse is a lightweight Raspberry Pi monitoring dashboard built with Rails 8,
Hotwire, SQLite, and Chart.js. It samples the local Linux host every 15 seconds,
updates connected browsers through Turbo Streams, and keeps 24 hours of raw
history.

## Metrics

- CPU utilization plus 1, 5, and 15 minute load averages
- Used and total memory and swap
- Aggregate and per-interface receive/transmit rates
- Capacity usage for physical mounted filesystems
- Collector freshness and collection errors

CPU and network values are calculated from successive kernel counters. The
collector reads `/proc/stat`, `/proc/loadavg`, `/proc/meminfo`, and
`/proc/net/dev`; disk capacity comes from GNU `df`. It is intentionally
Linux-only.

## Local setup

Requirements:

- Ruby 3.4
- SQLite 3
- GNU coreutils on the Raspberry Pi

```sh
bundle install
bin/rails db:prepare
bin/rails test
```

Run the web process and collector in separate terminals:

```sh
bin/rails server
bin/collector
```

The collector will report a clear procfs error on macOS. Parser and collector
tests use Linux fixtures and run on any supported development platform.

Configuration:

- `METRIC_INTERVAL` — sampling interval in seconds, clamped from 1 to 60
- `PORT` — Puma port, default `3000`
- `DATABASE_PATH` — production metrics database path
- `FORCE_SSL` — set to `true` only behind working HTTPS

## Raspberry Pi deployment

The deployment script detects 64-bit ARM, ARMv7, and ARMv6 targets. ARMv6 uses
the compatible `linux/arm/v5` image platform. A Raspberry Pi 3 automatically
uses `linux/arm/v7` with 32-bit Raspberry Pi OS or `linux/arm64` with a 64-bit
OS.

Docker deployment defaults to the original Pi at `192.168.1.130`. Pass another
hostname or IP address to deploy the same project to a different Pi:

```sh
./deploy.sh                         # original Raspberry Pi 1
./deploy.sh 192.168.1.140           # Raspberry Pi 3
PI_HOST=raspberrypi3.local ./deploy.sh
```

For a native installation, add the compilation prerequisites, SQLite, and a
Ruby version manager, then install the Ruby version from `.ruby-version`. The
exact package list varies by Raspberry Pi OS release; typical prerequisites
include `build-essential`, `libssl-dev`, `libyaml-dev`, and `libsqlite3-dev`.

Clone the repository to `/home/pi/pi-pulse`, then from its `rails/` folder:

```sh
bundle config set --local without "development test"
bundle install
RAILS_ENV=production bin/rails db:prepare
RAILS_ENV=production bin/rails assets:precompile
```

Install the services:

```sh
sudo cp deploy/pipulse.env.example /etc/pipulse.env
sudo chmod 600 /etc/pipulse.env
sudo cp deploy/pipulse-web.service deploy/pipulse-collector.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now pipulse-web pipulse-collector
```

Before starting, replace `SECRET_KEY_BASE` in `/etc/pipulse.env` with output
from `bin/rails secret`. The included units assume the user `pi`, the app
path `/home/pi/pi-pulse/rails`, and an rbenv-managed Ruby. Adjust the user, paths, and
`PATH` if your installation differs.

Open `http://<raspberry-pi-ip>:3000`. Check service output with:

```sh
systemctl status pipulse-web pipulse-collector
journalctl -u pipulse-web -u pipulse-collector -f
```

## Network boundary

PiPulse has no application login by design. Expose port 3000 only to a trusted
LAN, block it at the router, and do not configure public port forwarding. For
internet access, put PiPulse behind an authenticated HTTPS reverse proxy before
setting `FORCE_SSL=true`.

## Data behavior

Raw records older than 24 hours are deleted every ten minutes. Dashboard queries
downsample the historical series to about 360 points, while new values are
appended live. SQLite and Solid Cable data live under `storage/`; back up that
directory if history matters.
