// 引擎健康度：一眼回答「这台机器上两个引擎各自是什么状态」。
package domain

// EngineHealthVO 是单个引擎的健康快照。
//
// 它把散在三处的事实合成一份：探测结果（装没装、什么版本）、
// 进程事实（在不在跑、跑了多久、最近有没有报错）、落盘现状
// （会话几个、技能几个、配置文件在不在）。
// 分开查要三四个请求，而这一屏要的就是「合起来看一眼」。
type EngineHealthVO struct {
	// EngineKind 引擎种类（omp / pi）
	EngineKind string `json:"engine_kind"`
	// EngineLabel 给人看的名字
	EngineLabel string `json:"engine_label"`
	// Installed 探测到了可执行文件
	Installed bool `json:"installed"`
	// ExePath 可执行文件绝对路径；未安装为空
	ExePath string `json:"exe_path"`
	// Version 本地版本；未安装为空
	Version string `json:"version"`
	// ChannelKind 安装渠道；只决定卸载按钮存不存在
	ChannelKind string `json:"channel_kind"`
	// ChannelLabel 渠道的人话说明
	ChannelLabel string `json:"channel_label"`
	// LatestVersion 上游最新版；查不到为空
	LatestVersion string `json:"latest_version"`
	// Outdated 本地落后于上游
	Outdated bool `json:"outdated"`
	// InstallHint 没装时给的安装命令原文
	InstallHint string `json:"install_hint"`

	// Running 有进程在跑
	Running bool `json:"running"`
	// Busy 正在流式回答
	Busy bool `json:"busy"`
	// PID 进程号；没在跑为 0
	PID int `json:"pid"`
	// Protocol 协议版本；握手完成才有
	Protocol int `json:"protocol"`
	// StartedAt 进程拉起时刻（Unix 毫秒）
	StartedAt int64 `json:"started_at"`
	// CallCount 这个进程处理过的命令数
	CallCount int64 `json:"call_count"`
	// LastError 最近一次命令失败原文
	LastError string `json:"last_error,omitempty"`

	// SessionCount 会话文件数
	SessionCount int `json:"session_count"`
	// SkillCount 技能数
	SkillCount int `json:"skill_count"`
	// PluginCount 登记的插件数
	PluginCount int `json:"plugin_count"`
	// McpCount MCP 服务器数
	McpCount int `json:"mcp_count"`

	// SessionCountKnown 会话数是不是真扫出来的。
	//
	// 四个 known 与上面的四个计数成对出现：计数拿不到时（落盘布局解不出来、
	// 目录读不动）必须承认「不知道」，不能拿零值冒充 0 —— 界面显示「0 个会话」
	// 是一个断言，而实际情况可能是「没读到」。后者显示「—」。
	SessionCountKnown bool `json:"session_count_known"`
	// SkillCountKnown 技能数是不是真扫出来的
	SkillCountKnown bool `json:"skill_count_known"`
	// PluginCountKnown 插件数是不是真扫出来的
	PluginCountKnown bool `json:"plugin_count_known"`
	// McpCountKnown MCP 服务器数是不是真扫出来的
	McpCountKnown bool `json:"mcp_count_known"`

	// ConfigFiles 配置文件现状（路径 / 在不在 / 多大）
	ConfigFiles []ConfigFileVO `json:"config_files"`
	// RootDir 引擎的根目录（~/.omp 或 ~/.pi）
	RootDir string `json:"root_dir"`
	// AgentDir 当前 profile 的 agent 目录
	AgentDir string `json:"agent_dir"`
	// DirExists agent 目录是否真的存在（「装过」的弱证据）
	DirExists bool `json:"dir_exists"`
	// LayoutResolved 落盘布局是否解出来了。
	//
	// 解不出来时上面的路径类字段（root_dir / agent_dir / config_files）为空，
	// 但计数仍然可能有效——两者失败原因不同，不许用一个 early return 一起砍掉。
	LayoutResolved bool `json:"layout_resolved"`
	// ProbeUnknownReason 探测没能给出「装没装」这个结论的原因。
	//
	// 三种原因给用户的下一步完全不同，混成一句「未安装」会让用户去
	// 装一个其实已经装好的引擎：
	//   missing    PATH 与显式路径上都没有候选 —— 真的没装
	//   bad-path   显式路径配了，但那个文件不可用 —— 去改设置里的路径
	//   timeout    探测命令超时没能给出版本 —— 可能是装了但跑不起来
	// 空串表示探测已经给出结论（installed 可信）。
	ProbeUnknownReason string `json:"probe_unknown_reason,omitempty"`

	// Caps 能力表
	Caps map[string]bool `json:"caps"`
}

// HealthVO 是两个引擎的健康快照，加一份全局计数。
type HealthVO struct {
	// Engines 每个引擎一份，顺序固定 omp → pi
	Engines []EngineHealthVO `json:"engines"`
	// ActiveKind 当前选中的引擎
	ActiveKind string `json:"active_kind"`
	// Refreshing 有后台补验在跑（探测 / 计数 / 最新版）
	//
	// 它是「现在屏幕上这些数字可能比实际旧一点」的唯一信号。
	// 界面据此给一个不阻断操作的提示，而不是让用户以为已经是最新。
	Refreshing bool `json:"refreshing"`
	// Detecting 引擎探测正在进行中
	//
	// 它与 Refreshing 分开：Refreshing 是「数字可能旧了」，
	// Detecting 是「还不知道装没装」。后者必须区分，
	// 否则首发时界面会把「还没探到」显示成「没装」——
	// 而用户看到的是一个尚未成立的事实。
	Detecting bool `json:"detecting"`
	// Workspace 当前工作目录
	Workspace string `json:"workspace"`
	// DetectedAt 上次探测完成的时刻（Unix 毫秒）；从未探过时为 0
	//
	// 它回答「屏幕上这个版本号是什么时候验的」。用请求时刻冒充它，
	// 界面就会永远显示「刚刚检测过」，用户就无法判断可信度。
	DetectedAt int64 `json:"detected_at"`
	// TotalSessions 两个引擎的会话总数
	TotalSessions int `json:"total_sessions"`
	// TotalSkills 两个引擎的技能总数
	TotalSkills int `json:"total_skills"`
	// TotalPlugins 两个引擎的插件总数
	TotalPlugins int `json:"total_plugins"`
	// TotalMcp 两个引擎的 MCP 服务器总数
	TotalMcp int `json:"total_mcp"`
	// TotalsPartial 上面四个合计里至少有一项没能读到真实值。
	//
	// 未知的那一格在合计里算作 0，于是合计是一个**偏小的真话**而不是假话；
	// 但界面必须知道它是偏小的——否则「共 12 个技能」少了 pi 那一份，
	// 读的人会当它数全了。
	TotalsPartial bool `json:"totals_partial"`
	// LiveEngines 当前存活的引擎实例数（键是「引擎 + 工作目录」）
	//
	// 它是「有没有进程泄漏」的唯一可见信号：用户切目录会新建实例，
	// 空闲的会被回收。这个数字长期只增不减就说明回收没生效。
	LiveEngines int `json:"live_engines"`
	// MaxEngines 实例数上限；达到后按最久未用淘汰
	MaxEngines int `json:"max_engines"`
}
