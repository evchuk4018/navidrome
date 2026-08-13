package model

// RecordingPopularity contains aggregate popularity data for a recording.
// Providers may return zero values when a recording has no popularity data.
type RecordingPopularity struct {
	TotalListenCount int64
	TotalUserCount   int64
}
