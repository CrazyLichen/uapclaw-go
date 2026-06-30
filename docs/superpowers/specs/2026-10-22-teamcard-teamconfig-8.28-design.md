# 8.28 TeamCard / TeamConfig 完整实现设计

## 概述

实现步骤 8.28：TeamCard（团队身份卡片）和 TeamConfig（团队运行时配置）的完整实现，严格对齐 Python 源码的文件组织结构和字段定义。

## 流程位置与作用

### 在 Agent 会话流程中的位置

8.28 位于领域八「多 Agent 团队」子分组的第 2 步：

```
8.27 ✅  BaseTeam 接口       — 团队行为契约（已完成）
8.28 ☐  TeamCard / TeamConfig — 团队元数据与配置（当前实现）
8.29 ☐  EventDrivenTeamCard  — 事件驱动团队卡片（依赖 8.28）
8.30 ☐  TeamRuntime/Session  — 团队运行时与会话
8.31 ☐  HandoffTeam          — 交接团队
8.32 ☐  HierarchicalTeam     — 层级团队
```

### 作用

- **TeamCard**：团队的不可变身份名片，描述"团队是什么"和"由谁组成"。被资源管理器 `AgentTeamMgr` 用于注册/查找团队，被 `EventDrivenTeamCard`(8.29)、`HandoffTeam`(8.31) 等继承/嵌入。
- **TeamConfig**：团队的可变运行时配置，描述"团队怎么运行"。被 `BaseTeam.Configure()` 使用，控制最大 Agent 数、并发数、超时等。

## 一、文件组织对齐 Python

### 问题

当前 Go 侧把 TeamCard、TeamConfig、BaseTeam 全部放在 `team.go` 中，但 Python 的文件组织是分离的：

| Python 文件 | 内容 | Go 当前文件 |
|---|---|---|
| `multi_agent/schema/team_card.py` | TeamCard, EventDrivenTeamCard | 混在 `team.go` |
| `multi_agent/config.py` | TeamConfig | 混在 `team.go` |
| `multi_agent/team.py` | BaseTeam | `team.go` |

Go 项目中 `single_agent/schema/` 已按 Python 建了子包，但 `multi_agent/` 下缺少 `schema/` 子包。

### 解决方案：建立 schema 子包 + 拆分文件

**新增文件：**

```
multi_agent/
├── doc.go              # 包文档（更新）
├── team.go             # BaseTeam 接口 + AgentTeamProvider + TeamAgentProvider（仅保留接口和 Provider）
├── team_option.go      # TeamOptions + TeamOption（不变）
├── config.go           # TeamConfig + NewTeamConfig + 链式配置方法（新增）
└── schema/
    ├── doc.go          # schema 子包文档（新增）
    └── team_card.go    # TeamCard + NewTeamCard + TeamCardOption（新增）
```

**对应 Python 映射：**

| Python 路径 | Go 路径 |
|---|---|
| `multi_agent/schema/team_card.py` | `multi_agent/schema/team_card.go` |
| `multi_agent/config.py` | `multi_agent/config.go` |
| `multi_agent/team.py` | `multi_agent/team.go` |

### 变更清单

1. **新建** `multi_agent/schema/team_card.go`：TeamCard 完整结构体 + 构造函数 + 选项函数
2. **新建** `multi_agent/schema/doc.go`：schema 子包文档
3. **新建** `multi_agent/config.go`：TeamConfig 完整结构体 + 构造函数 + 链式配置方法
4. **修改** `multi_agent/team.go`：移除 TeamCard/TeamConfig 定义，改为 import `schema` 子包和本包 `config`
5. **修改** `multi_agent/doc.go`：更新文件目录和职责描述
6. **修改** `multi_agent/team_test.go`：更新 import 路径，TeamCard 从 schema 子包引用
7. **新建** `multi_agent/schema/team_card_test.go`：TeamCard 单元测试
8. **新建** `multi_agent/config_test.go`：TeamConfig 单元测试

## 二、TeamCard 完整定义

