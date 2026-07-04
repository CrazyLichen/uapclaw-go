# Go 代码规范审查报告

审查日期：2025-07-14
审查范围：全项目 499 个非测试 Go 源文件

---

## 一、问题汇总

| 问题类型 | 发现数量 | 已修复数量 | 严重程度 |
|---------|---------|-----------|---------|
| 英文行内注释（字段注释） | 5 | 5 | 高 |
| 英文字段名作为独立注释（缺中文） | 6 | 6 | 中 |
| 英文惯用法注释（no-op/ok/double-check） | 7 | 7 | 中 |
| 独立"接口"分隔注释（应合并到结构体区块） | 10 | 10 | 中 |
| 接口排在结构体之后 | 2 | 2 | 高 |
| 重复的分隔注释 | 50+ | 50+ | 低 |
| 全局变量散落在文件各处 | 1 | 1 | 高 |
| 导出/非导出函数交叉排列 | 1 | 1 | 高 |
| 不合理的 reexport | 14 | 0（见说明） | 中 |

**总计：发现约 96 处问题，已修复约 82 处。**

---

## 二、已修复问题详情

### 2.1 英文注释修复（18 处）

| 文件 | 原注释 | 修复后 |
|------|-------|--------|
| `foundation/tool/mcp/client/openapi_client.go:46` | `// GET/POST/PUT/DELETE/PATCH` | `// HTTP 方法（GET/POST/PUT/DELETE/PATCH）` |
| `foundation/tool/mcp/client/openapi_client.go:47` | `// /api/v1/items` | `// API 路径（如 /api/v1/items）` |
| `foundation/tool/mcp/client/openapi_client.go:57` | `// path, query, header, cookie` | `// 参数位置（path/query/header/cookie）` |
| `common/utils/net.go:94` | `// host:port` | `// 主机:端口` |
| `common/utils/net.go:101` | `// path` | `// 路径` |
| `foundation/store/graph/milvus/schema.go:289` | `// created_at` | `// created_at 创建时间戳` |
| `foundation/store/graph/milvus/schema.go:294` | `// user_id` | `// user_id 用户标识` |
| `foundation/store/graph/milvus/schema.go:300` | `// obj_type` | `// obj_type 对象类型` |
| `foundation/store/graph/milvus/schema.go:308` | `// language` | `// language 语言` |
| `foundation/store/graph/milvus/schema.go:314` | `// metadata` | `// metadata 元数据` |
| `foundation/store/graph/milvus/schema.go:319` | `// content` | `// content 内容` |
| `runner/message_queue/local.go:82,87,92` | `// no-op` | `// 空操作` |
| `foundation/llm/model_clients/intellirouter/router.go:691` | `// double-check` | `// 二次检查（加锁后再次确认）` |
| `foundation/tool/schema_utils.go:290` | `// ok` | `// 类型匹配，无需处理` |
| `common/utils/dict.go:359` | `// ok` | `// 类型匹配，无需处理` |
| `common/utils/dict.go:366` | `// ok` | `// 类型匹配，无需处理` |

### 2.2 独立"接口"分隔注释修复（10 处）

将 `// ──── 接口 ────` 分隔注释合并到 `// ──── 结构体 ────` 区块内，接口排在 struct 之前。

| 文件 | 修复说明 |
|------|---------|
| `foundation/llm/model_clients/intellirouter/router.go` | 删除独立"接口"分隔注释 |
| `foundation/prompt/variable.go` | 删除独立"接口"分隔注释 |
| `harness/task_loop/stop_condition.go` | 将接口移到结构体之前，删除独立"接口"分隔注释 |
| `session/controller/scope.go` | 删除独立"接口"分隔注释 |
| `single_agent/interfaces/interface.go` | 合并"接口"和"结构体"区块 |
| `common/crypto/crypto.go` | 删除独立"接口"分隔注释 |
| `common/crypto/registry.go` | 删除独立"接口"分隔注释 |
| `common/schema/tool_info.go` | 删除独立"接口"分隔注释 |
| `common/version/version_source.go` | 删除独立"接口"分隔注释 |
| `multi_agent/team_runtime/communicable_interface.go` | 将"接口"改为"结构体" |

### 2.3 重复分隔注释修复（50+ 处）

批量删除了 50 个文件中的重复分隔注释（同一类分隔注释出现多次），包括重复的"导出函数"、"非导出函数"等分隔注释。

涉及文件：background.go、pool.go、origin.go、sanitizer.go、param.go、status_code.go、init.go、early.go、dotenv.go、workflow.go、span.go、factory.go、inmemory.go、controller.go、intent_recognizer.go 等。

### 2.4 声明顺序修复（2 处）

| 文件 | 问题 | 修复方式 |
|------|------|---------|
| `context_engine/processor/offloader/message_summary_offloader.go` | 全局变量散落在导出函数之后 | 将全局变量移到全局变量区块 |
| `single_agent/agents/react_helpers.go` | `getAbilityManager`（非导出）在 `SetAbilityManager`（导出）之前 | 调换顺序，导出在前 |

---

## 三、未修复问题说明

### 3.1 Reexport 问题（14 处，未修复）

以下文件存在不合理的类型别名 reexport（非子包向父包模式），但已标注 `TODO: 考虑移除 reexport`：

| 文件 | reexport 数量 | 来源包 |
|------|-------------|--------|
| `single_agent/ability/ability_types.go` | 2 类型别名 | `single_agent/schema` |
| `single_agent/interrupt/response.go` | 3 类型别名 | `single_agent/schema` |
| `single_agent/interrupt/state.go` | 2 类型别名 + 4 常量 | `single_agent/schema` |
| `single_agent/interrupt/exception.go` | 1 类型别名 | `single_agent/schema` |
| `session/checkpointer/base.go` | 1 类型别名 | `session/interfaces` |

**未修复原因**：移除 reexport 需要修改所有调用方的 import 路径，属于功能性重构而非纯规范修复。建议后续专项处理。

### 3.2 枚举出现在常量之后（7 处，未修复）

以下文件中函数类型定义（`type Option func(...)`）出现在常量之后。按规范 `type Option func(...)` 应归类为枚举区块，排在常量之前。但当前这些文件中枚举和常量已经在一个连续的区块中，且 iota 常量紧跟枚举类型定义，强制拆分会影响可读性。

涉及文件：`llm/schema/config.go`、`llm/schema/message.go`、`store/vector_fields/base.go`、`runner/callback/framework.go`、`runner/resources_manager/base.go`、`session/controller/data_container.go`、`common/schema/card.go`

**未修复原因**：函数类型定义和 iota 常量在语义上紧密关联，拆分可能降低代码可读性。建议后续统一讨论是否将 `type Option func(...)` 归类为枚举或新增"函数类型"区块。

### 3.3 英文分隔子注释（6 处，保留）

`intellirouter/router.go` 中有 `// ──── HealthChecker ────` 等按类型名称分组的子注释，属于导出函数区块内的分组注释，用于提高长文件的可导航性。此类子注释在规范中未明确禁止，暂予保留。

---

## 四、编译验证

- 编译命令：`go build ./...`
- 编译结果：**通过** ✅
- 无编译错误、无编译警告

---

## 五、修改原则

本次审查严格遵循"只修改规范问题，不修改功能或逻辑"的原则：

1. ✅ 只修改了注释内容和分隔注释
2. ✅ 只调整了声明顺序（同文件内移动）
3. ✅ 未修改任何代码逻辑、函数签名、类型定义
4. ✅ 未修改 reexport（属于功能性重构）
5. ✅ 编译通过，功能不受影响
