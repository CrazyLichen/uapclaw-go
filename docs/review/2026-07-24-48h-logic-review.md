# 48小时代码逻辑审查报告

> 审查日期：2026-07-24
> 审查范围：提交 `5a20e9a` (refactor: 代码规范对齐 + session 元数据增量更新功能)
> 对比基准：Python 参考项目源码

## 审查章节概览

| 章节 | 内容 | 变更文件数 | 状态 |
|------|------|-----------|------|
| 10.3.15-18 | Session 元数据增量更新（SessionMetadataUpdate / SetSessionDeliveryContext / BuildServerPushMessage） | 2 | ✅ 已完成 |
| 9.70-9.80 | Evolving 模块代码规范对齐（optimizer/signal/schema/tool_call） | 25 | ✅ 已完成 |
| 10.3.2-6 | Adapter 代码规范对齐 | 7 | ✅ 已完成 |
| 11.1-3 | Gateway MessageHandler 代码规范对齐 | 6 | ✅ 已完成 |
| 6.x | agentcore 代码规范对齐（operator/callback/resources_manager 等） | 30+ | ✅ 已完成 |

> 说明：本次提交以代码规范对齐（声明排列顺序/中文注释/分隔注释统一）为主，仅 session 元数据增量更新为新功能逻辑。审查重点放在逻辑一致性上。

## 问题统计

| 严重程度 | 数量 |
|---------|------|
| 严重 | 8 |
| 一般 | 10 |
| 提示 | 7 |

---

## 一、Session 元数据增量更新（3 严重 / 2 一般 / 2 提示）

### S-01：[严重] SetSessionDeliveryContext 使用同步写入，绕过异步队列

**Python 参考代码** (`session_metadata.py:230-232`):
```python
# 所有写操作统一走异步队列
_enqueue_write(session_id, metadata)
```

**Go 问题代码** (`handle_session.go:242-244`):
```go
// 直接同步写入，绕过了异步队列
if err := writeSessionMetadata(GetSessionsDir(), sessionID, meta); err != nil {
    logger.Warn(logComponent).Str("session_id", sessionID).Err(err).Msg("写入会话元数据失败")
}
```

**问题分析**：Python 的 `set_session_delivery_context` 调用 `_enqueue_write()` 异步写入，与其他所有写操作一致。Go 的 `SetSessionDeliveryContext` 却直接调用 `writeSessionMetadata()` 同步写入，而同文件中的 `updateSessionMetadata` 正确使用了 `enqueueMetadataWrite()` 异步队列。这导致：

1. `SetSessionDeliveryContext` 会阻塞调用方（通常在请求处理路径上）
2. 与 `updateSessionMetadata` 可能产生并发写入竞争（一个同步、一个异步）

**修复方案**：
```go
// 改用异步队列写入，对齐 Python _enqueue_write(session_id, metadata)
enqueueMetadataWrite(sessionID, meta)
```

---

### S-02：[严重] writeSessionMetadata 缺少文件锁，并发写入可能损坏数据

**Python 参考代码** (`session_metadata.py:142-150`):
```python
_FILE_LOCK = threading.Lock()

def _write_metadata_sync(session_id: str, metadata: dict):
    with _FILE_LOCK:
        # 持锁写入，保证串行化
        path = _metadata_path(session_id)
        with open(path, "w", encoding="utf-8") as f:
            json.dump(metadata, f, ensure_ascii=False, indent=2)
```

**Go 问题代码** (`handle_session.go:656-663`):
```go
func writeSessionMetadata(sessionsDir, sessionID string, meta map[string]any) error {
    metaPath := filepath.Join(sessionsDir, sessionID, metadataFileName)
    data, err := json.MarshalIndent(meta, "", "  ")
    if err != nil {
        return err
    }
    return os.WriteFile(metaPath, data, 0o644) // 无文件锁
}
```

**问题分析**：Python 使用 `_FILE_LOCK` 保证同一进程内写入串行化。Go 的 `writeSessionMetadata` 没有任何锁保护，如果 `handleSessionRename`（同步调用）和 `SetSessionDeliveryContext`（也是同步调用）并发操作同一 session，`os.WriteFile` 不是原子操作，可能导致文件内容交错损坏。

