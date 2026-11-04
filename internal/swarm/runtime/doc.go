// Package runtime 提供 host 侧运行时协调能力（plan 状态机、请求模式解析）。
//
// 该包对齐 Python `jiuwenswarm/runtime/`：把"每轮该处于什么运行模式"与
// "计划状态该不该变"从 adapter 里抽出来，由控制器统一裁决。
//
// 文件目录：
//
//	runtime/
//	├── doc.go     # 包文档
//	├── request.go # 请求模式解析与 chat 方法判定
//	└── plan.go    # PlanModeController 计划状态机
//
// 对应 Python 代码：jiuwenswarm/runtime/
package runtime
