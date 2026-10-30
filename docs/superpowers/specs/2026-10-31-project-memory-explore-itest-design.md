# Project Memory 装配 + explore agent 开关 集成测试迁移设计

- 日期：2026-10-31
- 来源：`jiuwenswarm-develop/tests/system_tests/test_project_memory.py`（Python 系统测试，markers `integration` + `system`）
- 目标仓库：`uap-claw-go`
- 决策记录：分层全迁 / 顺手修 / 复用 `tests/integration/swarm/server/adapter` / 双层断言（section + 模型入参）/ 映射表驱动、先单测后集成

## 1. 背景与目标

Python 侧用一份系统测试覆盖了 `/init` 特性的三条链路：ProjectMemoryRail 生命周期、CodeAdapter 对 rail 的装配、explore_agent 的挂载与开关语义。Go 侧实现已基本对齐，但**装配层没有任何测试**——rail 是否带着正确的 workspace / 语言 / 附加目录进入 DeepAgent，explore/plan 子代理是否按 Python 语义挂载与取迭代上限，均无锚点。

本设计把该 Python 测试按「分层全迁」原则拆到 Go 的两层：

- 纯函数行为（文件发现、内容合并）→ `internal` 包单测
- 装配 / 开关 / 传播行为 → `tests/integration/swarm/server/adapter` 集成测试

迁移过程中发现 3 处 Go 与 Python 的行为偏差，按「顺手修」一并修复，测试作为回归锚点。

## 2. 现状盘点

### 2.1 Python 源测试结构

`tests/system_tests/test_project_memory.py` 共 8 个测试类：

| 类 | 名称 | 性质 |
|---|---|---|
| 1 | `TestProjectMemoryRailEndToEnd` | 真实文件系统上的 rail 生命周期 |
| 2 | `TestProjectMemoryFileDiscovery` | 纯函数（发现 / 走查 / 优先级 / glob / include / frontmatter） |
| 3 | `TestCodeModeIntegration` | 适配器装配 + 若干工具函数 |
| 4 | `TestExploreAgentSubagentIntegration` | explore 子代理挂载与开关 |
| 5 | `TestProjectMemoryMergeContent` | 纯函数（合并 / 截断） |
| 6 | `TestProjectMemoryRailModeSwitching` | **空壳**：`pass` 与 `assert True`，作者注释承认 "CodeAdapter 永远挂载" |
| 7 | `TestProjectMemoryRailCacheInvalidation` | 写工具清缓存 |
| 8 | `TestProjectMemoryRailLanguagePropagation` | 语言切换 |

### 2.2 Python 被测装配逻辑（`jiuwenswarm/server/runtime/agent_adapter/interface_code.py`）

- `_build_project_memory_rail()`：`workspace = project_dir or workspace_dir or "./"`；`language = _resolve_runtime_language()`（code 族固定 `en`）；附加目录三级回退 `instance_overrides → config_cache["project_memory"]["additional_directories"] → env`，**仅前两级都为 None 才读 env**。
- `_build_configured_subagents()`：explore/plan **恒挂载**（不读 `enabled`），`max_iterations = parse_int(子配置.max_iterations, parse_int(react.max_iterations, 15))`；code/browser 才按 `enabled: true` 开关。
- `_update_rails_for_mode()`：code 族仅卸载 TaskPlanning/SkillEvolution，保留 SubagentRail / ProjectMemoryRail / CodingMemoryRail。

### 2.3 Go 现状

| 组件 | 位置 | 状态 |
|---|---|---|
| rail 实现 | `internal/swarm/agents/harness/common/rails/project_memory_rail.go` + `project_memory/` | 完整 |
| 适配器装配 | `internal/swarm/server/adapter/code_adapter.go`（`buildProjectMemoryRail` / `buildConfiguredSubagents` / `updateRailsForMode`） | 完整，3 处偏差见 §4 |
| explore 子代理 | `internal/agentcore/harness/subagents/explore_agent.go` | 完整 |
| rail 单测 | `project_memory_rail_test.go`（36 例）、`project_memory/files_test.go`（46 例） | 比 Python 更厚，但有 1 处假断言、3 处缺口语义 |
| rail 集成 | `tests/integration/swarm/agents/harness/common/rails/project_memory_rail_test.go` | 等价 Python 类 1/7/8 |
| 装配集成 | 无 | **缺口** |

