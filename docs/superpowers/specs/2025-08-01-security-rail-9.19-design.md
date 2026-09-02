# 9.19-23 SecurityRail 体系设计

> 对齐 Python：`openjiuwen/harness/rails/security/` + `openjiuwen/harness/security/`

## 1. 概述

SecurityRail 体系是 DeepAgent 的安全护栏，分为两层：

| 层 | Rail | 钩子位置 | 作用 |
|---|------|---------|------|
| **Prompt 层** | SafetyPromptRail（别名 SecurityRail） | `before_model_call` | 在每次 LLM 调用前注入 7 条安全原则到 system prompt，引导模型自律 |
| **Tool 层** | PermissionInterruptRail | `before_tool_call` | 拦截每个工具调用，通过分层策略评估权限（DENY/ASK/ALLOW），ASK 时中断等用户确认，支持"始终允许"持久化 |

在 ReAct 循环中的位置：当 `create_deep_agent()` 工厂构建 Agent 时，SafetyPromptRail **始终**被添加为默认 Rail（priority=85），PermissionInterruptRail 在 `permissions.enabled=true` 时**条件**添加（priority=90）。

## 2. 包结构（对齐 Python）

```
internal/agentcore/harness/
├── security/                              ← 基础设施层（已有 models.go，扩展）
│   ├── doc.go                             ← 更新
│   ├── models.go                          ← 已有，补充 ToolPermissionHost / PermissionSceneHookInput 等
│   ├── permission_engine.go              ← 新增：PermissionEngine
│   ├── tiered_policy.go                  ← 新增：分层策略评估（★最复杂）
│   ├── shell_ast.go                      ← 新增：tree-sitter bash 解析 + 保守扫描 fallback
│   ├── checker.go                        ← 新增：ExternalDirectoryChecker
│   ├── patterns.go                       ← 新增：通配符/路径/URL/Command 匹配 + YAML 持久化
│   ├── suggestions.go                    ← 新增："始终允许"建议构建
│   ├── factory.go                        ← 新增：BuildPermissionInterruptRail
│   └── *_test.go
│
├── rails/security/                        ← Rail 层（新增包）
│   ├── doc.go
│   ├── base_security_rail.go             ← BaseSecurityRail + 决策类型 + apply 逻辑
│   ├── prompt_security_rail.go           ← SafetyPromptRail（别名 SecurityRail）
│   ├── tool_security_rail.go             ← PermissionInterruptRail
│   └── *_test.go
```

Python 对应关系：

| Python | Go |
|--------|-----|
| `harness/rails/security/base_security_rail.py` | `rails/security/base_security_rail.go` |
| `harness/rails/security/prompt_security_rail.py` | `rails/security/prompt_security_rail.go` |
| `harness/rails/security/tool_security_rail.py` | `rails/security/tool_security_rail.go` |
| `harness/security/models.py` | `security/models.go`（已有，扩展） |
| `harness/security/core.py` | `security/permission_engine.go` |
| `harness/security/tiered_policy.py` | `security/tiered_policy.go` |
| `harness/security/shell_ast.py` | `security/shell_ast.go` |
| `harness/security/checker.py` | `security/checker.go` |
| `harness/security/patterns.py` | `security/patterns.go` |
| `harness/security/host.py` | `security/models.go` 中补充（内容少，合并） |
| `harness/security/suggestions.py` | `security/suggestions.go` |
| `harness/security/factory.py` | `security/factory.go` |

## 3. 核心类型设计

### 3.1 rails/security/base_security_rail.go

