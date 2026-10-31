// 会话导出的出参：引擎把 HTML 写到暂存位，服务读回内容交前端下载。
package domain

// SessionExportVO 一次会话 HTML 导出的出参。
type SessionExportVO struct {
	// FileName 建议的下载文件名（取自会话文件名）
	FileName string `json:"file_name"`
	// Content 自包含 HTML 全文（单文件，无外部资源引用）
	Content string `json:"content"`
}
