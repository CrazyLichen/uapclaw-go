# 7.27 LongTermMemory 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现长期记忆编排器 LongTermMemory，组装已有记忆子模块提供统一的写入、检索、删除、更新 API。

**Architecture:** sync.Once 全局单例 + ReActAgent 式回调包装（公开方法触发回调→私有 Impl 做逻辑），独立 ltm 子包避免同层循环依赖。

**Tech Stack:** Go 1.23, sync.Once, CallbackFramework, AesStorageCodec, DistributedLock

---

## File Structure

### 新建文件

| 文件 | 职责 |
|------|------|
| `internal/agentcore/memory/ltm/doc.go` | 包文档 |
| `internal/agentcore/memory/ltm/models.go` | MemInfo / MemResult / AddMemResult |
| `internal/agentcore/memory/ltm/options.go` | AddMessagesOption / SearchOption 等函数式选项 |
| `internal/agentcore/memory/ltm/long_term_memory.go` | LongTermMemory 结构体 + sync.Once 单例 |
| `internal/agentcore/memory/ltm/register.go` | RegisterStore / RegisterPlugin / MigrateBetweenIndices |
| `internal/agentcore/memory/ltm/config_ops.go` | SetConfig / SetScopeConfig / GetScopeConfig / DeleteScopeConfig |
| `internal/agentcore/memory/ltm/add_messages.go` | AddMessages 核心流程 |
| `internal/agentcore/memory/ltm/search_ops.go` | SearchUserMem / SearchUserHistorySummary / GetVariables |
| `internal/agentcore/memory/ltm/delete_ops.go` | DeleteMemByID / DeleteMemByUserID / DeleteMemByScope / DeleteMessages / DeleteVariables |
| `internal/agentcore/memory/ltm/update_ops.go` | UpdateMemByID / UpdateVariables |
| `internal/agentcore/memory/ltm/query_ops.go` | GetRecentMessages / GetMessageByID / GetUserMemByPage / UserMemTotalNum |
| `internal/agentcore/memory/ltm/scope_helpers.go` | getScopeLLM / getScopeConfig / applyScopeEmbedding / getScopeEmbeddingModel |
| `internal/agentcore/memory/ltm/helpers.go` | checkMessages / getHistoryMessages / validateID / runMigration |
| `internal/agentcore/memory/ltm/models_test.go` | 模型测试 |
| `internal/agentcore/memory/ltm/options_test.go` | 选项测试 |
| `internal/agentcore/memory/ltm/long_term_memory_test.go` | 单例+结构体测试 |
| `internal/agentcore/memory/ltm/register_test.go` | 注册测试 |
| `internal/agentcore/memory/ltm/config_ops_test.go` | 配置操作测试 |
| `internal/agentcore/memory/ltm/add_messages_test.go` | 核心写入测试 |
| `internal/agentcore/memory/ltm/search_ops_test.go` | 搜索操作测试 |
| `internal/agentcore/memory/ltm/delete_ops_test.go` | 删除操作测试 |
| `internal/agentcore/memory/ltm/update_ops_test.go` | 更新操作测试 |
| `internal/agentcore/memory/ltm/query_ops_test.go` | 查询操作测试 |
| `internal/agentcore/memory/ltm/scope_helpers_test.go` | scope 辅助方法测试 |
| `internal/agentcore/memory/ltm/helpers_test.go` | 辅助方法测试 |

### 修改文件

| 文件 | 改动 |
|------|------|
| `internal/agentcore/foundation/store/index/base.go` | BaseMemoryIndex 接口加 SetEmbeddingModel |
| `internal/agentcore/runner/callback/events.go` | MemoryEventData 加 UserID/ScopeID/Query/MemoryType/MemoryID/Score/Timestamp 字段 |
| `internal/agentcore/memory/config/scope_config.go` | 加 ToJSON / MemoryScopeConfigFromJSON 序列化方法 |
| `IMPLEMENTATION_PLAN.md` | 7.19/7.22/7.23 状态 ☐→✅ |

---

## Task 1: 前置改动 — BaseMemoryIndex 加 SetEmbeddingModel

**Files:**
- Modify: `internal/agentcore/foundation/store/index/base.go:35-37`
- Test: `internal/agentcore/foundation/store/index/base_test.go`

- [ ] **Step 1: 在 BaseMemoryIndex 接口加 SetEmbeddingModel 方法**

在 `SetStorageCodec` 后面加：

