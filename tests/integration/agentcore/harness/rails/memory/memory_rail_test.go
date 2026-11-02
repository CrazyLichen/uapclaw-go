//go:build integration

package memory

import (
	"os"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	hsections "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	memory "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/memory"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryRailSuite 测试 MemoryRail 在 DeepAgent 上下文中的集成行为。
//
// 覆盖：
//   - Init 注册 5 个 memory 工具
//   - BeforeInvoke 初始化
//   - BeforeModelCall 注入 SectionMemory（默认/主动/只读模式）
//   - Uninit 清理工具和 Section
//
// 对应 Python 代码：openjiuwen/harness/rails/memory/memory_rail.py
type MemoryRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestMemoryRailSuite(t *testing.T) {
	suite.Run(t, new(MemoryRailSuite))
}

// TestMemoryRail_Init注册5个工具 测试 MemoryRail.Init 注册 5 个 memory 工具到 ability_manager。
// 对齐 Python: MemoryRail.init(agent) 中 _register_memory_tools
func (s *MemoryRailSuite) TestMemoryRail_Init注册5个工具() {
	rail := memory.NewMemoryRail(nil, false)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("Init 注册工具测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	// 执行 Invoke 触发 Init + BeforeInvoke + BeforeModelCall
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "测试 memory"})
	s.Require().NoError(err, "Invoke 不应返回错误")

	// 验证 ability_manager 包含 5 个 memory 工具
	am := agent.AbilityManager()
	s.Require().NotNil(am, "AbilityManager 不应为 nil")
	s.NotNil(am.Get("memory_search"), "应注册 memory_search")
	s.NotNil(am.Get("memory_get"), "应注册 memory_get")
	s.NotNil(am.Get("write_memory"), "应注册 write_memory")
	s.NotNil(am.Get("edit_memory"), "应注册 edit_memory")
	s.NotNil(am.Get("read_memory"), "应注册 read_memory")
}

// TestMemoryRail_BeforeModelCall_默认模式注入 测试 BeforeModelCall 以默认（inactive）模式注入 SectionMemory。
// 对齐 Python: MemoryRail.before_model_call(ctx) 中 BuildMemorySection
func (s *MemoryRailSuite) TestMemoryRail_BeforeModelCall_默认模式注入() {
	rail := memory.NewMemoryRail(nil, false)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("注入提示词测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// Invoke 触发 BeforeModelCall，注入 SectionMemory
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "记忆提示词"})
	s.Require().NoError(err, "BeforeModelCall 注入记忆节应成功完成")

	// 验证 SectionMemory 已注入
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb, "SystemPromptBuilder 不应为 nil")
	s.True(spb.HasSection(hsections.SectionMemory), "应存在 SectionMemory 节")

	// 验证节内容非空
	section := spb.GetSection(hsections.SectionMemory)
	s.Require().NotNil(section, "SectionMemory 节不应为 nil")
	s.NotEmpty(section.Content, "SectionMemory 节内容不应为空")
}

