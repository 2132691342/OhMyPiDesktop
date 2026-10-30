// Package env 管两个引擎的「装在哪、配置放哪、怎么读写那份配置」。
//
// 边界：引擎自己拥有配置语义（models / settings / sessions），本包只做
// 「找到那几份文件」与「按引擎自己的 schema 读写」。不自造第二套格式。
package env

import (
	"os"
	"path/filepath"
	"strings"

	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/pkg"
)

// Layout 是一个引擎的落盘布局。
//
// 两个引擎的目录名与文件名都不同（.omp/agent vs .pi/agent，
// models.yml vs models.json），所以这里是**按引擎分支的唯一地方**，
// 上层只拿这份结构，不再拼路径。
type Layout struct {
	Kind       engine.Kind `json:"kind"`
	Root       string      `json:"root"`        // ~/.omp 或 ~/.pi
	Agent      string      `json:"agent"`       // 当前 profile 的 agent 目录
	Sessions   string      `json:"sessions"`    // 会话目录
	Skills     string      `json:"skills"`      // 用户级技能目录
	Managed    string      `json:"managed"`     // 托管技能目录（仅 omp 有）
	Plugins    string      `json:"plugins"`     // 插件/包根目录
	Models     string      `json:"models"`      // models.yml / models.json
	Settings   string      `json:"settings"`    // config.yml / settings.json
	AuthFile   string      `json:"auth_file"`   // auth.json（仅 pi）
	ProjectDir string      `json:"project_dir"` // 工作区里的项目级目录名（.omp/.pi）
}

// ResolveLayout 按引擎与 profile 解析布局。
//
// profile 只有 omp 用（~/.omp/profiles/<name>/agent）；pi 没有 profile 概念，
// 传什么都返回默认目录——这样调用方不必自己判断引擎。
func ResolveLayout(kind engine.Kind, profile string) (Layout, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Layout{}, pkg.Wrap(pkg.CodeInternal, "读不到用户目录", err)
	}
	if kind == engine.KindPi {
		return piLayout(home), nil
	}
	return ompLayout(home, profile), nil
}

func ompLayout(home, profile string) Layout {
	root := filepath.Join(home, ".omp")
	if dir := strings.TrimSpace(os.Getenv("OMP_CONFIG_DIR")); dir != "" {
		root = dir
	}
	agent := filepath.Join(root, "agent")
	if p := strings.TrimSpace(profile); p != "" && p != "default" {
		agent = filepath.Join(root, "profiles", p, "agent")
	}
	return Layout{
		Kind:       engine.KindOMP,
		Root:       root,
		Agent:      agent,
		Sessions:   filepath.Join(agent, "sessions"),
		Skills:     filepath.Join(agent, "skills"),
		Managed:    filepath.Join(agent, "managed-skills"),
		Plugins:    filepath.Join(root, "plugins"),
		Models:     filepath.Join(agent, "models.yml"),
		Settings:   filepath.Join(agent, "config.yml"),
		ProjectDir: ".omp",
	}
}

func piLayout(home string) Layout {
	// pi 的 agent 目录可用 PI_CODING_AGENT_DIR 覆盖；没有 profile 概念。
	agent := filepath.Join(home, ".pi", "agent")
	if dir := strings.TrimSpace(os.Getenv("PI_CODING_AGENT_DIR")); dir != "" {
		agent = dir
	}
	root := filepath.Dir(agent)
	return Layout{
		Kind:       engine.KindPi,
		Root:       root,
		Agent:      agent,
		Sessions:   filepath.Join(agent, "sessions"),
		Skills:     filepath.Join(agent, "skills"),
		Managed:    "",
		Plugins:    filepath.Join(agent, "npm"),
		Models:     filepath.Join(agent, "models.json"),
		Settings:   filepath.Join(agent, "settings.json"),
		AuthFile:   filepath.Join(agent, "auth.json"),
		ProjectDir: ".pi",
	}
}

