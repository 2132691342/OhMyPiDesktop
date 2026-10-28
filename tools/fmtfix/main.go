// gofmt 一次（保留原行尾）。仓库里多数文件是 CRLF，本地 gofmt 会把它们当格式问题，
// 所以先归一化成 LF 交 gofmt，再按原来的行尾写回去。
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	roots := os.Args[1:]
	if len(roots) == 0 {
		roots = []string{"backend", "tools", "main.go", "tray.go", "assets.go"}
	}
	files := make([]string, 0, 128)
	for _, root := range roots {
		if strings.HasSuffix(root, ".go") {
			files = append(files, root)
			continue
		}
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info == nil || info.IsDir() {
				return nil
			}
			if strings.HasSuffix(path, ".go") {
				files = append(files, path)
			}
			return nil
		})
	}
	changed, failed := 0, 0
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			fmt.Println("读失败:", path, err)
			failed++
			continue
		}
		crlf := strings.Contains(string(raw), "\r\n")
		norm := strings.ReplaceAll(string(raw), "\r\n", "\n")
		tmp := filepath.Join(os.TempDir(), "gofmt-tmp.go")
		if err := os.WriteFile(tmp, []byte(norm), 0o644); err != nil {
			failed++
			continue
		}
		out, err := exec.Command("gofmt", "-l", tmp).Output()
		if err != nil {
			fmt.Println("gofmt 失败:", path, err)
			failed++
			continue
		}
		if len(strings.TrimSpace(string(out))) == 0 {
			continue
		}
		formatted, err := exec.Command("gofmt", tmp).Output()
		if err != nil {
			fmt.Println("gofmt 失败:", path, err)
			failed++
			continue
		}
		body := string(formatted)
		if crlf {
			body = strings.ReplaceAll(body, "\n", "\r\n")
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			failed++
			continue
		}
		changed++
		fmt.Println("已格式化:", path)
	}
	fmt.Printf("检查 %d 个文件，格式化 %d 个，失败 %d 个\n", len(files), changed, failed)
	if failed > 0 {
		os.Exit(1)
	}
}
