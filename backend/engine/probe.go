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
// 两步判据（全部只读、无副作用）：
//  1. `<bin> --version`：omp 打 `omp/18.13.1`（`${bin}/${version}`），
//     pi 打裸 `1.4.0`。前者一眼可判，后者只算「疑似 pi」。
//  2. 疑似 pi 时复核 `--help`：omp 的根帮助首行是 `omp v<ver>` 且有小节标题
//     `USAGE`（大写），pi 的是 `Usage:`。omp 的遗留 launch 分支会打裸版本号，
//     不复核就会把 omp 认成 pi。
//
// 不靠可执行文件名判种：用户可能重命名它，omp 还允许用 `OMP_APP_NAME` 改 bin。
//
// 「没版本输出」就是「没找到」——界面显示安装命令，不猜、也不退化成
// 「版本未知」这种半吊子状态。

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
	Source string // custom / path
}

// Candidates 按「显式路径 → PATH」列出所有候选。
//
// 只有这两个来源：本应用不装引擎，所以不存在「我们自己放的那一份」。
func Candidates(custom string, bin string) []Candidate {
	out := make([]Candidate, 0, 2)
	if strings.TrimSpace(custom) != "" {
		out = append(out, Candidate{Exe: strings.TrimSpace(custom), Source: "custom"})
	}
	if found, err := exec.LookPath(bin); err == nil {
		// 显式路径可能就是 PATH 上那一个，去重让界面只显示一次。
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

// matchKind 确认这个 exe 真的是我们要的那个引擎。
func matchKind(ctx context.Context, kind Kind, exe, version string) bool {
	help := ""
	if kind != KindOMP || !isOMPVersion(version) {
		// omp 的标准输出不需要复核帮助，先拿到 help 以备后续判据使用。
		help = runHelp(ctx, exe)
	}
	return matchKindWithHelp(kind, version, help)
}

// matchKindWithHelp 是判据本身，与进程无关，所以能被直接测。
//
// 拆出来是为了让测试打真身：判据是一串条件分支，如果只在 matchKind 里，
// 每次验证都得真起一个进程，慢且脆。help 由调用方提供。
func matchKindWithHelp(kind Kind, version, help string) bool {
	if kind == KindOMP {
		// `omp/18.13.1` 是 omp 的标准输出，可以直接认。
		if isOMPVersion(version) {
			return true
		}
		// 别的 name/semver：可能是被 OMP_APP_NAME 改过 bin 的 omp，
		// 也可能根本不是。靠 --help 的小节标题确认，不认前缀形状。
		if rePrefixed.MatchString(version) {
			return helpIsOMP(help)
		}
		return false
	}
	// pi 打裸版本号；但 omp 的遗留分支也会打裸版本号，所以必须复核 --help。
	if !reBare.MatchString(version) {
		return false
	}
	if helpIsOMP(help) {
		return false // 其实是 omp
	}
	// 正向确认它是 pi，而不是「不是 omp 的任何东西」。
	return strings.Contains(help, "Usage:") || strings.Contains(help, "--mode")
}

// isOMPVersion 判断 --version 输出是不是 omp 的标准格式。
//
// omp 打 `${bin}/${version}`，bin 默认是 `omp`，但可以被 OMP_APP_NAME 改掉——
// 所以只认 `omp/` 前缀这一种确定情形，其余交给 helpIsOMP。
// 有意不用宽松的 `name/semver` 匹配：那会把将来改打 `pi/1.4.0` 的版本误判成 omp。
func isOMPVersion(version string) bool {
	return strings.HasPrefix(version, "omp/")
}

// helpIsOMP 看根帮助里有没有 omp 独有的小节标题。
//
// omp 的首行是 `omp v<ver>`，紧接一行全大写 `USAGE`；
// pi 用的是 `Usage:`。这两个形状不会互相串。
func helpIsOMP(help string) bool {
	return strings.Contains(help, "\nUSAGE\n") || strings.HasPrefix(help, "omp v")
}

// runHelp 跑一次 --help；失败返回空串（判据会自然落到「不是」）。
func runHelp(ctx context.Context, exe string) string {
	out, _ := runCLI(ctx, exe, "--help")
	return out
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
			QueueEdit: true, QueueClear: false, LiveSteerCount: true, SkillToggle: true, Extensions: true,
			ExportHTML: true, SlashCommands: true,
		}
	}
	return Caps{Extensions: true, ExportHTML: true, QueueClear: true}
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

// FileUsable 判断一个候选路径能不能拿去当引擎跑。
//
// 它回答的是「这个文件在不在、是不是个文件」——比 Stat 多一层的是目录判定：
// 用户把路径指到一个目录（而不是可执行文件）时，探测会一直失败，
// 而界面只能说「没找到」，于是用户一遍遍重试一个根本不可能成的路径。
func FileUsable(exe string) bool {
	st, err := os.Stat(exe)
	return err == nil && !st.IsDir()
}

// ErrMissing 是「没找到引擎」的统一错误。
func ErrMissing(kind Kind) error {
	return pkg.New(pkg.CodeEngineUnsupported,
		"没有找到 "+string(kind)+" 引擎。装好后点「重新检测」，或先自己跑一次 "+InstallHint(kind), "")
}
