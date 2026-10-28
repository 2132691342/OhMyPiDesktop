package service

// 引擎的装 / 升级 / 卸载。
//
// 边界（AGENTS.md §0）：我们**只代跑引擎自己的安装程序**，从不自己下载、解压、
// 校验、写盘。所以这里的每一条命令都是一行可以照抄到终端里的原文，
// 界面上显示的命令与实际执行的是同一个字符串。
//
// 三件事在这里收口：
//  1. 命令从哪来（engine 包，纯数据）；
//  2. 命令能不能发出去（包管理器在不在、上一个长任务跑完没有）；
//  3. 输出怎么到界面（hub 推 app:progress，逐行追加）。

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"time"

	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/pkg"
)

// appsMu 让同一个应用同时只有一条长任务。
//
// 两个人同时点「安装」的结果是两条 npm 抢同一个全局目录，失败信息还是
// 互相覆盖的——宁可明确告诉用户「上一个还没跑完」。
type appsMu struct {
	mu   sync.Mutex
	busy map[engine.Kind]bool
}

var running = &appsMu{busy: map[engine.Kind]bool{}}

// AppRow 是「应用」页的一行：一个引擎的安装事实与可做的动作。
type AppRow struct {
	Kind engine.Kind `json:"kind"`
	// AppName 给人看的名字（界面标题用，不参与分支）
	AppName string `json:"app_name"`
	// Installed 是否探测到可执行文件
	Installed bool `json:"installed"`
	// Version 本地版本；未安装为空
	Version string `json:"version"`
	// Exe 可执行文件绝对路径；未安装为空
	Exe string `json:"exe"`
	// Source 从哪找到的：path / custom
	Source string `json:"source"`
	// Channel 安装渠道：npm / pnpm / bun / omp-installer / pi-installer / unknown
	Channel engine.Channel `json:"channel"`
	// ChannelLabel 渠道的人话说明（界面直接显示）
	ChannelLabel string `json:"channel_label"`
	// LatestVersion 上游最新版；查不到为空
	LatestVersion string `json:"latest_version"`
	// LatestNote 查不到最新版的原因（一句话）
	LatestNote string `json:"latest_note,omitempty"`
	// Outdated 本地版本落后于上游（两端都可解析时才为真）
	Outdated bool `json:"outdated"`

	// InstallCommand 安装命令原文（也用于「重装 / 换渠道」）
	InstallCommand string `json:"install_command"`
	// UpdateCommand 升级命令原文；空表示这个引擎没有自升级能力
	UpdateCommand string `json:"update_command"`
	// UninstallCommand 卸载命令原文；空表示这条渠道没有非交互式卸载
	UninstallCommand string `json:"uninstall_command"`
	// DocsURL 官方文档
	DocsURL string `json:"docs_url"`

	// CanInstall 现在能不能点安装（包管理器在、没有别的任务在跑）
	CanInstall bool `json:"can_install"`
	// CanUpdate 现在能不能点升级：已安装、有新版本、命令可执行
	CanUpdate bool `json:"can_update"`
	// CanUninstall 现在能不能点卸载：渠道可识别且命令可执行
	CanUninstall bool `json:"can_uninstall"`
	// Blocker 不能操作的原因（一句话，空表示没阻碍）
	Blocker string `json:"blocker,omitempty"`
	// Busy 这个应用当前有长任务在跑
	Busy bool `json:"busy"`
}

// Apps 汇总「应用」页需要的全部事实。
//
// 探测结果直接用缓存（一分钟），最新版查询走 30 分钟缓存：
// 进一次应用页不该起两个子进程、发三次外网请求。
func (a *App) Apps(ctx context.Context, fore bool) ([]AppRow, error) {
	a.detectAll(ctx, fore)
	latest := engine.QueryLatest(ctx, fore)
	out := make([]AppRow, 0, 2)
	for _, kind := range engine.AllKinds() {
		out = append(out, a.appRow(ctx, kind, latest[kind]))
	}
	return out, nil
}

