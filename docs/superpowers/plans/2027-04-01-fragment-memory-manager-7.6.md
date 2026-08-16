# 7.6+7.9 FragmentMemoryManager + 数据模型 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 FragmentMemoryManager（7.6）和记忆数据模型（7.9），MemUpdateChecker 暂用 stub

**Architecture:** FragmentMemoryManager 嵌入 memoryManagerBase 公共基类，实现 BaseMemoryManager 接口的 6 个方法 + ListFragmentMemories 特有方法。所有实际存储操作委托给 BaseMemoryIndex。MemUpdateChecker stub 直接返回全部 ADD。目录结构对齐 Python：manage/index/、manage/mem_model/、manage/update/

**Tech Stack:** Go 1.22+, GORM, 已有 BaseMemoryIndex 接口 + SimpleMemoryIndex 实现

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| 创建 | `manage/doc.go` | manage 包文档 |
| 创建 | `manage/mem_model/memory_unit.go` | MemoryType/OperationType/BaseMemoryUnit/FragmentMemoryUnit/VariableUnit/SummaryUnit |
| 创建 | `manage/mem_model/memory_unit_test.go` | 数据模型单元测试 |
| 重命名 | `manage/model/` → `manage/mem_model/` | 对齐 Python 目录名，package 声明 model→mem_model |
| 修改 | `manage/mem_model/doc.go` | 更新包名和文件目录 |
| 修改 | `manage/mem_model/*.go` | package model → package mem_model |
| 修改 | `manage/mem_model/*_test.go` | package model → package mem_model |
| 创建 | `manage/index/doc.go` | index 包文档 |
| 创建 | `manage/index/base_manager.go` | BaseMemoryManager 接口 + memoryManagerBase 嵌入结构体 |
| 创建 | `manage/index/base_manager_test.go` | 基类公共方法测试 |
| 创建 | `manage/index/fragment_manager.go` | FragmentMemoryManager 实现 |
| 创建 | `manage/index/fragment_manager_test.go` | FragmentMemoryManager 完整测试 |
| 创建 | `manage/update/doc.go` | update 包文档 |
| 创建 | `manage/update/update_checker.go` | MemUpdateChecker stub + 数据模型 |
| 创建 | `manage/update/update_checker_test.go` | stub 测试 |

---

### Task 1: 重命名 manage/model/ → manage/mem_model/

**Files:**
- Modify: `manage/model/` → `manage/mem_model/`（目录重命名）
- Modify: `manage/mem_model/*.go`（package 声明 + 内部 import）
- Modify: `manage/mem_model/*_test.go`（package 声明 + 内部 import）
- Modify: `manage/mem_model/doc.go`（更新包名和文件目录）

- [ ] **Step 1: 重命名目录**

```bash
mv internal/agentcore/memory/manage/model internal/agentcore/memory/manage/mem_model
```

- [ ] **Step 2: 更新所有 .go 文件的 package 声明**

将 `manage/mem_model/` 下所有 `.go` 文件的 `package model` 改为 `package mem_model`：

```bash
sed -i 's/^package model$/package mem_model/' internal/agentcore/memory/manage/mem_model/*.go
```

- [ ] **Step 3: 更新 doc.go 包文档**

将 `manage/mem_model/doc.go` 中的 `package model` 已在 Step 2 中更新，但需要更新文档内容中的包名引用和文件目录。更新 `doc.go` 的包说明文字：

```go
// Package mem_model 提供记忆系统的数据模型和数据库操作。
//
// 本包定义了消息存储相关的数据库模型（UserMessage）、
// 通用 SQL CRUD 层（SqlDbStore）、消息存储实现（SqlMessageStore）
// 和消息管理器（MessageManager）。
// Schema 版本管理已迁移到 migrator 包，加解密编解码已迁移到 codec 包。
//
// 文件目录：
//
//	mem_model/
//	├── doc.go                          # 包文档
//	├── memory_unit.go                  # 记忆数据模型（MemoryType/OperationType/FragmentMemoryUnit 等）
//	├── db_model.go                     # 数据库模型（UserMessage、ScopeUserMapping、MemoryMeta）+ CreateTables
//	├── sql_db_store.go                 # SqlDbStore 通用 SQL CRUD 层
//	├── sql_message_store.go            # SqlMessageStore 消息存储实现
//	├── message_manager.go              # MessageManager 消息管理器
//	├── scope_user_mapping_manager.go   # ScopeUserMappingManager 作用域用户映射管理器
//	└── data_id_manager.go              # DataIdManager 唯一 ID 生成器
//
// 对应 Python 代码：
//
//	openjiuwen/core/memory/manage/mem_model/
//
// 关联包：
//
//	memory/codec/              — AesStorageCodec 存储编解码器
//	memory/migration/migrator/ — MemoryMetaManager schema 版本管理器
//
// 核心类型/接口索引：
//
//	MemoryType                — 记忆类型枚举
//	OperationType             — 操作类型枚举
//	BaseMemoryUnit            — 记忆数据项基类
//	FragmentMemoryUnit        — 碎片记忆数据项
//	VariableUnit              — 变量记忆数据项
//	SummaryUnit               — 摘要记忆数据项
//	UserMessage               — 用户消息表 GORM 模型
//	ScopeUserMapping          — 作用域用户映射表 GORM 模型
//	MemoryMeta                — 记忆元数据表 GORM 模型
//	SqlDbStore                — 通用 SQL CRUD 层，封装 GORM 通用操作
//	SqlMessageStore           — BaseMessageStore 的 SQL 实现
//	MessageManager            — 消息管理器
//	ScopeUserMappingManager   — 作用域用户映射管理器
//	DataIdManager             — 唯一 ID 生成器，12字节=6时间+3随机+3哈希
package mem_model
```

- [ ] **Step 4: 搜索并更新所有引用 manage/model 的文件**

```bash
grep -r "memory/manage/model" internal/ --include="*.go" -l
```

对找到的文件，将 import 路径从 `memory/manage/model` 改为 `memory/manage/mem_model`。

- [ ] **Step 5: 编译验证**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/memory/...
```

- [ ] **Step 6: 运行已有测试**

```bash
cd /home/opensource/uapclaw-gateway && go test -tags=test ./internal/agentcore/memory/manage/mem_model/... -v
```

- [ ] **Step 7: 提交**

```bash
git add -A && git commit -m "refactor: 重命名 manage/model/ → manage/mem_model/ 对齐 Python 目录名"
```

---

### Task 2: 创建 mem_model/memory_unit.go — 数据模型

**Files:**
- Create: `manage/mem_model/memory_unit.go`
- Create: `manage/mem_model/memory_unit_test.go`

- [ ] **Step 1: 编写 memory_unit_test.go 失败测试**

```go
//go:build test

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
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway && go test -tags=test ./internal/agentcore/memory/manage/mem_model/... -run TestMemoryType -v
```

- [ ] **Step 3: 实现 memory_unit.go**

```go
package mem_model

// ──────────────────────────── 结构体 ────────────────────────────

// BaseMemoryUnit 记忆数据项基类。
//
// 对应 Python: openjiuwen/core/memory/manage/mem_model/memory_unit.py (BaseMemoryUnit)
type BaseMemoryUnit struct {
	// MemType 记忆类型
	MemType MemoryType
	// MemID 记忆唯一标识
	MemID string
}

// FragmentMemoryUnit 碎片记忆数据项，包含文本内容、关联消息 ID 和操作类型。
//
// 对应 Python: openjiuwen/core/memory/manage/mem_model/memory_unit.py (FragmentMemoryUnit)
type FragmentMemoryUnit struct {
	// BaseMemoryUnit 嵌入基类
	BaseMemoryUnit
	// Content 文本内容
	Content string
	// MessageMemID 关联消息 ID
	MessageMemID string
	// Timestamp 时间戳
	Timestamp string
	// OperationType 操作类型
	OperationType OperationType
}

// VariableUnit 变量记忆数据项。
//
// 对应 Python: openjiuwen/core/memory/manage/mem_model/memory_unit.py (VariableUnit)
type VariableUnit struct {
	// BaseMemoryUnit 嵌入基类
	BaseMemoryUnit
	// VariableName 变量名
	VariableName string
	// VariableMem 变量值
	VariableMem string
}

