package common

import "testing"

// TestRetrieverItem 构造与字段访问
func TestRetrieverItem(t *testing.T) {
	item := RetrieverItem{ItemID: "id1", Payload: "worker-1", Label: "L", Description: "D"}
	if item.ItemID != "id1" || item.Payload != "worker-1" || item.Label != "L" || item.Description != "D" {
		t.Fatalf("字段访问不符: %+v", item)
	}
	// 缺省字段为空（对齐 Python dataclass 默认 ""）
	empty := RetrieverItem{ItemID: "a", Payload: "b"}
	if empty.Label != "" || empty.Description != "" {
		t.Fatalf("缺省字段应为空串: %+v", empty)
	}
}

// TestRetrieverNode 树构造与子节点遍历
func TestRetrieverNode(t *testing.T) {
	root := RetrieverNode{
		NodeID: "/",
		Label:  "root",
		Children: []RetrieverNode{
			{NodeID: "/a", Label: "A", Items: []RetrieverItem{{ItemID: "i1", Payload: "p1"}}},
			{NodeID: "/b", Label: "B"},
		},
		Items: []RetrieverItem{{ItemID: "top", Payload: "top"}},
	}
	if len(root.Children) != 2 || root.Children[0].NodeID != "/a" {
		t.Fatalf("子节点结构不符: %+v", root)
	}
	if len(root.Children[0].Items) != 1 || root.Children[0].Items[0].Payload != "p1" {
		t.Fatalf("子节点条目不符: %+v", root.Children[0])
	}
	// 缺省 children/items 为空（对齐 Python 默认 ()）
	leaf := RetrieverNode{NodeID: "n", Label: "l"}
	if leaf.Children != nil && len(leaf.Children) != 0 {
		t.Fatalf("缺省子节点应为空: %+v", leaf)
	}
}

// TestRetrieverTrace 并发追加与值拷贝
func TestRetrieverTrace(t *testing.T) {
	tr := &RetrieverTrace{}
	detail := map[string]any{"k": "v"}
	tr.Record("enter", "n1", 1, detail)
	detail["k"] = "mutated" // 追加后改外部 map 不得影响已存事件
	tr.Record("leave", "n1", 1, nil)
	if len(tr.Events) != 2 {
		t.Fatalf("事件数不符: %d", len(tr.Events))
	}
	if tr.Events[0].Detail["k"] != "v" {
		t.Fatalf("detail 应为值拷贝: %+v", tr.Events[0].Detail)
	}
	if tr.Events[1].Detail == nil || len(tr.Events[1].Detail) != 0 {
		t.Fatalf("nil detail 应落空 map: %+v", tr.Events[1])
	}
}

// TestRetrieverTrace 并发安全
func TestRetrieverTrace_并发追加(t *testing.T) {
	tr := &RetrieverTrace{}
	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				tr.Record("e", "n", 0, nil)
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < 8; i++ {
		<-done
	}
	if len(tr.Events) != 800 {
		t.Fatalf("并发追加丢事件: %d", len(tr.Events))
	}
}

// TestRetrieverCandidateAndChoice 候选与选择项字段
func TestRetrieverCandidateAndChoice(t *testing.T) {
	c := RetrieverCandidate{Rank: 1, ItemID: "i", Payload: "p", BranchPath: []string{"a", "b"}}
	if c.Rank != 1 || len(c.BranchPath) != 2 || c.BranchPath[1] != "b" {
		t.Fatalf("候选字段不符: %+v", c)
	}
	ch := RetrieverChoice{ChoiceID: "c1", Payload: "p"}
	if ch.ChoiceID != "c1" || ch.Description != "" {
		t.Fatalf("选择项字段不符: %+v", ch)
	}
}
