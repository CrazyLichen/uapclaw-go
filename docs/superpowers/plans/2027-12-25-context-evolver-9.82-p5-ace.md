# 9.82 P5 ACE 算法实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 9.82 P5 ACE 算法，包括 Playbook 核心数据结构、检索 Op、总结 7 个 Op、prompt 模板、utils 工具函数以及 doc.go 回填。

**Architecture:** 在 `internal/agentcore/context_evolver/` 下新建 `retrieve/task/ace/` 和 `summary/task/ace/` 两个子包。Playbook/DeltaBatch/Bullet/DeltaOperation 统一放在 `summary/task/ace/playbook.go`，6 个 prompt 模板放在 `summary/task/ace/prompt.go`，7 个总结 Op 放在 `summary/task/ace/update.go`，SafeJSONLoads 放在 `summary/task/ace/utils.go`。检索 Op 放在 `retrieve/task/ace/run.go`。

**Tech Stack:** Go 1.22+，text/template，encoding/json，regexp，time

**Design Spec:** `docs/superpowers/specs/2027-12-25-context-evolver-9.82-p5-ace-design.md`

---

## 文件结构

```
新建文件：
  internal/agentcore/context_evolver/retrieve/task/ace/doc.go
  internal/agentcore/context_evolver/retrieve/task/ace/run.go
  internal/agentcore/context_evolver/retrieve/task/ace/run_test.go
  internal/agentcore/context_evolver/summary/task/ace/doc.go
  internal/agentcore/context_evolver/summary/task/ace/playbook.go
  internal/agentcore/context_evolver/summary/task/ace/playbook_test.go
  internal/agentcore/context_evolver/summary/task/ace/prompt.go
  internal/agentcore/context_evolver/summary/task/ace/prompt_test.go
  internal/agentcore/context_evolver/summary/task/ace/update.go
  internal/agentcore/context_evolver/summary/task/ace/update_test.go
  internal/agentcore/context_evolver/summary/task/ace/utils.go
  internal/agentcore/context_evolver/summary/task/ace/utils_test.go

修改文件：
  internal/agentcore/context_evolver/doc.go  # 回填目录段
```

---

### Task 1: Playbook 核心数据结构

**Files:**
- Create: `internal/agentcore/context_evolver/summary/task/ace/playbook.go`
- Test: `internal/agentcore/context_evolver/summary/task/ace/playbook_test.go`

- [ ] **Step 1: 编写 playbook_test.go 中的 OperationType/Bullet/DeltaOperation/DeltaOperation 测试**

```go
package ace

import (
	"encoding/json"
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
	if b.Helpful != 3 {
		t.Errorf("Helpful = %d, want 3", b.Helpful)
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
```

- [ ] **Step 2: 运行测试，确认编译失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/summary/task/ace/... 2>&1 | head -20`
Expected: 编译失败（package 不存在）

- [ ] **Step 3: 实现 playbook.go 中的 OperationType 枚举 + Bullet + BulletTag + DeltaOperation + DeltaBatch**

实现要点：
- `OperationType` 用 `iota` 枚举，`String()` 返回 "ADD"/"UPDATE"/"TAG"/"REMOVE"
- `ParseOperationType(s)` 不区分大小写，无效值返回 error
- `Bullet` 结构体对齐 Python Bullet dataclass，`ApplyMetadata` 增量更新 helpful/harmful/neutral
- `Bullet.Tag(tag, increment)` 只接受 "helpful"/"harmful"/"neutral"，更新 UpdatedAt
- `DeltaOperation.ToJSON()` / `NewDeltaOperationFromJSON()` 对齐 Python 的 from_json/to_json
- `DeltaBatch.ToJSON()` / `NewDeltaBatchFromJSON()` 对齐 Python 的 from_json/to_json
- 文件头部声明顺序对齐编码规范：结构体 → 枚举 → 常量 → 全局变量 → 导出函数 → 非导出函数

- [ ] **Step 4: 运行测试，确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/summary/task/ace/... -run "TestOperationType|TestParseOperationType|TestBullet|TestDeltaOperation|TestDeltaBatch" -v`
Expected: PASS

- [ ] **Step 5: 补充 Playbook 结构体及其测试**

在 `playbook_test.go` 中添加 Playbook 测试：

```go
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
	p.AddBullet("s2", "c2", nil, nil)
	result := p.MakePlaybookExcerpt([]string{"s2-00001"})
	if !strings.Contains(result, "c2") {
		t.Error("MakePlaybookExcerpt 应包含指定 bullet 内容")
	}
	if strings.Contains(result, "c1") {
		t.Error("MakePlaybookExcerpt 不应包含未指定的 bullet")
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
}

func strPtr(s string) *string { return &s }
```

