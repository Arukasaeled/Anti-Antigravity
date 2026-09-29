package config

import (
	"fmt"
	"net/url"
	"strings"
)

const DefaultWallpaperPath = "C:/Users/user/Pictures/E78978FCDE46412C17F90AAF7813EC8D.jpg"

// Config contains only Dream Skin settings. Portable installs keep this file
// beside 2ag.exe; the legacy ~/.2ag/config.json location remains supported.
type Config struct {
	WallpaperPath string            `json:"wallpaper_path"`
	Blur          int               `json:"blur"`
	Opacity       float64           `json:"opacity"`
	ModalOpacity  float64           `json:"modal_opacity"`
	Language      string            `json:"language"`
	Network       NetworkConfig     `json:"network"`
	Privacy       PrivacyConfig     `json:"privacy"`
	GravityBoost  GravityBoost      `json:"gravity_boost"`
	GlobalRules   string            `json:"global_rules"`
	EnvOverrides  map[string]string `json:"env_overrides"`
	Plugins       []Plugin          `json:"plugins"`
}

type NetworkConfig struct {
	Enabled           bool               `json:"enabled"`
	EndpointOverrides []EndpointOverride `json:"endpoint_overrides"`
	RuleTargets       []RuleTarget       `json:"rule_targets"`
}

type EndpointOverride struct {
	Host    string            `json:"host"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

// RuleTarget explicitly opts an HTTP JSON endpoint into a top-level string
// field update. TLS CONNECT traffic is opaque and cannot use this feature.
type RuleTarget struct {
	Host       string `json:"host"`
	PathPrefix string `json:"path_prefix"`
	Field      string `json:"field"`
}

type PrivacyConfig struct {
	BlockedHosts []string `json:"blocked_hosts"`
	BlockBeacons bool     `json:"block_beacons"`
}

type GravityBoost struct {
	SessionDelete      bool `json:"session_delete"`
	MarkdownExport     bool `json:"markdown_export"`
	PastePlaintextFix  bool `json:"paste_plaintext_fix"`
	SessionIdTag       bool `json:"session_id_tag"`
	CenteredWidth      bool `json:"centered_width"`
	PreserveScroll     bool `json:"preserve_scroll"`
	Stepwise           bool `json:"stepwise"`
	AnswerOutline      bool `json:"answer_outline"`
	ForceZhCn          bool `json:"force_zh_cn"`
	EnableDevtools     bool `json:"enable_devtools"`
	DisableAutoUpdate  bool `json:"disable_auto_update"`
}

type Plugin struct {
	Name        string   `json:"name"`
	Executable  string   `json:"executable"`
	Args        []string `json:"args,omitempty"`
	Enabled     bool     `json:"enabled"`
	DisplayName string   `json:"-"`
	Version     string         `json:"-"`
	Author      string         `json:"-"`
	Description string         `json:"-"`
	UIEntry     string         `json:"-"`
	Directory   string         `json:"-"`
	Config      map[string]any `json:"config,omitempty"`
}

func Default() Config {
	return Config{
		WallpaperPath: DefaultWallpaperPath,
		Blur:          20,
		Opacity:       0.55,
		ModalOpacity:  0.85,
		Language:      "zh-CN",
		Network:       NetworkConfig{Enabled: true},
		Privacy: PrivacyConfig{BlockedHosts: []string{
			"google-analytics.com", "www.google-analytics.com", "analytics.google.com",
			"www.googletagmanager.com", "crashlyticsreports-pa.googleapis.com",
		}, BlockBeacons: true},
		Plugins: []Plugin{},
	}
}

func (c *Config) Normalize() {
	defaults := Default()
	if strings.TrimSpace(c.WallpaperPath) == "" {
		c.WallpaperPath = defaults.WallpaperPath
	}
	if c.Language != "zh-CN" && c.Language != "en-US" {
		c.Language = defaults.Language
	}
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.WallpaperPath) == "" {
		return fmt.Errorf("wallpaper_path is empty")
	}
	if c.Blur < 0 || c.Blur > 40 {
		return fmt.Errorf("blur must be between 0 and 40")
	}
	if c.Opacity < 0.1 || c.Opacity > 0.9 {
		return fmt.Errorf("opacity must be between 0.1 and 0.9")
	}
	if c.ModalOpacity < 0.1 || c.ModalOpacity > 1.0 {
		return fmt.Errorf("modal_opacity must be between 0.1 and 1.0")
	}
	if c.Language != "zh-CN" && c.Language != "en-US" {
		return fmt.Errorf("language must be zh-CN or en-US")
	}
	for _, override := range c.Network.EndpointOverrides {
		if !validHost(override.Host) {
			return fmt.Errorf("invalid endpoint override host %q", override.Host)
		}
		target, err := url.Parse(override.URL)
		if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" || target.User != nil {
			return fmt.Errorf("invalid endpoint override URL %q", override.URL)
		}
		for name := range override.Headers {
			if strings.TrimSpace(name) == "" || strings.ContainsAny(name, "\r\n:") {
				return fmt.Errorf("invalid override header %q", name)
			}
		}
		for name, value := range override.Headers {
			if strings.ContainsAny(value, "\r\n") {
				return fmt.Errorf("invalid value for override header %q", name)
			}
		}
	}
	for _, target := range c.Network.RuleTargets {
		if !validHost(target.Host) || target.Field == "" || strings.ContainsAny(target.Field, ".[]") {
			return fmt.Errorf("invalid rule target %q", target.Host)
		}
	}
	for _, plugin := range c.Plugins {
		if plugin.Name == "" || plugin.Executable == "" || strings.ContainsAny(plugin.Name, "/\\?#%\r\n") {
			return fmt.Errorf("plugin name and executable are required")
		}
	}
	for name, value := range c.EnvOverrides {
		if strings.TrimSpace(name) == "" || strings.ContainsAny(name, "=\r\n") || strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("invalid environment override %q", name)
		}
	}
	return nil
}

func validHost(host string) bool {
	return host != "" && !strings.ContainsAny(host, "/\\:@ \t\r\n")
}

// WallpaperURL converts a local Windows path to a file URL. url.URL escapes
// spaces and other special characters without escaping the drive colon.
func (c Config) WallpaperURL() (string, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	path := strings.ReplaceAll(strings.TrimSpace(c.WallpaperPath), `\`, "/")
	if strings.HasPrefix(path, "//") {
		parts := strings.SplitN(strings.TrimPrefix(path, "//"), "/", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return "", fmt.Errorf("invalid UNC wallpaper path")
		}
		return (&url.URL{Scheme: "file", Host: parts[0], Path: "/" + parts[1]}).String(), nil
	}
	if len(path) < 3 || path[1] != ':' || path[2] != '/' || !isLetter(path[0]) {
		return "", fmt.Errorf("wallpaper_path must be an absolute Windows path")
	}
	return (&url.URL{Scheme: "file", Path: "/" + path}).String(), nil
}

func isLetter(ch byte) bool {
	return ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z'
}
