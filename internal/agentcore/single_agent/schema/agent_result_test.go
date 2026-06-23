package schema

import (
	"encoding/json"
	"testing"

	controllerschema "github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestPart_文本(t *testing.T) {
	part := NewTextPart("hello world")
	if part.Text == nil || *part.Text != "hello world" {
		t.Errorf("Text 期望 %q，实际 %v", "hello world", part.Text)
	}
}

func TestPart_URL(t *testing.T) {
	part := NewURLPart("https://example.com/file.pdf")
	if part.URL == nil || *part.URL != "https://example.com/file.pdf" {
		t.Errorf("URL 期望 %q，实际 %v", "https://example.com/file.pdf", part.URL)
	}
}

func TestPart_Data(t *testing.T) {
	data := map[string]any{"key": "value"}
	part := NewDataPart(data)
	if part.Data == nil {
		t.Error("Data 不应为 nil")
	}
}

func TestPart_RawBytes_序列化(t *testing.T) {
	// UTF-8 可解码的 bytes 应正常序列化为字符串
	raw := RawBytes("hello world")
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	// 期望输出 JSON 字符串 "hello world"（而非 base64）
	var decoded string
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("反序列化字符串失败: %v", err)
	}
	if decoded != "hello world" {
		t.Errorf("序列化结果期望 %q，实际 %q", "hello world", decoded)
	}
}

func TestPart_RawBytes_非UTF8(t *testing.T) {
	// 非 UTF-8 数据应返回错误（对齐 Python UnicodeDecodeError）
	raw := RawBytes{0x80, 0x81, 0x82}
	_, err := json.Marshal(raw)
	if err == nil {
		t.Error("非 UTF-8 数据序列化应返回错误")
	}
}

func TestPart_RawBytes_反序列化(t *testing.T) {
	// JSON 字符串 → RawBytes
	data := []byte(`"hello world"`)
	var raw RawBytes
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if string(raw) != "hello world" {
		t.Errorf("反序列化结果期望 %q，实际 %q", "hello world", string(raw))
	}
}

func TestPart_RawBytes_null(t *testing.T) {
	// null → nil RawBytes
	data := []byte("null")
	var raw RawBytes
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("反序列化 null 失败: %v", err)
	}
	if raw != nil {
		t.Errorf("null 反序列化期望 nil，实际 %v", raw)
	}
}

func TestPart_RawBytes_完整往返(t *testing.T) {
	// 序列化 + 反序列化往返
	original := RawBytes("测试中文内容")
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var restored RawBytes
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if string(restored) != string(original) {
		t.Errorf("往返结果期望 %q，实际 %q", string(original), string(restored))
	}
}

func TestPart_JSON序列化(t *testing.T) {
	text := "hello"
	url := "https://example.com"
	filename := "doc.pdf"
	mediaType := "application/pdf"
	part := Part{
		Text:      &text,
		URL:       &url,
		Filename:  &filename,
		MediaType: &mediaType,
		Metadata:  map[string]any{"source": "test"},
	}
	data, err := json.Marshal(part)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var decoded Part
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if *decoded.Text != text {
		t.Errorf("Text 期望 %q，实际 %q", text, *decoded.Text)
	}
}

func TestArtifact_基本(t *testing.T) {
	artifact := NewArtifact("response", NewTextPart("hello"))
	if artifact.Name == nil || *artifact.Name != "response" {
		t.Errorf("Name 期望 %q，实际 %v", "response", artifact.Name)
	}
	if len(artifact.Parts) != 1 {
		t.Fatalf("Parts 长度期望 1，实际 %d", len(artifact.Parts))
	}
}

func TestArtifact_JSON序列化(t *testing.T) {
	artifactID := "art-1"
	name := "summary"
	desc := "结果摘要"
	artifact := Artifact{
		ArtifactID: &artifactID,
		Name:       &name,
		Description: &desc,
		Parts:      []Part{NewTextPart("hello")},
		Metadata:   map[string]any{"key": "value"},
	}
	data, err := json.Marshal(artifact)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	// 验证 camelCase JSON tag
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("解析原始 JSON 失败: %v", err)
	}
	if _, ok := raw["artifactId"]; !ok {
		t.Error("JSON 中缺少 artifactId 字段（camelCase）")
	}
	if _, ok := raw["name"]; !ok {
		t.Error("JSON 中缺少 name 字段")
	}
}

func TestAgentResult_基本(t *testing.T) {
	result := NewAgentResult(controllerschema.TaskCompleted)
	if result.Status != controllerschema.TaskCompleted {
		t.Errorf("Status 期望 %v，实际 %v", controllerschema.TaskCompleted, result.Status)
	}
	if !result.IsTerminal() {
		t.Error("TaskCompleted 应为终态")
	}
}