### 2.4 迁移会暴露的 Go 偏差

| 编号 | 偏差 | 影响 |
|---|---|---|
| B1 | explore `MaxIterations` 硬编码 15，既不读 `subagents.explore_agent.max_iterations`，也不读 `react.max_iterations` | 配置项完全失效 |
| B2 | plan `MaxIterations` 硬编码 25；Python 恒传 `react.max_iterations`（默认 15），plan builder 自身的 25 默认值永不生效 | 默认值与配置项双重偏离 |
| B3 | `additional_directories` 只读 `instance_overrides`（且只认 `[]any`）与 env，env 为**无条件追加**；Python 有 `config_cache` 一级，且 env 只在两级皆空时兜底 | 配置项失效 + env 优先级错位 |

## 3. 迁移映射表

| Python 类 | Go 落点 | 动作 |
|---|---|---|
| 1 Rail 端到端（5 例） | 现有 `rails/project_memory_rail_test.go` 已覆盖前 4 例 | 不新增；env 附加目录下沉到装配层验（D2） |
| 2 文件发现（5 例） | `files_test.go` 已覆盖根加载 / glob / include / frontmatter paths | 修 1 假断言（C1）+ 补走查（C2） |
| 3 Code 装配（5 例） | 装配 2 例 → 新增集成 D1/D2；`_is_subagent_default_enabled` 生产零引用 → 不迁；`GitWorktreeInfo` / `resolve_workspace_path` 已被单测覆盖 | 集成 2 + 不迁 1 |
| 4 explore 开关（3 例） | 全无 | 新增集成 D3/D4/D5 |
| 5 merge（4 例） | 截断 / 空列表已有 | 补优先级排序（C3）+ 不同路径不合并（C4） |
| 6 模式切换（3 例） | 空壳 | 不迁空壳；以 D1（各 code 子模式均挂载）与 D8（SwitchMode 往返）替代 |
| 7 缓存失效（3 例） | rail 单测 + 现有集成均有 | 不新增 |
| 8 语言传播（4 例） | rail 单测 + 现有集成均有 | 不新增 |
| — | Python 无、Go 需锚 | D6 模型入参端到端、D7 trusted_dirs 注入、D3/D5 的 plan 回退 |

## 4. 行为修复（`internal/swarm/server/adapter/code_adapter.go`）

### B1 / B2：子代理迭代上限

`buildConfiguredSubagents` 中 explore 与 plan 的 `MaxIterations` 改为两段回退（`config` 即 react 配置段，`exploreAgentCfg` / `planAgentCfg` 为 `config["subagents"]` 下的对应子段）：

```go
defaultIters := parseIntAny(config["max_iterations"], 15)
exploreIters := parseIntAny(exploreAgentCfg["max_iterations"], defaultIters)
planIters := parseIntAny(planAgentCfg["max_iterations"], defaultIters)
```

`parseIntAny` 已存在于 `deep_adapter_config.go`，语义与 Python `parse_int` 一致（`nil` / 空串 / 非法值返回默认），直接复用，不新增助手。

- 修复后默认值：explore=15、plan=15（`react.max_iterations` 未配置时）
- `react.max_iterations: 8` → explore=8、plan=8
- `subagents.explore_agent.max_iterations: 5` + `react.max_iterations: 8` → explore=5、plan=8

### B3：附加目录三级回退

`buildProjectMemoryRail` 的 `additional_directories` 解析改为对齐 Python：

1. `instanceOverrides["project_memory_additional_directories"]` 存在（含显式 `nil`）→ 用它
2. 否则 `configCache["project_memory"]["additional_directories"]`
3. 仅当两者都为 `nil` → `os.Getenv("UAPCLAWSWARM_ADDITIONAL_DIRECTORIES")`
4. 归一化：`string` 按 `os.PathListSeparator` 切分后 trim；`[]any` / `[]string` 逐项转字符串后 trim；其他类型 → 空

