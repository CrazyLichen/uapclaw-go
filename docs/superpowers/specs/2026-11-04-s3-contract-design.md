# S3：附件消息跨层契约设计（收缩版）

> 上游基线：agent-core `5a35c39f`
> 前置：S1（attachment 地基，已完成）、S2（消费方与模式，同批设计）
> 关联缺陷：`docs/sync/defect-plan-2026-11-04.md` H 组（轨迹/观测）

## 1. 背景与目标

S1 把动态提示词作为 `UserMessage` 写进会话历史（内容 `<system-reminder>…</system-reminder>` + 四个 metadata 键）。这类消息在模型侧是正常输入，但在**轨迹与观测**层不是"用户说的话"，必须能被区分；否则会出现：

- 轨迹把提示词当成本轮真实用户输入（评测/微调样本被污染）
- 观测把附件投影后的 `SystemMessage` 当成系统指令（`gen_ai.system_instructions` 语义错位）

目标：补上"识别 + 使用"的契约，并用测试锁住压缩/rewind 后账本一致性。

## 2. 范围（已收缩）

范围内（3 块）：

1. 附件消息识别 API（常量仍留 `harness/prompts`，见 S2-D7）
2. 轨迹侧：按识别规则排除附件消息并修正"本轮用户输入"的切分
3. 观测侧：两处语义纠正（history vs system instructions、结构化输出的 kind 标注）

范围外（经核对**与上游行为一致，不需要改**）：

| 原设想 | 核对结论 |
| --- | --- |
| 轮次切分排除附件消息 | 上游 `context_utils.py:493-545` 与我们的 `processor/round.go:103` 逻辑相同（连续 user 合并），附件消息行为本就一致 |
| 统计口径排除附件消息 | 同口径（按 role 计数），不改 |
| Web 历史过滤附件消息 | `history.json` 是 host 的轮次级记录（`swarm/server/handle_history.go:86-130`），S1 只写 ModelContext，附件消息不进历史列表 |

## 3. 上游证据

| 主题 | 位置 |
| --- | --- |
| 识别函数 | `agent_evolving/trajectory/messages.py:104-119`（`_is_prompt_attachment_user`：metadata 标记 **或** 内容含 `<system-reminder>`） |
| 轨迹裁剪 | 同文件 `_trim_prompt_to_last_user`（保留最后一条真实 user 起的片段） |
| 观测-输入归类 | `extensions/observability/callback_handler.py:1718-1733`（附件属于 chat history，不属于 system instructions） |
| 观测-输出标注 | 同文件 `1915-1930`（system 角色但带 marker + `mode ∈ {snapshot,delta}` → 标注 `kind=prompt_attachment_history`） |
| 我们的轨迹入口 | `internal/evolving/trajectory/extractor.go:247-255`（从 `llm_params["messages"]` 还原消息，**会包含附件消息**） |
| 我们的观测入口 | `internal/agent_teams/observability/callback_handler.go` |

## 4. 设计

### 4.1 识别 API（`internal/agentcore/harness/prompts/attachment_delivery.go`）

```go
// IsPromptAttachmentUser 判断消息是否为附件投递产生的伪用户消息。
//
// Python: trajectory/messages.py:104-119 `_is_prompt_attachment_user`
// 判定顺序：
//  1. metadata 存在且 _openjiuwen_prompt_attachment_history 为 true → 是
//  2. content 含 "<system-reminder>" → 是（兼容被摘要/迁移后丢失 metadata 的形态）
func IsPromptAttachmentUser(msg llmschema.BaseMessage) bool

// IsPromptAttachmentHistory 判断是否为附件历史消息（S1 写入的那一类，要求 marker + mode）。
//
// 用于观测层区分 history 与 system instructions。
func IsPromptAttachmentHistory(msg llmschema.BaseMessage) bool
```

- 常量保持 `prompts` 包内（D7）；S3 消费方 import `harness/prompts`（用户确认与 Python 一致）
- 第二个函数比第一个严格（必须 `mode ∈ {snapshot, delta}`），对齐观测层判据

### 4.2 轨迹侧

`internal/evolving/trajectory/extractor.go`：

- 构造消息列表后，**不删除**附件消息（它们是模型真实输入，轨迹要可复现），但在"切分本轮用户输入"处跳过它们：

```go
// TrimPromptToLastUser 只保留最后一条真实用户消息起的片段。
//
// Python: trajectory/messages.py `_trim_prompt_to_last_user`
// 附件消息（IsPromptAttachmentUser）不算"真实用户消息"，不得作为切片起点。
func TrimPromptToLastUser(prompt []llmschema.BaseMessage) []llmschema.BaseMessage
```

- 若轨迹类型需要"用户轮次"标记（`TrajectoryStep` 上的 user 标记），同样用 `IsPromptAttachmentUser` 排除

### 4.3 观测侧（`internal/agent_teams/observability/callback_handler.go:356-368`）

