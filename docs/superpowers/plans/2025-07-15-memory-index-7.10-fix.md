# 7.10 Memory Index 逻辑补齐 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 补齐 Go 端 memory/manage/index 包与 Python 端的 9 项逻辑差异，包含接口重构、日志修复、时区统一、错误传播修复。

**Architecture:** 以 M2（接口重构）为核心先改接口再适配实现，其余修复按依赖顺序逐步进行。所有修改限定在 `internal/agentcore/memory/manage/index/` 包内 + 对应测试文件。

**Tech Stack:** Go 1.x, zerolog 结构化日志, `mem_model` 记忆数据模型, `foundation/store/index` 存储索引

**设计文档:** `docs/superpowers/specs/2025-07-15-memory-index-7.10-fix-design.md`

---

## 文件变更映射

| 文件 | 操作 | 职责 |
|------|------|------|
| `base_manager.go` | 修改 | 新增 MemoryOption 类型 + 修改 BaseMemoryManager 接口签名 + encrypt/decrypt 移到 memoryManagerBase |
| `base_manager_test.go` | 修改 | 适配接口变更 + 新增 MemoryOption 测试 + encrypt/decrypt 测试改 receiver 调用 |
| `fragment_manager.go` | 修改 | 接口适配 + H1 日志修复 + H4 返回原始列表 + M1 debug 日志 + M7 UTC |
| `fragment_manager_test.go` | 修改 | 适配接口签名变更 |
| `summary_manager.go` | 修改 | 接口适配 + H4 返回原始列表 + M7 UTC |
| `summary_manager_test.go` | 修改 | 适配接口签名变更 |
| `variable_manager.go` | 修改 | 接口适配 + H5 event_type 补齐 |
| `variable_manager_test.go` | 修改 | 适配接口签名变更 |
| `write_manager.go` | 修改 | 接口适配 + M3 错误传播修复 + 透传 opts |
| `write_manager_test.go` | 修改 | 适配接口签名变更 + M3 错误传播测试 |

---

### Task 1: 新增 MemoryOption 类型 + 修改 BaseMemoryManager 接口签名

**Files:**
- Modify: `internal/agentcore/memory/manage/index/base_manager.go:23-39` (接口)
- Modify: `internal/agentcore/memory/manage/index/base_manager.go:83-165` (新增类型+改函数)

- [ ] **Step 1: 在 base_manager.go 结构体区块后新增 MemoryOption 类型定义**

在 `base_manager.go` 第 54 行之后（`memoryManagerBase` 结构体之后）、枚举区块之前，插入：

```go
// MemoryOptionConfig 记忆操作选项配置（对齐 Python **kwargs 扩展点）。
// 对齐 Python: BaseMemoryManager 各方法的 **kwargs 参数
type MemoryOptionConfig struct {
	// llmModel LLM 模型实例，用于冲突检查（对齐 Python: add_memories(llm=None)）
	llmModel *llm.Model
}

// MemoryOption 记忆操作选项函数。
// 使用 Functional Options 模式，对齐 Python 的 **kwargs 扩展机制。
type MemoryOption func(*MemoryOptionConfig)

// WithLLMModel 设置 LLM 模型（对齐 Python: llm 参数）。
func WithLLMModel(model *llm.Model) MemoryOption {
	return func(cfg *MemoryOptionConfig) {
		cfg.llmModel = model
	}
}

// ApplyMemoryOptions 应用选项列表到配置。
func ApplyMemoryOptions(opts ...MemoryOption) *MemoryOptionConfig {
	cfg := &MemoryOptionConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}
```

- [ ] **Step 2: 修改 BaseMemoryManager 接口签名**

将 `base_manager.go` 第 23-39 行的接口定义改为：

