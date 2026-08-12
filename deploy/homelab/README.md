# Navidrome homelab deployment

This fork is deployed as the existing `musicplayer` Compose project's
Navidrome service. The Navidrome source is kept in
`/srv/storage/wowzerbowser/files/home music` on the homelab. The deployment
override is applied with the existing Compose file:

```sh
COMPOSE_BASE=/srv/storage/wowzerbowser/files/musicplayer/docker-compose.yml
COMPOSE_OVERRIDE='/srv/storage/wowzerbowser/files/home music/deploy/homelab/docker-compose.musicplayer.override.yml'
docker compose -f "$COMPOSE_BASE" -f "$COMPOSE_OVERRIDE" up -d --build navidrome
```

The canonical library is `/srv/storage/media/music`, mounted at `/music` with
write access so the authenticated external music downloader can import files.
The beets database and provider cache live in Navidrome's existing data volume.
Navidrome state is kept in the existing named volume
`musicplayer_navidrome_data`, mounted at `/data`. The host publishes Navidrome
only on `127.0.0.1:4533`; Tailscale Serve exposes it at the tailnet-only
`/navidrome` path on the existing HTTPS endpoint.

## Updating the fork

`origin` is the personal fork and `upstream` is the official repository:

```sh
cd '/srv/storage/wowzerbowser/files/home music'
git remote -v
git fetch upstream
git checkout main
git merge --ff-only upstream/master
git push origin main
```

After pulling the fork onto the homelab, rebuild and recreate only Navidrome:

```sh
cd '/srv/storage/wowzerbowser/files/home music'
COMPOSE_BASE=/srv/storage/wowzerbowser/files/musicplayer/docker-compose.yml
COMPOSE_OVERRIDE='/srv/storage/wowzerbowser/files/home music/deploy/homelab/docker-compose.musicplayer.override.yml'
git pull --ff-only origin main
docker compose -f "$COMPOSE_BASE" -f "$COMPOSE_OVERRIDE" up -d --build navidrome
```

Daily operations use the same two Compose files:

```sh
COMPOSE_BASE=/srv/storage/wowzerbowser/files/musicplayer/docker-compose.yml
COMPOSE_OVERRIDE='/srv/storage/wowzerbowser/files/home music/deploy/homelab/docker-compose.musicplayer.override.yml'
docker compose -f "$COMPOSE_BASE" -f "$COMPOSE_OVERRIDE" logs -f --tail=200 navidrome
docker compose -f "$COMPOSE_BASE" -f "$COMPOSE_OVERRIDE" restart navidrome
docker compose -f "$COMPOSE_BASE" -f "$COMPOSE_OVERRIDE" stop navidrome
docker compose -f "$COMPOSE_BASE" -f "$COMPOSE_OVERRIDE" start navidrome
```

No Navidrome application source files are changed by this deployment.
