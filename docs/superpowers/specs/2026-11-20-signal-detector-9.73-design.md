# 9.73 SignalDetector 设计文档

## 概述

实现自演化系统的信号检测模块，将 Agent 执行轨迹、用户反馈、评估结果等原始数据转换为结构化的 `EvolutionSignal`，供下游 Optimizer 消费。

对应 Python 代码：`openjiuwen/agent_evolving/signal/`

## 在自演化流程中的位置

```
加载 Case → 执行 Agent → 收集 Trajectory
                                  │
                                  ▼
                    信号检测 (SignalDetector)  ← 9.73
                    ├─ 离线：EvaluatedCase → Signal (已有)
                    ├─ 在线：Trajectory → Signal (from_conv)
                    └─ 团队：Team Trajectory → Signal (team)
                                  │
                                  ▼
                    Optimizer (9.72 系列)
                                  │
                                  ▼
                    Writeback 写回 SKILL.md
```

## 文件结构

```
internal/evolving/signal/
├── doc.go               # 包文档（更新文件目录）
├── signal.go            # EvolutionSignal + 补全枚举/工厂/指纹/ToDict
├── from_eval.go         # 已有，不变
├── from_conv.go         # 新增：ConversationSignalDetector
├── team.go              # 新增：TeamSignalDetector + 辅助类型
├── signal_test.go       # 补充测试
├── from_eval_test.go    # 已有，不变
├── from_conv_test.go    # 新增
└── team_test.go         # 新增

internal/evolving/trajectory/
└── types.go             # 新增 CrossMemberMetaKeys 常量
```

## 文件 1：signal.go 补全

回填到已有 `signal.go`，补充以下导出项：

### 新增枚举

- `EvolutionCategory` — `SkillExperience` / `NewSkill`，对齐 Python `EvolutionCategory(str, Enum)`
- `EvolutionTarget` — `Description` / `Body` / `Script`，对齐 Python `EvolutionTarget(str, Enum)`

### EvolutionSignal 新增方法

- `ToDict() map[string]any` — 对齐 Python `to_dict()`，输出 `{"type", "section", "excerpt", "skill_name", "context"?}`

### 新增导出函数

| 函数 | 签名 | 对齐 Python |
|------|------|------------|
| `MakeEvolutionSignal` | `(signalType, section, excerpt string, opts ...SignalOption) *EvolutionSignal` | `make_evolution_signal()` |
| `GetSignalSource` | `(sig *EvolutionSignal) *string` | `get_signal_source()` |
| `MakeSignalFingerprint` | `(sig *EvolutionSignal) [4]string` | `make_signal_fingerprint()` |

- `MakeEvolutionSignal`：合并 source/tool_name 到 context，对齐 Python `merged_context.setdefault("source", source)` / `merged_context.setdefault("tool_name", tool_name)`
- `MakeSignalFingerprint`：返回 `[4]string{signalType, toolName, skillName, excerpt[:200]}`，对齐 Python `(signal_type, str(tool_name), skill_name or "", excerpt[:200])`
- 使用 `SignalOption` 函数选项模式替代 Python 的 keyword-only 参数：
  - `WithSource(source string) SignalOption`
  - `WithToolName(toolName string) SignalOption`
  - `WithSkillName(skillName string) SignalOption`
  - `WithContext(context map[string]any) SignalOption`

## 文件 2：from_conv.go — ConversationSignalDetector

对齐 Python `openjiuwen/agent_evolving/signal/from_conv.py` (774 行)。

### 常量

#### 正则常量

| Go 名称 | 对齐 Python | 说明 |
|---------|------------|------|
| `failureKeywords` | `_FAILURE_KEYWORDS` | 执行失败关键词正则 |
| `correctionPattern` | `_CORRECTION_PATTERN` | 用户纠正模式正则 |
| `skillMDPattern` | `_SKILL_MD_PATTERN` | SKILL.md 路径正则 |
| `toolSchemaPattern` | `_TOOL_SCHEMA_PATTERN` | 工具 schema 输出正则 |
| `collaborationFailurePattern` | `_COLLABORATION_FAILURE_PATTERN` | 协作失败正则 |

#### 提示词常量（原文复刻，不翻译）

| Go 名称 | 对齐 Python |
|---------|------------|
| `userFeedbackPromptCN` | `_USER_FEEDBACK_PROMPT_CN` |
| `userFeedbackPromptEN` | `_USER_FEEDBACK_PROMPT_EN` |

#### 集合常量

| Go 名称 | 对齐 Python |
|---------|------------|
| `dataFetchTools` | `_DATA_FETCH_TOOLS` |
| `codeExecTools` | `_CODE_EXEC_TOOLS` |
| `execContentKeys` | `_EXEC_CONTENT_KEYS` |
| `collaborationSignalTypes` | `_COLLABORATION_SIGNAL_TYPES` |