现有 `normalizeAdditionalDirectories`（`files.go`，已被单测覆盖）负责路径归一化，此处只负责「取值 + 展平」，不重复实现。

### 不改的部分

- `updateRailsForMode` 缺 Python 的「三 rail 补注册」分支：`CreateAdapter` 仅在 `mode == "code"` 时返回 CodeAdapter，`mode != "code"` 的回退分支生产不可达；且 `buildProjectMemoryRail` 永不返回 `nil`，Python 的补注册分支在 Go 等价为死代码。保持现状，测试用 D8 守住「code 族切模式不触发 deep 分支」这一实际约定。

## 5. 单测补口（`internal/swarm/agents/harness/common/rails/project_memory/files_test.go`）

| 编号 | 用例 | 断言 |
|---|---|---|
| C1 | 修 `TestDiscoverAndLoadMemoryFiles_Local优先级最高` | 当前函数体在断言 `scoped.md` 不存在（复制粘贴残留，无效断言）。改为：结果同时含 project 与 local 两类，且 `local.Priority(40) > project.Priority(30)` |
| C2 | `TestDiscoverAndLoadMemoryFiles_子目录向上走查` | root 放 `.git` + `UAPCLAWSWARM.md`；`workspace = target = root/src/feature` → 仍加载 root 的文件（对齐 `test_discovery_walks_up_from_subdir`） |
| C3 | `TestMergeMemoryContent_四类优先级排序` | managed(10) < user(20) < project(30) < local(40) 的输出顺序 |
| C4 | `TestMergeMemoryContent_不同路径同内容不合并` | 两路径同正文 → 输出含两个 `### <kind> memory -- <path>` 头（明写「不去重」语义） |

## 6. 装配层集成测试

### 6.1 文件与基建

- 新增 `tests/integration/swarm/server/adapter/code_adapter_project_memory_test.go`（`//go:build integration`，`package adapter_test`）
- 新增 `tests/integration/swarm/server/adapter/itest_env_helpers_test.go`：把 `code_adapter_coding_memory_test.go::SetupTest` 中的 env 基建抽出为包级助手（provider 注册、`UAPCLAW_DATA_DIR`、`pathutil.ResetCache`、`ResetPersistentCheckpointer`、`config/` 与 `agent/workspace/` 骨架、env 备份恢复），coding_memory 套件改为调用。同目录回归即验证重构正确性。
- 更新 `tests/integration/swarm/server/adapter/doc.go` 的文件目录（该目录 doc.go 已有列出测试文件的先例）

### 6.2 用例矩阵

| # | 用例 | 断言 |
|---|---|---|
| D1 | `TestCreateInstance_各code子模式均挂载` | 对 `subMode ∈ {normal, plan}` 各建一实例：`FindRailsByType(*ProjectMemoryRail)` 恰好 1 个、`ResolveWorkspacePath() == project_dir`、`GetLanguage() == "en"` |
| D2 | `TestCreateInstance_附加目录三级回退` | 配置 `react.project_memory.additional_directories: [extraCfg]`，同时设 env `UAPCLAWSWARM_ADDITIONAL_DIRECTORIES=extraEnv`；跑一轮 `ProcessMessageImpl`（params 不带 `trusted_dirs`）后 section 含 `extraCfg` 的标记、**不含** `extraEnv` 的标记 |
| D3 | `TestCreateInstance_explore与plan默认迭代上限` | `DeepConfig().Subagents` 断言 explore=15、plan=15（B2 回归，修复前为 15/25） |
| D4 | `TestBuildConfiguredSubagents_explore开关被忽略` | `explore_agent.enabled: false` → explore 仍挂载；同 config 中 `code_agent.enabled: false` → code_agent 不挂载（对照组，证明「开关」只对可选子代理生效） |
| D5 | `TestBuildConfiguredSubagents_迭代上限覆盖与回退` | A：`react.max_iterations: 8` → explore=8、plan=8；B：再加 `explore_agent.max_iterations: 5` → explore=5、plan=8（B1 回归） |
| D6 | `TestProcessMessage_ProjectMemory写入模型入参` | ① `SystemPromptBuilder.GetSection("project_memory")` 存在且 `Content["en"]` 含标记；② 捕获的 LLM 入参中 system 消息含 `## Project Memory` 与标记；③ rail 仍在、语言仍 `en` |
| D7 | `TestProcessMessage_trustedDirs注入附加目录` | 请求 `params.trusted_dirs = [extraDir]`（project_dir 无记忆文件、extraDir 有）→ section 含 `TRUSTED-DIR-RULE`，证明 `updateRuntimeConfig` 的 `SetAdditionalDirectories` 与后续 `BeforeModelCall` 串通 |
| D8 | `TestSwitchMode_ProjectMemoryRail不被卸载` | `CreateInstance(mode="code")` → `SwitchMode(ctx, sessionID, "plan")` 无错 → rail 仍 1 个、SubagentRail 仍在、`ContextAssembleRail` **不存在**（证明未误入 deep 的 `agent.fast` 分支）→ 随后 `ProcessMessageImpl(mode="code.plan")` 仍注入 section |

