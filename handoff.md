# Deferred external lyric search

Lyric-body search is intentionally out of scope for the external catalog search change. A future implementation should introduce a provider-neutral contract similar to:

```go
type LyricsSearchProvider interface {
	SearchLyrics(ctx context.Context, query string, limit int) ([]LyricsSearchCandidate, error)
}

type LyricsSearchCandidate struct {
	RecordingMBID       string
	ISRC                string
	Artist              string
	Title               string
	LicensedSnippet     string
	Confidence          float64
	Attribution         string
	LicenseRestrictions string
}
```

Every candidate must resolve to a canonical MusicBrainz recording by recording MBID or ISRC before entering mixed results. Unresolved provider identifiers must never be returned to clients. Lyric-body matches belong below title, artist, and album matches, but above unrelated secondary-metadata-only matches.

The provider contract and UI must carry visible snippet attribution and applicable license restrictions. Providers must explicitly permit search-result snippets; full lyrics must not be cached or exposed unless their license permits it. Cache keys should be privacy-safe hashes, caches must be bounded and honor provider retention limits, and logs and metrics must never contain raw queries or lyric text.

Failures are enrichment failures: timeouts, malformed responses, or provider unavailability should yield canonical partial results rather than fail the search. Requests must support cancellation, rate limits, singleflight, and short bounded timeouts.

The UI should mark a lyric match, show only the licensed snippet and attribution, preserve server ordering, and use the normal canonical recording action. Tests must cover canonical resolution, missing IDs, confidence thresholds, tier placement, licensing and attribution rendering, privacy-safe diagnostics, cache expiry and bounds, cancellation, timeouts, partial failure, and injection or malformed-response cases.
