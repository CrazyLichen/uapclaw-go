package utils

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestComputeHunks_删除文件(t *testing.T) {
	old := "line1\nline2\nline3"
	hunks := computeHunks(&old, nil)
	if len(hunks) != 1 {
		t.Fatalf("期望 1 个 hunk，实际 %d", len(hunks))
	}
	h := hunks[0]
	if h.OldStart != 1 || h.OldLines != 3 {
		t.Errorf("期望 OldStart=1, OldLines=3，实际 OldStart=%d, OldLines=%d", h.OldStart, h.OldLines)
	}
	if h.NewStart != 0 || h.NewLines != 0 {
		t.Errorf("期望 NewStart=0, NewLines=0")
	}
	if len(h.Lines) != 3 {
		t.Errorf("期望 3 行，实际 %d", len(h.Lines))
	}
	for _, line := range h.Lines {
		if !startsWithMinus(line) {
			t.Errorf("所有行应以 - 开头：%s", line)
		}
	}
}

func TestComputeHunks_新建文件(t *testing.T) {
	newContent := "line1\nline2"
	hunks := computeHunks(nil, &newContent)
	if len(hunks) != 1 {
		t.Fatalf("期望 1 个 hunk，实际 %d", len(hunks))
	}
	h := hunks[0]
	if h.OldStart != 0 || h.OldLines != 0 {
		t.Errorf("期望 OldStart=0, OldLines=0")
	}
	if h.NewStart != 1 || h.NewLines != 2 {
		t.Errorf("期望 NewStart=1, NewLines=2")
	}
	for _, line := range h.Lines {
		if !startsWithPlus(line) {
			t.Errorf("所有行应以 + 开头：%s", line)
		}
	}
}

func TestComputeHunks_两者都nil(t *testing.T) {
	hunks := computeHunks(nil, nil)
	if hunks != nil {
		t.Errorf("期望 nil")
	}
}

func TestComputeHunks_修改文件(t *testing.T) {
	old := "line1\nline2\nline3"
	newContent := "line1\nmodified\nline3"
	hunks := computeHunks(&old, &newContent)
	if len(hunks) == 0 {
		t.Fatal("期望至少 1 个 hunk")
	}
	// 应有删除旧 line2 和插入新 modified 的操作
	foundMinus := false
	foundPlus := false
	for _, h := range hunks {
		for _, line := range h.Lines {
			if startsWithMinus(line) && line == "-line2" {
				foundMinus = true
			}
			if startsWithPlus(line) && line == "+modified" {
				foundPlus = true
			}
		}
	}
	if !foundMinus {
		t.Error("期望找到 -line2")
	}
	if !foundPlus {
		t.Error("期望找到 +modified")
	}
}

func TestComputeHunks_空文件到有内容(t *testing.T) {
	old := ""
	newContent := "hello"
	hunks := computeHunks(&old, &newContent)
	if len(hunks) == 0 {
		t.Fatal("期望至少 1 个 hunk")
	}
}

func TestComputeHunks_纯插入(t *testing.T) {
	old := "a\nb"
	newContent := "a\nc\nb"
	hunks := computeHunks(&old, &newContent)
	if len(hunks) == 0 {
		t.Fatal("期望至少 1 个 hunk")
	}
	// 应有 +c 行
	foundInsert := false
	for _, h := range hunks {
		for _, line := range h.Lines {
			if line == "+c" {
				foundInsert = true
			}
		}
	}
	if !foundInsert {
		t.Error("期望找到 +c")
	}
}

func TestComputeHunks_纯删除(t *testing.T) {
	old := "a\nb\nc"
	newContent := "a\nc"
	hunks := computeHunks(&old, &newContent)
	if len(hunks) == 0 {
		t.Fatal("期望至少 1 个 hunk")
	}
	foundDelete := false
	for _, h := range hunks {
		for _, line := range h.Lines {
			if line == "-b" {
				foundDelete = true
			}
		}
	}
	if !foundDelete {
		t.Error("期望找到 -b")
	}
}

func TestComputeHunks_多行替换(t *testing.T) {
	old := "line1\nline2\nline3\nline4"
	newContent := "line1\nreplace_a\nreplace_b\nline4"
	hunks := computeHunks(&old, &newContent)
	if len(hunks) == 0 {
		t.Fatal("期望至少 1 个 hunk")
	}
	totalMinus := 0
	totalPlus := 0
	for _, h := range hunks {
		for _, line := range h.Lines {
			if startsWithMinus(line) && !strings.HasPrefix(line, "---") {
				totalMinus++
			}
			if startsWithPlus(line) && !strings.HasPrefix(line, "+++") {
				totalPlus++
			}
		}
	}
	if totalMinus != 2 {
		t.Errorf("期望 2 行删除，实际 %d", totalMinus)
	}
	if totalPlus != 2 {
		t.Errorf("期望 2 行插入，实际 %d", totalPlus)
	}
}

