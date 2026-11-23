# PiPulse (Go)

A lightweight Raspberry Pi monitoring dashboard. It is a single Go binary that
samples the host every 15 seconds, keeps 24 hours of history in SQLite and
pushes each new sample to open dashboards with Server-Sent Events.

It shows the same dashboard as the [Rails version](../rails/README.md) with a
much smaller footprint, so it can run next to Pi-hole on a Raspberry Pi 3:

| | Go | Rails |
|---|---|---|
| Memory | ~13 MB | ~300–400 MB (web + collector) + Docker |
| Install | one ~11 MB binary + a systemd unit | Docker image or Ruby toolchain |
| Processes | 1 | 2 |

## Metrics

- CPU utilization, frequency, and 1, 5 and 15 minute load averages
- Used and total memory and swap
- Chip temperature and firmware throttling or undervoltage flags
- Aggregate and per-interface receive/transmit rates
- Capacity of mounted physical filesystems
- Top five processes by CPU and by memory, and logged-in users
- Size, last write and per-table row counts of SQLite databases
- Collector freshness and collection errors

CPU and network values are computed from successive kernel counters in
`/proc/stat` and `/proc/net/dev`. Memory comes from `/proc/meminfo`, disks
from `/proc/mounts` and `statfs`, temperature and clocks from `/sys`. It is
Linux-only.

## Layout

```
main.go                     configuration, startup and shutdown
internal/metrics            reads /proc and /sys (port of SystemMetrics::Reader)
internal/collector          sampling loop, error samples, pruning
internal/store              SQLite storage, history downsampling
internal/server             HTTP routes, JSON API and live event stream
web/                        dashboard page, CSS and JavaScript (embedded)
deploy/pipulse.service      systemd unit
deploy.sh                   cross-compile and install on a Pi
```

Routes:

- `GET /` — the dashboard
- `GET /api/dashboard` — latest sample and up to 360 history points
- `GET /events` — Server-Sent Events stream, one message per new sample
- `GET /up` — health check

## Development

Requires Go 1.27 or later. The SQLite driver is pure Go, so no C toolchain is
needed and cross-compiling works from any machine.

```sh
go test ./...
go run .
```

On macOS the collector stores error samples, because there is no `/proc`; the
tests use Linux fixtures and run anywhere. To try it with real data, run the
Linux binary in a container:

```sh
GOOS=linux go build -o dist/pipulse .
docker run --rm -p 3000:3000 -e DATABASE_PATH=/tmp/pipulse.db \
  -v "$PWD/dist/pipulse:/pipulse:ro" debian:trixie-slim /pipulse
```

Configuration (environment variables):

- `METRIC_INTERVAL` — sampling interval in seconds, clamped from 1 to 60 (default `15`)
- `PORT` — HTTP port (default `3000`)
- `LISTEN_ADDRESS` — interface to listen on (default `0.0.0.0`)
- `DATABASE_PATH` — SQLite file (default `pipulse.db`; `/var/lib/pipulse/pipulse.db` under systemd)
- `WATCH_DATABASES` — extra SQLite files to show, as comma-separated `name=/path` pairs (PiPulse's own database is always shown)
- `PROC_ROOT`, `THERMAL_PATH`, `CPU_FREQUENCY_PATH`, `CPU_MAX_FREQUENCY_PATH`, `THROTTLING_PATH` — source overrides

## Raspberry Pi deployment

`deploy.sh` detects the Pi's architecture (64-bit ARM, ARMv7 or ARMv6),
cross-compiles on your machine, copies the binary to `/usr/local/bin/pipulse`,
installs the systemd unit and restarts the service. The Pi needs nothing
installed beforehand; you only need SSH access and sudo.

```sh
./deploy.sh                          # pihole@pihole.local
./deploy.sh pi@192.168.1.130         # another Pi
PI_TARGET=pi@raspberrypi.local ./deploy.sh
```

Then open `http://<raspberry-pi>:3000`.

The service is built to stay out of the way of other services such as Pi-hole:

- it runs as a throwaway user (`DynamicUser`) that can only write to `/var/lib/pipulse`;
- it has low CPU priority (`Nice=10`) and a 64 MB memory cap;
- the rest of the system is read-only to it.

To change the port or interval, create `/etc/default/pipulse`:

```sh
echo "PORT=8080" | sudo tee /etc/default/pipulse
sudo systemctl restart pipulse
```

### Databases panel

The dashboard lists PiPulse's own database plus any SQLite file named in
`WATCH_DATABASES`. Size and last write update with every sample; row counts
are refreshed every 15 minutes in the background, read-only.

```sh
# HomeMonitor on the Pi 1
echo "WATCH_DATABASES=HomeMonitor=/home/pi/node_temperature/db/temperature_db.db" | sudo tee -a /etc/default/pipulse

# Pi-hole's query log and blocklists on the Pi 3
echo "WATCH_DATABASES=Pi-hole queries=/etc/pihole/pihole-FTL.db,Pi-hole gravity=/etc/pihole/gravity.db" | sudo tee -a /etc/default/pipulse

sudo systemctl restart pipulse
```

If a file is only readable by a group, such as `pihole`, let the service join
it. The size still shows without it, but the row counts need read access:

```sh
sudo mkdir -p /etc/systemd/system/pipulse.service.d
printf '[Service]\nSupplementaryGroups=pihole\n' | sudo tee /etc/systemd/system/pipulse.service.d/groups.conf
sudo systemctl daemon-reload && sudo systemctl restart pipulse
```

Check the service with:

```sh
systemctl status pipulse
journalctl -u pipulse -f
```

To uninstall:

```sh
sudo systemctl disable --now pipulse
sudo rm /usr/local/bin/pipulse /etc/systemd/system/pipulse.service
sudo rm -rf /var/lib/private/pipulse
```

## Network boundary

PiPulse has no login by design. Keep port 3000 on a trusted LAN and do not
forward it on your router. For access from the internet, put it behind an
authenticated HTTPS reverse proxy or a VPN such as Tailscale.

The dashboard loads Chart.js from the jsDelivr CDN, so the browser needs
internet access to draw the charts; the numbers show without it.

## Data behavior

Samples older than 24 hours are deleted every ten minutes. The dashboard
downsamples history to about 360 points, then appends new points live. Failed
readings are stored as error samples and shown as gaps in the charts.
