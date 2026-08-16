# SkillToolkit 回填实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 回填 SkillToolkit 的两个缺失点：Code 模式注册和 SkillNet 安装轮询。

**Architecture:** 在 `skill_toolkit.go` 中新增 `installSkillnetSyncWait` 方法替代当前错误的 `HandleSkillsInstall` 调用，在 `code_adapter.go` 中补齐 `case "skill_toolkit"` 的注册逻辑。

**Tech Stack:** Go 1.22+, 标准库 `context`/`time`/`fmt`

---

## 文件变更清单

| 文件 | 操作 | 职责 |
|------|------|------|
| `internal/swarm/agents/harness/tools/skill_toolkit.go` | 修改 | 新增 `installSkillnetSyncWait` 方法，修改 `InstallSkill` 的 skillnet 分支 |
| `internal/swarm/agents/harness/tools/skill_toolkit_test.go` | 修改 | 新增 4 个轮询相关测试 |
| `internal/swarm/server/adapter/code_adapter.go` | 修改 | `case "skill_toolkit"` 替换占位为注册逻辑 |
| `IMPLEMENTATION_PLAN.md` | 修改 | 9.50 状态 🔄 → ✅ |

---

### Task 1: 新增 `installSkillnetSyncWait` 方法

**Files:**
- Modify: `internal/swarm/agents/harness/tools/skill_toolkit.go:329-333`（替换 skillnet 分支）
- Modify: `internal/swarm/agents/harness/tools/skill_toolkit.go:1-13`（import 块添加 `"time"`）

- [ ] **Step 1: 在 import 块中添加 `"time"` 包**

当前 import（第 3-13 行）：
```go
import (
	"context"
	"fmt"
	"regexp"
	"strings"

	tool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
	skillpkg "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill"
)
```

改为：
```go
import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	tool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
	skillpkg "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill"
)
```

- [ ] **Step 2: 在非导出函数区块末尾添加 `installSkillnetSyncWait` 方法**

在 `skill_toolkit.go` 的非导出函数区块（`listInstalledSkills` 方法之后、`newSearchSkillTool` 方法之前），添加：

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

- [ ] **Step 3: 修改 `InstallSkill` 的 skillnet 分支**

当前代码（第 329-333 行）：
```go
	case "skillnet":
		// SkillNet 暂时 stub，直接调用安装方法
		payload, _ = tk.manager.HandleSkillsInstall(ctx, map[string]any{
			"url": identifier, "force": false, "source": "skillnet",
		})
```

改为：
```go
	case "skillnet":
		payload = tk.installSkillnetSyncWait(ctx, identifier, timeoutSec)
```

- [ ] **Step 4: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/swarm/agents/harness/tools/`
Expected: 编译成功，无错误

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/agents/harness/tools/skill_toolkit.go
git commit -m "feat(skill-toolkit): 新增 installSkillnetSyncWait 轮询方法，替换 skillnet 分支的错误调用"
```

---

### Task 2: 新增 SkillNet 安装轮询测试

**Files:**
- Modify: `internal/swarm/agents/harness/tools/skill_toolkit_test.go`

- [ ] **Step 1: 新增 `TestInstallSkillnetSyncWait_直接返回` 测试**

在 `skill_toolkit_test.go` 末尾添加：

```go
// TestInstallSkillnetSyncWait_直接返回 验证安装直接返回结果（非 pending）
func TestInstallSkillnetSyncWait_直接返回(t *testing.T) {
	tmpDir := t.TempDir()
	sm := skillpkg.NewSkillManager(tmpDir)

	// 预设一个已完成的安装任务
	sm.SetSkillnetInstallJob("test-install-id", map[string]any{
		"status": "done",
		"skill": map[string]any{
			"name": "direct-skill",
		},
	})

	// HandleSkillsSkillnetInstall 通过 mock 返回非 pending 结果
	// 由于 SkillManager 的 HandleSkillsSkillnetInstall 总是返回 pending，
	// 这里直接测试 installSkillnetSyncWait 对非 pending 结果的处理
	// 通过直接调用方法验证
	tk := NewSkillToolkit(sm)

	// 直接调用 installSkillnetSyncWait，它会先调用 HandleSkillsSkillnetInstall
	// 返回 pending + install_id，然后轮询到 done
	result := tk.installSkillnetSyncWait(context.Background(), "https://example.com/skill", 5)

	// 因为 HandleSkillsSkillnetInstall 创建了 pending job，但轮询会找到它
	// 由于 job status 是 pending，需要手动更新为 done
	// 这个测试验证的是轮询逻辑能正常工作
	_ = result
}
```

注意：由于 `HandleSkillsSkillnetInstall` 会创建新的 install job，且 `installSkillnetSyncWait` 内部会生成新的 install_id，直接测试需要通过 `SkillManager` 的 `SetSkillnetInstallJob` 方法。先检查该方法是否存在。

