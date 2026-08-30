# 7.7 SummaryManager / VariableManager 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现记忆系统的 SummaryManager、VariableManager 和 KvPrefixRegistry，完成领域七第 7.7 步。

**Architecture:** SummaryManager 嵌入 memoryManagerBase，通过 BaseMemoryIndex 存取摘要记忆；VariableManager 不嵌入 memoryManagerBase，独立持有 BaseKVStore + AesStorageCodec，通过 KV 存储管理变量记忆。KvPrefixRegistry 是全局前缀注册表，供迁移器发现 KV 前缀。

**Tech Stack:** Go 1.22+, testify/assert, 现有 fakeMemoryIndex + InMemoryKVStore 测试基础设施

**Design Spec:** `docs/superpowers/specs/2027-04-15-summary-variable-manager-7.7-design.md`

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| **新增** | `memory/common/doc.go` | common 包文档 |
| **新增** | `memory/common/kv_prefix_registry.go` | KvPrefixRegistry 实现 |
| **新增** | `memory/common/kv_prefix_registry_test.go` | KvPrefixRegistry 测试 |
| **新增** | `manage/index/summary_manager.go` | SummaryManager 完整实现 |
| **新增** | `manage/index/summary_manager_test.go` | SummaryManager 测试 |
| **新增** | `manage/index/variable_manager.go` | VariableManager 完整实现 |
| **新增** | `manage/index/variable_manager_test.go` | VariableManager 测试 |
| **修改** | `manage/index/doc.go` | 更新文件目录 |
| **修改** | `manage/doc.go` | 更新文件目录 |

---

### Task 1: KvPrefixRegistry 实现 + 测试

**Files:**
- Create: `internal/agentcore/memory/common/doc.go`
- Create: `internal/agentcore/memory/common/kv_prefix_registry.go`
- Create: `internal/agentcore/memory/common/kv_prefix_registry_test.go`

- [ ] **Step 1: 创建 common/doc.go**

```go
// Package common 提供记忆系统的公共工具。
//
// 本包包含 KV 前缀注册表（KvPrefixRegistry）等跨模块共享的基础设施组件，
// 供记忆管理器和迁移器使用。
//
// 文件目录：
//
//	common/
//	├── doc.go                  # 包文档
//	└── kv_prefix_registry.go   # KV 前缀注册表
//
// 对应 Python 代码：
//
//	openjiuwen/core/memory/common/
package common
```

- [ ] **Step 2: 创建 kv_prefix_registry.go**

对齐 Python: `openjiuwen/core/memory/common/kv_prefix_registry.py`