```go
// SetEmbeddingModel 设置或替换嵌入模型。
// Python: hasattr(memory_index, 'set_embedding_model') → memory_index.set_embedding_model(emb)
SetEmbeddingModel(model embedding.BaseEmbedding)
```

需要新增 import `"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"`。

- [ ] **Step 2: 在 Milvus adapter 等其他 BaseMemoryIndex 实现上加 no-op 的 SetEmbeddingModel**

对 `internal/agentcore/foundation/store/graph/milvus/adapter.go` 中的 MilvusAdapter 加：

```go
// SetEmbeddingModel 设置嵌入模型（Milvus 适配器暂不支持，no-op）。
func (a *MilvusAdapter) SetEmbeddingModel(_ embedding.BaseEmbedding) {}
```

- [ ] **Step 3: 运行编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/foundation/store/...`
Expected: 编译通过

- [ ] **Step 4: 运行现有测试确认不破坏**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/foundation/store/index/... -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add -A && git commit -m "feat(store): BaseMemoryIndex 接口加 SetEmbeddingModel 方法"
```

---

## Task 2: 前置改动 — MemoryEventData 加字段

**Files:**
- Modify: `internal/agentcore/runner/callback/events.go:160-169`
- Test: `internal/agentcore/runner/callback/events_test.go`

- [ ] **Step 1: 扩展 MemoryEventData 结构体**

将现有 `MemoryEventData` 替换为：

```go
// MemoryEventData 记忆事件数据，回调函数接收此结构获取上下文信息。
//
// Python: openjiuwen/core/runner/callback/events.py (MemoryEvents)
type MemoryEventData struct {
	// Event 事件类型
	Event MemoryEventType
	// UserID 用户标识
	UserID string
	// ScopeID 作用域标识
	ScopeID string
	// Query 搜索查询
	Query string
	// MemoryType 记忆类型
	MemoryType string
	// MemoryID 记忆标识
	MemoryID string
	// Score 相关度分数
	Score float64
	// Timestamp 时间戳
	Timestamp *time.Time
	// Key 记忆键（保留向后兼容）
	Key string
	// Value 记忆值
	Value any
	// Extra 额外数据
	Extra map[string]any
}
```

需要新增 import `"time"`。

- [ ] **Step 2: 更新已有测试中的 MemoryEventData 构造**

检查 `events_test.go` 和 `framework_test.go` 中所有 `MemoryEventData{}` 构造，确保编译通过。现有测试只设置 Event/Key 字段，新增字段为零值不影响。

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/runner/callback/... -count=1`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add -A && git commit -m "feat(callback): MemoryEventData 加 UserID/ScopeID/Query/MemoryType/MemoryID 等字段"
```

---

## Task 3: 前置改动 — MemoryScopeConfig 序列化方法

**Files:**
- Modify: `internal/agentcore/memory/config/scope_config.go`
- Test: `internal/agentcore/memory/config/scope_config_test.go`

- [ ] **Step 1: 在 scope_config.go 末尾加 ToJSON / MemoryScopeConfigFromJSON**

```go
// ToJSON 将 MemoryScopeConfig 序列化为 JSON 字符串。
// 对齐 Python: MemoryScopeConfig.model_dump_json(by_alias=True)
func (c *MemoryScopeConfig) ToJSON() (string, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("序列化 MemoryScopeConfig 失败: %w", err)
	}
	return string(data), nil
}

// MemoryScopeConfigFromJSON 从 JSON 字符串反序列化 MemoryScopeConfig。
// 对齐 Python: MemoryScopeConfig.model_validate_json(config_json)
func MemoryScopeConfigFromJSON(data string) (*MemoryScopeConfig, error) {
	cfg := &MemoryScopeConfig{}
	if err := json.Unmarshal([]byte(data), cfg); err != nil {
		return nil, fmt.Errorf("反序列化 MemoryScopeConfig 失败: %w", err)
	}
	return cfg, nil
}
```

需要新增 import `"encoding/json"` 和 `"fmt"`。

- [ ] **Step 2: 写测试**