func TestAgentResult_完整构造(t *testing.T) {
	taskID := "task-123"
	sessionID := "sess-456"
	result := &AgentResult{
		TaskID:    &taskID,
		SessionID: &sessionID,
		Status:    controllerschema.TaskWorking,
		Artifacts: []Artifact{
			NewArtifact("response", NewTextPart("结果内容")),
		},
		Metadata: map[string]any{"source": "test"},
	}
	if result.IsTerminal() {
		t.Error("TaskWorking 不应为终态")
	}
	if len(result.Artifacts) != 1 {
		t.Errorf("Artifacts 长度期望 1，实际 %d", len(result.Artifacts))
	}
}

func TestAgentResult_JSON序列化(t *testing.T) {
	taskID := "task-123"
	sessionID := "sess-456"
	result := &AgentResult{
		TaskID:    &taskID,
		SessionID: &sessionID,
		Status:    controllerschema.TaskCompleted,
		Artifacts: []Artifact{
			NewArtifact("response", NewTextPart("hello")),
		},
		Metadata: map[string]any{"key": "value"},
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	// 验证 JSON tag 命名
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("解析原始 JSON 失败: %v", err)
	}
	// task_id 用 snake_case
	if _, ok := raw["task_id"]; !ok {
		t.Error("JSON 中缺少 task_id 字段（snake_case）")
	}
	// sessionId 用 camelCase
	if _, ok := raw["sessionId"]; !ok {
		t.Error("JSON 中缺少 sessionId 字段（camelCase）")
	}
}

func TestAgentResult_对齐Python字段(t *testing.T) {
	// 验证与 Python AgentResult 字段对齐：
	// task_id (Optional[str]), sessionId (Optional[str]), status (TaskStatus),
	// artifacts (List[Artifact]), metadata (Dict[str, Any])
	result := &AgentResult{}
	if result.TaskID != nil {
		t.Errorf("默认 TaskID 应为 nil，实际 %v", result.TaskID)
	}
	if result.SessionID != nil {
		t.Errorf("默认 SessionID 应为 nil，实际 %v", result.SessionID)
	}
	if result.Status != "" {
		t.Errorf("默认 Status 应为空，实际 %q", result.Status)
	}
	if result.Artifacts != nil {
		t.Errorf("默认 Artifacts 应为 nil，实际 %v", result.Artifacts)
	}
	if result.Metadata != nil {
		t.Errorf("默认 Metadata 应为 nil，实际 %v", result.Metadata)
	}
}

func TestAgentResult_流式合并(t *testing.T) {
	// 模拟 Python _merge_agent_results 行为
	taskID1 := "task-1"
	sessionID1 := "sess-1"
	base := &AgentResult{
		TaskID:    &taskID1,
		SessionID: &sessionID1,
		Status:    controllerschema.TaskWorking,
		Artifacts: []Artifact{NewArtifact("response", NewTextPart("chunk1"))},
		Metadata:  map[string]any{"key1": "val1"},
	}

	taskID2 := "task-2"
	sessionID2 := "sess-2"
	update := &AgentResult{
		TaskID:    &taskID2,
		SessionID: &sessionID2,
		Status:    controllerschema.TaskCompleted,
		Artifacts: []Artifact{NewArtifact("response", NewTextPart("chunk2"))},
		Metadata:  map[string]any{"key2": "val2"},
	}

	// 合并逻辑（对齐 Python _merge_agent_results）
	merged := &AgentResult{
		TaskID:    update.TaskID,
		SessionID: update.SessionID,
		Status:    update.Status,
		Artifacts: append(base.Artifacts, update.Artifacts...),
		Metadata:  mergeMetadata(base.Metadata, update.Metadata),
	}
	if len(merged.Artifacts) != 2 {
		t.Errorf("合并后 Artifacts 长度期望 2，实际 %d", len(merged.Artifacts))
	}
	if !merged.IsTerminal() {
		t.Error("合并后终态应为 true")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// mergeMetadata 合并两个 metadata map，后者覆盖前者。
func mergeMetadata(base, update map[string]any) map[string]any {
	result := make(map[string]any, len(base)+len(update))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range update {
		result[k] = v
	}
	return result
}

// TestAgentCard_NewAgentCard_在新包中 验证 AgentCard 在 single_agent/schema/ 包中可用。
func TestAgentCard_NewAgentCard_在新包中(t *testing.T) {
	card := NewAgentCard(schema.WithName("test_agent"))
	if card.Name != "test_agent" {
		t.Errorf("Name 期望 %q，实际 %q", "test_agent", card.Name)
	}
}
