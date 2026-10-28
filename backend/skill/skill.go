package skill

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/pkg"
)

// 技能：omp 按目录发现 SKILL.md，本包按同样的规则读出来给界面看。
//
// 读的是 omp 真正会加载的位置，不是另建一份清单——那样两本账迟早分叉。
// 启停靠改 frontmatter 的 enabled 字段（与 omp 的约定一致），不移动目录：
// 移动目录会让 omp 的缓存键变化，也让「我明明装过」的困惑变多。

// Skill 是一条技能。
type Skill struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Path        string   `json:"path"` // SKILL.md 绝对路径
	Dir         string   `json:"dir"`
	Source      string   `json:"source"` // user / project / plugin
	Globs       []string `json:"globs,omitempty"`
	AlwaysApply bool     `json:"always_apply,omitempty"`
	Hidden      bool     `json:"hidden,omitempty"`
	Enabled     bool     `json:"enabled"`
	Editable    bool     `json:"editable"` // 只允许改用户目录的那份
}

// Roots 是技能发现根。
//
// 两个引擎的规则不同但可以共用一份扫描：pi 按 Agents Skills 规范认
// `SKILL.md` + 目录，omp 同源；差别在「有哪些根」与「怎么启停」，
// 由调用方把 Root 填对。
type Roots struct {
	Kind        engine.Kind // 决定 frontmatter 的语义（omp 认 enabled，pi 认 disable-model-invocation）
	UserDir     string      // 用户级技能目录
	ManagedDir  string      // 托管技能目录（仅 omp）
	ProjectDir  string      // 工作区里的项目级目录名（.omp / .pi）
	PluginDirs  []string    // 插件包自带的 skills 目录
	ProjectDirs []string    // 工作区里每一级的项目级技能目录
}

// Scan 按顺序扫根目录，同名以后出现的覆盖先出现的（与 omp 的优先级一致）。
func Scan(roots Roots) ([]Skill, error) {
	seen := map[string]int{}
	out := []Skill{}
	add := func(dir, source string, editable bool) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			file := filepath.Join(dir, entry.Name(), "SKILL.md")
			sk, err := parseSkill(file, source, editable)
			if err != nil {
				continue
			}
			if idx, ok := seen[sk.Name]; ok {
				out[idx] = *sk
				continue
			}
			seen[sk.Name] = len(out)
			out = append(out, *sk)
		}
	}
	for _, dir := range roots.ProjectDirs {
		add(dir, "project", false)
	}
	add(roots.UserDir, "user", true)
	add(roots.ManagedDir, "managed", false)
	for _, dir := range roots.PluginDirs {
		add(dir, "plugin", false)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return sourceRank(out[i].Source) < sourceRank(out[j].Source)
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func sourceRank(s string) int {
	switch s {
	case "project":
		return 0
	case "user":
		return 1
	case "managed":
		return 2
	case "plugin":
		return 3
	}
	return 9
}

// parseSkill 读 SKILL.md 的 frontmatter。
func parseSkill(path, source string, editable bool) (*Skill, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sk := &Skill{
		Path:     path,
		Dir:      filepath.Dir(path),
		Source:   source,
		Enabled:  true,
		Editable: editable,
		Name:     filepath.Base(filepath.Dir(path)),
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	inFront := false
	first := true
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if first {
			first = false
			if trimmed == "---" {
				inFront = true
				continue
			}
			return sk, nil // 没有 frontmatter，用目录名当名字
		}
		if inFront {
			if trimmed == "---" {
				break
			}
			key, value, ok := strings.Cut(trimmed, ":")
			if !ok {
				continue
			}
			key = strings.TrimSpace(strings.ToLower(key))
			value = strings.TrimSpace(value)
			value = strings.Trim(value, `"'`)
			switch key {
			case "name":
				if value != "" {
					sk.Name = value
				}
			case "description":
				sk.Description = value
			case "globs":
				sk.Globs = parseList(value)
			case "alwaysapply", "always_apply":
				sk.AlwaysApply = value == "true"
			case "hide":
				sk.Hidden = value == "true"
			case "enabled":
				sk.Enabled = value != "false"
			}
		}
	}
	return sk, nil
}

func parseList(v string) []string {
	v = strings.Trim(v, "[]")
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.Trim(strings.TrimSpace(p), `"'`)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Content 读技能正文（含 frontmatter，界面直接显示原文）。
func Content(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", pkg.Wrap(pkg.CodeNotFound, "读不到技能文件", err)
	}
	return string(raw), nil
}

// contentLimit 是技能正文的上限。
//
// 技能是一份写给模型看的提示词，几 KB 到几十 KB；上限主要防「有人把整个仓库
// 塞进 SKILL.md 然后把界面卡死」。
const contentLimit = 256 * 1024

// SaveContent 覆写技能正文。
//
// 原子写（临时文件 + Rename）：写到一半断电会留下半个 SKILL.md，
// 而那个文件是引擎启动时一定会读的，坏了就是「应用能起、技能全错」。
func SaveContent(path, text string) error {
	path = filepath.Clean(path)
	if filepath.Base(path) != "SKILL.md" {
		return pkg.New(pkg.CodeSkillInvalid, "只能改 SKILL.md", path)
	}
	if len(text) > contentLimit {
		return pkg.New(pkg.CodeSkillInvalid, "技能正文超过 256 KB，请拆成多份", path)
	}
	if _, err := os.Stat(path); err != nil {
		return pkg.Wrap(pkg.CodeNotFound, "这个技能文件不在了", err)
	}
	if err := pkg.WriteFileAtomic(path, []byte(text), 0o644); err != nil {
		return pkg.Wrap(pkg.CodeSkillReadOnly, "这个技能目录不可写", err)
	}
	return nil
}

// Toggle 改 frontmatter 的 enabled。只动这一行，其余原文保持不变。
func Toggle(path string, enabled bool) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return pkg.Wrap(pkg.CodeNotFound, "读不到技能文件", err)
	}
	lines := strings.Split(string(raw), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return pkg.New(pkg.CodeSkillInvalid, "这个技能没有 frontmatter，无法启停", path)
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return pkg.New(pkg.CodeSkillInvalid, "这个技能的 frontmatter 不完整", path)
	}
	value := "true"
	if !enabled {
		value = "false"
	}
	replaced := false
	for i := 1; i < end; i++ {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(lines[i])), "enabled:") {
			indent := lines[i][:len(lines[i])-len(strings.TrimLeft(lines[i], " \t"))]
			lines[i] = indent + "enabled: " + value
			replaced = true
			break
		}
	}
	if !replaced {
		// 没有这个键就补一个，放在 frontmatter 末尾
		lines = append(lines[:end], append([]string{"enabled: " + value}, lines[end:]...)...)
	}
	return pkg.WriteFileAtomic(path, []byte(strings.Join(lines, "\n")), 0o644)
}