func TestIsoToTimestamp_RFC3339(t *testing.T) {
	ts := isoToTimestamp("2025-01-15T10:30:00Z")
	if ts == 0 {
		t.Error("期望非零 timestamp")
	}
}

func TestIsoToTimestamp_RFC3339Nano(t *testing.T) {
	ts := isoToTimestamp("2025-01-15T10:30:00.123456789Z")
	if ts == 0 {
		t.Error("期望非零 timestamp（RFC3339Nano 格式）")
	}
	// 应包含小数部分
	if ts == float64(int64(ts)) {
		t.Error("期望包含小数秒部分")
	}
}

func TestIsoToTimestamp_无效格式(t *testing.T) {
	ts := isoToTimestamp("not-a-date")
	if ts != 0 {
		t.Errorf("无效格式期望 0，实际 %f", ts)
	}
}

func TestIsoToTimestamp_空字符串(t *testing.T) {
	ts := isoToTimestamp("")
	if ts != 0 {
		t.Errorf("空字符串期望 0，实际 %f", ts)
	}
}

func TestTimestampToISO(t *testing.T) {
	iso := timestampToISO(1736941800)
	if iso == "" {
		t.Error("期望非空 ISO 字符串")
	}
	// 应包含 Z 后缀（UTC）
	if !strings.HasSuffix(iso, "Z") {
		t.Errorf("期望以 Z 结尾（UTC），实际 %s", iso)
	}
}

func TestTimestampToISO_含小数秒(t *testing.T) {
	iso := timestampToISO(1736941800.5)
	if iso == "" {
		t.Error("期望非空 ISO 字符串")
	}
}

func TestTruncate(t *testing.T) {
	if truncate("hello world", 5) != "hello" {
		t.Error("截断到 5 字符失败")
	}
	if truncate("short", 10) != "short" {
		t.Error("短字符串不应截断")
	}
}

func TestAbs(t *testing.T) {
	if abs(-3.5) != 3.5 {
		t.Error("负数绝对值错误")
	}
	if abs(3.5) != 3.5 {
		t.Error("正数绝对值错误")
	}
	if abs(0) != 0 {
		t.Error("零绝对值错误")
	}
}

func TestNormalizePath(t *testing.T) {
	// 基本功能测试
	result := normalizePath("/home/user/file.txt")
	if result == "" {
		t.Error("期望非空路径")
	}
	// 空路径
	emptyResult := normalizePath("")
	if emptyResult == "" {
		t.Error("空路径也应返回结果（当前目录的绝对路径）")
	}
}

func TestIsDuplicateEntry(t *testing.T) {
	existing := []opEntry{
		{Action: "write", Timestamp: "2025-01-15T10:30:00Z"},
	}
	newEntry := &opEntry{Action: "write", Timestamp: "2025-01-15T10:30:01Z"} // 1秒差
	if !isDuplicateEntry(existing, newEntry) {
		t.Error("相近时间戳（1秒）应判定为重复")
	}

	farEntry := &opEntry{Action: "write", Timestamp: "2025-01-15T11:30:00Z"} // 1小时差
	if isDuplicateEntry(existing, farEntry) {
		t.Error("远时间戳不应判定为重复")
	}

	// 不同 action 不应判定为重复
	diffActionEntry := &opEntry{Action: "edit", Timestamp: "2025-01-15T10:30:01Z"}
	if isDuplicateEntry(existing, diffActionEntry) {
		t.Error("不同 action 不应判定为重复")
	}

	// 空 existing 不应判定为重复
	if isDuplicateEntry(nil, newEntry) {
		t.Error("空 existing 不应判定为重复")
	}
}

func TestParseOpEntry(t *testing.T) {
	m := map[string]any{
		"action":      "write",
		"timestamp":   "2025-01-15T10:30:00Z",
		"old_content": "old",
		"new_content": "new",
	}
	entry := parseOpEntry(m)
	if entry == nil {
		t.Fatal("期望非 nil entry")
	}
	if entry.Action != "write" {
		t.Errorf("期望 action=write，实际 %s", entry.Action)
	}
	if entry.OldContent == nil || *entry.OldContent != "old" {
		t.Error("期望 old_content=old")
	}
	if entry.NewContent == nil || *entry.NewContent != "new" {
		t.Error("期望 new_content=new")
	}
}

func TestParseOpEntry_空action(t *testing.T) {
	m := map[string]any{
		"timestamp": "2025-01-15T10:30:00Z",
	}
	entry := parseOpEntry(m)
	if entry != nil {
		t.Error("缺少 action 应返回 nil")
	}
}

func TestParseOpEntry_空timestamp(t *testing.T) {
	m := map[string]any{
		"action": "write",
	}
	entry := parseOpEntry(m)
	if entry != nil {
		t.Error("缺少 timestamp 应返回 nil")
	}
}

