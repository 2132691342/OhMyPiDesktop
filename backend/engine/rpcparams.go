package engine

// RPC 参数名集中在引擎层。
//
// 为什么不让 service 直接写 map[string]any{"subagentId": ...}：协议字段名属于引擎契约，
// 一旦写错**不会报错**——引擎把它当成缺参，回 success:false 或者干脆静默不做，
// 界面表现为「点了没反应」。这类错已经踩过两次（`id` vs `subagentId`、
// `provider` vs `providerId`），所以把名字收口到这里，并由测试对着真实命令表钉住。
//
// 只收「我们真的会发、且曾经写错过」的那几条；不要为了整齐把所有命令都包一层。

// SteerSubagentParams 给子代理插一句话。
func SteerSubagentParams(subagentID, message string) map[string]any {
	return map[string]any{"subagentId": subagentID, "message": message}
}

// CancelSubagentParams 硬杀一个子代理。
func CancelSubagentParams(subagentID string) map[string]any {
	return map[string]any{"subagentId": subagentID}
}

// SubagentMessagesParams 按字节游标读子代理对话。
func SubagentMessagesParams(subagentID string, fromByte int64) map[string]any {
	return map[string]any{"subagentId": subagentID, "fromByte": fromByte}
}

// LoginParams 发起 OAuth。参数名是 providerId，不是 provider。
func LoginParams(providerID string) map[string]any {
	return map[string]any{"providerId": providerID}
}
