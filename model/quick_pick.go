package model

import "time"

const (
	QuickPickSong     = "song"
	QuickPickPlaylist = "playlist"
)

// QuickPickRecommendationKind is the tile kind for Last.fm-driven smart picks.
const QuickPickRecommendationKind = "recommendation"

type QuickPickRecommendation struct {
	Title         string `json:"title"`
	Artist        string `json:"artist,omitempty"`
	Album         string `json:"album,omitempty"`
	RecordingMBID string `json:"recordingMbid,omitempty"`
}

type QuickPickItem struct {
	Kind           string                   `json:"kind"`
	Song           *MediaFile               `json:"song,omitempty"`
	Playlist       *Playlist                `json:"playlist,omitempty"`
	Recommendation *QuickPickRecommendation `json:"recommendation,omitempty"`
	Score          float64                  `json:"-"`
}

type QuickPickResponse struct {
	Items []QuickPickItem `json:"items"`
}

type PlaylistPlayMetric struct {
	PlaylistID   string
	TotalStarts  int64
	RecentStarts int64
	LastPlayed   *time.Time
}

type SongRecentPlayMetric struct {
	MediaFileID string
	RecentPlays int64
}

// QuickPickExposureMetric is the aggregate display history for one Quick Pick
// item. ItemKey is intentionally opaque to persistence so tracks can share
// their history across normal and Smart Pick tiles.
type QuickPickExposureMetric struct {
	ItemKey     string
	ShowCount   int64
	LastShownAt time.Time
}

type QuickPickMetricsRepository interface {
	SongRecentPlays(userID string, since time.Time) (map[string]int64, error)
	PlaylistMetrics(userID string, since time.Time) (map[string]PlaylistPlayMetric, error)
	RecordPlaylistPlay(userID, playlistID string, playedAt time.Time) error
	ExposureMetrics(userID string, itemKeys []string) (map[string]QuickPickExposureMetric, error)
	RecordExposures(userID string, itemKeys []string, shownAt time.Time) error
}
