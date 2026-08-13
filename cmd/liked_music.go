package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"unicode"

	"github.com/navidrome/navidrome/adapters/beets"
	"github.com/navidrome/navidrome/adapters/ytdlp"
	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/core/artwork"
	"github.com/navidrome/navidrome/core/playlists"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/scanner"
	"github.com/spf13/cobra"
	"golang.org/x/text/unicode/norm"
)

var likedMusicUser string

type likedMusicImportRow struct {
	Line   int
	Title  string
	Artist string
	URL    string
}

type likedMusicImportResult struct {
	Rows              int
	UniqueURLs        int
	DuplicateURLs     int
	Downloaded        int
	Synchronized      int
	Failures          []string
	ScanWarnings      []string
}

func init() {
	likedMusicImportCommand.Flags().StringVar(&likedMusicUser, "user", "admin", "target username or user ID")
	likedMusicRoot.AddCommand(likedMusicImportCommand)
	rootCmd.AddCommand(likedMusicRoot)
}

var likedMusicRoot = &cobra.Command{
	Use:   "liked-music",
	Short: "Manage the automatic liked music playlist",
}

var likedMusicImportCommand = &cobra.Command{
	Use:   "import-youtube <file>",
	Short: "Import a title, artist, and YouTube URL list into liked music",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runLikedMusicImport(cmd.Context(), args[0], likedMusicUser, cmd.OutOrStdout())
	},
}

func runLikedMusicImport(ctx context.Context, inputPath, username string, output io.Writer) error {
	rows, result, err := readLikedMusicRows(inputPath)
	if err != nil {
		return err
	}
	result.UniqueURLs = len(rows)

	ds, adminCtx := getAdminContext(ctx)
	defer db.Close(adminCtx)

	user, err := getUser(adminCtx, username, ds)
	if err != nil {
		return fmt.Errorf("find import user %q: %w", username, err)
	}
	ctx = request.WithUsername(request.WithUser(adminCtx, *user), user.UserName)

	before, err := ds.MediaFile(ctx).GetAll()
	if err != nil {
		return fmt.Errorf("read existing music library: %w", err)
	}

	assignments := make([]string, len(rows))
	downloadRows := make([]int, 0)
	ytClient := ytdlp.New()
	beetsClient := beets.New()
	cacheFolder := conf.Server.CacheFolder.String()
	if cacheFolder == "" {
		cacheFolder = os.TempDir()
	}
	tempDir, err := os.MkdirTemp(cacheFolder, "liked-music-import-")
	if err != nil {
		return fmt.Errorf("create import workspace: %w", err)
	}
	defer os.RemoveAll(tempDir) // The workspace contains only downloaded source files.

	for idx, row := range rows {
		matches := findLikedMusicMatches(before, row)
		switch len(matches) {
		case 1:
			assignments[idx] = matches[0].ID
		case 0:
			path, err := ytClient.DownloadURL(ctx, row.URL, tempDir)
			if err != nil {
				result.Failures = append(result.Failures, fmt.Sprintf("line %d %q: download failed: %v", row.Line, row.Title, err))
				continue
			}
			metadata := model.ExternalTrack{Title: row.Title, ArtistName: row.Artist}
			if err := beetsClient.Import(ctx, []string{path}, metadata, true); err != nil {
				result.Failures = append(result.Failures, fmt.Sprintf("line %d %q: library import failed: %v", row.Line, row.Title, err))
				continue
			}
			result.Downloaded++
			downloadRows = append(downloadRows, idx)
		default:
			result.Failures = append(result.Failures, fmt.Sprintf("line %d %q: ambiguous existing library match (%d songs)", row.Line, row.Title, len(matches)))
		}
	}

	if len(downloadRows) > 0 {
		if err := scanLikedMusicLibrary(ctx, ds); err != nil {
			result.ScanWarnings = append(result.ScanWarnings, err.Error())
			for _, idx := range downloadRows {
				result.Failures = append(result.Failures, fmt.Sprintf("line %d %q: library scan failed before synchronization", rows[idx].Line, rows[idx].Title))
			}
		} else {
			after, err := ds.MediaFile(ctx).GetAll()
			if err != nil {
				result.ScanWarnings = append(result.ScanWarnings, fmt.Sprintf("read library after scan: %v", err))
				for _, idx := range downloadRows {
					result.Failures = append(result.Failures, fmt.Sprintf("line %d %q: library could not be read after scan", rows[idx].Line, rows[idx].Title))
				}
			} else {
				newFiles := mediaFilesAddedSince(before, after)
				for _, idx := range downloadRows {
					matches := findLikedMusicMatches(newFiles, rows[idx])
					switch len(matches) {
					case 1:
						assignments[idx] = matches[0].ID
					case 0:
						result.Failures = append(result.Failures, fmt.Sprintf("line %d %q: downloaded file was not found after library scan", rows[idx].Line, rows[idx].Title))
					default:
						result.Failures = append(result.Failures, fmt.Sprintf("line %d %q: downloaded file matched %d library songs", rows[idx].Line, rows[idx].Title, len(matches)))
					}
				}
			}
		}
	}

	for idx, mediaFileID := range assignments {
		if mediaFileID == "" {
			continue
		}
		if err := starAndSyncLikedMusic(ctx, ds, mediaFileID); err != nil {
			result.Failures = append(result.Failures, fmt.Sprintf("line %d %q: star/synchronize failed: %v", rows[idx].Line, rows[idx].Title, err))
			continue
		}
		result.Synchronized++
	}

	fmt.Fprintf(output, "liked music import: rows=%d unique_urls=%d duplicate_urls_skipped=%d downloaded=%d synchronized=%d failed=%d\n",
		result.Rows, result.UniqueURLs, result.DuplicateURLs, result.Downloaded, result.Synchronized, len(result.Failures))
	for _, warning := range result.ScanWarnings {
		fmt.Fprintf(output, "scan warning: %s\n", warning)
	}
	for _, failure := range result.Failures {
		fmt.Fprintf(output, "failure: %s\n", failure)
	}
	return nil
}

