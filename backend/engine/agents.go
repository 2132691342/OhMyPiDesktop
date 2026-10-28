package engine

import (
	"context"
	"time"
)

// 子代理的视图模型。它是「界面要看什么」的定义，与 omp 的帧字段名解耦：
// 帧里的 completionPercent 在这里叫 Completion，界面不会因为引擎改字段名而重写。
//
// 只有 omp 提供这份数据（Caps.Subagents）。pi 下 SubagentStore 为 nil，
// 界面整块不渲染。

// Agent 是子代理在界面上的样子。
type Agent struct {
	ID             string        `json:"id"`
	Index          int           `json:"index"`
	Name           string        `json:"agent"`
	Source         string        `json:"agent_source"`
	Description    string        `json:"description"`
	Status         string        `json:"status"`
	Task           string        `json:"task"`
	SessionFile    string        `json:"session_file"`
	ParentToolCall string        `json:"parent_tool_call_id"`
	LastUpdate     int64         `json:"last_update"`
	Progress       AgentProgress `json:"progress"`
}

// AgentProgress 是子代理最近的进度快照。
type AgentProgress struct {
	Completion  int      `json:"completion"`
	Stage       string   `json:"stage"`
	CurrentTool string   `json:"current_tool"`
	RecentTools []string `json:"recent_tools"`
	ToolCount   int      `json:"tool_count"`
	Tokens      int64    `json:"tokens"`
	ContextUsed int      `json:"context_used"`
	ContextMax  int      `json:"context_max"`
	Cost        float64  `json:"cost"`
	DurationMS  int64    `json:"duration_ms"`
	Model       string   `json:"model"`
}

// Caller 是发一条命令并等响应的最小面。Proc 实现它；
// 观测台与探测之类只需要这一面，不该拿到整个进程。
type Caller interface {
	Call(ctx context.Context, command string, params any, out any, timeout time.Duration) error
}

// SubagentStore 是子代理观测台的状态。只有 omp 有实现。
type SubagentStore interface {
	Clear()
	EnsureSubscription(ctx context.Context, call Caller, level string) error
	Level() string
	Snapshot(items []map[string]any)
	List() []Agent
	Cursor(id string) int64
	SetCursor(id string, from int64)
	ApplyFrame(frameType string, raw map[string]any)
}
