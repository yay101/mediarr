package db

import "time"

type MediaType uint8

const (
	MediaTypeMovie MediaType = iota
	MediaTypeTV
	MediaTypeMusic
	MediaTypeBook
	MediaTypeManga
)

type MediaStatus uint8

const (
	MediaStatusMissing MediaStatus = iota
	MediaStatusQueued
	MediaStatusSearching
	MediaStatusDownloading
	MediaStatusAvailable
	MediaStatusFailed
)

type DownloadStatus uint8

const (
	DownloadStatusQueued DownloadStatus = iota
	DownloadStatusDownloading
	DownloadStatusSeeding
	DownloadStatusPaused
	DownloadStatusComplete
	DownloadStatusFailed
)

type DownloadProvider uint8

const (
	DownloadProviderTorrent DownloadProvider = iota
	DownloadProviderUsenet
)

type UserRole uint8

const (
	RoleAdmin UserRole = iota
	RoleUser
)

type ImageInfo struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type RatingInfo struct {
	Value float64 `json:"value"`
	Max   int     `json:"max"`
}

type PersonCredit struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Role  string `json:"role"`
	Image string `json:"image"`
}

type CreditsInfo struct {
	Directors []PersonCredit `json:"directors"`
	Cast      []PersonCredit `json:"cast"`
	Writers   []PersonCredit `json:"writers"`
}

type MovieMetadata struct {
	Overview    string      `json:"overview"`
	Poster      ImageInfo   `json:"poster"`
	Backdrop    ImageInfo   `json:"backdrop"`
	Rating      RatingInfo  `json:"rating"`
	Genres      []string    `json:"genres"`
	Credits     CreditsInfo `json:"credits"`
	Runtime     int         `json:"runtime"`
	Tagline     string      `json:"tagline"`
	Studio      string      `json:"studio"`
	IMDBID      string      `json:"imdb_id"`
	TMDBID      int         `json:"tmdb_id"`
	ReleaseDate string      `json:"release_date"`
}

type TVShowMetadata struct {
	Overview       string      `json:"overview"`
	Poster         ImageInfo   `json:"poster"`
	Backdrop       ImageInfo   `json:"backdrop"`
	Rating         RatingInfo  `json:"rating"`
	Genres         []string    `json:"genres"`
	Credits        CreditsInfo `json:"credits"`
	EpisodeCount   int         `json:"episode_count"`
	SeasonCount    int         `json:"season_count"`
	EpisodeRunTime int         `json:"episode_run_time"`
	Network        string      `json:"network"`
	ContentRating  string      `json:"content_rating"`
	InProduction   bool        `json:"in_production"`
	LastAirDate    string      `json:"last_air_date"`
	TMDBID         int         `json:"tmdb_id"`
	ReleaseDate    string      `json:"release_date"`
}

type TVEpisodeMetadata struct {
	Overview   string     `json:"overview"`
	Still      ImageInfo  `json:"still"`
	Rating     RatingInfo `json:"rating"`
	SeasonNum  int        `json:"season_number"`
	EpisodeNum int        `json:"episode_number"`
	AirDate    string     `json:"air_date"`
}

type MusicAlbumMetadata struct {
	Overview      string     `json:"overview"`
	Cover         ImageInfo  `json:"cover"`
	Rating        RatingInfo `json:"rating"`
	Genres        []string   `json:"genres"`
	Artist        string     `json:"artist"`
	Album         string     `json:"album"`
	ReleaseDate   string     `json:"release_date"`
	Label         string     `json:"label"`
	TrackCount    int        `json:"track_count"`
	DiscCount     int        `json:"disc_count"`
	Duration      int        `json:"duration"`
	MusicBrainzID string     `json:"musicbrainz_id"`
}

type BookMetadata struct {
	Overview      string     `json:"overview"`
	Cover         ImageInfo  `json:"cover"`
	Rating        RatingInfo `json:"rating"`
	Genres        []string   `json:"genres"`
	Authors       []string   `json:"authors"`
	ISBN          string     `json:"isbn"`
	PublishDate   string     `json:"publish_date"`
	PageCount     int        `json:"page_count"`
	Publisher     string     `json:"publisher"`
	OpenLibraryID string     `json:"openlibrary_id"`
}

type MangaMetadata struct {
	Overview    string     `json:"overview"`
	Cover       ImageInfo  `json:"cover"`
	Rating      RatingInfo `json:"rating"`
	Genres      []string   `json:"genres"`
	Authors     []string   `json:"authors"`
	Volumes     int        `json:"volumes"`
	Chapters    int        `json:"chapters"`
	Status      string     `json:"status"`
	MangaDexID  int        `json:"mangadex_id"`
	Demographic string     `json:"demographic"`
}

type Movie struct {
	ID        uint32      `db:"id,primary"`
	UserID    uint32      `db:"index"`
	Title     string      `db:"index"`
	Year      uint16      `db:"index"`
	TMDBID    uint32      `db:"index"`
	IMDBID    string      `db:"index"`
	Status    MediaStatus `db:"index"`
	Path      string
	Size      uint64
	Quality   string
	Metadata  MovieMetadata `db:"index"`
	AddedAt   time.Time     `db:"index"`
	UpdatedAt time.Time
}

type TVShow struct {
	ID        uint32      `db:"id,primary"`
	UserID    uint32      `db:"index"`
	Title     string      `db:"index"`
	Year      uint16      `db:"index"`
	TMDBID    uint32      `db:"index"`
	Status    MediaStatus `db:"index"`
	Monitored bool        `db:"index"`
	Path      string
	Metadata  TVShowMetadata `db:"index"`
	AddedAt   time.Time      `db:"index"`
	UpdatedAt time.Time
}