```
──────────────────────────── 结构体 ────────────────────────────

SecurityCheckContext          安全检查上下文
  CallbackCtx                 *agentinterfaces.AgentCallbackContext
  Event                       agentinterfaces.AgentCallbackEvent
  UserInput                   any
  AutoConfirmConfig           map[string]any
  SubjectID                   string

SecurityDecision             安全决策基类（非导出，仅作为类型标记）
  decisionType                string

SecurityAllow                允许执行
  SecurityDecision
  NewArgs                     string

SecurityReject               拒绝执行
  SecurityDecision
  Message                     string
  Result                      any
  ToolMessage                 *llmschema.ToolMessage

SecurityInterrupt            中断等待用户输入
  SecurityDecision
  Request                     saschema.InterruptRequester
  SubjectID                   string

SecurityAlertLevel           告警级别枚举
  SecurityAlertLevelInfo
  SecurityAlertLevelWarning
  SecurityAlertLevelError
  SecurityAlertLevelCritical

SecurityAlert                允许执行但告警
  SecurityDecision
  Message                     string
  Level                       SecurityAlertLevel
  AlertType                   string
  DisplayMode                 string

BaseSecurityRail             安全 Rail 抽象基类
  rails.DeepAgentRail         （嵌入）
  supportedEvents             map[agentinterfaces.AgentCallbackEvent]bool
  toolNames                   map[string]struct{}

──────────────────────────── 常量 ────────────────────────────

modelEvents                  {BeforeModelCall, AfterModelCall}

──────────────────────────── 导出函数 ────────────────────────────

NewBaseSecurityRail          创建安全 Rail 基类

──────────────────────────── 非导出函数 ────────────────────────────

runAndApply                  执行安全检查并应用决策（核心调度）
applySecurityDecision        应用安全决策（Allow=noop, Alert=log+stream, Reject=skipTool/forceFinish, Interrupt=raiseInterrupt）
runSecurityCheck             执行安全检查（抽象，子类实现）
applyReject                  拒绝分支处理（MODEL事件→forceFinish, BEFORE_TOOL_CALL→skipTool, AFTER_TOOL_CALL→forceFinish+toolResult）
applyInterrupt               中断分支处理（MODEL事件→转为Reject, TOOL事件→raiseInterrupt）
applyAlert                   告警分支处理（log+WriteStream OutputSchema with is_security_alert=true）
handleInterruptResume        中断恢复通用逻辑（auto_confirm 检查 → 解析用户输入 → store_auto_confirm → 返回 Allow/Reject）
buildForceFinishResult       构建 forceFinish 结果
raiseToolInterrupt           封装 raiseInterrupt 调用
skipTool                     封装 skipTool + toolResult 设置
isAutoConfirmed              静态方法：检查 auto_confirm 配置
storeAutoConfirm             静态方法：写入 auto_confirm
popLastUserMessage           从消息历史弹出最后一条用户消息
popMatchingMessages          按模式匹配弹出消息
sanitizeMatchingMessages     按模式匹配消毒消息（替换为 [REDACTED]）
extractMessageContent        提取消息文本内容
containsAnyPattern           检查文本是否包含任意模式
resolveSubjectID             解析 subject_id
resolveToolCallID            解析 tool_call_id
getUserInput                 从 session 获取用户输入
getAutoConfirmConfig         从 session 获取 auto_confirm 配置
```

**决策调度逻辑（applySecurityDecision）**：

```
SecurityAllow   → 不做任何操作，继续执行
SecurityAlert   → 记录日志 + WriteStream(OutputSchema{type:"message", metadata.is_security_alert=true}) → 继续执行
SecurityReject  → MODEL事件: cbc.RequestForceFinish(result)
                  BEFORE_TOOL_CALL: cbc.Extra()["_skip_tool"]=true + toolInputs.ToolResult=reject.Result
                  AFTER_TOOL_CALL: cbc.RequestForceFinish(result) + toolInputs.ToolResult/toolMsg
SecurityInterrupt → MODEL事件: 转为 SecurityReject（模型调用无法中断）
                    TOOL事件: raiseToolInterrupt → panic AbortError(ToolInterruptException)
```

**runAndApply 调度逻辑**：

