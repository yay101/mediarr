package tracker

import "time"

type TrackerConfig struct {
	ID           uint32            `json:"id" db:"id"`
	Name         string            `json:"name" db:"name"`
	Type         TrackerType       `json:"type" db:"type"`
	URL          string            `json:"url" db:"url"`
	Username     string            `json:"username,omitempty" db:"username"`
	Password     string            `json:"password,omitempty" db:"password"`
	APIKey       string            `json:"api_key,omitempty" db:"api_key"`
	PassKey      string            `json:"passkey,omitempty" db:"passkey"`
	Cookie       string            `json:"cookie,omitempty" db:"cookie"`
	CookieExpiry time.Time         `json:"cookie_expiry,omitempty" db:"cookie_expiry"`
	AuthToken    string            `json:"auth_token,omitempty" db:"auth_token"`
	Settings     map[string]string `json:"settings,omitempty" db:"-"`
	Enabled      bool              `json:"enabled" db:"enabled"`
	LastAuth     time.Time         `json:"last_auth,omitempty" db:"last_auth"`
	LastResult   bool              `json:"last_result" db:"last_result"`
}

type TrackerType string

const (
	TrackerTypeBasic        TrackerType = "basic"
	TrackerTypeAnonMouse    TrackerType = "anonamouse"
	TrackerTypeRedacted     TrackerType = "redacted"
	TrackerTypeBTN          TrackerType = "btn"
	TrackerTypeTorrentLeech TrackerType = "torrentleech"
)

func (t TrackerType) String() string { return string(t) }

func (c *TrackerConfig) Clone() *TrackerConfig {
	clone := *c
	if c.Settings != nil {
		clone.Settings = make(map[string]string, len(c.Settings))
		for k, v := range c.Settings {
			clone.Settings[k] = v
		}
	}
	return &clone
}

func (c *TrackerConfig) GetSetting(key, defaultValue string) string {
	if c.Settings == nil {
		return defaultValue
	}
	if v, ok := c.Settings[key]; ok {
		return v
	}
	return defaultValue
}

func (c *TrackerConfig) SetSetting(key, value string) {
	if c.Settings == nil {
		c.Settings = make(map[string]string)
	}
	c.Settings[key] = value
}
