# Navidrome homelab deployment

The homelab runs this fork as a standalone Compose project using the published
GitHub Container Registry image. It does not need a Navidrome source checkout
or a local build.

The live deployment layout is:

| Resource | Value |
| --- | --- |
| Server Compose file | `/srv/storage/wowzerbowser/files/navidrome/docker-compose.yml` |
| Compose project/service | `navidrome` / `navidrome` |
| Image | `ghcr.io/evchuk4018/navidrome:latest` |
| Music library | `/srv/storage/media/music` mounted read-write at `/music` |
| Navidrome data | external volume `musicplayer_navidrome_data` mounted at `/data` |
| Local listener | `127.0.0.1:4533` |
| Tailnet URL | `https://homelab.tail861ffd.ts.net/navidrome` |

The Compose configuration keeps the existing external data volume, so the
Navidrome database, configuration, playlists, users, and other application
state survive image pulls and container recreation. The music library remains
the existing `/srv/storage/media/music` bind mount.

The source file copied to the server is:

```text
deploy/homelab/docker-compose.homelab.yml
```

The complete installation, GHCR setup, systemd timer, status checks, and
rollback procedure are documented in [`deploy/README.md`](../README.md).

The server-only Compose project is intentionally separate from the Wowzer
Bowser application stack. Do not use `/srv/storage/wowzerbowser/deployment.env`,
the Wowzer Compose project, or a database-volume migration workflow for
Navidrome.
