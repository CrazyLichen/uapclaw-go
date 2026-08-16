# 9.65a-4 审查修复实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 9.65a-4 审查中发现的 5 个问题：member.go 类型回填、SpawnMember 校验文档修正、spawnAndPublish 提取+Startup/StartupMember 补全、BuildTeam 文档修正、ActiveTeam.Agent any→*TeamAgent

**Architecture:** 5 个修复任务按依赖顺序执行：先做类型回填（member.go + ActiveTeam.Agent），再做方法签名变更（spawnAndPublish），最后修文档

**Tech Stack:** Go 1.22+, 标准库

---

### Task 1: member.go DB/Messager 类型回填

**Files:**
- Modify: `internal/agent_teams/agent/member.go:24-29`
- Modify: `internal/agent_teams/agent/member_test.go`（如有使用 DB/Messager 字段）

- [ ] **Step 1: 修改 member.go 的 import 和字段类型**

在 `member.go` 中添加 `database` 和 `messager` 包的 import，将 `DB any` 和 `Messager any` 改为具体类型：

```go
import (
	"context"

	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)
```

字段类型：
```go
// DB 团队数据库实例
DB database.TeamDatabase
// Messager 消息总线实例
Messager messager.Messager
```

注意：移除 `// TODO(#9.65):` 注释。

- [ ] **Step 2: 编译检查**

Run: `cd /home/opensource/uapclaw-gateway && pgrep -f 'go (build|test)' | xargs -r kill; go build ./internal/agent_teams/...`
Expected: 编译通过，无错误

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/agent/... -count=1`
Expected: 所有测试通过

- [ ] **Step 4: 提交**

```bash
git add internal/agent_teams/agent/member.go
git commit -m "fix(9.65a-4): member.go DB/Messager any→具体类型回填"
```

---

### Task 2: ActiveTeam.Agent any→*TeamAgent + 删除 TeamBackendProvider

**Files:**
- Modify: `internal/agent_teams/runtime/pool.go` — 删除 `TeamBackendProvider` 接口，`Agent any` → `*agent.TeamAgent`
- Modify: `internal/agent_teams/runtime/manager.go` — `getTeamBackend` 简化，添加 `agent` import
- Modify: `internal/agent_teams/runtime/manager_test.go` — `mockAgent` 改为 `*agent.TeamAgent` 或子集
- Modify: `internal/agent_teams/interaction/human_agent_inbox.go` — `AgentLookup` 返回 `*agent.TeamAgent`
- Modify: `internal/agent_teams/interaction/human_agent_inbox_test.go` — 适配新 `AgentLookup` 类型

- [ ] **Step 1: 修改 runtime/pool.go**

删除 `TeamBackendProvider` 接口（第 11-18 行），修改 `ActiveTeam.Agent` 字段类型，添加 `agent` 包 import：

```go
import (
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
)
```

注意：删除 `TeamBackendProvider` 后，`tools` import 不再被 pool.go 使用（只在 `TeamBackendProvider` 接口中引用），应移除：

```go
import (
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent"
)
```

```go
// ActiveTeam 活跃团队条目。
type ActiveTeam struct {
	TeamName string
	// Agent TeamAgent Leader 实例
	Agent *agent.TeamAgent
	SessionID string
	State RuntimeState
	InteractGate *InteractGate
}
```

- [ ] **Step 2: 修改 runtime/manager.go**

添加 `agent` 包 import，简化 `getTeamBackend` 函数：

```go
import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	sessioninteraction "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)
```

```go
// getTeamBackend 从 Agent 中提取 TeamBackend。
func getTeamBackend(a *agent.TeamAgent) *tools.TeamBackend {
	if a == nil {
		return nil
	}
	return a.TeamBackend()
}
```

同时更新所有调用 `getTeamBackend(entry.Agent)` 的地方 — `entry.Agent` 类型从 `any` 变为 `*agent.TeamAgent`，调用签名不变所以无需改动。但需要检查 `entry.Agent` 是否有 `nil` 检查逻辑需要更新。

检查 `manager.go` 中所有 `entry.Agent` 的使用：
- `getTeamBackend(entry.Agent)` — 签名匹配，无需改动
- `entry.Agent.DeliverInput(...)` — 检查是否有 ⤵️ 待 9.55 回填的调用
- `entry.Agent.AutoStartAll()` — 检查是否有 ⤵️ 待 9.55 回填的调用

- [ ] **Step 3: 修改 runtime/manager_test.go**

`mockAgent` 不再需要（因为 `ActiveTeam.Agent` 是 `*agent.TeamAgent`），需要替换为真实 `*agent.TeamAgent`。

`TeamAgent` 的 `configurator` 字段是非导出的，但 `NewTeamAgent(card)` 会创建 `configurator`，
然后通过 `agent` 包的 `AgentConfigurator.SetTeamBackend(v)` 设置。

由于测试在 `runtime` 包中（不同包），无法直接设置 `configurator`。
方案：在 `agent` 包中添加一个 `SetTeamBackendForTest` 方法（或直接在 `team_agent.go` 中添加 `SetTeamBackend` 方法）：

```go
// team_agent.go 中添加
// SetTeamBackend 设置 TeamBackend（测试辅助）。
func (a *TeamAgent) SetTeamBackend(tb *tools.TeamBackend) {
	if a.configurator != nil {
		a.configurator.SetTeamBackend(tb)
	}
}
```

测试中：
```go
func newTestAgent(backend *tools.TeamBackend) *agent.TeamAgent {
	a := agent.NewTeamAgent(&agentschema.AgentCard{})
	a.SetTeamBackend(backend)
	return a
}
```

更新所有 `&mockAgent{backend: tb}` 为 `newTestAgent(tb)`。
删除 `mockAgent` 结构体及其 `TeamBackend()` 方法。

- [ ] **Step 4: 修改 interaction/human_agent_inbox.go**

添加 `agent` 包 import，修改 `AgentLookup` 类型：

```go
import (
	"context"
	"fmt"
	"sort"

	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)
