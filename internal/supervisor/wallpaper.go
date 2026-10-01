package supervisor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// 本文件负责「用户自定义壁纸」这条入口的**入参实测**。
//
// 存在的理由：壁纸路径是用户手输的，而它最终要通过 State → Event → CDP 落到宿主页面。
// 如果不在入口处把文件查清楚，失败的形态会是：界面报「已应用」→ 配置落盘 → 宿主
// 背景层依然是旧图（因为 ResolveWallpaperDataURL 读不到文件时就原样返回路径字符串，
// 而 https 宿主页面加载不了 file:// 路径）。用户看到的是「点了没反应」，
// 也就是本项目反复要根除的那种假成功。因此这里把「文件到底存不存在、是不是图片」
// 查实，并把结果作为回执交给界面显式展示。

// WallpaperFileInfo 是对候选壁纸文件的实测描述。
type WallpaperFileInfo struct {
	Path      string `json:"path"`       // 规范化后的绝对路径（正斜杠）
	Name      string `json:"name"`       // 文件名，供界面显示
	SizeBytes int64  `json:"size_bytes"` // 实测字节数
	MimeType  string `json:"mime_type"`  // 由文件头字节判定，不是按扩展名猜的
}

// maxWallpaperBytes 是接受的壁纸体积上限。
//
// 壁纸要以 base64 形式随注入表达式/推送载荷过一遍 CDP（文本帧），
// base64 会把体积撑大约 4/3，一张 32 MiB 的图已经接近 43 MiB 的字符串。
// 再大就不是「壁纸」而是误选了大文件，宁可当场拒绝也不要让 CDP 悄悄超时。
const maxWallpaperBytes = 32 << 20

// sniffImageMime 按文件头字节判定图片类型。
//
// 为什么不看扩展名：用户完全可能把 PNG 存成 .jpg（截图工具、另存为都会这么干），
// 而 data URL 里的 MIME 是浏览器唯一的类型依据，标错会让图片在某些路径下不渲染。
// 判定失败返回空串，由调用方决定回退策略。
func sniffImageMime(data []byte) string {
	if len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "image/jpeg"
	}
	if len(data) >= 8 && data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 &&
		data[4] == 0x0D && data[5] == 0x0A && data[6] == 0x1A && data[7] == 0x0A {
		return "image/png"
	}
	if len(data) >= 6 && (string(data[:6]) == "GIF87a" || string(data[:6]) == "GIF89a") {
		return "image/gif"
	}
	if len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return "image/webp"
	}
	if len(data) >= 2 && data[0] == 'B' && data[1] == 'M' {
		return "image/bmp"
	}
	return ""
}

// NormalizeWallpaperPath 把用户输入收敛成与配置/校验一致的形式。
//
// 三件事：去掉首尾空白（从资源管理器复制的路径常带不可见空白）、
// 剥掉 Windows「复制文件路径」给出的 file:/// 前缀、
// 把反斜杠统一成正斜杠（配置文件的既存约定，见 config.WallpaperURL）。
func NormalizeWallpaperPath(path string) string {
	trimmed := strings.TrimSpace(path)
	trimmed = strings.TrimPrefix(trimmed, "file:///")
	trimmed = strings.TrimPrefix(trimmed, "file://")
	// 去掉包裹路径的引号：用户从终端复制路径时经常连引号一起带过来
	if len(trimmed) >= 2 && (trimmed[0] == '"' && trimmed[len(trimmed)-1] == '"') {
		trimmed = trimmed[1 : len(trimmed)-1]
	}
	return strings.ReplaceAll(trimmed, `\`, "/")
}

// ValidateWallpaperFile 查实候选壁纸文件，返回实测信息或一个能直接展示给用户的原因。
func ValidateWallpaperFile(path string) (WallpaperFileInfo, error) {
	info := WallpaperFileInfo{}
	normalized := NormalizeWallpaperPath(path)
	if normalized == "" {
		return info, fmt.Errorf("壁纸路径为空")
	}

	// 必须是绝对 Windows 路径：相对路径的解析基准是 2ag.exe 所在目录，
	// 而用户心里想的是「我选的那个文件」，两者不一致时必须当场说清楚。
	if len(normalized) < 3 || normalized[1] != ':' || normalized[2] != '/' || !isDriveLetter(normalized[0]) {
		return info, fmt.Errorf("请填写绝对路径，形如 D:/Pictures/wallpaper.jpg")
	}

	stat, err := os.Stat(filepath.FromSlash(normalized))
	if err != nil {
		if os.IsNotExist(err) {
			return info, fmt.Errorf("文件不存在：%s", normalized)
		}
		return info, fmt.Errorf("无法访问该文件: %v", err)
	}
	if stat.IsDir() {
		return info, fmt.Errorf("这是一个文件夹，不是图片文件")
	}
	if stat.Size() == 0 {
		return info, fmt.Errorf("文件是空的（0 字节）")
	}
	if stat.Size() > maxWallpaperBytes {
		return info, fmt.Errorf("图片过大：%.1f MB，上限 %d MB", float64(stat.Size())/(1<<20), maxWallpaperBytes>>20)
	}

	// 只读文件头若干字节做类型判定，不把整张图读进内存（这张图稍后会由
	// ResolveWallpaperDataURL 真正读一遍，此处不必重复付代价）。
	file, err := os.Open(filepath.FromSlash(normalized))
	if err != nil {
		return info, fmt.Errorf("无法打开该文件: %v", err)
	}
	defer file.Close()
	head := make([]byte, 32)
	n, _ := file.Read(head)
	mimeType := sniffImageMime(head[:n])
	if mimeType == "" {
		return info, fmt.Errorf("这不是可识别的图片格式（支持 JPEG / PNG / GIF / WebP / BMP）")
	}

	info.Path = normalized
	info.Name = filepath.Base(filepath.FromSlash(normalized))
	info.SizeBytes = stat.Size()
	info.MimeType = mimeType
	return info, nil
}

func isDriveLetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
