# 代码逻辑审查报告 — 2026-07-16

> 审查范围：48小时内提交记录涉及的章节
> 审查方法：逐章节对比 Python 参考项目方法签名与步骤

## 审查范围

| 章节 | 功能 | 对应 Python 源码 | 提交 |
|------|------|-----------------|------|
| 9.20 | Interrupt JSON Schema | `openjiuwen/core/single_agent/interrupt/response.py` | c7c4515 |
| 9.55+9.57 | AgentConfigurator / TeamHarness / SpawnPayloadBuilder | `openjiuwen/agent_teams/` | 4ca4477, f891cb0, 55c58d8, 2fb16b0, b5ce992 |
| 9.70b | Evolving Dataset (Case/CaseLoader) | `openjiuwen/agent_evolving/dataset/` | 721e0b1, 32b054c, dc04cac |
| 9.71 | Evaluator (Base/Metric/ExactMatch/LLMAsJudge) | `openjiuwen/agent_evolving/evaluator/` | 288479a, b406d9e, 168f75b, 0d49bd5, f917a93, 908d961 |
| 10.3.9 | EvolutionHelpers | `jiuwenswarm/server/runtime/agent_adapter/evolution_helpers.py` | 094c512, c397ac5 |
| 10.3.21-22 | GatewayPush Transport + Session Delivery | `jiuwenswarm/server/gateway_push/`, `jiuwenswarm/server/runtime/session/session_metadata.py` | 3a896b2, 0e1bd62, 0e6fb05, a47360a |

---

## 🔴 严重问题（功能缺陷）

### S1. ToolCallInterruptRequest 序列化结构嵌套 vs 扁平（9.20）

**Python 行为**：`ToolCallInterruptRequest` 继承 `InterruptRequest`，`from_tool_call()` 先 `request.model_dump()` 扁平化基类所有字段（message, payload_schema, auto_confirm_key, ui_options），再 update 子类字段，最终所有字段在同一层级：

```json
{
  "message": "Please confirm",
  "payload_schema": {"type": "object"},
  "auto_confirm_key": "test_tool",
  "ui_options": null,
  "tool_name": "test_tool",
  "tool_call_id": "call_123",
  "tool_args": "{\"arg1\": \"value1\"}",
  "index": 0,
  "questions": [{"question": "What is your name?"}]
}
```

**Go 现状**：`ToolCallInterruptRequest` 将基类字段包裹在 `Request` 字段中，形成嵌套结构：

```go
type ToolCallInterruptRequest struct {
    Request    InterruptRequester `json:"request"`      // 嵌套！
    ToolName   string             `json:"tool_name"`
    ToolCallID string             `json:"tool_call_id"`
    ToolArgs   string             `json:"tool_args"`
    Index      int                `json:"index"`
}
```

实际输出：
```json
{
  "request": {
    "Message": "Please confirm",
    "PayloadSchema": {"type": "object"}
  },
  "tool_name": "test_tool",
  "tool_call_id": "call_123"
}
```

**影响**：前端/消费方按 Python 格式解析时，无法读取 message、payload_schema 等字段；AskUserRequest 子类的 questions 字段被困在嵌套的 request 对象内。

**修复方案**：将 ToolCallInterruptRequest 改为扁平结构，直接内嵌基类字段：

```go
type ToolCallInterruptRequest struct {
    // 基类字段（扁平，对齐 Python 继承的 model_dump）
    Message        string          `json:"message"`
    PayloadSchema  map[string]any  `json:"payload_schema"`
    AutoConfirmKey string          `json:"auto_confirm_key"`
    UIOptions      []map[string]any `json:"ui_options,omitempty"`
    // 子类字段
    ToolName   string `json:"tool_name"`
    ToolCallID string `json:"tool_call_id"`
    ToolArgs   string `json:"tool_args"`
    Index      int    `json:"index,omitempty"`
    // extra 字段（对齐 Python extra="allow"）
    Questions []any `json:"questions,omitempty"`
}
```

同时在 `NewToolCallInterruptRequest` 中从 `InterruptRequester` 接口提取基类字段值。

---

### S2. InterruptRequest 缺少 json tag 导致字段名大写不对齐（9.20）

