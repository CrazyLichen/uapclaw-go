// Package rsi 提供跨 RSI 场景（harness 演进 / artifact 优化）共享的
// 数据契约、引擎事件与模型用量记账。
//
// RSI（自我改进）流水线的最底层基础件：schema 是统一表格模板（15 个契约），
// events 是统一广播频道（先落盘再广播、错误不吞），usage 是装在模型调用
// 回路上的"电表"（按 call_id 去重、缺计数保留累计、jsonl 流水账、恢复重放）。
// 17.2-17.9 各引擎/provider 与 AgentServer 服务域都在这套契约上工作。
//
// 文件目录：
//
//	rsi/
//	├── doc.go     # 包文档
//	├── schema.go  # 共享数据契约（15 个 frozen dataclass 等价物）
//	├── events.go  # 引擎事件契约（EngineEvent 接口 + 5 事件 + Emit）
//	└── usage.go  # 用量归一化 + ModelUsageObserver + ctx 标签与公共钩子
//
// 对应 Python 代码：openjiuwen/rsi/{schema,events,usage}.py
package rsi