### 结构体

```go
// TeamCard 团队身份卡片，嵌入 BaseCard 提供统一身份标识。
//
// 对应 Python: openjiuwen/core/multi_agent/schema/team_card.py (TeamCard)
// Python 继承 BaseCard: id/name/description + agent_cards/topic/version/tags
type TeamCard struct {
    schema.BaseCard
    // AgentCards 成员 Agent 的卡片列表（仅元数据，非实例）
    AgentCards []*agentschema.AgentCard `json:"agent_cards,omitempty"`
    // Topic 团队主题/领域
    Topic string `json:"topic,omitempty"`
    // Version 团队版本号
    Version string `json:"version,omitempty"`
    // Tags 分类标签
    Tags []string `json:"tags,omitempty"`
}
```

### 构造函数与选项

```go
// TeamCardOption TeamCard 构造选项函数。
type TeamCardOption func(*TeamCard)

// NewTeamCard 创建 TeamCard 实例，默认 Version="1.0.0"。
func NewTeamCard(opts ...TeamCardOption) *TeamCard

// WithAgentCards 设置成员 Agent 卡片列表。
func WithAgentCards(cards []*agentschema.AgentCard) TeamCardOption

// WithTopic 设置团队主题。
func WithTopic(topic string) TeamCardOption

// WithTeamVersion 设置团队版本号。
func WithTeamVersion(version string) TeamCardOption

// WithTags 设置分类标签。
func WithTags(tags []string) TeamCardOption
```

### 方法

```go
// String 实现 fmt.Stringer 接口。
func (c *TeamCard) String() string
```

## 三、TeamConfig 完整定义

### 结构体

```go
// TeamConfig 团队运行时配置，控制团队的最大 Agent 数、并发数和超时。
//
// 对应 Python: openjiuwen/core/multi_agent/config.py (TeamConfig)
type TeamConfig struct {
    // MaxAgents 团队最大 Agent 数量，默认 10
    MaxAgents int `json:"max_agents,omitempty"`
    // MaxConcurrentMessages 最大并发消息数，默认 100
    MaxConcurrentMessages int `json:"max_concurrent_messages,omitempty"`
    // MessageTimeout 消息处理超时秒数，默认 30.0
    MessageTimeout float64 `json:"message_timeout,omitempty"`
    // Extra 额外配置字段，对应 Python model_config={"extra": "allow"}
    Extra map[string]any `json:"-"`
}
```

### 构造函数

```go
// NewTeamConfig 创建 TeamConfig 实例，设置默认值。
func NewTeamConfig() TeamConfig
```

默认值：MaxAgents=10, MaxConcurrentMessages=100, MessageTimeout=30.0

### 链式配置方法

```go
// ConfigureMaxAgents 链式配置最大 Agent 数量。
func (c *TeamConfig) ConfigureMaxAgents(maxAgents int) *TeamConfig

// ConfigureTimeout 链式配置消息超时秒数。
func (c *TeamConfig) ConfigureTimeout(timeout float64) *TeamConfig

// ConfigureConcurrency 链式配置最大并发消息数。
func (c *TeamConfig) ConfigureConcurrency(maxConcurrent int) *TeamConfig
```

### Extra 辅助方法

```go
// SetExtra 设置额外配置字段。
func (c *TeamConfig) SetExtra(key string, value any)

// GetExtra 获取额外配置字段。
func (c *TeamConfig) GetExtra(key string) (any, bool)
```

### Extra 字段设计说明

Python 中 `model_config={"extra": "allow"}` 允许 Pydantic 接受任意额外字段。Go 中用 `Extra map[string]any` 模拟，`json:"-"` 不参与序列化（Extra 是运行时注入，不需持久化）。

## 四、team.go 变更

从 `team.go` 中**移除** TeamCard 和 TeamConfig 定义，改为引用：

- TeamCard → 从 `multi_agent/schema` 子包引用，通过类型别名暴露到 `multi_agent` 包外层
- TeamConfig → 保留在 `config.go` 同包内，team.go 直接使用

