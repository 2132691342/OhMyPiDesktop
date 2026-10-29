package git

// 文件历史与单文件差异共享路径翻译（工作区相对 → 仓库根相对），错译会让界面显示「没有记录」。

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileHistoryAndDiff(t *testing.T) {
	if !Available() {
		t.Skip("这台机器上没有 git")
	}
	repo := t.TempDir()
	gitCmd(t, repo, "init", "-b", "main")
	gitCmd(t, repo, "config", "user.email", "test@example.com")
	gitCmd(t, repo, "config", "user.name", "Tester")

	if err := os.MkdirAll(filepath.Join(repo, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(repo, "sub", "a.txt")
	if err := os.WriteFile(file, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, repo, "add", ".")
	gitCmd(t, repo, "commit", "-m", "first")

	if err := os.WriteFile(file, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, repo, "add", ".")
	gitCmd(t, repo, "commit", "-m", "second")

	// 工作区指向仓库子目录：路径翻译的差异在这里才暴露。
	ws := filepath.Join(repo, "sub")

	commits, err := FileHistory(ws, "a.txt", 10)
	if err != nil {
		t.Fatalf("文件历史失败：%v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("文件历史条数 = %d, want 2（%+v）", len(commits), commits)
	}
	if commits[0].Subject != "second" {
		t.Fatalf("最新一条应当是 second，得到 %q", commits[0].Subject)
	}

	diff, err := FileDiffInCommit(ws, commits[0].CommitHash, "a.txt")
	if err != nil {
		t.Fatalf("单文件差异失败：%v", err)
	}
	var added int
	for _, line := range diff.DiffLines {
		if line.LineKind == "add" && strings.TrimSpace(line.LineText) == "two" {
			added++
		}
	}
	if added != 1 {
		t.Fatalf("期望差异里有一行新增的 two，得到 %d（%+v）", added, diff.DiffLines)
	}

	// 非法提交号必须拒绝，而不是把它当选项传给 git（防参数注入）。
	if _, err := FileDiffInCommit(ws, "--upload-pack=evil", "a.txt"); err == nil {
		t.Fatal("非法提交号应当被拒绝")
	}
}

func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v 失败：%v\n%s", args, err, out)
	}
}
