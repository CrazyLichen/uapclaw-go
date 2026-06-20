# 5.14 Session Utils 设计文档

## 概述

5.14 Session Utils 是领域五「会话与上下文引擎」→ 会话系统子系列（5.1~5.14）的最后一个步骤。本步骤将 Python `openjiuwen/core/session/utils.py` 中的工具函数迁移为 Go 的 `session/utils` 独立子包，导出供 `state`、`store`、`tracer`、`graph` 等多个包使用的通用工具函数。

## 流程位置

```
5.1  State 体系          ✅
5.2  BaseSession 接口    ✅
5.3  AgentSession        ✅
5.4  WorkflowSession     ✅
5.5  SessionNode         ✅
5.6  SessionController   ✅
5.7  Interaction         ✅
5.8  Checkpointer 接口   ✅
5.9  PersistenceCheckpointer ✅
5.10 StreamWriter        ✅
5.11 Session Tracer      ✅
5.12 Session Config      ☐
5.13 Session Constants   ☐
5.14 Session Utils       ☐  ★ 本步骤
──── 5.x 上下文引擎 ────
5.15 ModelContext 接口   ☐
```

### 作用

Python `session/utils.py` 是会话系统的**基础工具层**，被以下模块引用：

| 被引用模块 | 引用的函数 |
|-----------|-----------|
| `session/state/base.py` | `update_dict`, `get_by_schema` |
| `session/store.py` | `get_by_schema`, `update_dict` |
| `session/tracer/workflow_tracer.py` | `NESTED_PATH_SPLIT` |
| `graph/vertex.py` | `is_ref_path`, `extract_origin_key` |
| `graph/stream_actor/base.py` | `EndFrame`, `get_value_by_nested_path`, `extract_origin_key` |
| `session/__init__.py`（包导出） | `EndFrame`, `get_by_schema`, `get_value_by_nested_path`, `extract_origin_key`, `is_ref_path`, `NESTED_PATH_SPLIT` |

核心作用分三类：
1. **嵌套路径操作** — 支持 `a.b[0].c` 形式的深层字典/列表访问与自动构建
2. **引用路径解析** — 支持 `${start123.p2}` 形式的引用路径，用于状态引用和工作流参数绑定
3. **字典/容器操作** — 深拷贝、更新、展开嵌套结构等

## 关键决策

### 决策 1：新建 `session/utils` 包，导出函数

**选项**：A（新建 `session/utils` 包，导出函数） / B（保持 state/utils.go，仅导出） / C（新建 + 类型桥接）

**选择**：A — 新建 `session/utils/` 独立子包，将 `state/utils.go` 中的函数迁出并导出，`state` 包改为引用 `utils` 包。对齐 Python 的模块划分。

**Why**：Python 中 `utils.py` 是独立模块，被 state、store、tracer、graph 等多个包引用。Go 实现中这些函数目前全部放在 `state/utils.go` 中作为非导出函数，其他包无法访问。需要独立包来提供跨包共享能力。

### 决策 2：按依赖拆分，避免循环依赖

**选择**：utils 包只放纯工具函数（不依赖 `StateKey`），`getBySchema` 等依赖 `StateKey` 的函数保留在 `state` 包中。

**Why**：如果把 `getBySchema` 迁到 `utils`，`utils` 需要 `import state`（因为参数类型是 `state.StateKey`），而 `state` 也要 `import utils`（因为 `state` 内部大量使用工具函数），形成循环依赖。按依赖拆分后，依赖方向为 `state → utils`，单向无环。

### 决策 3：`create_wrapper_class` 不实现

**选择**：不实现，在 doc.go 中注明。

**Why**：该函数在整个 Python 项目中**零调用**（只有定义，没有 import 或调用），属于死代码。Go 中的会话门面体系（`NodeSessionFacade`、`RouterSessionFacade`）已用组合+委托模式手动实现了等价功能。Go 没有动态元编程能力，实现这个函数无价值。

### 决策 4：`EndFrame` / `Frame` 标记 ⤵️ 延后

**选择**：不实现，标记 ⤵️ 延后到 stream_actor 实现（领域八 8.x）。

**Why**：Go 的 `StreamQueue` 已用 `close(ch)` + `IsEndOfStream(err)` 替代了 Python 的单源 `EndFrame` 哨兵模式（见 `stream/queue.go` 第 19-22 行注释）。Python 中 `EndFrame` 的多源场景使用（`StreamActor.processor_queues`）属于 `stream_actor` 的需求，等 8.x 实现时再处理。

## 包结构

```
internal/agentcore/session/utils/
├── doc.go           # 包文档
├── path.go          # 嵌套路径操作
├── ref.go           # 引用路径操作
├── dict.go          # 字典操作
├── container.go     # 容器操作（深拷贝、安全扩展）
├── string.go        # 字符串辅助操作
└── constants.go     # 常量定义
```

对应 Python 代码：`openjiuwen/core/session/utils.py`

## 函数归属

### 迁到 `utils` 包（纯工具，不依赖 StateKey）

