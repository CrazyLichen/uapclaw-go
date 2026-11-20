package pregel

import (
	"encoding/json"
	"errors"
	"testing"
)

// TestPendingNode_JSONRoundTrip error↔string 双向适配（D-8）
// 自 store/base_test.go 随类型迁移（偏差 D6）。
func TestPendingNode_JSONRoundTrip(t *testing.T) {
	orig := PendingNode{NodeName: "n1", Status: "error", Exception: []error{errors.New("boom")}}
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	var back PendingNode
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.NodeName != "n1" || back.Status != "error" || len(back.Exception) != 1 || back.Exception[0].Error() != "boom" {
		t.Fatalf("还原失真: %+v", back)
	}
}

// TestPendingNode_JSONNilException null→nil（对齐 Python exception 默认 None）
func TestPendingNode_JSONNilException(t *testing.T) {
	var back PendingNode
	if err := json.Unmarshal([]byte(`{"node_name":"n","status":"interrupt","exception":null}`), &back); err != nil {
		t.Fatal(err)
	}
	if back.Exception != nil {
		t.Fatalf("null 应还原为 nil: %+v", back.Exception)
	}
}