```

```go
// AgentLookup 解析 human-agent 成员名到活跃 TeamAgent 运行时。
// 对齐 Python: AgentLookup = Callable[[str], Optional[TeamAgent]]
type AgentLookup func(sender string) *agent.TeamAgent
```

`driveAgent` 方法中 `agent` 变量类型从 `any` 变为 `*agent.TeamAgent`，nil 检查逻辑不变。但步骤 4-5（`agent.DeliverInput`）仍标注 ⤵️ 待 9.55 回填，现在类型已知可以调用但方法尚未实现，暂保留 ⤵️ 标记。

- [ ] **Step 5: 修改 interaction/human_agent_inbox_test.go**

将 `func(sender string) any { return nil }` 改为 `func(sender string) *agent.TeamAgent { return nil }`。

需要添加 `agent` 包 import。

- [ ] **Step 6: 编译检查**

Run: `cd /home/opensource/uapclaw-gateway && pgrep -f 'go (build|test)' | xargs -r kill; go build ./internal/agent_teams/...`
Expected: 编译通过

- [ ] **Step 7: 运行全部测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/... -count=1`
Expected: 所有测试通过

- [ ] **Step 8: 提交**

```bash
git add internal/agent_teams/runtime/pool.go internal/agent_teams/runtime/manager.go internal/agent_teams/runtime/manager_test.go internal/agent_teams/interaction/human_agent_inbox.go internal/agent_teams/interaction/human_agent_inbox_test.go
git commit -m "fix(9.65a-4): ActiveTeam.Agent any→*TeamAgent + 删除 TeamBackendProvider + AgentLookup 类型回填"
```

---

### Task 3: 提取 spawnAndPublish + 补全 Startup/StartupMember

**Files:**
- Modify: `internal/agent_teams/tools/team_backend.go` — 新增 `spawnAndPublish`，修改 `Startup`/`StartupMember` 签名
- Modify: `internal/agent_teams/tools/team_backend_test.go` — 适配新签名，补测试

- [ ] **Step 1: 在 team_backend.go 的非导出函数区块添加 spawnAndPublish**

