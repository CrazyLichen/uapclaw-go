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
	if dict["mode"] != 0 {
		t.Errorf("Expected mode=0 (Create), got %v", dict["mode"])
	}
	if dict["conflict_detected"] != false {
		t.Errorf("Expected conflict_detected=false, got %v", dict["conflict_detected"])
	}
	if dict["type"] != "memory" {
		t.Errorf("Expected type=memory, got %v", dict["type"])
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
	if dict["mode"] != 2 {
		t.Errorf("Expected mode=2 (Skip), got %v", dict["mode"])
	}
	files, ok := dict["conflicting_files"].([]string)
	if !ok || len(files) != 2 {
		t.Errorf("Expected 2 conflicting files, got %v", dict["conflicting_files"])
	}
}
