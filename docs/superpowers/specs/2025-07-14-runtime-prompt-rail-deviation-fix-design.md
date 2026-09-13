# RuntimePromptRail 实现偏差修复设计

## 背景

RuntimePromptRail (10.6.7) 实现已提交，但审查发现 3 个与计划的偏差，其中 2 个需要代码修复。

## 偏差清单与决策

| # | 偏差 | 决策 | 理由 |
|---|------|------|------|
| 1 | ForceEnglish 调用链缺失 | CodeAdapter 自己实现 `updateRuntimeConfig` | 对齐 Python 完全覆写模式，和已有 `resolvePromptLanguage`/`buildConfiguredSubagents` 等覆写模式一致 |
| 2 | buildRuntimePromptRail 省略 resolvePromptChannel | 补上调用，严格对齐 Python | CreateInstance 时行为等价，但形式上 1:1 对齐 |
| 3 | Task 5-8 合并为 1 个 commit | 保持现状 | 中间状态无法编译，拆分引入 stub 无意义 |

## 修复 1：CodeAdapter 实现 updateRuntimeConfig

### Python 对齐

Python CodeAdapter 的 `_update_runtime_config` **完全覆写** DeepAdapter 的方法（不调 `super()`），差异点：

1. `set_force_english(self._force_english_runtime_prompt)` — Code 模式强制英文
2. `_write_runtime_state` 用 `self._resolve_output_language()` 而非 `resolved_language`
3. 多了 ProjectMemoryRail 语言同步 + trusted_dirs 注入
4. 多了 `_update_tools_for_mode` / `_update_session_tools`

### Go 实现

在 `code_adapter.go` 新增 `updateRuntimeConfig` 方法：

```go
func (c *CodeAdapter) updateRuntimeConfig(ctx context.Context, config *runtimeConfig) {
    if config == nil {
        return
    }

    // 步骤 1: CWD 种子
    if config.CWD != "" {
        c.deep.seedRuntimeCwd(ctx, config.CWD)
    }

    // 步骤 2: language — CodeAdapter 自己的 resolveRuntimeLanguage
    resolvedLanguage := c.resolveRuntimeLanguage()

    // 步骤 3: channel
    resolvedChannel := commrails.FirstNonEmpty(
        config.ChannelID,
        resolvePromptChannel(config.SessionID),
        "web",
    )

    // 步骤 4: 写 YAML — Python CodeAdapter 用 _resolve_output_language()
    outputLanguage := c.resolveOutputLanguage()
    c.deep.writeRuntimeStateYAML(config.Mode, outputLanguage, resolvedChannel, config.ProjectDir)

    // 步骤 5: RuntimePromptRail setter — 关键差异行 SetForceEnglish
    if c.deep.runtimePromptRail != nil {
        c.deep.runtimePromptRail.SetLanguage(resolvedLanguage)
        c.deep.runtimePromptRail.SetForceEnglish(c.forceEnglishRuntimePrompt)
        c.deep.runtimePromptRail.SetChannel(resolvedChannel)
        c.deep.runtimePromptRail.SetTrustedDirs(config.TrustedDirs)
        c.deep.runtimePromptRail.SetRuntimePaths(config.CWD, config.ProjectDir)
        c.deep.runtimePromptRail.SetModelName(c.deep.resolveModelName())
        c.deep.runtimePromptRail.SetMode(config.Mode)
    }

    // 步骤 6-7: prompt + rail 模式切换
    c.deep.updatePromptForMode(config.Mode)
    c.deep.updateRailsForMode(config.Mode)

    // ⤵️ 待后续章节回填:
    // - ProjectMemoryRail 语言同步 + trusted_dirs
    // - _update_tools_for_mode
    // - _update_session_tools
}
```

### 为何不走 c.deep.updateRuntimeConfig

CodeAdapter 已有同包非导出方法覆写模式：
- `resolvePromptLanguage()` → 返回 `"en"`，不走 `c.deep`
- `resolveRuntimeLanguage()` → 自己实现，不走 `c.deep`
- `buildConfiguredSubagents()` → 自己实现，不走 `c.deep`

`updateRuntimeConfig` 遵循同样模式，不调 `c.deep.updateRuntimeConfig`，避免遗漏差异点（尤其是 `SetForceEnglish` 和 `resolveOutputLanguage`）。

## 修复 2：buildRuntimePromptRail 补 resolvePromptChannel

### 变更

```go
// Before:
defaultChannel := "web"
if d.isAcpToolProfile(d.instanceOverrides) {
    defaultChannel = "acp"
}

// After:
defaultChannel := "web"
if d.isAcpToolProfile(d.instanceOverrides) {
    defaultChannel = "acp"
} else {
    defaultChannel = resolvePromptChannel(d.sessionID)
}
```

### 行为等价性说明

CreateInstance 时 `d.sessionID` 尚未设置（空字符串），`resolvePromptChannel("")` 返回 `"web"`，与原来硬编码 `"web"` 等价。后续 per-request 的 `SetChannel` 会正确设置。

## 涉及文件

| 文件 | 操作 |
|------|------|
| `internal/swarm/server/adapter/code_adapter.go` | 新增 `updateRuntimeConfig` 方法 |
| `internal/swarm/server/adapter/deep_adapter_rails.go` | `buildRuntimePromptRail` 补 `else` 分支 |
| `internal/swarm/server/adapter/deep_adapter_config_test.go` | 补 CodeAdapter 的 updateRuntimeConfig 测试 |
