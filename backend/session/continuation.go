package session

// 续接链：引擎写不进自己会话文件时（别的进程占着）会续接到同目录新文件，
// 头里记着 continuationOf。列表把源文件折叠掉，绑旧文件时跟到链尾再绑。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// ContinuationTip 沿续接链走到尾：这条会话的对话若已在别的文件里继续，
// 返回最新的那个文件；没有续接就返回空串。omp 的 switch_session 不跟链，
// 绑到一个已被续接的旧文件等于把同一段对话劈成两半各写各的。
func (ix *Index) ContinuationTip(path string) string {
	cur := path
	for range 8 { // 链长以写冲突次数为限；上限只为防环
		next := ix.continuationSuccessor(cur)
		if next == "" {
			if cur == path {
				return ""
			}
			return cur
		}
		cur = next
	}
	return cur
}

// continuationSuccessor 在同目录里找 continuationOf 指向 path 那条会话的文件。
func (ix *Index) continuationSuccessor(path string) string {
	id := idFromName(filepath.Base(path))
	if id == "" {
		return ""
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		candidate := filepath.Join(filepath.Dir(path), entry.Name())
		if candidate == path {
			continue
		}
		if ix.headContinuation(candidate) == id {
			return candidate
		}
	}
	return ""
}

// headContinuation 只读文件头 4KB 取 continuationOf：头在文件最前面，
// 整文件读一遍只为一个字段太重（会话文件可能几 MB）。
func (ix *Index) headContinuation(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	head := make([]byte, 4*1024)
	n, _ := f.Read(head)
	var parsed struct {
		Type           string `json:"type"`
		ContinuationOf string `json:"continuationOf"`
	}
	// 头两行是 title 槽位与 session 头；逐行找 session 头，槽位的尾巴是不完整行，跳过即可。
	for _, line := range strings.SplitN(string(head[:n]), "\n", 4) {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		if json.Unmarshal([]byte(line), &parsed) == nil && parsed.Type == "session" {
			return parsed.ContinuationOf
		}
	}
	return ""
}