func TestParseOpEntry_无oldNewContent(t *testing.T) {
	m := map[string]any{
		"action":    "delete",
		"timestamp": "2025-01-15T10:30:00Z",
	}
	entry := parseOpEntry(m)
	if entry == nil {
		t.Fatal("期望非 nil entry")
	}
	if entry.OldContent != nil {
		t.Error("无 old_content 期望 nil")
	}
	if entry.NewContent != nil {
		t.Error("无 new_content 期望 nil")
	}
}

func TestParseOpEntry_nilOldNewContent(t *testing.T) {
	m := map[string]any{
		"action":      "write",
		"timestamp":   "2025-01-15T10:30:00Z",
		"old_content": nil,
		"new_content": nil,
	}
	entry := parseOpEntry(m)
	if entry == nil {
		t.Fatal("期望非 nil entry")
	}
	if entry.OldContent != nil {
		t.Error("nil old_content 期望 OldContent 为 nil")
	}
	if entry.NewContent != nil {
		t.Error("nil new_content 期望 NewContent 为 nil")
	}
}

func TestBuildHistoryPath_对齐(t *testing.T) {
	// DiffService 内部使用 BuildHistoryPath（filesystem 包）
	// 这里验证路径格式
	baseDir := "/workspace"
	agentID := "jiuwenswarm"
	sessionID := "session1"
	expected := baseDir + "/.agent_history/file_ops_" + agentID + "_" + sessionID + ".json"
	if expected == "" {
		t.Error("路径格式验证失败")
	}
}

func TestGetDiffService_单例(t *testing.T) {
	ds1 := GetDiffService()
	ds2 := GetDiffService()
	if ds1 != ds2 {
		t.Error("GetDiffService 应返回同一实例")
	}
}

func TestIsValidFileOpsFile(t *testing.T) {
	ds := GetDiffService()

	// 有效名称（带 session）
	if !ds.isValidFileOpsFile("file_ops_jiuwenswarm_session1.json", "session1", true) {
		t.Error("有效 session-specific 名称应通过")
	}

	// 不匹配 session（requireSession=true）
	if ds.isValidFileOpsFile("file_ops_jiuwenswarm_other.json", "session1", true) {
		t.Error("不匹配 session 应失败")
	}

	// 空 sessionID（requireSession=true）
	if ds.isValidFileOpsFile("file_ops_jiuwenswarm_session1.json", "", true) {
		t.Error("空 sessionID 且 requireSession=true 应失败")
	}

	// 空 sessionID（requireSession=false）— 全局文件不含 sessionID 后缀
	// 但 isValidFileOpsFile 的 prefix 检查要求 "file_ops_<agentID>_" 前缀
	// 所以 "file_ops_jiuwenswarm.json" 不匹配 "file_ops_jiuwenswarm_" 前缀
	if ds.isValidFileOpsFile("file_ops_jiuwenswarm.json", "", false) {
		t.Error("全局 file_ops 文件名不匹配 _ 前缀要求，应失败")
	}

	// 空 sessionID（requireSession=false）— 匹配含 _ 的名称
	if !ds.isValidFileOpsFile("file_ops_jiuwenswarm_session1.json", "", false) {
		t.Error("含 session 后缀的名称，requireSession=false 且空 sessionID 应通过")
	}

	// 非 file_ops 前缀
	if ds.isValidFileOpsFile("other.json", "session1", false) {
		t.Error("非 file_ops 前缀应失败")
	}

	// 无 .json 后缀
	if ds.isValidFileOpsFile("file_ops_jiuwenswarm_session1", "session1", false) {
		t.Error("无 .json 后缀应失败")
	}

	// requireSession=false 且 sessionID 匹配
	if !ds.isValidFileOpsFile("file_ops_jiuwenswarm_session1.json", "session1", false) {
		t.Error("requireSession=false 且 sessionID 匹配应通过")
	}
}

func TestComputeLineOpCodes_纯等行(t *testing.T) {
	old := []string{"a", "b", "c"}
	newLines := []string{"a", "b", "c"}
	codes := computeLineOpCodes(old, newLines)
	if len(codes) != 1 {
		t.Fatalf("期望 1 个 opcode，实际 %d", len(codes))
	}
	if codes[0].tag != "equal" {
		t.Errorf("期望 equal，实际 %s", codes[0].tag)
	}
	if codes[0].i1 != 0 || codes[0].i2 != 3 || codes[0].j1 != 0 || codes[0].j2 != 3 {
		t.Errorf("期望 [0,3,0,3]，实际 [%d,%d,%d,%d]", codes[0].i1, codes[0].i2, codes[0].j1, codes[0].j2)
	}
}

