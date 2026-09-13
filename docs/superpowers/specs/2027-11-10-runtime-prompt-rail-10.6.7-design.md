# RuntimePromptRail (10.6.7) 实现设计

## 概述

实现 `RuntimePromptRail`，在每次 LLM 调用前动态注入运行时状态相关的 PromptSection（时间、模型/模式/语言、平台环境、git 状态、浏览器策略、工作目录策略），确保 LLM 无需工具调用即可感知当前运行时环境。

同时回填 adapter 层依赖 RuntimePromptRail 的 stub：`buildRuntimePromptRail()`、`updatePromptForMode()`、`updateRuntimeConfig()` 的 7 个 setter 调用。

**对齐 Python**：`jiuwenswarm/agents/harness/common/rails/runtime_prompt_rail.py`（385 行）

## 在 Agent 会话流程中的位置

```
用户请求 → DeepAdapter.ProcessMessageStreamImpl()
  → updateRuntimeConfig() 每次请求前更新 7 个 setter + 写 runtime_state.yaml
  → Runner.RunAgentStreaming()
    → ReActAgent.Invoke() ReAct 循环
      → BeforeModelCall ← 🎯 RuntimePromptRail（注入 7 个 PromptSection）
        ├─ time (priority 92)
        ├─ runtime (priority 95)
        ├─ language_output (priority 93)
        ├─ env (priority 89)
        ├─ git_status (priority 87) — 条件
        ├─ browser_tool_policy (priority 98) — 条件
        └─ trusted_dirs_policy (priority 90) — 条件
      → callModel()

模式切换 → DeepAdapter.SwitchMode()
  → updatePromptForMode(mode) — 同步 SystemPromptBuilder.language + DeepConfig.language
```

## 涉及文件

| 文件 | 变更类型 | 说明 |
|------|---------|------|
| `internal/swarm/agents/harness/common/rails/runtime_prompt_rail.go` | 新建 | RuntimePromptRail 实现 |
| `internal/swarm/agents/harness/common/rails/runtime_prompt_rail_test.go` | 新建 | 单元测试 |
| `internal/swarm/agents/harness/common/rails/doc.go` | 修改 | 文件目录加 runtime_prompt_rail.go |
| `internal/swarm/server/adapter/deep_adapter_config.go` | 修改 | runtimeConfig 扩展 + updateRuntimeConfig 补 setter + writeRuntimeState 改 YAML |
| `internal/swarm/server/adapter/deep_adapter_rails.go` | 修改 | buildRuntimePromptRail 回填 + updatePromptForMode 回填 |
| `internal/swarm/server/adapter/deep_adapter.go` | 修改 | runtimePromptRail 字段类型改为 *RuntimePromptRail |
| `internal/swarm/server/adapter/code_adapter.go` | 修改 | CodeAdapter 调用 SetForceEnglish |

## 一、RuntimePromptRail 结构体

```go
// RuntimePromptRail 运行时提示词护栏 — 在 before_model_call 中注入时间及运行时状态。
// Python: RuntimePromptRail(DeepAgentRail) — runtime_prompt_rail.py (385 行)
type RuntimePromptRail struct {
    rails.DeepAgentRail
    // per-request 状态（7 个 setter）
    language     string
    channel      string
    trustedDirs  []string
    cwd          string
    projectDir   string
    modelName    string
    mode         string
    forceEnglish bool
}
```

- 嵌入 `rails.DeepAgentRail`，编译时验证满足 `agentinterfaces.AgentRail`
- `priority = 5`（对齐 Python `RuntimePromptRail.priority = 5`）
- 不存储 `systemPromptBuilder` 引用，在 `BeforeModelCall` 中通过 `cbc.Agent().SystemPromptBuilder()` 获取（与 AvatarPromptRail 一致）

### 7 个 Setter 方法

| Go 方法 | Python 方法 | 说明 |
|---------|------------|------|
| `SetLanguage(lang string)` | `set_language(language)` | per-request 更新语言 |
| `SetChannel(ch string)` | `set_channel(channel)` | per-request 更新频道 |
| `SetTrustedDirs(dirs []string)` | `set_trusted_dirs(trusted_dirs)` | per-request 更新可信目录 |
| `SetRuntimePaths(cwd, projectDir string)` | `set_runtime_paths(cwd, project_dir)` | per-request 更新 CWD 和项目目录 |
| `SetModelName(name string)` | `set_model_name(model_name)` | per-request 更新模型名称（fallback） |
| `SetMode(mode string)` | `set_mode(mode)` | per-request 更新模式（fallback） |
| `SetForceEnglish(force bool)` | `set_force_english(force)` | 强制英文 section（code 模式） |

