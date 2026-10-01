package config

import (
	"fmt"
	"net/url"
	"strings"
)

// 壁纸的两种语义，落在一个字段上：WallpaperPath。
//
//	空串（""）  = Native —— 完全使用 Antigravity 自己的原生背景，2Ag 不画任何背景层
//	绝对路径    = Custom —— 使用用户选择的那张图
//
// 这里曾经有一个 DefaultWallpaperPath 常量，指向开发机 Pictures 目录下的某一张具体图片。
// 它在五处被当作「空值兜底」使用（config.Normalize / supervisor.ResolveWallpaperDataURL
// 的两条分支 / supervisor.hubConfigFromConfig / patcher.normalizeWallpaperPath），
// 合起来造成了一个用户可见的缺陷：点「清除壁纸」后恢复的不是原生背景，而是那张被写死的
// 图片 —— 在用户眼里就是「回到了最开始添加过的那张壁纸」。
//
// 现在这条常量被彻底删除，且空串在整条链路上都必须被当作一个**有意义的值**传递，
// 不允许任何一层把它改写成一个具体的图片路径。

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

	// RuntimeMode 决定 2Ag 拉起的是「增强宿主」还是「官方宿主」。
	//
	// 它不是一个普通的增强开关，而是运行时形态本身，所以放在顶层而不是
	// GravityBoost 里 —— GravityBoost 的每一项都只在 enhanced 形态下才有意义。
	//
	//   enhanced（默认）：冻结宿主副本 + 独立账号沙箱 + CDP 注入补丁（G-Hub）
	//   official      ：官方安装目录的 Antigravity + 官方 profile + 零注入、零改凭据
	//
	// 添加它的直接原因是 Official Compatibility Audit 查实了两处跨形态共享状态
	// （Windows 凭据管理器 gemini:antigravity、%USERPROFILE%\.gemini\antigravity 数据树），
	// 用户需要一个「随时可以安全回到官方 Antigravity」的明确出口。
	RuntimeMode string `json:"runtime_mode"`
}

// 运行形态常量。取值故意用短且稳定的字符串：它们会落进 2ag.json、
// 出现在 HTTP 响应里、也被前端当作 class 名的一部分，改一次要同时动三处。
const (
	RuntimeModeEnhanced = "enhanced"
	RuntimeModeOfficial = "official"
)

// IsOfficialMode 报告当前配置是否处于官方形态。
//
// 判定收敛到这一个函数是有意为之：注入链路、启动链路、账号链路都要问同一个问题，
// 如果各处各自写 c.RuntimeMode == "official"，那么将来加第三种形态时必然漏掉某处。
func (c Config) IsOfficialMode() bool {
	return c.RuntimeMode == RuntimeModeOfficial
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
		// 出厂状态是 Native：全新安装的 2Ag 不应该自作主张给宿主画一张背景图，
		// 更不应该画一张来自开发机 Pictures 目录的图。
		WallpaperPath: "",
		Blur:          20,
		Opacity:       0.55,
		ModalOpacity:  0.85,
		Language:      "zh-CN",
		RuntimeMode:   RuntimeModeEnhanced,
		Network:       NetworkConfig{Enabled: true},
		Privacy: PrivacyConfig{BlockedHosts: []string{
			"google-analytics.com", "www.google-analytics.com", "analytics.google.com",
			"www.googletagmanager.com", "crashlyticsreports-pa.googleapis.com",
		}, BlockBeacons: true},
		Plugins: []Plugin{},
	}
}

func (c *Config) Normalize() {
	if c.Language != "zh-CN" && c.Language != "en-US" {
		c.Language = "zh-CN"
	}
	// 老配置（Enhanced 形态存在之前写下的 2ag.json）没有 runtime_mode 字段，
	// 反序列化后是空串。此处归一为 enhanced —— 它们本来就是增强形态的配置，
	// 不回填反而会让 Validate 把用户的旧配置判为非法，界面直接白屏。
	if c.RuntimeMode != RuntimeModeOfficial {
		c.RuntimeMode = RuntimeModeEnhanced
	}
	// 此处**刻意不回填 WallpaperPath**。
	//
	// 历史实现在这里写了 `if strings.TrimSpace(c.WallpaperPath) == "" { c.WallpaperPath = defaults.WallpaperPath }`，
	// 于是「清除壁纸」写入的空串在 300ms 后落盘、再读回时已经变成了那张写死的图片：
	// 用户点 Clear，界面报成功，宿主背景却在下一跳之后回退成「最开始那张」。
	// 空串在这里是有意义的值（= Native），必须原样穿过这一层。
}

func (c Config) Validate() error {
	// WallpaperPath 允许为空串（= Native 形态：使用宿主原生背景）。
	// 非空时必须是绝对 Windows 路径 —— 相对路径的解析基准会是 2ag.exe 所在目录，
	// 而用户心里想的是「我选的那个文件」。这一条由 ValidateWallpaperFile 在
	// 入口处做完整实测，这里只拦住明显非法的形状。
	if strings.TrimSpace(c.WallpaperPath) != "" {
		p := strings.ReplaceAll(strings.TrimSpace(c.WallpaperPath), `\`, "/")
		if len(p) < 3 || p[1] != ':' || p[2] != '/' || !isLetter(p[0]) {
			return fmt.Errorf("wallpaper_path must be an absolute Windows path or empty (native)")
		}
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
	if c.RuntimeMode != RuntimeModeEnhanced && c.RuntimeMode != RuntimeModeOfficial {
		return fmt.Errorf("runtime_mode must be enhanced or official")
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
//
// Native 形态（空路径）返回空串与 nil —— 「没有壁纸」不是错误。
func (c Config) WallpaperURL() (string, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	path := strings.ReplaceAll(strings.TrimSpace(c.WallpaperPath), `\`, "/")
	if path == "" {
		return "", nil
	}
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
