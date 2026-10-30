package git

// scope：一次 git 读写的定位结果（仓库根 + 工作区前缀），以及它的各种查询。
// 所有命令都在仓库根执行、路径参数都是仓库根相对——界面上的 path 在 status / diff / add 之间
// 必须是同一个东西，否则「点这个文件」和「提交这个文件」会指向两个不一样的路径；工作区是子目录时，这条翻译只在这里做一次。

import (
	"OhMyPiDesktop/backend/domain"
	"OhMyPiDesktop/backend/pkg"
	"os"
	"path/filepath"
	"strings"
)

type scope struct {
	// root 仓库根目录（git 的 top-level）
	root string
	// prefix 工作区相对仓库根的斜杠路径；工作区就是仓库根时为空
	prefix string
	// quotePath 仓库是否对非 ASCII 路径做八进制转义（core.quotepath）
	quotePath bool
}

// openScope 解析 dir 所在的仓库根与工作区前缀；不在仓库里返回 (nil, nil)。
func openScope(dir string) (*scope, error) {
	res, err := run(dir, statusTimeout, "rev-parse", "--show-toplevel", "--show-prefix")
	if err != nil {
		if isTimeout(err) {
			return nil, err
		}
		return nil, nil
	}
	// 不能用 splitLines：工作区就是仓库根时 --show-prefix 输出空行，
	// 去掉空行后只剩一半，于是根目录反而被判成「不是仓库」。
	parts := strings.Split(strings.ReplaceAll(res.stdout, "\r\n", "\n"), "\n")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return nil, nil
	}
	sc := &scope{
		root:      filepath.FromSlash(filepath.Clean(strings.TrimSpace(parts[0]))),
		quotePath: true,
	}
	if len(parts) > 1 {
		sc.prefix = strings.Trim(strings.TrimSpace(filepath.ToSlash(parts[1])), "/")
	}
	if q, err := run(sc.root, statusTimeout, "config", "--get", "core.quotepath"); err == nil {
		if strings.TrimSpace(q.stdout) == "false" {
			sc.quotePath = false
		}
	}
	return sc, nil
}

// relFromRoot 把工作区相对的界面上报路径换成仓库根相对的 git pathspec；
// 工作区就是仓库根时原样返回。空串表示整个工作区。
func (s *scope) relFromRoot(wsRel string) string {
	wsRel = strings.Trim(strings.TrimSpace(filepath.ToSlash(wsRel)), "/")
	if s.prefix == "" {
		return wsRel
	}
	if wsRel == "" || wsRel == "." {
		return s.prefix
	}
	return s.prefix + "/" + wsRel
}

// relToWorkspace 把仓库根相对的路径折回界面上报的工作区相对形式。
// 落在工作区之外的文件不上报——那些不是这个工作区的改动。
func (s *scope) relToWorkspace(rootRel string) (string, bool) {
	rootRel = strings.Trim(strings.TrimSpace(filepath.ToSlash(rootRel)), "/")
	if s.prefix == "" {
		return rootRel, true
	}
	if rootRel == s.prefix {
		return "", true
	}
	rest, ok := strings.CutPrefix(rootRel, s.prefix+"/")
	if !ok {
		return "", false
	}
	return rest, true
}

