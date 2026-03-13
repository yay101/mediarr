package oidc

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	lib "github.com/yay101/oidc"
)

type ProviderConfig struct {
	ID           string
	Name         string
	Issuer       string
	ClientID     string
	ClientSecret string
}

type Config struct {
	RedirectURL string
	Providers   []ProviderConfig
}

type Client struct {
	LibClient *lib.Client
	config    *Config
}

func NewClient(config *Config, logger *slog.Logger) (*Client, error) {
	var providers lib.Providers

	for _, p := range config.Providers {
		libProvider := lib.Provider{
			Id:                p.ID,
			Enabled:           true,
			Name:              p.Name,
			ClientId:          p.ClientID,
			ClientSecret:      p.ClientSecret,
			ConfigurationLink: p.Issuer,
			RedirectUri:       "/api/v1/auth/callback",
			Scopes:            []string{"openid", "profile", "email"},
		}

		// Handle well-known issuers for convenience
		issuer := strings.ToLower(p.Issuer)
		if strings.Contains(issuer, "google") && !strings.Contains(issuer, ".well-known") {
			libProvider.ConfigurationLink = "https://accounts.google.com/.well-known/openid-configuration"
		} else if (strings.Contains(issuer, "microsoft") || strings.Contains(issuer, "windows")) && !strings.Contains(issuer, ".well-known") {
			libProvider.ConfigurationLink = "https://login.microsoftonline.com/common/v2.0/.well-known/openid-configuration"
		}

		providers = append(providers, libProvider)
	}

	if len(providers) > 0 {
		providers[0].Default = true
	}

	// Extract domain from RedirectURL
	u, err := url.Parse(config.RedirectURL)
	domain := "*"
	if err == nil {
		domain = u.Host
	}

	client := lib.NewClient(
		[]string{domain, "localhost:8080", "127.0.0.1:8080"}, // allowed domains
		providers,
		"/api/v1/auth/callback", // authpath (redirect path)
		"/login",                // loginpath
		logger,
	)

	return &Client{
		LibClient: client,
		config:    config,
	}, nil
}

func (c *Client) SetCallback(fn func(accesstoken *string, refreshtoken *string, expiry *int, idtoken lib.IDToken) (bool, *http.Cookie)) {
	c.LibClient.Callback = fn
}
