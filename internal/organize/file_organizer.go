package organize

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/yay101/mediarr/internal/db"
)

// Regular expressions for parsing release filenames.
// These patterns handle common naming conventions for movies and TV episodes.

// Movie pattern matches: "Title (2024)" or "Title.2024"
var movieRegexp = regexp.MustCompile(`(?i)(.*)\.?\(?(\d{4})\)?`)

// Episode pattern matches: "Title S01E01" or "Title.s01e01"
var episodeRegexp = regexp.MustCompile(`(?i)(.*)\.?s(\d{1,2})e(\d{1,2})`)

// Multi-episode pattern matches: "Title S01E01E02" for episodes with multiple parts
var multiEpRegexp = regexp.MustCompile(`(?i)(.*)\.?s(\d{1,2})e(\d{1,2})(?:e(\d{1,2}))*`)

// Season-only pattern matches: "Title S01" (for folder naming)
var seasonRegexp = regexp.MustCompile(`(?i)(.*)\.?s(\d{1,2})`)

// Quality tags extracted from filenames
var qualityRegexp = regexp.MustCompile(`(?i)\[?(720p|1080p|2160p|4k|uhd|hdr|bluray|webrip|webdl|web-dl|hdtv|dvdrip|brrip|bdrip)\]?`)

// Codec tags extracted from filenames
var codecRegexp = regexp.MustCompile(`(?i)\[?(x264|x265|hevc|h264|h265|av1|xvid|divx)\]?`)

// Release group extracted from brackets at end of filename
var groupRegexp = regexp.MustCompile(`(?i)\[([^\]]+)\]\s*$`)

// File extensions that indicate media files
var extensionRegex = regexp.MustCompile(`\.(mkv|mp4|avi|m4v|mov|wmv|flv|webm)$`)

// MediaInfo contains parsed metadata from a release filename.
// Used for identifying and categorizing media files.
type MediaInfo struct {
	Title      string // Cleaned title without year/quality/codec
	Year       uint16 // Release year (movies)
	Season     uint32 // Season number (TV)
	Episode    uint32 // Episode number (TV)
	Quality    string // Quality tag (720p, 1080p, etc.)
	Codec      string // Video codec (x264, x265, etc.)
	Group      string // Release group name
	Resolution string // Standardized resolution
}

// OrganizeOptions controls the behavior of file organization.
type OrganizeOptions struct {
	UseHardlink       bool   // Use hardlinks instead of moving (preserves seeding)
	DeleteLeftovers   bool   // Remove leftover files after organizing
	Overwrite         bool   // Overwrite existing files at destination
	StorageLocationID uint32 // Target storage location ID (0 = auto-select)
}

// OrganizeResult contains the outcome of an organize operation.
type OrganizeResult struct {
	Success     bool   // Whether the operation succeeded
	FilesMoved  int    // Number of files successfully moved
	FilesFailed int    // Number of files that failed to move
	DestPath    string // Final destination path
	Error       error  // Error if unsuccessful
}

// Organizer handles moving and organizing media files into a structured library.
// It supports both movies and TV episodes with configurable naming conventions.
type Organizer struct {
	db *db.Database
}

// NewOrganizer creates an organizer instance with the specified database.
func NewOrganizer(database *db.Database) *Organizer {
	return &Organizer{
		db: database,
	}
}

