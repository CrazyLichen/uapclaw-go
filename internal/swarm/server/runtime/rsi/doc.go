// Package rsi 提供 AgentServer RSI（Auto Science 自进化）服务域：任务存储、
// 状态机、单队列 worker、HARNESS 引擎适配、事件链路与安装激活。
//
// 本包是 17.9a/17.9b 落点（17.9c 分发层接线见 swarm/server 包内
// rsi_handlers.go 与 agent_server.go 分支）。范围裁剪（spec D-9-1/D-9-2）：
// 仅 HARNESS 轨——不做 mock 双 provider / ArtifactEngineAdapter /
// program_threshold_provider / paper_provider；ARTIFACT 真身缝随 17.3/17.5 回填。
//
// 文件目录：
//
//	rsi/
//	├── doc.go                  # 包文档
//	├── errors.go               # 错误族 + failure_reason 助手
//	├── models.go               # RsiTask/RsiTaskView/枚举/usage/树节点契约
//	├── task_store.go           # JSON 文件存储 + 状态机 + P1 钩子
//	├── recovery.go             # 重启对账（只对账不重跑）
//	├── projector.go            # 进度/树聚合
//	├── events.go               # 引擎事件契约
//	├── event_consumer.go       # 事件链路（sink→有界队列→消费分发）
//	├── usage_recorder.go       # 用量记账
//	├── model_resolver.go       # 任务级模型解析
//	├── validation_dataset.go   # 数据集装载校验
//	├── harness_adapter.go      # HARNESS 引擎适配器（契约+build_request）
//	├── harness_provider.go     # HARNESS 引擎真身 provider
//	├── worker.go               # 单队列 worker（并发=1）
//	├── materializer.go         # 任务目录物化
//	├── artifact_files_service.go # 任务文件读取服务
//	├── services.go             # 任务服务 + 五查询服务
//	├── context.go              # 服务域装配根（RsiServiceContext）
//	├── harness_activation.go   # 激活指针存储 + installer
//	└── refs_provider.go       # harness_refs 三级解析
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/rsi/（服务域）+
// jiuwenswarm/server/rsi/（分发层，见 swarm/server rsi_handlers.go）+
// server/agent_ws_server.py 懒装配段（见 swarm/server agent_server.go）。
package rsi
