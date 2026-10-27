package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"OhMyPiDesktop/backend/domain"
	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/env/ompcfg"
	"OhMyPiDesktop/backend/env/picfg"
	"OhMyPiDesktop/backend/pkg"
	"OhMyPiDesktop/backend/session"
	"OhMyPiDesktop/backend/skill"
)

// 编排：会话、对话、模型、技能、插件、引擎环境。
// 每个方法都是一次界面动作的完整链路，不允许把「先查再改」拆成两个 HTTP 端点。

// ---------------- 会话 ----------------

// Sessions 列出会话。kind 为空表示全部引擎；workspace 为空表示全部目录。
//
// 两个引擎的会话目录不同，但列表形态一致——前端不必按引擎分叉渲染。
func (a *App) Sessions(kind engine.Kind, workspace string) ([]session.Session, error) {
	kinds := engine.AllKinds()
	if kind.Valid() {
		kinds = []engine.Kind{kind}
	}
	out := make([]session.Session, 0, 32)
	for _, k := range kinds {
		list, err := a.Index(k).List(workspace)
		if err != nil {
			// 一个引擎的会话目录读不到不该让整个列表失败：
			// pi 没装过就永远没有这个目录，那是正常状态。
			continue
		}
		out = append(out, list...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

// SessionDetail 读会话详情。会话文件在该引擎的会话目录里，靠文件名归引擎。
func (a *App) SessionDetail(kind engine.Kind, path string) (*session.Detail, error) {
	if path == "" {
		return nil, pkg.New(pkg.CodeBadRequest, "缺少会话路径", "")
	}
	eng := a.kindOfSession(kind, path)
	d, err := a.Index(eng).Detail(path)
	if err != nil {
		return nil, err
	}
	if d.CWD != "" {
		_ = a.SetWorkspace(d.CWD)
	}
	return d, nil
}

// kindOfSession 判断一个会话文件属于哪个引擎。
//
// 判据是「它的路径在哪一侧的会话目录下」；都不在（用户手动打开的文件）时
// 退回请求里给的引擎。不靠文件名猜：两个引擎的文件名格式在变。
func (a *App) kindOfSession(kind engine.Kind, path string) engine.Kind {
	for _, k := range engine.AllKinds() {
		layout, err := a.Layout(k)
		if err != nil {
			continue
		}
		if isUnder(path, layout.Sessions) {
			return k
		}
	}
	if kind.Valid() {
		return kind
	}
	return a.ActiveKind()
}

// DeleteSession 删掉会话文件。
//
// 不删正在跑的那个：引擎会往已删文件继续追加，表现为「删了又回来了」。
func (a *App) DeleteSession(kind engine.Kind, path string) error {
	eng := a.kindOfSession(kind, path)
	e := a.EngineFor(eng, dirOf(path))
	if e.Busy() && samePath(e.CurrentSessionFile(), path) {
		return pkg.New(pkg.CodeSessionBusy, "这个会话正在跑，先停下来再删", "")
	}
	if err := os.Remove(path); err != nil {
		return pkg.Wrap(pkg.CodeNotFound, "删除会话失败", err)
	}
	// 目录里的其它文件（子代理转录等）一起清掉。
	base := strings.TrimSuffix(path, ".jsonl")
	if st, err := os.Stat(base); err == nil && st.IsDir() {
		_ = os.RemoveAll(base)
	}
	return nil
}

// RenameSession 改名（走 RPC：引擎同时会更新文件头的标题槽）。
func (a *App) RenameSession(ctx context.Context, kind engine.Kind, path, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return pkg.New(pkg.CodeBadRequest, "标题不能为空", "")
	}
	eng := a.kindOfSession(kind, path)
	e := a.EngineFor(eng, dirOf(path))
	if current := e.CurrentSessionFile(); !samePath(current, path) {
		if err := e.Switch(ctx, path); err != nil {
			return err
		}
	}
	return e.SetSessionName(ctx, name)
}

// ---------------- 对话 ----------------

// ChatState 是对话页的一次快照。
type ChatState struct {
	Kind            engine.Kind            `json:"kind"`
	Session         string                 `json:"session"`
	SessionFile     string                 `json:"session_file"`
	WorkDir         string                 `json:"work_dir"`
	EngineRunning   bool                   `json:"engine_running"`
	Streaming       bool                   `json:"streaming"`
	Model           *engine.Model          `json:"model"`
	ThinkingLevel   string                 `json:"thinking_level"`
	Levels          []string               `json:"levels"`
	Context         *engine.ContextUsage   `json:"context"`
	TokensPerSecond *float64               `json:"tokens_per_second"`
	AutoCompaction  bool                   `json:"auto_compaction"`
	Queued          *engine.QueuedMessages `json:"queued"`
	Stats           *engine.SessionStats   `json:"stats"`
	Messages        []json.RawMessage      `json:"messages"`
	History         []HistoryTurn          `json:"history"`
	Commands        []engine.SlashCommand  `json:"commands"`
	Profile         string                 `json:"profile"`
	Caps            engine.Caps            `json:"caps"`
}

// ChatState 拉对话页快照。会话文件为空时用当前工作目录的引擎状态。
func (a *App) ChatState(ctx context.Context, kind engine.Kind, sessionFile string) (*ChatState, error) {
	if !kind.Valid() {
		kind = a.kindOfSession(kind, sessionFile)
	}
	dir := a.Workspace()
	if sessionFile != "" {
		if d, err := a.Index(a.kindOfSession(kind, sessionFile)).Detail(sessionFile); err == nil && d.CWD != "" {
			dir = d.CWD
		}
	}
	caps := a.Identity(kind).Caps
	state := &ChatState{
		Kind:        kind,
		SessionFile: sessionFile,
		WorkDir:     dir,
		Profile:     a.Profile(),
		Caps:        caps,
		Levels:      []string{},
		Messages:    []json.RawMessage{},
		History:     []HistoryTurn{},
		Commands:    []engine.SlashCommand{},
	}
	e := a.EngineFor(kind, dir)
	st, err := e.State(ctx)
	if err != nil {
		// 引擎没起来不算错误：界面要显示「输入框禁用 + 连不上」。
		return state, nil
	}
	state.Session = st.SessionID
	if st.SessionFile != "" {
		state.SessionFile = st.SessionFile
	}
	state.EngineRunning = true
	state.Streaming = st.IsStreaming
	state.Model = st.Model
	state.ThinkingLevel = st.ThinkingLevel
	state.Context = st.ContextUsage
	state.TokensPerSecond = st.TokensPerSecond
	state.AutoCompaction = st.AutoCompaction
	state.Queued = st.Queued
	state.Levels = e.ThinkingLevels(ctx)
	if stats, err := e.Stats(ctx); err == nil {
		state.Stats = stats
		if state.Context == nil {
			state.Context = stats.ContextUsage
		}
	}
	if msgs, err := e.Messages(ctx); err == nil && len(msgs) > 0 {
		state.Messages = msgs
	} else if state.SessionFile != "" {
		// 引擎在跑但拿不到消息（换过会话 / 刚启动）时退回文件投影。
		if d, err := a.Index(kind).Detail(state.SessionFile); err == nil {
			state.Messages = itemsToRaw(d.Items)
		}
	}
	state.History = ProjectHistory(state.Messages)
	if caps.SlashCommands {
		if cmds := e.Commands(ctx); len(cmds) > 0 {
			state.Commands = cmds
		}
	} else {
		state.Commands = e.Commands(ctx)
	}
	if state.SessionFile == "" && sessionFile != "" {
		state.SessionFile = sessionFile
	}
	return state, nil
}

// itemsToRaw 把文件投影的条目转回 message 形态，让投影只有一条路径。
func itemsToRaw(items []session.Item) []json.RawMessage {
	out := make([]json.RawMessage, 0, len(items))
	for _, it := range items {
		msg := map[string]any{"role": it.Role}
		if len(it.Blocks) > 0 {
			msg["content"] = it.Blocks
		} else {
			msg["content"] = []map[string]any{{"type": "text", "text": it.Text}}
		}
		if it.ToolCall != "" {
			msg["toolCallId"] = it.ToolCall
		}
		if it.ToolName != "" {
			msg["toolName"] = it.ToolName
		}
		if it.IsError {
			msg["isError"] = true
		}
		raw, err := json.Marshal(msg)
		if err != nil {
			continue
		}
		out = append(out, raw)
	}
	return out
}

// Send 发一轮提问。kind 为空时用会话所属引擎。
func (a *App) Send(ctx context.Context, kind engine.Kind, sessionFile, message string) (string, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return "", pkg.New(pkg.CodeBadRequest, "说点什么再发送", "")
	}
	if !kind.Valid() {
		kind = a.kindOfSession(kind, sessionFile)
		// 用户没指定引擎时，把选择落回设置：下一句话仍然发给同一个引擎。
		_ = a.SetActive(kind)
	}
	dir := a.Workspace()
	if sessionFile != "" {
		if d, err := a.Index(a.kindOfSession(kind, sessionFile)).Detail(sessionFile); err == nil && d.CWD != "" {
			dir = d.CWD
		}
	}
	e := a.EngineFor(kind, dir)
	if sessionFile != "" && !samePath(e.CurrentSessionFile(), sessionFile) && !e.Busy() {
		// 打开的是历史会话：先切过去再发，否则消息会落到另一个会话里。
		_ = e.Switch(ctx, sessionFile)
	}
	return e.Prompt(ctx, message)
}

// engineForSession 按会话解析出引擎（省略引擎参数时用）。
func (a *App) engineForSession(kind engine.Kind, sessionFile string) (*engine.Engine, error) {
	k := kind
	if !k.Valid() {
		k = a.kindOfSession(k, sessionFile)
	}
	dir := a.Workspace()
	if sessionFile != "" {
		if d, err := a.Index(a.kindOfSession(k, sessionFile)).Detail(sessionFile); err == nil && d.CWD != "" {
			dir = d.CWD
		}
	}
	return a.EngineFor(k, dir), nil
}

// Stop 停止当前轮。
func (a *App) Stop(ctx context.Context, kind engine.Kind, sessionFile string) error {
	e, err := a.engineForSession(kind, sessionFile)
	if err != nil {
		return err
	}
	return e.Abort(ctx)
}

// Steer 插话（当前轮正在跑时补一句）。
func (a *App) Steer(ctx context.Context, kind engine.Kind, sessionFile, message string) error {
	e, err := a.engineForSession(kind, sessionFile)
	if err != nil {
		return err
	}
	return e.Steer(ctx, message)
}

// FollowUp 排队（这一轮结束后再说）。
func (a *App) FollowUp(ctx context.Context, kind engine.Kind, sessionFile, message string) error {
	e, err := a.engineForSession(kind, sessionFile)
	if err != nil {
		return err
	}
	return e.FollowUp(ctx, message)
}

// ClearQueue 一次性清空排队（仅 Caps.QueueClear 的引擎提供）。
func (a *App) ClearQueue(ctx context.Context, kind engine.Kind, sessionFile string) error {
	e, err := a.engineForSession(kind, sessionFile)
	if err != nil {
		return err
	}
	return e.ClearQueue(ctx)
}

// RemoveQueued 移除一条排队消息（仅 Caps.QueueEdit 的引擎提供）。
func (a *App) RemoveQueued(ctx context.Context, kind engine.Kind, sessionFile, message, queue string) (bool, error) {
	e, err := a.engineForSession(kind, sessionFile)
	if err != nil {
		return false, err
	}
	return e.RemoveQueued(ctx, message, queue)
}

// Compact 手动压缩上下文。
func (a *App) Compact(ctx context.Context, kind engine.Kind, sessionFile, instructions string) error {
	e, err := a.engineForSession(kind, sessionFile)
	if err != nil {
		return err
	}
	return e.Compact(ctx, instructions)
}

// Answer 回答引擎的提问。
func (a *App) Answer(ctx context.Context, kind engine.Kind, sessionFile string, resp engine.UIResponse) error {
	e, err := a.engineForSession(kind, sessionFile)
	if err != nil {
		return err
	}
	return e.Answer(ctx, resp)
}

// SetModel 换模型。
func (a *App) SetModel(ctx context.Context, kind engine.Kind, sessionFile, provider, modelID string) error {
	e, err := a.engineForSession(kind, sessionFile)
	if err != nil {
		return err
	}
	return e.SetModel(ctx, provider, modelID)
}

// SetThinking 换思考档。
func (a *App) SetThinking(ctx context.Context, kind engine.Kind, sessionFile, level string) error {
	e, err := a.engineForSession(kind, sessionFile)
	if err != nil {
		return err
	}
	return e.SetThinking(ctx, level)
}

// SetAutoCompaction 开关自动压缩。
func (a *App) SetAutoCompaction(ctx context.Context, kind engine.Kind, sessionFile string, enabled bool) error {
	e, err := a.engineForSession(kind, sessionFile)
	if err != nil {
		return err
	}
	return e.SetAutoCompaction(ctx, enabled)
}

// NewChat 在某引擎的当前工作目录开一个新会话。
func (a *App) NewChat(ctx context.Context, kind engine.Kind) (string, error) {
	if !kind.Valid() {
		kind = a.ActiveKind()
	}
	e := a.EngineFor(kind, a.Workspace())
	if err := e.NewSession(ctx); err != nil {
		return "", err
	}
	st, err := e.State(ctx)
	if err != nil {
		return "", err
	}
	return st.SessionFile, nil
}

// ---------------- 模型与服务商 ----------------

// ModelRow 是可用模型在界面上的形态。
type ModelRow struct {
	Kind          engine.Kind `json:"kind"`
	Provider      string      `json:"provider"`
	ID            string      `json:"id"`
	Name          string      `json:"name"`
	API           string      `json:"api"`
	ContextWindow int         `json:"context_window"`
	MaxTokens     int         `json:"max_tokens"`
	Reasoning     bool        `json:"reasoning"`
	Input         []string    `json:"input"`
}

// Models 列出引擎眼下的可用模型。
func (a *App) Models(ctx context.Context, kind engine.Kind) ([]ModelRow, error) {
	if !kind.Valid() {
		kind = a.ActiveKind()
	}
	e := a.EngineFor(kind, a.Workspace())
	list, err := e.AvailableModels(ctx)
	if err != nil {
		return []ModelRow{}, nil
	}
	out := make([]ModelRow, 0, len(list))
	for _, m := range list {
		out = append(out, ModelRow{
			Kind: kind, Provider: m.Provider, ID: m.ID, Name: m.Name, API: m.API,
			ContextWindow: m.ContextWindow, MaxTokens: m.MaxTokens,
			Reasoning: m.Reasoning, Input: m.Input,
		})
	}
	return out, nil
}

// Providers 返回某引擎的服务商视图（密钥打码）。
//
// 两个引擎的配置文件格式完全不同，但视图形态一致：前端只认 ProvidersView。
func (a *App) Providers(kind engine.Kind) (*domain.ProvidersView, error) {
	if !kind.Valid() {
		kind = a.ActiveKind()
	}
	layout, err := a.Layout(kind)
	if err != nil {
		return nil, err
	}
	if kind == engine.KindPi {
		f, err := picfg.Read(layout.Models)
		if err != nil {
			return nil, err
		}
		out := &domain.ProvidersView{Providers: []domain.ProviderView{}, Path: layout.Models}
		for _, name := range f.Names() {
			p := f.Providers[name]
			v := domain.ProviderView{
				Name:    name,
				BaseURL: p.BaseURL,
				API:     p.API,
				Auth:    p.OAuth,
				APIKey:  picfg.MaskKey(p.APIKey),
				HasKey:  p.APIKey != "",
				Models:  []domain.ModelEntry{},
			}
			for _, m := range p.Models {
				v.Models = append(v.Models, domain.ModelEntry{
					ID: m.ID, Name: m.Name, API: m.API, Reasoning: m.Reasoning,
					Input: m.Input, ContextWindow: m.ContextWindow, MaxTokens: m.MaxTokens,
				})
			}
			out.Providers = append(out.Providers, v)
		}
		return out, nil
	}
	f, err := ompcfg.ReadModels(layout.Models)
	if err != nil {
		return nil, err
	}
	view := f.View(layout.Models)
	out := &domain.ProvidersView{Providers: []domain.ProviderView{}, Path: view.Path}
	for _, p := range view.Providers {
		out.Providers = append(out.Providers, domain.ProviderView{
			Name: p.Name, BaseURL: p.BaseURL, API: p.API, Auth: p.Auth,
			APIKey: p.APIKey, HasKey: p.HasKey, Models: toDomainModels(p.Models),
		})
	}
	return out, nil
}

// UpsertProvider 写一个服务商。
func (a *App) UpsertProvider(kind engine.Kind, name string, p domain.ProviderView) (*domain.ProvidersView, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, pkg.New(pkg.CodeBadRequest, "服务商名字不能为空", "")
	}
	if !kind.Valid() {
		kind = a.ActiveKind()
	}
	layout, err := a.Layout(kind)
	if err != nil {
		return nil, err
	}
	if kind == engine.KindPi {
		f, err := picfg.Read(layout.Models)
		if err != nil {
			return nil, err
		}
		next := picfg.Provider{
			Name: name, BaseURL: p.BaseURL, APIKey: p.APIKey, API: p.API, OAuth: p.Auth,
		}
		for _, m := range p.Models {
			next.Models = append(next.Models, picfg.Model{
				ID: m.ID, Name: m.Name, API: m.API, Reasoning: m.Reasoning,
				Input: m.Input, ContextWindow: m.ContextWindow, MaxTokens: m.MaxTokens,
			})
		}
		if prev, ok := f.Providers[name]; ok {
			next = picfg.Merge(prev, next)
		}
		if err := picfg.Upsert(layout.Models, name, next); err != nil {
			return nil, err
		}
		return a.Providers(kind)
	}
	next := ompcfg.Provider{
		BaseURL: p.BaseURL, APIKey: p.APIKey, API: p.API, Auth: p.Auth,
	}
	for _, m := range p.Models {
		next.Models = append(next.Models, ompcfg.ModelEntry{
			ID: m.ID, Name: m.Name, API: m.API, Reasoning: m.Reasoning,
			Input: m.Input, ContextWindow: m.ContextWindow, MaxTokens: m.MaxTokens,
		})
	}
	if f, err := ompcfg.ReadModels(layout.Models); err == nil {
		if prev, ok := f.Providers[name]; ok {
			next = ompcfg.MergeProvider(prev, next)
		}
	}
	if err := ompcfg.UpsertProvider(layout.Models, name, next); err != nil {
		return nil, err
	}
	return a.Providers(kind)
}

