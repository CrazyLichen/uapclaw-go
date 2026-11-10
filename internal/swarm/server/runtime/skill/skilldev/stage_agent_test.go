package skilldev

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/agents"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/local"
)

// ──────────────────────────── 结构体 ────────────────────────────

// stageAgentMockClient 阶段 Agent 测试用模型客户端。
// 不嵌入 BaseModelClient（嵌入 nil 接口会在虚方法调用时空指针崩溃），全部方法显式实现。
type stageAgentMockClient struct {
	// reply 固定回复内容
	reply string
	// calls 调用次数
	calls int
}

// Invoke 返回固定回复。
func (m *stageAgentMockClient) Invoke(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	m.calls++
	return llmschema.NewAssistantMessage(m.reply), nil
}

// Stream 未实现。
func (m *stageAgentMockClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, fmt.Errorf("not implemented")
}

// GenerateImage 未实现。
func (m *stageAgentMockClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

// GenerateSpeech 未实现。
func (m *stageAgentMockClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

// GenerateVideo 未实现。
func (m *stageAgentMockClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

// TranscribeAudio 未实现。
func (m *stageAgentMockClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

// Release 无需释放。
func (m *stageAgentMockClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}

// SupportsKVCacheRelease 不支持 KV 缓存释放。
func (m *stageAgentMockClient) SupportsKVCacheRelease() bool { return false }

// fakeStageTool 用于 MCP 工具白名单筛选的桩工具。
type fakeStageTool struct {
	tool.Tool
	// name 工具名
	name string
}

// Card 返回工具卡片。
func (f *fakeStageTool) Card() *tool.ToolCard {
	card := tool.NewToolCard(f.name, "fake stage tool", nil, nil)
	return card
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestCreateStageAgent_构造并注册文件工具 阶段 Agent 应带模型配置、系统提示词与白名单工具。
func TestCreateStageAgent_构造并注册文件工具(t *testing.T) {
	deps := newStageTestDeps(t)
	ctx := NewSkillDevContext("task-1", deps, NewSkillDevState("task-1"), t.TempDir(), make(chan SkillDevEvent, 8))

	agent, err := ctx.CreateStageAgent("generate", "你是技能生成器", []string{"file_read", "file_write"}, 3)
	require.NoError(t, err)
	require.NotNil(t, agent)

	assert.Equal(t, "skilldev_task-1_generate", agent.Card().GetName())
	assert.Equal(t, 3, stageAgentConfig(t, agent).MaxIterations)
	require.Len(t, stageAgentConfig(t, agent).PromptTemplate, 1)
	assert.Equal(t, "你是技能生成器", stageAgentConfig(t, agent).PromptTemplate[0]["content"])
	assert.Equal(t, "mock-model", stageAgentConfig(t, agent).ModelNameVal)
	assert.NotNil(t, stageAgentConfig(t, agent).ModelClientConfig)
	assert.Equal(t, deps.ModelClientConfig["client_provider"], stageAgentConfig(t, agent).ModelClientConfig.ClientProvider)
	assert.Equal(t, "task-1", ctx.TaskID)

	// 别名映射：file_read/file_write → read_file/write_file
	am := agent.AbilityManager()
	require.NotNil(t, am.Get("read_file"), "应注册 read_file")
	require.NotNil(t, am.Get("write_file"), "应注册 write_file")
	assert.Nil(t, am.Get("file_read"), "不应注册别名本身")
}

// TestCreateStageAgent_可真实调用模型 构造出的 Agent 能完成一次 ReAct 调用。
func TestCreateStageAgent_可真实调用模型(t *testing.T) {
	deps := newStageTestDeps(t)
	ctx := NewSkillDevContext("task-2", deps, NewSkillDevState("task-2"), t.TempDir(), make(chan SkillDevEvent, 8))

	agent, err := ctx.CreateStageAgent("plan", "你是规划器", nil, 2)
	require.NoError(t, err)

	output, err := agent.Invoke(context.Background(), map[string]any{"query": "生成一个 CSV 技能"})
	require.NoError(t, err)
	assert.Contains(t, fmt.Sprintf("%v", output), "stage-agent-reply")
}

// TestCreateStageAgent_默认迭代上限 非正数迭代上限回退到 20。
func TestCreateStageAgent_默认迭代上限(t *testing.T) {
	deps := newStageTestDeps(t)
	ctx := NewSkillDevContext("task-3", deps, NewSkillDevState("task-3"), t.TempDir(), make(chan SkillDevEvent, 8))

	agent, err := ctx.CreateStageAgent("validate", "校验", nil, 0)
	require.NoError(t, err)
	assert.Equal(t, defaultStageMaxIterations, stageAgentConfig(t, agent).MaxIterations)
}

// TestCreateStageAgent_模型配置无效 deps 缺少 model_client_config 或 provider 时报错。
func TestCreateStageAgent_模型配置无效(t *testing.T) {
	state := NewSkillDevState("task-4")

	nilCfg := &SkillDevDeps{ModelName: "mock-model"}
	ctxNil := NewSkillDevContext("task-4", nilCfg, state, t.TempDir(), make(chan SkillDevEvent, 4))
	_, err := ctxNil.CreateStageAgent("plan", "p", nil, 2)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "模型配置无效")

	noProvider := &SkillDevDeps{ModelName: "mock-model", ModelClientConfig: map[string]any{"api_key": "k"}}
	ctxNoProvider := NewSkillDevContext("task-4", noProvider, state, t.TempDir(), make(chan SkillDevEvent, 4))
	_, err = ctxNoProvider.CreateStageAgent("plan", "p", nil, 2)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "模型配置无效")
}

// TestRegisterTools_无系统操作时文件工具报错 SysOpConfig 为空时不允许构造文件工具。
func TestRegisterTools_无系统操作时文件工具报错(t *testing.T) {
	deps := newStageTestDeps(t)
	deps.SysOpConfig = nil
	ctx := NewSkillDevContext("task-5", deps, NewSkillDevState("task-5"), t.TempDir(), make(chan SkillDevEvent, 4))

	agent, err := ctx.CreateStageAgent("generate", "生成", nil, 2)
	require.NoError(t, err)

	err = ctx.RegisterTools(agent, []string{"file_read"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "需要 SysOperation 配置")
}

// TestRegisterTools_MCP工具筛选 MCP 工厂为空或工具缺失时报错，命中时注册成功。
func TestRegisterTools_MCP工具筛选(t *testing.T) {
	deps := newStageTestDeps(t)
	ctx := NewSkillDevContext("task-6", deps, NewSkillDevState("task-6"), t.TempDir(), make(chan SkillDevEvent, 4))
	agent, err := ctx.CreateStageAgent("plan", "规划", nil, 2)
	require.NoError(t, err)

	// 工厂为空：可选工具跳过（不阻断阶段）
	require.NoError(t, ctx.RegisterTools(agent, []string{"web_search"}))
	assert.Nil(t, agent.AbilityManager().Get("web_search"))

	// 工厂有工具但不含目标：同样跳过
	deps.MCPToolsFactory = func() []tool.Tool { return []tool.Tool{&fakeStageTool{name: "other_tool"}} }
	require.NoError(t, ctx.RegisterTools(agent, []string{"web_search"}))
	assert.Nil(t, agent.AbilityManager().Get("web_search"))

	// 命中
	deps.MCPToolsFactory = func() []tool.Tool {
		return []tool.Tool{&fakeStageTool{name: "web_search"}, nil}
	}
	require.NoError(t, ctx.RegisterTools(agent, []string{"web_search", "  "}))
	require.NotNil(t, agent.AbilityManager().Get("web_search"))
}

// TestRegisterTools_空参数与nil agent 空白名单直接返回，nil agent 报错。
func TestRegisterTools_空参数与nilAgent(t *testing.T) {
	deps := newStageTestDeps(t)
	ctx := NewSkillDevContext("task-7", deps, NewSkillDevState("task-7"), t.TempDir(), make(chan SkillDevEvent, 4))

	require.NoError(t, ctx.RegisterTools(nil, nil))

	agent, err := ctx.CreateStageAgent("plan", "规划", nil, 2)
	require.NoError(t, err)
	require.Error(t, ctx.RegisterTools(nil, []string{"read_file"}))
	require.NotNil(t, agent)
}

// TestResolveStageToolName 别名映射与未知名称透传。
func TestResolveStageToolName(t *testing.T) {
	cases := map[string]string{
		"file_read":  "read_file",
		"file_write": "write_file",
		"file_edit":  "edit_file",
		"shell":      "bash",
		"bash":       "bash",
		"web_search": "web_search",
		"  code  ":   "code",
		"":           "",
		"   ":        "",
		"read_file":  "read_file",
	}
	for raw, want := range cases {
		assert.Equal(t, want, resolveStageToolName(raw), "input=%q", raw)
	}
}

// TestBuildStageModelClientConfig_字段映射 完整字段与默认值映射。
func TestBuildStageModelClientConfig_字段映射(t *testing.T) {
	cfg, err := buildStageModelClientConfig(map[string]any{
		"client_provider": "openai",
		"client_id":       "cid",
		"api_key":         "k",
		"api_base":        "http://x",
		"timeout":         float64(15),
		"max_retries":     float64(1),
		"verify_ssl":      false,
		"ssl_cert":        "/tmp/cert.pem",
		"custom_headers":  map[string]any{"X-A": "1", "X-B": 2},
	})
	require.NoError(t, err)
	assert.Equal(t, "cid", cfg.ClientID)
	assert.Equal(t, "openai", cfg.ClientProvider)
	assert.Equal(t, 15.0, cfg.Timeout)
	assert.Equal(t, 1, cfg.MaxRetries)
	assert.False(t, cfg.VerifySSL)
	assert.Equal(t, "/tmp/cert.pem", cfg.SSLCert)
	assert.Equal(t, map[string]string{"X-A": "1"}, cfg.CustomHeaders)

	// provider 回退 model_provider + client_id 自动生成 + 默认数值
	cfg, err = buildStageModelClientConfig(map[string]any{"model_provider": "deepseek"})
	require.NoError(t, err)
	assert.Equal(t, "deepseek", cfg.ClientProvider)
	assert.True(t, strings.HasSuffix(cfg.ClientID, "-stagedev"))
	assert.Equal(t, 60.0, cfg.Timeout)
	assert.Equal(t, 3, cfg.MaxRetries)
	assert.True(t, cfg.VerifySSL)
}

// TestEnsureStageSysOperation_注册到资源管理器 SysOperation 生成唯一 ID 并注册。
func TestEnsureStageSysOperation_注册到资源管理器(t *testing.T) {
	deps := newStageTestDeps(t)
	ctx := NewSkillDevContext("task-8", deps, NewSkillDevState("task-8"), t.TempDir(), make(chan SkillDevEvent, 4))

	op, id, err := ctx.ensureStageSysOperation("generate")
	require.NoError(t, err)
	require.NotNil(t, op)
	assert.Equal(t, "skilldev_task-8_generate", id)
	assert.Equal(t, id, deps.SysOpConfig.ID)

	// 二次调用复用已设置的 ID
	_, id2, err := ctx.ensureStageSysOperation("generate")
	require.NoError(t, err)
	assert.Equal(t, id, id2)
}

// TestEnsureStageSysOperation_沙箱配置无效 sandbox 模式缺 gateway_config 时报错。
func TestEnsureStageSysOperation_沙箱配置无效(t *testing.T) {
	deps := newStageTestDeps(t)
	deps.SysOpConfig = sysop.NewSysOperationCard(sysop.WithSysOpMode(sysop.OperationModeSandbox))
	ctx := NewSkillDevContext("task-9", deps, NewSkillDevState("task-9"), t.TempDir(), make(chan SkillDevEvent, 4))

	_, _, err := ctx.ensureStageSysOperation("generate")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SysOperation")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// stageAgentConfig 取出阶段 Agent 的强类型配置。
func stageAgentConfig(t *testing.T, agent *agents.ReActAgent) *saconfig.ReActAgentConfig {
	t.Helper()
	cfg, ok := agent.Config().(*saconfig.ReActAgentConfig)
	require.True(t, ok, "配置类型应为 *ReActAgentConfig，实际 %T", agent.Config())
	return cfg
}

// newStageTestDeps 构造带 mock 模型与本地 SysOperation 的依赖。
func newStageTestDeps(t *testing.T) *SkillDevDeps {
	t.Helper()

	client := &stageAgentMockClient{reply: "stage-agent-reply"}
	provider := fmt.Sprintf("stage_agent_test_%d", time.Now().UnixNano())
	model_clients.GetClientRegistry().Register(provider, "llm",
		func(_ *llmschema.ModelRequestConfig, _ *llmschema.ModelClientConfig) (model_clients.BaseModelClient, error) {
			return client, nil
		})
	t.Cleanup(func() { _ = model_clients.GetClientRegistry().Unregister(provider, "llm") })

	return &SkillDevDeps{
		ModelName: "mock-model",
		ModelClientConfig: map[string]any{
			"client_provider": provider,
			"client_id":       provider + "_id",
			"api_key":         "test-key",
			"api_base":        "http://127.0.0.1:1",
		},
		SysOpConfig: sysop.NewSysOperationCard(
			sysop.WithSysOpWorkConfig(&sysop.LocalWorkConfig{RestrictToSandbox: false}),
		),
	}
}

// TestBuildStageTool_内置工具分支 文件/命令类工具按名称构造成功。
func TestBuildStageTool_内置工具分支(t *testing.T) {
	deps := newStageTestDeps(t)
	ctx := NewSkillDevContext("task-10", deps, NewSkillDevState("task-10"), t.TempDir(), make(chan SkillDevEvent, 4))
	_, _, err := ctx.ensureStageSysOperation("generate")
	require.NoError(t, err)

	names := []string{"read_file", "write_file", "edit_file", "bash", "code"}
	want := map[string]string{
		"read_file":  "read_file",
		"write_file": "write_file",
		"edit_file":  "edit_file",
		"bash":       "bash",
		"code":       "code",
	}
	for _, name := range names {
		stageTool, err := ctx.buildStageTool(name, "agent-x")
		require.NoError(t, err, "name=%s", name)
		require.NotNil(t, stageTool, "name=%s", name)
		require.NotNil(t, stageTool.Card(), "name=%s", name)
		assert.Equal(t, want[name], stageTool.Card().GetName(), "name=%s", name)
	}
}

// TestCreateStageAgent_模型创建失败 provider 未注册时构造模型失败并报错。
func TestCreateStageAgent_模型创建失败(t *testing.T) {
	deps := &SkillDevDeps{
		ModelName: "mock-model",
		ModelClientConfig: map[string]any{
			"client_provider": "unregistered_provider_for_test",
			"client_id":       "cid",
		},
	}
	ctx := NewSkillDevContext("task-11", deps, NewSkillDevState("task-11"), t.TempDir(), make(chan SkillDevEvent, 4))

	_, err := ctx.CreateStageAgent("plan", "p", nil, 2)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "模型创建失败")
}

// TestEmit_写入事件队列 事件带 task_id 前缀并进入队列。
func TestEmit_写入事件队列(t *testing.T) {
	deps := newStageTestDeps(t)
	queue := make(chan SkillDevEvent, 2)
	ctx := NewSkillDevContext("task-12", deps, NewSkillDevState("task-12"), t.TempDir(), queue)

	ctx.Emit(SkillDevEventTypeProgress, map[string]any{"message": "hi"})

	select {
	case evt := <-queue:
		assert.Equal(t, SkillDevEventTypeProgress, evt.EventType)
		assert.Equal(t, "task-12", evt.TaskID)
		assert.Equal(t, "task-12", evt.Payload["task_id"])
		assert.Equal(t, "hi", evt.Payload["message"])
	default:
		t.Fatal("事件应写入队列")
	}
}

// TestEmit_队列满时丢弃 队列满时不阻塞。
func TestEmit_队列满时丢弃(t *testing.T) {
	deps := newStageTestDeps(t)
	queue := make(chan SkillDevEvent)
	ctx := NewSkillDevContext("task-13", deps, NewSkillDevState("task-13"), t.TempDir(), queue)

	assert.NotPanics(t, func() {
		ctx.Emit(SkillDevEventTypeProgress, map[string]any{"message": "drop"})
	})
}

// TestStageCfgFloatAndInt_类型分支 覆盖各数值类型与非法输入。
func TestStageCfgFloatAndInt_类型分支(t *testing.T) {
	floatCases := []struct {
		in   any
		want float64
		ok   bool
	}{
		{float64(1.5), 1.5, true},
		{float32(2.5), 2.5, true},
		{3, 3, true},
		{int64(4), 4, true},
		{"5", 0, false},
	}
	for _, c := range floatCases {
		got, ok := stageCfgFloat(c.in)
		assert.Equal(t, c.ok, ok, "input=%v", c.in)
		if c.ok {
			assert.Equal(t, c.want, got, "input=%v", c.in)
		}
	}

	intCases := []struct {
		in   any
		want int
		ok   bool
	}{
		{1, 1, true},
		{int64(2), 2, true},
		{float64(3), 3, true},
		{"4", 0, false},
	}
	for _, c := range intCases {
		got, ok := stageCfgInt(c.in)
		assert.Equal(t, c.ok, ok, "input=%v", c.in)
		if c.ok {
			assert.Equal(t, c.want, got, "input=%v", c.in)
		}
	}
}

// SupportsKVCacheAffinity 测试替身：不支持 KV 亲和。
func (m *stageAgentMockClient) SupportsKVCacheAffinity() bool { return false }

// BuildKVCacheAffinityInvokeKwargs 测试替身：返回空亲和参数。
func (m *stageAgentMockClient) BuildKVCacheAffinityInvokeKwargs(_ any, _, _ string, _ int) map[string]any {
	return map[string]any{}
}
