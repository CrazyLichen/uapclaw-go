# 7.7 SummaryManager / VariableManager 实现设计

## 概述

实现记忆系统的摘要管理器（SummaryManager）和变量管理器（VariableManager），以及配套的 KV 前缀注册表（KvPrefixRegistry）。三者均属于 IMPLEMENTATION_PLAN.md 领域七第 7.7 步。

### 在 Agent 会话中的流程位置

```
用户消息 → DeepAgent → ReAct循环 → LLM 思考 → 工具调用（记忆工具）
                                        ↓
                              Memory 工具写入/读取
                                        ↓
                     WriteManager.add_memories / SearchManager.search（7.8）
                                        ↓
                    ┌─────────────┬──────────────┬──────────────┐
                    │ Fragment    │ Summary      │ Variable     │
                    │ Manager     │ Manager      │ Manager      │
                    │ (7.6 ✅)    │ (7.7 本次)  │ (7.7 本次)  │
                    └─────────────┴──────────────┴──────────────┘
                           ↓              ↓              ↓
                    BaseMemoryIndex   BaseMemoryIndex  BaseKVStore
                    (向量索引)         (向量索引)        (KV存储)
```

- **SummaryManager**：管理摘要型记忆（SummaryUnit），把对话摘要写入/读取向量索引，支持语义搜索。
- **VariableManager**：管理变量型记忆（VariableUnit），用 KV 存储保存用户偏好、会话变量等键值对，用 AES 加密。
- **KvPrefixRegistry**：全局前缀注册表，供 7.22-7.23 迁移器发现 KV 前缀。

## 已确认决策

| # | 决策 | 原因 |
|---|------|------|
| 1 | 严格按计划 7.7 只做 SummaryManager + VariableManager + KvPrefixRegistry | WriteManager/SearchManager/MemUpdateChecker 归 7.8 |
| 2 | VariableManager 不嵌入 memoryManagerBase，独立实现 | VariableManager 依赖 BaseKVStore 而非 BaseMemoryIndex，validateParams 中 memoryIndex==nil 检查会误报；Python 中 VariableManager 虽继承 BaseMemoryManager 但只用 _check_user_and_scope_id |
| 3 | VariableManager 的 update/delete/get/search 对齐 Python "Not implemented" | Python 中这些方法仅记日志后 pass |
| 4 | KvPrefixRegistry 随 7.7 一起实现 | VariableManager 构造时调用 register_current，代码量小（~50行），避免 ⤵️ 标记 |
| 5 | codec.encode() 返回 string，再转 []byte 写入 BaseKVStore | 对齐 Python: _codec.encode(var_value) → str → kv_store.set(key, str) |

## 依赖现状

| 依赖 | 状态 | 位置 |
|------|------|------|
| `BaseMemoryManager` 接口（MemoryUnit 签名） | ✅ 已就绪 | `manage/index/base_manager.go` |
| `MemoryUnit` 接口 + `SummaryUnit` / `VariableUnit` | ✅ 已就绪 | `manage/mem_model/memory_unit.go` |
| `BaseMemoryIndex` | ✅ 已就绪 | `foundation/store/index/base.go` |
| `BaseKVStore` + `InMemoryKVStore` | ✅ 已就绪 | `foundation/store/kv/base.go` + `in_memory.go` |
| `AesStorageCodec` | ✅ 已就绪 | `memory/codec/aes_storage_codec.go` |
| `KvPrefixRegistry` | ❌ 7.7 新增 | `memory/common/kv_prefix_registry.go` |

## 文件变更清单

| 操作 | 文件 | 说明 |
|------|------|------|
| **新增** | `memory/common/doc.go` | common 包文档 |
| **新增** | `memory/common/kv_prefix_registry.go` | KvPrefixRegistry 实现 |
| **新增** | `memory/common/kv_prefix_registry_test.go` | 测试 |
| **新增** | `manage/index/summary_manager.go` | SummaryManager 完整实现 |
| **新增** | `manage/index/summary_manager_test.go` | 测试 |
| **新增** | `manage/index/variable_manager.go` | VariableManager 完整实现 |
| **新增** | `manage/index/variable_manager_test.go` | 测试 |
| **修改** | `manage/index/doc.go` | 更新文件目录加入 summary_manager / variable_manager |
| **修改** | `manage/doc.go` | 更新文件目录加入 common/ |

