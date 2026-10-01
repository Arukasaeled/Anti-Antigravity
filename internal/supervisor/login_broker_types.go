package supervisor

// ============================================================================
// Official Antigravity Login Broker —— 类型与阶段常量（与平台无关）
//
// 为什么要 broker，而不是 2Ag 自己走一遍 OAuth：
//
//	2Ag 一旦自己发起 Google OAuth，它就必然要持有 client_id / client_secret，
//	而「一个本机工具持有 Google OAuth 客户端凭据」既不该发布、也不该存在 ——
//	用户会因此把自己的账号授权给一个他并不认识的应用。正确的位置是：
//	**官方 Antigravity 做它本来就在做的原生登录，2Ag 只捕获结果。**
//
// 流程（每一步都在 LoginBrokerStatus.Stage 里如实体现）：
//
//	backing_up  备份当前 gemini:antigravity（并归档进 DPAPI 保险库）
//	stopping    停掉运行中的 Antigravity（凭据与单实例锁都要释放）
//	 launching_official  以零参数启动官方 Antigravity（不注入、不改文件）
//	waiting_login       轮询凭据 + 只读探测官方窗口是否停在 /onboarding?login=true
//	detected            凭据成为一个**完整**的登录结果（不是 {"token":null} 中间态）
//	saving              加密写入 Account Vault 并更新 index.json
//	restoring           恢复流程开始前的原账号（原本没有账号则保留新账号）
//	done / failed / cancelled
// ============================================================================

const (
	brokerStageIdle      = "idle"
	brokerStageBackingUp = "backing_up"
	brokerStageStopping  = "stopping"
	brokerStageLaunching = "launching_official"
	brokerStageWaiting   = "waiting_login"
	brokerStageDetected  = "detected"
	brokerStageSaving    = "saving"
	brokerStageRestoring = "restoring"
	brokerStageDone      = "done"
	brokerStageFailed    = "failed"
	brokerStageCancelled = "cancelled"
)

// LoginBrokerStatus 是「添加账号」流程的完整对外状态。
//
// 刻意把阶段、已完成步骤、以及原始账号/当前账号都报出来：添加账号会短暂地改动
// 机器上唯一那份登录凭据，用户必须能一眼看到「现在到哪一步了」「我的原账号回来没有」。
type LoginBrokerStatus struct {
	Running  bool   `json:"running"`
	Stage    string `json:"stage"`
	Message  string `json:"message"`
	Steps    []string `json:"steps"`

	AccountEmail  string `json:"account_email,omitempty"`  // 本次添加到的账号
	OriginalEmail string `json:"original_email,omitempty"` // 流程开始前的账号（可能为空）
	CurrentEmail  string `json:"current_email,omitempty"`  // 流程结束时的实际登录身份
	OriginalMode  string `json:"original_mode,omitempty"`  // 流程开始前的运行形态

	VaultSaved    bool `json:"vault_saved"`
	Restored      bool `json:"restored"`
	HostRestarted bool `json:"host_restarted"`

	FailureReason string `json:"failure_reason,omitempty"`
	StartedAt     string `json:"started_at,omitempty"`
	UpdatedAt     string `json:"updated_at,omitempty"`
	FinishedAt    string `json:"finished_at,omitempty"`
}

// AccountSwitchResult 是账号切换（事务化）的真实回执。
//
// Verified 与 RollbackVerified 必须分开报：它们回答的是两个不同的问题 ——
// 「切过去了吗」和「没切过去的话，原来的账号回来了吗」。只报前者，用户就无法
// 判断自己现在到底在哪个账号上。
type AccountSwitchResult struct {
	Email            string `json:"email"`
	PreviousOwner    string `json:"previous_owner,omitempty"`
	Mode             string `json:"mode,omitempty"`
	HostRestarted    bool   `json:"host_restarted"`
	VerifiedOwner    string `json:"verified_owner,omitempty"`
	Verified         bool   `json:"verified"`
	RolledBack       bool   `json:"rolled_back"`
	RollbackVerified bool   `json:"rollback_verified"`
	Message          string `json:"message"`
}
