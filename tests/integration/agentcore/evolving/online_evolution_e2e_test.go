//go:build integration

package evolving_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/experience"
	"github.com/uapclaw/uapclaw-go/internal/evolving/optimizer/llm_resilience"
	skillcallopt "github.com/uapclaw/uapclaw-go/internal/evolving/optimizer/skill_call"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// OnlineEvolutionE2ESuite 在线进化管道端到端集成测试套件
//
// 对齐 Python: test_online_evolution_e2e.TestOnlineEvolutionE2E
// 测试链路：SignalDetector → SkillExperienceOptimizer → EvolutionStore
type OnlineEvolutionE2ESuite struct {
	isuite.BaseIntegrationSuite
	// tmpDir 临时技能根目录
	tmpDir string
}

// mockBaseModelClient 用于测试的模拟 LLM 客户端
type mockBaseModelClient struct {
	invokeFn func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error)
}

func (m *mockBaseModelClient) Invoke(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	if m.invokeFn != nil {
		return m.invokeFn(ctx, messages, opts...)
	}
	return nil, fmt.Errorf("mock invoke not configured")
}
func (m *mockBaseModelClient) Stream(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockBaseModelClient) GenerateImage(ctx context.Context, messages []*llmschema.UserMessage, opts ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockBaseModelClient) GenerateSpeech(ctx context.Context, messages []*llmschema.UserMessage, opts ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockBaseModelClient) GenerateVideo(ctx context.Context, messages []*llmschema.UserMessage, opts ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockBaseModelClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockBaseModelClient) Release(ctx context.Context, opts ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}
func (m *mockBaseModelClient) SupportsKVCacheRelease() bool { return false }

var _ model_clients.BaseModelClient = (*mockBaseModelClient)(nil)

// ──────────────────────────── 常量 ────────────────────────────

// generateRecordsLLMPolicy 生成记录的 LLM 调用策略
var generateRecordsLLMPolicy = llm_resilience.LLMInvokePolicy{
	AttemptTimeoutSecs: 10,
	TotalBudgetSecs:    60,
	MaxAttempts:        3,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestOnlineEvolutionE2E 运行在线进化管道 E2E 测试套件
func TestOnlineEvolutionE2E(t *testing.T) {
	suite.Run(t, new(OnlineEvolutionE2ESuite))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// SetupTest 每个测试前创建临时目录
func (s *OnlineEvolutionE2ESuite) SetupTest() {
	s.tmpDir = s.T().TempDir()
}

// newMockSkillModel 创建使用 mock client 的 Model 实例
func (s *OnlineEvolutionE2ESuite) newMockSkillModel(
	invokeFn func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error),
) *llm.Model {
	s.T().Helper()
	mockClient := &mockBaseModelClient{invokeFn: invokeFn}
	providerName := fmt.Sprintf("mock_evo_e2e_%d", time.Now().UnixNano())
	model_clients.GetClientRegistry().Register(providerName, "llm", func(modelConfig *llmschema.ModelRequestConfig, clientConfig *llmschema.ModelClientConfig) (model_clients.BaseModelClient, error) {
		return mockClient, nil
	})

	clientConfig := &llmschema.ModelClientConfig{
		ClientProvider: providerName,
		ClientID:       providerName + "_id",
	}
	modelConfig := &llmschema.ModelRequestConfig{
		ModelName: "mock-model",
	}

	model, err := llm.NewModel(clientConfig, modelConfig)
	s.Require().NoError(err, "创建 mock model 失败")
	return model
}

// prepareSkill 在临时目录下准备技能 SKILL.md
func (s *OnlineEvolutionE2ESuite) prepareSkill(name, content string) string {
	skillDir := filepath.Join(s.tmpDir, "skills", name)
	err := os.MkdirAll(skillDir, 0755)
	s.Require().NoError(err)
	err = os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0644)
	s.Require().NoError(err)
	return skillDir
}