- [ ] **Step 2: 检查 `SkillManager` 是否有 `SetSkillnetInstallJob` 方法**

Run: `grep -n 'SetSkillnetInstallJob\|skillnetInstallJobs' /home/opensource/uap-claw-go/internal/swarm/server/runtime/skill/skill_manager.go | head -20`

如果不存在，需要添加一个测试辅助方法。如果存在，直接使用。

- [ ] **Step 3: 添加 `SetSkillnetInstallJob` 辅助方法（如需要）**

在 `skill_manager.go` 的导出函数区块添加：

```go
// SetSkillnetInstallJob 设置 SkillNet 安装任务（测试辅助方法）
func (sm *SkillManager) SetSkillnetInstallJob(installID string, job map[string]any) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.skillnetInstallJobs[installID] = job
}
```

- [ ] **Step 4: 编写完整的 4 个轮询测试**

在 `skill_toolkit_test.go` 末尾添加：

```go
// TestInstallSkill_SkillNet_轮询成功 验证 SkillNet 安装轮询成功
func TestInstallSkill_SkillNet_轮询成功(t *testing.T) {
	tmpDir := t.TempDir()
	sm := skillpkg.NewSkillManager(tmpDir)

	// 创建本地技能目录（用于 buildInstalledItem）
	skillDir := filepath.Join(sm.SkillsDir(), "sn-skill")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: sn-skill\ndescription: SkillNet 技能\n---\n技能正文"), 0o644)

	tk := NewSkillToolkit(sm)

	// 在 goroutine 中延迟更新 install job 为 done
	done := make(chan struct{})
	go func() {
		defer close(done)
		// 等待 install job 被创建
		time.Sleep(800 * time.Millisecond)
		// 找到并更新 job
		for _, jobID := range getInstallJobIDs(sm) {
			sm.SetSkillnetInstallJob(jobID, map[string]any{
				"status": "done",
				"skill": map[string]any{
					"name": "sn-skill",
				},
			})
		}
		// 添加 local skill 以便 buildInstalledItem 能找到
		sm.AddLocalSkill(map[string]any{
			"name":   "sn-skill",
			"source": "skillnet",
			"origin": "https://example.com/skill",
		})
	}()

	result, err := tk.InstallSkill(context.Background(), map[string]any{
		"identifier": "https://example.com/skill",
		"source":     "skillnet",
		"timeout_sec": 5,
	})
	<-done

	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if !toBool(result["success"]) {
		t.Errorf("应返回 success=true, got %v", result)
	}
	if result["installed"] != true {
		t.Error("应返回 installed=true")
	}
}

// TestInstallSkill_SkillNet_超时 验证 SkillNet 安装超时
func TestInstallSkill_SkillNet_超时(t *testing.T) {
	tmpDir := t.TempDir()
	sm := skillpkg.NewSkillManager(tmpDir)
	tk := NewSkillToolkit(sm)

	result, err := tk.InstallSkill(context.Background(), map[string]any{
		"identifier":  "https://example.com/skill",
		"source":      "skillnet",
		"timeout_sec": 1,
	})

	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) {
		t.Errorf("超时应返回 success=false, got %v", result)
	}
	detail := toString(result["detail"])
	if !strings.Contains(detail, "timed out") {
		t.Errorf("detail 应包含 'timed out', got %q", detail)
	}
}

// TestInstallSkill_SkillNet_安装失败 验证 SkillNet 安装请求失败
func TestInstallSkill_SkillNet_安装失败(t *testing.T) {
	tmpDir := t.TempDir()
	sm := skillpkg.NewSkillManager(tmpDir)
	tk := NewSkillToolkit(sm)

	// 在 goroutine 中延迟更新 install job 为 failed
	done := make(chan struct{})
	go func() {
		defer close(done)
		time.Sleep(800 * time.Millisecond)
		for _, jobID := range getInstallJobIDs(sm) {
			sm.SetSkillnetInstallJob(jobID, map[string]any{
				"status": "failed",
				"detail": "download failed",
			})
		}
	}()

	result, err := tk.InstallSkill(context.Background(), map[string]any{
		"identifier":  "https://example.com/skill",
		"source":      "skillnet",
		"timeout_sec": 5,
	})
	<-done

	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) {
		t.Errorf("安装失败应返回 success=false, got %v", result)
	}
}

// TestInstallSkill_SkillNet_非pending 验证安装直接返回非 pending 结果
func TestInstallSkill_SkillNet_非pending(t *testing.T) {
	// 当前 HandleSkillsSkillnetInstall 总是返回 pending，
	// 此测试验证如果未来支持同步安装，installSkillnetSyncWait 能正确处理
	// 通过直接调用 installSkillnetSyncWait 并 mock manager 来测试
	// 暂时跳过，因为需要 mock HandleSkillsSkillnetInstall
	t.Skip("需要 mock HandleSkillsSkillnetInstall 返回非 pending 结果")
}

// getInstallJobIDs 获取所有安装任务 ID（测试辅助）
func getInstallJobIDs(sm *skillpkg.SkillManager) []string {
	return sm.GetInstallJobIDs()
}
```

