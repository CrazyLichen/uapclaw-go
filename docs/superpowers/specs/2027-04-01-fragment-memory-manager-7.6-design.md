# 7.6+7.9 FragmentMemoryManager + 数据模型 设计文档

## 概述

本文档描述 FragmentMemoryManager（7.6）和记忆数据模型（7.9）的合并实现方案。FragmentMemoryManager 是 agent 长期记忆系统的核心管理器，负责三种碎片记忆（用户画像、语义记忆、情景记忆）的全生命周期管理。7.9 的数据模型（MemoryType/OperationType/FragmentMemoryUnit 等）是 FragmentMemoryManager 的前置依赖，因此合并实现。7.8 的 MemUpdateChecker 先用 stub，后续回填。

## 流程位置

FragmentMemoryManager 在 agent 会话中的位置：

```
用户消息 → LongTermMemory.add_messages()
  │
  ├─ 1. MessageManager.add() → 存原始消息到 SQL
  ├─ 2. Generator.gen_all_memory() → LLM 提取记忆（按 mem_type 分组）
  │
  └─ 3. WriteManager.add_memories() → 分发到各子管理器
        │
        ├─ FragmentMemoryManager.add_memories()  ← ★ 7.6 在这里
        ├─ SummaryManager.add_memories()         ← 7.7
        └─ VariableManager.add_memories()        ← 7.7
```

## 目录结构（对齐 Python）

Python `manage/` 目录结构映射到 Go：

```
Python manage/                          →  Go manage/
├── index/                              →  ├── index/
│   ├── base_memory_manager.py          →  │   ├── base_manager.go
│   ├── fragment_memory_manager.py      →  │   ├── fragment_manager.go
│   ├── summary_manager.py              →  │   └── (7.7 占位)
│   ├── variable_manager.py             →  │   └── (7.7 占位)
│   └── write_manager.py                →  │   └── (7.8 占位)
├── mem_model/                          →  ├── mem_model/  (原 model/ 重命名)
│   ├── memory_unit.py                  →  │   ├── memory_unit.go (新增)
│   ├── data_id_manager.py              →  │   ├── data_id_manager.go (已有)
│   ├── db_model.py                     →  │   ├── db_model.go (已有)
│   ├── sql_db_store.py                 →  │   ├── sql_db_store.go (已有)
│   ├── sql_message_store.py            →  │   ├── sql_message_store.go (已有)
│   ├── message_manager.py              →  │   ├── message_manager.go (已有)
│   ├── scope_user_mapping_manager.py   →  │   ├── scope_user_mapping_manager.go (已有)
│   ├── semantic_store.py               →  │   └── (后续)
│   └── user_mem_store.py               →  │   └── (后续)
├── update/                             →  ├── update/
│   └── mem_update_checker.py           →  │   └── update_checker.go (stub)
└── search/                             →  └── search/
    └── search_manager.py               →      └── (7.8 占位)
```

### 重命名说明

`manage/model/` → `manage/mem_model/`：对齐 Python 目录名 `mem_model/`。当前无外部引用，重命名仅影响包内部 import 路径。

## 数据模型（7.9 → `mem_model/memory_unit.go`）

对齐 Python `memory_unit.py`：

### MemoryType 枚举

```go
// MemoryType 记忆类型枚举
// 对应 Python: MemoryType
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
```

### OperationType 枚举

```go
// OperationType 操作类型枚举
// 对应 Python: OperationType
type OperationType int

const (
    // OperationTypeAdd 新增
    OperationTypeAdd OperationType = iota
    // OperationTypeUpdate 更新
    OperationTypeUpdate
    // OperationTypeDelete 删除
    OperationTypeDelete
)
```

### BaseMemoryUnit 结构体

```go
// BaseMemoryUnit 记忆数据项基类
// 对应 Python: BaseMemoryUnit
type BaseMemoryUnit struct {
    // MemType 记忆类型
    MemType MemoryType
    // MemID 记忆唯一标识
    MemID string
}
```

### FragmentMemoryUnit 结构体

```go
// FragmentMemoryUnit 碎片记忆数据项
// 对应 Python: FragmentMemoryUnit
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
```

### VariableUnit 结构体

