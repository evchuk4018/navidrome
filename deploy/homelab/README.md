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
| Web URL | [https://navidrome.wowzerbowser.xyz/](https://navidrome.wowzerbowser.xyz/) |
| HomeTube mount | `/hometube` (set by `ND_HOMETUBEBASEURL`) |

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

HomeTube is an optional same-origin integration. The default
`ND_HOMETUBEBASEURL=/hometube` enables the native HomeTube views when the
existing Caddy `/hometube` route is available; setting it to an empty value
disables those views without changing music playback. Keep the HomeTube
service and its media/database volumes managed by the separate HomeTube
deployment. If that service is unavailable, users can retry from the
HomeTube page while the music application remains usable.

The Navidrome host must proxy the API beneath the same mount so browser
requests stay same-origin. The relevant Caddy handler is:

```caddyfile
handle /hometube/api/* {
    reverse_proxy 127.0.0.1:3010
}
```

Keep the existing `/hometube` page handler alongside this API handler. The
native UI calls `/hometube/api/...` and does not send Navidrome JWT headers.

To roll back the integration independently of the Navidrome image, set
`ND_HOMETUBEBASEURL=` in the server-side environment used by Compose and
recreate only the Navidrome service. Restore `/hometube` after the HomeTube
route is healthy. The setting does not remove HomeTube data or media.
