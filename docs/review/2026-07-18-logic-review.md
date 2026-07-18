# 2026-07-18 逻辑审查报告

> 审查范围：48小时内提交记录（`1869fe8..6d19b2b`，共7个提交）
> 涉及章节：9.70a Operator 基础接口、9.70b Dataset + Constant、9.70 Trainer、9.71 Evaluator、9.56-9.57 Blueprint/AgentConfigurator、10.3 Adapter/Session
> 对照标准：Python 参考项目方法签名和步骤一致性
> 审查方法：逐文件对比 Python 源码与 Go 实现，检查方法签名、逻辑步骤、占位代码

---

## 严重问题 (S)

### S01: LLMCallOperator 不支持多消息格式 prompt

**严重性**：严重 — 自演化框架无法修改或恢复多消息格式 prompt（如 system_message 列表场景），GetState/LoadState 类型收窄导致 `DefaultApplyUpdate` 中 `stateEqual` 在多消息场景下失败

**Python 样例**：

```python
# openjiuwen/core/operator/llm_call/base.py:31-33
class LLMCallOperator(Operator):
    def __init__(self, system_prompt: str | List[Dict], user_prompt: str | List[Dict], ...):
        self._system_prompt = PromptTemplate(content=system_prompt)  # str 或 List[Dict]
        self._user_prompt = PromptTemplate(content=user_prompt or DEFAULT_USER_PROMPT)

    def get_state(self) -> Dict[str, Any]:
        return {
            "system_prompt": self._system_prompt.content,  # 保留原始类型：str 或 List[Dict]
            "user_prompt": self._user_prompt.content,
        }

    def load_state(self, state: Dict[str, Any]) -> None:
        content = (
            state["system_prompt"]
            if isinstance(state["system_prompt"], (str, list))  # ✅ 支持 list 类型还原
            else str(state["system_prompt"])
        )
        self._system_prompt = PromptTemplate(content=content)
```

**Go 问题**：

```go
// internal/agentcore/operator/llm_call/llm_call_operator.go:66
func NewLLMCallOperator(systemPrompt, userPrompt string, ...) *LLMCallOperator {
    // ❌ 只接受 string，不接受 []map[string]any

// llm_call_operator.go:139-144
func (op *LLMCallOperator) GetState() map[string]any {
    return map[string]any{
        TargetSystemPrompt: op.systemPrompt.Content,  // ❌ 始终是 string
        TargetUserPrompt:   op.userPrompt.Content,    // ❌ 始终是 string
    }
}

// llm_call_operator.go:228-237
func promptContent(value any) string {
    switch v := value.(type) {
    case string:
        return v
    case []any:
        return fmt.Sprintf("%v", v)  // ❌ 产生无意义字符串如 "[map[role:system content:...]]"
    default:
        return fmt.Sprintf("%v", v)
    }
}
```

**修复方案**：

1. `NewLLMCallOperator` 的 `systemPrompt`/`userPrompt` 参数改为 `any`，支持 `string` 和 `[]map[string]any`
2. `promptContent` 对 `[]any`/`[]map[string]any` 类型保留原始结构存入 `PromptTemplate`
3. `GetState()` 返回原始类型（string 或 []map[string]any）
4. `LoadState()` 对 list 类型正确还原

---

### S02: SkillExperienceOperator.PreviewUpdate 错误路径缺少 ChangeType 字段

**严重性**：严重 — 下游无法获取 `change_type` 信息进行分类处理或日志追踪，与 Python 行为不一致

**Python 样例**：

```python
# openjiuwen/core/operator/skill_call/base.py:64-75
return ApplyResult(
    operator_id=self.operator_id, target=target, applied=False,
    mode=update.mode, effect=update.effect, value=update.payload,
    change_type=update.change_type,  # ✅ 包含 change_type
    errors=[f"unsupported target for SkillExperienceOperator: {target}"],
    metadata=dict(update.metadata),
)
```

**Go 问题**：

