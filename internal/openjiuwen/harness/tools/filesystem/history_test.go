package filesystem

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestBuildHistoryPath_完整参数(t *testing.T) {
	path := BuildHistoryPath("/home/user/workspace", "my_agent", "sess_123")
	expected := filepath.Join("/home/user/workspace", ".agent_history", "file_ops_my_agent_sess_123.json")
	if path != expected {
		t.Errorf("期望 %s，实际 %s", expected, path)
	}
}

func TestBuildHistoryPath_空baseDir(t *testing.T) {
	path := BuildHistoryPath("", "my_agent", "sess_123")
	expected := filepath.Join(".", ".agent_history", "file_ops_my_agent_sess_123.json")
	if path != expected {
		t.Errorf("期望 %s，实际 %s", expected, path)
	}
}

func TestBuildHistoryPath_空agentID(t *testing.T) {
	path := BuildHistoryPath("/workspace", "", "sess_123")
	expected := filepath.Join("/workspace", ".agent_history", "file_ops_default_sess_123.json")
	if path != expected {
		t.Errorf("期望 %s，实际 %s", expected, path)
	}
}

func TestAppendOpHistory_新增文件(t *testing.T) {
	tmpDir := t.TempDir()
	historyPath := BuildHistoryPath(tmpDir, "agent1", "sess1")

	content := "hello world"
	AppendOpHistory(historyPath, "/path/to/file.txt", "write", nil, &content)

	// 读取并验证
	data, err := os.ReadFile(historyPath)
	if err != nil {
		t.Fatalf("历史文件应存在: %v", err)
	}

	history := make(map[string]any)
	if err := json.Unmarshal(data, &history); err != nil {
		t.Fatalf("历史文件应可解析: %v", err)
	}

	entries, ok := history["/path/to/file.txt"].([]any)
	if !ok || len(entries) != 1 {
		t.Fatalf("期望 1 条记录，实际 %d", len(entries))
	}

	entry, ok := entries[0].(map[string]any)
	if !ok {
		t.Fatal("记录应为 map[string]any")
	}

	if entry["action"] != "write" {
		t.Errorf("期望 action=write，实际 %v", entry["action"])
	}
	if entry["old_content"] != nil {
		t.Errorf("期望 old_content=nil，实际 %v", entry["old_content"])
	}
	if entry["new_content"] != "hello world" {
		t.Errorf("期望 new_content='hello world'，实际 %v", entry["new_content"])
	}
}

func TestAppendOpHistory_多次追加(t *testing.T) {
	tmpDir := t.TempDir()
	historyPath := BuildHistoryPath(tmpDir, "agent1", "sess1")

	oldContent := "old text"
	newContent := "new text"
	AppendOpHistory(historyPath, "/path/to/file.txt", "write", nil, &oldContent)
	AppendOpHistory(historyPath, "/path/to/file.txt", "edit", &oldContent, &newContent)

	data, err := os.ReadFile(historyPath)
	if err != nil {
		t.Fatalf("历史文件应存在: %v", err)
	}

	history := make(map[string]any)
	_ = json.Unmarshal(data, &history)

	entries, ok := history["/path/to/file.txt"].([]any)
	if !ok || len(entries) != 2 {
		t.Fatalf("期望 2 条记录，实际 %d", len(entries))
	}
}

func TestAppendOpHistory_不同文件(t *testing.T) {
	tmpDir := t.TempDir()
	historyPath := BuildHistoryPath(tmpDir, "agent1", "sess1")

	content1 := "file1"
	content2 := "file2"
	AppendOpHistory(historyPath, "/a/file1.txt", "write", nil, &content1)
	AppendOpHistory(historyPath, "/b/file2.txt", "write", nil, &content2)

	data, err := os.ReadFile(historyPath)
	if err != nil {
		t.Fatalf("历史文件应存在: %v", err)
	}

	history := make(map[string]any)
	_ = json.Unmarshal(data, &history)

	if len(history) != 2 {
		t.Errorf("期望 2 个文件的历史记录，实际 %d", len(history))
	}
}

