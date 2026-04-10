package indexer

import (
	"context"
	"sync"
	"time"
)

// Category represents Usenet/BitTorrent categories used by indexers.
// These map to Torznab/Newznab category IDs.
type Category uint32

const (
	CategoryAll        Category = 0  // All categories
	CategoryMovie      Category = 1  // Movies
	CategoryTV         Category = 2  // TV shows
	CategoryAudio      Category = 3  // Audio/music
	CategoryConsole    Category = 4  // Console games
	CategoryPC         Category = 5  // PC games
	CategoryTVTV       Category = 6  // TV episodes
	CategoryMovieOther Category = 7  // Other movie formats
	CategoryAudioBook  Category = 8  // Audiobooks
	CategoryComics     Category = 9  // Comics
	CategoryPictures   Category = 10 // Pictures
	CategorySoftware   Category = 11 // Software
	CategoryGames      Category = 12 // Games (general)
	CategoryAnime      Category = 13 // Anime
	CategoryXXX        Category = 14 // Adult content
	CategoryBook       Category = 15 // E-books
	CategoryGame       Category = 16 // Games (alternate)
)

// MediaType represents the type of media being searched/downloaded.
// Used for internal categorization and matching.
type MediaType string

const (
	MediaTypeMovie MediaType = "movie"
	MediaTypeTV    MediaType = "tv"
	MediaTypeMusic MediaType = "music"
	MediaTypeBook  MediaType = "book"
	MediaTypeGame  MediaType = "game"
	MediaTypeAnime MediaType = "anime"
	MediaTypeComic MediaType = "comic"
)

// IndexerCapability describes what features an indexer supports.
// Used to determine which indexers can handle specific search types.
type IndexerCapability struct {
	SupportsTVSearch     bool       // Can search for TV shows
	SupportsMovieSearch  bool       // Can search for movies
	SupportsMusicSearch  bool       // Can search for music
	SupportsBookSearch   bool       // Can search for books/e-books
	SupportsAnimeSearch  bool       // Can search for anime
	SupportsGameSearch   bool       // Can search for games
	SupportsAdultSearch  bool       // Can search adult content
	SupportsRssSearch    bool       // Supports RSS feeds
	SupportsAudioSearch  bool       // Can search audio specifically
	SupportsManualSearch bool       // Supports manual searches
	SupportsApiKey       bool       // Requires/configures API key
	SupportedCategories  []Category // Which categories are supported
}

// SearchResult represents a single search result from an indexer.
// Contains all relevant metadata for deciding which result to download.
type SearchResult struct {
	Title       string     // Release/title name
	Link        string     // Download link (torrent/magnet/NZB)
	Guid        string     // Unique identifier for this result
	Category    Category   // Primary category
	Categories  []Category // All matching categories
	PublishDate time.Time  // When the release was published
	Size        int64      // Size in bytes
	Files       int        // Number of files in release
	Grabs       int        // Number of times downloaded
	Seeders     int        // Current seeder count (BitTorrent)
	Leechers    int        // Current leecher count (BitTorrent)
	InfoHash    string     // BitTorrent infohash (hex encoded)
	MagnetURI   string     // Magnet URI for BitTorrent
	TorrentURL  string     // Direct torrent file URL
	NZBLink     string     // NZB download link (Usenet)
	Quality     string     // Quality tag (720p, 1080p, etc.)
	Codec       string     // Video codec (x264, x265, etc.)
	Container   string     // Container format (mkv, mp4, etc.)
	Resolution  string     // Resolution (1920x1080, etc.)
	Group       string     // Release group name
	Origin      string     // Origin/release source (scene, p2p, etc.)
	Tags        []string   // Tags/labels attached to release
	AnimeType   string     // Anime type (TV, movie, OVA, etc.)
	Episode     string     // Episode identifier (S01E01)
	Season      string     // Season identifier
	MediaType   MediaType  // Internal media type
	Indexer     string     // Which indexer returned this

	// External ID fields for matching to library items
	TMDBID int    // TMDB movie/TV ID
	TVDBID int    // TVDB ID
	IMDBID string // IMDB ID

	Year        int  // Release year
	Peers       int  // Total peers (seeders + leechers)
	IsFreeleech bool // Free download (no ratio cost)
	IsRepack    bool // Is a repack/update release
	Priority    int  // Priority score for sorting
	Score       int  // Match score for automation
}

