# ResourceMgr 对齐 Python 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** ResourceMgr 全面对齐 Python，引入 CardInterface 通用接口，统一 Add/Remove/Get 走分发方法，消除冗余和 bug。

**Architecture:** 定义 CardInterface 只读接口替代 `*BaseCard` 的只读消费场景；idToCard 改存 CardInterface 保留子类类型信息；Add/Remove/Get 统一走 innerAddResource/innerRemoveResources/innerGetResources；合并 innerGetResourcesByProvider；session 改为 decorator.TracerSession。

**Tech Stack:** Go 1.23+, 标准库 reflect/type switch, 现有 ThreadSafeDict 泛型

---

## 文件结构

| 文件 | 职责 | 操作 |
|------|------|------|
| `internal/common/schema/card.go` | CardInterface 接口定义 + BaseCard/WorkflowCard 编译时验证 | 修改 |
| `internal/common/schema/card_test.go` | CardInterface 满足测试 | 修改 |
| `internal/agentcore/multi_agent/schema/team_card.go` | TeamCardInterface 嵌入 CardInterface，删除 GetBaseCard | 修改 |
| `internal/agentcore/multi_agent/schema/team_card_test.go` | 删除 GetBaseCard 测试，更新接口测试 | 修改 |
| `internal/agentcore/single_agent/schema/agent_card.go` | AgentCard 编译时验证 | 修改 |
| `internal/agentcore/foundation/tool/base.go` | ToolCard 编译时验证 | 修改 |
| `internal/agentcore/foundation/tool/mcp/types/types.go` | McpToolCard 编译时验证（注意 ToolInfo 签名差异） | 修改 |
| `internal/agentcore/runner/resources_manager/resource_manager.go` | 核心改造：idToCard、Add/Remove/Get、session、getCardType 等 | 修改 |
| `internal/agentcore/runner/resources_manager/resource_manager_test.go` | 更新测试 | 修改 |
| `internal/agentcore/runner/resources_manager/base.go` | AgentTeamEntry.Card 类型变更 | 修改 |

---

### Task 1: 定义 CardInterface 接口

**Files:**
- Modify: `internal/common/schema/card.go`
- Modify: `internal/common/schema/card_test.go`

- [x] **Step 1: 在 card.go 结构体区块添加 CardInterface 接口定义**

在 Ability 接口之后、BaseCard struct 之前添加：

```go
// CardInterface 通用卡片只读接口，所有 Card 类型均实现此接口。
//
// BaseCard/WorkflowCard/AgentCard/ToolCard/McpToolCard/TeamCard/EventDrivenTeamCard 均满足。
// 替代各处 *BaseCard 的只读消费场景（idToCard 缓存、日志格式化、类型判断等）。
//
// 不包含 ToolInfo()：McpToolCard.ToolInfo() 返回 *McpToolInfo（非 *ToolInfo），
// 签名不兼容。ToolInfo 通过具体 Card 类型直接调用获取。
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
}
```

- [x] **Step 2: 在 card.go 全局变量区块添加编译时验证**

```go
// 编译时验证 BaseCard 满足 CardInterface。
var _ CardInterface = (*BaseCard)(nil)

// 编译时验证 WorkflowCard 满足 CardInterface。
var _ CardInterface = (*WorkflowCard)(nil)
```

- [x] **Step 3: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/common/schema/`
Expected: PASS

- [x] **Step 4: 在 card_test.go 添加 CardInterface 满足测试**

```go
// TestBaseCard_满足CardInterface 验证 *BaseCard 满足 CardInterface。
func TestBaseCard_满足CardInterface(t *testing.T) {
	card := NewBaseCard(WithID("test-id"), WithName("test-name"))
	var iface CardInterface = card
	if iface.GetID() != "test-id" {
		t.Errorf("GetID() = %q, want %q", iface.GetID(), "test-id")
	}
	if iface.GetName() != "test-name" {
		t.Errorf("GetName() = %q, want %q", iface.GetName(), "test-name")
	}
}

