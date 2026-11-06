// Package usage 提供上下文用量快照：按类别拆分（系统提示词/工具/技能/消息）、
// 结合 provider 上报的权威输入量做归因，并聚合会话级 KV cache 命中率。
//
// 产物是 `context-usage.v1` 事件载荷，Web 前端按此 schema 校验后渲染用量指示器；
// 缺字段会被前端整条丢弃（见前端 features/contextUsage/contextUsageModel.ts）。
//
// 文件目录：
//
//	usage/
//	├── doc.go            # 包文档
//	├── models.go         # 快照与分类模型、序列化
//	├── analyzer.go       # 分类统计与 provider 归因，产出快照
//	└── session_aggregator.go # 会话级 KV cache 聚合
//
// 对应 Python 代码：openjiuwen/core/context_engine/usage/
package usage
