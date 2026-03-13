package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server      ServerConfig      `yaml:"server"`
	Database    DatabaseConfig    `yaml:"database"`
	Download    DownloadConfig    `yaml:"download"`
	Library     LibraryConfig     `yaml:"library"`
	TLS         TLSConfig         `yaml:"tls"`
	Auth        AuthConfig        `yaml:"auth"`
	MetadataAPI MetadataAPIConfig `yaml:"metadata_api"`
}

type LibraryConfig struct {
	Movies string `yaml:"movies"`
	TV     string `yaml:"tv"`
}

type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
	URL  string `yaml:"url"`

	WebDAV WebDAVConfig `yaml:"webdav"`
	FTP    FTPConfig    `yaml:"ftp"`
}

type WebDAVConfig struct {
	Enabled bool   `yaml:"enabled"`
	Path    string `yaml:"path"`
}

type FTPConfig struct {
	Enabled bool   `yaml:"enabled"`
	Port    int    `yaml:"port"`
	User    string `yaml:"user"`
	Pass    string `yaml:"pass"`
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
}

type DownloadConfig struct {
	Path     string         `yaml:"path"`
	TempPath string         `yaml:"temp_path"`
	Torrent  TorrentConfig  `yaml:"torrent"`
	Usenet   UsenetConfig   `yaml:"usenet"`
	Indexers IndexersConfig `yaml:"indexers"`
}

type TorrentConfig struct {
	Port               int `yaml:"port"`
	MaxConnections     int `yaml:"max_connections"`
	MaxPeersPerTorrent int `yaml:"max_peers_per_torrent"`
	UploadRateLimit    int `yaml:"upload_rate_limit"`
	DownloadRateLimit  int `yaml:"download_rate_limit"`
}

type UsenetConfig struct {
	Servers     []UsenetServer `yaml:"servers"`
	Connections int            `yaml:"connections"`
}

type UsenetServer struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	UseSSL   bool   `yaml:"use_ssl"`
}

type IndexersConfig struct {
	Torrent []IndexerConfig `yaml:"torrent"`
	Usenet  []IndexerConfig `yaml:"usenet"`
}

type IndexerConfig struct {
	Name     string            `yaml:"name"`
	Enabled  bool              `yaml:"enabled"`
	Type     string            `yaml:"type"`
	URL      string            `yaml:"url"`
	APIKey   string            `yaml:"api_key"`
	Settings map[string]string `yaml:"settings"`
}

type TLSConfig struct {
	Enabled       bool   `yaml:"enabled"`
	Domain        string `yaml:"domain"`
	CertCache     string `yaml:"cert_cache"`
	HTTP11        bool   `yaml:"http11"`
	HTTP2         bool   `yaml:"http2"`
	ChallengePort int    `yaml:"challenge_port"`
}

type AuthConfig struct {
	OIDC     OIDCConfig         `yaml:"oidc"`
	Defaults DefaultUsersConfig `yaml:"defaults"`
}

type OIDCConfig struct {
	Enabled     bool                 `yaml:"enabled"`
	RedirectURL string               `yaml:"redirect_url"`
	Providers   []OIDCProviderConfig `yaml:"providers"`
}

type OIDCProviderConfig struct {
	ID           string `yaml:"id"`
	Name         string `yaml:"name"`
	Issuer       string `yaml:"issuer"`
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
}

type DefaultUsersConfig struct {
	Admin struct {
		Username string `yaml:"username"`
		Password string `yaml:"password"`
	} `yaml:"admin"`
}

type MetadataAPIConfig struct {
	URL      string   `yaml:"url"`
	APIKey   string   `yaml:"api_key"`
	Timeout  Duration `yaml:"timeout"`
	CacheTTL Duration `yaml:"cache_ttl"`
}

type Duration struct {
	value string
}

func (d *Duration) String() string {
	return d.value
}

func (d *Duration) Set(s string) error {
	d.value = s
	return nil
}

func (d *Duration) Duration() (string, error) {
	return d.value, nil
}

var (
	cfgPath   = flag.String("config", "", "path to config file")
	dbPath    = flag.String("db", "", "database path (~/.mediarr/mediarr.db)")
	host      = flag.String("host", "", "server host")
	port      = flag.Int("port", 8080, "server port")
	downloads = flag.String("downloads", "", "downloads directory")
	verbose   = flag.Bool("v", false, "verbose logging")
)

func Load() (*Config, error) {
	flag.Parse()

	cfg := defaultConfig()

	if *cfgPath != "" {
		if err := loadFile(*cfgPath, cfg); err != nil {
			return nil, fmt.Errorf("failed to load config file: %w", err)
		}
	} else if _, err := os.Stat("config.yaml"); err == nil {
		if err := loadFile("config.yaml", cfg); err != nil {
			return nil, fmt.Errorf("failed to load default config.yaml: %w", err)
		}
	} else if _, err := os.Stat("../config.yaml"); err == nil {
		// Also check parent directory for cases where running from cmd/mediarr
		if err := loadFile("../config.yaml", cfg); err != nil {
			return nil, fmt.Errorf("failed to load ../config.yaml: %w", err)
		}
	}

	if *dbPath != "" {
		cfg.Database.Path = *dbPath
	}
	if *host != "" {
		cfg.Server.Host = *host
	}
	if *port != 8080 {
		cfg.Server.Port = *port
	}
	if *downloads != "" {
		cfg.Download.Path = *downloads
	}

	if cfg.Database.Path == "" {
		cfg.Database.Path = defaultDBPath()
	}
	if cfg.Download.Path == "" {
		cfg.Download.Path = defaultDownloadsPath()
	}
	if cfg.Library.Movies == "" {
		cfg.Library.Movies = filepath.Join(filepath.Dir(cfg.Download.Path), "movies")
	}
	if cfg.Library.TV == "" {
		cfg.Library.TV = filepath.Join(filepath.Dir(cfg.Download.Path), "tv")
	}
	if cfg.Download.TempPath == "" {
		cfg.Download.TempPath = filepath.Join(filepath.Dir(cfg.Database.Path), "temp")
	}
	if cfg.TLS.CertCache == "" {
		cfg.TLS.CertCache = filepath.Join(filepath.Dir(cfg.Database.Path), "certs")
	}

	return cfg, nil
}

func defaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: 8080,
			URL:  "http://localhost:8080",
			WebDAV: WebDAVConfig{
				Enabled: true,
				Path:    "/webdav",
			},
			FTP: FTPConfig{
				Enabled: true,
				Port:    21,
			},
		},
		Database: DatabaseConfig{
			Path: "",
		},
		Download: DownloadConfig{
			Path:     "",
			TempPath: "",
			Torrent: TorrentConfig{
				Port:               6881,
				MaxConnections:     100,
				MaxPeersPerTorrent: 50,
				UploadRateLimit:    0,
				DownloadRateLimit:  0,
			},
			Usenet: UsenetConfig{
				Connections: 4,
			},
		},
		TLS: TLSConfig{
			Enabled: false,
			HTTP2:   true,
			HTTP11:  true,
		},
		Auth: AuthConfig{
			OIDC: OIDCConfig{
				Enabled: false,
			},
		},
		MetadataAPI: MetadataAPIConfig{
			URL:     "http://localhost:8081",
			Timeout: Duration{value: "30s"},
		},
	}
}

func loadFile(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, cfg)
}

func defaultDBPath() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = "/tmp"
	}
	return filepath.Join(home, ".mediarr", "mediarr.db")
}

func defaultDownloadsPath() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = "/tmp"
	}
	return filepath.Join(home, "downloads")
}