在 `playbook.go` 中实现 Playbook 结构体：
- `NewPlaybook()` 初始化 `bullets`/`sections` map
- `AddBullet(section, content string, bulletID *string, metadata map[string]int) *Bullet`
- `UpdateBullet(bulletID string, content *string, metadata map[string]int) *Bullet`
- `TagBullet(bulletID, tag string, increment int) *Bullet`
- `RemoveBullet(bulletID string)`
- `GetBullet(bulletID string) *Bullet`
- `Bullets() []*Bullet`
- `BulletIDs() []string`
- `LoadBullet(bullet *Bullet)`
- `SetNextID(nextID int)`
- `ToDict() map[string]any`
- `PlaybookFromDict(payload map[string]any) *Playbook`
- `Dumps() (string, error)`
- `PlaybookLoads(data string) (*Playbook, error)`
- `ApplyDelta(delta *DeltaBatch)`
- `applyOperation(op *DeltaOperation)` — 非导出，按 ADD/UPDATE/TAG/REMOVE 分发
- `AsPrompt() string` — 生成 LLM 可读字符串，格式 `## {section}\n- [{id}] {content} (helpful=X, harmful=Y, neutral=Z)`
- `MakePlaybookExcerpt(bulletIDs []string) string`
- `Stats() map[string]any`
- `generateID(section string) string` — 非导出，`{section_prefix}-{nextID:05d}`

- [ ] **Step 6: 运行 Playbook 测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/summary/task/ace/... -run "TestPlaybook" -v`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/context_evolver/summary/task/ace/playbook.go internal/agentcore/context_evolver/summary/task/ace/playbook_test.go
git commit -m "feat(9.82-p5): 实现 Playbook 核心数据结构 (OperationType/Bullet/DeltaOperation/DeltaBatch/Playbook)"
```

---

### Task 2: ACE 包 doc.go 文件

**Files:**
- Create: `internal/agentcore/context_evolver/retrieve/task/ace/doc.go`
- Create: `internal/agentcore/context_evolver/summary/task/ace/doc.go`

- [ ] **Step 1: 创建 retrieve/task/ace/doc.go**

```go
// Package ace 提供 ACE（Adaptive Context Evolution）算法的检索操作。
//
// ACE 检索不使用语义搜索，而是全量加载 playbook bullets，
// 通过 metadata 过滤获取指定用户的所有 ACE 记忆。
//
// 文件目录：
//
//	ace/
//	├── doc.go           # 包文档
//	├── run.go           # ACERecallMemoryOp 检索操作
//	└── run_test.go      # 测试
//
// 对应 Python 代码：openjiuwen/extensions/context_evolver/retrieve/task/ace/
package ace
```

- [ ] **Step 2: 创建 summary/task/ace/doc.go**

```go
// Package ace 提供 ACE（Adaptive Context Evolution）算法的总结操作。
//
// ACE 总结管线通过 反思→策展→增量更新 的循环持续优化 Playbook：
//
//	LoadPlaybookOp >> (ReflectOp | ParallelReflectOp) >> (CurateOp | ParallelCurateOp) >> ApplyDeltaOp >> PersistMemoryOp
//
// 文件目录：
//
//	ace/
//	├── doc.go           # 包文档
//	├── playbook.go      # Playbook/DeltaBatch/Bullet/DeltaOperation/OperationType/BulletTag
//	├── update.go        # LoadPlaybookOp + ReflectOp + ParallelReflectOp + CurateOp + ParallelCurateOp + ApplyDeltaOp + PersistMemoryOp
//	├── prompt.go        # 6 个 prompt 模板 + ACEPrompt 结构体
//	└── utils.go         # SafeJSONLoads 安全 JSON 解析
//
// 对应 Python 代码：openjiuwen/extensions/context_evolver/summary/task/ace/
package ace
```

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/context_evolver/retrieve/task/ace/doc.go internal/agentcore/context_evolver/summary/task/ace/doc.go
git commit -m "docs(9.82-p5): 添加 ACE 包 doc.go"
```

---

### Task 3: utils.go — SafeJSONLoads

**Files:**
- Create: `internal/agentcore/context_evolver/summary/task/ace/utils.go`
- Test: `internal/agentcore/context_evolver/summary/task/ace/utils_test.go`

- [ ] **Step 1: 编写 utils_test.go**

```go
package ace

import (
	"testing"
)

func TestSafeJSONLoads_直接JSON(t *testing.T) {
	input := `{"reasoning": "test", "operations": []}`
	got, err := SafeJSONLoads(input)
	if err != nil {
		t.Fatalf("SafeJSONLoads 失败: %v", err)
	}
	if got["reasoning"] != "test" {
		t.Errorf("reasoning = %v, want %q", got["reasoning"], "test")
	}
}

func TestSafeJSONLoads_MarkdownCodeBlock(t *testing.T) {
	input := "```json\n{\"reasoning\": \"from block\"}\n```"
	got, err := SafeJSONLoads(input)
	if err != nil {
		t.Fatalf("SafeJSONLoads 失败: %v", err)
	}
	if got["reasoning"] != "from block" {
		t.Errorf("reasoning = %v, want %q", got["reasoning"], "from block")
	}
}

func TestSafeJSONLoads_任意JSON对象(t *testing.T) {
	input := "Some text before {\"key\": \"value\"} some text after"
	got, err := SafeJSONLoads(input)
	if err != nil {
		t.Fatalf("SafeJSONLoads 失败: %v", err)
	}
	if got["key"] != "value" {
		t.Errorf("key = %v, want %q", got["key"], "value")
	}
}

func TestSafeJSONLoads_解析失败(t *testing.T) {
	input := "no json here at all"
	_, err := SafeJSONLoads(input)
	if err == nil {
		t.Error("期望返回错误，实际返回 nil")
	}
}

