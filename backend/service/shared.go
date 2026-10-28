package service

// 跨文件共用的私有小工具。
//
// 它们不属于任何一个业务概念（路径比较、目录归属、字段兜底），
// 单独一处比散在五个文件里各抄一份安全：抄出来的副本会在某次修正时漏改。

import (
	"path/filepath"
	"strings"

	"OhMyPiDesktop/backend/domain"
	"OhMyPiDesktop/backend/env/ompcfg"
)

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
			ModelID: m.ID, ModelName: m.Name, API: m.API, Reasoning: m.Reasoning,
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
