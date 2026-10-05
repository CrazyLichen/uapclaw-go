//go:build integration

// Package workflow 提供工作流基础类型的集成测试。
//
// 测试覆盖：
//   - WorkflowOutput 构造与状态断言
//   - WorkflowExecutionState 枚举值
//   - 状态转换逻辑
//   - 错误传播模式
//
// 文件目录：
//
//	workflow/
//	├── doc.go              # 包文档
//	└── workflow_test.go    # Workflow 基础类型集成测试
//
// 对应 Python 代码：tests/system_tests/workflow/test_real_workflow.py
package workflow