// buildConversationWithScript 构建包含代码执行的对话消息列表
//
// 对齐 Python: _build_conversation_with_script()
func (s *OnlineEvolutionE2ESuite) buildConversationWithScript() []map[string]any {
	return []map[string]any{
		{
			"role": "assistant",
			"tool_calls": []map[string]any{
				{
					"id":        "tc_read",
					"name":      "read_file",
					"arguments": `{"file_path": "/skills/data-processor/SKILL.md"}`,
				},
			},
		},
		{
			"role":         "tool",
			"tool_call_id": "tc_read",
			"name":         "read_file",
			"content":      "# Data Processor Skill\n\nProcess CSV and JSON files.",
		},
		{
			"role": "assistant",
			"tool_calls": []map[string]any{
				{
					"id":        "tc_code",
					"name":      "code",
					"arguments": `{"code": "import pandas as pd\nimport matplotlib.pyplot as plt\n\ndf = pd.read_csv('data.csv')\nfig, ax = plt.subplots()\nax.bar(df['category'], df['value'])\nax.set_title('Category Distribution')\nplt.savefig('chart.png')\nprint('Chart saved to chart.png')"}`,
				},
			},
		},
		{
			"role":         "tool",
			"tool_call_id": "tc_code",
			"name":         "code",
			"content":      "Chart saved to chart.png",
		},
		{
			"role": "assistant",
			"tool_calls": []map[string]any{
				{
					"id":        "tc_bash",
					"name":      "bash",
					"arguments": `{"command": "cat /etc/hosts | head"}`,
				},
			},
		},
		{
			"role":         "tool",
			"tool_call_id": "tc_bash",
			"name":         "bash",
			"content":      "Error: permission denied reading /etc/hosts",
		},
		{
			"role":    "user",
			"content": "不对，应该用 sudo 读取系统文件",
		},
	}
}

// buildMockLLMResponseWithScript 构建包含脚本产物的模拟 LLM 响应
//
// 对齐 Python: _build_mock_llm_response_with_script()
func (s *OnlineEvolutionE2ESuite) buildMockLLMResponseWithScript() string {
	records := []map[string]any{
		{
			"action":       "append",
			"target":       "body",
			"section":      "Troubleshooting",
			"content":      "### Permission Denied on System Files\n- Use sudo when reading system files like /etc/hosts",
			"merge_target": nil,
		},
		{
			"action":          "append",
			"target":          "script",
			"section":         "Scripts",
			"content":         "import pandas as pd\nimport matplotlib.pyplot as plt\n\ndf = pd.read_csv('data.csv')\nfig, ax = plt.subplots()\nax.bar(df['category'], df['value'])\nax.set_title('Category Distribution')\nplt.savefig('chart.png')",
			"merge_target":    nil,
			"script_filename": "generate_bar_chart.py",
			"script_language": "python",
			"script_purpose":  "Generate bar chart from CSV data",
		},
	}
	data, _ := json.Marshal(records)
	return string(data)
}

// ──────────────────── 测试方法 ────────────────────

