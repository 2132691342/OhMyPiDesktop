// Package pkg 收口跨包共用的叶子工具：错误码、日志、原子写文件。
package pkg

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// AppError 是带错误码的可展示错误。前端按 Code 分流，Message 直接给用户看。
type AppError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

func (e *AppError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("[%s] %s: %s", e.Code, e.Message, e.Detail)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// New 构造错误。detail 为底层原因原文（可为空），只进日志不进界面主句。
func New(code, message, detail string) *AppError {
	return &AppError{Code: code, Message: message, Detail: detail}
}

// Wrap 把底层错误收成一个带码错误。
func Wrap(code, message string, err error) *AppError {
	if err == nil {
		return New(code, message, "")
	}
	return &AppError{Code: code, Message: message, Detail: err.Error()}
}

// As 取出错误码；非 AppError 统一按 1001（内部错误）处理。
func As(err error) *AppError {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr
	}
	return &AppError{Code: CodeInternal, Message: "内部错误", Detail: err.Error()}
}

// 错误码段位：1xxx 内部，2xxx 启动，3xxx 引擎进程/协议，4xxx 模型/凭据，
// 5xxx 技能/插件/能力不支持，6xxx 会话，7xxx 入参/鉴权，8xxx 文件/路径/网络，
// 9xxx 引擎自身的装 / 升级 / 卸载。
// 全表与「前端该怎么反应」见 docs/ERROR-CODES.md——两边必须同步。
const (
	CodeInternal = "1001"

	CodeStartup       = "2001"
	CodeDataDir       = "2002"
	CodeSettingsWrite = "2003"

	CodeEngineDown        = "3001"
	CodeEngineBusy        = "3002"
	CodeEngineTimeout     = "3003"
	CodeProtocol          = "3004"
	CodeEngineUnsupported = "3006"
	CodeEngineNoSession   = "3007"

	CodeModelInvalid   = "4001"
	CodeProviderExists = "4002"
	CodeLoginFailed    = "4003"

	CodeSkillInvalid  = "5002"
	CodeSkillReadOnly = "5003"
	CodePluginFailed  = "5004"
	CodeConfigRead    = "5005"
	CodeConfigParse   = "5006"

	CodeSessionInvalid = "6001"
	CodeSessionBusy    = "6002"

	CodeBadRequest   = "7001"
	CodeUnauthorized = "7002"

	CodeNotFound      = "8001"
	CodeFileBusy      = "8002"
	CodeNetwork       = "8003"
	CodeInstallFailed = "8004"

	// 9xxx 是「应用管理」段：装 / 升级 / 卸载引擎本身。
	// 它不属于 8xxx（文件与路径），也不是 5xxx（引擎自己的能力）——
	// 出错的是包管理器与网络，不是引擎拒绝了什么。
	CodeInstallUnavailable = "9001" // 缺少包管理器 / 命令不存在，命令没发出去
	CodeAppBusy            = "9002" // 同一个应用已有长任务在跑
)

// WriteFileAtomic 先写同目录临时文件再改名：中途失败不会留下半截配置。
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, perm); err != nil {
		return err
	}
	return os.Rename(name, path)
}