**Python 行为**：Pydantic `model_dump()` 默认输出 snake_case：

```json
{
  "message": "Please confirm",
  "payload_schema": {"type": "object"},
  "auto_confirm_key": "test_tool",
  "ui_options": null
}
```

**Go 现状**：`InterruptRequest` 结构体4个字段均无 json tag，序列化使用 PascalCase：

```go
type InterruptRequest struct {
    Message string           // → JSON: "Message"
    PayloadSchema map[string]any  // → JSON: "PayloadSchema"
    AutoConfirmKey string     // → JSON: "AutoConfirmKey"
    UIOptions []map[string]any // → JSON: "UIOptions"
}
```

**影响**：前端或其他消费方无法正确解析字段名，与 Python 格式不兼容。

**修复方案**：添加 snake_case 的 json tag：

```go
type InterruptRequest struct {
    Message        string          `json:"message"`
    PayloadSchema  map[string]any  `json:"payload_schema"`
    AutoConfirmKey string          `json:"auto_confirm_key"`
    UIOptions      []map[string]any `json:"ui_options,omitempty"`
}
```

---

### S3. buildServerPushWireChunk 未传递 is_complete（10.3.21-22）

**Python 行为**：从 msg 读取 `is_complete` 并传递给 `AgentResponseChunk`：

```python
chunk = AgentResponseChunk(
    request_id=str(msg.get("request_id", "")),
    channel_id=str(msg.get("channel_id", "")),
    payload=msg.get("payload"),
    is_complete=bool(msg.get("is_complete", False)),
)
```

**Go 现状**：`buildServerPushWireChunk` 未从 msg 读取 `is_complete`，始终为 false：

```go
func buildServerPushWireChunk(msg map[string]any) map[string]any {
    // ... 读取 request_id, channel_id, payload ...
    chunk := schema.NewAgentResponseChunk(requestID, channelID, payload)
    // is_complete 未从 msg 读取！
    wire := e2a.EncodeAgentChunkForWire(chunk, requestID, 0, false)
    // ...
}
```

**影响**：`is_complete` 是流结束标记，缺失导致接收方无法正确判断推送消息是否为最终帧，所有 server_push chunk 的 is_complete 恒为 false。

**修复方案**：

```go
func buildServerPushWireChunk(msg map[string]any) map[string]any {
    // ... 现有代码 ...
    isComplete := false
    if ic, ok := msg["is_complete"].(bool); ok {
        isComplete = ic
    }
    chunk := schema.NewAgentResponseChunk(requestID, channelID, payload)
    chunk.IsComplete = isComplete
    wire := e2a.EncodeAgentChunkForWire(chunk, requestID, 0, isComplete)
    // ...
}
```

---

### S4. writeSessionMetadata 不创建目录（10.3.21-22）

**Python 行为**：`_metadata_file()` 在获取路径时自动创建 session 目录：

```python
def _metadata_file(session_id: str) -> Path:
    session_dir = get_agent_sessions_dir() / session_id
    session_dir.mkdir(parents=True, exist_ok=True)  # ← 创建目录
    return session_dir / "metadata.json"
```

**Go 现状**：`writeSessionMetadata` 直接写文件，不创建目录：

```go
func writeSessionMetadata(sessionsDir, sessionID string, meta map[string]any) error {
    metaPath := filepath.Join(sessionsDir, sessionID, metadataFileName)
    data, err := json.MarshalIndent(meta, "", "  ")
    if err != nil {
        return err
    }
    return os.WriteFile(metaPath, data, 0o644)  // ← 不创建目录
}
```

**影响**：`SetSessionDeliveryContext` 在 session 目录不存在时写入失败（仅 Warn 日志），delivery context 丢失。

**修复方案**：

```go
func writeSessionMetadata(sessionsDir, sessionID string, meta map[string]any) error {
    sessionDir := filepath.Join(sessionsDir, sessionID)
    if err := os.MkdirAll(sessionDir, 0o755); err != nil {
        return err
    }
    metaPath := filepath.Join(sessionDir, metadataFileName)
    data, err := json.MarshalIndent(meta, "", "  ")
    if err != nil {
        return err
    }
    return os.WriteFile(metaPath, data, 0o644)
}
```