// appRow 拼一行。所有「能不能点」都在这里算完，界面不做第二次判断。
func (a *App) appRow(ctx context.Context, kind engine.Kind, latest engine.Latest) AppRow {
	id := a.Identity(kind)
	row := AppRow{
		Kind:           kind,
		AppName:        kind.Label(),
		Installed:      id.Exe != "",
		Version:        id.Version,
		Exe:            id.Exe,
		Source:         id.Source,
		Channel:        id.Channel,
		ChannelLabel:   channelLabel(id.Channel),
		LatestVersion:  latest.Version,
		LatestNote:     latest.Note,
		InstallCommand: installCommandText(kind),
		DocsURL:        engine.DocsURL(kind),
		Busy:           isBusy(kind),
	}
	if id.Channel == "" {
		row.Channel = engine.ChannelUnknown
		row.ChannelLabel = channelLabel(engine.ChannelUnknown)
	}

	if args := engine.UpdateArgs(kind); len(args) > 0 && id.Exe != "" {
		row.UpdateCommand = id.Exe + " " + strings.Join(args, " ")
	}
	if cmd := engine.UninstallCommand(kind, id.Channel); len(cmd) > 0 {
		row.UninstallCommand = strings.Join(cmd, " ")
	}

	row.Outdated = id.Version != "" && latest.Version != "" &&
		compareVersion(id.Version, latest.Version) < 0

	// 安装永远可以试（除非有别的任务在跑）：已经装了也能重装 / 换渠道。
	row.CanInstall = !row.Busy && installRunnable(kind) == nil
	if err := installRunnable(kind); err != nil {
		row.Blocker = err.Error()
	}
	row.CanUpdate = row.Installed && row.UpdateCommand != "" && !row.Busy && row.Outdated
	row.CanUninstall = row.Installed && row.UninstallCommand != "" && !row.Busy
	if row.Installed && row.UninstallCommand == "" && row.Blocker == "" {
		row.Blocker = uninstallBlocker(id.Channel)
	}
	return row
}

// installRunnable 回答「这台机器现在能不能跑安装命令」。
//
// 命令不存在就不发出去（AGENTS.md §3）：直接说缺哪个包管理器，
// 而不是让用户看一条 "exec: npm: executable file not found"。
func installRunnable(kind engine.Kind) error {
	if engine.InstallerScript(kind) != "" {
		if _, err := exec.LookPath("powershell"); err != nil {
			return pkg.New(pkg.CodeInstallUnavailable,
				"找不到 PowerShell，装不了 "+kind.Label()+"。请在终端里跑一次："+engine.InstallHint(kind), "")
		}
		return nil
	}
	if _, err := exec.LookPath("npm"); err != nil {
		return pkg.New(pkg.CodeInstallUnavailable,
			"找不到 npm，装不了 "+kind.Label()+"。装好 Node.js 再回来，或自己跑一次："+engine.InstallHint(kind), "")
	}
	return nil
}

// appProgressEvent 是安装进度的唯一事件名。
//
// 与 chat:* / agents:* 不同，它不挂在某个会话上（session_id 为空）：
// 装引擎与应用状态无关，任何页面都可能要看它。
const appProgressEvent = "app:progress"

// AppAction 是一次装 / 升级 / 卸载的请求。
type AppAction string

const (
	// ActionInstall 跑安装命令（已装则等于重装 / 换渠道）
	ActionInstall AppAction = "install"
	// ActionUpdate 跑引擎自己的升级子命令
	ActionUpdate AppAction = "update"
	// ActionUninstall 跑包管理器的全局卸载
	ActionUninstall AppAction = "uninstall"
)

// Valid 判断动作名是不是已知的。
func (a AppAction) Valid() bool {
	return a == ActionInstall || a == ActionUpdate || a == ActionUninstall
}