// TestOnlineEvolution_全管道信号到持久化 完整链路：
// 对话 → SignalDetector 信号检测 → SkillExperienceOptimizer (mock LLM) 记录生成 → EvolutionStore 持久化
//
// 对齐 Python: test_full_pipeline_signal_to_persist
func (s *OnlineEvolutionE2ESuite) TestOnlineEvolution_全管道信号到持久化() {
	ctx := s.Ctx
	skillName := "data-processor"
	skillContent := "# Data Processor Skill\n\nProcess CSV and JSON files.\n"
	s.prepareSkill(skillName, skillContent)

	// ── 阶段 1：信号检测 ──
	messages := s.buildConversationWithScript()
	detector := signal.NewConversationSignalDetector(
		signal.WithExistingSkills(map[string]bool{skillName: true}),
	)
	sigList := detector.Detect(messages)

	s.Require().GreaterOrEqual(len(sigList), 2, "应检测到 >=2 个信号")

	signalTypes := map[string]bool{}
	for _, sig := range sigList {
		signalTypes[sig.SignalType] = true
	}
	s.True(signalTypes["script_artifact"], "应包含 script_artifact 信号")
	// Python 断言 execution_failure 或 user_correction 至少一个
	s.True(signalTypes["execution_failure"] || signalTypes["user_correction"],
		"应包含 execution_failure 或 user_correction 信号")

	// 验证脚本信号的 skill_name
	scriptSignals := []*signal.EvolutionSignal{}
	for _, sig := range sigList {
		if sig.SignalType == "script_artifact" {
			scriptSignals = append(scriptSignals, sig)
		}
	}
	s.Require().NotEmpty(scriptSignals, "应有 script_artifact 信号")
	s.NotNil(scriptSignals[0].SkillName, "脚本信号应有 skill_name")
	s.Equal(skillName, *scriptSignals[0].SkillName, "脚本信号 skill_name 应为 data-processor")

	// ── 阶段 2：LLM 驱动的经验生成 (mock) ──
	model := s.newMockSkillModel(func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage(s.buildMockLLMResponseWithScript()), nil
	})
	optimizer := skillcallopt.NewSkillExperienceOptimizer(model, "mock-model", "en", generateRecordsLLMPolicy)

	store := checkpointing.NewEvolutionStore([]string{filepath.Join(s.tmpDir, "skills")})

	// 将 []*signal.EvolutionSignal 转为 []signal.EvolutionSignal（值类型）
	sigValues := make([]signal.EvolutionSignal, len(sigList))
	for i, sig := range sigList {
		sigValues[i] = *sig
	}

	evoCtx := &experience.EvolutionContext{
		SkillName:           skillName,
		Signals:             sigValues,
		SkillContent:        skillContent,
		Messages:            messages,
		ExistingDescRecords: []checkpointing.EvolutionRecord{},
		ExistingBodyRecords: []checkpointing.EvolutionRecord{},
	}

	records, err := optimizer.GenerateRecords(ctx, evoCtx)
	s.Require().NoError(err, "GenerateRecords 不应返回错误")
	s.Require().Equal(2, len(records), "应生成 2 条记录")

	// 验证文本记录和脚本记录
	textRecords := []checkpointing.EvolutionRecord{}
	scriptRecords := []checkpointing.EvolutionRecord{}
	for _, r := range records {
		if r.Change.Target == signal.EvolutionTargetScript {
			scriptRecords = append(scriptRecords, r)
		} else {
			textRecords = append(textRecords, r)
		}
	}
	s.Equal(1, len(textRecords), "应有 1 条文本记录")
	s.Equal(1, len(scriptRecords), "应有 1 条脚本记录")

	if len(scriptRecords) > 0 {
		s.NotNil(scriptRecords[0].Change.ScriptLanguage, "脚本记录应有 script_language")
		s.Equal("python", *scriptRecords[0].Change.ScriptLanguage, "脚本语言应为 python")
		s.NotNil(scriptRecords[0].Change.ScriptFilename, "脚本记录应有 script_filename")
		s.Equal("generate_bar_chart.py", *scriptRecords[0].Change.ScriptFilename, "脚本文件名应为 generate_bar_chart.py")
	}

	// ── 阶段 3：持久化 ──
	for _, r := range records {
		err := store.AppendRecord(ctx, skillName, r)
		s.Require().NoError(err, "AppendRecord 不应返回错误")
	}

	// 验证 evolutions.json
	evoLog, err := store.LoadFullEvolutionLog(ctx, skillName)
	s.Require().NoError(err, "LoadFullEvolutionLog 不应返回错误")
	s.Equal(2, len(evoLog.Entries), "evolutions.json 应有 2 条记录")

	// 验证脚本文件持久化
	scriptsDir := filepath.Join(s.tmpDir, "skills", skillName, "evolution", "scripts")
	s.DirExists(scriptsDir, "脚本目录应存在")

	pyFiles, err := filepath.Glob(filepath.Join(scriptsDir, "*.py"))
	s.Require().NoError(err)
	s.Equal(1, len(pyFiles), "应有 1 个 .py 脚本文件")

	scriptBytes, err := os.ReadFile(pyFiles[0])
	s.Require().NoError(err)
	scriptContent := string(scriptBytes)
	s.True(strings.Contains(scriptContent, "matplotlib"), "脚本内容应包含 matplotlib")
	s.True(strings.Contains(scriptContent, "pandas"), "脚本内容应包含 pandas")

	// 验证 SKILL.md 包含 evolution-index 块
	skillMDPath := filepath.Join(s.tmpDir, "skills", skillName, "SKILL.md")
	skillMDContent, err := os.ReadFile(skillMDPath)
	s.Require().NoError(err)
	skillMD := string(skillMDContent)
	s.True(strings.Contains(skillMD, "<!-- evolution-index-start -->"), "SKILL.md 应包含 evolution-index-start")
	s.True(strings.Contains(skillMD, "<!-- evolution-index-end -->"), "SKILL.md 应包含 evolution-index-end")
	s.True(strings.Contains(skillMD, "Evolution Experiences"), "SKILL.md 应包含 Evolution Experiences 标题")
	s.True(strings.Contains(skillMD, "**2**"), "SKILL.md 应显示记录数 2")
}