---

### S5. CaseLoader.Split 缺少 shuffle 步骤（9.70b）

**Python 行为**：`split()` 在拆分前先调用 `shuffle_cases(self._cases, seed)` 随机打乱：

```python
def split(self, ratio: float, seed: int = 0) -> Tuple["CaseLoader", "CaseLoader"]:
    if not 0.0 <= ratio <= 1.0:
        raise ValueError(f"ratio must be in [0.0, 1.0], got {ratio}")
    shuffled = shuffle_cases(self._cases, seed)
    cut = int(len(shuffled) * ratio)
    return CaseLoader(shuffled[:cut]), CaseLoader(shuffled[cut:])
```

**Go 现状**：直接按顺序拆分，无 shuffle：

```go
func (cl *CaseLoader) Split(ratio float64) (*CaseLoader, *CaseLoader) {
    // 无 shuffle，无 ratio 校验，无 seed 参数
    splitIdx := int(float64(len(cl.cases)) * ratio)
    trainCases := make([]Case, splitIdx)
    copy(trainCases, cl.cases[:splitIdx])
    // ...
}
```

**影响**：训练集总是前面的样本，验证集总是后面的样本，违反了随机拆分的设计意图，可能导致评估结果偏差。

**修复方案**：

```go
func (cl *CaseLoader) Split(ratio float64, seed ...int64) (*CaseLoader, *CaseLoader) {
    if ratio < 0.0 || ratio > 1.0 {
        panic(fmt.Sprintf("ratio must be in [0.0, 1.0], got %v", ratio))
    }
    // 先 shuffle
    s := int64(0)
    if len(seed) > 0 {
        s = seed[0]
    }
    shuffled := ShuffleCasesCopy(cl.cases, s)
    cut := int(float64(len(shuffled)) * ratio)
    return NewCaseLoader(shuffled[:cut]), NewCaseLoader(shuffled[cut:])
}
```

---

### S6. CaseLoader.Split 缺少 ratio 范围校验（9.70b）

**Python 行为**：`split()` 校验 ratio 在 [0.0, 1.0] 范围内，不合法时抛 ValueError。

**Go 现状**：完全没有校验，传入 ratio > 1.0 或 < 0.0 时不会报错，虽然代码有 splitIdx 的边界钳位，但语义上 ratio=-0.5 或 ratio=2.0 应该是调用方错误，应该立即暴露。

**修复方案**：与 S5 一并修复，在 Split 函数入口添加 ratio 范围校验。

---

### S7. BuildPushMessageFunc 参数顺序与 BuildServerPushMessage 不兼容（10.3.9）

**Python 行为**：`build_server_push_message` 签名使用关键字参数：

```python
def build_server_push_message(
    *,
    session_id: str,
    request_id: str,
    payload: dict[str, Any],
    fallback_channel_id: str | None = None,
) -> dict[str, Any]:
```

`push_evolution_status` 调用方式：

```python
build_push_message(
    session_id=push_context.session_id,
    request_id=status_update.request_id,
    fallback_channel_id=push_context.channel_id,
    payload=payload,
)
```

**Go 现状**：`BuildPushMessageFunc` 参数顺序为 `(sessionID, requestID, fallbackChannelID, payload)`，但 `BuildServerPushMessage` 参数顺序为 `(sessionID, requestID, payload, fallbackChannelID...)`：

```go
// evolution/helpers.go
type BuildPushMessageFunc func(sessionID, requestID, fallbackChannelID string, payload map[string]any) map[string]any

// handle_session.go
func BuildServerPushMessage(sessionID, requestID string, payload map[string]any, fallbackChannelID ...string) map[string]any
```

**影响**：如果直接把 `BuildServerPushMessage` 作为 `BuildPushMessageFunc` 传入，`fallbackChannelID` 和 `payload` 位置会对调，导致运行时类型断言错误或数据错乱。

**修复方案**：统一参数顺序。建议 `BuildPushMessageFunc` 改为与 `BuildServerPushMessage` 一致：

```go
type BuildPushMessageFunc func(sessionID, requestID string, payload map[string]any, fallbackChannelID ...string) map[string]any
```

