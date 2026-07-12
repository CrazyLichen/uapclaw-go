package message_handler

// ──────────────────────────── 导出函数 ────────────────────────────

// SetOutboundPipeline 注入 config 回调和对齐 Python set_outbound_pipeline 的 pipeline。
//
// 对齐 Python set_outbound_pipeline (L182-195)：
// 注入 _get_config_raw 和 _update_channel_in_config 回调。
// outboundPipeline 字段预留给 11.12 IM Pipeline 回填。
func (mh *MessageHandler) SetOutboundPipeline(
	getConfigRaw func() map[string]any,
	updateChannelInConfig func(channelID string, update map[string]any),
) {
	mh.getConfigRaw = getConfigRaw
	mh.updateChannelInConfig = updateChannelInConfig
	// TODO: outboundPipeline 注入（等 11.12 IM Pipeline 回填）
}
