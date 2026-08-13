package artwork

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// YouTubeThumbnailProvider keeps source parsing and yt-dlp behavior outside the artwork domain.
type YouTubeThumbnailProvider interface {
	SourceURL(metadata string) (string, bool)
	Thumbnail(context.Context, string) (*url.URL, error)
	SearchThumbnail(context.Context, string) (*url.URL, error)
}

func (r *resolver) resolveYouTubeAlbum(ctx context.Context, album model.Album, allowSearch bool) (resolution, bool, error) {
	if r.ext == nil || r.ext.youtube == nil {
		return resolution{}, false, nil
	}
	tracks, err := r.ds.MediaFile(ctx).GetAll(model.QueryOptions{
		Filters: squirrel.Eq{"album_id": album.ID, "missing": false},
		Sort:    "created_at",
		Order:   "asc",
	})
	if err != nil {
		return resolution{}, false, fmt.Errorf("load album tracks for YouTube artwork: %w", err)
	}

	var transient bool
	hasSourceURL := false
	seen := make(map[string]struct{})
	for _, track := range tracks {
		source, ok := r.ext.youtube.SourceURL(track.Comment)
		if !ok {
			continue
		}
		hasSourceURL = true
		if _, duplicate := seen[source]; duplicate {
			continue
		}
		seen[source] = struct{}{}
		reader, isTransient := r.fetchYouTubeThumbnail(ctx, func() (*url.URL, error) {
			return r.ext.youtube.Thumbnail(ctx, source)
		})
		if reader != nil {
			return resolution{reader: reader, source: "external:youtube", extError: transient}, true, nil
		}
		transient = transient || isTransient
	}

	// A metadata search is intentionally limited to the rollout/config-change backfill. Normal
	// scans and hourly absent-art rechecks must not repeatedly search YouTube for old files.
	if allowSearch && !hasSourceURL {
		for _, track := range tracks {
			query := strings.TrimSpace(track.Artist + " - " + track.Title)
			if track.Title == "" || query == "-" {
				continue
			}
			reader, isTransient := r.fetchYouTubeThumbnail(ctx, func() (*url.URL, error) {
				return r.ext.youtube.SearchThumbnail(ctx, query)
			})
			if reader != nil {
				return resolution{reader: reader, source: "external:youtube-search", extError: transient}, true, nil
			}
			transient = transient || isTransient
			break
		}
	}
	return resolution{extError: transient}, false, nil
}

func (r *resolver) fetchYouTubeThumbnail(ctx context.Context, lookup func() (*url.URL, error)) (io.ReadCloser, bool) {
	reader, _, err := r.ext.gate("youtube", func() (io.ReadCloser, string, error) {
		imageURL, err := lookup()
		if err != nil {
			return nil, "", err
		}
		if imageURL == nil {
			return nil, "", model.ErrNotFound
		}
		return fromURL(ctx, imageURL)
	})
	if reader != nil {
		return reader, false
	}
	if isTransientExternal(err) {
		log.Debug(ctx, "Artwork: YouTube thumbnail lookup failed", err)
		return nil, true
	}
	return nil, false
}
