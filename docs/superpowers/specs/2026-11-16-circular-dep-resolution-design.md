# 9.57 审查：循环依赖解决方案设计

## 背景

9.57 AgentConfigurator 实现完成后，审查发现 `harness.go` 因循环依赖无法导入 `atschema` 包，
导致 `role` 字段从 `atschema.TeamRole` 降级为 `string`，`agentSpec`/`deepAgent` 降级为 `any`。

## Python 端对比

Python 没有 Go 的循环依赖问题，原因有二：

1. **`TYPE_CHECKING` + `from __future__ import annotations`**：`harness.py` 在类型标注中引用
   `schema.team.TeamRole` 和 `schema.deep_agent_spec.DeepAgentSpec`，但运行时不触发导入，
   天然规避循环依赖。Go 没有 `TYPE_CHECKING` 等价物。

2. **`constants.py` 和 `i18n.py` 是独立文件**：Python 的 `schema/blueprint.py` 导入
   `from openjiuwen.agent_teams.constants import ...` 和 `from openjiuwen.agent_teams.i18n import t`，
   而 `constants.py` 和 `i18n.py` 不反向导入 `schema/`，无循环。

Go 端当前问题根因：`i18n.go` 和 `constants.go` 与 `harness.go` 同在根包 `agent_teams/`，
`schema/blueprint.go` 导入根包的 `T()`、`DefaultLeaderMemberName` 等符号，
形成 `schema/ → 根包` 的反向依赖，导致根包不能导入 `schema/`。

## 决策

### 决策 1：提取常量子包（方案 A）

将 `i18n.go` 和 `constants.go` 从根包提取为独立子包，对齐 Python 的文件组织：

| Python | Go 当前 | Go 重构后 |
|--------|---------|-----------|
| `agent_teams/constants.py` | `agent_teams/constants.go`（根包） | `agent_teams/constants/` 子包 |
| `agent_teams/i18n.py` | `agent_teams/i18n.go`（根包） | `agent_teams/i18n/` 子包 |

**重构后依赖方向：**

```
agent_teams/ (根包: harness.go)
    ↓ 可自由导入
agent_teams/schema/ (blueprint.go)
    ↓ 改为导入
agent_teams/constants/ + agent_teams/i18n/
    ↑ 根包也可导入
```

循环断开，`harness.go` 可使用 `atschema.TeamRole`、`atschema.DeepAgentSpec`。

**具体变更：**

1. 创建 `internal/agent_teams/constants/` 子包，迁移常量：
   - `HumanAgentMemberName`
   - `UserPseudoMemberName`
   - `DefaultLeaderMemberName`
   - `ReservedMemberNames`

2. 创建 `internal/agent_teams/i18n/` 子包，迁移 i18n 函数：
   - `T()`
   - `SetLanguage()`
   - `GetLanguage()`
   - `STRINGS` 字典

3. `schema/blueprint.go` 改为导入 `agent_teams/constants` 和 `agent_teams/i18n`
   而非 `agent_teams` 根包。

4. 根包 `agent_teams/` 保留 re-export 以减少外部调用方改动：
   - `constants.go` → 用 `const`/`var` 委托到子包（Go 不支持直接 re-export，
     需要逐个符号包装：`const DefaultLeaderMemberName = constants.DefaultLeaderMemberName`）
   - `i18n.go` → 用 `func T() string { return i18n.T() }` 委托
   - 外部调用方的 `agentteams.DefaultLeaderMemberName` 等无需改动

5. `harness.go` 恢复类型：
   - `role string` → `role atschema.TeamRole`
   - `BuildTeamHarness(agentSpec any, role string, ...)` → `BuildTeamHarness(agentSpec atschema.DeepAgentSpec, role atschema.TeamRole, ...)`
   - `deepAgent any` → `deepAgent hinterfaces.DeepAgentInterface`（需验证无新循环）

6. 同步更新测试文件中的类型适配。

### 决策 2：any 占位随章节逐步替换

~30 处 `any` 占位保持当前策略，等各章节（9.58-9.68）实现具体类型后直接替换。

**理由：**

1. Python 的 `TYPE_CHECKING` 本质也是延迟到类型可用时才标注，我们延迟到实现时替换是等价行为
2. 现在预定义接口可能和最终实现不匹配，后续要改两遍
3. 随章节替换每步验证编译，渐进安全

**`any` 消除路径：**

| any 字段 | 需等章节 | 消除方式 |
|----------|---------|---------|
| `harness.role` | 本重构 | `atschema.TeamRole` |
| `BuildTeamHarness.agentSpec` | 本重构 + #9.56 | `atschema.DeepAgentSpec` |
| `harness.deepAgent` | 本重构 + #9.57 后续 | `hinterfaces.DeepAgentInterface` |
| `infra.Messager` | #9.65 | 定义 Messager 接口后替换 |
| `infra.TeamBackend` | #9.58 | 定义 TeamBackend 类型后替换 |
| `infra.WorkspaceManager` | #9.66 | 定义 TeamWorkspaceManager 后替换 |
| `infra.TaskManager/MessageManager` | #9.58 | 随 TeamBackend 定义后替换 |
| `resources.WorktreeManager` | #9.66 | 定义 WorktreeManager 后替换 |
| `resources.MemoryManager` | #9.64 | 定义 TeamMemoryManager 后替换 |
| `resources.FirstIterGate` | #9.68 | 定义 FirstIterationGate 后替换 |
| `resources.ModelAllocator` | #9.64 | 定义 ModelAllocator 后替换 |
| `team_agent.spawnManager` | #9.58 | 定义 SpawnManager 后替换 |
| `team_agent.recoveryManager` | #9.61 | 定义 RecoveryManager 后替换 |
| `team_agent.sessionManager` | #9.59 | 定义 SessionManager 后替换 |
| `team_agent.streamController` | #9.60 | 定义 StreamController 后替换 |
| `team_agent.coordination` | #9.62 | 定义 CoordinationKernel 后替换 |
| MountedRails 全部 6 字段 | #9.68 + #9.66 | 定义各 Rail 类型后替换 |

## 实施时机

本重构应在 9.57 提交后、9.58 开始前执行，因为：

1. 9.58（SpawnManager）需要 `TeamBackend` 类型，而 `TeamBackend` 的定义可能需要
   在 `schema/` 或 `agent/` 包中引用 `atschema` 类型——如果循环依赖还在，又会遇到同样问题
2. 后续几乎所有章节都需要跨包引用 schema 类型，越早解决循环依赖越好
3. 重构范围可控：仅涉及 `constants`、`i18n` 两个子包的提取 + `schema/blueprint.go` 的 import 路径更新 + `harness.go` 的类型恢复
