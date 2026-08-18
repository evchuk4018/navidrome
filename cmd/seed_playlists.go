package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/navidrome/navidrome/adapters/musicbrainz"
	"github.com/navidrome/navidrome/core/artwork"
	"github.com/navidrome/navidrome/core/music"
	"github.com/navidrome/navidrome/core/playlists"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/persistence"
	"github.com/spf13/cobra"
)

var (
	seedPlaylistsUser          string
	seedPlaylistsSkipDownloads bool
	seedPlaylistsSkipPlaylists bool
)

type seedSongRef struct {
	Artist string `json:"artist"`
	Title  string `json:"title"`
}

type seedPlaylist struct {
	Name  string        `json:"name"`
	Songs []seedSongRef `json:"songs"`
}

type seedPlaylistsFile struct {
	Playlists []seedPlaylist `json:"playlists"`
}

type seedPlaylistResult struct {
	Name       string
	PlaylistID string
	Matched    int
	Total      int
}

type seedSummary struct {
	Defined       int
	UniqueSongs   int
	InLibrary     int
	AlreadyQueued int
	Queued        int
	ResolveFailed []string
	QueueFailed   []string
	Playlists     []seedPlaylistResult
}

func init() {
	seedPlaylistsCmd.Flags().StringVarP(&seedPlaylistsUser, "user", "u", "admin", "target username or ID")
	seedPlaylistsCmd.Flags().BoolVar(&seedPlaylistsSkipDownloads, "skip-downloads", false, "do not queue new downloads, only create/refresh playlists")
	seedPlaylistsCmd.Flags().BoolVar(&seedPlaylistsSkipPlaylists, "skip-playlists", false, "only resolve and queue downloads, do not create/refresh playlists")
	rootCmd.AddCommand(seedPlaylistsCmd)
}

