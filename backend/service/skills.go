package service

// 技能：列出引擎真正会加载的技能、启停、新建、改正文。
//
// 读的是引擎自己的技能目录，不另存一份清单——两本账迟早分叉。
// 写只允许落在用户目录：工作区与插件来的那一份属于仓库或包，改了就是本地漂移。

import (
	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/env"
	"OhMyPiDesktop/backend/pkg"
	"OhMyPiDesktop/backend/skill"

	"os"
	"path/filepath"
	"strings"
)

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

// SaveSkillContent 覆写一份技能正文。
//
// 只允许改用户目录里的那份：工作区与插件来的技能属于别人（跟着仓库或包版本走），
// 改了就变成「本地漂移」，下次拉代码或装包时静默被覆盖。
func (a *App) SaveSkillContent(kind engine.Kind, path, text string) (string, error) {
	k := a.KindOr(string(kind))
	layout, err := a.Layout(k)
	if err != nil {
		return "", err
	}
	if !isUnder(path, layout.Skills) {
		return "", pkg.New(pkg.CodeSkillReadOnly, "只能改用户目录里的技能", path)
	}
	if err := skill.SaveContent(path, text); err != nil {
		return "", err
	}
	return text, nil
}

// ---------------- 技能 ----------------

// Skills 列出某引擎会加载的技能。
func (a *App) Skills(kind engine.Kind) ([]skill.Skill, error) {
	layout, err := a.Layout(kind)
	if err != nil {
		return nil, err
	}
	roots := skill.Roots{
		Kind:        kind,
		UserDir:     layout.Skills,
		ManagedDir:  layout.Managed,
		ProjectDir:  layout.ProjectDir,
		ProjectDirs: a.projectSkillDirs(layout.ProjectDir),
		PluginDirs:  a.pluginSkillDirs(layout),
	}
	return skill.Scan(roots)
}

// projectSkillDirs 找出工作目录到仓库边界上每一级的项目级技能目录。
func (a *App) projectSkillDirs(projectDir string) []string {
	dir := a.Workspace()
	var out []string
	for i := 0; i < 8 && dir != ""; i++ {
		candidate := filepath.Join(dir, projectDir, "skills")
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			out = append(out, candidate)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return out
}

// pluginSkillDirs 找出已装包自带的 skills 目录。
//
// 两个引擎的包根不同（omp: ~/.omp/plugins/node_modules；pi: ~/.pi/agent/npm），
// 但都是 node_modules 布局，所以扫描逻辑共用。
func (a *App) pluginSkillDirs(layout env.Layout) []string {
	roots := []string{
		filepath.Join(layout.Plugins, "node_modules"),
		layout.Plugins,
	}
	var out []string
	seen := map[string]bool{}
	for _, nodeModules := range roots {
		entries, err := os.ReadDir(nodeModules)
		if err != nil {
			continue
		}
		walk := func(dir string) {
			sub, err := os.ReadDir(dir)
			if err != nil {
				return
			}
			for _, e := range sub {
				if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
					continue
				}
				cand := filepath.Join(dir, e.Name(), "skills")
				if st, err := os.Stat(cand); err == nil && st.IsDir() && !seen[cand] {
					seen[cand] = true
					out = append(out, cand)
				}
			}
		}
		walk(nodeModules)
		// scoped 包：node_modules/@scope/pkg/skills
		for _, e := range entries {
			if e.IsDir() && strings.HasPrefix(e.Name(), "@") {
				walk(filepath.Join(nodeModules, e.Name()))
			}
		}
	}
	return out
}