// DeleteProvider 删一个服务商。
func (a *App) DeleteProvider(kind engine.Kind, name string) (*domain.ProvidersView, error) {
	if !kind.Valid() {
		kind = a.ActiveKind()
	}
	layout, err := a.Layout(kind)
	if err != nil {
		return nil, err
	}
	if kind == engine.KindPi {
		if err := picfg.Delete(layout.Models, name); err != nil {
			return nil, err
		}
	} else if err := ompcfg.DeleteProvider(layout.Models, name); err != nil {
		return nil, err
	}
	return a.Providers(kind)
}

// LoginProviders 返回可登录的服务商（仅 omp 有 RPC 面）。
func (a *App) LoginProviders(ctx context.Context, kind engine.Kind) ([]engine.LoginProvider, error) {
	if !kind.Valid() {
		kind = a.ActiveKind()
	}
	if !a.Identity(kind).Caps.Login {
		return []engine.LoginProvider{}, nil
	}
	var out struct {
		Providers []engine.LoginProvider `json:"providers"`
	}
	if err := a.EngineFor(kind, a.Workspace()).Call(ctx, "get_login_providers", nil, &out, 20*time.Second); err != nil {
		return []engine.LoginProvider{}, nil
	}
	if out.Providers == nil {
		out.Providers = []engine.LoginProvider{}
	}
	return out.Providers, nil
}