// SearchCache caches search results to reduce indexer load.
// Multiple identical searches return cached results within maxAge.
type SearchCache struct {
	mu      sync.RWMutex
	entries map[string]cacheEntry
	maxAge  time.Duration
}

type cacheEntry struct {
	results []SearchResult
	time    time.Time
}

// GlobalSearchCache is the default search cache with 5-minute TTL.
// Used by the automation system to prevent duplicate searches.
var GlobalSearchCache = NewSearchCache(5 * time.Minute)

// NewSearchCache creates a cache with the specified TTL.
func NewSearchCache(maxAge time.Duration) *SearchCache {
	return &SearchCache{
		entries: make(map[string]cacheEntry),
		maxAge:  maxAge,
	}
}

// Get retrieves cached results if still valid. Returns nil,false if
// expired or not found. Returns a copy to prevent mutation.
func (sc *SearchCache) Get(key string) ([]SearchResult, bool) {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	entry, ok := sc.entries[key]
	if !ok {
		return nil, false
	}

	if time.Since(entry.time) > sc.maxAge {
		return nil, false
	}

	// Return copy to prevent external mutation
	results := make([]SearchResult, len(entry.results))
	copy(results, entry.results)
	return results, true
}

// Set stores search results with current timestamp.
func (sc *SearchCache) Set(key string, results []SearchResult) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	sc.entries[key] = cacheEntry{
		results: results,
		time:    time.Now(),
	}
}

// Clear removes all cached entries.
func (sc *SearchCache) Clear() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.entries = make(map[string]cacheEntry)
}

// CleanOld removes entries older than maxAge.
func (sc *SearchCache) CleanOld() {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	cutoff := time.Now().Add(-sc.maxAge)
	for key, entry := range sc.entries {
		if entry.time.Before(cutoff) {
			delete(sc.entries, key)
		}
	}
}

// Indexer defines the interface for indexer implementations.
// Each indexer type (Torznab, Newznab, Jackett) implements this interface.
type Indexer interface {
	Name() string
	GetCapabilities() IndexerCapability
	Search(ctx context.Context, query string, category Category, limit int) ([]SearchResult, error)
	Test(ctx context.Context) error
	GetConfig() *IndexerConfig
}

// IndexerConfig contains configuration for an indexer instance.
type IndexerConfig struct {
	ID           uint32      // Unique identifier
	Name         string      // Display name
	Type         IndexerType // Implementation type (torznab, newznab, etc.)
	URL          string      // Base URL of the indexer
	APIKey       string      // API key for authentication
	Username     string      // Username (if required)
	Password     string      // Password (if required)
	Categories   []Category  // Categories to search
	Enabled      bool        // Whether this indexer is active
	LastTest     time.Time   // When last tested
	LastResult   bool        // Result of last test
	RssUrl       string      // RSS feed URL (if different from API URL)
	DownloadPath string      // Default download path for results
}

// IndexerType specifies the implementation type of the indexer.
type IndexerType string

const (
	IndexerTypeTorznab IndexerType = "torznab" // Torznab-compatible API
	IndexerTypeNewznab IndexerType = "newznab" // Newznab-compatible API
	IndexerTypeDirect  IndexerType = "direct"  // Direct/API access
	IndexerTypeJackett IndexerType = "jackett" // Jackett aggregator
)

// IndexerFactory creates indexer instances from configuration.
// New indexer types should register a factory during init().
type IndexerFactory func(config *IndexerConfig) (Indexer, error)

// registry holds all registered indexer factories.
var registry = make(map[IndexerType]IndexerFactory)