```go
// BaseMemoryManager 记忆管理器抽象接口。
//
// 定义了记忆管理器的 6 个核心操作：AddMemories、Update、Search、Get、Delete、DeleteByUserID。
// 所有记忆管理器实现（FragmentMemoryManager、SummaryManager、VariableManager）必须实现此接口。
// opts ...MemoryOption 对齐 Python 的 **kwargs 扩展机制，llm 参数通过 WithLLMModel(opts) 传入。
//
// 对应 Python: openjiuwen/core/memory/manage/index/base_memory_manager.py (BaseMemoryManager)
type BaseMemoryManager interface {
	// AddMemories 批量添加记忆（含冲突检查和冗余消除）。
	// memories 的 key 为 mem_type 字符串（如 "user_profile"），value 为该类型的记忆列表。
	// 通过 WithLLMModel 选项传入 LLM 模型用于冲突检查（对齐 Python: add_memories(llm=None)）。
	AddMemories(ctx context.Context, userID string, scopeID string,
		memories map[string][]mem_model.MemoryUnit, opts ...MemoryOption) ([]mem_model.MemoryUnit, error)
	// Update 按 ID 更新记忆内容
	Update(ctx context.Context, userID string, scopeID string, memID string, newMemory string,
		opts ...MemoryOption) (bool, error)
	// Search 语义搜索记忆
	Search(ctx context.Context, userID string, scopeID string, query string, topK int, memTypes []string,
		opts ...MemoryOption) ([]*index.MemorySearchResult, error)
	// Get 按 ID 获取单条记忆
	Get(ctx context.Context, userID string, scopeID string, memID string,
		opts ...MemoryOption) (*index.MemoryDoc, error)
	// Delete 按 ID 删除记忆
	Delete(ctx context.Context, userID string, scopeID string, memID string,
		opts ...MemoryOption) (bool, error)
	// DeleteByUserID 删除用户+scope 下所有记忆
	DeleteByUserID(ctx context.Context, userID string, scopeID string,
		opts ...MemoryOption) (bool, error)
}
```

- [ ] **Step 3: 移除 base_manager.go 中不再需要的 llm import（确认其他地方仍需 llm）**

检查 `base_manager.go` 中 `llm` 包是否仍被使用（MemoryOptionConfig.llmModel 需要 `*llm.Model`），保留 import。