var seedPlaylistsCmd = &cobra.Command{
	Use:   "seed-playlists <file.json>",
	Short: "Seed playlists and queue downloads from a playlist definition file",
	Long: `Seed playlists from a JSON definition file and queue missing songs through
the external music download pipeline (MusicBrainz + yt-dlp + beets).

Each song already present in the library is matched and added to its playlist.
Songs that are not in the library are resolved against the MusicBrainz catalog
and queued as download jobs; the running server imports them in the background.
Re-running the command is idempotent: it refreshes the playlists with every song
that has been matched so far and skips songs that are already downloaded or
already queued.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSeedPlaylists(cmd.Context(), args[0], cmd.OutOrStdout())
	},
}

func runSeedPlaylists(ctx context.Context, path string, output io.Writer) error {
	def, err := loadSeedPlaylists(path)
	if err != nil {
		return err
	}
	if len(def.Playlists) == 0 {
		return errors.New("playlist definition contains no playlists")
	}

	ds, adminCtx := getAdminContext(ctx)
	defer db.Close(adminCtx)

	user, err := getUser(adminCtx, seedPlaylistsUser, ds)
	if err != nil {
		return fmt.Errorf("find seed user %q: %w", seedPlaylistsUser, err)
	}
	targetCtx := request.WithUsername(request.WithUser(adminCtx, *user), user.UserName)

	library, err := ds.MediaFile(targetCtx).GetAll()
	if err != nil {
		return fmt.Errorf("read existing music library: %w", err)
	}

	unique := dedupeSeedSongs(def)
	summary := &seedSummary{
		Defined:     countSeedSongs(def),
		UniqueSongs: len(unique),
	}

	matchedKeys := make(map[string]struct{}, len(unique))
	for _, ref := range unique {
		key := seedSongKey(ref)
		if len(findLikedMusicMatches(library, seedLibraryRow(ref))) > 0 {
			matchedKeys[key] = struct{}{}
		}
	}
	summary.InLibrary = len(matchedKeys)

	if !seedPlaylistsSkipDownloads {
		if err := queueSeedDownloads(targetCtx, user, unique, matchedKeys, summary); err != nil {
			return err
		}
	}

	if !seedPlaylistsSkipPlaylists {
		if err := refreshSeedPlaylists(targetCtx, ds, def, library, summary); err != nil {
			return err
		}
	}

	printSeedSummary(output, summary)
	return nil
}

func loadSeedPlaylists(path string) (seedPlaylistsFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return seedPlaylistsFile{}, fmt.Errorf("read playlist definition: %w", err)
	}
	var def seedPlaylistsFile
	if err := json.Unmarshal(data, &def); err != nil {
		return seedPlaylistsFile{}, fmt.Errorf("parse playlist definition: %w", err)
	}
	for i, playlist := range def.Playlists {
		name := strings.TrimSpace(playlist.Name)
		if name == "" {
			return seedPlaylistsFile{}, fmt.Errorf("playlist %d has no name", i+1)
		}
		def.Playlists[i].Name = name
	}
	return def, nil
}

func countSeedSongs(def seedPlaylistsFile) int {
	total := 0
	for _, playlist := range def.Playlists {
		total += len(playlist.Songs)
	}
	return total
}

// dedupeSeedSongs returns the unique songs across all playlists in first-seen
// order so a song referenced by several playlists is only downloaded once.
func dedupeSeedSongs(def seedPlaylistsFile) []seedSongRef {
	seen := make(map[string]struct{})
	out := make([]seedSongRef, 0)
	for _, playlist := range def.Playlists {
		for _, ref := range playlist.Songs {
			key := seedSongKey(ref)
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, ref)
		}
	}
	return out
}

func seedSongKey(ref seedSongRef) string {
	title := normalizeLikedMusicTitle(ref.Title)
	artist := normalizeLikedMusicArtist(ref.Artist)
	if title == "" || artist == "" {
		return ""
	}
	return artist + "\x00" + title
}

// queueSeedDownloads resolves each missing song against the MusicBrainz catalog
// and queues a download job so the running server imports it through the
// existing download pipeline.
func queueSeedDownloads(ctx context.Context, user *model.User, unique []seedSongRef, matchedKeys map[string]struct{}, summary *seedSummary) error {
	jobsRepo := persistence.NewMusicDownloadJobRepository(db.Db())
	existingJobs, err := jobsRepo.GetAllForUser(user.ID, 0)
	if err != nil {
		return fmt.Errorf("read existing download jobs: %w", err)
	}
	existingByKey := make(map[string]struct{}, len(existingJobs))
	existingBySource := make(map[string]struct{}, len(existingJobs))
	for _, job := range existingJobs {
		// Failed jobs are not considered handled: a re-run can retry them.
		if job.Status != model.MusicDownloadQueued && job.Status != model.MusicDownloadRunning && job.Status != model.MusicDownloadSuccess {
			continue
		}
		if job.SourceID != "" {
			existingBySource[job.SourceID] = struct{}{}
		}
		if key := seedSongKey(seedSongRef{Artist: job.Artist, Title: job.Title}); key != "" {
			existingByKey[key] = struct{}{}
		}
	}

	var catalog music.Catalog = musicbrainz.New()
	service := music.New(catalog, nil, nil, jobsRepo, nil)

	for _, ref := range unique {
		key := seedSongKey(ref)
		if _, ok := matchedKeys[key]; ok {
			continue
		}
		if _, ok := existingByKey[key]; ok {
			summary.AlreadyQueued++
			continue
		}
		track, err := resolveSeedTrack(catalog, ctx, ref)
		if err != nil {
			summary.ResolveFailed = append(summary.ResolveFailed, fmt.Sprintf("%s - %s: %v", ref.Artist, ref.Title, err))
			continue
		}
		if _, ok := existingBySource[track.ID]; ok {
			existingByKey[key] = struct{}{}
			summary.AlreadyQueued++
			continue
		}
		_, err = service.CreateDownload(ctx, user.ID, model.ExternalDownloadRequest{
			Kind:   model.MusicDownloadSong,
			ID:     track.ID,
			Origin: model.MusicDownloadOriginManual,
			Title:  ref.Title,
			Artist: ref.Artist,
		})
		if err != nil {
			summary.QueueFailed = append(summary.QueueFailed, fmt.Sprintf("%s - %s: %v", ref.Artist, ref.Title, err))
			continue
		}
		existingByKey[key] = struct{}{}
		existingBySource[track.ID] = struct{}{}
		summary.Queued++
	}
	return nil
}

func resolveSeedTrack(catalog music.Catalog, ctx context.Context, ref seedSongRef) (model.ExternalTrack, error) {
	// A structured Lucene query filters out covers/remixes whose titles merely
	// mention the artist, which free-text search ranks above the original.
	if track, err := resolveSeedTrackQuery(catalog, ctx, seedSearchQuery(ref), ref); err == nil {
		return track, nil
	}
	// Fallback: search by artist alone and tolerate version/remix suffixes in
	// the title, for references whose exact title does not exist in the catalog.
	return resolveSeedTrackQuery(catalog, ctx, fmt.Sprintf(`artist:"%s"`, seedEscapeLucene(strings.TrimSpace(ref.Artist))), ref)
}

func resolveSeedTrackQuery(catalog music.Catalog, ctx context.Context, query string, ref seedSongRef) (model.ExternalTrack, error) {
	songs, err := catalog.SearchSongs(ctx, query)
	if err != nil {
		return model.ExternalTrack{}, err
	}
	for _, song := range songs {
		if seedTrackMatches(ref, song) {
			return song, nil
		}
	}
	return model.ExternalTrack{}, model.ErrNotFound
}

// seedSearchQuery builds a MusicBrainz Lucene query that matches the credited
// artist and the (credit-stripped) recording title.
func seedSearchQuery(ref seedSongRef) string {
	artist := strings.TrimSpace(ref.Artist)
	title := strings.TrimSpace(stripSeedCredits(ref.Title))
	return fmt.Sprintf(`artist:"%s" AND recording:"%s"`, seedEscapeLucene(artist), seedEscapeLucene(title))
}

func seedEscapeLucene(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(value, `"`, `\"`)
}

