# SkillManager 集成测试迁移设计（对齐 Python jiuwenswarm / openjiuwen）

- 日期：2026-11-01
- 范围：`internal/swarm/server/runtime/skill`（swarm 版 SkillManager）+ `internal/agentcore/single_agent/skills`（agentcore 版技能子系统）
- 前置结论：**生产代码零改动**（仅新增测试文件、测试 fixture 与包内单测）

## 1. 背景与事实核查

### 1.1 Python 侧测试盘点（已逐文件核实）

| 来源 | 文件 | 行数 | 性质 | Go 现状 |
|---|---|---|---|---|
| jiuwenswarm | `tests/unit_tests/agentserver/test_skill_manager_enable_disable.py` | 106 | 纯字典单测 | 包内 `state_utils_test.go` 已覆盖 |
| jiuwenswarm | `.../test_skill_manager_path_safety.py` | 126 | `tmp_path` + 子类覆写私有方法 | 包内部分覆盖，**无集成** |
| jiuwenswarm | `.../test_skill_manager_team_skills_hub.py` | 354 | `tmp_path` + 子类 + monkeypatch env | 包内 httptest 覆盖，**无集成** |
| jiuwenswarm | `.../test_skill_toolkits.py` | 30 | 真实 `tmp_path` + 真 `SkillManager` | **无集成** |
| jiuwenswarm | `.../test_team_shared_skills.py` | 323 | `tmp_path` + 模块级 monkeypatch | **无集成** |
| jiuwenswarm | `.../test_team_manager_skill_state.py` | 426 | 大量 monkeypatch + 手写 Agent 桩 | 包内部分覆盖 |
| jiuwenswarm | `.../test_team_member_skill_toolkit_rail.py` | 122 | monkeypatch 模块属性 | **无任何测试文件** |
| agent-core | `tests/system_tests/agent/skill/test_skill_real_system.py` | 533 | 系统测试 + `RUN_REAL_LLM_TESTS=1` | ✅ 已迁移到 `tests/integration/real_llm/skill_real_system_test.go` |
| agent-core | `tests/system_tests/agent/skill/test_skill_init.py` | 86 | 单测（mock `SkillUtil` 构造器） | ❌ 未迁移 |
| agent-core | `tests/system_tests/agent/skill/test_remote_skill.py` | 69 | 真网络 + `RUN_GITHUB_TEST=1` | ❌ 未迁移 |
| agent-core | `tests/system_tests/agent/skill/test_skill_creator.py` | 105 | `RUN_REAL_LLM_TESTS=1` | ❌ Go **无 SkillCreator 实现** |
| agent-core | `tests/system_tests/agent/skill/test_skill_evaluator.py` | 105 | `RUN_REAL_LLM_TESTS=1` | ❌ Go **无 SkillEvaluator 实现** |
| agent-core | `tests/system_tests/harness/rail/test_team_skill_rails_system.py` | 226 | mock LLM + 真实 `tmp_path` | ❌ 行为流未迁移（仅选项级用例存在） |

**核实结论：Python 侧不存在 SkillManager 的集成/系统测试目录。** jiuwenswarm 的 `tests/integration` 只有 `test_acp_cli_stdio_integration.py`，`tests/system_tests` 无任何 skill 文件。所谓「迁移集成测试」，实质是**以 Python 单测/系统测试为行为规格，在 Go 重建跨子系统集成测试**。

### 1.2 Go 侧现状

| 项 | 事实 |
|---|---|
| `internal/swarm/server/runtime/skill/skill_manager.go` | 3261 行，54 个导出方法、86 个方法，与 Python 已基本对齐 |
| 包内单测 | 226 个用例，覆盖率 67.3%；`remote_import.go` 0%、`gitClone` 0%、`syncMarketplaceRepos` 18% |
| `tests/integration/swarm/server/skill_manager_test.go` | **Skip 空壳**（`TestSkillManager_待补充`）+ 1 条仅 1 断言（审计判无效） |
| `.integration-scope-exempt` | `tests/integration/swarm/server` 登记为 C 类，理由「需 export_test.go 桥接」 |
| 该豁免理由 | **不成立**：`export_test.go` 只在本包测试二进制中编译，`tests/integration` 作为依赖导入时不可见 |
| `tests/integration/agentcore/single_agent/skills/` | 26 个用例（agentcore 版），目录已豁免（C 类） |
| `tests/integration/agentcore/harness/rails/evolution/` | 已有 1204 行 team skill rail 集成测试，但只对齐 Python **单测**（构造/选项），系统测试的两条行为流缺失 |