// TestOnlineEvolution_DataFetch误报抑制 验证 data-fetch 工具输出中的
// 失败关键词不应产生 execution_failure 信号
//
// 对齐 Python: test_data_fetch_false_positive_suppressed
func (s *OnlineEvolutionE2ESuite) TestOnlineEvolution_DataFetch误报抑制() {
	messages := []map[string]any{
		{
			"role": "assistant",
			"tool_calls": []map[string]any{
				{"id": "tc_s", "name": "web_search", "arguments": "{}"},
			},
		},
		{
			"role":         "tool",
			"tool_call_id": "tc_s",
			"name":         "web_search",
			"content":      "Search results:\n1. How to handle Python timeout errors\n2. Common ValueError exceptions and fixes\n3. ConnectionError troubleshooting guide",
		},
		{
			"role": "assistant",
			"tool_calls": []map[string]any{
				{"id": "tc_r", "name": "read_file", "arguments": "{}"},
			},
		},
		{
			"role":         "tool",
			"tool_call_id": "tc_r",
			"name":         "read_file",
			"content":      "File content: raise ValueError('failed validation')",
		},
	}

	detector := signal.NewConversationSignalDetector()
	sigList := detector.Detect(messages)

	// 过滤 execution_failure 信号
	failureSignals := []*signal.EvolutionSignal{}
	for _, sig := range sigList {
		if sig.SignalType == "execution_failure" {
			failureSignals = append(failureSignals, sig)
		}
	}
	s.Empty(failureSignals, "data-fetch 工具不应产生 execution_failure 信号")
}

// TestOnlineEvolution_畸形LLM输出重试 验证第一轮 LLM 返回非法 JSON 时
// 优化器自动重试并恢复
//
// 对齐 Python: test_retry_on_malformed_llm_output
func (s *OnlineEvolutionE2ESuite) TestOnlineEvolution_畸形LLM输出重试() {
	ctx := s.Ctx
	skillName := "retry-skill"
	s.prepareSkill(skillName, "# Retry Skill\n")

	callCount := 0
	model := s.newMockSkillModel(func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		callCount++
		if callCount == 1 {
			// 第一轮返回非法 JSON
			return llmschema.NewAssistantMessage("This is not JSON at all { broken"), nil
		}
		// 第二轮返回有效 JSON
		validResp, _ := json.Marshal([]map[string]any{
			{
				"action":  "append",
				"target":  "body",
				"section": "Troubleshooting",
				"content": "### Recovered Fix\n- Retry succeeded",
			},
		})
		return llmschema.NewAssistantMessage(string(validResp)), nil
	})

	optimizer := skillcallopt.NewSkillExperienceOptimizer(model, "mock", "cn", generateRecordsLLMPolicy)
	detector := signal.NewConversationSignalDetector()
	sigList := detector.Detect([]map[string]any{
		{"role": "tool", "name": "bash", "content": "Error: command timeout"},
	})

	sigValues := make([]signal.EvolutionSignal, len(sigList))
	for i, sig := range sigList {
		sigValues[i] = *sig
	}

	evoCtx := &experience.EvolutionContext{
		SkillName:           skillName,
		Signals:             sigValues,
		SkillContent:        "# Retry Skill\n",
		Messages:            []map[string]any{{"role": "user", "content": "test"}},
		ExistingDescRecords: []checkpointing.EvolutionRecord{},
		ExistingBodyRecords: []checkpointing.EvolutionRecord{},
	}

	records, err := optimizer.GenerateRecords(ctx, evoCtx)
	s.Require().NoError(err, "重试后 GenerateRecords 不应返回错误")
	s.Equal(1, len(records), "重试后应生成 1 条记录")
	s.Contains(records[0].Change.Content, "Recovered Fix", "记录内容应包含 Recovered Fix")

	// 验证 LLM 被调用了至少 2 次（初次 + 重试）
	s.GreaterOrEqual(callCount, 2, "LLM 应至少被调用 2 次（初次 + 重试）")
}