**修复方案**：
```go
var metadataWriteMu sync.Mutex // 进程内文件锁

func writeSessionMetadata(sessionsDir, sessionID string, meta map[string]any) error {
    metadataWriteMu.Lock()
    defer metadataWriteMu.Unlock()

    metaPath := filepath.Join(sessionsDir, sessionID, metadataFileName)
    data, err := json.MarshalIndent(meta, "", "  ")
    if err != nil {
        return err
    }
    return os.WriteFile(metaPath, data, 0o644)
}
```

---

### S-03：[严重] 自动标题生成完全未实现，新建会话标题永远为空

**Python 参考代码** (`session_metadata.py:42-56`):
```python
_TITLE_MAX_LEN = 50

def _auto_title(user_content: str) -> str:
    """截取用户输入前50字符作为自动标题"""
    return user_content[:_TITLE_MAX_LEN].strip() or "New Session"

# 新建时
"title": title or auto_title,

# 更新时
if not metadata.get("title") and user_content:
    metadata["title"] = _auto_title(user_content)
```

**Go 问题代码** (`handle_session.go:847-848`):
```go
autoTitle := "" // ⤵️ 11.x: 等自动标题生成功能实现后回填
_ = autoTitle
```

**问题分析**：`_auto_title` 是 Python 中的基本兜底逻辑——当没有显式标题时，截取用户输入前 50 字符作为会话标题。Go 端完全未实现，导致所有新建会话的 `title` 字段永远是空字符串。⤵️ 标记是准确的，但这是一个影响用户体验的功能缺失：用户在会话列表中看到的都是空标题。

**修复方案**：
```go
const titleMaxLen = 50

// autoTitle 从用户内容生成自动标题，对齐 Python _auto_title
func autoTitle(userContent string) string {
    if userContent == "" {
        return "New Session"
    }
    if len(userContent) > titleMaxLen {
        userContent = userContent[:titleMaxLen]
    }
    trimmed := strings.TrimSpace(userContent)
    if trimmed == "" {
        return "New Session"
    }
    return trimmed
}
```

---

### G-01：[一般] SetSessionDeliveryContext 多出从 route_metadata 提取 channel_metadata 的逻辑

**Python 参考代码** (`session_metadata.py:set_session_delivery_context`):
```python
# Python 中无此逻辑，set_session_delivery_context 不操作 channel_metadata
```

**Go 问题代码** (`handle_session.go:193-196`):
```go
// 对齐 Python: channel_metadata 仅在首次为空时补充写入（不覆盖）
channelMetadata, _ := routeMetadata["channel_metadata"].(map[string]any)
if channelMetadata != nil && meta["channel_metadata"] == nil {
    meta["channel_metadata"] = deepCopyMap(channelMetadata)
}
```

**问题分析**：这段逻辑在 Python 的 `set_session_delivery_context` 中不存在，Go 自行添加的。虽然意图合理（首次补充写入 channel_metadata），但与 Python 行为不一致。Python 中 `channel_metadata` 的首次写入是在 `update_session_metadata` 中处理的。如果 Go 两处都写入，可能出现重复写入或写入时机不一致。

**修复方案**：移除 `SetSessionDeliveryContext` 中的 `channel_metadata` 逻辑，保持与 Python 一致。`channel_metadata` 的写入应仅通过 `updateSessionMetadata` 处理。

---

### G-02：[一般] BuildServerPushMessage 中 payload 无拷贝，并发不安全

**Python 参考代码** (`session_metadata.py:build_server_push_message`):
```python
"payload": dict(payload),  # 浅拷贝
```

**Go 问题代码** (`handle_session.go:284`):
```go
"payload": payload,  // 直接引用，无拷贝
```

**问题分析**：Python 用 `dict(payload)` 做了浅拷贝，避免调用方后续修改 payload 影响已构建的消息。Go 直接引用了 payload map，如果调用方在消息发送前修改了 payload 的内容（map 是引用类型），会导致消息内容被意外修改。

**修复方案**：
```go
"payload": deepCopyMap(payload),
```

---

### T-01：[提示] readSessionMetadata 返回 nil 与 Python 返回空 dict 的差异

**Python 参考代码** (`session_metadata.py:93-95`):
```python
def _read_metadata(session_id: str) -> dict:
    # 文件不存在时返回 {}
    return {}
```

