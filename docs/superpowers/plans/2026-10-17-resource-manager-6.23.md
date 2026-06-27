# 6.23 ResourceMgr 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 Agent/Tool/Workflow/Model/Prompt/SysOperation 全局注册表 ResourceMgr，作为 Runner 单例的核心依赖。

**Architecture:** 门面模式 — ResourceMgr 作为统一入口，内部委托 ResourceRegistry（聚合7个子管理器）和 TagMgr（标签双向索引）。子管理器分两类：基于 AbstractManager 的 Provider 模式（Agent/Workflow/Model）和直接存储模式（Prompt/Tool/SysOperation）。所有 add/get/remove 操作通过 4 个内部核心方法统一流转。

**Tech Stack:** Go 1.23+, sync.RWMutex, sync.Mutex, context.Context, Functional Options

**设计文档:** `docs/superpowers/specs/2026-10-17-resource-manager-6.23-design.md`

**Python 参考:** `openjiuwen/core/runner/resources_manager/` (13 个 Python 文件)

---

## 文件结构

### 新建文件（runner/resources_manager/）

| 文件 | 职责 | Python 对照 |
|------|------|------------|
| `doc.go` | 包文档 | `__init__.py` |
| `base.go` | Provider 类型别名、Tag 常量、枚举 | `base.py` |
| `base_test.go` | base.go 测试 | — |
| `thread_safe_dict.go` | 泛型线程安全字典 | `thread_safe_dict.py` |
| `thread_safe_dict_test.go` | ThreadSafeDict 测试 | — |
| `abstract_manager.go` | 泛型抽象管理器 | `abstract_manager.py` |
| `abstract_manager_test.go` | AbstractManager 测试 | — |
| `tag_manager.go` | 标签管理器，双向索引 | `tag_manager.py` |
| `tag_manager_test.go` | TagMgr 测试 | — |
| `resource_registry.go` | 聚合 7 个子管理器 | `resource_registry.py` |
| `resource_registry_test.go` | ResourceRegistry 测试 | — |
| `agent_manager.go` | Agent 管理器（本地+分布式⤵️） | `agent_manager.py` |
| `agent_manager_test.go` | AgentMgr 测试 | — |
| `agent_team_manager.go` | AgentTeam 管理器（⤵️预留） | `agent_team_manager.py` |
| `agent_team_manager_test.go` | AgentTeamMgr 测试 | — |
| `model_manager.go` | Model 管理器+trace 装饰 | `model_manager.py` |
| `model_manager_test.go` | ModelMgr 测试 | — |
| `prompt_manager.go` | Prompt 管理器，直接存储 | `prompt_manager.py` |
| `prompt_manager_test.go` | PromptMgr 测试 | — |
| `tool_manager.go` | Tool 管理器+MCP Server 全套 | `tool_manager.py` |
| `tool_manager_test.go` | ToolMgr 测试 | — |
| `workflow_manager.go` | Workflow 管理器+trace 装饰 | `workflow_manager.py` |
| `workflow_manager_test.go` | WorkflowMgr 测试 | — |
| `sys_operation_manager.go` | SysOperation 管理器（⤵️预留） | `sys_operation_manager.py` |
| `sys_operation_manager_test.go` | SysOperationMgr 测试 | — |
| `resource_manager.go` | ResourceMgr 门面类 | `resource_manager.py` |
| `resource_manager_test.go` | ResourceMgr 测试 | — |

### 修改文件

| 文件 | 修改内容 |
|------|---------|
| `single_agent/ability/ability_manager.go` | 导入改为 `runner/resources_manager`；字段类型+调用适配 |
| `single_agent/ability/ability_manager_test.go` | 更新 fakeResourceManager |
| `single_agent/base.go` | 删除 ResourceManager/NoopResourceManager 等 re-export |
| `single_agent/agents/react_agent.go` | 删除 NoopResourceManager 引用 |
| `single_agent/doc.go` | 移除 resource/ 条目 |
| `runner/doc.go` | 添加 resources_manager/ 子目录描述 |