// TogglePi 启停一个 pi 技能。
//
// pi 没有 frontmatter 的 enabled（那是 omp 的约定）。它只有
// `disable-model-invocation`：置 true 表示不进系统提示、只能靠 `/skill:name`
// 显式调用。所以「关掉」在 pi 上等于「不让模型自己用」——语义差异如实写在键上，
// 不假装两家有同一个开关。
func TogglePi(path string, enabled bool) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return pkg.Wrap(pkg.CodeNotFound, "读不到技能文件", err)
	}
	lines := strings.Split(string(raw), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return pkg.New(pkg.CodeSkillInvalid, "这个技能没有 frontmatter，无法启停", path)
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return pkg.New(pkg.CodeSkillInvalid, "这个技能的 frontmatter 不完整", path)
	}
	idx := -1
	for i := 1; i < end; i++ {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(lines[i])), "disable-model-invocation:") {
			idx = i
			break
		}
	}
	if enabled {
		// 启用 = 删掉这个键（默认就是让模型看见）。
		if idx >= 0 {
			lines = append(lines[:idx], lines[idx+1:]...)
		}
	} else if idx >= 0 {
		indent := lines[idx][:len(lines[idx])-len(strings.TrimLeft(lines[idx], " \t"))]
		lines[idx] = indent + "disable-model-invocation: true"
	} else {
		lines = append(lines[:end], append([]string{"disable-model-invocation: true"}, lines[end:]...)...)
	}
	return pkg.WriteFileAtomic(path, []byte(strings.Join(lines, "\n")), 0o644)
}

// DeleteByPath 删掉一个技能目录（调用方已校验目录归属）。
func DeleteByPath(path string) error {
	dir := filepath.Dir(path)
	if err := os.RemoveAll(dir); err != nil {
		return pkg.Wrap(pkg.CodeSkillInvalid, "删除失败", err)
	}
	return nil
}

// Create 在用户目录新建一个技能。
func Create(rootDir, name, description, body string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, `\/:*?"<>|`) {
		return "", pkg.New(pkg.CodeSkillInvalid, "技能名不合法", "不能为空，也不能含路径分隔符")
	}
	dir := filepath.Join(rootDir, name)
	file := filepath.Join(dir, "SKILL.md")
	if _, err := os.Stat(file); err == nil {
		return "", pkg.New(pkg.CodeSkillInvalid, "同名技能已经存在", name)
	}
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString("name: " + name + "\n")
	if d := strings.TrimSpace(description); d != "" {
		sb.WriteString("description: " + d + "\n")
	}
	sb.WriteString("---\n\n")
	if b := strings.TrimSpace(body); b != "" {
		sb.WriteString(b)
		sb.WriteString("\n")
	} else {
		sb.WriteString("# " + name + "\n\n什么时候用：\n\n## 步骤\n\n1. \n")
	}
	if err := pkg.WriteFileAtomic(file, []byte(sb.String()), 0o644); err != nil {
		return "", pkg.Wrap(pkg.CodeSkillInvalid, "技能文件写入失败", err)
	}
	return file, nil
}

// Delete 删掉一个技能目录（只允许用户目录下的）。
func Delete(sk Skill) error {
	if !sk.Editable {
		return pkg.New(pkg.CodeSkillInvalid, "只能删除用户目录里的技能", sk.Source)
	}
	if err := os.RemoveAll(sk.Dir); err != nil {
		return pkg.Wrap(pkg.CodeSkillInvalid, "删除失败", err)
	}
	return nil
}