**Go 问题代码** (`handle_session.go:631`):
```go
// 不存在时返回 nil
func readSessionMetadata(sessionsDir, sessionID string) map[string]any {
```

**问题分析**：Python 统一返回空 dict，调用方无需 nil 检查。Go 返回 nil，所有调用方都需要先做 nil 检查，否则 panic。虽然当前所有调用方都做了 nil 检查，但增加了心智负担和维护风险。

**修复方案**：将 `readSessionMetadata` 不存在时的返回值从 `nil` 改为 `map[string]any{}`，与 Python 对齐。

---

### T-02：[提示] 新建 metadata 时 Go 包含 team_name 字段，Python 不包含

**Python 参考代码** (`session_metadata.py:update_session_metadata`):
```python
# 新建时无 team_name 字段
meta = {
    "session_id": session_id,
    "channel_id": ...,
    "user_id": "",
    "created_at": ...,
    "last_message_at": ...,
    "title": ...,
    "message_count": ...,
    "mode": "unknown",
    "round_id": 0,
}
```

**Go 问题代码** (`handle_session.go:208-219`):
```go
meta = map[string]any{
    ...
    "team_name": "",  // Python 中不存在
    ...
}
```

**问题分析**：Go 新增了 `team_name` 字段，Python 的 `update_session_metadata` 中没有。虽然不影响功能（空字符串在 JSON 中等效于无），但与 Python 行为不一致。

**修复方案**：移除 `"team_name": ""`，仅在 `updateSessionMetadata` 中有 `TeamName` 参数时才写入。

---

## 二、Evolving / Optimizer Tool Call（3 严重 / 3 一般 / 2 提示）

### S-04：[严重] matchFailureKeyword 逻辑缺陷导致脚本产物检测误判

**Python 参考代码** (`from_conv.py:38-40`):
```python
# 使用正则负向前瞻，精确排除 "error = None"
_FAILURE_KEYWORDS = re.compile(
    r'error(?!\s*=\s*None)|exception|traceback|failed|failure',
    re.IGNORECASE
)
# 使用方式：
has_failure = bool(_FAILURE_KEYWORDS.search(content))
```

**Go 问题代码** (`from_conv.go:442-444`):
```go
func matchFailureKeyword(content string) bool {
    return failureKeywords.MatchString(content) && !errorEqualsNonePattern.MatchString(content)
}
```

**问题分析**：Go 的 `matchFailureKeyword` 用两个正则组合模拟 Python 的负向前瞻，但逻辑有缺陷。当 content 同时包含 `"error = None"` 和 `"exception"` 时：

1. `failureKeywords.MatchString` 匹配到 `"exception"` → true
2. `errorEqualsNonePattern.MatchString` 匹配到 `"error = None"` → true
3. `true && !true = false` → **误判为无失败**

而 Python 的 `error(?!\s*=\s*None)` 只排除紧跟 `= None` 的 `"error"`，其他关键词不受影响，`"exception"` 仍会匹配。

**影响范围**：`detectFromMessages` 第 615 行的 `hasFailure` 使用了此函数，误判会导致本该排除的失败脚本被当作成功产物。

**修复方案**：改用与 `findFailureKeywordIndex` 类似的逐个匹配验证逻辑：
```go
func matchFailureKeyword(content string) bool {
    return findFailureKeywordIndex(content) >= 0
}
```

或实现更精确的正则模拟：
```go
func matchFailureKeyword(content string) bool {
    for _, match := range failureKeywords.FindAllString(content, -1) {
        if strings.EqualFold(match, "error") {
            // 检查 error 后是否紧跟 = None
            idx := strings.Index(strings.ToLower(content), "error")
            if idx >= 0 && errorEqualsNonePattern.MatchString(content[idx:]) {
                continue // 跳过 error = None
            }
        }
        return true // 其他关键词直接匹配
    }
    return false
}
```

---

### S-05：[严重] ToolOptimizerBase toolName 硬编码为 "tool"，所有工具指向同一负例文件

**Python 参考代码** (`base.py:62`):
```python
self.config_desc['neg_ex_input_path'] = os.path.join(
    self.path_save_dir,
    f"{kwargs.get('tool_name', 'tool')}.json"
)
```

**Go 问题代码** (`base.go:86-87`):
```go
toolName := "tool"
o.configDesc["neg_ex_input_path"] = filepath.Join(o.pathSaveDir, toolName+".json")
```