### 删除文件

| 文件 | 原因 |
|------|------|
| `single_agent/resource/doc.go` | 占位代码 |
| `single_agent/resource/resource_manager.go` | 占位代码 |
| `single_agent/resource/resource_manager_test.go` | 占位代码 |

---

## Task 1: base.go + base_test.go

**Files:**
- Create: `internal/agentcore/runner/resources_manager/base.go`
- Create: `internal/agentcore/runner/resources_manager/base_test.go`

- [ ] **Step 1: 创建 base.go**

实现设计文档 4.1 节中的全部内容：
- `AgentProvider` / `WorkflowProvider` / `ModelProvider` 类型别名
- `Tag` 类型别名（`type Tag = string`）
- `TagMatchStrategy` / `TagUpdateStrategy` 枚举
- `TagAll` / `TagGlobal` / `TagActive` / `TagInactive` 常量
- 所有注释使用中文，声明排列遵循项目规范（结构体→枚举→常量→全局变量→导出函数→非导出函数）

对照 Python: `openjiuwen/core/runner/resources_manager/base.py`

- [ ] **Step 2: 创建 base_test.go**

测试内容：
- `TestTagMatchStrategy_值` — 验证枚举值
- `TestTagUpdateStrategy_值` — 验证枚举值
- `TestTag常量_值` — 验证 TagAll/TagGlobal/TagActive/TagInactive
- `TestAgentProvider_调用` — 验证 provider 函数类型可调用
- `TestWorkflowProvider_调用` — 同上
- `TestModelProvider_调用` — 同上

- [ ] **Step 3: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/runner/resources_manager/ -run "TestTag|TestAgent|TestWorkflow|TestModel" -v`

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/runner/resources_manager/base.go internal/agentcore/runner/resources_manager/base_test.go
git commit -m "feat(6.23): 实现 resources_manager base.go — Provider 类型/Tag 常量/枚举"
```

---

## Task 2: thread_safe_dict.go + thread_safe_dict_test.go

**Files:**
- Create: `internal/agentcore/runner/resources_manager/thread_safe_dict.go`
- Create: `internal/agentcore/runner/resources_manager/thread_safe_dict_test.go`

- [ ] **Step 1: 创建 thread_safe_dict.go**

实现设计文档 4.2 节：
- `ThreadSafeDict[K comparable, V any]` 结构体（`sync.RWMutex` + `map[K]V`）
- 方法：`NewThreadSafeDict` / `Get` / `Set` / `Delete` / `Len` / `Contains` / `GetOrSet` / `GetOrCreate` / `Pop` / `SetDefault` / `Update` / `Clear` / `Keys` / `Values` / `Items`
- 所有方法使用 `mu.RLock()`/`mu.Lock()` 保证并发安全
- `GetOrCreate` 接受 `creator func() V` 参数

对照 Python: `openjiuwen/core/runner/resources_manager/thread_safe_dict.py`

- [ ] **Step 2: 创建 thread_safe_dict_test.go**

测试内容：
- `TestThreadSafeDict_基本操作` — Set/Get/Delete
- `TestThreadSafeDict_不存在键` — Get 返回零值
- `TestThreadSafeDict_GetOrSet` — 存在时返回已有值，不存在时设置默认值
- `TestThreadSafeDict_GetOrCreate` — 不存在时调用 creator
- `TestThreadSafeDict_Pop` — 存在时返回并删除，不存在时返回零值
- `TestThreadSafeDict_SetDefault` — 类似 GetOrSet
- `TestThreadSafeDict_Update` — 批量更新
- `TestThreadSafeDict_Clear` — 清空
- `TestThreadSafeDict_KeysValuesItems` — 遍历
- `TestThreadSafeDict_LenContains` — 长度和包含
- `TestThreadSafeDict_并发安全` — 多 goroutine 并发读写

