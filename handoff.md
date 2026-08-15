# Handoff: Last.fm radio downloads never play in the Quick Pick queue

## Status
- Repo: navidrome, branch `main`, working tree clean, HEAD `5fafa7a5`.
- Main agent (me) has fully investigated the defect and simulated it. **Do not re-investigate** — the diagnosis below is verified against the actual `navidrome-music-player` library source (`node_modules/navidrome-music-player/lib/index.js`) and a runnable simulation. Pick one or more of the fix options at the bottom and implement it. Update this file with what you did and the verification results when done.

## User-reported symptom
Auto-downloaded Last.fm (YouTube) tracks from Quick Pick personal radio do not play from the queue:
1. The queue row shows "Downloading: <title>" but when playback reaches it, it is skipped and the next (buffer) song plays.
2. Clicking the row: ~10 s load, then it jumps to the next song.
3. The downloaded files themselves are fine — manually navigating to them in the library plays them.

## Architecture recap (already mapped, do not re-derive)
- Quick Pick tile -> `playSongRadio` (ui/src/quickpick/QuickPick.jsx:137): plays seed, creates personal radio session, `setRadioSession`, first `syncRadioTracks`.
- Server plans queue in pairs: Discovery (downloading, leads) then Library buffer (ready) — see `core/personalradio/service.go:531-537` (`plan`). 10 outstanding items (`queueLowWatermark`).
- Client polls `refillPersonalRadio(sessionId)` every 3 s while `isRadioPlanning(planningStatus)` (ui/src/audioplayer/Player.jsx:152-179).
- Each poll response -> `radioSongs` (ui/src/quickpick/provider.js:45) -> `syncRadioTracks` action (ui/src/actions/player.js:131) -> `reduceSyncRadioTracks` (ui/src/reducers/playerReducer.js:151).
- `mapToAudioLists` (ui/src/reducers/playerReducer.js:47): pending items (`radioPending: true`) get `musicSrc: null`, `isRadio: true`; **ready radio items go down the normal branch with `musicSrc: makeMusicSrc(trackId)` — a NEW closure/function every call** (playerReducer.js:39-45, 98).
- Skip mechanism: `skipPendingRadioItem` (ui/src/audioplayer/Player.jsx:133-147), called from `onAudioPlayTrackChange` (Player.jsx:530). It finds the item by `__PLAYER_KEY__`, and if `radioPending`, calls `audioInstance.playByIndex(audioLists.indexOf(nextPlayable))` — jumps the playhead past the pending item.
- Navidrome fork of react-jinke-music-player in `ui/node_modules/navidrome-music-player/` (version 4.25.4, source in `lib/`).

## Root cause (verified by simulation at `C:\Users\erhol\AppData\Local\Temp\opencode\radio-dup-sim.js`)
1. Every `syncRadioTracks` dispatch sets `clear: false` (playerReducer.js:172). After the seed settles, the Redux `clear` flag is already false, so the music player component receives the queue update in `UNSAFE_componentWillReceiveProps` (lib/index.js:2077-2104) with `clearPriorAudioLists` false -> it calls **`updateAudioLists`** (lib/index.js:1330-1339), the append-only merge — never `changeAudioLists` (replace).
2. `updateAudioLists` dedupes new items with `state.audioLists.findIndex(v => v.musicSrc === audio.musicSrc) === -1`. Since `mapToAudioLists` creates a **fresh `musicSrc` function per item per sync**, nothing ever matches, so **every poll appends a duplicate of every ready item** to the end of the library's internal list, and the original "Downloading: X" placeholder at its queue position is **never replaced**.
3. The visible queue panel renders the library's `state.audioLists` (lib/index.js:1755, passed to `AudioListsPanel` at 1992), so the user keeps seeing the stale "Downloading: X" row plus a list that grows ~5 rows per 3 s.
4. `skipPendingRadioItem` then fires on every track change into that stale pending stub — permanent skip; the real downloaded track exists only as a tail duplicate in a list growing faster than the playhead advances, so it never auto-plays. (The Redux queue itself is correct: `reduceSyncRadioTracks` does replace in place, uuid preserved.)
5. Clicking the pending row: `audioListsPlay` (lib/index.js:261) fires `onAudioPlayTrackChange` -> the same skip before loading; `musicSrc` is null so nothing can start; the ~10 s is the cold stream/transcode decision (`resolveStreamUrl`, ui/src/transcode/decisionService.js:85) for the buffer song it jumps to.
6. Contributing: `updateAudioLists` also re-runs `bindEvents` (lib/index.js:1337) which re-adds the same audio event listeners every poll -> duplicate `ended`/`error` handlers -> `handlePlay` can cascade (rapid skips).