**问题分析**：Python 通过 `kwargs.get('tool_name', 'tool')` 支持自定义工具名，仅在未传时用 `"tool"` 作默认值。Go 直接硬编码 `"tool"`，且 `ToolOptimizerBase` 没有 `toolName` 字段和对应的 Option 函数。这意味着优化多个工具时，所有工具的 `neg_ex_input_path` 都指向 `tool.json`，负例数据会互相覆盖。

**修复方案**：
1. 在 `ToolOptimizerBase` 中添加 `toolName string` 字段
2. 添加 `WithToolName(name string) ToolOptimizerOption` 选项函数
3. 默认值为 `"tool"`：
```go
if o.toolName == "" {
    o.toolName = "tool"
}
o.configDesc["neg_ex_input_path"] = filepath.Join(o.pathSaveDir, o.toolName+".json")
```

---

### S-06：[严重] NewSimpleEval 权重校验用 panic 而非返回 error

**Python 参考代码** (`customized_eval.py:34-35`):
```python
if abs(fn_call_weight + output_effectiveness_weight - 1.0) > 1e-6:
    raise ValueError(...)
```

**Go 问题代码** (`eval.go:117-120`):
```go
if math.Abs(fnCallWeight+outputEffectivenessWeight-1.0) > 1e-6 {
    panic(fmt.Sprintf("fn_call_weight 和 output_effectiveness_weight 之和必须为 1.0，得到 %f+%f=%f",
        fnCallWeight, outputEffectivenessWeight, fnCallWeight+outputEffectivenessWeight))
}
```

**问题分析**：Python 的 `raise ValueError` 是可捕获的异常，调用方可以 `try/except` 优雅处理。Go 的 `panic` 会导致整个 goroutine 崩溃，如果不被 `recover` 会终止进程。在库代码中使用 panic 是 Go 的反模式。

**修复方案**：将 `NewSimpleEval` 改为返回 `(*SimpleEval, error)`：
```go
func NewSimpleEval(
    apiWrapper APIWrapperFunc,
    config map[string]any,
    fnCallWeight, outputEffectivenessWeight float64,
    model *llm.Model,
) (*SimpleEval, error) {
    if math.Abs(fnCallWeight+outputEffectivenessWeight-1.0) > 1e-6 {
        return nil, fmt.Errorf("fn_call_weight 和 output_effectiveness_weight 之和必须为 1.0，得到 %f+%f=%f",
            fnCallWeight, outputEffectivenessWeight, fnCallWeight+outputEffectivenessWeight)
    }
    ...
    return eval, nil
}
```

---

### G-03：[一般] GetTeamTrajectoryIssues 类型断言在 JSON 反序列化后失败

**Python 参考代码** (`team.py:108-110`):
```python
def get_team_trajectory_issues(signal):
    issues = signal.context.get(_TRAJECTORY_ISSUES_KEY, [])
    return [item for item in issues if isinstance(item, dict)]
```

**Go 问题代码** (`team.go:344-358`):
```go
func GetTeamTrajectoryIssues(sig *EvolutionSignal) []map[string]string {
    ...
    slice, ok := issues.([]map[string]string)  // JSON 反序列化后变为 []any，断言失败
    if !ok {
        return nil
    }
    return slice
}
```

**问题分析**：Python 使用 `isinstance(item, dict)` 运行时过滤，对任何 dict 形态都有效。Go 的类型断言 `issues.([]map[string]string)` 在纯内存路径下可以成功，但如果信号经过 JSON 序列化/反序列化（如持久化后重新加载），`[]map[string]string` 会变成 `[]any`（每个元素为 `map[string]any`），断言失败返回 nil。

**修复方案**：增加兼容转换逻辑：
```go
func GetTeamTrajectoryIssues(sig *EvolutionSignal) []map[string]string {
    ctx := sig.Context
    if ctx == nil { return nil }
    issues, ok := ctx[teamTrajectoryIssuesKey]
    if !ok { return nil }

    // 优先尝试直接断言
    if slice, ok := issues.([]map[string]string); ok {
        return slice
    }
    // JSON 反序列化后降级处理
    if sliceAny, ok := issues.([]any); ok {
        result := make([]map[string]string, 0, len(sliceAny))
        for _, item := range sliceAny {
            if m, ok := item.(map[string]any); ok {
                converted := make(map[string]string, len(m))
                for k, v := range m {
                    converted[k] = fmt.Sprintf("%v", v)
                }
                result = append(result, converted)
            }
        }
        return result
    }
    return nil
}
```

