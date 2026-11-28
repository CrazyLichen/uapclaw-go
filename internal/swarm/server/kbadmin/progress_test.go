package kbadmin

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// openTestStoreForProgress 测试助手
func openTestStoreForProgress(t *testing.T) *Store {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "kbadmin.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// TestProgressSink_stage写入台账 阶段起止写入 stages_json + last_activity_at 刷新 + 事件发布
func TestProgressSink_stage写入台账(t *testing.T) {
	store := openTestStoreForProgress(t)
	_ = store.KB().Create(&KBInfo{ID: "kb-1", CreatedAt: 1, UpdatedAt: 1})
	_ = store.Doc().Create(&KBDoc{ID: "d1", KBID: "kb-1", FileName: "a.txt",
		ParseStatus: "processing", CreatedAt: 1, UpdatedAt: 1})
	var events []TaskEvent
	sink := NewProgressSink(store, func(e TaskEvent) { events = append(events, e) })
	job := &ParseJob{TaskID: "t1", DocID: "d1", KBID: "kb-1"}
	sink.OnStageStart(job, "parsing")
	sink.OnStageEnd(job, "parsing")
	doc, err := store.Doc().Get("d1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc.StagesJSON, `"parsing"`) {
		t.Fatalf("stages_json 应含 parsing: %s", doc.StagesJSON)
	}
	if doc.LastActivityAt == 0 {
		t.Fatal("last_activity_at 应刷新")
	}
	if len(events) != 2 || events[0].TaskID != "t1" || events[0].Phase != "started" {
		t.Fatalf("事件发布不符: %+v", events)
	}
}

// TestProgressSink_阶段失败记录错误信息
func TestProgressSink_阶段失败记录错误信息(t *testing.T) {
	store := openTestStoreForProgress(t)
	_ = store.Doc().Create(&KBDoc{ID: "d1", KBID: "kb-1", ParseStatus: "processing", CreatedAt: 1, UpdatedAt: 1})
	sink := NewProgressSink(store, nil)
	sink.OnStageStart(&ParseJob{TaskID: "t1", DocID: "d1", KBID: "kb-1"}, "embedding")
	sink.OnStageError(&ParseJob{TaskID: "t1", DocID: "d1", KBID: "kb-1"}, "embedding", errors.New("boom"))
	doc, _ := store.Doc().Get("d1")
	if !strings.Contains(doc.StagesJSON, "boom") {
		t.Fatalf("stages_json 应含错误信息: %s", doc.StagesJSON)
	}
}

// TestProgressSink_停滞扫描 超阈值 processing 文档标 suspected_stalled
func TestProgressSink_停滞扫描(t *testing.T) {
	store := openTestStoreForProgress(t)
	now := time.Now().Unix()
	_ = store.Doc().Create(&KBDoc{ID: "d1", KBID: "kb-1", ParseStatus: "processing",
		LastActivityAt: now - 600, StallState: "normal", CreatedAt: now, UpdatedAt: now})
	_ = store.Doc().Create(&KBDoc{ID: "d2", KBID: "kb-1", ParseStatus: "processing",
		LastActivityAt: now, StallState: "normal", CreatedAt: now, UpdatedAt: now})
	sink := NewProgressSink(store, nil)
	sink.ScanStalled(300 * time.Second)
	d1, _ := store.Doc().Get("d1")
	if d1.StallState != "suspected_stalled" {
		t.Fatalf("应标 suspected_stalled, got %s", d1.StallState)
	}
	d2, _ := store.Doc().Get("d2")
	if d2.StallState != "normal" {
		t.Fatalf("活动中的文档不应标停滞, got %s", d2.StallState)
	}
}
