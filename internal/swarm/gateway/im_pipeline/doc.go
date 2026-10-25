// Package im_pipeline 提供数字分身（group_digital_avatar）模式的入站/出站预处理管线。
//
// 入站管线（IMInboundPipeline）挂在 MessageHandler.forwardLoop 步骤6：
// 群聊消息相关性过滤（@提及/必答规则放行；否则 LLM 改写消息为更清晰的请求）+
// PendingInteraction（11.8）追问上下文回填 + resume 消息放行。
// 出站管线（IMOutboundPipeline）挂在 MessageHandler.PublishRobotMessages 入队前：
// 回复投递路由决策（LLM 分类 DM/CHAT + 关键词兜底，判定私发时写 reply_scope=dm）+
// [群聊追问@xx]/[私聊追问] 前缀解析创建 PendingInteraction 落盘。
//
// 本期（11.12）零适配器运行：管线全放行，Web 通道行为零变化；
// 真实平台适配器（飞书 11.16 / 企微 11.21）落地后经 RegisterAdapter 注册生效。
//
// 文件目录：
//
//	im_pipeline/
//	├── doc.go               # 包文档
//	├── im_inbound.go        # 入站管线：IMPlatformAdapter 接口 + IMConversationProcessor + IMInboundPipeline + 包级 helpers
//	├── im_inbound_test.go   # 入站单元测试（fakeAdapter/fakeLLMClient 共享辅助）
//	├── im_outbound.go       # 出站管线：IMOutboundPipeline（路由决策 + 追问前缀 + PendingInteraction 创建）
//	└── im_outbound_test.go  # 出站单元测试
//
// 对应 Python 代码：jiuwenswarm/gateway/im_pipeline/
package im_pipeline