```go
func TestMemoryScopeConfig_ToJSON和FromJSON(t *testing.T) {
	original := &MemoryScopeConfig{
		UserProfileDefinition:    "测试画像",
		SemanticMemoryDefinition: "测试语义",
		EpisodicMemoryDefinition: "测试情景",
	}
	jsonStr, err := original.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON 失败: %v", err)
	}
	parsed, err := MemoryScopeConfigFromJSON(jsonStr)
	if err != nil {
		t.Fatalf("MemoryScopeConfigFromJSON 失败: %v", err)
	}
	if parsed.UserProfileDefinition != original.UserProfileDefinition {
		t.Errorf("UserProfileDefinition = %q, want %q", parsed.UserProfileDefinition, original.UserProfileDefinition)
	}
	if parsed.SemanticMemoryDefinition != original.SemanticMemoryDefinition {
		t.Errorf("SemanticMemoryDefinition = %q, want %q", parsed.SemanticMemoryDefinition, original.SemanticMemoryDefinition)
	}
	if parsed.EpisodicMemoryDefinition != original.EpisodicMemoryDefinition {
		t.Errorf("EpisodicMemoryDefinition = %q, want %q", parsed.EpisodicMemoryDefinition, original.EpisodicMemoryDefinition)
	}
}

func TestMemoryScopeConfigFromJSON_空输入(t *testing.T) {
	cfg, err := MemoryScopeConfigFromJSON("{}")
	if err != nil {
		t.Fatalf("空 JSON 不应返回错误: %v", err)
	}
	if cfg.UserProfileDefinition != "" {
		t.Errorf("UserProfileDefinition = %q, want empty", cfg.UserProfileDefinition)
	}
}
```

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/config/... -count=1`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add -A && git commit -m "feat(config): MemoryScopeConfig 加 ToJSON/MemoryScopeConfigFromJSON 序列化方法"
```

---

## Task 4: 更新 IMPLEMENTATION_PLAN.md 状态

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 将 7.19 / 7.22 / 7.23 从 ☐ 更新为 ✅**

```
| 7.19 | ✅ | MemoryAnalyzer / Generator | ✅ MemoryAnalyzer（Analyze 包级函数）+ ✅ Generator（GenAllMemory 编排器 + 全部子方法） | `openjiuwen/core/memory/process/extract/` |
| 7.22 | ✅ | Migration Operations | 迁移操作注册表 ✅ | `openjiuwen/core/memory/migration/operation/` |
| 7.23 | ✅ | Migration Migrators | KV/SQL/Vector/Index/Message 迁移器 ✅ + run_migrations ✅ | `openjiuwen/core/memory/migration/migrator/` |
```

- [ ] **Step 2: 提交**

```bash
git add -A && git commit -m "docs: 更新 7.19/7.22/7.23 状态为已完成"
```

---

## Task 5: ltm 包骨架 — doc.go + models.go + options.go

**Files:**
- Create: `internal/agentcore/memory/ltm/doc.go`
- Create: `internal/agentcore/memory/ltm/models.go`
- Create: `internal/agentcore/memory/ltm/options.go`
- Create: `internal/agentcore/memory/ltm/models_test.go`
- Create: `internal/agentcore/memory/ltm/options_test.go`

- [ ] **Step 1: 创建 doc.go**

```go
// Package ltm 提供长期记忆编排器（LongTermMemory），记忆系统的顶层入口。
//
// LongTermMemory 是 Singleton 模式的全局实例，不实现任何存储逻辑，
// 而是组装和协调已有的各子模块（manage/、process/、config/、migration/、codec/、external/），
// 对外提供统一的记忆写入（AddMessages）、检索（SearchUserMem）、删除、更新 API。
//
// 在 Agent 会话中的位置：
//   - 写入时机：每轮对话结束后调用 AddMessages 存消息 + LLM 提取记忆碎片
//   - 检索时机：下轮对话前调用 SearchUserMem 检索相关记忆注入 Prompt
//   - 变量读取：通过 GetVariables 读取用户变量注入 Prompt
//
// 文件目录：
//
//	ltm/
//	├── doc.go                 # 包文档
//	├── models.go              # MemInfo / MemResult / AddMemResult 返回值模型
//	├── options.go             # 函数式选项（AddMessagesOption / SearchOption）
//	├── long_term_memory.go    # LongTermMemory 结构体 + sync.Once 单例
//	├── register.go            # RegisterStore / RegisterPlugin / MigrateBetweenIndices
//	├── config_ops.go          # SetConfig / SetScopeConfig / GetScopeConfig / DeleteScopeConfig
//	├── add_messages.go        # AddMessages 核心写入流程
//	├── search_ops.go          # SearchUserMem / SearchUserHistorySummary / GetVariables
//	├── delete_ops.go          # DeleteMemByID / DeleteMemByUserID / DeleteMemByScope 等
//	├── update_ops.go          # UpdateMemByID / UpdateVariables
//	├── query_ops.go           # GetRecentMessages / GetMessageByID / GetUserMemByPage 等
//	├── scope_helpers.go       # getScopeLLM / getScopeConfig / applyScopeEmbedding 等私有方法
//	└── helpers.go             # checkMessages / getHistoryMessages / validateID / runMigration
//
// 对应 Python 代码：openjiuwen/core/memory/long_term_memory.py
package ltm
```

