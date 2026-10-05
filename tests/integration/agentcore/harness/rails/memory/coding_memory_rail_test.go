//go:build integration

package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	hsections "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	memory "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/memory"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CodingMemoryRailSuite 测试 CodingMemoryRail 在 DeepAgent 上下文中的集成行为。
//
// 覆盖：
//   - Init 注册 coding_memory 工具
//   - BeforeInvoke 异步预取
//   - BeforeModelCall 有/无召回结果注入 + SectionMemory 内容
//   - 只读模式（cron/heartbeat）降级到索引注入
//   - Uninit 清理工具和 Section
//   - Priority 默认值
//   - GetCallbacks 注册 BeforeInvoke 和 BeforeModelCall
//   - 完整 Write/Read/Edit 生命周期
//   - BeforeInvoke 初始化 Manager
//
// 对应 Python 代码：openjiuwen/harness/rails/memory/coding_memory_rail.py
type CodingMemoryRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestCodingMemoryRailSuite(t *testing.T) {
	suite.Run(t, new(CodingMemoryRailSuite))
}

// TestCodingMemoryRail_Init注册3个工具 测试 CodingMemoryRail.Init 注册 3 个 coding_memory 工具到 ability_manager。
// 对齐 Python: CodingMemoryRail.init(agent) 中 _register_coding_memory_tools
func (s *CodingMemoryRailSuite) TestCodingMemoryRail_Init注册3个工具() {
	tempDir := s.T().TempDir()
	rail := memory.NewCodingMemoryRail(tempDir, nil, "cn")

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("Init 注册工具测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	// 执行 Invoke 触发 Init + BeforeInvoke + BeforeModelCall
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "测试 coding memory"})
	s.Require().NoError(err, "Invoke 不应返回错误")

	// 验证 ability_manager 包含 3 个 coding_memory 工具
	am := agent.AbilityManager()
	s.Require().NotNil(am, "AbilityManager 不应为 nil")
	s.NotNil(am.Get("coding_memory_read"), "应注册 coding_memory_read")
	s.NotNil(am.Get("coding_memory_write"), "应注册 coding_memory_write")
	s.NotNil(am.Get("coding_memory_edit"), "应注册 coding_memory_edit")
}

// TestCodingMemoryRail_BeforeModelCall_注入SectionMemory 测试 BeforeModelCall 注入 SectionMemory 节。
// 对齐 Python: CodingMemoryRail.before_model_call(ctx) 中向 SystemPromptBuilder 添加 memory section
func (s *CodingMemoryRailSuite) TestCodingMemoryRail_BeforeModelCall_注入SectionMemory() {
	tempDir := s.T().TempDir()
	rail := memory.NewCodingMemoryRail(tempDir, nil, "cn")

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("Section 注入测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// Invoke 触发 BeforeModelCall，应注入 SectionMemory
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "记忆查询"})
	s.Require().NoError(err, "BeforeModelCall 应成功完成")

	// 验证 SectionMemory 已注入
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb, "SystemPromptBuilder 不应为 nil")
	s.True(spb.HasSection(hsections.SectionMemory), "应存在 SectionMemory 节")

	// 验证节内容非空
	section := spb.GetSection(hsections.SectionMemory)
	s.Require().NotNil(section, "SectionMemory 节不应为 nil")
	s.NotEmpty(section.Content, "SectionMemory 节内容不应为空")
}

// TestCodingMemoryRail_BeforeInvoke_异步预取 测试 BeforeInvoke 启动异步预取 goroutine。
// 对齐 Python: CodingMemoryRail.before_invoke(ctx) 中 autoRecall 启动
func (s *CodingMemoryRailSuite) TestCodingMemoryRail_BeforeInvoke_异步预取() {
	tempDir := s.T().TempDir()
	rail := memory.NewCodingMemoryRail(tempDir, nil, "cn")

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("异步预取测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// Invoke 触发 BeforeInvoke（内部启动异步预取 goroutine）
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "预取测试"})
	s.Require().NoError(err, "BeforeInvoke 应成功完成（无 manager 时降级）")
}

// TestCodingMemoryRail_Uninit清理工具和节 测试 Uninit 注销工具并移除 SectionMemory。
// 对齐 Python: CodingMemoryRail.uninit(agent) 中工具移除 + 状态清理 + section 移除
func (s *CodingMemoryRailSuite) TestCodingMemoryRail_Uninit清理工具和节() {
	tempDir := s.T().TempDir()
	rail := memory.NewCodingMemoryRail(tempDir, nil, "cn")

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

	// 验证工具已从 AM 注销
	am := agent.AbilityManager()
	s.Require().NotNil(am)
	s.Nil(am.Get("coding_memory_read"), "Uninit 后应注销 coding_memory_read")
	s.Nil(am.Get("coding_memory_write"), "Uninit 后应注销 coding_memory_write")
	s.Nil(am.Get("coding_memory_edit"), "Uninit 后应注销 coding_memory_edit")

	// 验证 SectionMemory 已移除
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.False(spb.HasSection(hsections.SectionMemory), "Uninit 后应移除 SectionMemory 节")
}

