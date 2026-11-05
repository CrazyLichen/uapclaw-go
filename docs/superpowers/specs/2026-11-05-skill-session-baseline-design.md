# Skill 会话基线（session skill baseline）设计

> 对应 Python：`openjiuwen/harness/rails/skills/skill_use_rail.py`（agent-core HEAD `5a35c39f`，0.1.20）
> 计划落点：`IMPLEMENTATION_PLAN.md` **9.19-23 原文回填**（不新增行号、不调整章节顺序）
> 制定日期：2026-11-05

## 1. 背景

`IMPLEMENTATION_PLAN.md` 中**没有** skill baseline 章节（`baseline` 零命中），唯一相关落点是 9.19-23「其他 Rails」里的 SkillUseRail（状态 ✅）。该行列出的能力只有：增量加载、YAML 解析、enabled/disabled 过滤、SkillTool/ListSkillTool 注册、all/auto_list 提示词注入、演化经验附加、includeTools 幂等注册。

但 Go 代码自己承认了缺口（`internal/agentcore/harness/rails/skills/skill_use_rail.go:394`）：

> 动态的「技能变更/演进经验」才走附件（`_update_runtime_skill_attachment:760`，Go 侧未移植该子特性）

核代码后确认 9.19-23 属**超额声明**：会话基线（`skill_use` state）、运行时变更附件、SkillTool 的 session 视图、禁用后清基线全部缺失；`SkillTool`/`ListSkillTool` 也存在与上游 HEAD 的实质漂移。

本设计按"完整对齐"补齐上述缺口，并把 `skill_tool` 的漂移一并修掉。

## 2. 在 agent 会话中的位置与作用

```
装配期
  DeepAgent.Init
    └─ rails 注册（SkillUseRail，priority 见 §5.7 R-1）
         ├─ 注册 skill_tool（includeTools 时兜底 read_file/code/bash）
         ├─ auto_list 模式追加 list_skill
         └─ 决定 skill_mode（all / auto_list）

每轮用户输入
  BeforeInvoke
    ├─ refreshSkillPrompt()                刷新 self.skills（增量 + 过滤）
    ├─ ensureSessionBaseline(sess)         ★首次落盘基线（state key = "skill_use"）
    └─ syncSkillPromptAndAttachment(cbc)   见下

每次模型调用
  BeforeModelCall
    └─ syncSkillPromptAndAttachment(cbc)
         ├─ refreshSkillPromptIfChanged（扫描根/mtime 签名变化才重建）
         ├─ baselineSkills = 会话基线（无基线时退化为 self.skills）
         ├─ fetchEvolutionTexts(baseline ∪ current)
         ├─ buildSkillsSection(baseline, query) → 稳定系统提示词节
         └─ updateRuntimeSkillAttachment(additions, removals)

工具执行期
  skill_tool.invoke(opts.Session)
    └─ GetSkillsForSession(sess) = 基线 ∪ 本会话新发现（按 name 去重）

会话内技能变化（详见 §5.6 三处触发点）
  RefreshSkillRails()
    ├─ 重算扫描根 + disabled
    ├─ ReloadSkills(ctx)
    └─ ClearSessionBaseline(loopSession)
```

作用（与上游一致，逐条对应）：

| # | 作用 | 缺失后果 |
|---|---|---|
| 1 | 技能节取自基线，会话中途新增技能不改系统提示词前缀 | prefix cache 全废，成本/延迟上升 |
| 2 | `GetSkillsForSession` = 基线 ∪ 新增，会话内技能视图稳定 | 提示词里没有、工具里可查，视图不一致 |
| 3 | 技能变更只走附件增量（`skills.runtime_changes`） | 要么全量重写提示词（破 1），要么模型感知不到新技能 |
| 4 | 禁用/卸载后清基线，下轮重拍 | 被禁技能仍可经 skill_tool 调用（stale baseline） |
| 5 | 权限绑定与"必需技能"校验依赖同一视图（上游 `readonly_tool_bindings.py:123`、`validate_skill_source`） | 只读权限判定、MoA 校验失真 |