- [ ] **Step 2: 创建 models.go**

```go
package ltm

import (
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemInfo 记忆信息。
//
// Python: MemInfo
type MemInfo struct {
	// MemID 记忆唯一标识
	MemID string `json:"mem_id"`
	// Content 记忆内容
	Content string `json:"content"`
	// Type 记忆类型
	Type mem_model.MemoryType `json:"type"`
	// Timestamp 记忆时间戳
	Timestamp *time.Time `json:"timestamp"`
}

// MemResult 记忆搜索结果。
//
// Python: MemResult
type MemResult struct {
	// MemInfo 记忆信息
	MemInfo *MemInfo `json:"mem_info"`
	// Score 相关度分数
	Score float64 `json:"score"`
}

// AddMemResult 添加记忆操作结果。
//
// Python: AddMemResult
type AddMemResult struct {
	// Variables 变量记忆结果
	Variables []*mem_model.VariableUnit `json:"variables"`
	// UserProfile 用户画像记忆结果
	UserProfile []*mem_model.FragmentMemoryUnit `json:"user_profile"`
	// SemanticMemory 语义记忆结果
	SemanticMemory []*mem_model.FragmentMemoryUnit `json:"semantic_memory"`
	// EpisodicMemory 情景记忆结果
	EpisodicMemory []*mem_model.FragmentMemoryUnit `json:"episodic_memory"`
	// Summary 摘要记忆结果
	Summary []*mem_model.SummaryUnit `json:"summary"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 3: 创建 options.go**

```go
package ltm

import "time"

// ──────────────────────────── 结构体 ────────────────────────────

// addMessagesParams AddMessages 的全部参数（对齐 Python **kwargs）。
type addMessagesParams struct {
	// Messages 当前轮次消息列表
	Messages any // []llmschema.BaseMessage，用 any 避免循环测试依赖
	// AgentConfig Agent 记忆配置
	AgentConfig any // *config.AgentMemoryConfig
	// UserID 用户标识
	UserID string
	// ScopeID 作用域标识
	ScopeID string
	// SessionID 会话标识
	SessionID string
	// Timestamp 时间戳
	Timestamp *time.Time
	// GenMem 是否生成记忆
	GenMem bool
	// GenMemWithHistoryMsgNum 获取历史消息窗口大小
	GenMemWithHistoryMsgNum int
}

// searchParams SearchUserMem / SearchUserHistorySummary 的全部参数。
type searchParams struct {
	// Query 搜索查询
	Query string
	// Num 返回结果数
	Num int
	// UserID 用户标识
	UserID string
	// ScopeID 作用域标识
	ScopeID string
	// Threshold 最低相关度阈值
	Threshold float64
}

// ──────────────────────────── 枚 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DefaultValue 占位符默认值（对齐 Python DEFAULT_VALUE = "__default__"）
	DefaultValue = "__default__"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// AddMessagesOption AddMessages 的可选参数。
type AddMessagesOption func(*addMessagesParams)

// WithUserID 设置用户标识。
func WithUserID(uid string) AddMessagesOption {
	return func(p *addMessagesParams) { p.UserID = uid }
}

// WithScopeID 设置作用域标识。
func WithScopeID(sid string) AddMessagesOption {
	return func(p *addMessagesParams) { p.ScopeID = sid }
}

// WithSessionID 设置会话标识。
func WithSessionID(sid string) AddMessagesOption {
	return func(p *addMessagesParams) { p.SessionID = sid }
}

// WithTimestamp 设置时间戳。
func WithTimestamp(t time.Time) AddMessagesOption {
	return func(p *addMessagesParams) { p.Timestamp = &t }
}

// WithGenMem 设置是否生成记忆。
func WithGenMem(gen bool) AddMessagesOption {
	return func(p *addMessagesParams) { p.GenMem = gen }
}

// WithGenMemWithHistoryMsgNum 设置获取历史消息窗口大小。
func WithGenMemWithHistoryMsgNum(n int) AddMessagesOption {
	return func(p *addMessagesParams) { p.GenMemWithHistoryMsgNum = n }
}

// newAddMessagesParams 从选项构建参数（对齐 Python add_messages 的默认值）。
func newAddMessagesParams(messages any, agentConfig any, opts ...AddMessagesOption) *addMessagesParams {
	p := &addMessagesParams{
		Messages:                messages,
		AgentConfig:             agentConfig,
		UserID:                  DefaultValue,
		ScopeID:                 DefaultValue,
		SessionID:               DefaultValue,
		GenMem:                  true,
		GenMemWithHistoryMsgNum: 2,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// SearchOption SearchUserMem 的可选参数。
type SearchOption func(*searchParams)

// SearchWithUserID 设置用户标识。
func SearchWithUserID(uid string) SearchOption {
	return func(p *searchParams) { p.UserID = uid }
}

// SearchWithScopeID 设置作用域标识。
func SearchWithScopeID(sid string) SearchOption {
	return func(p *searchParams) { p.ScopeID = sid }
}

// SearchWithThreshold 设置最低相关度阈值。
func SearchWithThreshold(threshold float64) SearchOption {
	return func(p *searchParams) { p.Threshold = threshold }
}

// newSearchParams 从选项构建搜索参数。
func newSearchParams(query string, num int, opts ...SearchOption) *searchParams {
	p := &searchParams{
		Query:     query,
		Num:       num,
		UserID:    DefaultValue,
		ScopeID:   DefaultValue,
		Threshold: 0.3,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 4: 创建 models_test.go**

```go
package ltm

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
)

