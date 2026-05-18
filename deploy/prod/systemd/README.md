# Prod systemd units for Asty

Two units, one shared user, ordered so the agent's child `nats-server`
is up before the server tries to connect:

  asty-agent.service     — supervises NATS + processes + embedded gateway
  asty-server.service    — leader election + reconciler + dashboard API

`asty-server.service` carries `After=asty-agent.service` and
`Wants=asty-agent.service`, so:

  - `systemctl start asty-server` pulls in the agent automatically.
  - On boot, the agent's NATS comes up first, the server dials it
    once and stays connected.
  - If the agent later fails, the server's NATS reconnect logic kicks
    in (no unit dependency cascade).

## Install

```
useradd --system --no-create-home --shell /usr/sbin/nologin asty
mkdir -p /var/lib/asty /etc/asty
chown asty:asty /var/lib/asty
install -m 0755 ./bin/asty /usr/local/bin/asty
install -m 0644 ./deploy/prod/config.asty /etc/asty/config.asty
install -m 0644 ./deploy/prod/systemd/asty-agent.service /etc/systemd/system/
install -m 0644 ./deploy/prod/systemd/asty-server.service /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now asty-server   # pulls in asty-agent
```

## Secrets

Put A_TOKEN, A_NATS_PASSWORD, A_NATS_OBSERVER_PASSWORD,
A_NATS_APP_PASSWORD in `/etc/asty/asty.env` with permissions 0640
asty:asty. Both units `EnvironmentFile=-` read it on start; the `-`
prefix tolerates the file being absent so a fresh box can boot from
defaults before secrets land.

## Verifying

```
systemctl status asty-agent asty-server
journalctl -u asty-agent -u asty-server -f
curl -fs http://127.0.0.1:7060/health
curl -fs http://127.0.0.1:7060/metrics | head
```