```go
package common

import (
	"fmt"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// KvPrefixRegistry KV 前缀注册表，管理记忆模块使用的 KV 存储键前缀。
//
// 允许记忆模块注册当前和旧版前缀，使 KV 迁移器能动态发现正在使用的前缀，
// 而无需硬编码。当模块在版本演进中添加或移除前缀时，迁移器能自动适应。
//
// 对应 Python: openjiuwen/core/memory/common/kv_prefix_registry.py (KvPrefixRegistry)
type KvPrefixRegistry struct {
	// mu 保护并发访问
	mu sync.RWMutex
	// allPrefixes 所有前缀集合（current + legacy）
	allPrefixes map[string]bool
	// currentPrefixes 当前使用的前缀集合
	currentPrefixes map[string]bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// KVPrefixRegistry 全局 KV 前缀注册表实例。
	// 对齐 Python: kv_prefix_registry = KvPrefixRegistry()
	KVPrefixRegistry = NewKvPrefixRegistry()

	// logComponent 日志组件常量
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewKvPrefixRegistry 创建新的 KV 前缀注册表。
func NewKvPrefixRegistry() *KvPrefixRegistry {
	return &KvPrefixRegistry{
		allPrefixes:     make(map[string]bool),
		currentPrefixes: make(map[string]bool),
	}
}

// RegisterCurrent 注册一个当前（活跃）键前缀。
//
// 如果前缀为空或纯空白字符，返回 error。已存在的前缀不会重复添加。
//
// 对应 Python: KvPrefixRegistry.register_current
func (r *KvPrefixRegistry) RegisterCurrent(prefix string) error {
	if prefix == "" || len(prefix) != len(trimSpace(prefix)) {
		return fmt.Errorf("前缀不能为空或仅包含空白字符: %q", prefix)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.currentPrefixes[prefix] = true
	r.allPrefixes[prefix] = true
	return nil
}

// RegisterLegacy 注册一个旧版（已弃用）键前缀，用于迁移检测。
//
// 如果前缀为空或纯空白字符，返回 error。已存在的前缀不会重复添加。
//
// 对应 Python: KvPrefixRegistry.register_legacy
func (r *KvPrefixRegistry) RegisterLegacy(prefix string) error {
	if prefix == "" || len(prefix) != len(trimSpace(prefix)) {
		return fmt.Errorf("前缀不能为空或仅包含空白字符: %q", prefix)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.allPrefixes[prefix] = true
	return nil
}

// GetAllPrefixes 获取所有已注册的前缀（current + legacy）。
//
// 返回前缀切片的副本，调用方可以安全修改。
// 对应 Python: KvPrefixRegistry.get_all_prefixes
func (r *KvPrefixRegistry) GetAllPrefixes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]string, 0, len(r.allPrefixes))
	for prefix := range r.allPrefixes {
		result = append(result, prefix)
	}
	return result
}

// Unregister 从 current 和 all 集合中移除指定前缀。
//
// 对应 Python: KvPrefixRegistry.unregister
func (r *KvPrefixRegistry) Unregister(prefix string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.allPrefixes, prefix)
	delete(r.currentPrefixes, prefix)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// trimSpace 去除前后空白。
func trimSpace(s string) string {
	// 使用 strings.TrimSpace 实现
	return strings.TrimSpace(s)
}
```

**注意**：上面 `trimSpace` 实现需要 import `strings`，`strings.TrimSpace(s)` 就是 trimSpace 的实现。实际写代码时直接用 `strings.TrimSpace` 即可，`trimSpace` 辅助函数可省略，直接在 RegisterCurrent/RegisterLegacy 中用 `strings.TrimSpace(prefix) == ""` 判断。

- [ ] **Step 3: 创建 kv_prefix_registry_test.go**