// TestAddMemResult_零值 测试 AddMemResult 零值初始化。
func TestAddMemResult_零值(t *testing.T) {
	result := &AddMemResult{}
	if len(result.Variables) != 0 {
		t.Errorf("Variables 应为空切片")
	}
	if len(result.UserProfile) != 0 {
		t.Errorf("UserProfile 应为空切片")
	}
	if len(result.Summary) != 0 {
		t.Errorf("Summary 应为空切片")
	}
}

// TestMemInfo_字段赋值 测试 MemInfo 字段赋值。
func TestMemInfo_字段赋值(t *testing.T) {
	mi := &MemInfo{
		MemID:   "test-id",
		Content: "测试内容",
		Type:    mem_model.MemoryTypeUserProfile,
	}
	if mi.MemID != "test-id" {
		t.Errorf("MemID = %q, want %q", mi.MemID, "test-id")
	}
	if mi.Type != mem_model.MemoryTypeUserProfile {
		t.Errorf("Type = %v, want UserProfile", mi.Type)
	}
}
```

- [ ] **Step 5: 创建 options_test.go**

```go
package ltm

import "testing"

// TestNewAddMessagesParams_默认值 测试默认参数值对齐 Python。
func TestNewAddMessagesParams_默认值(t *testing.T) {
	p := newAddMessagesParams(nil, nil)
	if p.UserID != DefaultValue {
		t.Errorf("UserID = %q, want %q", p.UserID, DefaultValue)
	}
	if p.ScopeID != DefaultValue {
		t.Errorf("ScopeID = %q, want %q", p.ScopeID, DefaultValue)
	}
	if !p.GenMem {
		t.Errorf("GenMem = false, want true")
	}
	if p.GenMemWithHistoryMsgNum != 2 {
		t.Errorf("GenMemWithHistoryMsgNum = %d, want 2", p.GenMemWithHistoryMsgNum)
	}
}

// TestNewAddMessagesParams_自定义选项 测试选项覆盖默认值。
func TestNewAddMessagesParams_自定义选项(t *testing.T) {
	p := newAddMessagesParams(nil, nil,
		WithUserID("u1"),
		WithScopeID("s1"),
		WithGenMem(false),
	)
	if p.UserID != "u1" {
		t.Errorf("UserID = %q, want %q", p.UserID, "u1")
	}
	if p.GenMem {
		t.Errorf("GenMem = true, want false")
	}
}

// TestNewSearchParams_默认值 测试搜索参数默认值。
func TestNewSearchParams_默认值(t *testing.T) {
	p := newSearchParams("query", 5)
	if p.Threshold != 0.3 {
		t.Errorf("Threshold = %f, want 0.3", p.Threshold)
	}
	if p.UserID != DefaultValue {
		t.Errorf("UserID = %q, want %q", p.UserID, DefaultValue)
	}
}