// Login 发起 OAuth（仅 omp）。
func (a *App) Login(ctx context.Context, kind engine.Kind, provider string) error {
	if !kind.Valid() {
		kind = a.ActiveKind()
	}
	if !a.Identity(kind).Caps.Login {
		return pkg.New(pkg.CodeEngineUnsupported,
			"pi 的登录在终端里做：pi auth", "")
	}
	return a.EngineFor(kind, a.Workspace()).Call(ctx, "login", engine.LoginParams(provider), nil, 600*time.Second)
}

// ---------------- 技能 ----------------

// SkillRow 是技能在界面上的形态（带引擎归属）。
type SkillRow struct {
	skill.Skill
	Kind string `json:"kind"`
}

// SkillList 列出某引擎（或全部）的技能。
func (a *App) SkillList(kind engine.Kind) ([]SkillRow, error) {
	kinds := engine.AllKinds()
	if kind.Valid() {
		kinds = []engine.Kind{kind}
	}
	out := []SkillRow{}
	seen := map[string]bool{}
	for _, k := range kinds {
		list, err := a.Skills(k)
		if err != nil {
			continue
		}
		for _, s := range list {
			key := string(k) + "\x00" + s.Path
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, SkillRow{Skill: s, Kind: string(k)})
		}
	}
	return out, nil
}