```go
//go:build test

package common

import (
	"testing"
)

func TestNewKvPrefixRegistry(t *testing.T) {
	r := NewKvPrefixRegistry()
	if r == nil {
		t.Fatal("NewKvPrefixRegistry 返回 nil")
	}
	if len(r.GetAllPrefixes()) != 0 {
		t.Errorf("新建注册表应为空，得到 %d 个前缀", len(r.GetAllPrefixes()))
	}
}

func TestKvPrefixRegistry_RegisterCurrent(t *testing.T) {
	r := NewKvPrefixRegistry()
	err := r.RegisterCurrent("user_var")
	if err != nil {
		t.Fatalf("RegisterCurrent 返回 error: %v", err)
	}
	prefixes := r.GetAllPrefixes()
	if len(prefixes) != 1 {
		t.Fatalf("期望 1 个前缀，得到 %d", len(prefixes))
	}
	found := false
	for _, p := range prefixes {
		if p == "user_var" {
			found = true
		}
	}
	if !found {
		t.Error("期望包含 user_var 前缀")
	}
}

func TestKvPrefixRegistry_RegisterCurrent_Empty(t *testing.T) {
	r := NewKvPrefixRegistry()
	err := r.RegisterCurrent("")
	if err == nil {
		t.Fatal("空前缀应返回 error")
	}
}

func TestKvPrefixRegistry_RegisterCurrent_Whitespace(t *testing.T) {
	r := NewKvPrefixRegistry()
	err := r.RegisterCurrent("  ")
	if err == nil {
		t.Fatal("纯空白前缀应返回 error")
	}
}

func TestKvPrefixRegistry_RegisterLegacy(t *testing.T) {
	r := NewKvPrefixRegistry()
	err := r.RegisterLegacy("old_prefix")
	if err != nil {
		t.Fatalf("RegisterLegacy 返回 error: %v", err)
	}
	prefixes := r.GetAllPrefixes()
	if len(prefixes) != 1 {
		t.Fatalf("期望 1 个前缀，得到 %d", len(prefixes))
	}
}

func TestKvPrefixRegistry_RegisterLegacy_Empty(t *testing.T) {
	r := NewKvPrefixRegistry()
	err := r.RegisterLegacy("")
	if err == nil {
		t.Fatal("空前缀应返回 error")
	}
}

func TestKvPrefixRegistry_RegisterCurrent_Duplicate(t *testing.T) {
	r := NewKvPrefixRegistry()
	_ = r.RegisterCurrent("user_var")
	_ = r.RegisterCurrent("user_var") // 重复注册不应报错
	prefixes := r.GetAllPrefixes()
	if len(prefixes) != 1 {
		t.Errorf("重复注册应只有 1 个前缀，得到 %d", len(prefixes))
	}
}

func TestKvPrefixRegistry_RegisterLegacy_NotInCurrent(t *testing.T) {
	r := NewKvPrefixRegistry()
	_ = r.RegisterLegacy("legacy_prefix")
	// legacy 前缀只出现在 allPrefixes 中，不出现在 currentPrefixes 中
	r.mu.RLock()
	_, inCurrent := r.currentPrefixes["legacy_prefix"]
	r.mu.RUnlock()
	if inCurrent {
		t.Error("legacy 前缀不应出现在 currentPrefixes 中")
	}
}

func TestKvPrefixRegistry_GetAllPrefixes_Copy(t *testing.T) {
	r := NewKvPrefixRegistry()
	_ = r.RegisterCurrent("a")
	_ = r.RegisterLegacy("b")
	prefixes := r.GetAllPrefixes()
	prefixes[0] = "modified" // 修改返回值不应影响原注册表
	if len(r.GetAllPrefixes()) != 2 {
		t.Error("修改返回值不应影响原注册表")
	}
}

func TestKvPrefixRegistry_Unregister(t *testing.T) {
	r := NewKvPrefixRegistry()
	_ = r.RegisterCurrent("user_var")
	r.Unregister("user_var")
	prefixes := r.GetAllPrefixes()
	if len(prefixes) != 0 {
		t.Errorf("注销后期望 0 个前缀，得到 %d", len(prefixes))
	}
}

func TestKvPrefixRegistry_Unregister_Nonexistent(t *testing.T) {
	r := NewKvPrefixRegistry()
	r.Unregister("nonexistent") // 不存在的前缀不报错
	if len(r.GetAllPrefixes()) != 0 {
		t.Error("注销不存在的前缀不应产生副作用")
	}
}

func TestKVPrefixRegistry_GlobalInstance(t *testing.T) {
	// 验证全局实例可用
	err := KVPrefixRegistry.RegisterCurrent("test_global_prefix")
	if err != nil {
		t.Fatalf("全局实例 RegisterCurrent 返回 error: %v", err)
	}
	// 清理
	KVPrefixRegistry.Unregister("test_global_prefix")
}
```

- [ ] **Step 4: 运行测试**

```bash
cd /home/opensource/uapclaw-gateway && go test -tags test ./internal/agentcore/memory/common/... -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/memory/common/
git commit -m "feat(7.7): 实现 KvPrefixRegistry KV 前缀注册表"
```

---

### Task 2: SummaryManager 实现

**Files:**
- Create: `internal/agentcore/memory/manage/index/summary_manager.go`

- [ ] **Step 1: 创建 summary_manager.go**

对齐 Python: `openjiuwen/core/memory/manage/index/summary_manager.py`

