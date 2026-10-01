package assets

import (
	_ "embed"
)

// 本包只承载**真正被消费**的内嵌资源。
//
// 历史上有过一个 inject.js + dream-skin.css + Materialize() 的组合（把资源物化到
// 磁盘上再由 <script> / <link> 引用）。补丁改成 CDP 整段注入之后那套链路就没有调用方了。
// 而 inject.js 里写着一个**开发机的壁纸绝对路径**，且它同时被当作 DEFAULT_WALLPAPER ——
// 留着它意味着任何一次误引用都会把某个人的图片画到别人机器上。
//
// 所以这里删掉死代码连同它的内嵌，而不是只去改那个常量：一个没人调用的函数里
// 藏着一条真实可用的泄漏路径，比一条明显坏掉的引用更危险。

//go:embed logo.png
var LogoPNG []byte
