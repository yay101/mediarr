package db

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/yay101/embeddb"
)

type Database struct {
	path string
	mu   sync.RWMutex

	core *embeddb.DB

	movies             *embeddb.Table[Movie]
	tvshows            *embeddb.Table[TVShow]
	tvepisodes         *embeddb.Table[TVEpisode]
	musicalbums        *embeddb.Table[MusicAlbum]
	musictracks        *embeddb.Table[MusicTrack]
	books              *embeddb.Table[Book]
	audiobooks         *embeddb.Table[Audiobook]
	manga              *embeddb.Table[Manga]
	mangachapters      *embeddb.Table[MangaChapter]
	downloads          *embeddb.Table[DownloadJob]
	users              *embeddb.Table[User]
	settings           *embeddb.Table[Setting]
	rssfeeds           *embeddb.Table[RSSFeed]
	watchlist          *embeddb.Table[WatchlistItem]
	qualityprofiles    *embeddb.Table[QualityProfile]
	indexerconfigs     *embeddb.Table[IndexerConfig]
	storagelocations   *embeddb.Table[StorageLocation]
	storagepreferences *embeddb.Table[StoragePreference]
}

func New(path string) (*Database, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	return &Database{path: path}, nil
}

func (d *Database) getCore() (*embeddb.DB, error) {
	d.mu.RLock()
	db := d.core
	d.mu.RUnlock()
	if db != nil {
		return db, nil
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.core != nil {
		return d.core, nil
	}

	opened, err := embeddb.Open(d.path)
	if err != nil {
		return nil, err
	}
	d.core = opened
	return opened, nil
}

func (d *Database) getMovies() (*embeddb.Table[Movie], error) {
	d.mu.RLock()
	table := d.movies
	d.mu.RUnlock()
	if table != nil {
		return table, nil
	}

	core, err := d.getCore()
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.movies != nil {
		return d.movies, nil
	}

	table, err = embeddb.Use[Movie](core, "movies")
	if err != nil {
		return nil, err
	}
	d.movies = table
	return table, nil
}

func (d *Database) getTVShows() (*embeddb.Table[TVShow], error) {
	d.mu.RLock()
	table := d.tvshows
	d.mu.RUnlock()
	if table != nil {
		return table, nil
	}

	core, err := d.getCore()
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.tvshows != nil {
		return d.tvshows, nil
	}

	table, err = embeddb.Use[TVShow](core, "tv_shows")
	if err != nil {
		return nil, err
	}
	d.tvshows = table
	return table, nil
}

func (d *Database) getTVEpisodes() (*embeddb.Table[TVEpisode], error) {
	d.mu.RLock()
	table := d.tvepisodes
	d.mu.RUnlock()
	if table != nil {
		return table, nil
	}

	core, err := d.getCore()
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.tvepisodes != nil {
		return d.tvepisodes, nil
	}

	table, err = embeddb.Use[TVEpisode](core, "tv_episodes")
	if err != nil {
		return nil, err
	}
	d.tvepisodes = table
	return table, nil
}

func (d *Database) getMusicAlbums() (*embeddb.Table[MusicAlbum], error) {
	d.mu.RLock()
	table := d.musicalbums
	d.mu.RUnlock()
	if table != nil {
		return table, nil
	}

	core, err := d.getCore()
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.musicalbums != nil {
		return d.musicalbums, nil
	}

	table, err = embeddb.Use[MusicAlbum](core, "music_albums")
	if err != nil {
		return nil, err
	}
	d.musicalbums = table
	return table, nil
}

func (d *Database) getMusicTracks() (*embeddb.Table[MusicTrack], error) {
	d.mu.RLock()
	table := d.musictracks
	d.mu.RUnlock()
	if table != nil {
		return table, nil
	}

	core, err := d.getCore()
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.musictracks != nil {
		return d.musictracks, nil
	}

	table, err = embeddb.Use[MusicTrack](core, "music_tracks")
	if err != nil {
		return nil, err
	}
	d.musictracks = table
	return table, nil
}

func (d *Database) getBooks() (*embeddb.Table[Book], error) {
	d.mu.RLock()
	table := d.books
	d.mu.RUnlock()
	if table != nil {
		return table, nil
	}

	core, err := d.getCore()
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.books != nil {
		return d.books, nil
	}

	table, err = embeddb.Use[Book](core, "books")
	if err != nil {
		return nil, err
	}
	d.books = table
	return table, nil
}

func (d *Database) getAudiobooks() (*embeddb.Table[Audiobook], error) {
	d.mu.RLock()
	table := d.audiobooks
	d.mu.RUnlock()
	if table != nil {
		return table, nil
	}

	core, err := d.getCore()
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.audiobooks != nil {
		return d.audiobooks, nil
	}

	table, err = embeddb.Use[Audiobook](core, "audiobooks")
	if err != nil {
		return nil, err
	}
	d.audiobooks = table
	return table, nil
}

func (d *Database) getManga() (*embeddb.Table[Manga], error) {
	d.mu.RLock()
	table := d.manga
	d.mu.RUnlock()
	if table != nil {
		return table, nil
	}

	core, err := d.getCore()
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.manga != nil {
		return d.manga, nil
	}

	table, err = embeddb.Use[Manga](core, "manga")
	if err != nil {
		return nil, err
	}
	d.manga = table
	return table, nil
}

func (d *Database) getMangaChapters() (*embeddb.Table[MangaChapter], error) {
	d.mu.RLock()
	table := d.mangachapters
	d.mu.RUnlock()
	if table != nil {
		return table, nil
	}

	core, err := d.getCore()
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.mangachapters != nil {
		return d.mangachapters, nil
	}

	table, err = embeddb.Use[MangaChapter](core, "manga_chapters")
	if err != nil {
		return nil, err
	}
	d.mangachapters = table
	return table, nil
}

func (d *Database) getDownloads() (*embeddb.Table[DownloadJob], error) {
	d.mu.RLock()
	table := d.downloads
	d.mu.RUnlock()
	if table != nil {
		return table, nil
	}

	core, err := d.getCore()
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.downloads != nil {
		return d.downloads, nil
	}

	table, err = embeddb.Use[DownloadJob](core, "downloads")
	if err != nil {
		return nil, err
	}
	d.downloads = table
	return table, nil
}

func (d *Database) getUsers() (*embeddb.Table[User], error) {
	d.mu.RLock()
	table := d.users
	d.mu.RUnlock()
	if table != nil {
		return table, nil
	}

	core, err := d.getCore()
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.users != nil {
		return d.users, nil
	}

	table, err = embeddb.Use[User](core, "users")
	if err != nil {
		return nil, err
	}
	d.users = table
	return table, nil
}

func (d *Database) getSettings() (*embeddb.Table[Setting], error) {
	d.mu.RLock()
	table := d.settings
	d.mu.RUnlock()
	if table != nil {
		return table, nil
	}

	core, err := d.getCore()
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.settings != nil {
		return d.settings, nil
	}

	table, err = embeddb.Use[Setting](core, "settings", embeddb.UseOptions{MaxVersions: 10})
	if err != nil {
		return nil, err
	}
	d.settings = table
	return table, nil
}

func (d *Database) getRSSFeeds() (*embeddb.Table[RSSFeed], error) {
	d.mu.RLock()
	table := d.rssfeeds
	d.mu.RUnlock()
	if table != nil {
		return table, nil
	}

	core, err := d.getCore()
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.rssfeeds != nil {
		return d.rssfeeds, nil
	}

	table, err = embeddb.Use[RSSFeed](core, "rss_feeds")
	if err != nil {
		return nil, err
	}
	d.rssfeeds = table
	return table, nil
}

func (d *Database) getWatchlist() (*embeddb.Table[WatchlistItem], error) {
	d.mu.RLock()
	table := d.watchlist
	d.mu.RUnlock()
	if table != nil {
		return table, nil
	}

	core, err := d.getCore()
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.watchlist != nil {
		return d.watchlist, nil
	}

	table, err = embeddb.Use[WatchlistItem](core, "watchlist")
	if err != nil {
		return nil, err
	}
	d.watchlist = table
	return table, nil
}

func (d *Database) getQualityProfiles() (*embeddb.Table[QualityProfile], error) {
	d.mu.RLock()
	table := d.qualityprofiles
	d.mu.RUnlock()
	if table != nil {
		return table, nil
	}

	core, err := d.getCore()
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.qualityprofiles != nil {
		return d.qualityprofiles, nil
	}

	table, err = embeddb.Use[QualityProfile](core, "quality_profiles")
	if err != nil {
		return nil, err
	}
	d.qualityprofiles = table
	return table, nil
}

func (d *Database) getIndexerConfigs() (*embeddb.Table[IndexerConfig], error) {
	d.mu.RLock()
	table := d.indexerconfigs
	d.mu.RUnlock()
	if table != nil {
		return table, nil
	}

	core, err := d.getCore()
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.indexerconfigs != nil {
		return d.indexerconfigs, nil
	}

	table, err = embeddb.Use[IndexerConfig](core, "indexer_configs")
	if err != nil {
		return nil, err
	}
	d.indexerconfigs = table
	return table, nil
}

func (d *Database) Close() error {
	d.mu.Lock()
	core := d.core
	d.core = nil
	d.movies = nil
	d.tvshows = nil
	d.tvepisodes = nil
	d.musicalbums = nil
	d.musictracks = nil
	d.books = nil
	d.audiobooks = nil
	d.manga = nil
	d.mangachapters = nil
	d.downloads = nil
	d.users = nil
	d.settings = nil
	d.rssfeeds = nil
	d.watchlist = nil
	d.qualityprofiles = nil
	d.indexerconfigs = nil
	d.storagelocations = nil
	d.storagepreferences = nil
	d.mu.Unlock()

	if core == nil {
		return nil
	}
	return core.Close()
}

func (d *Database) Movies() (*embeddb.Table[Movie], error) {
	return d.getMovies()
}

func (d *Database) TVShows() (*embeddb.Table[TVShow], error) {
	return d.getTVShows()
}

func (d *Database) TVEpisodes() (*embeddb.Table[TVEpisode], error) {
	return d.getTVEpisodes()
}

func (d *Database) MusicAlbums() (*embeddb.Table[MusicAlbum], error) {
	return d.getMusicAlbums()
}

func (d *Database) MusicTracks() (*embeddb.Table[MusicTrack], error) {
	return d.getMusicTracks()
}

func (d *Database) Books() (*embeddb.Table[Book], error) {
	return d.getBooks()
}

func (d *Database) Audiobooks() (*embeddb.Table[Audiobook], error) {
	return d.getAudiobooks()
}

func (d *Database) Manga() (*embeddb.Table[Manga], error) {
	return d.getManga()
}

func (d *Database) MangaChapters() (*embeddb.Table[MangaChapter], error) {
	return d.getMangaChapters()
}

func (d *Database) Downloads() (*embeddb.Table[DownloadJob], error) {
	return d.getDownloads()
}

func (d *Database) Users() (*embeddb.Table[User], error) {
	return d.getUsers()
}

func (d *Database) Settings() (*embeddb.Table[Setting], error) {
	return d.getSettings()
}

func (d *Database) RSSFeeds() (*embeddb.Table[RSSFeed], error) {
	return d.getRSSFeeds()
}

func (d *Database) Watchlist() (*embeddb.Table[WatchlistItem], error) {
	return d.getWatchlist()
}

func (d *Database) QualityProfiles() (*embeddb.Table[QualityProfile], error) {
	return d.getQualityProfiles()
}

func (d *Database) IndexerConfigs() (*embeddb.Table[IndexerConfig], error) {
	return d.getIndexerConfigs()
}

func (d *Database) getStorageLocations() (*embeddb.Table[StorageLocation], error) {
	d.mu.RLock()
	table := d.storagelocations
	d.mu.RUnlock()
	if table != nil {
		return table, nil
	}

	core, err := d.getCore()
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.storagelocations != nil {
		return d.storagelocations, nil
	}

	table, err = embeddb.Use[StorageLocation](core, "storage_locations")
	if err != nil {
		return nil, err
	}
	d.storagelocations = table
	return table, nil
}

func (d *Database) getStoragePreferences() (*embeddb.Table[StoragePreference], error) {
	d.mu.RLock()
	table := d.storagepreferences
	d.mu.RUnlock()
	if table != nil {
		return table, nil
	}

	core, err := d.getCore()
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.storagepreferences != nil {
		return d.storagepreferences, nil
	}

	table, err = embeddb.Use[StoragePreference](core, "storage_preferences")
	if err != nil {
		return nil, err
	}
	d.storagepreferences = table
	return table, nil
}

func (d *Database) StorageLocations() (*embeddb.Table[StorageLocation], error) {
	return d.getStorageLocations()
}

func (d *Database) StoragePreferences() (*embeddb.Table[StoragePreference], error) {
	return d.getStoragePreferences()
}
