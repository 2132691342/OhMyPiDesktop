package git

// 参数校验与小工具。
//
// 写入不可逆，所以这里只做「接受什么」的判定，不猜用户的意图：
// 认不出来就是拒绝，让用户改参数，而不是近似执行掉。

import (
	"OhMyPiDesktop/backend/pkg"
	"path/filepath"
	"strings"
)

// pathsArg 把界面给的工作区相对路径收成仓库根相对的 pathspec；越界直接报错，不降级成「全部」。
func (s *scope) pathsArg(paths []string) ([]string, error) {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// 落进仓库的路径先过安全检查：绝对路径、盘符、`..` 一律拒绝，不降级成「全部」。
		if !validRepoRel(p) {
			return nil, pkg.New("1004", "路径越出工作目录", p)
		}
		out = append(out, s.relFromRoot(p))
	}
	if len(out) == 0 {
		// 空表示「工作区内全部改动」：限定在 prefix 下，不碰仓库别处。
		// pathspec 不能是空串（git 会直接报 fatal），工作区就是仓库根时用 `.`。
		target := s.relFromRoot("")
		if target == "" {
			target = "."
		}
		return []string{target}, nil
	}
	return out, nil
}

// validRepoRel 校验界面给的相对路径：拒绝绝对路径、盘符、`..` 与反斜杠越界。
func validRepoRel(p string) bool {
	p = filepath.ToSlash(strings.TrimSpace(p))
	if p == "" {
		return true
	}
	if strings.HasPrefix(p, "/") || strings.Contains(p, "://") {
		return false
	}
	if len(p) > 1 && p[1] == ':' {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return false
		}
	}
	return true
}

// validBranchName 分支名白名单：拒绝以 `-` 开头（会被当成选项）、拒绝 `..` 与空白。
// 斜杠允许：`feature/x` 是常见分层；但连续斜杠与结尾斜杠会让 git 造出嵌套 ref。
func validBranchName(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 128 || strings.HasPrefix(s, "-") {
		return false
	}
	if strings.Contains(s, "..") || strings.ContainsAny(s, " \t\n\\~^:?*[") {
		return false
	}
	if strings.HasSuffix(s, "/") || strings.Contains(s, "//") || strings.HasSuffix(s, ".lock") {
		return false
	}
	if strings.HasPrefix(s, "/") {
		return false
	}
	return true
}

// validRemoteName 远端名白名单：git 允许更宽的字符集，这里只放常见的几种。
func validRemoteName(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 64 || strings.HasPrefix(s, "-") {
		return false
	}
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '.' || r == '_' || r == '-'
		if !ok {
			return false
		}
	}
	return true
}

// validRemoteURL 远端地址白名单：只放 git 认得的四种形态，拒绝任何以 `-` 开头的串
// （那会被当成 git 的选项），也拒绝带空白的串。
func validRemoteURL(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 2048 || strings.HasPrefix(s, "-") {
		return false
	}
	if strings.ContainsAny(s, " \t\n\r") {
		return false
	}
	prefixes := []string{"https://", "http://", "ssh://", "git://", "file://", "git@"}
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	// 本地路径形态：盘符或 / 开头，或纯相对目录（不含 : 以免误吞 scp 风格）
	if strings.HasPrefix(s, "/") || (len(s) > 1 && s[1] == ':') {
		return true
	}
	return !strings.Contains(s, ":") && !strings.Contains(s, "..")
}

// splitUpstream 把 `origin/main` 拆成远端名与分支名；`feature/x` 这类带斜杠的分支要保住。
func splitUpstream(up string) (string, string) {
	name, rest, ok := strings.Cut(up, "/")
	if !ok {
		return "", up
	}
	return name, rest
}

// errText 取 AppError 里留给用户的原文（git 自己的输出），没有就退回 Error()。
func errText(err error) string {
	if err == nil {
		return ""
	}
	if ae, ok := err.(*pkg.AppError); ok {
		if ae.Detail != "" {
			return ae.Detail
		}
		return ae.Message
	}
	return err.Error()
}
