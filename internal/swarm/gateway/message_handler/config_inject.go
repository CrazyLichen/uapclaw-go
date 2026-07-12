package message_handler

// ──────────────────────────── 导出函数 ────────────────────────────

// SetOutboundPipeline 注入 config 回调和对齐 Python set_outbound_pipeline 的 pipeline。
//
// 对齐 Python set_outbound_pipeline (L182-195)：
// 注入 pipeline、_get_config_raw 和 _update_channel_in_config 回调。
func (mh *MessageHandler) SetOutboundPipeline(
	pipeline OutboundPipeline,
	getConfigRaw func() map[string]any,
	updateChannelInConfig func(channelID string, update map[string]any),
) {
	mh.outboundPipeline = pipeline
	mh.getConfigRaw = getConfigRaw
	mh.updateChannelInConfig = updateChannelInConfig
}