// TestWorkflowCard_满足CardInterface 验证 *WorkflowCard 满足 CardInterface。
func TestWorkflowCard_满足CardInterface(t *testing.T) {
	card := NewWorkflowCard(WithID("wf-1"), WithName("wf-name"))
	var iface CardInterface = card
	if iface.GetID() != "wf-1" {
		t.Errorf("GetID() = %q, want %q", iface.GetID(), "wf-1")
	}
}
```

- [x] **Step 5: 运行测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -v ./internal/common/schema/ -run "TestBaseCard_满足CardInterface|TestWorkflowCard_满足CardInterface" -count=1`
Expected: PASS

- [x] **Step 6: Commit**

```bash
git add internal/common/schema/card.go internal/common/schema/card_test.go
git commit -m "feat(schema): 新增 CardInterface 通用只读接口"
```

---

### Task 2: 其他 Card 类型添加编译时验证

**Files:**
- Modify: `internal/agentcore/single_agent/schema/agent_card.go`
- Modify: `internal/agentcore/foundation/tool/base.go`
- Modify: `internal/agentcore/foundation/tool/mcp/types/types.go`

- [x] **Step 1: AgentCard 添加编译时验证**

在 `agent_card.go` 的全局变量区块添加：

```go
// 编译时验证 AgentCard 满足 schema.CardInterface。
var _ schema.CardInterface = (*AgentCard)(nil)
```

- [x] **Step 2: ToolCard 添加编译时验证**

在 `base.go` 的全局变量区块添加：

```go
// 编译时验证 ToolCard 满足 schema.CardInterface。
var _ schema.CardInterface = (*ToolCard)(nil)
```

- [x] **Step 3: McpToolCard 添加编译时验证**

注意：McpToolCard.ToolInfo() 返回 *McpToolInfo，不满足 CardInterface（CardInterface 不含 ToolInfo）。McpToolCard 通过嵌入 ToolCard 继承了 GetID/GetName/GetDescription/String 方法。

在 `types.go` 的全局变量区块添加：

```go
// 编译时验证 McpToolCard 满足 schema.CardInterface。
var _ schema.CardInterface = (*McpToolCard)(nil)
```

- [x] **Step 4: 编译验证全部三个包**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/single_agent/schema/ && go build ./internal/agentcore/foundation/tool/ && go build ./internal/agentcore/foundation/tool/mcp/types/`
Expected: PASS（编译时验证通过说明接口满足）

- [x] **Step 5: Commit**

```bash
git add internal/agentcore/single_agent/schema/agent_card.go internal/agentcore/foundation/tool/base.go internal/agentcore/foundation/tool/mcp/types/types.go
git commit -m "feat: AgentCard/ToolCard/McpToolCard 添加 CardInterface 编译时验证"
```

---

### Task 3: TeamCardInterface 嵌入 CardInterface + 删除 GetBaseCard

**Files:**
- Modify: `internal/agentcore/multi_agent/schema/team_card.go`
- Modify: `internal/agentcore/multi_agent/schema/team_card_test.go`

- [x] **Step 1: TeamCardInterface 嵌入 schema.CardInterface，删除 GetBaseCard 声明**

将 TeamCardInterface 从：

```go
type TeamCardInterface interface {
	// ── BaseCard 层 ──
	GetID() string
	GetName() string
	GetDescription() string
	GetBaseCard() *schema.BaseCard

	// ── TeamCard 层 ──
	GetAgentCards() []*agentschema.AgentCard
	GetTopic() string
	GetVersion() string
	GetTags() []string

	// ── EventDrivenTeamCard 层（TeamCard 返回 nil）──
	GetSubscriptions() map[string][]string

	// ── 通用 ──
	String() string
}
```

改为：

```go
type TeamCardInterface interface {
	// ── 通用（嵌入 CardInterface）──
	schema.CardInterface

	// ── TeamCard 层 ──
	GetAgentCards() []*agentschema.AgentCard
	GetTopic() string
	GetVersion() string
	GetTags() []string

	// ── EventDrivenTeamCard 层（TeamCard 返回 nil）──
	GetSubscriptions() map[string][]string
}
```

- [x] **Step 2: 删除 TeamCard.GetBaseCard() 和 EventDrivenTeamCard.GetBaseCard() 方法**

删除以下两行：

```go
// GetBaseCard 返回嵌入的 BaseCard 指针。
func (c *TeamCard) GetBaseCard() *schema.BaseCard { return &c.BaseCard }
```

```go
// GetBaseCard 返回嵌入的 BaseCard 指针。
func (c *EventDrivenTeamCard) GetBaseCard() *schema.BaseCard { return &c.TeamCard.BaseCard }
```

- [x] **Step 3: 更新 TeamCard 编译时验证（CardInterface）**

确保已有：
```go
var _ TeamCardInterface = (*TeamCard)(nil)
var _ TeamCardInterface = (*EventDrivenTeamCard)(nil)
```

新增 CardInterface 验证：
```go
// 编译时验证 TeamCard 满足 schema.CardInterface。
var _ schema.CardInterface = (*TeamCard)(nil)

