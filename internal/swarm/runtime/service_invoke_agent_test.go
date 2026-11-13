package runtime

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// chatSendRequest 构造 chat.send 请求。
func declarativeChatRequest(t *testing.T, channelID, sessionID, mode string) *schema.AgentRequest {
	t.Helper()
	req := &schema.AgentRequest{
		ChannelID: channelID,
		ReqMethod: schema.ReqMethodChatSend,
		RequestID: "req-1",
		Timestamp: 1.0,
	}
	if sessionID != "" {
		sid := sessionID
		req.SessionID = &sid
	}
	params := map[string]any{"query": "hi"}
	if mode != "" {
		params["mode"] = mode
	}
	raw, err := json.Marshal(params)
	require.NoError(t, err)
	req.Params = raw
	return req
}

// stubAdmissionSession 让准入阶段的会话目录查询返回一个存在的会话。
// 生产实现是 GetSession（读真实会话目录）；这里只替换查询，不替换准入逻辑。
func stubAdmissionSession(t *testing.T) {
	t.Helper()
	original := getSessionForAdmission
	t.Cleanup(func() { getSessionForAdmission = original })
	getSessionForAdmission = func(input SessionGetInput) (*SessionSummary, error) {
		return &SessionSummary{
			SessionID: input.SessionID,
			ChannelID: normalizedChannelOrDefault(input.ChannelID),
			Mode:      RuntimeAgentModeWorkNormal,
		}, nil
	}
}

// stubAdmissionMissing 让准入阶段的会话目录查询返回不存在。
func stubAdmissionMissing(t *testing.T) {
	t.Helper()
	original := getSessionForAdmission
	t.Cleanup(func() { getSessionForAdmission = original })
	getSessionForAdmission = func(SessionGetInput) (*SessionSummary, error) {
		return nil, nil
	}
}

// declarativeDefinition 构造合法声明式定义。
func declarativeDefinition(instructions string) RuntimeAgentDefinition {
	return RuntimeAgentDefinition{
		Name: "it-agent", Instructions: instructions, ToolsWildcard: true,
	}
}

// preparedRuntime 构造注入了 Prepare 替身的 Runtime，执行时返回给定 agent。
func preparedRuntime(t *testing.T, agent ChatAgentPort) *AgentRuntime {
	t.Helper()
	return NewAgentRuntime(AgentRuntimeDeps{
		Prepare: func(context.Context, *schema.AgentRequest, string, map[string]any, string) (string, string, ChatAgentPort, error) {
			return "agent", "", agent, nil
		},
	})
}

// ──────────────────────────── InvokeAgent 测试分组 ────────────────────────────

// TestInvokeAgent_写入owner表 执行成功后该会话进入 owner 表。
// Python: service.py:1657 _claim_agent_execution_owner
func TestInvokeAgent_写入owner表(t *testing.T) {
	stubAdmissionSession(t)
	agent := &fakePlanAgent{}
	runtime := preparedRuntime(t, agent)
	req := declarativeChatRequest(t, "web", "s1", "agent.work.normal")

	_, err := runtime.InvokeAgent(context.Background(), req, declarativeDefinition("v1"))
	require.NoError(t, err)
	require.True(t, runtime.hasAgentExecutionOwner("web", "s1"))
}

// TestInvokeAgent_换定义冲突 同会话第二次换定义报 SESSION_CONFLICT。
// Python: service.py:1657 + :1609-1615
func TestInvokeAgent_换定义冲突(t *testing.T) {
	stubAdmissionSession(t)
	agent := &fakePlanAgent{}
	runtime := preparedRuntime(t, agent)

	_, err := runtime.InvokeAgent(context.Background(),
		declarativeChatRequest(t, "web", "s1", "agent.work.normal"), declarativeDefinition("v1"))
	require.NoError(t, err)

	_, err = runtime.InvokeAgent(context.Background(),
		declarativeChatRequest(t, "web", "s1", "agent.work.normal"), declarativeDefinition("v2"))
	require.ErrorIs(t, err, ErrAgentExecutionSessionConflict)
}

