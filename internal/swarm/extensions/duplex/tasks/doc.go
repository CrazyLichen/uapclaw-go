// Package tasks 提供双工音视频的持久任务管理。
//
// 一比一移植 Python jiuwenswarm/extensions/video_duplex/backend/tasks/
// （service/store/interactions/prompts/checkpoint/errors）与
// gateway/extensions/video_duplex/backend/tasks/bridge.py（归属偏差 O-1 见 spec）。
//
// 文件目录：
//
//	tasks/
//	├── doc.go          # 包文档
//	├── errors.go      # 队列与修订冲突错误（errors.py）
//	├── prompts.go     # 面向模型的任务指令文案（prompts.py）
//	├── store.go       # 任务账本存储（store.py）
//	├── interactions.go # 交互问题解析与校验（interactions.py）
//	├── checkpoint.go  # 任务检查点（checkpoint.py）
//	├── service.go     # TaskService 持久任务队列（service.py）
//	└── bridge.go     # 网关任务端点绑定（bridge.py，偏差 O-1）
//
// 对应 Python 代码：jiuwenswarm/extensions/video_duplex/backend/tasks/
// （bridge.go 对应 gateway/extensions/video_duplex/backend/tasks/bridge.py）
package tasks
