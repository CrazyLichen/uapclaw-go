//go:build integration

// Package external 提供外部服务真实连接的集成测试。
//
// 这些测试需要真实的外部服务（ChromaDB、GaussDB、MCP 等），
// 通过环境变量配置连接信息，未配置时自动跳过。
package external
