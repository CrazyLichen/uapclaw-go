# 9.29 VerificationAgent 设计文档

## 概述

实现验证子 Agent（VerificationAgent）及其配套的三个 Rail：SubagentRail、VerificationRail、VerificationContractRail。

VerificationAgent 是 DeepAgent ReAct 循环中的**质量门控环节**，其核心作用是对抗性验证——不是确认"看起来对"，而是主动尝试**破坏**实现，发现隐藏缺陷。

## 流程位置与作用

### 在 Agent 会话中的调用链路

```
用户请求 → 父 Agent(如 CodeAgent) 执行实现
    → 父 Agent 完成非平凡实现（3+ 文件编辑 / 后端变更 / 基础设施变更）
    → VerificationContractRail 提醒父 Agent 必须启动验证
    → 父 Agent 通过 TaskTool 调用 verification_agent
    → VerificationAgent 执行对抗性验证（读代码 + bash 运行命令）
    → 输出 VERDICT: PASS / FAIL / PARTIAL
    → FAIL 时父 Agent 修复后重验（确定性 session ID 保证会话延续）
    → PASS 时父 Agent 向用户汇报完成
```

### 核心作用

1. **对抗性验证**：主动尝试破坏实现，发现隐藏缺陷
2. **强制门控**：父 Agent **不能自判** PASS/FAIL，只有 VerificationAgent 能下判决
3. **只读约束**：不能修改项目文件（只能用 /tmp），只能读+执行命令
4. **确定性会话**：session ID 固定，支持 FAIL→修复→重验的闭环

## 文件目录结构

### 新增文件

```
rails/subagent/                                    # 新建子包
├── doc.go                                         # 包文档
├── subagent_rail.go                               # SubagentRail
├── verification_rail.go                           # VerificationRail
├── verification_contract_rail.go                  # VerificationContractRail
├── subagent_rail_test.go                          # SubagentRail 测试
├── verification_rail_test.go                      # VerificationRail 测试
└── verification_contract_rail_test.go             # VerificationContractRail 测试

subagents/
├── verification_agent.go                          # BuildVerificationAgentConfig + 双语提示词
└── verification_agent_test.go                     # VerificationAgent 测试
```

### 需修改的文件

| 文件 | 变更内容 |
|------|---------|
| `rails/doc.go` | 添加 subagent 子包条目 |
| `subagents/doc.go` | 添加 verification_agent.go 条目 |
| `factory.go` | 替换 SubagentRail 占位为真实实例化 + 动态注入 VerificationContractRail |
| `IMPLEMENTATION_PLAN.md` | 更新 9.29 和 9.19-23 的状态标记 |

## 对应 Python 代码

| Go 组件 | Python 源码路径 |
|---------|----------------|
| SubagentRail | `openjiuwen/harness/rails/subagent/subagent_rail.py` |
| VerificationRail | `openjiuwen/harness/rails/subagent/verification_rail.py` |
| VerificationContractRail | `openjiuwen/harness/rails/subagent/verification_contract_rail.py` |
| BuildVerificationAgentConfig | `openjiuwen/harness/subagents/verification_agent.py` |

---

## 组件一：SubagentRail

### 职责

注册 TaskTool（同步）或 SessionTools（异步）到 Agent，并在每轮模型调用前注入对应的 prompt section。

### 对齐 Python

`openjiuwen/harness/rails/subagent/subagent_rail.py` — `SubagentRail` 类

### 结构体定义

```go
type SubagentRail struct {
    rails.DeepAgentRail
    enableAsyncSubagent bool
    tools               []tool.Tool
    toolkit             *subagent.SessionToolkit   // 仅异步模式使用
    promptBuilder       saprompt.Builder           // system_prompt_builder 引用
}
```

### 关键方法