// 编译时验证 EventDrivenTeamCard 满足 schema.CardInterface。
var _ schema.CardInterface = (*EventDrivenTeamCard)(nil)
```

- [x] **Step 4: 更新测试文件**

1. 删除 `TestTeamCard_GetBaseCard` 测试函数
2. 删除 `TestEventDrivenTeamCard_GetBaseCard` 测试函数
3. 删除 `TestTeamCardInterface_TeamCard满足接口` 和 `TestTeamCardInterface_EventDrivenTeamCard满足接口` 中的 `GetBaseCard()` 断言行
4. 新增 `TestTeamCard_满足CardInterface` 和 `TestEventDrivenTeamCard_满足CardInterface` 测试

- [x] **Step 5: 编译并运行测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/multi_agent/schema/ && go test ./internal/agentcore/multi_agent/schema/ -count=1`
Expected: PASS

- [x] **Step 6: Commit**

```bash
git add internal/agentcore/multi_agent/schema/team_card.go internal/agentcore/multi_agent/schema/team_card_test.go
git commit -m "refactor: TeamCardInterface 嵌入 CardInterface，删除 GetBaseCard"
```

---

### Task 4: ResourceMgr 核心改造 — idToCard + 内部方法签名

**Files:**
- Modify: `internal/agentcore/runner/resources_manager/resource_manager.go`

- [x] **Step 1: idToCard 类型从 `*schema.BaseCard` 改为 `schema.CardInterface`**

1. `ResourceMgr` 结构体中：`idToCard *ThreadSafeDict[string, *schema.BaseCard]` → `idToCard *ThreadSafeDict[string, schema.CardInterface]`
2. `NewResourceMgr` 中：`idToCard: NewThreadSafeDict[string, *schema.BaseCard]()` → `idToCard: NewThreadSafeDict[string, schema.CardInterface]()`
3. `Release` 中：`m.idToCard = NewThreadSafeDict[string, *schema.BaseCard]()` → `m.idToCard = NewThreadSafeDict[string, schema.CardInterface]()`

- [x] **Step 2: innerAddResource 签名变更**

`func (m *ResourceMgr) innerAddResource(resourceID, resourceType string, resource any, resourceCard *schema.BaseCard, tag Tag, interfaceURL string) error`

改为：

`func (m *ResourceMgr) innerAddResource(resourceID, resourceType string, resource any, resourceCard schema.CardInterface, tag Tag, interfaceURL string) error`

内部逻辑不变（resourceCard 用 .String()、nil 检查等）。

- [x] **Step 3: dispatchAdd 删除 resourceCard 参数**

`func (m *ResourceMgr) dispatchAdd(resourceType, resourceID string, resource any, resourceCard *schema.BaseCard, interfaceURL string) error`