## 3. 上游实现清单

| 位置 | 内容 |
|---|---|
| `skill_use_rail.py:65-67` | `_SESSION_STATE_KEY="skill_use"`、`_SESSION_STATE_SCHEMA_VERSION=1`、`_RUNTIME_ATTACHMENT_SECTION="skills.runtime_changes"` |
| `:437-442` `before_invoke` | refresh → `_ensure_session_baseline` → `_sync_skill_prompt_and_attachment` |
| `:486-508` `_sync_skill_prompt_and_attachment` | 签名变化才刷新；baseline 取会话态；`_build_skills_section(baseline, query)`；无节则 `remove_section(SKILLS)` |
| `:673-687` `get_skills_for_session` | 无 state → `self.skills`；有 → 基线 ∪ 新发现（name 去重） |
| `:689-698` `_get/_ensure_session_baseline` | 已有态直接返回，否则 `_save_session_baseline(self.skills)` |
| `:700-721` `_load_session_baseline` | 校验 `schema_version`；逐条 `{name, description, directory}` 还原；非法条目跳过并告警 |
| `:733-750` `_save_session_baseline` | `session.update_state({"skill_use": {schema_version, baseline_skills:[…]}})` |
| `:752-781` `_update_runtime_skill_attachment` | additions/removals 差集；空则 `clear_section`，否则 `add_section(kind=SKILL, source=skill_use_rail)` |
| `:783-849` `_build_runtime_skill_change_content` | 中英双语；新增/移除/演进经验三段；全空返回 `""` → `clear_section` |
| `harness/tools/skills/skill_tool.py` | 见 §4 漂移表（路径安全、目录树、嵌套子技能、`content` 渲染、媒体提示） |
| `harness/tools/skills/list_skill.py:44-80` | `mode=all` / `filtered` / fallback 三路（Go 已对齐，仅缺 session） |
| `harness/tools/skills/markdown_media.py`（83 行） | `markdown_has_image_reference` / `markdown_has_video_reference` |
| `server/runtime/agent_adapter/interface_deep.py:11204-11260` | `refresh_skill_rails`：扫描根/disabled 更新 → reload → 清基线 → 传播子 adapter |
| `:11261-11274` | `_clear_skill_session_baseline`（`update_state({"skill_use": None})`，session 为 None 时 no-op） |

## 4. Go 现状与漂移

| 项 | Python HEAD | Go HEAD | 处置 |
|---|---|---|---|
| 基线存取（ensure/load/save） | ✅ | ❌ 全无 | 本次补齐 |
| 提示词节来源 | 基线 | `self.skills` | 本次补齐 |
| `skills.runtime_changes` 附件 | ✅ | ❌（:394 自认） | 本次补齐 |
| `get_skills_for_session` | ✅ | ❌ | 本次补齐（导出为 `GetSkillsForSession`） |
| 清基线触发链 | 3 处 | ❌（`ReloadSkills` 无调用方） | 本次补齐（§5.6） |
| `skill_tool` 描述 | 含"目录树 + 子技能 + relative_file_path" | 仅首句 | 本次补齐 |
| `skill_tool` 路径安全 | `_is_safe_relative_file_path` | 无校验 | 本次补齐 |
| `skill_tool` 返回 | `directory_tree`/`discovered_skill_names`/`content` | 仅 `skill_directory`/`skill_content` | 本次补齐 |
| `skill_tool` 媒体提示 | `hint` 模式三档提示 | 无 | 本次补齐（仅 hint） |
| `list_skill` | 三路 + session | 三路，无 session | 本次补 session |
| 团队技能 rail | `TeamSkillUseRail` 覆写 `get_skills_for_session` | 无 | 不在本次（25.10） |
| `mount/check/validate_skill_source` | ✅（MoA 用） | ❌ | 不在本次（MoA 章节） |
| session-scoped 子 adapter 传播 | `_session_adapters` | 无此概念 | 记偏差 D-1 |

## 5. 设计

### 5.1 数据契约

