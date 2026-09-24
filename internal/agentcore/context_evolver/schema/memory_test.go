package schema

import (
	"encoding/json"
	"testing"
	"time"

	coreschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/schema"
)

// ──────────────────────────── BaseMemory ────────────────────────────

func TestBaseMemory_GetWorkspaceID(t *testing.T) {
	bm := BaseMemory{WorkspaceID: "user1"}
	if bm.GetWorkspaceID() != "user1" {
		t.Errorf("GetWorkspaceID() = %q, want %q", bm.GetWorkspaceID(), "user1")
	}
}

func TestBaseMemory_默认值(t *testing.T) {
	bm := BaseMemory{}
	if bm.WorkspaceID != "" {
		t.Errorf("默认 WorkspaceID = %q, want 空", bm.WorkspaceID)
	}
}

// ──────────────────────────── MemoryInterface ────────────────────────────

func TestACEMemory_实现MemoryInterface(t *testing.T) {
	var _ MemoryInterface = (*ACEMemory)(nil)
}

func TestReasoningBankMemory_实现MemoryInterface(t *testing.T) {
	var _ MemoryInterface = (*ReasoningBankMemory)(nil)
}

func TestReMeMemory_实现MemoryInterface(t *testing.T) {
	var _ MemoryInterface = (*ReMeMemory)(nil)
}

// ──────────────────────────── MemoryItem 接口 ────────────────────────────

func TestACEMemory_实现MemoryItem接口(t *testing.T) {
	var _ MemoryItem = (*ACEMemory)(nil)
}

// ──────────────────────────── FormatMemoryString ────────────────────────────

func TestACEMemory_FormatMemoryString(t *testing.T) {
	m := ACEMemory{
		BaseMemory: BaseMemory{WorkspaceID: "ws1"},
		ID:         "mem_001",
		Section:    "python",
		Content:    "Use lru_cache for memoization",
		Helpful:    3,
		Harmful:    1,
		Neutral:    2,
	}
	got := m.FormatMemoryString()
	want := "[mem_001] helpful=3 harmful=1 neutral=2\nSection: python\nContent: Use lru_cache for memoization"
	if got != want {
		t.Errorf("FormatMemoryString() = %q, want %q", got, want)
	}
}

func TestACEMemory_FormatMemoryString_零值(t *testing.T) {
	m := ACEMemory{ID: "empty_id"}
	got := m.FormatMemoryString()
	want := "[empty_id] helpful=0 harmful=0 neutral=0\nSection: \nContent: "
	if got != want {
		t.Errorf("FormatMemoryString() = %q, want %q", got, want)
	}
}

// ──────────────────────────── ACEMemory ────────────────────────────

func TestACEMemory_ToVectorNode(t *testing.T) {
	mem := ACEMemory{
		BaseMemory: BaseMemory{WorkspaceID: "ws1"},
		ID:         "mem_001",
		Section:    "python",
		Content:    "Use lru_cache for memoization",
		Helpful:    3,
		Harmful:    1,
		Neutral:    2,
		CreatedAt:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
	}
	node := mem.ToVectorNode()
	if node.Metadata["type"] != "ace_memory" {
		t.Errorf("type = %v, want ace_memory", node.Metadata["type"])
	}
	if node.Content != "Use lru_cache for memoization" {
		t.Errorf("embedding content = %q, want 原始 content", node.Content)
	}
	if node.Metadata["section"] != "python" {
		t.Errorf("section = %v, want python", node.Metadata["section"])
	}
	if node.ID != "ace_ws1_mem_001" {
		t.Errorf("ID = %q, want ace_ws1_mem_001", node.ID)
	}
}

func TestACEMemory_FromVectorNode_往返(t *testing.T) {
	original := ACEMemory{
		BaseMemory: BaseMemory{WorkspaceID: "ws1"},
		ID:         "mem_002",
		Section:    "go",
		Content:    "Use sync.Pool",
		Helpful:    5,
		Harmful:    0,
		Neutral:    1,
		CreatedAt:  time.Date(2024, 3, 15, 10, 30, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2024, 3, 16, 10, 30, 0, 0, time.UTC),
	}
	node := original.ToVectorNode()
	restored := NewACEMemoryFromVectorNode(node)
	if restored.ID != original.ID {
		t.Errorf("ID = %q, want %q", restored.ID, original.ID)
	}
	if restored.Section != original.Section {
		t.Errorf("Section = %q, want %q", restored.Section, original.Section)
	}
	if restored.Content != original.Content {
		t.Errorf("Content = %q, want %q", restored.Content, original.Content)
	}
	if restored.Helpful != original.Helpful {
		t.Errorf("Helpful = %d, want %d", restored.Helpful, original.Helpful)
	}
	if restored.WorkspaceID != original.WorkspaceID {
		t.Errorf("WorkspaceID = %q, want %q", restored.WorkspaceID, original.WorkspaceID)
	}
}

