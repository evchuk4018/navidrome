package musicbrainz

import (
	"sort"
	"strings"
	"unicode"
)

// sortRecordingsByRelevance keeps MusicBrainz's ordering for equally relevant
// recordings while ensuring title matches are shown before broad metadata
// matches. This matters for queries such as "The One That Got Away", where
// artist-name matches can otherwise appear ahead of the requested recording.
func sortRecordingsByRelevance(recordings []mbRecording, query string) {
	sort.SliceStable(recordings, func(i, j int) bool {
		return recordingRelevance(recordings[i], query) < recordingRelevance(recordings[j], query)
	})
}

func recordingRelevance(recording mbRecording, query string) int {
	queryText := normalizeSearchText(query)
	title := normalizeSearchText(recording.Title)
	artist := normalizeSearchText(artistCreditName(recording.ArtistCredit))

	switch {
	case queryText == "" || title == "":
		return 3
	case title == queryText:
		return 0
	case strings.Contains(title, queryText) || strings.Contains(queryText, title):
		return 1
	case artist != "" && (artist == queryText || strings.Contains(artist, queryText) || strings.Contains(queryText, artist)):
		return 2
	default:
		return 3
	}
}

func normalizeSearchText(value string) string {
	var normalized strings.Builder
	spacePending := false

	for _, character := range strings.ToLower(value) {
		if unicode.IsLetter(character) || unicode.IsNumber(character) {
			if spacePending && normalized.Len() > 0 {
				normalized.WriteByte(' ')
			}
			normalized.WriteRune(character)
			spacePending = false
			continue
		}
		spacePending = true
	}

	return normalized.String()
}
