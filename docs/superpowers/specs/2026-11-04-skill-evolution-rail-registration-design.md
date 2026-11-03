# SkillEvolutionRail / SkillCreateRail 注册链路对齐设计

> 状态：待评审
> 日期：2026-11-04
> 范围：DeepAdapter 中技能演进/技能创建两条 rail 的构建与注册生命周期、热重载配置同步、单 Agent SkillCreateRail 移植
> Python 参考：`jiuwenswarm/server/runtime/agent_adapter/interface_deep.py`、`jiuwenswarm/server/runtime/agent_adapter/interface_code.py`、
> `openjiuwen/harness/rails/skills/skill_create_rail.py`、`openjiuwen/harness/rails/evolution/skill_evolution_rail.py`
>
> 行号基准：Go 侧 `18ff20e3`，Python 侧 jiuwenswarm / agent-core 工作副本；实施时以符号名为准，行号可能随并行改动漂移。

## 一、背景

用户反馈"演进 rail 好像只是默认创建了，但没有注册"。核对 Go 与 Python 两侧后确认：这条 rail 的注册状态在 Go 侧被一个双重语义的字段承载，冷启动/热重载会提前填充它，导致"计划模式注册"分支的判空条件恒为假而被跳过；同时同源的 `SkillCreateRail` 还缺 `skill_create` 门控、且挂的是团队版而非单 Agent 版。

本设计一次性修掉注册链路上的 5 个问题，并确立一条可验证的不变量。

## 二、五个问题（根因 / 所属流程 / 证据）

### 问题① 冷启动构建 rail，注册分支靠"字段为 nil"判断是否已注册

- **所属流程**：实例创建流程（`CreateInstance` → `buildAgentRails`）× 每轮请求的 runtime config 流程（`updateRuntimeConfig` → `updateRailsForMode` → `updatePlanModeRails`）
- **根因**：Python 的 `_build_agent_rails` 完全不构建该 rail（`interface_deep.py:2158` 注释「不在冷启动时挂载」），字段仅在 `_build_skill_evolution_rail` 内赋值（`:1965`），而该方法只被注册路径调用（`:2812`）——因此"字段非 nil ≈ 已注册"。Go 把构建提前到 `buildAgentRails`（`deep_adapter_rails.go:187-192`），字段非 nil 只代表"建过"；`updatePlanModeRails` 仍用 `if d.skillEvolutionRail == nil` 当判据（`:1109-1123`），语义断裂。
- **当前影响**：冷启动 `mode` 恒为 `"agent"`（`runtime/uapclaw.go:650`），`updateAgentModeRails`（`:1179-1186`）会把这个未注册实例 `UnregisterRail` 掉并置 nil，**顺手掩盖了①**；①单独存在不致命，但在热重载（问题②）下被放大。

### 问题② 热重载重建实例并覆盖字段，与已注册实例分裂

- **所属流程**：配置热重载流程（`ReloadAgentConfig`）
- **根因**：Python `_get_current_agent_rails` 对已存在的演进 rail 做**就地更新**（`update_llm` + 重算 `auto_scan`，`interface_deep.py:2314-2328`），实例与注册状态都不动；Go 的 `getCurrentAgentRails` 直接等于 `buildAgentRails`（`deep_adapter.go:3072-3074`），每次热重载重建全部 rail 并覆盖字段，而 `hotReloadRails` 只按类型替换（`deep_agent.go:1517-1531`），演进 rail 不在新列表里 → 已注册的旧实例 R1 被保留、字段却指向未注册的新实例 R2。
- **影响**：`watchEvolutionAndPush`（`deep_adapter_evolution.go:170`）、`/evolve` 命令（`deep_adapter_slash.go:73-100`）、审批路由（`deep_adapter_evolution.go:435`）全部读字段 R2；R2 从不接收回调 → 被动演进、状态推送、审批事件全灭，而 R1 仍在跑（旧 model）；模式切回 normal 时 `UnregisterRail(R2)` 摘不掉 R1 → plan-only rail 泄漏到 normal。
- **补充事实**：plan→normal→plan 每轮都会注销并置 nil，说明"实例跨模式持久"不是需求；②只影响"在 plan 模式下改配置"这一场景。