```
1. 从 cbc 构造 SecurityCheckContext（event, userInput, autoConfirmConfig, subjectID）
2. 调用子类 runSecurityCheck(securityCtx) → SecurityDecision
3. 调用 applySecurityDecision(securityCtx, decision)
```

**handleInterruptResume 逻辑**：

```
1. 检查 autoConfirmConfig 中是否有 autoConfirmKey
2. 如果有 → 返回 SecurityAllow
3. 获取 userInput
4. 如果无 userInput → 返回 nil（首次调用，走 Interrupt）
5. 解析 userInput：
   - approved + auto_confirm → storeAutoConfirm → 返回 SecurityAllow
   - approved → 返回 SecurityAllow
   - rejected → 返回 SecurityReject
```

### 3.2 rails/security/prompt_security_rail.go

```
──────────────────────────── 结构体 ────────────────────────────

SafetyPromptRail             安全提示词 Rail
  BaseSecurityRail            （嵌入）
  systemPromptBuilder         saprompt.SystemPromptBuilderInterface

SecurityRail = SafetyPromptRail   类型别名，对齐 Python

──────────────────────────── 常量 ────────────────────────────

safetyPromptRailPriority     85
supportedEvents              {BeforeModelCall}

──────────────────────────── 导出函数 ────────────────────────────

NewSafetyPromptRail          创建安全提示词 Rail

──────────────────────────── 非导出函数 ────────────────────────────

init                         获取 systemPromptBuilder
uninit                       移除 safety section
runSecurityCheck             注入 BuildSafetySection() → 返回 SecurityAllow
```

**行为**：`before_model_call` 时调用 `systemPromptBuilder.AddSection(BuildSafetySection())`，然后返回 `SecurityAllow`。SafetyPromptRail 的 `Init` 钩子保存 `systemPromptBuilder` 引用，`Uninit` 时移除 safety section。

### 3.3 rails/security/tool_security_rail.go

```
──────────────────────────── 结构体 ────────────────────────────

PermissionInterruptRail      权限中断 Rail
  interrupt.ConfirmInterruptRail  （嵌入，继承中断恢复机制）
  engine                      *security.PermissionEngine
  host                        *security.ToolPermissionHost
  config                      security.PermissionsSection
  toolNameAliases             map[string]string

──────────────────────────── 常量 ────────────────────────────

TOOL_NAME_ALIASES            {"free_search":"mcp_free_search", "paid_search":"mcp_paid_search",
                               "fetch_webpage":"mcp_fetch_webpage", "exec_command":"mcp_exec_command"}

──────────────────────────── 导出函数 ────────────────────────────

NewPermissionInterruptRail   创建权限中断 Rail

──────────────────────────── 非导出函数 ────────────────────────────

resolveInterrupt             重写 ResolveInterruptFn
normalizeToolName            通过 ALIASES 规范化工具名
getAutoConfirmKey            生成 auto_confirm key（shell 工具走 shellAST，其他走 toolName）
buildShellAutoConfirmKey     shell 命令的 auto_confirm key 生成
shouldStoreAutoConfirm       判断是否应该持久化 auto_confirm
persistAllowAlways           持久化"始终允许"规则
beforeToolCall               拦截入口（重写 ConfirmInterruptRail 的 hook）
updateConfig                 热更新权限配置
collectExternalDirectoryPersistPaths  收集外部目录持久化路径
parseToolArgs                解析工具参数
parseConfirmPayload          解析确认载荷
buildMessage                 构建用户可见的权限请求消息（CN/EN 双语）
buildAlwaysAllowHint         构建"始终允许"提示
```

**resolveInterrupt 核心逻辑**：