基线相关常量、结构与全部方法落在 `internal/agentcore/harness/rails/skills/skill_use_rail.go`（不新建包）。工具侧只新增 `internal/agentcore/harness/tools/skills/markdown_media.go`（对应上游同名模块），并同步该包 `doc.go` 的文件树（规则 1）。

```go
const (
    // skillSessionStateKey 会话技能基线的 session state 键
    // Python: SkillUseRail._SESSION_STATE_KEY
    skillSessionStateKey = "skill_use"
    // skillSessionStateSchemaVersion 基线结构版本
    skillSessionStateSchemaVersion = 1
    // runtimeSkillAttachmentSection 运行时技能变更附件节名
    runtimeSkillAttachmentSection = "skills.runtime_changes"
    // runtimeSkillAttachmentSource 附件来源标识
    runtimeSkillAttachmentSource = "skill_use_rail"
)

// skillBaselineState 会话技能基线持久化结构
type skillBaselineState struct {
    // SchemaVersion 结构版本，不匹配则整份忽略
    SchemaVersion int `json:"schema_version"`
    // BaselineSkills 首次快照时的技能列表
    BaselineSkills []skillBaselineEntry `json:"baseline_skills"`
}

// skillBaselineEntry 单条基线技能
type skillBaselineEntry struct {
    // Name 技能名
    Name string `json:"name"`
    // Description 技能描述
    Description string `json:"description"`
    // Directory 技能目录绝对路径（skill_tool 读文件必需，提示词里没有）
    Directory string `json:"directory"`
}
```

读取路径：`state.StringKey(skillSessionStateKey)` → `SessionFacade.GetState`（与 `ProgressiveToolRail` 同款，`progressive.go:687`）。写入：`SessionFacade.UpdateState(map[string]any{skillSessionStateKey: …})`。

**类型容错**（必须，落盘回读类型不稳定）：`schema_version` 按 `int` / `float64` 双类型判定；`baseline_skills` 可能是 `[]any`（每项 `map[string]any`）或 `[]map[string]any`，两路都要接；非法条目跳过并 `logger.Warn`，与上游 `isinstance` 校验对齐。

### 5.2 rail 新增/改动方法（结构忠实，方法归属不变）

| Go 方法 | 对齐 Python | 说明 |
|---|---|---|
| `syncSkillPromptAndAttachment(ctx, cbc) error` | `_sync_skill_prompt_and_attachment`（:486） | 单入口，BeforeInvoke / BeforeModelCall 共用 |
| `refreshSkillPromptIfChanged(ctx) error` | `_refresh_skill_prompt_if_changed`（:510） | 比对扫描根 + mtime 签名 |
| `buildSkillsSnapshotSignature() []skillSnapshotEntry` | `_build_skills_snapshot_signature`（:518） | 元素为 `{Path string; UpdateAt time.Time}`，供上一方法比较 |
| `GetSkillsForSession(sess) []*skillpkg.Skill` | `get_skills_for_session`（:673） | **导出**（Python 为 public 方法，SkillTool/ListSkillTool 与团队 rail 都要用） |
| `getSessionBaseline(sess) []*skillpkg.Skill` | `_get_session_baseline`（:689） | 私有 |
| `ensureSessionBaseline(sess) error` | `_ensure_session_baseline`（:692） | 首次调用落盘 |
| `loadSessionBaseline(sess) []*skillpkg.Skill` | `_load_session_baseline`（:700） | 含 schema/条目校验 |
| `loadSessionState(sess) (map[string]any, bool)` | `_load_session_state`（:722） | 返回"是否存在有效 state" |
| `saveSessionBaseline(sess, skills) error` | `_save_session_baseline`（:733） | `UpdateState` |
| `ClearSessionBaseline(sess) error` | `_clear_skill_session_baseline`（interface_deep:11261） | **导出**；`sess == nil` → no-op（对齐上游 `if loop_session is None: return`） |
| `updateRuntimeSkillAttachment(ctx, cbc, baseline) error` | `_update_runtime_skill_attachment`（:752） | 取附件管理器见 §5.4 |
| `buildRuntimeSkillChangeContent(additions, removals, baseline) string` | `_build_runtime_skill_change_content`（:783） | 中英双语文案一比一 |