同时更新 `PushEvolutionStatus` 和 `PushEvolutionEvent` 中的调用方式。

---

### S8. RunAgentCustomizer 缺少 recover 保护（9.57）

**Python 行为**：使用 try/except 吞掉异常，防止自定义钩子破坏团队启动：

```python
def run_agent_customizer(self, customizer: AgentCustomizer) -> None:
    try:
        customizer(self._deep_agent, self._member_name, self._role.value)
    except Exception as exc:
        team_logger.warning(
            "[{}] agent_customizer failed: {}",
            self._member_name or "?",
            exc,
        )
```

**Go 现状**：无 recover 保护，customizer panic 会导致整个程序崩溃：

```go
func (h *TeamHarness) RunAgentCustomizer(customizer AgentCustomizer) {
    if customizer == nil {
        return
    }
    // TODO(#9.logger): 添加 team_logger.Warn 记录异常
    customizer(h.deepAgent, h.memberName, h.role)
}
```

**影响**：自定义钩子中任何 panic 都会导致程序崩溃，而 Python 设计为 best-effort 安全执行。

**修复方案**：

```go
func (h *TeamHarness) RunAgentCustomizer(customizer AgentCustomizer) {
    if customizer == nil {
        return
    }
    defer func() {
        if r := recover(); r != nil {
            logger.Warn(logComponent).
                Str("member_name", h.memberName).
                Any("panic", r).
                Msg("agent_customizer panic，已恢复")
        }
    }()
    customizer(h.deepAgent, h.memberName, h.role)
}
```

---

## 🟡 一般问题

### G1. readSessionMetadata 返回 nil vs Python 返回 {}（10.3.21-22）

**Python 行为**：`_read_metadata` 在文件不存在或解析失败时返回空 dict `{}`。

**Go 现状**：`readSessionMetadata` 在相同情况下返回 `nil`。

**影响**：当前 `SetSessionDeliveryContext` 中先 `len(meta) == 0` 检查再索引，nil map 不会 panic。但如果有其他调用方使用 `readSessionMetadata` 返回值时没有 nil 检查，就会 panic。与 Python 不一致。

**修复方案**：返回空 map 而非 nil：

```go
func readSessionMetadata(sessionsDir, sessionID string) map[string]any {
    // ...
    if err != nil {
        return map[string]any{}  // 而非 nil
    }
    // ...
    return map[string]any{}  // 而非 nil
}
```

---

### G2. CaseLoader.ShuffleCases 缺少 seed 参数，不可复现（9.70b）

**Python 行为**：`shuffle_cases(cases, seed=0)` 支持指定 seed，确保可复现的随机行为。

**Go 现状**：`ShuffleCases()` 使用 `math/rand/v2` 全局随机源，不支持 seed。

**修复方案**：添加可选 seed 参数，使用 `rand.New(rand.NewPCG(seed, seed))` 创建独立随机源。

---

### G3. batch_evaluate 缺少 numParallel 范围校验和 min 限制（9.71）

**Python 行为**：

```python
TuneUtils.validate_digital_parameter(
    num_parallel, "num_parallel",
    TuneConstant.min_parallel_num, TuneConstant.max_parallel_num
)
num_workers = min(num_parallel, len(cases))
```

**Go 现状**：`validateBatchArgs` 只校验 cases 和 predicts 长度是否相等，**缺少 numParallel 范围校验和 `min(numParallel, len(cases))` 限制**。

**修复方案**：

```go
func validateBatchArgs(casesLen, predictsLen, numParallel int) error {
    if casesLen != predictsLen {
        return fmt.Errorf("cases 和 predicts 长度不一致: %d vs %d", casesLen, predictsLen)
    }
    if numParallel < 1 || numParallel > 20 {
        return fmt.Errorf("numParallel 必须在 [1, 20] 范围内，当前值: %d", numParallel)
    }
    return nil
}
// 实际使用时：actualParallel := min(numParallel, len(cases))
```

---

### G4. GroupEvolutionApprovals 返回值 missing 不对齐（10.3.9）

**Python 行为**：`return grouped, []`，始终返回空列表。

**Go 现状**：在缺少 request_id 时 `append(missing, "")` 添加空字符串，导致 missing 列表非空。

