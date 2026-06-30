# ResourceMgr 对齐 Python 设计 spec

## 1. 背景与动机

Go 的 ResourceMgr 早期实现中，AddXxx/RemoveXxx/GetXxx 方法**手动展开**了验证→分发→缓存→标记→日志步骤，未复用 `innerAddResource`/`innerRemoveResources`/`innerGetResources` 统一入口。而 Python 的 `add_agent`/`add_workflow`/`add_agent_team`/`add_tool`/`add_model`/`add_prompt` **全部走 `_inner_add_resource`**，remove/get 同理。

同时存在以下问题：
1. **idToCard 缓存丢失类型信息**：存 `*BaseCard` 后子类信息丢失，`getCardType` 无法识别原始类型
2. **GetToolInfos 中 getCardType 传入裸 BaseCard 是 bug**：永远匹配不到，按类型过滤失效
3. **innerGetResources 和 innerGetResourcesByProvider 冗余**：Go 无 async 区分，两者逻辑几乎相同
4. **session 参数用 any**：无编译时安全，dispatchGet 内部需类型断言
5. **TeamCardInterface 独立定义**：未复用通用 CardInterface，且 GetBaseCard 方法多余

## 2. 设计目标

1. **Add/Remove/Get 全面走分发方法**，与 Python `_inner_add_resource`/`_inner_remove_resources`/`_inner_get_resources` 对齐
2. **引入 CardInterface 通用接口**，idToCard 改存接口类型，保留子类类型信息
3. **删除 GetBaseCard()**，TeamCardInterface 嵌入 CardInterface
4. **合并 innerGetResourcesByProvider → innerGetResources**
5. **session 参数改为 decorator.TracerSession**，消除 any 类型断言

## 3. CardInterface 设计

### 3.1 接口定义

在 `internal/common/schema/card.go` 中新增：

```go
// CardInterface 通用卡片只读接口，所有 Card 类型均实现此接口。
//
// BaseCard/WorkflowCard/AgentCard/ToolCard/McpToolCard/TeamCard/EventDrivenTeamCard 均满足。
// 替代各处 *BaseCard 的只读消费场景（idToCard 缓存、日志格式化、类型判断等）。
//
// 对应 Python: BaseCard 基类（Python 继承天然满足此接口的方法）。
type CardInterface interface {
    // GetID 返回唯一标识符
    GetID() string
    // GetName 返回名称
    GetName() string
    // GetDescription 返回描述信息
    GetDescription() string
    // String 返回简洁的身份描述
    String() string
    // ToolInfo 返回工具描述信息，供 LLM function calling 消费
    ToolInfo() *ToolInfo
}
```

### 3.2 编译时验证

```go
var _ CardInterface = (*BaseCard)(nil)
var _ CardInterface = (*WorkflowCard)(nil)
```

AgentCard、ToolCard、McpToolCard、TeamCard、EventDrivenTeamCard 在各自包中验证。

### 3.3 不纳入 CardInterface 的方法

| 方法 | 原因 |
|------|------|
| `GetBaseCard()` | 删除。CardInterface 的 getter 方法已覆盖只读需求，不需要返回 *BaseCard |
| `AbilityName/AbilityID/AbilityKind` | 仅部分 Card 实现（WorkflowCard/AgentCard/ToolCard），TeamCard 不实现 |
| 写访问方法（SetID/SetName 等） | CardInterface 是只读接口 |

### 3.4 CardOption / NewBaseCard 不变

`CardOption func(*BaseCard)` 和 `NewBaseCard() *BaseCard` 是构造侧，需要具体类型写访问，不变。

## 4. TeamCardInterface 改造

### 4.1 嵌入 CardInterface

```go
type TeamCardInterface interface {
    schema.CardInterface                              // 嵌入通用接口
    GetAgentCards() []*agentschema.AgentCard
    GetTopic() string
    GetVersion() string
    GetTags() []string
    GetSubscriptions() map[string][]string
}
```

### 4.2 删除 GetBaseCard()

- TeamCard 上的 `func (c *TeamCard) GetBaseCard() *schema.BaseCard` 删除
- EventDrivenTeamCard 上的 `func (c *EventDrivenTeamCard) GetBaseCard() *schema.BaseCard` 删除
- TeamCardInterface 中的 `GetBaseCard() *schema.BaseCard` 方法声明删除

原因：CardInterface 的 `GetID()/GetName()/GetDescription()` 已覆盖只读需求。原 GetBaseCard 的唯一消费方是 AddAgentTeam 传给 innerAddResource 的 resourceCard 参数，改为传 CardInterface 后直接传 TeamCardInterface 本身（满足 CardInterface）。

## 5. idToCard 改造

### 5.1 类型变更

```go
// 之前
idToCard *ThreadSafeDict[string, *schema.BaseCard]

// 之后
idToCard *ThreadSafeDict[string, schema.CardInterface]
```