- [ ] **Step 3: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/runner/resources_manager/ -run "TestThreadSafeDict" -v`

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/runner/resources_manager/thread_safe_dict.go internal/agentcore/runner/resources_manager/thread_safe_dict_test.go
git commit -m "feat(6.23): 实现 ThreadSafeDict 泛型线程安全字典"
```

---

## Task 3: abstract_manager.go + abstract_manager_test.go

**Files:**
- Create: `internal/agentcore/runner/resources_manager/abstract_manager.go`
- Create: `internal/agentcore/runner/resources_manager/abstract_manager_test.go`

- [ ] **Step 1: 创建 abstract_manager.go**

实现设计文档 4.3 节：
- `AbstractManager[T any]` 结构体，嵌入 `ThreadSafeDict[string, func(context.Context) (T, error)]`
- `NewAbstractManager[T]()` 构造函数
- `registerProvider(resourceID string, provider func(context.Context) (T, error)) error` — 重复注册返回 error
- `getResource(ctx context.Context, resourceID string) (T, error)` — 调用 provider 获取资源
- `unregisterProvider(resourceID string) (func(context.Context) (T, error), error)` — 注销并返回 provider

对照 Python: `openjiuwen/core/runner/resources_manager/abstract_manager.py`

- [ ] **Step 2: 创建 abstract_manager_test.go**

测试内容：
- `TestAbstractManager_注册获取` — register → get 返回正确实例
- `TestAbstractManager_重复注册返回错误` — 同 ID 二次注册报错
- `TestAbstractManager_获取不存在返回错误` — 未注册 ID 报错
- `TestAbstractManager_注销` — unregister 后 get 报错
- `TestAbstractManager_注销不存在` — 不存在 ID 注销报错
- `TestAbstractManager_并发注册获取` — 多 goroutine 并发操作

使用 `int` 作为泛型参数简化测试。

- [ ] **Step 3: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/runner/resources_manager/ -run "TestAbstractManager" -v`

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/runner/resources_manager/abstract_manager.go internal/agentcore/runner/resources_manager/abstract_manager_test.go
git commit -m "feat(6.23): 实现 AbstractManager 泛型抽象管理器"
```

---

## Task 4: tag_manager.go + tag_manager_test.go

**Files:**
- Create: `internal/agentcore/runner/resources_manager/tag_manager.go`
- Create: `internal/agentcore/runner/resources_manager/tag_manager_test.go`

- [ ] **Step 1: 创建 tag_manager.go**

实现设计文档 4.4 节，对照 Python `tag_manager.py`（432行）：
- `TagMgr` 结构体（`resourceTags` / `tagToResource` / `mu`）
- `NewTagMgr()` 构造函数
- 全部 13 个公开方法（HasTag/ListTags/HasResource/TagResource/RemoveResource/RemoveResourceTags/UpdateResourceTags/RemoveTag/GetTagResources/FindResourcesByTags/HasResourceTag/GetResourcesTags/Display）
- 全部 9 个内部方法（setGlobalResource/addResourceTags/removeResource/removeResourceTags/replaceResourceTags/removeTag/findResourcesWithAllTags/normalizeTags/isBuiltinTag）
- GLOBAL 标签特殊逻辑：GLOBAL 资源不能有其他标签
- 日志同步对照 Python 中每个方法的 logger.info 调用

- [ ] **Step 2: 创建 tag_manager_test.go**

测试内容（对照 Python `tests/unit_tests/core/runner/test_tag_manager.py`）：
- `TestTagMgr_TagResource` — 添加标签
- `TestTagMgr_TagResource_GLOBAL` — GLOBAL 标签特殊逻辑
- `TestTagMgr_RemoveResource` — 移除资源
- `TestTagMgr_RemoveResourceTags` — 移除指定标签
- `TestTagMgr_RemoveResourceTags_不存在报错` — skip_if_not_exists=false 时报错
- `TestTagMgr_UpdateResourceTags_MERGE` — 合并策略
- `TestTagMgr_UpdateResourceTags_REPLACE` — 替换策略
- `TestTagMgr_RemoveTag` — 移除标签
- `TestTagMgr_FindResourcesByTags_ALL` — 全匹配策略
- `TestTagMgr_FindResourcesByTags_ANY` — 任一匹配策略
- `TestTagMgr_HasResourceTag` — 检查资源标签
- `TestTagMgr_GetResourcesTags` — 获取资源所有标签
- `TestTagMgr_Display` — 展示方法不 panic
- `TestTagMgr_ListTags` — 列出标签（排除空标签）

