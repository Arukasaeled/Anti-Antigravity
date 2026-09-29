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
			sm.state.Blur = 0
			sm.state.Opacity = 0.0
			sm.state.ModalOpacity = 1.0
		}
		changed = true
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
