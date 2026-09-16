# Handoff: Fix Quick Pick repetition and add controlled churn

## Status

- Repository: evchuk4018/navidrome
- Branch analyzed: main
- HEAD analyzed: 73a8a78a9aedb22f47b08ea6d9bb574542afbeed
- Date: 2026-09-16
- Scope: Quick Pick tile selection, playlist selection, Smart Picks seed selection, recommendation ranking, and persistent display fatigue.
- This handoff supersedes the older resolved radio-download handoff that previously occupied this file. That content remains available in Git history.
- No implementation has been made yet. This file is the implementation plan.

## User-visible problem

Quick Pick keeps showing the same small set of songs and playlists. The user wants the surface to stay strongly personalized, but some slots should rotate through items farther down the ranked list and liked songs so opening or refreshing Quick Pick produces useful churn rather than the exact same top-N result.

The fix should not turn Quick Pick into random library shuffle. It should preserve the strongest favorite as an anchor, then deliberately rotate the remaining slots among high-confidence liked and lower-ranked candidates while avoiding recently displayed items.

## Confirmed diagnosis

### 1. Quick Pick is deterministic top-N selection

The main problem is in core/quickpick/service.go.

At service.go:51, Get builds the candidate pool by unioning only these three fixed windows:

- service.go:60 - top 150 by play_count
- service.go:61 - top 150 by play_date
- service.go:62 - top 100 by starred_at

The union is put into a map keyed by media-file ID, then every candidate is passed to the shared recommendations.Rank function.

The shared ranker in core/recommendations/ranking.go is intentionally deterministic. Rank at ranking.go:203 sorts by descending score, and ties are resolved by stable candidate identity. There is no random tie breaker, no exposure history, no exploration quota, and no display fatigue.

After ranking, Quick Pick simply takes the first items until the nine-tile area is full. This means a user whose listening history changes slowly will see almost the exact same tiles indefinitely.

This behavior has existed since the initial Quick Pick implementation. It is not primarily a recent regression.

### 2. There is no distinction between candidate recall and final composition

Quick Pick currently treats the ranking result as the final display order.

Personal Radio already has the better architecture in this fork:

1. Retrieve a broad candidate pool.
2. Rank candidates.
3. Run a separate composition pass that applies diversity and slot constraints.

See:

- core/personalradio/service.go around localCandidateFilesForFallback at line 1537 for broad fallback recall.
- core/personalradio/composition.go:34 for composeRadioCandidates.
- core/personalradio/composition.go:146 for candidateSelectionScore, which applies artist and album penalties after ranking.

Quick Pick should adopt the same rank-then-compose pattern rather than modifying the shared ranker to become random.

### 3. Quick Pick has no impression or exposure history

The persistence layer records plays, not displays.

persistence/quick_pick_metrics_repository.go currently supplies:

- SongRecentPlays at line 91
- PlaylistMetrics at line 113
- RecordPlaylistPlay at line 141

That means Quick Pick knows what the user played, but it does not know what it showed the user five minutes ago, yesterday, or on the previous page load.

Without exposure history, the server cannot distinguish:

- a high-ranked song that has not been shown recently, from
- the exact same high-ranked song shown on every Quick Pick visit.

A display-fatigue signal is therefore required if the goal is reliable churn rather than probabilistic shuffle.

### 4. There is a real playlist score-scale bug after the shared-ranking refactor

This is separate from the general deterministic behavior and should be fixed even if no exposure system is added.

Before commit 36bd2289f279, songs and playlists both used similarly large logarithmic scoring formulas.

Commit 36bd2289f279 moved song scoring to core/recommendations/ranking.go, but playlist scoring in core/quickpick/service.go stayed on the old logarithmic scale.

Current song Quick Pick scores are approximately bounded by the shared ranker signals. For a normal local song with no provider similarity or transition signals, the major positive contributions are roughly:

- PlayHistory: up to 0.25
- RecentListening: up to 0.75
- Starred: up to 0.75
- TasteAffinity: up to 0.80
- Recency is a penalty of as much as -0.50

A very strong local song therefore tends to live around a 0 to roughly 2.5 scale.

Playlist scoring still uses:

- 3 * log1p(total starts)
- 5 * log1p(recent starts)
- up to +4 from recent last-played recency
- plus 0.35 * max song affinity

A playlist with even a small number of starts can therefore score 5 to 10 or more.

service.go:134 computes playlistSlots, and the following condition compares the third playlist raw score directly to approximately the seventh song raw score.

Those scores are no longer on compatible scales.

Result: the third playlist can appear artificially competitive almost all the time, which makes the same high-history playlists occupy too much of the nine-tile surface.

