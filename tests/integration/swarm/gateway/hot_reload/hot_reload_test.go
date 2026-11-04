//go:build integration

package hot_reload_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/suite"
	ceiface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor/compressor"
	model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/channel_manager/web"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HotReloadSuite 验证「界面保存配置」之后主 ReAct 循环与压缩链路是否跟随新模型/新 token 上限。
//
// 观察手段：所有模型的 client_provider 都指向测试注册的录制工厂，
// 工厂按 ModelRequestConfig.ModelName 给客户端打标；主循环走 Stream、压缩走 Invoke，
// 因此每条调用记录都能回答「哪条链路用了哪个模型」。
type HotReloadSuite struct {
	isuite.BaseIntegrationSuite
	// env 每测试隔离的集成环境（数据目录 / MockLLM provider / env 备份）
	env *testhelpers.AdapterTestEnv
	// log 模型调用记录（跨多个 client 实例共享）
	log *callLog
}

// callRecord 一次模型调用的记录。
type callRecord struct {
	// model 该客户端所属的模型名（来自 ModelRequestConfig.ModelName）
	model string
	// kind 调用类型：stream（主循环）/ invoke（压缩等同步调用）
	kind string
	// messages 入参消息
	messages model_clients.MessagesParam
}

// callLog 并发安全的调用记录集合。
type callLog struct {
	// mu 保护 calls
	mu sync.Mutex
	// calls 全部调用记录
	calls []callRecord
}