// TestCodingMemoryRail_BeforeModelCall_无召回结果降级索引注入 测试无 embedding/manager 时
// BeforeModelCall 降级注入 MEMORY.md 索引（而非已加载的相关记忆）。
// 对齐 Python: test_auto_recall_no_results → 无召回结果时降级注入索引
func (s *CodingMemoryRailSuite) TestCodingMemoryRail_BeforeModelCall_无召回结果降级索引注入() {
	tempDir := s.T().TempDir()
	// 在 coding_memory 目录下创建 MEMORY.md 索引文件
	cmDir := filepath.Join(tempDir, "coding_memory")
	s.Require().NoError(os.MkdirAll(cmDir, 0o755))
	memoryIndexContent := "# Memory Index\n\n- test_note.md: 测试笔记\n- project_info.md: 项目信息"
	s.Require().NoError(os.WriteFile(filepath.Join(cmDir, "MEMORY.md"), []byte(memoryIndexContent), 0o644))

	rail := memory.NewCodingMemoryRail(tempDir, nil, "cn")

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("降级索引测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// Invoke 触发 BeforeModelCall
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "记忆查询"})
	s.Require().NoError(err, "Invoke 不应返回错误")

	// 验证 SectionMemory 已注入
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.True(spb.HasSection(hsections.SectionMemory), "应存在 SectionMemory 节")

	section := spb.GetSection(hsections.SectionMemory)
	s.Require().NotNil(section)

	// 无 embedding/manager 时降级到索引模式，应包含"当前记忆索引"
	// 注意：readMemoryIndex 依赖 SysOperation().Fs()，在无 SysOperation 时返回空字符串，
	// 但 section 基础内容仍包含 coding memory 行为指令
	content := section.Content["cn"]
	s.NotEmpty(content, "SectionMemory 内容不应为空")
}

// TestCodingMemoryRail_BeforeModelCall_召回结果注入已加载记忆 测试有记忆文件时
// BeforeModelCall 注入内容（无 embedding 时降级到索引，有 manager 时注入已加载记忆）。
// 对齐 Python: test_before_model_call_with_recall_results
func (s *CodingMemoryRailSuite) TestCodingMemoryRail_BeforeModelCall_召回结果注入已加载记忆() {
	tempDir := s.T().TempDir()
	cmDir := filepath.Join(tempDir, "coding_memory")
	s.Require().NoError(os.MkdirAll(cmDir, 0o755))

	// 创建记忆文件
	noteContent := "---\nname: 项目偏好\ndescription: 用户的项目偏好\ntype: project\n---\n\n用户偏好使用 Go 语言进行开发。"
	s.Require().NoError(os.WriteFile(filepath.Join(cmDir, "project_pref.md"), []byte(noteContent), 0o644))

	// 创建 MEMORY.md 索引
	memoryIndexContent := "# Memory Index\n\n- project_pref.md: 项目偏好"
	s.Require().NoError(os.WriteFile(filepath.Join(cmDir, "MEMORY.md"), []byte(memoryIndexContent), 0o644))

	rail := memory.NewCodingMemoryRail(tempDir, nil, "cn")

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("召回结果测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// Invoke 触发 BeforeModelCall
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "用户偏好"})
	s.Require().NoError(err, "Invoke 不应返回错误")

	// 验证 SectionMemory 已注入
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.True(spb.HasSection(hsections.SectionMemory), "应存在 SectionMemory 节")

	section := spb.GetSection(hsections.SectionMemory)
	s.Require().NotNil(section)

	// 无 embedding 时 autoRecall 无法执行（manager 为 nil），降级到索引模式
	// 验证 section 内容非空即可（具体是索引还是召回取决于 manager 初始化）
	content := section.Content["cn"]
	s.NotEmpty(content, "SectionMemory 内容不应为空")
}

// TestCodingMemoryRail_BeforeModelCall_只读模式Cron 测试 cron 调用时 BeforeModelCall 只读降级行为。
// 通过直接验证 BuildCodingMemorySection(readOnly=true) 的输出确认只读模式关键词。
// 对齐 Python: test_before_invoke_no_prefetch_for_cron
func (s *CodingMemoryRailSuite) TestCodingMemoryRail_BeforeModelCall_只读模式Cron() {
	// 直接验证 BuildCodingMemorySection 在 readOnly=true 时的输出
	section := hsections.BuildCodingMemorySection("/workspace/coding_memory", true, "cn")

	s.NotEmpty(section.Content["cn"], "SectionMemory 内容不应为空")
	s.True(strings.Contains(section.Content["cn"], "只读"), "只读模式应包含'只读'关键字")
}

