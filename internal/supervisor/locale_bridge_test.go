package supervisor

import (
	"encoding/json"
	"testing"

	"github.com/2ag/2ag/internal/config"
)

func TestHubLocaleInitialAndIncrementalAgree(t *testing.T) {
	for _, language := range []string{"en-US", "zh-CN", "auto"} {
		cfg := config.Default()
		cfg.Language = language
		cfg.GravityBoost.ForceZhCn = true
		initial := hubConfigFromConfig(cfg)
		if initial.Language != cfg.UILanguage() || initial.LanguagePreference != language {
			t.Fatalf("initial locale mapping failed for %s", language)
		}
		payload, err := BuildHubStatePush(cfg, false)
		if err != nil {
			t.Fatal(err)
		}
		var state struct {
			Language   string              `json:"language"`
			Preference string              `json:"language_preference"`
			Boost      config.GravityBoost `json:"gravity_boost"`
		}
		if err := json.Unmarshal([]byte(payload), &state); err != nil {
			t.Fatal(err)
		}
		if state.Language != initial.Language || state.Preference != language || !state.Boost.ForceZhCn {
			t.Fatalf("incremental locale mapping failed for %s", language)
		}
	}
}
