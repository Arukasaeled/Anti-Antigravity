package supervisor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// QuotaWindow defines the 5-hour rolling window and weekly limit
type QuotaWindow struct {
	FiveHourResetAt string `json:"five_hour_reset_at"`
	WeeklyResetAt   string `json:"weekly_reset_at"`
	FiveHourPercent int    `json:"five_hour_percent"` // 5小时滑窗剩余 (0-100)
	FiveHourReset   string `json:"five_hour_reset"`   // 重置时间 (例如 "2h 20m")
	WeeklyPercent   int    `json:"weekly_percent"`    // 周配额剩余 (0-100)
	WeeklyReset     string `json:"weekly_reset"`      // 重置时间 (例如 "3d 3h")
	// FiveHourKnown / WeeklyKnown 表示「该桶本池真的解析到了」。
	//
	// 为什么不能靠 Percent 是否为 0 来判断：remainingFraction 为 0 是一个合法的
	// 真实读数（额度耗尽），与「缓存里根本没有这个桶」是两件完全不同的事。
	// 前端要按「主条与副文本必须来自同一个桶」渲染，就必须能区分这两者 ——
	// 否则一个只有 Gemini 分组的账号会在 Claude 池上画出「0% 剩余 · 待刷新」，
	// 看起来像真实读数，其实是拼凑。
	FiveHourKnown bool `json:"five_hour_known"`
	WeeklyKnown   bool `json:"weekly_known"`
	// Available 表示本配额池是否真的从授权缓存里读到了数据。
	// 历史实现在读不到缓存时编造一组"看起来合理"的百分比与重置时间，
	// 让界面无法区分「真实读数」与「凭空捏造」。现在如实标记，由界面显示"未载入"。
	Available bool `json:"available"`
	// UpdatedAt / Source 是这份读数的出处与新鲜度，直接取自授权缓存文件顶部的字段。
	//
	// 为什么必须带出来：百分比本身看不出「这是不是 9 小时前的快照」。配额缓存由第三方
	// 工具 antigravity_cockpit 写入、本工程只读，两者的节奏毫无关系 —— 一个陈旧的 100%
	// 与一个刚刚采样的 100% 在界面上长得一模一样，用户没有判断依据。这里原样给出毫秒
	// 时间戳与来源标识，由界面自行折算「多久以前」；后端不生成任何相对时间文案，
	// 因为一旦后端开始说「几分钟前」，就会把「采样时刻」悄悄变成「生成时刻」。
	UpdatedAt int64  `json:"updated_at"` // 授权缓存 updatedAt（Unix 毫秒）；0 = 未知
	Source    string `json:"source"`     // 例 "authorized · desktop"；空 = 未知
}

// AccountInstance defines account entity with dual quota pools
type AccountInstance struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	IsPrimary bool   `json:"is_primary"`
	IsActive  bool   `json:"is_active"`
	// Status 取值 "ACTIVE"（当前主控）/ "STANDBY"（在线备选）/ "OFFLINE"（未载入）。
	// 历史实现还会出现 "COOLDOWN"：那是把每个非主账号无条件标成「429 冷却中」的产物，
	// 而本工程从未向任何官方端点发起过握手去观测 429 —— 属于凭空判定，已移除。
	Status      string      `json:"status"`
	Weight      int         `json:"weight"`
	GeminiPool  QuotaWindow `json:"gemini_pool"`
	ClaudePool  QuotaWindow `json:"claude_pool"`
	Models      []string    `json:"models"` // 支持模型列表
	CooldownMsg string      `json:"cooldown_msg,omitempty"`
}

// LocalAccount is maintained for backward compatibility
type LocalAccount = AccountInstance

type cockpitAccountsFile struct {
	Version          string `json:"version"`
	CurrentAccountID string `json:"current_account_id"`
	Accounts         []struct {
		ID        string `json:"id"`
		Email     string `json:"email"`
		Name      string `json:"name"`
		CreatedAt int64  `json:"created_at"`
		LastUsed  int64  `json:"last_used"`
	} `json:"accounts"`
}