### 问题③ 单 Agent 技能创建：缺门控、挂错 rail 类

- **所属流程**：同①（实例创建 + 计划模式注册）
- **层 1 缺 `skill_create` 门控**：Python 在 builder 内判 `_get_skill_create_enabled`（`:1979-1986`，关闭时 `return None` 且不写字段），并在注册处做 enabled/else 双向处理（`:2825-2845`）；Go `buildSkillCreateRail`（`deep_adapter_rails.go:415-438`）不判 enabled，`updatePlanModeRails` 步骤 8（`:1136-1147`）只有 `if d.skillCreateRail == nil`、没有 else 注销。后果：字段被填后永不为 nil，**用户事后把 `skill_create` 从 false 改成 true 也不会重建**，打开开关不生效。
- **层 2 字段陷阱**：`buildSkillCreateRail` 在 builder 内部写字段（`:432`），`buildAgentRails` 冷启动无条件调用（`:196`），与①同源。
- **层 3 挂错类**：Python adapter 构建的是单 Agent 版 `openjiuwen/harness/rails/skills/skill_create_rail.py::SkillCreateRail`（`priority=85`、`after_task_iteration` 中按 `tool_call_threshold=10` / `tool_diversity_threshold=5` 判阈值 → `enqueue_follow_up` 注入 skill-creator 提议）；Go 构建的是 `TeamSkillCreateRail`（依赖 `NotifyTeamCompleted` 置 `completedSessionID`）。单 Agent 计划模式下 `NotifyTeamCompleted` 无人调用 → `maybeEnqueueCreationFollowUp` 恒早退，**即使打开 `skill_create` 也不会有任何创建提议**。全项目 grep 不到单版移植（无 `tool_call_threshold` / 非 team 的 skill-creator follow-up 文案）。

### 问题④ 构造参数与环境变量的 5 处偏差

| # | 项 | Python（`interface_deep.py:1948-1965`） | Go（`deep_adapter_rails.go:340-410`） |
|---|---|---|---|
| 1 | `auto_scan` 缺键 | `config.get("evolution",{}).get("auto_scan", False)` → 关（`:1955`） | `autoScan := true`（`:355`） → 开（Go rail 构造默认亦为 `true`，`skill_evolution_rail.go:191`，必须显式传） |
| 2 | `EVOLUTION_AUTO_SCAN` | `lower() in ("true","1","yes")`（`:1953`） | 仅 `"true"`（`:360-362`） |
| 3 | `auto_save` | 恒 `False`（`:1962`） | 读 `evolution.auto_save`（`:368-371`），可绕过审批 |
| 4 | `model_name` 兜底 | `_default_model_name or config["model_name"] or "gpt-4"`（`:1956`） | `defaultModelName or "qwen-max"`（`:384-387`），不读 `config.model_name` |
| 5 | sharing | builder 不传 `sharing_config`，adapter 路径恒关 | 读 `evolution.sharing` 并 `WithSharingConfigMap`（`:397-400`） |

`evolution.auto_save` 与 `evolution.sharing` 均未出现在出厂模板（`resources/config.yaml:263-274`），属死键。

### 问题⑤ slash 首轮懒加载只建不注册（Python 同缺陷）

