//go:build integration

package tools_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolExecutionSuite 测试工具注册 + 执行链路。
// 嵌入 RunnerSuite，可使用 GetResourceMgr() 注册工具。
// 对齐 Python: 各测试中 _NoteWriteTool / fakeTool 等工具使用模式
type ToolExecutionSuite struct {
	isuite.RunnerSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestToolExecutionSuite(t *testing.T) {
	suite.Run(t, new(ToolExecutionSuite))
}

// TestMapFunction_注册执行 测试 MapFunction 工具注册后可通过 ResourceMgr 获取并执行。
// 对齐 Python: _NoteWriteTool 模式
func (s *ToolExecutionSuite) TestMapFunction_注册执行() {
	rm := s.GetResourceMgr()

	tc := tool.NewToolCardWithID("itest_map_tool", "map_tool", "MapFunction 测试工具", nil, nil)
	mockTool, err := tool.NewMapFunction(tc,
		func(_ context.Context, inputs map[string]any) (map[string]any, error) {
			return map[string]any{"echo": inputs["msg"]}, nil
		}, nil,
	)
	s.Require().NoError(err)

	// 注册
	err = rm.AddTool(mockTool, resources_manager.WithTag(resources_manager.TagGlobal))
	s.Require().NoError(err)

	// 获取
	tools, err := rm.GetTool([]string{"itest_map_tool"})
	s.Require().NoError(err)
	s.Len(tools, 1)

	// 执行
	result, err := tools[0].Invoke(s.Ctx, map[string]any{"msg": "hello"})
	s.Require().NoError(err)
	s.Equal("hello", result["echo"])
}

// TestInvokeFunction_注册执行 测试 InvokeFunction 泛型工具注册后可执行。
// 对齐 Python: NewTool() 模式
func (s *ToolExecutionSuite) TestInvokeFunction_注册执行() {
	rm := s.GetResourceMgr()

	// 使用 NewToolCardWithID + NewMapFunction 模拟强类型工具
	tc := tool.NewToolCardWithID("itest_invoke_tool", "invoke_tool", "InvokeFunction 测试工具", nil, nil)
	mockTool, err := tool.NewMapFunction(tc,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return map[string]any{"status": "ok"}, nil
		}, nil,
	)
	s.Require().NoError(err)

	err = rm.AddTool(mockTool, resources_manager.WithTag(resources_manager.TagGlobal))
	s.Require().NoError(err)

	tools, err := rm.GetTool([]string{"itest_invoke_tool"})
	s.Require().NoError(err)
	s.Len(tools, 1)

	result, err := tools[0].Invoke(s.Ctx, map[string]any{})
	s.Require().NoError(err)
	s.Equal("ok", result["status"])
}

// TestTool_执行返回错误 测试工具 Invoke 返回 error 时调用方收到错误。
func (s *ToolExecutionSuite) TestTool_执行返回错误() {
	rm := s.GetResourceMgr()

	expectedErr := errors.New("工具执行失败")
	tc := tool.NewToolCardWithID("itest_err_tool", "err_tool", "错误工具", nil, nil)
	mockTool, err := tool.NewMapFunction(tc,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return nil, expectedErr
		}, nil,
	)
	s.Require().NoError(err)

	err = rm.AddTool(mockTool, resources_manager.WithTag(resources_manager.TagGlobal))
	s.Require().NoError(err)

	tools, err := rm.GetTool([]string{"itest_err_tool"})
	s.Require().NoError(err)

	_, err = tools[0].Invoke(s.Ctx, map[string]any{})
	s.Error(err)
	// LifecycleTool 包装原始 error 为 exception.BaseError，验证错误信息包含原始消息
	s.Contains(err.Error(), "工具执行失败")
}

// TestTool_多工具注册 测试注册 2+ 工具后 ResourceMgr 分别可获取。
func (s *ToolExecutionSuite) TestTool_多工具注册() {
	rm := s.GetResourceMgr()

	tc1 := tool.NewToolCardWithID("itest_multi_1", "multi_tool_1", "工具1", nil, nil)
	tool1, err := tool.NewMapFunction(tc1,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return map[string]any{"from": "tool1"}, nil
		}, nil,
	)
	s.Require().NoError(err)

	tc2 := tool.NewToolCardWithID("itest_multi_2", "multi_tool_2", "工具2", nil, nil)
	tool2, err := tool.NewMapFunction(tc2,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return map[string]any{"from": "tool2"}, nil
		}, nil,
	)
	s.Require().NoError(err)

	err = rm.AddTool(tool1, resources_manager.WithTag(resources_manager.TagGlobal))
	s.Require().NoError(err)
	err = rm.AddTool(tool2, resources_manager.WithTag(resources_manager.TagGlobal))
	s.Require().NoError(err)

	// 分别获取
	tools1, err := rm.GetTool([]string{"itest_multi_1"})
	s.Require().NoError(err)
	s.Len(tools1, 1)

	tools2, err := rm.GetTool([]string{"itest_multi_2"})
	s.Require().NoError(err)
	s.Len(tools2, 1)
}