func TestACEMemory_GetWorkspaceID(t *testing.T) {
	mem := ACEMemory{BaseMemory: BaseMemory{WorkspaceID: "ws_ace"}}
	if mem.GetWorkspaceID() != "ws_ace" {
		t.Errorf("GetWorkspaceID() = %q, want %q", mem.GetWorkspaceID(), "ws_ace")
	}
}

func TestACEMemory_JSON往返(t *testing.T) {
	original := ACEMemory{
		BaseMemory: BaseMemory{WorkspaceID: "ws1"},
		ID:         "mem_j",
		Section:    "test",
		Content:    "test content",
		Helpful:    1,
		CreatedAt:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ACEMemory
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.ID != original.ID {
		t.Errorf("ID = %q, want %q", restored.ID, original.ID)
	}
	if restored.WorkspaceID != original.WorkspaceID {
		t.Errorf("WorkspaceID = %q, want %q", restored.WorkspaceID, original.WorkspaceID)
	}
}

// ──────────────────────────── ReasoningBankMemory ────────────────────────────

func TestReasoningBankMemory_ToVectorNode(t *testing.T) {
	label := true
	mem := ReasoningBankMemory{
		BaseMemory: BaseMemory{WorkspaceID: "ws2"},
		Query:      "How to implement caching?",
		Memory: []ReasoningBankMemoryItem{
			{Title: "Python Caching", Description: "Memoization technique", Content: "Use functools.lru_cache"},
		},
		Label: &label,
	}
	node := mem.ToVectorNode()
	if node.Metadata["type"] != "reasoning_bank_memory" {
		t.Errorf("type = %v, want reasoning_bank_memory", node.Metadata["type"])
	}
	if node.Content != "How to implement caching?" {
		t.Errorf("embedding content = %q, want query", node.Content)
	}
	if node.Metadata["query"] != "How to implement caching?" {
		t.Errorf("query = %v, want 原始 query", node.Metadata["query"])
	}
}

func TestReasoningBankMemory_ToVectorNode_空Memory列表(t *testing.T) {
	mem := ReasoningBankMemory{
		BaseMemory: BaseMemory{WorkspaceID: "ws2"},
		Query:      "test query",
		Memory:     []ReasoningBankMemoryItem{},
	}
	node := mem.ToVectorNode()
	if node.Metadata["type"] != "reasoning_bank_memory" {
		t.Errorf("type = %v, want reasoning_bank_memory", node.Metadata["type"])
	}
}

func TestReasoningBankMemory_FromVectorNode_往返(t *testing.T) {
	label := false
	original := ReasoningBankMemory{
		BaseMemory: BaseMemory{WorkspaceID: "ws2"},
		Query:      "error handling",
		Memory: []ReasoningBankMemoryItem{
			{Title: "Error Handling", Description: "Best practices", Content: "Use specific exception types"},
		},
		Label: &label,
	}
	node := original.ToVectorNode()
	restored := NewReasoningBankMemoryFromVectorNode(node)
	if restored.Query != original.Query {
		t.Errorf("Query = %q, want %q", restored.Query, original.Query)
	}
	if len(restored.Memory) != 1 {
		t.Fatalf("Memory 长度 = %d, want 1", len(restored.Memory))
	}
	if restored.Memory[0].Title != "Error Handling" {
		t.Errorf("Memory[0].Title = %q, want %q", restored.Memory[0].Title, "Error Handling")
	}
	if restored.Label == nil || *restored.Label != false {
		t.Errorf("Label = %v, want false", restored.Label)
	}
}

func TestReasoningBankMemory_无顶层Content(t *testing.T) {
	mem := ReasoningBankMemory{
		Query: "test",
		Memory: []ReasoningBankMemoryItem{
			{Title: "T", Description: "D", Content: "C"},
		},
	}
	// RBMemory 没有顶层 content 字段，content 嵌套在 Memory 列表项中
	if mem.Query != "test" {
		t.Error("Query 字段应该可用")
	}
}

func TestReasoningBankMemory_GetWorkspaceID(t *testing.T) {
	mem := ReasoningBankMemory{BaseMemory: BaseMemory{WorkspaceID: "ws_rb"}}
	if mem.GetWorkspaceID() != "ws_rb" {
		t.Errorf("GetWorkspaceID() = %q, want %q", mem.GetWorkspaceID(), "ws_rb")
	}
}

func TestReasoningBankMemory_String(t *testing.T) {
	mem := ReasoningBankMemory{
		BaseMemory: BaseMemory{WorkspaceID: "ws2"},
		Query:      "test query",
		Memory:     []ReasoningBankMemoryItem{},
	}
	s := mem.String()
	if s == "" {
		t.Error("String() 不应为空")
	}
}

func TestReasoningBankMemory_JSON往返(t *testing.T) {
	label := true
	original := ReasoningBankMemory{
		BaseMemory: BaseMemory{WorkspaceID: "ws2"},
		Query:      "test query",
		Memory: []ReasoningBankMemoryItem{
			{Title: "T1", Description: "D1", Content: "C1"},
		},
		Label: &label,
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ReasoningBankMemory
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.Query != original.Query {
		t.Errorf("Query = %q, want %q", restored.Query, original.Query)
	}
}

func TestReasoningBankMemoryItem_JSON往返(t *testing.T) {
	original := ReasoningBankMemoryItem{
		Title:       "Test Title",
		Description: "Test Desc",
		Content:     "Test Content",
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ReasoningBankMemoryItem
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.Title != original.Title {
		t.Errorf("Title = %q, want %q", restored.Title, original.Title)
	}
}

// ──────────────────────────── ReMeMemory ────────────────────────────

func TestReMeMemory_ToVectorNode(t *testing.T) {
	mem := ReMeMemory{
		BaseMemory: BaseMemory{WorkspaceID: "ws3"},
		WhenToUse:  "When implementing caching in Python",
		Content:    "Use functools.lru_cache decorator",
		Score:      0.8,
		CreatedAt:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
		Metadata: ReMeMemoryMetadata{
			Tags:       []string{"python", "caching"},
			StepType:   "implementation",
			ToolsUsed:  []string{"functools"},
			Confidence: 0.95,
			Freq:       5,
			Utility:    4,
		},
	}
	node := mem.ToVectorNode()
	if node.Metadata["type"] != "reme_memory" {
		t.Errorf("type = %v, want reme_memory", node.Metadata["type"])
	}
	if node.Content != "When implementing caching in Python" {
		t.Errorf("embedding content = %q, want when_to_use", node.Content)
	}
	meta, ok := node.Metadata["metadata"].(map[string]any)
	if !ok {
		t.Fatal("metadata 字段不是 map[string]any")
	}
	if meta["step_type"] != "implementation" {
		t.Errorf("step_type = %v, want implementation", meta["step_type"])
	}
}

func TestReMeMemory_FromVectorNode_往返(t *testing.T) {
	original := ReMeMemory{
		BaseMemory: BaseMemory{WorkspaceID: "ws3"},
		WhenToUse:  "When handling errors",
		Content:    "Use specific exception types",
		Score:      0.92,
		CreatedAt:  time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2024, 3, 2, 0, 0, 0, 0, time.UTC),
		Metadata: ReMeMemoryMetadata{
			Tags:       []string{"python"},
			StepType:   "error_handling",
			ToolsUsed:  []string{"try"},
			Confidence: 0.9,
			Freq:       3,
			Utility:    5,
		},
	}
	node := original.ToVectorNode()
	restored := NewReMeMemoryFromVectorNode(node)
	if restored.WhenToUse != original.WhenToUse {
		t.Errorf("WhenToUse = %q, want %q", restored.WhenToUse, original.WhenToUse)
	}
	if restored.Content != original.Content {
		t.Errorf("Content = %q, want %q", restored.Content, original.Content)
	}
	if restored.Score != original.Score {
		t.Errorf("Score = %f, want %f", restored.Score, original.Score)
	}
	if restored.Metadata.StepType != original.Metadata.StepType {
		t.Errorf("Metadata.StepType = %q, want %q", restored.Metadata.StepType, original.Metadata.StepType)
	}
	if len(restored.Metadata.Tags) != 1 || restored.Metadata.Tags[0] != "python" {
		t.Errorf("Metadata.Tags = %v, want [python]", restored.Metadata.Tags)
	}
}

func TestReMeMemory_GetWorkspaceID(t *testing.T) {
	mem := ReMeMemory{BaseMemory: BaseMemory{WorkspaceID: "ws_reme"}}
	if mem.GetWorkspaceID() != "ws_reme" {
		t.Errorf("GetWorkspaceID() = %q, want %q", mem.GetWorkspaceID(), "ws_reme")
	}
}

func TestReMeMemory_JSON往返(t *testing.T) {
	original := ReMeMemory{
		BaseMemory: BaseMemory{WorkspaceID: "ws3"},
		WhenToUse:  "test when",
		Content:    "test content",
		Score:      0.75,
		CreatedAt:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		Metadata: ReMeMemoryMetadata{
			Tags:      []string{"t1"},
			StepType:  "s1",
			ToolsUsed: []string{"tool1"},
		},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ReMeMemory
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.WhenToUse != original.WhenToUse {
		t.Errorf("WhenToUse = %q, want %q", restored.WhenToUse, original.WhenToUse)
	}
	if restored.Score != original.Score {
		t.Errorf("Score = %f, want %f", restored.Score, original.Score)
	}
}

func TestReMeMemoryMetadata_JSON往返(t *testing.T) {
	original := ReMeMemoryMetadata{
		Tags:       []string{"a", "b"},
		StepType:   "impl",
		ToolsUsed:  []string{"t1", "t2"},
		Confidence: 0.85,
		Freq:       10,
		Utility:    3.5,
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ReMeMemoryMetadata
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.StepType != original.StepType {
		t.Errorf("StepType = %q, want %q", restored.StepType, original.StepType)
	}
	if restored.Freq != original.Freq {
		t.Errorf("Freq = %d, want %d", restored.Freq, original.Freq)
	}
}

// ──────────────────────────── VectorNodeToMemory 工厂 ────────────────────────────

func TestVectorNodeToMemory_ACE(t *testing.T) {
	node := coreschema.NewVectorNode("ace_ws1_001", "content", nil, map[string]any{
		"type":         "ace_memory",
		"id":           "mem_001",
		"section":      "python",
		"content":      "Use lru_cache",
		"helpful":      1,
		"harmful":      0,
		"neutral":      0,
		"workspace_id": "ws1",
		"created_at":   "2024-01-01T00:00:00Z",
		"updated_at":   "2024-01-01T00:00:00Z",
	})
	mem, err := VectorNodeToMemory(node)
	if err != nil {
		t.Fatalf("VectorNodeToMemory 失败: %v", err)
	}
	ace, ok := mem.(*ACEMemory)
	if !ok {
		t.Fatal("期望 *ACEMemory 类型")
	}
	if ace.ID != "mem_001" {
		t.Errorf("ID = %q, want mem_001", ace.ID)
	}
}

func TestVectorNodeToMemory_ReasoningBank(t *testing.T) {
	node := coreschema.NewVectorNode("rb_ws2_001", "query text", nil, map[string]any{
		"type":         "reasoning_bank_memory",
		"query":        "test query",
		"memory":       []any{map[string]any{"title": "T", "description": "D", "content": "C"}},
		"workspace_id": "ws2",
	})
	mem, err := VectorNodeToMemory(node)
	if err != nil {
		t.Fatalf("VectorNodeToMemory 失败: %v", err)
	}
	rb, ok := mem.(*ReasoningBankMemory)
	if !ok {
		t.Fatal("期望 *ReasoningBankMemory 类型")
	}
	if rb.Query != "test query" {
		t.Errorf("Query = %q, want test query", rb.Query)
	}
}

func TestVectorNodeToMemory_ReMe(t *testing.T) {
	node := coreschema.NewVectorNode("reme_ws3_001", "when to use", nil, map[string]any{
		"type":         "reme_memory",
		"when_to_use":  "test when",
		"content":      "test content",
		"workspace_id": "ws3",
		"created_at":   "2024-01-01T00:00:00Z",
		"updated_at":   "2024-01-01T00:00:00Z",
		"metadata": map[string]any{
			"tags":       []any{"tag1"},
			"step_type":  "test",
			"tools_used": []any{"tool1"},
			"confidence": 0.9,
			"freq":       float64(3),
			"utility":    4.0,
		},
	})
	mem, err := VectorNodeToMemory(node)
	if err != nil {
		t.Fatalf("VectorNodeToMemory 失败: %v", err)
	}
	reme, ok := mem.(*ReMeMemory)
	if !ok {
		t.Fatal("期望 *ReMeMemory 类型")
	}
	if reme.WhenToUse != "test when" {
		t.Errorf("WhenToUse = %q, want test when", reme.WhenToUse)
	}
}

func TestVectorNodeToMemory_未知类型(t *testing.T) {
	node := coreschema.NewVectorNode("unknown_001", "content", nil, map[string]any{
		"type": "unknown_memory",
	})
	_, err := VectorNodeToMemory(node)
	if err == nil {
		t.Error("期望返回 error，得到 nil")
	}
}

func TestVectorNodeToMemory_无type字段(t *testing.T) {
	node := coreschema.NewVectorNode("no_type_001", "content", nil, map[string]any{})
	_, err := VectorNodeToMemory(node)
	if err == nil {
		t.Error("期望返回 error，得到 nil")
	}
}

func TestVectorNodeToMemory_type非字符串(t *testing.T) {
	node := coreschema.NewVectorNode("bad_type_001", "content", nil, map[string]any{
		"type": 42,
	})
	_, err := VectorNodeToMemory(node)
	if err == nil {
		t.Error("期望返回 error，得到 nil")
	}
}