- [ ] **Step 3: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/runner/resources_manager/ -run "TestTagMgr" -v`

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/runner/resources_manager/tag_manager.go internal/agentcore/runner/resources_manager/tag_manager_test.go
git commit -m "feat(6.23): 实现 TagMgr 标签管理器，双向索引+GLOBAL特殊逻辑"
```

---

## Task 5: 各子管理器（Agent/AgentTeam/Model/Prompt/Workflow/SysOperation）

**Files:**
- Create: `internal/agentcore/runner/resources_manager/agent_manager.go`
- Create: `internal/agentcore/runner/resources_manager/agent_manager_test.go`
- Create: `internal/agentcore/runner/resources_manager/agent_team_manager.go`
- Create: `internal/agentcore/runner/resources_manager/agent_team_manager_test.go`
- Create: `internal/agentcore/runner/resources_manager/model_manager.go`
- Create: `internal/agentcore/runner/resources_manager/model_manager_test.go`
- Create: `internal/agentcore/runner/resources_manager/prompt_manager.go`
- Create: `internal/agentcore/runner/resources_manager/prompt_manager_test.go`
- Create: `internal/agentcore/runner/resources_manager/workflow_manager.go`
- Create: `internal/agentcore/runner/resources_manager/workflow_manager_test.go`
- Create: `internal/agentcore/runner/resources_manager/sys_operation_manager.go`
- Create: `internal/agentcore/runner/resources_manager/sys_operation_manager_test.go`

- [ ] **Step 1: 创建 agent_manager.go + agent_manager_test.go**

实现设计文档 4.6 节：
- `AgentMgr` 嵌入 `AbstractManager[interfaces.BaseAgent]`
- `NewAgentMgr()` / `AddAgent(agentID, provider)` / `RemoveAgent(agentID)` / `GetAgent(ctx, agentID)`
- 分布式相关标记 ⤵️ 预留

测试：AddAgent→GetAgent 正常流程、重复注册报错、RemoveAgent 后获取报错

对照 Python: `agent_manager.py`

- [ ] **Step 2: 创建 agent_team_manager.go + agent_team_manager_test.go**

实现设计文档 4.7 节：
- `AgentTeamMgr` 结构体，方法签名定义但标记 ⤵️ 预留
- 测试：仅验证结构体可构造

对照 Python: `agent_team_manager.py`

- [ ] **Step 3: 创建 model_manager.go + model_manager_test.go**

实现设计文档 4.8 节：
- `ModelMgr` 嵌入 `AbstractManager[model_clients.BaseModelClient]`
- `NewModelMgr()` / `AddModel(modelID, provider)` / `RemoveModel(modelID)` / `GetModel(ctx, modelID, session)`
- GetModel 中调用 `decorator.DecorateModelWithTrace(model, session)`，session 为 nil 时跳过装饰

测试：AddModel→GetModel 正常流程、trace 装饰验证、session=nil 不装饰

对照 Python: `model_manager.py`

- [ ] **Step 4: 创建 prompt_manager.go + prompt_manager_test.go**

实现设计文档 4.9 节：
- `PromptMgr` 不继承 AbstractManager，直接用 `ThreadSafeDict[string, *prompt.PromptTemplate]`
- `NewPromptMgr()` / `AddPrompt(templateID, template)` / `AddPrompts(templates)` / `RemovePrompt(templateID)` / `GetPrompt(templateID)`
- `PromptEntry` 辅助类型
- AddPrompt 验证 templateID/template 非空

测试：AddPrompt→GetPrompt、批量添加、空 ID 报错、RemovePrompt 后获取返回 nil

