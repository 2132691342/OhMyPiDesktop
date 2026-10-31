// 工作区文件面板的视图：只读列举与单文件预览。
package domain

// FileEntryVO 目录里的一个条目。
type FileEntryVO struct {
	// EntryName 文件或目录名
	EntryName string `json:"entry_name"`
	// IsDir 是目录
	IsDir bool `json:"dir"`
	// SizeBytes 字节数；目录不给
	SizeBytes int64 `json:"size,omitempty"`
}

// FileListVO 一次目录列举的出参。
type FileListVO struct {
	// DirPath 相对工作区的目录路径，空串即根目录
	DirPath string `json:"dir_path"`
	// Entries 条目，目录在前
	Entries []FileEntryVO `json:"entries"`
	// Truncated 为真表示条目被上限截断，界面要给一句「只看前 N 条」。
	Truncated bool `json:"truncated"`
}

// FileHitVO 全工作区搜索的一个命中。
type FileHitVO struct {
	// FilePath 相对工作区的路径（正斜杠），@ 引用直接用它
	FilePath string `json:"file_path"`
	// EntryName 文件名（不含目录），列表主标题
	EntryName string `json:"entry_name"`
}

// FileSearchVO 一次全工作区搜索的出参。
type FileSearchVO struct {
	// Query 原样返回的搜索词
	Query string `json:"query"`
	// Hits 命中，路径浅的在前
	Hits []FileHitVO `json:"hits"`
	// Truncated 为真表示命中数到了上限，界面要提示「结果不止这些」。
	Truncated bool `json:"truncated"`
}

// FileReadVO 单文件预览。
type FileReadVO struct {
	// FilePath 相对工作区的路径
	FilePath string `json:"file_path"`
	// Content 文件内容（文本文件）；二进制文件为空
	Content string `json:"content"`
	// SizeBytes 文件总字节数；Content 可能只是前面一段
	SizeBytes int64 `json:"size"`
	// IsBinary 二进制文件：只给标记，不把 PNG 读成乱码
	IsBinary bool `json:"binary"`
	// Truncated 内容被上限截断
	Truncated bool `json:"truncated"`
}
