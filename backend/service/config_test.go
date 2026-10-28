package service

import (
	"os"
	"path/filepath"
	"testing"

	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/env"
)

// 配置清单的可靠性在于两点：主配置排第一、凭据文件不在清单里。
// 前者决定设置页默认打开哪一份，后者决定界面上没有「一键看密钥」的入口。
func TestConfigCandidates(t *testing.T) {
	omp := configCandidates(engine.KindOMP, env.Layout{
		Models:   `C:\u\.omp\agent\models.yml`,
		Settings: `C:\u\.omp\agent\config.yml`,
		Agent:    `C:\u\.omp\agent`,
	})
	if len(omp) != 3 {
		t.Fatalf("omp 候选数 = %d, want 3", len(omp))
	}
	if filepath.Base(omp[0].path) != "models.yml" {
		t.Fatalf("omp 主配置应当是 models.yml，得到 %s", omp[0].path)
	}

	pi := configCandidates(engine.KindPi, env.Layout{
		Models:   `C:\u\.pi\agent\models.json`,
		Settings: `C:\u\.pi\agent\settings.json`,
		Agent:    `C:\u\.pi\agent`,
		AuthFile: `C:\u\.pi\agent\auth.json`,
	})
	if filepath.Base(pi[0].path) != "models.json" {
		t.Fatalf("pi 主配置应当是 models.json，得到 %s", pi[0].path)
	}
	for _, cand := range pi {
		if filepath.Base(cand.path) == "auth.json" {
			t.Fatal("凭据文件不该出现在可查看清单里")
		}
	}
}

// 读原文只接受清单里的路径：否则就成了任意文件读取接口。
func TestInCandidates(t *testing.T) {
	allowed := []configFile{
		{path: `C:\u\.omp\agent\models.yml`, label: "models.yml"},
		{path: `C:\u\.omp\agent\config.yml`, label: "config.yml"},
	}
	if !inCandidates(`c:\U\.omp\agent\MODELS.yml`, allowed) {
		t.Fatal("大小写不同的同一路径应当被接受")
	}
	if inCandidates(`C:\u\.omp\agent\auth.json`, allowed) {
		t.Fatal("清单外的路径必须被拒绝")
	}
	if inCandidates(`C:\Windows\win.ini`, allowed) {
		t.Fatal("工作区外的路径必须被拒绝")
	}
}

// 文件不存在时返回 Found=false 而不是错误：干净机器上首次打开设置页
// 不该先红一片。
func TestReadConfigTextMissing(t *testing.T) {
	dir := t.TempDir()
	vo, err := readConfigText(filepath.Join(dir, "nope.yml"))
	if err != nil {
		t.Fatalf("不存在的文件不该报错：%v", err)
	}
	if vo.Found || vo.Text != "" {
		t.Fatalf("不存在时应 Found=false 且 Text 为空，得到 %+v", vo)
	}
}

func TestReadConfigTextOK(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.yml")
	if err := os.WriteFile(path, []byte("providers: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	vo, err := readConfigText(path)
	if err != nil {
		t.Fatal(err)
	}
	if !vo.Found || vo.Text != "providers: {}\n" || vo.FilePath != path {
		t.Fatalf("读到的内容不对：%+v", vo)
	}
}

// 超过上限的文件要被截断并置 Truncated——否则一个几十 MB 的文件会把界面卡死。
func TestReadConfigTextTruncates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.json")
	big := make([]byte, configTextLimit+16)
	for i := range big {
		big[i] = 'x'
	}
	if err := os.WriteFile(path, big, 0o644); err != nil {
		t.Fatal(err)
	}
	vo, err := readConfigText(path)
	if err != nil {
		t.Fatal(err)
	}
	if !vo.Truncated {
		t.Fatal("超过上限应当被标记为截断")
	}
	if len(vo.Text) != configTextLimit {
		t.Fatalf("截断长度 = %d, want %d", len(vo.Text), configTextLimit)
	}
}