### 5.2 存入策略

| AddXxx | 存入值 | 说明 |
|--------|--------|------|
| AddAgent | `card`（*AgentCard） | 满足 CardInterface |
| AddAgentTeam | `card`（TeamCardInterface） | 满足 CardInterface |
| AddWorkflow | `card`（*WorkflowCard） | 满足 CardInterface |
| AddTool | `t.Card()`（*ToolCard） | 满足 CardInterface |
| AddModel | `nil` | 无 card，Python 也不传 |
| AddPrompt | `nil` | 无 card，Python 也不传 |
| AddSysOperation | `nil` | 无 card |
| AddMcpServer | `card`（*McpToolCard） | 满足 CardInterface |

### 5.3 联动改造的方法签名

| 方法 | 改造 |
|------|------|
| `innerAddResource` 第4参数 | `*schema.BaseCard` → `schema.CardInterface` |
| `dispatchAdd` 第4参数 | 删除 resourceCard 参数（Go 不使用） |
| `resourceCardStr` 第1参数 | `*schema.BaseCard` → `schema.CardInterface` |
| `getCardType` 第1参数 | `*schema.BaseCard` → `schema.CardInterface`，改用 type switch |
| `innerValidateResourceCard` 第1参数 | `*schema.BaseCard` → `schema.CardInterface` |
| `GetResourceByTag` 返回值 | `[]*schema.BaseCard` → `[]schema.CardInterface` |
| `GetSysOpToolCards` 返回值 | `[]*schema.BaseCard` → `[]schema.CardInterface` |

### 5.4 getCardType 重构

从 `reflect.TypeOf` switch 改为 `type switch`：

```go
func getCardType(card schema.CardInterface) string {
    if card == nil {
        return ""
    }
    switch card.(type) {
    case *mcp.McpToolCard:                    return "mcp"
    case *tool.ToolCard:                      return "function"
    case *maschema.TeamCard:                  return "team"
    case *maschema.EventDrivenTeamCard:       return "team"
    case *schema.WorkflowCard:                return "workflow"
    case *agentschema.AgentCard:              return "agent"
    default:                                  return ""
    }
}
```

比 reflect.TypeOf 更简洁，且正确识别子类。

### 5.5 GetToolInfos 修复

原 bug：`getCardType(&schema.BaseCard{ID: card.ID, Name: card.Name})` 传入裸 BaseCard，永远匹配不到。

修复：从 `idToCard` 取原始 card 传给 `getCardType`：

```go
func (m *ResourceMgr) GetToolInfos(toolIDs []string, toolTypes []string, opts ...ResourceOption) ([]*schema.ToolInfo, error) {
    tools, err := m.GetTool(toolIDs, opts...)
    if err != nil {
        return nil, err
    }
    results := make([]*schema.ToolInfo, 0, len(tools))
    for _, t := range tools {
        // 从 idToCard 获取原始 card（保留完整类型信息）
        toolCard := t.Card()
        if len(toolTypes) > 0 {
            cardType := getCardType(toolCard)  // 直接传 CardInterface
            matched := false
            for _, tt := range toolTypes {
                if cardType == tt { matched = true; break }
            }
            if !matched { continue }
        }
        if info := toolCard.ToolInfo(); info != nil {
            results = append(results, info)
        }
    }
    return results, nil
}
```

## 6. Add/Remove/Get 统一走分发方法

### 6.1 Add — 全部走 innerAddResource

| AddXxx | resourceCard | 特殊处理 |
|--------|-------------|---------|
| AddAgent | `card`（*AgentCard） | 无 |
| AddAgentTeam | `card`（TeamCardInterface） | 无 |
| AddWorkflow | `card`（*WorkflowCard） | 无 |
| AddTool | `t.Card()`（*ToolCard） | Refresh 在 innerAddResource 前处理 |
| AddModel | `nil` | 无 card |
| AddPrompt | `nil` | 无 card |
| AddSysOperation | `nil` | 无 card |
| AddMcpServer | `card`（*McpToolCard） | 保持手动展开（MCP 逻辑特殊） |

示例 — AddAgent 改造后：

```go
func (m *ResourceMgr) AddAgent(card *agentschema.AgentCard, provider AgentProvider, opts ...ResourceOption) error {
    o := applyResourceOptions(opts...)
    if err := m.innerValidateResourceID(card.ID, "agent"); err != nil {
        return err
    }
    if err := m.innerValidateProvider(provider, "agent"); err != nil {
        return err
    }
    return m.innerAddResource(card.ID, "agent", provider, card, o.Tag, o.InterfaceURL)
}
```

示例 — AddTool 改造后：