func readLikedMusicRows(path string) ([]likedMusicImportRow, likedMusicImportResult, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, likedMusicImportResult{}, fmt.Errorf("open liked music file: %w", err)
	}
	defer file.Close()

	seenURLs := make(map[string]struct{})
	rows := make([]likedMusicImportRow, 0)
	result := likedMusicImportResult{}
	lineScanner := bufio.NewScanner(file)
	lineScanner.Buffer(make([]byte, 64*1024), 1024*1024)
	lineNumber := 0
	for lineScanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(lineScanner.Text())
		if line == "" {
			continue
		}
		result.Rows++
		parts := strings.SplitN(line, " — ", 3)
		if len(parts) != 3 {
			result.Failures = append(result.Failures, fmt.Sprintf("line %d: expected title — artist — URL", lineNumber))
			continue
		}
		row := likedMusicImportRow{
			Line:   lineNumber,
			Title:  strings.TrimSpace(parts[0]),
			Artist: strings.TrimSpace(parts[1]),
			URL:    strings.TrimSpace(parts[2]),
		}
		if row.Title == "" || row.Artist == "" {
			result.Failures = append(result.Failures, fmt.Sprintf("line %d: title and artist are required", lineNumber))
			continue
		}
		if err := validateYouTubeImportURL(row.URL); err != nil {
			result.Failures = append(result.Failures, fmt.Sprintf("line %d: %v", lineNumber, err))
			continue
		}
		if _, ok := seenURLs[row.URL]; ok {
			result.DuplicateURLs++
			continue
		}
		seenURLs[row.URL] = struct{}{}
		rows = append(rows, row)
	}
	if err := lineScanner.Err(); err != nil {
		return nil, likedMusicImportResult{}, fmt.Errorf("read liked music file: %w", err)
	}
	if len(rows) == 0 {
		return nil, result, fmt.Errorf("liked music file contains no valid unique URLs")
	}
	result.UniqueURLs = len(rows)
	return rows, result, nil
}