| 方法 | 说明 |
|------|------|
| `NewSubagentRail(opts ...SubagentRailOption)` | 构造函数，默认 `enableAsyncSubagent=false` |
| `Init(agent)` | 捕获 system_prompt_builder；构建 available_agents 描述；注册 TaskTool 或 SessionTools |
| `Uninit(agent)` | 从 Agent 移除已注册工具 |
| `BeforeModelCall(ctx)` | 同步模式注入 `BuildTaskToolSection`；异步模式注入 `BuildSessionToolsSection` |
| `buildAvailableAgentsDescription(subagents)` | 构建可用子代理描述字符串 |
| `extractAgentMeta(spec)` | 提取代理名称和描述 |
| `extractAgentTools(spec, name)` | 4 级解析：显式 tools → 已注册 tools → 已知默认 → "All tools" |

### 常量与变量

```go
const subagentRailPriority = 95

// 已知代理工具映射（对齐 Python _KNOWN_AGENT_TOOLS）
var knownAgentTools = map[string]string{
    "explore_agent": "bash, glob, grep, list_files, read_file",
    "plan_agent":    "bash, glob, grep, list_files, read_file",
}
```

### 选项函数

```go
type SubagentRailOption func(*SubagentRail)

func WithEnableAsyncSubagent(enabled bool) SubagentRailOption
```

### 与现有代码的迁移

**factory.go 中需要替换的占位**：

1. `addDefaultRails` 中 SubagentRail 占位（L603-611）→ 替换为 `subagent.NewSubagentRail()` 实例化，仅当 `len(effectiveSubagents) > 0` 时添加
2. `injectGeneralPurposeSubagent` 中 `⤵️ SubagentRail 类型断言` （L362）→ 替换为 `r.(*subagent.SubagentRail)` 类型断言

**TaskTool 注册逻辑迁移**：SubagentRail.Init() 内部调用 `subagent.NewTaskTool()` 注册工具，将 factory.go 中散落的 TaskTool 注册代码整合进来。

---

## 组件二：VerificationRail

### 职责

为验证代理强制执行工具白名单和每轮约束提醒，包括工作空间范围守卫。

### 对齐 Python

`openjiuwen/harness/rails/subagent/verification_rail.py` — `VerificationRail` 类

### 结构体定义

```go
type VerificationRail struct {
    rails.DeepAgentRail
    allowedTools map[string]bool           // 工具白名单
    pathToolArg  map[string]string         // 路径工具参数映射
    promptBuilder saprompt.Builder         // system_prompt_builder 引用
}
```

### 常量

```go
const verificationRailPriority = 90
const reminderSectionName = "verification_reminder"
const reminderSectionPriority = 95

// 工具白名单（对齐 Python VERIFICATION_ALLOWED_TOOLS）
var defaultVerificationAllowedTools = map[string]bool{
    "read_file":    true,
    "bash":         true,
    "grep":         true,
    "glob":         true,
    "list_files":   true,
    "web_search":   true,
    "web_fetch":    true,
    "todo_create":  true,
    "todo_list":    true,
    "todo_modify":  true,
    "skill_tool":   true,
    "tool_search":  true,
}

// 路径工具参数映射（对齐 Python _PATH_TOOL_ARG）
var defaultPathToolArg = map[string]string{
    "list_files": "path",
    "read_file":  "file_path",
    "glob":       "path",
    "grep":       "path",
}
```

### 关键方法

| 方法 | 说明 |
|------|------|
| `NewVerificationRail(opts ...VerificationRailOption)` | 构造函数，默认使用 `defaultVerificationAllowedTools` |
| `Init(agent)` | 捕获 system_prompt_builder 引用 |
| `BeforeModelCall(ctx)` | 注入约束提醒 section（仅在 task loop 激活且非 plan 模式时） |
| `BeforeToolCall(ctx)` | 执行工具白名单 + 工作空间范围守卫 |

### 选项函数

```go
type VerificationRailOption func(*VerificationRail)

func WithAllowedTools(tools map[string]bool) VerificationRailOption
```

### BeforeToolCall 逻辑

1. 检查 `_skip_tool` 标记
2. MCP 工具（`mcp__*`）无条件放行
3. 白名单检查：不在白名单中的工具被拦截，返回错误消息
4. 工作空间范围守卫：对路径读取工具（read_file/glob/grep/list_files），检查路径是否在 workspace 范围内