```
1. 规范化工具名（ALIASES）
2. 如果 host.PermissionSceneHook 存在 → 先调用场景钩子（approve/reject 短路）
3. 如果 userInput == nil（首次调用）：
   a. 从 host.GetPermissionsSnapshot 或 updateConfig 刷新配置
   b. 调用 engine.CheckPermission(normalizedName, toolArgs)
   c. ALLOW → Approve
   d. DENY → Reject
   e. ASK → 检查 auto_confirm → 如果已确认 → Approve
   f. 如果 host.RequestPermissionConfirmation 存在 → 调用托管确认
      - 返回 PermissionConfirmResponse(approved+auto_confirm) → persist → Approve
      - 返回 PermissionConfirmResponse(approved) → Approve（一次性）
      - 返回 PermissionConfirmResponse(rejected) → Reject
      - 返回 "interrupt" → 走标准中断流程
      - 返回 nil → Reject
   g. 否则 → Interrupt(ConfirmPayload)
4. 如果 userInput != nil（恢复）：
   a. 解析为 ConfirmPayload
   b. approved + auto_confirm → persistAllowAlways → Approve
   c. approved → Approve（一次性）
   d. rejected → Reject(feedback)
```

### 3.4 security/permission_engine.go

```
──────────────────────────── 结构体 ────────────────────────────

PermissionEngine             权限评估引擎
  config                      security.PermissionsSection
  llm                         *llm.Model（可选，用于 LLM 辅助评估）
  modelName                   string
  workspaceRoot               string
  permissionChecksActive      func() bool
  enabled                     bool

──────────────────────────── 导出函数 ────────────────────────────

NewPermissionEngine          创建权限评估引擎
CheckPermission              主入口：检查权限（尊重 enabled/active 标志）
CheckToolPermissionDirectly  直接检查（绕过 enabled/active）
EvaluateGlobalPolicyDirectly 全局策略直接评估
UpdateConfig                 热更新配置
UpdateLLM                    更新 LLM 客户端
SetPermissionChecksActive    设置动态开关
Enabled                      是否启用

──────────────────────────── 非导出函数 ────────────────────────────

getReason                    生成原因描述
```

**CheckPermission 逻辑**：

```
1. 如果 enabled=false 或 permissionChecksActive()=false → 返回 PermissionResult{PermissionLevelAllow}
2. 调用 EvaluateGlobalPolicyDirectly(toolName, toolArgs)
3. 如果无匹配 → 默认 ASK
4. 调用 ExternalDirectoryChecker.CheckExternalPaths(toolName, toolArgs)
5. 取两者中更严格的
6. 返回 PermissionResult
```

### 3.5 security/tiered_policy.go

```
──────────────────────────── 结构体 ────────────────────────────

tieredInvocationContext      分层调用上下文（非导出）
  Config                      PermissionsSection
  ToolName                    string
  ToolArgs                    map[string]any
  Mode                        string

──────────────────────────── 常量 ────────────────────────────

shellTools                   {"bash", "mcp_exec_command", "create_terminal"}
pathTools                    {"read_file", "write_file", "edit_file", ...}
networkTools                 {"mcp_fetch_webpage", "mcp_free_search", "mcp_paid_search"}
pathArgKeys                  {"path", "file_path", "target_file", ...}
strictOrder                  DENY=0 < ASK=1 < ALLOW=2

──────────────────────────── 导出函数 ────────────────────────────

EvaluateTieredPolicy         分层策略评估（核心函数）
MaybeEscalateShellOperators  Shell 元字符升级检查
SeverityToDecision           严重级别→权限决策
Strictest                    取最严格权限
GetBuiltinSecurityRules      获取内置安全规则（sync.Once 缓存）
RuleToolsCategoryConsistent  规则工具类别一致性检查

──────────────────────────── 非导出函数 ────────────────────────────

evaluateSingleInvocation     单次调用评估
collectParamRuleHits         收集参数规则命中
collectApprovalOverrideHits  收集审批覆盖命中
baselineLevel                基线权限级别
finalizeHits                 从命中列表确定最终权限
shellAstFloor                Shell AST 结构底线
applyShellAstFloor           应用 Shell AST 底线
aggregateSubcommandResults   聚合子命令结果
withShellCommand             包装 shell 命令参数
commandText                  提取命令文本
shellPatternMatches          Shell 模式匹配
pathPatternMatches           路径模式匹配
toolArgValueLooksLikePath    工具参数值是否看起来像路径
iterPathStrings              迭代提取路径字符串
parseLevel                   解析权限级别字符串
toolCategory                 工具分类
```