---

### G-04：[一般] Eval results 结构与 Python 不一致（runs > 1 时）

**Python 参考代码** (`customized_eval.py:82-83`):
```python
# runs == 1 时返回单个结果
# runs > 1 时返回二维数组
return all_results[0] if runs == 1 else all_results
```

**Go 问题代码** (`eval.go:148-189`):
```go
// 所有 runs 扁平化为一维数组
return allResults, nil
```

**问题分析**：Python 在 `runs == 1` 时返回单个 result dict，`runs > 1` 时返回二维数组。Go 始终返回扁平化的一维数组。如果下游代码（如 `ToolDescriptionMethod.Step`）依赖 `runs == 1` 时返回单个 dict 的语义，Go 端的逻辑可能出错。

**修复方案**：保持与 Python 一致的返回语义，或确保所有调用方正确处理一维数组。

---

### G-05：[一般] Pipeline 保存结果有条件 vs Python 无条件保存

**Python 参考代码** (`customized_pipline.py:54-60`):
```python
# 始终保存
with open(save_path, "w") as f:
    json.dump(old_result + result, f, indent=2)
```

**Go 问题代码** (`pipeline.go:117-156`):
```go
// 仅在 save_dir 非空时保存
if saveDir, ok := config["save_dir"].(string); ok && saveDir != "" {
    ...
}
```

**问题分析**：Python 总是保存优化结果到文件，Go 仅在 `save_dir` 非空时保存。如果 `config["save_dir"]` 缺失或为空，优化结果将丢失。这在离线优化场景下可能导致数据丢失。

**修复方案**：与 Python 对齐，始终尝试保存。如果 `save_dir` 缺失，使用默认路径（如 `os.TempDir()`）或至少记录 Warn 日志。

---

### T-03：[提示] rits 重试次数差异（Python 2 次 vs Go 15 次）

**Python 参考代码** (`rits.py:28-29`):
```python
@retry(wait=wait_random_exponential(min=1, max=5), stop=stop_after_attempt(2))
```

**Go 问题代码** (`rits.go` 使用 `llm_resilience`):
```go
// 默认 MaxAttempts=15，远超 Python 的 2 次
LLMInvokePolicy{MaxAttempts: 15, ...}
```

**问题分析**：Python 默认 2 次尝试 + 指数退避（1-5s），Go 默认 15 次尝试。这导致 Go 在 API 不可用时重试时间远超 Python，可能影响优化循环的时效性。

**修复方案**：对齐 Python 的重试策略，将 `MaxAttempts` 设为 2，或提供可配置选项。

---

### T-04：[提示] ToolDescriptionMethod 缺少 Python 第1版 critique_descriptions

**Python 参考代码** (`description_example_method.py:74-142`):
```python
def critique_descriptions(self, tool, examples, prev_outputs):
    """第1版：简单版不带正负例分类"""
    ...
```

**Go 代码**：仅实现了第2版（正负例对比版 `CritiqueDescriptions`）。

**问题分析**：Python 有两个版本的 `critique_descriptions`，第2版覆盖了第1版的功能。Go 只实现了第2版，功能上等价，不影响运行结果。缺少向后兼容，但非关键问题。

---

## 三、Evolving / Signal 模块（1 严重 / 2 一般 / 1 提示）

### S-07：[严重] MakeTeamTrajectorySignal excerpt 中文化与提示词一比一复刻规则冲突

**Python 参考代码** (`team.py:96`):
```python
excerpt="Detected team skill trajectory issues requiring evolution."
```

**Go 问题代码** (`team.go:331`):
```go
excerpt: "检测到团队技能轨迹问题，需要进行进化。",
```

**问题分析**：项目规范明确要求"提示词一比一复刻 Python 原文，不做自行翻译"。虽然 `excerpt` 不是严格意义上的 LLM 提示词，但它是存入 `EvolutionSignal` 的内容，会被后续 LLM 调用消费。中文化后 LLM 接收到的上下文与 Python 不一致，可能影响演化信号的处理效果。

