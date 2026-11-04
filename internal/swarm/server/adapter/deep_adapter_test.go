package adapter

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	cronbridge "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools/cron"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewDeepAdapter 测试 DeepAdapter 构造函数默认值。
func TestNewDeepAdapter(t *testing.T) {
	d := NewDeepAdapter()
	if d.agentName != "main_agent" {
		t.Errorf("agentName = %q, want %q", d.agentName, "main_agent")
	}
	if d.isCodeAgent != false {
		t.Errorf("isCodeAgent = %v, want false", d.isCodeAgent)
	}
	if d.activeSessionIDs == nil {
		t.Error("activeSessionIDs is nil, want initialized map")
	}
}

// TestDeepAdapter_接口满足性 编译期检查 DeepAdapter 实现 AgentAdapter 接口。
func TestDeepAdapter_接口满足性(t *testing.T) {
	var _ AgentAdapter = (*DeepAdapter)(nil)
}

func TestDeepAdapter_Instance_未初始化(t *testing.T) {
	d := NewDeepAdapter()
	if inst := d.Instance(); inst != nil {
		t.Error("未初始化时 Instance() 应返回 nil")
	}
}

func TestDeepAdapter_Instance_nil适配器(t *testing.T) {
	var d *DeepAdapter
	if inst := d.Instance(); inst != nil {
		t.Error("nil DeepAdapter Instance() 应返回 nil")
	}
}

// TestDeepAdapter_SessionActive 测试 session 活跃计数四个方法的完整语义。
func TestDeepAdapter_SessionActive(t *testing.T) {
	d := NewDeepAdapter()

	// 初始状态：无活跃 session
	if d.isSessionActive("s1") {
		t.Error("初始状态 s1 不应活跃")
	}

	// mark 一次
	d.markSessionActive("s1")
	if !d.isSessionActive("s1") {
		t.Error("mark 后 s1 应活跃")
	}
	if d.otherActiveSessions("s1") != 0 {
		t.Error("仅 s1 活跃，otherActiveSessions(s1) 应为 0")
	}

	// mark 两次（Counter 语义）
	d.markSessionActive("s1")
	d.markSessionActive("s2")
	if d.otherActiveSessions("s1") != 1 {
		t.Errorf("s1 计数2 + s2 计数1，otherActiveSessions(s1) = %d, want 1", d.otherActiveSessions("s1"))
	}

	// unmark 一次 s1（计数从2降到1，不应删除）
	d.unmarkSessionActive("s1")
	if !d.isSessionActive("s1") {
		t.Error("unmark 一次后 s1 仍应活跃（计数=1）")
	}

	// unmark 再次 s1（计数归零，应删除）
	d.unmarkSessionActive("s1")
	if d.isSessionActive("s1") {
		t.Error("unmark 归零后 s1 不应活跃")
	}

	// unmark 不存在的 session 不应 panic
	d.unmarkSessionActive("nonexistent")
}

// TestDeepAdapter_ProcessInterrupt_Intent分支 测试 pause/resume/cancel/supplement 分支。
func TestDeepAdapter_ProcessInterrupt_Intent分支(t *testing.T) {
	d := NewDeepAdapter()
	ctx := t.Context()

	tests := []struct {
		name   string
		intent string
	}{
		{"pause", "pause"},
		{"resume", "resume"},
		{"cancel", "cancel"},
		{"supplement", "supplement"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sid := "test-session"
			params, _ := json.Marshal(map[string]any{"intent": tt.intent})
			req := schema.NewAgentRequest("req-1", "ch-1", schema.ReqMethod("chat.send"), params,
				schema.WithAgentSessionID(sid),
			)
			resp, err := d.ProcessInterrupt(ctx, req)
			if err != nil {
				t.Errorf("ProcessInterrupt intent=%s error: %v", tt.intent, err)
			}
			if resp == nil {
				t.Errorf("ProcessInterrupt intent=%s 返回 nil 响应", tt.intent)
			}
		})
	}
}

// TestDeepAdapter_ProcessInterrupt_未初始化 测试 instance=nil 时不 panic。
func TestDeepAdapter_ProcessInterrupt_未初始化(t *testing.T) {
	d := NewDeepAdapter()
	// instance 为 nil，但 ProcessInterrupt 不检查 instance，应正常返回
	ctx := t.Context()
	params, _ := json.Marshal(map[string]any{"intent": "cancel"})
	req := schema.NewAgentRequest("req-1", "ch-1", schema.ReqMethod("chat.send"), params)
	resp, err := d.ProcessInterrupt(ctx, req)
	if err != nil {
		t.Errorf("ProcessInterrupt error: %v", err)
	}
	if resp == nil {
		t.Error("ProcessInterrupt 返回 nil 响应")
	}
}