// SummaryUnit 摘要记忆数据项。
//
// 对应 Python: openjiuwen/core/memory/manage/mem_model/memory_unit.py (SummaryUnit)
type SummaryUnit struct {
	// BaseMemoryUnit 嵌入基类
	BaseMemoryUnit
	// Summary 摘要内容
	Summary string
	// MessageMemID 关联消息 ID
	MessageMemID string
	// Timestamp 时间戳
	Timestamp string
}

// ──────────────────────────── 枚举 ────────────────────────────

// MemoryType 记忆类型枚举。
//
// 对应 Python: openjiuwen/core/memory/manage/mem_model/memory_unit.py (MemoryType)
type MemoryType int

const (
	// MemoryTypeUserProfile 用户画像
	MemoryTypeUserProfile MemoryType = iota
	// MemoryTypeSemanticMemory 语义记忆
	MemoryTypeSemanticMemory
	// MemoryTypeEpisodicMemory 情景记忆
	MemoryTypeEpisodicMemory
	// MemoryTypeVariable 变量
	MemoryTypeVariable
	// MemoryTypeSummary 摘要
	MemoryTypeSummary
	// MemoryTypeUnknown 未知
	MemoryTypeUnknown
)

// OperationType 操作类型枚举。
//
// 对应 Python: openjiuwen/core/memory/manage/mem_model/memory_unit.py (OperationType)
type OperationType int