// RunAppAction 跑一条长任务，把输出逐行推给界面。
//
// 同步返回：调用方（HTTP handler）等它结束再回响应，前端靠 SSE 看进度。
// 不另开一个「查询任务状态」的接口——一个动作一个结果，进度走事件流。
func (a *App) RunAppAction(ctx context.Context, kind engine.Kind, action AppAction) (*AppRow, error) {
	if !kind.Valid() {
		return nil, pkg.New(pkg.CodeBadRequest, "不认识的引擎："+string(kind), "")
	}
	if !action.Valid() {
		return nil, pkg.New(pkg.CodeBadRequest, "不认识的操作："+string(action), "")
	}
	if !claim(kind) {
		return nil, pkg.New(pkg.CodeAppBusy, kind.Label()+" 还有一个任务在跑，等它结束再试", "")
	}
	defer release(kind)

	cmdline, err := a.actionCommand(kind, action)
	if err != nil {
		return nil, err
	}
	text := strings.Join(cmdline, " ")
	a.emitAppStart(kind, action, text)
	if err := runStreaming(ctx, a.Hub, kind, action, cmdline); err != nil {
		// 任务失败时强制重探：装了一半的目录不该继续显示「已安装 1.2.3」。
		a.detectAll(ctx, true)
		return nil, err
	}
	// 任务成功时也重探：否则界面还显示旧版本，用户会以为没生效。
	//
	// 用不带超时的背景 context 重探与查最新版：请求自己的 ctx 可能已经
	// 因为客户端断开而取消，那时元数据没刷新，界面就会停在旧状态。
	fresh := context.WithoutCancel(ctx)
	a.detectAll(fresh, true)
	engine.QueryLatest(fresh, true)
	return &AppRow{Kind: kind}, nil
}

// actionCommand 把动作翻成一条可执行命令行。
func (a *App) actionCommand(kind engine.Kind, action AppAction) ([]string, error) {
	switch action {
	case ActionInstall:
		if err := installRunnable(kind); err != nil {
			return nil, err
		}
		if script := engine.InstallerScript(kind); script != "" {
			return []string{"powershell", "-NoProfile", "-NonInteractive",
				"-ExecutionPolicy", "Bypass", "-Command", script}, nil
		}
		return append([]string{"npm"}, engine.NpmInstallArgs(kind)...), nil

	case ActionUpdate:
		id := a.Identity(kind)
		if id.Exe == "" {
			return nil, engine.ErrMissing(kind)
		}
		args := engine.UpdateArgs(kind)
		if len(args) == 0 {
			return nil, pkg.New(pkg.CodeEngineUnsupported,
				kind.Label()+" 没有自升级命令，请重跑安装命令："+engine.InstallHint(kind), "")
		}
		return append([]string{id.Exe}, args...), nil

	case ActionUninstall:
		id := a.Identity(kind)
		if id.Exe == "" {
			return nil, engine.ErrMissing(kind)
		}
		cmd := engine.UninstallCommand(kind, id.Channel)
		if len(cmd) == 0 {
			return nil, pkg.New(pkg.CodeEngineUnsupported, uninstallBlocker(id.Channel), "")
		}
		if _, err := exec.LookPath(cmd[0]); err != nil {
			return nil, pkg.New(pkg.CodeInstallUnavailable,
				"找不到 "+cmd[0]+"，没法卸载。可以手动删除："+id.Exe, "")
		}
		return cmd, nil
	}
	return nil, pkg.New(pkg.CodeBadRequest, "不认识的操作", "")
}

// ---------------- 事件 ----------------

// AppProgress 是一帧安装进度。逐行推，前端 append 到日志面板。
type AppProgress struct {
	Kind   engine.Kind `json:"kind"`
	Action AppAction   `json:"action"`
	// Phase start / line / done / fail
	Phase string `json:"phase"`
	// Line 命令输出的一行（phase=line）
	Line string `json:"line,omitempty"`
	// Command 要跑的命令原文（phase=start）
	Command string `json:"command,omitempty"`
	// Message 失败原因（phase=fail）
	Message string `json:"message,omitempty"`
}

