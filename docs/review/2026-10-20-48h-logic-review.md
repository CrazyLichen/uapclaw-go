# 48h 逻辑审查报告

> 审查时间：2026-10-20
> 修复时间：2026-10-20（用户逐项确认后统一修复，16 项实施 + 9 项跳过/保留）
> 审查范围：最近 48 小时（2026-10-18 08:58 ~ 2026-10-20）完成的实现计划章节
> 涉及章节：10.6.19（TeamManager 主体及附属模块，Task1~Task16 共 16 个功能 commit + 收尾）、13.7（TokenizerChunker + IndexSentenceSplitter）、13.8（HybridChunker）、13.18（Splitter 核心）、registry 注册一致性
> 参考项目：`/home/opensource/jiuwenswarm-develop/jiuwenswarm`（10.6.19 swarm 章节）、`/home/opensource/agent-core/openjiuwen`（13.x retrieval 章节）
> 审查方式：4 路并行 agent 审查 + team-lead 对全部 S/M 级发现独立源码逐行验证 + agent 报告撤回项复核
> 复核的修复 commit：b1c5d4c7、24695cf0、b2112dbf、a750fa26、53394efe（前次审查修复的验证，全部确认正确）

---

## 修复状态总览（2026-10-20 处置决策）

| 项 | 处置 | 说明 |
|---|------|------|
| S-01 | ✅ 已修复 | selectFirstModesTeam 断言改 dict + 新增 config_loader_test.go 覆盖 |
| S-02 | ✅ 已修复（8 点） | storage 键名/条件设置、workspace/transport/leader 默认值、resolveStorageConfig 改 params 结构、顶层键清理（T-01）、language/enable_hitt 补齐、enable_team_plan pop、buildAgentDefaults 默认 workspace 改 {stable_base: true}（验证新发现）、buildPredefinedMembers 补 Python 过滤链（验证新发现） |
| S-03 | ✅ 已修复 | defaults 断言改 list + default 旧格式回退、键名改 model_config_obj、model_name 注入、完成日志对齐（T-02 一并） |
| S-04 | ✅ 已修复（9 处） | 8 个导出访问器 + SetStreamTaskWaitGroup 加锁；lifecycle 四处锁内直读；EnsureMonitor 两处锁内直写；cleanupRuntimeLocals 补分段锁（pop 锁内 / IO 锁外） |
| S-05 | ✅ 已修复（方案 A） | DestroyTeam 包装器去锁 + 并发不死锁测试 |
| S-06 | ✅ 已修复 | 补 WithMaxRetries（mapIntField 双形态兼容）/ WithSSLCert 映射；client_id 已验证两侧均自动生成（default_factory ↔ uuid.New）无需映射 |
| S-07 | ✅ 已修复 | invoke/stream 两处无条件覆盖（缺省清空）+ 顺带消除冗余 query 声明 |
| M-01 | ✅ 已修复（严格对齐） | 字符串 $ref 分支 + 顶层 configBase["agents"] 查找 + 删 dict-$ref 分支 + 空 agents 注入 leader/teammate |
| M-02 | ⏭️ 跳过 | GetRunnerConfig 验证为纯内存快照读（无 IO），持锁调用无实际影响 |
| M-03 | ✅ 已修复 | roleStr(memberInfo.Role) 替换硬编码 "leader" |
| M-04 | ✅ 已修复 | ParsePort/NormalizeDistributedTransportFields/loadTeamSpec 签名加 error 恢复 raise 语义；GetEnrichedTeamSpec error 时返回 nil 走现有错误路径 |
| M-05 | ✅ 已修复（方案 A） | 对齐 Python detached task 语义：EnsureMonitor 不再注册 `_monitor` 条目（泄漏源头消除，非补 delete 打补丁） |
| M-06 | ✅ 已修复 | 跳过的非 dict 条目记 Warn 日志 |
| M-07/M-08 | ⏭️ 无需修复 | 合法占位（⤵️#10.6.20-21）/ 正面确认记录 |
| T-01/T-02 | ✅ 随 S-02 修复 | 顶层冗余键清理 + 完成日志字段补齐 |
| T-03 | ✅ 已修复 | 删除 FileSystemRail 死键 + 测试改 13 项断言 |
| T-04~T-10 | ⏭️ 无需修复 | 提醒项已纳入 S-04 实施注意 / 验证记录 / 合法占位 |

验证方式：`go build ./...` 通过；`go test ./...` 全量通过（仅 memory 包 FTS 2 项为存量环境问题，git stash 对照确认与本次修复无关）；4 个核心包 `go test -race` 通过。

---

## 统计摘要

| 严重程度 | 数量 |
|---------|------|
| 严重 (S) | 7 |
| 一般 (M) | 8 |
| 提示 (T) | 10 |
| **合计** | **25** |

> 编号说明：S-01~S-04 + M-01 + T-01~T-02 来自 config_loader.go 移植链（tm-support 报告 S-03 的深挖扩展，team-lead 独立验证发现 S-01/S-02 为 agent 未报的增量问题）；S-05 + M-02 + T-03~T-04 来自 TeamManager 锁规范（team-lead 独立验证）；S-06 + M-03 来自 runtime_inheritance（tm-support 报告，team-lead 复核确认）；S-07 + M-04~M-06 + T-05~T-08 来自 10.6.19 附属模块（tm-support + fix-verify 报告，team-lead 抽验）；M-07 + T-09 来自 fix-verify 复核发现。

**撤回项说明**：tm-support 曾报告 "railWhitelist 15 键含双 SecurityRail 键"（S-02 候选），team-lead 独立数键后确认实际 14 键无重复，agent 误读 L85 注释，已撤回不计入。

---

## 一、审查范围确认（步骤 1）

48h 窗口内完成的计划章节（依据 git log 与 IMPLEMENTATION_PLAN.md 交叉核对）：

| 章节 | 主要提交 | Python 参照 |
|------|---------|------------|
| 10.6.19 TeamManager 主体 | 10-19 12:41~10-19 21:xx（c34d7774 计划 + 69b96a73~95724e8d 共 16 个 Task commit） | jiuwenswarm/agents/harness/team/team_manager.py (2043 行) + team_runtime_inheritance.py + config_loader.py + distributed_runtime.py + exceptions.py + rails/ 两小 rail |
| 10.6.19 收尾 | 107603fa（GetDefaultModels 三处收敛）、304df9e1（计划表拆分） | 同上 |
| 13.7 TokenizerChunker | 47ee8c81（fmt）+ 13.7 原有 commit（10-18 前） | openjiuwen/.../chunker/tokenizer_chunker.py + text_splitter.py |
| 13.8 HybridChunker | 45121088（计划）+ 7a3df802 + 404f7dbf + e2a703ca（回填完整体）+ 543070eb（doc） | openjiuwen/.../chunker/hybrid_chunker.py |
| 13.18 Splitter 核心 | （随 13.7 交付，无独立 commit） | openjiuwen/.../chunker/splitter.py |
| 审查修复复核 | b1c5d4c7、24695cf0、b2112dbf、a750fa26、53394efe | 前次（2026-10-15/10-19）审查报告 |