// Layouts 返回两个引擎的布局（Bootstrap 一次给全）。
func Layouts(profile string) map[engine.Kind]Layout {
	out := map[engine.Kind]Layout{}
	for _, kind := range []engine.Kind{engine.KindOMP, engine.KindPi} {
		if l, err := ResolveLayout(kind, profile); err == nil {
			out[kind] = l
		}
	}
	return out
}

// Exists 判断某个布局的 agent 目录是否真的存在（「装过」的弱证据）。
func (l Layout) Exists() bool {
	st, err := os.Stat(l.Agent)
	return err == nil && st.IsDir()
}

// InstallCandidates 列出某引擎「官方安装器会把可执行文件放在哪」的候选路径。
//
// 为什么需要它：两个引擎的官方安装器都**不往 PATH 里写东西**（omp 装进
// `%LOCALAPPDATA%\omp`，pi 装进托管布局的 releases 目录），用户按官方文档
// 一路装完、终端里也能跑，而只看 PATH 的应用会报「未安装」——
// 然后建议用户再装一遍。这是最容易被当成「这个软件连装没装都看不出来」的那种错。
//
// 候选只包含「按官方安装器的落点推出来的确切路径」，不做目录扫描：
// 扫盘会把别的项目里同名的二进制也当成引擎。
func InstallCandidates(kind engine.Kind) []string {
	if kind == engine.KindOMP {
		dirs := []string{}
		if dir := strings.TrimSpace(os.Getenv("PI_INSTALL_DIR")); dir != "" {
			dirs = append(dirs, dir)
		}
		if local := localAppData(); local != "" {
			dirs = append(dirs, filepath.Join(local, "omp"))
		}
		if home, err := os.UserHomeDir(); err == nil {
			dirs = append(dirs, filepath.Join(home, ".omp", "bin"))
		}
		out := make([]string, 0, len(dirs))
		for _, dir := range dirs {
			out = append(out, filepath.Join(dir, engine.ExeName(kind)))
		}
		return out
	}
	return piManagedExeCandidates()
}

// piManagedExeCandidates 按 pi 官方安装器的托管布局列出候选。
//
// 布局是 `<root>/releases/<version>/node_modules/.bin/pi.cmd` + `<root>/current-version`，
// 而 `current-version` 里写着当前启用的是哪一个 release。先读它、再回退到
// 目录里最新的那个，与引擎自己的选版规则一致——猜错版本号比猜错目录更常见。
func piManagedExeCandidates() []string {
	roots := []string{}
	if root := strings.TrimSpace(os.Getenv("PI_MANAGED_INSTALL_ROOT")); root != "" {
		roots = append(roots, root)
	}
	if local := localAppData(); local != "" {
		roots = append(roots, filepath.Join(local, "pi"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, ".pi", "install"))
	}
	out := make([]string, 0, len(roots)*2)
	for _, root := range roots {
		if !dirExists(root) {
			continue
		}
		if version := strings.TrimSpace(readSmallFile(filepath.Join(root, "current-version"))); version != "" {
			out = append(out, filepath.Join(root, "releases", version, "node_modules", ".bin", "pi.cmd"))
		}
		if latest := newestReleaseDir(filepath.Join(root, "releases")); latest != "" {
			out = append(out, filepath.Join(latest, "node_modules", ".bin", "pi.cmd"))
		}
	}
	return out
}

// newestReleaseDir 返回 releases 目录里名字排序最大的那一项。
//
// 版本号是 `1.4.0` 这种形如 semver 的字符串，字典序在同层级版本上正确；
// 用不上完整排序——它只是 current-version 缺失时的个兜底。
func newestReleaseDir(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	best := ""
	for _, e := range entries {
		if e.IsDir() && e.Name() > best {
			best = e.Name()
		}
	}
	if best == "" {
		return ""
	}
	return filepath.Join(dir, best)
}

// readSmallFile 读一个只用来放一行字的文件；读不到返回空串。
func readSmallFile(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) > 128 {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func dirExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}
