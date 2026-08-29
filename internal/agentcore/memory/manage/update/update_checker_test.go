package update

import (
	"testing"
)

func TestMemUpdateChecker_Check_StubReturnsAllAdd(t *testing.T) {
	checker := &MemUpdateChecker{}
	newMemories := map[string]string{
		"mem-1": "用户喜欢阅读",
		"mem-2": "用户是工程师",
	}
	oldMemories := map[string]string{
		"old-1": "用户喜欢读书",
	}
	result, err := checker.Check(newMemories, oldMemories)
	if err != nil {
		t.Fatalf("不期望返回 error，但得到 %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("期望返回 2 个 action item，得到 %d", len(result))
	}
	for _, item := range result {
		if item.Status != MemoryStatusAdd {
			t.Errorf("item.ID=%s, Status=%d, want MemoryStatusAdd", item.ID, item.Status)
		}
		if _, ok := newMemories[item.ID]; !ok {
			t.Errorf("item.ID=%s 不在 newMemories 中", item.ID)
		}
	}
}

func TestMemUpdateChecker_Check_EmptyNewMemories(t *testing.T) {
	checker := &MemUpdateChecker{}
	newMemories := map[string]string{}
	oldMemories := map[string]string{"old-1": "old content"}
	result, err := checker.Check(newMemories, oldMemories)
	if err != nil {
		t.Fatalf("不期望返回 error，但得到 %v", err)
	}
	if len(result) != 0 {
		t.Errorf("期望返回 0 个 action item，得到 %d", len(result))
	}
}

func TestCheckResultString(t *testing.T) {
	if CheckResultRedundant.String() != "redundant" {
		t.Errorf("CheckResultRedundant.String() = %q, want %q", CheckResultRedundant.String(), "redundant")
	}
	if CheckResultConflicting.String() != "conflicting" {
		t.Errorf("CheckResultConflicting.String() = %q, want %q", CheckResultConflicting.String(), "conflicting")
	}
	if CheckResultNone.String() != "none" {
		t.Errorf("CheckResultNone.String() = %q, want %q", CheckResultNone.String(), "none")
	}
}

func TestMemoryStatusString(t *testing.T) {
	if MemoryStatusAdd.String() != "add" {
		t.Errorf("MemoryStatusAdd.String() = %q, want %q", MemoryStatusAdd.String(), "add")
	}
	if MemoryStatusDelete.String() != "delete" {
		t.Errorf("MemoryStatusDelete.String() = %q, want %q", MemoryStatusDelete.String(), "delete")
	}
}