// TestDeepAdapter_HandleUserAnswer_前缀分发 测试 request_id 前缀分发逻辑。
func TestDeepAdapter_HandleUserAnswer_前缀分发(t *testing.T) {
	d := NewDeepAdapter()
	ctx := t.Context()

	tests := []struct {
		name      string
		requestID string
		resolved  bool
	}{
		{"team_skill_evolve_前缀", "team_skill_evolve_123", false},
		{"evolve_simplify_前缀", "evolve_simplify_456", false},
		{"skill_evolve_前缀", "skill_evolve_789", false},
		{"未知前缀", "unknown_prefix", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params, _ := json.Marshal(map[string]any{
				"request_id": tt.requestID,
				"answers":    []any{},
			})
			req := schema.NewAgentRequest("req-1", "ch-1", schema.ReqMethod("chat.user_answer"), params)
			resp, err := d.HandleUserAnswer(ctx, req)
			if err != nil {
				t.Errorf("HandleUserAnswer error: %v", err)
			}
			if resp == nil {
				t.Fatal("HandleUserAnswer 返回 nil 响应")
			}
			if resp.OK != true {
				t.Errorf("OK = %v, want true", resp.OK)
			}
			if resp.Payload["accepted"] != true {
				t.Errorf("accepted = %v, want true", resp.Payload["accepted"])
			}
			if resp.Payload["resolved"] != tt.resolved {
				t.Errorf("resolved = %v, want %v", resp.Payload["resolved"], tt.resolved)
			}
		})
	}
}

// TestDeepAdapter_HandleHeartbeat_前缀检查 测试 heartbeat 前缀判断。
func TestDeepAdapter_HandleHeartbeat_前缀检查(t *testing.T) {
	d := NewDeepAdapter()
	ctx := t.Context()

	t.Run("heartbeat前缀", func(t *testing.T) {
		sid := "heartbeat_session_1"
		req := schema.NewAgentRequest("req-1", "ch-1", schema.ReqMethod("chat.send"), nil,
			schema.WithAgentSessionID(sid),
		)
		resp, err := d.HandleHeartbeat(ctx, req)
		if err != nil {
			t.Errorf("HandleHeartbeat error: %v", err)
		}
		// heartbeat 返回 nil（继续正常流程）
		if resp != nil {
			t.Errorf("heartbeat 应返回 nil，got %v", resp)
		}
	})

	t.Run("非heartbeat前缀", func(t *testing.T) {
		sid := "normal_session_1"
		req := schema.NewAgentRequest("req-1", "ch-1", schema.ReqMethod("chat.send"), nil,
			schema.WithAgentSessionID(sid),
		)
		resp, err := d.HandleHeartbeat(ctx, req)
		if err != nil {
			t.Errorf("HandleHeartbeat error: %v", err)
		}
		// 非 heartbeat 返回 nil
		if resp != nil {
			t.Errorf("非 heartbeat 应返回 nil，got %v", resp)
		}
	})
}

// TestDeepAdapter_ProcessMessageImpl_未初始化 测试 instance=nil 时返回错误。
func TestDeepAdapter_ProcessMessageImpl_未初始化(t *testing.T) {
	d := NewDeepAdapter()
	ctx := t.Context()
	req := schema.NewAgentRequest("req-1", "ch-1", schema.ReqMethod("chat.send"), nil)
	_, err := d.ProcessMessageImpl(ctx, req, nil)
	if err == nil {
		t.Error("instance=nil 时应返回错误")
	}
}

// TestDeepAdapter_ProcessMessageStreamImpl_未初始化 测试 instance=nil 时返回错误。
func TestDeepAdapter_ProcessMessageStreamImpl_未初始化(t *testing.T) {
	d := NewDeepAdapter()
	ctx := t.Context()
	req := schema.NewAgentRequest("req-1", "ch-1", schema.ReqMethod("chat.send"), nil)
	_, err := d.ProcessMessageStreamImpl(ctx, req, nil)
	if err == nil {
		t.Error("instance=nil 时应返回错误")
	}
}

