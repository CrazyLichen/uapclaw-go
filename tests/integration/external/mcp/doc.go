//go:build integration

// Package openapi 提供 MCP / OpenAPI 工具客户端的集成测试。
//
// 被测边界是外部 OpenAPI 服务：需要可访问的真实服务端才能跑通，缺省 Skip，
// 因此归入集成测试而非单元测试。
//
// 文件目录：
//
//	mcp/
//	├── doc.go         # 包文档
//	└── openapi_test.go # OpenAPI 客户端调用用例
//
// 对应 Python 代码：openjiuwen/core/foundation/tool/mcp/
package openapi
