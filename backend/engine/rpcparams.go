package engine

// RPC 参数名集中在引擎层：协议字段名属于引擎契约，写错不会报错——引擎当成缺参回 success:false 或静默不做，
// 界面表现为「点了没反应」。易错的形状就是 `id` vs `subagentId`、`provider` vs `providerId` 这类，
// 所以名字收口到这里并由测试对着真实命令表钉住；只收我们真的会发的那几条，不为整齐把所有命令都包一层。

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
