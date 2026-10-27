package omp

import (
	"context"
	"sync"
	"time"

	"OhMyPiDesktop/backend/engine"
)

// 子代理 roster 与字节游标（仅 omp）。
//
// 它是引擎进程状态的旁支：与进程同生命周期，会话切换时要跟着清。
// 放在这里而不是 service，是因为它读写的全是引擎帧字段——
// service 只做编排，不该知道 completionPercent 这种帧内部名字。

// 视图类型由 engine 包定义：roster 只负责把帧字段映射过去。

// Roster 管一个引擎进程的 roster 与字节游标。
//
// 它与引擎进程同生命周期：会话切换（new/switch/fork/open）会让引擎内部
// clear() 并丢弃旧 id 的帧，所以那几条命令成功后我们必须自己清干净。
type Roster struct {
	mu      sync.Mutex
	agents  map[string]*engine.Agent
	order   []string
	cursors map[string]int64
	sub     string
}

// NewRoster 构造 roster。
func NewRoster() *Roster {
	return &Roster{agents: map[string]*engine.Agent{}, cursors: map[string]int64{}, sub: "off"}
}

// EnsureSubscription 打开 progress 档。默认是 off——不打开界面永远是空的，
// 而且没有任何报错提示，所以这一步必须在引擎就绪后立刻做。
func (r *Roster) EnsureSubscription(ctx context.Context, call engine.Caller, level string) error {
	if level == "" {
		level = "progress"
	}
	r.mu.Lock()
	cur := r.sub
	r.mu.Unlock()
	if cur == level {
		return nil
	}
	var out struct {
		Level string `json:"level"`
	}
	if err := call.Call(ctx, "set_subagent_subscription", map[string]any{"level": level}, &out, 10*time.Second); err != nil {
		return err
	}
	r.mu.Lock()
	r.sub = level
	r.mu.Unlock()
	return nil
}

// Level 返回当前订阅档位。
func (r *Roster) Level() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sub
}

// Clear 在会话切换后清空。
func (r *Roster) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agents = map[string]*engine.Agent{}
	r.order = nil
	r.cursors = map[string]int64{}
}

// Snapshot 用 get_subagents 的权威结果替换 roster。
func (r *Roster) Snapshot(items []map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := map[string]bool{}
	for _, raw := range items {
		a := parseAgent(raw)
		if a.ID == "" {
			continue
		}
		seen[a.ID] = true
		if prev, ok := r.agents[a.ID]; ok && a.Progress.Stage == "" && a.Progress.Completion == 0 {
			// 快照里没有 progress 时不覆盖推送来的更详细那份。
			a.Progress = prev.Progress
		}
		if _, ok := r.agents[a.ID]; !ok {
			r.order = append(r.order, a.ID)
		}
		r.agents[a.ID] = &a
	}
	// 快照里消失的（终态后引擎已摘除）从本地也摘掉。
	kept := r.order[:0]
	for _, id := range r.order {
		if seen[id] {
			kept = append(kept, id)
		} else {
			delete(r.agents, id)
		}
	}
	r.order = kept
}

// List 返回按 index 排好的副本。
func (r *Roster) List() []engine.Agent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]engine.Agent, 0, len(r.order))
	for _, id := range r.order {
		if a, ok := r.agents[id]; ok {
			out = append(out, *a)
		}
	}
	sortAgents(out)
	return out
}

// Cursor 返回某子代理的字节游标。
func (r *Roster) Cursor(id string) int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cursors[id]
}

// SetCursor 记录某子代理的字节游标。
func (r *Roster) SetCursor(id string, from int64) {
	if id == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if from < 0 {
		from = 0
	}
	r.cursors[id] = from
}

// ApplyFrame 把 subagent_* 帧喂进 roster。
func (r *Roster) ApplyFrame(frameType string, raw map[string]any) {
	switch frameType {
	case "subagent_lifecycle":
		r.applyLifecycle(raw)
	case "subagent_progress":
		r.applyProgress(raw)
	}
}

