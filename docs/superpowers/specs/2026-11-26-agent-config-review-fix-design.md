# 10.3.13 AgentConfigService 实现审查修复设计

## 背景

10.3.13 AgentConfigService 已完成首次实现并提交（commit `e6665cb0`），审查发现若干偏差和 `any`/`interface{}` 弱类型问题，需修复。

## 决策汇总

| # | 问题 | 决策 | 优先级 |
|---|------|------|--------|
| 1 | 三层冗余结构体（runtime.AgentDefinition / runtime.CustomAgentDef / adapter.AgentDefinition） | 提取共享包 `swarm/server/types`，AgentDefinition 放独立包 | 高 |
| 2 | ListAvailableTools 缺 `disallowed_for_subagents`、硬编码、返回 `map[string]any` | 定义结构体返回类型 + 补字段 + 对齐分组名，`TOOL_GROUPS`/`DISALLOWED_FOR_SUBAGENTS` 标记 `⤵️ 10.3.7-11` 待回填 | 高 |
| 3 | `formatAgentFile(params any)` 用 any 做多态 | 只接受 `*AgentDefinition`，create_agent 先构造再写文件 | 中 |
| 4 | `agentDefinitionToMap` 手写 40 行逐字段转换 | json.Marshal→Unmarshal 替代，3 行代码 | 中 |
| 5 | `resolveLocationDir` 静默回退而非报错 | 对齐 Python 返回 error | 低 |
| 6 | DeepAdapter 5 个 `interface{}` 占位字段 | 暂不处理（均无实现，stub 合理） | — |
| 7 | YAML 配置解析 `map[string]any` 断言链 | 暂不处理（配置层固有设计） | — |

---

## 修复 1：提取共享包 `swarm/server/types`

### 问题

Python 只有一个 `AgentDefinition`（定义在 `agent_config_service.py`），`interface_deep.py` 通过函数内延迟 import 直接使用。Go 因为 adapter↔runtime 循环依赖，搞了三份：

```
runtime.AgentDefinition  (17 字段，含 json/yaml tag，Source 是 AgentSource 枚举)
runtime.CustomAgentDef   (11 字段，无 tag，Source 是 string)
adapter.AgentDefinition  (11 字段，无 tag，Source 是 string，与 CustomAgentDef 完全一致)
```

每新增字段需同步修改三处，维护负担大。

### 方案

新建 `swarm/server/types` 包，放置共享的 `AgentDefinition`：

```go
// Package types 提供 server 子包间共享的类型定义，避免循环依赖。
package types

// AgentDefinition Agent 定义数据模型。
// 对齐 Python: jiuwenswarm/server/runtime/agent_config_service.py AgentDefinition dataclass
type AgentDefinition struct {
    Name            string   `json:"name" yaml:"name"`
    Description     string   `json:"description" yaml:"description"`
    Prompt          string   `json:"prompt" yaml:"-"`
    Source          string   `json:"source" yaml:"-"`
    FilePath        string   `json:"file_path,omitempty" yaml:"-"`
    Model           string   `json:"model,omitempty" yaml:"model,omitempty"`
    Tools           []string `json:"tools" yaml:"tools,omitempty"`
    DisallowedTools []string `json:"disallowed_tools,omitempty" yaml:"disallowed_tools,omitempty"`
    Color           string   `json:"color,omitempty" yaml:"color,omitempty"`
    PermissionMode  string   `json:"permission_mode,omitempty" yaml:"permission_mode,omitempty"`
    MemoryScope     string   `json:"memory_scope,omitempty" yaml:"memory_scope,omitempty"`
    ShadowedBy      string   `json:"shadowed_by,omitempty" yaml:"-"`
    Enabled         *bool    `json:"enabled,omitempty" yaml:"-"`
    WhenToUse       string   `json:"when_to_use,omitempty" yaml:"when_to_use,omitempty"`
    MaxIterations   *int     `json:"max_iterations,omitempty" yaml:"max_iterations,omitempty"`
    Skills          []string `json:"skills,omitempty" yaml:"skills,omitempty"`
}
```

**设计决策**：
- `Source` 用 `string` 而非 `AgentSource` 枚举：共享包不应引入枚举约束，枚举定义留在 `runtime` 包
- `ShadowedBy` 用 `string`：同理
- json/yaml tag 保留：runtime 和 handle_agents 都需要序列化
- 同时将 `AgentSource` 常量和 `BuiltinAgents` 也移入此包（因为 handle_agents.go 和 runtime 包都需要）

### 改动范围

