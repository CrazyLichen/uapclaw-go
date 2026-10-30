//go:build integration

package adapter_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hsections "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	memrail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/memory"
	swarmschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CodeAdapterCodingMemorySuite 测试 CodeAdapter 装配链中的 CodingMemoryRail 行为。
//
// 覆盖：
//   - CreateInstance 装配 CodingMemoryRail 到 rails 列表
//   - embedding 配置降级（无 embed 段时 Rail 仍创建）
//   - coding_memory 目录按 project_name 创建（agent/workspace/coding_memory/{name}）
//   - 3 个 coding_memory 工具注册到 AbilityManager
//   - ProcessMessageImpl 注入 memory section
//   - coding_memory_write 工具调用全流程（落盘 + 索引更新）
//   - code_agent subagent 的 CodingMemoryRail 注入
//
// 对应 Python 代码：jiuwenswarm/server/runtime/agent_adapter/interface_code.py
// （create_coding_memory_rail + _build_agent_rails + _build_configured_subagents）
type CodeAdapterCodingMemorySuite struct {
	isuite.BaseIntegrationSuite
	// env 每测试隔离的集成环境（数据目录 / MockLLM provider / env 备份）
	env *adapterTestEnv
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestCodeAdapterCodingMemorySuite(t *testing.T) {
	suite.Run(t, new(CodeAdapterCodingMemorySuite))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// SetupTest 每个测试方法前构建隔离环境。
//
// 隔离三件套与收尾（ResourceMgr.Release / env 恢复 / pathutil.ResetCache）
// 统一由 adapterTestEnv 负责，见 itest_env_helpers_test.go。
func (s *CodeAdapterCodingMemorySuite) SetupTest() {
	s.env = newAdapterTestEnv(s.T(), s.MockLLM, "mock_coding_mem_itest")
}

// writeConfigYaml 写入含顶层 embed 段的 config/config.yaml。
//
// 关键点（对齐 buildModelCacheFromDefaults / NewModelClientConfig 校验）：
//   - model_name 必须在 model_client_config 内层（外层会被跳过）
//   - api_base 与 client_id 为必填校验项
//   - embed 写顶层段（resolveEmbeddingConfig 修复后的行为，对齐 Python
//     create_coding_memory_rail(config=get_config()) → config.get("embed")）
//   - react.subagents.code_agent.enabled 控制用例 7 的 subagent 装配
//   - modes.code.tools/rails 置空避免拉起无关工具与护栏
func (s *CodeAdapterCodingMemorySuite) writeConfigYaml(enableCodeAgent bool) {
	yamlContent := fmt.Sprintf(`models:
  defaults:
    - model_client_config:
        model_name: mock-model
        client_provider: %s
        api_key: mock-api-key
        api_base: http://127.0.0.1:1
        client_id: %s
      is_default: true

embed:
  embed_api_key: test-embed-key
  embed_base_url: http://127.0.0.1:1
  embed_model: text-embedding-test

modes:
  code:
    tools: []
    rails: []

react:
  agent_name: main_agent
  subagents:
    code_agent:
      enabled: %t
`, s.env.Provider, s.env.Provider, enableCodeAgent)

	s.Require().NoError(os.WriteFile(
		filepath.Join(s.env.DataDir, "config", "config.yaml"),
		[]byte(yamlContent), 0o644))
}

// writeConfigYamlNoEmbed 写入无 embed 段的 config（用例 2 降级场景）。
// 对齐 Python: test_code_adapter_builds_coding_memory_rail_without_embedding_config
// 的 config={"embed": {}}（空 embed 段 → 降级 fallback provider）。
func (s *CodeAdapterCodingMemorySuite) writeConfigYamlNoEmbed() {
	yamlContent := fmt.Sprintf(`models:
  defaults:
    - model_client_config:
        model_name: mock-model
        client_provider: %s
        api_key: mock-api-key
        api_base: http://127.0.0.1:1
        client_id: %s
      is_default: true

modes:
  code:
    tools: []
    rails: []
`, s.env.Provider, s.env.Provider)

	s.Require().NoError(os.WriteFile(
		filepath.Join(s.env.DataDir, "config", "config.yaml"),
		[]byte(yamlContent), 0o644))
}

// newCodeAdapter 构造 CodeAdapter 并执行 CreateInstance 装配链，
// 挂 Cleanup 收尾。project_dir 指向 dataDir 下 myproj（目录按项目名用例需真实创建）。
func (s *CodeAdapterCodingMemorySuite) newCodeAdapter() *adapter.CodeAdapter {
	c := adapter.NewCodeAdapter()
	s.Require().NoError(c.CreateInstance(s.Ctx, map[string]any{
		"project_dir": filepath.Join(s.env.DataDir, "myproj"),
	}, "agent.plan", ""))
	s.T().Cleanup(func() { _ = c.Cleanup() })
	return c
}

// TestCreateInstance_Rails装配 验证 CreateInstance 把 CodingMemoryRail 装配进 DeepAgent。
// 对齐 Python: JiuwenClawCodeAdapter._build_agent_rails() 固定 rail 之一
// （interface_code.py create_coding_memory_rail → rails_list）
func (s *CodeAdapterCodingMemorySuite) TestCreateInstance_Rails装配() {
	s.writeConfigYaml(false)

	c := s.newCodeAdapter()
	inst := c.Instance()
	s.Require().NotNil(inst)

	rails := inst.FindRailsByType(reflect.TypeOf(&memrail.CodingMemoryRail{}))
	s.NotEmpty(rails, "CodingMemoryRail 应装配到 rails 列表")
}

// TestBuildCodingMemoryRail_Embedding降级 验证无 embed 段时 Rail 仍创建（降级空 EmbeddingConfig）。
// 对齐 Python: test_code_adapter_builds_coding_memory_rail_without_embedding_config
// （embed 配置不完整 → embed_api_key 置空 → fallback provider，Rail 仍创建）
func (s *CodeAdapterCodingMemorySuite) TestBuildCodingMemoryRail_Embedding降级() {
	s.writeConfigYamlNoEmbed()

	c := s.newCodeAdapter()
	inst := c.Instance()
	s.Require().NotNil(inst)

	// buildCodingMemoryRail 永远返回非 nil（构造器无条件返回，降级到 fallback provider）
	rails := inst.FindRailsByType(reflect.TypeOf(&memrail.CodingMemoryRail{}))
	s.NotEmpty(rails, "无 embed 段时 CodingMemoryRail 仍应创建（降级）")
}

// TestCreateInstance_目录按项目名 验证 coding_memory 目录按 project_name 创建。
// Python: coding_memory_dir = agent_workspace_dir/coding_memory/{project_basename}
// （interface_code.py:139-141），agent_workspace_dir = {DATA_DIR}/agent/workspace
func (s *CodeAdapterCodingMemorySuite) TestCreateInstance_目录按项目名() {
	s.writeConfigYaml(false)
	s.Require().NoError(os.MkdirAll(filepath.Join(s.env.DataDir, "myproj"), 0o755))

	// CreateInstance 装配链内部执行 create_coding_memory_rail 的 makedirs
	_ = s.newCodeAdapter()

	// agentWorkspaceDir 修复后路径：{DATA_DIR}/agent/workspace/coding_memory/{project_name}
	want := filepath.Join(s.env.DataDir, "agent", "workspace", "coding_memory", "myproj")
	info, err := os.Stat(want)
	s.Require().NoError(err, "目录应创建: %s", want)
	s.True(info.IsDir())
}

// TestCreateInstance_工具注册 验证 3 个 coding_memory 工具注册到 AbilityManager。
// 对齐 Python: CodingMemoryRail.init() → _register_coding_memory_tools
// （coding_memory_read/write/edit 三工具挂 agent.ability_manager）
func (s *CodeAdapterCodingMemorySuite) TestCreateInstance_工具注册() {
	s.writeConfigYaml(false)

	c := s.newCodeAdapter()

	am := c.Instance().AbilityManager()
	s.Require().NotNil(am)
	s.NotNil(am.Get("coding_memory_read"), "应注册 coding_memory_read")
	s.NotNil(am.Get("coding_memory_write"), "应注册 coding_memory_write")
	s.NotNil(am.Get("coding_memory_edit"), "应注册 coding_memory_edit")
}

// TestProcessMessage_记忆Section注入 验证 ProcessMessageImpl 触发 BeforeModelCall 注入 memory section。
// 对齐 Python: CodingMemoryRail.before_model_call() → system_prompt_builder.add_section
// （先 RemoveSection 再 AddSection，含行为指令 + 索引/召回内容）。
// 入口先例：real_llm/system_prompt_capture_test.go:372-391（NewAgentRequest +
// ProcessMessageImpl 完整链路，无需 runner.Start）。
func (s *CodeAdapterCodingMemorySuite) TestProcessMessage_记忆Section注入() {
	s.writeConfigYaml(false)
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("section 注入验证"))

	c := s.newCodeAdapter()

	// ProcessMessageImpl 完整装配链入口（先例 real_llm/system_prompt_capture_test.go）
	params, _ := json.Marshal(map[string]any{
		"query": "查询记忆",
		"mode":  "agent.plan",
		"files": map[string]any{},
	})
	req := swarmschema.NewAgentRequest(
		"itest-req-1", "itest-channel", swarmschema.ReqMethodChatSend, params,
		swarmschema.WithAgentSessionID("itest-session-cm"),
	)
	inputs := map[string]any{
		"conversation_id": "itest-session-cm",
		"query":           "查询记忆",
		"channel":         "itest-channel",
		"language":        "zh",
	}
	resp, err := c.ProcessMessageImpl(s.Ctx, req, inputs)
	s.Require().NoError(err)
	s.Require().NotNil(resp)
	s.True(resp.OK, "响应应成功: %+v", resp)

	spb := c.Instance().SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.True(spb.HasSection(hsections.SectionMemory), "应注入 SectionMemory")

	section := spb.GetSection(hsections.SectionMemory)
	s.Require().NotNil(section)
	s.NotEmpty(section.Content, "SectionMemory 内容不应为空")
}

// TestProcessMessage_写记忆全流程 验证 MockLLM 驱动 coding_memory_write 工具调用→真实落盘→索引更新。
// 对齐 Python: CodingMemoryRail 注册工具 → agent 循环调用 → coding_memory_write_with_context
// （write 文件 + upsertMemoryIndex 更新 MEMORY.md）。
// 工具参数：path/content（coding_memory_tool.go Invoke 提取）；
// content 须含合法 frontmatter（name/description/type 必填，type ∈
// {user,feedback,project,reference}，frontmatter.go ValidateFrontmatter）。
func (s *CodeAdapterCodingMemorySuite) TestProcessMessage_写记忆全流程() {
	s.writeConfigYaml(false)

	// 工具内容：合法 frontmatter + body
	memContent := "---\nname: user_pref\ndescription: user preference for tests\ntype: user\n---\nalways run focused tests first"
	argsJSON := fmt.Sprintf(`{"path":"user_pref.md","content":%q}`, memContent)

	s.MockLLM.SetResponses(
		// 第 1 轮：触发 coding_memory_write 工具调用
		mockllm.CreateToolCallResponse("coding_memory_write", argsJSON),
		// 第 2 轮：文本收尾
		mockllm.CreateTextResponse("记忆已保存。"),
	)

	c := s.newCodeAdapter()

	params, _ := json.Marshal(map[string]any{
		"query": "记住：测试优先跑聚焦用例",
		"mode":  "agent.plan",
		"files": map[string]any{},
	})
	req := swarmschema.NewAgentRequest(
		"itest-req-2", "itest-channel", swarmschema.ReqMethodChatSend, params,
		swarmschema.WithAgentSessionID("itest-session-cm-write"),
	)
	inputs := map[string]any{
		"conversation_id": "itest-session-cm-write",
		"query":           "记住：测试优先跑聚焦用例",
		"channel":         "itest-channel",
		"language":        "zh",
	}
	resp, err := c.ProcessMessageImpl(s.Ctx, req, inputs)
	s.Require().NoError(err)
	s.Require().NotNil(resp)
	s.True(resp.OK, "响应应成功: %+v", resp)

	// 断言 1：MockLLM 被调用 2 次（工具调用轮 + 文本轮）
	s.Equal(2, s.MockLLM.InvokeCallCount(), "应为两轮模型调用")

	// 断言 2：记忆文件落盘（对齐 Python 双路径语义——文件走 workspace node：
	// validate_coding_memory_path → get_node_path("coding_memory") =
	// {workspaceDir}/coding_memory/{project}，workspaceDir=projectDir）
	memFile := filepath.Join(s.env.DataDir, "myproj", "coding_memory", "myproj", "user_pref.md")
	content, err := os.ReadFile(memFile)
	s.Require().NoError(err, "记忆文件应落盘: %s", memFile)
	s.Contains(string(content), "always run focused tests first", "文件内容应包含工具写入的 body")
	s.Contains(string(content), "name: user_pref", "文件应保留 frontmatter")

	// 断言 3：MEMORY.md 索引更新（索引走 ctx.coding_memory_dir = Rail 构造时
	// 恢复的绝对路径 = {agent_workspace_dir}/coding_memory/{project}，
	// 对齐 Python rail 注释 "Restore the absolute path ... so tools write
	// to the correct directory"）
	indexPath := filepath.Join(s.env.DataDir, "agent", "workspace", "coding_memory", "myproj", "MEMORY.md")
	indexContent, err := os.ReadFile(indexPath)
	s.Require().NoError(err, "MEMORY.md 索引应更新: %s", indexPath)
	s.Contains(string(indexContent), "user_pref.md", "索引应含新条目文件名")
}

// TestCodeAgentSubagent_CodingMemory注入 验证 code_agent subagent 装配包含 CodingMemoryRail。
// 对齐 Python: _build_configured_subagents 中
// if coding_memory_rail is not None:
//
//	code_agent_rails = [SysOperationRail(), coding_memory_rail]
//
// 链路：CreateSubagent → findSubagentSpec("code_agent") → buildSubagentCreateKwargs
// （Rails: subCfg.Rails）→ CreateCodeAgent → mergeRailsWithRequired → CreateDeepAgent。
func (s *CodeAdapterCodingMemorySuite) TestCodeAgentSubagent_CodingMemory注入() {
	s.writeConfigYaml(true) // react.subagents.code_agent.enabled: true

	c := s.newCodeAdapter()

	// 运行时创建 code_agent subagent（内部 findSubagentSpec → CreateCodeAgent，
	// kwargs.Rails 来自装配的 codeCfg.Rails = [FilesystemRail, CodingMemoryRail]）
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("subagent 就绪"))
	sub, err := c.Instance().CreateSubagent(s.Ctx, "code_agent", "itest-sub-session")
	s.Require().NoError(err)
	s.Require().NotNil(sub)

	// subagent 的 rails 应含 CodingMemoryRail（FindRailsByType 是 DeepAgentInterface 方法）
	rails := sub.FindRailsByType(reflect.TypeOf(&memrail.CodingMemoryRail{}))
	s.NotEmpty(rails, "code_agent subagent 应注入 CodingMemoryRail")

	// Rail Init 懒触发（对齐 Python _ensure_initialized 语义）：
	// 接口层无 EnsureInitialized，断言到 *DeepAgent 具体类型调用
	subDeep, ok := sub.(*harness.DeepAgent)
	s.Require().True(ok, "CreateCodeAgent 应返回 *DeepAgent")
	_, initErr := subDeep.EnsureInitialized(s.Ctx)
	s.Require().NoError(initErr)

	// subagent 的工具也应注册（Rail Init 触发 registerCodingMemoryTools）
	am := sub.AbilityManager()
	s.Require().NotNil(am)
	s.NotNil(am.Get("coding_memory_write"), "subagent 应注册 coding_memory_write")
}
