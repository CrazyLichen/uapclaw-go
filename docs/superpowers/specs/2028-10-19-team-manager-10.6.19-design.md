# 10.6.19 Swarm Team — TeamManager 完整对齐设计（方案 C）

> 状态：待评审
> 日期：2028-10-19
> 范围：IMPLEMENTATION_PLAN.md 10.6.19-23 合并行中的 10.6.19 部分（TeamManager 主体），
> 连带 10.6.23 TeamRails 的 team_runtime_inheritance.py + rails/ 两个小 rail（方案 C 决策），
> 以及 10.6.20 distributed_runtime.py 的纯函数层（方案 A 决策）。
> Python 参考：jiuwenswarm/agents/harness/team/

## 一、背景与目标

Go team 包已有 72 个方法的完整骨架（team_manager*.go 7 个文件），但存在 13 处 ⤵️(#9.72) 待回填 stub。本设计按"方案 C：按 Python 源码完整对齐"原则：

1. 逐方法核对 team_manager.py（2043 行）实现完整度，回填全部**本地可完成**的 stub
2. 补齐 Python team/ 目录中完全缺失的文件：team_runtime_inheritance.py（587 行）、rails/ 两个小 rail（150 行）、exceptions.py（48 行）
3. 带出 distributed_runtime.py 纯函数层（方案 A 决策，zmq 传输层仍留 10.6.20）
4. 所有保留的 ⤵️ 标记**编号修正**（#9.72 → 实际章节），文字内容不丢失

**已核实的利好条件**（探索结论）：
- `CodeAdapter.ConfigureTeamMemberAgent` 已实现（code_adapter.go:1362）——步骤 c 直接复用
- `TeamSkillEvolutionRail`/`TeamSkillCreateRail`/`SkillEvolutionRail` 构造函数全就绪（agentcore/harness/rails/evolution/）
- `SkillToolkit` 已实现（tools/skill_toolkit.go）——MemberSkillToolkitRail 依赖就绪
- `LoadExecutionDisabledSkills` 已实现（runtime/skill/state_utils.go:259）
- `ModelPoolEntry`/model_pool schema 已就绪（agent_teams/schema/blueprint.go:68）
- inheritance 依赖的全部基础 rails（TaskPlanning/SysOperation/Heartbeat/Security/AvatarPrompt/ResponsePrompt/RuntimePrompt/StreamEvent/ContextProcessor）Go 侧均已实现
- `DeepAgentInterface.AddRail` 已带 M-02 预留注释："补齐接口面（customizer 回填后 team 层需通过接口调用 agent.add_rail）"
- customizer 调用链完整：`RunAgentTeamStreaming → TeamRuntimeManager.Activate → AgentConfigurator 步骤 17 → harness.RunAgentCustomizer(ctx, customizer, memberName, role)`

## 二、customizer 完整调用链（定位）

```
DeepAdapter.processTeamMessageStream（首次请求，deep_adapter_team.go:392）
  → teamManager.GetEnrichedTeamSpec              ← 10.6.19 核心
      → loadTeamSpec                              ← 回填：分布式检查+model_pool 注入
      → BuildAgentCustomizer                      ← 回填：六步闭包
  → ateamruntime.RunAgentTeamStreaming(teamSpec, ...)（deep_adapter_team.go:744）
      → TeamRuntimeManager.Activate（runtime/manager.go:219）
          → AgentConfigurator（每个 member，agent_configurator.go:429）
              → harness.RunAgentCustomizer(ctx, spec.AgentCustomizer, memberName, role)
                  → customizer(ctx, deepAgent, memberName, role)  ← 六步执行点
```

## 三、交付物清单

### 新建文件（5 个）

#### 1. `team/runtime_inheritance.go`（对齐 team_runtime_inheritance.py，587 行）

- **结构体迁移**（真实类型迁移，不用别名，同包无破坏）：
  - `MemberInfo`、`RuntimeInfo`、`TeamWorkspaceInfo`、`TeamRailMountContext` 从 team_manager.go 迁入本文件
  - 迁移依据：字段语义全部属于 inheritance 层；TeamRailMountContext 在 Python 中虽定义于 team_manager.py L111-118，但字段全是 inheritance 的 dataclass
- **常量**：
  - `railWhitelist`（14 项：RuntimePromptRail/ResponsePromptRail/JiuClawStreamEventRail/TaskPlanningRail/SecurityRail/HeartbeatRail/AvatarPromptRail/FileSystemRail/SysOperationRail/TeamSkillEvolutionRail/TeamSkillCreateRail/SkillEvolutionRail/TeamWorkspaceReportPathRail/ContextProcessorRail）
  - `toolWhitelist`（39 项：free_search/fetch_webpage/paid_search/vision/…/acp_chat，逐项对齐 Python）
- **导出函数**：
  - `BuildMemberRails(memberInfo, runtime, teamWorkspace) []agentinterfaces.AgentRail` —— 逐 rail try-log 对齐 Python（每步 try/except → Go 每步独立错误处理+warning 日志）：
    1. RuntimePromptRail(language, channel)
    2. ResponsePromptRail()
    3. SysOperationRail()
    4. JiuClawStreamEventRail()
    5. TaskPlanningRail()
    6. SecurityRail()
    7. HeartbeatRail()
    8. AvatarPromptRail()
    9. team_ws_root 非空 → TeamWorkspaceReportPathRail(rootDir, teamID, language)
    10. role==leader && skills_dir → TeamSkillEvolutionRail（BuildEvolutionLLM + auto_scan + trajectory registry 绑定 + LoadExecutionDisabledSkills）
    11. role==leader && skills_dir && skill_create_enabled → TeamSkillCreateRail（autoTrigger=true）
    12. role!=leader && skills_dir → BuildSkillEvolutionRail（member 自演化）
    13. context_engine_enabled → BuildContextProcessorRail
  - `FilterInheritableAbilityCards(mainAgent) []*tool.ToolCard` —— 遍历 AbilityManager.List()，toolWhitelist 过滤
  - `GetDefaultModelName(config) string` —— models.default.model_client_config.model_name，失败回退 "gpt-4"
  - `ResolveModelConfig(config) (mcc, mco map[string]any, modelName string)` —— models.defaults 列表 is_default 优先 → 第一个 → models.default → react 段回退
  - `BuildEvolutionLLM(config) (*llm.Model, string)` —— temperature 默认 0.95
  - `BuildSkillEvolutionRail(skillsDir, config, teamTrajectorySink, teamID)` —— SkillEvolutionRail + SetTrajectorySink
  - `GetEvolutionAutoScanEnabled(config)` —— env EVOLUTION_AUTO_SCAN 优先，回退 evolution.auto_scan 默认 false
  - `GetSkillCreateEnabled(config)` —— env SKILL_CREATE 优先，回退 evolution.skill_create 默认 false
  - `GetContextEngineEnabled(config)` —— react.context_engine_config.enabled 默认 true
  - `BuildContextProcessorRail(config) *ContextProcessorRail` —— 读 message_summary_offloader_config/dialogue_compressor_config/current_round_compressor_config/round_level_compressor_config 四段 → (name, dict) pairs → NewContextProcessorRail(processors, preset=true)
- **非导出函数**：`getBoolEnv`、`getEvolutionConfig`

#### 2. `team/member_skill_toolkit_rail.go`（对齐 rails/team_member_skill_toolkit_rail.py，86 行）

- `MemberSkillToolkitRail` 实现 `agentinterfaces.AgentRail`，priority=95
- `Init(agent)`：
  1. agentID = card.ID 或 card.Name
  2. SkillManager(workspaceDir) + SkillToolkit(manager).GetTools()
  3. 每个 tool.card.ID 追加后缀 `_{agentID}`（qualifyToolID）
  4. 同名 ToolCard 已存在则先移除（Get + Remove）
  5. Runner.resource_mgr.AddTool(tools) + agent.AbilityManager().Add(tool.card)
- `Uninit(agent)` 反向移除
- 日志字段对齐：registered %d skill tools / workspace / agent_id

#### 3. `team/workspace_report_path_rail.go`（对齐 rails/team_workspace_report_path_rail.py，64 行）

- `TeamWorkspaceReportPathRail` priority=5
- `Init`：缓存 agent 的 SystemPromptBuilder（可为 nil）
- `BeforeModelCall(ctx)`：构造 content（Team Workspace Artifact Paths 段：绝对根路径、`.team/{team_id}/` 挂载路径、"report real path not mount path" 指引、示例路径），`AddSection(PromptSection{name: "team_workspace_report_paths", content: {cn, en}, priority: 67})`
- `Uninit`：`RemoveSection("team_workspace_report_paths")`
- content 文本从 Python 源码逐字复制（品牌标识按 jiuwenswarm→uapclaw 映射规则处理）

#### 4. `team/exceptions.go`（对齐 exceptions.py，48 行）

- `TeamError`（error 接口实现基类）+ 7 个子类：TeamCreateError/TeamRecoverError/TeamInteractError/TeamConfigError/TeamMonitorError/TeamSessionError/TeamStorageError

#### 5. `team/distributed_runtime.go`（纯函数层，对齐 distributed_runtime.py 无 zmq 部分）

- **导出函数**：
  - `IsDistributedMode(configBase) bool` —— team.runtime.mode=="distributed" 或 transport.type=="pyzmq"
  - `RuntimeRole(configBase) string` —— 默认 leader，仅接受 leader/teammate
  - `RuntimeMemberName(configBase, teamCfg) string` —— runtime.member_name → predefined_members[0].member_name 回退
  - `ParsePort(value any, defaultPort int, fieldName string) int` —— None/空串→default；int 解析失败或越界（1..65535）→ 返回 default 并 warning（Python raise ValueError → Go 日志降级，因 spec.go stub 签名返回 int）
  - `NormalizeDistributedTransportFields(configBase, teamCfg) map[string]any` —— deepcopy + pyzmq 检查 + leader 清 known_peers/bootstrap_peers + pubsub/leaders/teammates 地址推导（默认端口 18555/18556/18557/18600）
  - `MissingDistributedDependencies(configBase) []string` —— Go 无 zmq 包概念，检查恒返回空列表（对齐 Python 语义：仅分布式模式检查；Go 静态编译不存在运行时缺包）
  - `FallbackDistributedToLocal(configBase) map[string]any` —— runtime.mode=local + role=leader + transport.type=inprocess（去 params）+ postgres 存储→sqlite team.db + modes.team 逐项同样处理
  - `IsPostgresqlStorage(teamCfg) bool` —— storage.type ∈ {postgresql, postgres}
  - `ExtractPgEndpoint(teamCfg) (host string, port int)` —— connection_string 解析（postgresql+asyncpg:// 前缀归一化 → urlparse），默认 127.0.0.1:5432
  - `RunCommand(ctx, args, timeoutSec) (int, string)` —— os/exec + CombinedOutput + timeout kill（Python asyncio subprocess 对齐；spawn 失败 127 / 超时 124 / communicate 失败 126）
  - `IsPgAvailable(ctx, host, port) bool` —— pg_isready -h -p，rc==0
  - `TryStartPgCluster(ctx) bool` —— pg_lsclusters --no-header 解析 → pg_ctlcluster <ver> <name> start → 再探测
  - `EnsurePostgresqlForLeader(ctx, configBase)` —— 组合：IsDistributedMode 检查 → RuntimeRole==leader → IsPostgresqlStorage → ExtractPgEndpoint → IsPgAvailable 失败则 TryStartPgCluster → post-start ready 退避重试（init 0.4s / max 2.0s / backoff 1.45 / max 30s / 每 5s 日志）
- **常量**：subprocessTimeoutSec=120.0 等对齐 `_SUBPROCESS_TIMEOUT_SEC`/`_PG_POST_START_*` 系列

### 修改文件（6 个）

#### 6. `team/team_manager_spec.go`（核心回填）

- **BuildAgentCustomizer 六步实现**（customizer 闭包内部）：
  - **前置解析**（对齐 Python build_agent_customizer 闭包捕获）：globalSkillsDir（getAgentSkillsDir 已有）、globalSkillsStatePath、resolvedChannel、resolvedModelName=GetDefaultModelName()、teamWsRoot（spec.Workspace.RootPath 回退 TeamHome(teamName)/team-workspace）、teamWsSkillsDir=teamWsRoot/skills、teamTrajectoryRegistry=InMemoryTrajectoryRegistry()
  - **步骤 a（能力卡继承）**：`FilterInheritableAbilityCards(deepAgent)` → existingAbilityIds 去重 → `agent.AbilityManager().Add(card)` → addedCount 日志
  - **步骤 b（技能同步）**：五个内联函数（resolveMemberSpec/resolveMemberSkills/copyMemberConfiguredSkills/buildMemberSkillState/writeMemberSkillState）+ 主流程：memberSkillsDir=memberWorkspaceRoot/skills → mkdir → copyMemberConfiguredSkills → syncSkillsDir(teamWsSkillsDir→memberSkillsDir) → writeMemberSkillState → SkillManager 构造 → MemberSkillToolkitRail 挂载
    - copyMemberConfiguredSkills：global skills 目录中 SKILL.md 有效且在 selected 集合 → copytree（dest 存在跳过）→ missing 集合 warning
    - buildMemberSkillState：读全局 skills_state.json → NormalizeMarketplaces → installed_plugins/local_skills 按 actual_skill_names（member 目录实际 SKILL.md）过滤+补全（project source 条目）
  - **步骤 c（code adapter 配置）**：parent_adapter_mode=="code" → 调 `m.codeMemberConfigurer` 注入回调（参数：agent、deepAgent、memberSkillManager、memberName、role、sessionID、resolvedChannel、parentProjectDir、team_plan_runtime_language、forceEnglishRuntimePrompt=not is_team_plan_leader）；回调 nil 时 warning 日志跳过（对齐 Python except 语义）
    - team_plan 判定：request_metadata.mode=="team.plan" 或 spec.EnableTeamPlan；leader 时 language 取 spec.Language 回退 config preferred_language
  - **步骤 d（member rails）**：TeamWorkspaceInfo 组装 → `BuildMemberRails(...)` → 遍历：railWhitelist 过滤 → `agent.AddRail(rail)` → TeamSkillEvolutionRail/TeamSkillCreateRail → `RegisterTeamLiveRail`；TeamSkillEvolutionRail → `RegisterTeamSkillRail` + `RegisterTeamSkillSyncTarget(sessionID, teamWsSkillsDir, globalSkillsDir)`；SkillEvolutionRail → `RegisterTeamMemberSkillEvolutionRail`；TeamSkillCreateRail → `RegisterTeamSkillCreateRail`
  - **步骤 e（TeamRailContext）**：role==leader → `RegisterTeamRailContext(sessionID, &TeamRailMountContext{Agent, MemberInfo, Runtime, TeamWorkspace})`
  - **步骤 f（member runtime tools）**：`RegisterMemberRuntimeTools` 调用保留，内部仅日志骨架，工具注册 ⤵️(10.6.24/11.10) 保留
  - **rail_manager 扩展 rail 循环**：Go 无 get_rail_manager 对应物（扩展 rail 加载机制属 10.5.7 ExtensionLoader 延后），标注差异注释，空操作
- **loadTeamSpec 回填**：
  - 步骤 4 分布式标准化：`IsDistributedMode(cb)` → `MissingDistributedDependencies` warning（MISSING_DISTRIBUTE_DEPS 日志）→ `FallbackDistributedToLocal` error 日志（FALLBACK_TO_LOCAL）+ ACTION 日志 → cb 更新
  - `NormalizeDistributedTransportFields(cb, specDict)` 调用（IsDistributedMode 时）
  - **model_pool 注入**：GetDefaultModels(cb)（需从 config 层暴露，web/config_apply.go:300 已有 GetDefaultModels 先例，搬移/复用）>1 时 → 逐 entry 构造 ModelPoolEntry（model_name/api_key/api_base_url/api_provider/metadata.client+request）→ specDict["model_pool"]+["model_pool_strategy"]="by_model_name"
- **RegisterMemberRuntimeTools**：日志骨架（对齐 Python 日志字段），工具注册 ⤵️(10.6.24/11.10)
- **NormalizeDistributedTransportFields/ParsePort** 方法体：改为调 distributed_runtime.go 纯函数

#### 6a. `common/config/` 新增权威 GetDefaultModels（方案 iii 决策，新建文件 default_models.go）

对齐 Python common/config.py:697 `get_default_models`（common 层唯一实现）。现状：Go 已有 3 处独立实现（web/config_apply.go:302 无 env 回退、dreaming/sweeper.go:1034 无 env 回退、adapter/deep_adapter.go:2296 纯函数无 env 回退），全部缺 env 回退 + is_default 推断 + custom_headers 处理；api_key 解密依赖 ExtensionRegistry crypto（10.5.10 ⤵️ 延后，Python crypto 为 None 时跳过解密，Go 等价处理）。

- **导出函数**：
  - `GetDefaultModels(config map[string]any) []map[string]any` —— 优先级：models.defaults 列表 → models.default 单对象包装 → **环境变量回退**（MODEL_ALIAS/API_BASE/API_KEY/MODEL_NAME/MODEL_PROVIDER/CUSTOM_HEADERS 构造条目：timeout=1800、verify_ssl=false、temperature=0.95、alias 非空附加）
  - `InferIsDefault(entries) []map[string]any` —— 同 model_name 组仅 1 条→true；多条→首 true 其余 false；已有显式 true 保留、同组其余置 false（Python `_infer_is_default` 规则）
- **非导出函数**：
  - `decryptModelEntries(entries)` —— 深拷贝 + api_key 解密钩子（当前 crypto 未接入，空操作+⤵️(10.5.10) 标注，Python crypto=None 等价）+ custom_headers ParseCustomHeaders 处理（已有 normalize.go:76，注意：NormalizeConfig 全项目尚未接线，此处需手动调用 ParseCustomHeaders）+ InferIsDefault
- **签名设计**：`GetDefaultModels(config map[string]any)` 注入式——team_manager.py L323 调用 `get_default_models(config_base)` 传入已加载 dict，对齐此形态；config==nil 时内部 `New("").Load()`（对齐 Python config=None 时 get_config()）
- **doc.go 更新**：文件目录加 default_models.go 条目

#### 6b. 收敛三处旧实现（方案 iii 决策）

| 旧实现 | 处置 |
|--------|------|
| `web/config_apply.go:302 GetDefaultModels()` | 删除，调用点 :653 改 `config.GetDefaultModels(nil)`（行为增强：新增 env 回退+is_default，web 场景 config.yaml 存在时行为不变） |
| `dreaming/sweeper.go:1034 getDefaultModels()` | 删除，调用点 :789 改 `config.GetDefaultModels(nil)`（dreaming 10.6.14 已完成章节，改动仅 2 行：删实现+改调用） |
| `adapter/deep_adapter.go:2296 getDefaultModels(configBase)` | 删除，调用点 :1958（buildModelCacheFromDefaults）改 `config.GetDefaultModels(configBase)`（纯函数→权威实现，行为增强同上） |

- 收敛后 Go 侧唯一实现位于 common/config，结构对齐 Python common 层
- 需同步检查三处调用的下游语义（web resolvedDefaults 后续写回/is_default 消费、dreaming entries 消费、adapter modelCache 构造）确认新增 is_default 字段无副作用（is_default 为附加字段，下游按需读取，缺省不影响既有读取路径）

#### 7. `team/team_manager_skill.go`

- `EnsureTeamSharedSkillsInitialized` 完整实现：
  - copyGlobalSkillsToTeamSharedDir：globalSkillsDir 不存在 warning 返回；teamWsRoot 解析（spec.Workspace.RootPath 回退 TeamHome/team-workspace）；`.team_skills_copied` marker 存在跳过；copytree(dirs_exist_ok=True 等效：目标存在则逐项合并覆盖)
  - syncTeamSharedSkillsToAgentGlobal：resolveTeamSharedSkillsDir → syncSkillsDir(shared→global)
- `SyncTeamSkills`：调 `syncSkillsDir(source, target)`
- 新增非导出 `syncSkillsDir(source, target)`（对齐 `_sync_skills_dir`）：source 非目录返回；target mkdir；遍历 source 子目录（SKILL.md 有效）→ dest 存在先 rmtree → copytree → synced 计数 → synced>0 时 info 日志（含数量/源/目标）

#### 8. `team/team_manager_lifecycle.go`

- :176 `_ensure_postgresql_for_leader` → 调 `distributed_runtime.EnsurePostgresqlForLeader(ctx, configBase)`（GetEnrichedTeamSpec/CreateTeam 两个入口同步接入，对齐 Python 两处 await 调用）
- :216 步骤 8：`EnsureTeamSharedSkillsInitialized(spec)` 已由调用方（deep_adapter_team.go:425）执行——对齐 Python create_team 内部调用位置，将调用移入 CreateTeam（调用方已有调用需核对去重，保持单次执行语义：marker 文件幂等）
- :217 步骤 9 + :435/:638/:661 + :66：⤵️ 标记编号修正 #9.72 → #10.6.20/#10.6.22，文字保留；evolution watcher 激活路径核对（ensureTeamEvolutionWatcher 已实现）

#### 9. `team/team_manager.go` + `team_manager_distributed.go`

- team_manager.go：四个 dataclass 结构体迁出（→ runtime_inheritance.go）
- team_manager_distributed.go:30：⤵️ 标记编号修正 #9.72 → #10.6.20-21

#### 10. `agentcore/harness/interfaces/deep_agent.go`（接口面补齐）

- `DeepAgentInterface` 添加：`AbilityManager() agentinterfaces.AbilityManagerInterface`（deep_agent.go:409 实现已有，纯接口暴露）——步骤 a/b 依赖

#### 11. `swarm/server/adapter/`（注入点 + 标记修正）

- `team_helpers.go` EnsureEventBroadcastInjected 同一初始化点（或新建 EnsureTeamDependenciesInjected）注入 `SetCodeMemberConfigurer`：包装 `CodeAdapter.ConfigureTeamMemberAgent`（签名适配：interfaces.DeepAgentInterface → *harness.DeepAgent 断言）
- `deep_adapter_team.go:808`：⤵️ 标记编号修正 #9.72 → #10.6.20

### 类型定义（team 包内）

```go
// CodeMemberConfigurer code 模式 team member 配置回调（由 adapter 层注入）。
// 对齐 Python: customizer 内 from interface_code import configure_code_team_member_agent（延迟 import）
type CodeMemberConfigurer func(
    ctx context.Context,
    agent interfaces.DeepAgentInterface,
    parentAgent interfaces.DeepAgentInterface,
    skillManager *skill.SkillManager,
    memberName, role, sessionID, channelID, projectDir, runtimeLanguage string,
    forceEnglishRuntimePrompt bool,
)
```

## 四、⤵️ 回填点最终处置表（13 处全覆盖）

| # | 位置 | 内容 | 处置 |
|---|------|------|------|
| 1 | spec.go:132 步骤 a | 继承能力卡 | ✅ 实现（FilterInheritableAbilityCards） |
| 2 | spec.go:135 步骤 b | 技能同步 | ✅ 实现（五函数+syncSkillsDir+skills_state） |
| 3 | spec.go:137 步骤 c | code adapter 配置 | ✅ 实现（注入回调） |
| 4 | spec.go:139 步骤 d | member rails | ✅ 实现（BuildMemberRails+三类注册） |
| 5 | spec.go:141 步骤 e | TeamRailContext | ✅ 实现 |
| 6 | spec.go:143-144 步骤 f | runtime tools | 日志骨架，⤵️(10.6.24/11.10) 保留 |
| 7 | spec.go:170 RegisterMemberRuntimeTools | Cron+SendFile | 日志骨架，⤵️(10.6.24/11.10) 保留 |
| 8 | spec.go:176/183 | transport 标准化/ParsePort | ✅ 实现（distributed_runtime 纯函数） |
| 9 | spec.go:257/281 loadTeamSpec 步骤 4 | 分布式标准化 | ✅ 实现（检查+fallback+normalize+model_pool） |
| 10 | skill.go:29/35 | 共享技能初始化 | ✅ 实现（copy+sync+marker） |
| 11 | skill.go:54 | _sync_skills_dir | ✅ 实现（syncSkillsDir） |
| 12 | lifecycle.go:66 | evolution watcher | ✅ 核对激活路径 |
| 13 | lifecycle.go:176 | PG for leader | ✅ 实现（EnsurePostgresqlForLeader） |
| 14 | lifecycle.go:216 | 共享技能初始化 | ✅ 实现 |
| 15 | lifecycle.go:217 | attach hooks | ⤵️(10.6.20) 保留，编号修正 |
| 16 | lifecycle.go:435/638/661 | A2X 释放+transport 停止 | ⤵️(10.6.22) 保留，编号修正 |
| 17 | distributed.go:30 | AttachDistributedHooks | ⤵️(10.6.20-21) 保留，编号修正 |
| 18 | deep_adapter_team.go:808 | 分布式 hooks | ⤵️(10.6.20) 保留，编号修正 |

## 五、关键设计决策

1. **步骤 c 循环依赖破解（方案 1 注入回调）**：team 包定义 `CodeMemberConfigurer` + `SetCodeMemberConfigurer`；adapter 侧注入。回调 nil 时 warning 跳过，语义等价 Python 延迟 import 失败 → except 跳过。复用 SetOnEventBroadcast 注入先例。
2. **customizer 错误语义**：对齐 Python——六步各自独立 try/except 吞错（warning 日志），单步失败不阻断后续步骤。Go 实现：每步独立函数 + defer recover 或显式错误分支。
3. **PG 函数归属（方案 A）**：distributed_runtime.go 纯函数层全部实现于 team 包内；zmq 传输层（AttachDistributedHooksForRunnerRuntime 内部、remote_member_bootstrap）留 10.6.20-21。
4. **MissingDistributedDependencies Go 差异**：Python 检查 zmq/asyncpg 包存在性；Go 静态编译无运行时缺包概念，恒返回空列表（保留函数以对齐调用链结构与日志点）。
5. **ParsePort 错误语义降级**：Python raise ValueError（调用链上层吞掉）；Go stub 签名返回 int 无 error——warning 日志+返回 default（对齐 Python 调用方 catch 后用 default 的实际行为）。
6. **TeamRailMountContext 等四结构体真实迁移**（用户确认，不用别名）：同包迁移零破坏。
7. **rail_manager 扩展 rail 循环**：Go 无对应机制（10.5.7 ExtensionLoader 延后），空操作+差异注释。
8. **GetDefaultModels 依赖方向（方案 iii 定案：新增权威实现并收敛三处）**：common/config 新增 `default_models.go` 权威实现（defaults→default 优先级 + env 回退 + is_default 推断 + custom_headers 处理；api_key 解密 ⤵️(10.5.10)，等价 Python crypto=None 跳过路径）。签名注入式 `GetDefaultModels(config map[string]any)`，config==nil 内部加载（对齐 Python get_default_models(config=None)）。三处旧实现（web 11.14 ✅ / dreaming 10.6.14 ✅ / adapter 10.3.4-6 ✅）全部删除收敛到权威实现——用户确认接受触碰已完成章节；收敛改动每处仅 2 行（删+改调），行为增强（env 回退+is_default 附加字段）对既有 config.yaml 场景无副作用。依赖方向已验证：common/config 只依赖 common/utils，extensions→common/config 已存在故 crypto 不能进 common/config（解密钩子留空操作+⤵️ 标注）。注意 NormalizeConfig 当前全项目未接线，ParseCustomHeaders 需在 decryptModelEntries 内手动调用。
9. **品牌映射**：rail content 等用户可见文本按 jiuwenswarm→uapclaw 映射规则处理（feedback 记忆既有规则）。
10. **EnsureTeamSharedSkillsInitialized 调用位置**：对齐 Python 移入 CreateTeam（marker 幂等，调用方已有调用不冲突）。

## 六、章节状态标记策略（用户确认 2）

IMPLEMENTATION_PLAN.md 10.6.19-23 合并行拆分为：

```
| 10.6.19 | ✅ | TeamManager 主体 | 72 方法骨架回填+customizer 六步+inheritance 迁入 ... | team_manager.py |
| 10.6.20 | ☐ | DistributedRuntime | zmq 传输层（✅ 纯函数层已随 10.6.19 带出：IsDistributedMode/ParsePort/Normalize.../PG 五函数） | distributed_runtime.py |
| 10.6.21 | ☐ | RemoteMemberBootstrap | ... | remote_member_bootstrap.py |
| 10.6.22 | ☐ | A2X RegistryRuntime | ... | a2x/ |
| 10.6.23 | ✅ | TeamRails | inheritance+2 rail+exceptions 已随 10.6.19 实现（方案 C 合并决策） | team_runtime_inheritance.py · rails/ |
```

## 七、工作量与测试

| 项 | 规模（实现+测试） |
|----|------|
| runtime_inheritance.go | ~600 行 |
| member_skill_toolkit_rail.go | ~100 行 |
| workspace_report_path_rail.go | ~80 行 |
| exceptions.go | ~50 行 |
| distributed_runtime.go | ~350 行 |
| spec.go/skill.go/lifecycle.go 回填 | ~400 行 |
| 接口面+adapter 注入 | ~80 行 |
| common/config default_models.go + 三处收敛 | ~200 行（权威实现+测试；三处收敛各 2 行改动） |
| **合计** | **~1860 行 + 测试** |

测试策略：
- 全部新文件配 `_test.go`，覆盖率 ≥ 85%（项目规范）
- BuildMemberRails：mock 各 rail 构造依赖（config map 注入），验证 leader/teammate/禁用开关组合
- customizer 六步：fake DeepAgentInterface（AddRail/AbilityManager 记录调用），验证去重/注册顺序/错误吞没
- distributed_runtime：ParsePort 边界（nil/空串/非数字/0/65536）、NormalizeTransportFields leader/teammate 分支、FallbackDistributedToLocal config 变换、ExtractPgEndpoint URL 解析
- common/config GetDefaultModels：defaults 列表/default 单对象/env 回退（os.Setenv 构造）三优先级；InferIsDefault 分组规则（单条 true/多条首 true/显式 true 保留）；custom_headers JSON→map；收敛后 web/dreaming/adapter 三包既有测试全量回归
- syncSkillsDir/copyMemberConfiguredSkills：t.TempDir() 临时目录（SKILL.md 存在性/marker/覆盖语义）
- PG 命令编排：RunCommand 用 fake 可执行路径或 PATH 注入测试（pg_isready 缺失场景）；EnsurePostgresqlForLeader 非 leader/非 postgres 短路路径
- 注入回调：nil 注入 warning 路径 + 注入后调用转发验证

## 八、风险与缓解

| 风险 | 缓解 |
|------|------|
| GetDefaultModels 收敛触碰 3 个已完成章节（11.14 web / 10.6.14 dreaming / 10.3.4-6 adapter） | 每处仅 2 行改动（删实现+改调用）；is_default 为附加字段，下游按需读取；实现时逐一核对三处调用点下游消费逻辑；相关包测试全量回归 |
| GetDefaultModels 行为增强（env 回退）对 web 场景的影响 | web 场景 config.yaml 存在时走 defaults/default 分支，与旧实现一致；仅 config 无 models 段时新增 env 回退（旧实现返回 nil，新实现返回 env 条目）——需核对 web :653 下游对 nil vs env 条目的处理 |
| CodeAdapter.ConfigureTeamMemberAgent 期望 *harness.DeepAgent 而回调收到接口 | 注入包装层做类型断言，断言失败 warning 跳过（对齐 Python except） |
| create_team 内 spec.build() 与 Go CreateTeam(spec) 构造差异 | customizer 经 spec.AgentCustomizer 字段传递，TeamAgent.Configure → AgentConfigurator 步骤 17 调用链已核实 |
| member agent 的 DeepConfig().Workspace 可能为 nil | 对齐 Python：None 时 warning 跳过步骤 b 剩余部分 |
| copytree 目标已存在语义（dirs_exist_ok） | Go 手写递归复制：存在合并覆盖（marker 场景）/ 存在先删（syncSkillsDir 场景），分别对齐两处 Python 语义 |
