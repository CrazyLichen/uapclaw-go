package web

import (
	"encoding/json"
	"testing"
)

// ──────────────────────────── ReqFrame 测试 ────────────────────────────

// TestDecodeReqFrame_正常解码 测试正常 JSON 解码 ReqFrame
func TestDecodeReqFrame_正常解码(t *testing.T) {
	raw := `{"type":"req","id":"abc123","method":"chat.send","params":{"message":"hello"}}`
	f, err := DecodeReqFrame([]byte(raw))
	if err != nil {
		t.Fatalf("DecodeReqFrame 返回错误: %v", err)
	}
	if f.Type != "req" {
		t.Errorf("Type = %q, 期望 %q", f.Type, "req")
	}
	if f.ID != "abc123" {
		t.Errorf("ID = %q, 期望 %q", f.ID, "abc123")
	}
	if f.Method != "chat.send" {
		t.Errorf("Method = %q, 期望 %q", f.Method, "chat.send")
	}
	// 验证 Params 原始 JSON 保留
	if string(f.Params) != `{"message":"hello"}` {
		t.Errorf("Params = %q, 期望原始 JSON", string(f.Params))
	}
}

// TestDecodeReqFrame_空数据 测试空数据返回错误
func TestDecodeReqFrame_空数据(t *testing.T) {
	_, err := DecodeReqFrame(nil)
	if err == nil {
		t.Fatal("空数据应返回错误")
	}
	_, err = DecodeReqFrame([]byte{})
	if err == nil {
		t.Fatal("空字节数组应返回错误")
	}
}

// TestDecodeReqFrame_非法JSON 测试非法 JSON 返回错误
func TestDecodeReqFrame_非法JSON(t *testing.T) {
	_, err := DecodeReqFrame([]byte("not json"))
	if err == nil {
		t.Fatal("非法 JSON 应返回错误")
	}
}

// TestReqFrame_Validate_合法 测试合法 ReqFrame 通过校验
func TestReqFrame_Validate_合法(t *testing.T) {
	f := &ReqFrame{Type: "req", ID: "id1", Method: "chat.send"}
	if err := f.Validate(); err != nil {
		t.Fatalf("合法 ReqFrame 校验失败: %v", err)
	}
}

// TestReqFrame_Validate_类型错误 测试 type 非 "req" 时校验失败
func TestReqFrame_Validate_类型错误(t *testing.T) {
	f := &ReqFrame{Type: "res", ID: "id1", Method: "chat.send"}
	if err := f.Validate(); err == nil {
		t.Fatal("type=res 应校验失败")
	}
}

// TestReqFrame_Validate_ID为空 测试 ID 为空时校验失败
func TestReqFrame_Validate_ID为空(t *testing.T) {
	f := &ReqFrame{Type: "req", ID: "", Method: "chat.send"}
	if err := f.Validate(); err == nil {
		t.Fatal("ID 为空应校验失败")
	}
}

// TestReqFrame_Validate_Method为空 测试 Method 为空时校验失败
func TestReqFrame_Validate_Method为空(t *testing.T) {
	f := &ReqFrame{Type: "req", ID: "id1", Method: ""}
	if err := f.Validate(); err == nil {
		t.Fatal("Method 为空应校验失败")
	}
}

// TestReqFrame_Encode 测试 ReqFrame 编码
func TestReqFrame_Encode(t *testing.T) {
	f := &ReqFrame{
		Type:   "req",
		ID:     "id1",
		Method: "chat.send",
		Params: json.RawMessage(`{"key":"val"}`),
	}
	data, err := f.Encode()
	if err != nil {
		t.Fatalf("Encode 返回错误: %v", err)
	}
	// 解码回来验证
	var f2 ReqFrame
	if err := json.Unmarshal(data, &f2); err != nil {
		t.Fatalf("回解码失败: %v", err)
	}
	if f2.Type != f.Type || f2.ID != f.ID || f2.Method != f.Method {
		t.Errorf("回解码字段不匹配: %+v", f2)
	}
}

// ──────────────────────────── ResFrame 测试 ────────────────────────────

