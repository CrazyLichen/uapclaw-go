//go:build integration

// Package tools_test 提供工具注册与执行链路的集成测试。
//
// 覆盖 MapFunction / InvokeFunction 工具的注册、执行、错误传播等场景。
// 对齐 Python 各测试中 _NoteWriteTool / fakeTool 等工具使用模式。
//
// 文件目录：
//
//	tools/
//	├── doc.go                 # 包文档
//	└── tool_execution_test.go # 工具注册与执行测试
package tools_test
