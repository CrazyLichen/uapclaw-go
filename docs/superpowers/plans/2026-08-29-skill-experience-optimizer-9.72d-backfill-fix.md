# 9.72d 回填修复实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 TeamSkillExperienceOptimizer 中 evolutionStore 未被使用的问题，并将 EvolutionStore 接口迁移到 checkpointing 包 + LoadFullEvolutionLog 签名对齐 Python 异常语义

**Architecture:** 1) 将 `LoadFullEvolutionLog` 返回值改为 `(*EvolutionLog, error)`，内部错误场景返回 `(nil, err)`，适配所有调用方；2) 在 checkpointing 包新增 `EvolutionStoreReader` 接口，team_optimizer.go 删除局部接口并改用新接口；3) 补全 `loadSkillContent` / `loadExistingEvolutionsSummary` 两个方法并在 GenerateUserPatch/GenerateTrajectoryPatch 中使用

**Tech Stack:** Go 1.23+, 标准库

---

### Task 1: LoadFullEvolutionLog 签名变更 — StoreRecordsHelper

**Files:**
- Modify: `internal/evolving/checkpointing/store_records.go:88-120`

- [ ] **Step 1: 修改 StoreRecordsHelper.LoadFullEvolutionLog 签名和实现**

将 `store_records.go:90` 的函数签名从 `func (h *StoreRecordsHelper) LoadFullEvolutionLog(ctx context.Context, name string) *EvolutionLog` 改为 `func (h *StoreRecordsHelper) LoadFullEvolutionLog(ctx context.Context, name string) (*EvolutionLog, error)`。

修改内部逻辑：

```go
// LoadFullEvolutionLog 加载完整演进日志。
// 对应 Python: StoreRecordsHelper.load_full_evolution_log(name)
func (h *StoreRecordsHelper) LoadFullEvolutionLog(ctx context.Context, name string) (*EvolutionLog, error) {
	skillDir := h.store.ResolveSkillDir(ctx, name)
	if skillDir == "" {
		return nil, fmt.Errorf("skill %s not found in evolution store", name)
	}
	evoPath := filepath.Join(skillDir, evolutionFilename)
	if !isFile(evoPath) {
		return EmptyEvolutionLog(name), nil
	}
	fileContent, err := h.store.ReadFileText(ctx, evoPath)
	if err != nil {
		return nil, fmt.Errorf("read evolution log for skill %s: %w", name, err)
	}
	if fileContent == "" {
		return EmptyEvolutionLog(name), nil
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(fileContent), &data); err != nil {
		return nil, fmt.Errorf("parse evolution log for skill %s: %w", name, err)
	}
	evoLog, err := FromDictEvolutionLog(data)
	if err != nil {
		return nil, fmt.Errorf("decode evolution log for skill %s: %w", name, err)
	}
	return evoLog, nil
}
```

- [ ] **Step 2: 适配 store_records.go 内部 6 处 LoadFullEvolutionLog 调用**

在以下 6 个方法中，将 `evoLog := h.LoadFullEvolutionLog(ctx, name)` 改为 `evoLog, err := h.LoadFullEvolutionLog(ctx, name)`，并添加错误处理：

1. **`UpdateRecordScores` (line 148)**：
```go
evoLog, err := h.LoadFullEvolutionLog(ctx, name)
if err != nil {
	return 0, fmt.Errorf("load evolution log for update_record_scores: %w", err)
}
```

2. **`GetRecordsByScore` (line 181)**：
```go
evoLog, err := h.LoadFullEvolutionLog(ctx, name)
if err != nil {
	logger.Warn(logComponent).Str("skill", name).Err(err).Msg("[EvolutionStore] get_records_by_score: 加载失败")
	return nil
}
```

3. **`DeleteRecords` (line 203)**：
```go
evoLog, err := h.LoadFullEvolutionLog(ctx, name)
if err != nil {
	return 0, fmt.Errorf("load evolution log for delete_records: %w", err)
}
```

