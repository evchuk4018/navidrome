# Navidrome homelab deployment

This fork is deployed as the only music service in its own Compose project.
Connect to the host with:

```sh
ssh -o BatchMode=yes -o ConnectTimeout=10 evanh@100.98.43.68
```

The live deployment layout is:

| Resource | Value |
| --- | --- |
| Source checkout | `/srv/storage/wowzerbowser/files/home music` |
| Compose file | `/srv/storage/wowzerbowser/files/home music/deploy/homelab/docker-compose.homelab.yml` |
| Compose project/service | `navidrome` / `navidrome` |
| Container/image | `navidrome-1` / `home-music-navidrome:local` |
| Music library | `/srv/storage/media/music` mounted read-write at `/music` |
| Navidrome data | `musicplayer_navidrome_data` mounted at `/data` |
| Local listener | `127.0.0.1:4533` |
| Tailnet URL | `https://homelab.tail861ffd.ts.net/navidrome` |

The homelab override automatically authenticates Navidrome as the existing
`admin` user, so the web UI and API do not prompt for a login. This is intended
for the private tailnet deployment; Navidrome remains bound to loopback and is
not exposed publicly.

This is not part of the Wowzer Bowser application stack. Do not use
`/srv/storage/wowzerbowser/deployment.env`, its Compose project, or a separate
PostgreSQL migration workflow for Navidrome. Navidrome owns the database in its
`/data` volume and applies its application migrations during startup.

The standalone deployment file is used directly:

```sh
SOURCE='/srv/storage/wowzerbowser/files/home music'
COMPOSE_FILE="$SOURCE/deploy/homelab/docker-compose.homelab.yml"
docker compose -p navidrome -f "$COMPOSE_FILE" up -d --build navidrome
```

The canonical library is `/srv/storage/media/music`, mounted at `/music` with
write access so an authenticated external music downloader can import files.
Navidrome state is kept in the existing named volume
`musicplayer_navidrome_data`, mounted at `/data`. The host publishes Navidrome
only on `127.0.0.1:4533`; Caddy exposes it at the tailnet-only
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
COMPOSE_FILE="$SOURCE/deploy/homelab/docker-compose.homelab.yml"
test -z "$(git -C "$SOURCE" status --porcelain)"
git -C "$SOURCE" fetch origin main
git -C "$SOURCE" switch --detach origin/main
GIT_SHA="$(git -C "$SOURCE" rev-parse --short HEAD)"
GIT_TAG="$(git -C "$SOURCE" describe --tags --abbrev=0 2>/dev/null || true)-SNAPSHOT"
docker compose -p navidrome -f "$COMPOSE_FILE" \
  build --build-arg GIT_SHA="$GIT_SHA" --build-arg GIT_TAG="$GIT_TAG" navidrome
docker compose -p navidrome -f "$COMPOSE_FILE" up -d --wait navidrome
docker compose -p navidrome -f "$COMPOSE_FILE" ps navidrome
git -C "$SOURCE" rev-parse HEAD
```

The deployed revision should match `origin/main`, and the service should report
`healthy`.

Daily operations use the same standalone Compose file:

```sh
COMPOSE_FILE='/srv/storage/wowzerbowser/files/home music/deploy/homelab/docker-compose.homelab.yml'
docker compose -p navidrome -f "$COMPOSE_FILE" logs -f --tail=200 navidrome
docker compose -p navidrome -f "$COMPOSE_FILE" restart navidrome
docker compose -p navidrome -f "$COMPOSE_FILE" stop navidrome
docker compose -p navidrome -f "$COMPOSE_FILE" start navidrome
```

No Navidrome application source files are changed by this deployment.
