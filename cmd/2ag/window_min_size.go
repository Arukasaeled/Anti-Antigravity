package main

import (
	"log"
	"syscall"
	"unsafe"
)

// Manager 主视窗的最小尺寸护栏。
//
// 背景：go-webview2 的 WindowOptions 只有 Title / Width / Height / IconId / Center，
// 而 webview.SetSize(w, h, Hint) 的 HintMin 分支只会去填 Window 结构体里的
// minsz 字段，且必须在窗口创建之前就设置好才能生效；窗口已经跑起来之后再调
// SetSize(..., HintMin) 只会改变 minsz 而不会重新触发 WM_GETMINMAXINFO。
// 因此「最小宽高 1024x680」无法通过库的能力实现，必须自己子类化窗口过程，
// 截住 WM_GETMINMAXINFO 并把 PtMinTrackSize 抬到下限之上。
//
// 也正因如此，这里用 GWLP_WNDPROC 替换 + CallWindowProcW 转发：
// 先让 go-webview2 的原窗口过程把结构体填完，再覆盖最小值，避免破坏它自身的逻辑。

const (
	gwlpWndProc     = ^uintptr(3) // -4，以 uintptr 表示避免符号转换
	wmGetMinMaxInfo = 0x0024
)

type winPoint struct {
	X int32
	Y int32
}

// minMaxInfo 对应 Win32 的 MINMAXINFO（5 个 POINT，全部 int32，32/64 位布局一致）。
type minMaxInfo struct {
	PtReserved     winPoint
	PtMaxSize      winPoint
	PtMaxPosition  winPoint
	PtMinTrackSize winPoint
	PtMaxTrackSize winPoint
}

var (
	procSetWindowLongPtrW = user32.NewProc("SetWindowLongPtrW")
	procSetWindowLongW    = user32.NewProc("SetWindowLongW")
	procCallWindowProcW   = user32.NewProc("CallWindowProcW")

	// 仅由 GUI 线程读写；窗口存活期间保持有效。
	minSizeOrigProc uintptr
	minSizeW        int32
	minSizeH        int32
)

// installMinSizeGuard 给 hwnd 装上最小尺寸护栏。
// 尺寸以 CSS 像素给出，内部按窗口所在显示器 DPI 折算成物理像素。
func installMinSizeGuard(hwnd uintptr) {
	if hwnd == 0 {
		return
	}

	scale := dpiScaleForWindow(hwnd)
	w, h := physicalSize(managerMinWidthCSS, managerMinHeightCSS, scale)
	if w <= 0 || h <= 0 {
		return
	}

	// 取旧窗口过程：优先 SetWindowLongPtrW，32 位进程回退 SetWindowLongW。
	var prev uintptr
	if procSetWindowLongPtrW.Find() == nil {
		prev, _, _ = procSetWindowLongPtrW.Call(hwnd, gwlpWndProc, syscall.NewCallback(minSizeWndProc))
	} else if procSetWindowLongW.Find() == nil {
		prev, _, _ = procSetWindowLongW.Call(hwnd, gwlpWndProc, syscall.NewCallback(minSizeWndProc))
	} else {
		log.Printf("[2ag] 无法安装最小尺寸护栏：SetWindowLong(Ptr)W 均不可用")
		return
	}
	if prev == 0 {
		log.Printf("[2ag] 无法安装最小尺寸护栏：窗口过程替换返回 0")
		return
	}

	minSizeOrigProc = prev
	minSizeW = int32(w)
	minSizeH = int32(h)
	log.Printf("[2ag] 最小尺寸护栏已安装: %d x %d 物理像素 (未缩放 %d x %d CSS)",
		minSizeW, minSizeH, managerMinWidthCSS, managerMinHeightCSS)
}

// minSizeWndProc 先转发给原窗口过程，再抬升 PtMinTrackSize 下限。
//
// lParam 声明为 unsafe.Pointer 而非 uintptr：uintptr 转 unsafe.Pointer 会被
// go vet 的 unsafeptr 检查判为 "possible misuse of unsafe.Pointer"，
// 而且 uintptr 在 GC 移动对象时可能失效；直接收成指针既干净又安全。
// syscall.NewCallback 允许参数类型为指针（见 runtime compileCallback 的类型白名单）。
func minSizeWndProc(hwnd uintptr, msg uint32, wParam uintptr, lParam unsafe.Pointer) uintptr {
	var ret uintptr
	if minSizeOrigProc != 0 {
		ret, _, _ = procCallWindowProcW.Call(minSizeOrigProc, hwnd, uintptr(msg), wParam, uintptr(lParam))
	}

	if msg == wmGetMinMaxInfo && lParam != nil && minSizeW > 0 && minSizeH > 0 {
		mmi := (*minMaxInfo)(lParam)
		if mmi.PtMinTrackSize.X < minSizeW {
			mmi.PtMinTrackSize.X = minSizeW
		}
		if mmi.PtMinTrackSize.Y < minSizeH {
			mmi.PtMinTrackSize.Y = minSizeH
		}
	}
	return ret
}