### 1.3 规划期发现的生产 bug 与允许的两处改动

| 改动 | 位置 | 理由 | 是否改变对外行为 |
|---|---|---|---|
| 修复 `getLoopController` 类型断言 | `internal/agentcore/harness/rails/evolution/team_skill_create_rail.go:406` | `DeepAgent.LoopController()` 返回 `controller.ControllerInterface`，而断言要求返回 evolution 包未导出的 `loopController`，方法签名不一致 → 断言恒为 false → **团队技能创建提议路径为死代码**（follow-up 永不入队）。改为断言公开接口 + 对返回值二次断言 `EnqueueFollowUp` 能力 | 是：恢复本应触发的 follow-up 入队（Python 语义） |
| 抽出 `searchSkillsFromFiles` 纯函数 | `internal/agentcore/single_agent/skills/remote_skill_util.go` | 根目录 `SKILL.md` 跳过分支（`len(parts)==1`）只在整仓扫描时触发，集成层用 `Directory` 定位无法覆盖；抽纯函数后包内单测可直接驱动过滤分支 | 否：`SearchGitHubForSkills` 行为与返回完全一致 |

## 2. 目标与非目标

### 2.1 目标

1. **A 组**：把 jiuwenswarm 那 7 个单测的行为规格，在 `tests/integration/swarm/server/` 重建为**真实跨子系统**集成测试（真实文件系统、`state.json`、`agent-data.json`、真实 git 命令）。
2. **B 组**：迁移 openjiuwen 剩余三条系统测试（`test_skill_init`、`test_remote_skill`、`test_team_skill_rails_system`）。
3. 清理 `tests/integration/swarm/server` 的 Skip 空壳与已失效的豁免条目。

### 2.2 非目标

| 不做 | 原因 |
|---|---|
| `test_skill_creator.py` / `test_skill_evaluator.py` | Go 侧无 `SkillCreator`/`SkillEvaluator` 实现（属领域十四 devtools），迁移无被测对象 |
| `remote_import.go` 的远程 URL 下载路径进集成层 | 生产代码零改动前提下 `httpClient` 非导出，外部包无法指向 `httptest`；其 httptest 覆盖留在包内单测 |
| 修改除 §1.3 例外之外的生产代码 | 用户决策：零改动；两处例外已单列并说明理由 |
| `SkillManager` 超时/重试行为断言 | 超时与重试参数在包 `init` 时读取，`t.Setenv` 对已加载的全局变量无效 |

## 3. 约束

1. **生产代码基本零改动**：验收以 `git diff --stat internal/` 为准，允许的例外仅 §1.3 列出的两处（P0 bug 修复 + 纯函数抽取）。
2. **mock 只在边界**：网络端点（TeamSkillsHub/ClawHub/GitHub）与 LLM 可 mock；`SkillManager`、`SkillToolkit`、`TeamManager`、`EvolutionStore` 必须真实。
3. **门禁合规**：每个测试文件必须有 `//go:build integration` 且调用 `suite.Run(`；目录聚合 distinct internal import ≥ 2；调用跨度 ≥ 3 或登记豁免并附真实集成边界。
4. **注释与目录**：全中文注释；新增测试目录/文件同步更新对应 `doc.go`。
5. **观测方式**：只读磁盘产物与导出访问器，不触碰非导出字段（`skillsDir`/`state`/`mu`）。

## 4. 落点与用例清单

### 4.1 A 组：swarm 版 SkillManager

落点：`tests/integration/swarm/server/skill_manager_test.go`（原地重写，删除 Skip 空壳与包引用验证）。

