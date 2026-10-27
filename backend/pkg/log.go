package pkg

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// 日志：进 stderr（wails dev 里能看见）、进环形缓冲（界面「日志」读）、进文件。
//
// 写文件这条是必需的，不是备份：桌面壳没有控制台，用户看到的只有「启动失败」，
// 而失败原因只在 stderr 里——那只在开发机上有。没有文件日志，线上问题一律不可诊断。

var (
	logMu    sync.Mutex
	logRing  []string
	logLimit = 500
	logFile  *os.File
)

// SetLogDir 打开日志文件（按天切）。失败不影响运行：日志不是功能。
func SetLogDir(dir string) {
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	name := filepath.Join(dir, "desktop-"+time.Now().Format("20060102")+".log")
	f, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	logMu.Lock()
	logFile = f
	logMu.Unlock()
}

// LogPath 返回当前日志文件路径（未打开时为空）。
func LogPath() string {
	logMu.Lock()
	defer logMu.Unlock()
	if logFile == nil {
		return ""
	}
	return logFile.Name()
}

// Ring 返回最近的日志行副本（新的在后）。
func Ring() []string {
	logMu.Lock()
	defer logMu.Unlock()
	out := make([]string, len(logRing))
	copy(out, logRing)
	return out
}

func logf(level, format string, args ...any) {
	line := fmt.Sprintf("%s %-5s %s", time.Now().Format("15:04:05.000"), level, fmt.Sprintf(format, args...))
	fmt.Fprintln(os.Stderr, line)
	logMu.Lock()
	logRing = append(logRing, line)
	if len(logRing) > logLimit {
		logRing = logRing[len(logRing)-logLimit:]
	}
	if logFile != nil {
		_, _ = logFile.WriteString(line + "\n")
	}
	logMu.Unlock()
}

func Infof(format string, args ...any)  { logf("INFO", format, args...) }
func Warnf(format string, args ...any)  { logf("WARN", format, args...) }
func Errorf(format string, args ...any) { logf("ERROR", format, args...) }
