//go:build windows

package supervisor

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

// 本文件提供 Windows 原生「选择图片」对话框。
//
// 为什么必须有它：2Ag Manager 是一个 WebView2 视窗，不是浏览器。
// 网页里的 <input type="file"> 在 WebView2 上会走宿主自己的文件选择器，
// 而我们要的是「用户选完，Go 侧拿到绝对路径，再由状态机把图读出来送进宿主」——
// 也就是说这个选择必须发生在 Go 侧，选出的路径直接就是配置里要落的值。
//
// 因此这里直接调 Win32 通用对话框（comdlg32!GetOpenFileNameW），
// 不引入任何文件系统抽象层、不引入第三方对话框库、不新增 go.mod 依赖。

// ErrPickCancelled 表示用户在对话框里点了取消。
//
// 它是「正常结局」而不是失败：界面不该显示红色错误，也不该把取消
// 当成一次状态变更。
var ErrPickCancelled = errors.New("已取消选择")

// ErrPickBusy 表示已经有一个选择对话框开着。
//
// 对话框是系统级模态窗口，再开一个只会让用户看到两个一样的窗口、
// 且两个都会把结果写回同一个配置。
var ErrPickBusy = errors.New("已有一个图片选择对话框处于打开状态")

var (
	comdlg32                 = syscall.NewLazyDLL("comdlg32.dll")
	procGetOpenFileNameW     = comdlg32.NewProc("GetOpenFileNameW")
	procCommDlgExtendedError = comdlg32.NewProc("CommDlgExtendedError")

	ole32            = syscall.NewLazyDLL("ole32.dll")
	procCoInitialize = ole32.NewProc("CoInitializeEx")
	procCoUninit     = ole32.NewProc("CoUninitialize")
)

// OPENFILENAMEW（x64）的字段顺序与字节偏移。逐字段对齐到 Windows SDK 的定义：
//
//	0   lStructSize        DWORD           （后接 4 字节对齐填充）
//	8   hwndOwner          HWND
//	16  hInstance          HINSTANCE
//	24  lpstrFilter        LPCWSTR
//	32  lpstrCustomFilter  LPWSTR
//	40  nMaxCustFilter     DWORD
//	44  nFilterIndex       DWORD
//	48  lpstrFile          LPWSTR
//	56  nMaxFile           DWORD           （后接 4 字节对齐填充）
//	64  lpstrFileTitle     LPWSTR
//	72  nMaxFileTitle      DWORD           （后接 4 字节对齐填充）
//	80  lpstrInitialDir    LPCWSTR
//	88  lpstrTitle         LPCWSTR
//	96  Flags              DWORD
//	100 nFileOffset        WORD
//	102 nFileExtension     WORD            （104 已经 8 字节对齐，这里不需要填充）
//	104 lpstrDefExt        LPCWSTR
//	112 lCustData          LPARAM
//	120 lpfnHook           LPVOID
//	128 lpTemplateName     LPCWSTR
//	136 pvReserved         void*
//	144 dwReserved         DWORD
//	148 FlagsEx            DWORD
//	= 152 字节
//
// 布局错一个字段，后续所有指针都会被系统按错误偏移读走 —— 轻则对话框
// 弹不出来，重则崩在系统调用里。所以下面那两个数组长度断言不是装饰，
// 它们让任何一次布局漂移在编译期就炸掉。
type openFileNameW struct {
	lStructSize       uint32
	_                 uint32 // 对齐填充
	hwndOwner         uintptr
	hInstance         uintptr
	lpstrFilter       *uint16
	lpstrCustomFilter *uint16
	nMaxCustFilter    uint32
	nFilterIndex      uint32
	lpstrFile         *uint16
	nMaxFile          uint32
	_                 uint32 // 对齐填充
	lpstrFileTitle    *uint16
	nMaxFileTitle     uint32
	_                 uint32 // 对齐填充
	lpstrInitialDir   *uint16
	lpstrTitle        *uint16
	flags             uint32
	nFileOffset       uint16
	nFileExtension    uint16
	// 注意：这里**不能**再插对齐填充。nFileExtension 结束于偏移 104，
	// 而 104 已经是 8 的倍数，指针可以直接落在这里。多插一个 uint32
	// 会把整个尾部推后 8 字节、总长变成 160，而 Windows 会校验
	// lStructSize 是否为已知版本（152）—— 不匹配时对话框直接失败。
	lpstrDefExt    *uint16
	lCustData      uintptr
	lpfnHook       uintptr
	lpTemplateName *uint16
	pvReserved     uintptr
	dwReserved     uint32
	flagsEx        uint32
}

const openFileNameWBytes = 152

// 编译期布局断言：长度短于或长于 152 都会得到一个「负长度数组」编译错误。
var (
	_ [unsafe.Sizeof(openFileNameW{}) - openFileNameWBytes]struct{}
	_ [openFileNameWBytes - unsafe.Sizeof(openFileNameW{})]struct{}
)

// OPENFILENAMEW 的 Flags 位。
const (
	ofnHideReadOnly    = 0x00000004
	ofnNoChangeDir     = 0x00000008
	ofnPathMustExist   = 0x00000800
	ofnFileMustExist   = 0x00001000
	ofnExplorer        = 0x00080000
	ofnDontAddToRecent = 0x02000000
)