### 无需修改的文件

- `base_manager.go` — 接口已就绪，AddMemories 签名已为 MemoryUnit
- `fragment_manager.go` — 已适配 MemoryUnit 接口，内部类型断言
- `fragment_manager_test.go` — 测试已通过
- `memory_unit.go` — SummaryUnit/VariableUnit 已定义

## 详细设计

### 1. KvPrefixRegistry（memory/common/kv_prefix_registry.go）

对齐 Python: `openjiuwen/core/memory/common/kv_prefix_registry.py`

```go
type KvPrefixRegistry struct {
    allPrefixes     map[string]bool  // 所有前缀（current + legacy）
    currentPrefixes map[string]bool  // 当前使用的前缀
}

var KVPrefixRegistry = NewKvPrefixRegistry()  // 全局实例

func NewKvPrefixRegistry() *KvPrefixRegistry
func (r *KvPrefixRegistry) RegisterCurrent(prefix string) error    // 空/纯空白前缀返回 error
func (r *KvPrefixRegistry) RegisterLegacy(prefix string) error     // 同上
func (r *KvPrefixRegistry) GetAllPrefixes() []string               // 返回所有前缀的副本
func (r *KvPrefixRegistry) Unregister(prefix string)
```

### 2. SummaryManager（manage/index/summary_manager.go）

对齐 Python: `openjiuwen/core/memory/manage/index/summary_manager.py`

**结构体**：

```go
type SummaryManager struct {
    memoryManagerBase  // 嵌入基类（依赖 BaseMemoryIndex）
}
```

**方法映射**：

| Python 方法 | Go 方法 | 说明 |
|------------|--------|------|
| `__init__(memory_index, crypto_key)` | `NewSummaryManager(memoryIndex, cryptoKey)` | memType = "summary" |
| `add_memories` | `AddMemories` | 转换 SummaryUnit→MemoryDoc，写入索引；空结果记 Warn 日志返回空切片 |
| `update` | `Update` | 按 ID 获取旧文档，替换 text 后 UpdateMemories |
| `delete` | `Delete` | 按 ID 删除 |
| `delete_by_user_id` | `DeleteByUserID` | 按 user+scope 删除 |
| `get` | `Get` | 返回 MemoryDoc |
| `search` | `Search` | 语义搜索，按 mem_type="summary" 过滤 |
| `list_user_summary` | `ListUserSummary` | 分页列出，按 timestamp 降序 |
| `_convert_to_memory_docs` | `convertToMemoryDocs` | 内部：SummaryUnit → MemoryDoc（text=summary, fields={source_id}） |
| `_parse_timestamp` | 复用 `parseTimestamp` | 已在 fragment_manager.go 中定义 |

**关键逻辑**：
- AddMemories 中从 memories map 过滤 mem_type=="summary"，对每个 unit 做类型断言为 *SummaryUnit
- AddMemories 返回 `memories[self.mem_type]`（对齐 Python），但 Go 接口返回 `[]MemoryUnit`，需从过滤出的 SummaryUnit 列表转换
- search 的 memTypes 参数硬编码为 `[m.memType]`（对齐 Python: `mem_types=[self.mem_type]`）
- list_user_summary 返回 MemoryDoc 切片，按 Timestamp 降序排列

### 3. VariableManager（manage/index/variable_manager.go）

对齐 Python: `openjiuwen/core/memory/manage/index/variable_manager.py`

**结构体**：

```go
type VariableManager struct {
    kvStore   kv.BaseKVStore   // KV 存储（不嵌入 memoryManagerBase）
    cryptoKey []byte           // 加密密钥
    codec     *codec.AesStorageCodec  // AES 编解码器
    memType   string           // "variable"
}
```

