package patcher

import "testing"

// TestNormalizePreservesNativePreset 守住「恢复默认外观」（native 预设）的合法取值。
//
// 为什么需要这条测试：native 预设的物理取值就是 blur = 0 / opacity = 0.1 /
// modalOpacity = 1.0（见 internal/core/state.go 的 case "native" 分支与
// config.Validate() 允许的 [0,40] / [0.1,0.9] 区间）。任何形如
//
//	if blur <= 0 { blur = 28 }
//
// 的兜底都会把用户主动选择的 Default 静默改成「中等模糊」，界面上主题卡反推选中项
// 随即全灭 —— 因为 28/0.1 这个组合不匹配任何预设。历史实现同时在 Go 侧
// （internal/supervisor/cdp_injector.go 的 BuildHubConfig）与 JS 侧犯过这个错，
// JS 侧修了、Go 侧漏改了很久。
//
// Normalize 是补丁配置进入注入脚本前的最后一道闸门，因此把不变量钉在这里：
// 合法区间内的值必须原样通过，只有越界值才允许被回填。
func TestNormalizePreservesNativePreset(t *testing.T) {
	t.Run("native preset values survive Normalize", func(t *testing.T) {
		got := HubConfig{Blur: 0, Opacity: 0.1, ModalOpacity: 1.0}.Normalize()
		if got.Blur != 0 {
			t.Fatalf("blur = 0 是「恢复默认外观」的合法取值，Normalize 却改成了 %d —— "+
				"这会让用户选择 Default 后界面仍显示中等模糊，且主题卡反推不出选中项", got.Blur)
		}
		if got.Opacity != 0.1 {
			t.Fatalf("opacity = 0.1 是 native 预设的合法取值，Normalize 却改成了 %v", got.Opacity)
		}
		if got.ModalOpacity != 1.0 {
			t.Fatalf("modalOpacity = 1.0 是 native 预设的合法取值，Normalize 却改成了 %v", got.ModalOpacity)
		}
	})

	t.Run("upper bounds survive Normalize", func(t *testing.T) {
		// 各预设里最激进的一组（frosted：blur 30 / opacity 0.40）与区间上沿
		got := HubConfig{Blur: 40, Opacity: 0.9, ModalOpacity: 1.0}.Normalize()
		if got.Blur != 40 || got.Opacity != 0.9 {
			t.Fatalf("区间上沿是合法取值，Normalize 却改成了 blur=%d opacity=%v", got.Blur, got.Opacity)
		}
	})

	t.Run("out-of-range values are still corrected", func(t *testing.T) {
		// 阴性对照：证明 Normalize 并非简单地原样透传 —— 真正越界的值仍须被拦下，
		// 否则上面两条断言可能只是「函数什么都不做」的假通过。
		got := HubConfig{Blur: 999, Opacity: 5.0}.Normalize()
		if got.Blur == 999 {
			t.Fatal("blur = 999 超出 [0,40]，Normalize 必须拦下")
		}
		if got.Opacity == 5.0 {
			t.Fatal("opacity = 5.0 超出 [0.1,0.9]，Normalize 必须拦下")
		}
	})
}