func TestSafeJSONLoads_嵌套JSON(t *testing.T) {
	input := `{"reasoning": "test", "operations": [{"type": "ADD", "section": "s1", "content": "c1"}]}`
	got, err := SafeJSONLoads(input)
	if err != nil {
		t.Fatalf("SafeJSONLoads 失败: %v", err)
	}
	ops, ok := got["operations"].([]any)
	if !ok || len(ops) != 1 {
		t.Fatalf("operations 解析失败")
	}
}
```

- [ ] **Step 2: 运行测试，确认编译失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/summary/task/ace/... -run "TestSafeJSONLoads" -v 2>&1 | head -10`
Expected: 编译失败

- [ ] **Step 3: 实现 utils.go**

实现要点：
- `SafeJSONLoads(text string) (map[string]any, error)` 对齐 Python `_safe_json_loads`
- 三级回退：`json.Unmarshal` → 正则提取 `` ```json ... ``` `` → 正则提取 `{...}`
- 正则用 `regexp.MustCompile` 预编译为包级变量
- 解析失败时记录 Error 日志并返回 `fmt.Errorf("could not parse valid JSON from response")`
- logComponent 使用 `logger.ComponentAgentCore`

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/summary/task/ace/... -run "TestSafeJSONLoads" -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/context_evolver/summary/task/ace/utils.go internal/agentcore/context_evolver/summary/task/ace/utils_test.go
git commit -m "feat(9.82-p5): 实现 SafeJSONLoads 安全 JSON 解析"
```

---

### Task 4: prompt.go — 6 个 prompt 模板 + ACEPrompt 结构体

**Files:**
- Create: `internal/agentcore/context_evolver/summary/task/ace/prompt.go`
- Test: `internal/agentcore/context_evolver/summary/task/ace/prompt_test.go`

- [ ] **Step 1: 编写 prompt_test.go**

```go
package ace

import (
	"bytes"
	"testing"
)

func TestACEPrompt_所有模板可解析(t *testing.T) {
	prompts := NewACEPrompt()
	if prompts.ACEReflectorPrompt == nil {
		t.Error("ACEReflectorPrompt 未解析")
	}
	if prompts.ACEReflectorNoGTPrompt == nil {
		t.Error("ACEReflectorNoGTPrompt 未解析")
	}
	if prompts.ACECuratorPrompt == nil {
		t.Error("ACECuratorPrompt 未解析")
	}
	if prompts.ACEReflectorScalingPrompt == nil {
		t.Error("ACEReflectorScalingPrompt 未解析")
	}
	if prompts.ACEReflectorScalingNoGTPrompt == nil {
		t.Error("ACEReflectorScalingNoGTPrompt 未解析")
	}
	if prompts.ACECuratorScalingPrompt == nil {
		t.Error("ACECuratorScalingPrompt 未解析")
	}
}

func TestACEReflectorPrompt_渲染(t *testing.T) {
	prompts := NewACEPrompt()
	var buf bytes.Buffer
	err := prompts.ACEReflectorPrompt.Execute(&buf, reflectorPromptData{
		GroundTruth: "def solution(): pass",
		Feedback:    "test report",
		Playbook:    "## strategies\n- [s-00001] always auth (helpful=1, harmful=0, neutral=0)",
		Trajectory:  "trajectory text",
	})
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	result := buf.String()
	if !bytes.Contains([]byte(result), []byte("def solution(): pass")) {
		t.Error("渲染结果应包含 GroundTruth")
	}
	if !bytes.Contains([]byte(result), []byte("trajectory text")) {
		t.Error("渲染结果应包含 Trajectory")
	}
}

func TestACEReflectorNoGTPrompt_渲染(t *testing.T) {
	prompts := NewACEPrompt()
	var buf bytes.Buffer
	err := prompts.ACEReflectorNoGTPrompt.Execute(&buf, reflectorPromptData{
		Playbook:   "playbook text",
		Trajectory: "traj text",
	})
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	result := buf.String()
	if !bytes.Contains([]byte(result), []byte("playbook text")) {
		t.Error("渲染结果应包含 Playbook")
	}
}

func TestACECuratorPrompt_渲染(t *testing.T) {
	prompts := NewACEPrompt()
	var buf bytes.Buffer
	err := prompts.ACECuratorPrompt.Execute(&buf, curatorPromptData{
		QuestionContext: "find roommates",
		Playbook:        "playbook",
		Trajectory:      "trajectory",
		Reflection:      `{"reasoning": "test"}`,
	})
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	result := buf.String()
	if !bytes.Contains([]byte(result), []byte("find roommates")) {
		t.Error("渲染结果应包含 QuestionContext")
	}
	if !bytes.Contains([]byte(result), []byte(`{"reasoning": "test"}`)) {
		t.Error("渲染结果应包含 Reflection")
	}
}

func TestACEPrompt_默认实例存在(t *testing.T) {
	if defaultACEPrompt == nil {
		t.Error("defaultACEPrompt 不应为 nil")
	}
}
```

- [ ] **Step 2: 运行测试，确认编译失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/summary/task/ace/... -run "TestACEPrompt|TestACEReflector|TestACECurator" -v 2>&1 | head -10`
Expected: 编译失败

