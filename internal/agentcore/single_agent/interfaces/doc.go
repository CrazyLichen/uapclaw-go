// Package interfaces 提供 single_agent 领域的核心接口定义。
//
// 将 Workflow、Agent 等接口从此包导出，供 tracer、single_agent 等包共同引用，
// 避免 tracer → single_agent → context_engine 的循环依赖。
// 接口与具体实现分离，tracer 只需依赖接口定义，不需要导入 single_agent 包本体。
//
// 文件目录：
//
//	interfaces/
//	├── doc.go           # 包文档
//	└── workflow.go      # Workflow/Agent 接口及选项类型
package interfaces