```go
// internal/agentcore/operator/skill_call/skill_experience_operator.go:102-106
return schema.ApplyResultWithErrors(
    op.OperatorID(), target,
    update.Mode, update.Effect, update.Payload,
    fmt.Sprintf("unsupported target for SkillExperienceOperator: %s", target),
)
// ApplyResultWithErrors 不包含 ChangeType 参数，始终设为 nil
```

**修复方案**：

在 `ApplyResultWithErrors` 中增加 `changeType *string` 参数，或在 `SkillExperienceOperator.PreviewUpdate` 错误路径中手动构建完整 `ApplyResult`：

```go
return schema.ApplyResult{
    OperatorID: op.OperatorID(), Target: target, Applied: false,
    Mode: update.Mode, Effect: update.Effect, Value: update.Payload,
    ChangeType: update.ChangeType,  // ✅ 传递 change_type
    Records: []any{}, Errors: []string{fmt.Sprintf("unsupported target...")},
    Metadata: schema.MetadataClone(update.Metadata),
}
```

---

### S03: SkillExperienceOperator 使用本地常量而非 schema 共享常量

**严重性**：严重 — 如果 `schema.ExperiencesTarget` 值变更，skill_call 包不会同步，造成运行时行为不一致

**Python 样例**：

```python
# openjiuwen/core/operator/skill_call/base.py:9-14
from openjiuwen.agent_evolving.protocols import (
    EXPERIENCES_TARGET,  # ✅ 从共享契约层导入
    APPEND_MODE, MERGE_MODE, LOCAL_APPLY_COMPLETED, PENDING_CHANGE_EFFECT,
)
```

**Go 问题**：

```go
// internal/agentcore/operator/skill_call/skill_experience_operator.go:36
const (
    experiencesTarget = "experiences"  // ❌ 本地硬编码
)
// schema.ExperiencesTarget 已在 internal/evolving/schema/protocol.go:24 定义为 "experiences"
```

**修复方案**：

删除 `experiencesTarget` 本地常量，所有引用改为 `schema.ExperiencesTarget`。

---

### S04: LLMAsJudgeMetric.Compute 中 question 为 nil 时输出 `<nil>` 而非空字符串

**严重性**：严重 — 当没有提供 `question` 时，Go 模板中的 `[Question]: <nil>` 会干扰 LLM 判断，Python 中则是 `[Question]: ` 空字符串

**Python 样例**：

```python
# openjiuwen/agent_evolving/evaluator/metrics/llm_as_judge.py:40-51
def compute(self, prediction, label, question=None, **kwargs):
    messages = self._template.format({
        "question": str(question or ""),  # ✅ None → 空字符串 ""
    })
```

**Go 问题**：

```go
// internal/evolving/evaluator/metrics/llm_as_judge.go:82-90
mc := applyMetricOptions(opts...)
formatted, err := m.template.Format(map[string]any{
    "question": fmt.Sprintf("%v", mc.question),  // ❌ nil → "<nil>"
})
```

**修复方案**：

```go
questionStr := ""
if mc.question != nil {
    questionStr = fmt.Sprintf("%v", mc.question)
}
formatted, err := m.template.Format(map[string]any{
    "question": questionStr,
})
```

---

### S05: execute_updates 函数缺失（含 None 值过滤逻辑）

**严重性**：严重 — Python 的 `execute_updates` 是 updater→operator 应用链路的核心函数，Go 完全缺失

**Python 样例**：

```python
# openjiuwen/agent_evolving/update_execution.py
def execute_updates(
    operators: Mapping[str, Operator],
    updates: Mapping[tuple[str, str], Any],
) -> list[ApplyResult]:
    results = []
    # ✅ 过滤 None 值更新并单独生成错误结果
    non_none_updates = {key: value for key, value in updates.items() if value is None}
    for (operator_id, target), update in normalize_updates(non_none_updates).items():
        operator = operators.get(operator_id)
        if operator is None:
            results.append(ApplyResult(
                operator_id=operator_id, target=target, applied=False,
                mode=update.mode, effect=update.effect, value=update.payload,
                change_type=update.change_type,
                errors=[f"operator not found: {operator_id}"],
                metadata=dict(update.metadata),
            ))
            continue
        results.append(operator.apply_update(target, update))
    # ✅ 为 None 值更新生成错误结果
    for operator_id, target in (key for key, value in updates.items() if value is None):
        results.append(ApplyResult(
            operator_id=operator_id, target=target, applied=False,
            value=None, errors=["update value is None"],
        ))
    return results
```