- [ ] **Step 3: 实现 prompt.go**

实现要点：
- 6 个 prompt 常量**一比一复刻** Python `prompt.py` 原文
- JSON 示例中的 `{{` 使用 `{{"{{"}}` 转义
- Go 模板占位符使用 `{{.GroundTruth}}`、`{{.Playbook}}`、`{{.Trajectory}}` 等
- 定义 4 个模板数据结构：`reflectorPromptData`、`reflectorScalingPromptData`、`curatorPromptData`、`curatorScalingPromptData`
- `NewACEPrompt()` 构造函数，用 `template.Must(template.New(...).Parse(...))` 解析
- 包级变量 `defaultACEPrompt` 供 Op 使用
- Python prompt 原文路径：`/home/opensource/agent-core/openjiuwen/extensions/context_evolver/summary/task/ace/prompt.py`

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/summary/task/ace/... -run "TestACEPrompt|TestACEReflector|TestACECurator" -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/context_evolver/summary/task/ace/prompt.go internal/agentcore/context_evolver/summary/task/ace/prompt_test.go
git commit -m "feat(9.82-p5): 实现 ACE prompt 模板 (6 个模板 + ACEPrompt 结构体)"
```

---

### Task 5: retrieve/task/ace — ACERecallMemoryOp

**Files:**
- Create: `internal/agentcore/context_evolver/retrieve/task/ace/run.go`
- Test: `internal/agentcore/context_evolver/retrieve/task/ace/run_test.go`

- [ ] **Step 1: 编写 run_test.go**

```go
package ace

import (
	"context"
	"testing"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
	coreschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/schema"
	vector_store "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/vector_store"
	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/schema"
)

func TestACERecallMemoryOp_正常检索(t *testing.T) {
	sc := cecontext.NewServiceContext()
	vs := vector_store.NewMemoryVectorStore()
	// 预填充 ACE 记忆
	vs.LoadNode("ace_user1_sec-00001", coreschema.NewVectorNode(
		"ace_user1_sec-00001",
		"test content",
		[]float64{0.1, 0.2, 0.3},
		map[string]any{
			"type":         "ace_memory",
			"workspace_id": "user1",
			"id":           "sec-00001",
			"section":      "sec",
			"content":      "test content",
			"helpful":      1,
			"harmful":      0,
			"neutral":      0,
			"created_at":   "2025-01-01T00:00:00Z",
			"updated_at":   "2025-01-01T00:00:00Z",
		},
	))
	sc.RegisterService("vector_store", vs)

	op := NewACERecallMemoryOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}

	retrieved, ok := cecontext.GetTyped[[]ceschema.ACEMemory](rc, "retrieved_memories")
	if !ok {
		t.Fatal("retrieved_memories 未设置")
	}
	_ = retrieved // ACE 全量加载，具体数量取决于 vector store 实现
}

func TestACERecallMemoryOp_VectorStore未配置(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewACERecallMemoryOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	if err == nil {
		t.Error("期望返回错误，实际返回 nil")
	}
}
```

注意：测试代码中的 import 路径需要根据实际项目 module 名调整（`github.com/uapclaw/uapclaw-go`）。

- [ ] **Step 2: 运行测试，确认编译失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/retrieve/task/ace/... -v 2>&1 | head -10`
Expected: 编译失败

- [ ] **Step 3: 实现 run.go**

实现要点：
- `ACERecallMemoryOp` 嵌入 `op.OpBase`
- `NewACERecallMemoryOp(sc *cecontext.ServiceContext) *ACERecallMemoryOp`
- `Execute(ctx context.Context, rc *cecontext.RuntimeContext) error`
- 从 RuntimeContext 获取 `user_id`（默认 "default"）
- 用 dummy embedding（`[]float64` 长度 2560，全 0）+ `top_k=50` + `metadata_filter={"workspace_id": user_id, "type": "ace_memory"}` 调用 `vectorStore.Search()`
- 遍历 VectorNode，使用 `ceschema.NewACEMemoryFromVectorNode(node)` 转换，再转为 `ceschema.ACEMemory` 设置到 `rc.Set("retrieved_memories", memories)`
- 日志对齐 Python：
  - Debug: "Loading all ACE memories from vector store..."
  - Info: "Retrieved %d ACE memories (playbook bullets)"
  - Warn: "Failed to convert ACE memory from node %s: %v"

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/retrieve/task/ace/... -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/context_evolver/retrieve/task/ace/run.go internal/agentcore/context_evolver/retrieve/task/ace/run_test.go
git commit -m "feat(9.82-p5): 实现 ACERecallMemoryOp 检索操作"
```

---

### Task 6: summary/task/ace — 7 个总结 Op

**Files:**
- Create: `internal/agentcore/context_evolver/summary/task/ace/update.go`
- Test: `internal/agentcore/context_evolver/summary/task/ace/update_test.go`

这是最大的 Task，7 个 Op 需要逐一实现和测试。

- [ ] **Step 1: 编写 update_test.go 骨架和 LoadPlaybookOp 测试**

测试文件需要 fake LLM/Embedding/VectorStore 服务，复用 P3/P4 已有的 fake 模式（同包内定义 fake struct）。

```go
package ace

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
	coreschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/schema"
	vector_store "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/vector_store"
	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeLLMService 模拟 LLM 服务
