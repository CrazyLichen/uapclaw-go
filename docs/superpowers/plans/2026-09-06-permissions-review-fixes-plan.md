# Permissions 审查修复实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 10.6.5 Permissions 实现审查发现的 2 个问题（流式权限上下文丢失 + llm any 类型安全）

**Architecture:** 两项独立修复：(1) processMessageStreamImpl 对齐 processMessageImpl 的权限上下文注入；(2) 将 5 处 llm any 替换为 *llm.Model，与 7.8 memory 修复保持一致

**Tech Stack:** Go 1.23, project go-code-conventions

---

### Task 1: 修复 processMessageStreamImpl 权限上下文注入

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter.go:862-867`

- [ ] **Step 1: 修改 processMessageStreamImpl 权限上下文注入逻辑**

将 L862-867 从：
```go
	// 步骤 11-12: 权限上下文设置
	// 对齐 Python: TOOL_PERMISSION_CHANNEL_ID.set(channel_id) + setup_permission_context(request)
	ctx = schema.WithToolPermissionChannelID(ctx, req.ChannelID)
	if req.PermissionContext != nil {
		ctx = schema.WithPermissionContextValue(ctx, req.PermissionContext)
	}
```

改为（对齐 processMessageImpl L713-720）：
```go
	// 步骤 10-11: 权限上下文设置
	// 对齐 Python: TOOL_PERMISSION_CHANNEL_ID.set(channel_id) + setup_permission_context(request)
	ctx = schema.WithToolPermissionChannelID(ctx, req.ChannelID)
	permCtx := schema.NewPermissionContextFromRequest(req.ChannelID, req.Metadata)
	if permCtx != nil {
		ctx = schema.WithPermissionContextValue(ctx, permCtx)
	} else if req.PermissionContext != nil {
		// 回退：使用请求中已携带的 PermissionContext
		ctx = schema.WithPermissionContextValue(ctx, req.PermissionContext)
	}
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/swarm/server/adapter/...`
Expected: 无错误

- [ ] **Step 3: 运行 adapter 测试**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test -tags=test ./internal/swarm/server/adapter/... -count=1 -v 2>&1 | tail -20`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/server/adapter/deep_adapter.go
git commit -m "fix(adapter): processMessageStreamImpl 补充 NewPermissionContextFromRequest 调用