| 编号 | 用例组 | 关键断言 | 覆盖的 Python 语义 |
|---|---|---|---|
| A1 | 本地导入 → 卸载全链 | `HandleSkillsImportLocal` 后：`skills/<name>/SKILL.md` 存在、`skills_state.json` 的 `local_skills` 含记录、`<agentRoot>/agent-data.json` 含 `{name,path}`；`HandleSkillsUninstall` 后目录消失且 state 清理；`force=true` 覆盖、`force=false` 返回「已安装」 | `test_skill_toolkits`、`test_skill_manager_path_safety`（uninstall 段） |
| A2 | Marketplace 安装全链（真实 git） | 本地 `git init/add/commit` 仓库当远端 → `HandleSkillsInstall` → `_marketplace/<repo>` 克隆出 `SKILL.md`、技能复制到 `skills/<name>/`、`installed_plugins` 记录含 commit；再次安装走 pull 更新 | 被删的 `TestGitMethods`/`TestSyncMarketplaceRepos` 的真实语义 |
| A3 | TeamSkillsHub（httptest） | `TEAM_SKILLS_HUB_BASE_URL` 指向 `httptest`：info 透传 `?version=`、search 映射 items/count、install 落盘 `skills/<name>/SKILL.md`；zip-slip 条目被拒且不落盘；把 `TEAM_SKILLS_HUB_ALLOWED_DOWNLOAD_HOSTS` 收窄后，非白名单主机 `success=false` 且 detail 含 host | `test_skill_manager_team_skills_hub`（17 例） |
| A4 | 工具链 + 团队共享技能 | `SkillToolkit.InstallSkill/UninstallSkill` 作用于真实 manager 返回 `success`；`EnsureTeamSharedSkillsInitialized` 后 `<team>/skills/{a,b,c}` 镜像齐全、`.team_skills_copied` 标记存在、重复调用幂等、成员 `skills_state.json` 的 `installed_plugins` 为排序后的三名 | `test_skill_toolkits`、`test_team_shared_skills`、`test_team_manager_skill_state` |
| A5 | 启用/禁用与可见性 | `HandleSkillsToggle`（`enabled=false`）→ `ListDisabledSkills` 含该名 → `FilterVisibleSkillNames` 剔除 → 技能提示词不含被禁用技能 | `test_skill_manager_enable_disable` |
| A6 | 路径穿越防御 | 6 个非法名（`../evil`、`nested/skill`、`.`、`..`、空、Windows 路径）→ 导入/卸载均 `success=false` 且 detail 含 `invalid skill name`；frontmatter `name: ../evil` 时磁盘无 `evil/` | `test_skill_manager_path_safety`（前半） |

调用跨度目标：`swarm/server`（skill 包）+ `swarm/agents`（SkillToolkit、TeamManager）+ `agentcore/foundation` 或 `agentcore/runner`（工具卡片/资源管理）。

### 4.2 B 组：openjiuwen 系统测试

