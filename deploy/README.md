# Navidrome deployment

This fork publishes a production Navidrome image from every push to `main`:

```text
push main
  -> GitHub Actions builds the repository Dockerfile
  -> GHCR receives latest and the full commit SHA tag
  -> the homelab timer runs docker compose pull navidrome
  -> docker compose recreates Navidrome only when the image changed
```

The published image is:

```text
ghcr.io/evchuk4018/navidrome:latest
ghcr.io/evchuk4018/navidrome:<full-commit-sha>
```

The image is built from the repository's existing multi-stage `Dockerfile`, so
the fork's frontend and backend are included. The homelab does not clone the
repository or compile Navidrome.

## Repository files

- `.github/workflows/build-and-publish.yml` builds and publishes the image for
  pushes to `main`.
- `deploy/homelab/docker-compose.homelab.yml` is the production Compose file.
- `deploy/update-navidrome.sh` pulls and updates only the `navidrome` service.
- `deploy/navidrome-update.service` and `deploy/navidrome-update.timer` provide
  the systemd update job.

## GHCR visibility and authentication

The intended package visibility is public. GitHub Container Registry creates a
new package as private by default, so after the first successful workflow run:

1. Open the `navidrome` package from the `evchuk4018` account's Packages page.
2. Open Package settings and change the package visibility to **Public**.
3. Verify the tags from the homelab:

   ```sh
   docker pull ghcr.io/evchuk4018/navidrome:latest
   docker pull ghcr.io/evchuk4018/navidrome:<full-commit-sha>
   ```

Public container packages can be pulled anonymously. If the package is kept
private instead, authenticate as the `evanh` service user with a GitHub classic
PAT containing `read:packages`:

```sh
printf '%s' "$GHCR_READ_TOKEN" |
  docker login ghcr.io --username evchuk4018 --password-stdin
unset GHCR_READ_TOKEN
```

Use Docker's credential store or credential helper on the server. Never put a
token in this repository, the Compose file, or a systemd unit.

## Homelab installation

The live configuration is kept separately from any source checkout:

```text
/srv/storage/wowzerbowser/files/navidrome/docker-compose.yml
```

It preserves the existing resources:

- `/srv/storage/media/music` -> `/music`, read-write
- external Docker volume `musicplayer_navidrome_data` -> `/data`
- `127.0.0.1:4533` -> Navidrome port 4533
- Compose project/service `navidrome` / `navidrome`

Copy the deployment files to the host once. Run these commands from a checkout
of this repository on the workstation:

```sh
scp deploy/homelab/docker-compose.homelab.yml evanh@homelab:/tmp/navidrome-compose.yml
scp deploy/update-navidrome.sh evanh@homelab:/tmp/update-navidrome.sh
scp deploy/navidrome-update.service evanh@homelab:/tmp/navidrome-update.service
scp deploy/navidrome-update.timer evanh@homelab:/tmp/navidrome-update.timer

ssh evanh@homelab <<'REMOTE'
set -euo pipefail

sudo install -d -o evanh -g evanh -m 0755 /srv/storage/wowzerbowser/files/navidrome
sudo install -o evanh -g evanh -m 0644 \
  /tmp/navidrome-compose.yml \
  /srv/storage/wowzerbowser/files/navidrome/docker-compose.yml
sudo install -o root -g root -m 0755 \
  /tmp/update-navidrome.sh /usr/local/bin/update-navidrome.sh
sudo install -o root -g root -m 0644 \
  /tmp/navidrome-update.service \
  /etc/systemd/system/navidrome-update.service
sudo install -o root -g root -m 0644 \
  /tmp/navidrome-update.timer \
  /etc/systemd/system/navidrome-update.timer

sudo systemctl daemon-reload
sudo systemctl enable navidrome-update.timer
sudo systemctl start navidrome-update.service
sudo systemctl start navidrome-update.timer
REMOTE
```

If Last.fm settings or another Compose interpolation value must survive
systemd execution, create `/etc/navidrome/navidrome.env` on the server, make it
readable only by `evanh`, and keep it out of Git. For example:

```dotenv
ND_LASTFM_ENABLED=false
# ND_LASTFM_APIKEY=...
# ND_LASTFM_SECRET=...
NAVIDROME_IMAGE=ghcr.io/evchuk4018/navidrome:latest
```

The update script loads this file when it exists. It is optional for the
default public-image deployment.

## Manual deployment and status

The timer invokes the same commands as this manual update:

```sh
DEPLOY_DIR=/srv/storage/wowzerbowser/files/navidrome
docker compose \
  --project-name navidrome \
  --file "$DEPLOY_DIR/docker-compose.yml" \
  pull navidrome
docker compose \
  --project-name navidrome \
  --file "$DEPLOY_DIR/docker-compose.yml" \
  up -d --wait navidrome
```

Useful checks:

```sh
systemctl status navidrome-update.timer
systemctl status navidrome-update.service
journalctl -u navidrome-update.service --no-pager

docker compose -p navidrome \
  -f /srv/storage/wowzerbowser/files/navidrome/docker-compose.yml ps navidrome
docker ps
docker logs navidrome-navidrome-1
curl --fail http://127.0.0.1:4533/ping
```

The systemd service exits non-zero when the pull or update fails, so the error
is visible in `journalctl`. No image pruning is performed.

## Rollback

Every successful build has an immutable full-SHA tag. To pin the deployment to
a known good commit, set this value in the untracked server-side environment
file:

```dotenv
NAVIDROME_IMAGE=ghcr.io/evchuk4018/navidrome:<previous-sha>
```

Then run:

```sh
sudo systemctl start navidrome-update.service
```

To resume automatic updates, remove the `NAVIDROME_IMAGE` override or set it
back to `ghcr.io/evchuk4018/navidrome:latest`, then start the service again.
Do not delete recent SHA-tagged images from GHCR.
