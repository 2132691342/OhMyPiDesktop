package service

// MCP：读写引擎的 mcp.json，并按需向引擎问一次实时状态。
//
// 配置文件的形状两个引擎共用（`mcpServers` 映射），所以读写只有一份代码；
// 差别是「停用一个服务器写哪个键」（omp 用顶层名单，pi 写条目的 enabled），
// 由 mcpcfg 内部按引擎分叉，这里只传一个开关。
//
// 不代跑 `pi mcp add`：`add` 的参数面（OAuth 客户端、回调端口、头部、暴露模式 …）
// 比界面能表达的多，而我们写文件只动自己掌管的那几个键，
// 未知字段原样保留——比转发 CLI 更安全，也不必为每个版本的新参数改一遍前端。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"OhMyPiDesktop/backend/domain"
	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/env/mcpcfg"
	"OhMyPiDesktop/backend/pkg"
)

// ---------------- MCP ----------------

// McpList 列出某引擎的 MCP 服务器。
//
// live 为真时额外问一次引擎的连接状态（pi 有 `mcp list --json`，omp 没有），
// 并把状态并到同一批行上。探测失败不算错误：配置照样给，
// 只是状态栏留空——「引擎没起」不该让整页打不开。
func (a *App) McpList(ctx context.Context, kind engine.Kind, live bool) (*domain.McpListVO, error) {
	k := a.KindOr(string(kind))
	layout, err := a.Layout(k)
	if err != nil {
		return nil, err
	}
	userPath := mcpPathIn(layout.Agent)
	out := &domain.McpListVO{
		EngineKind: string(k),
		Servers:    []domain.McpServerVO{},
		UserPath:   userPath,
	}
	if project, ok := a.projectMcpPath(); ok {
		out.ProjectPath = project
	}

	rows := []domain.McpServerVO{}
	readErrs := []string{}
	// 用户级先读，项目级同名覆盖（与引擎的优先级一致）
	for _, path := range []string{userPath, out.ProjectPath} {
		if path == "" {
			continue
		}
		file, err := mcpcfg.Read(path)
		if err != nil {
			readErrs = append(readErrs, pkg.As(err).Message)
			continue
		}
		scope := scopeOfPath(path, out.ProjectPath)
		for _, name := range file.Names() {
			entry := file.Servers[name]
			enabled, byDeny := file.IsEnabled(name)
			row := domain.McpServerVO{
				ServerName:     name,
				TransportKind:  entry.Transport(),
				Desc:           entry.Description,
				Enabled:        enabled,
				DisabledByDeny: byDeny,
				ScopeKind:      scope,
				ConfigPath:     path,
			}
			if entry.URL != "" {
				row.ServerURL = entry.URL
			}
			if entry.Command != "" {
				row.CommandLine = strings.TrimSpace(entry.Command + " " + strings.Join(entry.Args, " "))
			}
			rows = append(rows, row)
		}
	}
	// 项目级同名覆盖用户级：按名字去重，后出现的赢
	out.Servers = dedupeServers(rows)
	out.ReadErrors = readErrs
	if live {
		a.applyLiveStatus(ctx, k, out)
	}
	return out, nil
}

// applyLiveStatus 把引擎报告的连接状态并到行上。
func (a *App) applyLiveStatus(ctx context.Context, kind engine.Kind, out *domain.McpListVO) {
	report, err := a.mcpLiveReport(ctx, kind)
	if err != nil || report == nil {
		return
	}
	out.LiveChecked = true
	byName := map[string]int{}
	for i, row := range out.Servers {
		byName[row.ServerName] = i
	}
	for _, item := range report.Servers {
		idx, ok := byName[item.Name]
		if !ok {
			continue
		}
		out.Servers[idx].LiveStatus = item.State
		out.Servers[idx].LiveError = item.Error
		out.Servers[idx].ToolNames = item.Tools
	}
	// 引擎报了配置里没有的服务器（比如扩展注册的）：补一行，标成 extension
	for _, item := range report.Servers {
		if _, ok := byName[item.Name]; ok {
			continue
		}
		out.Servers = append(out.Servers, domain.McpServerVO{
			ServerName: item.Name,
			Enabled:    true,
			ScopeKind:  "extension",
			LiveStatus: item.State,
			LiveError:  item.Error,
			ToolNames:  item.Tools,
		})
	}
	for _, text := range report.Errors {
		out.ReadErrors = append(out.ReadErrors, text)
	}
}