// TestParseParams 测试 json.RawMessage 解析。
func TestParseParams(t *testing.T) {
	t.Run("空输入", func(t *testing.T) {
		m := parseParams(nil)
		if len(m) != 0 {
			t.Errorf("nil 输入应返回空 map，got %d items", len(m))
		}
	})

	t.Run("空JSON", func(t *testing.T) {
		m := parseParams(json.RawMessage(``))
		if len(m) != 0 {
			t.Errorf("空 JSON 应返回空 map，got %d items", len(m))
		}
	})

	t.Run("无效JSON", func(t *testing.T) {
		m := parseParams(json.RawMessage(`invalid`))
		if len(m) != 0 {
			t.Errorf("无效 JSON 应返回空 map，got %d items", len(m))
		}
	})

	t.Run("有效JSON", func(t *testing.T) {
		m := parseParams(json.RawMessage(`{"query":"hello","mode":"agent.work.plan"}`))
		if m["query"] != "hello" {
			t.Errorf("query = %v, want hello", m["query"])
		}
		if m["mode"] != "agent.work.plan" {
			t.Errorf("mode = %v, want agent.plan", m["mode"])
		}
	})
}

// TestParamsString 测试字符串取值。
func TestParamsString(t *testing.T) {
	params := map[string]any{"key": "value", "num": 123}

	t.Run("存在字符串值", func(t *testing.T) {
		if v := paramsString(params, "key", "default"); v != "value" {
			t.Errorf("got %q, want %q", v, "value")
		}
	})

	t.Run("键不存在", func(t *testing.T) {
		if v := paramsString(params, "missing", "default"); v != "default" {
			t.Errorf("got %q, want %q", v, "default")
		}
	})

	t.Run("值非字符串", func(t *testing.T) {
		if v := paramsString(params, "num", "default"); v != "default" {
			t.Errorf("非字符串值应返回默认值，got %q", v)
		}
	})
}

// TestDeepAdapter_CreateInstance_默认值 测试 CreateInstance 基本参数处理。
func TestDeepAdapter_CreateInstance_默认值(t *testing.T) {
	d := NewDeepAdapter()
	ctx := t.Context()

	// 不传 config，mode="agent.work.plan"
	err := d.CreateInstance(ctx, nil, "agent.work.plan", "")
	if err != nil {
		t.Fatalf("CreateInstance error: %v", err)
	}
	if d.mode != "agent.work.plan" {
		t.Errorf("mode = %q, want %q", d.mode, "agent.work.plan")
	}
	if d.dreamingMode != "agent.work.plan" {
		t.Errorf("dreamingMode = %q, want %q", d.dreamingMode, "agent.work.plan")
	}
}

// TestDeepAdapter_CreateInstance_dreamingMode 测试 dreaming_mode 按 mode 前缀判断。
func TestDeepAdapter_CreateInstance_dreamingMode(t *testing.T) {
	ctx := t.Context()

	tests := []struct {
		mode      string
		wantDream string
	}{
		{"agent.work.plan", "agent.work.plan"},
		{"agent.work.normal", "agent.work.normal"},
		{"code", "agent"}, // 非 "agent" 前缀 → 默认 "agent"
		{"", "agent"},     // 空 mode → 默认 "agent"
		{"team", "agent"}, // 非 "agent" 前缀 → 默认 "agent"
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("mode=%s", tt.mode), func(t *testing.T) {
			d := NewDeepAdapter()
			err := d.CreateInstance(ctx, nil, tt.mode, "")
			if err != nil {
				t.Fatalf("CreateInstance error: %v", err)
			}
			if d.dreamingMode != tt.wantDream {
				t.Errorf("dreamingMode = %q, want %q", d.dreamingMode, tt.wantDream)
			}
		})
	}
}