// recordingClient 给 MockModelClient 打上模型名标签并记录调用。
type recordingClient struct {
	// MockModelClient 复用其全部接口实现
	*mockllm.MockModelClient
	// model 本客户端所属模型名
	model string
	// log 共享调用记录
	log *callLog
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestHotReloadSuite 启动界面热重载跟随性套件。
func TestHotReloadSuite(t *testing.T) {
	suite.Run(t, new(HotReloadSuite))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// SetupTest 每个测试前构建隔离环境，并把 "OpenAI" provider 替换成录制工厂。
//
// 本包（tests/integration/swarm/gateway/hot_reload）为独立测试包，
// 覆盖 "OpenAI" 注册项不会影响其它测试包；用例结束再注销。
func (s *HotReloadSuite) SetupTest() {
	s.log = &callLog{}
	s.env = testhelpers.NewAdapterTestEnv(s.T(), s.MockLLM, "mock_hot_reload")

	registry := model_clients.GetClientRegistry()
	_ = registry.Unregister("OpenAI", "llm")
	registry.Register("OpenAI", "llm", s.factory())
	s.T().Cleanup(func() { _ = registry.Unregister("OpenAI", "llm") })
}

// factory 返回按模型名打标的客户端工厂。
func (s *HotReloadSuite) factory() model_clients.ClientFactory {
	return func(modelConfig *llmschema.ModelRequestConfig, _ *llmschema.ModelClientConfig) (model_clients.BaseModelClient, error) {
		modelName := ""
		if modelConfig != nil {
			modelName = modelConfig.ModelName
		}
		return &recordingClient{
			MockModelClient: mockllm.NewMockModelClient(),
			model:           modelName,
			log:             s.log,
		}, nil
	}
}

// TestUISave_切换默认模型后主循环跟随 界面保存（config.save_all）把另一个模型
// 调整到默认位置后，下一轮 ReAct 主循环必须用新模型。
//
// 切换方式说明：models.defaults 的读取侧会 `InferIsDefault`（按 model_name 分组，
// 单条目组一律置 true），所以「勾选 is_default」无法表达全局默认；
// 真正决定默认模型的是**列表中第一个带纯 model_name key 的条目**（对齐 Python
// `_create_model` 遍历 `_model_name_to_keys` 插入序），因此这里用重排表达切换。
func (s *HotReloadSuite) TestUISave_切换默认模型后主循环跟随() {
	s.writeConfig("model-a", 800, 200, 800, 200)
	d := s.newAdapter()

	// 保存前：主循环走 model-a
	s.runTurn(d, "sess_hr_main", "req-hr-1", "你好")
	s.Require().Equal([]string{"model-a"}, s.log.modelsUsed("stream"),
		"保存前主循环应使用默认模型 model-a，实际: %v", s.log.modelsUsed("stream"))

	s.log.reset()

	// 界面保存：model-b 排到首位成为默认模型
	s.uiSaveConfig(d, []map[string]any{
		modelEntry(1, "model-b", true, 800, 200),
		modelEntry(0, "model-a", false, 800, 200),
	})

	s.Run("模型缓存已包含新旧模型", func() {
		cache := d.ModelCache()
		s.Require().Contains(cache, "model-a")
		s.Require().Contains(cache, "model-b")
	})

	s.runTurn(d, "sess_hr_main", "req-hr-2", "再说一句")
	s.Equal([]string{"model-b"}, s.log.modelsUsed("stream"),
		"保存后主循环应使用新默认模型 model-b，实际: %v", s.log.modelsUsed("stream"))
}

// TestUISave_切换模型与token后新会话压缩链路跟随 界面同时改模型与 token 上限后，
// 新建会话的压缩处理器必须用新模型、新阈值。
func (s *HotReloadSuite) TestUISave_切换模型与token后新会话压缩链路跟随() {
	s.writeConfig("model-a", 800, 200, 800, 200)
	d := s.newAdapter()

	// 保存前：窗口 800 → 触发 680 / 目标 440，压缩走 model-a
	trigger, target := thresholds(s.reactConfig(d).ContextProcessors)
	s.Equal(680, trigger, "保存前触发阈值 = 800×0.85")
	s.Equal(440, target, "保存前目标阈值 = 800×0.55")

	s.runTurn(d, "sess_hr_compress", "req-hr-3", "你好")
	s.Require().NotEmpty(s.log.modelsUsedCompression(),
		"小窗口下压缩链路应至少调用一次模型")
	s.Equal([]string{"model-a"}, s.log.modelsUsedCompression(),
		"保存前压缩应使用 model-a，实际: %v", s.log.modelsUsedCompression())

	s.log.reset()

	// 界面保存：model-b 排首位成为默认模型，输入上限 800 → 600
	s.uiSaveConfig(d, []map[string]any{
		modelEntry(1, "model-b", true, 600, 200),
		modelEntry(0, "model-a", false, 800, 200),
	})

	// 阈值在下一轮 ensureInitialized 重建 rail 时重算，因此断言放在轮次之后
	s.runTurn(d, "sess_hr_compress_new", "req-hr-4", "再压缩一次")

	trigger, target = thresholds(s.reactConfig(d).ContextProcessors)
	s.Equal(510, trigger, "保存后触发阈值应重算为 600×0.85")
	s.Equal(330, target, "保存后目标阈值应重算为 600×0.55")

	s.Require().NotEmpty(s.log.modelsUsedCompression(),
		"保存后压缩链路应至少调用一次模型")
	s.Equal([]string{"model-b"}, s.log.modelsUsedCompression(),
		"保存后压缩应使用 model-b，实际: %v", s.log.modelsUsedCompression())
}

// TestUISave_已存在会话的压缩处理器跟随 老会话也必须跟随热重载：
// 压缩处理器实例原本在「会话上下文首次创建」时固化，热重载只更新 specs 时
// 老会话会继续用旧实例（旧模型/旧阈值）。现在 ContextEngine 会在下一次
// CreateContext（轮次入口）按配置指纹重建处理器并刷新窗口原料。
func (s *HotReloadSuite) TestUISave_已存在会话的压缩处理器跟随() {
	s.writeConfig("model-a", 800, 200, 800, 200)
	d := s.newAdapter()

	s.runTurn(d, "sess_hr_legacy", "req-hr-5", "你好")
	s.Require().Equal([]string{"model-a"}, s.log.modelsUsedCompression(),
		"保存前压缩应使用 model-a，实际: %v", s.log.modelsUsedCompression())

	s.log.reset()

	s.uiSaveConfig(d, []map[string]any{
		modelEntry(1, "model-b", true, 600, 200),
		modelEntry(0, "model-a", false, 800, 200),
	})

	// 沿用同一 session：轮次入口按指纹重建处理器并刷新窗口原料
	s.runTurn(d, "sess_hr_legacy", "req-hr-6", "再压缩一次")

	trigger, target := thresholds(s.reactConfig(d).ContextProcessors)
	s.Equal(510, trigger, "老会话触发阈值应重算为 600×0.85")
	s.Equal(330, target, "老会话目标阈值应重算为 600×0.55")
	s.Equal([]string{"model-b"}, s.log.modelsUsedCompression(),
		"老会话的压缩应改用 model-b，实际: %v", s.log.modelsUsedCompression())
}

// writeConfig 写入含两个模型条目的 config.yaml。
//
// 窗口取 maxInput，比例 0.85/0.55 由 context_engine_config 固定，便于直接推算阈值。
func (s *HotReloadSuite) writeConfig(defaultModel string, aInput, aOutput, bInput, bOutput int) {
	s.T().Helper()

	yaml := fmt.Sprintf(`models:
  defaults:
    - model_client_config:
        model_name: model-a
        client_provider: OpenAI
        api_key: mock-api-key
        api_base: http://127.0.0.1:1
        max_input_tokens: %d
        max_output_tokens: %d
      is_default: %t
    - model_client_config:
        model_name: model-b
        client_provider: OpenAI
        api_key: mock-api-key
        api_base: http://127.0.0.1:1
        max_input_tokens: %d
        max_output_tokens: %d
      is_default: %t

react:
  agent_name: main_agent
  context_engine_config:
    enabled: true
    compression_trigger_ratio: 0.85
    compression_target_ratio: 0.55
`, aInput, aOutput, defaultModel == "model-a", bInput, bOutput, defaultModel == "model-b")

	s.Require().NoError(os.WriteFile(
		filepath.Join(s.env.DataDir, "config", "config.yaml"), []byte(yaml), 0o644))
}

// newAdapter 构建并初始化 DeepAdapter。
func (s *HotReloadSuite) newAdapter() *adapter.DeepAdapter {
	s.T().Helper()
	d := adapter.NewDeepAdapter()
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, "agent.work.plan", ""))
	s.T().Cleanup(func() { _ = d.Cleanup() })
	return d
}

