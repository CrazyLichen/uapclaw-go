//go:build llm

package graph_memory

// 此文件包含需要真实 LLM 服务的集成测试。
// 运行方式: go test -tags=llm ./internal/agentcore/memory/graph/graph_memory/...
//
// 当前为占位文件，后续补充真实 LLM 调用的集成测试用例。

import "testing"

// TestAddMemory_真实调用 测试完整的 AddMemory 管线
// TODO: 补充 LLM 集成测试
func TestAddMemory_真实调用(t *testing.T) {
	t.Skip("需要真实 LLM 服务")
}

// TestSearch_真实调用 测试完整的 Search 管线
// TODO: 补充 LLM 集成测试
func TestSearch_真实调用(t *testing.T) {
	t.Skip("需要真实 LLM 服务")
}

// TestInvokeLLM_真实调用 测试 LLM 调用
// TODO: 补充 LLM 集成测试
func TestInvokeLLM_真实调用(t *testing.T) {
	t.Skip("需要真实 LLM 服务")
}
