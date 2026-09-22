package schema

import (
	"encoding/json"
	"testing"
	"time"
)

// ──────────────────────────── ACE Request/Response ────────────────────────────

func TestACESummarizeRequest_JSON往返(t *testing.T) {
	gt := "expected output"
	fb := []string{"good", "bad"}
	original := ACESummarizeRequest{
		Matts:        "parallel",
		Query:        "How to implement caching?",
		Trajectories: []string{"Traj1", "Traj2"},
		GroundTruth:  &gt,
		Feedback:     fb,
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ACESummarizeRequest
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.Query != original.Query {
		t.Errorf("Query = %q, want %q", restored.Query, original.Query)
	}
	if restored.Matts != "parallel" {
		t.Errorf("Matts = %q, want parallel", restored.Matts)
	}
	if restored.GroundTruth == nil || *restored.GroundTruth != "expected output" {
		t.Errorf("GroundTruth = %v, want expected output", restored.GroundTruth)
	}
	if len(restored.Feedback) != 2 {
		t.Errorf("Feedback 长度 = %d, want 2", len(restored.Feedback))
	}
}

func TestACESummarizeResponse_JSON往返(t *testing.T) {
	original := ACESummarizeResponse{
		Status: "success",
		Memory: []ACEMemory{
			{BaseMemory: BaseMemory{WorkspaceID: "ws1"}, ID: "mem_001", Section: "python", Content: "test",
				CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				UpdatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
		},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ACESummarizeResponse
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.Status != "success" {
		t.Errorf("Status = %q, want success", restored.Status)
	}
	if len(restored.Memory) != 1 {
		t.Fatalf("Memory 长度 = %d, want 1", len(restored.Memory))
	}
	if restored.Memory[0].ID != "mem_001" {
		t.Errorf("Memory[0].ID = %q, want mem_001", restored.Memory[0].ID)
	}
}

func TestACERetrieveRequest_JSON往返(t *testing.T) {
	uid := "alice"
	original := ACERetrieveRequest{UserID: &uid}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ACERetrieveRequest
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.UserID == nil || *restored.UserID != "alice" {
		t.Errorf("UserID = %v, want alice", restored.UserID)
	}
}

func TestACERetrieveResponse_JSON往返(t *testing.T) {
	original := ACERetrieveResponse{
		Status:       "success",
		MemoryString: "Section: python\nContent: test",
		RetrievedMemory: []ACERetrievedMemory{
			{ID: "mem_001", Section: "python", Content: "test", Helpful: 1},
		},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ACERetrieveResponse
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.MemoryString != original.MemoryString {
		t.Errorf("MemoryString = %q, want %q", restored.MemoryString, original.MemoryString)
	}
	if len(restored.RetrievedMemory) != 1 {
		t.Fatalf("RetrievedMemory 长度 = %d, want 1", len(restored.RetrievedMemory))
	}
	if restored.RetrievedMemory[0].ID != "mem_001" {
		t.Errorf("RetrievedMemory[0].ID = %q, want mem_001", restored.RetrievedMemory[0].ID)
	}
}

func TestACERetrievedMemory_JSON往返(t *testing.T) {
	original := ACERetrievedMemory{
		ID:      "mem_001",
		Section: "python",
		Content: "Use lru_cache",
		Helpful: 5,
		Harmful: 1,
		Neutral: 2,
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ACERetrievedMemory
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.ID != original.ID {
		t.Errorf("ID = %q, want %q", restored.ID, original.ID)
	}
}

// ──────────────────────────── ReasoningBank Request/Response ────────────────────────────

func TestReasoningBankSummarizeRequest_JSON往返(t *testing.T) {
	l1, l2 := true, false
	original := ReasoningBankSummarizeRequest{
		Matts:        "parallel",
		Query:        "How to handle errors?",
		Trajectories: []string{"Traj1", "Traj2"},
		Label:        []*bool{&l1, &l2},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ReasoningBankSummarizeRequest
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.Query != original.Query {
		t.Errorf("Query = %q, want %q", restored.Query, original.Query)
	}
	if len(restored.Label) != 2 {
		t.Errorf("Label 长度 = %d, want 2", len(restored.Label))
	}
}

func TestReasoningBankSummarizeResponse_JSON往返(t *testing.T) {
	original := ReasoningBankSummarizeResponse{
		Status: "success",
		Memory: []ReasoningBankMemory{
			{BaseMemory: BaseMemory{WorkspaceID: "ws2"}, Query: "test query"},
		},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ReasoningBankSummarizeResponse
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.Status != "success" {
		t.Errorf("Status = %q, want success", restored.Status)
	}
	if len(restored.Memory) != 1 {
		t.Errorf("Memory 长度 = %d, want 1", len(restored.Memory))
	}
}

func TestReasoningBankRetrieveRequest_JSON往返(t *testing.T) {
	original := ReasoningBankRetrieveRequest{
		Query: "How to implement caching?",
		TopK:  3,
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ReasoningBankRetrieveRequest
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.Query != original.Query {
		t.Errorf("Query = %q, want %q", restored.Query, original.Query)
	}
	if restored.TopK != 3 {
		t.Errorf("TopK = %d, want 3", restored.TopK)
	}
}

func TestReasoningBankRetrieveResponse_JSON往返(t *testing.T) {
	original := ReasoningBankRetrieveResponse{
		Status:       "success",
		MemoryString: "Title: Python Caching\nDescription: Memoization...",
		RetrievedMemory: []ReasoningBankRetrievedMemory{
			{Title: "Python Caching", Description: "Memoization", Content: "Use lru_cache"},
		},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ReasoningBankRetrieveResponse
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.MemoryString != original.MemoryString {
		t.Errorf("MemoryString 不匹配")
	}
}

func TestReasoningBankRetrievedMemory_JSON往返(t *testing.T) {
	original := ReasoningBankRetrievedMemory{
		Title:       "Error Handling",
		Description: "Best practices",
		Content:     "Use specific exceptions",
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ReasoningBankRetrievedMemory
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.Title != original.Title {
		t.Errorf("Title = %q, want %q", restored.Title, original.Title)
	}
}

// ──────────────────────────── ReMe Request/Response ────────────────────────────

func TestReMeSummarizeRequest_JSON往返(t *testing.T) {
	original := ReMeSummarizeRequest{
		Matts:        "parallel",
		Trajectories: []string{"Traj1", "Traj2"},
		Score:        []float64{0.85, 0.92},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ReMeSummarizeRequest
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.Matts != "parallel" {
		t.Errorf("Matts = %q, want parallel", restored.Matts)
	}
	if len(restored.Score) != 2 {
		t.Errorf("Score 长度 = %d, want 2", len(restored.Score))
	}
}

func TestReMeSummarizeResponse_JSON往返(t *testing.T) {
	original := ReMeSummarizeResponse{
		Status: "success",
		Memory: []ReMeMemory{
			{BaseMemory: BaseMemory{WorkspaceID: "ws3"}, WhenToUse: "test when", Content: "test content", Score: 0.9,
				CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				UpdatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
		},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ReMeSummarizeResponse
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.Status != "success" {
		t.Errorf("Status = %q, want success", restored.Status)
	}
}

func TestReMeRetrieveRequest_JSON往返(t *testing.T) {
	original := ReMeRetrieveRequest{
		Query:         "How to implement caching?",
		TopKRetrieval: 10,
		TopKRerank:    5,
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ReMeRetrieveRequest
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.Query != original.Query {
		t.Errorf("Query = %q, want %q", restored.Query, original.Query)
	}
	if restored.TopKRetrieval != 10 {
		t.Errorf("TopKRetrieval = %d, want 10", restored.TopKRetrieval)
	}
	if restored.TopKRerank != 5 {
		t.Errorf("TopKRerank = %d, want 5", restored.TopKRerank)
	}
}

func TestReMeRetrieveResponse_JSON往返(t *testing.T) {
	original := ReMeRetrieveResponse{
		Status:       "success",
		MemoryString: "When to use: When implementing caching...",
		RetrievedMemory: []ReMeRetrievedMemory{
			{WhenToUse: "When implementing caching", Content: "Use lru_cache"},
		},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ReMeRetrieveResponse
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.MemoryString != original.MemoryString {
		t.Errorf("MemoryString 不匹配")
	}
}

func TestReMeRetrievedMemory_JSON往返(t *testing.T) {
	original := ReMeRetrievedMemory{
		WhenToUse: "When implementing caching",
		Content:   "Use functools.lru_cache",
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ReMeRetrievedMemory
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.WhenToUse != original.WhenToUse {
		t.Errorf("WhenToUse = %q, want %q", restored.WhenToUse, original.WhenToUse)
	}
}

// ──────────────────────────── 泛型 Response ────────────────────────────

func TestSummarizeResponse_ACE(t *testing.T) {
	resp := SummarizeResponse[ACEMemory]{
		Status: "success",
		Memory: []ACEMemory{
			{BaseMemory: BaseMemory{WorkspaceID: "ws1"}, ID: "mem_001", Section: "python", Content: "test"},
		},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored SummarizeResponse[ACEMemory]
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.Status != "success" {
		t.Errorf("Status = %q, want success", restored.Status)
	}
	if len(restored.Memory) != 1 {
		t.Errorf("Memory 长度 = %d, want 1", len(restored.Memory))
	}
	if restored.Memory[0].ID != "mem_001" {
		t.Errorf("Memory[0].ID = %q, want mem_001", restored.Memory[0].ID)
	}
}

func TestSummarizeResponse_ReasoningBank(t *testing.T) {
	resp := SummarizeResponse[ReasoningBankMemory]{
		Status: "success",
		Memory: []ReasoningBankMemory{
			{BaseMemory: BaseMemory{WorkspaceID: "ws2"}, Query: "test query"},
		},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored SummarizeResponse[ReasoningBankMemory]
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.Memory[0].Query != "test query" {
		t.Errorf("Memory[0].Query = %q, want test query", restored.Memory[0].Query)
	}
}

func TestSummarizeResponse_ReMe(t *testing.T) {
	resp := SummarizeResponse[ReMeMemory]{
		Status: "success",
		Memory: []ReMeMemory{
			{BaseMemory: BaseMemory{WorkspaceID: "ws3"}, WhenToUse: "test when", Content: "test content"},
		},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored SummarizeResponse[ReMeMemory]
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.Memory[0].WhenToUse != "test when" {
		t.Errorf("Memory[0].WhenToUse = %q, want test when", restored.Memory[0].WhenToUse)
	}
}

func TestRetrieveResponse_ACE(t *testing.T) {
	resp := RetrieveResponse[ACERetrievedMemory]{
		Status:       "success",
		MemoryString: "test string",
		RetrievedMemory: []ACERetrievedMemory{
			{ID: "mem_001", Content: "test"},
		},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored RetrieveResponse[ACERetrievedMemory]
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.MemoryString != "test string" {
		t.Errorf("MemoryString = %q, want test string", restored.MemoryString)
	}
	if len(restored.RetrievedMemory) != 1 {
		t.Errorf("RetrievedMemory 长度 = %d, want 1", len(restored.RetrievedMemory))
	}
}

func TestRetrieveResponse_ReasoningBank(t *testing.T) {
	resp := RetrieveResponse[ReasoningBankRetrievedMemory]{
		Status:       "success",
		MemoryString: "test string",
		RetrievedMemory: []ReasoningBankRetrievedMemory{
			{Title: "T1", Content: "C1"},
		},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored RetrieveResponse[ReasoningBankRetrievedMemory]
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.RetrievedMemory[0].Title != "T1" {
		t.Errorf("RetrievedMemory[0].Title = %q, want T1", restored.RetrievedMemory[0].Title)
	}
}

func TestRetrieveResponse_ReMe(t *testing.T) {
	resp := RetrieveResponse[ReMeRetrievedMemory]{
		Status:       "success",
		MemoryString: "test string",
		RetrievedMemory: []ReMeRetrievedMemory{
			{WhenToUse: "when test", Content: "content test"},
		},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored RetrieveResponse[ReMeRetrievedMemory]
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.RetrievedMemory[0].WhenToUse != "when test" {
		t.Errorf("RetrievedMemory[0].WhenToUse = %q, want when test", restored.RetrievedMemory[0].WhenToUse)
	}
}

// ──────────────────────────── 默认值 ────────────────────────────

func TestACESummarizeRequest_默认值(t *testing.T) {
	// 测试 JSON 反序列化后 matts 字段默认值为 "none"
	data := `{"query":"q","trajectories":["t1"]}`
	var req ACESummarizeRequest
	err := json.Unmarshal([]byte(data), &req)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if req.Matts != "none" {
		t.Errorf("默认 Matts = %q, want none", req.Matts)
	}
}

func TestReasoningBankSummarizeRequest_默认值(t *testing.T) {
	data := `{"query":"q","trajectories":["t1"]}`
	var req ReasoningBankSummarizeRequest
	err := json.Unmarshal([]byte(data), &req)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if req.Matts != "none" {
		t.Errorf("默认 Matts = %q, want none", req.Matts)
	}
}

func TestReMeSummarizeRequest_默认值(t *testing.T) {
	data := `{"trajectories":["t1"]}`
	var req ReMeSummarizeRequest
	err := json.Unmarshal([]byte(data), &req)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if req.Matts != "none" {
		t.Errorf("默认 Matts = %q, want none", req.Matts)
	}
}

func TestReasoningBankRetrieveRequest_默认值(t *testing.T) {
	data := `{"query":"q"}`
	var req ReasoningBankRetrieveRequest
	err := json.Unmarshal([]byte(data), &req)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if req.TopK != 5 {
		t.Errorf("默认 TopK = %d, want 5", req.TopK)
	}
}

func TestReMeRetrieveRequest_默认值(t *testing.T) {
	data := `{"query":"q"}`
	var req ReMeRetrieveRequest
	err := json.Unmarshal([]byte(data), &req)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if req.TopKRetrieval != 10 {
		t.Errorf("默认 TopKRetrieval = %d, want 10", req.TopKRetrieval)
	}
	if req.TopKRerank != 5 {
		t.Errorf("默认 TopKRerank = %d, want 5", req.TopKRerank)
	}
}
