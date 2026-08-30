package update

import (
	"testing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestFormatInput 测试格式化新旧记忆为提示词输入文本
func TestFormatInput(t *testing.T) {
	newMem := map[string]string{"2": "新内容2", "1": "新内容1", "3": "新内容3"}
	oldMem := map[string]string{"b": "旧内容b", "a": "旧内容a"}

	newStr, oldStr := formatInput(newMem, oldMem)

	// 新记忆应倒序排列
	if newStr != "3: 新内容3\n2: 新内容2\n1: 新内容1" {
		t.Errorf("newStr = %q, want 倒序排列", newStr)
	}
	// 旧记忆应正序排列
	if oldStr != "a: 旧内容a\nb: 旧内容b" {
		t.Errorf("oldStr = %q, want 正序排列", oldStr)
	}
}

// TestFormatInput_空输入 测试空输入
func TestFormatInput_空输入(t *testing.T) {
	newStr, oldStr := formatInput(nil, nil)
	if newStr != "" {
		t.Errorf("newStr = %q, want empty", newStr)
	}
	if oldStr != "" {
		t.Errorf("oldStr = %q, want empty", oldStr)
	}
}

// TestMapCheckItemsToActionItems_冗余跳过 测试 REDUNDANT 不产生 action
func TestMapCheckItemsToActionItems_冗余跳过(t *testing.T) {
	items := []*MemCheckItem{
		{InfoID: "1", InfoText: "内容1", Result: CheckResultRedundant, RelatedInfos: nil},
	}
	newMem := map[string]string{"1": "内容1"}

	actionItems := mapCheckItemsToActionItems(items, newMem)
	if len(actionItems) != 0 {
		t.Errorf("冗余项不应产生 action，实际 %d 项", len(actionItems))
	}
}

// TestMapCheckItemsToActionItems_冲突 测试 CONFLICTING 产生 ADD+DELETE
func TestMapCheckItemsToActionItems_冲突(t *testing.T) {
	items := []*MemCheckItem{
		{
			InfoID:   "2",
			InfoText: "新内容2",
			Result:   CheckResultConflicting,
			RelatedInfos: map[string]string{
				"1": "旧内容1",
			},
		},
	}
	newMem := map[string]string{"2": "新内容2"}

	actionItems := mapCheckItemsToActionItems(items, newMem)
	if len(actionItems) != 2 {
		t.Fatalf("冲突项应产生 2 个 action，实际 %d 项", len(actionItems))
	}
	// 第一个应为 ADD（新记忆）
	if actionItems[0].ID != "2" || actionItems[0].Status != MemoryStatusAdd {
		t.Errorf("第一个 action 应为 ADD(id=2), 实际 ID=%s Status=%s", actionItems[0].ID, actionItems[0].Status)
	}
	// 第二个应为 DELETE（旧记忆）
	if actionItems[1].ID != "1" || actionItems[1].Status != MemoryStatusDelete {
		t.Errorf("第二个 action 应为 DELETE(id=1), 实际 ID=%s Status=%s", actionItems[1].ID, actionItems[1].Status)
	}
}

// TestMapCheckItemsToActionItems_共存 测试 NONE 仅 ADD
func TestMapCheckItemsToActionItems_共存(t *testing.T) {
	items := []*MemCheckItem{
		{InfoID: "3", InfoText: "独立内容", Result: CheckResultNone, RelatedInfos: nil},
	}
	newMem := map[string]string{"3": "独立内容"}

	actionItems := mapCheckItemsToActionItems(items, newMem)
	if len(actionItems) != 1 {
		t.Fatalf("共存项应产生 1 个 action，实际 %d 项", len(actionItems))
	}
	if actionItems[0].ID != "3" || actionItems[0].Status != MemoryStatusAdd {
		t.Errorf("action 应为 ADD(id=3), 实际 ID=%s Status=%s", actionItems[0].ID, actionItems[0].Status)
	}
}

// TestMapCheckItemsToActionItems_内容fallback 测试 newMem 中无对应 ID 时使用 InfoText
func TestMapCheckItemsToActionItems_内容Fallback(t *testing.T) {
	items := []*MemCheckItem{
		{InfoID: "x", InfoText: "来自LLM的内容", Result: CheckResultNone, RelatedInfos: nil},
	}
	newMem := map[string]string{} // 不含 "x"

	actionItems := mapCheckItemsToActionItems(items, newMem)
	if len(actionItems) != 1 {
		t.Fatalf("应产生 1 个 action，实际 %d 项", len(actionItems))
	}
	if actionItems[0].Content != "来自LLM的内容" {
		t.Errorf("Content = %q, want %q", actionItems[0].Content, "来自LLM的内容")
	}
}

// TestParseCheckResult 测试枚举解析
func TestParseCheckResult(t *testing.T) {
	tests := []struct {
		input string
		want  CheckResult
	}{
		{"redundant", CheckResultRedundant},
		{"conflicting", CheckResultConflicting},
		{"none", CheckResultNone},
		{"unknown", CheckResultNone},
		{"", CheckResultNone},
	}
	for _, tt := range tests {
		got := parseCheckResult(tt.input)
		if got != tt.want {
			t.Errorf("parseCheckResult(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

// TestParseCheckItems_单对象 测试 map 输入
func TestParseCheckItems_单对象(t *testing.T) {
	parsed := map[string]any{
		"info_id":       "1",
		"info_text":     "内容",
		"result":        "none",
		"related_infos": map[string]any{},
	}

	items, err := parseCheckItems(parsed)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("应解析 1 项，实际 %d 项", len(items))
	}
	if items[0].InfoID != "1" {
		t.Errorf("InfoID = %q, want %q", items[0].InfoID, "1")
	}
	if items[0].Result != CheckResultNone {
		t.Errorf("Result = %v, want %v", items[0].Result, CheckResultNone)
	}
}

// TestParseCheckItems_数组 测试 slice 输入
func TestParseCheckItems_数组(t *testing.T) {
	parsed := []any{
		map[string]any{
			"info_id":       "1",
			"info_text":     "内容1",
			"result":        "redundant",
			"related_infos": map[string]any{"a": "旧1"},
		},
		map[string]any{
			"info_id":       "2",
			"info_text":     "内容2",
			"result":        "conflicting",
			"related_infos": map[string]any{},
		},
	}

	items, err := parseCheckItems(parsed)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("应解析 2 项，实际 %d 项", len(items))
	}
	if items[0].Result != CheckResultRedundant {
		t.Errorf("items[0].Result = %v, want %v", items[0].Result, CheckResultRedundant)
	}
	if items[1].Result != CheckResultConflicting {
		t.Errorf("items[1].Result = %v, want %v", items[1].Result, CheckResultConflicting)
	}
	if items[0].RelatedInfos["a"] != "旧1" {
		t.Errorf("items[0].RelatedInfos = %v, want 包含 a:旧1", items[0].RelatedInfos)
	}
}

// TestParseCheckItems_空数组 测试空数组返回错误
func TestParseCheckItems_空数组(t *testing.T) {
	parsed := []any{}

	_, err := parseCheckItems(parsed)
	if err == nil {
		t.Error("空数组应返回错误")
	}
}

// TestParseCheckItems_不支持的类型 测试非 map/slice 输入
func TestParseCheckItems_不支持的类型(t *testing.T) {
	_, err := parseCheckItems("invalid")
	if err == nil {
		t.Error("不支持的类型应返回错误")
	}
}

// TestAllAddItems 测试 fallback 函数
func TestAllAddItems(t *testing.T) {
	mem := map[string]string{"a": "内容a", "b": "内容b"}

	items := allAddItems(mem)
	if len(items) != 2 {
		t.Fatalf("应产生 2 项，实际 %d 项", len(items))
	}
	for _, item := range items {
		if item.Status != MemoryStatusAdd {
			t.Errorf("item %s Status = %v, want Add", item.ID, item.Status)
		}
	}
}

// TestCheckDuplicateIDs 测试重复 ID 检测
func TestCheckDuplicateIDs(t *testing.T) {
	newMem := map[string]string{"1": "a", "2": "b", "3": "c"}
	oldMem := map[string]string{"2": "old_b", "4": "d"}

	dupes := checkDuplicateIDs(newMem, oldMem)
	if len(dupes) != 1 || dupes[0] != "2" {
		t.Errorf("dupes = %v, want [2]", dupes)
	}
}

// TestCheckDuplicateIDs_无重复 测试无重复
func TestCheckDuplicateIDs_无重复(t *testing.T) {
	newMem := map[string]string{"1": "a"}
	oldMem := map[string]string{"2": "b"}

	dupes := checkDuplicateIDs(newMem, oldMem)
	if len(dupes) != 0 {
		t.Errorf("dupes = %v, want empty", dupes)
	}
}

// TestCheck_无模型 测试无 LLM 模型时返回全部 ADD
func TestCheck_无模型(t *testing.T) {
	checker := &MemUpdateChecker{}
	newMem := map[string]string{"1": "内容1", "2": "内容2"}

	items, err := checker.Check(nil, newMem, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("无模型时应返回 2 项 ADD，实际 %d 项", len(items))
	}
	for _, item := range items {
		if item.Status != MemoryStatusAdd {
			t.Errorf("item %s Status = %v, want Add", item.ID, item.Status)
		}
	}
}

// TestCheckResult_String 测试枚举 String 方法
func TestCheckResult_String(t *testing.T) {
	if CheckResultRedundant.String() != "redundant" {
		t.Errorf("CheckResultRedundant.String() = %q", CheckResultRedundant.String())
	}
	if CheckResultConflicting.String() != "conflicting" {
		t.Errorf("CheckResultConflicting.String() = %q", CheckResultConflicting.String())
	}
	if CheckResultNone.String() != "none" {
		t.Errorf("CheckResultNone.String() = %q", CheckResultNone.String())
	}
}

// TestMemoryStatus_String 测试枚举 String 方法
func TestMemoryStatus_String(t *testing.T) {
	if MemoryStatusAdd.String() != "add" {
		t.Errorf("MemoryStatusAdd.String() = %q", MemoryStatusAdd.String())
	}
	if MemoryStatusDelete.String() != "delete" {
		t.Errorf("MemoryStatusDelete.String() = %q", MemoryStatusDelete.String())
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