`BeforeInvoke` 改为：`refreshSkillPrompt` → `ensureSessionBaseline(cbc.Session())` → `syncSkillPromptAndAttachment`。
`BeforeModelCall` 改为：先提取 `currentQuery`（保留现有 `extractQueryFromMessages` 位置），再 `syncSkillPromptAndAttachment`。

`buildSkillsSection` 增参与上游一致：`buildSkillsSection(skills []*skillpkg.Skill)`（当前读 `r.skills`），`all` 模式在其内做预算裁剪，`auto_list` 模式忽略入参。

### 5.3 SkillTool / ListSkillTool

```go
// getSkills 返回会话可见技能视图；session 为 nil 时退化为当前加载集合
// Python: get_skills=lambda session=None: rail.get_skills_for_session(session)
type skillGetter func(sess sessioninterfaces.SessionFacade) []*skillpkg.Skill
```

- `NewSkillTool(op, getSkills skillGetter, language, agentID string, enableReadImageMultimodal bool)`：删除原 `func() []*Skill` 形参，不留兼容壳。
- `NewListSkillTool(getSkills skillGetter, listSkillModel, language, agentID)`：同上。
- `Invoke` 内从 `tool.NewToolCallOptions(opts...).Session` 取会话（Go 已有 `WithToolSession`，正是上游 `kwargs["session"]`）。
- rail `Init` 传入闭包：`func(sess sessioninterfaces.SessionFacade) []*skillpkg.Skill { return r.GetSkillsForSession(sess) }`。

`skill_tool` 实现补齐（新文件 `internal/agentcore/harness/tools/skills/markdown_media.go` + `skill_tool.go` 内助手）：

| 助手 | 对齐 | 关键约束 |
|---|---|---|
| `isSafeRelativeFilePath(p string) bool` | `_is_safe_relative_file_path`（:94） | 拒绝绝对路径、盘符、`..` 分量（同时按 POSIX/Windows 语义判断） |
| `buildSkillDirectoryAsciiTree(dir string)` | `_build_skill_directory_ascii_tree`（:164） | 深度 ≤ 4、行数 ≤ 200、跳过 `output/temp/assets/node_modules`、返回 `(tree, truncated, partialErrors)` |
| `collectNestedSkillNames(dir string)` | `_collect_nested_skill_names`（:198） | 含 `SKILL.md` 的子目录相对路径，上限 50 |
| `skillLayoutMetadata(dir string) map[string]any` | `_skill_layout_metadata`（:267） | 组装 `directory_tree`/`directory_tree_truncated`/`directory_tree_partial_errors`/`discovered_skill_names`/`discovered_skill_names_truncated` |
| `formatLayoutAppendixForModel(layout, skillName) string` | `_format_layout_appendix_for_model`（:283） | `## Directory layout` 代码块 + 用**父技能名**拼可用示例 |
| `skillMarkdownHasMedia` / `applySkillToolMarkdownImagesHint` | `:68-93` | 先剥离已注入提示再前置；图片提示按 `enableReadImageMultimodal` 二选一，视频提示恒加 |
| `markdownHasImageReference` / `markdownHasVideoReference` | `markdown_media.py:70-77` | markdown/HTML/裸路径三种形态 + 扩展名判定 |

`Invoke` 返回 `data` 增键：`content`（正文 + 附录，纯文本，`BuildToolMessageContent` 会优先取它）、`directory_tree`、`directory_tree_truncated`、`directory_tree_partial_errors`、`discovered_skill_names`、`discovered_skill_names_truncated`。失败分支保持 `success=false` + `error`。

**范围界定**：`multimodal_skill_mode` 只实现 `hint`（上游默认值），`attach` / `branch` 不移植（`branch` 依赖 `tools/mobile_gui/skill_branch`，未移植），9.19-23 回填文本中注明 ⤵️。