| 编号 | 落点 | 内容 | 与 Python 的差异 |
|---|---|---|---|
| B1 | `tests/integration/agentcore/single_agent/skills/skill_init_test.go` | `ReActAgent.SetSkillUtil` 幂等（二次设置不重建）、`SkillUtil.SetSysOperationID` 复用同一实例且 `RemoteSkillUtil()` 的 ID 同步、`RegisterSkills` 后 `HasSkill` 为真 | Go `ReActAgent` **无** `register_skill`/`lazy_init_skill`，Python「缺 `sys_operation_id` 抛 `AttributeError`」无对应路径，仅在注释与第 12 节记录，不伪造断言 |
| B2 | `tests/integration/external/github/remote_skill_test.go` | `RUN_GITHUB_TEST=1` 守卫；`GitHubTree` 指向本仓 `tests/fixtures/remote-skills`；`SearchGitHubForSkills` 返回 3 个文件、2 个技能、`README.md` 被排除；`DownloadFileFromGitHub` 取回 `references/example-reference.md` 内容逐字节相等 | Python 用自己的仓库 `dreamofapsychiccat/remote-skills-test` 并锁死数量；Go 用本仓 fixture，断言改为结构 + 内容双锁，不依赖第三方仓库 |
| B3 | `tests/integration/agentcore/harness/rails/evolution/team_skill_rails_system_test.go` | ①`TeamSkillCreateRail(min_team_members=2)`：2 次 `spawn_member` 后 `follow_up` 恰 1 条且含 `ask_user`、`team-skill-creator` ②`TeamSkillEvolutionRail`：`before_invoke` → 3 次 `after_tool_call` → `after_invoke` → 审批事件 → `ApproveRecord` → 真实 `EvolutionStore` 日志 1 条且 `section == "Workflow"`、内容含 `tighten handoff` | LLM 用 `tests/integration/mockllm`（Python 用 `_MockLLM` 桩类）；Go 无 `drain_pending_approval_events` 同名 API，改用 `ApprovalRuntime` 既有导出方法取事件与 `EvolutionStore()` |
| B4a | `tests/fixtures/remote-skills/` | 4 文件 fixture（见第 7 节） | — |
| B4b | ~~包内单测补根目录跳过分支~~ **不做** | — | 需抽出纯函数才能测，而改 `remote_skill_util.go` 会触发单元 job「修改文件覆盖率 ≥85%」门禁（该文件 5 个函数因 `githubAPI` 为包级常量本就 0%，无法 mock）；且 **Python `test_remote_skill.py` 同样不覆盖此分支**（fixture 根目录无 `SKILL.md`）。为对齐 Python 步骤与本轮零生产改动决策，放弃该用例 |

## 5. 数据流与观测点

```
A1  src/SKILL.md ──HandleSkillsImportLocal──▶ skills/<name>/SKILL.md
                                            → skills/skills_state.json  (local_skills[])
                                            → <agentRoot>/agent-data.json [{name,path}]
                                            → GetLocalSkills() / GetInstalledPlugins()

A2  本机 git 仓库(file://) ──HandleSkillsInstall──▶ skills/_marketplace/<repo>/…
                                                  → skills/<name>/SKILL.md
                                                  → state.installed_plugins[] + marketplaces[].commit

A3  httptest(TEAM_SKILLS_HUB_BASE_URL) ──HttpGet──▶ zip 下载 → 安全解压 → skills/<name>/SKILL.md

A4  TeamAgentSpec.members[].skills ──EnsureTeamSharedSkillsInitialized──▶
    <team>/skills/{a,b,c} + .team_skills_copied
    + <data>/agents/<member>/workspace/skills/ + 成员 skills_state.json

A5  HandleSkillsToggle(enabled=false) → ListDisabledSkills → FilterVisibleSkillNames → GetSkillPrompt()

B3  spawn_member×2 → follow_up；after_invoke → 审批事件 → ApproveRecord →
    EvolutionStore.load_full_evolution_log("research-team")
```

### 5.1 环境隔离要点（已核实）

- `NewSkillManager(tmpDir)` 完全隔离（`agentRoot`/`skillsDir`/`marketplaceDir`/`stateFile` 全部落 `tmpDir`）。
- `NewSkillManager("")` 走**全局** `workspace.AgentRootDir()/AgentSkillsDir()`，测试**禁止**传空串。
- `EnsureTeamSharedSkillsInitialized` 内部使用全局 `workspace.AgentSkillsDir()`，A4 必须 `t.Setenv("UAPCLAW_DATA_DIR", tmpDir)` + `pathutil.ResetCache()`。复用 `tests/integration/testhelpers.AdapterTestEnv` 已有的做法（含 `ResourceMgr.Release`）。
- `agent-data.json` 由 `refreshAgentDataIndexes` 写在 `skillsDir` 的父目录，可直接读盘断言。

### 5.2 相关环境变量（均为运行时 `os.Getenv` 读取，可 `t.Setenv`）