4. **`MarkRecordsApplied` (line 238)**：
```go
evoLog, err := h.LoadFullEvolutionLog(ctx, name)
if err != nil {
	return 0, fmt.Errorf("load evolution log for mark_records_applied: %w", err)
}
```

5. **`MergeRecords` (line 267)**：
```go
evoLog, err := h.LoadFullEvolutionLog(ctx, name)
if err != nil {
	return nil, fmt.Errorf("load evolution log for merge_records: %w", err)
}
```

6. **`UpdateRecordContent` (line 335)**：
```go
evoLog, err := h.LoadFullEvolutionLog(ctx, name)
if err != nil {
	return nil, fmt.Errorf("load evolution log for update_record_content: %w", err)
}
```

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/evolving/checkpointing/...`
Expected: 编译失败（EvolutionStore.LoadFullEvolutionLog 签名不匹配），这是预期的，Task 2 继续

- [ ] **Step 4: Commit**

```
feat: change LoadFullEvolutionLog to return (*EvolutionLog, error) in StoreRecordsHelper
```

---

### Task 2: LoadFullEvolutionLog 签名变更 — EvolutionStore 代理层

**Files:**
- Modify: `internal/evolving/checkpointing/evolution_store.go:489-491`

- [ ] **Step 1: 修改 EvolutionStore.LoadFullEvolutionLog 签名**

```go
// 变更前
func (s *EvolutionStore) LoadFullEvolutionLog(ctx context.Context, name string) *EvolutionLog {
	return s.records.LoadFullEvolutionLog(ctx, name)
}