- **所属流程**：slash 命令流程（`handleSlashCommand`）× runtime config 流程
- **证据**：`handleSlashCommand`（Go `deep_adapter.go:890` / Python `:4438`）**先于** `updateRuntimeConfig`（Go `:954` / Python `:4475`）。`/evolve*` 五条命令都先调 `ensureEvolutionRailForSlash`（`deep_adapter_slash.go:39-70`），该函数只 build + 写字段、**不注册**（`:88-93`，Python `:4291-4296` 同）。若 slash 结果不含 `followup_prompt`（`/evolve_list`、`/evolve_simplify` 等），Python `:4450-4457` / Go 对应分支直接 return，`_update_runtime_config` 根本不跑；即便继续走，`updatePlanModeRails` 因字段已非 nil 也会跳过注册。
- **影响**：会话首条 plan 消息若是 `/evolve*`，该实例永久处于"建了没注册"——`/evolve` 手动链路仍可用（斜杠处理直接调 rail 方法，不需要回调），但被动链路全灭（`AfterInvoke` 自动扫描、`chat.evolution_status` 推送、审批事件）。
- **性质**：Python 亦存在，属对 Python 缺陷的有意修正（见第七节）。

## 三、核心不变量

`d.skillEvolutionRail` / `d.skillCreateRail` **非 nil ⇒ 该实例已在 `d.instance` 上注册成功**。

三个写入点（计划模式注册、动态 rail、slash 懒加载）必须"先注册成功、再写字段"；注册失败保持 nil，下一条消息重试。例外仅一处：动态 rail 路径的注册发生在同一次 `CreateDeepAgent` 装配内（`buildDynamicRail` 只负责构建与写字段），装配完成后不变量成立。

由此推出的可观察行为：

- 计划模式下 rail 必定注册并接收回调；normal / code 模式下字段恒 nil；
- 热重载不重建实例，只换 LLM 与 `auto_scan`；
- 装配完成后任何时刻，`d.instance` 的已注册 rails 与字段要么同指一个实例，要么都为空。

## 四、改动清单

### 组 1：移除冷启动构建（问题①）

`internal/swarm/server/adapter/deep_adapter_rails.go`

- 删除 `183-192` 段中对 `buildSkillEvolutionRail` 的调用与类型断言；
- 删除 `194-196` 段中对 `buildSkillCreateRail` 的调用；
- 保留 `d.memoryRail = nil`（那是另一处对齐，语义不同）；`externalMemoryRail` 的生命周期由独立 `externalMemoryRailRegistered` 标志闭环，不在本次范围。

### 组 2：收窄签名 + 热重载就地更新（问题②）

- `buildSkillEvolutionRail` 返回类型改为 `*evolution.SkillEvolutionRail`，删除 3 处断言：`deep_adapter_rails.go:189`、`:1114`、`deep_adapter_slash.go:90`；
- 新增 `func (d *DeepAdapter) updateSkillEvolutionRail(config map[string]any)`：`UpdateLLM(d.modelSnapshot(), d.defaultModelName)` + `SetAutoScan(<env 三值优先 → config，默认 false>)`，对齐 Python `interface_deep.py:2314-2328`；
- 在 `getCurrentAgentRails`（`deep_adapter.go:3072`）顶部调用该 helper（与 Python 的位置一致：就地更新发生在列表构造之前）；
- `SkillEvolutionRail` 增加只读访问器 `ModelName() string`（`skill_evolution_rail.go`），供断言与排障。

### 组 3：skill_create 完整对齐（问题③）

**新增** `internal/agentcore/harness/rails/skills/skill_create_rail.go`（对齐 `openjiuwen/harness/rails/skills/skill_create_rail.py`，166 行）