### 6.3 D6 的捕获方式

采用 `callback.GetCallbackFramework().OnLLM(callback.LLMInvokeInput, ...)` / `OnLLM(LLMStreamInput, ...)`，`t.Cleanup(OffLLM)` 反注册，与 `tests/integration/real_llm/system_prompt_capture_test.go::registerPromptCapture` 同一手法，但不依赖真实 API。

不采用 `testhelpers.ModelCallObserver`：它 hook `CallbackBeforeModelCall` 并断言 `ModelCallInputs`，而 `ModelCallInputs` 是在 `react_model_call.go::callModel` 内部才 `SetInputs` 的，晚于 BeforeModelCall rail 的执行时机，在正常 ReAct 路径上取不到消息。该系统提示词提取逻辑（`extractSystemMessages`）需在本包内实现一份（约 30 行），不复用 real_llm 包内未导出函数。

## 7. 明确不做

| 项 | 理由 |
|---|---|
| Python 类 6 空壳用例 | `pass` / `assert True`，零信息量；已有 D1/D8 真实覆盖 |
| `_is_subagent_default_enabled` | Python 侧仅测试调用、生产零引用（`interface_deep.py` 定义后无调用点），迁过来等于给死代码写断言 |
| `updateRailsForMode` 三 rail 补注册 | 生产不可达（见 §4「不改的部分」） |
| Python 类 1/7/8 重复覆盖 | Go rail 单测（36 例）+ 现有 rail 集成已覆盖，重复迁移只增跑时 |
| 重构 `real_llm/system_prompt_capture_test.go` 复用新助手 | 超出本次范围；如需复用另起任务 |

## 8. 验证与门禁

1. `gofmt -l`、`go vet`、staticcheck、`go build ./...`
2. 单测：`go test ./internal/swarm/agents/harness/common/rails/... ./internal/swarm/server/adapter/...`
3. 目标集成：`gofmt` 后 `go test -tags "sqlite_fts5 test integration" -run TestCodeAdapterProjectMemory ./tests/integration/swarm/server/adapter/... -v`
4. 同目录回归：`-run TestCodeAdapterCodingMemory`（验证 env 助手重构未破坏既有套件）
5. 集成门禁：`go run ./cmd/integration-audit -verify`
6. 推送后 CI 四 job 全绿（Build / Lint / Unit Test / Integration Test）

## 9. 风险

| 风险 | 应对 |
|---|---|
| B2 把 plan 默认迭代上限 25 → 15，属运行时行为变更 | 设计已确认；提交信息明确标注，D3 作为回归锚点 |
| B3 改变 env 与配置的优先级（env 由「无条件追加」变「兜底」） | 与 Python 对齐；D2 显式覆盖「配置优先于 env」 |
| 共享 env 助手重构动到已绿套件 | 抽出的助手保持行为等价，Step 4 同目录回归验证 |
| D6 依赖进程级回调单例 | 注册即在 `t.Cleanup` 反注册；同包内无第二个注册方 |
| D8 的 `SwitchMode` 依赖 checkpointer 单例 | 复用 `ResetPersistentCheckpointer` + 每测试独立 `UAPCLAW_DATA_DIR`，与 coding_memory 套件同一隔离手法 |