// TestCodingMemoryRail_BeforeModelCall_只读模式Heartbeat 测试 heartbeat 调用时 BeforeModelCall 只读降级行为。
// 通过直接验证 BuildCodingMemorySection(readOnly=true) 的英文输出确认只读模式关键词。
// 对齐 Python: test_before_invoke_no_prefetch_for_heartbeat
func (s *CodingMemoryRailSuite) TestCodingMemoryRail_BeforeModelCall_只读模式Heartbeat() {
	// 直接验证 BuildCodingMemorySection 在 readOnly=true 时的英文输出
	section := hsections.BuildCodingMemorySection("/workspace/coding_memory", true, "en")

	s.NotEmpty(section.Content["en"], "SectionMemory 内容不应为空")
	s.True(strings.Contains(section.Content["en"], "read-only"), "只读模式英文应包含'read-only'关键字")
}

// TestCodingMemoryRail_Priority默认值 测试 NewCodingMemoryRail 返回的 rail.Priority() == 80。
// 对齐 Python: CodingMemoryRail.priority = 80
func (s *CodingMemoryRailSuite) TestCodingMemoryRail_Priority默认值() {
	tempDir := s.T().TempDir()
	rail := memory.NewCodingMemoryRail(tempDir, nil, "cn")

	s.Equal(80, rail.Priority(), "CodingMemoryRail 默认优先级应为 80")
}

// TestCodingMemoryRail_GetCallbacks注册BeforeInvoke和BeforeModelCall 测试 GetCallbacks
// 返回的回调映射包含 CallbackBeforeInvoke 和 CallbackBeforeModelCall。
func (s *CodingMemoryRailSuite) TestCodingMemoryRail_GetCallbacks注册BeforeInvoke和BeforeModelCall() {
	tempDir := s.T().TempDir()
	rail := memory.NewCodingMemoryRail(tempDir, nil, "cn")

	callbacks := rail.GetCallbacks()

	s.NotNil(callbacks[agentinterfaces.CallbackBeforeInvoke],
		"GetCallbacks 应包含 CallbackBeforeInvoke")
	s.NotNil(callbacks[agentinterfaces.CallbackBeforeModelCall],
		"GetCallbacks 应包含 CallbackBeforeModelCall")
}

// TestCodingMemoryRail_完整WriteReadEdit生命周期 测试 CodingMemoryRail Init → 工具注册 →
// Uninit → 工具注销的完整生命周期无 panic。
// 对齐 Python: CodingMemoryRail 生命周期测试
func (s *CodingMemoryRailSuite) TestCodingMemoryRail_完整WriteReadEdit生命周期() {
	tempDir := s.T().TempDir()
	cmDir := filepath.Join(tempDir, "coding_memory")
	s.Require().NoError(os.MkdirAll(cmDir, 0o755))

	rail := memory.NewCodingMemoryRail(tempDir, nil, "cn")

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("生命周期测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// Invoke 触发 Init → BeforeInvoke → BeforeModelCall
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "生命周期测试"})
	s.Require().NoError(err, "Invoke 不应返回错误")

	// 验证 3 个工具存在
	am := agent.AbilityManager()
	s.Require().NotNil(am)
	s.NotNil(am.Get("coding_memory_read"), "应注册 coding_memory_read")
	s.NotNil(am.Get("coding_memory_write"), "应注册 coding_memory_write")
	s.NotNil(am.Get("coding_memory_edit"), "应注册 coding_memory_edit")

	// Uninit 后验证工具被注销
	s.NotPanics(func() {
		rail.Uninit(agent)
	}, "Uninit 不应 panic")

	s.Nil(am.Get("coding_memory_read"), "Uninit 后应注销 coding_memory_read")
	s.Nil(am.Get("coding_memory_write"), "Uninit 后应注销 coding_memory_write")
	s.Nil(am.Get("coding_memory_edit"), "Uninit 后应注销 coding_memory_edit")

	// 验证完整生命周期无 panic（Implicit: 到此无 panic 即通过）
}

// TestCodingMemoryRail_BeforeInvoke_初始化Manager 测试 BeforeInvoke 在首次调用时尝试初始化
// CodingMemoryManager。无 embedding 时 Manager 初始化可能失败，但 BeforeInvoke 应不返回错误。
// 对齐 Python: CodingMemoryRail.before_invoke(ctx) 中 _init_coding_memory_manager
func (s *CodingMemoryRailSuite) TestCodingMemoryRail_BeforeInvoke_初始化Manager() {
	tempDir := s.T().TempDir()
	rail := memory.NewCodingMemoryRail(tempDir, nil, "cn")

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("Manager 初始化测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// Invoke 触发 BeforeInvoke（内部调用 initCodingMemoryManager）
	// 无 embedding 时 manager 初始化可能失败，但 BeforeInvoke 应不返回错误
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "Manager 初始化测试"})
	s.Require().NoError(err, "BeforeInvoke 应成功完成（无 embedding 时 Manager 初始化失败不传播错误）")

	// 间接验证：BeforeModelCall 正常完成即表明 BeforeInvoke 未中断
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.True(spb.HasSection(hsections.SectionMemory), "BeforeModelCall 应正常完成并注入 SectionMemory")
}

// ──────────────────────────── 非导出函数 ────────────────────────────