- 类型 `SkillCreateRail`，嵌入 `evolution.EvolutionRail`（`evolution.NewEvolutionRail(ext, opts...)`，`evolution_trigger=NONE`），`Priority()` 返回 85；
- 字段：`skillsDir`、`language`、`autoTrigger`、`toolCallThreshold=10`、`toolDiversityThreshold=5`、`proposalSent`；
- 行为：
  - `OnBeforeInvoke` 重置 `proposalSent`；
  - `OnAfterTaskIteration`：`autoTrigger` 关 → 返回；本轮已提议 → 返回；`Builder()` 为 nil → debug 返回；遍历 `Builder().Steps`，`Kind == StepKindTool` 且 `Detail.(*trajectory.ToolCallDetail)` 断言成功时收集 `ToolName`，`total < 10 || unique < 5` → 返回（`evolving/trajectory/types.go:41-53`、`:65-87`）；取 loop controller（`interface{ EnqueueFollowUp(string) }`，对齐 `team_skill_create_rail.go:22-24`），为 nil → `Warn` 丢弃；按 `language == "cn"` 选 CN/EN 文案格式化 `skillsDir` 后 `EnqueueFollowUp`，置 `proposalSent`；
  - 其余 `EvolutionExtension` 方法为空实现（`AllowEvolutionTrigger` 返回 true，`SnapshotForEvolution`/`RunEvolution` 空）；
- 文案常量 CN/EN **一比一复刻** `skill_create_rail.py:22-46`；
- 同目录 `doc.go` 补文件条目（规则 1），补单测（规则 3）。

**adapter 侧**

- `buildSkillCreateRail` 改为构建 `*skillrails.SkillCreateRail`：先判 `getSkillCreateEnabled()`，关闭时 `return nil` 且不写字段；`autoTrigger` 恒 `true`；不再读 `min_team_members_for_create`（Python 无此参数）；不在 builder 内写字段；
- 字段类型 `deep_adapter.go:187` 改为 `*skillrails.SkillCreateRail`；
- `updatePlanModeRails` 步骤 8 改为：enabled 且字段 nil → build → 注册成功才写字段；enabled 关且字段非 nil → 注销并置 nil（对齐 `interface_deep.py:2825-2845`）；
- `updateAgentModeRails` 的注销分支保持逻辑不变（类型随字段变更）；
- 删除死函数 `findSkillCreateRail`（`deep_adapter_team.go:62-78`，全项目无调用方且引用旧类型）。

### 组 4：构造参数全对齐（问题④）

`buildSkillEvolutionRail`

- 删除内部 `evolution.enabled` 判断（`:348-352`），门控交给两个调用方（计划模式注册、slash）；动态 rail 路径与 Python 一致不受 enabled 约束。注意这只是演进 rail 的行为：`skill_create` 的门控在 `buildSkillCreateRail` 内部（组 3），与 Python `_build_skill_create_rail` 判 enabled、`_build_skill_evolution_rail` 不判完全一致；
- `autoScan` 默认 `false`，`EVOLUTION_AUTO_SCAN` 认 `true/1/yes`；
- `auto_save` 恒 `false`：删除配置读取，但必须**显式**保留 `evolution.WithAutoSave(false)` —— Go rail 构造默认值是 `autoSave: true`（`skill_evolution_rail.go:192`），省略该选项等于放开审批；
- 删除 `evolution.sharing` 接线与 `WithSharingConfigMap` 调用（rail 层能力保留，等单独立项）；
- `model_name` 兜底链：`d.defaultModelName` → `configCacheSnapshot()["model_name"]` → `"qwen-max"`（末位字面量保持 Go 模板的模型体系，不做 `gpt-4` 硬搬）；
- 同步 `resources/config.yaml` evolution 段注释：删除 `auto_save`/`sharing` 死键说明，标注 `skill_create` 语义为"注册单 Agent SkillCreateRail"。

### 组 5：slash 懒加载补注册（问题⑤）

`internal/swarm/server/adapter/deep_adapter_slash.go`

- 签名改为 `ensureEvolutionRailForSlash(ctx context.Context, mode string) string`；
- 构建后若 `d.instance != nil` 则 `RegisterRail`，成功才写字段；失败返回"演进功能初始化失败。"；
- 5 个调用点补传 `ctx`（`:42`、`:47`、`:52`、`:57`、`:62`）；
- 注释写明：这是对 Python `_ensure_evolution_rail_for_slash` 只建不注册缺陷的有意修正。

### 组 6：动态 rail 写字段（问题①在 code 路径的同源口子）