在 `publishEvent` 方法附近添加：

```go
// spawnAndPublish 启动成员 agent 并发布 MemberSpawnedEvent。
// 对齐 Python: _spawn_and_publish(member_name, on_created)
// 事件发布失败仅记日志不抛异常。
func (tb *TeamBackend) spawnAndPublish(
	ctx context.Context,
	memberName string,
	onCreated func(ctx context.Context, memberName string) error,
) error {
	// 步骤 1: 调用 onCreated 回调（启动 agent 进程）
	if err := onCreated(ctx, memberName); err != nil {
		return err
	}
	// 步骤 2: 发布 MemberSpawnedEvent（失败只记日志不抛异常）
	tb.publishEvent(ctx, atschema.MemberSpawnedEvent{
		BaseEventMessage: atschema.BaseEventMessage{
			TeamName:   tb.teamName,
			MemberName: memberName,
		},
	})
	logger.Debug(tbLogComponent).Str("member_name", memberName).Str("team_name", tb.teamName).
		Msg("spawnAndPublish: MemberSpawnedEvent 已发布")
	// 步骤 3: 日志
	logger.Info(tbLogComponent).Str("member_name", memberName).Str("team_name", tb.teamName).
		Msg("spawnAndPublish: 成员已启动")
	return nil
}
```

- [ ] **Step 2: 修改 Startup 签名和实现**

```go
// Startup 启动所有 UNSTARTED 成员。
// 对齐 Python: TeamBackend.startup(on_created=...)
// 返回已启动的成员名列表。
func (tb *TeamBackend) Startup(
	ctx context.Context,
	onCreated func(ctx context.Context, memberName string) error,
) ([]string, error) {
	members, err := tb.db.Member().GetTeamMembers(ctx, tb.teamName, string(atschema.MemberStatusUnstarted))
	if err != nil {
		return nil, err
	}
	var started []string
	for _, m := range members {
		if m.MemberName == tb.memberName {
			continue // 跳过自身
		}
		ok, err := tb.StartupMember(ctx, m.MemberName, onCreated)
		if err != nil {
			return started, err
		}
		if ok {
			started = append(started, m.MemberName)
		}
	}
	return started, nil
}
```

- [ ] **Step 3: 修改 StartupMember 签名和实现**

```go
// StartupMember CAS 启动单个成员（UNSTARTED→STARTING）。
// 对齐 Python: TeamBackend.startup_member(member_name, on_created=...)
//
// Python 步骤：
//  1. CAS: UNSTARTED→STARTING
//  2. 调用 _spawn_and_publish(member_name, on_created)
//  3. 如果 _spawn_and_publish 失败 → 回滚 STARTING→UNSTARTED
func (tb *TeamBackend) StartupMember(
	ctx context.Context,
	memberName string,
	onCreated func(ctx context.Context, memberName string) error,
) (bool, error) {
	// 步骤 1: CAS 转换
	transitioned := tb.db.Member().TryTransitionMemberStatus(ctx, memberName, tb.teamName,
		string(atschema.MemberStatusUnstarted), string(atschema.MemberStatusStarting))
	if !transitioned {
		return false, nil
	}

	// 步骤 2: 调用 spawnAndPublish
	err := tb.spawnAndPublish(ctx, memberName, onCreated)
	if err != nil {
		// 步骤 3: 失败回滚 STARTING→UNSTARTED
		tb.db.Member().TryTransitionMemberStatus(ctx, memberName, tb.teamName,
			string(atschema.MemberStatusStarting), string(atschema.MemberStatusUnstarted))
		return false, err
	}

	return true, nil
}
```

- [ ] **Step 4: 更新 team_backend_test.go 中的 Startup/StartupMember 测试**

搜索所有 `tb.Startup(ctx)` 和 `tb.StartupMember(ctx, ...)` 调用，添加 `onCreated` 参数。

简单测试中传 `nil` 作为 onCreated（表示不启动 agent 进程，只做 CAS + 事件发布）：
```go
started, err := tb.Startup(ctx, nil)
```

