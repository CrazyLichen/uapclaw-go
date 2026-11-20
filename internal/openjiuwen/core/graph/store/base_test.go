package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/graph/pregel"
)

// ──────────────────────────── GraphStore 日志壳 ────────────────────────────

// fakeStore 可控返回的存储桩
type fakeStore struct {
	getState  *pregel.GraphState
	getErr    error
	saveErr   error
	delErr    error
	getCalls  int
	saveCalls int
	delCalls  int
	lastDelNs string
}

func (f *fakeStore) Get(_ context.Context, sessionID, ns string) (*pregel.GraphState, error) {
	f.getCalls++
	return f.getState, f.getErr
}
func (f *fakeStore) Save(_ context.Context, sessionID, ns string, state *pregel.GraphState) error {
	f.saveCalls++
	return f.saveErr
}
func (f *fakeStore) Delete(_ context.Context, sessionID, ns string) error {
	f.delCalls++
	f.lastDelNs = ns
	return f.delErr
}

// TestGraphStore_Get 未命中 debug 日志、异常透传
func TestGraphStore_Get(t *testing.T) {
	ctx := context.Background()
	gs := NewGraphStore(&fakeStore{getState: &pregel.GraphState{Ns: "ns"}})
	st, err := gs.Get(ctx, "s1", "ns")
	if err != nil || st == nil {
		t.Fatalf("命中透传失败: %v %+v", err, st)
	}

	gs = NewGraphStore(&fakeStore{}) // 未命中
	st, err = gs.Get(ctx, "s1", "ns")
	if err != nil || st != nil {
		t.Fatalf("未命中应透传 (nil,nil): %v %+v", err, st)
	}

	boom := errors.New("db down")
	gs = NewGraphStore(&fakeStore{getErr: boom})
	if _, err = gs.Get(ctx, "s1", "ns"); err != boom {
		t.Fatalf("异常应原样透传: %v", err)
	}
}

// TestGraphStore_Save 前后日志 + 异常透传
func TestGraphStore_Save(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("disk full")
	gs := NewGraphStore(&fakeStore{saveErr: boom})
	if err := gs.Save(ctx, "s1", "ns", &pregel.GraphState{}); err != boom {
		t.Fatalf("save 异常应透传: %v", err)
	}
	ok := NewGraphStore(&fakeStore{})
	if err := ok.Save(ctx, "s1", "ns", &pregel.GraphState{}); err != nil {
		t.Fatal(err)
	}
}

// TestGraphStore_Delete 空 ns 透传 all 语义 + 异常透传（Python 走 debug 级也 re-raise）
func TestGraphStore_Delete(t *testing.T) {
	ctx := context.Background()
	fs := &fakeStore{}
	gs := NewGraphStore(fs)
	if err := gs.Delete(ctx, "s1", ""); err != nil {
		t.Fatal(err)
	}
	if fs.lastDelNs != "" {
		t.Fatal("空 ns 应原样透传")
	}
	boom := errors.New("del fail")
	gs = NewGraphStore(&fakeStore{delErr: boom})
	if err := gs.Delete(ctx, "s1", "ns1"); err != boom {
		t.Fatalf("delete 异常应透传: %v", err)
	}
}

// TestGraphState_JSONRoundTrip 组合往返验证嵌套自动递归（Message kind + PendingNode error）
func TestGraphState_JSONRoundTrip(t *testing.T) {
	orig := &pregel.GraphState{
		Ns: "ns", Step: 2,
		ChannelValues: map[string][]pregel.ChannelSnapshot{
			"n1": {{Messages: []pregel.Message{pregel.NewBarrierMessage("b", "barrier:a&b->c")}}},
		},
		PendingBuffer: []pregel.Message{pregel.NewTriggerMessage("a", "n1")},
		PendingNode:   map[string]pregel.PendingNode{"n2": {NodeName: "n2", Status: "error", Exception: []error{errors.New("x")}}},
		NodeVersion:   map[string]int{"n1": 7},
	}
	var back pregel.GraphState
	if err := jsonRoundTrip(orig, &back); err != nil {
		t.Fatal(err)
	}
	mv := back.ChannelValues["n1"][0].Messages[0]
	if mv.Kind() != pregel.KindBarrier {
		t.Fatalf("嵌套 Message kind 未还原: %+v", mv)
	}
	if back.PendingBuffer[0].Kind() != pregel.KindTrigger {
		t.Fatalf("PendingBuffer kind 未还原: %+v", back.PendingBuffer[0])
	}
	if back.PendingNode["n2"].Exception[0].Error() != "x" {
		t.Fatalf("嵌套 Exception 未还原: %+v", back.PendingNode["n2"])
	}
	if back.NodeVersion["n1"] != 7 || back.Step != 2 {
		t.Fatalf("标量字段失真: %+v", back)
	}
}

// jsonRoundTrip 测试辅助：序列化再还原
func jsonRoundTrip(in any, out any) error {
	data, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}