| 变量 | 用途 |
|---|---|
| `UAPCLAW_DATA_DIR` | 全局 workspace 根，隔离 `workspace.AgentSkillsDir()`（A4 必需，配合 `pathutil.ResetCache()`） |
| `TEAM_SKILLS_HUB_BASE_URL` | TeamSkillsHub 端点（A3 指 `httptest`） |
| `TEAM_SKILLS_HUB_ALLOWED_DOWNLOAD_HOSTS` | 下载主机白名单（A3 收窄以触发拒绝分支） |
| `BUILTIN_SKILLS_DIR` | 内置技能目录（A4 或补充用例按需） |
| `IMPORT_LOCAL_ALLOWED_DOWNLOAD_HOSTS` | 本地导入的远程下载白名单（本轮不涉及 URL 导入，仅记录） |
| `RUN_GITHUB_TEST` / `GITHUB_TOKEN` / `GITHUB_SKILL_TEST_REF` | B2 守卫、令牌与 tree ref |

**不可 `t.Setenv` 的项**：`SKILLNET_DOWNLOAD_TIMEOUT`、`SKILLNET_MAX_RETRIES` —— 在包 `init` 时读取为包级变量，测试内设置无效（列入第 12 节差异）。

## 6. Mock 边界

| 依赖 | 处理 | 依据 |
|---|---|---|
| 文件系统 / `skills_state.json` / `agent-data.json` | 真实 `t.TempDir()` | 落盘语义即被测对象 |
| git 命令 | 真实 `git init/add/commit` + `file://` 远端 | 覆盖 `gitClone`（当前 0%） |
| TeamSkillsHub / ClawHub HTTP | `httptest` + `t.Setenv` 覆盖 base URL | 规则 7「mock 只在边界」 |
| LLM（B3） | `tests/integration/mockllm` 注册 `ClientRegistry` 后构造 `*llm.Model` | 已有基础设施 |
| GitHub API（B2） | 真实网络，`RUN_GITHUB_TEST=1` 守卫 | `httpClient` 非导出，零改动下无替代 |
| `SkillManager` / `SkillToolkit` / `TeamManager` / `EvolutionStore` | 真实，禁止 mock | 被测子系统本体 |

## 7. fixture 设计（`tests/fixtures/remote-skills/`）

```
tests/fixtures/remote-skills/
├── README.md                                    # 必须被排除
├── skills/
│   ├── example-skill/
│   │   ├── SKILL.md
│   │   └── references/example-reference.md      # 同目录兄弟文件，须被一并收集
│   └── second-skill/
│       └── SKILL.md                             # 多技能聚合
```

**不在 fixture 根放 `SKILL.md`**：`Directory` 定位时路径前缀使 `len(parts)>1`，根级文件会被误判为以目录名命名的技能。根目录跳过分支本轮不覆盖（见 §4.2 B4b 说明，Python 侧同样未覆盖）。

期望断言：文件数 3、技能数 2（`example-skill`、`second-skill`）、无 `README.md`、`references/example-reference.md` 内容为 `# Example Reference\n\nExample Reference`。

### 7.1 TreeRef 解析（避免 fixture 与测试同 PR 的鸡生蛋）

```
GITHUB_SKILL_TEST_REF  →  GitHubTree.TreeRef，缺省 "main"
CI:  ${{ github.sha }}  →  测试所在 commit 已在远端，且包含本 PR 的 fixture
本地: GITHUB_SKILL_TEST_REF=$(git rev-parse HEAD)（未推送时回落 main）
```

## 8. CI 改动

`.github/workflows/ci.yml` 的 `Integration Test` job 增加 3 个环境变量：

```yaml
- name: 运行集成测试（带覆盖率）
  env:
    CGO_ENABLED: 1
    RUN_GITHUB_TEST: "1"
    GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    GITHUB_SKILL_TEST_REF: ${{ github.sha }}
```

- `GITHUB_TOKEN` 为 GitHub 自动注入的内置令牌，只读公开仓库足够；把匿名 60 次/小时提升到 5000 次/小时，避免限流误红。
- `real-llm-ci.yml` 不改动：B2 只带 `integration` tag，不带 `llm`。

## 9. 豁免清单变更（`.integration-scope-exempt`）

| 变更 | 内容 |
|---|---|
| 删除 | `tests/integration/swarm/server`（C 类）—— 理由「需 export_test.go 桥接」已证伪，且 A 组落地后跨度达标 |
| 新增 | `tests/integration/external/github`（A 类真跨边界）—— 理由：被测边界为外部 GitHub API 网络调用，跨度天然为 `agentcore/single_agent` 单子系统；用例由 `RUN_GITHUB_TEST=1` 守卫 |

