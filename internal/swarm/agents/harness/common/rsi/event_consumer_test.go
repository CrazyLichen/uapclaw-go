package rsi

// events/event_journal/event_consumer 单测：解析形态、四子类分派、journal 落盘、队列消费。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeUsage / fakeArtifact 消费方双缝。
type fakeUsage struct {
	cumulative int
	engineEvts int
	summary    map[string]any
}

func (f *fakeUsage) RecordCumulative(string, map[string]any, *int) { f.cumulative++ }
func (f *fakeUsage) RecordEngineEvent(string, map[string]any, *int) {
	f.engineEvts++
	f.summary = map[string]any{"tokens": map[string]any{}, "call_count": f.engineEvts}
}
func (f *fakeUsage) UsageSummary(string) map[string]any { return f.summary }

type fakeArtifact struct{ snapshots int }

func (f *fakeArtifact) MakeSnapshot(string, string, string, []RsiArtifactPath) string {
	f.snapshots++
	return "snap-1"
}
func (f *fakeArtifact) Locate(string, string) (string, bool) { return "/tmp/snap", true }

// TestParseEngineEvent_两种形态 顶层扁平载荷收口 + 规范化透传。
func TestParseEngineEvent_两种形态(t *testing.T) {
	// 形态 1：引擎事件化顶层信封 + 扁平载荷。
	e := ParseEngineEvent(map[string]any{
		"family": "progress", "kind": "metric", "task_id": "t", "iteration": 2, "score": 0.9}, "")
	if e == nil || !e.IsProgressMetric() {
		t.Fatalf("形态1 解析漂移: %+v", e)
	}
	if e.Payload["iteration"] != 2 {
		t.Fatalf("扁平载荷未收口: %v", e.Payload)
	}
	// 形态 2：规范化事件透传。
	e2 := ParseEngineEvent(map[string]any{"payload": map[string]any{"x": 1},
		"family": "node", "kind": "created"}, "")
	if e2 == nil || !e2.IsNodeCreated() {
		t.Fatalf("形态2 解析漂移")
	}
	// 非法 family/kind → nil。
	if ParseEngineEvent(map[string]any{"family": "x", "kind": "y"}, "") != nil {
		t.Fatalf("非法事件应 nil")
	}
}

// TestConsumer_metric与usage分派 P2 载荷嵌套+平铺别名。
func TestConsumer_metric与usage分派(t *testing.T) {
	root := t.TempDir()
	p := NewProjector(root)
	usage := &fakeUsage{summary: map[string]any{"call_count": 1}}
	var progressPayloads []map[string]any
	cons := NewEventConsumer("t", usage, p, &fakeArtifact{}, nil)
	cons.BindPush(
		func(_ string, payload map[string]any) error {
			progressPayloads = append(progressPayloads, payload)
			return nil
		}, nil)
	cons.OnEngineEvent(map[string]any{
		"event_type": "progress", "iteration": 1, "total_iterations": 5, "score": 0.7, "baseline": 0.4,
		"usage": map[string]any{"input_tokens": 10},
	})
	if usage.cumulative != 1 {
		t.Fatalf("累计记账漂移")
	}
	if len(progressPayloads) != 1 {
		t.Fatalf("P2 次数漂移")
	}
	payload := progressPayloads[0]
	score, _ := payload["score"].(*float64)
	if safeInt(payload["iteration"]) != 1 || score == nil || *score != 0.7 {
		t.Fatalf("平铺别名漂移: %v", payload)
	}
	if _, ok := payload["progress"].(map[string]any); !ok {
		t.Fatalf("嵌套 progress 缺失")
	}
	if _, ok := payload["usage"]; !ok {
		t.Fatalf("usage 缺失")
	}
}

// TestConsumer_nodeCreated_采纳快照 P3 增量 + 快照 ID 回写。
func TestConsumer_nodeCreated_采纳快照(t *testing.T) {
	root := t.TempDir()
	p := NewProjector(root)
	p.RegisterRoot("t", nil, "")
	art := &fakeArtifact{}
	var deltas []map[string]any
	cons := NewEventConsumer("t", &fakeUsage{}, p, art, nil)
	cons.BindPush(nil, func(_ string, payload map[string]any) error {
		deltas = append(deltas, payload)
		return nil
	})
	cons.OnEngineEvent(map[string]any{
		"family": "node", "kind": "created",
		"payload": map[string]any{"ref": "c1", "accepted": true, "outcome": "ADOPTED",
			"artifacts": []any{map[string]any{"role": "role", "path": "/x", "format": "y"}}},
	})
	if art.snapshots != 1 {
		t.Fatalf("快照未生成")
	}
	if len(deltas) != 1 {
		t.Fatalf("P3 次数漂移")
	}
	// 同 ref 二次事件：派生序生成新节点 N2，P3 增量照推（对齐 Python 派生序语义）。
	cons.OnEngineEvent(map[string]any{"family": "node", "kind": "created",
		"payload": map[string]any{"ref": "c1", "accepted": true}})
	if len(deltas) != 2 {
		t.Fatalf("二次事件 P3 次数漂移: %d", len(deltas))
	}
}