改为：

`func (m *ResourceMgr) dispatchAdd(resourceType, resourceID string, resource any, interfaceURL string) error`

- innerAddResource 中调用处更新：`m.dispatchAdd(resourceType, resourceID, resource, resourceCard, interfaceURL)` → `m.dispatchAdd(resourceType, resourceID, resource, interfaceURL)`

- [x] **Step 4: resourceCardStr 签名变更**

`func resourceCardStr(card *schema.BaseCard, resourceID string) string` → `func resourceCardStr(card schema.CardInterface, resourceID string) string`

- [x] **Step 5: getCardType 从 reflect.TypeOf 改为 type switch**

```go
func getCardType(card schema.CardInterface) string {
	if card == nil {
		return ""
	}
	switch card.(type) {
	case *mcp.McpToolCard:
		return "mcp"
	case *tool.ToolCard:
		return "function"
	case *maschema.TeamCard:
		return "team"
	case *maschema.EventDrivenTeamCard:
		return "team"
	case *schema.WorkflowCard:
		return "workflow"
	case *agentschema.AgentCard:
		return "agent"
	default:
		return ""
	}
}
```

注意：删除 `reflect` 包中对 `getCardType` 相关的 case（`reflect.TypeOf((*maschema.TeamCard)(nil))` 等），可评估是否完全移除 reflect import。

- [x] **Step 6: innerValidateResourceCard 签名变更**

`func innerValidateResourceCard(card *schema.BaseCard, resourceType string, cardClassType reflect.Type) error` → `func innerValidateResourceCard(card schema.CardInterface, resourceType string, cardClassType reflect.Type) error`

内部 `card == nil` 判断不变，`reflect.TypeOf(card)` 在接口值上仍可获取底层动态类型。

- [x] **Step 7: GetResourceByTag 返回值变更**

`func (m *ResourceMgr) GetResourceByTag(tag Tag) []*schema.BaseCard` → `func (m *ResourceMgr) GetResourceByTag(tag Tag) []schema.CardInterface`

内部：`results := make([]*schema.BaseCard, 0, ...)` → `results := make([]schema.CardInterface, 0, ...)`

- [x] **Step 8: GetSysOpToolCards 返回值变更**

`func (m *ResourceMgr) GetSysOpToolCards(...) ([]*schema.BaseCard, error)` → `func (m *ResourceMgr) GetSysOpToolCards(...) ([]schema.CardInterface, error)`

- [x] **Step 9: session 类型从 any 改为 decorator.TracerSession**

1. `innerGetResources` 签名：`session any` → `session decorator.TracerSession`
2. `dispatchGet` 签名：`session any` → `session decorator.TracerSession`
3. dispatchGet 内部删除类型断言 `s, _ := session.(decorator.TracerSession)`，直接传 `session`