// branch 当前分支名；还没有第一次提交时返回空串。
func (s *scope) branch() string {
	res, err := run(s.root, statusTimeout, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	name := strings.TrimSpace(res.stdout)
	if name == "HEAD" {
		return ""
	}
	return name
}

// upstream 当前分支的上游引用；没有上游返回空串。
func (s *scope) upstream() string {
	res, err := run(s.root, statusTimeout, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(res.stdout)
}

// remotes 列出全部远端及其推送地址；没有远端返回空数组。
func (s *scope) remotes() []domain.GitRemoteVO {
	res, err := run(s.root, statusTimeout, "remote", "-v")
	if err != nil {
		return []domain.GitRemoteVO{}
	}
	out := make([]domain.GitRemoteVO, 0, 2)
	seen := map[string]bool{}
	for _, line := range splitLines(res.stdout) {
		name, url, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		// `remote -v` 给每行都续一个 ` (fetch)` / ` (push)` 标记，地址里不该带它。
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		if i := strings.LastIndex(url, " ("); i > 0 {
			url = url[:i]
		}
		out = append(out, domain.GitRemoteVO{RemoteName: name, RemoteURL: strings.TrimSpace(url)})
	}
	return out
}

// remoteNames 只要远端名，供推送推断目标用。
func (s *scope) remoteNames() []string {
	list := s.remotes()
	names := make([]string, 0, len(list))
	for _, r := range list {
		names = append(names, r.RemoteName)
	}
	return names
}

// repoState 是仓库当前所处的中间状态：普通 / 合并中 / 变基中 / 拣选或回退中。
type repoState struct {
	StateCode string
	Detail    string
}

// state 判定仓库是否有未完成的合并或变基；判定靠 git 自己，不靠猜目录里的文件。
func (s *scope) state() repoState {
	gitDir := s.gitDir()
	if gitDir == "" {
		return repoState{StateCode: domain.GitStateNone}
	}
	if pkg.DirExists(filepath.Join(gitDir, "rebase-merge")) ||
		pkg.DirExists(filepath.Join(gitDir, "rebase-apply")) {
		return repoState{StateCode: domain.GitStateRebase, Detail: s.readText(filepath.Join(gitDir, "rebase-merge", "head-name"))}
	}
	if pkg.FileExists(filepath.Join(gitDir, "MERGE_HEAD")) {
		return repoState{StateCode: domain.GitStateMerge, Detail: s.readText(filepath.Join(gitDir, "MERGE_MSG"))}
	}
	if pkg.FileExists(filepath.Join(gitDir, "CHERRY_PICK_HEAD")) {
		return repoState{StateCode: domain.GitStateCherryPick}
	}
	if pkg.FileExists(filepath.Join(gitDir, "REVERT_HEAD")) {
		return repoState{StateCode: domain.GitStateRevert}
	}
	return repoState{StateCode: domain.GitStateNone}
}

// gitDir 解析 .git 的真实路径（工作区可能是子目录，也可能是 worktree）。
func (s *scope) gitDir() string {
	res, err := run(s.root, statusTimeout, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(res.stdout)
}

// readText 读一个小文本文件的首行，读不到返回空串。
func (s *scope) readText(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(string(data), "\n")
	return strings.TrimSpace(line)
}

// changedFiles 列出工作区下被改动的文件，并补上增删行数。
// porcelainEntry 是 porcelain -z 里的一条记录。
type porcelainEntry struct {
	path     string // 仓库根相对路径
	status   string // 取 domain.GitChange*
	staged   bool
	conflict bool
}

// parsePorcelainZ 解析 `status --porcelain -z`：NUL 分隔不做路径转义（中文空格路径免反解）；
// 改名记录占两条 NUL 段（新路径在前、原路径在后），原路径要跳过。
func parsePorcelainZ(raw string) []porcelainEntry {
	parts := strings.Split(raw, "\x00")
	out := make([]porcelainEntry, 0, len(parts))
	for i := 0; i < len(parts); i++ {
		seg := parts[i]
		if len(seg) < 4 {
			continue
		}
		x, y, name := seg[0], seg[1], seg[3:]
		if x == 'R' || x == 'C' || y == 'R' || y == 'C' {
			i++ // 下一条是原路径，不是独立改动
		}
		staged, status, conflict := classify(x, y)
		if status == "" {
			continue
		}
		out = append(out, porcelainEntry{path: filepath.ToSlash(name), status: status, staged: staged, conflict: conflict})
	}
	return out
}

// unmergedCodes 是 porcelain 里七种未合并状态码，全部意味着「这个文件有冲突没解决」。
var unmergedCodes = map[string]bool{
	"DD": true, "AU": true, "UD": true, "UA": true, "DU": true, "AA": true, "UU": true,
}

// classify 把 porcelain 的两位状态码折成一个界面标签、是否已进待提交区、是否冲突。
func classify(x, y byte) (staged bool, status string, conflict bool) {
	if x == '?' && y == '?' {
		return false, domain.GitChangeUntracked, false
	}
	if x == '!' && y == '!' {
		return false, "", false
	}
	if unmergedCodes[string([]byte{x, y})] {
		return true, domain.GitChangeConflict, true
	}
	if x != ' ' && x != '?' {
		return true, string(rune(x)), false
	}
	return false, string(rune(y)), false
}

// parseDiff 把统一差异文本折成行结构；超出上限的行被丢掉并计入 dropped。