| 原 state/utils.go 函数 | utils 包导出名 | 文件 |
|----------------------|---------------|------|
| `splitNestedPath` | `SplitNestedPath` | `path.go` |
| `getValueByNestedPath` | `GetValueByNestedPath` | `path.go` |
| `rootToPath` | `RootToPath` | `path.go` |
| `rootToIndex` | `RootToIndex` | `path.go` |
| `isRefPath` | `IsRefPath` | `ref.go` |
| `extractOriginKey` | `ExtractOriginKey` | `ref.go` |
| `updateDict` | `UpdateDict` | `dict.go` |
| `updateByKey` | `UpdateByKey` | `dict.go` |
| `deleteByKey` | `DeleteByKey` | `dict.go` |
| `expandNestedStructure` | `ExpandNestedStructure` | `dict.go` |
| `safeExtendContainer` | `SafeExtendContainer` | `container.go` |
| `deepCopyMap` | `DeepCopyMap` | `container.go` |
| `deepCopySlice` | `DeepCopySlice` | `container.go` |
| `deepCopyValue` | `DeepCopyValue` | `container.go` |
| `deepCopyUpdates` | `DeepCopyUpdates` | `container.go` |
| `convertUpdatesFromJSON` | `ConvertUpdatesFromJSON` | `container.go` |
| `containsChar` | `ContainsChar` | `string.go` |
| `containsSubstring` | `ContainsSubstring` | `string.go` |
| `splitString` | `SplitString` | `string.go` |
| `parseListIndexes` | `ParseListIndexes` | `string.go` |

### 常量迁到 `utils` 包

| 原 state/utils.go 常量 | utils 包导出名 | 文件 |
|----------------------|---------------|------|
| `regexMaxLength` | `RegexMaxLength` | `constants.go` |
| `nestedPathSplit` | `NestedPathSplit` | `constants.go` |
| `nestedPathListSplit` | `NestedPathListSplit` | `constants.go` |

### 保留在 `state` 包（依赖 StateKey）

| 函数 | 原因 |
|------|------|
| `getBySchema` | 参数类型为 `state.StateKey` |
| `getBySchemaMap` | 调用 `getBySchema` |
| `getBySchemaList` | 调用 `getBySchema` |
| `getValueByNestedPathMap` | 被 `getBySchema` 调用 |
| `parentEntry` 结构体 | `rootToPath` 内部使用的回写追踪，已被 `RootToPath` 迁出后不再需要（utils 包内部自持） |
| `writeBackList` | `rootToPath` 的辅助函数，随 `RootToPath` 迁到 utils |

### 不实现

| Python 函数/类型 | 处置 | 原因 |
|-----------------|------|------|
| `create_wrapper_class` | 不实现，在 doc.go 注明 | Python 死代码，Go 已有手动委托替代 |
| `EndFrame` | 标记 ⤵️ 延后到 8.x stream_actor | Go 已用 `close(ch)` 替代单源场景 |
| `Frame`（类型别名） | 标记 ⤵️ 延后到 8.x stream_actor | 依赖 EndFrame |

## state 包回填改动

### state/utils.go 改造

迁出函数后，`state/utils.go` 仅保留依赖 `StateKey` 的函数。保留的函数内部调用改为使用 `utils` 包的导出版本：

```go
// 之前
paths := splitNestedPath(nestedKey)
originKey := extractOriginKey(schema.String())

// 之后
paths := utils.SplitNestedPath(nestedKey)
originKey := utils.ExtractOriginKey(schema.String())
```

### state 包其他文件改动

| 文件 | 改动内容 |
|------|---------|
| `inmemory_state.go` | `deepCopyValue` → `utils.DeepCopyValue`，`updateDict` → `utils.UpdateDict`，`deepCopyMap` → `utils.DeepCopyMap` |
| `inmemory_commit_state.go` | `deepCopyMap` → `utils.DeepCopyMap`，`deepCopyUpdates` → `utils.DeepCopyUpdates` |
| `key.go` | `deepCopyMap` → `utils.DeepCopyMap`，`deepCopySlice` → `utils.DeepCopySlice` |
| `workflow_commit_state.go` | `deepCopyUpdates` → `utils.DeepCopyUpdates`，`convertUpdatesFromJSON` → `utils.ConvertUpdatesFromJSON` |

### state/utils_test.go 测试迁移

纯工具函数的测试迁到 `utils/` 包的测试文件，依赖 `StateKey` 的函数测试保留在 `state` 包。

## doc.go 模板

```go
// Package utils 提供会话系统的通用工具函数，包括嵌套路径操作、引用路径解析、
// 字典操作和容器深拷贝等。
//
// 本包从 state/utils.go 中迁出，作为独立子包供 state、store、tracer、graph 等
// 多个包共享使用，避免循环依赖。
//
// 文件目录：
//
//	utils/
//	├── doc.go           # 包文档
//	├── path.go          # 嵌套路径操作（SplitNestedPath, GetValueByNestedPath, RootToPath, RootToIndex）
//	├── ref.go           # 引用路径操作（IsRefPath, ExtractOriginKey）
//	├── dict.go          # 字典操作（UpdateDict, UpdateByKey, DeleteByKey, ExpandNestedStructure）
//	├── container.go     # 容器操作（SafeExtendContainer, DeepCopyMap/Slice/Value/Updates, ConvertUpdatesFromJSON）
//	├── string.go        # 字符串辅助（ContainsChar, ContainsSubstring, SplitString, ParseListIndexes）
//	└── constants.go     # 常量（RegexMaxLength, NestedPathSplit, NestedPathListSplit）
//
// 对应 Python 代码：openjiuwen/core/session/utils.py
//
// 未移植说明：
//   - create_wrapper_class：Python 中零调用，Go 使用手动委托模式替代（见 NodeSessionFacade/RouterSessionFacade）
//   - EndFrame/Frame：Go 使用 close(ch) 替代单源场景，多源场景 ⤵️ 延后到 8.x stream_actor 回填
package utils
```

## 测试要求

- 每个迁出的导出函数必须有对应的单元测试
- 测试覆盖率 ≥ 85%
- `state` 包保留函数的测试不受影响
- 测试函数命名遵循项目规范：`TestXxx_场景描述`
