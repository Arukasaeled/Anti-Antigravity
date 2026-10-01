package core

import (
	"fmt"
	"sync"
	"time"

	"github.com/2ag/2ag/internal/config"
)

type ActionType string

const (
	SetThemeAction           ActionType = "SET_THEME"
	SetLanguageAction        ActionType = "SET_LANGUAGE"
	TogglePluginAction       ActionType = "TOGGLE_PLUGIN"
	UpdatePluginConfigAction ActionType = "UPDATE_PLUGIN_CONFIG"
	SetPresetAction          ActionType = "SET_PRESET"
	HostCtrlAction           ActionType = "HOST_CTRL"
	OpenDevtoolsAction       ActionType = "OPEN_DEVTOOLS"
	HotReloadAction          ActionType = "HOT_RELOAD"
	ExportConfigAction       ActionType = "EXPORT_CONFIG"
	ToggleBoostAction        ActionType = "TOGGLE_BOOST"
	SetRuntimeModeAction     ActionType = "SET_RUNTIME_MODE"
)

type StateAction struct {
	Type    ActionType `json:"type"`
	Payload any        `json:"payload"` // Payload structure depends on ActionType
}

type StateMachine struct {
	mu            sync.RWMutex
	state         config.Config
	configPath    string
	bus           *EventBus
	debounceTimer *time.Timer
	saveMu        sync.Mutex
}

func NewStateMachine(cfg config.Config, configPath string, bus *EventBus) *StateMachine {
	return &StateMachine{
		state:      cfg,
		configPath: configPath,
		bus:        bus,
	}
}

func (sm *StateMachine) GetState() config.Config {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.state
}