// TestOnlineEvolution_MergeTarget替换记录 验证新记录的 merge_target
// 替换旧记录而非追加重复
//
// 对齐 Python: test_merge_target_replaces_existing_record
func (s *OnlineEvolutionE2ESuite) TestOnlineEvolution_MergeTarget替换记录() {
	ctx := s.Ctx
	skillName := "merge-skill"
	s.prepareSkill(skillName, "# Merge Skill\n")

	store := checkpointing.NewEvolutionStore([]string{filepath.Join(s.tmpDir, "skills")})

	// 第一轮：生成初始记录
	model := s.newMockSkillModel(func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		resp, _ := json.Marshal([]map[string]any{
			{
				"action":  "append",
				"target":  "body",
				"section": "Troubleshooting",
				"content": "### Initial finding\n- v1 content",
			},
		})
		return llmschema.NewAssistantMessage(string(resp)), nil
	})

	optimizer := skillcallopt.NewSkillExperienceOptimizer(model, "mock", "en", generateRecordsLLMPolicy)
	detector := signal.NewConversationSignalDetector()
	sigList := detector.Detect([]map[string]any{
		{"role": "tool", "name": "bash", "content": "Error: timeout"},
	})

	sigValues := make([]signal.EvolutionSignal, len(sigList))
	for i, sig := range sigList {
		sigValues[i] = *sig
	}

	evoCtx := &experience.EvolutionContext{
		SkillName:           skillName,
		Signals:             sigValues,
		SkillContent:        "# Merge Skill\n",
		Messages:            []map[string]any{{"role": "user", "content": "hi"}},
		ExistingDescRecords: []checkpointing.EvolutionRecord{},
		ExistingBodyRecords: []checkpointing.EvolutionRecord{},
	}

	recordsV1, err := optimizer.GenerateRecords(ctx, evoCtx)
	s.Require().NoError(err)
	s.Require().NotEmpty(recordsV1, "第一轮应生成记录")

	for _, r := range recordsV1 {
		err := store.AppendRecord(ctx, skillName, r)
		s.Require().NoError(err)
	}
	oldID := recordsV1[0].ID

	// 第二轮：生成合并记录，带 merge_target
	mergeResp, _ := json.Marshal([]map[string]any{
		{
			"action":       "append",
			"target":       "body",
			"section":      "Troubleshooting",
			"content":      "### Updated finding\n- v2 content with more detail",
			"merge_target": oldID,
		},
	})
	model2 := s.newMockSkillModel(func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage(string(mergeResp)), nil
	})
	optimizer2 := skillcallopt.NewSkillExperienceOptimizer(model2, "mock", "en", generateRecordsLLMPolicy)

	existingBody := store.GetPendingRecords(ctx, skillName, nil)
	evoCtx2 := &experience.EvolutionContext{
		SkillName:           skillName,
		Signals:             sigValues,
		SkillContent:        "# Merge Skill\n",
		Messages:            []map[string]any{{"role": "user", "content": "hi again"}},
		ExistingDescRecords: []checkpointing.EvolutionRecord{},
		ExistingBodyRecords: existingBody,
	}

	recordsV2, err := optimizer2.GenerateRecords(ctx, evoCtx2)
	s.Require().NoError(err)

	for _, r := range recordsV2 {
		err := store.AppendRecord(ctx, skillName, r)
		s.Require().NoError(err)
	}

	// 验证最终日志：merge_target 替换旧记录，所以应有 1 条（不是 2 条）
	finalLog, err := store.LoadFullEvolutionLog(ctx, skillName)
	s.Require().NoError(err)
	s.Equal(1, len(finalLog.Entries), "merge_target 替换后应有 1 条记录")
	s.Contains(finalLog.Entries[0].Change.Content, "v2 content", "替换后内容应为 v2")
}

