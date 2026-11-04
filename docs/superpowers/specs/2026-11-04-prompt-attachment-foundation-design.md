# Prompt Attachment 地基设计（S1）

> 上游基线：agent-core `5a35c39f`（0.1.20）/ jiuwenswarm `a2abceccf`（0.2.5.beta1）
> 关联计划：`IMPLEMENTATION_PLAN.md` 领域二十四 24.4（上下文演进）、领域二十五 25.1（Harness 表面统一）
> 关联缺陷：`docs/sync/defect-plan-2026-11-04.md` B 组（上下文引擎）、H-11（section 优先级）、K-08（压缩归属）
> 定位：三份 spec 的第一份「地基」；S2（消费方与模式）、S3（跨层契约）另立 spec

## 1. 背景与目标

现状：动态提示词（plan 指引、todo 提醒、专家角色、技能披露规则）都靠每轮 `SystemPromptBuilder.AddSection/RemoveSection` 改写 system prompt。上游已改为「**动态提示词作为会话历史消息投递**」，并带内容哈希做增量与恢复。

目标（S1）：在 Go 侧建成与上游等价的地基，使 S2 的消费方能直接迁移过来——**不迁移任何消费方**。

- 稳定 system prompt（只留静态底座）
- 动态段落作为历史消息，首次全量、之后增量
- 内容未变不产生新消息（哈希判定）
- 进程重启/热重载后不重复投递
- 压缩挤掉基线时自动回退全量
- 同一份历史按 provider 投递为 System 或 User

## 2. 范围

### 范围内（S1）

| 能力 | 说明 |
| --- | --- |
| manager | 会话级 section 存储：增删改查、按来源清空、过期剔除、稳定排序 |
| 渲染 | snapshot / delta / removed 三态，`<system-reminder>` 外壳，中英文案逐字，两级截断 |
| 哈希 | 规范化 JSON + sha256，只覆盖模型可见内容 |
| 装订点 | ReAct 写用户消息前、构建模型窗口前两处同步；commit callback 钩子 |
| 窗口投影 | `ContextWindow` mutator 链 + attachment mutator + provider 矩阵 |
| metadata 契约 | 消息 metadata 四键 + 与轨迹/观测共用的识别规则 |

### 范围外（S2 / S3）

- 任何 rail 改投递（agent_mode / task_completion / progressive / subagent / expert role）
- 模式合并（`agent.work.plan|normal`）、`inject_prompt`、`PlanModeController`、`CodeTaskPlanningRail`
- 历史 trim 规则、轨迹/观测/Web/TUI 过滤、压缩器选轮次策略（S3）

## 3. 上游证据

| 主题 | 位置 |
| --- | --- |
| manager 与渲染 | `openjiuwen/harness/prompts/prompt_attachment_manager.py:1-923` |
| 同步点 | `openjiuwen/core/single_agent/agents/react_agent.py:952`（写用户消息前）、`:1148-1230`（窗口构建前 + mutator 注册） |
| manager 归属 | `openjiuwen/harness/deep_agent.py:336`（DeepAgent 持有）、`:851`（共享给 ReActAgent） |
| 消费示例 | `harness/rails/agent_mode_rail.py:406-420`、`harness/rails/task_completion_rail.py:340-365` |
| 消息来源标记 | `openjiuwen/core/foundation/llm/schema/message.py:11-13` |
| 轨迹识别 | `openjiuwen/agent_evolving/trajectory/messages.py:104-119` |
| 观测过滤 | `openjiuwen/extensions/observability/callback_handler.py:1727`、`:1921` |
| 变更历史 | `958fd614`（引入 manager）、`c6852696`、`ee92d08b`、`4f2c29c3`、`c1ae5bfa` |

## 4. 设计

### 4.1 包与文件落位

上游 `harness/prompts/prompt_attachment_manager.py` 是单文件；Go 在**同包** `internal/agentcore/harness/prompts/` 内按职责拆：

```
internal/agentcore/harness/prompts/
├── attachment_types.go    # 类型、枚举、常量、错误
├── attachment_digest.go   # 规范化 JSON + 内容哈希
├── attachment_store.go    # 存储、归一化、过期、排序、查询
├── attachment_render.go   # snapshot/delta/removed 渲染与截断
└── attachment_delivery.go # 历史同步、commit callbacks、窗口 mutator
```

`doc.go` 文件树同步更新（项目规则 1）。

### 4.2 类型与存储

