package message_handler

// ──────────────────────────── 导出函数 ────────────────────────────

// SetOutboundPipeline 注入出站管线和 config 回调。
//
// Python: set_outbound_pipeline (L182-195)：注入 pipeline、_get_config_raw 和
// _update_channel_in_config 回调。Go 合并三参一处（偏差③：既有 Go 结构不动，
// Python 双 setter 各注一半——config 回调与出站管线绑定注入）。
func (mh *MessageHandler) SetOutboundPipeline(
	pipeline OutboundPipeline,
	getConfigRaw func() map[string]any,
	updateChannelInConfig func(channelID string, update map[string]any),
) {
	mh.outboundPipeline = pipeline
	mh.getConfigRaw = getConfigRaw
	mh.updateChannelInConfig = updateChannelInConfig
}

// SetInboundPipeline 注入入站管线。
// Python: set_inbound_pipeline (L179-180)
func (mh *MessageHandler) SetInboundPipeline(pipeline InboundPipelineAPI) {
	mh.inboundPipeline = pipeline
}
