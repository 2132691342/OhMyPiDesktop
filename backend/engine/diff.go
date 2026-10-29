package engine

// 工具差异的提取与解析。
//
// 两个引擎都把差异放在工具结果的 details 里，但格式不同：
//
//	omp: details.diff 是「编号差异」——@@ 区块头 + "+42|正文" 这样的行，
//	     行号是它自己算的（删行为旧行号，增行为新行号，行为旧行号）。
//	pi:  details.diff 同样是编号差异但用空格分隔（"+42 正文"），
//	     另有一份 details.patch 才是标准 unified diff。
//
// 我们不猜格式：按行首字符判定，再看分隔符是竖线还是空格。
// 判错的后果是整份差异渲染成一堆乱码，而且用户看不出是界面坏了。

import (
	"encoding/json"
	"strconv"
	"strings"

	"OhMyPiDesktop/backend/domain"
)

// DiffKind 是差异行的种类。取值与 domain 的一致，这里用短名省得反复写包名。
const (
	DiffKindFile = domain.DiffLineFile
	DiffKindHunk = domain.DiffLineHunk
	DiffKindAdd  = domain.DiffLineAdd
	DiffKindDel  = domain.DiffLineDel
	DiffKindCtx  = domain.DiffLineCtx
)

// toolDetails 是工具结果里我们关心的那部分。
//
// 只声明需要的字段：工具结果的 details 形状每个引擎、每个工具都不一样，
// 声明得越多，越容易在某个引擎改版时静默失配。
type toolDetails struct {
	// Diff 编号差异（omp 与 pi 都有）
	Diff string `json:"diff"`
	// Patch 标准 unified diff（只有 pi 的 edit 工具给）
	Patch string `json:"patch"`
	// Path 被改动的文件路径（omp 的 edit 工具给）
	Path string `json:"path"`
	// PerFileResults 多文件改动的逐文件结果（omp 的 edit 工具给）
	PerFileResults []toolDetails `json:"perFileResults"`
}

// ExtractToolDiff 从工具结果里取出差异文本与被改动的文件路径。
//
// 入参有两种形状，两处都要支持：
//
//	流式帧: {"content":[...],"details":{"diff":"..."}}  —— details 嵌在 result 里
//	历史消息: {"diff":"..."}                        —— details 本身就是那个对象
//
// 只认一种的后果是「刚聊完能看到差异，刷新一下就没了」——
// 而用户会以为引擎把改动撤销了。
//
// 取的顺序有讲究：
//  1. patch 优先——它是标准 unified diff，能直接给出新旧行号；
//  2. 没有 patch 再用 diff——那是编号差异，行号要另算。
//
// 两个都空的工具（read / bash / grep）本来就没有差异，返回空不是错误。
func ExtractToolDiff(result json.RawMessage) (diffText, filePath string) {
	if len(result) == 0 {
		return "", ""
	}
	d := parseToolDetails(result)
	if d == nil {
		return "", ""
	}
	// 多文件改动：逐文件的差异各算一份，用文件头串起来。
	// 只取第一份会让用户看不到其余文件的改动，而 omp 的 edit 一次能改多个文件。
	if len(d.PerFileResults) > 0 {
		var sb strings.Builder
		for _, one := range d.PerFileResults {
			text := one.Patch
			if text == "" {
				text = one.Diff
			}
			if text == "" {
				continue
			}
			if sb.Len() > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString("--- ")
			sb.WriteString(one.Path)
			sb.WriteString("\n")
			sb.WriteString(text)
			if filePath == "" {
				filePath = one.Path
			}
		}
		if sb.Len() > 0 {
			return sb.String(), filePath
		}
		return "", filePath
	}
	if d.Patch != "" {
		return d.Patch, d.Path
	}
	if d.Diff != "" {
		return d.Diff, d.Path
	}
	return "", d.Path
}

// ToolFilePath 从工具参数里取出被改动的文件路径。
//
// 为什么要有这个：pi 的 edit 工具把路径放在参数里（details 里没有），
// 而 omp 把它放在 details.path。两条路都要覆盖——
// 缺了任何一条，那个引擎的「看工作区」入口就是空的。
func ToolFilePath(args json.RawMessage) string {
	if len(args) == 0 {
		return ""
	}
	var a struct {
		Path     string `json:"path"`
		FilePath string `json:"file_path"`
	}
	if json.Unmarshal(args, &a) != nil {
		return ""
	}
	if a.Path != "" {
		return a.Path
	}
	return a.FilePath
}

// parseToolDetails 从两种形状里认出工具细节。
//
// 判定办法：先按「details 嵌在里面」解一次，解出的对象里没有 diff 字段
// 就退回「本身就是 details」。
func parseToolDetails(raw json.RawMessage) *toolDetails {
	var wrapped struct {
		Details *toolDetails `json:"details"`
	}
	if json.Unmarshal(raw, &wrapped) == nil && wrapped.Details != nil {
		return wrapped.Details
	}
	var direct toolDetails
	if json.Unmarshal(raw, &direct) != nil {
		return nil
	}
	// 两者都没有差异字段：不是一次改文件的调用。
	if direct.Diff == "" && direct.Patch == "" && len(direct.PerFileResults) == 0 {
		return nil
	}
	return &direct
}

