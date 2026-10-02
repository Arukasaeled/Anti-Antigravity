# Anti-Antigravity (2Ag) v0.1.1

**首次公开发布。** Windows x64 · 单个 `2ag.exe` · 安装包 `Anti-Antigravity-Setup-x64.exe`

> **Known Issue（v0.1.1）—— 配额数据来源与「真实探测」文案不符**
>
> v0.1.1 的账号矩阵配额实际上读取的是本机 **Cockpit Tools 缓存**
> （`~/.antigravity_cockpit/cache/quota_api_v1_desktop/authorized`），
> **不是** 2Ag 自己发起的实时探测。发布说明里「真实配额探测 / 用本机凭据直接向对应端点查询」
> 这句话在 v0.1.1 **不成立**。
>
> v0.1.2（correctness / honesty patch）改为 2Ag 原生 live 探测，缓存仅作明确标注的 fallback。
> 请勿替换 v0.1.1 已发布的二进制；行为以 v0.1.1 实际代码为准。

## 发布产物

发布页附带两个文件：

| 文件 | 说明 |
| --- | --- |
| `Anti-Antigravity-Setup-x64.exe` | Windows x64 安装包 |
| `Anti-Antigravity-Setup-x64.exe.sha256` | 安装包的 SHA-256 校验值 |

校验安装包：

```powershell
Get-FileHash .\Anti-Antigravity-Setup-x64.exe -Algorithm SHA256
```

把结果与 `.sha256` 文件（也在发布页，并附在下方正文中）逐字符对照即可。

`.sha256` 的内容格式为一行：`<哈希>` + 两个空格 + `Anti-Antigravity-Setup-x64.exe`。

> **为什么安装包的哈希不在正文里写死**：安装包内部载荷经压缩 + LZMA 编码，Inno Setup 每次编译
> 都会重新压缩（并把编译时刻写进头部），所以同一份源码两次编译出的字节并不相同 —— 正文内容一致，
> 只是二进制不逐字节可复现。固定写死一个哈希必然与用户实际下载到的文件对不上，因此改为随发布页
> 提供 `.sha256` asset。

