package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"OhMyPiDesktop/backend/pkg"
)

// 探测：找出机器上装了哪些引擎、各是什么版本。
//
// 三步判据（全部只读、无副作用）：
//  1. `<bin> --version`：`omp/18.13.1` 形如 name/semver → omp；裸 semver → 疑似 pi；
//  2. 疑似 pi 时复核 `--help`：含 `USAGE` 或 ` v<ver>` → 其实是 omp
//     （omp 遗留的 launch 子命令会打裸版本号）；
//  3. 都没有结论就不再猜：靠目录存在性交叉印证，界面显示「版本未知」。
//
// pi 的 --version 打裸版本号，omp 打带前缀的；这条差异是判别的唯一可靠入口，
// 所以宁可三步也不靠可执行文件名。

var (
	rePrefixed = regexp.MustCompile(`^[A-Za-z][\w.-]*/\d+\.\d+\.\d+`)
	reBare     = regexp.MustCompile(`^\d+\.\d+\.\d+(-[\w.]+)?$`)
)

// ProbeTimeout 是单条探测命令的上限。装得很慢的机器上 --version 也会卡，
// 但卡过 5 秒就说明不是版本太慢而是进程起不来。
const ProbeTimeout = 5 * time.Second

// Candidate 是一个待探测的可执行文件。
type Candidate struct {
	Exe    string
	Source string // custom / managed / path
}

// Candidates 按「显式路径 → 自管目录 → PATH」列出所有候选。
func Candidates(custom string, managed string, bin string) []Candidate {
	out := make([]Candidate, 0, 3)
	if strings.TrimSpace(custom) != "" {
		out = append(out, Candidate{Exe: strings.TrimSpace(custom), Source: "custom"})
	}
	if managed != "" {
		out = append(out, Candidate{Exe: managed, Source: "managed"})
	}
	if found, err := exec.LookPath(bin); err == nil {
		// 同一个文件可能出现两次（自管目录也在 PATH 上），去重让界面只显示一次。
		dup := false
		for _, c := range out {
			if sameFile(c.Exe, found) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, Candidate{Exe: found, Source: "path"})
		}
	}
	return out
}

// ExeName 返回某引擎在 Windows 上的可执行文件名。
func ExeName(kind Kind) string {
	if runtime.GOOS == "windows" {
		return string(kind) + ".exe"
	}
	return string(kind)
}

// Detect 探测一个候选，返回 Identity。找不到或版本读不出时 Exe 为空。
//
// 单个引擎探测失败不该影响另一个：调用方对每个引擎各调一次，
// 缺失的引擎在界面上是「没找到 + 安装命令」，不是错误。
func Detect(ctx context.Context, kind Kind, candidates []Candidate) Identity {
	id := Identity{Kind: kind, Caps: CapsFor(kind)}
	for _, c := range candidates {
		st, err := os.Stat(c.Exe)
		if err != nil || st.IsDir() {
			continue
		}
		version, ok := probeVersion(ctx, c.Exe)
		if !ok {
			continue
		}
		if !matchKind(ctx, kind, c.Exe, version) {
			continue
		}
		id.Exe = c.Exe
		id.Version = version
		id.Source = c.Source
		return id
	}
	return id
}

// probeVersion 跑一次 --version 并取最后一个非空行。
//
// 取最后一行是因为 omp 的 `launch` 分支会先打日志再打版本号；
// 只认第一行会把日志当成版本。
func probeVersion(ctx context.Context, exe string) (string, bool) {
	out, err := runCLI(ctx, exe, "--version")
	if err != nil && strings.TrimSpace(out) == "" {
		return "", false
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.Contains(line, "Error") || strings.Contains(line, "error") {
			continue
		}
		return line, true
	}
	return "", false
}

// matchKind 用三步判据确认这个 exe 真的是我们要的那个引擎。
func matchKind(ctx context.Context, kind Kind, exe, version string) bool {
	if kind == KindOMP {
		return rePrefixed.MatchString(version) || strings.HasPrefix(version, "omp")
	}
	// pi：裸版本号；但 omp 的遗留分支也会打裸版本号，所以要复核 --help。
	if !reBare.MatchString(version) {
		return false
	}
	out, _ := runCLI(ctx, exe, "--help")
	if strings.Contains(out, "USAGE") || strings.Contains(out, " v"+version) {
		return false // 其实是 omp
	}
	return strings.Contains(out, "Usage:") || strings.Contains(out, "--mode")
}

// runCLI 跑一条 CLI 并返回合并输出，带超时。
func runCLI(ctx context.Context, exe string, args ...string) (string, error) {
	timeout, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(timeout, exe, args...)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// CapsFor 返回某引擎的能力表（探测期用：进程没起来也要能回答「能做什么」）。
func CapsFor(kind Kind) Caps {
	if kind == KindOMP {
		return Caps{
			Subagents: true, Goals: true, Todos: true, Login: true, Fragments: true,
			QueueEdit: true, LiveSteerCount: true, SkillToggle: true, Extensions: true,
			ExportHTML: true, SlashCommands: true,
		}
	}
	return Caps{Extensions: true, ExportHTML: true}
}

// InstallHint 返回该引擎的安装命令原文。
//
// 我们不代装、不改 PATH、不弹 UAC：只给用户一行能自己粘进终端的命令。
func InstallHint(kind Kind) string {
	if kind == KindOMP {
		return "irm https://omp.sh/install.ps1 | iex"
	}
	return "npm i -g @earendil-works/pi-coding-agent"
}

// ManagedDir 返回自管安装目录（用户选择「我们把引擎放哪」时用）。
func ManagedDir(kind Kind) string {
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		return filepath.Join(local, "OhMyPiDesktop", "engines", string(kind))
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".oh-my-pi-desktop", "engines", string(kind))
}

// sameFile 判断两条路径是否同一文件（Windows 大小写不敏感）。
func sameFile(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func hideWindow(cmd *exec.Cmd) { hideWindowImpl(cmd) }

// ErrMissing 是「没找到引擎」的统一错误。
func ErrMissing(kind Kind) error {
	return pkg.New(pkg.CodeEngineUnsupported,
		"没有找到 "+string(kind)+" 引擎。装好后点「重新检测」，或先自己跑一次 "+InstallHint(kind), "")
}
