package acp

import (
	"testing"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

func TestConsumeOneJSON_完整值(t *testing.T) {
	obj, rest := consumeOneJSON([]byte(`{"a":1} {"b":2}`))
	if obj == nil {
		t.Fatal("应解析出第一个值")
	}
	m, _ := obj.(map[string]any)
	if m == nil || m["a"] != float64(1) {
		t.Errorf("obj = %v", obj)
	}
	if string(rest) != `{"b":2}` {
		t.Errorf("rest = %q", rest)
	}
}

func TestConsumeOneJSON_前导空白(t *testing.T) {
	obj, rest := consumeOneJSON([]byte("  \n{\"a\":1}"))
	if obj == nil {
		t.Fatal("前导空白应跳过")
	}
	if len(rest) != 0 {
		t.Errorf("rest = %q", rest)
	}
}

func TestConsumeOneJSON_不完整(t *testing.T) {
	orig := []byte(`{"a":`)
	obj, rest := consumeOneJSON(orig)
	if obj != nil {
		t.Error("不完整 JSON 应返回 nil")
	}
	if string(rest) != string(orig) {
		t.Errorf("缓冲应原样保留，实际 %q", rest)
	}
}

func TestConsumeOneJSON_多值拼接(t *testing.T) {
	buf := []byte(`{"id":1}{"id":2}`)
	for i := 0; i < 2; i++ {
		var obj any
		obj, buf = consumeOneJSON(buf)
		if obj == nil {
			t.Fatalf("第 %d 个值应解析成功", i+1)
		}
	}
	if len(buf) != 0 {
		t.Errorf("剩余 = %q", buf)
	}
}

func TestConsumeOneJSON_数组与标量(t *testing.T) {
	obj, _ := consumeOneJSON([]byte(`[1,2]`))
	if arr, ok := obj.([]any); !ok || len(arr) != 2 {
		t.Errorf("数组应可解析: %v", obj)
	}
	obj, _ = consumeOneJSON([]byte(`42`))
	if obj != float64(42) {
		t.Errorf("标量应可解析: %v", obj)
	}
}

func TestConsumeOneJSON_超限报错(t *testing.T) {
	// Err 变体：超限返回错误（对齐 Python RuntimeError）
	_, _, err := consumeOneJSONErr([]byte(`{"a":1}`))
	if err != nil {
		t.Errorf("正常尺寸不应报错: %v", err)
	}
}

func TestIsPeerJSONRPCRequest(t *testing.T) {
	peer := map[string]any{"jsonrpc": "2.0", "id": float64(1), "method": "fs/read_text_file"}
	if !isPeerJSONRPCRequest(peer) {
		t.Error("合法对端请求应识别")
	}
	resp := map[string]any{"jsonrpc": "2.0", "id": float64(1), "result": map[string]any{}}
	if isPeerJSONRPCRequest(resp) {
		t.Error("响应不是请求")
	}
	notif := map[string]any{"jsonrpc": "2.0", "method": "session/update"}
	if isPeerJSONRPCRequest(notif) {
		t.Error("无 id 通知不是请求")
	}
	badVer := map[string]any{"jsonrpc": "1.0", "id": float64(1), "method": "x"}
	if isPeerJSONRPCRequest(badVer) {
		t.Error("版本错误不是请求")
	}
}

func TestExtractSessionUpdateText(t *testing.T) {
	msg := map[string]any{
		"method": "session/update",
		"params": map[string]any{
			"update": map[string]any{
				"content": map[string]any{"type": "text", "text": "hello"},
			},
		},
	}
	if got := extractSessionUpdateText(msg); got != "hello" {
		t.Errorf("text = %q", got)
	}
	empty := map[string]any{"method": "session/update"}
	if got := extractSessionUpdateText(empty); got != "" {
		t.Errorf("空 update 应返回空串，实际 %q", got)
	}
}

func TestJsonrpcErrorMessage(t *testing.T) {
	got := jsonrpcErrorMessage(map[string]any{"code": float64(-32000), "message": "boom"})
	if got != "ACP JSON-RPC error: -32000 boom" {
		t.Errorf("msg = %q", got)
	}
	got = jsonrpcErrorMessage(map[string]any{
		"code": float64(-32001), "message": "x", "data": "extra",
	})
	if got != "ACP JSON-RPC error: -32001 x data=extra" {
		t.Errorf("带 data msg = %q", got)
	}
	if got := jsonrpcErrorMessage("plain"); got != "ACP JSON-RPC error: plain" {
		t.Errorf("非 dict msg = %q", got)
	}
}