**修复方案**：移除 `missing = append(missing, "")` 行，或改为只调用 warn 回调不记录 missing：

```go
if requestID == nil {
    if len(warnMissing) > 0 && warnMissing[0] != nil {
        warnMissing[0](sessionID)
    }
    continue  // 不添加空字符串到 missing
}
```

---

### G5. EvolutionPushContext.ChannelID 类型差异（10.3.9）

**Python 行为**：`channel_id: str | None`，可以为 None。

**Go 现状**：`ChannelID string`，零值为空字符串 `""`。

**影响**：`BuildServerPushMessage` 中 Python 传入 None 作为 fallback_channel_id，Go 传入空字符串。Go 的 `fallbackChannelID ...string` 可变参数中空字符串与无参数行为不同，可能导致 channel_id 解析差异。

**修复方案**：将 ChannelID 改为 `*string` 以区分空字符串和 nil：

```go
type EvolutionPushContext struct {
    Transport gatewaypush.GatewayPushTransport
    ChannelID *string  // 对齐 Python str | None
    SessionID string
}
```

---

### G6. buildServerPushWireWithResponseKind 缺少 Provenance 字段（10.3.21-22）

**Python 行为**：显式设置 Provenance 的 converter、converted_at、details：

```python
provenance=E2AProvenance(
    source_protocol="e2a",
    converter=_CONVERTER,
    converted_at=utc_now_iso(),
    details={"kind": "server_push"},
)
```

**Go 现状**：`NewE2AProvenance()` 只设置 `SourceProtocol="e2a"`，没有 Converter、ConvertedAt、Details。

**影响**：缺少 converter/converted_at/details 会导致排查推送链路时无法追溯编码来源。

**修复方案**：在 `buildServerPushWireWithResponseKind` 中补充 Provenance 字段：

```go
e2aResp.Provenance.Converter = "github.com/uapclaw/uapclaw-go/internal/swarm/transport/wire:BuildServerPushWire"
e2aResp.Provenance.ConvertedAt = time.Now().UTC().Format(time.RFC3339)
e2aResp.Provenance.Details = map[string]any{"kind": "server_push"}
```

---

### G7. GatewayPushTransport 返回 error vs Python 返回 None（10.3.21-22）

**Python 行为**：`async def send_push(self, msg) -> None`，推送失败静默吞掉（best-effort）。

**Go 现状**：`SendPush(ctx context.Context, msg map[string]any) error`，返回 error。

**影响**：语义差异——Python 设计为 best-effort 推送，Go 改成了显式错误返回，可能让调用方意外中断。当前 `PushEvolutionStatus` 和 `PushEvolutionEvent` 确实处理了 error，但 `ChannelPushTransport.SendPush` 在无单例时返回 error，Python 的 `WebSocketGatewayPushTransport` 在相同情况下只 warn 不抛异常。

**修复方案**：在 `ChannelPushTransport.SendPush` 中，无单例时记录 Warn 日志但不返回 error（对齐 Python best-effort 语义）：

```go
func (t *ChannelPushTransport) SendPush(ctx context.Context, msg map[string]any) error {
    inst := GetInstance()
    if inst == nil {
        logger.Warn(logComponent).Msg("AgentServer 单例不存在，跳过推送")
        return nil  // 而非 return fmt.Errorf(...)
    }
    // ...
}
```

---

### G8. ApproveResult.NewArgs 空 vs nil 语义差异（9.20）

**Python 行为**：`if decision.new_args is not None` 判断，`new_args=""` 也会替换参数。

**Go 现状**：`if d.NewArgs != ""` 判断，空字符串不替换。

**影响**：如果调用方传入空字符串意图清空参数，Go 的行为与 Python 不同。当前所有调用方都不传空字符串，行为一致，但这是潜在语义不一致。

**修复方案**：将 `NewArgs` 改为 `*string`，用 nil 表示"不替换"：

```go
type ApproveResult struct {
    NewArgs *string  // nil=不替换，非nil=替换
}
```

---

### G9. CreateMemberHandle 缺少关键字段赋值（9.57）

**Python 行为**：

