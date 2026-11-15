# 15.12 skills 与工作区约定 设计（harness_providers/skills.py + external_cli_spawn 路径段）

- 日期：2026-11-28
- 状态：已与用户对齐（方案 B + 方案 ①）
- 上游 Python 锚点：
  - `openjiuwen/harness_providers/skills.py`（221 行）
  - `openjiuwen/agent_teams/spawn/external_cli_spawn.py`（路径段，行 239-298）
  - 测试：`tests/unit_tests/harness_providers/test_skills.py`（175 行，11 用例）
- 计划条目：`IMPLEMENTATION_PLAN.md` 领域十五 15.12（状态 ☐）

## 1. 背景与流程位置

领域十五把第三方 CLI agent（Claude Code / Codex / DeepSeek Harness）提升为协议层 provider。15.12 是外部成员"正式开工前"的最后一步准备，处于 15.6-15.9（provider 实现）之后、15.13（外部成员生命周期）之前。

**实施顺序（用户拍板：方案 B）**：15.12 提前于 15.7-15.9 实施——skills.py 是独立模块（模块级函数 + 进程锁，不 import provider），可先行落地；provider 侧的接线（config 字段 + 启动调用）挂 ⏵ 15.7/15.8/15.9。

**三分离归属（用户拍板：方案 ①）**：`_resolve_external_paths` 及 workspace meta 路径段随 15.12 一并做，提前建 `internal/agent_teams/runtime/externalcli/` 包；`_build_team_context_tracker` 本体与 spawn 主体留 ⏵ 15.13。

## 2. 三件事的职责（通俗版）

1. **Portable skills 复制**：每个 CLI 有固定的技能发现目录（`_SCAN_DIRS`：claudecode→`.claude/skills`、codex→`.agents/skills`、dsh→`.dsh/skills`）。`install_skills` 在 provider 启动前把技能包整个目录复制到发现目录：名字读自 SKILL.md front matter（缺省用目录名，正则 `[A-Za-z0-9][A-Za-z0-9._-]{0,127}`）；复制前安全校验（软链逃逸/环/非常规文件拒绝，内部软链物化）；重名三策略 skip/replace（备份+失败回滚）/append（`-2`/`-3` 改名并改写 front matter name）；包级 `_COPY_LOCK` 防同项目并发；复制后留存不清理。
2. **cwd/workspace/project_root 三分离**：外部 CLI 是独立进程，不挂载团队工作区。`_resolve_external_paths` 按 `explicit_cwd → worktree_path → project_dir → team_workspace` 选 cwd，四个候选中非 cwd 者（去重、normcase 比对）进 extra_dirs（Claude Code `--add-dir`）。
3. **workspace meta 暴露**：`_team_workspace_path`（workspace.root_path 或 `team_workspace_dir(team_name)` 默认拼出）与 `team_outputs_dir` 以绝对路径进入 TeamContextTracker（tracker 本体 ⏵ 15.13）；`_build_context_project_dir`/`_build_context_team_outputs_dir` 从 build_context 提取。

**单对话边界（2026-11-28 修正）**：外部 CLI 有**两条**消费路径——①团队 spawn（`external_cli_spawn.py:948`，三分离专属）；②**单会话绑定**（jiuwenswarm `external_harness_helpers.py`：界面配置面板启用第三方专家 → `resolve_external_harness_binding` 会话不可变绑定 AgentTemplate manifest → `start_external_harness` 调 `create_harness` → `HarnessIOAdapter.start`，即 15.1-15.5 协议层与 checkpoint 的首个真实消费方）。两条路径都经 provider `harness.start` 触发 `install_skills`，故 skills.go 不分路径。单会话 cwd 由会话项目上下文直接给定（`start_external_harness(cwd=...)` 入参），不走三分离。单会话接入件（binding/start/IOAdapter/会话 checkpoint/面板配置合并）归 **15.18**（用户 2026-11-28 拍板方案 A）。

## 3. Go 交付物

### 3.1 `internal/agentcore/harness_providers/skills.go`（skills.py 1:1）

