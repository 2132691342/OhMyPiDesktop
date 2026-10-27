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
	Kind      engine.Kind `json:"kind"`
	Root      string      `json:"root"`       // ~/.omp 或 ~/.pi
	Agent     string      `json:"agent"`      // 当前 profile 的 agent 目录
	Sessions  string      `json:"sessions"`   // 会话目录
	Skills    string      `json:"skills"`     // 用户级技能目录
	Managed   string      `json:"managed"`    // 托管技能目录（仅 omp 有）
	Plugins   string      `json:"plugins"`    // 插件/包根目录
	Models    string      `json:"models"`     // models.yml / models.json
	Settings  string      `json:"settings"`   // config.yml / settings.json
	AuthFile  string      `json:"auth_file"`  // auth.json（仅 pi）
	ProjectDir string     `json:"project_dir"` // 工作区里的项目级目录名（.omp/.pi）
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
