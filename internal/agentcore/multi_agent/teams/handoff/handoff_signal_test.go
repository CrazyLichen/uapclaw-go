package handoff

import (
	"context"
	"testing"

	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mockSessionFacade 测试用 SessionFacade 模拟实现
type mockSessionFacade struct {
	// stateData 模拟状态数据
	stateData map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestExtractHandoffSignal_顶层有HandoffTargetKey 测试 result 顶层包含 __handoff_to__ 键
func TestExtractHandoffSignal_顶层有HandoffTargetKey(t *testing.T) {
	result := map[string]any{
		HandoffTargetKey:  "agent_b",
		HandoffMessageKey: "请继续处理",
		HandoffReasonKey:  "需要专家处理",
	}
	signal := ExtractHandoffSignal(result, nil)
	if signal == nil {
		t.Fatal("期望返回非 nil HandoffSignal，实际为 nil")
	}
	if signal.Target != "agent_b" {
		t.Errorf("Target: 期望 agent_b，实际 %s", signal.Target)
	}
	if signal.Message != "请继续处理" {
		t.Errorf("Message: 期望 请继续处理，实际 %s", signal.Message)
	}
	if signal.Reason != "需要专家处理" {
		t.Errorf("Reason: 期望 需要专家处理，实际 %s", signal.Reason)
	}
}

// TestExtractHandoffSignal_output子键中有HandoffTargetKey 测试 result["output"] 中包含 __handoff_to__ 键
func TestExtractHandoffSignal_output子键中有HandoffTargetKey(t *testing.T) {
	result := map[string]any{
		"output": map[string]any{
			HandoffTargetKey:  "agent_c",
			HandoffMessageKey: "代码审查完成",
			HandoffReasonKey:  "交接给审查Agent",
		},
	}
	signal := ExtractHandoffSignal(result, nil)
	if signal == nil {
		t.Fatal("期望返回非 nil HandoffSignal，实际为 nil")
	}
	if signal.Target != "agent_c" {
		t.Errorf("Target: 期望 agent_c，实际 %s", signal.Target)
	}
}

// TestExtractHandoffSignal_result子键中有HandoffTargetKey 测试 result["result"] 中包含 __handoff_to__ 键
func TestExtractHandoffSignal_result子键中有HandoffTargetKey(t *testing.T) {
	result := map[string]any{
		"result": map[string]any{
			HandoffTargetKey: "agent_d",
		},
	}
	signal := ExtractHandoffSignal(result, nil)
	if signal == nil {
		t.Fatal("期望返回非 nil HandoffSignal，实际为 nil")
	}
	if signal.Target != "agent_d" {
		t.Errorf("Target: 期望 agent_d，实际 %s", signal.Target)
	}
}

// TestExtractHandoffSignal_content子键中有HandoffTargetKey 测试 result["content"] 中包含 __handoff_to__ 键
func TestExtractHandoffSignal_content子键中有HandoffTargetKey(t *testing.T) {
	result := map[string]any{
		"content": map[string]any{
			HandoffTargetKey:  "agent_e",
			HandoffReasonKey:  "内容交接",
			HandoffMessageKey: "附加信息",
		},
	}
	signal := ExtractHandoffSignal(result, nil)
	if signal == nil {
		t.Fatal("期望返回非 nil HandoffSignal，实际为 nil")
	}
	if signal.Target != "agent_e" {
		t.Errorf("Target: 期望 agent_e，实际 %s", signal.Target)
	}
}

// TestExtractHandoffSignal_无信号返回nil 测试无信号时返回 nil（不传 agentSession）
func TestExtractHandoffSignal_无信号返回nil(t *testing.T) {
	result := map[string]any{
		"output": "普通输出",
		"status": "completed",
	}
	signal := ExtractHandoffSignal(result, nil)
	if signal != nil {
		t.Errorf("期望返回 nil，实际为 %+v", signal)
	}
}

// TestExtractHandoffSignal_result为nil 测试 result 为 nil 时返回 nil
func TestExtractHandoffSignal_result为nil(t *testing.T) {
	signal := ExtractHandoffSignal(nil, nil)
	if signal != nil {
		t.Errorf("期望返回 nil，实际为 %+v", signal)
	}
}

// TestExtractHandoffSignal_target非字符串 测试 target 为非字符串类型时返回 nil
func TestExtractHandoffSignal_target非字符串(t *testing.T) {
	result := map[string]any{
		HandoffTargetKey: 123,
	}
	signal := ExtractHandoffSignal(result, nil)
	if signal != nil {
		t.Errorf("期望返回 nil，实际为 %+v", signal)
	}
}

// TestExtractHandoffSignal_target为空字符串 测试 target 为空字符串时返回 nil
func TestExtractHandoffSignal_target为空字符串(t *testing.T) {
	result := map[string]any{
		HandoffTargetKey: "",
	}
	signal := ExtractHandoffSignal(result, nil)
	if signal != nil {
		t.Errorf("期望返回 nil，实际为 %+v", signal)
	}
}

// TestExtractHandoffSignal_message和reason缺失 测试 message 和 reason 缺失时返回空字符串
func TestExtractHandoffSignal_message和reason缺失(t *testing.T) {
	result := map[string]any{
		HandoffTargetKey: "agent_f",
	}
	signal := ExtractHandoffSignal(result, nil)
	if signal == nil {
		t.Fatal("期望返回非 nil HandoffSignal，实际为 nil")
	}
	if signal.Message != "" {
		t.Errorf("Message: 期望空字符串，实际 %s", signal.Message)
	}
	if signal.Reason != "" {
		t.Errorf("Reason: 期望空字符串，实际 %s", signal.Reason)
	}
}

// TestExtractHandoffSignal_从Session消息历史查找 测试从 agentSession 消息历史中查找 handoff payload
func TestExtractHandoffSignal_从Session消息历史查找(t *testing.T) {
	// 构造包含 tool 消息的 session
	sess := &mockSessionFacade{
		stateData: map[string]any{
			"context": map[string]any{
				defaultContextID: map[string]any{
					"messages": []any{
						map[string]any{
							"role":    "user",
							"content": "请开始处理",
						},
						map[string]any{
							"role":    "assistant",
							"content": "正在处理",
						},
						map[string]any{
							"role":    "tool",
							"content": `{"__handoff_to__": "agent_b", "__handoff_reason__": "需要专家", "__handoff_message__": "上下文信息"}`,
						},
					},
				},
			},
		},
	}

	// result 中无 handoff 信号，应从 session 查找
	result := map[string]any{"status": "ok"}
	signal := ExtractHandoffSignal(result, sess)
	if signal == nil {
		t.Fatal("期望从 session 消息历史中找到 HandoffSignal，实际为 nil")
	}
	if signal.Target != "agent_b" {
		t.Errorf("Target: 期望 agent_b，实际 %s", signal.Target)
	}
	if signal.Reason != "需要专家" {
		t.Errorf("Reason: 期望 需要专家，实际 %s", signal.Reason)
	}
	if signal.Message != "上下文信息" {
		t.Errorf("Message: 期望 上下文信息，实际 %s", signal.Message)
	}
}

// TestExtractHandoffSignal_Session中无Handoff信号 测试 session 消息历史中无 handoff 信号时返回 nil
func TestExtractHandoffSignal_Session中无Handoff信号(t *testing.T) {
	sess := &mockSessionFacade{
		stateData: map[string]any{
			"context": map[string]any{
				defaultContextID: map[string]any{
					"messages": []any{
						map[string]any{
							"role":    "tool",
							"content": `{"status": "ok"}`,
						},
					},
				},
			},
		},
	}

	result := map[string]any{"status": "ok"}
	signal := ExtractHandoffSignal(result, sess)
	if signal != nil {
		t.Errorf("期望返回 nil，实际为 %+v", signal)
	}
}

// TestFindHandoffPayload_顶层 测试 findHandoffPayload 从顶层找到 payload
func TestFindHandoffPayload_顶层(t *testing.T) {
	result := map[string]any{
		HandoffTargetKey: "agent_a",
	}
	payload := findHandoffPayload(result)
	if payload == nil {
		t.Fatal("期望返回非 nil payload，实际为 nil")
	}
	if payload[HandoffTargetKey] != "agent_a" {
		t.Errorf("HandoffTargetKey: 期望 agent_a，实际 %v", payload[HandoffTargetKey])
	}
}

// TestFindHandoffPayload_output子路径 测试 findHandoffPayload 从 output 子路径找到 payload
func TestFindHandoffPayload_output子路径(t *testing.T) {
	result := map[string]any{
		"output": map[string]any{
			HandoffTargetKey: "agent_b",
		},
	}
	payload := findHandoffPayload(result)
	if payload == nil {
		t.Fatal("期望返回非 nil payload，实际为 nil")
	}
	if payload[HandoffTargetKey] != "agent_b" {
		t.Errorf("HandoffTargetKey: 期望 agent_b，实际 %v", payload[HandoffTargetKey])
	}
}

// TestFindHandoffPayload_content子路径 测试 findHandoffPayload 从 content 子路径找到 payload
func TestFindHandoffPayload_content子路径(t *testing.T) {
	result := map[string]any{
		"content": map[string]any{
			HandoffTargetKey: "agent_c",
		},
	}
	payload := findHandoffPayload(result)
	if payload == nil {
		t.Fatal("期望返回非 nil payload，实际为 nil")
	}
	if payload[HandoffTargetKey] != "agent_c" {
		t.Errorf("HandoffTargetKey: 期望 agent_c，实际 %v", payload[HandoffTargetKey])
	}
}

// TestFindHandoffPayload_nil 测试 findHandoffPayload result 为 nil 时返回 nil
func TestFindHandoffPayload_nil(t *testing.T) {
	payload := findHandoffPayload(nil)
	if payload != nil {
		t.Errorf("期望返回 nil，实际为 %+v", payload)
	}
}

// TestFindHandoffPayload_无信号 测试 findHandoffPayload 无信号时返回 nil
func TestFindHandoffPayload_无信号(t *testing.T) {
	result := map[string]any{"output": "plain"}
	payload := findHandoffPayload(result)
	if payload != nil {
		t.Errorf("期望返回 nil，实际为 %+v", payload)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestFindHandoffFromSession_无Context状态 测试 agent session 无 context 状态时返回 nil
func TestFindHandoffFromSession_无Context状态(t *testing.T) {
	sess := &mockSessionFacade{
		stateData: map[string]any{},
	}
	result := findHandoffFromSession(sess)
	if result != nil {
		t.Errorf("期望返回 nil，实际为 %+v", result)
	}
}

// TestFindHandoffFromSession_Context状态非map 测试 context 状态不是 map 类型时返回 nil
func TestFindHandoffFromSession_Context状态非map(t *testing.T) {
	sess := &mockSessionFacade{
		stateData: map[string]any{
			"context": "not_a_map",
		},
	}
	result := findHandoffFromSession(sess)
	if result != nil {
		t.Errorf("期望返回 nil，实际为 %+v", result)
	}
}

// TestFindHandoffFromSession_无默认上下文 测试无 defaultContextID 时返回 nil
func TestFindHandoffFromSession_无默认上下文(t *testing.T) {
	sess := &mockSessionFacade{
		stateData: map[string]any{
			"context": map[string]any{
				"other_context": map[string]any{},
			},
		},
	}
	result := findHandoffFromSession(sess)
	if result != nil {
		t.Errorf("期望返回 nil，实际为 %+v", result)
	}
}

// TestFindHandoffFromSession_默认上下文非map 测试 defaultContextID 对应值不是 map 时返回 nil
func TestFindHandoffFromSession_默认上下文非map(t *testing.T) {
	sess := &mockSessionFacade{
		stateData: map[string]any{
			"context": map[string]any{
				defaultContextID: "not_a_map",
			},
		},
	}
	result := findHandoffFromSession(sess)
	if result != nil {
		t.Errorf("期望返回 nil，实际为 %+v", result)
	}
}

// TestFindHandoffFromSession_无消息列表 测试无 messages 时返回 nil
func TestFindHandoffFromSession_无消息列表(t *testing.T) {
	sess := &mockSessionFacade{
		stateData: map[string]any{
			"context": map[string]any{
				defaultContextID: map[string]any{},
			},
		},
	}
	result := findHandoffFromSession(sess)
	if result != nil {
		t.Errorf("期望返回 nil，实际为 %+v", result)
	}
}

// TestFindHandoffFromSession_消息列表类型错误 测试 messages 不是 []any 类型时返回 nil
func TestFindHandoffFromSession_消息列表类型错误(t *testing.T) {
	sess := &mockSessionFacade{
		stateData: map[string]any{
			"context": map[string]any{
				defaultContextID: map[string]any{
					"messages": "not_a_slice",
				},
			},
		},
	}
	result := findHandoffFromSession(sess)
	if result != nil {
		t.Errorf("期望返回 nil，实际为 %+v", result)
	}
}

// TestFindHandoffFromSession_tool消息Content非字符串 测试 tool 消息 content 为非字符串时跳过
func TestFindHandoffFromSession_tool消息Content非字符串(t *testing.T) {
	sess := &mockSessionFacade{
		stateData: map[string]any{
			"context": map[string]any{
				defaultContextID: map[string]any{
					"messages": []any{
						map[string]any{
							"role":    "tool",
							"content": 123, // 非字符串
						},
					},
				},
			},
		},
	}
	result := findHandoffFromSession(sess)
	if result != nil {
		t.Errorf("期望返回 nil，实际为 %+v", result)
	}
}

// TestFindHandoffFromSession_tool消息Content为空 测试 tool 消息 content 为空字符串时跳过
func TestFindHandoffFromSession_tool消息Content为空(t *testing.T) {
	sess := &mockSessionFacade{
		stateData: map[string]any{
			"context": map[string]any{
				defaultContextID: map[string]any{
					"messages": []any{
						map[string]any{
							"role":    "tool",
							"content": "",
						},
					},
				},
			},
		},
	}
	result := findHandoffFromSession(sess)
	if result != nil {
		t.Errorf("期望返回 nil，实际为 %+v", result)
	}
}

// TestFindHandoffFromSession_非tool消息跳过 测试 role 非 tool 的消息被跳过
func TestFindHandoffFromSession_非tool消息跳过(t *testing.T) {
	sess := &mockSessionFacade{
		stateData: map[string]any{
			"context": map[string]any{
				defaultContextID: map[string]any{
					"messages": []any{
						map[string]any{
							"role":    "assistant",
							"content": `{"__handoff_to__": "agent_b"}`,
						},
					},
				},
			},
		},
	}
	result := findHandoffFromSession(sess)
	if result != nil {
		t.Errorf("期望返回 nil（assistant 消息应被跳过），实际为 %+v", result)
	}
}

// TestFindHandoffFromSession_tool消息非map类型跳过 测试消息非 map 类型时跳过
func TestFindHandoffFromSession_tool消息非map类型跳过(t *testing.T) {
	sess := &mockSessionFacade{
		stateData: map[string]any{
			"context": map[string]any{
				defaultContextID: map[string]any{
					"messages": []any{
						"plain_string_message", // 非 map 类型
					},
				},
			},
		},
	}
	result := findHandoffFromSession(sess)
	if result != nil {
		t.Errorf("期望返回 nil，实际为 %+v", result)
	}
}

// TestFindHandoffFromSession_JSON解析失败 测试 tool 消息 content 不是合法 JSON 时跳过
func TestFindHandoffFromSession_JSON解析失败(t *testing.T) {
	sess := &mockSessionFacade{
		stateData: map[string]any{
			"context": map[string]any{
				defaultContextID: map[string]any{
					"messages": []any{
						map[string]any{
							"role":    "tool",
							"content": "not_json",
						},
					},
				},
			},
		},
	}
	result := findHandoffFromSession(sess)
	if result != nil {
		t.Errorf("期望返回 nil（JSON 解析失败），实际为 %+v", result)
	}
}

// mockSessionFacade 实现 SessionFacade 接口的方法

func (m *mockSessionFacade) GetSessionID() string            { return "mock-session" }
func (m *mockSessionFacade) UpdateState(data map[string]any) {}
func (m *mockSessionFacade) GetState(key state.StateKey) (any, error) {
	return m.stateData[key.String()], nil
}
func (m *mockSessionFacade) DumpState() map[string]any                        { return m.stateData }
func (m *mockSessionFacade) WriteStream(_ context.Context, _ any) error       { return nil }
func (m *mockSessionFacade) WriteCustomStream(_ context.Context, _ any) error { return nil }
func (m *mockSessionFacade) GetEnv(key string, defaultValue ...any) any       { return nil }
func (m *mockSessionFacade) Interact(_ context.Context, _ any) error          { return nil }

// 编译时验证 mockSessionFacade 满足 SessionFacade 接口
var _ sessioninterfaces.SessionFacade = (*mockSessionFacade)(nil)