// TestMemoryRail_BeforeModelCall_主动模式内容 测试 BeforeModelCall 以 proactive 模式注入包含主动记录指令的 SectionMemory。
// 对齐 Python: MemoryRail(is_proactive=True) 中 BuildMemorySection(mode="proactive")
func (s *MemoryRailSuite) TestMemoryRail_BeforeModelCall_主动模式内容() {
	rail := memory.NewMemoryRail(nil, true)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("主动模式测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "主动记录"})
	s.Require().NoError(err)

	// 验证 SectionMemory 已注入
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.True(spb.HasSection(hsections.SectionMemory), "主动模式应注入 SectionMemory")

	section := spb.GetSection(hsections.SectionMemory)
	s.Require().NotNil(section)
	// 主动模式内容应包含"核心操作规范"或"proactive"关键字
	cnContent := section.Content["cn"]
	s.NotEmpty(cnContent, "中文内容不应为空")
}

// TestMemoryRail_Uninit清理工具和节 测试 Uninit 注销 5 个工具并移除 SectionMemory。
// 对齐 Python: MemoryRail.uninit(agent) 中工具移除 + 状态清理 + section 移除
func (s *MemoryRailSuite) TestMemoryRail_Uninit清理工具和节() {
	rail := memory.NewMemoryRail(nil, false)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("Uninit 测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 执行一次 Invoke
	_, _ = agent.Invoke(s.Ctx, map[string]any{"query": "测试"})

	// Uninit 应成功
	s.NotPanics(func() {
		rail.Uninit(agent)
	}, "Uninit 不应 panic")

	// 验证 5 个工具已从 AM 注销
	am := agent.AbilityManager()
	s.Require().NotNil(am)
	s.Nil(am.Get("memory_search"), "Uninit 后应注销 memory_search")
	s.Nil(am.Get("memory_get"), "Uninit 后应注销 memory_get")
	s.Nil(am.Get("write_memory"), "Uninit 后应注销 write_memory")
	s.Nil(am.Get("edit_memory"), "Uninit 后应注销 edit_memory")
	s.Nil(am.Get("read_memory"), "Uninit 后应注销 read_memory")

	// 验证 SectionMemory 已移除
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.False(spb.HasSection(hsections.SectionMemory), "Uninit 后应移除 SectionMemory 节")
}

// TestMemoryRail_运行时RegisterRail后read_memory可真实读取 回归两条注册路径的差异：
// RegisterRail（立即注册）跳过了 pending 循环，因此 DeepAgentRail 的
// workspace / sys_operation 注入必须由它自己完成（Python: register_rail
// deep_agent.py L1191-1193）。缺失时 MemoryRail 的 toolCtx.workspace 为 nil，
// read_memory 恒返回「工作空间未初始化」。
//
// 与同文件其它用例的差异：它们都通过 CreateDeepAgentParams.Rails（pending 路）
// 注册 rail，覆盖不到这条路径 —— 这正是该缺陷长期未被集成测试发现的原因。
func (s *MemoryRailSuite) TestMemoryRail_运行时RegisterRail后read_memory可真实读取() {
	root := s.T().TempDir()
	ws := workspace.NewWorkspace(root, "cn")
	s.Require().NoError(os.MkdirAll(root, 0o755))

	sysOp, err := sys_operation.NewSysOperation(sys_operation.NewSysOperationCard(
		sys_operation.WithSysOpMode(sys_operation.OperationModeLocal),
		sys_operation.WithSysOpWorkConfig(&sys_operation.LocalWorkConfig{RestrictToSandbox: true}),
	))
	s.Require().NoError(err)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("初始化完成"))
	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Workspace:     ws,
		SysOperation:  sysOp,
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// ensureInitialized 返回的 ctx 携带 CwdState(workspace)，即 fs 沙箱根；
	// 记忆工具读文件依赖它，直接用 s.Ctx 会被沙箱拒绝。
	initCtx, err := agent.EnsureInitialized(s.Ctx)
	s.Require().NoError(err)

	// 关键：运行时注册（RegisterRail），而不是走 CreateDeepAgentParams.Rails
	rail := memory.NewMemoryRail(nil, false)
	s.Require().NoError(agent.RegisterRail(initCtx, rail))

	writeTool := s.toolByName(agent, "write_memory")
	readTool := s.toolByName(agent, "read_memory")

	wres, err := writeTool.Invoke(initCtx, map[string]any{
		"path":    "MEMORY.md",
		"content": "- 集成标记: memory-rail-register-rail\n",
	})
	s.Require().NoError(err)
	s.Equal(true, wres["success"], "write_memory 应成功，实际: %+v", wres)

	rres, err := readTool.Invoke(initCtx, map[string]any{"path": "MEMORY.md"})
	s.Require().NoError(err)
	s.Equal(true, rres["success"],
		"read_memory 应成功（注入缺失时为「工作空间未初始化」），实际: %+v", rres)
	content, _ := rres["content"].(string)
	s.Contains(content, "memory-rail-register-rail", "应读到写入的内容")

	// 工具结果的文本渲染必须与 Python str(dict) 一致（{'k': 'v'}），
	// 而不是 Go 的 map[...]；这是前端卡片与日志里直接可见的形态。
	rendered := utils.PyStr(rres)
	s.NotContains(rendered, "map[", "工具结果渲染不应出现 Go 的 map[...] 记法")
	s.Contains(rendered, "'content':", "应渲染为 Python dict 形式")

	// memory_search：检索不可用/无命中时 results 也必须是数组（Python 恒为 list）
	searchTool := s.toolByName(agent, "memory_search")
	sres, err := searchTool.Invoke(initCtx, map[string]any{"query": "集成标记"})
	s.Require().NoError(err)
	rawResults, ok := sres["results"]
	s.Require().True(ok, "memory_search 结果必须含 results 键，实际: %+v", sres)
	_, isSlice := rawResults.([]any)
	s.True(isSlice, "results 必须是数组而非 null，实际 %T", rawResults)
	s.NotContains(utils.PyStr(sres), "map[", "检索结果渲染同样不得出现 map[...]")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// toolByName 从 AbilityManager 取卡片、再从 ResourceMgr 取工具实例。
// 记忆工具由 Rail 注册在全局 ResourceMgr 上，与 AbilityManager 的卡片同 ID。
func (s *MemoryRailSuite) toolByName(agent *harness.DeepAgent, name string) tool.Tool {
	s.T().Helper()
	am := agent.AbilityManager()
	s.Require().NotNil(am, "AbilityManager 不应为 nil")

	raw := am.Get(name)
	card, ok := raw.(*tool.ToolCard)
	s.Require().True(ok, "工具 %s 应已注册到 AbilityManager，实际 %T", name, raw)

	tools, err := runner.GetResourceMgr().GetTool([]string{card.ID})
	s.Require().NoError(err)
	s.Require().NotEmpty(tools, "ResourceMgr 应能取到工具实例: %s", card.ID)
	return tools[0]
}
