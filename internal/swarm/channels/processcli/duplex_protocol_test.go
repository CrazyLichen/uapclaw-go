package processcli

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/swarm/channels/processcli/protocol"
)

// TestDecodeControl_answer合法帧 合法 answer 帧全字段解码。
// Python: duplex_protocol.py:218-257 decode_control
func TestDecodeControl_answer合法帧(t *testing.T) {
	line := []byte(`{"schema_version":"` + protocol.CurrentSchemaVersion +
		`","type":"answer","request_id":"r1","session_id":"s1",` +
		`"interaction_id":"i1","answers":[{"selected_options":["approve"],"custom_input":""}]}`)
	ctrl, err := DecodeControl(line)
	require.NoError(t, err)
	require.Equal(t, "answer", ctrl.Kind)
	require.Equal(t, "r1", ctrl.RequestID)
	require.NotNil(t, ctrl.SessionID)
	require.Equal(t, "s1", *ctrl.SessionID)
	require.NotNil(t, ctrl.InteractionID)
	require.Equal(t, "i1", *ctrl.InteractionID)
	require.Len(t, ctrl.Answers, 1)
}

// TestDecodeControl_cancel合法帧 cancel 帧最小字段；不应携带 answer/tool 字段。
func TestDecodeControl_cancel合法帧(t *testing.T) {
	line := []byte(`{"schema_version":"` + protocol.CurrentSchemaVersion +
		`","type":"cancel","request_id":"r2"}`)
	ctrl, err := DecodeControl(line)
	require.NoError(t, err)
	require.Equal(t, "cancel", ctrl.Kind)
	require.Nil(t, ctrl.InteractionID)
	require.Empty(t, ctrl.Answers)
	require.Nil(t, ctrl.CallID)
}