// emitAppStart 推开始帧。命令单独一个字段：前端要把「这条命令」贴在日志上方，
// 而 line 是输出正文——两件事放同一个字段就会出现「命令被当成第一行输出」。
func (a *App) emitAppStart(kind engine.Kind, action AppAction, command string) {
	if a.Hub == nil {
		return
	}
	a.Hub.Emit("", appProgressEvent, AppProgress{Kind: kind, Action: action, Phase: "start", Command: command})
}

// ---------------- 小工具 ----------------

// claim 占用一个应用的执行位。
func claim(kind engine.Kind) bool {
	running.mu.Lock()
	defer running.mu.Unlock()
	if running.busy[kind] {
		return false
	}
	running.busy[kind] = true
	return true
}

func release(kind engine.Kind) {
	running.mu.Lock()
	defer running.mu.Unlock()
	delete(running.busy, kind)
}

func isBusy(kind engine.Kind) bool {
	running.mu.Lock()
	defer running.mu.Unlock()
	return running.busy[kind]
}

// installCommandText 是给人看的那一行（与实际执行的命令同源）。
func installCommandText(kind engine.Kind) string {
	if script := engine.InstallerScript(kind); script != "" {
		return "powershell -Command \"" + script + "\""
	}
	return "npm " + strings.Join(engine.NpmInstallArgs(kind), " ")
}

// channelLabel 把渠道翻成人话。界面直接显示，不在前端再写一份对照表。
func channelLabel(channel engine.Channel) string {
	switch channel {
	case engine.ChannelNPM:
		return "npm 全局安装"
	case engine.ChannelPNPM:
		return "pnpm 全局安装"
	case engine.ChannelBun:
		return "bun 全局安装"
	case engine.ChannelOMPInstaller:
		return "omp 官方安装器"
	case engine.ChannelPiInstaller:
		return "pi 官方安装器"
	default:
		return "渠道未知"
	}
}

// uninstallBlocker 解释「为什么没有卸载按钮」。
//
// 这一句是必需的：一个装着的应用没有卸载按钮，用户只会以为漏了。
func uninstallBlocker(channel engine.Channel) string {
	switch channel {
	case engine.ChannelOMPInstaller:
		return "omp 的官方安装器没有非交互式卸载，请删掉 %LOCALAPPDATA%\\omp 目录"
	case engine.ChannelPiInstaller:
		return "pi 的官方安装器只在交互模式里提供卸载，请重跑安装器并按 u"
	default:
		return "看不出这个文件是谁装的，为避免误删不提供卸载"
	}
}

// compareVersion 比较两个版本号，忽略前缀与预发布后缀。
//
// 只用到「谁更新」这一个判断，所以按点分段比数字即可；
// 预发布后缀一律当同版本（宁可少提示一次升级，也不要提示一个不存在的版本）。
func compareVersion(a, b string) int {
	as, bs := versionParts(a), versionParts(b)
	for i := 0; i < len(as) || i < len(bs); i++ {
		var av, bv int
		if i < len(as) {
			av = as[i]
		}
		if i < len(bs) {
			bv = bs[i]
		}
		if av != bv {
			if av < bv {
				return -1
			}
			return 1
		}
	}
	return 0
}

// versionParts 抽出点分段里的数字（`omp/18.13.2` → [18 13 2]）。
func versionParts(v string) []int {
	if i := strings.LastIndexAny(v, "/ "); i >= 0 {
		v = v[i+1:]
	}
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	out := []int{}
	for _, seg := range strings.Split(v, ".") {
		n := 0
		ok := false
		for _, r := range seg {
			if r < '0' || r > '9' {
				break
			}
			n = n*10 + int(r-'0')
			ok = true
		}
		if !ok {
			break
		}
		out = append(out, n)
	}
	return out
}

// appActionTimeout 是长任务的上限。装一个引擎可能几分钟，卡死不能永久占位。
const appActionTimeout = 20 * time.Minute