关键点：
- 嵌入 `memoryManagerBase`（依赖 BaseMemoryIndex）
- memType = "summary"
- AddMemories 从 memories map 中过滤 mem_type=="summary"，断言为 *SummaryUnit
- convertToMemoryDocs: SummaryUnit → MemoryDoc（text=summary, fields={source_id: messageMemID, metadata: {}}）
- search 的 memTypes 参数硬编码为 `[m.memType]`
- listUserSummary 返回 MemoryDoc 切片，按 Timestamp 降序
- 复用 fragment_manager.go 中已有的 parseTimestamp
- 所有 Python memory_logger 调用在 Go 中用 logger.ComponentAgentCore 结构化日志

方法签名（实现 BaseMemoryManager 接口）：

```go
func NewSummaryManager(memoryIndex index.BaseMemoryIndex, cryptoKey []byte) *SummaryManager
func (m *SummaryManager) AddMemories(ctx context.Context, userID string, scopeID string, memories map[string][]mem_model.MemoryUnit, llmModel ...*llm.Model) ([]mem_model.MemoryUnit, error)
func (m *SummaryManager) Update(ctx context.Context, userID string, scopeID string, memID string, newMemory string) (bool, error)
func (m *SummaryManager) Search(ctx context.Context, userID string, scopeID string, query string, topK int, memTypes []string) ([]*index.MemorySearchResult, error)
func (m *SummaryManager) Get(ctx context.Context, userID string, scopeID string, memID string) (*index.MemoryDoc, error)
func (m *SummaryManager) Delete(ctx context.Context, userID string, scopeID string, memID string) (bool, error)
func (m *SummaryManager) DeleteByUserID(ctx context.Context, userID string, scopeID string) (bool, error)
func (m *SummaryManager) ListUserSummary(ctx context.Context, userID string, scopeID string, offset int, batchSize int) ([]*index.MemoryDoc, error)
```

内部辅助方法：
```go
func (m *SummaryManager) convertToMemoryDocs(units []*mem_model.SummaryUnit) []*index.MemoryDoc
```

- [ ] **Step 2: Commit**

```bash
git add internal/agentcore/memory/manage/index/summary_manager.go
git commit -m "feat(7.7): 实现 SummaryManager 摘要记忆管理器"
```

---

### Task 3: SummaryManager 测试

**Files:**
- Create: `internal/agentcore/memory/manage/index/summary_manager_test.go`

- [ ] **Step 1: 创建 summary_manager_test.go**

复用 base_manager_test.go 中已有的 `fakeMemoryIndex`（同包内可用）。

测试用例：

```go
//go:build test

package index

import (
	"context"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/index"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
)
```

必须覆盖的测试：

| 测试名 | 场景 |
|--------|------|
| TestNewSummaryManager | 构造函数，memType="summary" |
| TestSummaryManager_AddMemories | 单条摘要添加 |
| TestSummaryManager_AddMemories_Multiple | 多条摘要 |
| TestSummaryManager_AddMemories_NonSummaryTypeIgnored | 非 summary 类型被忽略 |
| TestSummaryManager_AddMemories_EmptyResult | 空结果记 Warn 日志返回空 |
| TestSummaryManager_AddMemories_ValidateParams | userID 为空返回 error |
| TestSummaryManager_Update | 按 ID 更新 |
| TestSummaryManager_Update_NotFound | ID 不存在返回 false |
| TestSummaryManager_Delete | 按 ID 删除 |
| TestSummaryManager_DeleteByUserID | 按 user+scope 删除 |
| TestSummaryManager_Get | 按 ID 获取 |
| TestSummaryManager_Get_NotFound | ID 不存在返回 nil |
| TestSummaryManager_Search | 语义搜索 |
| TestSummaryManager_Search_WithMemTypes | 按 mem_type 过滤 |
| TestSummaryManager_ListUserSummary | 分页列出，按 timestamp 降序 |
| TestSummaryManager_ListUserSummary_Empty | 无数据返回空 |