Do not preserve this cross-type raw-score comparison.

### 5. Playlist scoring actively reinforces recently played playlists

Playlist LastPlayed contributes a positive boost:

4 * exp(-days / 14)

This is reasonable as a preference signal, but because there is no separate display-fatigue signal, the playlist that was just used is also the playlist most likely to keep being shown.

Play preference and display fatigue are different concepts and should remain separate.

### 6. The current playlist candidate query has an alphabetical recall bias

core/quickpick/service.go currently requests:

Playlist.GetAll with Sort: name, Order: asc, Max: 100

If a user has more than 100 visible playlists, playlists after the first 100 alphabetically cannot be considered at all.

This is not the main cause for a small library, but it is a recall bug for larger installations.

Also note that current playlist affinity scoring calls GetWithTracks once per playlist. That is already an N+1 pattern for up to 100 playlists. Do not simply raise the limit to 500 without addressing that cost.

### 7. Liked songs already have a canonical source in this fork

Do not make Quick Pick depend on locating a playlist literally named "liked music".

core/playlists/liked_music.go shows that the automatic Liked Music playlist is synchronized from each user's starred media-file annotation. The canonical user preference is MediaFile.Starred.

Quick Pick already fetches a starred_at window, but it merges that source into the same byID map and loses the information that a candidate came from the liked pool.

For churn, preserve source membership so one slot can explicitly prefer a liked song.

### 8. Smart Picks are seeded by the same top songs on every request

core/quickpick/service.go:160 begins recommendations.

The current logic takes up to recommendationSeedCount = 5 from the beginning of the already deterministic songs slice. Those same top five songs become the Last.fm or similarity-provider seeds on every load.

That means even if the provider is healthy, the request inputs are nearly identical, so Smart Picks naturally repeat.

The Smart Picks seeds should come from the composed main-song selection, which will include the anchor plus liked and deeper-ranked tracks, not blindly from the first five base-ranked tracks.

### 9. Smart Picks do not currently receive the persistent taste affinity attached to the main song pool

In Get, the main ranking candidates are batch-enriched from recommendations.TasteAffinityRepository before ranking.

The recommendation candidates built inside recommendations do not go through the same taste-affinity enrichment step before recommendations.Rank.

The Smart Picks section therefore uses provider similarity, local annotations, and recent-play inputs, but misses the long-term track, artist, genre, and album taste profile that already exists in persistence/taste_affinity_repository.go.

Extract the taste-enrichment helper so both main candidates and recommendation candidates receive it.

### 10. Smart Picks have no display-fatigue signal and no cross-section exclusion

The recommendation ranking call currently supplies Now, RecentPlays, and Limit, but no Fatigue map.

Also, recommendation matching does not receive the IDs already selected for the main nine tiles.

A local track can therefore remain a Smart Pick repeatedly, and there is no strong service-level rule preventing the same local media file from also being present in the main tile section.

Use one canonical exposure identity for a local media file regardless of whether it is displayed as kind=song or kind=recommendation.

### 11. The UI is not the primary source of repetition

ui/src/quickpick/QuickPick.jsx requests GET /quick-pick once on mount at line 105.

The backend order is then rendered as-is. The UI does not independently sort the favorites.

At lines 174-177 it only separates recommendation-kind items from everything else.

A server-side composition fix will therefore change the visible behavior without an API response-shape change.

A manual Refresh button can be added later, but it is not necessary to fix repeated results across visits.

## Recommended behavior

The main nine-tile area should be a composed surface, not raw top-N.

### Song policy

Preserve one stable anchor song:

- The highest base-ranked song is always eligible for the first song anchor slot.
- Keep the existing TestQuickPickKeepsTheRealFavoriteAtTheTop intent.
- Do not let exposure fatigue remove the user's actual strongest favorite from the surface entirely.

For the remaining song slots, deliberately mix these sources:

1. Liked slot
   - Pick a starred song that is not already selected.
   - Prefer the highest exposure-adjusted score.
   - Search the full liked candidate window, not only the first two or three base ranks.

2. Near-exploration slot
   - Prefer a candidate from approximately base rank 5 through 20.
   - Use exposure-adjusted score within this window.
   - If the pool is too small, relax to the general pool.

3. Deep-exploration slot
   - Prefer a candidate from approximately base rank 20 through 60.
   - Use exposure-adjusted score within this window.
   - If fewer than 20 candidates exist, relax to the near window and then the general pool.

4. Fill slots
   - Fill remaining positions from the exposure-adjusted ranking.
   - Apply artist and album diversity penalties during composition.
   - Never leave a slot empty because a preferred bucket is empty. Bucket rules are preferences, not hard failure conditions.

