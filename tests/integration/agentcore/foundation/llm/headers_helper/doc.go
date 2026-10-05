//go:build integration

// Package headers_helper 提供 agentcore/foundation/llm/headers_helper 模块的集成测试。
//
// 测试覆盖：
//   - BuildBaseHeaders 配置级 headers 构建
//   - MergeHeadersCaseInsensitive 大小写不敏感合并
//   - MergeRequestHeaders 配置级 + 请求级合并
//   - SanitizeHeaders 清洗受保护头部和空值
//   - IsProtectedHeader 头部保护检查
//   - ProtectedHeaders 受保护头部集合
//   - 空/nil 输入安全处理
//   - ModelClientConfig 自定义头部与 headers_helper 交互
//
// 文件目录：
//
//	headers_helper/
//	├── doc.go                    # 包文档
//	└── headers_helper_test.go    # Headers 辅助函数集成测试
//
// 对应 Python 代码：openjiuwen/core/foundation/llm/headers_helper.py
// 对齐 Python 测试：tests/system_tests/foundation/llm/test_custom_headers_system.py
package headers_helper