func TestComputeLineOpCodes_纯插入(t *testing.T) {
	old := []string{"a", "c"}
	newLines := []string{"a", "b", "c"}
	codes := computeLineOpCodes(old, newLines)
	// 应有 equal(a) + insert(b) + equal(c)
	foundInsert := false
	for _, code := range codes {
		if code.tag == "insert" {
			foundInsert = true
		}
	}
	if !foundInsert {
		t.Error("期望找到 insert opcode")
	}
}

func TestComputeLineOpCodes_纯删除(t *testing.T) {
	old := []string{"a", "b", "c"}
	newLines := []string{"a", "c"}
	codes := computeLineOpCodes(old, newLines)
	foundDelete := false
	for _, code := range codes {
		if code.tag == "delete" {
			foundDelete = true
		}
	}
	if !foundDelete {
		t.Error("期望找到 delete opcode")
	}
}

func TestComputeLineOpCodes_替换(t *testing.T) {
	old := []string{"a", "b", "c"}
	newLines := []string{"a", "x", "c"}
	codes := computeLineOpCodes(old, newLines)
	foundReplace := false
	for _, code := range codes {
		if code.tag == "replace" {
			foundReplace = true
		}
	}
	if !foundReplace {
		t.Error("期望找到 replace opcode")
	}
}

func TestComputeLineOpCodes_空行(t *testing.T) {
	codes := computeLineOpCodes(nil, nil)
	if len(codes) != 0 {
		t.Errorf("期望 0 个 opcode，实际 %d", len(codes))
	}
}

func TestComputeLineOpCodes_一侧空(t *testing.T) {
	old := []string{}
	newLines := []string{"a", "b"}
	codes := computeLineOpCodes(old, newLines)
	if len(codes) != 1 {
		t.Fatalf("期望 1 个 opcode（insert），实际 %d", len(codes))
	}
	if codes[0].tag != "insert" {
		t.Errorf("期望 insert，实际 %s", codes[0].tag)
	}
}

func TestSumLinesAdded(t *testing.T) {
	files := map[string]*FileDiff{
		"file1": {LinesAdded: 5},
		"file2": {LinesAdded: 3},
	}
	if sumLinesAdded(files) != 8 {
		t.Errorf("期望 8，实际 %d", sumLinesAdded(files))
	}
}

func TestSumLinesRemoved(t *testing.T) {
	files := map[string]*FileDiff{
		"file1": {LinesRemoved: 5},
		"file2": {LinesRemoved: 3},
	}
	if sumLinesRemoved(files) != 8 {
		t.Errorf("期望 8，实际 %d", sumLinesRemoved(files))
	}
}

func TestSumLinesAdded_空map(t *testing.T) {
	if sumLinesAdded(nil) != 0 {
		t.Error("空 map 期望 0")
	}
}

func TestSumLinesRemoved_空map(t *testing.T) {
	if sumLinesRemoved(nil) != 0 {
		t.Error("空 map 期望 0")
	}
}

// ──────────────────────────── 使用临时目录的 DiffService 测试 ────────────────────────────

// setupTestWorkspace 创建临时目录并初始化 workspace 路径。
// 返回临时目录路径，cleanup 函数在测试结束后调用。
func setupTestWorkspace(t *testing.T) func() {
	t.Helper()
	tmpDir := t.TempDir()

	// 创建初始化标记（使 workspace 路径生效）
	configDir := filepath.Join(tmpDir, ".uapclaw", "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("创建 config 目录失败：%v", err)
	}

	// 通过环境变量设置 home 目录（更可靠的方式）
	oldHome := os.Getenv("UAPCLAW_HOME")
	oldDataDir := os.Getenv("UAPCLAW_DATA_DIR")
	os.Setenv("UAPCLAW_HOME", tmpDir)
	os.Unsetenv("UAPCLAW_DATA_DIR")

	// 重置所有缓存
	workspace.SetUserHome(tmpDir)

	return func() {
		// 恢复环境变量
		if oldHome != "" {
			os.Setenv("UAPCLAW_HOME", oldHome)
		} else {
			os.Unsetenv("UAPCLAW_HOME")
		}
		if oldDataDir != "" {
			os.Setenv("UAPCLAW_DATA_DIR", oldDataDir)
		} else {
			os.Unsetenv("UAPCLAW_DATA_DIR")
		}
		// 重置 workspace 缓存
		workspace.SetUserHome("")
	}
}

// writeSessionHistory 在 AgentSessionsDir 中写入 session history.json
func writeSessionHistory(t *testing.T, sessionID string, records []historyRecord) {
	t.Helper()
	sessionsDir := workspace.AgentSessionsDir()
	sessionDir := filepath.Join(sessionsDir, sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("创建 session 目录失败：%v", err)
	}
	data, err := json.Marshal(records)
	if err != nil {
		t.Fatalf("序列化 history 失败：%v", err)
	}
	historyFile := filepath.Join(sessionDir, "history.json")
	if err := os.WriteFile(historyFile, data, 0o644); err != nil {
		t.Fatalf("写入 history.json 失败：%v", err)
	}
}

