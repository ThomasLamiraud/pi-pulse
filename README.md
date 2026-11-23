# PiPulse

A live monitoring dashboard for Raspberry Pi: CPU, memory, temperature,
throttling, network, disks and top processes, with 24 hours of history.

This repository holds two implementations of the same dashboard, plus a home
page that links to every app on the Pi:

| Folder | Stack | Memory on the Pi | Deployment |
|---|---|---|---|
| [`go/`](go/README.md) | Go, SQLite, Server-Sent Events | ~13 MB | one binary + systemd |
| [`rails/`](rails/README.md) | Rails 8, Hotwire, SQLite | ~300–400 MB | Docker or native Ruby |
| [`gate/`](gate/README.md) | Go | ~10 MB | one binary + systemd |

**Use the Go version** on small boards or next to other services such as
Pi-hole. The Rails version is the original and stays here for reference.

## Quick start (Go)

```sh
cd go
go test ./...
./deploy.sh pihole@pihole.local
```

Then open `http://pihole.local:3000`. See [`go/README.md`](go/README.md) for
configuration and details.

## Home gate

[`gate/`](gate/README.md) serves `http://homemonitor.local`: a launcher card
for each app listed in
[`gate/config.yaml`](gate/config.yaml), so you don't need to remember ports.

```sh
cd gate
./deploy.sh pi@homemonitor.local
```

## Quick start (Rails)

```sh
cd rails
bundle install
bin/rails db:prepare
bin/rails test
```

See [`rails/README.md`](rails/README.md) for running and deploying it.

None of them has a login: keep them on your local network.

## License

[MIT](LICENSE)
