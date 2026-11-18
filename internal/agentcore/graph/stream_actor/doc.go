// Package stream_actor 提供 workflow 流式输出的数据面中枢：把上游节点的过程性流式 chunk
// 按 stream_edges 路由给下游消费者的每条输入路径（schema 叶子路径），并按 source_groups
// 的 CNF 语义关流。
//
// 与 Pregel 控制面（loop/channel/routers）并行：控制面传图调度消息，本包传流式内容帧，
// 两者仅在 Vertex（8.11）执行体内部交汇。对应 Python: openjiuwen/core/graph/stream_actor/。
//
// 文件目录：
//
//	stream_actor/
//	├── doc.go           # 包文档
//	├── base.go         # StreamConsumer/StreamGraph/StreamPayload/StreamActor/StreamProcessor/帧工具/无界队列
//	└── manager.go      # StreamTransform/ActorManager/buildReverseGraph
//
// 并发模型（偏差 D1-D3，见 spec §4）：goroutine+chan 直译 asyncio；主/路径队列为带锁无界
// 内存队列（asyncio.Queue 无界语义，D2）；processor.run 启动点在 consume 注册之后（D1）；
// 所有阻塞点 select ctx/timeout 保证可取消。
package stream_actor