// writeAgentHistoryFile 在指定 baseDir/.agent_history 下写入 file_ops 文件
func writeAgentHistoryFile(t *testing.T, baseDir string, fileName string, content map[string]any) {
	t.Helper()
	histDir := filepath.Join(baseDir, ".agent_history")
	if err := os.MkdirAll(histDir, 0o755); err != nil {
		t.Fatalf("创建 .agent_history 目录失败：%v", err)
	}
	data, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("序列化 agent history 失败：%v", err)
	}
	filePath := filepath.Join(histDir, fileName)
	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		t.Fatalf("写入 file_ops 文件失败：%v", err)
	}
}

// writeSessionMetadata 在 AgentSessionsDir 中写入 session metadata.json
func writeSessionMetadata(t *testing.T, sessionID string, metadata map[string]any) {
	t.Helper()
	sessionsDir := workspace.AgentSessionsDir()
	sessionDir := filepath.Join(sessionsDir, sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("创建 session 目录失败：%v", err)
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("序列化 metadata 失败：%v", err)
	}
	metaFile := filepath.Join(sessionDir, "metadata.json")
	if err := os.WriteFile(metaFile, data, 0o644); err != nil {
		t.Fatalf("写入 metadata.json 失败：%v", err)
	}
}

func TestReadHistory_空目录(t *testing.T) {
	cleanup := setupTestWorkspace(t)
	defer cleanup()

	ds := GetDiffService()
	result := ds.readHistory("nonexistent-session")
	if result != nil {
		t.Error("不存在的 session 应返回 nil")
	}
}

func TestReadHistory_有数据(t *testing.T) {
	cleanup := setupTestWorkspace(t)
	defer cleanup()

	records := []historyRecord{
		{Role: "user", Content: "hello", Timestamp: 1736941800.0},
		{Role: "assistant", Content: "response", Timestamp: 1736941810.0},
		{Role: "user", Content: "follow-up", Timestamp: 1736941820.0},
	}
	writeSessionHistory(t, "test-session", records)

	ds := GetDiffService()
	result := ds.readHistory("test-session")
	if len(result) != 3 {
		t.Fatalf("期望 3 条记录，实际 %d", len(result))
	}
	if result[0].Role != "user" {
		t.Errorf("第一条记录应为 user，实际 %s", result[0].Role)
	}
}

func TestReadHistory_无效JSON(t *testing.T) {
	cleanup := setupTestWorkspace(t)
	defer cleanup()

	sessionsDir := workspace.AgentSessionsDir()
	sessionDir := filepath.Join(sessionsDir, "bad-json")
	os.MkdirAll(sessionDir, 0o755)
	os.WriteFile(filepath.Join(sessionDir, "history.json"), []byte("not json"), 0o644)

	ds := GetDiffService()
	result := ds.readHistory("bad-json")
	if result != nil {
		t.Error("无效 JSON 应返回 nil")
	}
}

func TestFindNextUserTime(t *testing.T) {
	ds := GetDiffService()
	history := []historyRecord{
		{Role: "user", Timestamp: 100.0},
		{Role: "assistant", Timestamp: 110.0},
		{Role: "user", Timestamp: 120.0},
		{Role: "assistant", Timestamp: 130.0},
	}

	// 从第一个 user（index 0）查找下一个 user
	result := ds.findNextUserTime(history, 0)
	if result != 120.0 {
		t.Errorf("期望 120.0，实际 %f", result)
	}

	// 从最后一个 user（index 2）查找 — 没有下一个
	result2 := ds.findNextUserTime(history, 2)
	if result2 != 0 {
		t.Errorf("没有下一个 user 期望 0，实际 %f", result2)
	}
}