type quotaSummaryBucket struct {
	BucketID          string  `json:"bucketId"`
	DisplayName       string  `json:"displayName"`
	RemainingFraction float64 `json:"remainingFraction"`
	ResetTime         string  `json:"resetTime"`
	Window            string  `json:"window"`
}

type quotaSummaryGroup struct {
	DisplayName string               `json:"displayName"`
	Description string               `json:"description"`
	Buckets     []quotaSummaryBucket `json:"buckets"`
}

type quotaCacheFile struct {
	Email string `json:"email"`
	// UpdatedAt 是这份快照的采样时刻（Unix 毫秒），由写入方 antigravity_cockpit 打在文件顶部。
	// Source / CustomSource 是它的出处标识（实测为 "authorized" / "desktop"）。
	// 三者都要原样带出来交给界面 —— 只显示百分比而不显示「什么时候采的」，
	// 用户无法分辨一个 9 小时前的旧快照与一个刚刚刷新的读数。
	UpdatedAt    int64  `json:"updatedAt"`
	Source       string `json:"source"`
	CustomSource string `json:"customSource"`
	Payload      struct {
		QuotaSummary struct {
			Groups []quotaSummaryGroup `json:"groups"`
		} `json:"quota_summary"`
	} `json:"payload"`
}

// quotaSourceLabel 把缓存顶部的 source / customSource 拼成一个可读的出处串。
// 两个字段都缺失时返回空串，调用方据此显示「出处未知」，不编造一个像样的名字。
func quotaSourceLabel(source, customSource string) string {
	source = strings.TrimSpace(source)
	customSource = strings.TrimSpace(customSource)
	switch {
	case source != "" && customSource != "":
		return source + " · " + customSource
	case source != "":
		return source
	default:
		return customSource
	}
}

// formatResetTime 把授权缓存里的 resetTime 折算成人类可读的倒计时。
//
// 满额（remainingFraction >= 0.999）时返回「已恢复满额」而不是旧的「满额」：
// 调用方把它拼进形如「5h 滑窗 · {reset} 后刷新」的句子，旧字面量会得到
// 「5h 滑窗 · 满额 后刷新」这种读不通的串 —— 那是前端把「桶已满」与「倒计时」
// 当成同一件事的后果。这里给出一句本身就能独立成立的话，
// 让上层可以直接显示，也可以据此判断「没有倒计时可报」。
func formatResetTime(resetStr string, fraction float64, fallback string) string {
	if fraction >= 0.999 {
		return "已恢复满额"
	}
	if resetStr != "" {
		t, err := time.Parse(time.RFC3339, resetStr)
		if err == nil {
			dur := time.Until(t)
			if dur > 24*time.Hour {
				days := int(dur / (24 * time.Hour))
				hours := int((dur % (24 * time.Hour)) / time.Hour)
				return fmt.Sprintf("%dd %dh", days, hours)
			}
			if dur > 0 {
				hours := int(dur / time.Hour)
				mins := int((dur % time.Hour) / time.Minute)
				return fmt.Sprintf("%dh %dm", hours, mins)
			}
		}
	}
	return fallback
}

// quotaGroupModelMarker 是授权缓存里每个模型组自带的一行说明的前缀。
// 真实样本：`Models within this group: Claude Opus, Claude Sonnet, GPT-OSS`
const quotaGroupModelMarker = "Models within this group:"

// parseModelsFromGroupDescription 从模型组描述里抽出该组真实覆盖的模型名。
// 抽不到就返回 nil —— 调用方据此显示「未探测」，不做任何兜底编造。
func parseModelsFromGroupDescription(desc string) []string {
	idx := strings.Index(desc, quotaGroupModelMarker)
	if idx < 0 {
		return nil
	}
	rest := desc[idx+len(quotaGroupModelMarker):]
	var out []string
	for _, part := range strings.Split(rest, ",") {
		if name := strings.TrimSpace(part); name != "" {
			out = append(out, name)
		}
	}
	return out
}