### 提示词（一比一复刻 Python 原文）

**英文提醒 `_REMINDER_EN`**：

```
=== VERIFICATION AGENT — ACTIVE CONSTRAINTS ===
1. You CANNOT create, modify, or delete project files. Use /tmp only for ephemeral test scripts.
2. Every check MUST include a 'Command run' block with verbatim terminal output. A check without a command block is a SKIP, not a PASS.
3. You MUST end your final response with exactly one of:
   VERDICT: PASS
   VERDICT: FAIL
   VERDICT: PARTIAL
   No markdown, no punctuation after the verdict word, no variation.
4. Reading code is NOT verification. Run commands and show actual output.
```

**中文提醒 `_REMINDER_CN`**：

```
=== 验证代理 -- 当前约束 ===
1. 你不能创建、修改或删除项目文件。/tmp 仅可用于临时测试脚本。
2. 每项检查必须包含'执行命令'块，并逐字粘贴终端输出。没有命令块的检查视为跳过，而非 PASS。
3. 你必须以以下之一结束最终回复：
   VERDICT: PASS
   VERDICT: FAIL
   VERDICT: PARTIAL
   不加 Markdown，判决词后不加标点，不得有任何格式变体。
4. 阅读代码不等于验证。运行命令并展示实际输出。
```

---

## 组件三：VerificationContractRail

### 职责

向父 Agent（实现代理）注入验证门控契约，提醒父 Agent 在非平凡实现完成后必须启动 verification_agent。

### 对齐 Python

`openjiuwen/harness/rails/subagent/verification_contract_rail.py` — `VerificationContractRail` 类

### 结构体定义

```go
type VerificationContractRail struct {
    rails.DeepAgentRail
    promptBuilder saprompt.Builder
    section      *saprompt.PromptSection    // 预构建的契约 section
}
```

### 常量

```go
const verificationContractRailPriority = 88
```

### 关键方法

| 方法 | 说明 |
|------|------|
| `NewVerificationContractRail()` | 构造函数 |
| `Init(agent)` | 捕获 system_prompt_builder，预构建契约 section（使用 `SectionVerificationContract`） |
| `BeforeModelCall(ctx)` | 每轮注入契约 section（remove + add 避免重复） |

### 挂载方式

在 factory.go 的 `addDefaultRails` 中，检测到 `effectiveSubagents` 包含 `verification_agent` 时，自动注入 `VerificationContractRail`。具体判断方式：遍历 effectiveSubagents，检查 SubAgentConfig.AgentCard.GetName() == "verification_agent"。

### 提示词（一比一复刻 Python 原文）

**英文契约 `_CONTRACT_EN`**：

```
## Verification Gate

After any non-trivial implementation turn, you MUST spawn the verification agent before reporting completion to the user.

**Non-trivial means any of:**
- 3 or more file edits in a single turn
- Backend, API, or service changes
- Infrastructure or configuration changes

**How to spawn:**
Use task_tool with subagent_type="verification_agent". Pass:
1. The original user request (verbatim)
2. All files changed (full paths)
3. The approach you took
4. Plan file path if one was used

**On VERDICT: PASS**
Spot-check the report: re-run 2-3 of the commands listed in the verification report and confirm the output matches what the verifier observed. If every spot-checked command matches, report completion to the user.

**On VERDICT: FAIL**
Do not report completion. Fix the issue, then re-invoke task_tool with subagent_type="verification_agent". The same verification session will resume (deterministic session ID). Pass the previous FAIL output and describe what you fixed. Repeat until VERDICT: PASS.

**On VERDICT: PARTIAL**
Report what was verified and what could not be verified due to environmental limitations (e.g. service could not start, tool unavailable). Be explicit about the gap.

**You cannot self-assign any verdict.** Only the verification agent issues PASS, FAIL, or PARTIAL. Your own checks and caveats do not substitute.
```

**中文契约 `_CONTRACT_CN`**：