**方法映射**：

| Python 方法 | Go 方法 | 说明 |
|------------|--------|------|
| `__init__(kv_store, crypto_key)` | `NewVariableManager(kvStore, cryptoKey)` | 注册 kv_prefix + 创建 codec |
| `add_memories` | `AddMemories` | 遍历 VariableUnit，_makeVariablePairs → kvStore.Set |
| `update` | `Update` | ⚠️ Not implemented — 记 Warn 日志，返回 false, nil |
| `delete` | `Delete` | ⚠️ Not implemented — 记 Error 日志，返回 false, nil |
| `delete_by_user_id` | `DeleteByUserID` | 按 user_var/session_var 前缀 DeleteByPrefix |
| `get` | `Get` | ⚠️ Not implemented — 记 Warn 日志，返回 nil, nil |
| `search` | `Search` | ⚠️ Not implemented — 记 Warn 日志，返回 nil, nil |
| `update_user_variable` | `UpdateUserVariable` | 查询存在性后更新 |
| `delete_user_variable` | `DeleteUserVariable` | 按 key 删除 |
| `query_variable` | `QueryVariable` | 按 name 查值或按前缀查全部；返回 map[string]string |
| `_make_variable_pairs` | `makeVariablePairs` | 构造 KV key/value |
| `_check_user_and_scope_id` | `checkUserAndScopeID` | 校验辅助，空时记 Error 日志 |
| `_check_exist` | `checkExist` | 检查变量是否存在 |

**KV key 格式**：

```
user_var/{userID}/{scopeID}/{varName}        ← 用户级变量
session_var/{userID}/{scopeID}/{sessionID}/{varName}  ← 会话级变量
```

**编码流程**（对齐 Python）：

```
写入: varMem → codec.Encode(varMem) → string → []byte → kvStore.Set(key, []byte)
读取: kvStore.Get(key) → []byte → string → codec.Decode(string) → 原文
```

**Not implemented 方法的行为**：
- Python: `memory_logger.warning("Not implemented method ...")` + `pass`
- Go: `logger.Warn(...)` + 返回零值（Update→false,nil; Delete→false,nil; Get→nil,nil; Search→nil,nil）

### 回填关系

| 位置 | 回填内容 | 目标 |
|------|---------|------|
| 7.6 FragmentMemoryManager ✅ | `⤵️ 回填: 7.8` — MemUpdateChecker stub | 7.8 不受 7.7 影响 |
| 7.7 SummaryManager / VariableManager | 无 ⤵️ 标记 | — |
| 7.8 WriteManager | 依赖 7.7 的 SummaryManager/VariableManager 实例加入 managers 字典 | 7.8 |
| 7.8 SearchManager | 依赖 SummaryManager.ListUserSummary / VariableManager.QueryVariable | 7.8 |
| 7.8 MemUpdateChecker 回填 | 依赖 PromptApplier（7.26 Memory Prompts） | 7.8 |

### 日志同步规则

- SummaryManager / VariableManager 中所有 Python `memory_logger.*` 调用，在 Go 中使用 `logger.ComponentAgentCore` 组件常量
- Python `memory_logger.warning(...)` → Go `logger.Warn(logComponent)`
- Python `memory_logger.error(...)` → Go `logger.Error(logComponent)`
- Python `memory_logger.debug(...)` → Go `logger.Debug(logComponent)`
- 日志字段：`event_type`、`memory_type`、`user_id`、`scope_id`、`memory_id` 等结构化字段一一对齐

### 测试要求

- 每个新增文件需配备 `*_test.go`
- SummaryManager 测试：使用 fakeMemoryIndex（复用 fragment_manager_test.go 中的）
- VariableManager 测试：使用 InMemoryKVStore
- KvPrefixRegistry 测试：注册/获取/注销/空值校验
- 覆盖率目标 ≥ 85%
