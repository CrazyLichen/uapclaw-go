# SkillToolkit 回填设计文档

## 概述

回填 SkillToolkit 的两个缺失点：Code 模式注册和 SkillNet 安装轮询。Team 模式 `MemberSkillToolkitRail` 留到 9.68 统一实现。

## 背景

SkillToolkit 核心逻辑（`search_skill`/`install_skill`/`uninstall_skill` 三个工具 + 辅助方法）已在 `internal/swarm/agents/harness/tools/skill_toolkit.go` 中完整实现，并在 Deep 模式 `deep_adapter_tools.go` 步骤 9 中注册。但存在两个未回填点：

1. **Code 模式**：`code_adapter.go` 的 `case "skill_toolkit"` 标记为 `⤵️ 10.6.3-10`，直接跳过
2. **SkillNet 安装轮询**：Python 的 `_install_skillnet_sync_wait` 在 Go 中未实现，当前 skillnet 分支错误地调用了 `HandleSkillsInstall`（应调用 `HandleSkillsSkillnetInstall` + 轮询）

## 改动清单

### 1. Code 模式注册（`code_adapter.go`）

**位置**：`internal/swarm/server/adapter/code_adapter.go` 第 622-624 行

**改动**：替换 `⤵️ 10.6.3-10` 占位，实现与 Deep 模式步骤 9 一致的注册逻辑。

```go
case "skill_toolkit":
    if c.deep.skillManager != nil {
        skillToolkit := skilltools.NewSkillToolkit(c.deep.skillManager)
        for _, t := range skillToolkit.GetTools() {
            existing, _ := runner.GetResourceMgr().GetTool([]string{t.Card().ID})
            if len(existing) == 0 {
                if err := runner.GetResourceMgr().AddTool(t); err != nil {
                    logger.Warn(logComponent).Err(err).Str("tool", "skill_toolkit").Msg("注册工具到 ResourceMgr 失败")
                }
            }
            toolCards = append(toolCards, t.Card())
        }
        logger.Info(logComponent).Msg("CodeAdapter: SkillToolkit 已注册")
    }
```

**需要新增 import**：`skilltools "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/tools"`

**对齐 Python**：`JiuwenClawCodeAdapter._build_skill_toolkit` + `build_code_tool_cards` 统一注册

### 2. SkillNet 安装轮询（`skill_toolkit.go`）

**位置**：`internal/swarm/agents/harness/tools/skill_toolkit.go`

#### 2.1 新增 `installSkillnetSyncWait` 方法

对齐 Python 的 `_install_skillnet_sync_wait`，使用 `context.WithTimeout` + `time.Ticker` 替代 Python 的 `asyncio.wait_for` + `asyncio.sleep(0.5)`。

```go
// installSkillnetSyncWait 在单次 tool 调用内轮询 SkillNet 安装状态，直到完成或超时。
// 对应 Python: SkillToolkit._install_skillnet_sync_wait(identifier, timeout_sec)
func (tk *SkillToolkit) installSkillnetSyncWait(ctx context.Context, identifier string, timeoutSec int) map[string]any {
    // 1. 发起安装
    payload, _ := tk.manager.HandleSkillsSkillnetInstall(ctx, map[string]any{"url": identifier, "force": false})
    if !toBool(payload["success"]) {
        return payload
    }
    if !toBool(payload["pending"]) {
        return payload
    }

    // 2. 提取 install_id
    installID := strings.TrimSpace(toString(payload["install_id"]))
    if installID == "" {
        return map[string]any{"success": false, "detail": "missing install_id from skillnet install"}
    }

    // 3. 轮询安装状态
    pollCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
    defer cancel()

    ticker := time.NewTicker(500 * time.Millisecond)
    defer ticker.Stop()

    for {
        select {
        case <-pollCtx.Done():
            return map[string]any{
                "success": false,
                "detail":  fmt.Sprintf("skill installation timed out after %d seconds", timeoutSec),
            }
        case <-ticker.C:
            statusPayload, _ := tk.manager.HandleSkillsSkillnetInstallStatus(
                pollCtx, map[string]any{"install_id": installID},
            )
            if toString(statusPayload["status"]) != "pending" {
                if !toBool(statusPayload["success"]) {
                    return statusPayload
                }
                return map[string]any{"success": true, "skill": statusPayload["skill"]}
            }
        }
    }
}
```

#### 2.2 修改 `InstallSkill` 的 skillnet 分支

**当前代码**（第 329-333 行）：
```go
case "skillnet":
    // SkillNet 暂时 stub，直接调用安装方法
    payload, _ = tk.manager.HandleSkillsInstall(ctx, map[string]any{
        "url": identifier, "force": false, "source": "skillnet",
    })
```

**改为**：
```go
case "skillnet":
    payload = tk.installSkillnetSyncWait(ctx, identifier, timeoutSec)
```

**原因**：
- 当前代码调用 `HandleSkillsInstall`（接收 `spec` 参数），但 Python 调用的是 `HandleSkillsSkillnetInstall`（接收 `url` 参数），这是两个不同的方法
- `HandleSkillsSkillnetInstall` 返回 `{"success": true, "pending": true, "install_id": "..."}`，需要轮询状态
- `installSkillnetSyncWait` 封装了完整的「发起安装 → 轮询状态 → 返回结果」流程

### 3. 标记更新

| 文件 | 改动 |
|------|------|
| `code_adapter.go` 第 622-624 行 | 移除 `⤵️ 10.6.3-10` 注释，替换为注册逻辑 |
| `IMPLEMENTATION_PLAN.md` 9.50 | 状态从 🔄 改为 ✅ |

### 4. 测试

需要新增以下测试：

1. **`TestInstallSkill_SkillNet_Pending`**：模拟 SkillNet 安装返回 pending → 轮询到 done 的完整流程
2. **`TestInstallSkill_SkillNet_Timeout`**：模拟 SkillNet 安装超时
3. **`TestInstallSkillnetSyncWait_NotPending`**：安装直接返回结果（非 pending）
4. **`TestInstallSkillnetSyncWait_InstallFailed`**：安装请求本身失败

## 不做的事

- Team 模式 `MemberSkillToolkitRail` — 留到 9.68 统一实现
- 不重构 switch-case 为映射表模式
- 不修改 Deep 模式步骤 9 的已实现逻辑