**Go 问题**：

`internal/evolving/schema/` 包中没有 `execute_updates` 对应实现。`trainer.ApplyUpdates` 是部分实现，但：
- 输入已经是 `map[UpdateKey]UpdateValue`（已归一化），不接受混合类型
- 没有 None 值过滤逻辑
- operator 不存在时使用 `ApplyResultWithErrors`（缺少 ChangeType 和 Metadata 字段）

**修复方案**：

在 `internal/evolving/schema/` 或 `internal/evolving/` 包中实现 `ExecuteUpdates` 函数，完整对齐 Python 逻辑：

```go
func ExecuteUpdates(
    operators map[string]operator.Operator,
    updates map[schema.UpdateKey]any,
) []schema.ApplyResult {
    var results []schema.ApplyResult
    // 1. 过滤 nil 值更新
    nonNilUpdates := make(map[schema.UpdateKey]any)
    for key, value := range updates {
        if value != nil {
            nonNilUpdates[key] = value
        }
    }
    // 2. 归一化后逐一应用
    normalized := schema.NormalizeUpdates(nonNilUpdates)
    for key, update := range normalized {
        op, ok := operators[key.OperatorID()]
        if !ok {
            results = append(results, schema.ApplyResult{
                OperatorID: key.OperatorID(), Target: key.Target(), Applied: false,
                Mode: update.Mode, Effect: update.Effect, Value: update.Payload,
                ChangeType: update.ChangeType,
                Errors: []string{"operator not found: " + key.OperatorID()},
                Metadata: schema.MetadataClone(update.Metadata),
            })
            continue
        }
        results = append(results, op.ApplyUpdate(key.Target(), update))
    }
    // 3. 为 nil 值更新生成错误结果
    for key, value := range updates {
        if value == nil {
            results = append(results, schema.ApplyResult{
                OperatorID: key.OperatorID(), Target: key.Target(), Applied: false,
                Errors: []string{"update value is nil"},
            })
        }
    }
    return results
}
```

---

### S06: handle_envelope.go code 模式 switchMode 缺少 sub_mode != "team" 条件和 session 持久化

**严重性**：严重 — code 模式切换后状态不持久化，且 team 子模式不应走 switchMode 路径

**Python 样例**：

```python
# jiuwenswarm/server/agent_ws_server.py:1145-1154
if mode == "code" and sub_mode != "team":  # ✅ 排除 team 子模式
    session = create_agent_session(session_id=request.session_id, card=agent.get_instance().card)
    await session.pre_run(inputs=None)
    agent.get_instance().switch_mode(session=session, mode=sub_mode)
    state = agent.get_instance().load_state(session)
    session.update_state({"deep_agent_state": state.to_session_dict()})
    await session.post_run()  # ✅ 完整 session 生命周期：pre_run → switch → load_state → update_state → post_run
```

**Go 问题**：

```go
// internal/swarm/server/handle_envelope.go:297-299
if mode == "code" {
    _ = agent.SwitchMode(mode, subMode)  // ❌ 缺少 sub_mode != "team" 条件
    // ❌ 没有 session 持久化逻辑
}
// 流式路径 handle_envelope.go:354-356 同样的问题
```

**修复方案**：

1. 添加 `subMode != "team"` 条件
2. 实现 session 持久化：`preRun → switchMode → loadState → updateState → postRun`

---

### S07: DeepAgentSpec 字段类型退化为 any，RailSpec 已定义但未被引用

**严重性**：严重 — 编译期类型安全丧失，且回填标注与实际状态矛盾

**Python 样例**：

```python
# openjiuwen/agent_teams/schema/deep_agent_spec.py:414-446
class DeepAgentSpec(BaseModel):
    tools: Optional[list[ToolCard | BuiltinToolSpec]] = None
    mcps: Optional[list[McpServerConfig]] = None
    subagents: Optional[list[SubAgentSpec]] = None
    rails: Optional[list[RailSpec]] = None

class SubAgentSpec(BaseModel):
    agent_card: AgentCard
    tools: list[ToolCard | BuiltinToolSpec] = []
    mcps: list[McpServerConfig] = []
    rails: Optional[list[RailSpec]] = None
```