func seedTrackMatches(ref seedSongRef, track model.ExternalTrack) bool {
	refArtist := normalizeLikedMusicArtist(ref.Artist)
	if refArtist == "" {
		return false
	}
	if !likedMusicArtistValueMatches(track.ArtistName, refArtist) {
		return false
	}
	return seedTitleMatches(ref.Title, track.Title)
}

func seedTitleMatches(refTitle, trackTitle string) bool {
	refNorm := normalizeLikedMusicTitle(stripSeedCredits(refTitle))
	if refNorm == "" {
		return false
	}
	if seedTitlesMatch(trackTitle, refNorm) {
		return true
	}
	// A reference may describe a version (Remix, Acoustic, ...) that the tagged
	// or catalogued track omits; accept the base title too.
	refVersionless := normalizeLikedMusicTitle(stripSeedTitleSuffix(refTitle))
	return refVersionless != "" && refVersionless != refNorm && seedTitlesMatch(trackTitle, refVersionless)
}

// seedTitlesMatch matches a raw track title against a normalized reference
// title, tolerating "(feat. ...)"/"(Remix)"/"(Remastered ...)" clauses that the
// tagged track omits.
func seedTitlesMatch(rawValue, refTitle string) bool {
	if likedMusicTitleValuesMatch(rawValue, refTitle) {
		return true
	}
	stripped := normalizeLikedMusicTitle(stripSeedTitleSuffix(rawValue))
	return stripped != "" && stripped == refTitle
}

// stripSeedTitleSuffix removes trailing parenthetical clauses that describe
// featured artists or version suffixes (feat., with, remix, remaster, ...) so a
// reference like "WAP (feat. Megan Thee Stallion)" also matches a track tagged
// simply "WAP".
func stripSeedTitleSuffix(value string) string {
	return stripSeedClauses(value, func(clause string) bool {
		return strings.Contains(clause, "feat") ||
			strings.Contains(clause, "with") ||
			strings.Contains(clause, "&") ||
			strings.Contains(clause, "remix") ||
			strings.Contains(clause, "remaster") ||
			strings.Contains(clause, "acoustic")
	})
}

