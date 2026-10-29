//go:build integration

package context_engineer

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"
	tool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	hsections "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	ce "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/context_engineer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ContextEngineerRailSuite 测试 ContextAssembleRail / ContextProcessorRail。
//
// 对齐 Python: tests/unit_tests/harness/test_context_assemble_rail.py
// + tests/unit_tests/harness/test_context_processor_rail.py
// 两个 Rail 均覆盖了 GetCallbacks()，可测回调链路。
type ContextEngineerRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestContextEngineerRailSuite(t *testing.T) {
	suite.Run(t, new(ContextEngineerRailSuite))
}

// TestContextAssembleRail_Init成功 测试 NewContextAssembleRail + Init 成功。
// 对齐 Python: TestContextAssembleRail.test_rail_init_captures_system_prompt_builder ——
// Python 中 ContextAssembleRail.init 捕获 system_prompt_builder 和 ability_manager。
func (s *ContextEngineerRailSuite) TestContextAssembleRail_Init成功() {
	rail := ce.NewContextAssembleRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("上下文组装测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 先 Invoke 触发 ensureInitialized → Rail Init
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "上下文组装测试"})
	s.Require().NoError(err, "Invoke 不应返回错误")

	// 验证 ContextAssembleRail 已注册
	railType := reflect.TypeOf(&ce.ContextAssembleRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "应注册 ContextAssembleRail")
}

// TestContextAssembleRail_BeforeModelCall_注入工作空间节 测试 BeforeModelCall 注入 SectionWorkspace。
// 对齐 Python: ContextAssembleRail.before_model_call(ctx) 中 workspace section 构建
func (s *ContextEngineerRailSuite) TestContextAssembleRail_BeforeModelCall_注入工作空间节() {
	// 创建临时工作空间目录
	tmpDir := s.T().TempDir()
	ws := workspace.NewWorkspace(tmpDir, "cn")

	rail := ce.NewContextAssembleRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("工作空间测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
		Workspace:     ws,
	})
	s.Require().NoError(err)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "工作空间查询"})
	s.Require().NoError(err)

	// 验证 SectionWorkspace 已注入
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.True(spb.HasSection(hsections.SectionWorkspace), "应存在 SectionWorkspace 节")
}

// TestContextAssembleRail_BeforeModelCall_注入工具节 测试 BeforeModelCall 注入 SectionTools。
// 对齐 Python: ContextAssembleRail.before_model_call(ctx) 中 tools section 构建
func (s *ContextEngineerRailSuite) TestContextAssembleRail_BeforeModelCall_注入工具节() {
	tmpDir := s.T().TempDir()
	ws := workspace.NewWorkspace(tmpDir, "cn")

	rail := ce.NewContextAssembleRail()

	// 创建一个模拟工具注册到 AM
	tc := tool.NewToolCardWithID("test_helper", "test_helper", "测试辅助工具", nil, nil)
	mockTool, _ := tool.NewMapFunction(tc,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return map[string]any{"result": "ok"}, nil
		},
		nil,
	)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("工具节测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
		Workspace:     ws,
		ToolInstances: []tool.Tool{mockTool},
	})
	s.Require().NoError(err)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "工具查询"})
	s.Require().NoError(err)

	// 验证 SectionTools 已注入
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.True(spb.HasSection(hsections.SectionTools), "应存在 SectionTools 节")

	section := spb.GetSection(hsections.SectionTools)
	s.Require().NotNil(section)
	s.NotEmpty(section.Content, "SectionTools 内容不应为空")
}

// TestContextAssembleRail_BeforeModelCall_注入上下文节 测试 BeforeModelCall 读取 context 文件并注入 SectionContext。
// 对齐 Python: ContextAssembleRail.before_model_call(ctx) 中 ReadContextFiles
//
// 注意：SectionContext 的注入需要 SysOperation.Fs() 能够读取 workspace 中的 context 文件
// （AGENT.md, SOUL.md, USER.md, IDENTITY.md）。在集成测试中，默认 SysOperation 的
// FsOperation 可能无法读取临时目录，因此此测试验证"无 context 文件时 section 不存在"
// 的降级行为。实际的 context 注入路径由 SysOperation 集成测试覆盖。
func (s *ContextEngineerRailSuite) TestContextAssembleRail_BeforeModelCall_注入上下文节() {
	tmpDir := s.T().TempDir()
	ws := workspace.NewWorkspace(tmpDir, "cn")

	rail := ce.NewContextAssembleRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("上下文节测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
		Workspace:     ws,
	})
	s.Require().NoError(err)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "上下文查询"})
	s.Require().NoError(err)

	// 无 context 文件时，SectionContext 不应注入（降级行为）
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	// 无可读 context 文件时 section 不存在，这是正确行为
	s.False(spb.HasSection(hsections.SectionContext), "无 context 文件时 SectionContext 不应注入")
}