// runTurn 跑一轮对话（流式入口）并等待结束。
func (s *HotReloadSuite) runTurn(d *adapter.DeepAdapter, sessionID, requestID, query string) {
	s.T().Helper()

	params, err := json.Marshal(map[string]any{"mode": "agent.work.plan"})
	s.Require().NoError(err)
	sid := sessionID
	req := &schema.AgentRequest{
		RequestID: requestID,
		ChannelID: "web",
		SessionID: &sid,
		Params:    params,
		IsStream:  true,
	}
	chunkCh, streamErr := d.ProcessMessageStreamImpl(s.Ctx, req, map[string]any{"query": query})
	s.Require().NoError(streamErr, "流式入口不应返回启动错误")
	for range chunkCh {
		// 只需跑完链路，事件内容不在本用例关注范围
	}
}

// uiSaveConfig 走 web 的 config.save_all 保存模型条目，并回调触发 AgentServer 侧热重载。
//
// 回调体等价 AgentManager.ReloadAgentsConfig 对单个 agent 的动作
// （agent_manager.go:396 → entry.agent.ReloadAgentConfig）。
func (s *HotReloadSuite) uiSaveConfig(d *adapter.DeepAdapter, models []map[string]any) {
	s.T().Helper()

	var reloaded bool
	var reloadErr error
	dispatcher := web.RegisterWebHandlers(&web.WebHandlersBindParams{
		OnConfigSaved: func(_ []string, envUpdates map[string]any, configPayload map[string]any) error {
			reloaded = true
			reloadErr = d.ReloadAgentConfig(s.Ctx, configPayload, envUpdates)
			return reloadErr
		},
	})

	resp, err := dispatcher.Dispatch("config.save_all", map[string]any{
		"config": map[string]any{},
		"models": toAnyList(models),
	}, "")
	s.Require().NoError(err)
	s.Require().Equal(true, resp["ok"], "config.save_all 应成功，实际回包: %+v", resp)
	s.Require().True(reloaded, "保存后必须触发热重载回调（等价 agent.reload_config）")
	s.Require().NoError(reloadErr, "热重载本身不应报错")
}

// toAnyList 把 []map[string]any 转为前端 JSON 反序列化后的 []any 形态。
//
// config.save_all 的 models 子载荷按 []any 解析（模拟 WebSocket 收到的 JSON 数组），
// 直接传 []map 会被判为 "models must be a non-empty list"。
func toAnyList(items []map[string]any) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	return out
}