// ToggleSkill 启停一个技能。
//
// omp 认 frontmatter 的 enabled；pi 没有这个开关，只能靠 disable-model-invocation
// 把它从系统提示里摘掉。所以「关闭」在 pi 上写的键不同——这是能力差异，
// 由 Caps.SkillToggle 告诉界面该不该显示开关。
func (a *App) ToggleSkill(kind engine.Kind, path string, enabled bool) error {
	k := a.KindOr(string(kind))
	layout, err := a.Layout(k)
	if err != nil {
		return err
	}
	// 只允许改用户目录里的那份：工作区与插件来的技能属于别人。
	if !isUnder(path, layout.Skills) {
		return pkg.New(pkg.CodeSkillReadOnly, "只能启停用户目录里的技能", path)
	}
	if k == engine.KindPi {
		return skill.TogglePi(path, enabled)
	}
	return skill.Toggle(path, enabled)
}

// CreateSkill 在用户目录新建一个技能。
func (a *App) CreateSkill(kind engine.Kind, name, description, body string) (string, error) {
	k := a.KindOr(string(kind))
	layout, err := a.Layout(k)
	if err != nil {
		return "", err
	}
	return skill.Create(layout.Skills, name, description, body)
}

// DeleteSkill 删掉一个技能（只允许用户目录）。
func (a *App) DeleteSkill(kind engine.Kind, path string) error {
	k := a.KindOr(string(kind))
	layout, err := a.Layout(k)
	if err != nil {
		return err
	}
	if !isUnder(path, layout.Skills) {
		return pkg.New(pkg.CodeSkillReadOnly, "只能删除用户目录里的技能", path)
	}
	return skill.DeleteByPath(path)
}

