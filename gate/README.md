# Home gate

A home page for a Raspberry Pi. Open `http://homemonitor.local` and you get one
card per app on your network, so you no longer type ports.

- Header: hostname, board model and uptime, with an "All apps up" or
  "1 app down" indicator.
- Apps: icon, name and port, with a status dot from a health check every 10
  seconds. A click opens the app.

It is a single Go binary that uses about 10 MB of RAM and needs nothing else
on the Pi.

## Configuration

Apps are listed in [`config.yaml`](config.yaml). `deploy.sh` installs it as
`/etc/gate/config.yaml`, so edit it here and redeploy to change the apps.

```yaml
port: 80
interval: 10s
apps:
  - name: HomeMonitor
    icon: temperature
    port: 8000
  - name: Pi-hole
    icon: shield-check
    host: 192.168.1.25
    port: 80
    path: /admin
```

| Field | Meaning |
|---|---|
| `title` | Header title; defaults to the hostname |
| `port` | Port the gate listens on (default `80`) |
| `interval` | How often apps are checked (default `10s`, minimum `2s`) |
| `apps[].name` | Shown on the card |
| `apps[].icon` | Any [Tabler icon](https://tabler.io/icons) name, such as `temperature`, `activity`, `shield-check`, `camera` |
| `apps[].port` | The app's port |
| `apps[].host` | Optional, for an app on another machine. Use its IP address: the gate cannot resolve `.local` names |
| `apps[].path` | Optional, added to the link, such as `/admin` |
| `apps[].health` | Optional path checked for the status dot; defaults to `path` |

An app counts as up when it answers with any HTTP status below 500.
Links use the address you opened the gate with, so they work with both
`homemonitor.local` and the Pi's IP address.

Check a config without starting the server:

```sh
go run . -config config.yaml -check
```

## Development

```sh
go test ./...
go run . -config config.yaml
```

On a Mac, set `port: 8080` in a copy of the config, since port 80 needs root.

Routes: `GET /` (the page), `GET /api/status` (everything the page shows, as
JSON) and `GET /up` (health check).

## Raspberry Pi deployment

```sh
./deploy.sh                          # pi@homemonitor.local
./deploy.sh pi@192.168.1.80          # or by IP
```

The script validates `config.yaml`, cross-compiles for the Pi's architecture,
installs the binary, the config and a systemd service, and restarts it. The
service runs as a throwaway user with only the right to use port 80, a 32 MB
memory cap and low CPU priority.

### The homemonitor.local address

The Pi announces its hostname on the local network, so renaming it is all
`homemonitor.local` needs. On the Pi:

```sh
sudo hostnamectl set-hostname homemonitor
sudo sed -i 's/\braspberrypi\b/homemonitor/g' /etc/hosts
sudo systemctl restart avahi-daemon
```

After that, `raspberrypi.local` stops working and `homemonitor.local` takes
its place. The IP address keeps working.

Check the service with:

```sh
systemctl status gate
journalctl -u gate -f
```

To uninstall:

```sh
sudo systemctl disable --now gate
sudo rm -r /usr/local/bin/gate /etc/systemd/system/gate.service /etc/gate
```

## Network boundary

The gate has no login. Keep it on your local network and do not forward port
80 on your router. The page loads Tabler icons from the jsDelivr CDN; without
internet access the cards still work, without icons.