// TestInvokeAgent_同定义幂等 相同指纹重复调用不冲突。
// Python: service.py:1616-1617
func TestInvokeAgent_同定义幂等(t *testing.T) {
	stubAdmissionSession(t)
	agent := &fakePlanAgent{}
	runtime := preparedRuntime(t, agent)

	for round := 0; round < 2; round++ {
		_, err := runtime.InvokeAgent(context.Background(),
			declarativeChatRequest(t, "web", "s1", "agent.work.normal"), declarativeDefinition("v1"))
		require.NoError(t, err, "round=%d", round)
	}
}

// TestInvokeAgent_非法模式被拒 模式不合法在执行前报错。
// Python: service.py:1654 resolve_mode_capability
func TestInvokeAgent_非法模式被拒(t *testing.T) {
	stubAdmissionSession(t)
	runtime := preparedRuntime(t, &fakePlanAgent{})
	_, err := runtime.InvokeAgent(context.Background(),
		declarativeChatRequest(t, "web", "s1", "bogus.mode"), declarativeDefinition("v1"))
	require.Error(t, err, "非法模式必须被拒")
}

// TestInvokeAgent_非chatSend被拒 bind 阶段拒绝非 chat.send。
// Python: service.py:1556-1562
func TestInvokeAgent_非chatSend被拒(t *testing.T) {
	stubAdmissionSession(t)
	runtime := preparedRuntime(t, &fakePlanAgent{})
	req := declarativeChatRequest(t, "web", "s1", "agent.work.normal")
	req.ReqMethod = schema.ReqMethodCommandGoal

	_, err := runtime.InvokeAgent(context.Background(), req, declarativeDefinition("v1"))
	require.ErrorIs(t, err, ErrAgentDefinitionInvalidRequest)
	require.Equal(t, "request", AgentDefinitionErrorField(err))
}

// TestInvokeAgent_模型不存在被拒 配置的 model 解析失败即拒绝。
// Python: service.py:707-714
func TestInvokeAgent_模型不存在被拒(t *testing.T) {
	stubAdmissionSession(t)
	runtime := NewAgentRuntime(AgentRuntimeDeps{
		Prepare: func(context.Context, *schema.AgentRequest, string, map[string]any, string) (string, string, ChatAgentPort, error) {
			return "agent", "", &fakePlanAgent{}, nil
		},
		ResolveModelCapability: func(string) (RuntimeModelDescriptor, error) {
			return RuntimeModelDescriptor{}, ErrModelNotFound
		},
	})
	model := "ghost-model"
	definition := RuntimeAgentDefinition{
		Name: "it-agent", Instructions: "x", ToolsWildcard: true, Model: &model,
	}
	_, err := runtime.InvokeAgent(context.Background(),
		declarativeChatRequest(t, "web", "s1", "agent.work.normal"), definition)
	require.ErrorIs(t, err, ErrAgentDefinitionModelNotFound)
	require.Equal(t, "model", AgentDefinitionErrorField(err))
}

// TestInvokeAgent_会话不存在被拒 目录读不到且协调器无快照 → NOT_FOUND。
// Python: service.py:1649 _require_owned_single_agent_session
func TestInvokeAgent_会话不存在被拒(t *testing.T) {
	stubAdmissionMissing(t)
	runtime := preparedRuntime(t, &fakePlanAgent{})
	_, err := runtime.InvokeAgent(context.Background(),
		declarativeChatRequest(t, "web", "definitely-not-exist-20-1", "agent.work.normal"),
		declarativeDefinition("v1"))
	require.ErrorIs(t, err, ErrSessionNotFound)
}

// TestInvokeAgent_无会话放行 无 session_id 时准入直接通过。
// Python: service.py:1589-1590 if not session_id: return
func TestInvokeAgent_无会话放行(t *testing.T) {
	runtime := preparedRuntime(t, &fakePlanAgent{})
	_, err := runtime.InvokeAgent(context.Background(),
		declarativeChatRequest(t, "web", "", "agent.work.normal"), declarativeDefinition("v1"))
	require.NoError(t, err)
	require.Empty(t, runtime.agentExecutionOwners, "无 session 不建 owner")
}

// ──────────────────────────── StreamAgent 测试分组 ────────────────────────────

// TestStreamAgent_写入owner表 流式执行同样写 owner 表。
// Python: service.py:1681
func TestStreamAgent_写入owner表(t *testing.T) {
	stubAdmissionSession(t)
	runtime := preparedRuntime(t, &fakePlanAgent{})
	req := declarativeChatRequest(t, "web", "s2", "agent.work.normal")

	err := runtime.StreamAgent(context.Background(), req,
		declarativeDefinition("v1"), func(*RuntimeEvent) bool { return true })
	require.NoError(t, err)
	require.True(t, runtime.hasAgentExecutionOwner("web", "s2"))
}