// dialogOwner 是「已创建的 Manager 视窗」句柄。
//
// 有 owner 才能让对话框成为该视窗的模态子窗口：居中于视窗、盖在它前面、
// 未关闭前点不动后面的界面。没有 owner 时对话框可能被 WebView2 视窗压住，
// 用户看到的现象就是「点了按钮什么都没发生」。
var (
	dialogOwnerMu sync.RWMutex
	dialogOwner   uintptr
)

// SetDialogOwnerWindow 由 Manager 在视窗创建后调用一次；传 0 表示清除。
func SetDialogOwnerWindow(hwnd uintptr) {
	dialogOwnerMu.Lock()
	dialogOwner = hwnd
	dialogOwnerMu.Unlock()
}

func currentDialogOwner() uintptr {
	dialogOwnerMu.RLock()
	defer dialogOwnerMu.RUnlock()
	return dialogOwner
}

// pickMu 保证同一时刻只有一个对话框。
var pickMu sync.Mutex

// utf16Filter 把 []string 形式的过滤器拼成对话框要求的双 NUL 结尾格式。
//
// 格式是「显示名\0模式\0显示名2\0模式2\0\0」：每一段各自以 NUL 结尾，
// 整串再以一个额外的 NUL 收尾。少那个额外的 NUL，Windows 会一路读过
// 缓冲区尾部去找终止符。
func utf16Filter(pairs []string) []uint16 {
	out := make([]uint16, 0, 256)
	for _, p := range pairs {
		// StringToUTF16 返回的切片本身就以 NUL 结尾，直接追加即可。
		out = append(out, syscall.StringToUTF16(p)...)
	}
	out = append(out, 0)
	return out
}

// PickImageFile 弹出原生「选择图片」对话框，返回用户选定的绝对路径。
//
// 返回值刻意区分三种情况，调用方（HTTP 处理器）必须原样转达给界面：
//   - (path, nil)             用户选了文件
//   - ("", ErrPickCancelled)  用户点了取消 —— 不是错误，界面不该报红
//   - ("", err)               对话框没起来（含系统错误码），必须显示原因
//
// 这个函数会阻塞到用户做出选择为止（对话框自己跑模态消息循环），
// 因此只能在请求协程里调用：绝不能拿到主消息循环的线程上去跑，
// 那会冻住整个 Manager 视窗。
func PickImageFile(title string) (string, error) {
	if !pickMu.TryLock() {
		return "", ErrPickBusy
	}
	defer pickMu.Unlock()

	// 全程锁在同一个 OS 线程上：COM 的套间状态是「每线程」的，
	// 而这个 goroutine 的底层线程随时可能被 Go 调度器换掉。
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// 通用对话框自己会处理套间，但显式初始化一次更稳：
	// 传 0 表示「不指定套间」，已初始化时返回 S_FALSE，被告知模式冲突
	// （RPC_E_CHANGED_MODE）也无所谓 —— 那时线程本来就可用于对话框。
	// 只有我们真的初始化成功（S_OK）才配对调用 CoUninitialize。
	const sOK = 0
	if hr, _, _ := procCoInitialize.Call(0, 0); hr == sOK {
		defer procCoUninit.Call()
	}

	// 4096 而不是 MAX_PATH：长路径（深层目录、\\?\ 前缀）在 260 下会被
	// 系统静默截断，用户拿到的是一个不存在的路径，之后校验失败却看不出原因。
	const bufChars = 4096
	fileBuf := make([]uint16, bufChars)

	titleUTF16 := syscall.StringToUTF16(title)
	// 扩展名过滤只用来收窄浏览范围；真正的类型判定在 ValidateWallpaperFile
	// 里按文件头字节做（把 PNG 存成 .jpg 是常见事，扩展名不可信）。
	filter := utf16Filter([]string{
		"图片文件", "*.jpg;*.jpeg;*.png;*.gif;*.webp;*.bmp",
		"JPEG", "*.jpg;*.jpeg",
		"PNG", "*.png",
		"GIF", "*.gif",
		"WebP", "*.webp",
		"BMP", "*.bmp",
		"所有文件", "*.*",
	})

	ofn := openFileNameW{
		lStructSize:  uint32(unsafe.Sizeof(openFileNameW{})),
		hwndOwner:    currentDialogOwner(),
		lpstrFilter:  &filter[0],
		nFilterIndex: 1,
		lpstrFile:    &fileBuf[0],
		nMaxFile:     bufChars,
		lpstrTitle:   &titleUTF16[0],
		flags: ofnExplorer | ofnFileMustExist | ofnPathMustExist |
			ofnHideReadOnly | ofnNoChangeDir | ofnDontAddToRecent,
	}

	ret, _, _ := procGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	if ret == 0 {
		// 0 有两种含义：用户取消，或对话框失败。靠 CommDlgExtendedError 分辨：
		// 取消时它返回 0，真有错时返回非 0 的 CDERR_* 码。
		if code, _, _ := procCommDlgExtendedError.Call(); code != 0 {
			return "", fmt.Errorf("系统文件对话框打开失败 (CDERR=0x%X)", code)
		}
		return "", ErrPickCancelled
	}

	picked := syscall.UTF16ToString(fileBuf)
	if picked == "" {
		return "", ErrPickCancelled
	}
	return picked, nil
}
