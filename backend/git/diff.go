package git

// 改动清单与差异：porcelain 解析、numstat、逐文件 diff、未跟踪文件。
// 未跟踪文件要单独读出来：git diff 不会显示它们，而「新加的文件」恰恰是
// 用户最需要看见的那一类改动。

import (
	"OhMyPiDesktop/backend/domain"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func (s *scope) changedFiles() ([]domain.GitFileVO, error) {
	args := append([]string{"status", "--porcelain", "-z"}, s.pathspec("")...)
	res, err := run(s.root, statusTimeout, args...)
	if err != nil {
		return nil, err
	}
	counts := s.numstat()
	out := make([]domain.GitFileVO, 0, 32)
	for _, entry := range parsePorcelainZ(res.stdout) {
		if entry.path == "" {
			continue
		}
		rel, ok := s.relToWorkspace(entry.path)
		if !ok {
			continue
		}
		c := counts[entry.path]
		out = append(out, domain.GitFileVO{
			FilePath:   rel,
			ChangeCode: entry.status,
			Staged:     entry.staged,
			Conflict:   entry.conflict,
			Additions:  c.add,
			Deletions:  c.del,
		})
	}
	return out, nil
}

// unquote 去掉 git 给非 ASCII / 带空格路径加的引号与八进制转义。
// 只有 core.quotepath 打开时才会加引号（默认打开），关掉时路径原样。
func (s *scope) unquote(raw string) string {
	p := filepath.ToSlash(strings.TrimSpace(raw))
	if !s.quotePath || len(p) < 2 || p[0] != '"' || p[len(p)-1] != '"' {
		return p
	}
	if unquoted, err := strconv.Unquote(p); err == nil {
		return filepath.ToSlash(unquoted)
	}
	return p[1 : len(p)-1]
}

// headerToWorkspace 把差异文件头里的仓库根相对路径折回工作区相对；落在工作区外的原样保留。
func (s *scope) headerToWorkspace(rootRel string) string {
	if rel, ok := s.relToWorkspace(rootRel); ok {
		return rel
	}
	return rootRel
}

// pathspec 拼出「限定在当前工作区内」的 pathspec 段。
func (s *scope) pathspec(rootRel string) []string {
	target := s.relFromRoot(rootRel)
	if target == "" {
		target = "."
	}
	return []string{"--", target}
}

type counters struct{ add, del int }

// numstat 取各文件的增删行数；二进制文件那两列是 `-`，按 0 计。
// 按 tab 切而不是按空白：路径里带空格是常态，Fields 会把路径切碎。
func (s *scope) numstat() map[string]counters {
	counts := make(map[string]counters)
	args := []string{"diff", "--numstat"}
	if s.hasHead() {
		args = append(args, "HEAD")
	}
	args = append(args, s.pathspec("")...)
	res, err := run(s.root, statusTimeout, args...)
	if err != nil {
		return counts
	}
	for _, line := range splitLines(res.stdout) {
		cols := strings.Split(line, "\t")
		if len(cols) < 3 {
			continue
		}
		path := s.unquote(cols[len(cols)-1])
		add, _ := strconv.Atoi(strings.TrimSpace(cols[0]))
		del, _ := strconv.Atoi(strings.TrimSpace(cols[1]))
		counts[path] = counters{add: add, del: del}
	}
	return counts
}

// diffAll 整个工作区的差异。未跟踪文件不在索引里，git diff 给不出内容，逐个补读正文。
func (s *scope) diffAll() (*domain.GitDiffVO, error) {
	args := []string{"diff", "--no-color", "--no-ext-diff", "--no-textconv", "-U3"}
	if s.hasHead() {
		args = append(args, "HEAD")
	}
	args = append(args, s.pathspec("")...)
	res, err := run(s.root, statusTimeout, args...)
	if err != nil {
		return nil, err
	}
	lines, dropped := parseDiff(res.stdout, s.headerToWorkspace)
	total := dropped

	// 未跟踪文件整篇按新增读出来，否则「新建的文件」在这一页永远看不到自己。
	for _, st := range s.untrackedInScope() {
		if len(lines) >= maxLines {
			total++
			continue
		}
		rel, ok := s.relToWorkspace(st)
		if !ok {
			continue
		}
		add, rest := s.untrackedSection(rel, st)
		lines = append(lines, add...)
		total += rest
	}
	return &domain.GitDiffVO{FilePath: "", DiffLines: lines, Dropped: total, Truncated: total > 0}, nil
}

// diffOne 单个文件的差异；未跟踪文件同样补读正文。
func (s *scope) diffOne(full, wsRel, rootRel string) (*domain.GitDiffVO, error) {
	if !s.isTracked(rootRel) {
		lines, dropped := s.untrackedSection(wsRel, rootRel)
		return &domain.GitDiffVO{FilePath: wsRel, DiffLines: lines, Dropped: dropped, Truncated: dropped > 0}, nil
	}
	args := []string{"diff", "--no-color", "--no-ext-diff", "--no-textconv", "-U3"}
	if s.hasHead() {
		args = append(args, "HEAD")
	}
	args = append(args, "--", rootRel)
	res, err := run(s.root, statusTimeout, args...)
	if err != nil {
		return nil, err
	}
	lines, dropped := parseDiff(res.stdout, s.headerToWorkspace)
	return &domain.GitDiffVO{FilePath: wsRel, DiffLines: lines, Dropped: dropped, Truncated: dropped > 0}, nil
}

// untrackedInScope 列出工作区内未跟踪且未被忽略的文件（仓库根相对路径）。
func (s *scope) untrackedInScope() []string {
	args := append([]string{"ls-files", "--others", "--exclude-standard", "-z"}, s.pathspec("")...)
	res, err := run(s.root, statusTimeout, args...)
	if err != nil {
		return nil
	}
	out := make([]string, 0, 8)
	for _, p := range strings.Split(res.stdout, "\x00") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, filepath.ToSlash(p))
		}
	}
	return out
}

