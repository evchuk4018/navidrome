package musicbrainz

import (
	"sort"
	"strings"
	"unicode"

	"github.com/navidrome/navidrome/model"
)

// sortRecordingsByRelevance keeps title relevance ahead of popularity while
// using ListenBrainz counts to rank recordings within the same relevance
// bucket. This matters for queries such as "The One That Got Away", where the
// popular Katy Perry recording competes with many recordings using the same
// title.
func sortRecordingsByRelevance(recordings []mbRecording, query string, popularity ...map[string]model.RecordingPopularity) {
	counts := map[string]model.RecordingPopularity(nil)
	if len(popularity) > 0 {
		counts = popularity[0]
	}

	sort.SliceStable(recordings, func(i, j int) bool {
		leftRelevance := recordingRelevance(recordings[i], query)
		rightRelevance := recordingRelevance(recordings[j], query)
		if leftRelevance != rightRelevance {
			return leftRelevance < rightRelevance
		}

		leftPopularity := counts[recordings[i].ID]
		rightPopularity := counts[recordings[j].ID]
		if leftPopularity.TotalListenCount != rightPopularity.TotalListenCount {
			return leftPopularity.TotalListenCount > rightPopularity.TotalListenCount
		}
		if leftPopularity.TotalUserCount != rightPopularity.TotalUserCount {
			return leftPopularity.TotalUserCount > rightPopularity.TotalUserCount
		}
		if recordings[i].Score != recordings[j].Score {
			return recordings[i].Score > recordings[j].Score
		}
		return recordings[i].ID < recordings[j].ID
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