```python
return TeamMember(
    member_name=member_name,
    team_name=infra.team_backend.team_name,
    agent_card=agent_card,
    db=infra.team_backend.db,
    messager=infra.messager,
    desc=blueprint.ctx.persona,
)
```

**Go 现状**：缺少 TeamName、DB、Messager 的赋值：

```go
return &TeamMember{
    MemberName:  memberName,
    DisplayName: memberName,
    AgentCard:   agentCard,
    Desc:        blueprint.Ctx.Persona,
}
```

**影响**：TeamMember 的 Status/UpdateStatus 等方法调用时 DB 为 nil。当前方法是 TODO 空壳，但设计意图是对齐的。

**修复方案**：待 infra 实现完成后，从 infra 获取 TeamName/DB/Messager 并赋值。

---

### G10. SpawnPayloadBuilder.BuildSpawnPayload 中 leader_member_name 空字符串 vs null（9.57）

**Python 行为**：`team_spec` 为 None 时 `leader_member_name` 值为 `None`（序列化为 JSON null）。

**Go 现状**：`teamSpec` 为 nil 时 `leaderMemberName` 为空字符串 `""`。

**影响**：跨进程 wire 契约中 null 和空字符串语义不同。如果子进程端 FromSpawnPayload 区分 null 和空字符串，会导致 Bug。

**修复方案**：将 payload 中的 leader_member_name 改为 `*string` 类型：

```go
var leaderMemberName *string
if teamSpec != nil {
    leaderMemberName = &teamSpec.LeaderMemberName
}
```

---

## 🔵 提示问题

### T1. TeamAgentBlueprint.MemberName() 返回指针可被外部修改（9.57）

**Python 行为**：`frozen=True` dataclass，所有字段不可修改。

**Go 现状**：`MemberName()` 返回 `&b.Ctx.MemberName`（字段地址），调用者可通过指针修改 struct 内部字段，破坏"蓝图不可变"设计意图。

**修复方案**：返回字符串副本而非指针：

```go
func (b *TeamAgentBlueprint) MemberName() *string {
    if b.Ctx.MemberName == "" {
        return nil
    }
    s := b.Ctx.MemberName  // 创建副本
    return &s
}
```

---

### T2. Reject() 函数签名缺少 ToolMessage 参数（9.20）

**Python 行为**：`reject(tool_result=..., tool_message=...)` 支持同时传入。

**Go 现状**：`Reject(toolResult any)` 只接受 toolResult，不支持 toolMessage 参数。结构体有 ToolMessage 字段但公共 API 不暴露。

**修复方案**：添加可选参数或创建 WithToolMessage 辅助函数。

---

### T3. LLMAsJudgeMetric.Compute 硬编码 context.Background()（9.71）

**Python 行为**：使用 `asyncio.run(self._model.invoke(messages))` 创建新 event loop。

**Go 现状**：`m.model.Invoke(context.Background(), ...)` 无法被取消或超时控制。

**修复方案**：Metric 接口的 Compute 方法签名应考虑支持 context，或在 MetricOption 中传递。

---

### T4. Case.CaseID 格式不一致（9.70b）

**Python 行为**：`uuid.uuid4().hex` 生成32位无连字符字符串（如 `550e8400e29b41d4a716446655440000`）。

**Go 现状**：`uuid.New().String()` 生成带连字符的标准 UUID 格式（如 `550e8400-e29b-41d4-a716-446655440000`）。

**修复方案**：使用 `uuid.New().String()` 后移除连字符，或使用 `fmt.Sprintf("%x", uuid.New())`：

```go
CaseID: strings.ReplaceAll(uuid.New().String(), "-", ""),
```

---

### T5. Case 的 inputs/label 缺少非空校验（9.70b）

**Python 行为**：Pydantic `min_length=1` 确保 inputs 和 label 不能为空 dict。

**Go 现状**：`NewCase` 没有对 inputs/label 做非空校验。

**修复方案**：在 NewCase 中添加非空校验。

---

### T6. EvaluatedCase.Score 直接赋值不触发钳位（9.70b）

**Python 行为**：Pydantic `field_validator` 在任何赋值场景都自动钳位到 [0, 1]。

**Go 现状**：钳位逻辑只在 `SetScore` 方法中，直接赋值 `ec.Score = 1.5` 不触发。