// OrganizeMovie moves a movie file to the library with proper naming.
// Destination format: "Movie Title (Year)/Movie Title (Year) [Quality].ext"
// Returns OrganizeResult with success status and destination path.
func (o *Organizer) OrganizeMovie(movie *db.Movie, sourcePath, destDir string, opts OrganizeOptions) *OrganizeResult {
	// Validate source is a file, not a directory
	info, err := os.Stat(sourcePath)
	if err != nil {
		return &OrganizeResult{
			Success: false,
			Error:   fmt.Errorf("source path stat failed: %w", err),
		}
	}
	if info.IsDir() {
		return &OrganizeResult{
			Success: false,
			Error:   fmt.Errorf("source path is a directory, not a file: %s", sourcePath),
		}
	}

	// Use stored quality or detect from filename
	quality := movie.Quality
	if quality == "" {
		quality = o.DetectQuality(sourcePath)
	}

	ext := filepath.Ext(sourcePath)
	folderName := fmt.Sprintf("%s (%d)", movie.Title, movie.Year)
	fileName := fmt.Sprintf("%s (%d) [%s]%s", movie.Title, movie.Year, quality, ext)

	// Create destination directory structure
	targetDir := filepath.Join(destDir, folderName)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return &OrganizeResult{
			Success: false,
			Error:   fmt.Errorf("create directory: %w", err),
		}
	}

	targetPath := filepath.Join(targetDir, fileName)

	// Skip if target exists and overwrite is disabled
	if !opts.Overwrite {
		if _, err := os.Stat(targetPath); err == nil {
			return &OrganizeResult{
				Success:    true,
				FilesMoved: 0,
				DestPath:   targetPath,
			}
		}
	}

	// Move or link the file
	if err := o.moveOrLink(sourcePath, targetPath, opts.UseHardlink); err != nil {
		return &OrganizeResult{
			Success: false,
			Error:   err,
		}
	}

	// Clean up download folder if requested
	if opts.DeleteLeftovers {
		o.CleanupLeftovers(filepath.Dir(sourcePath))
	}

	// Update movie record with new path and status
	movie.Path = targetPath
	movie.Quality = quality
	movie.Status = db.MediaStatusAvailable

	table, err := o.db.Movies()
	if err == nil {
		_ = table.Update(movie.ID, movie)
	}

	return &OrganizeResult{
		Success:    true,
		FilesMoved: 1,
		DestPath:   targetPath,
	}
}

// OrganizeEpisode moves a TV episode file to the library with proper naming.
// Destination format: "Show Title/Season X/Show Title - SXXEXX - Episode Title [Quality].ext"
func (o *Organizer) OrganizeEpisode(show *db.TVShow, episode *db.TVEpisode, sourcePath, destDir string, opts OrganizeOptions) *OrganizeResult {
	info, err := os.Stat(sourcePath)
	if err != nil {
		return &OrganizeResult{
			Success: false,
			Error:   fmt.Errorf("source path stat failed: %w", err),
		}
	}
	if info.IsDir() {
		return &OrganizeResult{
			Success: false,
			Error:   fmt.Errorf("source path is a directory, not a file: %s", sourcePath),
		}
	}

	quality := o.DetectQuality(sourcePath)
	ext := filepath.Ext(sourcePath)
	showDir := filepath.Join(destDir, show.Title)
	seasonDir := filepath.Join(showDir, fmt.Sprintf("Season %d", episode.Season))

	if err := os.MkdirAll(seasonDir, 0755); err != nil {
		return &OrganizeResult{
			Success: false,
			Error:   fmt.Errorf("create directory: %w", err),
		}
	}

	// Standard Plex-style naming: "Show - SXXEXX - Title [Quality].ext"
	fileName := fmt.Sprintf("%s - S%02dE%02d - %s [%s]%s", show.Title, episode.Season, episode.Episode, episode.Title, quality, ext)
	targetPath := filepath.Join(seasonDir, fileName)

	if !opts.Overwrite {
		if _, err := os.Stat(targetPath); err == nil {
			return &OrganizeResult{
				Success:    true,
				FilesMoved: 0,
				DestPath:   targetPath,
			}
		}
	}

	if err := o.moveOrLink(sourcePath, targetPath, opts.UseHardlink); err != nil {
		return &OrganizeResult{
			Success: false,
			Error:   err,
		}
	}

	if opts.DeleteLeftovers {
		o.CleanupLeftovers(filepath.Dir(sourcePath))
	}

	episode.Path = targetPath
	episode.Status = db.MediaStatusAvailable

	table, err := o.db.TVEpisodes()
	if err == nil {
		_ = table.Update(episode.ID, episode)
	}

	return &OrganizeResult{
		Success:    true,
		FilesMoved: 1,
		DestPath:   targetPath,
	}
}