**Go 问题**：

```go
// internal/agent_teams/schema/deep_agent_spec.go:173-179
type DeepAgentSpec struct {
    Tools []any `json:"tools,omitempty"`       // ❌ 应为 ToolCard | BuiltinToolSpec 联合类型
    Mcps []any `json:"mcps,omitempty"`         // ❌ 应为 McpServerConfig
    Subagents []any `json:"subagents,omitempty"` // ❌ SubAgentSpec 已定义但未引用
    Rails []any `json:"rails,omitempty"`        // ❌ RailSpec 已在第98行定义但未引用
}

// deep_agent_spec.go:118-126
type SubAgentSpec struct {
    AgentCard any `json:"agent_card"`    // ❌ agentschema.AgentCard 已在 DeepAgentSpec.Card 使用
    Tools any `json:"tools"`            // ❌ 注释说"⤵️ 回填"但 RailSpec 等类型已就绪
    Mcps any `json:"mcps"`              // ❌ 同上
    Rails any `json:"rails,omitempty"`  // ❌ RailSpec 已在同文件定义，不需要回填
}
```

**修复方案**：

1. `DeepAgentSpec.Tools` 改为具体类型（Go 使用 interface 或 sum-type 模式）
2. `DeepAgentSpec.Subagents` 改为 `[]SubAgentSpec`
3. `DeepAgentSpec.Rails` 改为 `[]RailSpec`
4. `SubAgentSpec.Rails` 改为 `[]RailSpec`（同文件已定义）
5. `SubAgentSpec.AgentCard` 改为 `*agentschema.AgentCard`

---

### S08: AgentManager 完整实现缺失，仅返回 stubAgent

**严重性**：严重 — 多 channel、多 mode、project_dir 缓存键的 Agent 实例池完全无法工作

**Python 样例**：

```python
# jiuwenswarm/server/runtime/agent_manager.py
class AgentManager:
    agents: dict[str, dict[str, JiuWenClaw]]  # channel → cache_key → agent
    _agent_create_params: dict[str, dict]      # 记录创建参数
    _client_capabilities_by_channel: dict      # ACP 客户端能力

    def get_agent_nowait(self, channel_id, mode=None, project_dir=None, sub_mode=None):
        # 三级 fallback 查找

    def recreate_agent(self, channel_id, cache_key):
        # 按 backup 参数重建

    def reload_agents_config(self):
        # 热重载配置
```

**Go 问题**：

`internal/swarm/server/runtime/agent_manager.go` 是纯 stub，`GetAgent` 直接返回 `stubAgent{}`，没有多实例管理能力。此为计划中的待实现项（10.3.12 🔄），但严重影响当前 swarm 功能可用性。

**修复方案**：

按 10.3.12 章节计划实现完整 AgentManager，包含多 channel 缓存、创建参数记录、三级 fallback 查找。

---

## 一般问题 (G)

### G01: IsPassResult 对字符串结果未处理空格

**Python 样例**：

```python
# openjiuwen/agent_evolving/evaluator/evaluator.py:136-149
def _is_pass_result(self, result: Any) -> bool:
    if result is True:
        return True
    if isinstance(result, str):
        return result.strip().lower() == "true"  # ✅ 先 strip 再 lower，处理 " True " 等
    return False
```

**Go 问题**：

```go
// internal/evolving/evaluator/metrics/llm_as_judge.go:127-134
func IsPassResult(result any) bool {
    if result == true { return true }
    if s, ok := result.(string); ok {
        return s == "true" || s == "True" || s == "TRUE"  // ❌ 不处理空格
    }
    return false
}
```

**修复方案**：

```go
func IsPassResult(result any) bool {
    if result == true { return true }
    if s, ok := result.(string); ok {
        return strings.EqualFold(strings.TrimSpace(s), "true")
    }
    return false
}
```