type fakeLLMService struct {
	response string
	err      error
}

// fakeEmbeddingService 模拟 Embedding 服务
type fakeEmbeddingService struct {
	embeddings [][]float64
	err        error
}

// ──────────────────────────── 导出方法 ────────────────────────────

func (f *fakeLLMService) Generate(_ context.Context, _ string, _ ...cecontext.GenerateOption) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.response, nil
}

func (f *fakeEmbeddingService) Embed(_ context.Context, _ string) ([]float64, error) {
	if f.err != nil {
		return nil, f.err
	}
	if len(f.embeddings) > 0 {
		return f.embeddings[0], nil
	}
	return []float64{0.1, 0.2, 0.3}, nil
}

func (f *fakeEmbeddingService) EmbedBatch(_ context.Context, texts []string) ([][]float64, error) {
	if f.err != nil {
		return nil, f.err
	}
	result := make([][]float64, len(texts))
	for i := range texts {
		if i < len(f.embeddings) {
			result[i] = f.embeddings[i]
		} else {
			result[i] = []float64{0.1, 0.2, 0.3}
		}
	}
	return result, nil
}

// newTestServiceContext 创建包含 fake 服务的 ServiceContext
func newTestServiceContext(llmResponse string) *cecontext.ServiceContext {
	sc := cecontext.NewServiceContext()
	sc.RegisterService("llm", &fakeLLMService{response: llmResponse})
	sc.RegisterService("embedding_model", &fakeEmbeddingService{})
	sc.RegisterService("vector_store", vector_store.NewMemoryVectorStore())
	return sc
}

// newTestServiceContextWithVS 创建包含预填充 VectorStore 的 ServiceContext
func newTestServiceContextWithVS(llmResponse string) *cecontext.ServiceContext {
	sc := cecontext.NewServiceContext()
	sc.RegisterService("llm", &fakeLLMService{response: llmResponse})
	sc.RegisterService("embedding_model", &fakeEmbeddingService{})
	vs := vector_store.NewMemoryVectorStore()
	// 预填充 2 条 ACE 记忆
	vs.LoadNode("ace_user1_sec-00001", coreschema.NewVectorNode(
		"ace_user1_sec-00001", "auth content", []float64{0.1, 0.2, 0.3},
		map[string]any{
			"type": "ace_memory", "workspace_id": "user1",
			"id": "sec-00001", "section": "sec", "content": "auth content",
			"helpful": 1, "harmful": 0, "neutral": 0,
			"created_at": "2025-01-01T00:00:00Z", "updated_at": "2025-01-01T00:00:00Z",
		},
	))
	vs.LoadNode("ace_user1_sec-00002", coreschema.NewVectorNode(
		"ace_user1_sec-00002", "other content", []float64{0.2, 0.3, 0.4},
		map[string]any{
			"type": "ace_memory", "workspace_id": "user1",
			"id": "sec-00002", "section": "sec", "content": "other content",
			"helpful": 0, "harmful": 1, "neutral": 0,
			"created_at": "2025-01-02T00:00:00Z", "updated_at": "2025-01-02T00:00:00Z",
		},
	))
	sc.RegisterService("vector_store", vs)
	return sc
}

func TestLoadPlaybookOp_正常加载(t *testing.T) {
	sc := newTestServiceContextWithVS("")
	op := NewLoadPlaybookOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}

	playbookRaw, _ := rc.Get("playbook")
	playbook, ok := playbookRaw.(*Playbook)
	if !ok || playbook == nil {
		t.Fatal("playbook 未设置或类型不正确")
	}
	if len(playbook.Bullets()) != 2 {
		t.Errorf("Bullets len = %d, want 2", len(playbook.Bullets()))
	}
}

func TestLoadPlaybookOp_空Playbook(t *testing.T) {
	sc := newTestServiceContext("")
	op := NewLoadPlaybookOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}

	playbookRaw, _ := rc.Get("playbook")
	playbook, ok := playbookRaw.(*Playbook)
	if !ok || playbook == nil {
		t.Fatal("playbook 未设置")
	}
	if len(playbook.Bullets()) != 0 {
		t.Errorf("Bullets len = %d, want 0", len(playbook.Bullets()))
	}
}

func TestLoadPlaybookOp_VectorStore未配置(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewLoadPlaybookOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	if err == nil {
		t.Error("期望返回错误")
	}
}
```

- [ ] **Step 2: 实现 LoadPlaybookOp**

在 `update.go` 中实现：
- `LoadPlaybookOp` 嵌入 `op.OpBase`
- `NewLoadPlaybookOp(sc *cecontext.ServiceContext) *LoadPlaybookOp`
- `Execute` 逻辑：Search → 转 Bullet → LoadBullet → 推算 nextID → 设置 context.playbook
- 加载失败时回退到空 Playbook（对齐 Python 的 try/except 回退）
- logComponent 使用 `logger.ComponentAgentCore`

- [ ] **Step 3: 运行 LoadPlaybookOp 测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/summary/task/ace/... -run "TestLoadPlaybookOp" -v`
Expected: PASS