```
## 验证门控

在任何非平凡实现轮次之后，你必须在向用户汇报完成之前启动验证代理。

**非平凡指以下任意情况：**
- 单轮内编辑了 3 个或更多文件
- 后端、API 或服务变更
- 基础设施或配置变更

**如何启动：**
使用 task_tool，subagent_type="verification_agent"。传入：
1. 原始用户请求（原文）
2. 所有已更改的文件（完整路径）
3. 你采用的实现方式
4. 计划文件路径（如有）

**收到 VERDICT: PASS 时**
抽查报告：从验证报告中重新运行 2-3 条命令，确认输出与验证代理观察到的一致。若每条抽查命令均匹配，则向用户汇报完成。

**收到 VERDICT: FAIL 时**
不得汇报完成。修复问题后，再次调用 task_tool，subagent_type="verification_agent"。同一验证会话将继续（确定性会话 ID）。传入之前的 FAIL 输出并说明你修复了什么。重复此过程直到收到 VERDICT: PASS。

**收到 VERDICT: PARTIAL 时**
汇报哪些内容已验证，哪些因环境限制（如服务无法启动、工具不可用）未能验证。请明确说明缺口所在。

**你不能自行指定任何判决。** 只有验证代理才能发出 PASS、FAIL 或 PARTIAL。你自己的检查和注意事项不能替代验证代理的判决。
```

---

## 组件四：BuildVerificationAgentConfig

### 职责

构建 VerificationAgent 的 SubAgentConfig，供 DeepAdapter._build_configured_subagents 调用。

### 对齐 Python

`openjiuwen/harness/subagents/verification_agent.py` — `build_verification_agent_config()` + `create_verification_agent()`

### 工厂函数签名

```go
func BuildVerificationAgentConfig(model *llm.Model, params *hschema.SubagentCreateParams) *hschema.SubAgentConfig
```

### 差异化配置

| 属性 | 值 | 说明 |
|------|----|------|
| AgentCard.name | `"verification_agent"` | 常量 `VerificationAgentFactoryName` |
| MaxIterations | `40` | 远高于普通 Agent 的 15，验证需要更多迭代 |
| FactoryName | `""` | 空字符串，走通用路径 |
| RestrictToWorkDir | `false` | 与 PlanAgent/ExploreAgent 对齐 |
| 默认 Rails | `[SysOperationRail(), VerificationRail()]` | SysOperationRail 非只读 + VerificationRail 白名单 |

### 辅助函数

```go
func DefaultVerificationAgentSystemPrompt(language string) string
func DefaultVerificationAgentDescription(language string) string
```

### 提示词（一比一复刻 Python 原文）

**VERIFICATION_AGENT_DESC**：

- CN: `"验证专家——对抗性测试实现的正确性，尝试破坏并发现隐藏缺陷"`
- EN: `"Verification specialist — adversarially tests implementation correctness, trying to break it and find hidden defects"`

**VERIFICATION_AGENT_SYSTEM_PROMPT_EN**：完整复制 Python `VERIFICATION_AGENT_SYSTEM_PROMPT_EN`（约 120 行）

**VERIFICATION_AGENT_SYSTEM_PROMPT_CN**：完整复制 Python `VERIFICATION_AGENT_SYSTEM_PROMPT_CN`（约 110 行）

---

## 回填标记更新

| 位置 | 原标记 | 新标记 |
|------|--------|--------|
| IMPLEMENTATION_PLAN.md 9.29 | `☐ VerificationAgent` | `✅ VerificationAgent` |
| IMPLEMENTATION_PLAN.md 9.19-23 | `☐ 其他 Rails` 中 Verification 部分 | 标注 `⤴️ 9.29 回填 VerificationRail + VerificationContractRail` |
| factory.go L603-611 | `⤵️ 9.8-9.24 回填：SubagentRail` | 替换为真实 `SubagentRail` 实例化 |
| factory.go L362 | `⤵️ SubagentRail 类型断言待回填` | 替换为 `r.(*subagent.SubagentRail)` 类型断言 |

---

## 测试计划

### SubagentRail 测试

