// Package evolution 提供技能演进（Skill Evolution）推送层的共享辅助工具。
//
// 本包是推送层，依赖 gateway_push 包实现事件推送能力。
// 纯逻辑函数（事件分类、状态提取、Noop 检测、审批分组）已提取到 evolution/logic 子包，
// adapter 包可安全导入 logic 子包而不会产生循环依赖。
//
// 依赖方向：
//
//	evolution/logic/  ← 纯逻辑，零外部依赖（adapter 可安全导入）
//	  ↑
//	evolution/        ← 推送层，依赖 gateway_push + evolution/logic
//	  ↑ (不可逆)
//	adapter/          ← import evolution/logic（不 import evolution/）
//
// 核心功能：
//   - 推送桥接：通过 EvolutionPushContext 将演进状态推送到 Gateway 侧
//   - 广播进度：通过回调函数将演进进度事件广播到所有等待的请求
//
// 文件目录：
//
//	evolution/
//	├── doc.go            # 包文档
//	├── helpers.go        # 3 结构体 + 3 函数类型 + 4 导出推送函数
//	└── logic/            # 纯逻辑子包（无 gateway_push 依赖）
//	    ├── doc.go        # 子包文档
//	    ├── helpers.go    # 3 结构体 + 常量/变量 + 18 导出函数 + 5 非导出函数
//	    └── helpers_test.go
//
// 对应 Python 代码：jiuwenswarm/server/runtime/agent_adapter/evolution_helpers.py
package evolution