func (r *Roster) applyLifecycle(raw map[string]any) {
	id := str(raw["id"])
	if id == "" {
		return
	}
	status := str(raw["status"])
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.agents[id]
	if !ok {
		if status == "completed" || status == "failed" || status == "aborted" {
			return // 终态帧可能是我们最后一次见到它，但没有前情就不必新建
		}
		a = &engine.Agent{ID: id}
		r.order = append(r.order, id)
		r.agents[id] = a
	}
	if v := str(raw["agent"]); v != "" {
		a.Name = v
	}
	if v := str(raw["agentSource"]); v != "" {
		a.Source = v
	}
	if v := str(raw["description"]); v != "" {
		a.Description = v
	}
	if v := str(raw["task"]); v != "" {
		a.Task = v
	}
	if v := str(raw["sessionFile"]); v != "" {
		a.SessionFile = v
	}
	if v := str(raw["parentToolCallId"]); v != "" {
		a.ParentToolCall = v
	}
	if v, ok := raw["index"].(float64); ok {
		a.Index = int(v)
	}
	if v, ok := raw["lastUpdate"].(float64); ok {
		a.LastUpdate = int64(v)
	}
	if status != "" {
		a.Status = status
	}
	if a.Status == "" {
		a.Status = "pending"
	}
}

func (r *Roster) applyProgress(raw map[string]any) {
	id := str(raw["id"])
	if id == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.agents[id]
	if !ok {
		a = &engine.Agent{ID: id, Status: "running"}
		r.order = append(r.order, id)
		r.agents[id] = a
	}
	if v := str(raw["agent"]); v != "" {
		a.Name = v
	}
	if v := str(raw["task"]); v != "" {
		a.Task = v
	}
	if v, ok := raw["index"].(float64); ok {
		a.Index = int(v)
	}
	progress, ok := raw["progress"].(map[string]any)
	if !ok {
		return
	}
	if v, ok := progress["completionPercent"].(float64); ok {
		a.Progress.Completion = int(v)
	}
	if v := str(progress["stage"]); v != "" {
		a.Progress.Stage = v
	}
	if v := str(progress["currentTool"]); v != "" {
		a.Progress.CurrentTool = v
	}
	if v, ok := progress["toolCount"].(float64); ok {
		a.Progress.ToolCount = int(v)
	}
	if v, ok := progress["cost"].(float64); ok {
		a.Progress.Cost = v
	}
	if v, ok := progress["tokens"].(float64); ok {
		a.Progress.Tokens = int64(v)
	}
	if v, ok := progress["contextTokens"].(float64); ok {
		a.Progress.ContextUsed = int(v)
	}
	if v, ok := progress["contextWindow"].(float64); ok {
		a.Progress.ContextMax = int(v)
	}
	if v, ok := progress["durationMs"].(float64); ok {
		a.Progress.DurationMS = int64(v)
	}
	if v := str(progress["model"]); v != "" {
		a.Progress.Model = v
	}
	if a.Status == "pending" {
		a.Status = "running"
	}
}

func parseAgent(raw map[string]any) engine.Agent {
	a := engine.Agent{
		ID:             str(raw["id"]),
		Index:          int(num(raw["index"])),
		Name:           str(raw["agent"]),
		Source:         str(raw["agentSource"]),
		Description:    str(raw["description"]),
		Status:         str(raw["status"]),
		Task:           str(raw["task"]),
		SessionFile:    str(raw["sessionFile"]),
		ParentToolCall: str(raw["parentToolCallId"]),
		LastUpdate:     int64(num(raw["lastUpdate"])),
	}
	if deep, ok := raw["progress"].(map[string]any); ok {
		a.Progress.Completion = int(num(deep["completionPercent"]))
		a.Progress.Stage = str(deep["stage"])
		a.Progress.CurrentTool = str(deep["currentTool"])
		a.Progress.ToolCount = int(num(deep["toolCount"]))
		a.Progress.Cost = num(deep["cost"])
		a.Progress.Tokens = int64(num(deep["tokens"]))
		a.Progress.ContextUsed = int(num(deep["contextTokens"]))
		a.Progress.ContextMax = int(num(deep["contextWindow"]))
		a.Progress.DurationMS = int64(num(deep["durationMs"]))
		a.Progress.Model = str(deep["model"])
	}
	return a
}

func sortAgents(list []engine.Agent) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j].Index < list[j-1].Index; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func num(v any) float64 {
	f, _ := v.(float64)
	return f
}
