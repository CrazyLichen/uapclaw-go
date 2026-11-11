// Package anthropic 提供 Anthropic Messages API（/v1/messages）模型客户端。
// 适用于 https://api.anthropic.com 与 https://openrouter.ai/api 等
// Anthropic 形状端点。提示词缓存断点布局：tools → system → messages。
//
// 设计要点（spec 2026-11-11-domain23.1-anthropic-client-design.md）：
//   - 不引 anthropic SDK（P-3）：手写 HTTP + SSE，SDK keepalive 语义由
//     transport_pool 五元组分桶 + 100/20/60s 参数替代
//   - reasoning 接线复用 llm/reasoning 包（23.3 交付，spec D-7 回收）
//
// 文件目录：
//
//	anthropic/
//	├── doc.go              # 包文档
//	├── convert_blocks.go   # 内容块归一/图片/缓存标记/工具 schema 转换
//	├── convert_replay.go   # thinking 回放块白名单与流式累积
//	├── convert_messages.go # 消息列表转换 + system 分区 + 缓存断点
//	├── build_params.go     # BuildAnthropicParams（reasoning 接线/采样互斥/上限）
//	├── usage.go            # UsageFromAnthropic
//	├── parse_response.go   # 非流式解析
//	├── event_chunk.go      # SSE 事件 → chunk
//	└── client.go          # AnthropicModelClient + Invoke/Stream + init 注册
//
// 对应 Python 代码：
//
//	openjiuwen/core/foundation/llm/model_clients/anthropic_model_client.py（1,633 行）
package anthropic
