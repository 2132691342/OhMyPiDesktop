// 引擎配置文件的原文读取：设置页要能让人看到「这个引擎到底读的是哪份文件」。
// 只读不写：写引擎配置有节点级接口（ompcfg / picfg），它们的正确性建立在「只改自己掌管的那几个键」上，
// 原文编辑会打乱这个假设。

package service

import (
	"os"
	"path/filepath"
	"strings"

	"OhMyPiDesktop/backend/domain"
	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/env"
	"OhMyPiDesktop/backend/pkg"
)

// configTextLimit 是单次返回的字节上限。引擎配置一般几 KB；上限只有一个理由：
// 用户误点几十 MB 的文件时不要把界面卡死。
const configTextLimit = 512 * 1024

// configFile 是一个候选配置文件：路径 + 它是什么。
type configFile struct {
	path  string
	label string
}

// ConfigTextVO 是一份配置文件的原文。
type ConfigTextVO struct {
	// FilePath 这份原文来自哪个文件
	FilePath string `json:"file_path"`
	// Text 文件内容；Found 为假时是空串
	Text string `json:"text"`
	// Truncated 内容被上限截断
	Truncated bool `json:"truncated"`
	// Found 文件存在；为假时界面说「这个文件还没生成过」而不是报错
	Found bool `json:"found"`
}

// ConfigText 读一份引擎配置的原文。file 为空读主配置文件；给了 file 必须是 ConfigFiles 列出的之一——
// 否则就成了一个任意文件读取接口。
func (a *App) ConfigText(kind engine.Kind, file string) (*ConfigTextVO, error) {
	layout, err := a.Layout(kind)
	if err != nil {
		return nil, err
	}
	allowed := configCandidates(kind, layout)
	target := strings.TrimSpace(file)
	if target == "" {
		target = allowed[0].path
	} else if !inCandidates(target, allowed) {
		return nil, pkg.New(pkg.CodeConfigRead, "这个文件不在引擎的配置清单里", "")
	}
	return readConfigText(target)
}

// ConfigFiles 列出该引擎的配置文件及其现状。
func (a *App) ConfigFiles(kind engine.Kind) (*domain.ConfigFilesVO, error) {
	layout, err := a.Layout(kind)
	if err != nil {
		return nil, err
	}
	out := &domain.ConfigFilesVO{EngineKind: string(kind), Entries: []domain.ConfigFileVO{}}
	for _, cand := range configCandidates(kind, layout) {
		row := domain.ConfigFileVO{FilePath: cand.path, FileLabel: cand.label}
		if st, err := os.Stat(cand.path); err == nil && !st.IsDir() {
			row.Exists = true
			row.SizeBytes = st.Size()
		}
		out.Entries = append(out.Entries, row)
	}
	return out, nil
}

// configCandidates 按引擎罗列它的配置文件。顺序即界面顺序：主配置（服务商与模型）最常被打开，排最前。
// 凭据文件不在这张表里——界面上不该有「一键看密钥」的入口。
func configCandidates(kind engine.Kind, layout env.Layout) []configFile {
	mcp := filepath.Join(layout.Agent, "mcp.json")
	if kind == engine.KindOMP {
		return []configFile{
			{path: layout.Models, label: "models.yml"},
			{path: layout.Settings, label: "config.yml"},
			{path: mcp, label: "mcp.json"},
		}
	}
	return []configFile{
		{path: layout.Models, label: "models.json"},
		{path: layout.Settings, label: "settings.json"},
		{path: mcp, label: "mcp.json"},
	}
}

// inCandidates 判断路径是否在候选清单里。
//
// 大小写不敏感：Windows 上同一个文件可能被两种大小写写法指到。
func inCandidates(path string, allowed []configFile) bool {
	clean := strings.ToLower(filepath.Clean(path))
	for _, cand := range allowed {
		if strings.ToLower(filepath.Clean(cand.path)) == clean {
			return true
		}
	}
	return false
}

// readConfigText 读文件并按上限截断。文件不存在返回 Found=false 而不是错误：
// 「还没生成过」是首次使用的正常状态，报成错误会让设置页在干净机器上先红一片。
func readConfigText(path string) (*ConfigTextVO, error) {
	vo := &ConfigTextVO{FilePath: path}
	st, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return vo, nil
		}
		return nil, pkg.Wrap(pkg.CodeConfigRead, "读不到这个配置文件", err)
	}
	if st.IsDir() {
		return nil, pkg.New(pkg.CodeConfigRead, "这个路径是目录，不是配置文件", "")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, pkg.Wrap(pkg.CodeConfigRead, "读不到这个配置文件", err)
	}
	vo.Found = true
	if len(raw) > configTextLimit {
		raw = raw[:configTextLimit]
		vo.Truncated = true
	}
	vo.Text = string(raw)
	return vo, nil
}
