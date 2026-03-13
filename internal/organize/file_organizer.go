package organize

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/yay101/mediarr/internal/db"
)

var (
	movieRegexp   = regexp.MustCompile(`(?i)(.*)\.?\(?(\d{4})\)?`)
	episodeRegexp = regexp.MustCompile(`(?i)(.*)\.?s(\d{1,2})e(\d{1,2})`)
)

type MediaInfo struct {
	Title   string
	Year    uint16
	Season  uint32
	Episode uint32
	Quality string
}

type Organizer struct {
	db *db.Database
}

func NewOrganizer(database *db.Database) *Organizer {
	return &Organizer{
		db: database,
	}
}

func (o *Organizer) OrganizeMovie(movie *db.Movie, sourcePath, destDir string, useHardlink bool) error {
	ext := filepath.Ext(sourcePath)
	folderName := fmt.Sprintf("%s (%d)", movie.Title, movie.Year)
	fileName := fmt.Sprintf("%s (%d) [%s]%s", movie.Title, movie.Year, movie.Quality, ext)

	targetDir := filepath.Join(destDir, folderName)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	targetPath := filepath.Join(targetDir, fileName)

	if useHardlink {
		if err := os.Link(sourcePath, targetPath); err != nil {
			// Fallback to copy if link fails (e.g. cross-filesystem)
			return o.moveFile(sourcePath, targetPath)
		}
	} else {
		if err := os.Rename(sourcePath, targetPath); err != nil {
			return o.moveFile(sourcePath, targetPath)
		}
	}

	movie.Path = targetPath
	movie.Status = db.MediaStatusAvailable

	table, err := o.db.Movies()
	if err == nil {
		_ = table.Update(movie.ID, movie)
	}

	return nil
}

func (o *Organizer) OrganizeEpisode(show *db.TVShow, episode *db.TVEpisode, sourcePath, destDir string, useHardlink bool) error {
	ext := filepath.Ext(sourcePath)
	showDir := filepath.Join(destDir, show.Title)
	seasonDir := filepath.Join(showDir, fmt.Sprintf("Season %d", episode.Season))

	if err := os.MkdirAll(seasonDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	fileName := fmt.Sprintf("%s - S%02dE%02d - %s%s", show.Title, episode.Season, episode.Episode, episode.Title, ext)
	targetPath := filepath.Join(seasonDir, fileName)

	if useHardlink {
		if err := os.Link(sourcePath, targetPath); err != nil {
			return o.moveFile(sourcePath, targetPath)
		}
	} else {
		if err := os.Rename(sourcePath, targetPath); err != nil {
			return o.moveFile(sourcePath, targetPath)
		}
	}

	episode.Path = targetPath
	episode.Status = db.MediaStatusAvailable

	table, err := o.db.TVEpisodes()
	if err == nil {
		_ = table.Update(episode.ID, episode)
	}

	return nil
}

func (o *Organizer) moveFile(src, dst string) error {
	// Simple rename
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}

	// If rename fails (different filesystems), do a copy and delete
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	err = os.WriteFile(dst, data, 0644)
	if err != nil {
		return err
	}
	return os.Remove(src)
}

func (o *Organizer) DetectMedia(fileName string) MediaInfo {
	info := MediaInfo{}

	// Remove extension for parsing
	base := strings.TrimSuffix(fileName, filepath.Ext(fileName))

	// Try TV pattern first (more specific)
	if matches := episodeRegexp.FindStringSubmatch(base); len(matches) >= 4 {
		info.Title = cleanTitle(matches[1])
		s, _ := strconv.Atoi(matches[2])
		e, _ := strconv.Atoi(matches[3])
		info.Season = uint32(s)
		info.Episode = uint32(e)
		return info
	}

	// Try Movie pattern
	if matches := movieRegexp.FindStringSubmatch(base); len(matches) >= 3 {
		info.Title = cleanTitle(matches[1])
		y, _ := strconv.Atoi(matches[2])
		info.Year = uint16(y)
		return info
	}

	info.Title = cleanTitle(base)
	return info
}

func cleanTitle(title string) string {
	title = strings.ReplaceAll(title, ".", " ")
	title = strings.ReplaceAll(title, "_", " ")
	return strings.TrimSpace(title)
}