These starting rank windows are intentionally conservative. They pull from farther down a list that is still already personalized, rather than from arbitrary library content.

### Playlist policy

Do not compare playlist raw scores with song raw scores.

Recommended default:

- Use at most 2 playlist slots when there are enough viable songs.
- Allow a third playlist only as a fallback when there are fewer than 6 viable song candidates.
- Rotate playlist selection using normalized playlist relevance plus exposure fatigue.
- Add a modest playlist.Starred preference because playlist favorites are already supported in this fork.
- Do not keep two permanently fixed playlist anchors. The song anchor is enough stability for the page.

A safe normalized playlist selection score is conceptually:

normalized relevance - exposure penalty

Do not subtract a 0 to 1 penalty directly from the current unbounded logarithmic playlist score because it will barely move highly played playlists. Normalize playlist scores within the current candidate set first, or select by rank/percentile.

### Smart Picks policy

- Build Smart Pick seeds from the song tiles selected by composition, not songs[0:5].
- Prefer 4 or 5 distinct selected songs as provider seeds.
- Because the selected main songs now include liked and exploration slots, provider calls will naturally vary.
- Exclude every local media-file ID already present in the main nine tiles.
- Attach taste affinity to matched recommendation candidates.
- Apply the same recent-display exposure fatigue to recommendation candidates.
- Song and recommendation tiles for the same local media-file ID must share the same exposure key.
- Increase recommendationPerSeed from 5 to 8 only if exposure filtering causes the matched recommendation pool to become too small. Start by measuring tests with 5. Do not raise provider calls unnecessarily.

## Persistent exposure design

### New migration

Create:

db/migrations/20260916010000_add_quick_pick_exposure.sql

Recommended schema:

~~~sql
-- +goose Up
create table quick_pick_exposure
(
    user_id       varchar(255) not null references user (id) on delete cascade,
    item_key      varchar(512) not null,
    show_count    integer not null default 0,
    last_shown_at datetime not null,
    primary key (user_id, item_key)
);

create index quick_pick_exposure_user_last_shown
    on quick_pick_exposure (user_id, last_shown_at desc);

-- +goose Down
drop index if exists quick_pick_exposure_user_last_shown;
drop table if exists quick_pick_exposure;
~~~

Use one opaque canonical item_key rather than separate item_type and item_id columns. This keeps batched candidate lookup simple.

Suggested keys:

- track:<media-file-id>
- playlist:<playlist-id>

A recommendation backed by local media file abc uses track:abc, exactly like a normal song tile. This makes cross-section fatigue automatic.

### Why an aggregate table is sufficient

The churn requirement needs:

- whether this item was shown recently,
- how often it has been shown,
- the most recent display time.

It does not currently need a full append-only impression event log.

An aggregate row per user/item is smaller and allows a cheap UPSERT.

If future analytics requires exact impression sequences, an append-only event table can be added separately. Do not overbuild that now.

### Repository interface changes

Edit model/quick_pick.go.

Add something equivalent to:

~~~go
type QuickPickExposureMetric struct {
    ItemKey     string
    ShowCount   int64
    LastShownAt time.Time
}
~~~

Extend QuickPickMetricsRepository with:

~~~go
ExposureMetrics(userID string, itemKeys []string) (map[string]QuickPickExposureMetric, error)
RecordExposures(userID string, itemKeys []string, shownAt time.Time) error
~~~

Names can vary, but keep the interface generic around item keys. Do not make persistence depend on QuickPickItem JSON objects.

### Repository implementation

Edit persistence/quick_pick_metrics_repository.go.

Implement ExposureMetrics as a batched IN query:

~~~sql
select item_key, show_count, last_shown_at
from quick_pick_exposure
where user_id = ?
  and item_key in (?, ?, ...)
~~~

Important details:

- Deduplicate requested keys first.
- Chunk keys so SQLite parameter limits are never approached. 300 to 500 keys per query is safe.
- Reuse nullableSQLiteTime for last_shown_at if useful, although the schema is not nullable.
- Return an empty map for zero keys.
- Scope every query by user_id.

Implement RecordExposures as one transaction and UPSERT each unique key:

~~~sql
insert into quick_pick_exposure
    (user_id, item_key, show_count, last_shown_at)
values (?, ?, 1, ?)
on conflict (user_id, item_key) do update set
    show_count = quick_pick_exposure.show_count + 1,
    last_shown_at = excluded.last_shown_at
~~~

Recording display history is non-critical personalization telemetry. Recommended failure mode in the Quick Pick service is fail-open:

- log the exposure write failure,
- still return the composed response,
- do not turn a valid Quick Pick page into HTTP 500 because impression recording failed.