// TestStreamAgent_换定义冲突 流式路径同样受 SESSION_CONFLICT 约束。
// Python: service.py:1681 + :1609-1615
func TestStreamAgent_换定义冲突(t *testing.T) {
	stubAdmissionSession(t)
	runtime := preparedRuntime(t, &fakePlanAgent{})
	noop := func(*RuntimeEvent) bool { return true }

	require.NoError(t, runtime.StreamAgent(context.Background(),
		declarativeChatRequest(t, "web", "s3", "agent.work.normal"), declarativeDefinition("v1"), noop))

	err := runtime.StreamAgent(context.Background(),
		declarativeChatRequest(t, "web", "s3", "agent.work.normal"), declarativeDefinition("v2"), noop)
	require.ErrorIs(t, err, ErrAgentExecutionSessionConflict)
}

// ──────────────────────────── 三个读端 ────────────────────────────

// TestAnswerInteraction_无owner不报错 读端找不到 owner 是正常态。
// Python: service.py:1995-1997 if owner: invoke_kwargs[...]
func TestAnswerInteraction_无owner不报错(t *testing.T) {
	runtime := preparedRuntime(t, &fakePlanAgent{})
	req := declarativeChatRequest(t, "web", "never-owned", "agent.work.normal")
	req.ReqMethod = schema.ReqMethodChatAnswer

	_, err := runtime.AnswerInteraction(context.Background(), req)
	require.NoError(t, err)
}

// TestAnswerInteraction_有owner透传 绑定后读端取到同一执行体。
// Python: service.py:1995 owner = self._agent_execution_owner(request)
func TestAnswerInteraction_有owner透传(t *testing.T) {
	runtime := preparedRuntime(t, &fakePlanAgent{})
	execution := ownerExecution(t, "v1")
	require.NoError(t, runtime.claimAgentExecutionOwner(
		ownerRequest("web", "s1"), execution))

	req := declarativeChatRequest(t, "web", "s1", "agent.work.normal")
	req.ReqMethod = schema.ReqMethodChatAnswer
	got := runtime.agentExecutionOwner(req)
	require.NotNil(t, got)
	require.Equal(t, execution.Mode, got.Mode)
}

// TestAnswerInteraction_非CHAT_ANSWER被拒 方法必须是 chat.answer。
// Python: service.py:1990-1992
func TestAnswerInteraction_非CHAT_ANSWER被拒(t *testing.T) {
	runtime := preparedRuntime(t, &fakePlanAgent{})
	req := declarativeChatRequest(t, "web", "s1", "agent.work.normal")

	_, err := runtime.AnswerInteraction(context.Background(), req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "CHAT_ANSWER")
}

// TestAnswerInteraction_nil请求被拒 nil 请求安全拒绝。
func TestAnswerInteraction_nil请求被拒(t *testing.T) {
	runtime := preparedRuntime(t, &fakePlanAgent{})
	_, err := runtime.AnswerInteraction(context.Background(), nil)
	require.Error(t, err)
}

// TestStreamInteractionAnswer_无owner不报错 流式读端同样容忍无 owner。
// Python: service.py:2083
func TestStreamInteractionAnswer_无owner不报错(t *testing.T) {
	runtime := preparedRuntime(t, &fakePlanAgent{})
	req := declarativeChatRequest(t, "web", "none", "agent.work.normal")
	req.ReqMethod = schema.ReqMethodChatAnswer

	err := runtime.StreamInteractionAnswer(context.Background(), req,
		func(*RuntimeEvent) bool { return true })
	require.NoError(t, err)
}

// ──────────────────────────── 绑定语义 ────────────────────────────

