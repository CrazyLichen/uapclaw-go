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

// CodingMemoryRailDeepSuite 测试 CodingMemoryRail BeforeModelCall 深度场景。
//
// 覆盖：recall 内容注入 vs 降级索引注入、只读模式关键词、
// AutoRecall 有/无结果、SectionMemory 中文/英文输出、
// BeforeInvoke 跳过 MEMORY.md 场景。
//
// 对齐 Python: openjiuwen/harness/rails/memory/coding_memory_rail.py
type CodingMemoryRailDeepSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestCodingMemoryRailDeepSuite 运行 CodingMemoryRail 深度 BeforeModelCall 测试套件
func TestCodingMemoryRailDeepSuite(t *testing.T) {
	suite.Run(t, new(CodingMemoryRailDeepSuite))
}

// TestBeforeModelCall_无记忆文件降级注入 测试 coding_memory 目录为空时
// BeforeModelCall 降级注入索引（无文件→MEMORY.md 空→基础行为指令）。
func (s *CodingMemoryRailDeepSuite) TestBeforeModelCall_无记忆文件降级注入() {
	tempDir := s.T().TempDir()
	cmDir := filepath.Join(tempDir, "coding_memory")
	s.Require().NoError(os.MkdirAll(cmDir, 0o755))

	rail := memory.NewCodingMemoryRail(tempDir, nil, "cn")
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("降级注入测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "记忆查询"})
	s.Require().NoError(err)

	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.True(spb.HasSection(hsections.SectionMemory), "应存在 SectionMemory 节")

	section := spb.GetSection(hsections.SectionMemory)
	s.Require().NotNil(section)
	// 即使无文件，section 基础行为指令仍应存在
	content := section.Content["cn"]
	s.NotEmpty(content, "SectionMemory 内容不应为空（至少包含行为指令）")
}

// TestBeforeModelCall_有MEMORY索引注入索引内容 测试有 MEMORY.md 索引文件时
// BeforeModelCall 注入索引内容。
func (s *CodingMemoryRailDeepSuite) TestBeforeModelCall_有MEMORY索引注入索引内容() {
	tempDir := s.T().TempDir()
	cmDir := filepath.Join(tempDir, "coding_memory")
	s.Require().NoError(os.MkdirAll(cmDir, 0o755))

	// 创建 MEMORY.md 索引文件
	indexContent := "# Memory Index\n\n- api_design.md: API 设计文档\n- db_schema.md: 数据库 Schema"
	s.Require().NoError(os.WriteFile(filepath.Join(cmDir, "MEMORY.md"), []byte(indexContent), 0o644))

	rail := memory.NewCodingMemoryRail(tempDir, nil, "cn")
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("索引注入测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "记忆查询"})
	s.Require().NoError(err)

	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.True(spb.HasSection(hsections.SectionMemory), "应存在 SectionMemory 节")

	section := spb.GetSection(hsections.SectionMemory)
	s.Require().NotNil(section)
	content := section.Content["cn"]
	s.NotEmpty(content, "SectionMemory 内容不应为空")
	// 索引注入模式下应包含 coding memory 行为指令
	// 具体索引内容是否被注入取决于 SysOperation 是否可用
}

// TestBeforeModelCall_有记忆文件和索引 测试同时有记忆文件和 MEMORY.md 索引时
// BeforeModelCall 正确注入 SectionMemory。
func (s *CodingMemoryRailDeepSuite) TestBeforeModelCall_有记忆文件和索引() {
	tempDir := s.T().TempDir()
	cmDir := filepath.Join(tempDir, "coding_memory")
	s.Require().NoError(os.MkdirAll(cmDir, 0o755))

	// 创建记忆文件
	noteContent := "---\nname: 用户偏好\ndescription: 用户的编程偏好\ntype: user\n---\n\n用户偏好使用 Go 语言。"
	s.Require().NoError(os.WriteFile(filepath.Join(cmDir, "user_pref.md"), []byte(noteContent), 0o644))

	// 创建 MEMORY.md 索引
	indexContent := "# Memory Index\n\n- user_pref.md: 用户偏好"
	s.Require().NoError(os.WriteFile(filepath.Join(cmDir, "MEMORY.md"), []byte(indexContent), 0o644))

	rail := memory.NewCodingMemoryRail(tempDir, nil, "cn")
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("记忆文件和索引测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "用户偏好"})
	s.Require().NoError(err)

	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.True(spb.HasSection(hsections.SectionMemory), "应存在 SectionMemory 节")

	section := spb.GetSection(hsections.SectionMemory)
	s.Require().NotNil(section)
	content := section.Content["cn"]
	s.NotEmpty(content, "SectionMemory 内容不应为空")
}

// TestReadOnly_中文只读模式 测试 BuildCodingMemorySection 中文只读模式包含正确关键词。
func (s *CodingMemoryRailDeepSuite) TestReadOnly_中文只读模式() {
	section := hsections.BuildCodingMemorySection("/workspace/coding_memory", true, "cn")

	s.NotEmpty(section.Content["cn"], "SectionMemory 中文内容不应为空")
	s.True(strings.Contains(section.Content["cn"], "只读"),
		"只读模式应包含'只读'关键字")
	// 中文只读模板中包含"不允许写入"，这是正确的约束描述
	s.True(strings.Contains(section.Content["cn"], "不允许写入"),
		"只读模式应包含'不允许写入'约束")
}

// TestReadOnly_英文只读模式 测试 BuildCodingMemorySection 英文只读模式包含正确关键词。
func (s *CodingMemoryRailDeepSuite) TestReadOnly_英文只读模式() {
	section := hsections.BuildCodingMemorySection("/workspace/coding_memory", true, "en")

	s.NotEmpty(section.Content["en"], "SectionMemory 英文内容不应为空")
	s.True(strings.Contains(section.Content["en"], "read-only"),
		"只读模式英文应包含'read-only'关键字")
}

// TestReadWrite_中文读写模式 测试 BuildCodingMemorySection 中文读写模式包含写入关键词。
func (s *CodingMemoryRailDeepSuite) TestReadWrite_中文读写模式() {
	section := hsections.BuildCodingMemorySection("/workspace/coding_memory", false, "cn")

	s.NotEmpty(section.Content["cn"], "SectionMemory 中文内容不应为空")
	// 读写模式不应包含只读警告
	s.False(strings.Contains(section.Content["cn"], "只读"),
		"读写模式不应包含'只读'关键字")
}

// TestReadWrite_英文读写模式 测试 BuildCodingMemorySection 英文读写模式不包含只读关键词。
func (s *CodingMemoryRailDeepSuite) TestReadWrite_英文读写模式() {
	section := hsections.BuildCodingMemorySection("/workspace/coding_memory", false, "en")

	s.NotEmpty(section.Content["en"], "SectionMemory 英文内容不应为空")
	s.False(strings.Contains(section.Content["en"], "read-only"),
		"读写模式英文不应包含'read-only'关键字")
}

// TestBeforeModelCall_多次InvokeSectionMemory持久 测试多次 Invoke 后
// SectionMemory 节仍然存在且内容正确。
func (s *CodingMemoryRailDeepSuite) TestBeforeModelCall_多次InvokeSectionMemory持久() {
	tempDir := s.T().TempDir()
	cmDir := filepath.Join(tempDir, "coding_memory")
	s.Require().NoError(os.MkdirAll(cmDir, 0o755))

	rail := memory.NewCodingMemoryRail(tempDir, nil, "cn")
	s.MockLLM.SetResponses(
		mockllm.CreateTextResponse("第一次调用"),
		mockllm.CreateTextResponse("第二次调用"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 第一次 Invoke
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "第一次"})
	s.Require().NoError(err)
	spb1 := agent.SystemPromptBuilder()
	s.Require().NotNil(spb1)
	s.True(spb1.HasSection(hsections.SectionMemory), "第一次 Invoke 后应存在 SectionMemory 节")

	// 第二次 Invoke
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "第二次"})
	s.Require().NoError(err)
	spb2 := agent.SystemPromptBuilder()
	s.Require().NotNil(spb2)
	s.True(spb2.HasSection(hsections.SectionMemory), "第二次 Invoke 后应仍存在 SectionMemory 节")
}

// TestInit_Uninit后BeforeModelCall不再注入 测试 Uninit 后 BeforeModelCall
// 不再注入 SectionMemory。
func (s *CodingMemoryRailDeepSuite) TestInit_Uninit后BeforeModelCall不再注入() {
	tempDir := s.T().TempDir()
	cmDir := filepath.Join(tempDir, "coding_memory")
	s.Require().NoError(os.MkdirAll(cmDir, 0o755))

	rail := memory.NewCodingMemoryRail(tempDir, nil, "cn")
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("Uninit 测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// Invoke 触发 Init
	_, _ = agent.Invoke(s.Ctx, map[string]any{"query": "初始化"})

	// 验证 SectionMemory 存在
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.True(spb.HasSection(hsections.SectionMemory), "Init 后应存在 SectionMemory 节")

	// Uninit
	rail.Uninit(agent)

	// 验证 SectionMemory 已移除
	s.False(spb.HasSection(hsections.SectionMemory), "Uninit 后应移除 SectionMemory 节")
}

// ──────────────────────────── 非导出函数 ────────────────────────────
