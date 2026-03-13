package indexer

import (
	"context"
	"time"
)

type Category uint32

const (
	CategoryAll        Category = 0
	CategoryMovie      Category = 1
	CategoryTV         Category = 2
	CategoryAudio      Category = 3
	CategoryConsole    Category = 4
	CategoryPC         Category = 5
	CategoryTVTV       Category = 6
	CategoryMovieOther Category = 7
	CategoryAudioBook  Category = 8
	CategoryComics     Category = 9
	CategoryPictures   Category = 10
	CategorySoftware   Category = 11
	CategoryGames      Category = 12
	CategoryAnime      Category = 13
	CategoryXXX        Category = 14
	CategoryBook       Category = 15
	CategoryGame       Category = 16
)

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

type IndexerCapability struct {
	SupportsTVSearch     bool
	SupportsMovieSearch  bool
	SupportsMusicSearch  bool
	SupportsBookSearch   bool
	SupportsAnimeSearch  bool
	SupportsGameSearch   bool
	SupportsAdultSearch  bool
	SupportsRssSearch    bool
	SupportsAudioSearch  bool
	SupportsManualSearch bool
	SupportsApiKey       bool
	SupportedCategories  []Category
}

type SearchResult struct {
	Title       string
	Link        string
	Guid        string
	Category    Category
	Categories  []Category
	PublishDate time.Time
	Size        int64
	Files       int
	Grabs       int
	Seeders     int
	Leechers    int
	InfoHash    string
	MagnetURI   string
	TorrentURL  string
	NZBLink     string
	Quality     string
	Codec       string
	Container   string
	Resolution  string
	Group       string
	Origin      string
	Tags        []string
	AnimeType   string
	Episode     string
	Season      string
	MediaType   MediaType
	Indexer     string
}

type Indexer interface {
	Name() string
	GetCapabilities() IndexerCapability
	Search(ctx context.Context, query string, category Category, limit int) ([]SearchResult, error)
	Test(ctx context.Context) error
	GetConfig() *IndexerConfig
}

type IndexerConfig struct {
	ID           uint32
	Name         string
	Type         IndexerType
	URL          string
	APIKey       string
	Username     string
	Password     string
	Categories   []Category
	Enabled      bool
	LastTest     time.Time
	LastResult   bool
	RssUrl       string
	DownloadPath string
}

type IndexerType string

const (
	IndexerTypeTorznab IndexerType = "torznab"
	IndexerTypeNewznab IndexerType = "newznab"
	IndexerTypeDirect  IndexerType = "direct"
	IndexerTypeJackett IndexerType = "jackett"
)

type IndexerFactory func(config *IndexerConfig) (Indexer, error)

var registry = make(map[IndexerType]IndexerFactory)

func Register(idxType IndexerType, factory IndexerFactory) {
	registry[idxType] = factory
}

func CreateIndexer(config *IndexerConfig) (Indexer, error) {
	factory, ok := registry[config.Type]
	if !ok {
		return nil, ErrUnknownIndexerType
	}
	return factory(config)
}

type IndexerError string

func (e IndexerError) Error() string { return string(e) }

const (
	ErrUnknownIndexerType IndexerError = "unknown indexer type"
	ErrInvalidConfig      IndexerError = "invalid indexer configuration"
	ErrSearchFailed       IndexerError = "search failed"
	ErrTestFailed         IndexerError = "indexer test failed"
	ErrNotSupported       IndexerError = "operation not supported"
)

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