Reading exposure history can also fail-open to an empty map if desired. Existing play metrics should remain hard errors because they are part of the current behavior. Keep this distinction explicit.

## Exposure-fatigue function

Add the Quick Pick-specific fatigue calculation in the Quick Pick package, not in core/recommendations/ranking.go.

Reason: the shared ranker is also used by Personal Radio. Changing global default weights or global score semantics for a surface-level churn requirement risks changing radio behavior.

Suggested starting function:

~~~go
func exposureFatigue(metric model.QuickPickExposureMetric, now time.Time) float64 {
    if metric.LastShownAt.IsZero() || now.IsZero() {
        return 0
    }

    age := now.Sub(metric.LastShownAt.UTC())
    if age < 0 {
        age = 0
    }

    // Strongly suppress an item immediately after display, then decay.
    recency := math.Exp(-float64(age) / float64(36*time.Hour))

    // Repeatedly displayed items receive a slightly stronger temporary penalty,
    // but the effect still decays with age.
    countFactor := math.Min(1, 0.70+0.10*math.Log1p(float64(metric.ShowCount)))
    return clamp(recency*countFactor, 0, 1)
}
~~~

The exact 36-hour value is tunable. The important behavior is:

- just shown: penalty near 1,
- one to two days later: materially reduced,
- old exposure: penalty approaches 0,
- no permanent punishment for a historically common favorite.

For songs, feed the resulting map into recommendations.Options.Fatigue. The shared ranker already supports Fatigue and subtracts it with default weight 1.

For playlists, use the same fatigue value inside normalized playlist composition rather than subtracting it from the raw logarithmic playlist score.

## Candidate recall changes

### Main song pool

Refactor the top of core/quickpick/service.go into a helper, preferably in a new file:

core/quickpick/candidates.go

Suggested constants:

~~~go
const (
    playCountPoolSize = 200
    recentPoolSize    = 200
    likedPoolSize     = 250
    fallbackPoolMin   = 40
    fallbackRandomMax = 75
)
~~~

Exact sizes can be tuned.

Build a candidate metadata map rather than only map[string]MediaFile:

~~~go
type recalledSong struct {
    file      model.MediaFile
    fromPlays bool
    fromRecent bool
    fromLiked bool
}
~~~

For the starred_at query:

- mark fromLiked only when file.Starred is actually true,
- do not assume every result returned after the last starred row is liked.

The repository sort mapping for starred_at sorts by starred and starred_at, so liked files should lead the result, but explicit file.Starred checking keeps the contract clear.

If the union is smaller than fallbackPoolMin, optionally call MediaFile.GetRandom with a small Max and merge those items as fallback recall. Do not use random library candidates on every healthy request. Random fallback is for sparse histories and new users, not the normal churn mechanism.

### Base ranking and adjusted ranking

Rank the same personalized candidates twice:

1. Base rank
   - taste affinity
   - recent plays
   - current shared ranking signals
   - no exposure fatigue

2. Adjusted rank
   - identical candidate features
   - same recent plays
   - same taste
   - Fatigue populated from Quick Pick exposure metrics

Use base rank for:

- the stable anchor,
- rank-window membership,
- playlist track-affinity scoring.

Use adjusted rank for:

- liked-slot choice,
- near/deep exploration choice within their base-rank windows,
- general fill order.

This prevents exposure history from changing what "rank 20" means while still rotating candidates inside each relevance bucket.

### Song candidate struct

Replace the current two-field songCandidate with enough information for composition, for example:

~~~go
type songCandidate struct {
    song          model.MediaFile
    baseScore     float64
    adjustedScore float64
    baseRank      int
    fromLiked     bool
}
~~~

Do not rely on list indexes after several re-sorts. Store baseRank explicitly.

## New Quick Pick composition layer

Create:

- core/quickpick/composition.go
- core/quickpick/composition_test.go

The service should orchestrate data loading. Composition should own display policy.

Suggested shape:

~~~go
type compositionOptions struct {
    Limit     int
    Now       time.Time
    Exposures map[string]model.QuickPickExposureMetric
}

type composedQuickPick struct {
    Items     []model.QuickPickItem
    SeedSongs []model.MediaFile
}

func composeQuickPick(
    songs []songCandidate,
    playlists []playlistCandidate,
    options compositionOptions,
) composedQuickPick
~~~

The exact API can differ. The important separation is that service.go should no longer contain all of the slot policy inline.

### Composition algorithm

1. Select playlists first only to determine quota, not to force them to the beginning semantically.
2. Default playlist quota:
   - min(2, number of viable playlists)
   - if fewer than 6 viable songs, allow a third playlist
   - remove the current cross-type raw score comparison entirely.