// OrganizeSeason processes all episodes from a season directory.
// Matches files to episodes using filename parsing, then organizes each.
func (o *Organizer) OrganizeSeason(show *db.TVShow, season uint32, sourceDir, destDir string, opts OrganizeOptions) []*OrganizeResult {
	results := make([]*OrganizeResult, 0)

	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return []*OrganizeResult{{Success: false, Error: err}}
	}

	seasonDir := filepath.Join(destDir, show.Title, fmt.Sprintf("Season %d", season))
	if err := os.MkdirAll(seasonDir, 0755); err != nil {
		return []*OrganizeResult{{Success: false, Error: err}}
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		sourcePath := filepath.Join(sourceDir, entry.Name())
		info := o.DetectMedia(entry.Name())

		// Filter to only episodes from the requested season
		if info.Season != season {
			continue
		}

		episodeNum := info.Episode
		if episodeNum == 0 {
			// Fallback episode number detection
			episodeNum = o.guessEpisodeNumber(entry.Name())
		}

		var episode db.TVEpisode
		episode.ShowID = show.ID
		episode.Season = season
		episode.Episode = episodeNum
		episode.Title = info.Title

		result := o.OrganizeEpisode(show, &episode, sourcePath, destDir, opts)
		results = append(results, result)
	}

	if opts.DeleteLeftovers {
		o.CleanupLeftovers(sourceDir)
	}

	return results
}

// moveOrLink moves or links a file to its destination.
// Attempts hardlink first if UseHardlink is true (preserves seeding).
// Falls back to copy+delete if hardlink fails (cross-filesystem).
func (o *Organizer) moveOrLink(src, dst string, useHardlink bool) error {
	// Remove existing destination if present
	if _, err := os.Stat(dst); err == nil {
		if err := os.Remove(dst); err != nil {
			return fmt.Errorf("remove existing: %w", err)
		}
	}

	if useHardlink {
		if linkErr := os.Link(src, dst); linkErr == nil {
			// Hardlink successful - do NOT remove source (seeding continues)
			return nil
		} else {
			slog.Debug("hardlink failed, falling back to move", "src", src, "dst", dst, "error", linkErr)
		}
	}

	return o.moveFile(src, dst)
}

// moveFile moves a file, handling cross-filesystem moves via copy.
// Uses os.Rename for efficiency when source and dest are on same filesystem.
// Falls back to copy+delete for cross-filesystem moves.
func (o *Organizer) moveFile(src, dst string) error {
	// Try fast rename first
	if err := os.Rename(src, dst); err == nil {
		return nil
	}

	// Cross-filesystem move required - copy then delete
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	// Copy file contents
	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	// Preserve original permissions
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	if err := os.Chmod(dst, srcInfo.Mode()); err != nil {
		slog.Warn("failed to set permissions", "error", err)
	}

	// Delete source after successful copy
	return os.Remove(src)
}

// DetectMedia parses a filename to extract media metadata.
// Handles both TV episode naming (SXXEXX) and movie naming (Title Year).
func (o *Organizer) DetectMedia(fileName string) MediaInfo {
	info := MediaInfo{}

	// Strip extension and trailing dots
	base := extensionRegex.ReplaceAllString(fileName, "")
	base = strings.TrimSuffix(base, ".")

	// Try TV episode pattern first
	if matches := episodeRegexp.FindStringSubmatch(base); len(matches) >= 4 {
		info.Title = cleanTitle(matches[1])
		if s, err := strconv.Atoi(matches[2]); err == nil {
			info.Season = uint32(s)
		}
		if e, err := strconv.Atoi(matches[3]); err == nil {
			info.Episode = uint32(e)
		}
		info.Quality = extractQuality(base)
		info.Codec = extractCodec(base)
		info.Resolution = extractResolution(base)
		info.Group = extractGroup(base)
		return info
	}

	// Try movie pattern
	if matches := movieRegexp.FindStringSubmatch(base); len(matches) >= 3 {
		info.Title = cleanTitle(matches[1])
		if y, err := strconv.Atoi(matches[2]); err == nil {
			info.Year = uint16(y)
		}
		info.Quality = extractQuality(base)
		info.Codec = extractCodec(base)
		info.Resolution = extractResolution(base)
		info.Group = extractGroup(base)
		return info
	}

	// Fallback: just clean the title
	info.Title = cleanTitle(base)
	info.Quality = extractQuality(base)
	return info
}