---

### G02: BatchEvaluate 缺少 numParallel 范围校验

**Python 样例**：

```python
# openjiuwen/agent_evolving/evaluator/evaluator.py:73-76
TuneUtils.validate_digital_parameter(
    num_parallel, "num_parallel", TuneConstant.min_parallel_num, TuneConstant.max_parallel_num
)
num_workers = min(num_parallel, len(cases))
```

**Go 问题**：

```go
// internal/evolving/evaluator/evaluator.go:362-373
func validateBatchArgs(casesLen, predictsLen, numParallel int) error {
    if casesLen != predictsLen {
        return exception.NewBaseError(...)
    }
    return nil  // ❌ 不校验 numParallel 范围，也不做 min(numParallel, casesLen) 保护
}
```

**修复方案**：

在 `validateBatchArgs` 中增加 `numParallel` 范围校验（1-20），并在 `BatchEvaluate` 中使用 `min(numParallel, len(cases))` 限制实际并发数。

---

### G03: ACP capabilities 注入缺少 INITIALIZE 排除和三层 fallback

**Python 样例**：

```python
# jiuwenswarm/server/agent_ws_server.py:803-810
if request.channel_id == "acp" and request.req_method != ReqMethod.INITIALIZE:  # ✅ 排除 INITIALIZE
    metadata.setdefault(  # ✅ 不覆盖已有值
        "acp_client_capabilities",
        ws_caps or self._agent_manager.get_client_capabilities("acp"),  # ✅ 三层 fallback
    )
```

**Go 问题**：

```go
// internal/swarm/server/handle_envelope.go:59-61
if request.ChannelID == acpChannelID {
    s.injectACPCapabilities(request, envelope)  // ❌ 不排除 INITIALIZE，会覆盖已有值
}
```

**修复方案**：

1. 添加 `request.ReqMethod != schema.ReqMethodInitialize` 条件
2. 使用 `setdefault` 语义（不覆盖已有 key）
3. 实现 ws_caps → agent_manager fallback 链

---

### G04: ToolCallOperator.toString 静默丢弃非字符串值

**Python 样例**：

```python
# openjiuwen/core/operator/tool_call/base.py:88
self._descriptions = value.copy()  # ✅ 直接赋值，Python dict value 可以是任意类型
```

**Go 问题**：

```go
// internal/agentcore/operator/tool_call/tool_call_operator.go:186-190
func toString(v any) string {
    if s, ok := v.(string); ok { return s }
    return ""  // ❌ 非 string 值静默变为空字符串，丢失信息
}
```

**修复方案**：

对非 string 值使用 `fmt.Sprintf("%v", v)` 转为字符串，而非返回空字符串。

---

### G05: UpdaterRequiresForward 默认值不一致（Python=True, Go=False）

**Python 样例**：

```python
# openjiuwen/agent_evolving/trainer/trainer.py:103-108
def _updater_requires_forward(self) -> bool:
    requires = getattr(self._updater, "requires_forward_data", None)
    if callable(requires):
        return requires()
    return True  # ✅ 默认需要前向数据
```

**Go 问题**：

```go
// internal/evolving/trainer/trainer.go:242-244
func (t *Trainer) UpdaterRequiresForward(_ any) bool {
    // TODO: 依赖 9.70c Updater Protocol 填充后实现
    return false  // ❌ Python 默认 True，Go 返回 False
}
```

**修复方案**：

改为 `return true`，对齐 Python 默认行为。

---

### G06: session.create 返回字段名 sessionId vs session_id

**Python 样例**：

Python 的 session.create 返回 payload 中使用 `"session_id"` 键（下划线命名）。

**Go 问题**：

```go
// internal/swarm/server/handle_session.go:404
schema.WithPayload(map[string]any{"sessionId": sessionID}),  // ❌ 驼峰命名，与 Python 和其他字段不一致
```

**修复方案**：

改为 `"session_id"` 对齐 Python 命名约定。

---

### G07: Cancel 路径调用 applyResolvedModeToRequest 会修改 request.Params

**Python 样例**：