func TestAppendOpHistory_截断超过上限(t *testing.T) {
	tmpDir := t.TempDir()
	historyPath := BuildHistoryPath(tmpDir, "agent1", "sess1")

	// 写入超过 MaxHistoryPerFile 的条目
	for i := 0; i < MaxHistoryPerFile+10; i++ {
		content := "content_" + string(rune(i))
		AppendOpHistory(historyPath, "/path/to/file.txt", "write", nil, &content)
	}

	data, err := os.ReadFile(historyPath)
	if err != nil {
		t.Fatalf("历史文件应存在: %v", err)
	}

	history := make(map[string]any)
	_ = json.Unmarshal(data, &history)

	entries, ok := history["/path/to/file.txt"].([]any)
	if !ok {
		t.Fatal("记录应为 []any")
	}
	if len(entries) > MaxHistoryPerFile {
		t.Errorf("条目数不应超过 %d，实际 %d", MaxHistoryPerFile, len(entries))
	}
}

func TestDetectAndRecordDeletions_文件已被删除(t *testing.T) {
	tmpDir := t.TempDir()
	historyPath := BuildHistoryPath(tmpDir, "agent1", "sess1")

	// 先写入一条 write 记录
	content := "original"
	AppendOpHistory(historyPath, "/nonexistent/file.txt", "write", nil, &content)

	// /nonexistent/file.txt 不存在，应该被检测为 delete
	DetectAndRecordDeletions(historyPath)

	data, err := os.ReadFile(historyPath)
	if err != nil {
		t.Fatalf("历史文件应存在: %v", err)
	}

	history := make(map[string]any)
	_ = json.Unmarshal(data, &history)

	entries, ok := history["/nonexistent/file.txt"].([]any)
	if !ok || len(entries) != 2 {
		t.Fatalf("期望 2 条记录（write + delete），实际 %d", len(entries))
	}

	lastEntry, ok := entries[1].(map[string]any)
	if !ok {
		t.Fatal("最后一条记录应为 map[string]any")
	}
	if lastEntry["action"] != "delete" {
		t.Errorf("期望 action=delete，实际 %v", lastEntry["action"])
	}
}

func TestDetectAndRecordDeletions_文件仍存在(t *testing.T) {
	tmpDir := t.TempDir()
	historyPath := BuildHistoryPath(tmpDir, "agent1", "sess1")

	// 创建一个实际存在的文件
	existingFile := filepath.Join(tmpDir, "existing.txt")
	_ = os.WriteFile(existingFile, []byte("content"), 0o644)

	// 写入历史
	content := "original"
	AppendOpHistory(historyPath, existingFile, "write", nil, &content)

	// 文件存在，不应追加 delete
	DetectAndRecordDeletions(historyPath)

	data, err := os.ReadFile(historyPath)
	if err != nil {
		t.Fatalf("历史文件应存在: %v", err)
	}

	history := make(map[string]any)
	_ = json.Unmarshal(data, &history)

	entries, ok := history[existingFile].([]any)
	if !ok || len(entries) != 1 {
		t.Fatalf("期望只有 1 条记录（write），实际 %d", len(entries))
	}
}

func TestDetectAndRecordDeletions_历史文件不存在(t *testing.T) {
	tmpDir := t.TempDir()
	historyPath := filepath.Join(tmpDir, ".agent_history", "nonexistent.json")

	// 不应报错
	DetectAndRecordDeletions(historyPath)
}

func TestDetectAndRecordDeletions_已有delete记录(t *testing.T) {
	tmpDir := t.TempDir()
	historyPath := BuildHistoryPath(tmpDir, "agent1", "sess1")

	// 写入一条 delete 记录
	AppendOpHistory(historyPath, "/nonexistent/file.txt", "delete", nil, nil)

	// 再次检测，不应重复追加 delete
	DetectAndRecordDeletions(historyPath)

	data, err := os.ReadFile(historyPath)
	if err != nil {
		t.Fatalf("历史文件应存在: %v", err)
	}

	history := make(map[string]any)
	_ = json.Unmarshal(data, &history)

	entries, ok := history["/nonexistent/file.txt"].([]any)
	if !ok || len(entries) != 1 {
		t.Errorf("期望只有 1 条记录，不应重复追加 delete，实际 %d", len(entries))
	}
}