- [ ] **Step 4: 补充 ReflectOp/ParallelReflectOp 测试**

在 `update_test.go` 中追加：

```go
func TestReflectOp_正常反思(t *testing.T) {
	reflectionJSON := `{"reasoning": "test reasoning", "error_identification": "wrong approach", "root_cause_analysis": "misunderstood", "correct_approach": "use API", "key_insight": "always check API first"}`
	sc := newTestServiceContext(reflectionJSON)
	op := NewReflectOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", "user1")
	rc.Set("query", "find roommates")
	rc.Set("trajectories", []string{"trajectory text"})
	rc.Set("playbook", NewPlaybook())
	rc.Set("ground_truth", "reference code")
	rc.Set("feedback", []string{"test report"})
	rc.Set("matts", "none")

	err := op.Execute(context.Background(), rc)
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	reflection, ok := cecontext.GetTyped[map[string]any](rc, "reflection")
	if !ok || reflection == nil {
		t.Fatal("reflection 未设置")
	}
	if reflection["reasoning"] != "test reasoning" {
		t.Errorf("reasoning = %v, want %q", reflection["reasoning"], "test reasoning")
	}
}

func TestReflectOp_跳过非none模式(t *testing.T) {
	sc := newTestServiceContext("should not be called")
	op := NewReflectOp(sc, false)
	rc := cecontext.NewRuntimeContext()
	rc.Set("matts", "parallel")

	err := op.Execute(context.Background(), rc)
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	// parallel 模式下 ReflectOp 应跳过，不设置 reflection
	_, ok := cecontext.GetTyped[map[string]any](rc, "reflection")
	if ok {
		t.Error("parallel 模式下不应设置 reflection")
	}
}

func TestParallelReflectOp_正常反思(t *testing.T) {
	reflectionJSON := `{"reasoning": "multi traj", "key_insight": "compare patterns"}`
	sc := newTestServiceContext(reflectionJSON)
	op := NewParallelReflectOp(sc, false)
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", "user1")
	rc.Set("query", "find roommates")
	rc.Set("trajectories", []string{"traj1", "traj2"})
	rc.Set("playbook", NewPlaybook())
	rc.Set("matts", "parallel")

	err := op.Execute(context.Background(), rc)
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	reflection, ok := cecontext.GetTyped[map[string]any](rc, "reflection")
	if !ok || reflection == nil {
		t.Fatal("reflection 未设置")
	}
}

func TestParallelReflectOp_轨迹不足(t *testing.T) {
	sc := newTestServiceContext("should not be called")
	op := NewParallelReflectOp(sc, false)
	rc := cecontext.NewRuntimeContext()
	rc.Set("matts", "parallel")
	rc.Set("trajectories", []string{"only one"})
	rc.Set("playbook", NewPlaybook())

	err := op.Execute(context.Background(), rc)
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	// 轨迹不足时不设置 reflection
	_, ok := cecontext.GetTyped[map[string]any](rc, "reflection")
	if ok {
		t.Error("轨迹不足时不应设置 reflection")
	}
}
```

- [ ] **Step 5: 实现 ReflectOp 和 ParallelReflectOp**

在 `update.go` 中实现：
- `ReflectOp` 嵌入 `op.OpBase` + `useGroundTruth bool`
- `NewReflectOp(sc, useGroundTruth bool) *ReflectOp`
- `ParallelReflectOp` 嵌入 `op.OpBase` + `useGroundTruth bool`
- `NewParallelReflectOp(sc, useGroundTruth bool) *ParallelReflectOp`
- 两者 `Execute` 逻辑对齐设计文档 4.3/4.4 节

- [ ] **Step 6: 运行 ReflectOp/ParallelReflectOp 测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/summary/task/ace/... -run "TestReflectOp|TestParallelReflectOp" -v`
Expected: PASS

- [ ] **Step 7: 补充 CurateOp/ParallelCurateOp 测试**

在 `update_test.go` 中追加：

```go
func TestCurateOp_正常策展(t *testing.T) {
	curationJSON := `{"reasoning": "need new strategy", "operations": [{"type": "ADD", "section": "strategies", "content": "always authenticate first"}]}`
	sc := newTestServiceContext(curationJSON)
	op := NewCurateOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", "user1")
	rc.Set("query", "find roommates")
	rc.Set("trajectories", []string{"trajectory"})
	rc.Set("playbook", NewPlaybook())
	rc.Set("reflection", map[string]any{"reasoning": "test"})
	rc.Set("matts", "none")

	err := op.Execute(context.Background(), rc)
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	deltaRaw, _ := rc.Get("delta")
	delta, ok := deltaRaw.(*DeltaBatch)
	if !ok || delta == nil {
		t.Fatal("delta 未设置或类型不正确")
	}
	if len(delta.Operations) != 1 {
		t.Fatalf("Operations len = %d, want 1", len(delta.Operations))
	}
	if delta.Operations[0].Type != OperationAdd {
		t.Errorf("Operations[0].Type = %v, want ADD", delta.Operations[0].Type)
	}
}

