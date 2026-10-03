# RC4：账号身份与资格提示

## 根因与原生证据

RC3 将 `GetAuthStatus.hasValidAuth=false / ineligible` 当成账号切换的终止条件。2026-10-03 的实际官方 2.19.1 同时给出：

- `GetUserStatus` 返回目标账号，`HasAuthToken=true`。
- `GetAuthStatus` 返回地区资格提示；前端 Send 被 `isSignedIn=false` 禁用。
- 通过原生 Enter 输入路径发出的短检查实际返回公开回复 `2Ag kala check: 2`；两条回复均已完成，期间资格读数没有改变。

这证明本机该读数与任务执行能力不一致。它不能用来单独判定切号失败，也不能证明永久地区限制。没有改宿主资格返回值，没有模拟回复。

## 验收条件

`NativeAuthState.valid` 仍是 GetAuthStatus 的原始值；新增 `authenticated`，来自同一托管 LanguageServer 的 HasAuthToken 与 GetUserStatus 邮箱。身份一致、完整可续期归档、当前 access expiry 与 Host 核验仍然必要。

仅对已认证会话的 `ineligible` 采用非终止警告。身份不一致、缺凭据、过期 access、`verificationRequired`、`tosViolation` 仍失败；临时网络错误保留核验期限。账号切换成功代表身份与登录材料已核验，未运行任务时不声称任务成功。

Manager 与 G-Hub 消费同一切号回执，保留 `eligibility_warning` 和原生结果；健康面板分列身份、资格检查与发送提示。

## 精简 consumer 凭据

Cockpit 写入的原生活动 blob 可以没有 ID token。通过 refresh grant 精确匹配已有完整归档时，仅在归档副本保留原来签发的 ID；保持新 access、expiry 和原生字段。不会生成 JWT、借用其他账号身份，也不会在只读检查时改写活动凭据。不认识的 grant 仍要求重新添加账号。

Broker 备份、周期 checkpoint、启动与切号复用这个身份绑定。刷新前捕获原归档；同账号重启的恢复记录保留刚续期的 grant，防止恢复时覆盖新登录。若停止时宿主又轮换出新的 grant，先保存最终原生凭据和恢复记录，再重新验证、续期，更新启动目标及回滚凭据；没有轮换时不会让停止 flush 覆盖预检的新凭据。

## 强制发送

原生 Lexical 编辑器的 DOM 与内部编辑状态独立。直接写 `textContent` 再发合成 input 会出现文字可见但内部仍空的情况，随后 Enter 无法提交。G-Hub 的带文本强制发送改用原生 `insertText` 编辑路径，等待一帧再派发 Enter；写入失败则中止。快捷键不传文本时仍发送用户已有输入。

在增强宿主中实际通过 `window.__2ag.forceSend(text)` 发出短检查，观察到新会话的公开完整回复 `2Ag enhanced kala: 2`，完成后 Agent 为 idle，耗时约 5.3 秒。没有使用历史回复判定成功，也没有读取隐藏推理。

## 验证

2026-10-03，使用本机官方 Antigravity 2.19.1 及由它建立的增强副本；A、B 为两个实际归档账号，测试结束保留 B。

| 场景 | 实际结果 |
| --- | --- |
| 官方 B → A | 目标 Google 身份、续期、原生邮箱、Host 一致；事务 Committed，原生 valid=true |
| 官方 A → B | 身份、续期、原生邮箱、Host 一致；Committed，authenticated=true，原生 ineligible 保留警告 |
| 增强 G-Hub 可见按钮 B → A | 新 Host 原生身份为 A，Manager 回执成功；新 G-Hub 账号为 A |
| 增强 G-Hub 可见按钮 A → B | 新 Host 原生身份为 B，回执成功并含资格警告；新 G-Hub 账号为 B |
| B 实际 Agent 工作 | 官方 Enter 与增强 G-Hub 强制发送均取得新公开回复，原生资格提示同时仍为 ineligible |
| 正常退出并重开 Manager | B 未变，增强 Host 与会话保留，Vault 仍完整；新 Manager 读取真实原生 B 身份 |
| Manager 重开后续期 | 后续增强切号预检成功刷新 B 并核验 Google 身份，健康状态记录新的 access expiry / last refresh |
| Manager 画面 | 原生屏幕截图显示完整控制台、账号配额及增强运行状态，不是仅检查 DOM |
| 最终进程 | 一个 Manager、一个增强 Host 根实例；零官方根实例，无临时登录实例 |

增强双向切号完成时 Manager PID 为 40756，Host PID 为 55944；PID 仅是该次快照，不作为产品状态常量。原生资格依然警告，账号身份与实际任务回复另行核验。

必要验证：

- `go test ./internal/supervisor ./internal/api -count=1`：通过。
- `go test ./cmd/2ag ./internal/patcher -count=1`：通过；Manager 包无测试文件。
- 两个修改的注入脚本 `node --check`，`git diff --check`：通过。
- 回归覆盖认证与资格分离、错误身份、未知 grant、原生最终轮换及未轮换 flush 的恢复选择。
- 两轴只读代码复审发现的三处凭据备份／轮换问题均已修复，最终未发现剩余可行动 P1/P2。
- Go release build、Inno Setup 与发布泄漏闸门通过；21 个发布文件、189 个自有源码文件，不包含官方运行时、私人凭据或本地实测日志。

本地安装包：`dist/v020-account-switch-rc4/Anti-Antigravity-Setup-x64.exe`。本机就地升级目录为 `D:\2ag-final`；安装不覆盖已有配置或冻结宿主。安装后再次核对二进制及注入脚本哈希。

最终安装退出码 0；安装后二进制与 staging 的 SHA-256 一致：`486207701D5E876602FDABFD67BD30D66865268F67A094E9B3A3696EE83024AE`。最终安装包为 9,245,023 字节，SHA-256：`EB71D98EF9930A4AED7CA3868C4F2E3DF9484E42C9DE559A1E20E47F23D863CA`。

最后一次 Manager 冷启动 PID 55364，Host 保留 PID 55944，仍为 B；原生 authenticated、完整 Vault、refresh credential 均为真，ineligible 原样保留。没有用 active_account 文件代替原生身份。记录的 last refresh 为 16:25:41，access expiry 为当天 09:25:32 UTC，不输出 token。

## 真实限制

- 没有解释原生资格服务为何与本机任务行为不同，也没有修改其返回值。这次两个账号的结果不能保证所有账号、地区或模型可用。
- 切号的成功回执证明原生身份与完整登录材料一致；不自动调用模型替用户验证每次任务。
- 未知的精简 refresh grant 不借用旧 ID，需要重新添加；归档完整校验保留。
- 本轮未重新进行真人 OAuth、撤销授权或人工制造地区失败；历史回滚和可控边界测试不替代这些现场验证。
- Live Trace 沿用已实现能力，本轮不新增采集范围或重做 5000/10000 事件压测，不展示隐藏 CoT。
- 本地安装包已生成；源码 push 与 GitHub Release 上传是不同操作，本轮不自动发布 Release。