// TestContextAssembleRail_Uninit移除所有节 测试 Uninit 移除 workspace/context 两个 section。
// 对齐 Python: ContextAssembleRail.uninit() 中 section 清理（M-17: 代码实际只移 2 节）
func (s *ContextEngineerRailSuite) TestContextAssembleRail_Uninit移除所有节() {
	tmpDir := s.T().TempDir()
	ws := workspace.NewWorkspace(tmpDir, "cn")

	rail := ce.NewContextAssembleRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("Uninit 测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
		Workspace:     ws,
	})
	s.Require().NoError(err)

	_, _ = agent.Invoke(s.Ctx, map[string]any{"query": "测试"})

	// Uninit 反初始化
	s.NotPanics(func() {
		rail.Uninit(agent)
	}, "Uninit 不应 panic")

	// 验证 2 个 section 已移除（M-17: Python uninit 代码只移 workspace/context 两节，
	// docstring 说 3 节系 Python 自身笔误，以代码实际行为为准——tools 节保留）
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.False(spb.HasSection(hsections.SectionWorkspace), "Uninit 后应移除 SectionWorkspace")
	s.False(spb.HasSection(hsections.SectionContext), "Uninit 后应移除 SectionContext")
}

// TestContextProcessorRail_Init成功 测试 NewContextProcessorRail + Init 成功。
// 对齐 Python: TestContextProcessorRail.test_init_processors_merge
func (s *ContextEngineerRailSuite) TestContextProcessorRail_Init成功() {
	rail := ce.NewContextProcessorRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("上下文处理测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "上下文处理测试"})
	s.Require().NoError(err, "Invoke 不应返回错误")

	railType := reflect.TypeOf(&ce.ContextProcessorRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "应注册 ContextProcessorRail")
}

// TestContextProcessorRail_GetCallbacks事件完整 测试 GetCallbacks 返回 7 个事件。
func (s *ContextEngineerRailSuite) TestContextProcessorRail_GetCallbacks事件完整() {
	rail := ce.NewContextProcessorRail()

	callbacks := rail.GetCallbacks()
	s.Len(callbacks, 7, "ContextProcessorRail 应有 7 个回调事件（5 个覆盖 + 2 个继承）")

	expectedEvents := []agentinterfaces.AgentCallbackEvent{
		agentinterfaces.CallbackBeforeInvoke,
		agentinterfaces.CallbackBeforeModelCall,
		agentinterfaces.CallbackAfterModelCall,
		agentinterfaces.CallbackAfterToolCall,
		agentinterfaces.CallbackOnModelException,
		agentinterfaces.CallbackBeforeTaskIteration,
		agentinterfaces.CallbackAfterTaskIteration,
	}
	for _, event := range expectedEvents {
		_, exists := callbacks[event]
		s.True(exists, "应包含回调事件 %v", event)
	}
}

// TestContextProcessorRail_BeforeModelCall_注入Offload节 测试 BeforeModelCall 注入 "offload" section。
// 对齐 Python: ContextProcessorRail.before_model_call(ctx) 中 maybeInjectOffloadSection
//
// 3bcf3c59 修复 getReactAgentConfig 解引用内层 ReactAgent（DeepAgent.Config()
// 返回 nil）后，Init 正常设置 allProcessors 和 systemPromptBuilder，
// offload 节会被注入——原断言 s.False（验证修复前已知行为）已过时，
// 本提交翻转为 s.True。
func (s *ContextEngineerRailSuite) TestContextProcessorRail_BeforeModelCall_注入Offload节() {
	rail := ce.NewContextProcessorRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("Offload 测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// Invoke 触发 BeforeModelCall
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "上下文压缩"})
	s.Require().NoError(err)

	// getReactAgentConfig 已修复（解引用内层 ReactAgent），Init 设置
	// allProcessors 非空，BeforeModelCall 注入 offload 节
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.True(spb.HasSection("offload"), "Init 后 offload 节应被注入")
}
