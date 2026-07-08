# 9.15 HeartbeatRail 设计文档

## 1. 概述

实现 9.15 HeartbeatRail，即心跳护栏 Rail。这是心跳检测场景的提示词守门员，负责：
1. 在心跳运行（`RunKind=HEARTBEAT`）时，读取工作区 `HEARTBEAT.md` 文件内容
2. 将内容注入到 LLM 系统提示词中（通过 `BuildHeartbeatSection` 构建节）
3. 非心跳运行时静默跳过，`HEARTBEAT.md` 为空时 LLM 输出 `HEARTBEAT_OK`

对齐 Python: `openjiuwen/harness/rails/heartbeat_rail.py` (67 行)

## 2. 在 Agent 会话流程中的位置

```
优先级排序（数值越小越先执行）：
  ProgressiveToolRail  →  priority=70  ✅ (9.11)
  HeartbeatRail        →  priority=80  ☐ (9.15) ← 本实现
  TaskCompletionRail   →  priority=80  ✅ (9.12)
  AgentModeRail        →  priority=85  ✅ (9.14)
  TaskPlanningRail     →  priority=90  ✅ (9.13)
```

HeartbeatRail 与 TaskCompletionRail 优先级相同（80），执行顺序取决于注册顺序。

HeartbeatRail 的 BeforeModelCall 在心跳运行时注入心跳提示词节，使 LLM 根据
`<heartbeat_user_task>` 标签内容是否为空决定输出 `HEARTBEAT_OK`（存活确认）
还是执行具体任务指令。

## 3. 前置依赖

| 依赖项 | 状态 | 说明 |
|--------|------|------|
| DeepAgentRail 基类 | ✅ | `rails/base.go` 提供 SysOperation/Workspace |
| BuildHeartbeatSection() | ✅ | `prompts/sections/heartbeat.go` 构建心跳提示词节 |
| cleanHeartbeatContent() | ✅ | 同上，清理 HEARTBEAT.md 内容 |
| RunKindHeartbeat | ✅ | `interfaces/callback.go` 心跳运行模式常量 |
| InvokeInputs.IsHeartbeat() | ✅ | 同上，心跳判断方法 |
| WorkspaceNodeHEARTBEATMD | ✅ | `workspace/workspace.go` 路径常量 |
| SystemPromptBuilderInterface | ✅ | `prompts/builder.go` AddSection/RemoveSection/Language |
| SysOperation.Fs().ReadFile() | ✅ | `sys_operation/sys_operation.go` 接口已定义 |
| DeepAdapter.heartbeatRail | ⤵️ | 占位 `interface{}`，本步骤回填 |

## 4. 实现范围

### 4.1 HeartbeatRail 主体

| 文件 | 职责 | 对齐 Python |
|------|------|-------------|
| `rails/heartbeat.go` | HeartbeatRail 结构体 + Init/Uninit/BeforeModelCall | `heartbeat_rail.py` |
| `rails/heartbeat_test.go` | 单元测试 | — |

### 4.2 DeepAdapter 回填

| 文件 | 修改 | 说明 |
|------|------|------|
| `deep_adapter.go` | `heartbeatRail interface{}` → `heartbeatRail *rails.HeartbeatRail` | 回填具体类型 |
| `deep_adapter.go` | 更新注释 `⤵️ 10.6.3-10` → `⤴️ 9.15` | 标记回填来源 |

### 4.3 IMPLEMENTATION_PLAN.md 状态更新

9.15 状态 ☐ → ✅

## 5. 详细设计

### 5.1 HeartbeatRail 结构体

```go
// HeartbeatRail 心跳护栏，在心跳运行时注入 HEARTBEAT.md 内容到系统提示词。
//
// 对齐 Python: openjiuwen/harness/rails/heartbeat_rail.py HeartbeatRail
type HeartbeatRail struct {
    DeepAgentRail
    // systemPromptBuilder 系统提示词构建器
    systemPromptBuilder saprompt.SystemPromptBuilderInterface
    // heartbeatDir HEARTBEAT.md 文件路径
    heartbeatDir string
}
```

### 5.2 构造函数

```go
// NewHeartbeatRail 创建心跳护栏实例。
func NewHeartbeatRail() *HeartbeatRail {
    r := &HeartbeatRail{}
    r.DeepAgentRail = *NewDeepAgentRail()
    r.SetPriority(heartbeatRailPriority) // 80
    return r
}
```

### 5.3 Init 钩子

对齐 Python `HeartbeatRail.init()`:

1. 获取 `systemPromptBuilder`：通过类型断言 `agent.(hinterfaces.DeepAgentInterface)` 获取 `SystemPromptBuilder()`
2. 获取 `sysOperation` 和 `workspace`：通过基类 getter `SysOperation()` / `Workspace()`，若为 nil 则从 `deepConfig` 回填（防御性兜底）
3. 计算 `heartbeatDir`：`workspace.GetNodePath(workspace.WorkspaceNodeHEARTBEATMD)`

### 5.4 Uninit 钩子

对齐 Python `HeartbeatRail.uninit()`:

