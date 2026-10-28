// 技能编辑与配置查看的入参。
package domain

// SkillSaveREQ 保存一份技能正文。
type SkillSaveREQ struct {
	// EngineKind 这个技能属于哪个引擎（决定「只能改用户目录」用的是哪份布局）
	EngineKind string `json:"engine_kind"`
	// FilePath SKILL.md 的绝对路径
	FilePath string `json:"file_path"`
	// Text 新的完整原文（含 frontmatter）
	Text string `json:"text"`
}