// SkillContent 读技能原文。
func (a *App) SkillContent(path string) (string, error) {
	return skill.Content(path)
}

// ---------------- 插件 / 扩展 ----------------

// PluginRow 是一条已装的插件/扩展。
type PluginRow struct {
	Kind    engine.Kind `json:"kind"`
	Name    string      `json:"name"`
	Version string      `json:"version"`
	Source  string      `json:"source"`
	Path    string      `json:"path"`
}

// Plugins 列出已装的插件。
//
// 都走引擎自己的 CLI：omp 有 `omp plugin list --json`，pi 有 `pi list --json`。
// 不自己解 node_modules——包的布局是引擎的私有契约。
func (a *App) Plugins(ctx context.Context, kind engine.Kind) ([]PluginRow, error) {
	kinds := engine.AllKinds()
	if kind.Valid() {
		kinds = []engine.Kind{kind}
	}
	out := []PluginRow{}
	for _, k := range kinds {
		if !a.Identity(k).Caps.Extensions {
			continue
		}
		list, err := a.pluginsOf(ctx, k)
		if err != nil {
			continue
		}
		out = append(out, list...)
	}
	return out, nil
}

func (a *App) pluginsOf(ctx context.Context, kind engine.Kind) ([]PluginRow, error) {
	id := a.Identity(kind)
	if id.Exe == "" {
		return nil, engine.ErrMissing(kind)
	}
	args := []string{"plugin", "list", "--json"}
	if kind == engine.KindPi {
		args = []string{"list", "--json"}
	}
	var raw []struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Source  string `json:"source"`
		Spec    string `json:"spec"`
		Path    string `json:"path"`
	}
	if err := runJSON(ctx, id.Exe, &raw, args...); err != nil {
		return nil, err
	}
	out := make([]PluginRow, 0, len(raw))
	for _, p := range raw {
		source := firstNonEmpty(p.Source, p.Spec, p.Path)
		out = append(out, PluginRow{
			Kind: kind, Name: p.Name, Version: p.Version, Source: source, Path: p.Path,
		})
	}
	return out, nil
}