流式请求的权限上下文注入与 processMessageImpl 对齐：
先从 metadata 构建，fallback 到 req.PermissionContext"
```

---

### Task 2: llm any → *llm.Model（security 包核心 3 处）

**Files:**
- Modify: `internal/agentcore/harness/security/permission_engine.go`
- Modify: `internal/agentcore/harness/security/models.go`
- Modify: `internal/agentcore/harness/security/factory.go`

- [ ] **Step 1: 修改 permission_engine.go — 字段 + 构造函数 + UpdateLLM**

3 处 `llm any` → `llm *llm.Model`：

a) L26 字段：`llm any` → `llm *llm.Model`

b) L46 构造函数参数：
```go
func NewPermissionEngine(config map[string]any, llm *llm.Model, modelName string, workspaceRoot string) *PermissionEngine {
```

c) L86 UpdateLLM 参数：
```go
func (e *PermissionEngine) UpdateLLM(llm *llm.Model, modelName string) {
```

d) 添加 import：
```go
import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)
```

- [ ] **Step 2: 修改 models.go — PermissionRailConstructor 函数类型**

L145 参数：`llm any` → `llm *llm.Model`

```go
type PermissionRailConstructor func(
	config map[string]any,
	engine *PermissionEngine,
	toolNames []string,
	llm *llm.Model,
	modelName string,
	host *ToolPermissionHost,
) agentinterfaces.AgentRail
```

添加 import：
```go
import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)
```

- [ ] **Step 3: 修改 factory.go — BuildPermissionInterruptRail 参数**

L29 参数：`llm any` → `llm *llm.Model`

```go
func BuildPermissionInterruptRail(
	permissions map[string]any,
	engine *PermissionEngine,
	host *ToolPermissionHost,
	workspaceRoot string,
	llm *llm.Model,
	modelName string,
	ctor PermissionRailConstructor,
) agentinterfaces.AgentRail {
```

无需新增 import（factory.go 已 import `agentinterfaces` + `logger`，llm 类型在同包 PermissionRailConstructor 中已改）。

- [ ] **Step 4: 编译验证 security 包**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/harness/security/...`
Expected: 编译失败（调用方还没改），记录错误数

- [ ] **Step 5: 运行 security 包测试（预期部分失败）**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test -tags=test ./internal/agentcore/harness/security/... -count=1 2>&1 | tail -5`
Expected: 测试中 `NewPermissionEngine(config, nil, "", ...)` 的 `nil` 仍兼容 `*llm.Model`（指针零值），应 PASS

---

### Task 3: llm any → *llm.Model（rails/security 包 + 调用方 3 处）

**Files:**
- Modify: `internal/agentcore/harness/rails/security/tool_security_rail.go`
- Modify: `internal/agentcore/harness/deep_agent.go`
- Modify: `internal/swarm/server/adapter/deep_adapter_rails.go`
- Modify: `internal/swarm/server/adapter/code_adapter.go`

- [ ] **Step 1: 修改 tool_security_rail.go — NewPermissionInterruptRail 参数**

L83 参数：`llm any` → `llm *llm.Model`

```go
func NewPermissionInterruptRail(
	config map[string]any,
	engine *harnesssecurity.PermissionEngine,
	toolNames []string,
	llm *llm.Model,
	modelName string,
	host *harnesssecurity.ToolPermissionHost,
) *PermissionInterruptRail {
```

添加 import：
```go
import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	harnesssecurity "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	sessioninteraction "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	sessionstate "github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	utils "github.com/uapclaw/uapclaw-go/internal/common/utils"
)
```

- [ ] **Step 2: 修改 deep_agent.go — 构造闭包签名**

L1473 闭包参数：`llm any` → `llm *llm.Model`

```go
			func(config map[string]any, engine *hsecurity.PermissionEngine, toolNames []string, llm *llm.Model, modelName string, h *hsecurity.ToolPermissionHost) agentinterfaces.AgentRail {
				return securityrail.NewPermissionInterruptRail(config, engine, toolNames, llm, modelName, h)
			},
```

确认 deep_agent.go 是否已 import llm 包：
- `d.model` 类型为 `*llm.Model`（由 `resolveModelForRequest` 返回），llm 包应已在 import 中

- [ ] **Step 3: 修改 deep_adapter_rails.go — 构造闭包签名**

L463 闭包参数：`llm any` → `llm *llm.Model`

```go
		func(config map[string]any, engine *harnesssecurity.PermissionEngine, toolNames []string, llm *llm.Model, mn string, h *harnesssecurity.ToolPermissionHost) sainterfaces.AgentRail {
			return secrail.NewPermissionInterruptRail(config, engine, toolNames, llm, mn, h)
		},
```

确认 deep_adapter_rails.go 是否需要新增 import llm 包（d.model 已是 `*llm.Model`，可能在 deep_adapter.go 中 import 了 llm）

- [ ] **Step 4: 修改 code_adapter.go — buildPermissionRail 参数 + 闭包签名**

L1013 方法参数：`llm any` → `llm *llm.Model`

```go
func (c *CodeAdapter) buildPermissionRail(configBase map[string]any, llm *llm.Model, modelName string) sainterfaces.AgentRail {
```

L1046 闭包参数：`l any` → `l *llm.Model`

```go
		func(config map[string]any, engine *harnesssecurity.PermissionEngine, names []string, l *llm.Model, mn string, h *harnesssecurity.ToolPermissionHost) sainterfaces.AgentRail {
			return secrail.NewPermissionInterruptRail(config, engine, names, l, mn, h)
		},
```

确认 code_adapter.go 是否需要新增 import llm 包

- [ ] **Step 5: 全量编译验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 无错误

- [ ] **Step 6: 全量测试验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test -tags=test ./internal/agentcore/harness/security/... ./internal/agentcore/harness/rails/security/... ./internal/agentcore/harness/... ./internal/swarm/server/adapter/... -count=1 2>&1 | tail -10`
Expected: 全部 PASS

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/harness/security/permission_engine.go internal/agentcore/harness/security/models.go internal/agentcore/harness/security/factory.go internal/agentcore/harness/rails/security/tool_security_rail.go internal/agentcore/harness/deep_agent.go internal/swarm/server/adapter/deep_adapter_rails.go internal/swarm/server/adapter/code_adapter.go
git commit -m "refactor(security): llm any → *llm.Model，消除类型规避

5 处签名替换：PermissionEngine 字段/构造/UpdateLLM +
PermissionRailConstructor + NewPermissionInterruptRail
调用方签名同步：deep_agent + deep_adapter + code_adapter 闭包

已验证无循环依赖：security→llm 单向，llm 不依赖 harness"
```

---

### Task 4: 最终验证 + 推送

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 无错误

- [ ] **Step 2: 关键包测试**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test -tags=test ./internal/agentcore/harness/... ./internal/agentcore/harness/security/... ./internal/agentcore/harness/rails/security/... ./internal/swarm/server/adapter/... ./internal/swarm/schema/... ./internal/swarm/agents/harness/common/rails/permissions/... -count=1 2>&1 | tail -15`
Expected: 全部 PASS

- [ ] **Step 3: 推送**

```bash
git push origin main
```