```python
# jiuwenswarm/server/agent_ws_server.py:1068-1108
mode_param = request.params.get("mode", "")  # ✅ 只读取，不修改 request
if mode_param:
    mode, sub_mode, _canonical = resolve_agent_request_mode(mode_param)
```

**Go 问题**：

```go
// internal/swarm/server/handle_envelope.go:430-466
mode, subMode := applyResolvedModeToRequest(request)  // ❌ 会修改 request.Params 的 mode 字段
```

**修复方案**：

cancel 路径中仅读取 mode 参数做 resolve，不调用 `applyResolvedModeToRequest`（该函数有副作用）。

---

### G08: LLMAsJudgeMetric 缺少 Python DefaultEvaluator 的重试逻辑

**Python 样例**：

```python
# openjiuwen/agent_evolving/evaluator/evaluator.py:151-175
def _extract_evaluate_result(self, response, case, predict):
    evaluated_result = TuneUtils.parse_json_from_llm_response(response)
    if evaluated_result and "result" in evaluated_result and "reason" in evaluated_result:
        return evaluated_result
    # ✅ 首次解析失败，使用重试模板再调一次 LLM
    messages = LLM_METRIC_RETRY_TEMPLATE.format({...}).to_messages()
    response = asyncio.run(self._model.invoke(messages)).content
    return TuneUtils.parse_json_from_llm_response(response)
```

**Go 问题**：

Go 的 `DefaultEvaluator.extractEvaluateResult` 已实现重试逻辑（正确），但 `LLMAsJudgeMetric.parseResult` 没有重试逻辑，解析失败直接返回 0.0。`LLMMetricRetryTemplate` 已在 Go 中定义但未被 `LLMAsJudgeMetric` 使用。

**修复方案**：

在 `LLMAsJudgeMetric.parseResult` 解析失败时，使用 `LLMMetricRetryTemplate` 重试一次 LLM 调用。

---

### G09: UpdateValue 零值 Mode/Effect 为空字符串而非 Python 默认值

**Python 样例**：

```python
# openjiuwen/agent_evolving/types.py:26-33
@dataclass(frozen=True)
class UpdateValue:
    payload: Any
    mode: UpdateMode = REPLACE_MODE  # ✅ 默认 "replace"
    effect: UpdateEffect = STATE_EFFECT  # ✅ 默认 "state"
```

**Go 问题**：

```go
// internal/evolving/schema/update.go:53-64
type UpdateValue struct {
    Payload any
    Mode UpdateMode     // ❌ 零值为 ""，不是 "replace"
    Effect UpdateEffect // ❌ 零值为 ""，不是 "state"
}
// 注释中已标注：Go 的 struct 零值中 Mode 和 Effect 为空字符串
```

**修复方案**：

代码注释中已说明此问题并提供了 `NewUpdateValue` 构造函数。建议：
1. 在所有使用点强制使用 `NewUpdateValue` 或 `NormalizeUpdateValue`
2. 或添加 lint 规则禁止直接构造 `UpdateValue{}` 字面量

---

### G10: EvaluatedCase.Score 直接赋值绕过钳位

**Python 样例**：

```python
# openjiuwen/agent_evolving/dataset/case.py:53-57
@field_validator("score")
@classmethod
def clamp_score(cls, v: float) -> float:
    return max(0.0, min(1.0, v))  # ✅ 任何方式设置 score 都会被钳位
```

**Go 问题**：

```go
// internal/evolving/dataset/case.go:81-89
func (ec *EvaluatedCase) SetScore(score float64) {
    if score > 1.0 { score = 1.0 }
    if score < 0.0 { score = 0.0 }
    ec.Score = score  // ✅ SetScore 有钳位
}
// 但 ec.Score = 1.5  ❌ 直接赋值不受约束
```

**修复方案**：

在文档和代码注释中标注必须使用 `SetScore` 方法。或使用 unexported 字段 + getter/setter 模式强制约束。

---

## 提示 (T)

### T01: Case UUID 格式差异

Python 使用 `uuid.uuid4().hex`（32字符无连字符），Go 使用 `uuid.New().String()`（36字符带连字符）。功能无影响，但跨语言共享测试数据时 ID 格式不同。