3. Song slot count = 9 - selected playlist count.
4. Add the best base-ranked song as anchor.
5. Add a liked candidate if available and not selected.
6. Add one candidate from base ranks 5 to 20.
7. Add one candidate from base ranks 20 to 60.
8. Fill remaining song slots from adjusted rank.
9. During steps 5 through 8, apply a small artist/album repetition penalty, using the same conceptual approach as Personal Radio composition.
10. If any preferred bucket is empty, skip it and fill later from general adjusted rank.
11. Dedupe by local media-file ID.
12. Never return fewer items solely because the composition policy could not satisfy a preferred source.

### Diversity penalty

Copy the architecture, not necessarily the exact constants, from core/personalradio/composition.go.

Suggested starting penalties for a nine-tile visual surface can be gentler than radio:

- repeated artist: -0.40 for first repeat, then -0.25 additional each repeat
- repeated album: -0.20 for first repeat, then -0.15 additional each repeat

Do not penalize the anchor itself. Use penalties when choosing subsequent slots.

Because adjusted song scores are on the shared ranker's small scale, these penalties are meaningful.

### Playlist composition

Change playlistCandidate to carry at least:

~~~go
type playlistCandidate struct {
    playlist       model.Playlist
    baseScore      float64
    normalizedScore float64
    adjustedScore  float64
}
~~~

Recommended base score changes:

- keep existing play-history terms initially,
- keep LastPlayed preference,
- keep track affinity,
- add a modest playlist.Starred bonus, for example +1.0 before normalization.

Then normalize all viable playlist base scores into 0 to 1. If every playlist has the same score, treat them as equal relevance rather than divide by zero.

Adjusted playlist score:

~~~text
0.75 * normalized relevance
+ 0.25 * starred signal
- 0.70 * exposure fatigue
~~~

If starred is already included in base relevance, do not double-count it. Choose one location.

The point is not the exact constants. The point is to compare exposure penalty to a normalized relevance signal.

For two slots:

- choose highest adjusted score,
- then choose the next highest after applying an optional small repeated-owner or overlap policy if desired.
- Because playlist contents can heavily overlap, a future refinement could penalize high track overlap, but do not make that a prerequisite for this fix.

### Playlist N+1 warning

Current playlist affinity calls GetWithTracks for every fetched playlist.

Do not increase the playlist candidate count aggressively while this remains.

For this change:

- keep the existing 100 visible playlist cap or make only a small increase,
- change the sort from name asc if desired to updated_at desc or another relevance-oriented ordering,
- use exposure churn within that set.

A later optimization can batch playlist-track affinity in SQL.

If you decide to solve the alphabetical recall bias now, a safer implementation is:

1. fetch visible playlists,
2. pre-score using playlist play metrics plus playlist starred state,
3. take a shortlist,
4. only call GetWithTracks for that shortlist to add song affinity,
5. finalize ranking.

That removes the N+1 pressure from a larger recall pool.

## Smart Picks changes

Refactor recommendations in core/quickpick/service.go, or move most of it into core/quickpick/candidates.go.

### Seed selection

Change the function input from the full ranked songs list to selected seed songs from composition.

Current:

~~~go
recommendations(ctx, songs, now, recent)
~~~

Recommended:

~~~go
recommendations(ctx, userID, selectedSeedSongs, excludedMediaFileIDs, now, recent, exposures)
~~~

Pass userID because recommendation candidates need taste-affinity lookup.

### Taste enrichment

Extract the repeated taste code into a helper:

~~~go
func (s *service) applyTasteAffinities(
    userID string,
    candidates []recommendations.Candidate,
)
~~~

Use it for:

- base Quick Pick song candidates,
- matched Smart Pick candidates.

The persistence implementation already supplies recommendations.TasteAffinityRepository through the concrete Quick Pick metrics repository, so this should not require a new constructor or Wire provider.

### Recommendation exposure

After MatchSongsIndexed produces local media files:

- build track:<local.ID> exposure keys,
- fetch missing exposure metrics for these candidates,
- create Fatigue keyed by local media-file ID,
- call recommendations.Rank with Fatigue.

Do not change the global ranking defaults.

### Cross-section dedupe

Pass a set of local media-file IDs already chosen for the main section.

Reject recommendation matches whose local.ID is in that set before ranking.

Also dedupe matched recommendation candidates by the same canonical local track identity. Provider-level CandidateID handles provider duplicates, but final display identity should be the actual local media file.

### Recommendation count

Keep recommendationLimit = 4.