// DetectQuality extracts quality information from a filename.
func (o *Organizer) DetectQuality(fileName string) string {
	return extractQuality(fileName)
}

// extractQuality finds quality indicators like 720p, 1080p, bluray, etc.
func extractQuality(s string) string {
	if matches := qualityRegexp.FindStringSubmatch(s); len(matches) >= 2 {
		return strings.ToUpper(matches[1])
	}
	return "Unknown"
}

// extractCodec finds video codec identifiers like x264, x265, HEVC.
func extractCodec(s string) string {
	if matches := codecRegexp.FindStringSubmatch(s); len(matches) >= 2 {
		return strings.ToUpper(matches[1])
	}
	return ""
}

// extractResolution standardizes resolution from various formats.
// Maps 2160/4K/UHD to "2160p", 1080 to "1080p", etc.
func extractResolution(s string) string {
	s = strings.ToUpper(s)
	if strings.Contains(s, "2160") || strings.Contains(s, "4K") || strings.Contains(s, "UHD") {
		return "2160p"
	}
	if strings.Contains(s, "1080") {
		return "1080p"
	}
	if strings.Contains(s, "720") {
		return "720p"
	}
	if strings.Contains(s, "480") || strings.Contains(s, "SD") {
		return "480p"
	}
	return ""
}

// extractGroup finds release group from bracketed suffix.
func extractGroup(s string) string {
	if matches := groupRegexp.FindStringSubmatch(s); len(matches) >= 2 {
		return matches[1]
	}
	return ""
}

// cleanTitle removes quality/codec/group tags and normalizes spacing.
// Produces a clean title suitable for folder naming.
func cleanTitle(title string) string {
	title = qualityRegexp.ReplaceAllString(title, "")
	title = codecRegexp.ReplaceAllString(title, "")
	title = groupRegexp.ReplaceAllString(title, "")
	title = strings.ReplaceAll(title, ".", " ")
	title = strings.ReplaceAll(title, "_", " ")
	title = strings.TrimSpace(title)
	title = regexp.MustCompile(`\s+`).ReplaceAllString(title, " ")
	return title
}

// guessEpisodeNumber attempts to find episode number from filename.
// Tries multiple patterns: "e01", "ep01", "episode01", and final numeric segment.
func (o *Organizer) guessEpisodeNumber(fileName string) uint32 {
	base := extensionRegex.ReplaceAllString(fileName, "")

	// Try various episode patterns
	episodes := regexp.MustCompile(`(?:e|ep|episode)\s*(\d+)`)
	matches := episodes.FindAllStringSubmatch(strings.ToLower(base), -1)
	if len(matches) > 0 {
		if n, err := strconv.Atoi(matches[len(matches)-1][1]); err == nil {
			return uint32(n)
		}
	}

	// Try last numeric segment of filename
	parts := strings.Split(base, ".")
	for i := len(parts) - 1; i >= 0; i-- {
		if n, err := strconv.Atoi(parts[i]); err == nil && n > 0 && n < 1000 {
			return uint32(n)
		}
	}

	return 0
}

