package beets

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/model"
)

type Runner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type osRunner struct{}

func (osRunner) Run(ctx context.Context, executable string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, executable, args...) // #nosec G204 -- executable is fixed by the server image.
	return cmd.CombinedOutput()
}

type Client struct {
	executable  string
	runner      Runner
	musicFolder string
	configPath  string
	libraryPath string

	configOnce sync.Once
	configErr  error
}

func New() *Client {
	dataFolder := conf.Server.DataFolder.String()
	if dataFolder == "" {
		dataFolder = os.TempDir()
	}
	return NewWithRunner(
		"beet",
		osRunner{},
		conf.Server.MusicFolder,
		filepath.Join(dataFolder, "navidrome-beets.yaml"),
		filepath.Join(dataFolder, "navidrome-beets.db"),
	)
}

func NewWithRunner(executable string, runner Runner, musicFolder, configPath, libraryPath string) *Client {
	if strings.TrimSpace(executable) == "" {
		executable = "beet"
	}
	return &Client{
		executable:  executable,
		runner:      runner,
		musicFolder: musicFolder,
		configPath:  configPath,
		libraryPath: libraryPath,
	}
}

func (c *Client) Import(ctx context.Context, files []string, metadata model.ExternalTrack, singleton bool) error {
	if len(files) == 0 {
		return fmt.Errorf("no files to import")
	}
	if strings.TrimSpace(c.musicFolder) == "" {
		return fmt.Errorf("music folder is not configured")
	}
	if c.runner == nil {
		return fmt.Errorf("beets runner is not configured")
	}
	if err := c.ensureConfig(); err != nil {
		return err
	}

	args := []string{
		"-c", c.configPath,
		"import", "-q", "--quiet-fallback=asis",
	}
	if singleton {
		args = append(args, "-s")
	}
	appendField := func(name, value string) {
		if strings.TrimSpace(value) != "" {
			args = append(args, "--set", name+"="+value)
		}
	}
	appendField("artist", metadata.ArtistName)
	appendField("albumartist", metadata.ArtistName)
	appendField("album", metadata.AlbumTitle)
	appendField("mb_trackid", metadata.ID)
	if metadata.Year > 0 {
		appendField("year", strconv.Itoa(metadata.Year))
	}
	if singleton {
		appendField("title", metadata.Title)
	}
	args = append(args, files...)
	output, err := c.runner.Run(ctx, c.executable, args...)
	if err != nil {
		return fmt.Errorf("beets failed: %s", commandOutput(output, err))
	}
	return nil
}

func (c *Client) ensureConfig() error {
	c.configOnce.Do(func() {
		if strings.TrimSpace(c.configPath) == "" {
			c.configErr = fmt.Errorf("beets config path is not configured")
			return
		}
		if err := os.MkdirAll(filepath.Dir(c.configPath), 0700); err != nil {
			c.configErr = fmt.Errorf("create beets config directory: %w", err)
			return
		}
		config := strings.Join([]string{
			"directory: " + yamlString(c.musicFolder),
			"library: " + yamlString(c.libraryPath),
			"import:",
			"    move: yes",
			"    write: yes",
			"    autotag: yes",
			"    timid: no",
			"    quiet: yes",
			"    quiet_fallback: asis",
			"paths:",
			"    default: $albumartist/$album/$track - $title",
			"    singleton: $artist/Singles/$title",
			"",
		}, "\n")
		if err := os.WriteFile(c.configPath, []byte(config), 0600); err != nil {
			c.configErr = fmt.Errorf("write beets config: %w", err)
		}
	})
	return c.configErr
}

func yamlString(value string) string {
	return strconv.Quote(value)
}

func commandOutput(output []byte, err error) string {
	message := strings.TrimSpace(string(output))
	if message == "" {
		return err.Error()
	}
	if len(message) > 1000 {
		message = message[len(message)-1000:]
	}
	return message
}

var _ interface {
	Import(context.Context, []string, model.ExternalTrack, bool) error
} = (*Client)(nil)