对照 Python: `prompt_manager.py`

- [ ] **Step 5: 创建 workflow_manager.go + workflow_manager_test.go**

实现设计文档 4.11 节：
- `WorkflowMgr` 嵌入 `AbstractManager[interfaces.Workflow]`
- `NewWorkflowMgr()` / `AddWorkflow(workflowID, provider)` / `AddWorkflows(workflows)` / `RemoveWorkflow(workflowID)` / `GetWorkflow(ctx, workflowID, session)`
- `WorkflowEntry` 辅助类型
- GetWorkflow 中调用 `decorator.DecorateWorkflowWithTrace(w, session)`

测试：AddWorkflow→GetWorkflow 正常流程、trace 装饰、批量添加

对照 Python: `workflow_manager.py`

- [ ] **Step 6: 创建 sys_operation_manager.go + sys_operation_manager_test.go**

实现设计文档 4.12 节：
- `SysOperationMgr` 结构体，方法签名定义但标记 ⤵️ 预留
- 测试：仅验证结构体可构造

对照 Python: `sys_operation_manager.py`

- [ ] **Step 7: 运行全部子管理器测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/runner/resources_manager/ -run "TestAgentMgr|TestAgentTeamMgr|TestModelMgr|TestPromptMgr|TestWorkflowMgr|TestSysOperationMgr" -v`

- [ ] **Step 8: 提交**

```bash
git add internal/agentcore/runner/resources_manager/agent_manager.go internal/agentcore/runner/resources_manager/agent_manager_test.go internal/agentcore/runner/resources_manager/agent_team_manager.go internal/agentcore/runner/resources_manager/agent_team_manager_test.go internal/agentcore/runner/resources_manager/model_manager.go internal/agentcore/runner/resources_manager/model_manager_test.go internal/agentcore/runner/resources_manager/prompt_manager.go internal/agentcore/runner/resources_manager/prompt_manager_test.go internal/agentcore/runner/resources_manager/workflow_manager.go internal/agentcore/runner/resources_manager/workflow_manager_test.go internal/agentcore/runner/resources_manager/sys_operation_manager.go internal/agentcore/runner/resources_manager/sys_operation_manager_test.go
git commit -m "feat(6.23): 实现 6 个子管理器（Agent/AgentTeam⤵️/Model/Prompt/Workflow/SysOp⤵️）"
```

---

## Task 6: tool_manager.go + tool_manager_test.go

**Files:**
- Create: `internal/agentcore/runner/resources_manager/tool_manager.go`
- Create: `internal/agentcore/runner/resources_manager/tool_manager_test.go`

- [ ] **Step 1: 创建 tool_manager.go**

实现设计文档 4.10 节，对照 Python `tool_manager.py`（最复杂的子管理器）：

- `McpServerResource` / `SysOpToolResource` 数据类
- `ToolMgr` 结构体（tools/mcpServerNameToIDs/mcpServerResources/sysOpResources/mcpServerLocks/mu）
- `NewToolMgr()` 构造函数
- 普通工具方法：`AddTool` / `GetTool`（含 trace 装饰） / `RemoveTool`
- MCP 工具方法：`GetMcpTool` / `GetMcpTools` / `GetMcpToolID` / `GenerateMcpToolID`（静态）
- MCP Server 方法：`AddToolServer`（含 server_id 粒度 Mutex 锁 + 去重） / `RemoveToolServer` / `RefreshToolServer`
- MCP 辅助方法：`GetMcpServerIDs` / `GetMcpClient` / `GetMcpServerConfig` / `GetMcpToolIDs`
- 系统操作工具：`AddSysOperationTools` / `RemoveSysOperationTools` / `GetSysOperationToolIDs`
- 生命周期：`Release(ctx)` — 遍历所有 MCP 连接调用 Disconnect，忽略单个错误
- 内部方法：`createClient`（调用 `mcp.NewMcpClient`） / `innerRefreshMcpTools` / `innerRemoveMcpTools` / `mcpServerLock`

- [ ] **Step 2: 创建 tool_manager_test.go**

测试内容：
- `TestToolMgr_AddTool` — 添加工具
- `TestToolMgr_AddTool_重复报错` — 同 ID 二次添加报错
- `TestToolMgr_GetTool` — 获取工具
- `TestToolMgr_GetTool_不存在` — 不存在返回 error
- `TestToolMgr_RemoveTool` — 移除工具
- `TestToolMgr_GenerateMcpToolID` — 生成 MCP 工具 ID
- `TestToolMgr_AddSysOperationTools` — 添加系统操作工具
- `TestToolMgr_RemoveSysOperationTools` — 移除系统操作工具
- `TestToolMgr_GetSysOperationToolIDs` — 获取系统操作工具 ID
- `TestToolMgr_Release` — 释放不 panic

注意：AddToolServer/RemoveToolServer/RefreshToolServer 涉及真实 MCP 连接，需要 mock McpClient 接口或使用 `//go:build integration` 标签隔离。单元测试中通过 mock McpClient 测试核心逻辑。