// CleanupLeftovers removes common download artifacts from a directory.
// Targets partial downloads, temporary files, and non-media files.
func (o *Organizer) CleanupLeftovers(dir string) {
	patterns := []string{
		"*.part",     // Partial downloads (various clients)
		"*.part.*",   // Segmented downloads
		"*.tmp",      // Temporary files
		"*.temp",     // Temporary files
		"*.!qb",      // QBittorrent incomplete
		"*.bak",      // Backup files
		"*.aria2",    // Aria2 control files
		"*.download", // Download markers
		"*.missing",  // Missing file markers
		"*.txt",      // Text files (NFO alternatives)
		"*.nfo",      // NFO files (sometimes kept, often removed)
		"*.jpg",      // Cover images
		"*.png",      // Cover images
		"*.sfv",      // Checksum files
		"*sample*.*", // Sample clips (with extension to avoid false matches)
	}

	for _, pattern := range patterns {
		matches, _ := filepath.Glob(filepath.Join(dir, pattern))
		for _, match := range matches {
			if err := os.Remove(match); err != nil {
				slog.Debug("failed to remove leftover", "file", match, "error", err)
			} else {
				slog.Debug("removed leftover file", "file", match)
			}
		}
	}

	// Remove empty files (failed downloads)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		if info.Size() == 0 {
			os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
}

// IsMediaFile checks if a filename has a media file extension.
func (o *Organizer) IsMediaFile(fileName string) bool {
	ext := strings.ToLower(filepath.Ext(fileName))
	mediaExts := map[string]bool{
		".mkv": true, ".mp4": true, ".avi": true, ".m4v": true,
		".mov": true, ".wmv": true, ".flv": true, ".webm": true,
		".ts": true, ".m2ts": true,
	}

	if mediaExts[ext] {
		return true
	}

	// Fallback to regex check
	match := extensionRegex.FindStringSubmatch(strings.ToLower(fileName))
	return len(match) > 0
}

// FindMediaFiles returns all media files in a directory.
// Filters out non-media files using IsMediaFile.
func (o *Organizer) FindMediaFiles(dir string) []string {
	var mediaFiles []string

	entries, err := os.ReadDir(dir)
	if err != nil {
		return mediaFiles
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		if o.IsMediaFile(entry.Name()) {
			mediaFiles = append(mediaFiles, filepath.Join(dir, entry.Name()))
		}
	}

	return mediaFiles
}

// OrganizeDownloadedFiles is a convenience function for organizing downloaded content.
// Detects media type and calls appropriate organizer based on type.
func (o *Organizer) OrganizeDownloadedFiles(sourceDir, destDir, title string, season, episode uint32, mediaType string, opts OrganizeOptions) []*OrganizeResult {
	results := make([]*OrganizeResult, 0)

	files := o.FindMediaFiles(sourceDir)
	if len(files) == 0 {
		return []*OrganizeResult{{
			Success: false,
			Error:   fmt.Errorf("no media files found in %s", sourceDir),
		}}
	}

	if mediaType == "tv" && season > 0 {
		// Process TV episodes
		for _, sourcePath := range files {
			info := o.DetectMedia(filepath.Base(sourcePath))

			// Use provided season/episode if not detected from filename
			if info.Season == 0 {
				info.Season = season
			}
			if info.Episode == 0 && episode > 0 {
				info.Episode = episode
			}

			var ep db.TVEpisode
			ep.Season = info.Season
			ep.Episode = info.Episode
			ep.Title = info.Title

			var show db.TVShow
			show.Title = title

			result := o.OrganizeEpisode(&show, &ep, sourcePath, destDir, opts)
			results = append(results, result)
		}
	} else if mediaType == "movie" {
		// Process movie
		var movie db.Movie
		movie.Title = title

		if len(files) > 0 {
			info := o.DetectMedia(filepath.Base(files[0]))
			movie.Year = info.Year

			result := o.OrganizeMovie(&movie, files[0], destDir, opts)
			results = append(results, result)
		}
	}

	if opts.DeleteLeftovers && len(results) > 0 {
		o.CleanupLeftovers(sourceDir)
	}

	return results
}
