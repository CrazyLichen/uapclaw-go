//go:build integration

// Package progressive 提供延迟工具目录（ProgressiveToolRail）集成测试。
//
// 覆盖曝光标签映射到提示词与顶层过滤、deferred 工具不可直调、
// tool_search + tool_call 闭环、目录 delta 推送、同会话不重复推送、
// schema 变更后授权失效。
//
// 对齐 Python: tests/unit_tests/harness/test_progressive_tool_rail.py
//
// 文件目录：
//
//	progressive/
//	├── doc.go                    # 包文档
//	└── progressive_rail_test.go  # 延迟工具目录测试（7 测试）
//
// 对应 Python 代码：openjiuwen/harness/rails/progressive_tool_rail.py
package progressive