### GetCallbacks

注册 `CallbackBeforeModelCall`，与 AvatarPromptRail 同模式：

```go
func (r *RuntimePromptRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
    callbacks := r.DeepAgentRail.GetCallbacks()
    callbacks[agentinterfaces.CallbackBeforeModelCall] = func(ctx context.Context, railCtx any) error {
        return r.BeforeModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
    }
    return callbacks
}
```

## 二、BeforeModelCall 注入 7 个 PromptSection

完全对齐 Python `before_model_call()` 逻辑。

### 2.1 time (priority 92) — 始终注入

```go
// 中文（language=="cn" && !forceEnglish）
"# 时间说明\n\n- 当用户询问"最新、当前、今年、本年、实时、近期"等信息并需要搜索时，搜索 query 必须优先使用当前年份或日期"

// 英文
"# Time Description\n\n- When the user asks for latest/current/this-year/recent information and search is needed, search queries must prefer the current year or date."
```

### 2.2 runtime (priority 95) — 始终注入

- 读取 `workspace.ConfigDir()/runtime_state.yaml`
- `FileNotFoundError` → 空 map，不中断
- 其他错误 → warn 日志，空 map
- 字段 fallback 链：`runtime_state[key] → setter 值 → "unknown"`

```go
model := firstNonEmpty(runtimeState["model"], r.modelName, "unknown")
mode := firstNonEmpty(runtimeState["mode"], r.mode, "unknown")
languageVal := firstNonEmpty(runtimeState["language"], r.language, "unknown")
channel := firstNonEmpty(runtimeState["channel"], r.channel, "unknown")
```

中文内容：
```
# 运行时状态
- 当前模型：{model}
- 当前模式：{mode}
- 当前语言：{language_val}
- 当前渠道：{channel}
- 当用户询问「你是什么模型」「当前用的是哪个模型」等问题时，直接用上方「当前模型」的值回答，只说模型名称，不要介绍身份或列出能力
```

### 2.3 language_output (priority 93) — 始终注入

语言名映射（对齐 Python `_LANGUAGE_NAMES`）：

```go
var languageNames = map[string]string{"cn": "Chinese", "zh": "Chinese", "en": "English"}
```

内容（中英相同）：
```
# Language
Always respond in {language_name}. Use {language_name} for all explanations, comments, and communications with the user. Technical terms and code identifiers should remain in their original form.
```

### 2.4 env (priority 89) — 始终注入

- `runtime.GOOS` → 对齐 Python `sys.platform`
- `os.Getenv("SHELL")` → shell 名称
- `runtime.Version()` + `runtime.GOOS`/`runtime.GOARCH` → 对齐 Python `platform` 信息
- 跨平台命令差异表（Windows vs Linux/macOS），对齐 Python 完整模板

### 2.5 git_status (priority 87) — 条件注入

- 仅当 `runtime_state["git_branch"]` 存在且非 "N/A" 时注入
- 内容：current branch / main branch / git user / status / recent commits
- 语言：始终英文（对齐 Python，git_status section 无中文版本）

### 2.6 browser_tool_policy (priority 98) — 条件注入

- 仅当 `r.channel == "web"` 时注入
- 内容：使用 `spawn_sub_agent` + `browser_agent`，禁止 bash/execute_code 等方式
- 语言：始终英文（对齐 Python）

### 2.7 trusted_dirs_policy (priority 90) — 条件注入

- 仅当 `r.channel == "tui"` 且有有效目录时注入
- 使用 `existingDirs()` / `existingDir()` / `samePath()` 工具方法（对齐 Python 静态方法）
- 内容：系统目录 / 当前项目目录 / 其他可访问目录 + 重要规则
- 中英双语（根据 `language` + `forceEnglish` 判断）

## 三、runtimeConfig 扩展

对齐 Python `_RuntimeConfig`（`interface_deep.py` L3108-3119）：

```go
type runtimeConfig struct {
    // 已有字段
    CWD          string
    Language     string
    Channel      string
    ProjectDir   string
    WorkspaceDir string
    // 新增字段，对齐 Python _RuntimeConfig
    SessionID       string         // Python: session_id
    Mode            string         // Python: mode，默认 "agent.plan"
    RequestID       string         // Python: request_id
    ChannelID       string         // Python: channel_id
    RequestMetadata map[string]any // Python: request_metadata，⤵️ 具体类型待 10.4 定义
    TrustedDirs     []string       // Python: trusted_dirs
}
```

