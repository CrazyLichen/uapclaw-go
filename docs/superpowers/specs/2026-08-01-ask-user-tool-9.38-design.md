# AskUserTool 空壳工具实现设计 (9.38)

## 概述

实现 `internal/agentcore/harness/tools/ask_user/` 子包，为 Agent 提供向用户提问的空壳工具（AskUserTool）。AskUserTool 的 invoke/stream 方法返回空 `map[string]any{}`，真正的用户交互逻辑在 AskUserRail（`harness/rails/interrupt/`）中通过中断机制完成。

本设计同时修正 `harness/prompts/tools/ask_user.go` 中双语描述的换行差异，确保提示词严格一比一复刻 Python。

服务端扩展（StructuredAskUserRail + interrupt_helpers + build_interactive_input）属于 10.6.3-10（Swarm Rails），不在 9.38 范围内。

## 在 Agent 会话中的流程位置与作用

```
LLM Think → 生成 ask_user 工具调用
         → BaseInterruptRail.BeforeToolCall 拦截
         → 发现 tool_name == "ask_user"
         → resolveInterrupt 判断是否有用户输入
            ├─ 无输入 → InterruptResult → 抛出 AbortError → 会话挂起
            │         → ToolInterruptHandler 收集中断请求
            │         → 生成 chat.ask_user_question 事件
            │         → 前端渲染问题选项 → 用户选择 → 恢复会话
            └─ 有输入 → 解析为 AskUserPayload → RejectResult
                      → 格式化回答文本注入 ToolResult
                      → LLM 继续下一轮 Think
```

核心作用：
1. **HITL 交互机制**：让 Agent 在需求模糊、多种方案可选、涉及用户偏好时主动暂停等待用户决策
2. **空壳工具**：AskUserTool.invoke/stream 永远不会被真正调用到，方法体 `return {}` 只是占位
3. **Rail 驱动**：真正逻辑在 AskUserRail 的 resolveInterrupt 中完成（中断→等待→恢复→格式化回答）

## 方案选择

**选定方案 A：新增 ask_user/ 子包**。

- 在 `harness/tools/` 下新增 `ask_user/` 子包，包含 AskUserTool 空壳 + NewAskUserTool 工厂函数
- AskUserRail.Init 改为从此子包创建工具（而非 inline 创建）
- 修正提示词换行差异
- 完整对齐 Python `openjiuwen/harness/tools/ask_user.py` 的文件组织

方案 B（不新增子包，保持 inline 创建）被否决，因为不严格对齐 Python 文件组织，也不符合 Go 项目中其他工具子包的组织约定。

## 文件组织

```
internal/agentcore/harness/tools/ask_user/
├── doc.go            # 包文档
├── ask_user.go       # AskUserTool struct + NewAskUserTool 工厂函数
└── ask_user_test.go  # 单元测试
```

## Python → Go 类型映射

| Python 类型 | Go 类型 | 说明 |
|-------------|---------|------|
| `AskUserTool(Tool)` | `AskUserTool struct` (空壳) | Python invoke/stream 返回 `{}`；Go 用 MapFunction 包装空 invokeFn |
| `AskUserTool.__init__(language, agent_id)` | `NewAskUserTool(language, agentID)` | 从 BuildToolCard 建卡 → NewMapFunction 包装 |
| `AskUserTool.invoke(query, **kwargs) → {}` | MapFunction.invokeFn 返回 `map[string]any{}` | 空壳，永远不被真正调用 |
| `AskUserTool.stream(query, **kwargs) → yield {}` | MapFunction.streamFn = nil | stream 不支持，与 Python `yield {}` 等价（Rail 拦截后不走 stream） |

## AskUserTool 空壳实现 (ask_user.go)

```go
package ask_user

// ──────────────────────────── 结构体 ────────────────────────────

// AskUserTool 向用户提问的空壳工具。
// invoke/stream 返回空 map{}，真正逻辑在 AskUserRail 中通过中断机制完成。
//
// 对齐 Python: AskUserTool(Tool) — openjiuwen/harness/tools/ask_user.py
type AskUserTool struct{}

// ──────────────────────────── 常量 ────────────────────────────

const (
    // logComponent 日志组件标识
    logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAskUserTool 创建 AskUserTool 空壳实例。
// 从 prompts/tools 注册表获取 ToolCard，用 MapFunction 包装空壳 invoke 函数。
//
// 对齐 Python: AskUserTool.__init__(language, agent_id)
func NewAskUserTool(language, agentID string) (tool.Tool, error) {
    card, err := hprompts.BuildToolCard("ask_user", "ask_user", language, nil, agentID)
    if err != nil {
        logger.Warn(logComponent).Str("event_type", "ask_user_tool_create").Err(err).Msg("构建 ask_user ToolCard 失败")
        return nil, fmt.Errorf("构建 ask_user ToolCard 失败: %w", err)
    }

    askUserTool, err := tool.NewMapFunction(card,
        func(_ context.Context, _ map[string]any) (map[string]any, error) {
            return map[string]any{}, nil
        },
        nil,
    )
    if err != nil {
        return nil, fmt.Errorf("创建 AskUserTool 失败: %w", err)
    }

    logger.Info(logComponent).Str("event_type", "ask_user_tool_create").Str("tool_id", card.ID).Msg("AskUserTool 创建成功")
    return askUserTool, nil
}
```