// cockpitCacheDir 返回第三方 cockpit 工具的配额缓存目录。
//
// 解析顺序与 cockpitDataDir() 保持一致（先环境变量、再当前用户主目录），
// 只是多下钻一层到配额缓存：
//
//	$COCKPIT_TOOLS_DATA_DIR/cache/quota_api_v1_desktop/authorized
//	~/.antigravity_cockpit/cache/quota_api_v1_desktop/authorized
//
// 为什么要抽成函数：历史实现在两处各写死了同一条绝对路径（指向开发机上的
// 第三方工具目录）。那两行**只对一台机器成立** —— 别的用户装完 2Ag 之后，
// 配额永远读不到，界面永远显示「未载入」，而代码里看不出哪里错了。
// 现在路径只在这里拼一次，且是以当前用户为基准的。
func cockpitCacheDir() string {
	dir := cockpitDataDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "cache", "quota_api_v1_desktop", "authorized")
}

// queryCacheFor 扫描本地授权配额缓存，一次性取出该账号的真实双配额池与模型清单。
//
// 返回 ok=false 表示「本地根本没有这个账号的授权缓存」——调用方必须如实呈现
// 「未载入」，绝不能回填任何看起来合理的数字。
//
// 历史实现的两处伪造都发生在这个函数的调用方与尾部：
//  1. 缓存未命中时按邮箱分支编造两套漂亮百分比（按账号分成两档，与真实读数无关）；
//  2. 模型清单写死为 SupportedOfficialModels（"Gemini 3.8 Flash High" 等 5 个名字）。
//
// 那 5 个名字既不来自任何真实来源，也不随账号变化 —— 而缓存文件里每个组本来就带着
// "Models within this group: ..."，那才是账号卡片「支持模型」胶囊的唯一真实出处。
func queryCacheFor(email string) (geminiPool QuotaWindow, claudePool QuotaWindow, models []string, ok bool) {
	// 缓存目录解析统一走 cockpitCacheDir()：它认 $COCKPIT_TOOLS_DATA_DIR，
	// 否则落到当前用户的 ~/.antigravity_cockpit。
	// 历史实现这里写死了两个绝对路径（开发机上的第三方工具目录），
	// 后果是**任何别的用户都读不到配额** —— 那两行只对一台机器成立。
	cacheDirs := []string{}
	if cdir := cockpitCacheDir(); cdir != "" {
		cacheDirs = append(cacheDirs, cdir)
	}

	seenModel := map[string]bool{}

	for _, cdir := range cacheDirs {
		matches, err := filepath.Glob(filepath.Join(cdir, "*.json"))
		if err != nil {
			continue
		}
		for _, m := range matches {
			data, err := os.ReadFile(m)
			if err != nil {
				continue
			}
			var qcf quotaCacheFile
			if err := json.Unmarshal(data, &qcf); err == nil {
				if strings.EqualFold(qcf.Email, email) {
					for _, grp := range qcf.Payload.QuotaSummary.Groups {
						isGemini := strings.Contains(grp.DisplayName, "Gemini")
						isClaude := strings.Contains(grp.DisplayName, "Claude") || strings.Contains(grp.DisplayName, "GPT")

						for _, name := range parseModelsFromGroupDescription(grp.Description) {
							if !seenModel[name] {
								seenModel[name] = true
								models = append(models, name)
							}
						}

						for _, b := range grp.Buckets {
							pct := int(b.RemainingFraction * 100)
							if pct > 100 {
								pct = 100
							}
							bucketed := b.Window == "5h" || b.Window == "weekly"

							if isGemini {
								if bucketed {
									geminiPool.Available = true
								}
								if b.Window == "5h" {
									geminiPool.FiveHourPercent = pct
									geminiPool.FiveHourReset = formatResetTime(b.ResetTime, b.RemainingFraction, "待刷新")
									geminiPool.FiveHourKnown = true
									geminiPool.FiveHourResetAt = b.ResetTime
								} else if b.Window == "weekly" {
									geminiPool.WeeklyPercent = pct
									geminiPool.WeeklyReset = formatResetTime(b.ResetTime, b.RemainingFraction, "待刷新")
									geminiPool.WeeklyKnown = true
									geminiPool.WeeklyResetAt = b.ResetTime
								}
							} else if isClaude {
								if bucketed {
									claudePool.Available = true
								}
								if b.Window == "5h" {
									claudePool.FiveHourPercent = pct
									claudePool.FiveHourReset = formatResetTime(b.ResetTime, b.RemainingFraction, "待刷新")
									claudePool.FiveHourKnown = true
									claudePool.FiveHourResetAt = b.ResetTime
								} else if b.Window == "weekly" {
									claudePool.WeeklyPercent = pct
									claudePool.WeeklyReset = formatResetTime(b.ResetTime, b.RemainingFraction, "待刷新")
									claudePool.WeeklyKnown = true
									claudePool.WeeklyResetAt = b.ResetTime
								}
							}
						}
					}
					// Available 按池分别标记：只有真的解析到该池的 5h / weekly 桶才算可用。
					// 历史上无条件把两个池都标成可用，于是「只有 Gemini 分组」的账号
					// 在界面上也会显示一个 0% 的 Claude 池，看起来像「额度耗尽的真实读数」。
					// 新鲜度与出处来自缓存文件顶部，而不是按池分别采样 —— 一个文件只可能
					// 有一次采样时刻。两个池都带上同一组值，界面因此能对整张账号卡片
					// 说「这份数据是 X 时刻采的、来自 Y」，而不会出现两个池自称不同时间。
					source := quotaSourceLabel(qcf.Source, qcf.CustomSource)
					if geminiPool.Available {
						geminiPool.UpdatedAt = qcf.UpdatedAt
						geminiPool.Source = source
					}
					if claudePool.Available {
						claudePool.UpdatedAt = qcf.UpdatedAt
						claudePool.Source = source
					}
					recordQuotaHistory(email, geminiPool, claudePool)
					return geminiPool, claudePool, models, true
				}
			}
		}
	}

	// 未命中任何授权缓存：如实返回「无数据」。
	//
	// 历史实现在这里按邮箱分支编造两组漂亮数字（按账号分成两档），
	// 界面上完全看不出这些数值与真实配额毫无关系。可用性由 Available 明确标记，
	// 由前端渲染为「未载入」，绝不冒充真实读数。
	return QuotaWindow{Available: false}, QuotaWindow{Available: false}, nil, false
}