- 若 `systemPromptBuilder` 非 nil，调用 `RemoveSection("heartbeat")`

### 5.5 BeforeModelCall 钩子

对齐 Python `HeartbeatRail.before_model_call()`:

```
1. 若 systemPromptBuilder 为 nil → 返回 nil
2. 若 cbc.Extra()["run_kind"] != "heartbeat" → 返回 nil（非心跳运行）
3. 若 SysOperation() 为 nil → 日志警告，返回 nil
4. 调用 SysOperation().Fs().ReadFile(ctx, heartbeatDir) 读取 HEARTBEAT.md
5. 若 err != nil → 日志警告，content = ""
6. 调用 sections.BuildHeartbeatSection(content, systemPromptBuilder.Language())
7. 若 section 非 nil → systemPromptBuilder.AddSection(section)
8. 若 section 为 nil → systemPromptBuilder.RemoveSection("heartbeat")
```

### 5.6 Python → Go 对照表

| Python | Go |
|--------|-----|
| `priority = 80` | `heartbeatRailPriority = 80` |
| `self.system_prompt_builder = None` | `systemPromptBuilder saprompt.SystemPromptBuilderInterface` |
| `self.heartbeat_dir = None` | `heartbeatDir string` |
| `ctx.extra.get("run_kind") != RunKind.HEARTBEAT` | `cbc.Extra()["run_kind"] != string(interfaces.RunKindHeartbeat)` |
| `self.sys_operation.fs().read_file(heartbeat_dir, mode="text")` | `r.SysOperation().Fs().ReadFile(ctx, heartbeatDir)` |
| `read_res.code == 0` | `err == nil` |
| `build_heartbeat_section(language, content)` | `sections.BuildHeartbeatSection(content, language)` |
| `self.system_prompt_builder.add_section(section)` | `r.systemPromptBuilder.AddSection(section)` |
| `self.system_prompt_builder.remove_section("heartbeat")` | `r.systemPromptBuilder.RemoveSection("heartbeat")` |

## 6. 日志同步

对齐 Python logger 调用：

| Python | Go | 位置 |
|--------|-----|------|
| `logger.info("[HeartbeatRail] No deep_config configured")` | `logger.Info(logComponent).Str("event_type", "heartbeat_no_deep_config").Msg("deepConfig 未配置")` | Init |
| `logger.warning("HeartbeatRail: sys_operation not configured")` | `logger.Warn(logComponent).Str("event_type", "heartbeat_no_sys_operation").Msg("sysOperation 未配置")` | BeforeModelCall |
| `logger.warning("HeartbeatRail: failed to read HEARTBEAT.md")` | `logger.Warn(logComponent).Str("event_type", "heartbeat_read_failed").Str("path", heartbeatDir).Err(err).Msg("读取 HEARTBEAT.md 失败")` | BeforeModelCall |

## 7. 测试策略

### 7.1 单元测试覆盖

| 测试函数 | 场景 |
|---------|------|
| `TestNewHeartbeatRail` | 构造函数，验证优先级和初始状态 |
| `TestHeartbeatRail_Init` | Init 正常初始化 |
| `TestHeartbeatRail_Init_无DeepConfig` | deepConfig 为 nil 时日志记录 |
| `TestHeartbeatRail_Uninit` | Uninit 移除 heartbeat 节 |
| `TestHeartbeatRail_Uninit_无Builder` | systemPromptBuilder 为 nil 时不崩溃 |
| `TestHeartbeatRail_BeforeModelCall_非心跳运行` | run_kind 非 heartbeat 时跳过 |
| `TestHeartbeatRail_BeforeModelCall_心跳运行_注入节` | 正常心跳运行，HEARTBEAT.md 有内容 |
| `TestHeartbeatRail_BeforeModelCall_心跳运行_空内容` | HEARTBEAT.md 为空时移除节 |
| `TestHeartbeatRail_BeforeModelCall_无SysOperation` | sysOperation 为 nil 时日志警告 |
| `TestHeartbeatRail_BeforeModelCall_读取失败` | ReadFile 返回错误时日志警告 |

### 7.2 Mock 策略

- `SystemPromptBuilderInterface`：mock 实现 AddSection/RemoveSection/Language
- `SysOperation` + `FsOperation`：mock 实现 ReadFile
- `BaseAgent`：mock 实现 DeepAgentInterface 类型断言
- `AgentCallbackContext`：构造时设置 Extra()["run_kind"]

## 8. 回填清单

| 位置 | 修改 | 标记 |
|------|------|------|
| `deep_adapter.go:118-120` | `heartbeatRail interface{}` → `heartbeatRail *rails.HeartbeatRail` | `⤵️ 10.6.3-10` → `⤴️ 9.15` |
| `IMPLEMENTATION_PLAN.md` | 9.15 状态 ☐ → ✅ | — |

## 9. 不在范围内

- HeartbeatRail 的创建和注册到 DeepAgent（由 swarm 层 10.6.3-10 处理）
- `HandleHeartbeat` 方法（已在 deep_adapter.go 中实现）
- 网关侧心跳调度服务（11.11）
- SysOperation 具体实现（9.32-9.35）
