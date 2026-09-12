# 7.10 Memory Index 逻辑补齐设计

> 日期：2025-07-15
> 状态：已确认

## 背景

对 Python `openjiuwen/core/memory/manage/index/` 与 Go `internal/agentcore/memory/manage/index/` 进行逐方法精细比对，发现 9 项需要对齐的差异。

## 修复清单

### H1：FragmentManager 类型断言 warn 日志 `memory_type` 字段错误

**文件**：`fragment_manager.go` `AddMemories` 方法

**问题**：warn 日志 `Str("memory_type", m.memType)` 永远输出 `"fragment"`，Python 输出的是 memories map 的实际 key（如 `"user_profile"`）。

**修复**：改为 `Str("memory_type", memType)`，使用外层循环变量。

### H2：FragmentManager `ListFragmentMemories` error 日志缺少关键字段

**文件**：`fragment_manager.go` `ListFragmentMemories` 方法

**问题**：error 日志缺少 `user_id`、`scope_id`、`event_type` 字段。

**修复**：补齐三个字段：
```go
logger.Error(logComponent).
    Str("mem_type", memType).
    Str("memory_type", m.memType).
    Str("user_id", userID).
    Str("scope_id", scopeID).
    Str("event_type", "MEMORY_STORE").
    Msg("非法碎片记忆类型")
```

### H4：SummaryManager / FragmentManager `AddMemories` 返回值与 Python 不一致

**文件**：`summary_manager.go`、`fragment_manager.go` 的 `AddMemories` 方法

**问题**：Python 返回 `memories[self.mem_type]`（原始列表），Go 只返回过滤后的结果列表。

**修复**：

- **SummaryManager**：在过滤循环前保存 `memories[m.memType]` 原始列表引用，成功写入后返回该原始列表
- **FragmentManager**：在类型断言循环前保存 `memories` 中所有属于 fragment 类型的原始单元列表，成功处理后返回该原始列表（而非仅返回冲突检测后的结果列表）

### H5：VariableManager `Get` warn 日志缺少 `event_type`

**文件**：`variable_manager.go` `Get` 方法

**问题**：warn 日志缺少 `event_type` 字段，Python 有。

**修复**：补齐 `Str("event_type", "MEMORY_STORE")`。

### M1：FragmentManager `AddMemories` 批量添加前补 debug 日志

**文件**：`fragment_manager.go` `AddMemories` 方法

**问题**：Python `_add_memory_to_store` 有逐条 debug 日志（`"Add memory"` + `memory_type` + `event_type` + `user_id` + `scope_id`），Go 缺失。

**修复**：在批量调用 `memoryIndex.AddMemories` 前加 debug 日志：
```go
logger.Debug(logComponent).
    Str("memory_type", memType).
    Str("event_type", "MEMORY_STORE").
    Str("user_id", userID).
    Str("scope_id", scopeID).
    Int("count", len(docs)).
    Msg("添加记忆")
```

### M2：BaseMemoryManager 接口加 `opts ...MemoryOption`，llm 移入 Option

**文件**：`base_manager.go`、所有实现类、`write_manager.go`

**问题**：Python `**kwargs` 扩展点在 Go 中完全丢失；`llmModel ...*llm.Model` 是变参，无法再加第二个变参。

**修复**：

1. 新增类型定义：
```go
// MemoryOptionConfig 记忆操作选项配置（对齐 Python **kwargs 扩展点）
type MemoryOptionConfig struct {
    llmModel *llm.Model
}

// MemoryOption 记忆操作选项函数
type MemoryOption func(*MemoryOptionConfig)

// WithLLMModel 设置 LLM 模型（对齐 Python llm 参数）
func WithLLMModel(model *llm.Model) MemoryOption {
    return func(cfg *MemoryOptionConfig) {
        cfg.llmModel = model
    }
}

// ApplyMemoryOptions 应用选项列表到配置
func ApplyMemoryOptions(opts ...MemoryOption) *MemoryOptionConfig {
    cfg := &MemoryOptionConfig{}
    for _, opt := range opts {
        opt(cfg)
    }
    return cfg
}
```

