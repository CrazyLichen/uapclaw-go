// Package runtime 提供 host 侧运行时编排能力（运行时事件、chat turn 编排、请求解析、plan 状态机）。
//
// 该包对齐 Python `jiuwenswarm/runtime/`：把"选 agent、每轮该处于什么运行模式、
// 计划状态该不该变、控制事件怎么下发"从 adapter 与 server 里抽出来，由运行时统一裁决。
//
// 文件目录：
//
//	runtime/
//	├── doc.go     # 包文档
//	├── events.go  # RuntimeEvent 与响应/片段包装
//	├── service.go # AgentRuntime（Invoke/Stream 编排、控制事件发射）
//	├── request.go # 请求模式解析（含 Web 组合）、工程目录解析、metadata 同步、chat turn 准备、取消请求
//	└── plan.go    # PlanModeController 计划状态机
//
// 对应 Python 代码：jiuwenswarm/runtime/（service.py / request.py / plan.py / events.py）
package runtime