## 四、writeRuntimeState 改写 YAML

当前：写环境变量 `JCLAW_RUNTIME_XXX`
改为：写 `workspace.ConfigDir()/runtime_state.yaml`，对齐 Python `_write_runtime_state()`

### 4.1 新签名

```go
func (d *DeepAdapter) writeRuntimeStateYAML(mode, language, channel string, projectDir string)
```

### 4.2 YAML 字段

```yaml
model: <resolveModelName()>
mode: <mode_display>     # ⤵️ _MODE_DISPLAY_MAP 待 10.4 SlashCommand 定义
language: <language>
channel: <channel>
agent: <d.agentName>
platform: <runtime.GOOS runtime.GOARCH>
go_version: <runtime.Version()>  # 对齐 Python 的 python_version
git_branch: <git rev-parse --abbrev-ref HEAD> 或 "N/A"
git_main_branch: <git rev-parse --verify origin/main|main|master> 或 ""
git_status: <git status --short> 最多 50 行
git_recent_commits: <git log --oneline -5>
git_user: <git config user.name>
```

### 4.3 Git 信息获取

- `exec.LookPath("git")` 检测 git 可用
- 在 `projectDir` 下执行 git 命令
- 每个命令 5 秒超时（`exec.CommandContext`）
- 全部失败不中断，git_branch 置 "N/A"

### 4.4 移除旧的环境变量写入

`writeRuntimeState(key, value)` 方法删除，替换为 `writeRuntimeStateYAML()`。所有调用点更新。

## 五、回填链路

### 5.1 buildRuntimePromptRail()

```go
// 之前：返回 nil
// 之后：
func (d *DeepAdapter) buildRuntimePromptRail() *rails.RuntimePromptRail {
    defaultChannel := "web"
    if d.isAcpToolProfile(d.instanceOverrides) {
        defaultChannel = "acp"
    } else {
        defaultChannel = resolvePromptChannel(d.sessionID) // ⤵️ 需确认 sessionID 来源
    }
    rail := rails.NewRuntimePromptRail(
        rails.WithLanguage(d.resolveRuntimeLanguage()),
        rails.WithChannel(defaultChannel),
    )
    logger.Info(logComponent).Msg("RuntimePromptRail 创建成功")
    return rail
}
```

### 5.2 updateRuntimeConfig() 补 7 个 setter

```go
func (d *DeepAdapter) updateRuntimeConfig(ctx context.Context, config *runtimeConfig) {
    // ... 现有 CWD/language/channel/project_dir/workspace_dir 步骤保留但改用 YAML

    // 步骤 6: 写 runtime_state.yaml（替代旧的环境变量写入）
    d.writeRuntimeStateYAML(
        config.Mode,
        config.Language,
        config.Channel,
        config.ProjectDir,
    )

    // 步骤 7: RuntimePromptRail 7 个 setter
    if d.runtimePromptRail != nil {
        d.runtimePromptRail.SetLanguage(d.resolveRuntimeLanguage())
        resolvedChannel := firstNonEmpty(config.ChannelID, resolvePromptChannel(config.SessionID), "web")
        d.runtimePromptRail.SetChannel(resolvedChannel)
        d.runtimePromptRail.SetTrustedDirs(config.TrustedDirs)
        d.runtimePromptRail.SetRuntimePaths(config.CWD, config.ProjectDir)
        d.runtimePromptRail.SetModelName(d.resolveModelName())
        d.runtimePromptRail.SetMode(config.Mode)
    }

    // 步骤 8: updatePromptForMode
    d.updatePromptForMode(config.Mode)

    // 步骤 9: rail/tool 模式切换（仅 evolution 分支已实现）
    d.updateRailsForMode(config.Mode)
}
```

### 5.3 updatePromptForMode()

```go
func (d *DeepAdapter) updatePromptForMode(mode string) {
    if d.instance == nil {
        return
    }
    resolvedLanguage := d.resolveRuntimeLanguage()
    builder := d.instance.SystemPromptBuilder()
    if builder != nil {
        builder.SetLanguage(resolvedLanguage)  // 需扩展 SystemPromptBuilderInterface 加入 SetLanguage
    }
    // 同步 DeepConfig.Language（直接赋值导出字段）
    if d.instance.DeepConfig() != nil {
        d.instance.DeepConfig().Language = resolvedLanguage
    }
    logger.Info(logComponent).Str("mode", mode).Str("language", resolvedLanguage).Msg("updatePromptForMode 执行完成")
}
```