- [ ] **Step 2: 运行测试**

```bash
cd /home/opensource/uapclaw-gateway && go test -tags test ./internal/agentcore/memory/manage/index/... -run TestSummaryManager -v
```

Expected: PASS

- [ ] **Step 3: 检查覆盖率**

```bash
cd /home/opensource/uapclaw-gateway && go test -tags test -coverprofile=coverage.out ./internal/agentcore/memory/manage/index/... && go tool cover -func=coverage.out | grep summary_manager
```

Expected: ≥ 85%

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/memory/manage/index/summary_manager_test.go
git commit -m "test(7.7): SummaryManager 单元测试"
```

---

### Task 4: VariableManager 实现

**Files:**
- Create: `internal/agentcore/memory/manage/index/variable_manager.go`

- [ ] **Step 1: 创建 variable_manager.go**

对齐 Python: `openjiuwen/core/memory/manage/index/variable_manager.py`

关键点：
- **不嵌入 memoryManagerBase**，独立持 kvStore/cryptoKey/codec/memType
- 构造时调用 `common.KVPrefixRegistry.RegisterCurrent("user_var")` 和 `RegisterCurrent("session_var")`
- AddMemories: 遍历 VariableUnit → makeVariablePairs → codec.Encode(value) → []byte → kvStore.Set
- update/delete/get/search: ⚠️ Not implemented — 记日志后返回零值
- DeleteByUserID: 按 user_var/session_var 前缀 DeleteByPrefix
- QueryVariable: 按 name 查值（kvStore.Get + codec.Decode）或按前缀查全部（kvStore.GetByPrefix + codec.Decode）
- UpdateUserVariable: 先 QueryVariable 检查存在性，再 makeVariablePairs + kvStore.Set
- DeleteUserVariable: makeVariablePairs(key only) + kvStore.Delete
- makeVariablePairs: 构造 key（user_var/userID/scopeID/varName），codec.Encode(value) 转 []byte
- checkUserAndScopeID: 空值时记 Error 日志
- checkExist: 检查变量字典中是否存在且非空

结构体：

```go
type VariableManager struct {
	kvStore   kv.BaseKVStore
	cryptoKey []byte
	codec     *codec.AesStorageCodec
	memType   string
}
```

方法签名（实现 BaseMemoryManager 接口）：

```go
func NewVariableManager(kvStore kv.BaseKVStore, cryptoKey []byte) (*VariableManager, error)
func (m *VariableManager) AddMemories(ctx context.Context, userID string, scopeID string, memories map[string][]mem_model.MemoryUnit, llmModel ...*llm.Model) ([]mem_model.MemoryUnit, error)
func (m *VariableManager) Update(ctx context.Context, userID string, scopeID string, memID string, newMemory string) (bool, error)
func (m *VariableManager) Search(ctx context.Context, userID string, scopeID string, query string, topK int, memTypes []string) ([]*index.MemorySearchResult, error)
func (m *VariableManager) Get(ctx context.Context, userID string, scopeID string, memID string) (*index.MemoryDoc, error)
func (m *VariableManager) Delete(ctx context.Context, userID string, scopeID string, memID string) (bool, error)
func (m *VariableManager) DeleteByUserID(ctx context.Context, userID string, scopeID string) (bool, error)
func (m *VariableManager) UpdateUserVariable(ctx context.Context, userID string, scopeID string, varName string, varMem string) error
func (m *VariableManager) DeleteUserVariable(ctx context.Context, userID string, scopeID string, varName string) error
func (m *VariableManager) QueryVariable(ctx context.Context, userID string, scopeID string, name string, sessionID string) (map[string]string, error)
```

内部辅助方法：
```go
func (m *VariableManager) makeVariablePairs(usrID string, forDeletion bool, scopeID string, varName string, sessionID string, userVarValue string, sessionVarValue string) (string, []byte)
func (m *VariableManager) checkUserAndScopeID(userID string, scopeID string, context string)
func checkExist(variableDict map[string]string, variableName string) bool
```

常量：
```go
const (
	separator       = "/"
	userVarPrefix   = "user_var"
	sessionVarPrefix = "session_var"
)
```

编码/解码流程：
- 写入: `m.codec.Encode(varMem)` → string → `[]byte(encoded)` → `m.kvStore.Set(ctx, key, []byte(encoded))`
- 读取: `m.kvStore.Get(ctx, key)` → `[]byte` → `string(raw)` → `m.codec.Decode(strValue)` → 原文

- [ ] **Step 2: Commit**

```bash
git add internal/agentcore/memory/manage/index/variable_manager.go
git commit -m "feat(7.7): 实现 VariableManager 变量记忆管理器"
```

---

### Task 5: VariableManager 测试

**Files:**
- Create: `internal/agentcore/memory/manage/index/variable_manager_test.go`

- [ ] **Step 1: 创建 variable_manager_test.go**

使用 `kv.NewInMemoryKVStore()` 作为测试 KV 后端。

测试用例：

| 测试名 | 场景 |
|--------|------|
| TestNewVariableManager | 构造函数，memType="variable" |
| TestNewVariableManager_NilKVStore | nil kvStore 仍可创建（7.8 WriteManager 注册用） |
| TestVariableManager_AddMemories | 添加变量 |
| TestVariableManager_AddMemories_NonVariableTypeIgnored | 非 variable 类型被忽略 |
| TestVariableManager_AddMemories_KVStoreNil | kvStore 为 nil 时返回空 |
| TestVariableManager_Update_NotImplemented | 记 Warn 日志返回 false |
| TestVariableManager_Delete_NotImplemented | 记 Error 日志返回 false |
| TestVariableManager_Get_NotImplemented | 记 Warn 日志返回 nil |
| TestVariableManager_Search_NotImplemented | 记 Warn 日志返回 nil |
| TestVariableManager_DeleteByUserID | 按 user_var/session_var 前缀删除 |
| TestVariableManager_DeleteByUserID_KVStoreNil | kvStore 为 nil 时返回 false |
| TestVariableManager_UpdateUserVariable | 更新已存在的变量 |
| TestVariableManager_UpdateUserVariable_NotExist | 变量不存在时不更新 |
| TestVariableManager_DeleteUserVariable | 按名删除变量 |
| TestVariableManager_QueryVariable_ByName | 按 name 查值 |
| TestVariableManager_QueryVariable_All | name 为空时查全部 |
| TestVariableManager_QueryVariable_WithSessionID | 按 sessionID 查会话级变量 |
| TestVariableManager_QueryVariable_CheckUserAndScopeID | 空值时记 Error 日志 |
| TestVariableManager_MakeVariablePairs_UserVar | 构造 user_var key |
| TestVariableManager_MakeVariablePairs_SessionVar | 构造 session_var key |
| TestCheckExist | checkExist 辅助方法 |
| TestVariableManager_AddMemories_WithCryptoKey | 有加密 key 时的写入/读取 |

- [ ] **Step 2: 运行测试**

```bash
cd /home/opensource/uapclaw-gateway && go test -tags test ./internal/agentcore/memory/manage/index/... -run TestVariableManager -v
```

Expected: PASS

- [ ] **Step 3: 检查覆盖率**

```bash
cd /home/opensource/uapclaw-gateway && go test -tags test -coverprofile=coverage.out ./internal/agentcore/memory/manage/index/... && go tool cover -func=coverage.out | grep variable_manager
```

Expected: ≥ 85%

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/memory/manage/index/variable_manager_test.go
git commit -m "test(7.7): VariableManager 单元测试"
```