窗口内章节交付结论（正面确认，非问题）：
- **13.7/13.8/13.18/registry 四项全部对齐**（fix-verify + team-lead 双重验证）：Tokenizer 三态接口、resolveChunkSize 三分支、overlap==0→chunkSize/5、HybridChunker 4 分支 + Process 局部覆写（Go embedding 不派发的正确应对）、splitter 主循环/flush/splitLongSegment/detectChinese、registry 工厂三态、tokenizer 注册为文档化主动增强（设计文档偏差 5）
- **5 个修复 commit 全部验证正确**：a750fa26 的 25 项修复 + 6 项误报判断成立；53394efe 的测试桩遮蔽（Go embedding 教训的正确应用）+ chroma ONNX EF（SDK 源码级根因确认）修复正确
- **10.6.20/21/22/24 均为 ☐ 未完成**，lifecycle.go 中 ⤵️(#10.6.20)/⤵️(#10.6.22) 占位标记合法（非缺失实现）

---

## 二、严重问题 (S)

### S-01: selectFirstModesTeam 对 modes.team 的类型断言与 Python 配置格式互斥——生产配置下整个 team spec 加载退化为全默认值

**Python 参考** (`jiuwenswarm/agents/harness/team/config_loader.py:26-40`)：

```python
def _select_first_modes_team(config_base: dict[str, Any]) -> dict[str, Any]:
    modes_raw = config_base.get("modes", {})
    if not isinstance(modes_raw, dict):
        return {}

    teams_raw = modes_raw.get("team", {})
    if not isinstance(teams_raw, dict):        # ← modes.team 是 dict（team_name → team_spec）
        return {}

    for team_name, team_raw in teams_raw.items():   # ← 按 key 遍历取第一个
        if isinstance(team_raw, dict):
            logger.debug("[TeamConfigLoader] selected team from modes.team: %s", team_name)
            return team_raw

    return {}
```

Python 生产配置 `jiuwenswarm/resources/config.yaml:871-878` 实际格式：

```yaml
modes:
  team:
    jiuwen_team:            # ← key = team_name，value = team 配置
      team_name: jiuwen_team
      lifecycle: persistent
      ...
```

且 Python 测试 `test_team_config_loader.py:13-14` 明确：`_wrap_modes_team` 构造 `{"modes": {"team": team_mapping}}`（dict-of-dicts）。写入侧 `config.py:1037` `data["modes"]["team"] = team_mapping`（`_build_modes_team_mapping` 返回 dict）。

**Go 问题** (`internal/swarm/agents/harness/team/config_loader.go:104-124`)：

```go
func selectFirstModesTeam(configBase map[string]any) map[string]any {
	modes, ok := configBase["modes"].(map[string]any)
	if !ok {
		...
		return make(map[string]any)
	}
	teamList, ok := modes["team"].([]any)   // ← 断言为 list，与生产 dict 格式互斥！
	if !ok || len(teamList) == 0 {
		...
		return make(map[string]any)
	}
	first, ok := teamList[0].(map[string]any)
	...
}
```

`modes["team"].([]any)` 对 dict 格式的配置**恒失败**，返回空 map → `LoadTeamSpecDict` 从空 teamRaw 构建 → team_name/lifecycle/teammate_mode/spawn_mode 全部走默认、leader persona 丢失、agents 配置为空、workspace/storage/transport 全空 → `NewTeamAgentSpecFromDict` 产出接近全零的 spec → `GetEnrichedTeamSpec`（生产入口）→ `loadTeamSpec` 返回后所有下游拿到的都是兜底默认值。

**Go 项目内部自相矛盾的证据**：`web_handlers.go:1441` `flattenTeamConfig` 正确断言 `m["team"].(map[string]any)`，其测试 `web_handlers_test.go:587` 注释明确写着 "Python: modes.team 为 dict（key=team_name, value=team_spec）"。config_loader.go 是唯一断言为 list 的地方。

**测试为何没拦住**：`team_manager_spec_test.go:36` 用 `"modes": map[string]any{"team": map[string]any{}}` —— 空 dict 在两种断言下都返回空结果，测试碰巧无法区分两种格式。

**修复方案**：

```go
func selectFirstModesTeam(configBase map[string]any) map[string]any {
	modes, ok := configBase["modes"].(map[string]any)
	if !ok {
		return make(map[string]any)
	}
	// 对齐 Python：modes.team 为 dict（team_name → team_spec），取第一个 dict 值
	teams, ok := modes["team"].(map[string]any)
	if !ok || len(teams) == 0 {
		return make(map[string]any)
	}
	for _, teamRaw := range teams {
		if first, ok := teamRaw.(map[string]any); ok {
			logger.Debug(logComponent).
				Str("team_name", strField(first, "team_name")).
				Msg("selected first modes.team config")
			return first
		}
	}
	return make(map[string]any)
}
```

并补充测试：非空 dict 格式（`{"modes": {"team": {"t1": {...}}}}`）断言能取到 team_name。

**流程示例（问题触发链）**：

```
用户配置 config.yaml（Python 部署同款格式）
  modes.team.jiuwen_team = {team_name: jiuwen_team, leader: {persona: 天才项目管理专家...}, ...}
        ↓
用户请求 team 模式 → DeepAdapter.processTeamMessageStream
        ↓
TeamManager.GetEnrichedTeamSpec → loadTeamSpec → LoadTeamSpecDict
        ↓
selectFirstModesTeam: modes["team"].([]any) 断言失败（实际是 dict）
        ↓
teamRaw = {} → teamCfg 几乎全空
        ↓
NewTeamAgentSpecFromDict → TeamName="agent_team"（默认）、Leader persona=""（配置里的中文 persona 丢失）
        ↓
CreateTeam → AgentCard description 为空 → leader 系统提示词缺失团队上下文
        ↓
团队行为与 Python 部署完全不同（静默错误，无任何报错日志）
```

---

### S-02: LoadTeamSpecDict 读 storage_raw 键（Python 读 storage）+ 顶层默认值键写入方式偏离——storage/默认模型/默认 workspace 注入链全部失效

**Python 参考** (`config_loader.py:300-346`)：

```python
def load_team_spec_dict(config_base):
    team_raw = _select_first_modes_team(config_base)
    ...
    agents = _build_agents_config(team_raw, config_base)   # 默认模型/workspace 注入进每个 agent
    spec_dict = deepcopy(team_raw)
    spec_dict.pop("enable_team_plan", None)
    spec_dict["team_name"] = str(team_raw.get("team_name", "team")).strip() or "team"
    spec_dict["lifecycle"] = team_raw.get("lifecycle", "persistent")
    spec_dict["teammate_mode"] = team_raw.get("teammate_mode", "build_mode")
    spec_dict["spawn_mode"] = team_raw.get("spawn_mode", "inprocess")
    spec_dict["enable_hitt"] = team_raw.get("enable_hitt", True)
    spec_dict["leader"] = _build_leader_spec(team_raw)
    spec_dict["agents"] = agents
    spec_dict["language"] = str(config_base.get("preferred_language", "zh")).strip().lower()
    workspace_spec = _build_workspace_spec(team_raw)      # ← 默认 {"enabled": True}
    if workspace_spec is not None:
        spec_dict["workspace"] = workspace_spec
    spec_dict["transport"] = _build_transport_spec(team_raw)   # ← 默认 {"type": "inprocess"}
    ...
    storage_raw = team_raw.get("storage", {})             # ← 读 "storage" 键
    if storage_raw:
        spec_dict["storage"] = _resolve_storage_config(storage_raw)
```

要点：
1. **默认模型/默认 workspace 通过 `_build_agents_config` → `_build_agent_spec_dict` 注入到每个 agent 的 `model`/`workspace` 键**（setdefault 语义），不在顶层写 `default_model`
2. `storage` 读 `team_raw.get("storage")`（YAML 里就是 `storage:`），仅非空时设置
3. `workspace` 默认 `{"enabled": True}`（`_DEFAULT_TEAM_WORKSPACE`，config_loader.py:22）——**启用**团队工作空间是默认行为
4. `transport` 默认 `{"type": "inprocess"}`
5. `leader` 由 `_build_leader_spec` 构造（member_name="team_leader" / display_name="Team Leader" / persona="天才项目管理专家" 回退）
6. `language`、`enable_hitt` 顶层设置；`enable_team_plan` pop 掉

**Go 问题** (`config_loader.go:41-79`)：

```go
teamRaw := selectFirstModesTeam(configBase)

storage := resolveStorageConfig(resolveMapField(teamRaw, "storage_raw"))  // ← 错键！Python 读 "storage"

defaultModel := buildDefaultModelDict(configBase)
defaultWorkspace, maxIter, timeout := buildAgentDefaults()
agents := buildAgentsConfig(teamRaw, configBase)

teamCfg := make(map[string]any)
for k, v := range teamRaw {
	teamCfg[k] = v
}
teamCfg["storage"] = storage                    // ← 恒覆盖：teamRaw 原始 storage 键被空 dict 覆盖
teamCfg["default_model"] = defaultModel         // ← Python 无此顶层键（且因 S-03 恒为空 map）
teamCfg["default_workspace"] = defaultWorkspace // ← Python 无此顶层键
teamCfg["max_iterations"] = maxIter             // ← Python 无此顶层键
teamCfg["completion_timeout"] = timeout         // ← Python 无此顶层键
teamCfg["agents"] = agents
teamCfg["workspace"] = buildWorkspaceSpec(teamRaw)  // ← 见 S-02a：默认值丢失
teamCfg["transport"] = buildTransportSpec(teamRaw)  // ← 见 S-02b
teamCfg["leader"] = buildLeaderSpec(teamRaw)        // ← 见 S-02c
teamCfg["predefined_members"] = buildPredefinedMembers(teamRaw)
```

具体偏差：

- **S-02a `buildWorkspaceSpec`**（config_loader.go:324-330）：`teamRaw["workspace"]` 非 dict 时返回**空 map**，Python 返回 `_DEFAULT_TEAM_WORKSPACE = {"enabled": True}` + `setdefault("version_control", False)`。后果：`NewTeamAgentSpecFromDict` 反序列化后 `spec.Workspace = &TeamWorkspaceConfig{Enabled: false}` → `agent_configurator.go:214` `if spec.Workspace != nil && spec.Workspace.Enabled` 不成立 → **团队工作空间管理器不创建**（Python 默认创建）→ TeamPolicyRail 少挂 workspace 挂载段、TeamWorkspaceRail 不创建、成员共享工作空间全部失效。
- **S-02b `buildTransportSpec`**（:332-340）：非 dict 时返回空 map（`TransportSpec{Type: ""}`），Python 默认 `{"type": "inprocess"}`。Type 为空时 `TransportSpec.Build()` 走 `transportRegistry[""]` 未注册报错——但 `defaultTransportForSpawnMode`（blueprint.go:341）仅在 `SpawnMode=="inprocess"` 时补默认，如果 spawn_mode 也因 S-01 丢了就连锁空。dict 时也**不 setdefault type=inprocess**（Python L247）。
- **S-02c `buildLeaderSpec`**（:344-350）：非 dict 时返回**空 map**；Python `_build_leader_spec` 返回带默认值的完整 dict（member_name="team_leader"、display_name="Team Leader"、name 回退 "TeamLeader"、persona="天才项目管理专家"）。dict 时也不做字段级默认值回退（Python L253-263 的三级 name 回退 + 字段默认值全无）。
- **S-02d `resolveStorageConfig` 键名**（:182-207）：读顶层 `db_type`/`connection_string`；Python `_resolve_storage_config` 读 `params.connection_string` 且仅当该键存在时解析（`resolve_team_sqlite_db_path` 读 `type`/`params`）。Go 侧 `storage.type: sqlite`（YAML 实际格式）永远走不到 connection_string 自动填充分支（db_type 键不存在）。
- **S-02e 顶层键写入方式**：`default_model`/`default_workspace`/`max_iterations`/`completion_timeout` 写顶层，Python 是注入到每个 agent 的 `model`/`workspace`/`max_iterations`/`completion_timeout`（`_build_agent_spec_dict` L165-168 setdefault）。Go 的 `buildAgentSpecDict`（:222-243）用"先填默认再覆盖"模拟 setdefault 语义上等价，但顶层多余键会被 `NewTeamAgentSpecFromDict` JSON 桥接忽略（TeamAgentSpec 无 default_model 字段）——真正的注入链在 `buildAgentsConfig` 内部有做，此顶层键是冗余无害偏差，但 `language`/`enable_hitt` 缺失 + `enable_team_plan` 未 pop 是真偏差。

**修复方案**（对齐 Python 逐项）：

1. `resolveMapField(teamRaw, "storage_raw")` → `resolveMapField(teamRaw, "storage")`；仅当 teamRaw["storage"] 非 nil 时才设置 `teamCfg["storage"]`
2. `buildWorkspaceSpec`：非 dict → 返回 `{"enabled": true}`；dict → 深拷贝 + `setdefault enabled=true` + `setdefault version_control=false`
3. `buildTransportSpec`：非 dict → `{"type": "inprocess"}`；dict → 深拷贝 + `setdefault type=inprocess`
4. `buildLeaderSpec`：非 dict → 返回 Python 默认四字段 dict；dict → 字段级默认值回退（member_name="team_leader"、display_name="Team Leader"、name 三级回退、persona="天才项目管理专家"）
5. 删除顶层 `default_model`/`default_workspace`/`max_iterations`/`completion_timeout` 写入（buildAgentsConfig 内部已注入），补 `language`（读 `configBase["preferred_language"]` 默认 "zh"）、`enable_hitt`（默认 true），pop `enable_team_plan`
6. `resolveStorageConfig` 改读 `params.connection_string` + `type`（sqlite 判定用 type）

---

### S-03: resolveDefaultModelConfig 类型断言错误 + buildDefaultModelDict 键名错误——团队成员默认模型配置恒为空

**Python 参考** (`config_loader.py:90-125`)：

```python
def _resolve_default_model_config(config_base):
    models_raw = config_base.get("models", {})
    if not isinstance(models_raw, dict):
        return {}
    defaults_raw = models_raw.get("defaults")
    if isinstance(defaults_raw, list):              # ← defaults 是 LIST
        for item in defaults_raw:
            if isinstance(item, dict):
                return item                          # ← 取第一个 dict 条目
    legacy_default = models_raw.get("default")
    if isinstance(legacy_default, dict):
        return legacy_default
    return {}

def _build_default_model_dict(config_base):
    model_config = _resolve_default_model_config(config_base)
    model_client_config = dict(model_config.get("model_client_config", {}))
    model_request_config = dict(model_config.get("model_config_obj", {}))   # ← 键名 model_config_obj
    model_name = model_client_config.get("model_name", "")
    if model_name and "model" not in model_request_config:                  # ← model_name 注入
        model_request_config["model"] = model_name
    ...
```

**Go 问题** (`config_loader.go:140-174`)：

```go
func resolveDefaultModelConfig(configBase map[string]any) map[string]any {
	models, ok := configBase["models"].(map[string]any)
	if !ok {
		return make(map[string]any)
	}
	defaults, ok := models["defaults"].(map[string]any)   // ← 断言为 map，实际是 []any list，恒失败
	if !ok {
		return make(map[string]any)
	}
	return defaults
}

func buildDefaultModelDict(configBase map[string]any) map[string]any {
	defaults := resolveDefaultModelConfig(configBase)
	result := make(map[string]any)
	if mcc, ok := defaults["model_client_config"].(map[string]any); ok {
		result["model_client_config"] = utils.DeepCopyMap(mcc)
	} else {
		result["model_client_config"] = make(map[string]any)
	}
	if mrc, ok := defaults["model_request_config"].(map[string]any); ok {  // ← 键名错误：Python 读 model_config_obj
		result["model_request_config"] = utils.DeepCopyMap(mrc)
	} else {
		result["model_request_config"] = make(map[string]any)
	}
	...
	// ← 缺 model_name → model_request_config["model"] 注入
}
```

三处偏差：
1. **类型断言**：`models["defaults"].(map[string]any)` 恒失败（生产格式 defaults 是 list）→ 也不回退到 `models["default"]` 旧格式 → 恒返回空
2. **键名**：读 `model_request_config`，Python 读 `model_config_obj`（Go runtime_inheritance.go:262/1004 都正确用了 `model_config_obj`，此处不一致）
3. **缺注入**：Python L113-115 将 model_name 注入 `model_request_config["model"]`

后果：`buildAgentsConfig` → `buildAgentSpecDict` 注入的 `model` 字典为 `{model_client_config: {}, model_request_config: {}}`——agents 未显式配置 model 时成员**无模型配置**（`ValidateLeaderModelResolved` 若 leaderAgent.Model 为空会报错，或静默用 nil model）。

此问题与 107603fa commit 声明的 "GetDefaultModels 三处收敛" 直接冲突——收敛检查发现第 4 处旁路旧实现且该处自身有 bug（tm-support T-04 发现，team-lead 深挖确认并扩展出 S-01/S-02）。

**修复方案**：

```go
func resolveDefaultModelConfig(configBase map[string]any) map[string]any {
	models, ok := configBase["models"].(map[string]any)
	if !ok {
		return make(map[string]any)
	}
	// 对齐 Python：defaults 为 list 时取第一个 dict 条目
	if defaults, ok := models["defaults"].([]any); ok {
		for _, item := range defaults {
			if m, ok := item.(map[string]any); ok {
				return m
			}
		}
	}
	// 旧格式回退：default 单对象
	if def, ok := models["default"].(map[string]any); ok {
		return def
	}
	return make(map[string]any)
}

func buildDefaultModelDict(configBase map[string]any) map[string]any {
	defaults := resolveDefaultModelConfig(configBase)
	mcc, _ := defaults["model_client_config"].(map[string]any)   // 键名对齐
	if mcc == nil {
		mcc = map[string]any{}
	}
	mco, _ := defaults["model_config_obj"].(map[string]any)
	if mco == nil {
		mco = map[string]any{}
	}
	// 对齐 Python L113-115：model_name 注入
	if name, _ := mcc["model_name"].(string); name != "" {
		if _, exists := mco["model"]; !exists {
			mco["model"] = name
		}
	}
	// 对齐 Python L117-121：加载完成日志（model_name + provider）
	logger.Debug(logComponent)...
	return map[string]any{
		"model_client_config": utils.DeepCopyMap(mcc),
		"model_request_config": utils.DeepCopyMap(mco),
	}
}
```

注意：修复后此处与 `config.GetDefaultModels`（权威实现，含解密+is_default 推断）语义仍不同——Python `_resolve_default_model_config` 就是 config_loader 的本地简化版（取第一条，不做解密/推断），**不应**改为调用 GetDefaultModels（会引入解密行为差异），只需修正类型断言与键名。

---

### S-04: TeamManager 无锁访问器系统性 data race——8 个 map 访问方法并发读写可触发 Go fatal error

**Python 参考** (`team_manager.py:1441-1448`)：

```python
def get_team_agent(self, session_id: str) -> TeamAgent | None:
    return self._team_agents.get(session_id)      # ← 无锁，但 asyncio 单线程 + GIL 下 dict.get 原子

def get_monitor_handler(self, session_id: str) -> TeamMonitorHandler | None:
    return self._team_monitors.get(session_id)

def register_monitor(self, session_id: str, handler: TeamMonitorHandler) -> None:
    self._team_monitors[session_id] = handler
```

Python 的这些访问器不加 `self._lock` 是安全的：单事件循环线程内无真并发，GIL 保证 dict 单操作原子。**Go 一比一照搬无锁实现则是数据竞争**：Go map 并发读写是运行时 fatal error（`fatal error: concurrent map read and map write`，进程崩溃），不是脏读。

**Go 问题**：以下 8 个方法在 HTTP 请求 goroutine（deep_adapter_team.go 每请求 `go func()`）中被调用，与持锁的 lifecycle 写路径（CreateTeam :215 写 teamAgents、cleanupRuntimeLocals :252/265/276 delete 三个 map、destroyTeam :619 delete）形成并发读写：

| 方法 | 位置 | 无锁操作 | 并发写方（持锁） |
|------|------|---------|----------------|
| `GetTeamAgent` | team_manager.go:304 | 读 teamAgents | CreateTeam :215 / StopSessionRuntime :420 / destroyTeam :619 |
| `GetMonitor` | monitor.go:108 | 读 teamMonitors | cleanupRuntimeLocals :276 |
| `RegisterMonitor` | monitor.go:114 | 写 teamMonitors | cleanupRuntimeLocals :276 |
| `GetTeamEvolutionWatcher` | monitor.go:121 | 读 teamEvolutionWatchers | cleanupRuntimeLocals :252 |
| `RegisterTeamEvolutionWatcher` | monitor.go:128 | 写 teamEvolutionWatchers | cleanupRuntimeLocals :252 |
| `HasStreamTask` | monitor.go:36 | 读 streamTasks | cleanupRuntimeLocals :265 |
| `RegisterStreamTask` | monitor.go:59 | 写 streamTasks | CancelAllStreamTasks :102 / cleanupRuntimeLocals :265 |
| `PopStreamTask` | monitor.go:44 | 读+删 streamTasks | 同上 |

实际触发路径示例：请求 A（新建 session）执行 `CreateTeam` 持锁写 `m.teamAgents[sessionID]` 的同时，请求 B（查询既有 session 状态）执行 `ensureMonitorForActiveRuntime` → `GetTeamAgent(sessionID)` 无锁读同一 map → race detector 必报、生产环境概率性 fatal。适配器层无上游串行化（`processTeamMessageStream` 直接 `go func()`，多请求真并发）。

**修复方案**（统一加锁，注意避免与 lifecycle 既有锁序冲突）：

```go
// GetTeamAgent 获取内存中的 TeamAgent 实例（并发安全）。
func (m *TeamManager) GetTeamAgent(sessionID string) *agent.TeamAgent {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.teamAgents[sessionID]
}
```

同款修改应用到其余 7 个方法。已验证不会引入死锁：
- lifecycle 四处调用（Terminate/Stop/Cancel/Pause :300-301/:354-355/:404-405/:466-467）已在 `m.mu` 持锁区内调用 `HasStreamTask`/`hasLocalTeamRuntime`/`hasMonitor`——**这几个内部方法保持无锁**（调用方持锁），只改 8 个**导出**访问器；lifecycle 内部对导出方法的无锁内联调用（`m.HasStreamTask(sessionID)`）需同步改为直读或内联形式
- `EnsureMonitor`（monitor.go:168-170）已正确持锁读 teamMonitors，其中 `m.RegisterMonitor(sessionID, handler)`（:202）与 `m.RegisterStreamTask(sessionID+"_monitor", cancel)`（:209）调用将变成二次加锁——`sync.Mutex` 不可重入，需将这两处改为锁内直写或将 EnsureMonitor 的注册段移到解锁后

复杂流程示例（以 GetTeamAgent race 为例）：

```
goroutine A（HTTP 请求 1，新 session 创建）          goroutine B（HTTP 请求 2，状态查询）
────────────────────────────────────                ───────────────────────────────────
CreateTeam(ctx, "sess-1", ...)
  m.mu.Lock()
  m.teamAgents["sess-1"] = teamAgent   ← 写
  m.mu.Unlock()
                                                    ensureMonitorForActiveRuntime(ctx, ..., "sess-1")
                                                      tm.GetTeamAgent("sess-1")
                                                        return m.teamAgents["sess-1"]  ← 无锁读
                                                        ↓
                                              与 goroutine A 的写并发 → fatal error: concurrent map read and map write
                                              （-race 下 100% 复现；生产环境低概率进程崩溃）
```

---

### S-05: TeamManager.DestroyTeam 公共包装器自死锁——持锁调用 destroyTeam 后者再次 Lock

**Python 参考** (`team_manager.py:1391-1393`)：

```python
async def destroy_team(self, session_id: str) -> bool:
    async with self._lock:                 # ← 公共方法加锁
        return await self._destroy_team(session_id)   # ← 私有方法不加锁（正确）
```

Python `_destroy_team`（L1400-1432）内部无任何 `self._lock` 获取。调用链 `destroy_team` → `_destroy_team` 只有一次加锁。

**Go 问题** (`team_manager_lifecycle.go:262-266 + 613-621`)：

```go
func (m *TeamManager) DestroyTeam(ctx context.Context, sessionID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.destroyTeam(ctx, sessionID)    // ← 持锁调用
}

func (m *TeamManager) destroyTeam(ctx context.Context, sessionID string) (bool, error) {
	m.cleanupRuntimeLocals(ctx, sessionID)

	m.mu.Lock()                              // ← 再次 Lock：sync.Mutex 不可重入 → 死锁
	teamAgent, ok := m.teamAgents[sessionID]
	...
}
```

`sync.Mutex` 不可重入：goroutine 持锁后再次 Lock **永久阻塞**。对比同文件 `destroyOtherSessions`（:587-599）的正确写法——先持锁收集 staleIDs、**解锁后**再调用 `destroyTeam`。

当前无生产调用者（Go 侧 grep 仅 `teamAgent.DestroyTeam` 即 TeamAgent 自己的方法，非 TeamManager 的），但 Python 侧公共 `destroy_team` 有真实调用者：`remote_member_bootstrap.py:2275`（`get_team_manager("default").destroy_team(envelope_session_id)`，属未回填的 #10.6.20 remote member bootstrap）——**一旦按计划回填 10.6.20，此方法被调用即死锁**。

**修复方案**（二选一）：

方案 A（推荐，对齐 destroyOtherSessions 模式）：

```go
func (m *TeamManager) DestroyTeam(ctx context.Context, sessionID string) (bool, error) {
	return m.destroyTeam(ctx, sessionID)   // destroyTeam 内部已自行加锁，包装器不加
}
```

方案 B（对齐 Python 结构，destroyTeam 拆出无锁内层）：

```go
func (m *TeamManager) DestroyTeam(ctx context.Context, sessionID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.destroyTeamLocked(ctx, sessionID)   // 内层不 Lock，destroyTeam 改名或删除
}
```

同时补一个测试：并发调用 `DestroyTeam` 验证不死锁（`go test -timeout 5s`，死锁会超时失败）。

---

### S-06: BuildEvolutionLLM 丢失 max_retries / ssl_cert 字段——evolution LLM 重试与证书语义退化

**Python 参考** (`team_runtime_inheritance.py:451-476`)：

```python
def build_evolution_llm(config=None):
    ...
    model_client_config, model_config_obj, model_name = resolve_model_config(config)
    request_config = ModelRequestConfig(
        model=model_name,
        temperature=model_config_obj.get("temperature", 0.95),
    )
    client_config = ModelClientConfig(**model_client_config)   # ← 全字段透传
    return Model(model_client_config=client_config, model_config=request_config), model_name
```

`ModelClientConfig` 是 pydantic 模型且 `model_config = {"extra": "allow"}`（foundation/llm/schema/config.py），`**model_client_config` 将 **mcc 中全部字段**（max_retries、ssl_cert 及任意扩展字段）传入构造。

**Go 问题** (`runtime_inheritance.go:279-310`)：

```go
func BuildEvolutionLLM(config map[string]any) (*llm.Model, string) {
	mcc, mco, modelName := ResolveModelConfig(config)
	...
	var clientConfigOpts []llmschema.ModelClientConfigOption
	if t, ok := mcc["timeout"].(float64); ok {
		clientConfigOpts = append(clientConfigOpts, llmschema.WithTimeout(t))
	}
	if v, ok := mcc["verify_ssl"].(bool); ok {
		clientConfigOpts = append(clientConfigOpts, llmschema.WithVerifySSL(v))
	}
	if headers := customHeadersToStringMap(mcc["custom_headers"]); headers != nil {
		clientConfigOpts = append(clientConfigOpts, llmschema.WithCustomHeaders(headers))
	}
	// ← max_retries、ssl_cert 未映射（WithMaxRetries/WithSSLCert 在 llm/schema/config.go:208/218 已存在但未使用）
	clientConfig, err := llmschema.NewModelClientConfig(provider, apiKey, apiBase, clientConfigOpts...)
	...
}
```

后果：用户配置 `max_retries: 10` 时 evolution LLM（TeamSkillEvolutionRail 使用）实际重试次数退为 `NewModelClientConfig` 默认值 3（config.go:241）；配置 `ssl_cert`（自定义 CA 证书路径，企业内网自签场景）完全丢失 → https 校验行为退为系统默认。

**修复方案**：

```go
	if r, ok := mcc["max_retries"]; ok {          // JSON 数值统一 float64，兼容 int
		if n, ok := toInt(r); ok {
			clientConfigOpts = append(clientConfigOpts, llmschema.WithMaxRetries(n))
		}
	}
	if cert, ok := mcc["ssl_cert"].(string); ok && cert != "" {
		clientConfigOpts = append(clientConfigOpts, llmschema.WithSSLCert(cert))
	}
```

（`toInt` 处理 YAML 解析出的 float64/int64 双形态；若项目已有同类 helper 可复用。）

---

### S-07: PendingUserQuery 跨轮保留旧值——inputs 无 query key 时 Go 注入上轮查询（Python 清空）

**Python 参考**（TeamRunner/TeamAgent Invoke 路径，`inputs.get("query", "")`）：

```python
# Python 每次 Invoke 从 inputs 取 query，取不到就是空串——不保留历史
query = str(inputs.get("query", "") or "")
self._state.pending_user_query = query      # ← 恒覆盖：无 query 即清空
```

**Go 问题**（fix-verify 在复核 a750fa26 M-30 修复时发现，team_agent.go Invoke/Stream 路径）：

```go
// team_agent.go（M-30 修复后）：从 inputs 取 query，仅当 key 存在时设置
if q, ok := inputs["query"]; ok {            // ← key 不存在时跳过 → 保留旧值
	if qs, ok := q.(string); ok {
		a.state.PendingUserQuery = qs
	}
}
```

场景：同一 TeamAgent 连续两次 Invoke，第一次 inputs 带 `query: "任务A"`，第二次 inputs 无 query key（如纯工具回调/内部触发）→ Go 侧 `PendingUserQuery` 仍是 "任务A" → 通过 KernelHost.PendingUserQuery() 注入 kernel 的用户查询是过期内容；Python 侧为 ""。

**修复方案**：

```go
// 对齐 Python inputs.get("query", "")：无 key 或非 string 都清空
q, _ := inputs["query"].(string)    // 缺省零值 ""
a.state.PendingUserQuery = q
```

（若 M-30 修复时的意图是"key 存在才更新"，需对照 Python 确认——Python 语义是无条件覆盖为 `inputs.get("query", "")`。）

---

## 三、一般问题 (M)

### M-01: buildAgentsConfig 的 $ref 引用形式与查找路径偏离 Python——`$name` 字符串引用完全不被识别

**Python 参考** (`config_loader.py:186-203`)：

```python
for agent_key, raw_agent_config in agents_raw.items():
    if isinstance(raw_agent_config, str) and raw_agent_config.startswith("$"):   # ← 字符串形式 "$agent_leader"
        ref_name = raw_agent_config[1:]
        if ref_name in top_agents:                       # ← top_agents = config_base["agents"]（顶层）
            agent_config = deepcopy(top_agents[ref_name])
        else:
            logger.warning(...)
            agent_config = {}
    else:
        agent_config = dict(raw_agent_config) if isinstance(raw_agent_config, dict) else {}
```

Python 生产配置（resources/config.yaml:883）：`agents: {leader: $agent_leader}`——字符串引用，查 `config_base["agents"]`。

**Go 问题** (`config_loader.go:275-308`)：

```go
if specMap, ok := spec.(map[string]any); ok {
	if ref, ok := specMap["$ref"].(string); ok && ref != "" {    // ← dict 形式 {"$ref": "..."}
		if configBaseAgents != nil {
			if refConfig, ok := configBaseAgents[ref].(map[string]any); ok {   // ← 查 models["agents"]
```

两处偏差：
1. 引用形式：Go 只识别 `{"$ref": "name"}` dict 形式，Python 的 `"$name"` 字符串形式**直接走 else 分支**（非 dict → `agentConfig = make(map[string]any)`）→ 生产配置的 `leader: $agent_leader` 引用静默变成空配置
2. 查找路径：Go 查 `configBase["models"]["agents"]`，Python 查 `configBase["agents"]`（顶层）

次要偏差：Python 空 agents 配置时注入 `{"leader": {}, "teammate": {}}` 两键（L178-179），Go 仅保证 leader（L314-317）缺 teammate。

**修复方案**：增加字符串引用分支 + 修正查找路径到 `configBase["agents"]`（可保留 `models.agents` 作为兼容回退但需注释声明）；空配置时补 teammate 键。

### M-02: DestroyTeam 之外——`hasLocalTeamRuntime` 读 `config.GetRunnerConfig()` 在持锁路径内的配置热加载开销

（team-lead 验证 S-04 时确认的次要问题）：`hasLocalTeamRuntime`（monitor.go:226-234）每次调用读全局 `config.GetRunnerConfig()`，而它在 Terminate/Stop/Cancel/Pause 四个 lifecycle 方法的**持锁区**内被调用。若 GetRunnerConfig 内部有锁或 IO（配置文件加载），持锁调用会放大锁持有时间。当前 GetRunnerConfig 实现为内存快照读取则无实际影响——**判 M 弱**，建议将 `cfg.DistributedMode` 判定提升到锁外或构造时缓存。

### M-03: TeamSkillEvolutionRail 构造 member_role 硬编码 "leader"——role 传播链断裂

**Python 参考** (`team_runtime_inheritance.py:236-255`)：

```python
if role == "leader" and team_ws_skills_dir:
    ...
    team_skill_rail = TeamSkillEvolutionRail(
        ...
        member_role=role,        # ← 传入实际 role 变量
```

**Go 问题** (`runtime_inheritance.go:544-565`)：

```go
if roleStr(memberInfo.Role) == "leader" && teamWsSkillsDir != "" {
	...
	if sink, ok := teamTrajectoryRegistry.(trajectory.TrajectorySink); ok {
		opts = append(opts, evolution.WithTeamSkillTrajectorySink(sink, teamID, "leader"))   // ← 硬编码
	}
```

当前调用点外层守卫已限定 role=="leader"，行为等价；但 Python 语义是传变量，Go 写死字面量——未来此构造逻辑被 teammate 路径复用（或守卫条件变化）时静默传错 role。sink 断言失败时静默不绑定（L563-565）与 Python `trajectory_sink=bound_team_trajectory_registry` 直接传值的差异同理（Python 无类型断言需求）。

**修复方案**：`evolution.WithTeamSkillTrajectorySink(sink, teamID, roleStr(memberInfo.Role))`。

### M-04: ParsePort 错误降级（raise → warn+default）——配置端口非法时 Python 显式失败、Go 静默连默认端口

**Python 参考** (`distributed_runtime.py:53-71`)：`raise ValueError(f"Invalid {field_name}: ...")`，异常沿 `normalize_distributed_transport_fields`（无 try/except）→ `_load_team_spec`（无 try/except）→ `get_or_create_team` 传播到调用方，配置错误**显式暴露**。

**Go 问题** (`distributed_runtime.go:111-157`)：`logger.Warn + return defaultPort`。注释自述"上层吞掉后使用默认值"——但 Python 侧实际**没有**上层吞掉（tm-support 验证 + team-lead 复核 L292-352/L318 调用链无 except）。非法端口（如 `direct_port: "abc"`）时 Python 报错终止，Go 静默用 18555 连接——分布式部署下表现为难排查的连接失败。

**修复方案**：恢复 error 返回（`NormalizeDistributedTransportFields` 签名加 error），或至少保持 Warn 日志并接受语义差异（需在注释中修正"上层吞掉"的错误描述）。考虑分布式传输路径属 #10.6.20-22 回填链路，可与回填一并处理。

### M-05: `_monitor` 后缀 streamTask 条目在 session 清理路径泄漏

`EnsureMonitor`（monitor.go:209）注册 `sessionID+"_monitor"` 的 cancel 到 streamTasks，但 `cleanupRuntimeLocals`（:257-267）只 delete `m.streamTasks[sessionID]`——`_monitor` 条目永久残留（cancel 从不被调用）。M-05 修复（monitor_handler.go:105-107 注释）已通过 Stop 时 close eventQueue 保证消费者 goroutine 退出（不泄漏 goroutine），但 map 条目本身泄漏：长期运行进程跨多 session 后 streamTasks 积累残留条目。

**修复方案**：`cleanupRuntimeLocals` 步骤 2 增加 `delete(m.streamTasks, sessionID+"_monitor")`（在 cancel 主任务之后）；或 EnsureMonitor 改为注册进独立的 monitorCancels map。

### M-06: GetDefaultModels 边缘差异——defaults 列表全非 dict 条目时 Go 静默 fallthrough

**Python**（config.py:708-709）：`if "defaults" in models and isinstance(models["defaults"], list) and models["defaults"]: return _decrypt_model_entries(models["defaults"])`——defaults 非空 list 直接返回，后续 `entry.get("model_client_config")` 对非 dict 条目会 AttributeError。

**Go**（default_models.go:35-45）：过滤非 dict 条目后 `len(entries)==0` 时 fallthrough 到旧格式/环境变量回退。

全非 dict 条目是配置错误场景（Python 崩溃 vs Go 回退），Go 行为更宽容但语义偏离。判 M 弱（或 T）。**修复方案**：保持 Go 宽容行为但补 Warn 日志说明条目被跳过。

### M-07: attachDistributedHooksForRunnerRuntime 恒返回 false——非分布式模式的 debug 日志分支缺失（回填时注意项）

**Python 参考**（team_manager.py:1457-1563）：非分布式模式返回 False 前有 debug 日志；分布式模式挂 5 hooks 返回 True。

**Go 问题**（team_manager_distributed.go:25-32）：恒 false + ⤵️(#10.6.20-21) 标记。zmq 层与 hooks 本体属 10.6.20/21 合法延后，**占位本身合规**。列为 M 是给回填时的注意事项：函数内需实现非分布式模式的 debug 日志分支，且恒 false 期间调用方在分布式模式下也走"未挂 hooks"路径——若 10.6.19 的调用方逻辑依赖返回值区分模式需确认无误。

### M-08: ParsePort 之外的 distributed_runtime 正常路径确认项（无问题，记录备查）

IsDistributedMode/RuntimeRole/IsPostgresqlStorage/ExtractPgEndpoint/FallbackDistributedToLocal/NormalizeDistributedTransportFields 主流程、EnsurePostgresqlForLeader 三级启动+指数退避逐行对齐（tm-support T-03/T-07 + team-lead 抽验）。此条为正面确认记录，无需修复。

---

## 四、提示 (T)

### T-01: 顶层 default_model/default_workspace/max_iterations/completion_timeout 冗余键

见 S-02e：这些键写入 teamCfg 顶层但 `NewTeamAgentSpecFromDict` 反序列化忽略（TeamAgentSpec 无对应字段）。实际注入链在 buildAgentsConfig 内部完成。修复 S-02 时一并清理。

### T-02: LoadTeamSpecDict 缺少完成日志的字段对齐

Python L339-345 完成日志含 `lifecycle`/`agents keys`/`predefined_members member_name 列表`，Go L74-76 仅 team_name。补齐日志字段利于排查（对齐日志同步规则）。

### T-03: `FileSystemRail` 死键清理

railWhitelist（runtime_inheritance.go:96）含 `"FileSystemRail"` 键，Go 无对应反射类型名，永不匹配。Python 侧同名键同样是死键（SysOperationRail 的构造日志历史命名混乱），两边一致故不算偏差。建议 Go 侧删除该键并同步测试（测试白名单 want 列表也含此项）——或保留并在注释中说明（现状已有注释声明，L87）。

### T-04: `hasLocalTeamRuntime` 的 `_, ok := m.teamAgents[sessionID]`（monitor.go:232）持锁直读

lifecycle 四个方法持锁调用它是安全的（S-04 修复时注意保持此内部方法无锁）。此条提醒 S-04 修复时**不要**顺手给 hasLocalTeamRuntime 加锁（会死锁）。

### T-05: exceptions 8 类 Error() 前缀差异

Go `TeamError("x").Error()` = `"TeamError: x"`，Python `str(TeamError("x"))` = `"x"`。层级（嵌入+Unwrap）、New 构造完全对齐。前缀利于日志排查（判 T）；若上层做错误消息字符串匹配会失配——当前无此类消费。保留现状。

### T-06: MemberSkillToolkitRail 幂等标记依赖 GetTools 返回非 nil（已验证成立）

`r.tools != nil`（member_skill_toolkit_rail.go:42）幂等标记依赖 `toolkit.GetTools()` 返回非 nil 切片——已验证 skill_toolkit.go:166-170 返回三元素字面量切片恒非 nil，语义与 Python `is not None` 等价。此条为验证记录，无需修复。若未来 GetTools 改为动态构建需注意保持非 nil。

### T-07: RuntimeMemberName 返回空串 vs None（已验证无影响面）

Go 返回 `""`、Python 返回 `None`。两侧均无生产消费点（Go 无调用者，Python 仅 wrapper 转发）。验证记录，无需修复。

### T-08: EnsureMonitor 的 consumeCtx 使用 context.Background()

monitor.go:208 `context.WithCancel(context.Background())`——消费者 goroutine 生命周期与请求 ctx 解耦（对齐 Python asyncio.create_task 的 detached 语义），且 M-05 修复后通过 close(eventQueue) 保证退出。合理差异，记录备查。

### T-09: run_command 超时路径子进程回收（已验证无问题）

Go `exec.CommandContext` 超时时库内部处理 kill+wait（CombinedOutput 调用 c.Wait()），无僵尸进程。Python 显式 `await proc.wait()` 与之等价。tm-support 曾疑虑后自行撤回，team-lead 复核确认。

### T-10: GetDefaultModels 的 decryptModelEntries api_key 解密占位（⤵️ 10.5.10 合法）

crypto 依赖方向问题（extensions→common/config 已有依赖，crypto 不能进 common/config），空操作等价 Python crypto=None 跳过路径。占位合规，回填 10.5.10 时接线。

---

## 五、占位标记合法性核查（步骤 3 结论）

对照 IMPLEMENTATION_PLAN.md 逐一确认（本审查重点检查项）：

| 占位标记 | 位置 | 计划状态 | 结论 |
|---------|------|---------|------|
| ⤵️(#10.6.20) attach distributed hooks | lifecycle.go:223 | 10.6.20 ☐ | 合法回填点 |
| ⤵️(#10.6.20-21) AttachDistributedHooksForRunnerRuntime | team_manager_distributed.go | ☐ | 合法（M-07 为回填注意事项） |
| ⤵️(#10.6.22) remote member bootstrap | lifecycle 相关注释 | 10.6.22 ☐ | 合法（S-05 死锁在此回填时触发） |
| ⤵️(10.5.10) api_key 解密 | default_models.go:142 | ☐ | 合法（T-10） |
| ⤴️ 13.8 hybridFactory 回填 | registry.go | 13.8 ✅ | **已回填完成**（e2a703ca），闭环确认 |
| ⤵️(#9.57) WorkspaceSpec 字段 | deep_agent_spec.go:13 | 9.57 状态 | 未回填（历史遗留，不在本窗口） |

**结论**：本窗口内的占位标记全部合法，无"假装待回填实则已实现"或"标记回填点但目标章节已完成仍缺失"的情况。唯一风险点：S-05 的死锁会在 #10.6.20 回填时自动触发，建议在回填该章节前先修 S-05。

---

## 六、修复优先级建议

1. **S-01 + S-02 + S-03**（同一文件 config_loader.go，一次修复）：modes.team dict 断言 → storage 键名 → resolveDefaultModelConfig 类型断言/键名/注入。这三个问题叠加使**生产配置下整个 team spec 加载链路退化为默认值**，是 10.6.19 功能在真实部署下的最大风险。修复后用 Python 生产 config.yaml（resources/config.yaml）做一次端到端 spec 加载对比测试。
2. **S-04**（8 个无锁访问器加锁）：改动面小（8 个方法 + EnsureMonitor 两处调用点改直写），但需仔细核对 lifecycle 内部对这 8 个方法的既有调用（改为内联直读）。`go test -race` 全量验证。
3. **S-05**（DestroyTeam 死锁）：一行改动（包装器去锁），必须在 #10.6.20 回填前完成。
4. **S-06**（BuildEvolutionLLM 字段）：两行 Option 追加。
5. **S-07**（PendingUserQuery 覆盖语义）：一行改动，对照 Python 语义确认。
6. **M-01**（$ref 形式）：字符串引用分支 + 查找路径，与 S-01~03 同文件可一并修。
7. M-03/M-04/M-05：随对应章节回填或单独小修。
8. T 级：随手清理或保留（多数为验证记录）。

---

## 七、验证方式说明

- 所有 S/M 级问题均经 team-lead 独立读取两侧源码逐行对照确认（非仅采信 agent 报告）
- agent 报告中的 1 项 S 级误报（railWhitelist 键数）经独立计数后撤回；2 项待验证 M（M-04 幂等标记、M-03 RuntimeMemberName）已关闭
- 5 个修复 commit（b1c5d4c7/24695cf0/b2112dbf/a750fa26/53394efe）经 fix-verify 逐项 Python 对照复核，全部确认正确无回归
- 13.7/13.8/13.18/registry 经双重验证（fix-verify agent + team-lead 独立读 registry.go/char_chunker.go/hybrid 相关）确认全部对齐，本报告零问题
