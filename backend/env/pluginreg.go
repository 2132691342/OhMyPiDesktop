package env

// 插件登记清单：引擎把「装了哪些包」记在自己那份清单里；只读清单，不自己扫 node_modules——
// 扫目录会把传递依赖（几百个间接包）一起当成插件，清单里那几行才是引擎真正会加载的东西。
// 两个引擎的清单位置与形状都不同（pi 在 settings.json 的 packages 数组，omp 在 plugins/package.json 的 dependencies 映射），这里按引擎分叉，上层只拿统一形态。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"OhMyPiDesktop/backend/pkg"
)

// PluginEntry 是登记清单里的一行。
type PluginEntry struct {
	// Name 包名（去掉 npm: 前缀与版本范围后的裸名）
	Name string
	// Source 清单里那一行的原文；卸载要把它原样交回给引擎 CLI
	Source string
	// Scope 登记在哪一层：user / project
	Scope string
	// Enabled 启用位。pi 没有这个概念恒为 true；omp 读锁文件，
	// 没有记录就是启用（锁文件只记「被关掉的」）
	Enabled bool
}

// PluginDetail 是包目录里读出来的事实。
type PluginDetail struct {
	// Path 包目录绝对路径；空表示目录不在盘上
	Path string
	// Version package.json 里的版本
	Version string
	// Desc package.json 里的 description
	Desc string
	// Resources 这个包提供的资源种类（extensions / skills / prompts / themes）
	Resources []string
}

// PackageSpec 是 npm 源的解析结果（`npm:name@version` → name + version）。
type PackageSpec struct {
	// Name 包名（含 scope，如 @scope/pkg）
	Name string
	// Version 版本范围或精确版本；空表示没写
	Version string
}

// ReadPluginEntries 读某引擎的插件登记清单。
// 读不到文件不是错误：干净机器上那份文件根本不存在，
// 「没装插件」是正常状态，报成错误会让界面在首次使用时先红一片。
func ReadPluginEntries(kind string, layout Layout) ([]PluginEntry, string, error) {
	if kind == "pi" {
		return readPiEntries(layout.Settings)
	}
	return readOMPEntries(filepath.Join(layout.Plugins, "package.json"))
}

// readPiEntries 读 pi 的 settings.json：packages 是一组 source 字符串。
// 形状允许两种：字符串数组（当前）与 `{enableSkillCommands, customDirectories}` 的旧对象形式；
// 后者已经废弃、不算「包清单」，遇到就当没有包。
func readPiEntries(settingsPath string) ([]PluginEntry, string, error) {
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, settingsPath, nil
		}
		return nil, settingsPath, pkg.Wrap(pkg.CodeConfigRead, "读不到 settings.json", err)
	}
	var top struct {
		Packages []string `json:"packages"`
	}
	if err := json.Unmarshal(stripBOM(raw), &top); err != nil {
		return nil, settingsPath, pkg.Wrap(pkg.CodeConfigParse, "settings.json 格式有误", err)
	}
	out := make([]PluginEntry, 0, len(top.Packages))
	for _, line := range top.Packages {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		spec := ParsePackageSource(line)
		if spec.Name == "" {
			continue
		}
		out = append(out, PluginEntry{Name: spec.Name, Source: line, Scope: "user", Enabled: true})
	}
	return out, settingsPath, nil
}

// readOMPEntries 读 omp 的 plugins/package.json：dependencies 是「包名 → 版本范围」。
// 键就是包名；source 用裸名而不是 `name@range`——omp 的 uninstall 认的是包名，
// 拼上范围反而会被当成没装过。
func readOMPEntries(path string) ([]PluginEntry, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, path, nil
		}
		return nil, path, pkg.Wrap(pkg.CodeConfigRead, "读不到 plugins/package.json", err)
	}
	var top struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal(stripBOM(raw), &top); err != nil {
		return nil, path, pkg.Wrap(pkg.CodeConfigParse, "plugins/package.json 格式有误", err)
	}
	enabledBits := ompEnabledBits(filepath.Join(filepath.Dir(path), "omp-plugins.lock.json"))
	out := make([]PluginEntry, 0, len(top.Dependencies))
	for name := range top.Dependencies {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		entry := PluginEntry{Name: name, Source: name, Scope: "user", Enabled: true}
		if bit, ok := enabledBits[name]; ok {
			entry.Enabled = bit
		}
		out = append(out, entry)
	}
	return out, path, nil
}