Initially keep recommendationSeedCount and recommendationPerSeed conservative. If tests show exposure and dedupe frequently leave fewer than four Smart Picks, raise recommendationPerSeed from 5 to 8 before increasing seed count. This increases breadth with fewer provider requests than adding more seeds.

## Recording impressions

After the complete response has been composed, build canonical exposure keys for every returned tile:

- song -> track:<song.ID>
- recommendation -> track:<song.ID>
- playlist -> playlist:<playlist.ID>

Deduplicate keys before recording. A song that somehow appears twice should count as one page impression, not two.

Call RecordExposures only after the response is successfully built.

Recommended behavior:

~~~go
if err := s.metrics.RecordExposures(userID, keys, now); err != nil {
    log.Warn(ctx, "Unable to record Quick Pick exposures", "userID", userID, "error", err)
}
~~~

Then return the response normally.

GET gaining a non-critical impression side effect is acceptable for this internal native endpoint and is the smallest implementation. If strict HTTP semantics are later desired, move impressions to a POST from the UI, but that adds another round trip and is not needed for this fix.

## File-by-file implementation map

### REQUIRED: core/quickpick/service.go

Current important locations:

- line 51: Get
- lines 60-62: three fixed song recall windows
- line 134: playlist slot allocation
- line 160: recommendations

Changes:

1. Move candidate-recall and composition details out to helpers/new files.
2. Preserve source flags for liked-song membership.
3. Apply taste once through a reusable helper.
4. Produce both base and exposure-adjusted song rankings.
5. Remove playlist-vs-song raw-score slot comparison.
6. Use composeQuickPick to build main items.
7. Seed Smart Picks from selected/composed songs.
8. Pass main media-file IDs as exclusions to Smart Picks.
9. Apply taste and exposure fatigue to Smart Picks.
10. Record final exposures fail-open.
11. Keep RecordPlaylistPlay behavior unchanged.

### REQUIRED: core/quickpick/candidates.go - new

Responsibilities:

- recall song candidates from play count, play date, starred, and sparse-history fallback;
- preserve source membership such as fromLiked;
- attach taste identities;
- construct base and adjusted rankings;
- build canonical track/playlist exposure keys;
- optionally contain playlist normalization helpers;
- contain no HTTP behavior.

This keeps service.go from becoming another large recommendation orchestrator.

### REQUIRED: core/quickpick/composition.go - new

Responsibilities:

- enforce nine-tile policy;
- preserve one best-song anchor;
- choose liked slot;
- choose near and deep rank-window slots;
- fill from adjusted ranking;
- apply artist/album diversity;
- choose normalized exposure-aware playlists;
- return selected songs for Smart Pick seeds.

No database access should live here.

### REQUIRED: core/quickpick/composition_test.go - new

Use fixed candidates and fixed timestamps.

Cover:

1. best base-ranked favorite remains present as anchor;
2. recently exposed non-anchor top song loses to a nearby candidate;
3. liked song below the top ranks receives the liked slot;
4. near rank-window candidate is selected;
5. deep rank-window candidate is selected;
6. duplicate media-file IDs cannot occupy multiple slots;
7. repeated artist is deprioritized when comparable alternatives exist;
8. small candidate pool relaxes bucket rules and still fills as many slots as possible;
9. playlist quota defaults to 2 and does not use cross-type raw score;
10. playlist recently shown is rotated behind a similarly relevant alternative;
11. fewer than 6 viable songs can permit a third playlist;
12. exposure penalty decays with age.

### REQUIRED: core/quickpick/service_test.go

Current test at line 39 explicitly requires the real favorite to stay at the top when no playlist displaces it. Preserve the intent.

Extend fakeMetrics to implement the new exposure methods.

Add service-level tests:

- response records exposure keys after successful selection;
- exposure write failure does not fail the response;
- main and Smart Pick sections do not duplicate a local media-file ID;
- Smart Pick provider seeds include composed liked/deeper songs rather than always raw top five;
- recommendation candidate receives taste affinity when repository provides it;
- recommendation candidate receives exposure fatigue;
- recently shown Smart Pick is rotated when an alternative exists;
- liked song can surface even when below the original top-N display positions;
- repeated GET with the first result recorded as exposure produces changed non-anchor slots;
- sparse library still returns a useful response.

### REQUIRED: model/quick_pick.go

Changes:

- add QuickPickExposureMetric or equivalent;
- extend QuickPickMetricsRepository with exposure read/write methods;
- optionally add canonical item-key constants/helpers here if multiple packages need them.

Do not change the public JSON shape of QuickPickResponse unless there is a separate reason.

### REQUIRED: persistence/quick_pick_metrics_repository.go

Changes:

