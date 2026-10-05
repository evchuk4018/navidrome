# Library genre tags and related radio

## Observation (homelab, 2026-09-23)

The Navidrome database contained 966 media files, 833 available for playback. The 833 available files had these `genre` tag counts:

| Tag value | Available tracks |
| --- | ---: |
| Music | 761 |
| People & Blogs | 38 |
| Entertainment | 19 |
| Gaming | 2 |
| Travel & Events | 2 |
| Comedy | 1 |
| Education | 1 |
| Film & Animation | 1 |
| Howto & Style | 1 |
| No genre tag | 7 |

These are video category labels, not useful musical genres. In particular, `Music` covered 761/833 available tracks (91%). It made Quick Play's previous genre match treat Demi Lovato's “Heart Attack” as close to Radiohead's “Creep” and Drake's “0 to 100”. The Heart Attack related-radio session had 676 local fallback candidates. In contrast, Instant Mix used the seed artist and similar artists' top songs when ListenBrainz returned no track matches, producing a much closer queue. Last.fm was disabled; the two seed-track ListenBrainz requests tested at the time returned empty lists.

At the time of this 2026-09-23 snapshot, the source of these tags was not
verified. The follow-up trace recorded below verified that the download path
maps YouTube categories into the embedded genre field and that Beets preserves
them. The database counts establish the historical symptom; the later source
finding and repair workflow are documented below.

## Safe future repair

1. Record a current database backup and a manifest of each available file's path, embedded genre, database genre tags, and source/download job. Preserve the original audio files.
2. Trace a sample of the affected files through the download, tagging, and scanner paths to identify where the category was introduced. Fix that writer before retagging, so a rescan cannot recreate the value.
3. Select a small, reviewed batch with reliable genre evidence. Do not infer a genre from `Music`, artist name alone, or the current Quick Play queue. Keep files with uncertain style untagged until reviewed.
4. Retag the **files** in that batch using a music metadata source or manual review. Make changes atomic per file and retain the manifest for rollback. Do not edit only Navidrome's database rows: the next scan may restore the embedded tags.
5. Rescan the affected library, compare the resulting tags and file counts with the manifest, and check representative related-radio queues. Continue in batches only if the first batch is correct. Restore files and rescan if a batch is wrong.

No retag, database migration, or rescan was part of the Quick Play fix at
that time. The existing-library repair is handled separately by the reviewed
workflow below.

## Resolved source and conservative repair workflow (homelab, 2026-10-05)

The preseed/download path is now traced. `yt-dlp`'s `FFmpegMetadataPP`
constructs the genre metadata from the YouTube `genre`, `genres`,
`categories`, and `tags` fields. For these downloads that field is the video
category (`Music`, `People & Blogs`, and similar), and Beets preserves it
when `import -A` records the item. The downloader now passes a final
`Metadata+ffmpeg_o:-metadata genre=` argument, which removes that category
after yt-dlp has assembled the other metadata while retaining the source URL
comment, title, and artist. The temporary silent-MP3 self-test in
`deploy/repair_music_genres.py` exercises this exact metadata postprocessor
path.

The 2026-10-04 audit found 835 available tracks, with no missing Navidrome
media-file IDs, 112 (13.41%) missing recording MBIDs, and 7 (0.84%) missing
genre tags. Every genre value that was present was a YouTube category, so the
library had no trustworthy embedded musical genre values to use as a fallback.
The repair utility therefore keeps existing musical or mixed genres and only
proposes replacements for blank or category-only files. It accepts an artist
only after a representative recording has one exact normalized MusicBrainz
artist credit, then uses at most two positive artist genre votes as explicitly
labelled artist-level fallback evidence. Multi-artist, mismatched, missing,
and unsupported evidence is skipped.

Before the genre lookup, the recording-ID repair applied nine reviewed exact
MusicBrainz matches. The follow-up scanner check reports 835 available tracks
and 103 still missing recording MBIDs. The coherent pre-enrichment snapshots
are retained at
`/data/metadata-repair-backups/20261004-before-enrichment/navidrome.db` and
`/data/metadata-repair-backups/20261004-before-enrichment/navidrome-beets.db`.
The ID-repair manifest and per-file backups are kept in that same durable
backup directory.