---

### Task 6: 更新 doc.go 文件

**Files:**
- Modify: `internal/agentcore/memory/manage/index/doc.go`
- Modify: `internal/agentcore/memory/manage/doc.go`

- [ ] **Step 1: 更新 manage/index/doc.go**

在文件目录中加入 summary_manager.go 和 variable_manager.go：

```go
// Package index 提供记忆管理器接口和实现。
//
// 本包定义了 BaseMemoryManager 抽象接口和 memoryManagerBase 嵌入结构体，
// 提供记忆管理器的公共逻辑（参数校验、异常包装、加解密）。
// FragmentMemoryManager 管理碎片记忆，SummaryManager 管理摘要记忆，
// VariableManager 管理变量记忆（独立实现，不嵌入 memoryManagerBase）。
//
// 文件目录：
//
//	index/
//	├── doc.go               # 包文档
//	├── base_manager.go      # BaseMemoryManager 接口 + memoryManagerBase 嵌入结构体
//	├── fragment_manager.go  # FragmentMemoryManager 碎片记忆管理器
//	├── summary_manager.go   # SummaryManager 摘要记忆管理器
//	└── variable_manager.go  # VariableManager 变量记忆管理器
//
// 对应 Python 代码：
//
//	openjiuwen/core/memory/manage/index/
package index
```