| 文件 | 改动 |
|------|------|
| `swarm/server/types/doc.go` | 新建，包文档 |
| `swarm/server/types/agent_definition.go` | 新建，AgentDefinition + AgentSource + BuiltinAgents |
| `swarm/server/runtime/agent_config.go` | 删除 `AgentDefinition`、`CustomAgentDef`、`AgentSource`、`BuiltinAgents` 定义，改为导入 types |
| `swarm/server/runtime/agent_config_test.go` | 导入 types |
| `swarm/server/adapter/deep_adapter_config.go` | 删除 `adapter.AgentDefinition` 和 `AgentConfigLister`，改为导入 types |
| `swarm/server/adapter/deep_adapter.go` | `configLister` 字段类型改为 `AgentConfigLister`（接口留在 adapter） |
| `swarm/server/runtime/uapclaw.go` | 删除 `agentConfigListerBridge` 中的字段拷贝逻辑，直接返回切片 |
| `swarm/server/handle_agents.go` | 导入 types |

### 循环依赖验证

```
types  ← runtime  (runtime 导入 types)
types  ← adapter  (adapter 导入 types)
runtime → adapter (uapclaw.go 导入 adapter，注入 configLister)
adapter ↛ runtime (adapter 不导入 runtime)
```

无循环依赖。

---

## 修复 2：ListAvailableTools 结构化

### 问题

1. 缺失 `disallowed_for_subagents` 字段（Python 返回了）
2. 工具列表硬编码，无法动态扩展
3. 返回 `map[string]any` 无类型安全
4. 分组名与 Python 不一致：Go 是 `文件/搜索/高级/多模态`，Python 是 `核心/搜索/代码智能/高级/可视化`

### 方案

```go
// ToolInfo 工具信息
type ToolInfo struct {
    Name         string `json:"name"`
    InternalName string `json:"internal_name"`
    Description  string `json:"description"`
    Group        string `json:"group"`
}

// AvailableToolsResult 可用工具查询结果
type AvailableToolsResult struct {
    Tools                  []ToolInfo `json:"tools"`
    Groups                 []string   `json:"groups"`
    DisallowedForSubagents []string   `json:"disallowed_for_subagents"`
}
```

`ListAvailableTools` 返回 `*AvailableToolsResult`。

`TOOL_GROUPS` 和 `DISALLOWED_FOR_SUBAGENTS` 当前硬编码在方法内部，用注释标记 `⤵️ 10.3.7-11` 待回填（等 code_agent_rail 实现后动态化）。

分组名对齐 Python：`核心/搜索/代码智能/高级/可视化`。

---

## 修复 3：formatAgentFile 只接受 *AgentDefinition

### 问题

Python 的 `_format_agent_file` 接受 `CreateAgentParams | AgentDefinition`，Go 用 `any` 模拟联合类型，两个 case 分支代码完全相同（约 30 行重复）。

### 方案

`formatAgentFile` 签名改为 `func formatAgentFile(def *types.AgentDefinition) string`，只保留一个分支。

`CreateAgent` 调整步骤顺序：
1. 参数校验（不变）
2. 检查重名（不变）
3. 构造 `AgentDefinition`（原步骤 6 提前）
4. `formatAgentFile(def)` 写文件（原步骤 4）
5. 记录日志
6. 返回 def

---

## 修复 4：agentDefinitionToMap 用 json.Marshal→Unmarshal

### 问题

`agentDefinitionToMap` 手写 40 行逐字段转 `map[string]any`，硬编码键名，加字段需手动同步。

### 方案

```go
func agentDefinitionToMap(a *types.AgentDefinition) map[string]any {
    data, _ := json.Marshal(a)
    var m map[string]any
    json.Unmarshal(data, &m)
    return m
}
```

3 行代码，自动跟随 json tag 的 `omitempty` 规则。

---

## 修复 5：resolveLocationDir 对齐 Python 返回 error

### 问题

Python 对无效 location 抛 `ValueError`，Go 对 default case 静默回退到 `local`。

### 方案

```go
func (s *AgentConfigService) resolveLocationDir(location string) (string, error) {
    switch location {
    case AgentSourceUser:
        return s.userAgentsDir(), nil
    case AgentSourceProject:
        return s.projectAgentsDir(), nil
    case AgentSourceLocal:
        return s.localAgentsDir(), nil
    default:
        return "", fmt.Errorf("无效的 location: %s，有效值: user, project, local", location)
    }
}
```

调用方需处理 error。

---

## 不修复项

| # | 问题 | 原因 |
|---|------|------|
| 6 | DeepAdapter 5 个 `interface{}` 占位字段 | 均无具体实现，stub 标注了 `⤵️` 回填章节，合理 |
| 7 | YAML `map[string]any` 断言链 | 配置层固有设计，Python 同样使用 dict + isinstance |