// TestNewSearchParams_自定义选项 测试搜索选项覆盖。
func TestNewSearchParams_自定义选项(t *testing.T) {
	p := newSearchParams("q", 10,
		SearchWithUserID("u2"),
		SearchWithThreshold(0.5),
	)
	if p.UserID != "u2" {
		t.Errorf("UserID = %q, want %q", p.UserID, "u2")
	}
	if p.Threshold != 0.5 {
		t.Errorf("Threshold = %f, want 0.5", p.Threshold)
	}
}
```

- [ ] **Step 6: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/ltm/... -count=1`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add -A && git commit -m "feat(ltm): 创建 ltm 包骨架 — doc.go + models.go + options.go + 测试"
```

---

## Task 6: LongTermMemory 结构体 + 单例 + helpers

**Files:**
- Create: `internal/agentcore/memory/ltm/long_term_memory.go`
- Create: `internal/agentcore/memory/ltm/helpers.go`
- Create: `internal/agentcore/memory/ltm/helpers_test.go`
- Create: `internal/agentcore/memory/ltm/long_term_memory_test.go`

- [ ] **Step 1: 创建 helpers.go**

包含 `validateID`、`checkMessages`、`getHistoryMessages`、`runMigration` 四个私有辅助方法。

- [ ] **Step 2: 创建 helpers_test.go**

覆盖 validateID 三种失败场景 + 成功、checkMessages 截断和 human 消息检测、getHistoryMessages 基本逻辑、runMigration 成功和失败。

- [ ] **Step 3: 创建 long_term_memory.go**

包含 `LongTermMemory` 结构体定义、`sync.Once` 单例 `GetLongTermMemory()`、`NewLongTermMemory()` 构造函数、`logComponent` 常量。

- [ ] **Step 4: 创建 long_term_memory_test.go**

覆盖单例返回同一实例、NewLongTermMemory 初始化零值检查。

- [ ] **Step 5: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/ltm/... -count=1`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add -A && git commit -m "feat(ltm): LongTermMemory 结构体 + sync.Once 单例 + helpers"
```

---

## Task 7: RegisterStore + RegisterPlugin + MigrateBetweenIndices

**Files:**
- Create: `internal/agentcore/memory/ltm/register.go`
- Create: `internal/agentcore/memory/ltm/register_test.go`

- [ ] **Step 1: 创建 register.go**

包含 `RegisterStore`（注册 4 种 store + 自动创建 SimpleMemoryIndex + create_tables + 4 类 run_migration）、`RegisterPlugin`（注册 BaseMemoryIndex 插件）、`MigrateBetweenIndices`（跨索引迁移静态方法）。

对齐 Python `register_store` 完整流程：kv_store 必填校验 → vector_store 类型校验 → db_store 类型校验 → message_store 类型校验 → 赋值 → 自动注册 SimpleMemoryIndex → create_tables → 自动创建 SqlMessageStore → set_config → 4 类 migration。

- [ ] **Step 2: 创建 register_test.go**

覆盖 kv_store 为 nil 返回错误、正常注册流程、RegisterPlugin 设置 memoryIndex、MigrateBetweenIndices 批量迁移。

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/ltm/... -count=1`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add -A && git commit -m "feat(ltm): RegisterStore + RegisterPlugin + MigrateBetweenIndices"
```

---

## Task 8: SetConfig + Scope 配置 CRUD

**Files:**
- Create: `internal/agentcore/memory/ltm/config_ops.go`
- Create: `internal/agentcore/memory/ltm/scope_helpers.go`
- Create: `internal/agentcore/memory/ltm/config_ops_test.go`
- Create: `internal/agentcore/memory/ltm/scope_helpers_test.go`

- [ ] **Step 1: 创建 scope_helpers.go**

包含 `getScopeLLM`（scope 配置优先→系统默认→baseLLM）、`getScopeConfig`（内存缓存优先→KVStore 读取→解密 API Key）、`applyScopeEmbedding`（设置 memoryIndex 的 embedding 模型）、`getScopeEmbeddingModel`（获取/缓存 scope embedding）。

- [ ] **Step 2: 创建 config_ops.go**

包含 `SetConfig`（初始化所有 manager：ScopeUserMappingManager、MessageManager、FragmentMemoryManager、SummaryManager、VariableManager、WriteManager、SearchManager、Generator + 设置 LLM）、`SetScopeConfig`（深拷贝→加密 API Key→存 KVStore→清除 embedding 缓存）、`GetScopeConfig`（从 KVStore 读取→解密 API Key）、`DeleteScopeConfig`（删除 KVStore 条目+内存缓存）。

- [ ] **Step 3: 创建测试**

scope_helpers_test.go 覆盖 getScopeLLM 三级回退、getScopeConfig 缓存优先+解密、applyScopeEmbedding 调用 memoryIndex.SetEmbeddingModel。

config_ops_test.go 覆盖 SetConfig 初始化各 manager、SetScopeConfig 加密持久化、GetScopeConfig 解密读取、DeleteScopeConfig 清理。

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/ltm/... -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add -A && git commit -m "feat(ltm): SetConfig + Scope 配置 CRUD + scope helpers"
```