### 5.4 DeepAdapter 字段类型变更

```go
// 之前
runtimePromptRail sainterfaces.AgentRail

// 之后
runtimePromptRail *rails.RuntimePromptRail
```

### 5.5 CodeAdapter 调用 SetForceEnglish

CodeAdapter 通过 `c.deep` 访问 DeepAdapter 的 `runtimePromptRail`。在 CodeAdapter 的请求处理流程中追加：
```go
if c.deep.runtimePromptRail != nil {
    c.deep.runtimePromptRail.SetForceEnglish(c.forceEnglishRuntimePrompt)
}
```

## 六、工具方法

对齐 Python 3 个静态方法：

```go
// existingDirs 规范化并过滤存在的目录，保留顺序，去重。
// Python: RuntimePromptRail._existing_dirs(paths)
func existingDirs(paths []string) []string

// existingDir 返回规范化后的存在目录路径，不存在返回空字符串。
// Python: RuntimePromptRail._existing_dir(path)
func existingDir(path string) string

// samePath 大小写不敏感的路径相等比较。
// Python: RuntimePromptRail._same_path(left, right)
func samePath(left, right string) bool
```

## 七、测试覆盖

### 单元测试（`runtime_prompt_rail_test.go`）

1. `TestNewRuntimePromptRail` — 构造 + 默认值
2. `TestSetLanguage` / `TestSetChannel` / `TestSetTrustedDirs` / `TestSetRuntimePaths` / `TestSetModelName` / `TestSetMode` / `TestSetForceEnglish` — 7 个 setter
3. `TestBeforeModelCall_所有Section` — mock SystemPromptBuilder，验证 7 个 section 全部注入
4. `TestBeforeModelCall_仅TimeRuntimeLanguageEnv` — forceEnglish=true 时只注入 4 个始终 section
5. `TestBeforeModelCall_GitStatus条件` — 无 git_branch 时不注入
6. `TestBeforeModelCall_BrowserPolicy条件` — channel!="web" 时不注入
7. `TestBeforeModelCall_TrustedDirsPolicy条件` — channel!="tui" 或无有效目录时不注入
8. `TestExistingDirs` — 去重 + 排序保留 + 跳过不存在
9. `TestExistingDir` — 存在/不存在
10. `TestSamePath` — 大小写/路径分隔符

### adapter 层测试

- `TestBuildRuntimePromptRail` — 验证返回非 nil
- `TestUpdatePromptForMode` — 验证 SystemPromptBuilder.language 被同步
- `TestWriteRuntimeStateYAML` — 验证 YAML 文件生成（用 t.TempDir()）

## 八、依赖与风险

### 依赖

- `workspace.ConfigDir()` — 已实现，返回配置目录
- `saprompt.SystemPromptBuilderInterface` — 已有 `AddSection`/`RemoveSection`/`Language`，但缺少 `SetLanguage`，需扩展接口
- `rails.DeepAgentRail` — 已有基类
- `yaml` 库 — 需 `gopkg.in/yaml.v3`（已在项目依赖中）
- `hschema.DeepAgentConfig.Language` — 已有 string 字段，可直接赋值

### 风险

1. **SystemPromptBuilderInterface 缺少 SetLanguage**：`SetLanguage` 方法存在于具体类型 `SystemPromptBuilder` 上，但不在 `SystemPromptBuilderInterface` 接口中。需扩展接口加入 `SetLanguage(lang string)`，这是最小侵入方案，且 `*SystemPromptBuilder` 已隐式满足。
2. **DeepConfig().SetLanguage()**：`DeepAgentConfig` 是 struct，`Language` 是导出字段，直接赋值 `d.instance.DeepConfig().Language = resolvedLanguage` 即可，无需 setter 方法。
3. **resolvePromptChannel 获取 sessionID**：当前 Go 端是包级函数 `resolvePromptChannel(sessionID)`，`buildRuntimePromptRail` 构造时需要 sessionID。方案：`buildRuntimePromptRail` 改为接收 `sessionID string` 参数，从 DeepAdapter 的调用上下文传入（runtimeConfig.SessionID 或 CreateInstance 时的 sessionID）。