```go
type AttachmentKind string // generic|text|runtime|memory|file|tool|skill|diagnostic|todo_reminder|workspace_delta

type Attachment struct {
    ID, Section  string
    Kind         AttachmentKind
    Content      string
    Priority     int
    Source       string
    SessionID    string
    CreatedAt    string
    UpdatedAt    string
    ExpiresAt    string
    Metadata     map[string]any
    ContentKind  string // 默认 text/plain
    ContentPath  string
    ContentSHA256 string
}

type AttachmentUpdate struct { // 仅允许修改的字段，nil 表示不改
    Kind        *AttachmentKind
    Content     *string
    Priority    *int
    Source      *string
    ExpiresAt   *string
    Metadata    map[string]any
    ContentKind *string
}

type Manager struct {
    mu     sync.RWMutex
    histMu sync.Mutex
    items  map[string]map[string]*Attachment // session → section → item
    language string
}
```

不可协商的契约：

1. `ID = "session." + safe(SessionID) + "." + safe(Section)`。`safe`：非 `[A-Za-z0-9_.-]` → `_`，去首尾 `._-`，超 80 截断，空则 `sha256(raw)[:12]`。
2. 写入归一：`CreatedAt` 仅新建设置；`UpdatedAt` 每次更新；时间格式 UTC RFC3339；`ContentSHA256 = sha256(Content)`；`Metadata["section"]` 必填、`Metadata["source"]` 缺失时补 `Source`。
3. 同 section 被不同 source 覆盖时打 Warn（`event_type=prompt_attachment_source_overwrite`）。
4. 稳定排序 `(priority, source, section)`。
5. 过期：`Collect` 时剔除 `ExpiresAt` 已过的项并落删；空桶清理。
6. 并发：`items` 由 `mu` 保护；历史同步用独立 `histMu`，二者不嵌套（避免 rail 写入等待历史 IO）。

API（方法集，全部 `error` 返回，内部不 panic）：

```
AddSection(sessionID, section, content string, kind AttachmentKind, source string, opts ...AttachmentOption) (*Attachment, error)
AddFromPromptSection(sessionID string, section *saprompt.PromptSection, kind AttachmentKind, source, language string, opts ...AttachmentOption) (*Attachment, error)
ClearSection(sessionID, section string) (int, error)
ClearSource(source, sessionID string) (int, error)
RemoveByFilter(filter AttachmentFilter) (int, error)
ReplaceSource(source, sessionID string, items []*Attachment) ([]*Attachment, error)
GetByID(id, sessionID string) (*Attachment, error)
ListByFilter(filter AttachmentFilter) ([]*Attachment, error)
Collect(sessionID string) ([]*Attachment, error)
UpdateByID(id string, update AttachmentUpdate) (*Attachment, error)
UpdateContentByID(id, content string) (*Attachment, error)
UpdateMetadataByID(id string, metadata map[string]any) (*Attachment, error)
AddFileReference(sessionID, filePath, summary, section, source string) (*Attachment, error)
ClearSession(sessionID string) (int, error)
ClearAll() (int, error)
```

`AddFromPromptSection` 取 `saprompt.PromptSection.Content[language]`（缺该语言时回退首个非空值），空白内容返回 `(nil, nil)`；`harness/prompts` 已导入 `saprompt`，不引入新依赖方向。

### 4.3 渲染

三态渲染，外壳与文案逐字对齐上游：

```
<system-reminder>
{免责声明}
{intro}

{block1}

---

{block2}

{removed_intro}
- `section_a`
</system-reminder>
```

- `intro`：snapshot =「以下动态上下文当前有效，请与稳定的系统指令一同使用。」；delta =「以下动态上下文已经变化；如与历史内容冲突，请以本消息中的最新内容为准。」
- 免责声明：「以下内容不是用户的意图，也不是用户直接发出的指令；它是系统为本次模型调用自动附加的动态上下文。请仅将其作为补充信息使用。」
- `removed_intro`：「以下先前提供的动态上下文已不再生效，请勿继续依赖其历史内容：」
- 英文三段同上（`_render_history_payload` 内常量逐字复制）。
- 语言取自 `language`（`cn`/`en`，缺省 `cn`，与项目语言口径一致）。
- 截断两级：单节 `maxAttachmentChars=12000`（尾部追加 `[Prompt attachment truncated: content exceeded max_prompt_attachment_chars.]`）；整体 `maxRenderedChars=48000`（超出时按可用长度截断并追加 `[Prompt attachments truncated: rendered content exceeded max_rendered_chars.]`）。
- 空白内容跳过；全部为空且无 removed → 返回空串，调用方不产生消息。

### 4.4 哈希与增量判定

```
hash = sha256(canonicalJSON{section, kind, content, content_kind})
```