```go
// VariableUnit 变量记忆数据项
// 对应 Python: VariableUnit
type VariableUnit struct {
    // BaseMemoryUnit 嵌入基类
    BaseMemoryUnit
    // VariableName 变量名
    VariableName string
    // VariableMem 变量值
    VariableMem string
}
```

### SummaryUnit 结构体

```go
// SummaryUnit 摘要记忆数据项
// 对应 Python: SummaryUnit
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
```

## BaseMemoryManager 接口 + 嵌入结构体（`index/base_manager.go`）

### 接口定义

```go
// BaseMemoryManager 记忆管理器抽象接口
// 对应 Python: BaseMemoryManager
type BaseMemoryManager interface {
    // AddMemories 批量添加记忆（含冲突检查和冗余消除）
    AddMemories(ctx context.Context, userID string, scopeID string,
        memories map[string][]*memmodel.BaseMemoryUnit, llm Model, opts ...Option) ([]*memmodel.FragmentMemoryUnit, error)
    // Update 按 ID 更新记忆内容
    Update(ctx context.Context, userID string, scopeID string, memID string, newMemory string, opts ...Option) (bool, error)
    // Search 语义搜索记忆
    Search(ctx context.Context, userID string, scopeID string, query string, topK int, opts ...Option) ([]*index.MemorySearchResult, error)
    // Get 按 ID 获取单条记忆
    Get(ctx context.Context, userID string, scopeID string, memID string) (*index.MemoryDoc, error)
    // Delete 按 ID 删除记忆
    Delete(ctx context.Context, userID string, scopeID string, memID string, opts ...Option) (bool, error)
    // DeleteByUserID 删除用户+scope 下所有记忆
    DeleteByUserID(ctx context.Context, userID string, scopeID string, opts ...Option) (bool, error)
}
```

### 嵌入结构体

```go
// memoryManagerBase 记忆管理器公共基类
// 嵌入此结构体后，实现类只需实现 BaseMemoryManager 接口即可。
// 提供 validateParams / wrapException / encryptMemoryIfNeeded / decryptMemoryIfNeeded 公共逻辑。
type memoryManagerBase struct {
    // memoryIndex 记忆索引（KV + 向量库）
    memoryIndex index.BaseMemoryIndex
    // cryptoKey 加密密钥
    cryptoKey []byte
    // memType 类型标识（如 "fragment"）
    memType string
}
```

**公共方法**：

| 方法 | 签名 | 对齐 Python |
|------|------|------------|
| `validateParams` | `(userID, scopeID string, statusCode StatusCode, memType string) error` | `_validate_required_params` |
| `wrapException` | `(e error, statusCode StatusCode, memType string)` | `_wrap_exception` |
| `encryptMemoryIfNeeded` | `(key []byte, plaintext string) string` | `encrypt_memory_if_needed` (静态) |
| `decryptMemoryIfNeeded` | `(key []byte, ciphertext string) string` | `decrypt_memory_if_needed` (静态) |

## FragmentMemoryManager（`index/fragment_manager.go`）

### 结构体

```go
// FragmentMemoryManager 碎片记忆管理器
// 对应 Python: FragmentMemoryManager
//
// 管理三种碎片记忆类型：user_profile、semantic_memory、episodic_memory。
// 一个实例同时服务三种类型（对齐 Python 中 managers 字典映射到同一实例的设计）。
type FragmentMemoryManager struct {
    // memoryManagerBase 嵌入公共基类
    memoryManagerBase
}
```

### 常量

```go
const (
    // FragmentMemoryTypeUserProfile 用户画像类型
    FragmentMemoryTypeUserProfile = "user_profile"
    // FragmentMemoryTypeSemanticMemory 语义记忆类型
    FragmentMemoryTypeSemanticMemory = "semantic_memory"
    // FragmentMemoryTypeEpisodicMemory 情景记忆类型
    FragmentMemoryTypeEpisodicMemory = "episodic_memory"

    // UpdateCheckOldMemoryNum 添加新记忆时检索相关旧记忆的 top_k 数量
    UpdateCheckOldMemoryNum = 5
    // UpdateCheckOldMemoryRelevanceThreshold 旧记忆相关度阈值
    UpdateCheckOldMemoryRelevanceThreshold = 0.75
)

// FragmentMemoryTypes 碎片记忆类型列表
var FragmentMemoryTypes = []string{
    FragmentMemoryTypeUserProfile,
    FragmentMemoryTypeSemanticMemory,
    FragmentMemoryTypeEpisodicMemory,
}
```