**修复方案**：将 Score 字段改为私有，只暴露 GetScore/SetScore 方法，或在 JSON 反序列化后调用 SetScore。

---

### T7. evaluate 中 str() vs fmt.Sprintf("%v") 格式化差异（9.71）

**Python 行为**：`str({"query": "hello"})` → `{'query': 'hello'}`（单引号 Python dict 格式）。

**Go 现状**：`fmt.Sprintf("%v", map[string]any{"query": "hello"})` → `map[query:hello]`（Go map 格式）。

**影响**：传递给 LLM 的问题/答案格式不同，可能影响评估结果。但考虑到 LLM 的理解能力，差异可能不大。

---

### T8. MetricResult 丢失 Union[float, Dict] 灵活性（9.71）

**Python 行为**：`MetricResult = Union[float, Dict[str, float]]`，compute 可以返回单个 float。

**Go 现状**：`type MetricResult = map[string]float64`，强制返回 map。

**影响**：当前 ExactMatchMetric 和 LLMAsJudgeMetric 都返回 map，功能上无问题，但接口设计偏差。

---

### T9. add_policy 方法缺失（9.20）

**Python 行为**：提供 `add_policy` 作为 `add_tool` 的废弃别名。

**Go 现状**：无 `AddPolicy` 方法。

**影响**：废弃接口，影响极低。

---

### T10. AgentCustomizer 第二个参数 string vs Optional[str]（9.57）

**Python 行为**：`AgentCustomizer = Callable[[Any, Optional[str], str], None]`，第二个参数可以为 None。

**Go 现状**：`type AgentCustomizer func(deepAgent any, memberName string, roleValue string)`，memberName 不能为 nil。

**影响**：无法表达"memberName 未设置"的语义，用空字符串代替。

---

### T11. MountedRails 导出但 Python 中是私有的（9.57）

**Python 行为**：`_MountedRails` 下划线前缀表示内部/私有。

**Go 现状**：`MountedRails` 大写导出。

**影响**：外部包可以访问 MountedRails，但 Python 中只通过 TeamHarness.rails 属性暴露只读访问。

---

### T12. SendPush 缺少 else 分支日志（10.3.21-22）

**Python 行为**：response_kind 为空时也会记录日志。

**Go 现状**：response_kind 为空时不记录任何日志。

---

### T13. EvolutionOutcomeFromEvent 的 nil payload 检查永远不触发（10.3.9）

**Go 现状**：`if payload == nil` 检查，但 `EventPayloadDict` 空时返回 `map[string]any{}`（非 nil），所以此分支永远不会执行。

**影响**：不是 Bug（因为 EventPayloadDict 总返回非 nil map），但如果 EventPayloadDict 行为改变，会遗漏空 payload 的默认值返回。

---

## 统计

| 级别 | 数量 | 问题编号 |
|------|------|---------|
| 🔴 严重 | 8 | S1-S8 |
| 🟡 一般 | 10 | G1-G10 |
| 🔵 提示 | 13 | T1-T13 |
| **合计** | **31** | |

### 各章节问题分布

| 章节 | 严重 | 一般 | 提示 |
|------|------|------|------|
| 9.20 Interrupt | 2 | 1 | 2 |
| 9.57 AgentConfigurator/TeamHarness | 2 | 2 | 4 |
| 9.70b Dataset | 2 | 1 | 3 |
| 9.71 Evaluator | 0 | 1 | 3 |
| 10.3.9 EvolutionHelpers | 1 | 2 | 1 |
| 10.3.21-22 GatewayPush+Session | 2 | 3 | 1 |

### 优先修复建议

1. **S3 + S4**：`is_complete` 缺失 + `writeSessionMetadata` 不创建目录 — 直接影响推送功能正确性
2. **S1 + S2**：InterruptRequest 序列化格式和字段名 — 影响前后端协议兼容性
3. **S5 + S6**：CaseLoader.Split 缺少 shuffle 和校验 — 影响评估结果正确性
4. **S7**：BuildPushMessageFunc 参数顺序不兼容 — 会导致运行时错误
5. **S8**：RunAgentCustomizer 缺少 recover — 会导致程序崩溃