### 5.4 附件

取管理器沿用既有模式（`AgentModeRail.attachmentManager`、`ProgressiveToolRail.attachments`）：

```go
if provider, ok := cbc.Agent().(interface{ PromptAttachments() *hprompts.Manager }); ok { … }
```

- `sessionID := cbc.Session().GetSessionID()`；为空直接返回（对齐上游 `if not writer.session_id: return`）。
- 有内容：`AddSection(sessionID, runtimeSkillAttachmentSection, content, prompts.AttachmentSkill, runtimeSkillAttachmentSource)`。
- 无内容：`ClearSection(sessionID, runtimeSkillAttachmentSection)`。
- 历史落痕由既有 `prompts.Manager.Sync`（`attachment_delivery.go:30`，首次快照/之后 delta）负责，本次不改。

### 5.5 提示词节

`all` 模式：逐行 `1. \`name\`：description`，无技能时用"当前任务没有选择任何技能"回退文案；`auto_list` 模式：固定一句话，**不列任何技能**（上游无"部分描述"机制，确认见 `_build_skills_section:526-558`）。预算裁剪 `applySkillBudget` 保持现状（默认不限）。

### 5.6 触发链（三处，推送式）

1. **技能安装/卸载**：`internal/swarm/agents/harness/tools/skill_toolkit.go` 的 `InstallSkill` / `UninstallSkill` 成功后 → `rail.ReloadSkills(ctx)` + `rail.ClearSessionBaseline(opts.Session)`。
2. **MCP 热同步**：`internal/swarm/server/adapter/deep_adapter_mcp.go` `syncMcpServersForRuntime` 的 to_remove / to_add 完成后 → `d.RefreshSkillRails(ctx)`。
3. **配置热更新**：`internal/swarm/server/adapter/deep_adapter_config.go` `updateRuntimeConfig`（及 `ReloadAgentConfig`）末尾 → `d.RefreshSkillRails(ctx)`。

新增 `func (d *DeepAdapter) RefreshSkillRails(ctx context.Context)`（对齐 `interface_deep.py:11154-11260`）：

```go
// 1. 重算扫描根：workspace skills + 绑定包目录（前置、去重）—— Python _skill_scan_dirs
// 2. 更新 disabled：d.skillManager.ListExecutionDisabledSkills()
// 3. d.skillRail.ReloadSkills(ctx)
// 4. d.skillRail.ClearSessionBaseline(d.instance.LoopSession())   // 无活跃会话 → no-op
```

`d.instance.LoopSession()` 与 Python `self._instance._loop_session` 同构（`deep_agent.go:1341`、赋值 `2788`、清空 `2821`）。

### 5.7 附带对齐项（与本次改动同一文件，建议一并修）

- **R-1 `skillUseRailPriority` 100 → 95**：上游 HEAD 已从 100 降到 95（`skill_use_rail.py:57-61`，理由是 init 的 owner 检查必须晚于真正持有 read_file/code/bash 的 rail）。Go 注释仍写"Python: priority = 100"，属过时注释 + 真值漂移。
- **R-2 init 的 owner 检查**：上游 `ability_manager.get(tool.card.name) is not None → continue`（`skill_use_rail.py:377-386`）；Go 无条件注册且会先 `RemoveTool` 同 ID 资源，可能与 SysOperationRail 的 read_file/bash 抢注册（丢只读/deny 策略）。
- **R-3 删除 ResourceMgr 预删除逻辑**：上游无此行为（`add_ability` 单入口），随 R-2 一并删除。

**R-1/R-2/R-3 已确认纳入本次范围**（2026-11-05 决策）。它们会改变工具注册顺序与归属，实施时必须跑全量回归（含 SysOperationRail 只读模式、Bash deny 策略相关用例），并在 9.19-23 回填文本中追加一句说明。

### 5.8 计划回填（9.19-23 原文追加，不新增行号）

在 9.19-23 的 Skill 段落末尾追加：