// untrackedSection 把一个未跟踪文件读成「整篇新增」的行块；返回行块与被丢掉的行数。
func (s *scope) untrackedSection(wsRel, rootRel string) ([]domain.GitDiffLineVO, int) {
	full := filepath.Join(s.root, filepath.FromSlash(rootRel))
	info, err := os.Stat(full)
	if err != nil {
		return nil, 0
	}
	// 目录与超大文件都不读正文：前者要展开成许多文件，后者会把内存吃掉。
	if info.IsDir() {
		return nil, 0
	}
	if info.Size() > maxUntrackedBytes {
		return nil, 1
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return nil, 0
	}
	out := []domain.GitDiffLineVO{{LineKind: domain.GitLineFile, LineText: wsRel}}
	// 二进制文件不逐行渲染：读成 UTF-8 再显示只会得到一堆替换字符，
	// 看起来像「文件坏了」而不是「这是二进制」。
	if isBinary(data) {
		return append(out, domain.GitDiffLineVO{LineKind: domain.GitLineHunk, LineText: "(binary)"}), 0
	}
	dropped := 0
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	rows := strings.Split(text, "\n")
	for i, row := range rows {
		if i == len(rows)-1 && row == "" {
			break
		}
		if i >= maxLines {
			dropped++
			continue
		}
		out = append(out, domain.GitDiffLineVO{LineKind: domain.GitLineAdd, NewNo: i + 1, LineText: row})
	}
	return out, dropped
}

// isBinary 用「头部有没有 NUL」判断：文本文件极少含 NUL，二进制几乎必含。
func isBinary(data []byte) bool {
	head := data
	if len(head) > 8000 {
		head = head[:8000]
	}
	for _, b := range head {
		if b == 0 {
			return true
		}
	}
	return false
}

// aheadBehind 与上游分支的领先 / 落后数；没有上游时两者都是 0。
func (s *scope) aheadBehind() (int, int) {
	if !s.hasHead() {
		return 0, 0
	}
	res, err := run(s.root, statusTimeout, "rev-list", "--left-right", "--count", "HEAD...@{upstream}")
	if err != nil {
		return 0, 0
	}
	fields := strings.Fields(res.stdout)
	if len(fields) != 2 {
		return 0, 0
	}
	ahead, _ := strconv.Atoi(fields[0])
	behind, _ := strconv.Atoi(fields[1])
	return ahead, behind
}

// hasHead 报告仓库有没有第一次提交：没有 HEAD 的仓库 `git diff HEAD` 会直接失败。
func (s *scope) hasHead() bool {
	res, err := run(s.root, statusTimeout, "rev-parse", "--verify", "--quiet", "HEAD")
	if err != nil {
		return false
	}
	return strings.TrimSpace(res.stdout) != ""
}

// isTracked 判断仓库根相对路径是否已被 Git 跟踪。
func (s *scope) isTracked(rootRel string) bool {
	res, err := run(s.root, statusTimeout, "ls-files", "--error-unmatch", "--", rootRel)
	return err == nil && strings.TrimSpace(res.stdout) != ""
}
