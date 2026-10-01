//go:build !windows

package supervisor

import "errors"

// 非 Windows 平台上没有 Win32 通用对话框。本文件存在的意义不是「降级」，
// 而是让 internal/supervisor 在其它平台仍能编译 —— 与 process_stub.go、
// sidecars_stub.go、antigravity_credential_stub.go 遵循同一约定。
//
// ErrPickUnsupported 必须被 HTTP 处理器当成「明确失败」返回给界面，
// 绝不能变成一个空路径或者一个假的成功。
var ErrPickUnsupported = errors.New("当前平台不支持系统文件选择对话框")

// ErrPickCancelled 与 Windows 版语义一致：用户主动取消，不是错误。
var ErrPickCancelled = errors.New("已取消选择")

// ErrPickBusy 与 Windows 版语义一致：已有一个对话框开着。
var ErrPickBusy = errors.New("已有一个图片选择对话框处于打开状态")

// SetDialogOwnerWindow 在非 Windows 平台是空操作。
func SetDialogOwnerWindow(hwnd uintptr) {}

// PickImageFile 在非 Windows 平台一律返回 ErrPickUnsupported。
func PickImageFile(title string) (string, error) {
	return "", ErrPickUnsupported
}