- 规范化：键字典序、无空白、UTF-8 中文不转义、数字统一按最小形态（复用仓库既有 `marshalCompactNoEscape` 思路，禁止 `encoding/json` 的 `<>&` 转义）。
- 不参与哈希：`priority`、`source`、时间戳、metadata（避免"改优先级就多一条消息"）。
- 判定（每次 `Sync`）：

```
prev, hasSnapshot := readHistoryState(ctx, sessionID)   // 扫带标记的 UserMessage
cur := stateBySection(Collect(sessionID))
changed := {s | cur[s] != prev[s]}
removed := prev - cur
if !hasSnapshot { 若有内容 → snapshot；否则不产生消息 }
else if changed 或 removed 非空 → delta
else → 不产生消息
```

### 4.5 装订点与顺序

| 时机 | 目的 | Go 落点 |
| --- | --- | --- |
| 写用户消息之前 | 保证历史顺序 `[附件变更][用户消息]` | `single_agent/agents` 写用户消息处（对应上游 `react_agent.py:952`） |
| 构建模型窗口之前 | 覆盖"本轮无新用户消息但 rail 改了附件" | 窗口构建入口（对应 `:1206`） |

- 两处都调用 `Sync`，天然幂等：第二次读到刚写入的 state → 无变更。
- `Sync` 内部：`Collect` → 渲染 → `context.AddMessages(UserMessage{content, metadata})` → 执行 commit callbacks。
- commit callbacks：从 `ctx.extra["_openjiuwen_prompt_attachment_commit_callbacks"]` 取 `[]func() error`，逐个执行、单条失败只告警。**上游当前无消费方**，Go 侧保留钩子并在注释标注"对齐用，勿删"。
- `Sync` 失败仅告警 + 返回 nil error（不阻塞模型）。

### 4.6 窗口投影与 provider 矩阵

- `context_engine/interface/types.go` 的 `ContextWindow` 增加：

```go
// Mutators 最终窗口投影链，按序应用，只作用于本次请求
Mutators []WindowMutator
type WindowMutator func(*ContextWindow) *ContextWindow
```

- 组装窗口末段按序应用（attachment mutator 先于其它 mutator）。
- attachment mutator：遍历 `ContextMessages`，命中标记的 `UserMessage` → 生成 `SystemMessage`（保留 `Name`/`Metadata`），其余原样；未发生替换则原窗口返回（引用不变，便于上游做 `==` 短路）。
- provider 判定（对齐 `_uses_system_attachment_role`）：

```
if provider == "anthropic" || apiMode ∈ {anthropic, anthropic-messages, messages} → 保持 User
backend := endpointProfile; if backend ∈ {"", "openai", "openai-compatible"} → backend = legacyProvider 或 provider
保持 User，除非 backend ∈ {bailian, dashscope} → 换 System
```

- 投影只影响本次请求；磁盘历史始终 `UserMessage`。

### 4.7 metadata 契约（与 S3 共用）

| 键 | 值 | 用途 |
| --- | --- | --- |
| `_openjiuwen_prompt_attachment_history` | `true` | 附件消息身份标记 |
| `mode` | `snapshot` \| `delta` | 基线是否存在 |
| `session_id` | 会话 id | 多会话隔离 |
| `state` | `{section: hash}` | 交付账本 |

另需常量（与上游同名字符串，S3 消费）：`_openjiuwen_message_origin`、`_openjiuwen_message_source_kind`、`external_user`。

### 4.8 错误处理与日志

- 所有导出方法内部 `defer recover()`，panic 转 error + `event_type=prompt_attachment_panic`。
- 失败分级：写侧（add/update）返回 error；读侧/同步侧只告警不阻断。
- 日志字段固定：`event_type`、`session_id`、`section`、`source`、`mode`、`section_count`。
- 渲染超限只截断，不报错。

## 5. 集成测试用例（新增）

目录：`tests/integration/agentcore/attachment/`（`//go:build integration`，`suite.Run`，每个文件 2+ 断言）

