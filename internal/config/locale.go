package config

import "strings"

func ValidLanguage(language string) bool {
	return language == "auto" || language == "en-US" || language == "zh-CN"
}

// ResolveLanguage deliberately uses the OS locale, not the injected host's
// navigator.language, which the independent force_zh_cn option can override.
func ResolveLanguage(preference, system string) string {
	if preference == "zh-CN" || preference == "en-US" {
		return preference
	}
	if strings.HasPrefix(strings.ToLower(system), "zh") {
		return "zh-CN"
	}
	return "en-US"
}

func (c Config) UILanguage() string { return ResolveLanguage(c.Language, SystemLanguage()) }
