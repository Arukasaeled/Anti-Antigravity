package patcher

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// hubSource 是编译期快照，仅作为「磁盘上找不到源文件」时的兜底。
//
// 注意：热重载曾经直接复用它，于是「热重载补丁」热重载的永远是构建时那份字节，
// 磁盘上后来改过的 injected_hub.js 一个字都进不去 —— 按钮点了像生效、实则无效。
// 现在真实来源改由 loadHubSource() 从磁盘读取，embed 只保留为最后一道保险。
//
//go:embed injected_hub.js
var hubSource string

//go:embed context_reader.js
var contextReaderSource string

//go:embed context_view.js
var ContextViewSource string

// ContextProbeExpression reads one live conversation without installing any hooks.
func ContextProbeExpression() string {
	_, origin := loadHubSource()
	return loadHubCompanion(origin, "context_reader.js", contextReaderSource) + "()"
}

func ContextViewScript() string {
	_, origin := loadHubSource()
	return loadHubCompanion(origin, "context_view.js", ContextViewSource)
}

func loadHubCompanion(origin, name, embedded string) string {
	if !strings.HasPrefix(origin, "embedded:") {
		if raw, err := os.ReadFile(filepath.Join(filepath.Dir(origin), name)); err == nil && len(raw) > 0 {
			return string(raw)
		}
	}
	return embedded
}

// hubSourceFileName 是补丁源文件名。
const hubSourceFileName = "injected_hub.js"

// hubSourceError 记录最近一次磁盘读取失败的原因，供 UI 侧如实展示，
// 不再静默吞掉（此前异常被上层忽略，是「假热重载」能长期存活的直接原因）。
var hubSourceError string

// HubSourceDiskCandidates 返回按优先级排列的补丁源候选路径。
//
// 覆盖三种运行形态：
//  1. 开发态：进程工作目录就在仓库根，源文件位于 internal/patcher/ 下；
//  2. 安装态：源文件随发行包落在 <2ag 根>/assets/ 下；
//  3. 打包态：源文件落在 <2ag 根>/dist/staging/assets/ 下。
func HubSourceDiskCandidates() []string {
	var candidates []string

	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(cwd, "internal", "patcher", hubSourceFileName),
			filepath.Join(cwd, "assets", hubSourceFileName),
			filepath.Join(cwd, "dist", "staging", "assets", hubSourceFileName),
		)
	}

	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(exeDir, "internal", "patcher", hubSourceFileName),
			filepath.Join(exeDir, "assets", hubSourceFileName),
			filepath.Join(exeDir, "dist", "staging", "assets", hubSourceFileName),
		)
	}

	return candidates
}

// loadHubSource 读取磁盘上最新鲜的补丁源，读取失败时退回编译期快照。
// 第二个返回值说明真实来源（磁盘绝对路径，或 "embedded:<原因>"），供 UI 展示。
func loadHubSource() (string, string) {
	for _, candidate := range HubSourceDiskCandidates() {
		data, err := os.ReadFile(candidate)
		if err != nil {
			continue
		}
		text := string(data)
		if strings.TrimSpace(text) == "" {
			hubSourceError = fmt.Sprintf("磁盘文件为空: %s", candidate)
			continue
		}
		if !strings.Contains(text, "__2AG_INITIAL_CONFIG__") {
			// 缺占位符的文件注进去只会是一段不生效的死脚本，宁可继续找下一个候选。
			hubSourceError = fmt.Sprintf("磁盘文件缺少注入占位符 __2AG_INITIAL_CONFIG__: %s", candidate)
			continue
		}
		hubSourceError = ""
		abs, absErr := filepath.Abs(candidate)
		if absErr != nil {
			abs = candidate
		}
		return text, abs
	}

	reason := "磁盘上未找到 " + hubSourceFileName
	if hubSourceError != "" {
		reason = hubSourceError
	}
	return hubSource, "embedded:" + reason
}

// HubSourceInfo 报告当前补丁源的真实来源与字节数，供诊断面板与热重载回执使用。
func HubSourceInfo() (source string, size int) {
	text, origin := loadHubSource()
	return origin, len(text)
}

// HubConfig is the small state bridge shared by Go's initial injection and the
// in-app localStorage controls. The renderer owns subsequent changes.
type HubConfig struct {
	Wallpaper     string            `json:"wallpaper"`
	LogoURL       string            `json:"logo_url"`
	WallpaperPath string            `json:"wallpaper_path"`
	Blur          int               `json:"blur"`
	Opacity       float64           `json:"opacity"`
	ModalOpacity  float64           `json:"modal_opacity"`
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
	GravityBoost  any               `json:"gravity_boost,omitempty"`
}

func (c HubConfig) Normalize() HubConfig {
	// 此处**刻意不把空的 Wallpaper 填成 WallpaperServerURL**。
	//
	// 这是壁纸链路里最后一道、也是最隐蔽的一道「空值兜底」：Normalize 被
	// BuildHubExpression 调用，而那是整段注入与增量推送共同的出口。历史实现在这里
	// 把空串改写成 http://127.0.0.1:18082/bg.jpg，于是「清除壁纸」写入的空串在
	// 抵达补丁之前又变成了一个非空字符串 —— 补丁据此认为「有壁纸」，
	// 不但不拆除背景层，还把宿主透明化 CSS 一直挂着。
	//
	// 那个 loopback 地址是 CLI 模式（main.go）专用的壁纸源，而 CLI 路径由
	// buildInjectionExpression 显式赋 initial.Wallpaper，不依赖这里的兜底。
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
		c.Plugins = make(map[string]any)
	}
	return c
}

func BuildHubExpression(config HubConfig) (string, error) {
	expression, _, err := BuildHubExpressionWithSource(config)
	return expression, err
}

// BuildHubExpressionWithSource 与 BuildHubExpression 相同，另外回报补丁源的真实来源，
// 便于热重载端点把「这次究竟注入的是磁盘最新文件还是编译期快照」如实告诉用户。
func BuildHubExpressionWithSource(config HubConfig) (string, string, error) {
	config = config.Normalize()
	data, err := json.Marshal(config)
	if err != nil {
		return "", "", fmt.Errorf("encode 2Ag hub config: %w", err)
	}
	text, origin := loadHubSource()
	if strings.TrimSpace(text) == "" {
		return "", origin, fmt.Errorf("2Ag hub 源为空（%s）", origin)
	}
	text = strings.Replace(text, "__2AG_CONTEXT_READER__", loadHubCompanion(origin, "context_reader.js", contextReaderSource), 1)
	text = strings.Replace(text, "__2AG_CONTEXT_VIEW__", loadHubCompanion(origin, "context_view.js", ContextViewSource), 1)
	return strings.Replace(text, "__2AG_INITIAL_CONFIG__", string(data), 1), origin, nil
}