> ⤴️ 2026-11-05 回填 skill baseline：会话基线（`skill_use` state，schema v1）+ `GetSkillsForSession` 合并视图 + `skills.runtime_changes` 附件（中英双语）+ `ClearSessionBaseline` + 三处触发链（技能安装/卸载、MCP 热同步、配置热更新）+ `skill_tool` 全量对齐（路径安全、目录树、嵌套子技能、`content` 渲染、媒体提示 hint）；⤵️ `multimodal_skill_mode=attach/branch` 未移植（branch 依赖 mobile_gui skill_branch）；⤵️ `mount_skill_root`/`validate_skill_source` 随 MoA 章节；⤵️ `TeamSkillUseRail` 见 25.10

### 5.9 删除清单（规则 8）

| 对象 | 位置 |
|---|---|
| "Go 侧未移植该子特性"注释 | `skill_use_rail.go:394` |
| `getSkills func() []*skillpkg.Skill` 字段类型与两处闭包 | `tools/skills/skill_tool.go:55-81`、`list_skill.go:29-70`、`skill_use_rail.go:266-292` |
| `NewSkillTool` 旧 4 参签名（无兼容壳） | 同上 + 全部 `_test.go` 调用点 |
| ResourceMgr 同 ID 预删除逻辑（R-3） | `skill_use_rail.go:296-318` |
| 旧"仅返回 skill_directory/skill_content"分支 | `tools/skills/skill_tool.go:82-153`（改为带 layout 的新实现） |

## 6. 已知偏差

- **D-1 无 session-scoped 子 adapter**：上游 `refresh_skill_rails` 会遍历 `_session_adapters` 逐个刷新（`interface_deep.py:11254`）。Go 无此概念，`RefreshSkillRails` 只作用于当前 adapter 的 rail。理由：Go adapter 不随会话复制；回收条件：未来引入会话级 adapter 时一并补。
- **保真说明（非偏差）**：`_loop_session` 只在一次运行期间非空（`deep_agent.py:2664` 设 / `2862` 清），因此无活跃会话时清基线本就是 no-op（上游 `getattr(instance, "_loop_session", None)` 为 None 直接 return）。Go 照抄该 best-effort 语义，不额外"修"。

## 7. 测试计划

单测（`internal/agentcore/harness/rails/skills/`）：

1. 无 session state → `GetSkillsForSession` 返回 `r.skills`
2. 首次 `BeforeInvoke` 落盘基线；再次 `BeforeInvoke` 不覆盖（已是同一份）
3. `schema_version` 不匹配 → 忽略并告警，退化为 `r.skills`
4. 基线条目非法（缺 name/directory）→ 跳过该条，其余可用
5. 基线 ∪ 新发现（同名去重）；基线中已删除的技能仍可见（视图稳定）
6. 附件：有 additions/removals → `AddSection`；无变化 → `ClearSection`；`session` 为 nil → 不 panic
7. `buildRuntimeSkillChangeContent` 中英文案、演进经验段
8. `ClearSessionBaseline(nil)` no-op；非 nil 后 `GetState` 为 nil

单测（`internal/agentcore/harness/tools/skills/`）：

9. `isSafeRelativeFilePath`：绝对路径、盘符、`..`、`a/../b` 全拒
10. `buildSkillDirectoryAsciiTree`：深度/行数截断、跳过目录、`partialErrors`
11. `collectNestedSkillNames`：上限 50、truncated 标记
12. `content` 附录渲染：含目录树与"父技能名"示例
13. `markdownHasImageReference`/`markdownHasVideoReference`：markdown 图片、HTML img、裸路径、非媒体链接
14. 媒体提示：`hint` 注入一次、重复调用不叠加（先剥离）

集成（`tests/integration/agentcore/harness/`，跨 harness rail + tools + session 三子系统）：

15. BeforeInvoke → BeforeModelCall（系统提示词含基线技能）→ `skill_tool(session)` 读基线技能 → `ClearSessionBaseline` → 被禁技能不可见

覆盖率：改动文件 ≥ 85%（`go test -cover`）。

## 8. 风险