| Python 符号（行号） | Go 符号 | 说明 |
|---|---|---|
| `SkillSource`（23-40） | `SkillSource` 值类型 + `NewSkillSource(dir, mode, enabledSkills)` | 构造期校验对齐 `__post_init__`：dir 非空、mode∈{all,auto_list}、enabled_skills 字符串数组且 tuple 化 |
| `normalize_skills`（43-59） | `NormalizeSkills(values, conflict)` | conflict 先校验 ∈{skip,replace,append}；输入收窄为 `[]SkillSource`（D-15.12-g），dict/str→SkillSource 由 provider config 反序列化层完成 |
| `_name`（62-80） | `skillName(dir)` | utf-8-sig 读、front matter 未闭合/非 dict 报错、name 正则校验；SKILL.md 须在包内（resolve 相对性） |
| `_discover`（83-90） | `discoverBundles(root)` | 根含 SKILL.md 即单包；否则按排序递归子目录（跳过软链目录） |
| `_validate_tree`（93-103） | `validateTree(path, root, ancestors)` | resolve(strict)→EvalSymlinks 组合（D-15.12-a）；逃逸/环/非常规文件三类拒绝 |
| `_matches`（106-117） | `matchSkills(scan, name)` | 目录名与 front-matter 名 casefold 双轨比对（D-15.12-b：EqualFold）；YAMLError 容错跳过 |
| `_next_copy_name`（120-127） | `nextCopyName(scan, name)` | `-2` 起步、128 字符截断 |
| `_rename_copied_skill`（130-143） | `renameCopiedSkill(bundle, name)` | 无 front matter 直接返回；yaml.safe_dump(allow_unicode, sort_keys=False) 语义对齐 |
| `install_skills`（146-221） | `InstallSkills(sources, provider, cwd, conflict, scanDir)` | 全量步骤 1:1：sources 空→空；conflict 校验；scan_dir 覆盖 `_SCAN_DIRS`；project resolve(strict)；planned 收集；`_COPY_LOCK`→包级 `sync.Mutex`；scan 逃逸校验；mkdir；逐包 skip/replace/append 分支；tempdir 暂存（D-15.12-c）；existed/backup/rename 回滚序逐行对齐；返回 installed tuple |

包级 `_SCAN_DIRS`/`_COPY_LOCK` → 常量块 + 包级 `sync.Mutex`。

### 3.2 `internal/agent_teams/runtime/externalcli/`（新包，提前建）

本轮只落 external_cli_spawn.py 行 239-298 路径段：

| Python 符号（行号） | Go 符号 |
|---|---|
| `_path_value`（239-243） | `pathValue(value)` — 非 str/空串→nil |
| `_same_path`（246-247） | `samePath(l, r)` — normcase+normpath → `filepath.Clean` + 大小写归一（Windows 语义保留） |
| `_append_extra_dir`（250-257） | `appendExtraDir(result, path, cwd)` |
| `_team_workspace_path`（260-264） | `teamWorkspacePath(workspace, teamName)` — **前置件缺口**：`agent_teams.paths.team_workspace_dir`（paths.py:168，`{team_home}/team-workspace/`）Go 无对应物；落点为既有 `internal/agent_teams/paths.go`（该文件即 paths.py 的 1:1 对应物，已有 `TeamHome`）新增 `TeamWorkspaceDir(teamName)`，externalcli 的 `teamWorkspacePath` 转调之 |
| `_build_context_project_dir`（267-271） | `buildContextProjectDir(spec)` |
| `_build_context_team_outputs_dir`（274-278） | `buildContextTeamOutputsDir(spec)` |
| `_resolve_external_paths`（281-298） | `ResolveExternalPaths(spec, ctx, configuredCwd, teamName)` — 选 cwd 顺序与 extra_dirs 收集逐行对齐 |

`doc.go` 按规则 1 建立并注明「15.13 续写 spawn 主体」。

### 3.3 接线（不在本节，挂 ⏵）

- claudecode/codex/dsh config 的 `skills`/`skill_conflict`/`add_dirs` 字段与构造期 `normalize_skills` → ⏵ 15.7/15.8/15.9
- provider `harness.py` 启动时 `install_skills` 调用（线程化）→ ⏵ 15.7/15.8/15.9
- `_build_team_context_tracker`、spawn 主体 → ⏵ 15.13
- `jiuwenswarm` 侧 external_cli 面板/runtime manifest → ⏵ 15.16

## 4. 已知偏差（全部经用户 2026-11-28 拍板）