**修复方案**：
```go
excerpt: "Detected team skill trajectory issues requiring evolution.",
```

---

### G-06：[一般] TeamSignalDetector 缺少 llm_policy 公共回退参数

**Python 参考代码** (`team.py:23-27`):
```python
def __init__(self, ..., llm_policy=None, trajectory_issue_llm_policy=None, user_intent_llm_policy=None):
    policy = llm_policy or trajectory_issue_llm_policy or user_intent_llm_policy
    if not policy:
        raise ValueError("At least one LLM policy must be provided")
```

**Go 问题代码** (`team.go:NewTeamSignalDetector`):
```go
func NewTeamSignalDetector(llmModel *llm.Model, model string, language string,
    trajectoryIssueLLMPolicy, userIntentLLMPolicy *llm_resilience.LLMInvokePolicy) *TeamSignalDetector {
```

**问题分析**：Python 有 `llm_policy` 作为三个策略参数的公共回退，Go 缺少此参数。如果用户只想设置一个全局策略，Python 只需传 `llm_policy`，Go 则需要显式传两个参数。

**修复方案**：添加 `llmPolicy ...*llm_resilience.LLMInvokePolicy` 参数作为公共回退：
```go
func NewTeamSignalDetector(llmModel *llm.Model, model string, language string,
    trajectoryIssueLLMPolicy, userIntentLLMPolicy *llm_resilience.LLMInvokePolicy,
    llmPolicy ...*llm_resilience.LLMInvokePolicy) *TeamSignalDetector {
    // policy = llmPolicy || trajectoryIssueLLMPolicy || userIntentLLMPolicy
    ...
}
```

---

### G-07：[一般] TeamSignalDetector 构造函数缺策略时 panic 而非返回 error

**Python 参考代码** (`team.py:27-28`):
```python
if not policy:
    raise ValueError("At least one LLM policy must be provided")
```

**Go 问题代码** (`team.go:NewTeamSignalDetector`):
```go
// 使用 panic
panic("NewTeamSignalDetector: 至少需要一个 LLMInvokePolicy")
```

**问题分析**：同 S-06，库代码中不应使用 panic。Python 的 `raise ValueError` 可被调用方捕获，Go 的 panic 会导致进程崩溃。

**修复方案**：返回 `(*TeamSignalDetector, error)`。

---

### T-05：[提示] Go 缺少 TrajectoryIssue 结构体，用 map[string]string 替代

**Python 参考代码** (`team.py:14-18`):
```python
@dataclass(frozen=True)
class TrajectoryIssue:
    issue_type: str
    description: str
    affected_role: str
    severity: str
```

**Go 代码**：直接使用 `map[string]string`。

**问题分析**：功能等价但类型安全性弱于 Python。Python 的 `TrajectoryIssue` 是 frozen dataclass，有明确的字段约束。Go 的 `map[string]string` 无字段约束，可能传入错误的 key。建议后续用 struct 替代。

---

## 四、Callback Framework（1 严重 / 1 一般 / 1 提示）

### S-08：[严重] WorkflowEvents 缺少 ComponentStreamOutput 事件常量

**Python 参考代码** (`events.py:WorkflowEvents`):
```python
class WorkflowEvents(EventBase):
    COMPONENT_STREAM_INPUT = EventBase.get_event("workflow_component_stream_input")
    COMPONENT_STREAM_OUTPUT = EventBase.get_event("workflow_component_stream_output")  # ← 存在
```

**Go 问题代码** (`events.go:WorkflowEventType`):
```go
ComponentStreamInput  WorkflowEventType = "_framework:component_stream_input"
// ComponentStreamOutput 缺失！
```

**问题分析**：Go 有 `ComponentStreamInput` 但缺少 `ComponentStreamOutput`。这是一个明确的事件常量缺失，如果下游代码尝试注册或触发 `workflow_component_stream_output` 事件，将无法使用类型安全的 API。

**修复方案**：
```go
ComponentStreamOutput WorkflowEventType = "_framework:component_stream_output"
```

---

### G-08：[一般] CallbackFramework 缺少 trigger_stream / trigger_generator 流式触发

**Python 参考代码** (`framework.py`):
```python
async def trigger_stream(self, event, input_stream, ...):
    """接收 AsyncIterator，对每个元素触发事件并 yield 结果"""

async def trigger_generator(self, event, ...):
    """触发事件并聚合 async generator 输出"""
```