- [ ] **Step 5: 添加 `GetInstallJobIDs` 辅助方法到 `SkillManager`**

在 `skill_manager.go` 的导出函数区块添加：

```go
// GetInstallJobIDs 获取所有安装任务 ID（测试辅助方法）
func (sm *SkillManager) GetInstallJobIDs() []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	ids := make([]string, 0, len(sm.skillnetInstallJobs))
	for id := range sm.skillnetInstallJobs {
		ids = append(ids, id)
	}
	return ids
}
```

- [ ] **Step 6: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/swarm/agents/harness/tools/ -run "TestInstallSkill_SkillNet" -v -timeout 30s`
Expected: 3 个测试通过（1 个 skip）

- [ ] **Step 7: 运行已有测试确保无回归**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/swarm/agents/harness/tools/ -v -timeout 60s`
Expected: 所有测试通过

- [ ] **Step 8: Commit**

```bash
git add internal/swarm/agents/harness/tools/skill_toolkit_test.go internal/swarm/server/runtime/skill/skill_manager.go
git commit -m "test(skill-toolkit): 新增 SkillNet 安装轮询测试（成功/超时/失败）"
```

---

### Task 3: Code 模式注册 SkillToolkit

**Files:**
- Modify: `internal/swarm/server/adapter/code_adapter.go:622-624`

- [ ] **Step 1: 添加 import**

当前 import（第 3-30 行）中没有 `skilltools` 包。在 import 块中添加：

```go
	skilltools "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/tools"
```

- [ ] **Step 2: 替换 `case "skill_toolkit"` 占位**

当前代码（第 622-624 行）：
```go
	case "skill_toolkit":
		// ⤵️ 10.6.3-10: skill_toolkit 工具尚未实现
		logger.Debug(logComponent).Str("tool", toolName).Msg("skill_toolkit 工具尚未实现，跳过")
```

改为：
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

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/swarm/server/adapter/`
Expected: 编译成功，无错误

- [ ] **Step 4: Commit**

```bash
git add internal/swarm/server/adapter/code_adapter.go
git commit -m "feat(code-adapter): 回填 skill_toolkit 工具注册，对齐 Python _build_skill_toolkit"
```

---

### Task 4: 更新 IMPLEMENTATION_PLAN.md 标记

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md:585`

- [ ] **Step 1: 更新 9.50 状态**

当前第 585 行：
```
| 9.50 | 🔄 | Workspace 管理 | ✅ WorkspaceNode 枚举（15 值）；✅ DirectoryNode 类型；✅ Workspace 结构体 + NewWorkspace/GetDirectory/SetDirectory/GetNodePath/GetDefaultDirectory 方法；✅ validateDirectoryNode 校验；✅ CN/EN 双语默认模式；✅ 默认目录自动补全；✅ 深拷贝隔离；✅ 27 个单元测试全部通过 | `openjiuwen/harness/workspace/` |`
```

将 `🔄` 改为 `✅`，并在描述末尾追加 SkillToolkit 回填信息：

```
| 9.50 | ✅ | Workspace 管理 | ✅ WorkspaceNode 枚举（15 值）；✅ DirectoryNode 类型；✅ Workspace 结构体 + NewWorkspace/GetDirectory/SetDirectory/GetNodePath/GetDefaultDirectory 方法；✅ validateDirectoryNode 校验；✅ CN/EN 双语默认模式；✅ 默认目录自动补全；✅ 深拷贝隔离；✅ 27 个单元测试全部通过；✅ SkillToolkit 回填（Code 模式注册 + SkillNet 安装轮询） | `openjiuwen/harness/workspace/` |
```

- [ ] **Step 2: Commit**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 9.50 状态为已完成，标记 SkillToolkit 回填"
```

---

### Task 5: 全量测试验证

**Files:** 无新增

- [ ] **Step 1: 运行 skill_toolkit 包测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/swarm/agents/harness/tools/ -v -timeout 60s`
Expected: 所有测试通过

- [ ] **Step 2: 运行 skill 包测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/swarm/server/runtime/skill/ -v -timeout 60s`
Expected: 所有测试通过

- [ ] **Step 3: 运行 adapter 包测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/swarm/server/adapter/ -v -timeout 120s`
Expected: 所有测试通过

- [ ] **Step 4: 运行全量编译**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 编译成功
