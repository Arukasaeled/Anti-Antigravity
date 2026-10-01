# themes/

这里存放**可选的主题定义文件**。它们是一份纯数据描述，供外部工具或人工参考
用来说明某套预设的观感参数。

2Ag 实际生效的主题预设是硬编码在 `internal/patcher/injected_hub.js` 的
`THEME_PRESETS` 里的六套（Default / 2Ag Spectrum / Obsidian / Slate Aurora /
Cyberpunk / Gemini Dusk）。这个目录里的文件**不被运行时代码读取** ——
它们不代表「多放一个文件就会多一套主题」。

`wallpaper` 字段刻意留空/不写：壁纸是**用户自己的数据**，由用户在 Skin Studio
里选择，随 2ag.json 的 `wallpaper_path` 持久化。任何在这里预设某张图片的做法
都会把某个人的图片带进发布包 —— 那正是本项目明令禁止的事。