**EvaluateTieredPolicy 优先级链**：

```
1. Tool-level deny baseline → DENY 立即短路
2. Shell 工具: ParseShellForPermission → shell_ast_floor
3. Shell "simple" 含子命令: 逐个评估 → 聚合（取最严格）
4. evaluateSingleInvocation:
   a. builtin 参数规则命中（任何 DENY → 立即短路）
   b. user 参数规则命中（任何 DENY → 立即短路）
   c. approval_overrides 命中 → ALLOW
   d. builtin 命中 → finalize（取最严格）
   e. user 命中 → finalize（取最严格）
   f. baseline（tools.X）
   g. defaults.*
   h. fallback: ASK
5. 应用 shell_ast_floor（如有管道/重定向等结构特征，升级权限）
6. 应用 MaybeEscalateShellOperators（shell 元字符 ALLOW→ASK，但 approval_overrides 豁免）
```

### 3.6 security/shell_ast.go

```
──────────────────────────── 结构体 ────────────────────────────

ShellAstKind                 解析结果类型枚举
  ShellAstKindSimple          可信任，子命令可评估
  ShellAstKindTooComplex      结构风险，不可信任
  ShellAstKindParseUnavailable 解析器不可用

ShellStructureFlags          Shell 结构标志
  Pipeline                    bool
  CompoundOperators           bool
  Subshell                    bool
  CommandGroup                bool
  CommandSubstitution         bool
  ProcessSubstitution         bool
  ParameterExpansion          bool
  Heredoc                     bool
  InputRedirection            bool
  OutputRedirection           bool
  Operators                   []string

ShellSubcommand              Shell 子命令
  Text                        string
  Argv                        []string
  Redirects                   []string

ShellAstParseResult          Shell AST 解析结果
  Kind                        ShellAstKind
  Subcommands                 []ShellSubcommand
  Flags                       ShellStructureFlags
  Reason                      string
  Backend                     string  ("tree-sitter" / "conservative")

──────────────────────────── 导出函数 ────────────────────────────

ParseShellForPermission      解析 Shell 命令用于权限评估

──────────────────────────── 非导出函数 ────────────────────────────

parseWithTreeSitter          tree-sitter 精确解析
parseWithConservativeFallback 保守正则扫描 fallback
scanShellStructure           正则扫描 Shell 结构特征
collectStructureFlags        从 tree-sitter AST 收集结构标志
extractSubcommands           从 tree-sitter AST 提取子命令
isRiskyStructure             判断是否为风险结构（命令替换/进程替换/参数展开/heredoc/subshell/命令组）
```

**双后端策略**：

| 后端 | 条件 | 行为 |
|------|------|------|
| tree-sitter | CGO 可用 | 精确 AST 解析，提取子命令 + 结构标志。风险结构 → TooComplex，否则 → Simple |
| 保守扫描 | tree-sitter 不可用或解析失败 | 正则扫描结构特征 + shlex 分词。风险结构 → ParseUnavailable，否则 → Simple |

tree-sitter 依赖：
- `github.com/tree-sitter/go-tree-sitter v0.25.0`（CGO，项目已有 `CGO_ENABLED=1`）
- `github.com/tree-sitter/tree-sitter-bash v0.25.1`

### 3.7 security/checker.go