| 测试函数 | 覆盖内容 |
|---------|---------|
| `TestNewSubagentRail_默认配置` | 验证默认 enableAsyncSubagent=false、priority=95 |
| `TestNewSubagentRail_启用异步` | 验证 WithEnableAsyncSubagent(true) |
| `TestBuildAvailableAgentsDescription_空列表` | 返回空字符串 |
| `TestBuildAvailableAgentsDescription_多个子代理` | 格式 `- name: desc (Tools: ...)` |
| `TestExtractAgentTools_显式工具` | SubAgentConfig 有 tools 时提取工具名 |
| `TestExtractAgentTools_已知默认` | explore_agent/plan_agent 返回已知工具列表 |
| `TestExtractAgentTools_回退` | 未知代理返回 "All tools" |
| `TestSubagentRail_Init_无子代理跳过` | subagents 为空时不注册工具 |

### VerificationRail 测试

| 测试函数 | 覆盖内容 |
|---------|---------|
| `TestNewVerificationRail_默认白名单` | 验证默认 12 个允许工具 |
| `TestNewVerificationRail_自定义白名单` | WithAllowedTools 选项 |
| `TestVerificationRail_BeforeToolCall_允许工具` | 白名单内工具通过 |
| `TestVerificationRail_BeforeToolCall_拦截工具` | write_file/edit_file 被拦截 |
| `TestVerificationRail_BeforeToolCall_MCP放行` | mcp__* 前缀无条件放行 |
| `TestVerificationRail_BeforeToolCall_路径范围守卫` | 超出 workspace 的路径被拦截 |
| `TestVerificationRail_BeforeModelCall_注入提醒` | task loop 激活时注入 section |
| `TestVerificationRail_BeforeModelCall_跳过Plan模式` | plan 模式下不注入 |

### VerificationContractRail 测试

| 测试函数 | 覆盖内容 |
|---------|---------|
| `TestNewVerificationContractRail_优先级` | priority=88 |
| `TestVerificationContractRail_Init_预构建Section` | section 使用 SectionVerificationContract |
| `TestVerificationContractRail_BeforeModelCall_注入契约` | remove + add 避免重复 |
| `TestVerificationContractRail_BeforeModelCall_无Builder跳过` | promptBuilder 为 nil 时跳过 |

### VerificationAgent 测试

| 测试函数 | 覆盖内容 |
|---------|---------|
| `TestBuildVerificationAgentConfig_默认配置` | 验证默认 MaxIterations=40、FactoryName=""、RestrictToWorkDir=false |
| `TestBuildVerificationAgentConfig_CN提示词` | 验证中文提示词/描述的关键词 |
| `TestBuildVerificationAgentConfig_EN提示词` | 验证英文提示词/描述的关键词 |
| `TestBuildVerificationAgentConfig_自定义MaxIterations` | 用户指定 MaxIterations 时覆盖默认值 |
| `TestBuildVerificationAgentConfig_用户覆盖Card` | 自定义 AgentCard |
| `TestBuildVerificationAgentConfig_用户覆盖SystemPrompt` | 自定义 SystemPrompt |
| `TestBuildVerificationAgentConfig_默认Rails` | 默认包含 SysOperationRail + VerificationRail |
| `TestBuildVerificationAgentConfig_RestrictToWorkDir` | nil→false、显式true→true、显式false→false |
| `TestDefaultVerificationAgentSystemPrompt` | 辅助函数 + 未知语言回退 |
| `TestDefaultVerificationAgentDescription` | 辅助函数 + 未知语言回退 |

---

## 风险与注意事项

1. **提示词一比一复刻**：所有提示词字符串必须严格复制 Python 原文，不做任何自行翻译或改写
2. **factory.go 迁移**：将 TaskTool 注册逻辑迁移到 SubagentRail.Init() 时，需确保不破坏现有功能
3. **VerificationContractRail 动态注入**：需正确判断 effectiveSubagents 中是否包含 verification_agent，避免在非验证场景误注入
4. **确定性 Session ID**：`buildSubSessionID` 已处理 verification_agent，无需额外修改
5. **循环依赖**：`rails/subagent/` 可能需要导入 `tools/subagent/`（TaskTool），需确认不会产生循环依赖