// stripSeedCredits removes only the parenthetical clauses describing featured
// artists, keeping version markers such as "(Remix)" that are part of the title.
func stripSeedCredits(value string) string {
	return stripSeedClauses(value, func(clause string) bool {
		return strings.Contains(clause, "feat") || strings.Contains(clause, "with") || strings.Contains(clause, "&")
	})
}

func stripSeedClauses(value string, match func(string) bool) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	lower := strings.ToLower(value)
	for {
		start := strings.Index(lower, " (")
		if start < 0 {
			return strings.TrimSpace(value)
		}
		relativeEnd := strings.Index(lower[start:], ")")
		if relativeEnd < 0 {
			return strings.TrimSpace(value)
		}
		clause := lower[start+2 : start+relativeEnd]
		if !match(clause) {
			return strings.TrimSpace(value)
		}
		value = strings.TrimSpace(value[:start] + value[start+relativeEnd+1:])
		lower = strings.ToLower(value)
	}
}

// seedLibraryRow maps a playlist song reference to the liked-music import row
// shape, stripping version/feature clauses so library matching is consistent
// with download resolution.
func seedLibraryRow(ref seedSongRef) likedMusicImportRow {
	return likedMusicImportRow{Title: stripSeedTitleSuffix(ref.Title), Artist: ref.Artist}
}

func refreshSeedPlaylists(ctx context.Context, ds model.DataStore, def seedPlaylistsFile, library model.MediaFiles, summary *seedSummary) error {
	plsSvc := playlists.NewPlaylists(ds, artwork.NewUploader(ds))

	existingPls, err := plsSvc.GetAll(ctx, model.QueryOptions{Sort: "name"})
	if err != nil {
		return fmt.Errorf("list existing playlists: %w", err)
	}
	existingByName := make(map[string]string, len(existingPls))
	for _, pls := range existingPls {
		existingByName[strings.ToLower(strings.TrimSpace(pls.Name))] = pls.ID
	}

	for _, playlist := range def.Playlists {
		result, err := refreshSeedPlaylist(ctx, plsSvc, playlist, library, existingByName)
		if err != nil {
			return fmt.Errorf("refresh playlist %q: %w", playlist.Name, err)
		}
		summary.Playlists = append(summary.Playlists, result)
	}
	return nil
}

func refreshSeedPlaylist(ctx context.Context, plsSvc playlists.Playlists, playlist seedPlaylist, library model.MediaFiles, existingByName map[string]string) (seedPlaylistResult, error) {
	ids := make([]string, 0, len(playlist.Songs))
	seen := make(map[string]struct{}, len(playlist.Songs))
	for _, ref := range playlist.Songs {
		for _, mf := range findLikedMusicMatches(library, seedLibraryRow(ref)) {
			if _, ok := seen[mf.ID]; ok {
				continue
			}
			seen[mf.ID] = struct{}{}
			ids = append(ids, mf.ID)
		}
	}

	result := seedPlaylistResult{Name: playlist.Name, Total: len(playlist.Songs), Matched: len(ids)}

	var (
		playlistID string
		err        error
	)
	if existingID, ok := existingByName[strings.ToLower(strings.TrimSpace(playlist.Name))]; ok {
		playlistID, err = plsSvc.Create(ctx, existingID, "", ids)
	} else {
		playlistID, err = plsSvc.Create(ctx, "", playlist.Name, ids)
	}
	if err != nil {
		return result, err
	}
	result.PlaylistID = playlistID
	return result, nil
}

func printSeedSummary(output io.Writer, summary *seedSummary) {
	fmt.Fprintf(output, "seed-playlists: defined=%d unique=%d in_library=%d queued=%d already_queued=%d\n",
		summary.Defined, summary.UniqueSongs, summary.InLibrary, summary.Queued, summary.AlreadyQueued)
	for _, failure := range summary.ResolveFailed {
		fmt.Fprintf(output, "resolve failure: %s\n", failure)
	}
	for _, failure := range summary.QueueFailed {
		fmt.Fprintf(output, "queue failure: %s\n", failure)
	}
	for _, playlist := range summary.Playlists {
		fmt.Fprintf(output, "playlist %q (%s): matched %d/%d songs\n",
			playlist.Name, playlist.PlaylistID, playlist.Matched, playlist.Total)
	}
}