**Go 代码**：完全缺失这两个方法。

**问题分析**：Go 没有 async generator 原语，通常用 channel 模式替代。如果项目中有流式处理需求（如 Agent 的 stream 输出触发回调链），可能需要设计 channel 版本的 trigger 方法。当前缺失不影响已实现的功能，但可能在后续回填 10.6.x 时成为阻塞点。

**修复方案**：评估业务需求，如有流式处理场景，设计 channel 版本：
```go
func (fw *CallbackFramework) TriggerStream(ctx context.Context, event string, inputCh <-chan any, data map[string]any) <-chan any {
    outputCh := make(chan any, 16)
    go func() {
        defer close(outputCh)
        for item := range inputCh {
            results := fw.trigger(ctx, event, item, data)
            for _, r := range results {
                select {
                case outputCh <- r:
                case <-ctx.Done():
                    return
                }
            }
        }
    }()
    return outputCh
}
```

---

### T-06：[提示] CallbackFramework 缺少 emit_before/after/around 装饰器语义

**Python 参考代码** (`framework.py`):
```python
def emit_before(event, ...):
    """在函数执行前触发事件"""

def emit_after(event, ...):
    """在函数执行后触发事件"""

def emit_around(before, after, ...):
    """在函数执行前后触发事件"""
```

**Go 代码**：完全缺失装饰器体系，用显式方法调用替代。

**问题分析**：Go 的设计选择是合理的——Go 没有装饰器语法，显式调用更符合语言习惯。当前所有需要 emit 语义的场景都已通过显式 `TriggerXxx` 调用实现，功能等价。仅声明式语义有差异，不影响运行结果。

---

## 五、Adapter / Gateway MessageHandler（0 严重 / 2 一般 / 1 提示）

### G-09：[一般] MessageHandler 非单例，与 Python 行为不一致

**Python 参考代码** (`message_handler.py:46-51`):
```python
class MessageHandler:
    _instance = None
    _singleton_initialized = False

    def __new__(cls, *args, **kwargs):
        if cls._instance is None:
            cls._instance = super().__new__(cls)
        return cls._instance
```

**Go 代码** (`message_handler.go`):
```go
func NewMessageHandler(...) *MessageHandler {
    // 每次调用创建新实例
    return &MessageHandler{...}
}
```

**问题分析**：Python 的 `MessageHandler` 是单例模式，确保全局唯一实例。Go 的 `NewMessageHandler` 每次创建新实例。如果上层代码依赖单例行为（如 `channelStates` 共享、`pendingEvolutionApproval` 去重），创建多个实例会导致状态不一致。

**修复方案**：使用 `sync.Once` 实现单例模式：
```go
var (
    messageHandlerInstance *MessageHandler
    messageHandlerOnce     sync.Once
)

func GetMessageHandler(...) *MessageHandler {
    messageHandlerOnce.Do(func() {
        messageHandlerInstance = NewMessageHandler(...)
    })
    return messageHandlerInstance
}
```

---

### G-10：[一般] DeepAdapter.ProcessMessageImpl 缺少 ok=True 设置

**Python 参考代码** (`interface_deep.py:4490-4495`):
```python
return AgentResponse(
    ok=True,  # ← 显式设置
    response_type=AgentResponseType.FINAL_ANSWER,
    content=content,
    ...
)
```

**Go 问题代码** (`deep_adapter.go:750-752`):
```go
return &schema.AgentResponse{
    ResponseType: schema.AgentResponseTypeFinalAnswer,
    Content:      content,
    // 缺少 Ok: true
}
```

**问题分析**：Python 的 `AgentResponse` 默认 `ok=False`，成功时需要显式设置 `ok=True`。Go 的 `AgentResponse` 可能也有类似默认值，如果调用方检查 `Ok` 字段判断响应是否成功，会误判为失败。

**修复方案**：
```go
return &schema.AgentResponse{
    Ok:           true, // 对齐 Python ok=True
    ResponseType: schema.AgentResponseTypeFinalAnswer,
    Content:      content,
}
```

---

### T-07：[提示] AgentCard ID 不一致（Python 'jiuwenswarm' vs Go 'uapclaw'）

