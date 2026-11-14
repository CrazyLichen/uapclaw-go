package common

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// metadataPath：非法 session_id 与路径逃逸拒绝。
// 对齐 Python: model_selection_store.py:20-28。
func TestMetadataPath_校验与逃逸防御(t *testing.T) {
	if _, err := metadataPath(""); err == nil || err.Error() != "invalid session_id" {
		t.Errorf("空 session_id 应拒绝: %v", err)
	}
	if _, err := metadataPath("../evil"); err == nil {
		t.Errorf("路径逃逸应拒绝")
	}
	if _, err := metadataPath("has space"); err == nil {
		t.Errorf("非法字符 session_id 应拒绝")
	}
}

// Get/Set round-trip：写入后可读回，错误码与文案对齐。
// 对齐 Python: model_selection_store.py:43-60。
func TestSessionModelSelection_RoundTrip(t *testing.T) {
	setSessionsRoot(t)
	// 无文件 → nil
	sel, err := GetSessionModelSelection("sess_roundtrip")
	if err != nil || sel != nil {
		t.Fatalf("无 metadata 应返回 nil: %v %v", sel, err)
	}
	// 先造一个合法 session 目录（写方要求会话存在？Python 侧写方自行创建）
	want := &ModelSelection{Type: "model_group", ID: "mgp_a", RouteID: "rte_1"}
	if err := SetSessionModelSelection("sess_roundtrip", want); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	got, err := GetSessionModelSelection("sess_roundtrip")
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if got == nil || got.Type != "model_group" || got.ID != "mgp_a" || got.RouteID != "rte_1" {
		t.Errorf("round-trip 字段不符: %+v", got)
	}
	// 合并式：其余字段不动（对齐 Python merge_fields={"model_selection"}）
	metaPath := filepath.Join(os.Getenv("UAPCLAW_DATA_DIR"), "agent", "sessions", "sess_roundtrip", "metadata.json")
	raw, _ := os.ReadFile(metaPath)
	if !strings.Contains(string(raw), "\"session_id\"") {
		t.Errorf("metadata 应含 session_id 字段: %s", raw)
	}
}

// 读损坏 metadata → MODEL_GROUP_INVALID 包装。
// 对齐 Python: model_selection_store.py:50-51。
func TestSessionModelSelection_损坏包装(t *testing.T) {
	dir := setSessionsRoot(t)
	bad := filepath.Join(dir, "agent", "sessions", "s_bad", "metadata.json")
	if err := os.MkdirAll(filepath.Dir(bad), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte("not-json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := GetSessionModelSelection("s_bad")
	se, ok := err.(*ModelSelectionError)
	if !ok || se.Code != ModelGroupInvalid {
		t.Errorf("应包装为 MODEL_GROUP_INVALID: %v", err)
	}
}

// 清除语义：Set(nil) 后读回 nil。
// 对齐 Python: session_metadata.py:801 selection=None 清除。
func TestSessionModelSelection_清除(t *testing.T) {
	setSessionsRoot(t)
	if err := SetSessionModelSelection("sess_clear", &ModelSelection{Type: "model", ID: "mdl_a"}); err != nil {
		t.Fatal(err)
	}
	if err := SetSessionModelSelection("sess_clear", nil); err != nil {
		t.Fatal(err)
	}
	got, err := GetSessionModelSelection("sess_clear")
	if err != nil || got != nil {
		t.Errorf("清除后应返回 nil: %v %v", got, err)
	}
	// 重置路径缓存，避免影响其它用例
	path.ResetCache()
}