The completed primary genre preview is recorded at
`/data/metadata-repair-backups/20261004-before-enrichment/genre-manifest-parent.json`.
It reviewed 341 artist groups using 448 cached/rate-limited MusicBrainz
requests, found 590 candidate files across 152 verified artist groups, and
skipped 245 files for ambiguous/mismatched anchors or absent artist genres.
The remaining 134 rows were already marked missing by Navidrome. No audio
file or database row was changed by this preview. The focal `TREFUEGO`
tracks were kept out of the primary preview because they have no recording
MBID. Two independent MusicBrainz recording credits identify the same
TREFUEGO artist MBID, so a supplementary artist-level lookup was safe to
attempt without choosing either recording; that artist endpoint returned no
positive genre votes, so no TREFUEGO genre supplement was proposed.

Copy the helper scripts into the durable tools directory first; the published
Navidrome image contains the server binary but does not bundle these repair
helpers. Run the preview from that directory with the same cache and rate-lock
used by any recording-ID repair. The preview reads the database and files and
writes only its manifest and MusicBrainz cache:

```text
python /data/metadata-repair-tools/genre-tools/repair_music_genres.py --dry-run \
  --db /data/navidrome.db --music-root /music \
  --manifest /data/metadata-repair-backups/20261004-before-enrichment/genre-manifest.json \
  --cache-dir /data/metadata-repair-backups/20261004-before-enrichment/musicbrainz-genre-cache \
  --rate-lock /data/musicbrainz-repair.rate.lock
```

Review the per-file source URLs, hashes, paths, old genres, and proposed
genres before applying. The apply mode copies every changed audio file to a
backup directory outside `/music`, records each backup before changing the
live inode, edits only the genre field on a temporary same-format copy, and
atomically replaces the original while preserving all other native Mutagen
frames. It rechecks the database row, resolved path, original file hash,
native-frame hash, and old genre list at apply time:

```text
python /data/metadata-repair-tools/genre-tools/repair_music_genres.py --apply --yes \
  --db /data/navidrome.db --music-root /music \
  --manifest /data/metadata-repair-backups/20261004-before-enrichment/genre-manifest.json \
  --backup-dir /data/metadata-repair-backups/20261004-before-enrichment/genre-audio-backup-YYYYMMDD
```

After a reviewed file apply, `deploy/sync_beets_genres.py` can update only
the exact matching Beets item rows through Beets' `Library`/`Item` API. It
stores the old and new multi-value lists in the same manifest so a later
rollback restores only those rows; it skips rows whose Beets genre became
musical or mixed during review. Its dry-run opens a coherent temporary SQLite
snapshot, so Beets migrations cannot change the live library while planning.
Pass the live Beets config and `/music` explicitly; the default Beets music
directory would otherwise be the container user's home directory. Run a scan
after the file and Beets steps.
Both layers can be rolled back with their manifests; audio rollback touches
the restored files so the scanner sees the restoration:

```text
python /data/metadata-repair-tools/genre-tools/sync_beets_genres.py --dry-run \
  --manifest /data/metadata-repair-backups/20261004-before-enrichment/genre-manifest.json \
  --beets-db /data/navidrome-beets.db \
  --beets-config /data/navidrome-beets.yaml --music-root /music
python /data/metadata-repair-tools/genre-tools/sync_beets_genres.py --apply --yes \
  --manifest /data/metadata-repair-backups/20261004-before-enrichment/genre-manifest.json \
  --beets-db /data/navidrome-beets.db \
  --beets-config /data/navidrome-beets.yaml --music-root /music
python /data/metadata-repair-tools/genre-tools/repair_music_genres.py --rollback --yes \
  --db /data/navidrome.db --music-root /music \
  --manifest /data/metadata-repair-backups/20261004-before-enrichment/genre-manifest.json
python /data/metadata-repair-tools/genre-tools/sync_beets_genres.py --rollback --yes \
  --manifest /data/metadata-repair-backups/20261004-before-enrichment/genre-manifest.json \
  --beets-db /data/navidrome-beets.db \
  --beets-config /data/navidrome-beets.yaml --music-root /music
```
