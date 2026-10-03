package supervisor

import (
	"log"
	"strings"
	"sync"
)

// 运行形态的进程内镜像。
//
// 为什么需要它，而不是每个调用点各自去读 2ag.json：
//
//	注入链路（HotReloadCDP / WatchAndInjectCDP / PushHubState / LaunchEnhancedHost）
//	分布在四个文件里，各自被不同的事件路径调用。如果让它们都去 config.Load() 判形态，
//	一是每次注入都要摸一次磁盘（而 PushHubState 在拖滑块时以 100ms 粒度被调用），
//	二是「忘记判」会成为默认失误 —— 只要有一条路径漏了，官方形态下就会照样往宿主
//	里打补丁，而这恰恰是本轮要根除的那类静默越界。
//
// 形态由 Manager 按实际宿主初始化，只有明确启动/重启动作才应用配置。
// 选择状态不更新该镜像。四个链路入口统一问 IsOfficialRuntime()。默认值是 enhanced：与 config.Normalize()
// 对老配置的回填保持一致，也保证任何「没人推过形态」的极端情况下退回历史行为。
var (
	runtimeModeMu sync.RWMutex
	runtimeMode   = "enhanced"
)

// RuntimeModeOfficialValue 与 internal/config 的常量保持字面一致。
//
// 不复用 config 包是为了避免 supervisor → config 的反向依赖被引入两次以上；
// 这两个字符串是跨进程边界（HTTP 响应、2ag.json、前端 class 名）的协议值，
// 本来就该在两侧各写一次，改的时候一起改。
const RuntimeModeOfficialValue = "official"

// SetRuntimeMode 更新当前注入/启动策略；不得由配置模式的 StateChanged 调用。
func SetRuntimeMode(mode string) {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		mode = "enhanced"
	}
	runtimeModeMu.Lock()
	prev := runtimeMode
	runtimeMode = mode
	runtimeModeMu.Unlock()
	if prev != mode {
		log.Printf("[2ag] 运行形态切换: %s → %s", prev, mode)
	}
}

// IsOfficialRuntime 报告当前是否处于官方形态。
func IsOfficialRuntime() bool {
	runtimeModeMu.RLock()
	defer runtimeModeMu.RUnlock()
	return runtimeMode == RuntimeModeOfficialValue
}
