# Navidrome homelab deployment

This fork is deployed as the `navidrome` service in the existing `musicplayer`
Compose project. Connect to the host with:

```sh
ssh -o BatchMode=yes -o ConnectTimeout=10 evanh@100.98.43.68
```

The live deployment layout is:

| Resource | Value |
| --- | --- |
| Source checkout | `/srv/storage/wowzerbowser/files/home music` |
| Base Compose file | `/srv/storage/wowzerbowser/files/musicplayer/docker-compose.yml` |
| Navidrome override | `/srv/storage/wowzerbowser/files/home music/deploy/homelab/docker-compose.musicplayer.override.yml` |
| Compose project/service | `musicplayer` / `navidrome` |
| Container/image | `musicplayer-navidrome-1` / `home-music-navidrome:local` |
| Music library | `/srv/storage/media/music` mounted read-write at `/music` |
| Navidrome data | `musicplayer_navidrome_data` mounted at `/data` |
| Local listener | `127.0.0.1:4533` |
| Tailnet URL | `https://homelab.tail861ffd.ts.net/navidrome` |

The homelab override automatically authenticates Navidrome as the existing
`admin` user, so the web UI and API do not prompt for a login. This is intended
for the private tailnet deployment; Navidrome remains bound to loopback and is
not exposed publicly.

This is not part of the Wowzer Bowser application stack. Do not use
`/srv/storage/wowzerbowser/deployment.env`, its Compose project, or its
PostgreSQL migration workflow for Navidrome. Navidrome owns the database in its
`/data` volume and applies its application migrations during startup.

The deployment override is applied alongside the existing Compose file:

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
`/navidrome` path shown above.

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

## Deploying `origin/main`

The server checkout is deployment-only and is intentionally allowed to remain
detached at `origin/main`. Verify that it is clean, fetch the pushed commit,
check it out explicitly, then rebuild and recreate only Navidrome:

```sh
SOURCE='/srv/storage/wowzerbowser/files/home music'
COMPOSE_BASE=/srv/storage/wowzerbowser/files/musicplayer/docker-compose.yml
COMPOSE_OVERRIDE='/srv/storage/wowzerbowser/files/home music/deploy/homelab/docker-compose.musicplayer.override.yml'
test -z "$(git -C "$SOURCE" status --porcelain)"
git -C "$SOURCE" fetch origin main
git -C "$SOURCE" switch --detach origin/main
docker compose -f "$COMPOSE_BASE" -f "$COMPOSE_OVERRIDE" up -d --build --wait navidrome
docker compose -f "$COMPOSE_BASE" -f "$COMPOSE_OVERRIDE" ps navidrome
git -C "$SOURCE" rev-parse HEAD
```

The deployed revision should match `origin/main`, and the service should report
`healthy`. This workflow does not rebuild or restart the `musicplayer` web,
worker, or PostgreSQL services.

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