// modelEntry 构造一条前端风格的模型条目。
func modelEntry(originIndex int, modelName string, isDefault bool, maxInput, maxOutput int) map[string]any {
	return map[string]any{
		"model_name":        modelName,
		"api_key":           "mock-api-key",
		"api_base":          "http://127.0.0.1:1",
		"model_provider":    "OpenAI",
		"is_default":        isDefault,
		"max_input_tokens":  maxInput,
		"max_output_tokens": maxOutput,
		"origin_index":      originIndex,
	}
}

// thresholds 从处理器列表取 RoundLevelCompressor 的触发/目标阈值。
func thresholds(specs []ceiface.ProcessorSpec) (int, int) {
	for _, spec := range specs {
		if cfg, ok := spec.Config.(*compressor.RoundLevelCompressorConfig); ok {
			return cfg.TriggerTotalTokens, cfg.TargetTotalTokens
		}
	}
	return 0, 0
}

// add 追加一条调用记录。
func (l *callLog) add(record callRecord) {
	l.mu.Lock()
	l.calls = append(l.calls, record)
	l.mu.Unlock()
}

// reset 清空记录。
func (l *callLog) reset() {
	l.mu.Lock()
	l.calls = nil
	l.mu.Unlock()
}

// modelsUsed 返回指定调用类型出现过的模型名（去重、保序）。
func (l *callLog) modelsUsed(kind string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()

	seen := make(map[string]struct{}, len(l.calls))
	out := make([]string, 0, len(l.calls))
	for _, record := range l.calls {
		if record.kind != kind {
			continue
		}
		if _, ok := seen[record.model]; ok {
			continue
		}
		seen[record.model] = struct{}{}
		out = append(out, record.model)
	}
	return out
}

// modelsUsedCompression 返回压缩链路（按压缩提示词识别）出现过的模型名。
//
// 压缩提示词特征：系统消息含 "Compression Expert"，用户消息含 "[Compression Task]"。
func (l *callLog) modelsUsedCompression() []string {
	l.mu.Lock()
	defer l.mu.Unlock()

	seen := make(map[string]struct{}, len(l.calls))
	out := make([]string, 0, len(l.calls))
	for _, record := range l.calls {
		if !looksLikeCompression(record.messages) {
			continue
		}
		if _, ok := seen[record.model]; ok {
			continue
		}
		seen[record.model] = struct{}{}
		out = append(out, record.model)
	}
	return out
}

// looksLikeCompression 判断一次调用是否为压缩链路。
func looksLikeCompression(messages model_clients.MessagesParam) bool {
	for _, message := range messages.Messages() {
		text := message.GetContent().Text()
		if strings.Contains(text, "Compression Expert") || strings.Contains(text, "[Compression Task]") {
			return true
		}
	}
	return false
}

// count 返回指定调用类型的总次数。
func (l *callLog) count(kind string) int {
	l.mu.Lock()
	defer l.mu.Unlock()

	total := 0
	for _, record := range l.calls {
		if record.kind == kind {
			total++
		}
	}
	return total
}

// Invoke 记录同步调用后委托给 MockModelClient。
func (c *recordingClient) Invoke(
	ctx context.Context,
	messages model_clients.MessagesParam,
	opts ...model_clients.InvokeOption,
) (*llmschema.AssistantMessage, error) {
	c.log.add(callRecord{model: c.model, kind: "invoke", messages: messages})
	return c.MockModelClient.Invoke(ctx, messages, opts...)
}

// Stream 记录流式调用后委托给 MockModelClient。
func (c *recordingClient) Stream(
	ctx context.Context,
	messages model_clients.MessagesParam,
	opts ...model_clients.StreamOption,
) (<-chan *llmschema.AssistantMessageChunk, error) {
	c.log.add(callRecord{model: c.model, kind: "stream", messages: messages})
	return c.MockModelClient.Stream(ctx, messages, opts...)
}

// reactConfig 返回主 ReActAgent 的配置。
func (s *HotReloadSuite) reactConfig(d *adapter.DeepAdapter) *saconfig.ReActAgentConfig {
	s.T().Helper()

	reactAgent := d.Instance().ReactAgent()
	s.Require().NotNil(reactAgent)
	cfg, ok := reactAgent.Config().(*saconfig.ReActAgentConfig)
	s.Require().True(ok, "应为 *ReActAgentConfig，实际 %T", reactAgent.Config())
	return cfg
}