### 核心方法 AddMemories 流程

对齐 Python `FragmentMemoryManager.add_memories`：

```
AddMemories(ctx, userID, scopeID, memories, llm)
  │
  ├─ Step 1: validateParams(userID, scopeID, memoryIndex, StatusMemoryAddMemoryExecutionError, "fragment")
  │
  ├─ Step 2: getNewMemUnitsAndUpdateMemories()
  │   ├─ 遍历 memories，按 operation_type 分为三类：
  │   │   ├─ UPDATE → memoryIndex.UpdateMemories()
  │   │   ├─ DELETE → 加入 deleteSet
  │   │   └─ ADD (默认) → 加入 newMemUnits
  │   └─ 返回 newMemUnits
  │
  ├─ [无新记忆且有删除] → memoryIndex.DeleteMemories(), 返回结果
  │
  ├─ Step 3: getRelatedOldMemories()
  │   └─ 对每条新记忆调用 self.Search()，top_k=5, score>0.75
  │
  ├─ [无旧记忆且仅 1 条新记忆] → 直接写入，跳过冲突检查
  │   ├─ memoryIndex.AddMemories()
  │   └─ 返回结果
  │
  ├─ Step 4: MemUpdateChecker.Check() ← ⤵️ 回填: 7.8
  │   └─ 当前 stub：直接返回所有新记忆为 ADD
  │
  └─ Step 5: 执行删除 + 添加
      ├─ memoryIndex.DeleteMemories()
      └─ memoryIndex.AddMemories()
```

### 其他方法

| 方法 | 返回值 | 对齐 Python |
|------|--------|------------|
| `Update` | `(bool, error)` | `update` |
| `Search` | `([]*index.MemorySearchResult, error)` | `search` |
| `Get` | `(*index.MemoryDoc, error)` | `get` |
| `Delete` | `(bool, error)` | `delete` |
| `DeleteByUserID` | `(bool, error)` | `delete_by_user_id` |
| `ListFragmentMemories` | `([]*index.MemoryDoc, error)` | `list_fragment_memories` |

### 内部方法

| 方法 | 对齐 Python |
|------|------------|
| `parseTimestamp` | `_parse_timestamp` (静态) |
| `processConflictInfo` | `_process_conflict_info` (静态) |
| `convertToMemoryDoc` | `_convert_to_memory_doc` |
| `docToDict` | `_doc_to_dict` |
| `getNewMemUnitsAndUpdateMemories` | `_get_new_mem_units_and_update_memories` |
| `getRelatedOldMemories` | `_get_related_old_memories` |
| `addMemoryToStore` | `_add_memory_to_store` |

## MemUpdateChecker Stub（`update/update_checker.go`）

### 数据模型

```go
// CheckResult 记忆检查结果枚举
// 对应 Python: CheckResult
type CheckResult int

const (
    // CheckResultRedundant 冗余
    CheckResultRedundant CheckResult = iota
    // CheckResultConflicting 冲突
    CheckResultConflicting
    // CheckResultNone 共存
    CheckResultNone
)

// MemoryStatus 记忆动作状态枚举
// 对应 Python: MemoryStatus
type MemoryStatus int

const (
    // MemoryStatusAdd 添加
    MemoryStatusAdd MemoryStatus = iota
    // MemoryStatusDelete 删除
    MemoryStatusDelete
)

// MemoryActionItem 记忆动作项
// 对应 Python: MemoryActionItem
type MemoryActionItem struct {
    // ID 记忆 ID
    ID string
    // Content 记忆内容
    Content string
    // Status 动作状态
    Status MemoryStatus
}

// MemCheckItem 记忆检查结果项
// 对应 Python: MemCheckItem
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
```

### Stub 实现

```go
// MemUpdateChecker 记忆冲突检查器
// ⤵️ 回填: 7.8 — 当前 stub 实现，直接返回所有新记忆为 ADD
// 对齐 Python 中 base_chat_model=None 时 MemUpdateChecker.Check() 的行为
type MemUpdateChecker struct{}

// Check 检查新记忆与旧记忆的冗余/冲突
// 当前 stub 实现：直接返回所有新记忆为 ADD（对齐 Python 中 base_chat_model=None 的行为）
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
```