const (
	// OperationTypeAdd 新增
	OperationTypeAdd OperationType = iota
	// OperationTypeUpdate 更新
	OperationTypeUpdate
	// OperationTypeDelete 删除
	OperationTypeDelete
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseMemoryType 从字符串解析 MemoryType，未匹配时返回 MemoryTypeUnknown。
func ParseMemoryType(s string) MemoryType {
	switch s {
	case "user_profile":
		return MemoryTypeUserProfile
	case "semantic_memory":
		return MemoryTypeSemanticMemory
	case "episodic_memory":
		return MemoryTypeEpisodicMemory
	case "variable":
		return MemoryTypeVariable
	case "summary":
		return MemoryTypeSummary
	default:
		return MemoryTypeUnknown
	}
}

// ParseOperationType 从字符串解析 OperationType，未匹配时返回 OperationTypeAdd。
func ParseOperationType(s string) OperationType {
	switch s {
	case "add":
		return OperationTypeAdd
	case "update":
		return OperationTypeUpdate
	case "delete":
		return OperationTypeDelete
	default:
		return OperationTypeAdd
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// String 实现 fmt.Stringer 接口，对齐 Python MemoryType.value
func (mt MemoryType) String() string {
	switch mt {
	case MemoryTypeUserProfile:
		return "user_profile"
	case MemoryTypeSemanticMemory:
		return "semantic_memory"
	case MemoryTypeEpisodicMemory:
		return "episodic_memory"
	case MemoryTypeVariable:
		return "variable"
	case MemoryTypeSummary:
		return "summary"
	case MemoryTypeUnknown:
		return "unknown"
	default:
		return "unknown"
	}
}

// String 实现 fmt.Stringer 接口，对齐 Python OperationType.value
func (ot OperationType) String() string {
	switch ot {
	case OperationTypeAdd:
		return "add"
	case OperationTypeUpdate:
		return "update"
	case OperationTypeDelete:
		return "delete"
	default:
		return "add"
	}
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && go test -tags=test ./internal/agentcore/memory/manage/mem_model/... -v
```

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/memory/manage/mem_model/memory_unit.go internal/agentcore/memory/manage/mem_model/memory_unit_test.go && git commit -m "feat: 添加 7.9 记忆数据模型 MemoryType/OperationType/FragmentMemoryUnit 等"
```

---

### Task 3: 创建 manage/doc.go + manage/index/doc.go

**Files:**
- Create: `manage/doc.go`
- Create: `manage/index/doc.go`

- [ ] **Step 1: 创建 manage/doc.go**

```go
// Package manage 提供记忆管理器及其子组件。
//
// 本包实现记忆系统的核心管理器（FragmentMemoryManager、SummaryManager、VariableManager）
// 以及统一写入管理器（WriteManager）和搜索管理器（SearchManager）。
// 所有管理器通过 BaseMemoryManager 接口统一抽象，通过 BaseMemoryIndex 接口委托存储操作。
//
// 文件目录：
//
//	manage/
//	├── doc.go              # 包文档
//	├── index/              # 记忆管理器实现
//	│   ├── doc.go          # index 包文档
//	│   ├── base_manager.go # BaseMemoryManager 接口 + memoryManagerBase 嵌入结构体
//	│   └── fragment_manager.go # FragmentMemoryManager 碎片记忆管理器
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

- [ ] **Step 2: 创建 manage/index/doc.go**

```go
// Package index 提供记忆管理器接口和实现。
//
// 本包定义了 BaseMemoryManager 抽象接口和 memoryManagerBase 嵌入结构体，
// 提供记忆管理器的公共逻辑（参数校验、异常包装、加解密）。
// FragmentMemoryManager 是 BaseMemoryManager 的核心实现，
// 管理三种碎片记忆（用户画像、语义记忆、情景记忆）的全生命周期。
//
// 文件目录：
//
//	index/
//	├── doc.go               # 包文档
//	├── base_manager.go      # BaseMemoryManager 接口 + memoryManagerBase 嵌入结构体
//	└── fragment_manager.go  # FragmentMemoryManager 碎片记忆管理器
//
// 对应 Python 代码：
//
//	openjiuwen/core/memory/manage/index/
package index
```

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/memory/manage/doc.go internal/agentcore/memory/manage/index/doc.go && git commit -m "docs: 添加 manage 包和 index 包文档"
```

---

### Task 4: 创建 manage/index/base_manager.go — BaseMemoryManager 接口 + 嵌入结构体

**Files:**
- Create: `manage/index/base_manager.go`
- Create: `manage/index/base_manager_test.go`

- [ ] **Step 1: 编写 base_manager_test.go 失败测试**

```go
//go:build test

package index

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

func TestValidateParams_UserIDEmpty(t *testing.T) {
	base := &memoryManagerBase{
		memoryIndex: &fakeMemoryIndex{},
		memType:     "fragment",
	}
	err := base.validateParams("", "scope-1", exception.StatusMemoryAddMemoryExecutionError, "fragment")
	if err == nil {
		t.Fatal("期望返回 error，但得到 nil")
	}
	var baseErr *exception.BaseError
	if !exception.AsBaseError(err, &baseErr) {
		t.Fatalf("期望 *BaseError，得到 %T", err)
	}
	if baseErr.Status() != exception.StatusMemoryAddMemoryExecutionError {
		t.Errorf("Status = %v, want %v", baseErr.Status(), exception.StatusMemoryAddMemoryExecutionError)
	}
}

func TestValidateParams_ScopeIDEmpty(t *testing.T) {
	base := &memoryManagerBase{
		memoryIndex: &fakeMemoryIndex{},
		memType:     "fragment",
	}
	err := base.validateParams("user-1", "", exception.StatusMemoryAddMemoryExecutionError, "fragment")
	if err == nil {
		t.Fatal("期望返回 error，但得到 nil")
	}
}

func TestValidateParams_MemoryIndexNil(t *testing.T) {
	base := &memoryManagerBase{
		memoryIndex: nil,
		memType:     "fragment",
	}
	err := base.validateParams("user-1", "scope-1", exception.StatusMemoryAddMemoryExecutionError, "fragment")
	if err == nil {
		t.Fatal("期望返回 error，但得到 nil")
	}
}

func TestValidateParams_Success(t *testing.T) {
	base := &memoryManagerBase{
		memoryIndex: &fakeMemoryIndex{},
		memType:     "fragment",
	}
	err := base.validateParams("user-1", "scope-1", exception.StatusMemoryAddMemoryExecutionError, "fragment")
	if err != nil {
		t.Fatalf("不期望返回 error，但得到 %v", err)
	}
}

func TestWrapException_BaseErrorPassthrough(t *testing.T) {
	base := &memoryManagerBase{memType: "fragment"}
	originalErr := exception.BuildError(exception.StatusMemoryAddMemoryExecutionError,
		exception.WithParam("memory_type", "fragment"),
		exception.WithMsg("original error"),
	)
	err := base.wrapException(originalErr, exception.StatusMemoryUpdateMemoryExecutionError, "fragment")
	var baseErr *exception.BaseError
	if !exception.AsBaseError(err, &baseErr) {
		t.Fatalf("期望 *BaseError，得到 %T", err)
	}
	// BaseError 应原样透传，status 不变
	if baseErr.Status() != exception.StatusMemoryAddMemoryExecutionError {
		t.Errorf("Status = %v, want %v (应透传原始 BaseError)", baseErr.Status(), exception.StatusMemoryAddMemoryExecutionError)
	}
}

func TestWrapException_OtherErrorWrapped(t *testing.T) {
	base := &memoryManagerBase{memType: "fragment"}
	originalErr := exception.NewBaseError(exception.StatusMemoryAddMemoryExecutionError,
		exception.WithMsg("some error"),
	)
	err := base.wrapException(originalErr, exception.StatusMemoryUpdateMemoryExecutionError, "fragment")
	var baseErr *exception.BaseError
	if !exception.AsBaseError(err, &baseErr) {
		t.Fatalf("期望 *BaseError，得到 %T", err)
	}
}

func TestEncryptDecryptMemoryIfNeeded(t *testing.T) {
	// 空 key → passthrough
	result := encryptMemoryIfNeeded(nil, "hello")
	if result != "hello" {
		t.Errorf("空 key 时应返回原文，得到 %q", result)
	}
	result = decryptMemoryIfNeeded(nil, "hello")
	if result != "hello" {
		t.Errorf("空 key 时应返回原文，得到 %q", result)
	}
	// 空字符串 → passthrough
	result = encryptMemoryIfNeeded([]byte{1, 2, 3}, "")
	if result != "" {
		t.Errorf("空字符串时应返回原文，得到 %q", result)
	}
}

func TestMemoryTypeConstants(t *testing.T) {
	// 验证 FragmentMemoryTypes 列表
	expected := []string{"user_profile", "semantic_memory", "episodic_memory"}
	for i, typ := range FragmentMemoryTypes {
		if typ != expected[i] {
			t.Errorf("FragmentMemoryTypes[%d] = %q, want %q", i, typ, expected[i])
		}
	}
	// 验证 MemoryType 枚举值与字符串一致
	if mem_model.MemoryTypeUserProfile.String() != "user_profile" {
		t.Errorf("MemoryTypeUserProfile.String() = %q, want %q", mem_model.MemoryTypeUserProfile.String(), "user_profile")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway && go test -tags=test ./internal/agentcore/memory/manage/index/... -run TestValidateParams -v
```

- [ ] **Step 3: 实现 base_manager.go**

```go
package index

import (
	"errors"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/index"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/codec"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BaseMemoryManager 记忆管理器抽象接口。
//
// 定义了记忆管理器的 6 个核心操作：AddMemories、Update、Search、Get、Delete、DeleteByUserID。
// 所有记忆管理器实现（FragmentMemoryManager、SummaryManager、VariableManager）必须实现此接口。
//
// 对应 Python: openjiuwen/core/memory/manage/index/base_memory_manager.py (BaseMemoryManager)
type BaseMemoryManager interface {
	// AddMemories 批量添加记忆（含冲突检查和冗余消除）。
	// memories 的 key 为 mem_type 字符串（如 "user_profile"），value 为该类型的记忆列表。
	AddMemories(ctx context.Context, userID string, scopeID string,
		memories map[string][]*mem_model.FragmentMemoryUnit) ([]*mem_model.FragmentMemoryUnit, error)
	// Update 按 ID 更新记忆内容
	Update(ctx context.Context, userID string, scopeID string, memID string, newMemory string) (bool, error)
	// Search 语义搜索记忆
	Search(ctx context.Context, userID string, scopeID string, query string, topK int, memTypes []string) ([]*index.MemorySearchResult, error)
	// Get 按 ID 获取单条记忆
	Get(ctx context.Context, userID string, scopeID string, memID string) (*index.MemoryDoc, error)
	// Delete 按 ID 删除记忆
	Delete(ctx context.Context, userID string, scopeID string, memID string) (bool, error)
	// DeleteByUserID 删除用户+scope 下所有记忆
	DeleteByUserID(ctx context.Context, userID string, scopeID string) (bool, error)
}

// memoryManagerBase 记忆管理器公共基类。
//
// 嵌入此结构体后，实现类只需实现 BaseMemoryManager 接口即可。
// 提供 validateParams / wrapException / encryptMemoryIfNeeded / decryptMemoryIfNeeded 公共逻辑。
//
// 对应 Python: openjiuwen/core/memory/manage/index/base_memory_manager.py (BaseMemoryManager 非抽象方法)
type memoryManagerBase struct {
	// memoryIndex 记忆索引（KV + 向量库）
	memoryIndex index.BaseMemoryIndex
	// cryptoKey 加密密钥
	cryptoKey []byte
	// memType 类型标识（如 "fragment"）
	memType string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// FragmentMemoryTypeUserProfile 用户画像类型
	FragmentMemoryTypeUserProfile = "user_profile"
	// FragmentMemoryTypeSemanticMemory 语义记忆类型
	FragmentMemoryTypeSemanticMemory = "semantic_memory"
	// FragmentMemoryTypeEpisodicMemory 情景记忆类型
	FragmentMemoryTypeEpisodicMemory = "episodic_memory"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// FragmentMemoryTypes 碎片记忆类型列表
	FragmentMemoryTypes = []string{
		FragmentMemoryTypeUserProfile,
		FragmentMemoryTypeSemanticMemory,
		FragmentMemoryTypeEpisodicMemory,
	}

	// logComponent 日志组件常量
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// validateParams 校验必填参数，缺少时返回 *BaseError。
// 对齐 Python: BaseMemoryManager._validate_required_params
func (b *memoryManagerBase) validateParams(userID, scopeID string, statusCode exception.StatusCode, memType string) error {
	if userID == "" {
		return exception.BuildError(statusCode,
			exception.WithParam("memory_type", memType),
			exception.WithMsg("user_id is required"),
		)
	}
	if scopeID == "" {
		return exception.BuildError(statusCode,
			exception.WithParam("memory_type", memType),
			exception.WithMsg("scope_id is required"),
		)
	}
	if b.memoryIndex == nil {
		return exception.BuildError(statusCode,
			exception.WithParam("memory_type", memType),
			exception.WithMsg("memory_index is not initialized"),
		)
	}
	return nil
}

// wrapException 包装异常为统一 *BaseError。
// 如果原始错误已经是 *BaseError，原样返回；否则包装为新的 *BaseError。
// 对齐 Python: BaseMemoryManager._wrap_exception
func (b *memoryManagerBase) wrapException(e error, statusCode exception.StatusCode, memType string) error {
	var baseErr *exception.BaseError
	if errors.As(e, &baseErr) {
		return baseErr
	}
	return exception.BuildError(statusCode,
		exception.WithParam("memory_type", memType),
		exception.WithMsg(e.Error()),
		exception.WithCause(e),
	)
}

// encryptMemoryIfNeeded 如果 key 非空且 plaintext 非空，使用 AES 加密；否则返回原文。
// 加密失败时返回原文并记录 Warn 日志（对齐 Python 容错行为）。
// 对齐 Python: BaseMemoryManager.encrypt_memory_if_needed
func encryptMemoryIfNeeded(key []byte, plaintext string) string {
	if len(key) == 0 || plaintext == "" {
		return plaintext
	}
	c, err := codec.NewAesStorageCodec(key)
	if err != nil {
		logger.Warn(logComponent).Err(err).
			Str("event_type", "MEMORY_PROCESS").
			Msg("创建编解码器失败，返回原文")
		return plaintext
	}
	return c.Encode(plaintext)
}

// decryptMemoryIfNeeded 如果 key 非空且 ciphertext 非空，使用 AES 解密；否则返回原文。
// 解密失败时返回原文并记录 Warn 日志（对齐 Python 容错行为）。
// 对齐 Python: BaseMemoryManager.decrypt_memory_if_needed
func decryptMemoryIfNeeded(key []byte, ciphertext string) string {
	if len(key) == 0 || ciphertext == "" {
		return ciphertext
	}
	c, err := codec.NewAesStorageCodec(key)
	if err != nil {
		logger.Warn(logComponent).Err(err).
			Str("event_type", "MEMORY_PROCESS").
			Msg("创建编解码器失败，返回原文")
		return ciphertext
	}
	return c.Decode(ciphertext)
}
```

注意：需要在 `base_manager.go` 中添加 `import "context"` 到 import 块。

- [ ] **Step 4: 在 base_manager_test.go 中添加 fakeMemoryIndex**

在测试文件中添加 fakeMemoryIndex 实现，供后续所有测试使用：

```go
// fakeMemoryIndex 用于测试的 BaseMemoryIndex 假实现
type fakeMemoryIndex struct {
	index.MemoryIndexBase
	memories map[string]*index.MemoryDoc // key = userID/scopeID/memID
}

func newFakeMemoryIndex() *fakeMemoryIndex {
	return &fakeMemoryIndex{
		memories: make(map[string]*index.MemoryDoc),
	}
}

func (f *fakeMemoryIndex) key(userID, scopeID, memID string) string {
	return userID + "/" + scopeID + "/" + memID
}

func (f *fakeMemoryIndex) AddMemories(_ context.Context, userID string, scopeID string, memories []*index.MemoryDoc) error {
	for _, doc := range memories {
		f.memories[f.key(userID, scopeID, doc.ID)] = doc
	}
	return nil
}

func (f *fakeMemoryIndex) UpdateMemories(_ context.Context, userID string, scopeID string, memories []*index.MemoryDoc) error {
	for _, doc := range memories {
		f.memories[f.key(userID, scopeID, doc.ID)] = doc
	}
	return nil
}

func (f *fakeMemoryIndex) DeleteMemories(_ context.Context, userID string, scopeID string, ids []string) error {
	for _, id := range ids {
		delete(f.memories, f.key(userID, scopeID, id))
	}
	return nil
}

func (f *fakeMemoryIndex) DeleteByUser(_ context.Context, userID string) error {
	for k := range f.memories {
		if len(k) > len(userID) && k[:len(userID)] == userID {
			delete(f.memories, k)
		}
	}
	return nil
}

func (f *fakeMemoryIndex) DeleteByScope(_ context.Context, scopeID string) error {
	for k := range f.memories {
		if len(k) > len(scopeID) && k[len(k)-len(scopeID):] == scopeID {
			delete(f.memories, k)
		}
	}
	return nil
}

func (f *fakeMemoryIndex) DeleteByUserAndScope(_ context.Context, userID string, scopeID string) error {
	for k := range f.memories {
		if len(k) > len(userID) && k[:len(userID)] == userID {
			delete(f.memories, k)
		}
	}
	return nil
}

func (f *fakeMemoryIndex) Search(_ context.Context, userID string, scopeID string, query string, memTypes []string, topK int) ([]*index.MemorySearchResult, error) {
	var results []*index.MemorySearchResult
	for _, doc := range f.memories {
		results = append(results, &index.MemorySearchResult{Doc: doc, Score: 0.8})
	}
	if len(results) > topK {
		results = results[:topK]
	}
	return results, nil
}

func (f *fakeMemoryIndex) GetByID(_ context.Context, userID string, scopeID string, memID string) (*index.MemoryDoc, error) {
	doc, ok := f.memories[f.key(userID, scopeID, memID)]
	if !ok {
		return nil, nil
	}
	return doc, nil
}

func (f *fakeMemoryIndex) ListMemories(_ context.Context, userID string, scopeID string, offset int, limit int, memTypes []string) ([]*index.MemoryDoc, error) {
	var results []*index.MemoryDoc
	for _, doc := range f.memories {
		results = append(results, doc)
	}
	if offset < len(results) {
		results = results[offset:]
	}
	if limit < len(results) {
		results = results[:limit]
	}
	return results, nil
}
```

- [ ] **Step 5: 运行测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && go test -tags=test ./internal/agentcore/memory/manage/index/... -v
```

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/memory/manage/index/ && git commit -m "feat: 添加 BaseMemoryManager 接口 + memoryManagerBase 嵌入结构体"
```

---

### Task 5: 创建 manage/update/update_checker.go — MemUpdateChecker Stub

**Files:**
- Create: `manage/update/doc.go`
- Create: `manage/update/update_checker.go`
- Create: `manage/update/update_checker_test.go`

- [ ] **Step 1: 创建 update/doc.go**

```go
// Package update 提供记忆冲突检查器。
//
// 本包实现 MemUpdateChecker，用于检测新记忆与旧记忆之间的冗余和冲突。
// 当前为 stub 实现，直接返回所有新记忆为 ADD；7.8 实现时替换为 LLM 驱动的冲突检查。
//
// 文件目录：
//
//	update/
//	├── doc.go             # 包文档
//	└── update_checker.go  # MemUpdateChecker 记忆冲突检查器（⤵️ 回填: 7.8 stub）
//
// 对应 Python 代码：
//
//	openjiuwen/core/memory/manage/update/
package update
```

- [ ] **Step 2: 编写 update_checker_test.go 失败测试**

```go
//go:build test

package update

import (
	"testing"
)

func TestMemUpdateChecker_Check_StubReturnsAllAdd(t *testing.T) {
	checker := &MemUpdateChecker{}
	newMemories := map[string]string{
		"mem-1": "用户喜欢阅读",
		"mem-2": "用户是工程师",
	}
	oldMemories := map[string]string{
		"old-1": "用户喜欢读书",
	}
	result, err := checker.Check(newMemories, oldMemories)
	if err != nil {
		t.Fatalf("不期望返回 error，但得到 %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("期望返回 2 个 action item，得到 %d", len(result))
	}
	for _, item := range result {
		if item.Status != MemoryStatusAdd {
			t.Errorf("item.ID=%s, Status=%d, want MemoryStatusAdd", item.ID, item.Status)
		}
		if _, ok := newMemories[item.ID]; !ok {
			t.Errorf("item.ID=%s 不在 newMemories 中", item.ID)
		}
	}
}

func TestMemUpdateChecker_Check_EmptyNewMemories(t *testing.T) {
	checker := &MemUpdateChecker{}
	newMemories := map[string]string{}
	oldMemories := map[string]string{"old-1": "old content"}
	result, err := checker.Check(newMemories, oldMemories)
	if err != nil {
		t.Fatalf("不期望返回 error，但得到 %v", err)
	}
	if len(result) != 0 {
		t.Errorf("期望返回 0 个 action item，得到 %d", len(result))
	}
}

func TestCheckResultString(t *testing.T) {
	if CheckResultRedundant.String() != "redundant" {
		t.Errorf("CheckResultRedundant.String() = %q, want %q", CheckResultRedundant.String(), "redundant")
	}
	if CheckResultConflicting.String() != "conflicting" {
		t.Errorf("CheckResultConflicting.String() = %q, want %q", CheckResultConflicting.String(), "conflicting")
	}
	if CheckResultNone.String() != "none" {
		t.Errorf("CheckResultNone.String() = %q, want %q", CheckResultNone.String(), "none")
	}
}

func TestMemoryStatusString(t *testing.T) {
	if MemoryStatusAdd.String() != "add" {
		t.Errorf("MemoryStatusAdd.String() = %q, want %q", MemoryStatusAdd.String(), "add")
	}
	if MemoryStatusDelete.String() != "delete" {
		t.Errorf("MemoryStatusDelete.String() = %q, want %q", MemoryStatusDelete.String(), "delete")
	}
}
```

- [ ] **Step 3: 实现 update_checker.go**

```go
package update

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryActionItem 记忆动作项，表示一条记忆的 ADD 或 DELETE 动作。
//
// 对应 Python: openjiuwen/core/memory/manage/update/mem_update_checker.py (MemoryActionItem)
type MemoryActionItem struct {
	// ID 记忆 ID
	ID string
	// Content 记忆内容
	Content string
	// Status 动作状态
	Status MemoryStatus
}

// MemCheckItem 记忆检查结果项。
//
// 对应 Python: openjiuwen/core/memory/manage/update/mem_update_checker.py (MemCheckItem)
type MemCheckItem struct {
	// InfoID 记忆 ID
	InfoID string
	// InfoText 记忆内容
	InfoText string
	// Result 检查结果
	Result CheckResult
	// RelatedInfos 关联的旧记忆
	RelatedInfos map[string]string
}

// MemUpdateChecker 记忆冲突检查器。
//
// ⤵️ 回填: 7.8 — 当前 stub 实现，直接返回所有新记忆为 ADD。
// 7.8 实现时替换为 LLM 驱动的冲突检查（使用 PromptApplier + Model）。
//
// 对应 Python: openjiuwen/core/memory/manage/update/mem_update_checker.py (MemUpdateChecker)
type MemUpdateChecker struct{}

// ──────────────────────────── 枚举 ────────────────────────────

// CheckResult 记忆检查结果枚举。
//
// 对应 Python: openjiuwen/core/memory/manage/update/mem_update_checker.py (CheckResult)
type CheckResult int

const (
	// CheckResultRedundant 冗余
	CheckResultRedundant CheckResult = iota
	// CheckResultConflicting 冲突
	CheckResultConflicting
	// CheckResultNone 共存
	CheckResultNone
)

// MemoryStatus 记忆动作状态枚举。
//
// 对应 Python: openjiuwen/core/memory/manage/update/mem_update_checker.py (MemoryStatus)
type MemoryStatus int

const (
	// MemoryStatusAdd 添加
	MemoryStatusAdd MemoryStatus = iota
	// MemoryStatusDelete 删除
	MemoryStatusDelete
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// Check 检查新记忆与旧记忆的冗余/冲突。
//
// 当前 stub 实现：直接返回所有新记忆为 ADD（对齐 Python 中 base_chat_model=None 时
// MemUpdateChecker.Check() 的行为）。
//
// ⤵️ 回填: 7.8 — 7.8 实现时替换为 LLM 驱动的冲突检查。
func (c *MemUpdateChecker) Check(newMemories map[string]string, oldMemories map[string]string) ([]*MemoryActionItem, error) {
	result := make([]*MemoryActionItem, 0, len(newMemories))
	for id, content := range newMemories {
		result = append(result, &MemoryActionItem{
			ID:      id,
			Content: content,
			Status:  MemoryStatusAdd,
		})
	}
	return result, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// String 实现 fmt.Stringer 接口，对齐 Python CheckResult.value
func (cr CheckResult) String() string {
	switch cr {
	case CheckResultRedundant:
		return "redundant"
	case CheckResultConflicting:
		return "conflicting"
	case CheckResultNone:
		return "none"
	default:
		return "unknown"
	}
}

// String 实现 fmt.Stringer 接口，对齐 Python MemoryStatus.value
func (ms MemoryStatus) String() string {
	switch ms {
	case MemoryStatusAdd:
		return "add"
	case MemoryStatusDelete:
		return "delete"
	default:
		return "unknown"
	}
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && go test -tags=test ./internal/agentcore/memory/manage/update/... -v
```

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/memory/manage/update/ && git commit -m "feat: 添加 MemUpdateChecker stub（⤵️ 回填: 7.8）"
```

---

### Task 6: 创建 manage/index/fragment_manager.go — FragmentMemoryManager

**Files:**
- Create: `manage/index/fragment_manager.go`
- Create: `manage/index/fragment_manager_test.go`

- [ ] **Step 1: 编写 fragment_manager_test.go 失败测试**

```go
//go:build test

package index

import (
	"context"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/index"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/update"
)

func TestNewFragmentMemoryManager(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	mgr := NewFragmentMemoryManager(fakeIdx, nil)
	if mgr == nil {
		t.Fatal("NewFragmentMemoryManager 返回 nil")
	}
	if mgr.memType != "fragment" {
		t.Errorf("memType = %q, want %q", mgr.memType, "fragment")
	}
}

func TestFragmentMemoryManager_AddMemories_SingleAdd(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	mgr := NewFragmentMemoryManager(fakeIdx, nil)

	memories := map[string][]*mem_model.FragmentMemoryUnit{
		"user_profile": {
			{
				BaseMemoryUnit: mem_model.BaseMemoryUnit{
					MemType: mem_model.MemoryTypeUserProfile,
					MemID:   "mem-001",
				},
				Content:       "用户喜欢阅读",
				OperationType: mem_model.OperationTypeAdd,
			},
		},
	}
	result, err := mgr.AddMemories(context.Background(), "user-1", "scope-1", memories)
	if err != nil {
		t.Fatalf("AddMemories 返回 error: %v", err)
	}
	_ = result // stub checker 返回全部 ADD
}

func TestFragmentMemoryManager_Search(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	// 先添加一些记忆
	_ = fakeIdx.AddMemories(context.Background(), "user-1", "scope-1", []*index.MemoryDoc{
		{ID: "mem-001", Text: "用户喜欢阅读", Type: "user_profile", Timestamp: time.Now()},
	})

	mgr := NewFragmentMemoryManager(fakeIdx, nil)
	result, err := mgr.Search(context.Background(), "user-1", "scope-1", "阅读", 5, nil)
	if err != nil {
		t.Fatalf("Search 返回 error: %v", err)
	}
	if len(result) == 0 {
		t.Error("期望返回搜索结果，但得到空列表")
	}
}

func TestFragmentMemoryManager_Get(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	_ = fakeIdx.AddMemories(context.Background(), "user-1", "scope-1", []*index.MemoryDoc{
		{ID: "mem-001", Text: "用户喜欢阅读", Type: "user_profile", Timestamp: time.Now()},
	})

	mgr := NewFragmentMemoryManager(fakeIdx, nil)
	doc, err := mgr.Get(context.Background(), "user-1", "scope-1", "mem-001")
	if err != nil {
		t.Fatalf("Get 返回 error: %v", err)
	}
	if doc == nil {
		t.Fatal("期望返回 MemoryDoc，但得到 nil")
	}
	if doc.ID != "mem-001" {
		t.Errorf("doc.ID = %q, want %q", doc.ID, "mem-001")
	}
}

func TestFragmentMemoryManager_Get_NotFound(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	mgr := NewFragmentMemoryManager(fakeIdx, nil)
	doc, err := mgr.Get(context.Background(), "user-1", "scope-1", "nonexistent")
	if err != nil {
		t.Fatalf("Get 返回 error: %v", err)
	}
	if doc != nil {
		t.Errorf("期望返回 nil，但得到 %+v", doc)
	}
}

func TestFragmentMemoryManager_Update(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	_ = fakeIdx.AddMemories(context.Background(), "user-1", "scope-1", []*index.MemoryDoc{
		{ID: "mem-001", Text: "旧内容", Type: "user_profile", Timestamp: time.Now()},
	})

	mgr := NewFragmentMemoryManager(fakeIdx, nil)
	ok, err := mgr.Update(context.Background(), "user-1", "scope-1", "mem-001", "新内容")
	if err != nil {
		t.Fatalf("Update 返回 error: %v", err)
	}
	if !ok {
		t.Error("期望返回 true，但得到 false")
	}
}

func TestFragmentMemoryManager_Update_NotFound(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	mgr := NewFragmentMemoryManager(fakeIdx, nil)
	ok, err := mgr.Update(context.Background(), "user-1", "scope-1", "nonexistent", "新内容")
	if err != nil {
		t.Fatalf("Update 返回 error: %v", err)
	}
	if ok {
		t.Error("期望返回 false，但得到 true")
	}
}

func TestFragmentMemoryManager_Delete(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	_ = fakeIdx.AddMemories(context.Background(), "user-1", "scope-1", []*index.MemoryDoc{
		{ID: "mem-001", Text: "用户喜欢阅读", Type: "user_profile", Timestamp: time.Now()},
	})

	mgr := NewFragmentMemoryManager(fakeIdx, nil)
	ok, err := mgr.Delete(context.Background(), "user-1", "scope-1", "mem-001")
	if err != nil {
		t.Fatalf("Delete 返回 error: %v", err)
	}
	if !ok {
		t.Error("期望返回 true，但得到 false")
	}
}

func TestFragmentMemoryManager_Delete_NotFound(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	mgr := NewFragmentMemoryManager(fakeIdx, nil)
	ok, err := mgr.Delete(context.Background(), "user-1", "scope-1", "nonexistent")
	if err != nil {
		t.Fatalf("Delete 返回 error: %v", err)
	}
	if ok {
		t.Error("期望返回 false，但得到 true")
	}
}

func TestFragmentMemoryManager_DeleteByUserID(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	_ = fakeIdx.AddMemories(context.Background(), "user-1", "scope-1", []*index.MemoryDoc{
		{ID: "mem-001", Text: "用户喜欢阅读", Type: "user_profile", Timestamp: time.Now()},
	})

	mgr := NewFragmentMemoryManager(fakeIdx, nil)
	ok, err := mgr.DeleteByUserID(context.Background(), "user-1", "scope-1")
	if err != nil {
		t.Fatalf("DeleteByUserID 返回 error: %v", err)
	}
	if !ok {
		t.Error("期望返回 true，但得到 false")
	}
}

func TestFragmentMemoryManager_ValidateParams_UserIDEmpty(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	mgr := NewFragmentMemoryManager(fakeIdx, nil)
	_, err := mgr.AddMemories(context.Background(), "", "scope-1", nil)
	if err == nil {
		t.Fatal("期望返回 error，但得到 nil")
	}
}

func TestFragmentMemoryManager_ValidateParams_ScopeIDEmpty(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	mgr := NewFragmentMemoryManager(fakeIdx, nil)
	_, err := mgr.AddMemories(context.Background(), "user-1", "", nil)
	if err == nil {
		t.Fatal("期望返回 error，但得到 nil")
	}
}

func TestFragmentMemoryManager_AddMemories_WithFragmentUnits(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	mgr := NewFragmentMemoryManager(fakeIdx, nil)

	memories := map[string][]*mem_model.FragmentMemoryUnit{
		"user_profile": {
			{
				BaseMemoryUnit: mem_model.BaseMemoryUnit{
					MemType: mem_model.MemoryTypeUserProfile,
					MemID:   "mem-001",
				},
				Content:       "用户喜欢阅读",
				OperationType: mem_model.OperationTypeAdd,
			},
		},
	}
	result, err := mgr.AddMemories(context.Background(), "user-1", "scope-1", memories)
	if err != nil {
		t.Fatalf("AddMemories 返回 error: %v", err)
	}
	// stub checker 返回全部 ADD，所以应该有结果
	_ = result
}

func TestRemoveUpdateEntriesFromProcessResult(t *testing.T) {
	processResult := map[string]*mem_model.FragmentMemoryUnit{
		"mem-001": {OperationType: mem_model.OperationTypeUpdate},
		"mem-002": {OperationType: mem_model.OperationTypeAdd},
	}
	deleteSet := map[string]bool{"mem-001": true}
	removeUpdateEntriesFromProcessResult(deleteSet, processResult)
	if _, ok := processResult["mem-001"]; ok {
		t.Error("mem-001 应被移除")
	}
	if _, ok := processResult["mem-002"]; !ok {
		t.Error("mem-002 应保留")
	}
}

func TestAppendMemUnitListToDict(t *testing.T) {
	dict := map[string]*mem_model.FragmentMemoryUnit{
		"mem-001": {BaseMemoryUnit: mem_model.BaseMemoryUnit{MemID: "mem-001"}, Content: "旧内容"},
	}
	list := []*mem_model.FragmentMemoryUnit{
		{BaseMemoryUnit: mem_model.BaseMemoryUnit{MemID: "mem-001"}, Content: "新内容"},
		{BaseMemoryUnit: mem_model.BaseMemoryUnit{MemID: "mem-002"}, Content: "新增内容"},
	}
	appendMemUnitListToDict(dict, list)
	if dict["mem-001"].Content != "新内容" {
		t.Errorf("mem-001 应被覆盖，得到 %q", dict["mem-001"].Content)
	}
	if dict["mem-002"].Content != "新增内容" {
		t.Errorf("mem-002 应被添加，得到 %q", dict["mem-002"].Content)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway && go test -tags=test ./internal/agentcore/memory/manage/index/... -run TestNewFragment -v
```

- [ ] **Step 3: 实现 fragment_manager.go**

```go
package index

import (
	"context"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/index"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/update"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// FragmentMemoryManager 碎片记忆管理器，管理三种碎片记忆的全生命周期。
//
// 管理三种碎片记忆类型：user_profile、semantic_memory、episodic_memory。
// 一个实例同时服务三种类型（对齐 Python 中 managers 字典映射到同一实例的设计）。
// 所有实际存储操作委托给 BaseMemoryIndex，FragmentMemoryManager 只负责业务逻辑
//（冲突检查、操作分发、数据转换）。
//
// 对应 Python: openjiuwen/core/memory/manage/index/fragment_memory_manager.py (FragmentMemoryManager)
type FragmentMemoryManager struct {
	// memoryManagerBase 嵌入公共基类
	memoryManagerBase
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// UpdateCheckOldMemoryNum 添加新记忆时检索相关旧记忆的 top_k 数量
	UpdateCheckOldMemoryNum = 5
	// UpdateCheckOldMemoryRelevanceThreshold 旧记忆相关度阈值，高于此值才纳入冲突检查
	UpdateCheckOldMemoryRelevanceThreshold = 0.75
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewFragmentMemoryManager 创建碎片记忆管理器。
//
// 对齐 Python: FragmentMemoryManager.__init__(memory_index, crypto_key)
func NewFragmentMemoryManager(memoryIndex index.BaseMemoryIndex, cryptoKey []byte) *FragmentMemoryManager {
	return &FragmentMemoryManager{
		memoryManagerBase: memoryManagerBase{
			memoryIndex: memoryIndex,
			cryptoKey:   cryptoKey,
			memType:     "fragment",
		},
	}
}

// AddMemories 批量添加记忆（含冲突检查和冗余消除）。
//
// 流程：
//  1. 分离 ADD/UPDATE/DELETE 操作
//  2. 搜索相关旧记忆
//  3. MemUpdateChecker 冲突检查（⤵️ 回填: 7.8，当前 stub 直接返回全部 ADD）
//  4. 执行删除 + 添加
//
// 对齐 Python: FragmentMemoryManager.add_memories
func (m *FragmentMemoryManager) AddMemories(ctx context.Context, userID string, scopeID string,
	memories map[string][]*mem_model.FragmentMemoryUnit) ([]*mem_model.FragmentMemoryUnit, error) {

	if err := m.validateParams(userID, scopeID, m.memoryIndex,
		exception.StatusMemoryAddMemoryExecutionError, m.memType); err != nil {
		return nil, err
	}

	deleteSet := make(map[string]bool)
	processResult := make(map[string]*mem_model.FragmentMemoryUnit)

	// Step 1: 分离 ADD/UPDATE/DELETE 操作
	newMemUnits, err := m.getNewMemUnitsAndUpdateMemories(ctx, userID, scopeID, memories, deleteSet, processResult)
	if err != nil {
		return nil, err
	}
	newMemContent := make(map[string]string)
	for id, unit := range newMemUnits {
		newMemContent[id] = unit.Content
	}

	// 无新记忆且有删除 → 执行删除，返回结果
	if len(newMemUnits) == 0 {
		if len(deleteSet) > 0 {
			ids := mapKeys(deleteSet)
			if err := m.memoryIndex.DeleteMemories(ctx, userID, scopeID, ids); err != nil {
				return nil, m.wrapException(err, exception.StatusMemoryAddMemoryExecutionError, m.memType)
			}
			removeUpdateEntriesFromProcessResult(deleteSet, processResult)
		}
		return mapValues(processResult), nil
	}

	// Step 2: 搜索相关旧记忆
	oldMemories, err := m.getRelatedOldMemories(ctx, newMemContent, userID, scopeID)
	if err != nil {
		return nil, m.wrapException(err, exception.StatusMemoryAddMemoryExecutionError, m.memType)
	}

	// 无旧记忆且仅 1 条新记忆 → 直接写入，跳过冲突检查
	if len(oldMemories) == 0 && len(newMemContent) == 1 {
		if len(deleteSet) > 0 {
			ids := mapKeys(deleteSet)
			if err := m.memoryIndex.DeleteMemories(ctx, userID, scopeID, ids); err != nil {
				return nil, m.wrapException(err, exception.StatusMemoryAddMemoryExecutionError, m.memType)
			}
			removeUpdateEntriesFromProcessResult(deleteSet, processResult)
		}
		addList := mapValues(newMemUnits)
		addDocs := m.convertToMemoryDocs(addList)
		if err := m.memoryIndex.AddMemories(ctx, userID, scopeID, addDocs); err != nil {
			return nil, m.wrapException(err, exception.StatusMemoryAddMemoryExecutionError, m.memType)
		}
		appendMemUnitListToDict(processResult, addList)
		return mapValues(processResult), nil
	}

	// Step 3: MemUpdateChecker 冲突检查 ← ⤵️ 回填: 7.8
	checker := &update.MemUpdateChecker{}
	actionItems, err := checker.Check(newMemContent, oldMemories)
	if err != nil {
		return nil, m.wrapException(err, exception.StatusMemoryAddMemoryExecutionError, m.memType)
	}
	logger.Info(logComponent).
		Int("action_count", len(actionItems)).
		Str("event_type", "MEMORY_PROCESS").
		Msg("记忆冲突检查完成")

	// Step 4: 执行添加/删除操作
	var addUnitList []*mem_model.FragmentMemoryUnit
	for _, item := range actionItems {
		if item.Status == update.MemoryStatusAdd {
			if unit, ok := newMemUnits[item.ID]; ok {
				addUnitList = append(addUnitList, unit)
			}
		} else if item.Status == update.MemoryStatusDelete {
			deleteSet[item.ID] = true
		}
	}

	if len(deleteSet) > 0 {
		ids := mapKeys(deleteSet)
		if err := m.memoryIndex.DeleteMemories(ctx, userID, scopeID, ids); err != nil {
			return nil, m.wrapException(err, exception.StatusMemoryAddMemoryExecutionError, m.memType)
		}
		removeUpdateEntriesFromProcessResult(deleteSet, processResult)
	}
	if len(addUnitList) > 0 {
		addDocs := m.convertToMemoryDocs(addUnitList)
		if err := m.memoryIndex.AddMemories(ctx, userID, scopeID, addDocs); err != nil {
			return nil, m.wrapException(err, exception.StatusMemoryAddMemoryExecutionError, m.memType)
		}
		appendMemUnitListToDict(processResult, addUnitList)
	}

	return mapValues(processResult), nil
}

// Update 按 ID 更新记忆内容。
//
// 对齐 Python: FragmentMemoryManager.update
func (m *FragmentMemoryManager) Update(ctx context.Context, userID string, scopeID string, memID string, newMemory string) (bool, error) {
	if err := m.validateParams(userID, scopeID, m.memoryIndex,
		exception.StatusMemoryUpdateMemoryExecutionError, m.memType); err != nil {
		return false, err
	}

	oldDoc, err := m.memoryIndex.GetByID(ctx, userID, scopeID, memID)
	if err != nil {
		return false, m.wrapException(err, exception.StatusMemoryUpdateMemoryExecutionError, m.memType)
	}
	if oldDoc == nil {
		return false, nil
	}

	updatedDoc := &index.MemoryDoc{
		ID:        memID,
		Text:      newMemory,
		Type:      oldDoc.Type,
		Timestamp: time.Now(),
		Fields:    oldDoc.Fields,
	}
	if err := m.memoryIndex.UpdateMemories(ctx, userID, scopeID, []*index.MemoryDoc{updatedDoc}); err != nil {
		return false, m.wrapException(err, exception.StatusMemoryUpdateMemoryExecutionError, m.memType)
	}
	return true, nil
}

// Search 语义搜索记忆。
//
// 对齐 Python: FragmentMemoryManager.search
func (m *FragmentMemoryManager) Search(ctx context.Context, userID string, scopeID string, query string, topK int, memTypes []string) ([]*index.MemorySearchResult, error) {
	if err := m.validateParams(userID, scopeID, m.memoryIndex,
		exception.StatusMemoryGetMemoryExecutionError, m.memType); err != nil {
		return nil, err
	}

	if len(memTypes) == 0 {
		memTypes = FragmentMemoryTypes
	}
	results, err := m.memoryIndex.Search(ctx, userID, scopeID, query, memTypes, topK)
	if err != nil {
		return nil, m.wrapException(err, exception.StatusMemoryGetMemoryExecutionError, m.memType)
	}
	return results, nil
}

// Get 按 ID 获取单条记忆。
//
// 对齐 Python: FragmentMemoryManager.get
func (m *FragmentMemoryManager) Get(ctx context.Context, userID string, scopeID string, memID string) (*index.MemoryDoc, error) {
	if err := m.validateParams(userID, scopeID, m.memoryIndex,
		exception.StatusMemoryGetMemoryExecutionError, m.memType); err != nil {
		return nil, err
	}

	doc, err := m.memoryIndex.GetByID(ctx, userID, scopeID, memID)
	if err != nil {
		return nil, m.wrapException(err, exception.StatusMemoryGetMemoryExecutionError, m.memType)
	}
	return doc, nil
}

// Delete 按 ID 删除记忆。
//
// 对齐 Python: FragmentMemoryManager.delete
func (m *FragmentMemoryManager) Delete(ctx context.Context, userID string, scopeID string, memID string) (bool, error) {
	if err := m.validateParams(userID, scopeID, m.memoryIndex,
		exception.StatusMemoryDeleteMemoryExecutionError, m.memType); err != nil {
		return false, err
	}

	doc, err := m.memoryIndex.GetByID(ctx, userID, scopeID, memID)
	if err != nil {
		return false, m.wrapException(err, exception.StatusMemoryDeleteMemoryExecutionError, m.memType)
	}
	if doc == nil {
		logger.Error(logComponent).
			Str("memory_id", memID).
			Str("user_id", userID).
			Str("scope_id", scopeID).
			Str("event_type", "MEMORY_STORE").
			Msg("删除记忆失败，记忆不存在")
		return false, nil
	}
	if err := m.memoryIndex.DeleteMemories(ctx, userID, scopeID, []string{memID}); err != nil {
		return false, m.wrapException(err, exception.StatusMemoryDeleteMemoryExecutionError, m.memType)
	}
	return true, nil
}

// DeleteByUserID 删除用户+scope 下所有记忆。
//
// 对齐 Python: FragmentMemoryManager.delete_by_user_id
func (m *FragmentMemoryManager) DeleteByUserID(ctx context.Context, userID string, scopeID string) (bool, error) {
	if err := m.validateParams(userID, scopeID, m.memoryIndex,
		exception.StatusMemoryDeleteMemoryExecutionError, m.memType); err != nil {
		return false, err
	}

	if err := m.memoryIndex.DeleteByUserAndScope(ctx, userID, scopeID); err != nil {
		return false, m.wrapException(err, exception.StatusMemoryDeleteMemoryExecutionError, m.memType)
	}
	return true, nil
}

// ListFragmentMemories 分页列出碎片记忆。
//
// 对齐 Python: FragmentMemoryManager.list_fragment_memories
func (m *FragmentMemoryManager) ListFragmentMemories(ctx context.Context, userID string, scopeID string, offset int, batchSize int, memType string) ([]*index.MemoryDoc, error) {
	if err := m.validateParams(userID, scopeID, m.memoryIndex,
		exception.StatusMemoryGetMemoryExecutionError, m.memType); err != nil {
		return nil, err
	}

	var memTypes []string
	if memType != "" {
		memTypes = []string{memType}
	} else {
		memTypes = FragmentMemoryTypes
	}

	docs, err := m.memoryIndex.ListMemories(ctx, userID, scopeID, offset, batchSize, memTypes)
	if err != nil {
		return nil, m.wrapException(err, exception.StatusMemoryGetMemoryExecutionError, m.memType)
	}
	return docs, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getNewMemUnitsAndUpdateMemories 分离 ADD/UPDATE/DELETE 操作，执行 UPDATE 和 DELETE。
//
// 对齐 Python: FragmentMemoryManager._get_new_mem_units_and_update_memories
func (m *FragmentMemoryManager) getNewMemUnitsAndUpdateMemories(
	ctx context.Context,
	userID string, scopeID string,
	memories map[string][]*mem_model.FragmentMemoryUnit,
	deleteSet map[string]bool,
	processResult map[string]*mem_model.FragmentMemoryUnit,
) (map[string]*mem_model.FragmentMemoryUnit, error) {
	newMemUnits := make(map[string]*mem_model.FragmentMemoryUnit)
	updateMemUnits := make(map[string]*mem_model.FragmentMemoryUnit)

	for memType, memoryList := range memories {
		if !isFragmentMemoryType(memType) {
			continue
		}
		for _, unit := range memoryList {
			switch unit.OperationType {
			case mem_model.OperationTypeUpdate:
				if unit.Content != "" {
					if _, exists := updateMemUnits[unit.MemID]; exists {
						logger.Warn(logComponent).
							Str("memory_id", unit.MemID).
							Str("event_type", "MEMORY_STORE").
							Msg("更新记忆重复，旧值将被覆盖")
					}
					updateMemUnits[unit.MemID] = unit
				}
			case mem_model.OperationTypeDelete:
				deleteSet[unit.MemID] = true
				processResult[unit.MemID] = unit
			default: // OperationTypeAdd
				if unit.Content != "" {
					newMemUnits[unit.MemID] = unit
				}
			}
		}
	}

	// 执行 UPDATE 操作
	if len(updateMemUnits) > 0 {
		updateDocs := m.convertToMemoryDocs(mapValues(updateMemUnits))
		if err := m.memoryIndex.UpdateMemories(ctx, userID, scopeID, updateDocs); err != nil {
			return nil, m.wrapException(err, exception.StatusMemoryUpdateMemoryExecutionError, m.memType)
		}
		for id, unit := range updateMemUnits {
			processResult[id] = unit
		}
	}

	return newMemUnits, nil
}

// getRelatedOldMemories 搜索相关旧记忆用于冲突检查。
//
// 对齐 Python: FragmentMemoryManager._get_related_old_memories
func (m *FragmentMemoryManager) getRelatedOldMemories(
	ctx context.Context,
	newMemContent map[string]string,
	userID string, scopeID string,
) (map[string]string, error) {
	oldMemories := make(map[string]string)
	oldMemIDs := make(map[string]bool)

	for _, newMem := range newMemContent {
		searchResults, err := m.Search(ctx, userID, scopeID, newMem, UpdateCheckOldMemoryNum, nil)
		if err != nil {
			return nil, err
		}
		for _, result := range searchResults {
			if result.Doc != nil && result.Score > UpdateCheckOldMemoryRelevanceThreshold {
				if !oldMemIDs[result.Doc.ID] {
					oldMemories[result.Doc.ID] = result.Doc.Text
					oldMemIDs[result.Doc.ID] = true
				}
			}
		}
	}
	return oldMemories, nil
}

// convertToMemoryDoc 将 FragmentMemoryUnit 转换为 MemoryDoc。
//
// 对齐 Python: FragmentMemoryManager._convert_to_memory_doc
func (m *FragmentMemoryManager) convertToMemoryDoc(unit *mem_model.FragmentMemoryUnit) *index.MemoryDoc {
	ts := parseTimestamp(unit.Timestamp)
	return &index.MemoryDoc{
		ID:        unit.MemID,
		Text:      unit.Content,
		Type:      unit.MemType.String(),
		Timestamp: ts,
		Fields:    map[string]any{"source_id": unit.MessageMemID},
	}
}

// convertToMemoryDocs 批量转换 FragmentMemoryUnit 为 MemoryDoc。
func (m *FragmentMemoryManager) convertToMemoryDocs(units []*mem_model.FragmentMemoryUnit) []*index.MemoryDoc {
	docs := make([]*index.MemoryDoc, 0, len(units))
	for _, unit := range units {
		docs = append(docs, m.convertToMemoryDoc(unit))
	}
	return docs
}

// parseTimestamp 解析多种时间格式为 time.Time。
//
// 对齐 Python: FragmentMemoryManager._parse_timestamp
func parseTimestamp(ts string) time.Time {
	if ts == "" {
		return time.Now()
	}
	layouts := []string{
		"2006-01-02 15-04-05",
		"2006-01-02 15:04:05",
		time.RFC3339,
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, ts); err == nil {
			return t
		}
	}
	if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
		return t
	}
	return time.Now()
}

// isFragmentMemoryType 判断是否为碎片记忆类型。
func isFragmentMemoryType(memType string) bool {
	for _, t := range FragmentMemoryTypes {
		if t == memType {
			return true
		}
	}
	return false
}

// removeUpdateEntriesFromProcessResult 从结果中移除被删除的 UPDATE 条目。
//
// 对齐 Python: _remove_update_entries_from_process_result
func removeUpdateEntriesFromProcessResult(deleteSet map[string]bool, processResult map[string]*mem_model.FragmentMemoryUnit) {
	for memID := range deleteSet {
		if unit, ok := processResult[memID]; ok && unit.OperationType == mem_model.OperationTypeUpdate {
			delete(processResult, memID)
		}
	}
}

// appendMemUnitListToDict 将列表追加到字典（去重 + 覆盖）。
//
// 对齐 Python: _append_mem_unit_list_to_dict
func appendMemUnitListToDict(dict map[string]*mem_model.FragmentMemoryUnit, list []*mem_model.FragmentMemoryUnit) {
	for _, unit := range list {
		if _, exists := dict[unit.MemID]; exists {
			logger.Warn(logComponent).
				Str("memory_id", unit.MemID).
				Str("event_type", "MEMORY_STORE").
				Msg("记忆重复，旧值将被覆盖")
		}
		dict[unit.MemID] = unit
	}
}

// mapKeys 返回 map 的所有 key 为切片
func mapKeys[K comparable, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// mapValues 返回 map 的所有 value 为切片
func mapValues[K comparable, V any](m map[K]V) []V {
	values := make([]V, 0, len(m))
	for _, v := range m {
		values = append(values, v)
	}
	return values
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && go test -tags=test ./internal/agentcore/memory/manage/index/... -v
```

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/memory/manage/index/ && git commit -m "feat: 添加 FragmentMemoryManager 实现（7.6）"
```

---

### Task 7: 更新 IMPLEMENTATION_PLAN.md 状态

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 7.6 和 7.9 状态**

将 `IMPLEMENTATION_PLAN.md` 中：
- `7.6 | ☐` → `7.6 | ✅`
- `7.9 | ☐` → `7.9 | ✅`

- [ ] **Step 2: 提交**

```bash
git add IMPLEMENTATION_PLAN.md && git commit -m "docs: 更新 IMPLEMENTATION_PLAN 7.6+7.9 状态为完成"
```

---

### Task 8: 编译验证 + 全量测试

**Files:**
- 无新增

- [ ] **Step 1: 编译整个项目**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./...
```

- [ ] **Step 2: 运行 manage 包下所有测试**

```bash
cd /home/opensource/uapclaw-gateway && go test -tags=test ./internal/agentcore/memory/manage/... -v -cover
```

- [ ] **Step 3: 检查覆盖率**

确认覆盖率 ≥ 85%。如果不足，补充测试用例。

- [ ] **Step 4: 提交最终状态**

```bash
git add -A && git commit -m "test: 补充 7.6+7.9 测试覆盖率"
```