- [ ] **Step 3: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/runner/resources_manager/ -run "TestToolMgr" -v`

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/runner/resources_manager/tool_manager.go internal/agentcore/runner/resources_manager/tool_manager_test.go
git commit -m "feat(6.23): 实现 ToolMgr 工具管理器+MCP Server 全套"
```

---

## Task 7: resource_registry.go + resource_registry_test.go

**Files:**
- Create: `internal/agentcore/runner/resources_manager/resource_registry.go`
- Create: `internal/agentcore/runner/resources_manager/resource_registry_test.go`

- [ ] **Step 1: 创建 resource_registry.go**

实现设计文档 4.5 节：
- `ResourceRegistry` 结构体（7 个子管理器字段）
- `NewResourceRegistry()` 构造函数
- 7 个访问器方法：`Tool()` / `Prompt()` / `Model()` / `Workflow()` / `Agent()` / `AgentTeam()` / `SysOperation()`
- `RemoveByID(resourceID string)` — 依次尝试在各子管理器中移除

对照 Python: `resource_registry.py`

- [ ] **Step 2: 创建 resource_registry_test.go**

测试内容：
- `TestResourceRegistry_创建` — 验证所有子管理器非 nil
- `TestResourceRegistry_访问器` — 验证 7 个访问器返回正确类型
- `TestResourceRegistry_RemoveByID_工具` — 通过 RemoveByID 移除工具
- `TestResourceRegistry_RemoveByID_不存在` — 不存在 ID 不报错

- [ ] **Step 3: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/runner/resources_manager/ -run "TestResourceRegistry" -v`

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/runner/resources_manager/resource_registry.go internal/agentcore/runner/resources_manager/resource_registry_test.go
git commit -m "feat(6.23): 实现 ResourceRegistry 聚合 7 个子管理器"
```

---

## Task 8: resource_manager.go + resource_manager_test.go

**Files:**
- Create: `internal/agentcore/runner/resources_manager/resource_manager.go`
- Create: `internal/agentcore/runner/resources_manager/resource_manager_test.go`

- [ ] **Step 1: 创建 resource_manager.go — 第一部分：结构体+Options+分派常量**

实现设计文档 4.13.1 节：
- `ResourceMgr` 结构体（registry / tagMgr / idToCard）
- `NewResourceMgr()` 构造函数
- 4 组模块级分派常量（registryAccessors / asyncGetTypes / sessionGetTypes / idReturnTypes）

实现设计文档第 5 节：
- `ResourceOption` / `resourceOptions` / 6 个 With* 函数
- `McpOption` / `mcpOptions` / 6 个 WithMcp* 函数
- `TagOption` / `tagOptions` / WithSkipIfTagNotExists

- [ ] **Step 2: 创建 resource_manager.go — 第二部分：Agent/Workflow/Model/Prompt/Tool 公开方法**