// 变更后
func (s *EvolutionStore) LoadFullEvolutionLog(ctx context.Context, name string) (*EvolutionLog, error) {
	return s.records.LoadFullEvolutionLog(ctx, name)
}
```

- [ ] **Step 2: 适配 LoadEvolutionLog (line 404-421)**

`LoadEvolutionLog` 内部调用了 `LoadFullEvolutionLog`，需适配：

```go
// 变更前
func (s *EvolutionStore) LoadEvolutionLog(ctx context.Context, name string, target *signal.EvolutionTarget) *EvolutionLog {
	evoLog := s.LoadFullEvolutionLog(ctx, name)
	// ...

// 变更后
func (s *EvolutionStore) LoadEvolutionLog(ctx context.Context, name string, target *signal.EvolutionTarget) *EvolutionLog {
	evoLog, err := s.LoadFullEvolutionLog(ctx, name)
	if err != nil {
		logger.Warn(logComponent).Str("skill", name).Err(err).Msg("[EvolutionStore] load_evolution_log failed")
		return EmptyEvolutionLog(name)
	}
	// ... 后续过滤逻辑不变
```

- [ ] **Step 3: 适配 AppendRecord (line 442)**

```go
// 变更前
evoLog := s.LoadFullEvolutionLog(ctx, name)

// 变更后
evoLog, loadErr := s.LoadFullEvolutionLog(ctx, name)
if loadErr != nil {
	s.getSkillLock(name).Unlock()
	return fmt.Errorf("load evolution log for append_record: %w", loadErr)
}
```

- [ ] **Step 4: 适配 store_projection.go (line 42)**

```go
// 变更前
evoLog := h.store.LoadFullEvolutionLog(ctx, name)

// 变更后
evoLog, err := h.store.LoadFullEvolutionLog(ctx, name)
if err != nil {
	logger.Warn(logComponent).Str("skill", name).Err(err).Msg("[StoreProjection] 加载演进日志失败")
	return err
}
```

注意：`RenderEvolutionMarkdown` 的返回类型是 `error`，所以可以直接返回。

- [ ] **Step 5: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/evolving/checkpointing/...`
Expected: 编译失败（experience 包调用方未适配），Task 3 继续

- [ ] **Step 6: Commit**

```
feat: change EvolutionStore.LoadFullEvolutionLog to return (*EvolutionLog, error)
```

---

### Task 3: LoadFullEvolutionLog 签名变更 — experience 包调用方

**Files:**
- Modify: `internal/evolving/experience/manager.go:360`
- Modify: `internal/evolving/experience/tracker.go:164`
- Modify: `internal/evolving/experience/common.go:236`

- [ ] **Step 1: 适配 manager.go:360**

```go
// 变更前
evoLog := m.store.LoadFullEvolutionLog(ctx, skillName)

// 变更后
evoLog, err := m.store.LoadFullEvolutionLog(ctx, skillName)
if err != nil {
	return "", fmt.Errorf("load evolution log for request_simplify: %w", err)
}
```

- [ ] **Step 2: 适配 tracker.go:164**

```go
// 变更前
evoLog := t.store.LoadFullEvolutionLog(ctx, skillName)

// 变更后
evoLog, err := t.store.LoadFullEvolutionLog(ctx, skillName)
if err != nil {
	return fmt.Errorf("load evolution log for record_presented_records: %w", err)
}
```

- [ ] **Step 3: 适配 common.go:236**

```go
// 变更前
recordsLog := store.LoadFullEvolutionLog(ctx, request.SkillName)

// 变更后
recordsLog, err := store.LoadFullEvolutionLog(ctx, request.SkillName)
if err != nil {
	return nil, fmt.Errorf("load evolution log for rebuild_context: %w", err)
}
```

- [ ] **Step 4: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/evolving/...`
Expected: 编译失败（测试文件未适配），Task 4 继续

- [ ] **Step 5: Commit**

```
feat: adapt experience package for LoadFullEvolutionLog error return
```

---

### Task 4: LoadFullEvolutionLog 签名变更 — 测试文件适配

**Files:**
- Modify: `internal/evolving/checkpointing/checkpointing_test.go` (多处)
- Modify: `internal/evolving/optimizer/skill_call/llm_mock_test.go:1622,1636`

- [ ] **Step 1: 适配 checkpointing_test.go 中所有 LoadFullEvolutionLog 调用**

搜索 `checkpointing_test.go` 中所有 `LoadFullEvolutionLog` 调用，添加 `err` 接收和 `require.NoError` 断言。

模式：
```go
// 变更前
evoLog := store.LoadFullEvolutionLog(ctx, "xxx")

// 变更后
evoLog, err := store.LoadFullEvolutionLog(ctx, "xxx")
require.NoError(t, err)
```

涉及行号（近似）：1462, 1805, 2332, 2350, 2400, 2410, 2437, 2447, 2460, 2479, 2508, 2736

- [ ] **Step 2: 适配 llm_mock_test.go:1622,1636**

```go
// 变更前
log := opt.evolutionStore.LoadFullEvolutionLog(context.Background(), "test_skill")

// 变更后
log, err := opt.evolutionStore.LoadFullEvolutionLog(context.Background(), "test_skill")
require.NoError(t, err)
```

```go
// 变更前
log := store.LoadFullEvolutionLog(context.Background(), "any")

// 变更后
log, err := store.LoadFullEvolutionLog(context.Background(), "any")
require.NoError(t, err)
```

注意：此时 `mockEvolutionStore` 的方法签名也需要适配，但完整 mock 重构在 Task 6。此处先将 mock 的 `LoadFullEvolutionLog` 改为返回 `(*checkpointing.EvolutionLog, error)`。

- [ ] **Step 3: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/checkpointing/... ./internal/evolving/experience/... ./internal/evolving/optimizer/skill_call/... -count=1 -timeout 120s`
Expected: PASS

- [ ] **Step 4: Commit**

```
test: adapt test files for LoadFullEvolutionLog error return
```

---

### Task 5: checkpointing 包新增 EvolutionStoreReader 接口

**Files:**
- Modify: `internal/evolving/checkpointing/evolution_store.go`

- [ ] **Step 1: 在 evolution_store.go 结构体区块（interface 之前）新增接口**

在 `BaseOptimizer` 接口或 `EvolutionStore` 结构体之前，新增：

```go
// EvolutionStoreReader 技能经验优化器所需的演进存储只读接口。
// 从 skill_call/team_optimizer.go 迁移至此，与 Python evolution_store.py 位置对齐。
//
// 对应 Python: EvolutionStore
type EvolutionStoreReader interface {
	// ReadSkillContent 读取技能内容
	ReadSkillContent(ctx context.Context, skillName string) (string, error)
	// LoadFullEvolutionLog 加载完整演进日志
	LoadFullEvolutionLog(ctx context.Context, skillName string) (*EvolutionLog, error)
}
```

- [ ] **Step 2: 验证 EvolutionStore 结构体满足接口**

`checkpointing.EvolutionStore` 已有 `ReadSkillContent(ctx context.Context, name string) (string, error)` 和修改后的 `LoadFullEvolutionLog(ctx context.Context, name string) (*EvolutionLog, error)`，签名匹配。编译期自动满足。

可添加编译期断言：
```go
var _ EvolutionStoreReader = (*EvolutionStore)(nil)
```

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/evolving/checkpointing/...`
Expected: PASS

- [ ] **Step 4: Commit**

```
feat: add EvolutionStoreReader interface to checkpointing package
```

---

### Task 6: team_optimizer.go 删除局部接口 + 字段类型迁移

**Files:**
- Modify: `internal/evolving/optimizer/skill_call/team_optimizer.go`

- [ ] **Step 1: 删除局部 EvolutionStore 接口 (lines 26-32)**

删除以下代码：
```go
// EvolutionStore 接口 — 用于 loadSkillContent / loadExistingEvolutionsSummary。
// 对齐 Python: EvolutionStore；签名对齐 checkpointing.EvolutionStore 结构体方法。
type EvolutionStore interface {
	// ReadSkillContent 读取技能内容
	ReadSkillContent(ctx context.Context, skillName string) (string, error)
	// LoadFullEvolutionLog 加载完整演进日志
	LoadFullEvolutionLog(ctx context.Context, skillName string) *EvolutionLog
}
```

- [ ] **Step 2: 修改 evolutionStore 字段类型**

```go
// 变更前
evolutionStore EvolutionStore

// 变更后
evolutionStore checkpointing.EvolutionStoreReader
```

- [ ] **Step 3: 修改 NewTeamSkillExperienceOptimizer 参数类型**

```go
// 变更前
func NewTeamSkillExperienceOptimizer(llmModel *llm.Model, model string, language string, debugDir string, recordLLMPolicy llm_resilience.LLMInvokePolicy, evolutionStore EvolutionStore) *TeamSkillExperienceOptimizer {

// 变更后
func NewTeamSkillExperienceOptimizer(llmModel *llm.Model, model string, language string, debugDir string, recordLLMPolicy llm_resilience.LLMInvokePolicy, evolutionStore checkpointing.EvolutionStoreReader) *TeamSkillExperienceOptimizer {
```

- [ ] **Step 4: 添加 checkpointing import**

确保 `team_optimizer.go` 的 import 中包含：
```go
"github.com/uapclaw/uap-claw-go/internal/evolving/checkpointing"
```

- [ ] **Step 5: 更新 llm_mock_test.go 中的 mockEvolutionStore**

将 mock 从满足旧局部 `EvolutionStore` 接口改为满足 `checkpointing.EvolutionStoreReader` 接口：

```go
type mockEvolutionStore struct {
	skillContent string
	evoLog       *checkpointing.EvolutionLog
	readErr      error
	loadErr      error
}

func (m *mockEvolutionStore) ReadSkillContent(ctx context.Context, name string) (string, error) {
	return m.skillContent, m.readErr
}

func (m *mockEvolutionStore) LoadFullEvolutionLog(ctx context.Context, name string) (*checkpointing.EvolutionLog, error) {
	return m.evoLog, m.loadErr
}
```

注意：旧的 `mockEvolutionStore` 可能只有 `ReadSkillContent` 和 `LoadFullEvolutionLog` 两个方法。如果旧 mock 还有 `SkillExists` 等方法，检查是否仍被使用，未使用则删除。

- [ ] **Step 6: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/evolving/optimizer/skill_call/...`
Expected: PASS

- [ ] **Step 7: Commit**

```
refactor: migrate EvolutionStore interface to checkpointing.EvolutionStoreReader
```

---

### Task 7: 补全 loadSkillContent / loadExistingEvolutionsSummary 方法

**Files:**
- Modify: `internal/evolving/optimizer/skill_call/team_optimizer.go`

- [ ] **Step 1: 在非导出函数区新增 loadSkillContent**

在 `team_optimizer.go` 的 `// ──────────────────────────── 非导出函数 ────────────────────────────` 区块，`callLLM` 之前新增：

```go
// loadSkillContent 从 evolutionStore 读取技能内容摘要。
// 对齐 Python: TeamSkillExperienceOptimizer._load_skill_content(skill_name)
func (o *TeamSkillExperienceOptimizer) loadSkillContent(
	ctx context.Context,
	skillName string,
) string {
	if o.evolutionStore == nil {
		return langDefault("无", "N/A", o.language)
	}
	content, err := o.evolutionStore.ReadSkillContent(ctx, skillName)
	if err != nil {
		logger.Warn(logComponent).
			Str("skill_name", skillName).
			Err(err).
			Msg("[TeamSkillOptimizer] loadSkillContent 失败，使用 fallback")
		return langDefault("无", "N/A", o.language)
	}
	if strings.TrimSpace(content) == "" {
		return langDefault("无", "N/A", o.language)
	}
	return summarizeSkillContentTeam(content)
}
```

- [ ] **Step 2: 新增 loadExistingEvolutionsSummary**

紧跟 `loadSkillContent` 之后：

```go
// loadExistingEvolutionsSummary 从 evolutionStore 加载已有演进经验摘要。
// 对齐 Python: TeamSkillExperienceOptimizer._load_existing_evolutions_summary(skill_name)
func (o *TeamSkillExperienceOptimizer) loadExistingEvolutionsSummary(
	ctx context.Context,
	skillName string,
) string {
	if o.evolutionStore == nil {
		return langDefault("无已有演进经验", "No existing evolution records", o.language)
	}
	evoLog, err := o.evolutionStore.LoadFullEvolutionLog(ctx, skillName)
	if err != nil {
		logger.Warn(logComponent).
			Str("skill_name", skillName).
			Err(err).
			Msg("[TeamSkillOptimizer] loadExistingEvolutionsSummary 失败，使用 fallback")
		return langDefault("无已有演进经验", "No existing evolution records", o.language)
	}
	if evoLog == nil {
		return langDefault("无已有演进经验", "No existing evolution records", o.language)
	}
	return summarizeExistingEvolutions(evoLog.Entries, o.language)
}
```

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/evolving/optimizer/skill_call/...`
Expected: PASS

- [ ] **Step 4: Commit**

```
feat: add loadSkillContent and loadExistingEvolutionsSummary methods
```

---

### Task 8: GenerateUserPatch / GenerateTrajectoryPatch 使用新方法

**Files:**
- Modify: `internal/evolving/optimizer/skill_call/team_optimizer.go`

- [ ] **Step 1: 替换 GenerateUserPatch 中的 fallback 调用**

在 `GenerateUserPatch` 方法中（约 line 370-371）：

```go
// 变更前
skillContent := summarizeSkillContentTeamFallback(skillName, o.language)
existingEvolutions := langDefault("无已有演进经验", "No existing evolution records", o.language)

// 变更后
skillContent := o.loadSkillContent(ctx, skillName)
existingEvolutions := o.loadExistingEvolutionsSummary(ctx, skillName)
```

- [ ] **Step 2: 替换 GenerateTrajectoryPatch 中的 fallback 调用**

在 `GenerateTrajectoryPatch` 方法中（约 line 466）：

```go
// 变更前
existingEvolutions := langDefault("无已有演进经验", "No existing evolution records", o.language)

// 变更后
existingEvolutions := o.loadExistingEvolutionsSummary(ctx, skillName)
```

- [ ] **Step 3: 删除不再使用的 summarizeSkillContentTeamFallback 函数**

删除 `team_optimizer.go` 末尾（约 line 979-983）：

```go
// 删除以下函数
func summarizeSkillContentTeamFallback(skillName string, language string) string {
	if language == "en" {
		return "N/A"
	}
	return "无"
}
```

- [ ] **Step 4: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/evolving/optimizer/skill_call/...`
Expected: PASS

- [ ] **Step 5: 运行现有测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/optimizer/skill_call/... -count=1 -timeout 120s`
Expected: PASS（所有现有测试传 nil evolutionStore，行为不变）

- [ ] **Step 6: Commit**

```
feat: use loadSkillContent/loadExistingEvolutionsSummary in GenerateUserPatch/GenerateTrajectoryPatch
```

---

### Task 9: 新增 loadSkillContent / loadExistingEvolutionsSummary 测试

**Files:**
- Modify: `internal/evolving/optimizer/skill_call/team_optimizer_test.go`
- Modify: `internal/evolving/optimizer/skill_call/llm_mock_test.go`

- [ ] **Step 1: 确认 mockEvolutionStore 已在 llm_mock_test.go 中**

Task 6 已更新 mock，确保 `skillContent`、`evoLog`、`readErr`、`loadErr` 字段可用。

- [ ] **Step 2: 在 team_optimizer_test.go 新增 loadSkillContent 测试**

```go
func TestLoadSkillContent_无Store(t *testing.T) {
	opt := NewTeamSkillExperienceOptimizer(nil, "test", "cn", "", TeamSkillRecordLLMPolicy, nil)
	result := opt.loadSkillContent(context.Background(), "any_skill")
	assert.Equal(t, "无", result)
}

func TestLoadSkillContent_无Store_英文(t *testing.T) {
	opt := NewTeamSkillExperienceOptimizer(nil, "test", "en", "", TeamSkillRecordLLMPolicy, nil)
	result := opt.loadSkillContent(context.Background(), "any_skill")
	assert.Equal(t, "N/A", result)
}

func TestLoadSkillContent_有Store(t *testing.T) {
	store := &mockEvolutionStore{skillContent: "# My Skill\nSome content here"}
	opt := NewTeamSkillExperienceOptimizer(nil, "test", "cn", "", TeamSkillRecordLLMPolicy, store)
	result := opt.loadSkillContent(context.Background(), "my_skill")
	assert.Contains(t, result, "My Skill")
}

func TestLoadSkillContent_Store报错(t *testing.T) {
	store := &mockEvolutionStore{readErr: fmt.Errorf("disk error")}
	opt := NewTeamSkillExperienceOptimizer(nil, "test", "cn", "", TeamSkillRecordLLMPolicy, store)
	result := opt.loadSkillContent(context.Background(), "my_skill")
	assert.Equal(t, "无", result)
}

func TestLoadSkillContent_空内容(t *testing.T) {
	store := &mockEvolutionStore{skillContent: "   "}
	opt := NewTeamSkillExperienceOptimizer(nil, "test", "cn", "", TeamSkillRecordLLMPolicy, store)
	result := opt.loadSkillContent(context.Background(), "my_skill")
	assert.Equal(t, "无", result)
}
```

- [ ] **Step 3: 新增 loadExistingEvolutionsSummary 测试**

```go
func TestLoadExistingEvolutionsSummary_无Store(t *testing.T) {
	opt := NewTeamSkillExperienceOptimizer(nil, "test", "cn", "", TeamSkillRecordLLMPolicy, nil)
	result := opt.loadExistingEvolutionsSummary(context.Background(), "any_skill")
	assert.Equal(t, "无已有演进经验", result)
}

func TestLoadExistingEvolutionsSummary_无Store_英文(t *testing.T) {
	opt := NewTeamSkillExperienceOptimizer(nil, "test", "en", "", TeamSkillRecordLLMPolicy, nil)
	result := opt.loadExistingEvolutionsSummary(context.Background(), "any_skill")
	assert.Equal(t, "No existing evolution records", result)
}

func TestLoadExistingEvolutionsSummary_有Store(t *testing.T) {
	evoLog := &checkpointing.EvolutionLog{
		SkillID: "my_skill",
		Entries: []checkpointing.EvolutionRecord{
			{ID: "r1", Change: checkpointing.EvolutionPatch{Section: "Body", Content: "Some advice"}},
		},
	}
	store := &mockEvolutionStore{evoLog: evoLog}
	opt := NewTeamSkillExperienceOptimizer(nil, "test", "cn", "", TeamSkillRecordLLMPolicy, store)
	result := opt.loadExistingEvolutionsSummary(context.Background(), "my_skill")
	assert.Contains(t, result, "已有演进经验")
	assert.Contains(t, result, "r1")
}

func TestLoadExistingEvolutionsSummary_Store报错(t *testing.T) {
	store := &mockEvolutionStore{loadErr: fmt.Errorf("io error")}
	opt := NewTeamSkillExperienceOptimizer(nil, "test", "cn", "", TeamSkillRecordLLMPolicy, store)
	result := opt.loadExistingEvolutionsSummary(context.Background(), "my_skill")
	assert.Equal(t, "无已有演进经验", result)
}

func TestLoadExistingEvolutionsSummary_空日志(t *testing.T) {
	store := &mockEvolutionStore{evoLog: nil, loadErr: nil}
	opt := NewTeamSkillExperienceOptimizer(nil, "test", "cn", "", TeamSkillRecordLLMPolicy, store)
	result := opt.loadExistingEvolutionsSummary(context.Background(), "my_skill")
	assert.Equal(t, "无已有演进经验", result)
}
```

- [ ] **Step 4: 运行新增测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/optimizer/skill_call/... -run "TestLoadSkillContent|TestLoadExistingEvolutionsSummary" -count=1 -timeout 60s`
Expected: PASS

- [ ] **Step 5: Commit**

```
test: add loadSkillContent and loadExistingEvolutionsSummary tests
```

---

### Task 10: 更新 IMPLEMENTATION_PLAN.md 9.72d 行

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md:620`

- [ ] **Step 1: 更新 9.72d 行内容**

```markdown
<!-- 变更前 -->
| 9.72d | ✅ | SkillExperienceOptimizer | 技能经验优化器（LLM 生成经验草稿→EvolutionRecord）+ TeamSkillExperienceOptimizer；✅ TextualParameter.Gradients string→any 前置变更 | `openjiuwen/agent_evolving/optimizer/skill_call/` |

<!-- 变更后 -->
| 9.72d | ✅ | SkillExperienceOptimizer | 技能经验优化器（LLM 生成经验草稿→EvolutionRecord）+ TeamSkillExperienceOptimizer；✅ TextualParameter.Gradients string→any 前置变更；⤴️ 9.79 orchestrator 回填验证 ✅；⤴️ optimizer/doc.go 回填验证 ✅；⤴️ Gradients any 回填验证 ✅ | `openjiuwen/agent_evolving/optimizer/skill_call/` |
```

- [ ] **Step 2: Commit**

```
docs: update 9.72d backfill verification markers in IMPLEMENTATION_PLAN.md
```

---

### Task 11: 全量编译 + 测试验证

**Files:**
- None (verification only)

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uap-claw-go && go build ./...`
Expected: PASS

- [ ] **Step 2: 运行受影响包的测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/... -count=1 -timeout 180s`
Expected: PASS

- [ ] **Step 3: 检查覆盖率**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/evolving/checkpointing/... ./internal/evolving/experience/... ./internal/evolving/optimizer/skill_call/...`
Expected: 各包覆盖率 ≥ 85%