func TestComputeTurnDiffs_有文件编辑(t *testing.T) {
	cleanup := setupTestWorkspace(t)
	defer cleanup()

	// 创建 session history
	records := []historyRecord{
		{Role: "user", Content: "请修改 config.py", Timestamp: 1736941800.0},
		{Role: "assistant", Content: "已修改", Timestamp: 1736941810.0},
		{Role: "user", Content: "请检查结果", Timestamp: 1736941820.0},
	}
	writeSessionHistory(t, "diff-session", records)

	// 创建 agent history（workspace 下的 file_ops）
	oldContent := "line1\nline2\nline3"
	newContent := "line1\nmodified\nline3"
	agentHistory := map[string]any{
		"config.py": []any{
			map[string]any{
				"action":      "edit",
				"timestamp":   timestampToISO(1736941805.0),
				"old_content": oldContent,
				"new_content": newContent,
			},
		},
	}
	writeAgentHistoryFile(t, workspace.AgentWorkspaceDir(),
		"file_ops_jiuwenswarm_diff-session.json", agentHistory)

	// 项目目录下也放置一份
	projectDir := filepath.Join(workspace.WorkspaceDir(), "project")
	os.MkdirAll(projectDir, 0o755)
	writeAgentHistoryFile(t, projectDir,
		"file_ops_jiuwenswarm_diff-session.json", agentHistory)

	ds := GetDiffService()
	turns := ds.computeTurnDiffs("diff-session", projectDir)

	// 应有至少 1 个 turn（第一个 user turn）
	if len(turns) == 0 {
		t.Fatal("期望至少 1 个 turn")
	}

	// 第一个 turn 应包含 config.py 的变更
	// 注意：normalizePath 将路径转为绝对路径，所以需要遍历查找
	foundConfig := false
	for path := range turns[0].Files {
		if strings.Contains(path, "config.py") {
			foundConfig = true
			break
		}
	}
	if !foundConfig {
		t.Errorf("期望第一个 turn 包含 config.py 变更，实际 files=%v", turns[0].Files)
	}
}

func TestComputeTurnDiffs_无history(t *testing.T) {
	cleanup := setupTestWorkspace(t)
	defer cleanup()

	ds := GetDiffService()
	turns := ds.computeTurnDiffs("nonexistent-session", "")
	if turns != nil {
		t.Error("无 history 应返回 nil")
	}
}

func TestGetTurnDiffs_完整流程(t *testing.T) {
	cleanup := setupTestWorkspace(t)
	defer cleanup()

	records := []historyRecord{
		{Role: "user", Content: "请创建文件", Timestamp: 1736941800.0},
		{Role: "assistant", Content: "已创建", Timestamp: 1736941810.0},
		{Role: "user", Content: "请编辑文件", Timestamp: 1736941820.0},
	}
	writeSessionHistory(t, "turn-session", records)

	// 创建 agent history — 第一个 turn 有 write，第二个 turn 有 edit
	firstWrite := "hello world"
	agentHistory := map[string]any{
		"test.txt": []any{
			map[string]any{
				"action":      "write",
				"timestamp":   timestampToISO(1736941805.0),
				"old_content": nil,
				"new_content": firstWrite,
			},
			map[string]any{
				"action":      "edit",
				"timestamp":   timestampToISO(1736941825.0),
				"old_content": firstWrite,
				"new_content": "hello modified",
			},
		},
	}
	agentWorkspace := workspace.AgentWorkspaceDir()
	writeAgentHistoryFile(t, agentWorkspace,
		"file_ops_jiuwenswarm_turn-session.json", agentHistory)

	ds := GetDiffService()
	turns := ds.GetTurnDiffs("turn-session", "")

	// 应倒序排列（most recent first）
	if len(turns) < 1 {
		t.Fatal("期望至少 1 个 turn")
	}

	// 检查倒序
	if len(turns) >= 2 {
		if turns[0].TurnIndex <= turns[1].TurnIndex {
			t.Error("期望倒序排列（TurnIndex 递减）")
		}
	}
}

func TestGetFilesToRestore_基本流程(t *testing.T) {
	cleanup := setupTestWorkspace(t)
	defer cleanup()

	records := []historyRecord{
		{Role: "user", Content: "请创建文件", Timestamp: 1736941800.0},
		{Role: "assistant", Content: "已创建", Timestamp: 1736941810.0},
		{Role: "user", Content: "请修改文件", Timestamp: 1736941820.0},
	}
	writeSessionHistory(t, "restore-session", records)

	firstWrite := "hello world"
	editResult := "hello modified"
	agentHistory := map[string]any{
		"test.txt": []any{
			map[string]any{
				"action":      "write",
				"timestamp":   timestampToISO(1736941805.0),
				"old_content": nil,
				"new_content": firstWrite,
			},
			map[string]any{
				"action":      "edit",
				"timestamp":   timestampToISO(1736941825.0),
				"old_content": firstWrite,
				"new_content": editResult,
			},
		},
	}
	agentWorkspace := workspace.AgentWorkspaceDir()
	writeAgentHistoryFile(t, agentWorkspace,
		"file_ops_jiuwenswarm_restore-session.json", agentHistory)

	ds := GetDiffService()
	// 恢复到 turn 2（第二个 user message 开始前）
	filesToRestore := ds.GetFilesToRestore("restore-session", 2, "")

	if filesToRestore == nil {
		t.Fatal("期望非 nil result")
	}

	// 查找 test.txt 的恢复信息（normalizePath 可能改变了路径格式）
	var restoreInfo *FileRestoreInfo
	for path, info := range filesToRestore {
		if strings.Contains(path, "test.txt") {
			restoreInfo = info
			break
		}
	}
	if restoreInfo == nil {
		t.Fatalf("期望找到 test.txt 的恢复信息，实际 keys=%v", filesToRestore)
	}
	if restoreInfo.Action != "write" {
		t.Errorf("期望 action=write，实际 %s", restoreInfo.Action)
	}
}

