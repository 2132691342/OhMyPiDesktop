package pkg

import (
	"os"
	"path/filepath"
	"strings"
)

// 文件与路径的叶子工具。凡是「把用户给的相对路径拼到工作目录上」的地方
// 都必须走 SafeJoin：路径穿越是桌面应用最容易漏的一类洞。

// FileExists 判断路径存在且是普通文件。
func FileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// DirExists 判断路径存在且是目录。
func DirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// NormalizePath 把用户输入的路径规范成当前平台的形态。
func NormalizePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return p
	}
	return filepath.FromSlash(p)
}

// SafeJoin 把 target 拼到 root 下，并保证结果仍在 root 内。
//
// 先 Clean 再比前缀不够：符号链接可以把 /root/link 指到 /etc。
// 所以比较的是两侧的「真实路径」（Windows 上再折叠大小写）。
func SafeJoin(root, target string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", New("1003", "工作目录为空", "")
	}
	absRoot, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", Wrap("1003", "工作目录解析失败", err)
	}
	full := NormalizePath(target)
	if !filepath.IsAbs(full) {
		full = filepath.Join(absRoot, full)
	}
	full = filepath.Clean(full)

	realRoot, err := resolveReal(absRoot)
	if err != nil {
		return "", err
	}
	realFull, err := resolveReal(full)
	if err != nil {
		return "", err
	}
	if !IsInside(realRoot, realFull) {
		return "", New("1004", "不能操作工作目录之外的路径", realFull)
	}
	return full, nil
}

// RelPath 求 full 相对 root 的路径；失败时原样返回。
func RelPath(root, full string) string {
	rel, err := filepath.Rel(root, full)
	if err != nil {
		return full
	}
	return rel
}

// RealPath 求真实路径；解析失败时退回 Clean 过的路径。
func RealPath(p string) string {
	if real, err := resolveReal(p); err == nil {
		return real
	}
	return filepath.Clean(p)
}

// IsInside 判断 child 是否在 root 之内（含 root 本身）。
func IsInside(root, child string) bool {
	rel, err := filepath.Rel(root, child)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolveReal 求真实路径：路径不存在时退回最近一级存在的祖先，
// 否则「新建文件」这类操作会因为目标不存在而被判成越界。
func resolveReal(p string) (string, error) {
	cur := filepath.Clean(p)
	var tail []string
	for {
		if resolved, err := filepath.EvalSymlinks(cur); err == nil {
			full := resolved
			for i := len(tail) - 1; i >= 0; i-- {
				full = filepath.Join(full, tail[i])
			}
			return foldCase(full), nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return foldCase(filepath.Clean(p)), nil
		}
		tail = append(tail, filepath.Base(cur))
		cur = parent
	}
}

// foldCase 在 Windows 上折叠大小写：同一个目录可能被写成 D:\a 与 d:\A。
func foldCase(p string) string {
	if strings.ContainsRune(p, '\\') || filepath.Separator == '\\' {
		return strings.ToLower(p)
	}
	return p
}