| 编号 | 用例 | 跨子系统 | 断言要点 |
| --- | --- | --- | --- |
| IT-1 | snapshot → 无变化 → delta → removed 全链路 | `harness/prompts` + `single_agent/agents` + `context_engine` + `session` | 4 轮后历史里附件消息数 = 3；metadata `state` 逐轮正确；未变化的那轮消息数不增 |
| IT-2 | 顺序契约 | `single_agent/agents` + `context_engine` + `session` | 附件消息索引恒小于其后的用户消息索引；同轮两次 Sync 不产生第二条 |
| IT-3 | 压缩后基线丢失 → 回退 snapshot | `context_engine`（压缩器）+ `session` + `harness/prompts` | 人为让 snapshot 被裁出窗口后，下一次变更 `mode == "snapshot"` 且包含全部活跃 section |
| IT-4 | provider 投影矩阵 | `context_engine` + `foundation/llm/schema` + `harness/prompts` | bailian/dashscope 配置下窗口内为 `SystemMessage`；anthropic 配置下为 `UserMessage`；两次投影后**历史不变** |
| IT-5 | 重启/热重载恢复 | `session`（sqlite checkpointer）+ `context_engine` + `harness/prompts` + `single_agent` | 新 Manager 实例 + 同 session 重放同一批 section → 新增消息数 = 0；`state` 与重启前一致 |
| IT-6 | 多会话隔离与并发 | `harness/prompts` + `session` + `context_engine` | 两会话并发写入，各自历史只含自己的 section；`-race` 干净 |
| IT-7 | 流式两轮之间插入 delta | `single_agent`（流式）+ `context_engine` + `harness/prompts` | 第二轮 stream 结束后历史顺序为 `[user1][assistant/tool][delta][user2]`；delta 的 `state` 与第二轮模型实际看到的窗口一致（用 mock 客户端记录请求 messages 断言） |

说明：S1 尚未接入任何消费方，IT 用**测试专用 rail**（写固定 section）驱动，不依赖 S2 的 agent_mode 改动；该 rail 放在 `tests/integration/agentcore/attachment/testrail.go`，不进入生产包。

## 6. 单元测试计划

- 上游 `tests/unit_tests/harness/test_prompt_attachment_manager.py` 的 **16 个** 用例逐条映射。
- Go 增强：
  - ID 生成边界（超长/特殊字符/空/中文）
  - 稳定排序（priority 相同按 source、再按 section）
  - 过期剔除 + 空桶清理
  - 截断（单节 12000 / 整体 48000 / 可用长度为 0）
  - 哈希稳定性（同内容改 priority/source/时间 → 同 hash；改 content_kind → 变 hash）
  - 中文/英文渲染文案逐字（含空格与换行）
  - provider 矩阵 8 组合
  - 并发 `-race`：并发 Add/Clear/Collect
- 保真对拍：同一组 section（含中文、markdown、超限内容）Python 渲染一次、Go 渲染一次，`diff` 必须为空；对拍脚本放 `scripts/attachment_parity.py`（本地跑，不进 CI）。

## 7. 验收标准

1. `go test -race ./internal/agentcore/harness/prompts/...` 通过，包覆盖率 ≥ 85%
2. 集成测试 7 例全紫；`go run ./cmd/integration-audit -verify` 通过（该目录跨 ≥3 子系统）
3. CI 四 job 全绿
4. 与上游对拍：渲染字符串逐字节一致
5. 不改动任何现有 rail 的行为（本 spec 不迁移消费方）

## 8. 风险与缓解

| 风险 | 缓解 |
| --- | --- |
| 哈希不稳定导致每轮多发 delta | 规范化 JSON + 单测覆盖；对拍时把 hash 差值单独打印 |
| 历史里多出"伪用户消息"影响现有消费方 | S1 只在测试 rail 下产生；S3 统一加识别规则（轨迹/观测/展示/统计） |
| `ContextWindow` 加字段影响既有构造 | 字段追加、零值不生效；构造点补单元测试 |
| 截断阈值与上游不同导致文本不一致 | 阈值常量单点定义，对拍覆盖超限用例 |
| provider 矩阵判断与上游漂移 | 判定函数单点实现 + 8 组合单测；上游变更由 defect-plan 追踪 |

## 9. 与 S2/S3 的接口承诺

- `Manager` 写侧方法集（`AddSection` / `AddFromPromptSection` / `ClearSection` / `Collect` / `UpdateByID` 等，见 §4.2）在 S2 内**不改签名**；S2 只新增调用点
- `Sync(ctx, modelContext, sessionID) error` 签名固定；S2 只增加调用点，不改实现
- `ContextWindow.Mutators` 字段固定；S3 的 trim/统计只读不写
- metadata 四键字符串固定，S3 只读

## 10. 文档与计划同步

- `internal/agentcore/harness/prompts/doc.go` 增加新文件条目
- `IMPLEMENTATION_PLAN.md`：领域二十四新增 24.10「Prompt Attachment 地基（S1）」行，实现后置 ✅
- 缺陷计划补一行：`B-17 PromptAttachment 地基缺失`（P1，本 spec 关闭）