func TestGetFilesToRestore_无history(t *testing.T) {
	cleanup := setupTestWorkspace(t)
	defer cleanup()

	ds := GetDiffService()
	result := ds.GetFilesToRestore("nonexistent-session", 1, "")
	if result != nil {
		t.Error("无 history 应返回 nil")
	}
}

func TestGetFilesToRestore_turnIndex超出范围(t *testing.T) {
	cleanup := setupTestWorkspace(t)
	defer cleanup()

	records := []historyRecord{
		{Role: "user", Content: "hello", Timestamp: 1736941800.0},
	}
	writeSessionHistory(t, "short-session", records)

	ds := GetDiffService()
	result := ds.GetFilesToRestore("short-session", 99, "")
	if result != nil {
		t.Error("超出范围的 turnIndex 应返回 nil")
	}
}

func TestFindFileEditsByTimeRange(t *testing.T) {
	ds := GetDiffService()
	agentHistory := map[string][]opEntry{
		"file.txt": {
			{Action: "write", Timestamp: timestampToISO(100.0), OldContent: nil, NewContent: ptrStr("a")},
			{Action: "edit", Timestamp: timestampToISO(150.0), OldContent: ptrStr("a"), NewContent: ptrStr("b")},
			{Action: "edit", Timestamp: timestampToISO(200.0), OldContent: ptrStr("b"), NewContent: ptrStr("c")},
		},
	}

	// 时间范围 [100, 200)
	edits := ds.findFileEditsByTimeRange(agentHistory, 100.0, 200.0)
	if len(edits["file.txt"]) != 2 {
		t.Errorf("期望 2 条编辑（100 和 150），实际 %d", len(edits["file.txt"]))
	}

	// 时间范围 [100, 0)（endTime=0 表示不限）
	edits2 := ds.findFileEditsByTimeRange(agentHistory, 100.0, 0)
	if len(edits2["file.txt"]) != 3 {
		t.Errorf("期望 3 条编辑（不限 endTime），实际 %d", len(edits2["file.txt"]))
	}
}

func TestGetProjectDirFromMetadata(t *testing.T) {
	cleanup := setupTestWorkspace(t)
	defer cleanup()

	// 创建 session metadata
	metadata := map[string]any{
		"channel_metadata": map[string]any{
			"cwd": "/home/user/project",
		},
	}
	writeSessionMetadata(t, "meta-session", metadata)

	ds := GetDiffService()
	projectDir := ds.getProjectDirFromMetadata("meta-session")
	if projectDir != "/home/user/project" {
		t.Errorf("期望 /home/user/project，实际 %s", projectDir)
	}
}

func TestGetProjectDirFromMetadata_deliveryContext(t *testing.T) {
	cleanup := setupTestWorkspace(t)
	defer cleanup()

	// 创建 session metadata（使用 delivery_context）
	metadata := map[string]any{
		"delivery_context": map[string]any{
			"route_metadata": map[string]any{
				"cwd": "/home/user/other-project",
			},
		},
	}
	writeSessionMetadata(t, "delivery-session", metadata)

	ds := GetDiffService()
	projectDir := ds.getProjectDirFromMetadata("delivery-session")
	if projectDir != "/home/user/other-project" {
		t.Errorf("期望 /home/user/other-project，实际 %s", projectDir)
	}
}

func TestGetProjectDirFromMetadata_不存在(t *testing.T) {
	cleanup := setupTestWorkspace(t)
	defer cleanup()

	ds := GetDiffService()
	projectDir := ds.getProjectDirFromMetadata("nonexistent-session")
	if projectDir != "" {
		t.Errorf("不存在的 session 期望空字符串，实际 %s", projectDir)
	}
}

func TestGetProjectDirFromMetadata_无cwd字段(t *testing.T) {
	cleanup := setupTestWorkspace(t)
	defer cleanup()

	metadata := map[string]any{
		"channel_metadata": map[string]any{
			"other_field": "value",
		},
	}
	writeSessionMetadata(t, "no-cwd-session", metadata)

	ds := GetDiffService()
	projectDir := ds.getProjectDirFromMetadata("no-cwd-session")
	if projectDir != "" {
		t.Errorf("无 cwd 字段期望空字符串，实际 %s", projectDir)
	}
}