// TestOnlineEvolution_SignalDetector脚本产物检测 验证代码执行后成功
// 产生 script_artifact 信号，失败时产生 execution_failure 信号
func (s *OnlineEvolutionE2ESuite) TestOnlineEvolution_SignalDetector脚本产物检测() {
	skillName := "data-processor"
	detector := signal.NewConversationSignalDetector(
		signal.WithExistingSkills(map[string]bool{skillName: true}),
	)

	// 场景 1：代码执行成功 → 应产生 script_artifact
	successMessages := []map[string]any{
		{
			"role": "assistant",
			"tool_calls": []map[string]any{
				{"id": "tc1", "name": "read_file", "arguments": `{"file_path": "/skills/data-processor/SKILL.md"}`},
			},
		},
		{"role": "tool", "tool_call_id": "tc1", "name": "read_file", "content": "skill content"},
		{
			"role": "assistant",
			"tool_calls": []map[string]any{
				{"id": "tc2", "name": "code", "arguments": `{"code": "print('hello world this is a long script that does something useful')"}`},
			},
		},
		{"role": "tool", "tool_call_id": "tc2", "name": "code", "content": "hello world"},
	}

	sigs := detector.Detect(successMessages)
	hasScriptArtifact := false
	for _, sig := range sigs {
		if sig.SignalType == "script_artifact" {
			hasScriptArtifact = true
		}
	}
	s.True(hasScriptArtifact, "代码执行成功应产生 script_artifact 信号")

	// 场景 2：代码执行失败 → 不应产生 script_artifact，应产生 execution_failure
	failMessages := []map[string]any{
		{
			"role": "assistant",
			"tool_calls": []map[string]any{
				{"id": "tc3", "name": "read_file", "arguments": `{"file_path": "/skills/data-processor/SKILL.md"}`},
			},
		},
		{"role": "tool", "tool_call_id": "tc3", "name": "read_file", "content": "skill content"},
		{
			"role": "assistant",
			"tool_calls": []map[string]any{
				{"id": "tc4", "name": "code", "arguments": `{"code": "print('this is another long script for testing failure')"}`},
			},
		},
		{"role": "tool", "tool_call_id": "tc4", "name": "code", "content": "Error: script execution failed with timeout"},
	}

	detector2 := signal.NewConversationSignalDetector(
		signal.WithExistingSkills(map[string]bool{skillName: true}),
	)
	sigs2 := detector2.Detect(failMessages)
	hasScriptArtifact2 := false
	hasExecutionFailure := false
	for _, sig := range sigs2 {
		if sig.SignalType == "script_artifact" {
			hasScriptArtifact2 = true
		}
		if sig.SignalType == "execution_failure" {
			hasExecutionFailure = true
		}
	}
	s.False(hasScriptArtifact2, "代码执行失败不应产生 script_artifact 信号")
	s.True(hasExecutionFailure, "代码执行失败应产生 execution_failure 信号")
}

// TestOnlineEvolution_SignalDetector用户纠正检测 验证用户纠正模式
// 能产生 user_correction 信号（走 fallback 正则路径，不依赖 LLM）
func (s *OnlineEvolutionE2ESuite) TestOnlineEvolution_SignalDetector用户纠正检测() {
	skillName := "data-processor"
	detector := signal.NewConversationSignalDetector(
		signal.WithExistingSkills(map[string]bool{skillName: true}),
	)

	messages := []map[string]any{
		{
			"role": "assistant",
			"tool_calls": []map[string]any{
				{"id": "tc1", "name": "read_file", "arguments": `{"file_path": "/skills/data-processor/SKILL.md"}`},
			},
		},
		{"role": "tool", "tool_call_id": "tc1", "name": "read_file", "content": "skill content"},
		{"role": "user", "content": "不对，应该用 sudo 读取系统文件"},
	}

	// 使用 DetectUserMessageFeedback（走 fallback 正则，因为没绑定 LLM）
	sigs, err := detector.DetectUserIntent(s.Ctx, messages)
	s.Require().NoError(err)
	s.NotEmpty(sigs, "应检测到用户纠正信号")
	s.Equal("user_intent", sigs[0].SignalType, "信号类型应为 user_intent")
}

// TestOnlineEvolution_EvolutionStore记录操作 验证 EvolutionStore 的
// AppendRecord、LoadFullEvolutionLog、GetPendingRecords、MarkRecordsApplied 操作
func (s *OnlineEvolutionE2ESuite) TestOnlineEvolution_EvolutionStore记录操作() {
	ctx := s.Ctx
	skillName := "store-test"
	s.prepareSkill(skillName, "# Store Test Skill\n")

	store := checkpointing.NewEvolutionStore([]string{filepath.Join(s.tmpDir, "skills")})

	// 创建记录
	patch, err := checkpointing.NewEvolutionPatch("Troubleshooting", "append", "### Fix\n- Step 1", signal.EvolutionTargetBody)
	s.Require().NoError(err)
	record := checkpointing.MakeEvolutionRecord("execution_failure", "test context", *patch, 0.7, nil, nil)

	// 追加记录
	err = store.AppendRecord(ctx, skillName, *record)
	s.Require().NoError(err)

	// 验证日志
	evoLog, err := store.LoadFullEvolutionLog(ctx, skillName)
	s.Require().NoError(err)
	s.Equal(1, len(evoLog.Entries), "应有 1 条记录")
	s.Equal(record.ID, evoLog.Entries[0].ID, "记录 ID 应匹配")
	s.False(evoLog.Entries[0].Applied, "新记录应为未应用状态")

	// 获取待定记录
	pending := store.GetPendingRecords(ctx, skillName, nil)
	s.Equal(1, len(pending), "应有 1 条待定记录")

	// 标记已应用
	applied, err := store.MarkRecordsApplied(ctx, skillName, []string{record.ID})
	s.Require().NoError(err)
	s.Equal(1, applied, "应标记 1 条记录为已应用")

	// 验证已应用后待定记录为 0
	pending2 := store.GetPendingRecords(ctx, skillName, nil)
	s.Equal(0, len(pending2), "标记后应无待定记录")
}