## 包级辅助函数

对齐 Python 中 `fragment_memory_manager.py` 的模块级函数：

| Go 函数 | Python 函数 | 说明 |
|---------|------------|------|
| `removeUpdateEntriesFromProcessResult` | `_remove_update_entries_from_process_result` | 从结果中移除被删除的 UPDATE 条目 |
| `appendMemUnitListToDict` | `_append_mem_unit_list_to_dict` | 将列表追加到字典（去重 + 覆盖） |

## 依赖关系

```
FragmentMemoryManager
  ├── depends on → BaseMemoryManager (接口 + memoryManagerBase 嵌入)
  ├── depends on → index.BaseMemoryIndex (接口，已实现 SimpleMemoryIndex) ✅
  ├── depends on → mem_model.BaseMemoryUnit/FragmentMemoryUnit (7.9 数据模型) ← 本次实现
  ├── depends on → MemUpdateChecker (冲突检查器) ← ⤵️ 回填: 7.8 stub
  └── depends on → exception.StatusCode (已有) ✅
```

## 回填标记清单

| 位置 | 标记 | 说明 |
|------|------|------|
| `update/update_checker.go` | `⤵️ 回填: 7.8` | MemUpdateChecker stub，7.8 实现时替换为 LLM 驱动的冲突检查 |
| `index/fragment_manager.go` AddMemories | `⤵️ 回填: 7.8` | Check() 调用点，7.8 实现后传入真实 LLM |
| `lite/coding_memory_tool_ops.go` | `⤵️ 回填: 7.8` | 已有的 TODO，7.8 实现时替换 runChecker |

## IMPLEMENTATION_PLAN 状态更新

| 步骤 | 当前 → 新状态 | 说明 |
|------|-------------|------|
| 7.6 | ☐ → ✅ | FragmentMemoryManager 完整实现 |
| 7.9 | ☐ → ✅ | 数据模型（MemoryType/OperationType/FragmentMemoryUnit 等）在 mem_model/memory_unit.go 中实现 |
| 7.8 | ☐ → ☐ | MemUpdateChecker stub 占位，标记 ⤵️ 回填，后续实现时替换 |

## 文件清单

| 新增文件 | 对齐 Python | 说明 |
|---------|------------|------|
| `manage/index/doc.go` | `manage/index/__init__.py` | 包文档 |
| `manage/index/base_manager.go` | `manage/index/base_memory_manager.py` | BaseMemoryManager 接口 + memoryManagerBase 嵌入结构体 |
| `manage/index/fragment_manager.go` | `manage/index/fragment_memory_manager.py` | FragmentMemoryManager 实现 |
| `manage/mem_model/memory_unit.go` | `manage/mem_model/memory_unit.py` | MemoryType/OperationType/FragmentMemoryUnit 等数据模型 |
| `manage/update/doc.go` | `manage/update/__init__.py` | 包文档 |
| `manage/update/update_checker.go` | `manage/update/mem_update_checker.py` | MemUpdateChecker stub |

| 重命名 | 说明 |
|--------|------|
| `manage/model/` → `manage/mem_model/` | 对齐 Python 目录名 |

| 修改文件 | 说明 |
|---------|------|
| `manage/doc.go` | 更新包文档，添加 index/update 子包说明 |
| `manage/mem_model/doc.go` | 更新文件目录，添加 memory_unit.go |
| `manage/mem_model/*.go` | package 声明从 `model` 改为 `mem_model`，更新内部 import |

## 测试策略

- `memory_unit_test.go`：枚举值 String() 测试、FragmentMemoryUnit 字段序列化测试
- `base_manager_test.go`：validateParams 参数校验测试、wrapException 异常包装测试、encrypt/decrypt 测试
- `fragment_manager_test.go`：AddMemories 完整流程测试（ADD/UPDATE/DELETE 分离、冲突检查 stub、直接写入快路径）、Search/Get/Update/Delete 测试、ListFragmentMemories 测试
- `update_checker_test.go`：stub Check 方法测试（验证返回全部 ADD）
- 所有测试使用 fake 实现 BaseMemoryIndex 接口（httptest 风格），不依赖外部存储