// TestConsumer_nodeStage与providerNode stage 推 P3；provider 节点保留原 ID。
func TestConsumer_nodeStage与providerNode(t *testing.T) {
	root := t.TempDir()
	p := NewProjector(root)
	p.RegisterRoot("t", nil, "")
	cons := NewEventConsumer("t", &fakeUsage{}, p, &fakeArtifact{}, nil)
	var deltas []map[string]any
	cons.BindPush(nil, func(_ string, payload map[string]any) error {
		deltas = append(deltas, payload)
		return nil
	})
	cons.OnEngineEvent(map[string]any{
		"event_type": "node.stage", "node_ref": "ROOT",
		"stage": map[string]any{"name": "评测中"},
	})
	if len(deltas) != 1 {
		t.Fatalf("stage P3 漂移")
	}
	// provider 完整节点：h0 → ROOT 归一，原 ID 进 ref 索引。
	cons.OnEngineEvent(map[string]any{
		"event_type": "node", "node": map[string]any{
			"node_id": "h0", "type": "ADOPTED", "adopted": true},
	})
	tree, _ := p.DeriveTree("t")
	nodes := tree["nodes"].([]map[string]any)
	if nodes[0]["node_id"] != "ROOT" {
		t.Fatalf("根归一漂移: %v", nodes[0]["node_id"])
	}
}

// TestConsumer_status事件丢弃 Provider 状态事件不改 store（状态权威在 TaskStore）。
func TestConsumer_status事件丢弃(t *testing.T) {
	p := NewProjector(t.TempDir())
	cons := NewEventConsumer("t", &fakeUsage{}, p, &fakeArtifact{}, nil)
	cons.OnEngineEvent(map[string]any{"event_type": "status"})
	if _, err := p.DeriveTree("t"); !isNotFoundErr(err) {
		t.Fatalf("status 事件不应产生投影")
	}
}

func isNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	type coder interface{ Error() string }
	_ = err.(coder)
	var e *RsiError
	return asRsiError(err, &e) && e.Code == "TASK_NOT_FOUND"
}

func asRsiError(err error, target **RsiError) bool {
	if e, ok := err.(*RsiError); ok {
		*target = e
		return true
	}
	return false
}

// TestJournal_追加与校验 task_id 校验 + jsonl 落盘。
func TestJournal_追加与校验(t *testing.T) {
	root := t.TempDir()
	j := NewEventJournal(root)
	if _, err := j.Append("t", map[string]any{"family": "node", "kind": "created"}); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Append("../escape", nil); err == nil {
		t.Fatalf("路径穿越未拦")
	}
	if _, err := j.Append("", nil); err == nil {
		t.Fatalf("空 task_id 未拦")
	}
	data, _ := os.ReadFile(filepath.Join(root, "t", "events.jsonl"))
	var rec map[string]any
	if err := json.Unmarshal(data[:len(data)-1], &rec); err != nil {
		t.Fatal(err)
	}
	if rec["event_type"] != "node.created" || rec["schema_version"] != 1.0 {
		t.Fatalf("journal 记录漂移: %v", rec)
	}
	if !strings.Contains(rec["recorded_at"].(string), "T") {
		t.Fatalf("recorded_at 漂移")
	}
}

// TestConsumeQueue_哨兵结束 事件逐条消费，nil 哨兵关闭 done。
func TestConsumeQueue_哨兵结束(t *testing.T) {
	root := t.TempDir()
	p := NewProjector(root)
	p.RegisterRoot("t", nil, "")
	cons := NewEventConsumer("t", &fakeUsage{}, p, &fakeArtifact{}, nil)
	queue := make(chan map[string]any, 8)
	done := make(chan struct{})
	go ConsumeQueue(queue, cons, done)
	queue <- map[string]any{"family": "progress", "kind": "metric", "iteration": 1}
	queue <- nil
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("哨兵未触发结束")
	}
	got, err := p.DeriveProgress("t")
	if err != nil || got["iteration"] != 1 {
		t.Fatalf("消费漂移: %v %v", got, err)
	}
}

// TestConsumeQueue_单事件失败不终止 panic 恢复后继续消费。
func TestConsumeQueue_单事件失败不终止(t *testing.T) {
	root := t.TempDir()
	p := NewProjector(root)
	cons := NewEventConsumer("t", &fakeUsage{}, p, &fakeArtifact{}, nil)
	queue := make(chan map[string]any, 8)
	done := make(chan struct{})
	go ConsumeQueue(queue, cons, done)
	queue <- map[string]any{"family": "bad", "kind": "x"} // 不 panic 但未识别
	queue <- map[string]any{"family": "progress", "kind": "metric", "iteration": 3}
	queue <- nil
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("消费者未存活")
	}
}