1. **SkillTool 签名变更波及面**：`NewSkillTool`/`NewListSkillTool` 调用点含测试（`skills_test.go` 6 处）。一次性改完，不留兼容壳。
2. **阈值门禁**：`skill_use_rail.go` 由 1041 行增至约 1240 行，改动行覆盖率需 ≥85%，新增方法必须全覆盖。
3. **附件与历史**：`Sync` 会写 `UserMessage` 进历史；接入后若 section 内容每轮抖动（如演进经验变化），会产生额外历史消息。上游行为如此，测试需用稳定数据断言，避免偶发。
4. **优先级调整（R-1）**若纳入，会改变 rail init 顺序，可能影响现有依赖注册顺序的用例，需全量回归。

## 9. 实施后复核与追加决策（2026-11-05）

首版实现（merge `00d8cbbc`）落地后做了一次逐条复核，发现 5 处与上游不符/来源错误，并逐项定了处置：

| 编号 | 复核发现 | 决策 |
|---|---|---|
| D-1 | `RefreshSkillRails` 被挂在 `updateRuntimeConfig` 末尾，而该函数在 `ProcessMessageImpl` 步骤 16-17 **每条消息**都执行；Python `_update_runtime_config`（`interface_deep.py:14671`）整段不碰技能 rail，刷新只发生在技能域 RPC / MCP 卸载 / `install_candidate` | **B（零偏差）**：删除该调用，不做替代；禁用对存量会话不即时生效一事记 ⤵️ 领域二十七（visibility 写 handler 落地后在其处清基线） |
| D-2 | 只有新增的 `buildSkillsSnapshotSignature` 用**目录** mtime；loader `refreshSkillsIncrementally` 本身已用 `SKILL.md` mtime（与 Python 一致） | **A**：抽共享 `discoverSkillDirs`，loader 与签名同源，签名取 `SKILL.md` mtime |
| D-3 | 技能发现只扫一层；上游 `_discover_skill_dirs` 递归 + 遇首个 SKILL.md 即止 + 跳过 `.`/`output,temp,assets,node_modules` + `visited` 环保护 | **A**：与 D-2 合并实现（递归 + 停止边界 + 跳过表 + 环保护 + 逐层排序）；rail 侧跳过表与 `skill_tool` 侧 `_TREE_SKIP_DIR_NAMES` 按上游保持两套独立常量 |
| D-4 | `RefreshSkillRails` 在扫描根为空时提前 return，跳过 disabled 更新/重载/清基线 | **A**：删除提前 return，按 Python 顺序无条件执行（空目录只导致 reload 告警） |
| D-5 | `skillScanDirs` 读 `instanceOverrides["skills"]`：来源错误且 Go 侧**无写入方**。Python 的 `deep_config.skills` 由 `_bind_skill`（`extension_binder.py:216`）在包绑定时写入，`_instance_overrides`（`interface_deep.py:2108`）只是 create_instance 透传 dict，两者不是同一来源 | **C2**：本轮把 `extension_binder` 全量移植登记为 **15.15a**，落地后回填 `skillScanDirs` 数据源为 `DeepAgentConfig.Skills` 并删除错误分支 |

同时确认的两条事实（写入 §2/§6 口径）：

- Go `DeepAgentConfig.Skills`（`schema/config.go:234`）与 `CreateDeepAgentParams.Skills`（`harness_config/params.go:58`）**字段已对齐**，factory 默认 rail 分支在用（`factory.go:631`）；缺的是**运行时写入侧**（`extension_binder` 未移植，Go 全仓 `ResolvedSkill`/`ResourceRef`/`LoadRecord`/`ExtensionParts` 0 命中）。
- 会话 MCP 技能目录未纳入扫描根，属 §6 D-1（无 session-scoped adapter）的必然结果，不是新增偏差；上游只在 session-scoped 子 adapter 上追加（`interface_deep.py:10217-10229`）。

下一步：先落地 D-2/D-3/D-4 并补齐覆盖率与集成用例隔离（使 CI 转绿），再按 15.15a 的独立计划移植扩展绑定器。
