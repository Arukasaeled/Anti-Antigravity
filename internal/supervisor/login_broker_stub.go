//go:build !windows

package supervisor

import "fmt"

// 非 Windows 平台的占位实现。
//
// 登录 Broker 的每一步都建立在 Windows 凭据管理器（gemini:antigravity）之上：
// 官方 Antigravity 只把那台机器的登录态写在那里，2Ag 也只是从那里捕获。
// 没有那个存储层就没有可捕获的对象，所以这里如实报「不支持」，而不是给一条
// 看起来能用、实际永远等不到凭据的假流程。
var errBrokerUnsupported = fmt.Errorf("添加账号（官方原生登录 Broker）仅在 Windows 上可用")

func LoginBrokerStatusNow() LoginBrokerStatus {
	return LoginBrokerStatus{Stage: brokerStageIdle, Message: "当前平台不支持登录 Broker"}
}

func StartLoginBroker(configuredMode string) error { return errBrokerUnsupported }

func CancelLoginBroker() error { return errBrokerUnsupported }

func SwitchAccountTransactional(email, configuredMode string) (AccountSwitchResult, error) {
	return AccountSwitchResult{Email: email}, errBrokerUnsupported
}
