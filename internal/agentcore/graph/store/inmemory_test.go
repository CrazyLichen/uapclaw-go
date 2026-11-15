package store

import "testing"

// newFilledStore 造一条已保存状态
func newFilledStore(t *testing.T) *InMemoryStore {
	t.Helper()
	s := NewInMemoryStore()
	if err := s.Save("sess", "ns1", &GraphState{Ns: "ns1", Step: 1}); err != nil {
		t.Fatalf("save 失败: %v", err)
	}
	return s
}

// TestInMemoryStore_Get 命中返回副本；未命中 (nil, nil)
func TestInMemoryStore_Get(t *testing.T) {
	s := newFilledStore(t)
	got, err := s.Get("sess", "ns1")
	if err != nil || got == nil || got.Ns != "ns1" {
		t.Fatalf("命中失败: %v %+v", err, got)
	}
	got.Step = 99
	again, _ := s.Get("sess", "ns1")
	if again.Step != 1 {
		t.Fatal("Get 应返回副本")
	}
	miss, err := s.Get("sess", "nope")
	if err != nil || miss != nil {
		t.Fatalf("未命中应为 (nil,nil): %v %+v", err, miss)
	}
}

// TestInMemoryStore_Save 存副本，外部改动不影响内部
func TestInMemoryStore_Save(t *testing.T) {
	s := NewInMemoryStore()
	st := &GraphState{Ns: "ns1", Step: 1}
	if err := s.Save("sess", "ns1", st); err != nil {
		t.Fatal(err)
	}
	st.Step = 42
	again, _ := s.Get("sess", "ns1")
	if again.Step != 1 {
		t.Fatal("Save 应存副本")
	}
}

// TestInMemoryStore_Delete 三分支：无 session / 全删 / 前缀删 + 删空连带
func TestInMemoryStore_Delete(t *testing.T) {
	// 无 session：静默
	s := NewInMemoryStore()
	if err := s.Delete("ghost", "ns"); err != nil {
		t.Fatal(err)
	}

	// 全删（ns 空串）
	s = newFilledStore(t)
	_ = s.Save("sess", "ns2", &GraphState{Ns: "ns2", Step: 1})
	if err := s.Delete("sess", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get("sess", "ns1"); got != nil {
		t.Fatal("全删后 ns1 仍在")
	}

	// 前缀删：ns=ns1 应只删 ns1 开头的（含 ns10），ns2 保留；删空后外层键连带删除
	s = newFilledStore(t)
	_ = s.Save("sess", "ns10", &GraphState{Ns: "ns10", Step: 1})
	_ = s.Save("sess", "ns2", &GraphState{Ns: "ns2", Step: 1})
	if err := s.Delete("sess", "ns1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get("sess", "ns1"); got != nil {
		t.Fatal("前缀删后 ns1 仍在")
	}
	if got, _ := s.Get("sess", "ns10"); got != nil {
		t.Fatal("前缀删应命中 ns10")
	}
	if got, _ := s.Get("sess", "ns2"); got == nil {
		t.Fatal("ns2 不应被删")
	}
	if err := s.Delete("sess", "ns2"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.store["sess"]; ok {
		t.Fatal("session 删空后应连带删除外层键")
	}
}