### import 变更

`team.go` 需新增 import 本包 schema 子包：

```go
import (
    agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
    "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"  // 新增
    ...
)
```

### 类型别名（保持外部 API 兼容）

为避免外部包（如 `resources_manager`）修改 import 路径，在 `team.go` 中保留类型别名：

```go
// TeamCard 团队身份卡片（类型别名，实际定义在 schema 子包）。
type TeamCard = schema.TeamCard
```

**注意：** Go 的类型别名 `type A = B` 是完全等价的，所有使用 `multiagent.TeamCard` 的外部代码无需修改 import。

**构造函数调用约定：** `NewTeamCard` 定义在 schema 子包中。外部代码构造 TeamCard 实例时需通过 schema 子包调用：`schema.NewTeamCard(opts...)`。如果后续觉得调用不便，也可在 `multi_agent` 包层添加转发函数 `NewTeamCard`（直接调用 `schema.NewTeamCard`），但这会引入同包函数名冲突，暂不添加。

## 五、回填影响点

| 文件 | 回填内容 |
|------|---------|
| `multi_agent/team.go` | 移除 TeamCard/TeamConfig 定义，添加 TeamCard 类型别名，import schema 子包 |
| `multi_agent/team_test.go` | TeamCard 构造方式更新（使用 `schema.NewTeamCard` 或类型别名 + 直接构造），TeamConfig 改用 `NewTeamConfig()` |
| `multi_agent/doc.go` | 文件目录更新：新增 config.go、schema/ 子包；team.go 职责描述更新 |
| `resources_manager/base.go` | **无需修改** — 类型别名保证 `multiagents.TeamCard` 仍可用 |
| `resources_manager/resource_manager.go` | **无需修改** — 同上 |
| `resources_manager/agent_team_manager.go` | **无需修改** — 同上 |

## 六、测试覆盖

### schema/team_card_test.go

| 测试用例 | 覆盖目标 |
|---------|---------|
| `TestNewTeamCard_默认值` | 验证 Version 默认 "1.0.0"，AgentCards/Tags 默认 nil |
| `TestNewTeamCard_带选项` | WithAgentCards/WithTopic/WithTeamVersion/WithTags 选项 |
| `TestTeamCard_String` | fmt.Stringer 输出格式 |
| `TestTeamCard_JSON序列化` | JSON marshal/unmarshal，omitempty 行为 |
| `TestTeamCard_嵌入BaseCard` | 嵌入 BaseCard 后 ID/Name/Description 可访问 |

### config_test.go

| 测试用例 | 覆盖目标 |
|---------|---------|
| `TestNewTeamConfig_默认值` | 验证 MaxAgents=10, MaxConcurrentMessages=100, MessageTimeout=30.0 |
| `TestTeamConfig_链式配置` | ConfigureMaxAgents/ConfigureTimeout/ConfigureConcurrency 链式调用 |
| `TestTeamConfig_Extra` | SetExtra/GetExtra 读写 |
| `TestTeamConfig_JSON序列化` | JSON marshal/unmarshal（Extra 不序列化） |

### team_test.go（更新）

| 测试用例 | 更新内容 |
|---------|---------|
| `TestBaseTeam_编译时接口检查` | TeamConfig{} 改为 NewTeamConfig()；TeamCard 构造更新 |

## 七、声明排列（遵循编码规范）

所有文件严格按规范 2 排列：结构体 → 枚举 → 常量 → 全局变量 → 导出函数 → 非导出函数。

### schema/team_card.go

```
结构体区块：  TeamCard → TeamCardOption
导出函数区块：NewTeamCard → WithAgentCards → WithTopic → WithTeamVersion → WithTags
               TeamCard.String
非导出函数区块：（无）
```

### config.go

```
结构体区块：  TeamConfig
导出函数区块：NewTeamConfig → ConfigureMaxAgents → ConfigureTimeout → ConfigureConcurrency
               SetExtra → GetExtra
非导出函数区块：（无）
```
