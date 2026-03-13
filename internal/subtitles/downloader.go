package subtitles

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yay101/mediarr/internal/db"
)

type Downloader struct {
	db     *db.Database
	client *http.Client
}

func NewDownloader(database *db.Database) *Downloader {
	return &Downloader{
		db: database,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

type SubtitleInfo struct {
	ID       string
	Language string
	Format   string
	URL      string
	Score    int
}

func (d *Downloader) DownloadSubtitles(mediaType db.MediaType, mediaID uint32, language string) error {
	var mediaPath string
	var title string

	switch mediaType {
	case db.MediaTypeMovie:
		table, err := d.db.Movies()
		if err != nil {
			return err
		}
		movie, err := table.Get(mediaID)
		if err != nil {
			return err
		}
		mediaPath = movie.Path
		title = movie.Title
	case db.MediaTypeTV:
		table, err := d.db.TVEpisodes()
		if err != nil {
			return err
		}
		ep, err := table.Get(mediaID)
		if err != nil {
			return err
		}
		mediaPath = ep.Path

		showTable, err := d.db.TVShows()
		if err == nil {
			show, _ := showTable.Get(ep.ShowID)
			if show != nil {
				title = fmt.Sprintf("%s S%02dE%02d", show.Title, ep.Season, ep.Episode)
			}
		}
	default:
		return fmt.Errorf("unsupported media type")
	}

	if mediaPath == "" {
		return fmt.Errorf("media path not set")
	}

	// Search for subtitles (Stub for actual API call)
	fmt.Printf("Searching for %s subtitles for: %s\n", language, title)

	// Example: In a real implementation we would call an API like OpenSubtitles here
	// results, err := d.searchOpenSubtitles(title, language)
	// if err != nil || len(results) == 0 { return fmt.Errorf("no subtitles found") }
	// best := results[0]
	// err = d.downloadFile(best.URL, subPath)

	subPath := strings.TrimSuffix(mediaPath, filepath.Ext(mediaPath)) + "." + language + ".srt"
	fmt.Printf("Subtitles would be downloaded to: %s\n", subPath)

	return nil
}

func (d *Downloader) ScanForSubtitles(mediaType db.MediaType, mediaID uint32) ([]string, error) {
	var mediaPath string

	switch mediaType {
	case db.MediaTypeMovie:
		table, err := d.db.Movies()
		if err != nil {
			return nil, err
		}
		movie, err := table.Get(mediaID)
		if err != nil {
			return nil, err
		}
		mediaPath = movie.Path
	case db.MediaTypeTV:
		table, err := d.db.TVEpisodes()
		if err != nil {
			return nil, err
		}
		ep, err := table.Get(mediaID)
		if err != nil {
			return nil, err
		}
		mediaPath = ep.Path
	default:
		return nil, fmt.Errorf("unsupported media type for subtitles")
	}

	if mediaPath == "" {
		return nil, nil
	}

	dir := filepath.Dir(mediaPath)
	base := strings.TrimSuffix(filepath.Base(mediaPath), filepath.Ext(mediaPath))

	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var subs []string
	for _, f := range files {
		name := f.Name()
		if strings.HasPrefix(name, base) {
			ext := strings.ToLower(filepath.Ext(name))
			if ext == ".srt" || ext == ".ass" || ext == ".vtt" || ext == ".ssa" {
				subs = append(subs, filepath.Join(dir, name))
			}
		}
	}

	return subs, nil
}