```
──────────────────────────── 结构体 ────────────────────────────

ExternalDirectoryChecker     外部目录检测器
  config                      map[string]string  // external_directory 配置
  workspaceRoot               string

──────────────────────────── 常量 ────────────────────────────

shellOperatorsRe             正则匹配 Shell 操作符 [;&|`<>]|\$[({]|\r?\n
commandExecTools             {"mcp_exec_command"}
pathAwareCommands            {"cd", "rm", "cp", "mv", ...}

──────────────────────────── 导出函数 ────────────────────────────

NewExternalDirectoryChecker  创建外部目录检测器
CheckExternalPaths           检查工具调用是否访问工作空间外路径

──────────────────────────── 非导出函数 ────────────────────────────

extractPathsFromCommand      从 Shell 命令提取路径
looksLikePath                判断 token 是否看起来像路径
```

**CheckExternalPaths 逻辑**：

```
1. 如果无 workspaceRoot → 返回 nil
2. Shell 工具: extractPathsFromCommand 提取路径
3. 路径工具: iterPathStrings 提取路径
4. 过滤出工作空间外路径（使用 contains_path）
5. 如果有外部路径: 检查 external_directory 配置
   - 所有路径被 allow 覆盖 → PermissionLevelAllow
   - 否则根据 * 默认 → DENY/ASK
6. 返回 PermissionResult 或 nil
```

### 3.8 security/patterns.go

```
──────────────────────────── 结构体 ────────────────────────────

PatternMatcher               通用模式匹配器
PathMatcher                  路径匹配器
URLMatcher                   URL 匹配器
CommandMatcher               命令匹配器

──────────────────────────── 常量 ────────────────────────────

shellApprovalTools           {"bash", "mcp_exec_command", "create_terminal"}
pathApprovalTools            {"read_file", "write_file", ...}
pathApprovalKeys             ("path", "file_path", ...)
wildcardChars                安全字符类 [-a-zA-Z0-9 \\._/:"\']（防注入）

──────────────────────────── 导出函数 ────────────────────────────

MatchWildcard                通配符匹配（* → 安全字符类，防 shell 注入）
BuildCommandAllowPattern     构建命令允许模式
ContainsPath                 路径包含检查（防路径遍历）
WritePermissionsSectionToAgentConfigYAML  写入权限到 YAML
MergePermissionAllowRuleIntoPermissions   合并"始终允许"规则到权限
MergeExternalDirectoryAllowIntoPermissions 合并外部目录允许到权限
PersistCliTrustedDirectory   CLI 信任目录持久化

──────────────────────────── 非导出函数 ────────────────────────────

resolveAgentConfigYAMLPath   解析 YAML 路径
loadAgentConfigRoot          加载 YAML 根
saveAgentConfigRoot          保存 YAML 根
ensureSingleAllowOverride    确保单一审批覆盖
isSameAllowOverride          判断是否相同审批覆盖
buildApprovalOverrideID      构建审批覆盖 ID
persistTieredToolAllow       持久化工具允许
persistTieredApprovalOverrideSuggestions  持久化审批覆盖建议
```

**MatchWildcard 安全设计**：

```
* → [-a-zA-Z0-9 \\._/:"\']*  （限制字符集，防止 shell 元字符注入）
? → [-a-zA-Z0-9 \\._/:"\']   （单字符通配）
尾部 " *" → ( [-a-zA-Z0-9 \\._/:"\']*)?  （允许无参数匹配，如 "ls *" 匹配 "ls"）
使用 regexp.FullMatchString 全串锚定
```

### 3.9 security/suggestions.go

```
──────────────────────────── 结构体 ────────────────────────────

PermissionSuggestion         权限建议
  Tools                       []string
  MatchType                   string   ("command" / "path")
  Pattern                     string
  Action                      string   (默认 "allow")
  Scope                       string   (默认 "exact")
  Reason                      string

──────────────────────────── 常量 ────────────────────────────

shellSuggestionTools         {"bash", "mcp_exec_command", "create_terminal"}
pathSuggestionTools          {"read_file", "write_file", ...}
pathSuggestionKeys           ("path", "file_path", ...)

──────────────────────────── 导出函数 ────────────────────────────

BuildPermissionSuggestions   构建权限建议
BuildShellPermissionSuggestions 构建 Shell 命令权限建议

──────────────────────────── 非导出函数 ────────────────────────────

buildSingleShellSuggestion   构建单条 Shell 建议
extractPrefixBeforeHeredoc   提取 heredoc 前缀
extractSimpleCommandPrefix   提取简单命令前缀
buildPrefixPattern           构建前缀模式
buildPathPermissionSuggestion 构建路径权限建议
valueLooksLikePath           判断值是否看起来像路径
dedupeSuggestions            去重建议
```

### 3.10 security/models.go（补充）

在已有类型（PermissionLevel/PermissionResult/PermissionConfirmResponse/ApprovalOverrideEntry/PermissionsSection）基础上补充：

```
──────────────────────────── 补充结构体 ────────────────────────────

PermissionSceneHookInput     场景钩子输入
  ToolName                    string
  ToolArgs                    map[string]any
  NormalizedName              string

PermissionConfirmationRequest 托管确认请求
  ToolName                    string
  ToolArgs                    map[string]any
  Result                      *PermissionResult
  Suggestions                 []PermissionSuggestion

ToolPermissionHost           宿主注入钩子
  GetPermissionsSnapshot     func() map[string]any
  PersistAllowRule           func(rule map[string]any) bool
  ResolveWorkspaceDir        func() string
  PermissionYAMLPath         string
  ToolPermissionChecksActive func() bool
  RequestPermissionConfirmation PermissionConfirmationHook
  PermissionSceneHook         PermissionSceneHookFn

──────────────────────────── 补充函数类型 ────────────────────────────

PermissionSceneHookFn        func(ctx context.Context, input PermissionSceneHookInput) ([]string, error)
  // 返回 (approved_tool_names, error)。approved_tool_names 非空→场景预批准短路，空→不短路走标准流程
PermissionConfirmationHook   func(ctx context.Context, req PermissionConfirmationRequest) (*PermissionConfirmResponse, string, error)
  // 返回 (response, action, error)。action="interrupt"→走标准中断流程，action=""→按 response.approved 处理
```

### 3.11 security/factory.go

```
──────────────────────────── 导出函数 ────────────────────────────

BuildPermissionInterruptRail 构建权限中断 Rail
  参数: permissions PermissionsSection, engine *PermissionEngine, host *ToolPermissionHost, workspaceRoot string
  返回: *PermissionInterruptRail 或 nil（当 permissions.enabled=false 时）
```

## 4. 实现步骤（依赖顺序）

| Step | 文件 | 内容 | 依赖 | 预估行数 |
|------|------|------|------|---------|
| 1 | `rails/security/base_security_rail.go` | BaseSecurityRail + 4种决策类型 + runAndApply + applySecurityDecision + handleInterruptResume | DeepAgentRail, 已有 interrupt 机制 | ~500 |
| 2 | `rails/security/prompt_security_rail.go` | SafetyPromptRail（别名 SecurityRail） | Step 1, BuildSafetySection | ~80 |
| 3 | `security/shell_ast.go` | tree-sitter bash 解析 + 保守扫描 fallback | go-tree-sitter, tree-sitter-bash | ~400 |
| 4 | `security/tiered_policy.go` | 分层策略评估 | Step 3, resources.ParseBuiltinRules | ~800 |
| 5 | `security/patterns.go` | 通配符/路径/URL/Command 匹配 + YAML 持久化 | security/models, tiered_policy | ~700 |
| 6 | `security/checker.go` + `security/suggestions.go` | ExternalDirectoryChecker + "始终允许"建议 | security/models, patterns | ~200+200 |
| 7 | `security/models.go` 补充 + `security/permission_engine.go` | ToolPermissionHost + PermissionEngine 编排层 | Steps 4-6 | ~200 |
| 8 | `rails/security/tool_security_rail.go` | PermissionInterruptRail（重写 ResolveInterruptFn） | Steps 1+7, ConfirmInterruptRail | ~450 |
| 9 | `security/factory.go` + 回填 | BuildPermissionInterruptRail + factory.go/deep_agent.go 占位替换 | Step 8 | ~60 |

## 5. 回填清单

| 文件 | 行号 | 当前占位 | 回填内容 |
|------|------|---------|---------|
| `factory.go` | ~551-555 | `NewBaseRail()` + `⤵️ 9.8-9.24 回填` | `NewSafetyPromptRail()` |
| `factory.go` | ~551 | `alreadyProvidedByType(userProvidedTypes, nil)` | `alreadyProvidedByType(userProvidedTypes, reflect.TypeOf(&security2.SafetyPromptRail{}))` |
| `deep_agent.go` | ~1456-1459 | Debug 日志 `⤵️ 9.11 回填` | `BuildPermissionInterruptRail(...)` → `d.pendingRails` |
| `IMPLEMENTATION_PLAN.md` | 566 | `Security(☐)` | → `Security(✅)` |

## 6. 新增依赖

```
github.com/tree-sitter/go-tree-sitter v0.25.0      ← CGO（项目已有 CGO_ENABLED=1）
github.com/tree-sitter/tree-sitter-bash v0.25.1    ← bash grammar
```

已验证：编译通过，运行正常，精确解析管道/heredoc/命令替换/复合命令/重定向。

## 7. 测试策略

| 组件 | 测试方式 | 覆盖重点 |
|------|---------|---------|
| BaseSecurityRail | mock AgentCallbackContext | 4种 Decision 的 apply 行为、runAndApply 调度、handleInterruptResume auto_confirm 流程 |
| SafetyPromptRail | mock SystemPromptBuilder | init/uninit + prompt 注入 + 语言切换 |
| ShellAST | 直接调用 | tree-sitter 解析：管道/命令替换/heredoc/复合命令/重定向；fallback：风险结构检测 |
| TieredPolicy | 直接调用 | 优先级链每层短路、shell AST floor、severity→decision 映射、approval_overrides 豁免 |
| Patterns | 直接调用 | 通配符匹配边界（注入字符/路径遍历）、YAML 读写往返 |
| Checker | 直接调用 | 工作空间外路径检测、Shell 路径提取、external_directory 配置 |
| Suggestions | 直接调用 | Shell 命令建议、路径建议、heredoc 前缀提取 |
| PermissionEngine | mock TieredPolicy + Checker | enabled/active 标志、组合评估、严格取值 |
| PermissionInterruptRail | mock PermissionEngine + Host | resolve_interrupt 全分支（scene_hook 短路、ALLOW/DENY/ASK、auto_confirm、托管确认、中断恢复） |
| Factory | 直接调用 | enabled=false 返回 nil |

所有测试均为默认单元测试，无需 build tag 隔离。目标覆盖率 ≥ 85%。

## 8. 已有基础（无需重新实现）

| 已有组件 | 文件 | 说明 |
|---------|------|------|
| PermissionLevel / PermissionResult / PermissionConfirmResponse / ApprovalOverrideEntry / PermissionsSection | `security/models.go` | 数据模型就绪 |
| SecurityRule / BuiltinRules / ParseBuiltinRules | `resources/resources.go` | 内置规则解析就绪，builtin_rules.yaml 与 Python 一致 |
| BuildSafetySection | `prompts/sections/safety.go` | 双语安全提示词就绪 |
| ConfirmInterruptRail / BaseInterruptRail | `rails/interrupt/` | 中断恢复机制完整 |
| skipTool / forceFinish / auto_confirm session state | `ability_manager.go` / `interrupt/handler.go` | 底层机制就绪 |
| shlexSplit | `tools/shell/rm_tracker.go` | 基础分词可用（shell_ast.go 会扩展） |