// Register adds an indexer factory to the global registry.
// Call from init() in each indexer implementation.
func Register(idxType IndexerType, factory IndexerFactory) {
	registry[idxType] = factory
}

// CreateIndexer instantiates an indexer from configuration.
func CreateIndexer(config *IndexerConfig) (Indexer, error) {
	factory, ok := registry[config.Type]
	if !ok {
		return nil, ErrUnknownIndexerType
	}
	return factory(config)
}

// IndexerError represents indexer-specific errors.
type IndexerError string

func (e IndexerError) Error() string { return string(e) }

const (
	ErrUnknownIndexerType IndexerError = "unknown indexer type"
	ErrInvalidConfig      IndexerError = "invalid indexer configuration"
	ErrSearchFailed       IndexerError = "search failed"
	ErrTestFailed         IndexerError = "indexer test failed"
	ErrNotSupported       IndexerError = "operation not supported"
)

// ParseCategory converts string category names to Category values.
// Accepts common variations (e.g., "tv", "tvshow", "tvshows").
func ParseCategory(catStr string) Category {
	switch catStr {
	case "movie", "movies":
		return CategoryMovie
	case "tv", "tvshow", "tvshows":
		return CategoryTV
	case "audio", "music":
		return CategoryAudio
	case "book", "ebooks":
		return CategoryBook
	case "anime":
		return CategoryAnime
	case "comics":
		return CategoryComics
	case "games", "game":
		return CategoryGame
	case "software":
		return CategorySoftware
	case "xxx":
		return CategoryXXX
	default:
		return CategoryAll
	}
}

// MapCategoryToTorznab converts internal categories to Torznab category IDs.
// Each media type maps to multiple Torznab IDs for different subcategories.
func MapCategoryToTorznab(cat Category) []string {
	switch cat {
	case CategoryMovie:
		return []string{"2000", "2010", "2020", "2030", "2040", "2050", "2060"}
	case CategoryTV:
		return []string{"5000", "5010", "5020", "5030", "5040", "5050", "5060", "5070", "5080"}
	case CategoryAudio:
		return []string{"3000", "3010", "3020", "3030", "3040", "3050"}
	case CategoryBook:
		return []string{"7000", "7010", "7020", "7030", "7040", "7050"}
	case CategoryAnime:
		return []string{"5070"}
	case CategoryGame:
		return []string{"4000", "4010", "4020", "4030", "4040", "4050", "4060", "4070", "4080", "4090"}
	case CategorySoftware:
		return []string{"13000", "13010", "13020", "13030", "13040", "13050"}
	case CategoryXXX:
		return []string{"6000", "6010", "6020", "6030", "6040", "6050", "6060", "6070", "6080"}
	default:
		return []string{}
	}
}

// MapTorznabToCategory converts Torznab category IDs back to internal categories.
func MapTorznabToCategory(catStr string) Category {
	switch catStr {
	case "2000", "2010", "2020", "2030", "2040", "2050", "2060":
		return CategoryMovie
	case "5000", "5010", "5020", "5030", "5040", "5050", "5060", "5080":
		return CategoryTV
	case "5070":
		return CategoryAnime
	case "3000", "3010", "3020", "3030", "3040", "3050":
		return CategoryAudio
	case "7000", "7010", "7020", "7030", "7040", "7050":
		return CategoryBook
	case "4000", "4010", "4020", "4030", "4040", "4050", "4060", "4070", "4080", "4090":
		return CategoryGame
	case "13000", "13010", "13020", "13030", "13040", "13050":
		return CategorySoftware
	case "6000", "6010", "6020", "6030", "6040", "6050", "6060", "6070", "6080":
		return CategoryXXX
	default:
		return CategoryAll
	}
}

func (m MediaType) String() string   { return string(m) }
func (c Category) Int() int          { return int(c) }
func (c Category) String() string    { return string(rune(c)) }
func (t IndexerType) String() string { return string(t) }