// InstallPlugin 装一个插件/扩展（走引擎 CLI）。
func (a *App) InstallPlugin(ctx context.Context, kind engine.Kind, source string) (string, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return "", pkg.New(pkg.CodeBadRequest, "给出包名或仓库地址", "")
	}
	k := a.KindOr(string(kind))
	id := a.Identity(k)
	if id.Exe == "" {
		return "", engine.ErrMissing(k)
	}
	args := []string{"plugin", "install", source}
	if k == engine.KindPi {
		args = []string{"install", source}
	}
	out, err := runText(ctx, id.Exe, 10*time.Minute, args...)
	if err != nil {
		return out, pkg.Wrap(pkg.CodePluginFailed, "安装失败", err)
	}
	return out, nil
}

// UninstallPlugin 卸一个插件/扩展。
func (a *App) UninstallPlugin(ctx context.Context, kind engine.Kind, source string) (string, error) {
	k := a.KindOr(string(kind))
	id := a.Identity(k)
	if id.Exe == "" {
		return "", engine.ErrMissing(k)
	}
	args := []string{"plugin", "uninstall", source}
	if k == engine.KindPi {
		args = []string{"remove", source}
	}
	out, err := runText(ctx, id.Exe, 3*time.Minute, args...)
	if err != nil {
		return out, pkg.Wrap(pkg.CodePluginFailed, "卸载失败", err)
	}
	return out, nil
}

