package skill

// 保存技能正文只测会静默出错的两件：只认 SKILL.md、文件不存在或超限时拒绝写入。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 非 SKILL.md 的文件不许改：否则这个接口就是任意文件写入。
func TestSaveContentRejectsOtherFiles(t *testing.T) {
	dir := t.TempDir()
	other := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(other, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveContent(other, "hacked"); err == nil {
		t.Fatal("非 SKILL.md 应当被拒绝")
	}
}

// 写入的内容就是给的原文：frontmatter 由调用方原样带回来。
func TestSaveContentWritesAndKeepsFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	original := "---\nname: demo\ndescription: 演示\n---\n\n正文\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(original, "正文", "新正文", 1)
	if err := SaveContent(path, updated); err != nil {
		t.Fatal(err)
	}
	got, err := Content(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != updated {
		t.Fatalf("写入后读到的内容不一致：\n%q\n%q", got, updated)
	}
}

// 两种拒绝：文件不存在（不该凭空造一个技能）、正文超限（引擎每次启动都要读它）。
func TestSaveContentRejectsBadInput(t *testing.T) {
	dir := t.TempDir()
	if err := SaveContent(filepath.Join(dir, "SKILL.md"), "x"); err == nil {
		t.Error("文件不存在时应当报错，而不是凭空造一个技能")
	}

	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte("---\nname: x\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveContent(path, strings.Repeat("a", contentLimit+1)); err == nil {
		t.Error("超过上限的正文应当被拒绝")
	}
}
