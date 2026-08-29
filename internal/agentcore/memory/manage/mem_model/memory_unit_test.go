package mem_model

import (
	"testing"
)

func TestMemoryTypeString(t *testing.T) {
	tests := []struct {
		mt       MemoryType
		expected string
	}{
		{MemoryTypeUserProfile, "user_profile"},
		{MemoryTypeSemanticMemory, "semantic_memory"},
		{MemoryTypeEpisodicMemory, "episodic_memory"},
		{MemoryTypeVariable, "variable"},
		{MemoryTypeSummary, "summary"},
		{MemoryTypeUnknown, "unknown"},
	}
	for _, tt := range tests {
		if got := tt.mt.String(); got != tt.expected {
			t.Errorf("MemoryType(%d).String() = %q, want %q", tt.mt, got, tt.expected)
		}
	}
}

func TestOperationTypeString(t *testing.T) {
	tests := []struct {
		ot       OperationType
		expected string
	}{
		{OperationTypeAdd, "add"},
		{OperationTypeUpdate, "update"},
		{OperationTypeDelete, "delete"},
	}
	for _, tt := range tests {
		if got := tt.ot.String(); got != tt.expected {
			t.Errorf("OperationType(%d).String() = %q, want %q", tt.ot, got, tt.expected)
		}
	}
}

func TestParseMemoryType(t *testing.T) {
	tests := []struct {
		input    string
		expected MemoryType
	}{
		{"user_profile", MemoryTypeUserProfile},
		{"semantic_memory", MemoryTypeSemanticMemory},
		{"episodic_memory", MemoryTypeEpisodicMemory},
		{"variable", MemoryTypeVariable},
		{"summary", MemoryTypeSummary},
		{"unknown", MemoryTypeUnknown},
		{"nonexistent", MemoryTypeUnknown},
	}
	for _, tt := range tests {
		if got := ParseMemoryType(tt.input); got != tt.expected {
			t.Errorf("ParseMemoryType(%q) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}

func TestParseOperationType(t *testing.T) {
	tests := []struct {
		input    string
		expected OperationType
	}{
		{"add", OperationTypeAdd},
		{"update", OperationTypeUpdate},
		{"delete", OperationTypeDelete},
		{"nonexistent", OperationTypeAdd},
	}
	for _, tt := range tests {
		if got := ParseOperationType(tt.input); got != tt.expected {
			t.Errorf("ParseOperationType(%q) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}

func TestFragmentMemoryUnitFields(t *testing.T) {
	unit := FragmentMemoryUnit{
		BaseMemoryUnit: BaseMemoryUnit{
			MemType: MemoryTypeUserProfile,
			MemID:   "test-id-001",
		},
		Content:       "用户喜欢阅读科幻小说",
		MessageMemID:  "msg-001",
		Timestamp:     "2027-04-01 12:00:00",
		OperationType: OperationTypeAdd,
	}
	if unit.MemType != MemoryTypeUserProfile {
		t.Errorf("MemType = %d, want %d", unit.MemType, MemoryTypeUserProfile)
	}
	if unit.MemID != "test-id-001" {
		t.Errorf("MemID = %q, want %q", unit.MemID, "test-id-001")
	}
	if unit.Content != "用户喜欢阅读科幻小说" {
		t.Errorf("Content = %q, want %q", unit.Content, "用户喜欢阅读科幻小说")
	}
	if unit.OperationType != OperationTypeAdd {
		t.Errorf("OperationType = %d, want %d", unit.OperationType, OperationTypeAdd)
	}
}

func TestVariableUnitFields(t *testing.T) {
	unit := VariableUnit{
		BaseMemoryUnit: BaseMemoryUnit{
			MemType: MemoryTypeVariable,
			MemID:   "var-001",
		},
		VariableName: "age",
		VariableMem:  "25",
	}
	if unit.MemType != MemoryTypeVariable {
		t.Errorf("MemType = %d, want %d", unit.MemType, MemoryTypeVariable)
	}
	if unit.VariableName != "age" {
		t.Errorf("VariableName = %q, want %q", unit.VariableName, "age")
	}
}

func TestSummaryUnitFields(t *testing.T) {
	unit := SummaryUnit{
		BaseMemoryUnit: BaseMemoryUnit{
			MemType: MemoryTypeSummary,
			MemID:   "sum-001",
		},
		Summary:      "用户偏好摘要",
		MessageMemID: "msg-001",
		Timestamp:    "2027-04-01 12:00:00",
	}
	if unit.MemType != MemoryTypeSummary {
		t.Errorf("MemType = %d, want %d", unit.MemType, MemoryTypeSummary)
	}
	if unit.Summary != "用户偏好摘要" {
		t.Errorf("Summary = %q, want %q", unit.Summary, "用户偏好摘要")
	}
}