实现设计文档 4.13.2 节中各资源类型的 add/get/remove 方法，每个方法内部调用 innerAddResource/innerRemoveResources/innerGetResources/innerGetResourcesByProvider：
- Agent: AddAgent / AddAgents / RemoveAgent / GetAgent
- Workflow: AddWorkflow / AddWorkflows / RemoveWorkflow / GetWorkflow
- Tool: AddTool / GetTool / RemoveTool
- Model: AddModel / AddModels / RemoveModel / GetModel
- Prompt: AddPrompt / AddPrompts / RemovePrompt / GetPrompt

- [ ] **Step 3: 创建 resource_manager.go — 第三部分：SysOperation/MCP/Tag/ToolInfo 公开方法**

- SysOperation: AddSysOperation（基础部分+工具注册⤵️） / RemoveSysOperation / GetSysOperation / GetSysOpToolCards（⤵️）
- MCP Server: AddMcpServer / RefreshMcpServer / RemoveMcpServer / GetMcpTool / GetMcpToolInfos / GetMcpServerConfig / GetMcpToolIDs / ListMcpResources / ReadMcpResource
- Tag: GetResourceByTag / ListTags / HasTag / RemoveTag / UpdateResourceTag / AddResourceTag / RemoveResourceTag / GetResourceTag / ResourceHasTag
- ToolInfo: GetToolInfos
- 生命周期: Release(ctx)

- [ ] **Step 4: 创建 resource_manager.go — 第四部分：内部核心方法+验证方法**

实现设计文档 4.13.3 节：
- getMgr / dispatchAdd / dispatchRemove / dispatchGet
- innerAddResource / innerRemoveResources / innerFindResourceIDs / innerGetResources / innerGetResourcesByProvider

实现设计文档 4.13.4 节（所有验证方法）：
- innerValidateTag / innerValidateResourceCard / innerValidateResourceID / innerValidateResourceIDs
- innerValidateProvider / innerValidateProviders / innerValidateResource / innerValidateServerConfig
- getCardType / innerGetServerIDs

- [ ] **Step 5: 创建 resource_manager_test.go**

测试内容（对照 Python `tests/unit_tests/core/runner/test_resource_manager.py`）：
- `TestResourceMgr_AddAgent_正常` — 添加 Agent
- `TestResourceMgr_AddAgent_重复报错` — 重复 ID 报错
- `TestResourceMgr_GetAgent_正常` — 获取 Agent
- `TestResourceMgr_GetAgent_不存在` — 不存在返回空列表
- `TestResourceMgr_RemoveAgent_正常` — 移除 Agent
- `TestResourceMgr_AddWorkflow_正常` — 添加/获取 Workflow
- `TestResourceMgr_AddModel_正常` — 添加/获取 Model
- `TestResourceMgr_AddPrompt_正常` — 添加/获取 Prompt
- `TestResourceMgr_AddTool_正常` — 添加/获取 Tool
- `TestResourceMgr_Tag操作` — AddAgent+tag / GetAgent+tag / RemoveAgent+tag
- `TestResourceMgr_Release` — 释放不 panic
- `TestResourceMgr_验证方法` — innerValidateTag/innerValidateResourceID 等

- [ ] **Step 6: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/runner/resources_manager/ -run "TestResourceMgr" -v`

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/runner/resources_manager/resource_manager.go internal/agentcore/runner/resources_manager/resource_manager_test.go
git commit -m "feat(6.23): 实现 ResourceMgr 门面类 — 全局注册表+标签+MCP+验证"
```

---

## Task 9: doc.go

**Files:**
- Create: `internal/agentcore/runner/resources_manager/doc.go`
- Modify: `internal/agentcore/runner/doc.go`

- [ ] **Step 1: 创建 resources_manager/doc.go**

按照项目 doc.go 规范编写：
- 包功能概述（中文）
- 文件目录（树形结构，不含 _test.go）
- 对应 Python 代码路径

- [ ] **Step 2: 修改 runner/doc.go**

