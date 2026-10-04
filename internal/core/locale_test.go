package core

import (
	"path/filepath"
	"testing"

	"github.com/2ag/2ag/internal/config"
)

func TestUILanguageDoesNotChangeHostTranslationOrDispatchLifecycle(t *testing.T) {
	cfg := config.Default()
	cfg.GravityBoost.ForceZhCn = true
	bus := NewEventBus()
	commands := bus.Subscribe(CommandEvent)
	sm := NewStateMachine(cfg, filepath.Join(t.TempDir(), "2ag.json"), bus)
	for _, value := range []string{"en-US", "zh-CN", "auto"} {
		if err := sm.ApplyAction(StateAction{Type: SetLanguageAction, Payload: map[string]any{"language": value}}); err != nil {
			t.Fatal(err)
		}
		if got := sm.GetState(); got.Language != value || !got.GravityBoost.ForceZhCn {
			t.Fatalf("host translation changed: %+v", got)
		}
	}
	if err := sm.ApplyAction(StateAction{Type: SetLanguageAction, Payload: map[string]any{"language": "bad"}}); err == nil {
		t.Fatal("invalid language accepted")
	}
	if err := sm.ApplyAction(StateAction{Type: CompleteOnboardingAction}); err != nil {
		t.Fatal(err)
	}
	if err := sm.ApplyAction(StateAction{Type: SetRuntimeModeAction, Payload: map[string]any{"mode": "official"}}); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-commands:
		t.Fatalf("configuration dispatched lifecycle command: %+v", event)
	default:
	}
}