## AskUserRail.Init 改动

**修改文件**: `harness/rails/interrupt/ask_user_rail.go`

**改动**: Init 方法中的 inline 创建逻辑（BuildToolCard + NewMapFunction）替换为调用 `askuser.NewAskUserTool(language, agentID)`。

**import 变化**: 新增 `askuser "github.com/uapclaw/uap-claw-go/internal/agentcore/harness/tools/ask_user"`；删除不再需要的 `hprompts` 和 `tool` import（如果 Init 是唯一使用点）。

## 提示词换行修正

**修改文件**: `harness/prompts/tools/ask_user.go`

### cn 描述修正

Python 实际值（三句在同一行，无换行分隔）：
```
【禁止】选项中添加'其他'、'自定义'等兜底选项，系统已自动提供。【推荐】将推荐选项放第一位，label末尾加'（推荐）'。preview字段仅用于单选问题的视觉比较场景。
```

Go 当前值（三句各自独占一行，有 `\n` 分隔）——需修正为同一行。

### en 描述修正

Python 实际值（三句在同一行，空格分隔）：
```
FORBIDDEN: Adding 'Other', 'Custom' etc. as options — system provides this automatically. RECOMMENDED: Place recommended option first, append '(Recommended)' to its label. Preview field is only for single-select questions with visual comparison needs.
```

Go 当前值（三句各自独占一行，`\n` 分隔）——需修正为同一行，空格分隔。

**关键**: Go raw string 中物理换行会变成 `\n`，所以三句必须在 raw string 的同一物理行上。

## 测试设计

### ask_user_test.go（约 5 个测试）

| 用例 | 说明 | 对齐 Python |
|------|------|-------------|
| `TestNewAskUserTool` | 默认 cn 语言创建成功 | `AskUserTool(language="cn")` |
| `TestNewAskUserTool_en语言` | en 语言创建成功 | `AskUserTool(language="en")` |
| `TestNewAskUserTool_ToolCard属性` | card.name == "ask_user" + input_params 结构 | `build_tool_card(name="ask_user")` |
| `TestNewAskUserTool_Invoke空壳` | invoke 返回空 map{} | `AskUserTool.invoke → {}` |
| `TestNewAskUserTool_Stream不支持` | stream 返回 ErrStreamNotSupported | `AskUserTool.stream → yield {}`（Go 用 nil streamFn 表示不支持） |

### 已有测试验证

重构后需确认以下测试仍然通过：
- `ask_user_rail_test.go`（18 个单元测试）
- `ask_user_rail_integration_test.go`（2 个集成测试）

覆盖率目标：≥ 85%。

## 错误处理

| 场景 | Python | Go |
|------|--------|-----|
| ToolCard 构建失败 | 不可能（Python 注册表保证存在） | `BuildToolCard` 返回 err → 返回 error |
| NewMapFunction 失败 | 不可能（Python 直接构造） | `ValidateToolCard` 失败 → 返回 error |

## 日志

对齐 Python `ask_user.py`：Python 中 AskUserTool **没有任何 logger 调用**，Go 运行时层仅补充防御性日志：
- 成功创建：Info 级别记录 `tool_id`
- ToolCard 构建失败：Warn 级别记录 err

## 对应 Python 代码

- 工具本体：`openjiuwen/harness/tools/ask_user.py`（15 行，AskUserTool 空壳）
- 提示词元数据：`openjiuwen/harness/prompts/tools/ask_user.py`（Go 已实现，仅需修正换行）
- Rail 拦截：`openjiuwen/harness/rails/interrupt/ask_user_rail.py`（Go 已实现）

## 9.38 范围边界

| 属于 9.38 | 不属于 9.38（归 10.6.3-10） |
|-----------|---------------------------|
| AskUserTool 空壳子包 | StructuredAskUserRail 服务端扩展 |
| 提示词换行修正 | interrupt_helpers 中断事件转换 |
| AskUserRail.Init 改为子包创建 | _build_interactive_input_from_answers 会话恢复 |
| IMPLEMENTATION_PLAN.md AskUser ✅ | 前端 ask_user_question 格式转换 |

## IMPLEMENTATION_PLAN.md 进度更新

完成后将 9.38-49 合并行内 `AskUser` 改为 `✅AskUser`（与 `✅Cron` 格式一致），其余工具状态保持 ☐。

## 完整改动清单

| 改动 | 文件 | 说明 |
|------|------|------|
| 新增 | `harness/tools/ask_user/doc.go` | 包文档 |
| 新增 | `harness/tools/ask_user/ask_user.go` | AskUserTool 空壳 + NewAskUserTool |
| 新增 | `harness/tools/ask_user/ask_user_test.go` | 单元测试 |
| 修改 | `harness/rails/interrupt/ask_user_rail.go` | Init 改为调用 `askuser.NewAskUserTool` |
| 修改 | `harness/prompts/tools/ask_user.go` | 修正 cn/en 描述换行差异 |
| 修改 | `IMPLEMENTATION_PLAN.md` | 9.38-49: AskUser → ✅AskUser |