---

## Task 9: AddMessages 核心写入流程

**Files:**
- Create: `internal/agentcore/memory/ltm/add_messages.go`
- Create: `internal/agentcore/memory/ltm/add_messages_test.go`

- [ ] **Step 1: 创建 add_messages.go**

包含公开方法 `AddMessages`（① emit_before MEMORY_ADDED → ② addMessagesImpl）和私有方法 `addMessagesImpl`。

`addMessagesImpl` 完整流程对齐 Python：
1. validateID(scopeID)
2. getScopeLLM(scopeID)
3. getScopeConfig(scopeID)
4. applyScopeEmbedding(scopeID)
5. DistributedLock(kvStore, "user/{userID}")
6. LLM 为空返回错误
7. getHistoryMessages(userID, scopeID, sessionID, historyWindowSize)
8. scopeUserMappingManager.Add(userID, scopeID)
9. timestamp 为 nil 时 time.Now()
10. 遍历 messages 调 messageManager.Add()
11. genMem=false → 返回空 AddMemResult
12. checkMessages → 无 human 消息返回空 AddMemResult
13. generator.GenAllMemory(ctx, params)
14. writeManager.AddMemories(ctx, userID, scopeID, allMemory, llm)
15. 按 MemoryType 分类填充 AddMemResult

- [ ] **Step 2: 创建 add_messages_test.go**

覆盖：回调触发验证、genMem=false 返回空结果、无 human 消息返回空结果、正常写入流程、LLM 未初始化返回错误、scopeID 无效返回错误。

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/ltm/... -count=1`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add -A && git commit -m "feat(ltm): AddMessages 核心写入流程 + 回调包装"
```

---

## Task 10: SearchUserMem + SearchUserHistorySummary + GetVariables

**Files:**
- Create: `internal/agentcore/memory/ltm/search_ops.go`
- Create: `internal/agentcore/memory/ltm/search_ops_test.go`

- [ ] **Step 1: 创建 search_ops.go**

包含：
- `SearchUserMem`（① emit_before SEARCH_STARTED → ② searchUserMemImpl → ③ trigger SEARCH_FINISHED）
- `SearchUserHistorySummary`（同上模式，搜索类型限 SUMMARY）
- `GetVariables`（names 为 nil/单个 string/[]string 三种分支，对齐 Python 的 Union[str, list[str], None]）

`searchUserMemImpl` 流程：validateID → searchManager 初始化检查 → applyScopeEmbedding → SearchManager.Search → 排序截断 → 构建 []MemResult。

- [ ] **Step 2: 创建 search_ops_test.go**

覆盖：回调触发验证（before + after）、searchManager 未初始化返回错误、scopeID 无效返回错误、正常搜索返回 MemResult。

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/ltm/... -count=1`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add -A && git commit -m "feat(ltm): SearchUserMem + SearchUserHistorySummary + GetVariables"
```

---

## Task 11: Delete 操作

**Files:**
- Create: `internal/agentcore/memory/ltm/delete_ops.go`
- Create: `internal/agentcore/memory/ltm/delete_ops_test.go`

- [ ] **Step 1: 创建 delete_ops.go**