- [ ] **Step 4: 运行编译确认接口变更导致所有实现类报错**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/memory/manage/index/... 2>&1 | head -30`
Expected: 编译错误，提示 FragmentMemoryManager/SummaryManager/VariableManager 未实现新接口

- [ ] **Step 5: 适配 FragmentMemoryManager 签名**

修改 `fragment_manager.go` 第 70-71 行：

```go
// 修改前
func (m *FragmentMemoryManager) AddMemories(ctx context.Context, userID string, scopeID string,
	memories map[string][]mem_model.MemoryUnit, llmModel ...*llm.Model) ([]mem_model.MemoryUnit, error) {

// 修改后
func (m *FragmentMemoryManager) AddMemories(ctx context.Context, userID string, scopeID string,
	memories map[string][]mem_model.MemoryUnit, opts ...MemoryOption) ([]mem_model.MemoryUnit, error) {
```

修改 `fragment_manager.go` 第 149-154 行（提取 llmModel 的逻辑）：

```go
// 修改前
var model *llm.Model
if len(llmModel) > 0 {
	model = llmModel[0]
}

// 修改后
cfg := ApplyMemoryOptions(opts...)
model := cfg.llmModel
```

修改 FragmentManager 的其他 5 个方法签名，加入 `opts ...MemoryOption`：

- `Update` (第 207 行): `func (m *FragmentMemoryManager) Update(ctx context.Context, userID string, scopeID string, memID string, newMemory string, opts ...MemoryOption) (bool, error)`
- `Search` (第 237 行): `func (m *FragmentMemoryManager) Search(ctx context.Context, userID string, scopeID string, query string, topK int, memTypes []string, opts ...MemoryOption) ([]*index.MemorySearchResult, error)`
- `Get` (第 264 行): `func (m *FragmentMemoryManager) Get(ctx context.Context, userID string, scopeID string, memID string, opts ...MemoryOption) (*index.MemoryDoc, error)`
- `Delete` (第 280 行): `func (m *FragmentMemoryManager) Delete(ctx context.Context, userID string, scopeID string, memID string, opts ...MemoryOption) (bool, error)`
- `DeleteByUserID` (第 308 行): `func (m *FragmentMemoryManager) DeleteByUserID(ctx context.Context, userID string, scopeID string, opts ...MemoryOption) (bool, error)`

- [ ] **Step 6: 适配 SummaryManager 签名**

修改 `summary_manager.go` 所有 6 个方法签名，将 `_ ...*llm.Model` 替换为 `opts ...MemoryOption`：

- `AddMemories` (第 55-56 行): `func (m *SummaryManager) AddMemories(ctx context.Context, userID string, scopeID string, memories map[string][]mem_model.MemoryUnit, opts ...MemoryOption) ([]mem_model.MemoryUnit, error)`
- `Update` (第 116 行): `func (m *SummaryManager) Update(ctx context.Context, userID string, scopeID string, memID string, newMemory string, opts ...MemoryOption) (bool, error)`
- `Delete` (第 147 行): `func (m *SummaryManager) Delete(ctx context.Context, userID string, scopeID string, memID string, opts ...MemoryOption) (bool, error)`
- `DeleteByUserID` (第 162 行): `func (m *SummaryManager) DeleteByUserID(ctx context.Context, userID string, scopeID string, opts ...MemoryOption) (bool, error)`
- `Get` (第 177 行): `func (m *SummaryManager) Get(ctx context.Context, userID string, scopeID string, memID string, opts ...MemoryOption) (*index.MemoryDoc, error)`
- `Search` (第 195 行): `func (m *SummaryManager) Search(ctx context.Context, userID string, scopeID string, query string, topK int, _ []string, opts ...MemoryOption) ([]*index.MemorySearchResult, error)`

- [ ] **Step 7: 适配 VariableManager 签名**

修改 `variable_manager.go` 所有 6 个方法签名：

- `AddMemories` (第 105-106 行): `func (m *VariableManager) AddMemories(ctx context.Context, userID string, scopeID string, memories map[string][]mem_model.MemoryUnit, opts ...MemoryOption) ([]mem_model.MemoryUnit, error)`
- `Update` (第 166 行): `func (m *VariableManager) Update(_ context.Context, userID string, scopeID string, memID string, _ string, opts ...MemoryOption) (bool, error)`
- `Search` (第 180 行): `func (m *VariableManager) Search(_ context.Context, userID string, scopeID string, query string, _ int, _ []string, opts ...MemoryOption) ([]*index.MemorySearchResult, error)`
- `Get` (第 194 行): `func (m *VariableManager) Get(_ context.Context, userID string, scopeID string, memID string, opts ...MemoryOption) (*index.MemoryDoc, error)`
- `Delete` (第 207 行): `func (m *VariableManager) Delete(_ context.Context, userID string, scopeID string, memID string, opts ...MemoryOption) (bool, error)`
- `DeleteByUserID` (第 223 行): `func (m *VariableManager) DeleteByUserID(ctx context.Context, userID string, scopeID string, opts ...MemoryOption) (bool, error)`

- [ ] **Step 8: 适配 WriteManager 签名 + 透传 opts**

修改 `write_manager.go` 第 54-55 行：

```go
// 修改前
func (w *WriteManager) AddMemories(ctx context.Context, userID string, scopeID string,
	memories map[string][]mem_model.MemoryUnit, llmModel ...*llm.Model) ([]mem_model.MemoryUnit, error) {

// 修改后
func (w *WriteManager) AddMemories(ctx context.Context, userID string, scopeID string,
	memories map[string][]mem_model.MemoryUnit, opts ...MemoryOption) ([]mem_model.MemoryUnit, error) {
```

修改 `write_manager.go` 第 73 行（透传 opts）：

```go
// 修改前
memUnits, err := manager.AddMemories(ctx, userID, scopeID, memories, llmModel...)

// 修改后
memUnits, err := manager.AddMemories(ctx, userID, scopeID, memories, opts...)
```

- [ ] **Step 9: 修复 base_manager_test.go 中的 encrypt/decrypt 测试调用**

修改 `base_manager_test.go` 第 205-214 行：

```go
// 修改前
result := encryptMemoryIfNeeded(nil, "hello")
...
result = decryptMemoryIfNeeded(nil, "hello")
...
result = encryptMemoryIfNeeded([]byte{1, 2, 3}, "")

// 修改后
base := &memoryManagerBase{memType: "test"}
result := base.encryptMemoryIfNeeded(nil, "hello")
...
result = base.decryptMemoryIfNeeded(nil, "hello")
...
result = base.encryptMemoryIfNeeded([]byte{1, 2, 3}, "")
```

- [ ] **Step 10: 新增 MemoryOption 测试**

在 `base_manager_test.go` 末尾新增：

```go
func TestMemoryOption_WithLLMModel(t *testing.T) {
	model := &llm.Model{}
	cfg := ApplyMemoryOptions(WithLLMModel(model))
	if cfg.llmModel != model {
		t.Error("WithLLMModel 未正确设置 llmModel")
	}
}

func TestMemoryOption_Empty(t *testing.T) {
	cfg := ApplyMemoryOptions()
	if cfg.llmModel != nil {
		t.Error("空 opts 时 llmModel 应为 nil")
	}
}
```

- [ ] **Step 11: 修复所有测试文件中的方法调用签名**

在 `fragment_manager_test.go`、`summary_manager_test.go`、`variable_manager_test.go`、`write_manager_test.go` 中，将所有 `AddMemories(..., llmModel)` 调用改为 `AddMemories(..., WithLLMModel(llmModel))` 或 `AddMemories(...)` （无 llm 时）。

同时，`Update`/`Search`/`Get`/`Delete`/`DeleteByUserID` 调用末尾无需添加 opts 参数（Go 变参默认为空）。

- [ ] **Step 12: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/memory/manage/index/...`
Expected: 编译通过

- [ ] **Step 13: 运行测试验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/memory/manage/index/... -v -count=1 2>&1 | tail -30`
Expected: 所有测试通过

- [ ] **Step 14: 提交**

```bash
git add internal/agentcore/memory/manage/index/
git commit -m "feat(memory): 重构 BaseMemoryManager 接口，引入 MemoryOption 替代 llmModel 变参

- 新增 MemoryOption/MemoryOptionConfig/WithLLMModel/ApplyMemoryOptions 类型
- BaseMemoryManager 所有方法签名改为 opts ...MemoryOption
- llmModel 参数移入 MemoryOption（WithLLMModel）
- 适配 FragmentMemoryManager/SummaryManager/VariableManager/WriteManager
- 对齐 Python **kwargs 扩展机制 (M2)"
```

---

### Task 2: encrypt/decrypt 从包级函数改为 memoryManagerBase 方法 (M5)

**Files:**
- Modify: `internal/agentcore/memory/manage/index/base_manager.go:132-164`
- Modify: `internal/agentcore/memory/manage/index/base_manager_test.go:203-218`

- [ ] **Step 1: 修改 encryptMemoryIfNeeded 为 memoryManagerBase 方法**

修改 `base_manager.go` 第 135 行：

```go
// 修改前
func encryptMemoryIfNeeded(key []byte, plaintext string) string {

// 修改后
func (b *memoryManagerBase) encryptMemoryIfNeeded(key []byte, plaintext string) string {
```

修改 `base_manager.go` 第 152 行：

```go
// 修改前
func decryptMemoryIfNeeded(key []byte, ciphertext string) string {

// 修改后
func (b *memoryManagerBase) decryptMemoryIfNeeded(key []byte, ciphertext string) string {
```

- [ ] **Step 2: 更新 base_manager.go 中的注释**

修改第 44 行注释：

```go
// 修改前
// 提供 validateParams / wrapException / encryptMemoryIfNeeded / decryptMemoryIfNeeded 公共逻辑。

// 修改后
// 提供 validateParams / wrapException / encryptMemoryIfNeeded / decryptMemoryIfNeeded 公共方法。
```

- [ ] **Step 3: 修复测试文件中的调用**

在 `base_manager_test.go` 中（已在 Task 1 Step 9 完成部分适配），确认 fragment_manager_test.go 中的调用也已适配：

修改 `fragment_manager_test.go` 中所有 `encryptMemoryIfNeeded(key, text)` 为 `(&memoryManagerBase{}).encryptMemoryIfNeeded(key, text)` 或构造 base 实例调用。

- [ ] **Step 4: 编译+测试验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/memory/manage/index/... -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/memory/manage/index/
git commit -m "refactor(memory): encrypt/decrypt 改为 memoryManagerBase 方法 (M5)

- encryptMemoryIfNeeded/decryptMemoryIfNeeded 从包级函数改为方法
- 对齐 Python @staticmethod 可被子类关联的语义
- 修复测试文件中的调用方式"
```

---

### Task 3: time.Now() 统一改为 time.Now().UTC() (M7)

**Files:**
- Modify: `internal/agentcore/memory/manage/index/fragment_manager.go:225,475,490`
- Modify: `internal/agentcore/memory/manage/index/summary_manager.go:135`

- [ ] **Step 1: 修改 fragment_manager.go 中的 3 处 time.Now()**

第 225 行（Update 方法）：
```go
// 修改前
Timestamp: time.Now(),
// 修改后
Timestamp: time.Now().UTC(),
```

第 475 行（parseTimestamp 函数，空字符串回退）：
```go
// 修改前
return time.Now()
// 修改后
return time.Now().UTC()
```

第 490 行（parseTimestamp 函数，解析失败回退）：
```go
// 修改前
return time.Now()
// 修改后
return time.Now().UTC()
```

- [ ] **Step 2: 修改 summary_manager.go 中的 1 处 time.Now()**

第 135 行（Update 方法）：
```go
// 修改前
Timestamp: time.Now(),
// 修改后
Timestamp: time.Now().UTC(),
```

- [ ] **Step 3: 编译+测试验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/memory/manage/index/... -count=1`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/memory/manage/index/fragment_manager.go internal/agentcore/memory/manage/index/summary_manager.go
git commit -m "fix(memory): 统一 time.Now() 为 time.Now().UTC() 对齐 Python UTC 时区 (M7)

- fragment_manager.go: Update + parseTimestamp 两处回退
- summary_manager.go: Update 一处
- 对齐 Python datetime.now(timezone.utc).astimezone()"
```

---

### Task 4: 日志修复 (H1 + H2 + H5 + M1)

**Files:**
- Modify: `internal/agentcore/memory/manage/index/fragment_manager.go:86,333-336,184`
- Modify: `internal/agentcore/memory/manage/index/variable_manager.go:195-200`

- [ ] **Step 1: H1 - 修复 FragmentManager 类型断言 warn 日志 memory_type 字段**

修改 `fragment_manager.go` 第 86 行：

```go
// 修改前
logger.Warn(logComponent).Str("memory_type", m.memType).

// 修改后
logger.Warn(logComponent).Str("memory_type", key).
```

注意：循环变量在当前代码中名为 `key`（第 80 行 `for key, units := range memories`）。

- [ ] **Step 2: H2 - 补齐 ListFragmentMemories error 日志缺失字段**

修改 `fragment_manager.go` 第 333-336 行：

```go
// 修改前
logger.Error(logComponent).
	Str("mem_type", memType).
	Str("memory_type", m.memType).
	Msg("非法碎片记忆类型")

// 修改后
logger.Error(logComponent).
	Str("mem_type", memType).
	Str("memory_type", m.memType).
	Str("user_id", userID).
	Str("scope_id", scopeID).
	Str("event_type", "MEMORY_STORE").
	Msg("非法碎片记忆类型")
```

- [ ] **Step 3: H5 - 补齐 VariableManager Get warn 日志 event_type 字段**

修改 `variable_manager.go` 第 195-200 行：

```go
// 修改前
logger.Warn(logComponent).
	Strs("memory_id", []string{memID}).
	Str("memory_type", m.memType).
	Str("user_id", userID).
	Str("scope_id", scopeID).
	Msg("未实现方法 get")

// 修改后
logger.Warn(logComponent).
	Strs("memory_id", []string{memID}).
	Str("memory_type", m.memType).
	Str("user_id", userID).
	Str("scope_id", scopeID).
	Str("event_type", "MEMORY_STORE").
	Msg("未实现方法 get")
```

- [ ] **Step 4: M1 - FragmentManager AddMemories 批量添加前补 debug 日志**

在 `fragment_manager.go` 中，步骤 4 的 `memoryIndex.AddMemories` 调用前（约第 184-185 行之间），插入：

```go
logger.Debug(logComponent).
	Str("memory_type", m.memType).
	Str("event_type", "MEMORY_STORE").
	Str("user_id", userID).
	Str("scope_id", scopeID).
	Int("count", len(addDocs)).
	Msg("添加记忆")
```

同样，在"无旧记忆且仅 1 条新记忆直接写入"路径（约第 140 行 `addDocs` 后），也插入类似的 debug 日志。

- [ ] **Step 5: 编译+测试验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/memory/manage/index/... -count=1`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/memory/manage/index/
git commit -m "fix(memory): 修复日志字段缺失和错误 (H1+H2+H5+M1)

- H1: FragmentManager 类型断言日志 memory_type 改用实际 map key
- H2: ListFragmentMemories 非法类型日志补齐 user_id/scope_id/event_type
- H5: VariableManager.Get 日志补齐 event_type
- M1: FragmentManager.AddMemories 批量添加前补 debug 日志"
```

---

### Task 5: AddMemories 返回原始列表对齐 Python (H4)

**Files:**
- Modify: `internal/agentcore/memory/manage/index/summary_manager.go:63-108`
- Modify: `internal/agentcore/memory/manage/index/fragment_manager.go:70-192`

- [ ] **Step 1: 修改 SummaryManager.AddMemories 返回原始列表**

修改 `summary_manager.go` 第 63-108 行，在过滤循环前保存原始列表，写入成功后返回原始列表：

```go
func (m *SummaryManager) AddMemories(ctx context.Context, userID string, scopeID string,
	memories map[string][]mem_model.MemoryUnit, opts ...MemoryOption) ([]mem_model.MemoryUnit, error) {

	if err := m.validateParams(userID, scopeID,
		exception.StatusMemoryAddMemoryExecutionError, m.memType); err != nil {
		return nil, err
	}

	// 保存原始列表（对齐 Python: return memories[self.mem_type]）
	var originalUnits []mem_model.MemoryUnit
	if units, ok := memories[m.memType]; ok {
		originalUnits = units
	}

	// 过滤 summary 类型的 SummaryUnit
	var summaryUnits []*mem_model.SummaryUnit
	for memType, units := range memories {
		if memType != m.memType {
			continue
		}
		for _, unit := range units {
			summary, ok := unit.(*mem_model.SummaryUnit)
			if !ok {
				logger.Warn(logComponent).
					Str("event_type", "MEMORY_STORE").
					Str("memory_type", m.memType).
					Str("user_id", userID).
					Str("scope_id", scopeID).
					Msg("mem_unit 不是 SummaryUnit 类型，跳过")
				continue
			}
			summaryUnits = append(summaryUnits, summary)
		}
	}

	if len(summaryUnits) == 0 {
		logger.Warn(logComponent).
			Str("event_type", "MEMORY_STORE").
			Str("memory_type", m.memType).
			Str("user_id", userID).
			Str("scope_id", scopeID).
			Msg("无有效摘要文档可添加")
		return originalUnits, nil
	}

	docs := m.convertToMemoryDocs(summaryUnits)
	if err := m.memoryIndex.AddMemories(ctx, userID, scopeID, docs); err != nil {
		return nil, m.wrapException(err, exception.StatusMemoryAddMemoryExecutionError, m.memType)
	}

	// 对齐 Python: return memories[self.mem_type]（返回原始列表）
	return originalUnits, nil
}
```

- [ ] **Step 2: 修改 FragmentManager.AddMemories 返回原始列表**

在 `fragment_manager.go` 的 `AddMemories` 方法中，在类型断言循环前保存 fragment 类型的原始单元列表：

在第 79 行前插入：
```go
// 保存原始列表（对齐 Python: return memories[self.mem_type]）
var originalFragmentUnits []mem_model.MemoryUnit
for key, units := range memories {
	if isFragmentMemoryType(key) {
		originalFragmentUnits = append(originalFragmentUnits, units...)
	}
}
```

将所有 `return fragmentUnitsToMemoryUnits(mapValues(processResult)), nil` 改为 `return originalFragmentUnits, nil`（有 3 处返回点：第 119 行、第 144 行、第 192 行）。

- [ ] **Step 3: 编译+测试验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/memory/manage/index/... -count=1`
Expected: PASS（可能需要调整部分测试的预期返回值）

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/memory/manage/index/
git commit -m "fix(memory): AddMemories 返回原始列表对齐 Python (H4)

- SummaryManager: 保存 memories[memType] 原始列表，写入成功后返回
- FragmentManager: 保存 fragment 类型原始单元列表，处理后返回
- 对齐 Python: return memories[self.mem_type]"
```

---

### Task 6: WriteManager 错误传播修复 (M3)

**Files:**
- Modify: `internal/agentcore/memory/manage/index/write_manager.go:92-115,122-145`

- [ ] **Step 1: 修改 UpdateMemByID 错误处理**

修改 `write_manager.go` 第 93-103 行：

```go
// 修改前
memType, err := w.getMemTypeFromIndex(ctx, userID, scopeID, memID)
if err != nil || memType == "" {
	logger.Warn(writeLogComponent).
		Str("memory_id", memID).
		Str("memory_type", memType).
		Str("user_id", userID).
		Str("scope_id", scopeID).
		Str("event_type", "MEMORY_STORE").
		Msg("获取记忆类型失败，跳过本次更新")
	return nil
}

// 修改后
memType, err := w.getMemTypeFromIndex(ctx, userID, scopeID, memID)
if err != nil {
	return err  // 传播底层错误（对齐 Python 异常向上传播）
}
if memType == "" {
	logger.Warn(writeLogComponent).
		Str("memory_id", memID).
		Str("user_id", userID).
		Str("scope_id", scopeID).
		Str("event_type", "MEMORY_STORE").
		Msg("记忆类型未知，跳过本次更新")
	return nil
}
```

- [ ] **Step 2: 修改 DeleteMemByID 错误处理（同上模式）**

修改 `write_manager.go` 第 123-133 行：

```go
// 修改前
memType, err := w.getMemTypeFromIndex(ctx, userID, scopeID, memID)
if err != nil || memType == "" {
	logger.Warn(writeLogComponent).
		Str("memory_id", memID).
		Str("memory_type", memType).
		Str("user_id", userID).
		Str("scope_id", scopeID).
		Str("event_type", "MEMORY_STORE").
		Msg("获取记忆类型失败，跳过本次删除")
	return nil
}

// 修改后
memType, err := w.getMemTypeFromIndex(ctx, userID, scopeID, memID)
if err != nil {
	return err  // 传播底层错误
}
if memType == "" {
	logger.Warn(writeLogComponent).
		Str("memory_id", memID).
		Str("user_id", userID).
		Str("scope_id", scopeID).
		Str("event_type", "MEMORY_STORE").
		Msg("记忆类型未知，跳过本次删除")
	return nil
}
```

- [ ] **Step 3: 新增 M3 测试**

在 `write_manager_test.go` 中新增测试，验证 GetByID 错误时 UpdateMemByID/DeleteMemByID 返回 error：

```go
func TestWriteManager_UpdateMemByID_GetByIDError(t *testing.T) {
	// 构造一个 GetByID 会返回 error 的 fakeMemoryIndex
	idx := &errorMemoryIndex{}
	managers := map[string]BaseMemoryManager{
		"fragment": NewFragmentMemoryManager(idx, nil),
	}
	wm := NewWriteManager(managers, idx)
	err := wm.UpdateMemByID(context.Background(), "user-1", "scope-1", "mem-1", "new text")
	if err == nil {
		t.Fatal("期望 GetByID error 向上传播，但得到 nil")
	}
}

func TestWriteManager_DeleteMemByID_GetByIDError(t *testing.T) {
	idx := &errorMemoryIndex{}
	managers := map[string]BaseMemoryManager{
		"fragment": NewFragmentMemoryManager(idx, nil),
	}
	wm := NewWriteManager(managers, idx)
	err := wm.DeleteMemByID(context.Background(), "user-1", "scope-1", "mem-1")
	if err == nil {
		t.Fatal("期望 GetByID error 向上传播，但得到 nil")
	}
}

// errorMemoryIndex GetByID 返回 error 的假实现
type errorMemoryIndex struct {
	index.MemoryIndexBase
}

func (e *errorMemoryIndex) SetStorageCodec(_ index.StorageCodec) {}
func (e *errorMemoryIndex) AddMemories(_ context.Context, _, _ string, _ []*index.MemoryDoc) error {
	return nil
}
func (e *errorMemoryIndex) UpdateMemories(_ context.Context, _, _ string, _ []*index.MemoryDoc) error {
	return nil
}
func (e *errorMemoryIndex) DeleteMemories(_ context.Context, _, _ string, _ []string) error {
	return nil
}
func (e *errorMemoryIndex) DeleteByUser(_ context.Context, _ string) error { return nil }
func (e *errorMemoryIndex) DeleteByScope(_ context.Context, _ string) error { return nil }
func (e *errorMemoryIndex) DeleteByUserAndScope(_ context.Context, _, _ string) error {
	return nil
}
func (e *errorMemoryIndex) Search(_ context.Context, _, _, _ string, _ []string, _ int) ([]*index.MemorySearchResult, error) {
	return nil, nil
}
func (e *errorMemoryIndex) GetByID(_ context.Context, _, _, _ string) (*index.MemoryDoc, error) {
	return nil, fmt.Errorf("模拟数据库连接断开")
}
func (e *errorMemoryIndex) ListMemories(_ context.Context, _, _ string, _, _ int, _ []string) ([]*index.MemoryDoc, error) {
	return nil, nil
}
```

- [ ] **Step 4: 编译+测试验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/memory/manage/index/... -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/memory/manage/index/
git commit -m "fix(memory): WriteManager GetByID 错误向上传播 (M3)

- UpdateMemByID/DeleteMemByID: GetByID error 时 return err 而非静默跳过
- 新增 errorMemoryIndex 测试辅助类型
- 对齐 Python 异常向上传播行为"
```

---

### Task 7: 更新 doc.go 包文档

**Files:**
- Modify: `internal/agentcore/memory/manage/index/doc.go`

- [ ] **Step 1: 更新 doc.go 反映 MemoryOption 新增和接口变更**

在 doc.go 中补充 MemoryOption 相关说明，更新 BaseMemoryManager 接口描述。具体内容根据实际 doc.go 当前内容调整。

- [ ] **Step 2: 提交**

```bash
git add internal/agentcore/memory/manage/index/doc.go
git commit -m "docs(memory): 更新 index 包文档反映 MemoryOption 接口变更"
```