| 编号 | Python | Go | 理由 | 回收条件 |
|---|---|---|---|---|
| D-15.12-a | `Path.resolve(strict=True)`（169/173/186 等处） | `filepath.Abs` + `os.Stat` + `filepath.EvalSymlinks` 组合 | Go 无 strict resolve | 无；行为等价（不存在→error、软链→真实路径） |
| D-15.12-b | `str.casefold()`（115） | `strings.EqualFold`（Unicode 简单折叠） | 技能名正则已限 `[A-Za-z0-9._-]`，特殊折叠字符（ß 等）不可达，影响面趋近零 | 技能名允许集放宽时复刻完整折叠表 |
| D-15.12-c | `tempfile.TemporaryDirectory(prefix, dir=project)` 上下文管理器 | `os.MkdirTemp(project, ".openjiuwen-skill-*")` + defer 清理 | 语言特性差异；prefix 语义对齐（Go 的 pattern 末尾 `*` 等价 prefix+随机段） | 无 |
| D-15.12-d | yaml（`yaml.safe_load`/`safe_dump`，utf-8-sig BOM 剥离、`splitlines` 行界） | gopkg.in/yaml.v3 + 既有 splitlines 语义复刻惯例（13.18 先例） | 库差异；dump 的 `sort_keys=False`→yaml.v3 默认保序、`allow_unicode`→默认不转义 | 无 |
| D-15.12-e | Python 单文件 `external_cli_spawn.py` 由 15.12/15.13 两批填充 | `externalcli` 包同批文件分两批续写 | 方案 ① 提前建包的结构代价 | 15.13 完成后同一文件内合并核对方法清单 |
| D-15.12-f | `_path_value(value: object)`（239-243，返回 `str \| None`） | `pathValue(value string) string`（空白→空串） | Python 调用面入参恒为 str/None（`configured_cwd`/`ctx.worktree_path`/getattr 结果），Go 收窄为 string + 空串语义等价 | 调用面出现非 str 类型时恢复 any + 类型守卫 |
| D-15.12-g | `normalize_skills` 接受 `list/tuple` 内混合 `SkillSource/str/Mapping` 三形态 | 收窄为 `[]SkillSource`，dict/str→SkillSource 转换移至 provider config 反序列化层 | 用户 2026-11-28 拍板（Go 侧原始配置在 config unmarshal 处即可定型，避免三形态运行时分派）；三分支在 Python 各有真实调用方（config `__post_init__` / `entry.skills` / 二次幂等直通） | 15.7-15.9 接线时若发现 unmarshal 层无法承载，恢复三形态 |
| D-15.12-h | `TeamAgentSpec.build_context: Optional[Any]`（blueprint.py:645，exclude=True，装活实例） | `BuildContext harnessschema.BuildContextInterface` + `json:"-"` | 计划原定 any 槽位基于"物化前为 dict"的错误前提；Python exclude=True + 活载体语义下接口类型更忠实，且避免 JSON 多吐字段 | `build_context_seed` 字段与 materialize 工厂流程 ⏵ 15.13（Python blueprint.py:660-699） |

## 5. 测试面

- `test_skills.py` 11 用例全量 1:1 转 Go（参数化还原 pytest parametrize）：
  1. 完整包 + 三 conflict 策略（×provider×relative 参数化）
  2. 库选择 + front-matter 名与目录名冲突
  3. copy 失败保留既有技能（monkeypatch shutil.copytree）
  4. 软链逃逸拒绝 + 自复制 no-op
  5. 并发 skip 只落一份完整技能
  6. 并发 append 保留两份
  7. cwd 缺省用进程 cwd
  8. 非法配置/技能名
  9. install rename 失败恢复原目录
  10. 内部软链物化
  11. replace 目标为软链时不改动源
- 三分离函数用例：从 external_cli_spawn 测试中取路径段对应部分（选 cwd 四级顺序、extra_dirs 去重、normcase 边界）。
- 并发用例用 `sync.WaitGroup`+goroutine 对齐线程语义；monkeypatch 处用接口/函数变量注入。
- 覆盖率目标：`harness_providers` 包 ≥85%（skills.go 函数级 100%）、`externalcli` 包 ≥85%。

## 6. 删除清单

无——本节为全新实现，无被替代的旧实现（已核实：Go 侧无 skills.go、无 ResolveExternalPaths、无 externalcli 包；`DeepAdapter.skillScanDirs` 属内部 DeepAgent 技能扫描，与外部 CLI 发现目录复制无关，不动）。

## 7. 锚点核查（完成后验收）

- `rg -i "install_skills|InstallSkills" internal/` 命中且仅 harness_providers/skills.go + 测试
- `rg "ResolveExternalPaths" internal/` 命中且仅 externalcli 包 + 测试
- `IMPLEMENTATION_PLAN.md` 15.12 行 ☐→✅，⏵ 15.7/15.8/15.9/15.13 回填标记落对应行
- `go build ./...`、`go vet ./...`、`CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...` 全绿