**Python 参考代码** (`interface_deep.py:2588`):
```python
id='jiuwenswarm',
```

**Go 代码** (`deep_adapter.go:396`):
```go
ID: "uapclaw",
```

**问题分析**：Go 使用了项目名 `uapclaw` 作为 AgentCard ID，Python 使用 `jiuwenswarm`。如果下游依赖此 ID 做路由或鉴权，会导致不匹配。但这是项目品牌迁移的一部分，可能是有意为之。建议在实现计划中记录此决策。

---

## ⤵️ 占位代码验证汇总

| 模块 | 占位代码 | 是否真的未实现 | 回填标记 | 备注 |
|------|---------|--------------|---------|------|
| handle_session | `autoTitle` 自动标题 | ✅ 确认未实现 | ⤵️ 11.x | 影响 UX，建议优先实现 |
| handle_session | `UserContent` 字段 | ✅ 确认未实现 | ⤵️ 11.x | 与 autoTitle 联动 |
| DeepAdapter | `_try_init_a2x_client` | ✅ 确认未实现 | ⤵️ A2X/11.10 | A2X 协议适配器 |
| DeepAdapter | `_sync_a2x_runtime_state` | ✅ 确认未实现 | ⤵️ A2X/11.10 | 同上 |
| DeepAdapter | `load_user_rails` | ✅ 确认未实现 | ⤵️ 10.6.3-10 | 用户自定义 Rail |
| DeepAdapter | `_build_configured_subagents` | ✅ 确认未实现 | ⤵️ agentcore.DeepAgent | 子代理构建 |
| DeepAdapter | 13 个 Rail builder | ✅ 确认未实现 | ⤵️ 10.6.3-10 | 护栏系统 |
| DeepAdapter | 10 个 Tool 注册 | ✅ 确认未实现 | ⤵️ 9.38-49 | 工具卡片 |
| DeepAdapter | `_update_runtime_config` | ✅ 确认未实现 | ⤵️ 10.6.3-10 | **核心缺失**，每次请求前都调用 |
| MessageHandler | Inbound/Outbound Pipeline | ✅ 确认未实现 | ⤵️ 11.12 | 数字分身管道 |
| MessageHandler | Gateway Hook | ✅ 确认未实现 | ⤵️ 11.13 | 网关钩子 |
| MessageHandler | cancel_team_work | ✅ 确认未实现 | **无标记** | ⚠️ 缺少回填标记 |
| MessageHandler | SessionMap | ✅ 确认未实现 | **无标记** | ⚠️ 缺少回填标记 |
| MessageHandler | Cron controller | ✅ 确认未实现 | **无标记** | ⚠️ 缺少回填标记 |

> ⚠️ 注意：MessageHandler 中的 `cancel_team_work`、`SessionMap`、`Cron controller` 缺少 ⤵️ 回填标记，建议补充标记以避免遗漏。

---

## 修复优先级建议

### P0 — 必须立即修复（功能影响）

| 编号 | 问题 | 影响范围 |
|------|------|---------|
| S-01 | SetSessionDeliveryContext 同步写入 | 每次请求处理路径阻塞 + 并发竞争 |
| S-02 | writeSessionMetadata 缺文件锁 | 并发写入可能损坏 metadata.json |
| S-04 | matchFailureKeyword 误判 | 脚本产物检测误判，影响 from_conv 信号质量 |

### P1 — 尽快修复（正确性影响）

| 编号 | 问题 | 影响范围 |
|------|------|---------|
| S-03 | 自动标题未实现 | 所有会话标题为空 |
| S-05 | toolName 硬编码 | 多工具优化时负例文件互相覆盖 |
| S-06 | NewSimpleEval 用 panic | 库代码 panic 是反模式 |
| S-08 | 缺 ComponentStreamOutput | 工作流流式输出事件缺失 |

### P2 — 计划修复（一致性影响）

| 编号 | 问题 | 影响范围 |
|------|------|---------|
| S-07 | excerpt 中文化 | 与提示词复刻规则冲突 |
| G-01 | 多出 channel_metadata 逻辑 | 与 Python 行为不一致 |
| G-03 | 类型断言失败 | JSON 反序列化后 trajectory_issues 丢失 |
| G-10 | 缺少 Ok=true | AgentResponse 成功判断可能出错 |