### ConversationSignalDetector 结构体

```go
type ConversationSignalDetector struct {
    existingSkills map[string]bool
    llm            *llm.Model
    model          string
    language       string
}
```

### 方法映射

| Go 方法 | Python 方法 | 说明 |
|---------|------------|------|
| `NewConversationSignalDetector(opts ...ConvDetectorOption)` | `__init__(existing_skills)` | 构造函数 |
| `Detect(trajectoryOrMessages)` | `detect(trajectory_or_messages)` | 主入口，接受 Trajectory 或 `[]map[string]any` |
| `DetectTrajectorySignals(trajectory, messages)` | `detect_trajectory_signals(trajectory, messages)` | 被动轨迹信号 |
| `BindLLM(llm, model, language)` | `bind_llm(llm, model, language)` | 绑定 LLM |
| `DetectUserMessageFeedback(ctx, trajectoryOrMessages)` | `detect_user_message_feedback(...)` | 用户纠正反馈（async→返回信号） |
| `DetectUserIntent(ctx, trajectoryOrMessages)` | `detect_user_intent(...)` | LLM 判断用户意图（async→返回信号） |
| `ConvertTrajectoryToMessages(trajectory)` | `convert_trajectory_to_messages(trajectory)` | 静态方法，Trajectory→消息列表 |

### 私有方法映射

| Go 方法 | Python 方法 |
|---------|------------|
| `detectFromMessages(messages)` | `_detect_from_messages(messages)` |
| `resolveActiveSkill(msgIdx, skillReadHistory)` | `_resolve_active_skill(msg_idx, skill_read_history)` |
| `detectSkillFromToolCalls(toolCalls)` | `_detect_skill_from_tool_calls(tool_calls)` |
| `isExistingSkill(skillName)` | `_is_existing_skill(skill_name)` |
| `isSkillMDReadTool(name)` | `_is_skill_md_read_tool(name)` |
| `inferSkillFromMessages(messages)` | `_infer_skill_from_messages(messages)` |
| `fallbackUserFeedbackSignals(userMessages, skillName)` | `_fallback_user_feedback_signals(...)` |
| `makeUserFeedbackSignal(excerpt, skillName)` | `_make_user_feedback_signal(excerpt, skill_name)` |
| `extractCodeFromArgs(toolCall)` | `_extract_code_from_args(tool_call)` |
| `detectCollaborationSignals(trajectory)` | `_detect_collaboration_signals(trajectory)` |
| `isTeamMemberContext(trajectory)` | `_is_team_member_context(trajectory)` |
| `resolveActiveSkillForStep(step, allSteps, skillReadHistory)` | `_resolve_active_skill_for_step(...)` |
| `extractToMember(callArgs)` | `_extract_to_member(call_args)` |
| `extractTaskID(callArgs)` | `_extract_task_id(call_args)` |
| `deduplicate(signals)` | `_deduplicate(signals)` |

### 辅助函数

| Go 函数 | Python 函数 |
|---------|------------|
| `getField(obj, key, default)` | `_get_field(obj, key, default)` |
| `extractAroundMatch(content, matchStart, matchEnd, before, after)` | `_extract_around_match(content, match, before, after)` |
| `responseToText(response)` | `_response_to_text(response)` |

### 类型别名

```go
type SignalDetector = ConversationSignalDetector
```

对齐 Python `SignalDetector = ConversationSignalDetector`。

### Detect 方法重载处理

Python `detect()` 接受 `Union[Trajectory, List[dict]]`，Go 使用 `any` 参数 + 类型断言：

```go
func (d *ConversationSignalDetector) Detect(trajectoryOrMessages any) []*EvolutionSignal
```

### LLM 调用方式

**ConversationSignalDetector**：直接调用 `llm.Model.Invoke()` + `context.WithTimeout(30s)`，失败走 fallback 正则。严格对齐 Python。

**TeamSignalDetector**：使用 `llm_resilience.InvokeTextWithRetry()`，对齐 Python。

## 文件 3：team.go — TeamSignalDetector

对齐 Python `openjiuwen/agent_evolving/signal/team.py` (502 行)。

### 提示词常量（原文复刻，不翻译）

| Go 名称 | 对齐 Python |
|---------|------------|
| `teamUserRequestPromptCN` | `_TEAM_USER_REQUEST_PROMPT_CN` |
| `teamUserRequestPromptEN` | `_TEAM_USER_REQUEST_PROMPT_EN` |
| `teamTrajectoryIssuePromptCN` | `_TEAM_TRAJECTORY_ISSUE_PROMPT_CN` |
| `teamTrajectoryIssuePromptEN` | `_TEAM_TRAJECTORY_ISSUE_PROMPT_EN` |

### 类型