// ompEnabledBits 读 omp 锁文件（omp-plugins.lock.json）里的启用位：plugins.<包名>.enabled。
// 文件不存在或解析不了都返回 nil（全部按启用）：锁文件是引擎自己的账，
// 读不懂时宁可多显示一个「开着」也不能把整个清单读挂。
func ompEnabledBits(lockPath string) map[string]bool {
	raw, err := os.ReadFile(lockPath)
	if err != nil {
		return nil
	}
	var top struct {
		Plugins map[string]struct {
			Enabled *bool `json:"enabled"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(stripBOM(raw), &top); err != nil {
		return nil
	}
	bits := make(map[string]bool, len(top.Plugins))
	for name, cfg := range top.Plugins {
		bits[name] = cfg.Enabled == nil || *cfg.Enabled
	}
	return bits
}

// ParsePackageSource 解析一条 source 原文，取出包名与版本。三种形态都要认：
// `npm:name@1.2.3` / `npm:@scope/pkg@1.2.3`（pi 的写法）、`name@1.2.3` / `@scope/pkg`（裸 npm spec）、本地路径或 `git:` 源（名字取最后一段，交给引擎自己解释）。
// 版本只从**最后一个** @ 切开：scope 包自带前导 @，按第一个切会把 `@scope/pkg` 切成空名加 `scope/pkg`。
func ParsePackageSource(source string) PackageSpec {
	text := strings.TrimSpace(source)
	if text == "" {
		return PackageSpec{}
	}
	if idx := strings.Index(text, ":"); idx > 0 && !strings.Contains(text[:idx], string(filepath.Separator)) {
		// 前缀只认已知的三种协议，且不能吃掉 Windows 盘符（C:\…）
		switch text[:idx] {
		case "npm", "git", "file":
			text = strings.TrimSpace(text[idx+1:])
		}
	}
	name, version := text, ""
	if strings.HasPrefix(name, "@") {
		if cut := strings.LastIndex(name, "@"); cut > 0 {
			name, version = name[:cut], name[cut+1:]
		}
	} else if cut := strings.LastIndex(name, "@"); cut > 0 {
		name, version = name[:cut], name[cut+1:]
	}
	return PackageSpec{Name: strings.TrimSpace(name), Version: strings.TrimSpace(version)}
}

// ReadPluginDetail 读一个包目录里的 package.json。
// 目录不在盘上不算错误：清单里登记了但目录被删掉是很常见的状态
// （换机器、手删、包管理器 uninstall 只删了目录），界面要能显示「未安装」。
func ReadPluginDetail(modulesDir string, entry PluginEntry) PluginDetail {
	dir := filepath.Join(modulesDir, filepath.FromSlash(entry.Name))
	file := filepath.Join(dir, "package.json")
	raw, err := os.ReadFile(file)
	if err != nil {
		return PluginDetail{}
	}
	var top struct {
		Version     string `json:"version"`
		Description string `json:"description"`
		OMP         struct {
			Extensions []string `json:"extensions"`
			Skills     []string `json:"skills"`
			Prompts    []string `json:"prompts"`
			Themes     []string `json:"themes"`
		} `json:"omp"`
		PI struct {
			Extensions []string `json:"extensions"`
			Skills     []string `json:"skills"`
			Prompts    []string `json:"prompts"`
			Themes     []string `json:"themes"`
		} `json:"pi"`
	}
	if err := json.Unmarshal(stripBOM(raw), &top); err != nil {
		// 包还在、package.json 坏了：至少把路径给出去
		return PluginDetail{Path: dir}
	}
	detail := PluginDetail{Path: dir, Version: top.Version, Desc: top.Description}
	// 两个引擎写的是同一个 manifest 形状，只是键名不同（omp / pi）
	manifest := top.OMP
	if len(manifest.Extensions) == 0 && len(manifest.Skills) == 0 &&
		len(manifest.Prompts) == 0 && len(manifest.Themes) == 0 {
		manifest = top.PI
	}
	if len(manifest.Extensions) > 0 {
		detail.Resources = append(detail.Resources, "extensions")
	}
	if len(manifest.Skills) > 0 {
		detail.Resources = append(detail.Resources, "skills")
	}
	if len(manifest.Prompts) > 0 {
		detail.Resources = append(detail.Resources, "prompts")
	}
	if len(manifest.Themes) > 0 {
		detail.Resources = append(detail.Resources, "themes")
	}
	return detail
}

// stripBOM 去掉 UTF-8 BOM：两个引擎的配置文件都可能带它。
func stripBOM(raw []byte) []byte {
	if len(raw) >= 3 && raw[0] == 0xEF && raw[1] == 0xBB && raw[2] == 0xBF {
		return raw[3:]
	}
	return raw
}