但 `spawnAndPublish` 中 `onCreated` 为 `nil` 时会 panic。需要在 `spawnAndPublish` 中加 nil 检查：

```go
func (tb *TeamBackend) spawnAndPublish(
	ctx context.Context,
	memberName string,
	onCreated func(ctx context.Context, memberName string) error,
) error {
	// 步骤 1: 调用 onCreated 回调（启动 agent 进程）
	if onCreated != nil {
		if err := onCreated(ctx, memberName); err != nil {
			return err
		}
	}
	// ... 事件发布和日志不变
}
```

更新测试用例：
- `TestStartup`：`tb.Startup(ctx, nil)` 或传一个空回调
- `TestStartupMember`：`tb.StartupMember(ctx, "teammate1", nil)` 或传一个空回调
- 新增 `TestStartupMember_回调失败回滚`：传一个返回 error 的回调，验证回滚到 UNSTARTED
- 新增 `TestSpawnAndPublish`：测试事件发布 + 日志

- [ ] **Step 5: 编译检查**

Run: `cd /home/opensource/uapclaw-gateway && pgrep -f 'go (build|test)' | xargs -r kill; go build ./internal/agent_teams/...`
Expected: 编译通过

- [ ] **Step 6: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/tools/... -count=1 -v -run "TestStartup|TestStartupMember|TestSpawnAndPublish"`
Expected: 所有测试通过

- [ ] **Step 7: 运行全量测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/... -count=1`
Expected: 所有测试通过

- [ ] **Step 8: 提交**

```bash
git add internal/agent_teams/tools/team_backend.go internal/agent_teams/tools/team_backend_test.go
git commit -m "feat(9.65a-4): 提取 spawnAndPublish + Startup/StartupMember 补全 onCreated 回调和失败回滚"
```

---

### Task 4: 修正设计文档描述

**Files:**
- Modify: `docs/superpowers/specs/2027-03-10-team-backend-9.65a-4-design.md:267,280`

- [ ] **Step 1: 修正 SpawnMember 校验描述（第 267 行附近）**

将：
```
1. 校验：member_name 非空、不含"/"、非保留名
```

改为：
```
1. 校验：仅检查 DB 中是否已存在同名成员，格式校验留给 Tool 层（9.68）
```

- [ ] **Step 2: 修正 BuildTeam db.Initialize 描述（第 280 行附近）**

将：
```
2. DB 初始化：db.initialize() + db.create_cur_session_tables()
```

改为：
```
2. DB 初始化：不在 build_team 中调用（db.initialize 在 runtime/manager 和 coordination/kernel 中调用，create_cur_session_tables 在 SessionManager.bind_session 中调用）
```

- [ ] **Step 3: 提交**

```bash
git add docs/superpowers/specs/2027-03-10-team-backend-9.65a-4-design.md
git commit -m "docs(9.65a-4): 修正 SpawnMember 校验描述和 BuildTeam db.Initialize 描述"
```

---

### Task 5: 全量构建 + 测试验证 + doc.go 更新

**Files:**
- Modify: `internal/agent_teams/runtime/doc.go`（如有变更）
- Modify: `internal/agent_teams/agent/doc.go`（如有变更）
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 全量构建**

Run: `cd /home/opensource/uapclaw-gateway && pgrep -f 'go (build|test)' | xargs -r kill; go build ./...`
Expected: 编译通过

- [ ] **Step 2: 全量测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/... -count=1`
Expected: 所有测试通过

- [ ] **Step 3: 检查覆盖率**

Run: `cd /home/opensource/uapclaw-gateway && go test -cover ./internal/agent_teams/tools/...`
Expected: TeamBackend 覆盖率 ≥ 82%

- [ ] **Step 4: 更新 doc.go（如有变更）**

检查 `runtime/doc.go` 和 `agent/doc.go` 是否需要更新文件树。

- [ ] **Step 5: 提交**

```bash
git add -A
git commit -m "docs(9.65a-4): 审查修复完成，更新 doc.go + IMPLEMENTATION_PLAN.md"
```

- [ ] **Step 6: 推送**

```bash
git push
```