在文件目录中添加 `resources_manager/` 子目录条目。

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/runner/resources_manager/doc.go internal/agentcore/runner/doc.go
git commit -m "docs(6.23): 添加 resources_manager doc.go，更新 runner doc.go"
```

---

## Task 10: 删除旧代码 + 迁移引用

**Files:**
- Delete: `internal/agentcore/single_agent/resource/doc.go`
- Delete: `internal/agentcore/single_agent/resource/resource_manager.go`
- Delete: `internal/agentcore/single_agent/resource/resource_manager_test.go`
- Modify: `internal/agentcore/single_agent/ability/ability_manager.go`
- Modify: `internal/agentcore/single_agent/ability/ability_manager_test.go`
- Modify: `internal/agentcore/single_agent/base.go`
- Modify: `internal/agentcore/single_agent/agents/react_agent.go`
- Modify: `internal/agentcore/single_agent/doc.go`

- [ ] **Step 1: 删除 single_agent/resource/ 目录**

```bash
git rm internal/agentcore/single_agent/resource/doc.go internal/agentcore/single_agent/resource/resource_manager.go internal/agentcore/single_agent/resource/resource_manager_test.go
```

- [ ] **Step 2: 修改 ability_manager.go**

1. 将 `resource "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/resource"` 导入替换为 `resources_manager "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"`
2. 将 `resourceMgr resource.ResourceManager` 字段类型改为 `resourceMgr *resources_manager.ResourceMgr`
3. 适配调用方法签名：
   - `am.resourceMgr.GetTool(toolID)` → `am.resourceMgr.GetTool(toolID)` 返回 `([]tool.Tool, error)`，取 `[0]`
   - `am.resourceMgr.GetWorkflow(wfID)` → `am.resourceMgr.GetWorkflow(ctx, wfID)` 返回 `([]interfaces.Workflow, error)`
   - `am.resourceMgr.GetAgent(agentID)` → `am.resourceMgr.GetAgent(ctx, agentID)` 返回 `([]interfaces.BaseAgent, error)`
   - `am.resourceMgr.GetMcpToolInfos(serverID)` → `am.resourceMgr.GetMcpToolInfos(ctx, serverID)`
4. 构造函数 `NewAbilityManager` 的 `resourceMgr` 参数类型改为 `*resources_manager.ResourceMgr`

- [ ] **Step 3: 修改 ability_manager_test.go**

1. 删除 `fakeResourceManager` 测试替身
2. 创建 `fakeResourceMgr` 基于 `*resources_manager.ResourceMgr`（或直接用 `resources_manager.NewResourceMgr()`）
3. 更新所有测试用例

- [ ] **Step 4: 修改 base.go**

删除以下 re-export 类型别名：
- `ResourceManager = resource.ResourceManager`
- `NoopResourceManager = resource.NoopResourceManager`
- `ResourceOptions = resource.ResourceOptions`
- `ResourceOption = resource.ResourceOption`
- 对应的 `WithResourceTag` / `WithResourceSession` / `NewResourceOptions`

- [ ] **Step 5: 修改 react_agent.go**

删除 `&resource.NoopResourceManager{}` 引用，构造时传入 `nil`（AbilityManager 内部 nil 检查已存在，会使用默认行为）

- [ ] **Step 6: 修改 single_agent/doc.go**

移除文件目录中 `resource/` 条目。

- [ ] **Step 7: 运行全量测试验证**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/single_agent/... -v -count=1`

- [ ] **Step 8: 提交**

```bash
git add -A internal/agentcore/single_agent/
git commit -m "refactor(6.23): 删除 single_agent/resource 占位代码，迁移至 runner/resources_manager"
```

---

## Task 11: 全量编译 + 覆盖率检查 + IMPLEMENTATION_PLAN 更新

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 全量编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./...`

- [ ] **Step 2: 覆盖率检查**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/runner/resources_manager/...`

目标：覆盖率 ≥ 85%

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md**

将 6.23 步骤的状态从 `☐` 改为 `✅`

- [ ] **Step 4: 最终提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 IMPLEMENTATION_PLAN 6.23 状态为 ✅"
```