func (sm *StateMachine) ApplyAction(action StateAction) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	changed := false
	switch action.Type {
	case SetThemeAction:
		payload, ok := action.Payload.(map[string]any)
		if !ok {
			return fmt.Errorf("invalid payload for SET_THEME")
		}
		if wp, ok := payload["wallpaper_path"].(string); ok {
			sm.state.WallpaperPath = wp
			changed = true
		}
		if blur, ok := payload["blur"].(float64); ok { // JSON decodes numbers to float64
			sm.state.Blur = int(blur)
			changed = true
		}
		if opacity, ok := payload["opacity"].(float64); ok {
			sm.state.Opacity = opacity
			changed = true
		}
		if modalOpacity, ok := payload["modal_opacity"].(float64); ok {
			sm.state.ModalOpacity = modalOpacity
			changed = true
		}
	case SetLanguageAction:
		payload, ok := action.Payload.(map[string]any)
		if !ok {
			return fmt.Errorf("invalid payload for SET_LANGUAGE")
		}
		if lang, ok := payload["language"].(string); ok {
			sm.state.Language = lang
			changed = true
		}
	case TogglePluginAction:
		payload, ok := action.Payload.(map[string]any)
		if !ok {
			return fmt.Errorf("invalid payload for TOGGLE_PLUGIN")
		}
		pluginName, ok := payload["name"].(string)
		enabled, eok := payload["enabled"].(bool)
		if ok && eok {
			for i, p := range sm.state.Plugins {
				if p.Name == pluginName {
					sm.state.Plugins[i].Enabled = enabled
					changed = true
					break
				}
			}
		}
	case UpdatePluginConfigAction:
		payload, ok := action.Payload.(map[string]any)
		if !ok {
			return fmt.Errorf("invalid payload for UPDATE_PLUGIN_CONFIG")
		}
		pluginName, _ := payload["name"].(string)
		key, _ := payload["key"].(string)
		val := payload["value"]
		for i, p := range sm.state.Plugins {
			if p.Name == pluginName {
				if sm.state.Plugins[i].Config == nil {
					sm.state.Plugins[i].Config = make(map[string]any)
				}
				sm.state.Plugins[i].Config[key] = val
				changed = true
				break
			}
		}
	case SetPresetAction:
		payload, ok := action.Payload.(map[string]any)
		if !ok {
			return fmt.Errorf("invalid payload for SET_PRESET")
		}
		presetName, _ := payload["preset"].(string)
		switch presetName {
		case "pure_dark":
			sm.state.Blur = 10
			sm.state.Opacity = 0.90
			sm.state.ModalOpacity = 0.95
		case "cyberpunk":
			sm.state.Blur = 15
			sm.state.Opacity = 0.70
			sm.state.ModalOpacity = 0.90
		case "frosted":
			sm.state.Blur = 30
			sm.state.Opacity = 0.40
			sm.state.ModalOpacity = 0.80
		case "native":
			// 恢复默认外观 = 关掉壁纸与模糊，只留宿主原生底色。
			// 注意 Opacity 必须留在 Validate 允许的 [0.1, 0.9] 区间内：
			// 历史实现写 0.0，而 schedulePersistLocked 里 config.Save 会先做
			// Validate，于是这次变更在 300ms 后必然保存失败且错误被 `_ =` 丢弃 ——
			// 界面上「恢复默认外观」永远提示成功，磁盘上却一个字都没落下。
			sm.state.Blur = 0
			sm.state.Opacity = 0.1
			sm.state.ModalOpacity = 1.0
		case "electric":
			// Gemini Dusk：前端主题库第四张卡（index.html 的 applyPreset('electric')）
			// 历史上在这里没有对应分支，于是点击后 changed=true、状态却没变，
			// 界面照样打「应用主题预设: electric」，用户看到的是一次静默空操作。
			sm.state.Blur = 24
			sm.state.Opacity = 0.65
			sm.state.ModalOpacity = 0.88
		case "spectrum":
			// 2Ag Spectrum：产品自己的签名主题。
			// 底色仍是中性暗面（不能被品牌色染成彩虹），四色只以极低不透明度的
			// ambient glow 参与，主调偏蓝、次调偏绿。透明度比 frosted 更暗一些，
			// 因为它的看点在那层光，不在壁纸本身。
			sm.state.Blur = 18
			sm.state.Opacity = 0.78
			sm.state.ModalOpacity = 0.92
		default:
			// 未知预设必须显式失败，而不是假装成功 —— 否则前端永远无法区分
			// 「已应用」与「后端根本没这个预设」。
			return fmt.Errorf("unknown preset: %s", presetName)
		}
		changed = true
	case ToggleBoostAction:
		payload, ok := action.Payload.(map[string]any)
		if !ok {
			return fmt.Errorf("invalid payload for TOGGLE_BOOST")
		}
		key, _ := payload["key"].(string)
		val, _ := payload["value"].(bool)
		switch key {
		case "session_delete": sm.state.GravityBoost.SessionDelete = val
		case "markdown_export": sm.state.GravityBoost.MarkdownExport = val
		case "paste_plaintext_fix": sm.state.GravityBoost.PastePlaintextFix = val
		case "session_id_tag": sm.state.GravityBoost.SessionIdTag = val
		case "centered_width": sm.state.GravityBoost.CenteredWidth = val
		case "preserve_scroll": sm.state.GravityBoost.PreserveScroll = val
		case "force_zh_cn": sm.state.GravityBoost.ForceZhCn = val
		case "enable_devtools": sm.state.GravityBoost.EnableDevtools = val
		case "disable_auto_update": sm.state.GravityBoost.DisableAutoUpdate = val
		}
		changed = true
	case SetRuntimeModeAction:
		payload, ok := action.Payload.(map[string]any)
		if !ok {
			return fmt.Errorf("invalid payload for SET_RUNTIME_MODE")
		}
		mode, _ := payload["mode"].(string)
		if mode != config.RuntimeModeEnhanced && mode != config.RuntimeModeOfficial {
			// 与 SET_PRESET 同样的立场：未知取值必须显式失败。
			// 运行形态决定「是否向宿主动手」，静默吞掉一个拼错的 mode
			// 会让用户以为自己切到了官方形态，实际仍带着注入在跑。
			return fmt.Errorf("unknown runtime mode: %s", mode)
		}
		// 只有真变化才算 changed —— 否则重复点同一个模式也会触发一次
		// 落盘与后续的宿主重启，把一次点击变成一次无谓的工作台中断。
		if sm.state.RuntimeMode != mode {
			sm.state.RuntimeMode = mode
			changed = true
		}
	case HostCtrlAction, OpenDevtoolsAction, HotReloadAction, ExportConfigAction:
		// Dispatch as CommandEvent, do not mutate state
		sm.bus.Publish(Event{
			Type:    CommandEvent,
			Payload: action,
		})
	default:
		return fmt.Errorf("unknown action type: %s", action.Type)
	}

	if changed {
		stateCopy := sm.state
		sm.bus.Publish(Event{
			Type:    StateChangedEvent,
			Payload: stateCopy,
		})
		sm.schedulePersistLocked()
	}

	return nil
}

func (sm *StateMachine) schedulePersistLocked() {
	if sm.debounceTimer != nil {
		sm.debounceTimer.Stop()
	}
	sm.debounceTimer = time.AfterFunc(300*time.Millisecond, func() {
		sm.saveMu.Lock()
		defer sm.saveMu.Unlock()
		
		sm.mu.RLock()
		stateCopy := sm.state
		sm.mu.RUnlock()

		_ = config.Save(sm.configPath, stateCopy)
	})
}