// mcpReport 是 `pi mcp list --json` 的输出形状。
type mcpReport struct {
	Servers []struct {
		Name    string   `json:"name"`
		Scope   string   `json:"scope"`
		State   string   `json:"state"`
		Error   string   `json:"error"`
		Tools   []string `json:"tools"`
		Enabled bool     `json:"enabled"`
	} `json:"servers"`
	Errors []string `json:"errors"`
}

// mcpLiveReport 向引擎问一次 MCP 状态。只有 pi 提供这个子命令。
func (a *App) mcpLiveReport(ctx context.Context, kind engine.Kind) (*mcpReport, error) {
	if kind != engine.KindPi {
		return nil, nil
	}
	id := a.Identity(kind)
	if id.Exe == "" {
		return nil, engine.ErrMissing(kind)
	}
	raw, err := runText(ctx, id.Exe, 90*time.Second, "mcp", "list", "--json")
	if err != nil && strings.TrimSpace(raw) == "" {
		return nil, err
	}
	body := extractJSON(raw)
	if body == "" {
		return nil, pkg.New(pkg.CodeProtocol, "引擎没返回 MCP 状态", "")
	}
	var report mcpReport
	if err := json.Unmarshal([]byte(body), &report); err != nil {
		return nil, pkg.Wrap(pkg.CodeProtocol, "MCP 状态无法解析", err)
	}
	return &report, nil
}