## Fix options (pick one or more; implement, do not re-derive)
- **Option A (recommended, simplest, least invasive):** Stop feeding the library fresh objects every poll. Only dispatch `syncRadioTracks` when the response actually changed the queue (e.g. compare `radioItemId` + `radioPending` + song id against the current queue in the reducer or in `Player.jsx`'s `appendRadioItems`). Unchanged items then keep their exact objects -> `updateAudioLists` appends nothing.
  - Caveat: an item that transitions pending -> ready still produces a new object with a new `musicSrc` function, so it would still be appended as a duplicate once. Combine with:
- **Option B:** Make the ready radio item's `musicSrc` a stable **string** (pre-resolved URL) instead of a fresh function, so `updateAudioLists`'s `v.musicSrc === audio.musicSrc` matches the old item (if the old musicSrc was also that string) — or better, fix the matching by uuid in the library? Library is vendored in node_modules; prefer fixing in our repo unless patching the fork (package is `navidrome-music-player`, source lives under `node_modules`, no local fork directory seen). String musicSrc also removes the per-sync function churn. Could resolve lazily via `subsonic.streamUrl(trackId)` string for ready radio items.
- **Option C:** Fix the skip: make `skipPendingRadioItem` consult the authoritative Redux queue (`playerStateRef.current.queue` by `radioItemId`) instead of the library's stale `audioLists`, and skip only if the item is genuinely pending; additionally, when a pending item becomes ready, position the playhead back so it plays — or simply don't skip once ready (already the case in redux, but the library copy is stale — Option A/B fix the staleness).
- Optionally: keep `clear: true` semantics for radio syncs so the library uses `changeAudioLists` (replace-in-place) instead of `updateAudioLists` — but `clear: true` resets playback (resetAudioStatus) unless the current track matches (quietUpdate path), so verify the seed keeps playing if this route is taken.

## Constraints / conventions
- Go tests: `go test ./core/personalradio/... ./core/music/...`. UI tests: `npx vitest run src/reducers/playerReducer.test.js src/quickpick/provider.test.js src/audioplayer/` from `ui/`.
- Keep the radio feedback flow intact (`onAudioPlay`, `radioPlaybackRef`, `reportRadioFeedback`, refill on play).
- The "skip pending" concept is intended (commit 1a993341) — the failure is that the skipped item never becomes playable in place. Prefer making the in-place replacement actually reach the library over removing the skip.
- Re-run the UI test suites and `go build ./...` / `go test` as relevant; the repo has `.golangci.yml` (lint) and the UI has eslint via `npm run lint`.
- Do not commit unless asked; update this handoff.md with a short "Resolution" section (what you implemented, files touched, verification run) and leave it in place.

## Key file/line index
- ui/src/audioplayer/Player.jsx:133-147 (`skipPendingRadioItem`), :152-179 (poll), :530-555 (onAudioPlayTrackChange)
- ui/src/reducers/playerReducer.js:47-112 (`mapToAudioLists`), :151-173 (`reduceSyncRadioTracks`), :205-221 (`reduceSyncQueue`), :273-288 (REFRESH_QUEUE)
- ui/src/actions/player.js:131-136 (`syncRadioTracks`), :21-33 (`filterSongs`)
- ui/src/quickpick/provider.js:45-75 (`radioSongs`)
- ui/src/quickpick/QuickPick.jsx:137-165 (`playSongRadio`)
- ui/src/transcode/decisionService.js:85-91 (`resolveStreamUrl`)
- node_modules/navidrome-music-player/lib/index.js:169-172 (getPlayIndex), :261-333 (audioListsPlay), :769-792 (onAudioError), :794-854 (handlePlay/onAudioEnd), :1058-1074 (checkCurrentPlayingAudioIsInUpdatedAudioLists), :1195-1205 (getPlayInfoOfNewList), :1330-1339 (updateAudioLists), :1403-1421 (changeAudioLists), :1422-1435 (updatePlayIndex/playByIndex), :2077-2104 (UNSAFE_componentWillReceiveProps), :2146+ (defaultProps)
- Simulation proof: `C:\Users\erhol\AppData\Local\Temp\opencode\radio-dup-sim.js` (run with `node`)
## Resolution

- Implemented Option A in `ui/src/audioplayer/Player.jsx:52-68,103-111`: radio responses are compared by `radioItemId`, pending state, and track ID before dispatching, so unchanged 3-second polls do not feed fresh objects to the player library.
- Implemented the optional `clear: true` replacement route in `ui/src/reducers/playerReducer.js:151-185`. Changed radio items preserve their UUID and use the music player's quiet replacement path, while unchanged reducer syncs return the existing state; this replaces a pending placeholder in place without restarting the seed or appending duplicates.
- Added regression coverage in `ui/src/reducers/playerReducer.test.js:64-134` for in-place resolution, the replacement flag, and unchanged-sync identity.
- Verification:
  - `npx vitest run src/reducers/playerReducer.test.js src/quickpick/provider.test.js src/audioplayer/` — passed, 6 files and 38 tests.
  - `npm run lint` (from `ui`) — passed.
  - `npm run build` (from `ui`) — passed (Vite build; existing chunk-size warnings only).
  - `go test ./core/personalradio/... ./core/music/...` — could not run because `go` is not installed or available on PATH in this environment.

## Follow-up resolution (2026-08-15): auto-download flow fixed server-side

The queue symptom persisted because the server never resolved downloaded radio items. Investigation (live server logs, MusicBrainz/Last.fm API probes, DB + file inspection, and a container-side beets reproduction) found three independent defects:

1. **~50% of downloads fail at the metadata stage.** Last.fm `track.getSimilar` returns `mbid` values that are not valid MusicBrainz recording IDs roughly half the time (verified: IDs like `a209f013-6477-4d54-a982-c3a4252e60ca` / `2b72769a-f72e-3a75-ae97-a91fae433338` return 404 from MusicBrainz for every entity type). `catalog.Recording(mbid)` then fails with `load recording: get recording: data not found` and the job dies before yt-dlp even runs. A minority of failures are MusicBrainz HTTP 503s (rate limiting) with no retry.
   - Fix: `core/music/service.go` now falls back to a catalog search by artist+title (`resolveRecordingBySearch`, new `Catalog.SearchSongs`) when the recording lookup fails, and the musicbrainz client retries transient 429/503/504 responses (3 attempts, `Retry-After` aware) and sends a proper UA (`Navidrome/2 (https://github.com/navidrome/navidrome)`).

2. **beets never writes tags to imported files.** `beet import -A` (ASIS mode) applies `--set` fields to its library DB and uses them for the destination path, but per `beets/importer/tasks.py` (`if write and (self.apply or self.choice_flag == Action.RETAG): item.try_write()`) it does NOT rewrite the file tags. Files therefore kept the yt-dlp-embedded YouTube video title ("Madonna - Vogue (Official Video)") and no `mb_trackid`.
   - Fix: `adapters/beets/client.go` runs a second `beet write` step after import, selecting the imported items by `mb_trackid:<id>` (song) or `albumartist:/album:` (album), so the scanner indexes the clean title/artist and the MusicBrainz recording ID.

3. **The matcher could not resolve completed downloads.** With (2) unfixed, imported files had video titles and empty `mbz_recording_id`, so both `matchByMBID` and the title phase of `matchByTitle` failed and every completed download was marked "succeeded but imported track did not match", the radio item stayed pending, the queue skipped it ("buffers then skips to an already downloaded song"), and the planner re-downloaded the same tracks (duplicates like `Vogue.1.mp3`). With (1)+(2) fixed, valid-MBID downloads match by MBID and fallback-search downloads match by title+artist, so items become ready in place.

Files changed: `adapters/beets/client.go`, `adapters/beets/client_test.go`, `adapters/musicbrainz/client.go`, `adapters/musicbrainz/client_test.go`, `core/music/service.go`, `core/music/service_test.go`.

Verification:
- `go build -tags "netgo sqlite_fts5" ./...` and `go vet` on the same tags — clean (built in `golang:1.26-trixie` container on homelab, since no local Go).
- `go test -tags "netgo sqlite_fts5" -count=1 ./adapters/musicbrainz/... ./adapters/beets/... ./core/music/... ./core/personalradio/... ./core/quickpick/... ./core/matcher/...` — all pass, including new tests for the search fallback, the beets write step, and transient retries.
- `npx vitest run src/reducers/playerReducer.test.js src/audioplayer/` — 5 files, 35 tests passed.
- Live beets reproduction on homelab confirmed `beet write mb_trackid:<id>` rewrites the file title to the clean metadata.
