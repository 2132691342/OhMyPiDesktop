// 差异行：引擎吐出来的改动与 git 吐出来的改动统一成同一种形状。
//
// 两种来源的差异最终都渲染成同一张表，所以形状必须一致：
// 一个旧行号、一个新行号、一段正文、一个行种类。
package domain

// 差异行的种类。
const (
	// DiffLineFile 文件头（哪个文件）
	DiffLineFile = "file"
	// DiffLineHunk 区块头（@@ -a,b +c,d @@）
	DiffLineHunk = "hunk"
	// DiffLineAdd 新增行
	DiffLineAdd = "add"
	// DiffLineDel 删除行
	DiffLineDel = "del"
	// DiffLineCtx 上下文行（没改动的上下文）
	DiffLineCtx = "ctx"
)

// DiffLineVO 差异中的一行。
type DiffLineVO struct {
	// LineKind 行种类：file / hunk / add / del / ctx
	LineKind string `json:"line_kind"`
	// OldNo 旧文件行号；新增行为 0
	OldNo int `json:"old_no"`
	// NewNo 新文件行号；删除行为 0
	NewNo int `json:"new_no"`
	// LineText 正文
	LineText string `json:"line_text"`
}