// TestNewResFrame_成功 测试构造成功响应帧
func TestNewResFrame_成功(t *testing.T) {
	payload := map[string]any{"result": "ok"}
	f := NewResFrame("req1", true, payload, "", "")
	if f.Type != "res" {
		t.Errorf("Type = %q, 期望 %q", f.Type, "res")
	}
	if f.ID != "req1" {
		t.Errorf("ID = %q, 期望 %q", f.ID, "req1")
	}
	if !f.OK {
		t.Error("OK 应为 true")
	}
	if f.Error != "" {
		t.Errorf("Error = %q, 成功时应为空", f.Error)
	}
	if f.Code != "" {
		t.Errorf("Code = %q, 成功时应为空", f.Code)
	}
}

// TestNewResFrame_失败 测试构造失败响应帧
func TestNewResFrame_失败(t *testing.T) {
	f := NewResFrame("req1", false, nil, "invalid request", "BAD_REQUEST")
	if f.OK {
		t.Error("OK 应为 false")
	}
	if f.Error != "invalid request" {
		t.Errorf("Error = %q, 期望 %q", f.Error, "invalid request")
	}
	if f.Code != "BAD_REQUEST" {
		t.Errorf("Code = %q, 期望 %q", f.Code, "BAD_REQUEST")
	}
}

// TestNewResFrame_失败无错误信息 测试失败时 errMsg 为空使用默认值
func TestNewResFrame_失败无错误信息(t *testing.T) {
	f := NewResFrame("req1", false, nil, "", "")
	if f.Error != "request failed" {
		t.Errorf("Error = %q, 期望默认值 %q", f.Error, "request failed")
	}
}

// TestNewResFrame_失败无错误码 测试失败时 code 为空不设置 code 字段
func TestNewResFrame_失败无错误码(t *testing.T) {
	f := NewResFrame("req1", false, nil, "some error", "")
	if f.Code != "" {
		t.Errorf("Code = %q, 应为空", f.Code)
	}
}

// TestResFrame_Encode_成功 测试成功响应帧编码
func TestResFrame_Encode_成功(t *testing.T) {
	f := NewResFrame("req1", true, map[string]any{"data": 42}, "", "")
	data, err := f.Encode()
	if err != nil {
		t.Fatalf("Encode 返回错误: %v", err)
	}
	// 验证 JSON 不包含 error/code 字段（omitempty）
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("JSON 解码失败: %v", err)
	}
	if _, ok := m["error"]; ok {
		t.Error("成功响应不应包含 error 字段")
	}
	if _, ok := m["code"]; ok {
		t.Error("成功响应不应包含 code 字段")
	}
}