```go
func (m *ResourceMgr) AddTool(t tool.Tool, opts ...ResourceOption) error {
    o := applyResourceOptions(opts...)
    toolCard := t.Card()
    toolID := toolCard.ID
    if err := m.innerValidateResourceID(toolID, "tool"); err != nil {
        return err
    }
    // refresh 前置处理（与 Python _refresh_existing_tool_if_needed 对齐）
    if o.Refresh {
        _, _ = m.registry.Tool().RemoveTool(toolID)
    }
    return m.innerAddResource(toolID, "tool", t, toolCard, o.Tag, "")
}
```

### 6.2 Remove — 全部走 innerRemoveResources

当前 Go 的 RemoveXxx 手动展开验证→find→遍历→dispatchRemove→Pop card→日志。

统一后：
- RemoveAgent → `innerRemoveResources(agentIDs, "agent", tag, tagMatchStrategy, skipIfNotExists)`
- RemoveWorkflow → 同上，resourceType="workflow"
- RemoveTool → 同上，resourceType="tool"
- RemoveModel → 同上，resourceType="model"
- RemovePrompt → 同上，resourceType="prompt"
- RemoveAgentTeam → 同上，resourceType="team"

### 6.3 Get — 统一走 innerGetResources

| GetXxx | resourceType | 传 session？ |
|--------|-------------|-------------|
| GetAgent | "agent" | 否 |
| GetAgentTeam | "team" | 否 |
| GetWorkflow | "workflow" | 是 |
| GetTool | "tool" | 是 |
| GetModel | "model" | 是 |
| GetPrompt | "prompt" | 否 |

## 7. 合并 innerGetResourcesByProvider

删除 `innerGetResourcesByProvider`，所有调用方改用 `innerGetResources`。

`innerGetResources` 补充 `len(ids) == 0` 提前返回优化（原 ByProvider 独有）。

## 8. Session 类型改造

### 8.1 参数类型变更

| 方法 | 之前 | 之后 |
|------|------|------|
| `innerGetResources` session 参数 | `any` | `decorator.TracerSession` |
| `dispatchGet` session 参数 | `any` | `decorator.TracerSession` |

### 8.2 dispatchGet 简化

```go
func (m *ResourceMgr) dispatchGet(ctx context.Context, resourceType, resourceID string, session decorator.TracerSession) (any, error) {
    switch resourceType {
    case "workflow":
        return m.registry.Workflow().GetWorkflow(ctx, resourceID, session)
    case "agent":
        return m.registry.Agent().GetAgent(ctx, resourceID)
    case "team":
        return m.registry.AgentTeam().GetAgentTeam(ctx, resourceID)
    case "tool":
        return m.registry.Tool().GetTool(resourceID, session)
    case "prompt":
        return m.registry.Prompt().GetPrompt(resourceID)
    case "model":
        return m.registry.Model().GetModel(ctx, resourceID, session)
    case "sys_operation":
        return m.registry.SysOperation().GetSysOperation(resourceID)
    default:
        return nil, exception.BuildError(...)
    }
}
```

不再需要 `session.(decorator.TracerSession)` 类型断言。

## 9. 改造影响范围

### 9.1 新增/修改文件

| 文件 | 改动 |
|------|------|
| `common/schema/card.go` | 新增 CardInterface 接口 + 编译时验证 |
| `common/schema/card_test.go` | 新增 CardInterface 满足测试 |
| `multi_agent/schema/team_card.go` | TeamCardInterface 嵌入 CardInterface，删除 GetBaseCard |
| `multi_agent/schema/team_card_test.go` | 更新测试 |
| `runner/resources_manager/resource_manager.go` | 全部改造（idToCard、Add/Remove/Get、session、getCardType 等） |
| `runner/resources_manager/resource_manager_test.go` | 更新测试 |
| `single_agent/schema/agent_card.go` | 新增编译时验证 `var _ schema.CardInterface = (*AgentCard)(nil)` |
| `foundation/tool/base.go` | 新增编译时验证 `var _ schema.CardInterface = (*ToolCard)(nil)` |
| `foundation/tool/mcp/types/types.go` | 新增编译时验证 `var _ schema.CardInterface = (*McpToolCard)(nil)` |

### 9.2 下游影响

- `GetResourceByTag` 返回值从 `[]*schema.BaseCard` 改为 `[]schema.CardInterface`，调用方需适配
- `RemoveAgent` 返回值从 `[]*agentschema.AgentCard` 不变（仍构造具体类型返回）
- `innerAddResource`/`dispatchGet` 等为非导出方法，无外部 API 影响

## 10. 不改造的内容

| 项 | 原因 |
|----|------|
| `CardOption func(*BaseCard)` | 构造侧需要写访问 |
| `NewBaseCard() *BaseCard` | 构造函数必须返回具体类型 |
| `AddMcpServer` | MCP 逻辑特殊（批量添加、serverConfig 处理），保持手动展开 |
| `McpServerConfig` | 不嵌入 BaseCard，不实现 CardInterface |
| `BaseCard.ToolInfo()` | 保留默认 nil 实现，子类各自覆写 |