// QueryDualPools 返回指定账号的真实双配额池（薄封装，兼容既有调用方）。
func QueryDualPools(email string) (geminiPool QuotaWindow, claudePool QuotaWindow) {
	geminiPool, claudePool, _, _ = queryCacheFor(email)
	return geminiPool, claudePool
}

// ScanLocalAccounts scans cockpit-tools and Antigravity profiles for real local accounts
func ScanLocalAccounts() []AccountInstance {
	// 同样走 cockpitDataDir()/cockpitCacheDir() 的统一解析：
	// 历史实现写死了开发机的第三方工具路径，别的用户拿不到账号清单。
	paths := []string{}
	if dir := cockpitDataDir(); dir != "" {
		paths = append(paths, filepath.Join(dir, "accounts.json"))
	}
	if appData := os.Getenv("APPDATA"); appData != "" {
		paths = append(paths, filepath.Join(appData, "antigravity_cockpit", "accounts.json"))
	}

	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err == nil {
			var caf cockpitAccountsFile
			if err := json.Unmarshal(data, &caf); err == nil && len(caf.Accounts) > 0 {
				var result []AccountInstance
				for _, a := range caf.Accounts {
					isPrimary := (a.ID == caf.CurrentAccountID && caf.CurrentAccountID != "")
					role := "BACKUP"
					status := "STANDBY"
					weight := 5
					cooldown := ""

					if isPrimary {
						role = "PRIMARY"
						status = "ACTIVE"
						weight = 10
					} else {
						// 历史实现在此把每个非主账号无条件标记为「429 冷却中」，
						// 而这里从未向任何接口发起过握手 —— 纯属凭空判定。
						// 未握手的账号就是「离线 / 未载入」，不是限流。
						status = "OFFLINE"
						cooldown = "未载入配额 (离线)"
					}

					gemPool, claudePool, models, cacheOK := queryCacheFor(a.Email)
					if !cacheOK && cooldown == "" {
						// 主账号也一样：读不到授权缓存就是读不到，不能因为它是主控
						// 就假装额度正常。文案交由前端按 available 标记渲染。
						cooldown = "未载入配额"
					}

					result = append(result, AccountInstance{
						ID:          a.ID,
						Email:       a.Email,
						Name:        a.Name,
						Role:        role,
						IsPrimary:   isPrimary,
						IsActive:    isPrimary,
						Weight:      weight,
						Status:      status,
						GeminiPool:  gemPool,
						ClaudePool:  claudePool,
						Models:      models,
						CooldownMsg: cooldown,
					})
				}
				// 门禁必须是 >= 1，不能是 >= 2。
				// 历史实现写成 len(result) >= 2，于是账号文件里只有一个账号时
				// 整个重排与 return 都被跳过，函数一路落到末尾的 return []AccountInstance{} ——
				// 唯一真实存在的账号被「过滤」成空列表，界面显示「未检测到账号」。
				// 这里 result 至少有一个元素（外层已判 len(caf.Accounts) > 0），门禁恒真，
				// 保留它只是为了让「非空即继续」这一意图显式化。
				if len(result) >= 1 {
					active := GetActiveAccountEmail()
					if active != "" {
						for i := range result {
							if strings.EqualFold(result[i].Email, active) {
								result[i].IsPrimary = true
								result[i].IsActive = true
								result[i].Role = "PRIMARY"
								result[i].Status = "ACTIVE"
								// 只有真的读到了配额才清空提示。主控身份不改变
								// 「本地授权缓存里有没有这个账号的额度」这一事实。
								if result[i].GeminiPool.Available || result[i].ClaudePool.Available {
									result[i].CooldownMsg = ""
								} else if result[i].CooldownMsg == "" {
									result[i].CooldownMsg = "未载入配额"
								}
							} else {
								result[i].IsPrimary = false
								result[i].IsActive = false
								result[i].Role = "BACKUP"
								result[i].Status = "OFFLINE"
								if result[i].CooldownMsg == "" {
									result[i].CooldownMsg = "未载入配额 (离线)"
								}
							}
						}
					}
					return result
				}
			}
		}
	}

	// 扫描不到任何真实账号文件时，返回空列表。
	//
	// 历史实现在这里凭空造出两个账号（写死两个真实存在的邮箱）
	// 并配上偷来的真实 ID，让界面看起来"检测到了账号矩阵"。真实账号文件读不到时，
	// 正确行为是空 —— 界面应显示"未检测到账号"，而不是展示两个幽灵账号。
	return []AccountInstance{}
}