func TestCurateOp_跳过非none模式(t *testing.T) {
	sc := newTestServiceContext("should not be called")
	op := NewCurateOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("matts", "parallel")

	err := op.Execute(context.Background(), rc)
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	_, ok := rc.Get("delta")
	if ok {
		t.Error("parallel 模式下 CurateOp 不应设置 delta")
	}
}

func TestParallelCurateOp_正常策展(t *testing.T) {
	curationJSON := `{"reasoning": "multi strategy", "operations": [{"type": "TAG", "bullet_id": "s-00001", "section": "", "metadata": {"helpful": 1}}]}`
	sc := newTestServiceContext(curationJSON)
	op := NewParallelCurateOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", "user1")
	rc.Set("query", "find roommates")
	rc.Set("trajectories", []string{"traj1", "traj2"})
	rc.Set("playbook", NewPlaybook())
	rc.Set("reflection", map[string]any{"reasoning": "test"})
	rc.Set("matts", "parallel")

	err := op.Execute(context.Background(), rc)
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	deltaRaw, _ := rc.Get("delta")
	delta, ok := deltaRaw.(*DeltaBatch)
	if !ok || delta == nil {
		t.Fatal("delta 未设置")
	}
}
```

- [ ] **Step 8: 实现 CurateOp 和 ParallelCurateOp**

在 `update.go` 中实现：
- `CurateOp` 嵌入 `op.OpBase`
- `NewCurateOp(sc) *CurateOp`
- `ParallelCurateOp` 嵌入 `op.OpBase`
- `NewParallelCurateOp(sc) *ParallelCurateOp`
- 两者 `Execute` 逻辑对齐设计文档 4.5/4.6 节

- [ ] **Step 9: 运行 CurateOp/ParallelCurateOp 测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/summary/task/ace/... -run "TestCurateOp|TestParallelCurateOp" -v`
Expected: PASS

- [ ] **Step 10: 补充 ApplyDeltaOp 测试**

在 `update_test.go` 中追加：

```go
func TestApplyDeltaOp_正常应用(t *testing.T) {
	sc := newTestServiceContextWithVS("")
	op := NewApplyDeltaOp(sc, 50)
	p := NewPlaybook()
	content := "new insight"
	delta := &DeltaBatch{
		Reasoning: "test",
		Operations: []DeltaOperation{
			{Type: OperationAdd, Section: "strategies", Content: &content},
		},
	}

	rc := cecontext.NewRuntimeContext()
	rc.Set("delta", delta)
	rc.Set("playbook", p)
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	memories, ok := cecontext.GetTyped[[]ceschema.ACEMemory](rc, "memories")
	if !ok {
		t.Fatal("memories 未设置")
	}
	if len(memories) != 1 {
		t.Errorf("memories len = %d, want 1", len(memories))
	}
}

func TestApplyDeltaOp_空Delta(t *testing.T) {
	sc := newTestServiceContextWithVS("")
	op := NewApplyDeltaOp(sc, 50)
	rc := cecontext.NewRuntimeContext()
	rc.Set("delta", &DeltaBatch{Reasoning: "", Operations: nil})
	rc.Set("playbook", NewPlaybook())
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	memories, ok := cecontext.GetTyped[[]ceschema.ACEMemory](rc, "memories")
	if !ok {
		t.Fatal("memories 未设置")
	}
	if len(memories) != 0 {
		t.Errorf("memories len = %d, want 0", len(memories))
	}
}

func TestApplyDeltaOp_淘汰低分(t *testing.T) {
	sc := newTestServiceContextWithVS("")
	// maxBullets=2，已有 2 条，再 ADD 1 条应淘汰 1 条
	op := NewApplyDeltaOp(sc, 2)
	p := NewPlaybook()
	p.AddBullet("sec", "high score", nil, map[string]int{"helpful": 10})
	p.AddBullet("sec", "low score", nil, map[string]int{"harmful": 5})
	content := "new bullet"
	delta := &DeltaBatch{
		Reasoning: "test",
		Operations: []DeltaOperation{
			{Type: OperationAdd, Section: "sec", Content: &content},
		},
	}

	rc := cecontext.NewRuntimeContext()
	rc.Set("delta", delta)
	rc.Set("playbook", p)
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	// 淘汰后 playbook 应有 2 条 bullet（保留高分 + 新增）
	if len(p.Bullets()) != 2 {
		t.Errorf("Bullets len = %d, want 2", len(p.Bullets()))
	}
}

func TestApplyDeltaOp_Update降级为ADD(t *testing.T) {
	sc := newTestServiceContextWithVS("")
	op := NewApplyDeltaOp(sc, 50)
	p := NewPlaybook()
	content := "new content"
	bulletID := "nonexist-99999"
	delta := &DeltaBatch{
		Reasoning: "test",
		Operations: []DeltaOperation{
			{Type: OperationUpdate, Section: "general", BulletID: &bulletID, Content: &content},
		},
	}

	rc := cecontext.NewRuntimeContext()
	rc.Set("delta", delta)
	rc.Set("playbook", p)
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	// UPDATE 找不到 bullet 时降级为 ADD
	if len(p.Bullets()) != 1 {
		t.Errorf("Bullets len = %d, want 1", len(p.Bullets()))
	}
}
```

- [ ] **Step 11: 实现 ApplyDeltaOp**