type TVEpisode struct {
	ID        uint32 `db:"id,primary"`
	ShowID    uint32 `db:"index"`
	Season    uint32 `db:"index"`
	Episode   uint32 `db:"index"`
	Title     string
	TMDBID    uint32      `db:"index,unique"`
	Status    MediaStatus `db:"index"`
	Monitored bool        `db:"index"`
	Path      string
	Size      uint64
	Metadata  TVEpisodeMetadata `db:"index"`
	AirDate   time.Time
	AddedAt   time.Time `db:"index"`
	UpdatedAt time.Time
}

type MusicAlbum struct {
	ID            uint32      `db:"id,primary"`
	Title         string      `db:"index"`
	Artist        string      `db:"index"`
	Year          uint16      `db:"index"`
	MusicBrainzID string      `db:"index,unique"`
	Status        MediaStatus `db:"index"`
	Path          string
	Size          uint64
	Metadata      MusicAlbumMetadata `db:"index"`
	AddedAt       time.Time          `db:"index"`
	UpdatedAt     time.Time
}

type MusicTrack struct {
	ID            uint32 `db:"id,primary"`
	AlbumID       uint32 `db:"index"`
	Title         string `db:"index"`
	TrackNum      uint32
	Duration      uint32
	MusicBrainzID string `db:"index,unique"`
	Path          string
	Size          uint64
	AddedAt       time.Time `db:"index"`
	UpdatedAt     time.Time
}

type Book struct {
	ID            uint32      `db:"id,primary"`
	Title         string      `db:"index"`
	Author        string      `db:"index"`
	Year          uint16      `db:"index"`
	ISBN          string      `db:"index,unique"`
	OpenLibraryID string      `db:"index,unique"`
	Status        MediaStatus `db:"index"`
	Path          string
	Size          uint64
	Metadata      BookMetadata `db:"index"`
	AddedAt       time.Time    `db:"index"`
	UpdatedAt     time.Time
}

type Manga struct {
	ID         uint32      `db:"id,primary"`
	Title      string      `db:"index"`
	Year       uint16      `db:"index"`
	MangaDexID string      `db:"index,unique"`
	Status     MediaStatus `db:"index"`
	Path       string
	Metadata   MangaMetadata `db:"index"`
	AddedAt    time.Time     `db:"index"`
	UpdatedAt  time.Time
}

type MangaChapter struct {
	ID          uint32 `db:"id,primary"`
	MangaID     uint32 `db:"index"`
	Chapter     uint32 `db:"index"`
	Volume      uint32
	Title       string
	MangaDexID  uint32      `db:"index,unique"`
	Language    string      `db:"index"`
	Status      MediaStatus `db:"index"`
	Path        string
	Size        uint64
	Group       string
	ReleaseDate time.Time
	AddedAt     time.Time `db:"index"`
	UpdatedAt   time.Time
}

type DownloadJob struct {
	ID        uint32           `db:"id,primary"`
	UserID    uint32           `db:"index"`
	MediaType MediaType        `db:"index"`
	MediaID   uint32           `db:"index"`
	Title     string           `db:"index"`
	Provider  DownloadProvider `db:"index"`

	InfoHash  string
	MagnetURI string
	NZBData   string
	Group     string

	BytesDone  uint64
	BytesTotal uint64
	Progress   float32
	Status     DownloadStatus `db:"index"`
	ErrorMsg   string

	CreatedAt   time.Time `db:"index"`
	UpdatedAt   time.Time
	CompletedAt time.Time
}

type User struct {
	ID           uint32 `db:"id,primary"`
	Username     string `db:"index,unique"`
	PasswordHash string
	OIDCSubject  string `db:"index,unique"`
	Role         UserRole
	CreatedAt    time.Time
}

type Setting struct {
	Key       string `db:"id,primary"`
	Value     string
	UpdatedAt time.Time
}

type IndexerConfig struct {
	ID           uint32 `db:"id,primary"`
	Name         string `db:"index"`
	Type         string `db:"index"`
	URL          string
	APIKey       string
	Username     string
	Password     string
	Categories   string
	Enabled      bool `db:"index"`
	LastTest     time.Time
	LastResult   bool
	RssUrl       string
	DownloadPath string
	CreatedAt    time.Time `db:"index"`
	UpdatedAt    time.Time
}

type RSSFeed struct {
	ID          uint32 `db:"id,primary"`
	Name        string `db:"index"`
	URL         string `db:"index,unique"`
	Filter      string
	Interval    uint32
	Enabled     bool `db:"index"`
	DownloadDir string
	MediaType   MediaType
	LastGUID    string
	LastCheck   time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type WatchlistItem struct {
	ID          uint32    `db:"id,primary"`
	UserID      uint32    `db:"index"`
	MediaType   MediaType `db:"index"`
	MediaID     uint32    `db:"index"`
	Title       string    `db:"index"`
	Year        uint16    `db:"index"`
	Quality     string
	Keywords    string
	Status      MediaStatus `db:"index"`
	Complete    bool        `db:"index"`
	LastSearch  time.Time
	SearchCount uint32
	AddedAt     time.Time `db:"index"`
	UpdatedAt   time.Time
}

type QualityProfile struct {
	ID            uint32 `db:"id,primary"`
	Name          string `db:"index,unique"`
	PreferredType string
	MinScore      uint32
	AllowedRes    string
	PreferredRes  string
	AllowedCodecs string
	AllowedGroups string
	MinSize       int64
	MaxSize       int64
	CreatedAt     time.Time `db:"index"`
	UpdatedAt     time.Time
}