**与上游的形态偏差（已核实）**：上游用新版 semconv 把请求拆成 `gen_ai.system_instructions` 与 chat history 两堆
（`callback_handler.py:1718-1733`），并在结构化输出上打 `openjiuwen.kind`（`:1915-1930`）。
本项目观测层是 OpenLLMetry 的**扁平形态** `gen_ai.prompt.<i>.role/content`，没有 `system_instructions` 属性。

因此按等价语义落地：在消息条目上追加两个标注

```
gen_ai.prompt.<i>.openjiuwen.kind = "prompt_attachment_history"
gen_ai.prompt.<i>.openjiuwen.mode = "snapshot" | "delta"
```

- 只有 `IsPromptAttachmentHistory` 为真（marker + 合法 mode）才标注，真 system prompt 不标注
- 这样下游可区分"稳定系统提示"与"附件投影后的 system"，语义与上游一致，形态随我们的 semconv

### 4.4 账本一致性契约（测试锁定，不改实现）

压缩与 rewind 会让附件消息消失或折进摘要，行为应保持上游语义：

| 场景 | 期望行为 |
| --- | --- |
| 压缩把 snapshot 折进摘要 | `has_history_snapshot` 为假 → 下次变更发 **全量 snapshot**（不得发 orphan delta） |
| 压缩只丢了 delta | 基线仍在 → 下次变更发 delta |
| rewind 截断到更早 turn | context 里的附件消息随历史消失 → 后续按快照重新建立 |
| 压缩产生摘要消息 | 摘要消息**不得**带附件 marker（不得污染账本） |

## 5. 测试

### 5.1 集成测试（验收主体，消息传递级）

目录：`tests/integration/agentcore/attachment/`（追加文件）与 `tests/integration/swarm/server/adapter/attachment_mode/`（轨迹/观测用例）。
模拟方式：真实请求 → adapter/agent → MockLLM 记录窗口 → 抽取轨迹/观测数据后断言。

| 编号 | 场景与模拟方式 | 跨子系统 | 关键断言 |
| --- | --- | --- | --- |
| IT-S3-1 | 轨迹切分：一轮 chat（产生 snapshot 附件）后抽轨迹 | evolving + context_engine + prompts | 轨迹消息里**保留**附件消息（可复现）；"本轮用户输入"是真实 query 文本；附件不作为切片起点、不被当作 user turn 内容 |
| IT-S3-2 | 观测标注：`client_provider=dashscope`（附件投影为 System）跑一轮 | observability + context_engine + prompts | 附件条目带 `openjiuwen.kind=prompt_attachment_history` 与 `mode` |
| IT-S3-3 | 观测负向：普通对话（无附件） | observability + prompts | 真 system prompt 条目**不带** `openjiuwen.kind` |
| IT-S3-4 | 压缩折掉 snapshot：制造"附件 snapshot 被摘要替换"后触发一次变更 | context_engine + prompts + session | 变更消息 `mode == snapshot`，内容含全部活跃 section（不发 orphan delta） |
| IT-S3-5 | 压缩只丢 delta：基线仍在时触发变更 | 同上 | `mode == delta`，只含变化节 |
| IT-S3-6 | rewind 后重建：rewind 到更早 turn，再产生一次变更 | session(sessionops) + context_engine + prompts | 首次 Sync 发 snapshot；历史消息顺序为 `[附件][用户]` |
| IT-S3-7 | 摘要不污染账本：压缩生成摘要消息 | context_engine + prompts | 摘要消息不带附件 marker；后续 `state` 不被摘要改写 |

### 5.2 单元测试

| 用例 | 断言 |
| --- | --- |
| `IsPromptAttachmentUser` | marker 命中 / 仅内容含 `<system-reminder>` 命中 / 普通消息不命中 |
| `IsPromptAttachmentHistory` | 需 marker + `mode ∈ {snapshot,delta}`；缺失或非法不命中 |
| `TrimPromptToLastUser` | 附件在最后一条真实 user 之后被裁掉；附件不得成为切片起点 |

## 6. 验收

1. 单测 + **7 例集成测试**（IT-S3-1~7，消息传递级）全绿（`-race`）
2. `integration-audit -verify` 通过；改动行覆盖率 ≥85%
3. CI 四 job 全绿

## 7. 风险

| 风险 | 缓解 |
| --- | --- |
| 识别过宽（内容含 `<system-reminder>` 即判附件）误伤真实用户输入 | 仅用于"找本轮真实用户输入"与观测标注，不用于删除消息；单测覆盖普通消息不命中 |
| 轨迹消费方 import `harness/prompts` 造成层级倒挂 | 用户已确认与 Python 一致；如后续出现循环依赖，再把判定下移（不在本次） |
| 压缩实现与上游语义偏差导致账本不一致 | 用 IT-S3-1~4 锁行为；只断言"发 snapshot/delta"与"marker 不污染"，不锁摘要文本 |
