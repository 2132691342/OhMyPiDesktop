// 工作区文件面板的视图：只读列举与单文件预览。
package domain

// FileEntryVO 目录里的一个条目。
type FileEntryVO struct {
	Name string `json:"name"`
	Dir  bool   `json:"dir"`
	Size int64  `json:"size,omitempty"`
}

// FileListVO 一次目录列举的出参。Path 是相对工作区的目录路径，空串即根目录。
type FileListVO struct {
	Path    string        `json:"path"`
	Entries []FileEntryVO `json:"entries"`
	// Truncated 为真表示条目被上限截断，界面要给一句「只看前 N 条」。
	Truncated bool `json:"truncated"`
}

// FileReadVO 单文件预览。
type FileReadVO struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Size     int64  `json:"size"`
	Binary   bool   `json:"binary"`
	Truncated bool  `json:"truncated"`
}