// TestBindAgentExecutionRequest_写mode与workMode code 前缀决定 work_mode。
// Python: service.py:1571-1576
func TestBindAgentExecutionRequest_写mode与workMode(t *testing.T) {
	runtime := NewAgentRuntime(AgentRuntimeDeps{})

	cases := map[string]string{
		"agent.code.plan":   "code",
		"agent.code.normal": "code",
		"agent.work.plan":   "work",
		"agent.work.normal": "work",
	}
	for mode, wantWorkMode := range cases {
		req := declarativeChatRequest(t, "web", "s1", mode)
		execution, err := PrepareAgentExecution(declarativeDefinition("v1"), mode)
		require.NoError(t, err)

		bound, err := runtime.bindAgentExecutionRequest(req, execution)
		require.NoError(t, err)
		params := parseRequestParams(bound)
		require.Equal(t, mode, params["mode"])
		require.Equal(t, wantWorkMode, params["work_mode"], "mode=%s", mode)
	}
}

// TestBindAgentExecutionRequest_params非对象被拒 非对象 params 报错。
// Python: service.py:1563-1568
func TestBindAgentExecutionRequest_params非对象被拒(t *testing.T) {
	runtime := NewAgentRuntime(AgentRuntimeDeps{})
	req := &schema.AgentRequest{
		ChannelID: "web", ReqMethod: schema.ReqMethodChatSend,
		Params: json.RawMessage(`[1,2,3]`),
	}
	execution, err := PrepareAgentExecution(declarativeDefinition("v1"), "agent.work.normal")
	require.NoError(t, err)

	_, err = runtime.bindAgentExecutionRequest(req, execution)
	require.ErrorIs(t, err, ErrAgentDefinitionInvalidRequest)
	require.Equal(t, "request.params", AgentDefinitionErrorField(err))
}

// TestBindAgentExecutionRequest_补齐modelName 定义带模型且请求未给时补 selection_key。
// Python: service.py:1577-1581
func TestBindAgentExecutionRequest_补齐modelName(t *testing.T) {
	runtime := NewAgentRuntime(AgentRuntimeDeps{
		ResolveModelCapability: func(model string) (RuntimeModelDescriptor, error) {
			return RuntimeModelDescriptor{SelectionKey: model + "#0"}, nil
		},
	})
	model := "m1"
	definition := declarativeDefinition("v1")
	definition.Model = &model
	execution, err := PrepareAgentExecution(definition, "agent.work.normal")
	require.NoError(t, err)

	req := declarativeChatRequest(t, "web", "s1", "agent.work.normal")
	bound, err := runtime.bindAgentExecutionRequest(req, execution)
	require.NoError(t, err)
	require.Equal(t, "m1#0", parseRequestParams(bound)["model_name"])
}

// TestBindAgentExecutionRequest_不覆盖显式model 请求已给 model_name 时不覆盖。
// Python: service.py:1578 if execution.definition.model and not params.get("model_name")
func TestBindAgentExecutionRequest_不覆盖显式model(t *testing.T) {
	runtime := NewAgentRuntime(AgentRuntimeDeps{
		ResolveModelCapability: func(model string) (RuntimeModelDescriptor, error) {
			return RuntimeModelDescriptor{SelectionKey: model + "#0"}, nil
		},
	})
	model := "m1"
	definition := declarativeDefinition("v1")
	definition.Model = &model
	execution, err := PrepareAgentExecution(definition, "agent.work.normal")
	require.NoError(t, err)

	req := declarativeChatRequest(t, "web", "s1", "agent.work.normal")
	params := parseRequestParams(req)
	params["model_name"] = "explicit#9"
	writeRequestParams(req, params)

	bound, err := runtime.bindAgentExecutionRequest(req, execution)
	require.NoError(t, err)
	require.Equal(t, "explicit#9", parseRequestParams(bound)["model_name"])
}

// ──────────────────────────── 准入辅助 ────────────────────────────

// TestRequestSessionIDValue_去空白 取会话标识并去空白。
func TestRequestSessionIDValue_去空白(t *testing.T) {
	require.Equal(t, "", requestSessionIDValue(nil))
	require.Equal(t, "", requestSessionIDValue(&schema.AgentRequest{}))
	require.Equal(t, "s1", requestSessionIDValue(ownerRequest("web", "  s1  ")))
}

// TestNormalizedChannelOrDefault_归一化 空白回 default、小写化。
// Python: str(x or "default").strip().lower() or "default"
func TestNormalizedChannelOrDefault_归一化(t *testing.T) {
	require.Equal(t, "default", normalizedChannelOrDefault(""))
	require.Equal(t, "default", normalizedChannelOrDefault("   "))
	require.Equal(t, "web", normalizedChannelOrDefault("  WEB  "))
}
