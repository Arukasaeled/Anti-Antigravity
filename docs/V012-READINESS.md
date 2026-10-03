# v0.1.2 发布决策验证记录

日期：2026-10-02。基线：`ea472c3`。结论：最初任务要求的开发与发布决策验证已完成。原生白屏已解决，用户确认「已正常显示，可以实测」，并进一步确认 G-Hub 内切换账号真实可用。正式发布需用户明确确认后完成版本、最终安装包、SHA256 与提交。

## 原生窗口验证补正

上一轮将 DOM/CDP 验证通过等同于可真人实测，结论不充分。验证启动使用了 `Start-Process -WindowStyle Hidden`：外层 Manager 窗口后来显示，但 WebView2 子窗口仍隐藏，导致用户看到纯白客户区。

同一份候选程序、相同配置改为 `-WindowStyle Normal` 后，WebView2 子窗口可见，实际桌面像素显示完整概览与配额。此问题的修复是纠正验证启动方式，没有修改前端或清除用户数据。供用户操作的 Manager 应正常可见启动；后台辅助进程继续遵守隐藏启动要求。

补充 `scripts/verify-manager-paint.ps1`，直接采样原生客户区桌面像素，避免 DOM 健康却窗口不可见的假通过。检查须在页面加载完成后运行；截图位于本地忽略目录，不能将其误当成完整人工交互验收。

对照证据：原隐藏启动的客户区顶部白色像素占比 100%，检查失败；正常启动后原生检查通过，WebView2 子窗口可见。新增检查在 Windows PowerShell 5.1 和 PowerShell 7 中均通过；用户随后确认窗口已正常显示。当前保留可见的候选 Manager，供用户实测，不自动启动其他宿主或重新登录。

## 本轮修复

- Broker 清凭据前保存并同步 DPAPI 恢复记录；Manager/run 初始化前恢复，逐字节校验成功才删除。恢复失败保留记录并停止正常初始化。
- 跨进程凭据操作锁防止另一个 Manager/run 把正在登录的事务误判为崩溃。
- Manager「导入当前官方账号」使用真实系统凭据，校验、存 Vault、登记、刷新列表；错误返回 JSON 分类。
- AUTO/DIRECT/PROXY 只作用于此次官方宿主的子进程；不永久修改 Windows 系统代理。
- 区分凭据缺失、未登录、网络/代理、EOF/reset、token 端点、未写入、超时、身份不符与恢复失败。
- 注入入口与持续巡检拒绝官方客户端 CDP；账号切换在备份失败或宿主未停时中止，回滚先停宿主再写凭据，并如实报告校验结果。
- 修复原有测试向真实账号表写入示例数据的问题；会改系统凭据的测试改为显式 opt-in。验证脚本避免把废弃字段说明和缓存函数声明误判为生产调用，并检查 Go 退出码。

## 真机证据

| 项目 | 实际结果 |
|---|---|
| 基线恢复 | 从最新有效 DPAPI 备份恢复缺失的系统凭据；ReadHostLoginEmail 与备份账号一致；官方正常工作台可见 |
| A 一键导入 | 在原生 Manager 实际点击；Vault 与自有登记表存在，UI 刷新成功 |
| B 完整 Broker | 用户完成另一账号 Google 登录；捕获并保存新账号，恢复原账号，状态 done / vault_saved / restored，恢复记录清除 |
| C 崩溃恢复 | backup+clear 后结束实际 Manager 进程；新 Manager 启动恢复原凭据；独立 DPAPI checkpoint 证明逐字节一致，记录清除 |
| D DIRECT | 测试证明子进程移除原代理变量并设置直连参数；实际完整 Google 登录成功 |
| E PROXY | 实际官方子进程使用检测到的本机代理参数；取消流程恢复原凭据。未声称代理下完整 OAuth 成功 |
| F 两账号配额 | 两个 Vault 账号均为 live / ok；Gemini 与 Claude+GPT 的 5h、weekly 四桶均 known |
| G 两界面一致 | 当前身份在 Manager 与冻结宿主 G-Hub 均显示四桶 100% / 78% / 100% / 100%；两个页面无 pageerror |
| G-Hub 真人切号 | 用户于 2026-10-02 明确确认「g-hub 里切换账号真实可用」，作为人工交互验收证据 |
| 官方隔离 | 在 enhanced 配置下官方目标仍被 HotReload 拒绝、Watcher 跳过；真实 DOM 无 G-Hub 与 Dream-Skin |

截图保留于本地忽略目录 `dist/verification/`，包括导入、双账号矩阵、G-Hub、官方登录和最终干净工作台。凭据与 DPAPI checkpoint 均不在仓库；checkpoint 验证后已删除。

## 自动验证

- `go test ./... -count=1`、`go build ./...`、`go vet ./...` 通过。
- 最后代码修订后补跑 `scripts/verify-v012.ps1`、完整测试及 vet；脚本验证恢复、操作锁、网络模式、错误分类、CDP、配额与编译。
- 保持仓库原有 CRLF；`git -c core.whitespace=cr-at-eol diff --check` 通过。
- 独立无官方运行时目录使用 `scripts/pack.ps1 -SkipInstaller` 预检查；泄漏闸门通过，G-Hub 源码与打包副本 SHA256 相同。

## Deferred 与发布边界

- 上游代理偶发 EOF 尚未解决；PROXY 的实际路由已验证，代理下完整交互登录本轮未再执行。
- Native OAuth 替代 Broker 需另作产品决策；本轮保留 Broker。长期闲置账号 access token 到期后需官方重新登录，缓存降级继续明确标注非实时。
- 历史 Cockpit 库中的示例账号来自本轮之前，兼容层仍只读；未为清理它们修改第三方目录。
- 未提交、push、tag、创建 Release、执行 git add -A 或覆盖 v0.1.1 产物。原工作目录的脏改动保留，产品修改在独立工作树中整理；宣传视频等目录不纳入发布改动。
- 正式发布前再处理 README 版本、最终安装包、SHA256、最终泄漏闸门和 clean commit。