// McpSave 新增或覆盖一个 MCP 服务器。
func (a *App) McpSave(req domain.McpSaveREQ) (*domain.McpListVO, error) {
	kind := a.KindOr(req.EngineKind)
	name := strings.TrimSpace(req.ServerName)
	if name == "" {
		return nil, pkg.New(pkg.CodeBadRequest, "服务器名不能为空", "")
	}
	if len(name) > 100 {
		return nil, pkg.New(pkg.CodeBadRequest, "服务器名太长（最多 100 个字符）", "")
	}
	path, err := a.mcpTargetPath(kind, req.TargetProject)
	if err != nil {
		return nil, err
	}
	entry := mcpcfg.Entry{Description: strings.TrimSpace(req.Desc)}
	command := strings.TrimSpace(req.CommandText)
	if command != "" {
		parts := splitCommand(command)
		entry.Command = parts[0]
		if len(parts) > 1 {
			entry.Args = parts[1:]
		}
	}
	if url := strings.TrimSpace(req.ServerURL); url != "" {
		entry.URL = url
		entry.Type = strings.TrimSpace(req.TransportKind)
		if entry.Type == "" {
			entry.Type = "http"
		}
	}
	// 命令与 URL 都没有就没有连接方式：写一个连不上的条目只会让引擎启动时报错
	if entry.Command == "" && entry.URL == "" {
		return nil, pkg.New(pkg.CodeBadRequest, "给一条命令或一个 URL", "")
	}
	if len(req.EnvList) > 0 {
		entry.Env = map[string]string{}
		for _, line := range req.EnvList {
			key, value, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			entry.Env[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	if dir := strings.TrimSpace(req.WorkDir); dir != "" {
		entry.CWD = dir
	}
	if err := mcpcfg.Upsert(path, name, entry); err != nil {
		return nil, err
	}
	a.InvalidateCounts()
	return a.McpList(context.Background(), kind, false)
}

// McpToggle 启停一个 MCP 服务器。
func (a *App) McpToggle(kind engine.Kind, name string, enabled bool) (*domain.McpListVO, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, pkg.New(pkg.CodeBadRequest, "要启停哪个服务器？", "")
	}
	k := a.KindOr(string(kind))
	layout, err := a.Layout(k)
	if err != nil {
		return nil, err
	}
	// 停用时 omp 走顶层名单，pi 写条目上的 enabled
	if err := mcpcfg.SetEnabled(mcpPathIn(layout.Agent), name, enabled, k == engine.KindOMP); err != nil {
		return nil, err
	}
	a.InvalidateCounts()
	return a.McpList(context.Background(), k, false)
}

// McpRemove 删掉一个 MCP 服务器。
func (a *App) McpRemove(kind engine.Kind, name string) (*domain.McpListVO, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, pkg.New(pkg.CodeBadRequest, "要删哪个服务器？", "")
	}
	k := a.KindOr(string(kind))
	layout, err := a.Layout(k)
	if err != nil {
		return nil, err
	}
	if err := mcpcfg.Remove(mcpPathIn(layout.Agent), name); err != nil {
		return nil, err
	}
	a.InvalidateCounts()
	return a.McpList(context.Background(), k, false)
}

// mcpTargetPath 决定写到用户级还是项目级。
//
// 项目级只写到当前工作目录里引擎自己的项目配置位（.omp / .pi）之下：
// 那个目录存在，才说明引擎把这个目录当成项目；往一个随便打开的目录里
// 塞 .omp/mcp.json 等于往别人的文件夹里写配置。
func (a *App) mcpTargetPath(kind engine.Kind, project bool) (string, error) {
	if !project {
		layout, err := a.Layout(kind)
		if err != nil {
			return "", err
		}
		return mcpPathIn(layout.Agent), nil
	}
	path, ok := a.projectMcpPath()
	if !ok {
		return "", pkg.New(pkg.CodeBadRequest,
			"当前目录里没有 "+projectDirName(kind)+" 目录，引擎不会读这里的项目级配置",
			a.Workspace())
	}
	return path, nil
}

// projectDirName 返回该引擎的项目级目录名（.omp / .pi）。
func projectDirName(kind engine.Kind) string {
	if kind == engine.KindPi {
		return ".pi"
	}
	return ".omp"
}

// mcpPathIn 返回某 agent 目录下的 mcp.json。
func mcpPathIn(agentDir string) string { return filepath.Join(agentDir, "mcp.json") }

// projectMcpPath 返回当前工作目录里的项目级 mcp.json；不在仓库里时返回 false。
//
// 判据是「这个目录下有 .omp 或 .pi 目录」：那是引擎自己的项目级配置位，
// 有它才说明这个目录被引擎当成项目。
func (a *App) projectMcpPath() (string, bool) {
	dir := a.Workspace()
	if dir == "" {
		return "", false
	}
	for _, name := range []string{".omp", ".pi"} {
		if st, err := os.Stat(filepath.Join(dir, name)); err == nil && st.IsDir() {
			return filepath.Join(dir, name, "mcp.json"), true
		}
	}
	return "", false
}

// scopeOfPath 判断一份配置是用户级还是项目级。
func scopeOfPath(path, projectPath string) string {
	if projectPath != "" && strings.EqualFold(filepath.Clean(path), filepath.Clean(projectPath)) {
		return "project"
	}
	return "user"
}

// dedupeServers 按名字去重，后出现的覆盖先出现的（项目级压用户级）。
func dedupeServers(rows []domain.McpServerVO) []domain.McpServerVO {
	idx := map[string]int{}
	out := make([]domain.McpServerVO, 0, len(rows))
	for _, row := range rows {
		if at, ok := idx[row.ServerName]; ok {
			out[at] = row
			continue
		}
		idx[row.ServerName] = len(out)
		out = append(out, row)
	}
	return out
}

// splitCommand 把一条命令行切成命令与参数。
//
// 支持引号：MCP 的 stdio 命令里带空格的参数很常见（路径、
// `-y @modelcontextprotocol/server-filesystem` 这类包名的参数没有，但路径有）。
func splitCommand(line string) []string {
	var out []string
	var cur strings.Builder
	quoted := false
	quoteChar := byte(0)
	flush := func() {
		text := strings.TrimSpace(cur.String())
		if text != "" {
			out = append(out, text)
		}
		cur.Reset()
	}
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quoted:
			if c == quoteChar {
				quoted = false
				continue
			}
			cur.WriteByte(c)
		case c == '"' || c == '\'':
			quoted = true
			quoteChar = c
		case c == ' ' || c == '\t':
			flush()
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return out
}