| Go 类型 | 对齐 Python |
|---------|------------|
| `TeamSignalType` 枚举 | `TeamSignalType(str, Enum)` |
| `UserIntent` 结构体 | `UserIntent` frozen dataclass |
| `TrajectoryIssue` 结构体 | `TrajectoryIssue` frozen dataclass |

### 导出函数

| Go 函数 | Python 函数 |
|---------|------------|
| `ParseTeamModelJSON(raw)` | `parse_team_model_json(raw)` |
| `BuildTeamTrajectorySummary(trajectory)` | `build_team_trajectory_summary(trajectory)` |
| `MakeTeamUserIntentSignal(skillName, userIntent)` | `make_team_user_intent_signal(...)` |
| `MakeTeamTrajectorySignal(skillName, skillContent, trajectoryIssues)` | `make_team_trajectory_signal(...)` |
| `GetTeamTrajectoryIssues(signal)` | `get_team_trajectory_issues(signal)` |
| `GetTeamSignalSkillContent(signal)` | `get_team_signal_skill_content(signal)` |

### TeamSignalDetector 结构体

```go
type TeamSignalDetector struct {
    llm                        *llm.Model
    model                      string
    language                   string
    trajectoryIssueLLMPolicy   llm_resilience.LLMInvokePolicy
    userIntentLLMPolicy        llm_resilience.LLMInvokePolicy
}
```

构造时必须至少传入一个 `LLMInvokePolicy`，对齐 Python 的 `ValueError`。

### 方法映射

| Go 方法 | Python 方法 |
|---------|------------|
| `DetectUserIntent(ctx, messages, teamSkillContent)` | `detect_user_intent(messages, team_skill_content)` |
| `DetectTrajectorySignals(ctx, trajectory, skillName, skillContent)` | `detect_trajectory_signals(...)` |
| `DetectTrajectoryIssues(ctx, trajectory, skillContent)` | `detect_trajectory_issues(...)` |

### 私有函数

| Go 函数 | Python 函数 |
|---------|------------|
| `tryParseJSON(text)` | `_try_parse_json(text)` |
| `fixJSONText(text)` | `_fix_json_text(text)` |
| `extractBalancedJSON(text, opener, closer)` | `_extract_balanced_json(text, opener, closer)` |
| `extractRolesSummary(teamSkillContent)` | `_extract_roles_summary(team_skill_content)` |
| `normalizeIssue(item)` | `_normalize_issue(item)` |

## trajectory 包新增

在 `internal/evolving/trajectory/types.go` 新增常量：

```go
var (
    // CrossMemberMetaKeys 跨成员元数据键集合。
    // 对应 Python: CROSS_MEMBER_META_KEYS = frozenset({"invoke_id", "parent_invoke_id", "child_invokes"})
    CrossMemberMetaKeys = map[string]bool{
        "invoke_id":        true,
        "parent_invoke_id": true,
        "child_invokes":    true,
    }
)
```

## 回填清单

| 回填来源 | 回填目标 | 说明 |
|---------|---------|------|
| 9.73 signal.go 补全 | 已有 `EvolutionSignal` 结构体 | 新增 `ToDict()` 方法 |
| 9.73 signal.go 补全 | 已有 `signal.go` | 新增枚举 + 工厂函数 + 指纹函数 |
| 9.73 from_conv.go | `signal` 包新增文件 | ConversationSignalDetector 完整实现 |
| 9.73 team.go | `signal` 包新增文件 | TeamSignalDetector 完整实现 |
| 9.73 trajectory | `trajectory/types.go` | 新增 `CrossMemberMetaKeys` |

## 关键设计决策

1. **提示词原文复刻**：所有 prompt 字符串一比一复刻 Python 原文，不做翻译
2. **LLM 调用方式**：ConversationSignalDetector 直接调用 `Model.Invoke`；TeamSignalDetector 走 `InvokeTextWithRetry`
3. **LLM 类型**：直接使用 `*llm.Model`，不定义额外接口
4. **CrossMemberMetaKeys**：放在 trajectory 包，对齐 Python 放置位置
5. **Detect 参数**：使用 `any` + 类型断言模拟 Python Union 类型
6. **异步方法**：Go 中使用 `context.Context` 作为第一个参数

## 依赖图

```
signal/from_conv.go ──→ trajectory (Trajectory, CrossMemberMetaKeys)
                   ──→ schema (UserIntentSignal, TrajectoryIssueSignal)
                   ──→ llm (Model)
                   ──→ signal/base (EvolutionSignal, MakeEvolutionSignal, MakeSignalFingerprint)

signal/team.go     ──→ trajectory (Trajectory)
                   ──→ schema (UserIntentSignal, TrajectoryIssueSignal)
                   ──→ llm (Model)
                   ──→ optimizer/llm_resilience (InvokeTextWithRetry, LLMInvokePolicy)
                   ──→ signal/base (EvolutionSignal, MakeEvolutionSignal)
```