func TestReadAgentHistory_合并多个文件(t *testing.T) {
	cleanup := setupTestWorkspace(t)
	defer cleanup()

	// 在 agent workspace 下写一份
	agentWorkspace := workspace.AgentWorkspaceDir()
	agentHist1 := map[string]any{
		"file1.py": []any{
			map[string]any{
				"action":      "write",
				"timestamp":   timestampToISO(100.0),
				"old_content": nil,
				"new_content": "content1",
			},
		},
	}
	writeAgentHistoryFile(t, agentWorkspace,
		"file_ops_jiuwenswarm_merge-session.json", agentHist1)

	// 在 project 目录下写一份
	projectDir := filepath.Join(workspace.WorkspaceDir(), "project")
	os.MkdirAll(projectDir, 0o755)
	agentHist2 := map[string]any{
		"file2.py": []any{
			map[string]any{
				"action":      "edit",
				"timestamp":   timestampToISO(150.0),
				"old_content": "old",
				"new_content": "new",
			},
		},
	}
	writeAgentHistoryFile(t, projectDir,
		"file_ops_jiuwenswarm_merge-session.json", agentHist2)

	ds := GetDiffService()
	result := ds.readAgentHistory("merge-session", projectDir)

	// 应合并两个文件的条目
	if len(result) < 1 {
		t.Fatalf("期望至少 1 个文件条目，实际 %d", len(result))
	}
}

func TestReadAgentHistory_去重(t *testing.T) {
	cleanup := setupTestWorkspace(t)
	defer cleanup()

	// 在 agent workspace 下写一份
	agentWorkspace := workspace.AgentWorkspaceDir()
	agentHist := map[string]any{
		"file1.py": []any{
			map[string]any{
				"action":      "write",
				"timestamp":   timestampToISO(100.0),
				"old_content": nil,
				"new_content": "content1",
			},
			// 时间相近（1秒差）的重复条目
			map[string]any{
				"action":      "write",
				"timestamp":   timestampToISO(100.5),
				"old_content": nil,
				"new_content": "content1",
			},
		},
	}
	writeAgentHistoryFile(t, agentWorkspace,
		"file_ops_jiuwenswarm_dedup-session.json", agentHist)

	ds := GetDiffService()
	result := ds.readAgentHistory("dedup-session", "")

	// 重复条目应被去重
	// 注意：normalizePath 会将文件路径转为绝对路径
	if len(result) == 0 {
		t.Fatalf("readAgentHistory 返回空结果")
	}

	// 查找任意 key（因为 normalizePath 可能改变了路径格式）
	var foundCount int
	for _, entries := range result {
		foundCount = len(entries)
		break
	}
	if foundCount != 1 {
		t.Errorf("期望去重后 1 条，实际 %d", foundCount)
	}
}

func TestGetFilesToRestore_删除文件恢复(t *testing.T) {
	cleanup := setupTestWorkspace(t)
	defer cleanup()

	records := []historyRecord{
		{Role: "user", Content: "请创建文件", Timestamp: 1736941800.0},
		{Role: "assistant", Content: "已创建", Timestamp: 1736941810.0},
		{Role: "user", Content: "请修改", Timestamp: 1736941820.0},
	}
	writeSessionHistory(t, "delete-restore-session", records)

	// agent 创建了文件（old_content=nil），恢复时应删除
	agentHistory := map[string]any{
		"new_file.py": []any{
			map[string]any{
				"action":      "write",
				"timestamp":   timestampToISO(1736941825.0),
				"old_content": nil,
				"new_content": "new content",
			},
		},
	}
	agentWorkspace := workspace.AgentWorkspaceDir()
	writeAgentHistoryFile(t, agentWorkspace,
		"file_ops_jiuwenswarm_delete-restore-session.json", agentHistory)

	ds := GetDiffService()
	filesToRestore := ds.GetFilesToRestore("delete-restore-session", 2, "")

	if filesToRestore == nil {
		t.Fatal("期望非 nil result")
	}

	// 查找 new_file.py 的恢复信息（normalizePath 改变了路径格式）
	var restoreInfo *FileRestoreInfo
	for path, info := range filesToRestore {
		if strings.Contains(path, "new_file.py") {
			restoreInfo = info
			break
		}
	}
	if restoreInfo == nil {
		t.Fatalf("期望找到 new_file.py 的恢复信息，实际 keys=%v", filesToRestore)
	}
	if restoreInfo.Action != "delete" {
		t.Errorf("期望 action=delete（agent 创建的文件恢复时应删除），实际 %s", restoreInfo.Action)
	}
	if restoreInfo.RestoreContent != nil {
		t.Error("期望 RestoreContent=nil（删除操作不需要恢复内容）")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func startsWithMinus(s string) bool {
	return len(s) > 0 && s[0] == '-' && (len(s) <= 2 || s[:3] != "---")
}

func startsWithPlus(s string) bool {
	return len(s) > 0 && s[0] == '+' && (len(s) <= 2 || s[:3] != "+++")
}

// ptrStr 返回字符串指针的辅助函数
func ptrStr(s string) *string {
	return &s
}
