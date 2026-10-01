---
title: Upgrade
description: How to update CH-UI to the latest version
---

## Before You Upgrade

Take a backup of the CH-UI database first. `ch-ui backup` writes a consistent
snapshot even while the server is running:

```bash
ch-ui backup /backups/ch-ui-before-upgrade.db
```

Stored credentials in that file are encrypted with the app secret key, and the
key is not in the database. Keep it with the backup: either the
`APP_SECRET_KEY` you set, or the generated `.app_secret_key` file that sits
next to the database when you never set one. A restore without the same key
cannot decrypt the credentials. See [`backup`](/docs/cli#backup).

## Update Command

```bash
ch-ui update
```

The updater downloads the latest release asset for your OS and architecture,
verifies its checksum, and replaces the binary on disk.

## What Happens

1. CH-UI fetches the latest release from GitHub.
2. It reads the release's `checksums.txt`. If the file is missing or has no
   entry for your platform, the update stops before downloading anything.
3. The binary for your platform is downloaded and its SHA-256 is compared with
   the published one. On a mismatch the download is deleted and the current
   binary stays.
4. The current binary is replaced in place.
5. If a CH-UI server is running (found through its PID file, default
   `ch-ui-server.pid`), it is stopped and started again in the background on
   the new version.

If your server uses a different PID file, pass it so the updater can find it:

```bash
ch-ui update --pid-file /var/run/ch-ui-server.pid
```

To restart the server yourself instead, use `--restart-server=false` and then:

```bash
ch-ui server restart
```

The same applies when the server runs under systemd or another supervisor
without a PID file: restart it through that supervisor.

## Validate

```bash
ch-ui version
ch-ui server status
curl -fsS http://localhost:3488/health
```

Then sign in and run a query on each connection you rely on.

## Docker

Back up first, then pull the latest image and recreate the container. The
database and the generated `.app_secret_key` both live in the `/app/data`
volume, so they carry over.

```bash
docker exec ch-ui-server ch-ui backup /app/data/ch-ui-before-upgrade.db
docker pull ghcr.io/caioricciuti/ch-ui:latest
docker stop ch-ui-server
docker rm ch-ui-server
docker run -d \
  --name ch-ui-server \
  --restart unless-stopped \
  -p 3488:3488 \
  -v ch-ui-data:/app/data \
  ghcr.io/caioricciuti/ch-ui:latest
```

If you pass `APP_SECRET_KEY` as an environment variable, pass the same value to
the new container.

## Connector Updates

If you run connectors on remote hosts, update them to match the server version
and restart them so the new binary is the one running:

```bash
ch-ui update           # on each connector host
ch-ui service restart  # if running as a service
```

If the connector runs in the foreground or with `ch-ui connect --detach`, stop
it and start it again. Check in **Admin > Connections** that each tunnel shows
as connected.