// TestResFrame_Encode_失败 测试失败响应帧编码
func TestResFrame_Encode_失败(t *testing.T) {
	f := NewResFrame("req1", false, nil, "fail", "ERR_X")
	data, err := f.Encode()
	if err != nil {
		t.Fatalf("Encode 返回错误: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("JSON 解码失败: %v", err)
	}
	if _, ok := m["error"]; !ok {
		t.Error("失败响应应包含 error 字段")
	}
	if _, ok := m["code"]; !ok {
		t.Error("有 code 的失败响应应包含 code 字段")
	}
}

// ──────────────────────────── EventFrame 测试 ────────────────────────────

// TestNewEventFrame_完整 测试构造完整事件帧
func TestNewEventFrame_完整(t *testing.T) {
	payload := map[string]any{"content": "chunk1"}
	f := NewEventFrame("chat.chunk", payload, 1, "stream1")
	if f.Type != "event" {
		t.Errorf("Type = %q, 期望 %q", f.Type, "event")
	}
	if f.Event != "chat.chunk" {
		t.Errorf("Event = %q, 期望 %q", f.Event, "chat.chunk")
	}
	if f.Seq != 1 {
		t.Errorf("Seq = %d, 期望 %d", f.Seq, 1)
	}
	if f.StreamID != "stream1" {
		t.Errorf("StreamID = %q, 期望 %q", f.StreamID, "stream1")
	}
}

// TestNewEventFrame_无流式字段 测试 seq=0 streamID="" 时 omitempty
func TestNewEventFrame_无流式字段(t *testing.T) {
	f := NewEventFrame("chat.final", map[string]any{}, 0, "")
	data, err := f.Encode()
	if err != nil {
		t.Fatalf("Encode 返回错误: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("JSON 解码失败: %v", err)
	}
	if _, ok := m["seq"]; ok {
		t.Error("seq=0 应被 omitempty 忽略")
	}
	if _, ok := m["stream_id"]; ok {
		t.Error("stream_id=\"\" 应被 omitempty 忽略")
	}
}

// TestEventFrame_Encode 测试事件帧编码往返
func TestEventFrame_Encode(t *testing.T) {
	f := NewEventFrame("chat.chunk", map[string]any{"text": "hi"}, 3, "sid")
	data, err := f.Encode()
	if err != nil {
		t.Fatalf("Encode 返回错误: %v", err)
	}
	var f2 EventFrame
	if err := json.Unmarshal(data, &f2); err != nil {
		t.Fatalf("回解码失败: %v", err)
	}
	if f2.Type != f.Type || f2.Event != f.Event || f2.Seq != f.Seq || f2.StreamID != f.StreamID {
		t.Errorf("回解码字段不匹配: %+v", f2)
	}
}

// ──────────────────────────── DecodeFrame 测试 ────────────────────────────

// TestDecodeFrame_正常解码 测试通用帧解码
func TestDecodeFrame_正常解码(t *testing.T) {
	raw := `{"type":"req","id":"x","method":"chat.send","params":{}}`
	m, err := DecodeFrame([]byte(raw))
	if err != nil {
		t.Fatalf("DecodeFrame 返回错误: %v", err)
	}
	if m["type"] != "req" {
		t.Errorf("type = %v, 期望 %q", m["type"], "req")
	}
}

// TestDecodeFrame_空数据 测试空数据返回错误
func TestDecodeFrame_空数据(t *testing.T) {
	_, err := DecodeFrame(nil)
	if err == nil {
		t.Fatal("空数据应返回错误")
	}
}

// TestDecodeFrame_非法JSON 测试非法 JSON 返回错误
func TestDecodeFrame_非法JSON(t *testing.T) {
	_, err := DecodeFrame([]byte("{bad"))
	if err == nil {
		t.Fatal("非法 JSON 应返回错误")
	}
}

// ──────────────────────────── FrameType 枚举测试 ────────────────────────────

// TestFrameType_值验证 测试帧类型枚举值
func TestFrameType_值验证(t *testing.T) {
	if FrameTypeReq != "req" {
		t.Errorf("FrameTypeReq = %q, 期望 %q", FrameTypeReq, "req")
	}
	if FrameTypeRes != "res" {
		t.Errorf("FrameTypeRes = %q, 期望 %q", FrameTypeRes, "res")
	}
	if FrameTypeEvent != "event" {
		t.Errorf("FrameTypeEvent = %q, 期望 %q", FrameTypeEvent, "event")
	}
}

// ──────────────────────────── 编解码往返测试 ────────────────────────────

// TestReqFrame_编解码往返 测试 ReqFrame 完整编解码往返
func TestReqFrame_编解码往返(t *testing.T) {
	original := &ReqFrame{
		Type:   "req",
		ID:     "round-trip-id",
		Method: "command.compact",
		Params: json.RawMessage(`{"session_id":"s1"}`),
	}
	data, err := original.Encode()
	if err != nil {
		t.Fatalf("Encode 失败: %v", err)
	}
	decoded, err := DecodeReqFrame(data)
	if err != nil {
		t.Fatalf("DecodeReqFrame 失败: %v", err)
	}
	if decoded.Type != original.Type {
		t.Errorf("Type 不匹配: %q vs %q", decoded.Type, original.Type)
	}
	if decoded.ID != original.ID {
		t.Errorf("ID 不匹配: %q vs %q", decoded.ID, original.ID)
	}
	if decoded.Method != original.Method {
		t.Errorf("Method 不匹配: %q vs %q", decoded.Method, original.Method)
	}
}

// TestResFrame_编解码往返 测试 ResFrame 完整编解码往返
func TestResFrame_编解码往返(t *testing.T) {
	f := NewResFrame("rt1", true, map[string]any{"key": "val"}, "", "")
	data, err := f.Encode()
	if err != nil {
		t.Fatalf("Encode 失败: %v", err)
	}
	var f2 ResFrame
	if err := json.Unmarshal(data, &f2); err != nil {
		t.Fatalf("回解码失败: %v", err)
	}
	if f2.Type != f.Type || f2.ID != f.ID || f2.OK != f.OK {
		t.Errorf("回解码字段不匹配: %+v", f2)
	}
}