dispatchGet 简化后：
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
		return nil, exception.BuildError(exception.StatusResourceGetError,
			exception.WithParam("resource_type", resourceType),
			exception.WithParam("resource_id", resourceID),
			exception.WithParam("reason", fmt.Sprintf("不支持的资源类型: %s", resourceType)),
		)
	}
}
```

- [x] **Step 10: 合并 innerGetResourcesByProvider → innerGetResources**

1. 删除 `innerGetResourcesByProvider` 方法整体
2. 在 `innerGetResources` 中添加 `len(ids) == 0` 提前返回（原 ByProvider 独有）
3. 所有调用 `innerGetResourcesByProvider` 的地方改为调用 `innerGetResources`

- [x] **Step 11: AddMcpServer 中 idToCard.Set 改为存 McpToolCard 本身**

原：`m.idToCard.Set(card.ID, baseCard)` 其中 `baseCard := &schema.BaseCard{ID: card.ID, Name: card.Name, Description: card.Description}`

改为：`m.idToCard.Set(card.ID, card)` 直接存 `*McpToolCard`

- [x] **Step 12: GetToolInfos 修复 — 使用 ToolCard 本身调用 getCardType**

原 bug 行：`cardType := getCardType(&schema.BaseCard{ID: card.ID, Name: card.Name})`

改为：`cardType := getCardType(toolCard)` 其中 `toolCard := t.Card()`

整体 GetToolInfos 逻辑调整：

```go
func (m *ResourceMgr) GetToolInfos(toolIDs []string, toolTypes []string, opts ...ResourceOption) ([]*schema.ToolInfo, error) {
	tools, err := m.GetTool(toolIDs, opts...)
	if err != nil {
		return nil, err
	}
	results := make([]*schema.ToolInfo, 0, len(tools))
	for _, t := range tools {
		toolCard := t.Card()
		// 按类型过滤（与 Python 对齐）
		if len(toolTypes) > 0 {
			cardType := getCardType(toolCard)
			matched := false
			for _, tt := range toolTypes {
				if cardType == tt {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		if info := toolCard.ToolInfo(); info != nil {
			results = append(results, info)
		}
	}
	return results, nil
}
```

- [x] **Step 13: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/runner/resources_manager/`
Expected: PASS

- [x] **Step 14: Commit**

```bash
git add internal/agentcore/runner/resources_manager/resource_manager.go
git commit -m "refactor: ResourceMgr 核心改造 — idToCard CardInterface + session + getCardType + 合并 innerGet"
```

---

### Task 5: AddXxx 统一走 innerAddResource

**Files:**
- Modify: `internal/agentcore/runner/resources_manager/resource_manager.go`

- [x] **Step 1: AddAgent 改造**

将手动展开改为调用 innerAddResource：

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

- [x] **Step 2: AddWorkflow 改造**

```go
func (m *ResourceMgr) AddWorkflow(card *schema.WorkflowCard, provider WorkflowProvider, opts ...ResourceOption) error {
	o := applyResourceOptions(opts...)
	if err := m.innerValidateResourceID(card.ID, "workflow"); err != nil {
		return err
	}
	if err := m.innerValidateProvider(provider, "workflow"); err != nil {
		return err
	}
	return m.innerAddResource(card.ID, "workflow", provider, card, o.Tag, "")
}
```

- [x] **Step 3: AddTool 改造**

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

- [x] **Step 4: AddModel 改造**

```go
func (m *ResourceMgr) AddModel(modelID string, provider ModelProvider, opts ...ResourceOption) error {
	o := applyResourceOptions(opts...)
	if err := m.innerValidateResourceID(modelID, "model"); err != nil {
		return err
	}
	if err := m.innerValidateProvider(provider, "model"); err != nil {
		return err
	}
	return m.innerAddResource(modelID, "model", provider, nil, o.Tag, "")
}
```

- [x] **Step 5: AddPrompt 改造**

```go
func (m *ResourceMgr) AddPrompt(promptID string, template *prompt.PromptTemplate, opts ...ResourceOption) error {
	o := applyResourceOptions(opts...)
	if err := m.innerValidateResourceID(promptID, "prompt"); err != nil {
		return err
	}
	return m.innerAddResource(promptID, "prompt", template, nil, o.Tag, "")
}
```

注意：AddPrompt 原来没有 validateProvider，Python 也没有。Go 原来 validate 的是 template 本身（innerValidateResource），这里简化后只 validate ID。

- [x] **Step 6: AddSysOperation 改造**

```go
func (m *ResourceMgr) AddSysOperation(sysOperationID string, instance any, opts ...ResourceOption) error {
	o := applyResourceOptions(opts...)
	if err := m.innerValidateResourceID(sysOperationID, "sys_operation"); err != nil {
		return err
	}
	return m.innerAddResource(sysOperationID, "sys_operation", instance, nil, o.Tag, "")
}
```

- [x] **Step 7: AddAgentTeam 改造**

当前已走 innerAddResource，但 resourceCard 参数从 `card.GetBaseCard()` 改为 `card`（TeamCardInterface 满足 CardInterface）：

```go
func (m *ResourceMgr) AddAgentTeam(card maschema.TeamCardInterface, provider multiagents.AgentTeamProvider, opts ...ResourceOption) error {
	if err := m.innerValidateProvider(provider, "team"); err != nil {
		return err
	}
	return m.innerAddResource(card.GetID(), "team", provider, card, "", "")
}
```

- [x] **Step 8: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/runner/resources_manager/`
Expected: PASS

- [x] **Step 9: Commit**

```bash
git add internal/agentcore/runner/resources_manager/resource_manager.go
git commit -m "refactor: AddXxx 统一走 innerAddResource，对齐 Python"
```

---

### Task 6: RemoveXxx 统一走 innerRemoveResources

**Files:**
- Modify: `internal/agentcore/runner/resources_manager/resource_manager.go`

- [x] **Step 1: RemoveAgent 改造**

将手动展开的验证→find→遍历→dispatchRemove→Pop→日志 改为调用 innerRemoveResources，然后将 `[]any` 结果转为 `[]*agentschema.AgentCard`。

```go
func (m *ResourceMgr) RemoveAgent(agentIDs []string, opts ...ResourceOption) ([]*agentschema.AgentCard, error) {
	o := applyResourceOptions(opts...)
	results, err := m.innerRemoveResources(agentIDs, "agent", o.Tag, o.TagMatchStrategy, o.SkipIfTagNotExists)
	if err != nil {
		return nil, err
	}
	removed := make([]*agentschema.AgentCard, 0, len(results))
	for _, r := range results {
		if card, ok := r.(schema.CardInterface); ok && card != nil {
			removed = append(removed, &agentschema.AgentCard{
				BaseCard: schema.BaseCard{ID: card.GetID(), Name: card.GetName(), Description: card.GetDescription()},
			})
		}
	}
	return removed, nil
}
```

- [x] **Step 2: RemoveWorkflow 改造**

同 RemoveAgent 模式，结果转为 `[]*schema.WorkflowCard`。

- [x] **Step 3: RemoveTool 改造**

当前返回 `[]string`（工具 ID 列表），统一后 innerRemoveResources 根据 `idReturnTypes["tool"]` 返回 ID 或 card。检查 Python 的 remove_tool 返回值：返回 ToolCard。但 Go 当前返回 `[]string`。

保持 Go 当前返回值 `[]string` 不变，innerRemoveResources 中 `idReturnTypes["tool"]` 配置为返回 ID。

- [x] **Step 4: RemoveModel / RemovePrompt / RemoveSysOperation 改造**

统一走 innerRemoveResources，各自转换返回类型。

- [x] **Step 5: RemoveAgentTeam 改造**

当前实现直接调 registry.AgentTeam().RemoveAgentTeam()，统一走 innerRemoveResources。

- [x] **Step 6: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/runner/resources_manager/`
Expected: PASS

- [x] **Step 7: Commit**

```bash
git add internal/agentcore/runner/resources_manager/resource_manager.go
git commit -m "refactor: RemoveXxx 统一走 innerRemoveResources，对齐 Python"
```

---

### Task 7: GetXxx 统一走 innerGetResources

**Files:**
- Modify: `internal/agentcore/runner/resources_manager/resource_manager.go`

- [x] **Step 1: GetAgent 改造**

```go
func (m *ResourceMgr) GetAgent(ctx context.Context, agentIDs []string, opts ...ResourceOption) ([]interfaces.BaseAgent, error) {
	o := applyResourceOptions(opts...)
	results, err := m.innerGetResources(ctx, agentIDs, "agent", o.Tag, o.TagMatchStrategy, o.Session)
	if err != nil {
		return nil, err
	}
	agents := make([]interfaces.BaseAgent, 0, len(results))
	for _, r := range results {
		if a, ok := r.(interfaces.BaseAgent); ok {
			agents = append(agents, a)
		}
	}
	return agents, nil
}
```

- [x] **Step 2: GetWorkflow / GetTool / GetModel / GetPrompt / GetAgentTeam 改造**

同 GetAgent 模式，各自调用 innerGetResources 并转换返回类型。

- [x] **Step 3: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/runner/resources_manager/`
Expected: PASS

- [x] **Step 4: Commit**

```bash
git add internal/agentcore/runner/resources_manager/resource_manager.go
git commit -m "refactor: GetXxx 统一走 innerGetResources，对齐 Python"
```

---

### Task 8: 下游影响修复 + base.go 适配

**Files:**
- Modify: `internal/agentcore/runner/resources_manager/base.go`
- Other: 查找并修复所有 GetResourceByTag 返回值变更的下游调用方

- [x] **Step 1: 修复 AgentTeamEntry.Card 类型**

在 `base.go` 中，如果 `AgentTeamEntry.Card` 的类型从 `*schema.BaseCard` 或类似引用需要适配，更新为 `schema.CardInterface`。

- [x] **Step 2: 查找 GetResourceByTag 下游调用方**

Run: `cd /home/opensource/uap-claw-go && grep -rn "GetResourceByTag" --include="*.go" | grep -v "_test.go" | grep -v "resource_manager.go"`

修复所有调用方：将 `[]*schema.BaseCard` 改为 `[]schema.CardInterface`。

- [x] **Step 3: 编译验证全部受影响包**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/runner/...`
Expected: PASS

- [x] **Step 4: Commit**

```bash
git add -A
git commit -m "fix: 修复 CardInterface 下游影响"
```

---

### Task 9: 测试修复 + 全量验证

**Files:**
- Modify: `internal/agentcore/runner/resources_manager/resource_manager_test.go`
- Modify: `internal/common/schema/card_test.go`
- Modify: `internal/agentcore/multi_agent/schema/team_card_test.go`

- [x] **Step 1: 更新 resource_manager_test.go**

1. 所有 `*schema.BaseCard` 构造（如 `&schema.BaseCard{ID: ..., Name: ...}`）改为传具体 Card 类型
2. `getCardType` 测试改为传 CardInterface 参数
3. `innerValidateResourceCard` 测试改为传 CardInterface 参数
4. `GetResourceByTag` 测试返回值从 `[]*schema.BaseCard` 改为 `[]schema.CardInterface`
5. `innerGetResourcesByProvider` 相关测试删除或合并到 innerGetResources 测试
6. session 相关测试从 `any` 改为 `decorator.TracerSession`

- [x] **Step 2: 运行 ResourceMgr 测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -v ./internal/agentcore/runner/resources_manager/ -count=1`
Expected: PASS

- [x] **Step 3: 运行全量构建和测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./... && go test ./internal/common/schema/ ./internal/agentcore/multi_agent/schema/ ./internal/agentcore/runner/resources_manager/ ./internal/agentcore/single_agent/schema/ ./internal/agentcore/foundation/tool/ ./internal/agentcore/foundation/tool/mcp/types/ -count=1`
Expected: PASS

- [x] **Step 4: Commit**

```bash
git add -A
git commit -m "test: 更新 ResourceMgr 对齐改造后的全部测试"
```

---

### Task 10: 更新 doc.go + IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `internal/common/schema/doc.go`
- Modify: `internal/agentcore/multi_agent/schema/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [x] **Step 1: 更新 common/schema/doc.go**

在包功能概述中提及 CardInterface 接口，在文件目录中确认 card.go 的职责描述。

- [x] **Step 2: 更新 multi_agent/schema/doc.go**

提及 TeamCardInterface 嵌入 schema.CardInterface，删除 GetBaseCard 相关描述。

- [x] **Step 3: 更新 IMPLEMENTATION_PLAN.md**

将本次改造相关的步骤标记为完成（✅）。

- [x] **Step 4: Commit**

```bash
git add -A
git commit -m "docs: 更新 doc.go 和 IMPLEMENTATION_PLAN.md"
```
