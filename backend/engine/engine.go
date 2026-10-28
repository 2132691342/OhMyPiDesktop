// Package engine 是引擎的抽象层：进程、协议信封、事件翻译与监督者的公共部分。
//
// 它不认识具体引擎。`omp` 与 `pi` 各自只实现一个 Dialect（启动参数、握手、
// 私有帧），其余全部复用这里的代码。
//
// 这一层不实现 Agent：工具、压缩、重试、缓存、扩展、子代理全由引擎自己演进。
package engine

import (
	"context"
	"encoding/json"
)

// Kind 是引擎种类。取值即 CLI 名，也用于设置键与事件载荷。
type Kind string

const (
	KindOMP Kind = "omp"
	KindPi  Kind = "pi"
)

// Valid 判断是不是已知种类。
func (k Kind) Valid() bool { return k == KindOMP || k == KindPi }

// AllKinds 返回全部引擎，顺序即界面里的呈现顺序。
func AllKinds() []Kind { return []Kind{KindOMP, KindPi} }

// Label 是给人看的名字。
func (k Kind) Label() string {
	if k == KindPi {
		return "pi"
	}
	return "omp"
}

// Caps 是「这个引擎能干什么」的唯一答案。
//
// 界面按它隐藏入口，绝不渲染灰按钮——灰按钮会被当成「我还没权限」。
// 新增能力先加在这里，再让两个 Dialect 各自回答。
type Caps struct {
	Subagents      bool `json:"subagents"`        // 子代理观测台（get_subagents / steer / cancel）
	Goals          bool `json:"goals"`            // 目标（goal_updated）
	Todos          bool `json:"todos"`            // 待办阶段（todoPhases）
	Login          bool `json:"login"`            // 界面里发起 OAuth
	Fragments      bool `json:"fragments"`        // 协议 v2 分片重组
	QueueEdit      bool `json:"queue_edit"`       // 移除 / 提前排队单条消息（remove_queued_message）
	QueueClear     bool `json:"queue_clear"`      // 一次性清空排队（clear_queue）
	LiveSteerCount bool `json:"live_steer_count"` // queue_update.liveSteered
	SkillToggle    bool `json:"skill_toggle"`     // 靠 frontmatter 启停技能
	Extensions     bool `json:"extensions"`       // 扩展/插件 CLI 管理
	ExportHTML     bool `json:"export_html"`      // 导出为 HTML
	SlashCommands  bool `json:"slash_commands"`   // available_commands_update 推送
}

// Identity 是一次探测的结果：哪份可执行文件、什么版本、能干什么。
type Identity struct {
	Kind    Kind   `json:"kind"`
	Exe     string `json:"exe"`
	Version string `json:"version"`
	Source  string `json:"source"` // path / custom / managed
	Caps    Caps   `json:"caps"`
}

// Launch 是拉起一个引擎进程所需的全部输入。
//
// ExtraArgs 与 Env 由调用方组装：engine 包不认识 profile 与配置目录。
type Launch struct {
	Exe       string
	WorkDir   string
	ExtraArgs []string
	Env       []string
}

// Frame 是读协程交给翻译层的一帧原文。
//
// Raw 是行原文的独立副本：读协程会复用缓冲区，交出去的切片必须归调用方所有。
type Frame struct {
	Type string
	Raw  json.RawMessage
}

// Dialect 是一个具体引擎与公共层之间唯一的接缝。
type Dialect interface {
	Kind() Kind
	Caps() Caps

	// Args 拼出 CLI 参数。工作目录由进程 cwd 决定，但 omp 还额外接受 --cwd。
	Args(workDir string, extra []string) []string

	// Handshake 等引擎就绪。omp 等 ready 帧并协商 v2；pi 无握手，等首帧或超时。
	Handshake(ctx context.Context, p *Proc) error

	// Consume 处理引擎私有帧。返回 true 表示这一帧不向上派发。
	Consume(e *Engine, f Frame) bool
}

// Emitter 把事件推给界面。sessionID 为空表示全局通道。
type Emitter func(sessionID string, event string, data any)

// Getenv 让 Dialect 在拼参数时读设置，而不必 import env 包。
type Getenv func(key, fallback string) string
