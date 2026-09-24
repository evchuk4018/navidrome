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

The source of these tags is **not verified**. They may have been written by an earlier download or tagging step, imported from existing files, or supplied by another metadata source. The database counts establish the symptom, not provenance. This change leaves the file tags and database metadata untouched; related-radio matching ignores generic video category values.

## Safe future repair

1. Record a current database backup and a manifest of each available file's path, embedded genre, database genre tags, and source/download job. Preserve the original audio files.
2. Trace a sample of the affected files through the download, tagging, and scanner paths to identify where the category was introduced. Fix that writer before retagging, so a rescan cannot recreate the value.
3. Select a small, reviewed batch with reliable genre evidence. Do not infer a genre from `Music`, artist name alone, or the current Quick Play queue. Keep files with uncertain style untagged until reviewed.
4. Retag the **files** in that batch using a music metadata source or manual review. Make changes atomic per file and retain the manifest for rollback. Do not edit only Navidrome's database rows: the next scan may restore the embedded tags.
5. Rescan the affected library, compare the resulting tags and file counts with the manifest, and check representative related-radio queues. Continue in batches only if the first batch is correct. Restore files and rescan if a batch is wrong.

No retag, database migration, or rescan is part of the Quick Play fix.