// TestDeepAdapter_CreateInstance_config覆盖 测试 config 字典中 agent_name/project_dir 提取。
func TestDeepAdapter_CreateInstance_config覆盖(t *testing.T) {
	d := NewDeepAdapter()
	ctx := t.Context()

	config := map[string]any{
		"agent_name":  "custom_agent",
		"project_dir": "/tmp/project",
	}
	err := d.CreateInstance(ctx, config, "agent.work.plan", "")
	if err != nil {
		t.Fatalf("CreateInstance error: %v", err)
	}
	if d.agentName != "custom_agent" {
		t.Errorf("agentName = %q, want %q", d.agentName, "custom_agent")
	}
	if d.projectDir != "/tmp/project" {
		t.Errorf("projectDir = %q, want %q", d.projectDir, "/tmp/project")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newModelConfigTestAdapter 构造用于模型配置判定的适配器，隔离外部 config.yaml。
// 第 4 步兜底会读盘，若不置空会让判定结果依赖运行机器上的真实配置。
func newModelConfigTestAdapter() *DeepAdapter {
	d := NewDeepAdapter()
	d.defaultModelConfigReader = nil
	return d
}

// usableTestModel 构造一个「凭据可用」的模型（真实 api_key + 非占位 api_base）。
func usableTestModel() *llm.Model {
	return &llm.Model{ClientConfig: &llmschema.ModelClientConfig{
		APIKey:  "sk-test",
		APIBase: "https://api.openai.com/v1",
	}}
}

// TestHasValidModelConfig_未配置模型返回false 验证「什么都没配」时判定无效。
//
// 旧实现空字符串恒返回 true，导致首次安装（config.yaml 自带占位 api_base）被放行，
// 心跳真的向 example.com 发请求并报底层网络错误。对齐 Python _has_valid_model_config：
// 空模型名只表示「用默认模型」，仍需默认模型凭据可用。
func TestHasValidModelConfig_未配置模型返回false(t *testing.T) {
	d := newModelConfigTestAdapter()
	if d.hasValidModelConfig("") {
		t.Error("无任何可用模型时应返回 false（模型未正确配置）")
	}
	// 默认模型存在但凭据缺失，同样无效
	d.model = &llm.Model{ClientConfig: &llmschema.ModelClientConfig{}}
	if d.hasValidModelConfig("") {
		t.Error("默认模型 api_key/api_base 为空时应返回 false")
	}
}

// TestHasValidModelConfig_默认模型可用返回true 验证空模型名 + 默认模型凭据可用。
func TestHasValidModelConfig_默认模型可用返回true(t *testing.T) {
	d := newModelConfigTestAdapter()
	d.model = usableTestModel()
	if !d.hasValidModelConfig("") {
		t.Error("默认模型凭据可用时应返回 true")
	}
}

// TestHasValidModelConfig_缓存中存在 验证缓存中凭据可用的模型返回 true。
func TestHasValidModelConfig_缓存中存在(t *testing.T) {
	d := newModelConfigTestAdapter()
	d.modelCache = map[string]*llm.Model{"gpt-4": usableTestModel()}
	if !d.hasValidModelConfig("gpt-4") {
		t.Error("缓存中凭据可用的模型应返回 true")
	}
}

// TestHasValidModelConfig_占位或空凭据返回false 验证占位 api_base 与空凭据被判无效。
//
// Python: _mcc_looks_usable —— api_key 非空 && api_base 非空 &&
// api_base 不在 _PLACEHOLDER_API_BASES（含 https://example.com/compatible-mode/v1）。
func TestHasValidModelConfig_占位或空凭据返回false(t *testing.T) {
	cases := map[string]*llm.Model{
		"占位 api_base": {ClientConfig: &llmschema.ModelClientConfig{
			APIKey:  "sk-test",
			APIBase: "https://example.com/compatible-mode/v1",
		}},
		"空 api_key": {ClientConfig: &llmschema.ModelClientConfig{
			APIBase: "https://api.openai.com/v1",
		}},
		"空 api_base": {ClientConfig: &llmschema.ModelClientConfig{
			APIKey: "sk-test",
		}},
		"ClientConfig 为 nil": {},
	}
	for name, model := range cases {
		d := newModelConfigTestAdapter()
		d.modelCache = map[string]*llm.Model{"m": model}
		if d.hasValidModelConfig("m") {
			t.Errorf("%s 应返回 false（凭据不可用）", name)
		}
	}
	// nil 模型条目同样无效（旧用例曾把 nil 当「存在即有效」）
	d := newModelConfigTestAdapter()
	d.modelCache = map[string]*llm.Model{"gpt-4": nil}
	if d.hasValidModelConfig("gpt-4") {
		t.Error("缓存中 nil 模型应返回 false")
	}
}

// TestHasValidModelConfig_回退到缓存中任意可用模型 验证第 3 条回退（Python 同序）。
func TestHasValidModelConfig_回退到缓存中任意可用模型(t *testing.T) {
	d := newModelConfigTestAdapter()
	d.modelCache = map[string]*llm.Model{
		"broken": {ClientConfig: &llmschema.ModelClientConfig{}},
		"good":   usableTestModel(),
	}
	if !d.hasValidModelConfig("nonexistent") {
		t.Error("指定模型不存在但缓存中另有可用模型时应返回 true")
	}
}

// TestHasValidModelConfig_config兜底 验证第 4 步：内存里判定不出时读 config.yaml 兜底。
//
// Python 原文：
//
//	mcc = get_config().get("models", {}).get("default", {}).get("model_client_config", {})
//	if isinstance(mcc, dict) and _mcc_looks_usable(mcc): return True
func TestHasValidModelConfig_config兜底(t *testing.T) {
	// 有效凭据 → true
	d := newModelConfigTestAdapter()
	d.defaultModelConfigReader = func() map[string]any {
		return map[string]any{"api_key": "sk-config", "api_base": "https://api.deepseek.com/v1"}
	}
	if !d.hasValidModelConfig("") {
		t.Error("config.yaml 有可用凭据时应返回 true")
	}

	// 占位 api_base → 无效
	d = newModelConfigTestAdapter()
	d.defaultModelConfigReader = func() map[string]any {
		return map[string]any{"api_key": "sk-config", "api_base": "https://example.com/compatible-mode/v1"}
	}
	if d.hasValidModelConfig("") {
		t.Error("config.yaml 仍是占位 api_base 时应返回 false")
	}

	// 缺 api_key → 无效
	d = newModelConfigTestAdapter()
	d.defaultModelConfigReader = func() map[string]any {
		return map[string]any{"api_base": "https://api.deepseek.com/v1"}
	}
	if d.hasValidModelConfig("") {
		t.Error("config.yaml 缺 api_key 时应返回 false")
	}

	// 读取失败（nil）→ 无效，且不 panic
	d = newModelConfigTestAdapter()
	d.defaultModelConfigReader = func() map[string]any { return nil }
	if d.hasValidModelConfig("") {
		t.Error("config.yaml 读取失败时应返回 false")
	}

	// 非字符串凭据：Python str(v or "") 会强转，等价可用
	d = newModelConfigTestAdapter()
	d.defaultModelConfigReader = func() map[string]any {
		return map[string]any{"api_key": 123456, "api_base": "https://api.deepseek.com/v1"}
	}
	if !d.hasValidModelConfig("") {
		t.Error("非字符串 api_key 应经 str() 语义强转后判定为可用")
	}
}

// TestAnyToConfigString_Python真值语义 验证 str(v or "") 的假值收敛。
//
// Python `mcc.get("api_key", "") or ""`：None / "" / 0 / False 都当「空」，
// 所以 api_key: 0 在 Python 侧等于未配置；非零数值则 str() 成字符串。
func TestAnyToConfigString_Python真值语义(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"nil", nil, ""},
		{"空串", "", ""},
		{"普通串", "sk-1", "sk-1"},
		{"字符串 0", "0", "0"},
		{"整数 0", 0, ""},
		{"非零整数", 123456, "123456"},
		{"false", false, ""},
		{"true", true, "true"},
		{"float 0", 0.0, ""},
		{"float 非零", 1.5, "1.5"},
		{"空切片", []string{}, ""},
		{"uint 0", uint(0), ""},
		{"非零 uint", uint(7), "7"},
		{"空 map", map[string]any{}, ""},
		{"空数组", [0]int{}, ""},
	}
	for _, c := range cases {
		if got := anyToConfigString(c.in); got != c.want {
			t.Errorf("%s: anyToConfigString(%#v) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

// TestHasValidModelConfig_数值0凭据视为未配置 验证 api_key: 0 不被误判为可用。
func TestHasValidModelConfig_数值0凭据视为未配置(t *testing.T) {
	d := newModelConfigTestAdapter()
	d.defaultModelConfigReader = func() map[string]any {
		return map[string]any{"api_key": 0, "api_base": "https://api.deepseek.com/v1"}
	}
	if d.hasValidModelConfig("") {
		t.Error("api_key 为 0（Python 假值）应视为未配置")
	}
}

// TestHasValidModelConfig_缓存中不存在 验证缓存中不存在的模型返回 false
func TestHasValidModelConfig_缓存中不存在(t *testing.T) {
	d := newModelConfigTestAdapter()
	if d.hasValidModelConfig("nonexistent") {
		t.Error("缓存中不存在的模型应返回 false")
	}
}

// ──────────────────────────── 可选接口编译期检查 ────────────────────────────

// TestDeepAdapter_实现ContextCompressor 编译期检查 DeepAdapter 实现 ContextCompressor。
func TestDeepAdapter_实现ContextCompressor(t *testing.T) {
	var _ ContextCompressor = (*DeepAdapter)(nil)
}

// TestDeepAdapter_实现DreamingController 编译期检查 DeepAdapter 实现 DreamingController。
func TestDeepAdapter_实现DreamingController(t *testing.T) {
	var _ DreamingController = (*DeepAdapter)(nil)
}

// TestDeepAdapter_实现GatewayDisconnectHandler 编译期检查 DeepAdapter 实现 GatewayDisconnectHandler。
func TestDeepAdapter_实现GatewayDisconnectHandler(t *testing.T) {
	var _ GatewayDisconnectHandler = (*DeepAdapter)(nil)
}

// TestCodeAdapter_实现ContextCompressor 编译期检查 CodeAdapter 实现 ContextCompressor。
func TestCodeAdapter_实现ContextCompressor(t *testing.T) {
	var _ ContextCompressor = (*CodeAdapter)(nil)
}

// TestCodeAdapter_实现DreamingController 编译期检查 CodeAdapter 实现 DreamingController。
func TestCodeAdapter_实现DreamingController(t *testing.T) {
	var _ DreamingController = (*CodeAdapter)(nil)
}

// TestCodeAdapter_实现GatewayDisconnectHandler 编译期检查 CodeAdapter 实现 GatewayDisconnectHandler。
func TestCodeAdapter_实现GatewayDisconnectHandler(t *testing.T) {
	var _ GatewayDisconnectHandler = (*CodeAdapter)(nil)
}

// TestDeepAdapter_CancelSchedulerRunningTasks_Nil安全 未初始化实例不 panic
// 对齐 Python _cancel_scheduler_running_tasks 的 try/except 降级语义
func TestDeepAdapter_CancelSchedulerRunningTasks_Nil安全(t *testing.T) {
	d := NewDeepAdapter()
	// instance 为 nil → 直接返回，不 panic
	d.cancelSchedulerRunningTasks()

	// AbortOnGatewayDisconnect 全流程也不 panic
	d.AbortOnGatewayDisconnect(t.Context())
}

// TestDeepAdapter_BindRuntimeCronContext_绑定语义 ctx 注入四元组
// 对齐 Python _bind_runtime_cron_context：channel 归一化默认 web、
// metadata 注入 request_id、mode 透传
func TestDeepAdapter_BindRuntimeCronContext_绑定语义(t *testing.T) {
	ctx := cronbridge.BindRuntimeCronContext(t.Context(),
		"  ", "sess-1", map[string]any{"k": "v"}, "req-9", "agent.work.plan")

	rcc := cronbridge.FromRuntimeCronContext(ctx)
	if rcc.ChannelID != "web" {
		t.Errorf("空 channel 应归一化 web，got %q", rcc.ChannelID)
	}
	if rcc.SessionID != "sess-1" {
		t.Errorf("session = %q", rcc.SessionID)
	}
	if rcc.Metadata["request_id"] != "req-9" {
		t.Errorf("metadata request_id 应注入: %v", rcc.Metadata["request_id"])
	}
	if rcc.Metadata["k"] != "v" {
		t.Errorf("原 metadata 应保留: %v", rcc.Metadata["k"])
	}
	if rcc.Mode != "agent.work.plan" {
		t.Errorf("mode = %q", rcc.Mode)
	}
}

// TestDeepAdapter_BindRuntimeCronContext_完整参数 非空 channel 原样传递
func TestDeepAdapter_BindRuntimeCronContext_完整参数(t *testing.T) {
	ctx := cronbridge.BindRuntimeCronContext(t.Context(),
		"feishu", "s", nil, "  ", "")

	rcc := cronbridge.FromRuntimeCronContext(ctx)
	if rcc.ChannelID != "feishu" {
		t.Errorf("channel = %q, want feishu", rcc.ChannelID)
	}
	// nil metadata → 空 map + 空 request_id 不注入
	if rcc.Metadata == nil || len(rcc.Metadata) != 0 {
		t.Errorf("nil metadata 应为空 map: %v", rcc.Metadata)
	}
	if rcc.Mode != "" {
		t.Errorf("空 mode 应保持空: %q", rcc.Mode)
	}
}