## 10. 验收标准

1. `go run ./cmd/integration-audit -min-call-span 3 -defer-span -root tests/integration/swarm/server` 通过（跨度 ≥ 3、有效用例 ≥ 1）。
2. 删除 `swarm/server` 豁免条目后，`go run ./cmd/integration-audit -min-call-span 3 -defer-span` 全量静态审计通过。
3. `external/github` 登记后，`-verify` 覆盖交叉验证（若判灰区）与静态层均通过。
4. `git diff --stat internal/` 仅含 `_test.go` 文件（生产代码零改动）。
5. 远端 CI `Build / Lint / Unit Test / Integration Test` 四绿。

## 11. 风险与缓解

| 风险 | 缓解 |
|---|---|
| GitHub API 抖动/限流导致主 CI 红 | 内置 `GITHUB_TOKEN`；失败信息打印 HTTP status 与 `X-RateLimit-*` |
| 外部仓库内容变化导致断言失败 | fixture 在本仓，随代码版本化；内容断言失败时打印期望/实际 |
| 本地 `git clone` 触发 `safe.directory` 限制 | 用 `file://` URL，必要时 `GIT_CONFIG_COUNT/GIT_CONFIG_KEY_0=safe.directory` 放行 |
| A4 全局 `UAPCLAW_DATA_DIR` 污染其他用例 | `t.Setenv` + `pathutil.ResetCache()`，`Setup/TearDown` 清理；相关用例不 `t.Parallel()` |
| 审计判灰区（跨度 2） | 先 `-root` 定向审计；不足则补一条经 `agentcore/foundation`（ToolCard）的断言 |
| B3 审批事件 API 命名差异导致用例难写 | 先读 `evolution.ApprovalRuntime` 导出方法定稿断言，再落测试；若事件不可观测则改为断言 `EvolutionStore` 日志 |

## 12. 已知差异清单（Go vs Python）

| 差异 | 说明 | 处置 |
|---|---|---|
| `BaseAgent.register_skill` / `lazy_init_skill` 缺失 | Go 只有 `SkillUtil` 与 `ReActAgent.SetSkillUtil` | B1 只迁 SkillUtil 层语义，注释标注差异 |
| `remote_import` 无 client 注入点 | `httpClient` 为非导出字段 | 补测留在包内单测，集成层不覆盖 |
| `TeamSkillRail` 命名 | Go 为 `TeamSkillEvolutionRail` | B3 注释注明对应关系 |
| 根目录 `SKILL.md` 跳过分支 | 只在 `Directory==""` 时触发，且需改生产代码才能测 | 本轮不覆盖（Python 亦未覆盖） |
| `SkillManager` 超时/重试参数 | 包 `init` 时读取全局变量 | 不做断言 |
| `MemberSkillToolkitRail` 无测试 | Python 有 `test_team_member_skill_toolkit_rail.py` | 本轮不做（依赖 `Runner` 全局替换，Go 无 monkeypatch）；如需要，走 A4 的工具链链路部分替代 |

## 13. 实施顺序（供 writing-plans 展开）

1. **B4**：新增 `tests/fixtures/remote-skills/` + 包内单测补根目录跳过分支（最小、可独立验证）。
2. **A1/A6**：swarm `skill_manager_test.go` 重写第一批（导入/卸载/路径穿越），打通目录跨度。
3. **A3/A5**：TeamSkillsHub（httptest）与启用禁用可见性。
4. **A2/A4**：真实 git marketplace 与团队共享技能链路（含 `UAPCLAW_DATA_DIR` 隔离）。
5. **B1/B3**：agentcore 侧两条（SkillUtil 注入幂等、team skill rails 行为流）。
6. **B2 + CI**：GitHub 远程技能测试 + `ci.yml` 三个环境变量。
7. **收尾**：删除 `swarm/server` 豁免、登记 `external/github`、`-root` 定向审计 + 全量审计自检、推送等 CI 四绿。