// ParseToolDiff 把差异文本解析成统一的行结构。
//
// 两种输入格式：
//
//	unified:  @@ -12,7 +12,9 @@  + 新增行 / - 删除行 / 空格 上下文行
//	numbered: @@ -12,7 +12,9 @@  +42|新增行 / -41|删除行 / 40|上下文行
//
// 两种都带 @@ 区块头，所以先认区块头，再按行首字符决定行种类。
// numbered 格式的正文与行号之间是竖线或空格：竖线是 omp，空格是 pi。
func ParseToolDiff(text string) []domain.DiffLineVO {
	out := []domain.DiffLineVO{}
	if text == "" {
		return out
	}
	oldNo, newNo := 0, 0
	for _, row := range strings.Split(text, "\n") {
		row = strings.TrimRight(row, "\r")
		if row == "" {
			continue
		}
		if strings.HasPrefix(row, "@@") {
			oldNo, newNo = parseHunkHeader(row)
			out = append(out, domain.DiffLineVO{LineKind: DiffKindHunk, LineText: row})
			continue
		}
		// 文件头（"--- a/x" / "+++ b/x" / "diff --git"）不是内容行。
		// 当成增删行渲染的话，用户会看到文件头被涂成红绿两色——
		// 那是假的改动。
		if isDiffFileHeader(row) {
			out = append(out, domain.DiffLineVO{LineKind: DiffKindFile, LineText: row})
			continue
		}
		switch row[0] {
		case '+':
			body, no := splitNumbered(row[1:])
			if no > 0 {
				newNo = no
			}
			out = append(out, domain.DiffLineVO{LineKind: DiffKindAdd, NewNo: newNo, LineText: body})
			newNo++
		case '-':
			body, no := splitNumbered(row[1:])
			if no > 0 {
				oldNo = no
			}
			out = append(out, domain.DiffLineVO{LineKind: DiffKindDel, OldNo: oldNo, LineText: body})
			oldNo++
		case ' ':
			body, no := splitNumbered(row[1:])
			if no > 0 {
				oldNo = no
				newNo = no
			}
			out = append(out, domain.DiffLineVO{LineKind: DiffKindCtx, OldNo: oldNo, NewNo: newNo, LineText: body})
			oldNo++
			newNo++
		case '\\':
			// "\ No newline at end of file"：说明性行，不计入行号。
			out = append(out, domain.DiffLineVO{LineKind: DiffKindCtx, LineText: row})
		default:
			out = append(out, domain.DiffLineVO{LineKind: DiffKindCtx, LineText: row})
		}
	}
	return out
}

// isDiffFileHeader 判断这一行是不是 unified diff 的文件头。
//
// 文件头有三种形态："--- a/x"、"+++ b/x"、"diff --git a/x b/x"。
// 它们都以 - 或 + 开头，不加区分的话会被当成增删行——
// 于是用户看到文件头被涂成红色与绿色，而那是根本不存在的改动。
func isDiffFileHeader(row string) bool {
	if strings.HasPrefix(row, "diff --git ") {
		return true
	}
	if strings.HasPrefix(row, "--- ") || strings.HasPrefix(row, "+++ ") {
		return true
	}
	return false
}

// parseHunkHeader 从 "@@ -12,7 +12,9 @@" 里取出新旧起始行号。
//
// 取不到就返回 0：这时后面按顺序累加的行号会不准，但至少正文还在。
func parseHunkHeader(row string) (oldStart, newStart int) {
	fields := strings.Fields(row)
	for _, f := range fields {
		if len(f) < 2 {
			continue
		}
		sign, rest := f[0], f[1:]
		switch sign {
		case '-':
			oldStart = leadingNumber(rest)
		case '+':
			newStart = leadingNumber(rest)
		}
	}
	return oldStart, newStart
}

// leadingNumber 取一串开头的数字。
func leadingNumber(s string) int {
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0
	}
	n, err := strconv.Atoi(s[:end])
	if err != nil {
		return 0
	}
	return n
}

// splitNumbered 从编号差异的一行里拆出「行号」与「正文」。
//
// 有行号就返回它，没有就返回 0（调用方据此决定用累加的行号）。
func splitNumbered(body string) (text string, lineNo int) {
	if i := strings.IndexByte(body, '|'); i > 0 {
		if n := leadingNumber(body[:i]); n > 0 {
			return body[i+1:], n
		}
	}
	// pi 的编号差异用空格分隔："+42 正文"。
	if i := strings.IndexByte(body, ' '); i > 0 {
		if n := leadingNumber(body[:i]); n > 0 {
			return body[i+1:], n
		}
	}
	return body, 0
}
