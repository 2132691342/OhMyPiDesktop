// 设置页与应用启动行为的视图：开关状态、配置文件清单。
package domain

// ConfigFileVO 是一个可查看的引擎配置文件。
type ConfigFileVO struct {
	// FilePath 绝对路径
	FilePath string `json:"file_path"`
	// FileLabel 给人看的文件名（models.yml / settings.json …）
	FileLabel string `json:"file_label"`
	// Exists 文件现在存不存在
	Exists bool `json:"exists"`
	// SizeBytes 字节数；不存在为 0
	SizeBytes int64 `json:"size_bytes"`
}

// ConfigFilesVO 是一个引擎的配置文件清单。
type ConfigFilesVO struct {
	// EngineKind 这份清单属于哪个引擎
	EngineKind string `json:"engine_kind"`
	// Entries 文件条目，顺序固定：主配置在前
	Entries []ConfigFileVO `json:"entries"`
}
