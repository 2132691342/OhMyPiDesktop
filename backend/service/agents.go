// 子代理观测台（仅 omp）。不做调度：omp 没有 spawn 的 RPC，子代理只能由模型自己调 task 工具拉起。
// 两条数据源并用：get_subagents 拉取式、权威，用来对账（帧会丢，终态只出现一次）；
// subagent_progress 推送式、高频，驱动进度条。只靠推送会漏，只靠拉取会卡。
package service

import (
	"context"
	"time"

	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/pkg"
)

// 类型由 engine 包定义：它读写的全是引擎帧字段，service 不该知道那些名字。
// 这里只做别名，让 HTTP 层的类型不必跨两个包。
type Agent = engine.Agent
type AgentProgress = engine.AgentProgress

// TranscriptPage 是一次字节游标拉取的结果。
type TranscriptPage struct {
	Entries  []map[string]any `json:"entries"`
	Messages []map[string]any `json:"messages"`
	Next     int64            `json:"next"`
	Reset    bool             `json:"reset"`
}

// ---------------- 对外的五条用例 ----------------

// agentEngine 取出当前 omp 引擎与它已就绪的进程；pi 下直接报「不支持」。
func (a *App) agentEngine(ctx context.Context, kind engine.Kind) (*engine.Engine, *engine.Proc, engine.SubagentStore, error) {
	k := a.KindOr(string(kind))
	if !a.Identity(k).Caps.Subagents {
		return nil, nil, nil, pkg.New(pkg.CodeEngineUnsupported,
			string(k)+" 没有子代理，这个面板不适用", "")
	}
	eng := a.EngineFor(k, a.Workspace())
	store := eng.Subagents()
	if store == nil {
		return nil, nil, nil, pkg.New(pkg.CodeEngineUnsupported, "这个引擎没有子代理观测台", "")
	}
	proc, err := eng.Client(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	return eng, proc, store, nil
}

// Agents 返回 roster。顺便补一次对账：帧不是可靠流，每次问都要先纠正。
func (a *App) Agents(ctx context.Context, kind engine.Kind) ([]Agent, error) {
	_, proc, store, err := a.agentEngine(ctx, kind)
	if err != nil {
		return nil, err
	}
	if err := store.EnsureSubscription(ctx, proc, "progress"); err != nil {
		return nil, err
	}
	var out struct {
		Subagents []map[string]any `json:"subagents"`
	}
	if err := proc.Call(ctx, "get_subagents", nil, &out, 15*time.Second); err != nil {
		return nil, err
	}
	store.Snapshot(out.Subagents)
	return store.List(), nil
}

// AgentMessages 按字节游卡拉一段子代理对话。
func (a *App) AgentMessages(ctx context.Context, kind engine.Kind, id string, from int64) (*TranscriptPage, error) {
	_, proc, store, err := a.agentEngine(ctx, kind)
	if err != nil {
		return nil, err
	}
	var out struct {
		Entries  []map[string]any `json:"entries"`
		Messages []map[string]any `json:"messages"`
		NextByte int64            `json:"nextByte"`
		FromByte int64            `json:"fromByte"`
		Reset    bool             `json:"reset"`
	}
	if err := proc.Call(ctx, "get_subagent_messages", engine.SubagentMessagesParams(id, from), &out, 20*time.Second); err != nil {
		return nil, err
	}
	page := &TranscriptPage{
		Entries:  orEmpty(out.Entries),
		Messages: orEmpty(out.Messages),
		Next:     out.NextByte,
		Reset:    out.Reset,
	}
	// nextByte 是「下一次该从哪读」的游标；引擎没给（老版本）就原地不动，
	// 不能退回到 from，否则同一段会被反复重读。
	if page.Next == 0 {
		page.Next = from
	}
	store.SetCursor(id, page.Next)
	return page, nil
}

// SteerAgent 给子代理插一句话（不打断父轮次）。
func (a *App) SteerAgent(ctx context.Context, kind engine.Kind, id, message string) error {
	_, proc, _, err := a.agentEngine(ctx, kind)
	if err != nil {
		return err
	}
	return proc.Call(ctx, "steer_subagent", engine.SteerSubagentParams(id, message), nil, 30*time.Second)
}

// CancelAgent 硬杀一个子代理。返回 false 是幂等答案（它已经结束了），不是错误。
func (a *App) CancelAgent(ctx context.Context, kind engine.Kind, id string) (bool, error) {
	_, proc, _, err := a.agentEngine(ctx, kind)
	if err != nil {
		return false, err
	}
	var out struct {
		Cancelled bool `json:"cancelled"`
	}
	if err := proc.Call(ctx, "cancel_subagent", engine.CancelSubagentParams(id), &out, 30*time.Second); err != nil {
		return false, err
	}
	return out.Cancelled, nil
}

// SubscribeAgents 设置订阅档位（off / progress / events）。
func (a *App) SubscribeAgents(ctx context.Context, kind engine.Kind, level string) error {
	_, proc, store, err := a.agentEngine(ctx, kind)
	if err != nil {
		return err
	}
	return store.EnsureSubscription(ctx, proc, level)
}

// ClearAgents 在会话切换后清空 roster 与游标并广播。
func (a *App) ClearAgents(kind engine.Kind, workDir string) {
	if a.Engine == nil || workDir == "" {
		return
	}
	store := a.EngineFor(a.KindOr(string(kind)), workDir).Subagents()
	if store != nil {
		store.Clear()
	}
	if a.Hub != nil {
		a.Hub.Emit("", "agents:cleared", map[string]any{"kind": string(kind)})
	}
}

func orEmpty[T any](list []T) []T {
	if list == nil {
		return []T{}
	}
	return list
}
