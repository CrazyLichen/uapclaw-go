package lite

import "testing"

// TestWriteResult_ToDict 测试 WriteResult.ToDict
func TestWriteResult_ToDict(t *testing.T) {
	wr := WriteResult{
		Success:          true,
		Path:             "/tmp/test.md",
		Mode:             WriteModeCreate,
		ConflictDetected: false,
		ConflictingFiles: nil,
		Note:             "ok",
		Error:            "",
		Type:             "memory",
	}
	dict := wr.ToDict()
	if dict["success"] != true {
		t.Errorf("Expected success=true, got %v", dict["success"])
	}
	if dict["path"] != "/tmp/test.md" {
		t.Errorf("Expected path=/tmp/test.md, got %v", dict["path"])
	}
	if dict["mode"] != "create" {
		t.Errorf("Expected mode=create, got %v", dict["mode"])
	}
	// 无冲突时不应包含 conflict_detected 键
	if _, ok := dict["conflict_detected"]; ok {
		t.Errorf("Expected no conflict_detected key, got %v", dict["conflict_detected"])
	}
	if dict["type"] != "memory" {
		t.Errorf("Expected type=memory, got %v", dict["type"])
	}
	if dict["note"] != "ok" {
		t.Errorf("Expected note=ok, got %v", dict["note"])
	}
}

// TestWriteResult_ToDict_冲突场景 测试冲突场景
func TestWriteResult_ToDict_冲突场景(t *testing.T) {
	wr := WriteResult{
		Success:          false,
		Path:             "/tmp/conflict.md",
		Mode:             WriteModeSkip,
		ConflictDetected: true,
		ConflictingFiles: []string{"a.md", "b.md"},
		Note:             "conflict",
		Error:            "duplicate",
		Type:             "feedback",
	}
	dict := wr.ToDict()
	if dict["success"] != false {
		t.Errorf("Expected success=false, got %v", dict["success"])
	}
	if dict["mode"] != "skip" {
		t.Errorf("Expected mode=skip, got %v", dict["mode"])
	}
	if dict["conflict_detected"] != true {
		t.Errorf("Expected conflict_detected=true, got %v", dict["conflict_detected"])
	}
	files, ok := dict["conflicting_files"].([]string)
	if !ok || len(files) != 2 {
		t.Errorf("Expected 2 conflicting files, got %v", dict["conflicting_files"])
	}
	if dict["note"] != "conflict" {
		t.Errorf("Expected note=conflict, got %v", dict["note"])
	}
	if dict["error"] != "duplicate" {
		t.Errorf("Expected error=duplicate, got %v", dict["error"])
	}
}

// TestWriteResult_ToDict_无类型无冲突 测试无类型无冲突场景
func TestWriteResult_ToDict_无类型无冲突(t *testing.T) {
	wr := WriteResult{
		Success: true,
		Path:    "/tmp/simple.md",
		Mode:    WriteModeAppend,
	}
	dict := wr.ToDict()
	if dict["mode"] != "append" {
		t.Errorf("Expected mode=append, got %v", dict["mode"])
	}
	// 无类型时不应包含 type 键
	if _, ok := dict["type"]; ok {
		t.Errorf("Expected no type key, got %v", dict["type"])
	}
	// 无冲突时不应包含 conflict_detected 键
	if _, ok := dict["conflict_detected"]; ok {
		t.Errorf("Expected no conflict_detected key, got %v", dict["conflict_detected"])
	}
	// 无错误时不应包含 error 键
	if _, ok := dict["error"]; ok {
		t.Errorf("Expected no error key, got %v", dict["error"])
	}
}

// TestWriteMode_String 测试 WriteMode.String()
func TestWriteMode_String(t *testing.T) {
	if WriteModeCreate.String() != "create" {
		t.Errorf("Expected create, got %s", WriteModeCreate.String())
	}
	if WriteModeAppend.String() != "append" {
		t.Errorf("Expected append, got %s", WriteModeAppend.String())
	}
	if WriteModeSkip.String() != "skip" {
		t.Errorf("Expected skip, got %s", WriteModeSkip.String())
	}
}
