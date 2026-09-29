package patcher

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed injected_hub.js
var hubSource string

// HubConfig is the small state bridge shared by Go's initial injection and the
// in-app localStorage controls. The renderer owns subsequent changes.
type HubConfig struct {
	Wallpaper     string            `json:"wallpaper"`
	LogoURL       string            `json:"logo_url"`
	WallpaperPath string            `json:"wallpaper_path"`
	Blur          int               `json:"blur"`
	Opacity       float64           `json:"opacity"`
	Language      string            `json:"language"`
	Preset        string            `json:"preset"`
	Plugins       map[string]any    `json:"plugins"`
	CDPPort       int               `json:"cdp_port,omitempty"`
	HostPID       int               `json:"host_pid,omitempty"`
	Env           map[string]string `json:"env_overrides,omitempty"`
	ProxyURL      string            `json:"proxy_url,omitempty"`
	Network       any               `json:"network,omitempty"`
	Privacy       any               `json:"privacy,omitempty"`
	GlobalRules   string            `json:"global_rules,omitempty"`
	PluginURL     string            `json:"plugin_url,omitempty"`
}

func (c HubConfig) Normalize() HubConfig {
	if strings.TrimSpace(c.Wallpaper) == "" {
		c.Wallpaper = WallpaperServerURL
	}
	if strings.TrimSpace(c.LogoURL) == "" {
		c.LogoURL = "http://" + WallpaperServerAddress + "/logo.png"
	}
	if c.Blur < 0 || c.Blur > 40 {
		c.Blur = 20
	}
	if c.Opacity < 0.1 || c.Opacity > 0.9 {
		c.Opacity = 0.55
	}
	if c.Preset == "" {
		c.Preset = "Dark Dream"
	}
	if c.Language != "zh-CN" && c.Language != "en-US" {
		c.Language = "zh-CN"
	}
	if c.Plugins == nil {
		c.Plugins = map[string]any{"eyesControl": false}
	}
	return c
}

func BuildHubExpression(config HubConfig) (string, error) {
	config = config.Normalize()
	data, err := json.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("encode 2Ag hub config: %w", err)
	}
	return strings.Replace(hubSource, "__2AG_INITIAL_CONFIG__", string(data), 1), nil
}