func validateYouTubeImportURL(source string) error {
	parsed, err := url.ParseRequestURI(source)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("invalid YouTube URL %q", source)
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "youtu.be" && host != "youtube.com" && !strings.HasSuffix(host, ".youtube.com") {
		return fmt.Errorf("URL is not a YouTube URL: %q", source)
	}
	return nil
}

func findLikedMusicMatches(files model.MediaFiles, row likedMusicImportRow) []model.MediaFile {
	rowTitle := normalizeLikedMusicTitle(row.Title)
	rowArtist := normalizeLikedMusicArtist(row.Artist)
	if rowTitle == "" || rowArtist == "" {
		return nil
	}
	seen := make(map[string]struct{})
	matches := make([]model.MediaFile, 0)
	for _, file := range files {
		if file.Missing || normalizeLikedMusicTitle(file.Title) != rowTitle || !mediaFileArtistMatches(file, rowArtist) {
			continue
		}
		if _, ok := seen[file.ID]; ok {
			continue
		}
		seen[file.ID] = struct{}{}
		matches = append(matches, file)
	}
	return matches
}

func mediaFileArtistMatches(file model.MediaFile, normalizedArtist string) bool {
	if normalizeLikedMusicArtist(file.Artist) == normalizedArtist || normalizeLikedMusicArtist(file.AlbumArtist) == normalizedArtist {
		return true
	}
	for _, artist := range file.Participants.AllNames() {
		if normalizeLikedMusicArtist(artist) == normalizedArtist {
			return true
		}
	}
	return false
}

func normalizeLikedMusicTitle(value string) string {
	value = strings.ToLower(norm.NFKC.String(strings.TrimSpace(value)))
	for _, suffix := range []string{
		" (official music video)",
		" official music video",
		" (official video)",
		" official video",
		" (lyric video)",
		" lyric video",
		" (lyrics)",
		" lyrics",
		" (official audio)",
		" official audio",
		" (audio)",
		" audio",
	} {
		value = strings.TrimSuffix(value, suffix)
	}
	return normalizeLikedMusicText(value)
}

func normalizeLikedMusicArtist(value string) string {
	value = strings.ToLower(norm.NFKC.String(strings.TrimSpace(value)))
	value = strings.TrimSuffix(value, " - topic")
	return normalizeLikedMusicText(value)
}

func normalizeLikedMusicText(value string) string {
	var normalized strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			normalized.WriteRune(r)
		} else {
			normalized.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(normalized.String()), " ")
}

func mediaFilesAddedSince(before, after model.MediaFiles) model.MediaFiles {
	known := make(map[string]struct{}, len(before))
	for _, file := range before {
		known[file.ID] = struct{}{}
	}
	added := make(model.MediaFiles, 0)
	for _, file := range after {
		if !file.Missing {
			if _, exists := known[file.ID]; !exists {
				added = append(added, file)
			}
		}
	}
	return added
}

func scanLikedMusicLibrary(ctx context.Context, ds model.DataStore) error {
	playlistService := playlists.NewPlaylists(ds, artwork.NewUploader(ds))
	progress, err := scanner.CallScan(ctx, ds, playlistService, false, nil)
	if err != nil {
		return err
	}
	var scanErrors []error
	for status := range progress {
		if status.Error != "" {
			scanErrors = append(scanErrors, errors.New(status.Error))
		}
	}
	return errors.Join(scanErrors...)
}

func starAndSyncLikedMusic(ctx context.Context, ds model.DataStore, mediaFileID string) error {
	return ds.WithTxImmediate(func(tx model.DataStore) error {
		if err := tx.MediaFile(ctx).SetStar(true, mediaFileID); err != nil {
			return err
		}
		_, err := playlists.SyncLikedMusic(ctx, tx, mediaFileID, true)
		return err
	})
}