安装包里**没有任何 Google Antigravity 运行时字节**。增强形态第一次启动时，2Ag 会从
你本机的官方 Antigravity 安装复制出一份自己的冻结宿主副本（`app\`，解包后约 570 MB），
之后一直使用它 —— 见下面「安装 → 冻结宿主」。

---

## 这是什么

一个 Windows 上的 Antigravity 增强外壳。它**不修改官方 Antigravity 的安装文件**
（`app.asar` 原样保留），而是在启动官方宿主时额外拉起本机回环服务，并通过 CDP
把界面注入宿主渲染进程。宿主与 2Ag 的全部进程挂在同一条 Windows Job Object 下，
宿主退出即一并清干净。

2Ag **不包含、也不分发** Google Antigravity：官方运行时需要你自己安装，2Ag 只在
你自己的那份安装之上做增强。

## 亮点

**2Ag Manager** —— WebView2 原生窗口，六个面板：概览（宿主状态 / 模型配额 /
协议网关 / 上游兼容性）、账号、会话、视觉工坊、重力加倍、环境诊断。

**G-Hub / G-Cockpit** —— 注入官方宿主的浮标 + 战术面板 + 象限环开关。
不切窗口就能看状态、换账号、重启宿主沙箱。

**动态 CDP 端口** —— 端口不再固定。启动时自己领一个空闲端口，从根上消掉
「固定端口被占用 / 注入早于宿主准备好」的竞态（此前是 35ms 定时窗口）。

**真实配额探测** —— 按 Gemini 池与 Claude / ChatGPT 池两个官方配额池分别读数，
用本机凭据直接向对应端点查询。

**账号矩阵与官方原生登录** —— 添加账号由**你本机的官方 Antigravity** 完成 Google 登录：
2Ag 以零参数启动官方本体，你在官方窗口点一次「Continue with Google」即可，
2Ag 只负责捕获登录结果、加密入库，并自动恢复你原来的账号。
**2Ag 不持有也不分发任何 Google OAuth Client 凭据**（旧版本里那条内置 client_id /
client_secret 的自有 OAuth 路线已被彻底删除）。
账号保险库使用 **DPAPI(CurrentUser)** 加密（换用户、换机器都解不开），
索引文件只有邮箱等元数据、不含 token；切换账号是事务化的 ——
停宿主 → 写凭据 → 重启 → 回读校验，失败自动回滚并如实报错。

**视觉工坊** —— 壁纸 / 模糊 / 不透明度 / 模态透明度，六套内嵌主题预设，
明暗双调色板（Material 3 / Gemini 视觉语言，暗色不是亮色的反相）。

**本机回环代理** —— 按配置拦截遥测域名与信标，支持明文 HTTP 端点改道与全局规则。

**高 DPI 下不再发虚** —— 进程显式声明 Per-Monitor DPI Aware V2。此前在 125% / 150%
缩放下 Manager 是被 Windows 按 1.5 倍整体拉伸的（`devicePixelRatio` 只有 1），文字发虚；
现在按显示器 DPI 原生渲染，窗口物理尺寸与布局完全不变。

**不随包分发官方运行时** —— 安装包里没有任何 Google 字节。宿主是增强形态第一次启动时
在**你本机**按需建立的（物理复制，官方安装只读），安装包因此从 156 MB 降到几十兆。

## 隐私与数据

- 出网只有两处：`127.0.0.1`，以及宿主本来就要访问的 Google 账号 / API 端点。
  **没有作者服务器、没有统计上报。**
- 所有用户数据来自**当前用户本机**，写回本机 `2ag.json`。
- **不持有任何 Google OAuth Client 凭据，也不代替你向 Google 发起授权**：添加账号时
  Google 登录由官方 Antigravity 完成，2Ag 只捕获结果。账号凭据以 DPAPI(CurrentUser)
  加密存放于 `~/.2ag/vault/*.bin`；token 不进 `2ag.json`、不进日志、不进 API 响应、不进前端。
- 新装时（配置为空）需要用户数据的面板显示 `--` / 不可用，不会显示任何开发机数值。
- 历史上有一处写死的开发机壁纸路径被当作「空值的默认值」使用，本版已彻底移除
  （连同那条已经没人调用的旧注入链路一起删掉）。
- 打包闸门是**结构性**的：产物里出现 `C:\Users\<某人>\`、构建机用户名或真实邮箱，
  打包直接失败。发布包里没有任何上游 / 第三方运行时字节，所以闸门的作用域就是
  2Ag 自己的全部产物。

## 安装

1. **先安装官方 Antigravity**（2Ag 不含官方运行时；默认装在
   `%LOCALAPPDATA%\Programs\Antigravity`）。
2. 运行 `Anti-Antigravity-Setup-x64.exe`（免管理员，装到当前用户目录）。
3. 从开始菜单或桌面快捷方式启动 `2Ag`。首次启动会生成中性 `2ag.json`，
   不会携带任何预设壁纸、账号或插件。

**冻结宿主（首次启动增强形态时）：** 增强形态需要一份 2Ag 自己的宿主副本，落点是
`2ag.exe` 同级的 `app\`。安装包里没有它 —— 你按下「启动 / 热接管」时，2Ag 会从
**你本机**的官方安装目录物理复制一份出来（约 570 MB，需要数十秒）。

- 复制过程对官方安装目录**只读**：不修改、不移动它的任何文件，也不是 junction。
- 副本建好后 2Ag 一直用它。官方 updater 更新的是你那份官方安装，两者物理隔离；
  版本是否落后由**更新守护**如实报出，要不要同步由你决定，2Ag 不会自动替换。
- 本机既没有官方 Antigravity、也还没有冻结副本时，Manager 照常打开，只显示
  「未检测到 Antigravity」——不伪造版本、PID 或配额。

**升级说明：** 安装程序对 `2ag.json` 与 `2ag.log` 使用 *onlyifdoesntexist* ——
已存在则原样保留，你的配置不会被覆盖。卸载时会删除这两份文件。

**升级不会动你已有的冻结宿主副本**：从 0.1.0 / 0.1.1 升级上来的用户，安装目录里那份
`app\` 会被原样保留（那是你机器上已经存在的东西），既不删除也不重新复制。新装用户
没有这份副本，第一次启动增强形态时按上面的流程建立。

从 0.1.0 覆盖升级时，安装程序还会按文件名精确清掉 0.1.1 已不再使用的几个残留：
`assets\inject.js`、`assets\dream-skin.css`、`assets\assets.go`、`2ag.exe.manifest`，
以及 `themes\default\wallpaper.jpg` —— 最后这一张是 0.1.0 随包发出去的构建机壁纸，
不属于产品素材。`themes\` 与 `plugins\` 目录本身不会被清理，你自己放进去的东西不会被动。

**添加账号（需要登录时）：** Manager 的「账号 → 接入新账号」里选「官方原生登录」。
2Ag 会备份并临时清空当前凭据、以**零参数**启动你本机的官方 Antigravity（不注入、
不改它的任何文件），你在官方窗口点「Continue with Google」完成 Google 登录后，
2Ag 捕获新凭据、用 DPAPI 加密存入保险库，并自动恢复你原来的账号（流程开始时宿主在运行的话，
再按原运行形态重启它）。全过程 2Ag 不接触你的 Google 密码、授权码，也不持有任何 OAuth 客户端凭据。

**前置条件：** 本机需已安装官方 Antigravity（增强形态首次启动时用来建立冻结宿主；
添加账号时由它完成原生登录）；WebView2 运行时（Windows 11 自带，Windows 10 通常随 Edge 存在）。

## 已知限制

- **仅 Windows x64。**
- **不解密 TLS。** HTTPS `CONNECT` 是不透明隧道，所以代理无法改写 HTTPS 请求体。
  这是刻意的设计边界，不是待办项。
- `themes/` 目录只是纯数据描述，运行时生效的主题预设内嵌在注入载荷里 ——
  多放一个文件不会多一套主题。
- 冻结宿主（frozen host）不会被自动升级；升级由兼容性 / 更新守护给出结论，由你决定。
- **切换账号需要重启宿主**：宿主的登录身份来自机器级凭据，只在进程启动时读取。
  因此 2Ag 统一走「停宿主 → 写凭据 → 启动 → 回读校验」，不依赖「运行中直接改凭据」。
- **保险库是 DPAPI(CurrentUser) 加密**：换 Windows 用户或换机器都解不开（这是刻意绑定的，
  不是缺陷）。凭据一旦从保险库和系统凭据管理器里同时丢失，只能重新登录获取。
- **许可证只覆盖 2Ag 自有代码**（Apache License 2.0，见仓库根 `LICENSE`）；Google Antigravity
  运行时的版权归 Google，2Ag 与 Google、Anthropic、OpenAI 没有官方关联。
- 上游第三方组件**不在本发布包里**：官方 Antigravity 运行时中那些组件（如
  `resources/app.asar.unpacked/` 下的 `chrome-devtools-mcp`）版权归其各自作者，
  授权条款随上游附带，它们只存在于你自己安装的官方 Antigravity 中。

## 第三方模型标识

Gemini / Claude / ChatGPT 图形是其各自权利人的商标，此处**仅用于标注数据属于哪个
模型池**，不表示任何关联或背书。

## 许可

2Ag 自有代码与文档以 [Apache License 2.0](LICENSE) 授权 —— 完整文本见仓库根 `LICENSE`。

**商标与第三方内容**：Google / Antigravity / Gemini / Claude / ChatGPT 等名称与标识归各自
权利人所有，此处仅作描述性指称；2Ag 与 Google、Anthropic、OpenAI **没有官方关联**，也未获其
赞助、授权或背书。Google Antigravity 运行时及其第三方组件不在本发布包内（版权归各自权利人，
授权条款随你本机安装的官方 Antigravity 附带）。