在 `update.go` 中实现：
- `ApplyDeltaOp` 嵌入 `op.OpBase` + `maxBullets int`
- `NewApplyDeltaOp(sc *cecontext.ServiceContext, maxBullets int) *ApplyDeltaOp`
- `Execute` 逻辑对齐设计文档 4.7 节，包括：
  - 淘汰低分 bullet（score=helpful-harmful 升序）
  - ADD/UPDATE/TAG/REMOVE 分发
  - UPDATE 找不到 bullet 时降级为 ADD
  - 删除 removed bullets 从 vector store
  - 更新 affected bullets 到 vector store（embed + upsert）

- [ ] **Step 12: 运行 ApplyDeltaOp 测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/summary/task/ace/... -run "TestApplyDeltaOp" -v`
Expected: PASS

- [ ] **Step 13: 补充 PersistMemoryOp 测试**

在 `update_test.go` 中追加：

```go
func TestPersistMemoryOp_正常持久化(t *testing.T) {
	// 使用 t.TempDir() 创建临时目录
	tmpDir := t.TempDir()
	helper := cepersistence.NewMemoryPersistenceHelper(
		"json",
		fmt.Sprintf("%s/{algo_name}/{user_id}.json", tmpDir),
		"localhost", 19530, "vector_nodes",
	)

	sc := newTestServiceContextWithVS("")
	op := NewPersistMemoryOp(sc, helper)
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	persistCount, ok := cecontext.GetTyped[int](rc, "persist_count")
	if !ok {
		t.Fatal("persist_count 未设置")
	}
	if persistCount != 2 {
		t.Errorf("persist_count = %d, want 2", persistCount)
	}
}

func TestPersistMemoryOp_无记忆(t *testing.T) {
	tmpDir := t.TempDir()
	helper := cepersistence.NewMemoryPersistenceHelper(
		"json",
		fmt.Sprintf("%s/{algo_name}/{user_id}.json", tmpDir),
		"localhost", 19530, "vector_nodes",
	)

	sc := newTestServiceContext("") // 空 VectorStore
	op := NewPersistMemoryOp(sc, helper)
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	persistCount, ok := cecontext.GetTyped[int](rc, "persist_count")
	if !ok {
		t.Fatal("persist_count 未设置")
	}
	if persistCount != 0 {
		t.Errorf("persist_count = %d, want 0", persistCount)
	}
}
```

- [ ] **Step 14: 实现 PersistMemoryOp**

在 `update.go` 中实现：
- `PersistMemoryOp` 嵌入 `op.OpBase` + `helper *cepersistence.MemoryPersistenceHelper`
- `NewPersistMemoryOp(sc *cecontext.ServiceContext, helper *cepersistence.MemoryPersistenceHelper) *PersistMemoryOp`
- 包级常量 `aceAlgoName = "ace"`
- `Execute` 逻辑对齐设计文档 4.8 节

- [ ] **Step 15: 运行 PersistMemoryOp 测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/summary/task/ace/... -run "TestPersistMemoryOp" -v`
Expected: PASS

- [ ] **Step 16: 运行所有 update 测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/summary/task/ace/... -run "TestLoadPlaybookOp|TestReflectOp|TestParallelReflectOp|TestCurateOp|TestParallelCurateOp|TestApplyDeltaOp|TestPersistMemoryOp" -v`
Expected: PASS

- [ ] **Step 17: 提交**

```bash
git add internal/agentcore/context_evolver/summary/task/ace/update.go internal/agentcore/context_evolver/summary/task/ace/update_test.go
git commit -m "feat(9.82-p5): 实现 ACE 总结 Op (LoadPlaybookOp/ReflectOp/ParallelReflectOp/CurateOp/ParallelCurateOp/ApplyDeltaOp/PersistMemoryOp)"
```

---

### Task 7: doc.go 回填 + 全量测试

**Files:**
- Modify: `internal/agentcore/context_evolver/doc.go`

- [ ] **Step 1: 更新根 doc.go 文件目录段**

在 `internal/agentcore/context_evolver/doc.go` 的文件目录段中加入 ACE 条目：

```
├── retrieve/task/ace/          # ACE 检索管线 (ACERecallMemoryOp)
├── summary/task/ace/           # ACE 总结管线 (LoadPlaybookOp + ReflectOp + ParallelReflectOp + CurateOp + ParallelCurateOp + ApplyDeltaOp + PersistMemoryOp + Playbook + prompt + utils)
```

- [ ] **Step 2: 运行全量 context_evolver 包测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/... -v 2>&1 | tail -30`
Expected: PASS（所有测试）

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/context_evolver/doc.go
git commit -m "docs(9.82-p5): 回填 doc.go 文件目录段，加入 ACE 条目"
```

---

### Task 8: 覆盖率检查 + 编译验证

- [ ] **Step 1: 编译检查**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 编译通过

- [ ] **Step 2: 覆盖率检查**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/context_evolver/... 2>&1 | grep -E "ace|coverage|FAIL"`
Expected: ACE 相关包覆盖率 ≥ 85%

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md**

在 `IMPLEMENTATION_PLAN.md` 中将 9.82 P5 状态从 `☐` 改为 `✅`。

- [ ] **Step 4: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 9.82 P5 ACE 状态为已完成"
```