// TestDecodeControl_toolResult二选一 result 与 error 必须恰好其一。
// Python: duplex_protocol.py:200-204
func TestDecodeControl_toolResult二选一(t *testing.T) {
	base := `"schema_version":"` + protocol.CurrentSchemaVersion +
		`","type":"tool_result","request_id":"r3","session_id":"s3","call_id":"c1"`

	// result+error 同时存在 → 拒
	_, err := DecodeControl([]byte(`{` + base + `,"result":1,"error":"x"}`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "exactly one result or error")

	// 两者都缺 → 拒
	_, err = DecodeControl([]byte(`{` + base + `}`))
	require.Error(t, err)

	// 只有 error → 过
	ctrl, err := DecodeControl([]byte(`{` + base + `,"error":"boom"}`))
	require.NoError(t, err)
	require.NotNil(t, ctrl.Error)
	require.Equal(t, "boom", *ctrl.Error)

	// 只有 result → 过
	ctrl, err = DecodeControl([]byte(`{` + base + `,"result":42}`))
	require.NoError(t, err)
	require.Equal(t, float64(42), ctrl.Result)
}

// TestDecodeControl_未知字段拒绝 白名单外的字段一律拒。
// Python: duplex_protocol.py:207-211
func TestDecodeControl_未知字段拒绝(t *testing.T) {
	line := []byte(`{"schema_version":"` + protocol.CurrentSchemaVersion +
		`","type":"cancel","request_id":"r4","evil":1}`)
	_, err := DecodeControl(line)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown fields")
}

// TestDecodeControl_缺失必填字段 必填字段缺失一律拒。
// Python: duplex_protocol.py:210-211
func TestDecodeControl_缺失必填字段(t *testing.T) {
	line := []byte(`{"schema_version":"` + protocol.CurrentSchemaVersion +
		`","type":"cancel"}`)
	_, err := DecodeControl(line)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing required fields")
}

// TestDecodeControl_版本不支持 schema_version 非当前版本拒绝。
// Python: duplex_protocol.py:212-215
func TestDecodeControl_版本不支持(t *testing.T) {
	line := []byte(`{"schema_version":"0.0.1","type":"cancel","request_id":"r5"}`)
	_, err := DecodeControl(line)
	require.Error(t, err)
	require.Contains(t, err.Error(), "schema_version is not supported")
}

// TestDecodeControl_类型非法 type 不在三类之内拒绝。
// Python: duplex_protocol.py:206
func TestDecodeControl_类型非法(t *testing.T) {
	line := []byte(`{"schema_version":"` + protocol.CurrentSchemaVersion +
		`","type":"bogus","request_id":"r6"}`)
	_, err := DecodeControl(line)
	require.Error(t, err)
	require.Contains(t, err.Error(), "answer, tool_result or cancel")
}

// TestDecodeControl_answer字段类型校验 selected_options/card_id/question/custom_input。
// Python: duplex_protocol.py:76-91
func TestDecodeControl_answer字段类型校验(t *testing.T) {
	ver := protocol.CurrentSchemaVersion
	cases := []struct {
		name string
		line string
	}{
		{"selected_options非数组", `{"schema_version":"` + ver + `","type":"answer","request_id":"r","session_id":"s","interaction_id":"i","answers":[{"selected_options":"nope"}]}`},
		{"selected_options含非字符串", `{"schema_version":"` + ver + `","type":"answer","request_id":"r","session_id":"s","interaction_id":"i","answers":[{"selected_options":[1]}]}`},
		{"card_id为空", `{"schema_version":"` + ver + `","type":"answer","request_id":"r","session_id":"s","interaction_id":"i","answers":[{"card_id":"  "}]}`},
		{"question非字符串", `{"schema_version":"` + ver + `","type":"answer","request_id":"r","session_id":"s","interaction_id":"i","answers":[{"question":7}]}`},
		{"custom_input非字符串", `{"schema_version":"` + ver + `","type":"answer","request_id":"r","session_id":"s","interaction_id":"i","answers":[{"custom_input":true}]}`},
		{"answers为空数组", `{"schema_version":"` + ver + `","type":"answer","request_id":"r","session_id":"s","interaction_id":"i","answers":[]}`},
		{"answers非对象元素", `{"schema_version":"` + ver + `","type":"answer","request_id":"r","session_id":"s","interaction_id":"i","answers":[1]}`},
		{"answers非JSON值", `{"schema_version":"` + ver + `","type":"answer","request_id":"r","session_id":"s","interaction_id":"i","answers":[{"extra":null}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "answers非JSON值" {
				// null 是合法 JSON 值，Python _copy_json 允许 None——此用例应通过
				_, err := DecodeControl([]byte(tc.line))
				require.NoError(t, err)
				return
			}
			_, err := DecodeControl([]byte(tc.line))
			require.Error(t, err, tc.name)
		})
	}
}

// TestDecodeControl_深拷贝防御 修改 ToDict 结果不得影响已解码对象。
// Python: duplex_protocol.py:169-187 to_dict
func TestDecodeControl_深拷贝防御(t *testing.T) {
	line := []byte(`{"schema_version":"` + protocol.CurrentSchemaVersion +
		`","type":"answer","request_id":"r","session_id":"s","interaction_id":"i",` +
		`"answers":[{"selected_options":["a"]}]}`)
	ctrl, err := DecodeControl(line)
	require.NoError(t, err)
	dict := ctrl.ToDict()
	dict["answers"].([]any)[0].(map[string]any)["selected_options"].([]any)[0] = "mutated"
	require.Equal(t, "a", ctrl.Answers[0]["selected_options"].([]any)[0])

	// ToDict 两次返回互不影响
	dict2 := ctrl.ToDict()
	require.Equal(t, "a", dict2["answers"].([]any)[0].(map[string]any)["selected_options"].([]any)[0])
}

// TestDecodeControl_非UTF8 非法 UTF-8 拒绝。
// Python: duplex_input 层面在 Go 由 decodeStrict 的 JSON 校验兜住；
// 顶层非法字节直接报错。
func TestDecodeControl_非UTF8(t *testing.T) {
	_, err := DecodeControl([]byte{0xff, 0xfe})
	require.Error(t, err)
}

// TestDecodeControl_requestId关联捞出 损坏帧仍带 request_id 诊断。
// Python: duplex_protocol.py:232-235
func TestDecodeControl_requestId关联捞出(t *testing.T) {
	line := []byte(`{"request_id":"kept","type":"bogus"}`)
	_, err := DecodeControl(line)
	dpe, ok := err.(*DuplexProtocolError)
	require.True(t, ok, "应为 *DuplexProtocolError, got %T", err)
	require.NotNil(t, dpe.RequestID)
	require.Equal(t, "kept", *dpe.RequestID)
}

// TestDecodeControl_空行 空行由 DecodeMachineDocument 拒绝且消息做 run→control 替换。
// Python: duplex_protocol.py:229-231 message.replace("run input", "control input", 1)
func TestDecodeControl_空行(t *testing.T) {
	_, err := DecodeControl([]byte("\n"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "control input must not be empty")
}

// TestDuplexControl_构造校验_answer缺session 直接构造时按 kind 校验（Python __post_init__）。
func TestDuplexControl_构造校验(t *testing.T) {
	sid, iid := "s", "i"
	// answer 缺 session_id → 拒
	_, err := NewDuplexControl("answer", "r", nil, &iid, []map[string]any{{"a": 1}}, nil, nil, nil)
	require.Error(t, err)
	// cancel 携带 answer 字段 → 拒
	_, err = NewDuplexControl("cancel", "r", &sid, &iid, nil, nil, nil, nil)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "cancel must not contain"))
	// tool_result 携带 answer 字段 → 拒
	_, err = NewDuplexControl("tool_result", "r", &sid, &iid, nil, ptrString("c"), nil, nil)
	require.Error(t, err)
	// kind 非法 → 拒
	_, err = NewDuplexControl("bogus", "r", nil, nil, nil, nil, nil, nil)
	require.Error(t, err)
}

// ptrString 测试助手。
func ptrString(s string) *string { return &s }