// ---------------- 引擎状态 ----------------

// EngineStatus 是「设置 · 引擎」页需要的全部事实。
type EngineStatus struct {
	Engines []EngineInfo      `json:"engines"`
	Running []engine.RunRow   `json:"running"`
	Install map[string]string `json:"install"`
}

// EngineInfo 是单个引擎的探测结果与落盘位置。
type EngineInfo struct {
	engine.Identity
	Installed bool        `json:"installed"`
	Layout    envLayoutVO `json:"layout"`
}

type envLayoutVO struct {
	Root     string `json:"root"`
	Agent    string `json:"agent"`
	Sessions string `json:"sessions"`
	Skills   string `json:"skills"`
	Plugins  string `json:"plugins"`
	Models   string `json:"models"`
	Settings string `json:"settings"`
	Exists   bool   `json:"exists"`
}

// EngineStatus 组装引擎状态。
func (a *App) EngineStatus(ctx context.Context) (*EngineStatus, error) {
	a.detectAll(ctx, false)
	out := &EngineStatus{
		Engines: []EngineInfo{},
		Running: a.Engine.Running(),
		Install: map[string]string{},
	}
	if out.Running == nil {
		out.Running = []engine.RunRow{}
	}
	for _, kind := range engine.AllKinds() {
		id := a.Identity(kind)
		info := EngineInfo{Identity: id, Installed: id.Exe != ""}
		if layout, err := a.Layout(kind); err == nil {
			info.Layout = envLayoutVO{
				Root: layout.Root, Agent: layout.Agent, Sessions: layout.Sessions,
				Skills: layout.Skills, Plugins: layout.Plugins, Models: layout.Models,
				Settings: layout.Settings, Exists: layout.Exists(),
			}
		}
		out.Engines = append(out.Engines, info)
		out.Install[string(kind)] = engine.InstallHint(kind)
	}
	return out, nil
}

// DetectEngines 强制重新探测（设置页的「重新检测」）。
func (a *App) DetectEngines(ctx context.Context) (*EngineStatus, error) {
	a.detectAll(ctx, true)
	return a.EngineStatus(ctx)
}

// RestartEngines 重启引擎（有进程在跑时停掉后重新拉起）。
func (a *App) RestartEngines(ctx context.Context, kind engine.Kind) error {
	if kind.Valid() {
		a.Engine.StopKind(kind)
		_, err := a.Engine.Ensure(ctx, kind, a.Workspace())
		return err
	}
	a.Engine.StopAll()
	for _, k := range engine.AllKinds() {
		if a.Identity(k).Exe == "" {
			continue
		}
		if _, err := a.Engine.Ensure(ctx, k, a.Workspace()); err != nil {
			return err
		}
	}
	return nil
}

// ---------------- 小工具 ----------------

func dirOf(path string) string { return filepath.Dir(path) }

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

func isUnder(path, dir string) bool {
	if path == "" || dir == "" {
		return false
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func toDomainModels(list []ompcfg.ModelEntry) []domain.ModelEntry {
	out := make([]domain.ModelEntry, 0, len(list))
	for _, m := range list {
		out = append(out, domain.ModelEntry{
			ID: m.ID, Name: m.Name, API: m.API, Reasoning: m.Reasoning,
			Input: m.Input, ContextWindow: m.ContextWindow, MaxTokens: m.MaxTokens,
		})
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