包含：
- `DeleteMemByID`（① emit_before MEMORY_DELETED → ② 分布式锁 → writeManager.DeleteMemByID）
- `DeleteMemByUserID`（① emit_before MEMORY_DELETED → ② 分布式锁 → writeManager.DeleteMemByUserID）
- `DeleteMemByScope`（遍历 scope_user_mapping → 逐用户分布式锁 → writeManager.DeleteMemByUserID → 删除 mapping）
- `DeleteMessagesByUserAndScope`（messageManager.DeleteByUserAndScope）
- `DeleteVariables`（分布式锁 → variableManager.DeleteUserVariable 逐个删除）

- [ ] **Step 2: 创建 delete_ops_test.go**

覆盖：回调触发验证、writeManager 未初始化返回错误、scopeID 无效返回错误、正常删除流程。

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/ltm/... -count=1`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add -A && git commit -m "feat(ltm): DeleteMemByID + DeleteMemByUserID + DeleteMemByScope + DeleteVariables"
```

---

## Task 12: Update 操作

**Files:**
- Create: `internal/agentcore/memory/ltm/update_ops.go`
- Create: `internal/agentcore/memory/ltm/update_ops_test.go`

- [ ] **Step 1: 创建 update_ops.go**

包含：
- `UpdateMemByID`（① emit_before MEMORY_UPDATED → ② 分布式锁 → applyScopeEmbedding → writeManager.UpdateMemByID）
- `UpdateVariables`（分布式锁 → variableManager.UpdateUserVariable 逐个更新）

- [ ] **Step 2: 创建 update_ops_test.go**

覆盖：回调触发验证、writeManager/variableManager 未初始化返回错误、正常更新流程。

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/ltm/... -count=1`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add -A && git commit -m "feat(ltm): UpdateMemByID + UpdateVariables"
```

---

## Task 13: Query 操作（只读查询）

**Files:**
- Create: `internal/agentcore/memory/ltm/query_ops.go`
- Create: `internal/agentcore/memory/ltm/query_ops_test.go`

- [ ] **Step 1: 创建 query_ops.go**

包含：
- `GetRecentMessages`（validateID → messageManager.Get → 提取 BaseMessage 列表）
- `GetMessageByID`（messageManager 未初始化返回错误 → messageManager.GetByID）
- `UserMemTotalNum`（validateID → searchManager.ListUserProfile → 返回 len）
- `GetUserMemByPage`（validateID → searchManager.ListUserMem → 构建 []MemInfo）

- [ ] **Step 2: 创建 query_ops_test.go**

覆盖：messageManager/searchManager 未初始化返回错误、scopeID 无效返回错误、正常查询流程。

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/ltm/... -count=1`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add -A && git commit -m "feat(ltm): GetRecentMessages + GetMessageByID + UserMemTotalNum + GetUserMemByPage"
```

---

## Task 14: 全量编译验证 + 覆盖率检查

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uapclaw-gateway && go build ./...`
Expected: 编译通过

- [ ] **Step 2: 覆盖率检查**

Run: `cd /home/opensource/uapclaw-gateway && go test -cover ./internal/agentcore/memory/ltm/...`
Expected: 覆盖率 ≥ 85%

- [ ] **Step 3: 最终提交**

```bash
git add -A && git commit -m "feat(ltm): 7.27 LongTermMemory 完整实现 — 编译通过 + 覆盖率达标"
```

---

## Spec 覆盖检查

| 设计文档需求 | 对应 Task |
|-------------|----------|
| BaseMemoryIndex 加 SetEmbeddingModel | Task 1 |
| MemoryEventData 加字段 | Task 2 |
| MemoryScopeConfig 序列化 | Task 3 |
| IMPLEMENTATION_PLAN 状态更新 | Task 4 |
| ltm 包骨架 + models + options | Task 5 |
| LongTermMemory 结构体 + 单例 + helpers | Task 6 |
| RegisterStore / RegisterPlugin / MigrateBetweenIndices | Task 7 |
| SetConfig + Scope 配置 CRUD | Task 8 |
| AddMessages 核心写入流程 | Task 9 |
| SearchUserMem / SearchUserHistorySummary / GetVariables | Task 10 |
| Delete 操作（5 个方法） | Task 11 |
| Update 操作（2 个方法） | Task 12 |
| Query 操作（4 个方法） | Task 13 |
| 全量编译 + 覆盖率 | Task 14 |
| Python 22 个公开方法 + 9 个私有方法 | Task 6-13 |
| ReActAgent 式回调包装 | Task 9-12 |
| sync.Once 单例 | Task 6 |
| 日志同步（logger 对齐 Python memory_logger） | Task 6-13（各方法中同步） |
