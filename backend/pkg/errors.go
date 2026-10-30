// Package pkg 收口跨包共用的叶子工具：错误码、日志、原子写文件、ID。
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

// 错误码段位：1xxx 本地环境（内部 / 路径 / Git），2xxx 启动与装配，3xxx 引擎进程与协议，4xxx 模型与凭据，
// 5xxx 技能 / 插件 / 引擎配置，6xxx 会话，7xxx 入参与鉴权，8xxx 文件 / 路径 / 网络，9xxx 引擎自身的装 / 升 / 卸。
// 一个码只能有一个意思（界面上同一个码必须给同一种反应）；全表与前端处置见 docs/ERROR-CODES.md——两边必须同步。
const (
	// 1xxx：本地环境出错了。内部错误、路径校验、Git 操作。
	CodeInternal = "1001"
	// CodeWorkspaceInvalid 工作目录为空 / 不存在 / 解析失败。
	CodeWorkspaceInvalid = "1003"
	// CodePathEscape 路径越出工作目录。
	CodePathEscape = "1004"
	// CodeFileMissing 文件不存在。
	CodeFileMissing = "1005"
	// CodeUntrackedDelete 删除未跟踪文件失败。
	CodeUntrackedDelete = "1007"
	// CodeGitTimeout Git 命令超时。
	CodeGitTimeout = "1014"
	// CodeGitUnavailable 没装 Git / 不是仓库 / 读不到 Git 的信息。
	CodeGitUnavailable = "1015"
	// CodeCommitMessage 提交信息没填或太长。
	CodeCommitMessage = "1016"
	// CodeNothingToCommit 没有可提交的改动。
	CodeNothingToCommit = "1017"
	// CodeGitBadInput Git 的入参不合法（分支名 / 远端名 / 强制推送级别）。
	CodeGitBadInput = "1018"
	// CodeGitRejected Git 拒绝了这次操作（工作区有改动 / 钩子拒绝 / 分支删不掉）。
	CodeGitRejected = "1019"
	// CodeGitIdentity Git 的用户名与邮箱没配。
	CodeGitIdentity = "1020"
	// CodeConflictUnresolved 还有冲突没解决。
	CodeConflictUnresolved = "1021"
	// CodeRemoteAmbiguous 远端目标不明确（多个远端 / 当前分支没有上游）。
	CodeRemoteAmbiguous = "1022"
	// CodeNoRemote 这个仓库还没配置远端。
	CodeNoRemote = "1023"
	// CodePushRejected 推送被拒（远端有本地没有的提交）。
	CodePushRejected = "1024"
	// CodePushAuth 远端拒绝了身份验证。
	CodePushAuth = "1025"
	// CodePushNetwork 连不上远端。
	CodePushNetwork = "1026"
	// CodeLeaseRejected 带租约的强制推送被拒（远端又变了）。
	CodeLeaseRejected = "1027"

	// 2xxx：启动与装配。
	CodeStartup       = "2001"
	CodeDataDir       = "2002"
	CodeSettingsWrite = "2003"

	// 3xxx：引擎进程与协议。
	CodeEngineDown        = "3001"
	CodeEngineBusy        = "3002"
	CodeEngineTimeout     = "3003"
	CodeProtocol          = "3004"
	CodeEngineUnsupported = "3006"

	// 4xxx：模型与凭据。
	CodeModelInvalid = "4001"
	CodeLoginFailed  = "4003"

	// 5xxx：技能 / 插件 / 引擎配置。
	CodeSkillInvalid  = "5002"
	CodeSkillReadOnly = "5003"
	CodePluginFailed  = "5004"
	CodeConfigRead    = "5005"
	CodeConfigParse   = "5006"

	// 6xxx：会话。
	CodeSessionInvalid = "6001"
	CodeSessionBusy    = "6002"

	// 7xxx：入参与鉴权。
	CodeBadRequest = "7001"
	// CodeUnauthorized 启动令牌不匹配。
	CodeUnauthorized = "7002"
	// CodeForbiddenSource 请求不是来自本机。
	CodeForbiddenSource = "7003"

	// 8xxx：文件 / 路径 / 网络。
	CodeNotFound = "8001"
	// CodeNetwork 网络请求失败（含连不上后端）。
	CodeNetwork = "8003"
	// CodeInstallFailed 装 / 升 / 卸的命令跑失败了。
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
