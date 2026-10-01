package supervisor

import (
	"testing"

	"github.com/2ag/2ag/internal/config"
)

// TestHubConfigFromConfigPreservesNativePreset 守住「恢复默认外观」（native 预设）
// 的合法取值不被注入路径悄悄改写。
//
// 缺陷形态（真机实测）：用户在舱内点 Default 后，磁盘配置确实写成
// blur = 0 / opacity = 0.1 / modal_opacity = 1（internal/core/state.go 的 case "native"），
// 但 BuildHubConfig 历史上带一句 `if blur <= 0 { blur = 28 }`，把 0 改成 28 再交给补丁。
// 后果不是「模糊度差一点」，而是主题卡反推选中项时全灭 —— 因为 28/0.1 这个组合
// 不匹配 THEME_PRESETS 里的任何一项，面板会显示「当前没有任何主题被选中」，
// 而用户明明选了 Default。
//
// 这条测试盯的是 hubConfigFromConfig 而不是 patcher.HubConfig.Normalize：
// 后者只管越界回填（区间是 [0,40]），从来不会把 0 改掉，所以拿它写测试会假通过 ——
// 真正吞掉 native 取值的是本函数里的兜底。
func TestHubConfigFromConfigPreservesNativePreset(t *testing.T) {
	t.Run("native preset reaches the patch untouched", func(t *testing.T) {
		got := hubConfigFromConfig(config.Config{
			Blur:         0,
			Opacity:      0.1,
			ModalOpacity: 1.0,
		})
		if got.Blur != 0 {
			t.Fatalf("blur = 0（native 预设 / 恢复默认外观）是合法取值，注入路径却改成了 %d —— "+
				"用户点 Default 后界面仍会渲染模糊，且舱内主题卡反推不出选中项", got.Blur)
		}
		if got.Opacity != 0.1 {
			t.Fatalf("opacity = 0.1（native 预设）是合法取值，注入路径却改成了 %v", got.Opacity)
		}
	})

	t.Run("other presets reach the patch untouched", func(t *testing.T) {
		// 逐个核对 THEME_PRESETS 里的真实数值，任何一项被改写都会让高亮落空
		presets := []struct {
			name    string
			blur    int
			opacity float64
		}{
			{"pure_dark", 10, 0.90},
			{"cyberpunk", 15, 0.70},
			{"electric", 24, 0.65},
			{"frosted", 30, 0.40},
		}
		for _, p := range presets {
			got := hubConfigFromConfig(config.Config{Blur: p.blur, Opacity: p.opacity, ModalOpacity: 0.9})
			if got.Blur != p.blur || got.Opacity != p.opacity {
				t.Errorf("%s 预设被注入路径改写：期望 blur=%d opacity=%v，实得 blur=%d opacity=%v",
					p.name, p.blur, p.opacity, got.Blur, got.Opacity)
			}
		}
	})

	t.Run("empty config falls back to config.Default only", func(t *testing.T) {
		// config.Load 在文件缺失时返回 Default()（20 / 0.55）。这里传零值 Config 是为了
		// 说明：零值绝不代表「用户选了 native」，真正的缺省语义由 config.Default 承担，
		// 注入路径不该自造第三套默认值（历史上 28 / 0.6 就是自造的产物）。
		defaults := config.Default()
		got := hubConfigFromConfig(defaults)
		if got.Blur != defaults.Blur || got.Opacity != defaults.Opacity {
			t.Fatalf("缺省配置应原样透传，期望 blur=%d opacity=%v，实得 blur=%d opacity=%v",
				defaults.Blur, defaults.Opacity, got.Blur, got.Opacity)
		}
	})
}
