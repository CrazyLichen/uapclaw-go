package ace

import (
	"strings"
	"testing"
	"time"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestOperationType_String(t *testing.T) {
	tests := []struct {
		op       OperationType
		expected string
	}{
		{OperationAdd, "ADD"},
		{OperationUpdate, "UPDATE"},
		{OperationTag, "TAG"},
		{OperationRemove, "REMOVE"},
	}
	for _, tt := range tests {
		if got := tt.op.String(); got != tt.expected {
			t.Errorf("OperationType.String() = %q, want %q", got, tt.expected)
		}
	}
}

func TestParseOperationType(t *testing.T) {
	tests := []struct {
		input    string
		expected OperationType
		wantErr  bool
	}{
		{"ADD", OperationAdd, false},
		{"UPDATE", OperationUpdate, false},
		{"TAG", OperationTag, false},
		{"REMOVE", OperationRemove, false},
		{"add", OperationAdd, false},
		{"INVALID", 0, true},
	}
	for _, tt := range tests {
		got, err := ParseOperationType(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseOperationType(%q) 期望返回错误，实际返回 nil", tt.input)
			}
		} else {
			if err != nil {
				t.Errorf("ParseOperationType(%q) 意外错误: %v", tt.input, err)
			}
			if got != tt.expected {
				t.Errorf("ParseOperationType(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		}
	}
}

func TestBullet_ApplyMetadata(t *testing.T) {
	b := &Bullet{
		ID:        "test-00001",
		Section:   "test",
		Content:   "test content",
		Helpful:   1,
		Harmful:   0,
		Neutral:   0,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	b.ApplyMetadata(map[string]int{"helpful": 2, "harmful": 1})
	// 对齐 Python: 直接赋值 self.helpful = metadata.get("helpful", self.helpful)，Helpful=2（非 1+2=3）
	if b.Helpful != 2 {
		t.Errorf("Helpful = %d, want 2", b.Helpful)
	}
	if b.Harmful != 1 {
		t.Errorf("Harmful = %d, want 1", b.Harmful)
	}
}

func TestBullet_Tag(t *testing.T) {
	b := &Bullet{ID: "t-00001", Section: "t", Content: "c"}
	err := b.Tag("helpful", 3)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if b.Helpful != 3 {
		t.Errorf("Helpful = %d, want 3", b.Helpful)
	}
	err = b.Tag("invalid", 1)
	if err == nil {
		t.Error("期望返回错误，实际返回 nil")
	}
}

func TestDeltaOperation_ToJSON_FromJSON(t *testing.T) {
	content := "new content"
	bulletID := "sec-00001"
	op := DeltaOperation{
		Type:     OperationAdd,
		Section:  "sec",
		Content:  &content,
		BulletID: &bulletID,
		Metadata: map[string]int{"helpful": 1},
	}
	jsonMap := op.ToJSON()
	got := NewDeltaOperationFromJSON(jsonMap)
	if got.Type != op.Type {
		t.Errorf("Type = %v, want %v", got.Type, op.Type)
	}
	if got.Section != op.Section {
		t.Errorf("Section = %q, want %q", got.Section, op.Section)
	}
	if got.Content == nil || *got.Content != content {
		t.Errorf("Content = %v, want %q", got.Content, content)
	}
	if got.BulletID == nil || *got.BulletID != bulletID {
		t.Errorf("BulletID = %v, want %q", got.BulletID, bulletID)
	}
	if got.Metadata["helpful"] != 1 {
		t.Errorf("Metadata[helpful] = %d, want 1", got.Metadata["helpful"])
	}
}

func TestDeltaBatch_ToJSON_FromJSON(t *testing.T) {
	content := "hello"
	batch := DeltaBatch{
		Reasoning: "test reasoning",
		Operations: []DeltaOperation{
			{Type: OperationAdd, Section: "s1", Content: &content},
		},
	}
	jsonMap := batch.ToJSON()
	got := NewDeltaBatchFromJSON(jsonMap)
	if got.Reasoning != batch.Reasoning {
		t.Errorf("Reasoning = %q, want %q", got.Reasoning, batch.Reasoning)
	}
	if len(got.Operations) != 1 {
		t.Fatalf("Operations len = %d, want 1", len(got.Operations))
	}
	if got.Operations[0].Type != OperationAdd {
		t.Errorf("Operations[0].Type = %v, want %v", got.Operations[0].Type, OperationAdd)
	}
}

func TestPlaybook_AddBullet(t *testing.T) {
	p := NewPlaybook()
	b := p.AddBullet("strategies", "always authenticate first", nil, nil)
	if b == nil {
		t.Fatal("AddBullet 返回 nil")
	}
	if b.Section != "strategies" {
		t.Errorf("Section = %q, want %q", b.Section, "strategies")
	}
	if !strings.HasPrefix(b.ID, "strategies-") {
		t.Errorf("ID = %q, 应以 'strategies-' 开头", b.ID)
	}
	if b.Helpful != 0 || b.Harmful != 0 || b.Neutral != 0 {
		t.Errorf("计数器应全部为 0")
	}
}

func TestPlaybook_AddBullet_带元数据(t *testing.T) {
	p := NewPlaybook()
	b := p.AddBullet("sec", "content", nil, map[string]int{"helpful": 2, "harmful": 1})
	if b.Helpful != 2 {
		t.Errorf("Helpful = %d, want 2", b.Helpful)
	}
	if b.Harmful != 1 {
		t.Errorf("Harmful = %d, want 1", b.Harmful)
	}
}

func TestPlaybook_AddBullet_指定ID(t *testing.T) {
	p := NewPlaybook()
	customID := "custom-001"
	b := p.AddBullet("sec", "content", &customID, nil)
	if b.ID != customID {
		t.Errorf("ID = %q, want %q", b.ID, customID)
	}
}

func TestPlaybook_UpdateBullet(t *testing.T) {
	p := NewPlaybook()
	p.AddBullet("sec", "old", nil, nil)
	content := "new"
	got := p.UpdateBullet("sec-00001", &content, map[string]int{"helpful": 1})
	if got == nil {
		t.Fatal("UpdateBullet 返回 nil")
	}
	if got.Content != "new" {
		t.Errorf("Content = %q, want %q", got.Content, "new")
	}
	if got.Helpful != 1 {
		t.Errorf("Helpful = %d, want 1", got.Helpful)
	}
	// 更新不存在的 bullet
	got = p.UpdateBullet("nonexist", &content, nil)
	if got != nil {
		t.Error("更新不存在的 bullet 应返回 nil")
	}
}

func TestPlaybook_TagBullet(t *testing.T) {
	p := NewPlaybook()
	p.AddBullet("sec", "content", nil, nil)
	got := p.TagBullet("sec-00001", "helpful", 2)
	if got == nil {
		t.Fatal("TagBullet 返回 nil")
	}
	if got.Helpful != 2 {
		t.Errorf("Helpful = %d, want 2", got.Helpful)
	}
}

func TestPlaybook_TagBullet_不存在(t *testing.T) {
	p := NewPlaybook()
	got := p.TagBullet("nonexist", "helpful", 1)
	if got != nil {
		t.Error("TagBullet 不存在的 bullet 应返回 nil")
	}
}

func TestPlaybook_RemoveBullet(t *testing.T) {
	p := NewPlaybook()
	p.AddBullet("sec", "c1", nil, nil)
	p.RemoveBullet("sec-00001")
	if p.GetBullet("sec-00001") != nil {
		t.Error("bullet 应已被删除")
	}
	if len(p.Bullets()) != 0 {
		t.Errorf("Bullets len = %d, want 0", len(p.Bullets()))
	}
	// 删除不存在的 bullet 不 panic
	p.RemoveBullet("nonexist")
}

func TestPlaybook_RemoveBullet_清理空section(t *testing.T) {
	p := NewPlaybook()
	p.AddBullet("sec", "c1", nil, nil)
	p.RemoveBullet("sec-00001")
	if _, ok := p.sections["sec"]; ok {
		t.Error("空 section 应被删除")
	}
}

func TestPlaybook_ApplyDelta(t *testing.T) {
	p := NewPlaybook()
	content1 := "new insight"
	content2 := "updated content"
	delta := &DeltaBatch{
		Reasoning: "test",
		Operations: []DeltaOperation{
			{Type: OperationAdd, Section: "sec1", Content: &content1},
			{Type: OperationTag, Section: "", BulletID: strPtr("sec1-00001"), Metadata: map[string]int{"helpful": 1}},
			{Type: OperationUpdate, Section: "", BulletID: strPtr("sec1-00001"), Content: &content2},
			{Type: OperationRemove, Section: "", BulletID: strPtr("sec1-00001")},
		},
	}
	p.ApplyDelta(delta)
	// ADD 后 TAG 后 UPDATE 后 REMOVE，最终不存在
	if p.GetBullet("sec1-00001") != nil {
		t.Error("bullet 应已被删除")
	}
}

func TestPlaybook_ApplyDelta_TAG无BulletID(t *testing.T) {
	p := NewPlaybook()
	delta := &DeltaBatch{
		Reasoning: "test",
		Operations: []DeltaOperation{
			{Type: OperationTag, Section: "", Metadata: map[string]int{"helpful": 1}},
		},
	}
	p.ApplyDelta(delta) // 不 panic 即可
	if len(p.Bullets()) != 0 {
		t.Errorf("无 BulletID 的 TAG 不应创建 bullet")
	}
}

func TestPlaybook_Serialization(t *testing.T) {
	p := NewPlaybook()
	p.AddBullet("sec", "content", nil, map[string]int{"helpful": 1})
	data, err := p.Dumps()
	if err != nil {
		t.Fatalf("Dumps 失败: %v", err)
	}
	loaded, err := PlaybookLoads(data)
	if err != nil {
		t.Fatalf("Loads 失败: %v", err)
	}
	if len(loaded.Bullets()) != 1 {
		t.Fatalf("Bullets len = %d, want 1", len(loaded.Bullets()))
	}
	got := loaded.GetBullet("sec-00001")
	if got == nil {
		t.Fatal("bullet 未加载")
	}
	if got.Helpful != 1 {
		t.Errorf("Helpful = %d, want 1", got.Helpful)
	}
}

func TestPlaybook_AsPrompt(t *testing.T) {
	p := NewPlaybook()
	p.AddBullet("strategies", "always authenticate", nil, map[string]int{"helpful": 1, "harmful": 0})
	result := p.AsPrompt()
	if !strings.Contains(result, "## strategies") {
		t.Error("AsPrompt 应包含 section 标题")
	}
	if !strings.Contains(result, "[strategies-00001]") {
		t.Error("AsPrompt 应包含 bullet ID")
	}
	if !strings.Contains(result, "helpful=1") {
		t.Error("AsPrompt 应包含计数器")
	}
}

func TestPlaybook_MakePlaybookExcerpt(t *testing.T) {
	p := NewPlaybook()
	p.AddBullet("s1", "c1", nil, nil)
	b2 := p.AddBullet("s2", "c2", nil, nil)
	result := p.MakePlaybookExcerpt([]string{b2.ID})
	if !strings.Contains(result, "c2") {
		t.Error("MakePlaybookExcerpt 应包含指定 bullet 内容")
	}
	if strings.Contains(result, "c1") {
		t.Error("MakePlaybookExcerpt 不应包含未指定的 bullet")
	}
}

func TestPlaybook_MakePlaybookExcerpt_去重(t *testing.T) {
	p := NewPlaybook()
	p.AddBullet("s1", "c1", nil, nil)
	result := p.MakePlaybookExcerpt([]string{"s1-00001", "s1-00001"})
	// 去重，只出现一次
	count := strings.Count(result, "c1")
	if count != 1 {
		t.Errorf("去重后 c1 出现 %d 次, want 1", count)
	}
}

func TestPlaybook_Stats(t *testing.T) {
	p := NewPlaybook()
	p.AddBullet("s1", "c1", nil, map[string]int{"helpful": 2})
	p.AddBullet("s2", "c2", nil, map[string]int{"harmful": 1})
	stats := p.Stats()
	sections, _ := stats["sections"].(int)
	if sections != 2 {
		t.Errorf("sections = %v, want 2", sections)
	}
	bullets, _ := stats["bullets"].(int)
	if bullets != 2 {
		t.Errorf("bullets = %v, want 2", bullets)
	}
}

func TestPlaybook_GenerateID(t *testing.T) {
	p := NewPlaybook()
	id1 := p.generateID("strategies_and_hard_rules")
	if !strings.HasPrefix(id1, "strategies_and_hard_rules-") {
		t.Errorf("ID = %q, 应以 'strategies_and_hard_rules-' 开头", id1)
	}
	id2 := p.generateID("")
	if !strings.HasPrefix(id2, "general-") {
		t.Errorf("空 section 的 ID = %q, 应以 'general-' 开头", id2)
	}
	// 连续生成 ID 递增
	id3 := p.generateID("test")
	id4 := p.generateID("test")
	if id3 == id4 {
		t.Error("连续生成的 ID 应不同")
	}
}

func TestPlaybook_LoadBullet(t *testing.T) {
	p := NewPlaybook()
	b := &Bullet{ID: "manual-001", Section: "manual", Content: "manual content"}
	p.LoadBullet(b)
	got := p.GetBullet("manual-001")
	if got == nil {
		t.Fatal("LoadBullet 后 GetBullet 返回 nil")
	}
	if got.Content != "manual content" {
		t.Errorf("Content = %q, want %q", got.Content, "manual content")
	}
}

func TestPlaybook_SetNextID(t *testing.T) {
	p := NewPlaybook()
	p.SetNextID(100)
	id := p.generateID("sec")
	if !strings.Contains(id, "00101") {
		t.Errorf("SetNextID(100) 后 generateID = %q, 应包含 00101", id)
	}
}

func TestPlaybook_BulletIDs(t *testing.T) {
	p := NewPlaybook()
	p.AddBullet("s1", "c1", nil, nil)
	p.AddBullet("s2", "c2", nil, nil)
	ids := p.BulletIDs()
	if len(ids) != 2 {
		t.Errorf("BulletIDs len = %d, want 2", len(ids))
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func strPtr(s string) *string { return &s }