var (
	activeAccountMu sync.RWMutex
	// currentActiveEmail 初始为空：主控账号必须由真实来源确定
	// （宿主 CDP 探针读到的登录邮箱，或用户在界面上的显式选择）。
	// 历史实现把它预设成一个写死的邮箱，于是即使 cockpit 的
	// accounts.json 里 current_account_id 是空的，界面也会凭空指认某个账号为主控。
	currentActiveEmail = ""
)

// SetActiveAccount 设置当前激活主控账号，并同步落盘。
//
// 落盘是必需的，不是锦上添花：currentActiveEmail 只活在进程内存里，2Ag 一重启就归零，
// 而 CDP 邮箱探针（QueryHostEmailViaCDP）在宿主页面里读不到邮箱时返回空字符串
// （实测宿主 localStorage 里只有一个 2ag 自己的键，DOM 里也没有邮箱节点），
// 于是 GetActiveAccountInstance 会回落到 accounts[0] —— 界面显示的「当前主控」
// 与实际运行中的沙箱不是同一个账号，配额信息也随之张冠李戴。
func SetActiveAccount(email string) {
	if email == "" {
		return
	}
	activeAccountMu.Lock()
	currentActiveEmail = email
	activeAccountMu.Unlock()
	SetPersistedActiveAccount(email)
}