`deep_adapter_rails.go` 的 `buildDynamicRail`（`case "buildSkillEvolutionRail"`，`:1640-1641`）

- 构建后写 `d.skillEvolutionRail`；注册由同一次 `CreateDeepAgent` 装配完成；code 模式首条消息由 `CodeAdapter.updateRailsForMode`（`code_adapter.go:577-582`）注销并置 nil，闭环。

## 五、测试与验收

### 单元测试

`internal/swarm/server/adapter`

- `buildAgentRails` 执行后 `skillEvolutionRail` / `skillCreateRail` 恒为 nil（覆盖组 1）；
- `buildSkillEvolutionRail` 参数矩阵：缺 `auto_scan` → false；`EVOLUTION_AUTO_SCAN=1` → true；`auto_save: true` 被忽略；`model_name` 兜底链（覆盖组 4）；
- `buildSkillCreateRail`：`skill_create=false` → nil 且不写字段；true → 返回 `*skills.SkillCreateRail` 且 `autoTrigger` 为真（覆盖组 3）；
- `updateSkillEvolutionRail`：`ModelName()` 与 `AutoScan()` 随配置变化（覆盖组 2）。

`internal/agentcore/harness/rails/skills`

- 新 rail：阈值不满足不提议；满足后每轮只提议一次；`before_invoke` 重置；无 controller 时 Warn 且不入队；CN/EN 文案与 `skillsDir` 替换正确。

### 集成测试

`tests/integration/swarm/server/adapter`（跨 adapter / harness / rails / evolving）

1. 计划模式消息 → 字段非空 **且** `d.instance` 的已注册 rails 中存在 `*evolution.SkillEvolutionRail`（问题①②的回归锚点）；
2. 计划模式热重载 → rail 实例指针不变、`ModelName()` 更新为新默认模型（问题②）；
3. 会话首条消息为 `/evolve` → 字段非空且已注册（问题⑤，改动前必红）；
4. `skill_create=true` → 注册的是 `*skills.SkillCreateRail`；构造一轮超阈值会话后 follow-up 入队（问题③）。

### 验收

按项目规则 6：改动推送后远端 CI 的 Build / Lint / Unit Test / Integration Test 四个 job 全绿方为完成。

## 六、非目标

- `getCurrentAgentRails` 从"全量重建 14+ rail"收敛到 Python 的 6 项白名单（`interface_deep.py:2340-2352`）—— 影响面独立，另开议题验证后再动；
- `ExternalMemoryRail` 的注册标志模式改造（现状已用独立标志闭环，不受本次影响）；
- rail 层 sharing 能力（`WithSharingConfigMap`）本身，仅断开 adapter 接线。

## 七、偏离记录

| 偏离 | 理由 |
|---|---|
| slash 懒加载补注册（问题⑤） | Python `_ensure_evolution_rail_for_slash` 只建不注册，导致首条 `/evolve` 后被动演进永久失效；本设计选择修正而非复刻，注释与测试中标注 |
| `model_name` 末位字面量保留 `"qwen-max"` | Python 用 `"gpt-4"` 是其模型体系默认值，Go 模板默认不同；兜底链中间层已对齐 |
| 新增 `SkillEvolutionRail.ModelName()` | Python 无对应访问器，属 Go 侧可观测性补充（测试需要断言就地更新是否生效） |

## 八、风险与回滚

- 字段类型变更影响 4 处引用 + 1 个死函数，编译期全覆盖；
- 新 rail 在 `EnableTaskLoop=false`（单轮模式）下不工作，与 Python 一致（`skill_create_rail.py:93-99` 同样 Warn 丢弃）；
- 若单版 rail 移植引入回归，可通过配置 `evolution.skill_create=false` 立即停用（改造后该键真正生效）；
- 全部改动可按组拆分提交，组 1/2/4/5 与组 3/6 解耦，组 3 可独立回滚。