- implement batched exposure lookup;
- implement transactional exposure UPSERT;
- keep all reads/writes user scoped;
- reuse current SQL time handling;
- dedupe keys.

Do not put ranking or decay math in persistence.

### REQUIRED: persistence/quick_pick_metrics_repository_test.go

Add real SQL tests for:

1. empty lookup returns empty map;
2. first impression creates count=1 and last_shown_at;
3. second impression increments count and advances last_shown_at;
4. two users with the same item_key remain isolated;
5. batched lookup returns only requested keys;
6. duplicate keys in one RecordExposures call count once for the page;
7. transaction behavior remains consistent on error.

### REQUIRED: db/migrations/20260916010000_add_quick_pick_exposure.sql

Add aggregate exposure table and index described above.

Follow existing Goose Up/Down conventions.

### LIKELY NO CHANGE: core/recommendations/ranking.go

It was inspected deeply.

Do not add randomness or Quick Pick-specific exposure behavior to shared defaults.

The ranker already exposes exactly what Quick Pick needs through Options.Fatigue.

Only touch this file if a truly generic helper is required and can be demonstrated not to alter Personal Radio.

### LIKELY NO CHANGE: core/personalradio/composition.go

Reference implementation only.

Use its rank-then-compose architecture and diversity concept.

Do not couple Quick Pick to Personal Radio internal structs.

### LIKELY NO CHANGE: core/personalradio/service.go

Reference implementation only.

Its exhaustive local fallback proves this fork already separates candidate recall from ranking.

No Quick Pick fix should require changing Personal Radio behavior.

### LIKELY NO CHANGE: core/playlists/liked_music.go

Reference implementation only.

It confirms MediaFile.Starred is the canonical liked-song signal.

Do not query or mutate the automatic playlist for Quick Pick selection.

### POSSIBLE: tests/mock_mediafile_repo.go

Current mock GetAll returns all files sorted by ID and does not faithfully honor the production Sort/Max behavior.

Avoid changing this global mock unless several tests need production-like query behavior. A Quick Pick-specific fake repository in the Quick Pick test package is safer and less likely to break unrelated tests.

If the production code begins depending on Offset or Max during recall, make sure the Quick Pick test fake models those operations accurately.

### POSSIBLE: tests/mock_playlist_repo.go

Same caution. The mock GetAll returns its All slice without applying sort/max.

Use a Quick Pick-local fake if playlist candidate-window behavior needs exact simulation.

### OPTIONAL PHASE 2: ui/src/quickpick/QuickPick.jsx

Server-side churn is sufficient for visits/reloads.

Optional improvement:

- add a Refresh Quick Pick button;
- extract loading into a callback;
- clicking refresh calls GET /quick-pick again;
- because the previous result has already been recorded as exposure, the refreshed response should rotate non-anchor slots;
- disable button while loading;
- do not clear existing tiles until replacement succeeds.

If this is added, also create a component test. There is currently no QuickPick component test file in this fork.

### NO CHANGE EXPECTED: ui/src/quickpick/provider.js

GET /quick-pick can remain unchanged unless the optional refresh UX requires no new API features.

### NO CHANGE EXPECTED: server/nativeapi/quick_pick.go

The route can keep calling quickPick.Get with user.ID.

No request parameter is needed for the recommended design.

### NO CHANGE EXPECTED: cmd/wire.go / cmd/wire_gen.go / injectors

The exposure methods belong on the existing QuickPickMetricsRepository implementation.

Do not create a separate repository constructor. Keeping one existing dependency means Wire should not need regeneration for this feature.

## Important score and behavior cautions

### Do not globally lower PlayHistory or Starred weights

Those weights are shared with Personal Radio.

The current problem is surface composition, not necessarily the quality of the shared relevance score.

### Do not use random shuffle as the main solution

Random shuffle can:

- remove the user's actual favorite,
- return irrelevant long-tail tracks,
- repeat by chance,
- make tests flaky,
- make behavior hard to debug.

Use deterministic exposure-aware rotation first. Random recall is only a sparse-history fallback.

### Do not hard-exclude recently shown items

If a user has five songs total, hard exclusion can make the surface empty.

Use soft fatigue, then relax bucket/diversity rules when needed.

### Do not treat recent listening as the same thing as recent display

A user may want to play a song repeatedly but not need the same song occupying every recommendation slot.

Listening preference and surface fatigue should remain separate signals.

### Do not compare raw playlist and song scores

They are currently different scoring systems and scales.

Slot quota should be policy-based or normalized first.

### Do not create a dependency on the Liked Music playlist name

Use starred annotations.

The automatic playlist is a synchronized representation, not the source of truth.

## Performance notes