### T02: CaseLoader 缺少独立 shuffle_cases/split_cases 函数

Python 导出了 `shuffle_cases(cases, seed)` 和 `split_cases(cases, ratio)` 独立函数，Go 把这些功能绑定为 CaseLoader 方法。功能等价但 API 形式不同。

### T03: UpdateValue 不可变性差异

Python `UpdateValue` 是 `@dataclass(frozen=True)` 不可变，Go 是可变 struct。Go 语言限制，可接受。

### T04: ExactMatchMetric 非字符串类型使用 reflect.DeepEqual

Python 统一走 `str()` 转换比较，Go 对非字符串类型走 `reflect.DeepEqual`。这是 Go 版本的合理改进，`reflect.DeepEqual` 对 map/dict 类型比较更精确。

### T05: MetricResult 类型统一

Python 使用 `Union[float, Dict[str, float]]`，Go 统一为 `map[string]float64`。Go 版本简化了类型处理，合理改进。

### T06: Trainer 核心方法全部为桩

Train/Forward/Evaluate/Predict 等方法全部返回 `not implemented`，属计划中的分阶段实现，依赖 9.70a/9.70b/9.70c/9.71/9.77/9.78 等后续章节。

### T07: Updater Protocol 体系和 Signal 模块完全缺失

属计划中但尚未实现（9.70c Updater Protocol），trainer.go 中有明确 TODO 和 any 占位标注。

### T08: code_adapter.go 多个核心步骤仍为 stub

步骤 14/16/18/19/20/21/24 在 Python 中完整实现，Go 中标记为 ⤵️ 待回填。属 10.3.7-11 🔄 章节范围。

### T09: agent_server.go Stop 顺序与 Python 有差异

Go 多了 `cancelAllStreamTasks` 步骤和 `AgentManager.Cleanup` 调用。属于 Go 特有的资源管理需求，非逻辑差异。

### T10: evolution/helpers.go 和 sysop_builder/policy.go/display.go 与 Python 完整对齐

确认无误。

---

## 汇总

| 分类 | 数量 | 问题编号 |
|------|------|---------|
| 严重 | 8 | S01-S08 |
| 一般 | 10 | G01-G10 |
| 提示 | 10 | T01-T10 |

### 严重问题修复优先级建议

| 优先级 | 问题 | 修复难度 | 影响范围 |
|--------|------|---------|---------|
| P0 | S04: question nil→`<nil>` | 低（1行） | LLMAsJudgeMetric 所有调用 |
| P0 | S03: 本地常量→共享常量 | 低（删1行+改引用） | SkillExperienceOperator 所有调用 |
| P0 | S02: PreviewUpdate 缺 ChangeType | 低（改错误路径构建） | SkillExperienceOperator 错误路径 |
| P1 | S05: execute_updates 缺失 | 中（新增函数） | Updater→Operator 应用链路 |
| P1 | S06: switchMode 条件+持久化 | 中（补逻辑） | code 模式切换 |
| P1 | S01: LLMCallOperator 多消息格式 | 中高（改类型+序列化） | 多消息 prompt 演化 |
| P2 | S07: DeepAgentSpec 类型退化 | 高（需重构字段类型） | agent_teams 构建 |
| P2 | S08: AgentManager 完整实现 | 高（新模块） | swarm 多实例管理 |

### 一般问题修复优先级建议

| 优先级 | 问题 | 修复难度 |
|--------|------|---------|
| P0 | G01: IsPassResult 空格处理 | 低 |
| P0 | G06: sessionId→session_id | 低 |
| P1 | G02: numParallel 范围校验 | 低 |
| P1 | G04: toString 静默丢弃 | 低 |
| P1 | G05: UpdaterRequiresForward 默认值 | 低 |
| P1 | G08: LLMAsJudgeMetric 重试逻辑 | 中 |
| P2 | G03: ACP capabilities 注入 | 中 |
| P2 | G07: cancel 路径 request 修改 | 中 |
| P2 | G09: UpdateValue 零值 | 低（文档） |
| P2 | G10: Score 钳位 | 低（文档） |
