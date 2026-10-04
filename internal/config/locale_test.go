package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveLanguage(t *testing.T) {
	for _, tc := range []struct{ preference, system, want string }{
		{"auto", "zh-TW", "zh-CN"}, {"auto", "zh-CN", "zh-CN"},
		{"auto", "de-DE", "en-US"}, {"auto", "", "en-US"},
		{"en-US", "zh-CN", "en-US"}, {"zh-CN", "en-US", "zh-CN"},
	} {
		if got := ResolveLanguage(tc.preference, tc.system); got != tc.want {
			t.Errorf("%+v: got %s", tc, got)
		}
	}
}

func TestLanguageAndWelcomeMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "2ag.json")
	fresh, err := Load(path)
	if err != nil || fresh.Language != "auto" || fresh.OnboardingVersion != 0 {
		t.Fatalf("fresh config: %+v, %v", fresh, err)
	}
	for _, language := range []string{"zh-CN", "en-US"} {
		if err := os.WriteFile(path, []byte(`{"language":"`+language+`","gravity_boost":{"force_zh_cn":true}}`), 0600); err != nil {
			t.Fatal(err)
		}
		old, err := Load(path)
		if err != nil || old.Language != language || old.OnboardingVersion != 1 || !old.GravityBoost.ForceZhCn {
			t.Fatalf("existing config: %+v, %v", old, err)
		}
	}
	if err := Save(path, fresh); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil || loaded.OnboardingVersion != 0 || loaded.Language != "auto" {
		t.Fatalf("new template: %+v, %v", loaded, err)
	}
}