// GetActiveAccountEmail 获取当前主控账号。
// 内存为空时（典型场景：2Ag 刚重启）从磁盘恢复上次真实拉起宿主所用的账号，
// 恢复成功后回写内存，后续调用不再触碰磁盘。
func GetActiveAccountEmail() string {
	activeAccountMu.RLock()
	email := currentActiveEmail
	activeAccountMu.RUnlock()
	if email != "" {
		return email
	}
	restored := GetPersistedActiveAccount()
	if restored == "" {
		return ""
	}
	activeAccountMu.Lock()
	currentActiveEmail = restored
	activeAccountMu.Unlock()
	return restored
}

// activeAccountFile 记录「上一次真实拉起宿主时用的账号」。
// 与 managed_host.pid 同目录、同风格（纯文本单行），便于人工核对与排障。
const activeAccountFile = "active_account.txt"

// SetPersistedActiveAccount 把主控账号落盘。
// 这是「换号后重启 2Ag 仍认得自己托管的是哪个沙箱」的唯一持久凭据来源。
func SetPersistedActiveAccount(email string) {
	email = strings.TrimSpace(email)
	if email == "" {
		return
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return
	}
	stateDir := filepath.Join(userHome, ".2ag")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(stateDir, activeAccountFile), []byte(email), 0644)
}

// GetPersistedActiveAccount 读取上次落盘的主控账号；无记录或读失败时返回空字符串。
func GetPersistedActiveAccount() string {
	userHome, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(userHome, ".2ag", activeAccountFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// GetActiveAccountInstance 返回当前被设为主控的账号实例及其真实双配额池
func GetActiveAccountInstance() AccountInstance {
	activeEmail := GetActiveAccountEmail()
	accounts := ScanLocalAccounts()
	for _, acc := range accounts {
		if strings.EqualFold(acc.Email, activeEmail) {
			return acc
		}
	}
	// 有明确主控、但账号文件里查不到它（账号被移除，或 accounts.json 被改写）：
	// 如实返回这个邮箱的真实配额，但绝不宣称 ACTIVE / PRIMARY。
	//
	// 历史实现在这里无条件写 Role:"PRIMARY" + IsPrimary:true + IsActive:true + Status:"ACTIVE"，
	// 于是任意一个字符串（包括空字符串、或者早已从账号文件里删掉的幽灵邮箱）都会被界面
	// 渲染成「主控活跃」，并配上它那必然是空的配额 —— 默认值伪装成真实状态。
	// 查不到就是未载入：Role 只能是 BACKUP、Status 只能是 OFFLINE，
	// 配额照实给出（读不到就是 available=false，由前端按 available 渲染「--」）。
	if activeEmail != "" {
		g, c, models, _ := queryCacheFor(activeEmail)
		return AccountInstance{
			Email:      activeEmail,
			Role:       "BACKUP",
			IsPrimary:  false,
			IsActive:   false,
			Status:     "OFFLINE",
			GeminiPool: g,
			ClaudePool: c,
			Models:     models,
		}
	}
	if len(accounts) > 0 {
		return accounts[0]
	}
	// 账号列表为空、且没有任何主控标记：如实报告「未检测到账号」。
	// 这里绝不能回 ACTIVE —— 那会让一个空邮箱在界面上显示成活跃主控。
	return AccountInstance{
		Role:   "BACKUP",
		Status: "OFFLINE",
	}
}
