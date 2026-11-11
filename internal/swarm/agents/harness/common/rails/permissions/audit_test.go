package permissions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newAuditFacts 构造测试用决策事实。
//
// 注意 ToolDecisionFacts 没有 ToolName / ToolCategory 顶层字段，
// 工具名与分类都在 Capability 里（Capability.ToolName / Capability.Category）。
func newAuditFacts() ToolDecisionFacts {
	return ToolDecisionFacts{
		AccessesKnown: true,
		ReadPaths:     []string{"/tmp/a"},
		WritePaths:    []string{},
		ExternalPaths: []string{},
		Capability: ToolCapability{
			ToolName:          "bash",
			Category:          "shell",
			RiskTier:          "low",
			StaticSideEffects: map[string]struct{}{"execute": {}},
		},
	}
}

// TestEmitPermissionAudit_无写入器返回nil 测试 persistentWriter 为 nil 时直接返回。
func TestEmitPermissionAudit_无写入器返回nil(t *testing.T) {
	got := EmitPermissionAudit(newAuditFacts(), AuditOptions{
		Decision: "allow",
		Reason:   "test",
	}, nil)
	if got != nil {
		t.Errorf("got = %v, want nil", got)
	}
}

// TestEmitPermissionAudit_有写入器返回结果 测试正常落盘路径。
func TestEmitPermissionAudit_有写入器返回结果(t *testing.T) {
	dir := t.TempDir()
	writer := NewPersistentAuditWriter(dir)

	got := EmitPermissionAudit(newAuditFacts(), AuditOptions{
		Decision: "allow",
		Reason:   "test_reason",
	}, writer)

	if got == nil {
		t.Fatal("got = nil, want 写入结果")
	}
	if !got.Persisted {
		t.Errorf("Persisted = false, reason = %s", got.Reason)
	}
	path := filepath.Join(dir, AuditSubdirectory, AuditFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}
	if len(data) == 0 {
		t.Error("审计文件为空")
	}
	if data[len(data)-1] != '\n' {
		t.Error("记录必须以换行结尾")
	}
}

// TestEmitPermissionAudit_脱敏后不含原始参数 测试 observe-only 脱敏边界。
func TestEmitPermissionAudit_脱敏后不含原始参数(t *testing.T) {
	dir := t.TempDir()
	writer := NewPersistentAuditWriter(dir)

	_ = EmitPermissionAudit(newAuditFacts(), AuditOptions{
		Decision: "allow",
		Reason:   "bash -c 'curl http://x.com?token=secret'",
		Extra:    map[string]any{"decision_source": "deterministic", "unlisted": "should_be_dropped"},
	}, writer)

	path := filepath.Join(dir, AuditSubdirectory, AuditFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}
	content := string(data)

	if strings.Contains(content, "secret") {
		t.Error("审计记录泄露了原始命令内容")
	}
	if strings.Contains(content, "unlisted") {
		t.Error("非白名单键 unlisted 不应写入")
	}
	if !strings.Contains(content, "deterministic") {
		t.Error("白名单键 decision_source 应写入")
	}
}

// TestEmitPermissionAudit_日志段失败不影响落盘 测试两段独立异常边界。
func TestEmitPermissionAudit_日志段失败不影响落盘(t *testing.T) {
	dir := t.TempDir()
	writer := NewPersistentAuditWriter(dir)

	// Extra 里塞一个不可序列化的值：日志段应吞掉异常，落盘段仍成功
	got := EmitPermissionAudit(newAuditFacts(), AuditOptions{
		Decision: "allow",
		Reason:   "ok",
		Extra:    map[string]any{"authorization_stage": func() {}},
	}, writer)

	if got == nil || !got.Persisted {
		t.Error("日志段异常不应阻断持久化")
	}
}

// TestEmitPermissionAudit_写入失败不panic 测试落盘异常被吞。
func TestEmitPermissionAudit_写入失败不panic(t *testing.T) {
	writer := NewPersistentAuditWriter(nil) // data_root nil → 写失败

	got := EmitPermissionAudit(newAuditFacts(), AuditOptions{Decision: "allow"}, writer)

	// Python 语义：persistent_writer.write 的返回值直接透传
	if got != nil && got.Persisted {
		t.Error("Persisted = true, want false（data_root 为 nil）")
	}
}

// TestEmitPermissionAudit_空决策不panic 测试空选项。
func TestEmitPermissionAudit_空决策不panic(t *testing.T) {
	dir := t.TempDir()
	writer := NewPersistentAuditWriter(dir)

	got := EmitPermissionAudit(newAuditFacts(), AuditOptions{}, writer)
	if got == nil {
		t.Fatal("got = nil")
	}
}

// TestNormalizeForSortedJSON_各类载体 测试序列化归一化各分支。
func TestNormalizeForSortedJSON_各类载体(t *testing.T) {
	cases := []struct {
		name  string
		input any
	}{
		{"map 嵌套", map[string]any{"b": 1, "a": map[string]any{"d": 2, "c": 3}}},
		{"[]any", []any{map[string]any{"x": 1}, 2}},
		{"[]string", []string{"p", "q"}},
		{"标量字符串", "text"},
		{"标量数字", 42},
		{"nil", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := normalizeForSortedJSON(c.input)
			// 只验证不 panic 且可序列化
			if _, err := marshalSortedCompact(got); err != nil {
				t.Errorf("marshal 失败: %v", err)
			}
		})
	}
}

// TestMarshalSortedCompact_键有序 测试输出键按字典序。
func TestMarshalSortedCompact_键有序(t *testing.T) {
	got, err := marshalSortedCompact(map[string]any{
		"zebra": 1,
		"alpha": 2,
		"mike":  3,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	alphaIdx := strings.Index(got, "alpha")
	mikeIdx := strings.Index(got, "mike")
	zebraIdx := strings.Index(got, "zebra")
	if !(alphaIdx < mikeIdx && mikeIdx < zebraIdx) {
		t.Errorf("键未按字典序输出: %s", got)
	}
}

// TestMarshalSortedCompact_无空格 测试 separators=(",",":") 效果。
func TestMarshalSortedCompact_无空格(t *testing.T) {
	got, err := marshalSortedCompact(map[string]any{"a": 1, "b": 2})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(got, ": ") || strings.Contains(got, ", ") {
		t.Errorf("输出含空格: %s", got)
	}
}
