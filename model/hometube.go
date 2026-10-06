package model

// HomeTubeVideo is catalog metadata only. Media and download state belong to HomeTube.
type HomeTubeVideo struct {
	ID              string  `structs:"id" json:"id"`
	Title           string  `structs:"title" json:"title"`
	ChannelID       string  `structs:"channel_id" json:"channelId"`
	ChannelName     string  `structs:"channel_name" json:"channelName"`
	ThumbnailURL    string  `structs:"thumbnail_url" json:"thumbnailUrl"`
	DurationSeconds float32 `structs:"duration_seconds" json:"durationSeconds"`
	Starred         bool    `structs:"-" json:"starred"`
}

type PlaylistEntry struct {
	Source string         `json:"source"`
	ID     string         `json:"id"`
	Video  *HomeTubeVideo `json:"video,omitempty"`
}

type HomeTubeVideoRepository interface {
	ResourceRepository
	Get(id string) (*HomeTubeVideo, error)
	Put(video *HomeTubeVideo) error
	SetFavorite(id string, favorite bool) error
}
