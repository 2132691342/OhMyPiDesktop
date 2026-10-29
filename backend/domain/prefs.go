// 设置页与应用启动行为的视图：开关状态、上次会话、数据目录。
package domain

// AppPrefsVO 是一次设置页刷新需要的全部内容。
//
// 一次拉齐而不是分三个接口：设置页要同时显示「注册表里是什么」与
// 「数据目录在哪」，分三次请求只会让开关抖三下。
type AppPrefsVO struct {
	// Values 是设置 KV 全量（键见 domain.Key*）
	Values map[string]string `json:"values"`
	// AutoStart 开机自启的真实状态，以注册表为准而不是设置文件
	AutoStart bool `json:"auto_start"`
	// RestoreSession 启动时是否回到上次那个会话
	RestoreSession bool `json:"restore_session"`
	// LastSessionFile 上次那个会话的文件路径；空表示没有可恢复的
	LastSessionFile string `json:"last_session_file"`
	// LastSessionTitle 上次那个会话的标题；取不到为空
	LastSessionTitle string `json:"last_session_title,omitempty"`
	// LastEngineKind 上次那个会话属于哪个引擎
	LastEngineKind string `json:"last_engine_kind"`
	// DataDir 应用自己的落盘目录（设置文件与日志都在里面）
	DataDir string `json:"data_dir"`
	// LogDir 日志目录
	LogDir string `json:"log_dir"`
	// CloseAction 点窗口关闭按钮时做什么：tray / quit
	CloseAction string `json:"close_action"`
	// TrayHintPending 是否还没给过「收托盘」的首次提示
	TrayHintPending bool `json:"tray_hint_pending"`
}

// AutoStartREQ 开关开机自启。
type AutoStartREQ struct {
	// Enabled 目标状态。关掉时删注册表值，打开时写当前 exe 的绝对路径
	Enabled bool `json:"enabled"`
}

// CloseActionREQ 改关窗行为。
type CloseActionREQ struct {
	// Action 取值 tray（收进托盘）或 quit（退出应用）
	Action string `json:"action"`
}

// RestoreSessionREQ 开关「启动时回到上次会话」。
type RestoreSessionREQ struct {
	// Enabled 目标状态
	Enabled bool `json:"enabled"`
}

// LastSessionREQ 记住当前会话（供下次启动恢复）。
type LastSessionREQ struct {
	// EngineKind 会话所属引擎（omp / pi）
	EngineKind string `json:"engine_kind"`
	// SessionFile 会话文件绝对路径；空表示清掉记录
	SessionFile string `json:"session_file"`
}
