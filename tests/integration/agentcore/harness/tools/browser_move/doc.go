//go:build integration

// Package browser_move 提供 BrowserMove 模块的集成测试。
//
// 测试覆盖：
//   - BrowserRuntimeRail 构造与优先级
//   - GetCallbacks 返回的钩子映射完整性
//   - BrowserConfig 默认值与校验
//   - IsBrowserProgressTool 工具名判断
//   - ExtractProgressPayload 进度标签提取
//   - AgentRail 接口满足性
//
// 文件目录：
//
//	browser_move/
//	├── doc.go                 # 包文档
//	└── browser_tools_test.go # BrowserMove 工具集成测试
//
// 对应 Python 代码：tests/system_tests/harness/tools/browser_move/test_browser_tools.py
package browser_move
