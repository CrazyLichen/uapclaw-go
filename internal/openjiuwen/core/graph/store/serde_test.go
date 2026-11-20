package store

import (
	"bytes"
	"encoding/json"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/graph/pregel"
	"testing"
)

// TestJSONSerializer_RoundTrip 类型元组往返
func TestJSONSerializer_RoundTrip(t *testing.T) {
	s := &JSONSerializer{}
	typ, data, err := s.DumpsTyped(map[string]any{"k": "值"})
	if err != nil || typ != "json" {
		t.Fatalf("dumps: %v %s", err, typ)
	}
	// ensure_ascii=False / SetEscapeHTML(false) 语义：< > & 与非 ASCII 均不转义（D-9）
	if !json.Valid(data) || bytes.Contains(data, []byte("\\u003c")) {
		t.Fatal("产物非法或含 HTML 转义")
	}
	var got map[string]any
	if err := s.LoadsTyped("json", data, &got); err != nil {
		t.Fatal(err)
	}
	if got["k"] != "值" {
		t.Fatalf("往返失真: %+v", got)
	}
}

// TestJSONSerializer_HTMLNoEscape 字节层面验证不转义 < > &
func TestJSONSerializer_HTMLNoEscape(t *testing.T) {
	s := &JSONSerializer{}
	_, data, err := s.DumpsTyped(map[string]string{"k": "a<b>&c"})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"k":"a<b>&c"}` {
		t.Fatalf("应与 Python ensure_ascii=False 字节一致: %s", data)
	}
}

// TestJSONSerializer_LoadsTyped 边界：nil 数据与类型不匹配均不动 target 且返回 nil
func TestJSONSerializer_LoadsTyped(t *testing.T) {
	s := &JSONSerializer{}
	var m map[string]any
	if err := s.LoadsTyped("json", nil, &m); err != nil || m != nil {
		t.Fatalf("nil 数据应不动 target: %v %+v", err, m)
	}
	if err := s.LoadsTyped("pickle", []byte(`{}`), &m); err != nil || m != nil {
		t.Fatalf("类型不匹配应不动 target: %v %+v", err, m)
	}
}

// TestJSONSerializer_LoadsTypedTarget 还原到具体结构体（KV GraphStore 的真实用法，D-12）
func TestJSONSerializer_LoadsTypedTarget(t *testing.T) {
	s := &JSONSerializer{}
	orig := &pregel.GraphState{Ns: "ns", Step: 3, NodeVersion: map[string]int{"n": 1}}
	_, data, err := s.DumpsTyped(orig)
	if err != nil {
		t.Fatal(err)
	}
	var back pregel.GraphState
	if err := s.LoadsTyped("json", data, &back); err != nil {
		t.Fatal(err)
	}
	if back.Ns != "ns" || back.Step != 3 || back.NodeVersion["n"] != 1 {
		t.Fatalf("还原失真: %+v", back)
	}
}

// TestCreateSerializer 工厂三路：json 拒绝 / pickle 放行（JSON 承担）/ 未知拒绝
func TestCreateSerializer(t *testing.T) {
	if _, err := CreateSerializer("json"); err == nil || err.Error() != "json is not yet supported" {
		t.Fatalf("json 应按 Python raise 语义拒绝: %v", err)
	}
	s, err := CreateSerializer("pickle")
	if err != nil || s == nil {
		t.Fatalf("pickle 应返回实现: %v", err)
	}
	if _, err := CreateSerializer("msgpack"); err == nil || err.Error() != "Unknown serializer type: msgpack" {
		t.Fatalf("未知类型应拒绝: %v", err)
	}
}