2. 接口签名变更（所有方法移除 `llmModel ...*llm.Model`，加 `opts ...MemoryOption`）：
```go
type BaseMemoryManager interface {
    AddMemories(ctx context.Context, userID, scopeID string,
        memories map[string][]mem_model.MemoryUnit, opts ...MemoryOption) ([]mem_model.MemoryUnit, error)
    Update(ctx context.Context, userID, scopeID, memID, newMemory string,
        opts ...MemoryOption) (bool, error)
    Delete(ctx context.Context, userID, scopeID, memID string,
        opts ...MemoryOption) (bool, error)
    DeleteByUserID(ctx context.Context, userID, scopeID string,
        opts ...MemoryOption) (bool, error)
    Search(ctx context.Context, userID, scopeID, query string, topK int, memTypes []string,
        opts ...MemoryOption) ([]*index.MemorySearchResult, error)
    Get(ctx context.Context, userID, scopeID, memID string,
        opts ...MemoryOption) (*index.MemoryDoc, error)
}
```

3. FragmentManager `AddMemories` 中从 opts 提取 llm：
```go
cfg := ApplyMemoryOptions(opts...)
llmModel := cfg.llmModel
```

4. WriteManager 透传 opts：
```go
result, err := manager.AddMemories(ctx, userID, scopeID, memories, opts...)
```

5. 波及范围：3 个实现类 + WriteManager + 对应测试文件。

### M3：WriteManager `getMemTypeFromIndex` 吞掉 GetByID 错误

**文件**：`write_manager.go` `UpdateMemByID` / `DeleteMemByID` 方法

**问题**：`getMemTypeFromIndex` 返回 error 时，调用方只 warn 日志 + 静默跳过，不传播错误。Python 让异常向上传播。

**修复**：
```go
memType, err := w.getMemTypeFromIndex(ctx, userID, scopeID, memID)
if err != nil {
    return err  // 传播底层错误
}
if memType == "" {
    logger.Warn(...).Msg("记忆类型未知，跳过本次更新")
    return nil
}
```

### M5：encrypt/decrypt 从包级函数改为 memoryManagerBase 方法

**文件**：`base_manager.go`

**问题**：Python 中是 `@staticmethod` 可被子类覆写，Go 是包级函数不可覆写。虽无实际覆写需求，但作为方法与结构体关联语义更清晰。

**修复**：
```go
// 修改前
func encryptMemoryIfNeeded(key []byte, plaintext string) string { ... }
func decryptMemoryIfNeeded(key []byte, ciphertext string) string { ... }

// 修改后
func (b *memoryManagerBase) encryptMemoryIfNeeded(key []byte, plaintext string) string { ... }
func (b *memoryManagerBase) decryptMemoryIfNeeded(key []byte, ciphertext string) string { ... }
```

测试文件需适配：构造 `memoryManagerBase` 实例来调用。

### M7：所有 `time.Now()` 改为 `time.Now().UTC()`

**文件**：`fragment_manager.go`、`summary_manager.go`

**问题**：Python 强制返回 UTC 时区 datetime，Go `time.Now()` 可能是本地时区。

**修复**：涉及以下位置：
- `fragment_manager.go`：`parseTimestamp` 两处 `time.Now()` → `time.Now().UTC()`
- `fragment_manager.go`：`Update` 方法中 `time.Now()` → `time.Now().UTC()`
- `summary_manager.go`：`Update` 方法中 `time.Now()` → `time.Now().UTC()`

## 不修复项

| ID | 原因 |
|----|------|
| H3 | 保持 Go struct 风格返回 MemoryDoc/MemorySearchResult，不做 Python dict 映射 |
| M4 | 保持 Go 单字符串日志格式，不做 Python list 包裹 |
| M6 | Python `_process_conflict_info` 死代码，不补 |
| M8 | 保持 Go nil 惯例，不做空切片对齐 |

## 实施顺序

1. M2（接口重构，最大改动，先改接口再适配实现）
2. M5（encrypt/decrypt 移到 memoryManagerBase）
3. M7（time.Now.UTC 统一）
4. H1 + H2 + H5 + M1（日志修复，简单独立）
5. H4（返回原始列表）
6. M3（WriteManager 错误传播）
7. 更新测试覆盖
