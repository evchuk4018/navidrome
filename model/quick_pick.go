package model

import "time"

const (
	QuickPickSong     = "song"
	QuickPickPlaylist = "playlist"
)

type QuickPickItem struct {
	Kind     string     `json:"kind"`
	Song     *MediaFile `json:"song,omitempty"`
	Playlist *Playlist  `json:"playlist,omitempty"`
	Score    float64    `json:"-"`
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

type QuickPickMetricsRepository interface {
	SongRecentPlays(userID string, since time.Time) (map[string]int64, error)
	PlaylistMetrics(userID string, since time.Time) (map[string]PlaylistPlayMetric, error)
	RecordPlaylistPlay(userID, playlistID string, playedAt time.Time) error
}