- [ ] **Step 2: 更新 manage/doc.go**

在文件目录中加入 common/ 子包：

```go
// Package manage 提供记忆管理器及其子组件。
//
// 本包实现记忆系统的核心管理器（FragmentMemoryManager、SummaryManager、VariableManager）
// 以及统一写入管理器（WriteManager）和搜索管理器（SearchManager）。
// FragmentMemoryManager 和 SummaryManager 通过 BaseMemoryManager 接口统一抽象，
// 通过 BaseMemoryIndex 接口委托存储操作。
// VariableManager 独立实现 BaseMemoryManager 接口，通过 BaseKVStore 委托 KV 存储操作。
//
// 文件目录：
//
//	manage/
//	├── doc.go              # 包文档
//	├── index/              # 记忆管理器实现
//	│   ├── doc.go          # index 包文档
//	│   ├── base_manager.go # BaseMemoryManager 接口 + memoryManagerBase 嵌入结构体
//	│   ├── fragment_manager.go # FragmentMemoryManager 碎片记忆管理器
//	│   ├── summary_manager.go  # SummaryManager 摘要记忆管理器
//	│   └── variable_manager.go # VariableManager 变量记忆管理器
//	├── mem_model/          # 记忆数据模型和数据库操作
//	│   ├── doc.go          # mem_model 包文档
//	│   ├── memory_unit.go  # 记忆数据模型（MemoryType/OperationType/FragmentMemoryUnit 等）
//	│   └── ...             # 其他数据模型和数据库操作
//	└── update/             # 记忆冲突检查
//	    ├── doc.go          # update 包文档
//	    └── update_checker.go # MemUpdateChecker 记忆冲突检查器
//
// 对应 Python 代码：
//
//	openjiuwen/core/memory/manage/
package manage
```

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/memory/manage/index/doc.go internal/agentcore/memory/manage/doc.go
git commit -m "docs(7.7): 更新 doc.go 文件目录"
```

---

### Task 7: 全量编译验证 + IMPLEMENTATION_PLAN 状态更新

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 全量编译**

```bash
cd /home/opensource/uapclaw-gateway && go build ./...
```

Expected: 编译成功

- [ ] **Step 2: 全量测试**

```bash
cd /home/opensource/uapclaw-gateway && go test -tags test ./internal/agentcore/memory/... -v
```

Expected: 所有测试通过

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md**

将 7.7 行状态从 `☐` 改为 `✅`：

```
| 7.7 | ✅ | SummaryManager / VariableManager | ✅ SummaryManager + ✅ VariableManager + ✅ KvPrefixRegistry | `openjiuwen/core/memory/manage/` |
```

- [ ] **Step 4: Commit**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "chore(7.7): 标记 IMPLEMENTATION_PLAN 7.7 为已完成"
```