### Songs

The existing three song queries already hydrate annotation and artwork data.

Increasing the windows modestly is acceptable, but avoid loading the whole library on each page view.

Because current union size can already approach 400 unique tracks, the first churn implementation can work mostly within the existing pool.

Only use GetRandom fallback when the recalled pool is too small.

### Exposure lookup

Candidate-specific item_key lookup keeps the exposure query bounded.

Chunk IN lists to protect SQLite limits.

The final page contains only 9 main items plus up to 4 Smart Picks, so recording impressions is tiny.

### Playlists

Current playlist affinity is the biggest scaling concern because it performs GetWithTracks per candidate.

Do not significantly expand the playlist pool until that path is batched or shortlisted.

### Provider calls

Changing Smart Pick seeds to composed songs should provide more variation without more provider calls.

Raise recommendationPerSeed only after tests or logs show candidate exhaustion.

## Suggested implementation order

1. Add migration for quick_pick_exposure.
2. Extend model/quick_pick.go repository contract.
3. Implement persistence read/write and its tests.
4. Add canonical exposure key helpers.
5. Add candidates.go and preserve liked-source membership.
6. Add base and exposure-adjusted song ranking.
7. Add composition.go and unit tests.
8. Replace current playlistSlots raw-score comparison with fixed/policy quota.
9. Normalize and exposure-adjust playlist selection.
10. Extract reusable taste-affinity enrichment.
11. Seed Smart Picks from composed main songs.
12. Add Smart Pick exposure fatigue and main-section exclusion.
13. Record final page exposures fail-open.
14. Extend service tests.
15. Run Go tests and full build.
16. Only then consider optional UI refresh button.

## Verification commands

At minimum:

~~~sh
go test ./core/quickpick/... ./core/recommendations/... ./persistence/...
go test ./core/personalradio/...
go test ./...
go build ./...
~~~

If the optional UI refresh control is added:

~~~sh
cd ui
npx vitest run src/quickpick/
npm run lint
npm run build
~~~

## Acceptance criteria

The change is complete when all of these are true:

1. The strongest favorite song remains available as a stable anchor.
2. Reopening or refreshing Quick Pick does not keep all non-anchor song tiles identical when there are enough candidates.
3. At least one liked song can surface even if it is not in the top few base ranks.
4. At least one slot can come from materially farther down the personalized rank list.
5. Recently displayed non-anchor items are temporarily deprioritized, not permanently banned.
6. Playlist display rotates when alternatives exist.
7. Quick Pick no longer decides playlist slot count by comparing raw playlist scores to raw song scores.
8. More than two playlists appear only because song supply is insufficient or a clearly defined normalized policy says so, not because of the old score mismatch.
9. Smart Pick seeds vary with the composed main-song set.
10. Smart Picks apply long-term taste affinity.
11. Smart Picks apply exposure fatigue.
12. The same local media file cannot appear in both the main section and Smart Picks in one response.
13. A song displayed as a Smart Pick and later as a normal song shares the same exposure history.
14. Sparse/new-user libraries degrade gracefully and do not return short pages solely because churn rules are too strict.
15. Exposure persistence failure does not prevent Quick Pick from loading.
16. Personal Radio ranking behavior is unchanged.
17. Existing Quick Pick favorite-anchor test remains semantically valid.
18. New persistence and composition tests pass deterministically.

## Smallest acceptable patch if the full exposure system must be staged

If this needs to be split into two PRs, do not fall back to pure randomness.

PR 1:

- remove the incompatible playlist-vs-song raw score comparison;
- cap playlists at 2 unless fewer than 6 songs exist;
- add a dedicated liked-song slot;
- add near/deep rank-window composition using deterministic rank windows;
- diversify Smart Pick seeds from the composed songs;
- dedupe Smart Picks against the main section.

PR 2:

- add persistent quick_pick_exposure;
- add fatigue-based rotation;
- add exposure-aware playlist selection;
- add exposure-aware Smart Picks.

PR 1 will create more variety, but PR 2 is what prevents the surface from converging back to the same choices on repeated visits.

## Final recommendation

Implement the full exposure-aware composition design.

The repeated output is not primarily a Last.fm problem and not a UI rendering problem. The current Quick Pick backend is doing exactly what it was written to do: deterministically rank a narrow preference pool and take the first items.

The correct fix is to add a Quick Pick-specific composition layer on top of relevance ranking, with:

- one stable song anchor,
- explicit liked-song representation,
- deliberate near/deep rank exploration,
- persistent display fatigue,
- normalized playlist selection,
- varied Smart Pick seeds,
- cross-section dedupe.

That preserves personalization while giving the page controlled churn.