// TestOnlineEvolution_EvolutionStore脚本持久化 验证 target=script 的记录
// 会将脚本文件持久化到 evolution/scripts/ 目录
func (s *OnlineEvolutionE2ESuite) TestOnlineEvolution_EvolutionStore脚本持久化() {
	ctx := s.Ctx
	skillName := "script-store"
	s.prepareSkill(skillName, "# Script Store Skill\n")

	store := checkpointing.NewEvolutionStore([]string{filepath.Join(s.tmpDir, "skills")})

	// 创建脚本记录
	scriptName := "analyze_data.py"
	scriptLang := "python"
	scriptPurpose := "Analyze CSV data"
	patch, err := checkpointing.NewEvolutionPatch("Scripts", "append", "import pandas as pd\ndf = pd.read_csv('data.csv')", signal.EvolutionTargetScript)
	s.Require().NoError(err)
	patch.ScriptFilename = &scriptName
	patch.ScriptLanguage = &scriptLang
	patch.ScriptPurpose = &scriptPurpose

	record := checkpointing.MakeEvolutionRecord("script_artifact", "test context", *patch, 0.8, nil, nil)

	err = store.AppendRecord(ctx, skillName, *record)
	s.Require().NoError(err)

	// 验证脚本文件
	scriptsDir := filepath.Join(s.tmpDir, "skills", skillName, "evolution", "scripts")
	s.DirExists(scriptsDir, "脚本目录应存在")

	pyFiles, err := filepath.Glob(filepath.Join(scriptsDir, "*.py"))
	s.Require().NoError(err)
	s.Equal(1, len(pyFiles), "应有 1 个 .py 文件")

	// 验证脚本内容
	scriptContent, err := os.ReadFile(pyFiles[0])
	s.Require().NoError(err)
	s.Contains(string(scriptContent), "pandas", "脚本内容应包含 pandas")
	s.Contains(string(scriptContent), "read_csv", "脚本内容应包含 read_csv")

	// 验证脚本索引
	indexMD := filepath.Join(scriptsDir, "_index.md")
	s.FileExists(indexMD, "_index.md 应存在")
	indexContent, err := os.ReadFile(indexMD)
	s.Require().NoError(err)
	s.Contains(string(indexContent), "analyze_data.py", "索引应包含脚本文件名")
	s.Contains(string(indexContent), "python", "索引应包含脚本语言")
}

// TestOnlineEvolution_EvolutionStore并发追加 验证多个并发 AppendRecord
// 不会丢失数据（RWMutex 保护）
func (s *OnlineEvolutionE2ESuite) TestOnlineEvolution_EvolutionStore并发追加() {
	ctx := s.Ctx
	skillName := "concurrent-skill"
	s.prepareSkill(skillName, "# Concurrent Skill\n")

	store := checkpointing.NewEvolutionStore([]string{filepath.Join(s.tmpDir, "skills")})

	// 并发追加 5 条记录
	done := make(chan bool, 5)
	for i := 0; i < 5; i++ {
		go func(idx int) {
			patch, _ := checkpointing.NewEvolutionPatch("Troubleshooting", "append",
				fmt.Sprintf("### Fix %d\n- Step %d", idx, idx), signal.EvolutionTargetBody)
			record := checkpointing.MakeEvolutionRecord("execution_failure", fmt.Sprintf("context %d", idx), *patch, 0.7, nil, nil)
			_ = store.AppendRecord(ctx, skillName, *record)
			done <- true
		}(i)
	}

	// 等待所有 goroutine 完成
	for i := 0; i < 5; i++ {
		<-done
	}

	// 验证所有记录都持久化了
	evoLog, err := store.LoadFullEvolutionLog(ctx, skillName)
	s.Require().NoError(err)
	s.Equal(5, len(evoLog.Entries), "并发追加后应有 5 条记录")
}
