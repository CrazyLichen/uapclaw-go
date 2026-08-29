# 2026-08-22 审查问题修复实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 2026-08-22 逻辑审查中确认的 28 个真实问题，对齐 Python 行为

**Architecture:** 按章节分 5 组任务，组内按依赖关系排序。每组完成后独立编译验证。

**Tech Stack:** Go 1.x, gopkg.in/yaml.v3（新增依赖）

**设计文档：** `docs/superpowers/specs/2026-08-22-review-fixes-design.md`

---

## 依赖关系

```
Task 1 (7.6 memory) → 独立
Task 2 (9.65a TeamBackend) → 独立
Task 3 (10.6.3 AskUserRail) → 独立
Task 4 (10.3 skill) → 内部有依赖：S10.3.6→M10.3.2, S10.3.2→T10.3.2
Task 5 (10.6.1-2 Prompt) → 独立
```

---

## Task 1: 7.6 FragmentMemoryManager 修复

**Files:**
- Modify: `internal/agentcore/memory/manage/index/base_manager.go`
- Modify: `internal/agentcore/memory/manage/index/fragment_manager.go`
- Modify: `internal/agentcore/memory/manage/update/update_checker.go`
- Test: `internal/agentcore/memory/manage/index/fragment_manager_test.go`
- Test: `internal/agentcore/memory/manage/update/update_checker_test.go`

### 1.1 S7.2: BaseMemoryManager 接口改用 BaseMemoryUnit 基类型

- [x] **Step 1: 修改 BaseMemoryManager.AddMemories 签名**

`internal/agentcore/memory/manage/index/base_manager.go` L25:
```go
// 修改前
AddMemories(ctx context.Context, userID string, scopeID string, memories map[string][]*mem_model.FragmentMemoryUnit) ([]*mem_model.FragmentMemoryUnit, error)
// 修改后
AddMemories(ctx context.Context, userID string, scopeID string, memories map[string][]*mem_model.BaseMemoryUnit, llm ...*llm.Model) ([]*mem_model.BaseMemoryUnit, error)
```

注意：同时添加 `llm ...*llm.Model` 可选参数（S7.1 一起改）。需要导入 `llm` 包。

- [x] **Step 2: 修改 FragmentMemoryManager.AddMemories 签名和实现**

`internal/agentcore/memory/manage/index/fragment_manager.go` L68:
```go
// 修改前
func (m *FragmentMemoryManager) AddMemories(ctx context.Context, userID string, scopeID string, memories map[string][]*mem_model.FragmentMemoryUnit) ([]*mem_model.FragmentMemoryUnit, error)
// 修改后
func (m *FragmentMemoryManager) AddMemories(ctx context.Context, userID string, scopeID string, memories map[string][]*mem_model.BaseMemoryUnit, llm ...*llm.Model) ([]*mem_model.BaseMemoryUnit, error)
```

在方法体开头添加类型断言，将 `BaseMemoryUnit` 转为 `FragmentMemoryUnit`：
```go
// 类型断言：将基类型转为碎片记忆类型（对齐 Python: isinstance(mem_unit, FragmentMemoryUnit)）
fragmentMemories := make(map[string][]*mem_model.FragmentMemoryUnit, len(memories))
for key, units := range memories {
    fragUnits := make([]*mem_model.FragmentMemoryUnit, 0, len(units))
    for _, unit := range units {
        frag, ok := unit.(*mem_model.FragmentMemoryUnit)
        if !ok {
            continue // 跳过非 FragmentMemoryUnit 类型
        }
        fragUnits = append(fragUnits, frag)
    }
    fragmentMemories[key] = fragUnits
}
```

后续逻辑使用 `fragmentMemories` 代替原来的 `memories`。

返回值也需要对应转换。

- [x] **Step 3: 修改 MemUpdateChecker.Check 签名**

`internal/agentcore/memory/manage/update/update_checker.go` L79:
```go
// 修改前
func (c *MemUpdateChecker) Check(newMemories map[string]string, oldMemories map[string]string) ([]*MemoryActionItem, error)
// 修改后
func (c *MemUpdateChecker) Check(newMemories map[string]string, oldMemories map[string]string, opts ...CheckOption) ([]*MemoryActionItem, error)
```

添加 CheckOption 定义：
```go
// ──────────────────────────── 结构体 ────────────────────────────

// checkConfig Check 配置
type checkConfig struct {
    // model LLM 模型（对齐 Python: base_chat_model）
    model *llm.Model
    // retries 重试次数（对齐 Python: retries=3）
    retries int
}

// ──────────────────────────── 导出函数 ────────────────────────────

// CheckOption Check 可选参数
type CheckOption func(*checkConfig)

// WithModel 设置 LLM 模型（对齐 Python: base_chat_model）
func WithModel(m *llm.Model) CheckOption {
    return func(c *checkConfig) { c.model = m }
}

// WithRetries 设置重试次数（对齐 Python: retries）
func WithRetries(n int) CheckOption {
    return func(c *checkConfig) { c.retries = n }
}
```

在 Check 方法体中解析 opts：
```go
cfg := &checkConfig{retries: 3}
for _, opt := range opts {
    opt(cfg)
}
```

当前 stub 实现不使用 cfg，但接口已就绪供 7.8 回填。

- [x] **Step 4: 修改 FragmentMemoryManager.AddMemories 中 Check 调用**

`fragment_manager.go` 中 Check 调用处传递 llm 参数：
```go
actionItems, err := m.checker.Check(newMemContent, oldMemories, WithModel(model)...)
// 其中 model 从 llm 参数获取：
var model *llm.Model
if len(llm) > 0 {
    model = llm[0]
}
```

- [x] **Step 5: 添加 ⤵️ 标记注释**

在 `AddMemories` 方法签名旁添加注释说明 llm 参数供 7.8 回填使用。

- [x] **Step 6: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/memory/...`
Expected: 编译成功

- [x] **Step 7: 更新测试**

更新 `fragment_manager_test.go` 和 `update_checker_test.go` 中的调用签名，适配新参数。

- [x] **Step 8: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/memory/... -v -count=1`
Expected: 全部 PASS

- [x] **Step 9: Commit**

```bash
git add internal/agentcore/memory/
git commit -m "fix(S7.1+S7.2): AddMemories 改用 BaseMemoryUnit + 添加 llm 参数, Check 添加 CheckOption"
```

### 1.2 M7.1: Search 添加排序+截断

- [x] **Step 1: 修改 FragmentMemoryManager.Search**

`fragment_manager.go` L205-222，在 `return results, nil` 前添加：
```go
// 防御性排序（对齐 Python: result.sort(key=lambda x: x["score"], reverse=True)）
sort.Slice(results, func(i, j int) bool {
    return results[i].Score > results[j].Score
})
// 截断（对齐 Python: return result[:top_k]）
if topK > 0 && len(results) > topK {
    results = results[:topK]
}
```

需要导入 `sort` 包。

- [x] **Step 2: 编译+测试**

Run: `go build ./internal/agentcore/memory/... && go test ./internal/agentcore/memory/... -v -count=1`
Expected: 编译成功，测试 PASS

- [x] **Step 3: Commit**

```bash
git add internal/agentcore/memory/manage/index/fragment_manager.go
git commit -m "fix(M7.1): Search 添加 score 降序排序和 topK 截断"
```

### 1.3 M7.2: ListFragmentMemories 添加校验+排序

- [x] **Step 1: 修改 ListFragmentMemories 添加 memType 校验**

`fragment_manager.go` L283，在 `var memTypes []string` 之前添加：
```go
// 非 FragmentMemoryType 校验（对齐 Python: mem_type.value not in FRAGMENT_MEMORY_TYPE）
if memType != "" && !isFragmentMemoryType(memType) {
    logger.Error(logComponent).
        Str("mem_type", memType).
        Str("memory_type", m.memType).
        Msg("非法碎片记忆类型")
    return nil, nil
}
```

注意：`isFragmentMemoryType` 已在 L447 定义。

- [x] **Step 2: 添加结果排序**

在 `return docs, nil` 前添加：
```go
// 降序排序（对齐 Python: result.sort(key=lambda x: (x['mem'], str(x.get('timestamp') or '')), reverse=True)）
sort.Slice(docs, func(i, j int) bool {
    if docs[i].MemType != docs[j].MemType {
        return docs[i].MemType > docs[j].MemType
    }
    tsI := parseTimestamp(docs[i].Timestamp)
    tsJ := parseTimestamp(docs[j].Timestamp)
    if tsI.Equal(tsJ) {
        return false
    }
    return tsI.After(tsJ)
})
```

注意：`parseTimestamp` 已在 L426 定义。

- [x] **Step 3: 编译+测试**

Run: `go build ./internal/agentcore/memory/... && go test ./internal/agentcore/memory/... -v -count=1`
Expected: 编译成功，测试 PASS

- [x] **Step 4: Commit**

```bash
git add internal/agentcore/memory/manage/index/fragment_manager.go
git commit -m "fix(M7.2): ListFragmentMemories 添加 memType 校验和降序排序"
```

### 1.4 M7.3: 添加 ⤵️ 标记注释

- [x] **Step 1: 在 AddMemories 的 ⤵️ 注释处补充说明**

`fragment_manager.go` 约 L63 已有 `⤵️ 回填: 7.8` 注释，补充：
```go
// ⤵️ 回填: 7.8 — LLM 驱动冲突检查实现时需补：
//   1. MemUpdateChecker.Check 完整逻辑（当前 stub）
//   2. processConflictInfo 方法（将 LLM 返回的数字 ID 映射回实际记忆 ID）
```

- [x] **Step 2: Commit**

```bash
git add internal/agentcore/memory/manage/index/fragment_manager.go
git commit -m "docs(M7.3): 补充 processConflictInfo ⤵️ 回填标记"
```

---

## Task 2: 9.65a-4 TeamBackend 修复

**Files:**
- Modify: `internal/agent_teams/tools/team_backend.go`
- Test: `internal/agent_teams/tools/team_backend_test.go`

### 2.1 S9.2: ShutdownMember 加 ShutdownOption

- [x] **Step 1: 添加 ShutdownOption 定义**

在 `team_backend.go` 的适当位置（TeamBackendOption 之后）添加：
```go
// ShutdownOption ShutdownMember 可选参数
type ShutdownOption func(*shutdownConfig)

// shutdownConfig ShutdownMember 配置
type shutdownConfig struct {
    // force 是否强制关闭（对齐 Python: force=False）
    force bool
}

// WithForce 设置强制关闭（对齐 Python: shutdown_member(force=True)）
func WithForce(force bool) ShutdownOption {
    return func(c *shutdownConfig) { c.force = force }
}
```

- [x] **Step 2: 修改 ShutdownMember 签名**

L418:
```go
// 修改前
func (tb *TeamBackend) ShutdownMember(ctx context.Context, memberName string) atschema.MemberOpResult
// 修改后
func (tb *TeamBackend) ShutdownMember(ctx context.Context, memberName string, opts ...ShutdownOption) atschema.MemberOpResult
```

方法体开头解析 opts：
```go
cfg := &shutdownConfig{force: false}
for _, opt := range opts {
    opt(cfg)
}
```

将事件发布中的 `Force: false` 改为 `Force: cfg.force`。

- [x] **Step 3: 编译验证**

Run: `go build ./internal/agent_teams/...`
Expected: 编译成功（可能需要更新调用方）

- [x] **Step 4: 更新所有 ShutdownMember 调用方**

搜索所有调用 `tb.ShutdownMember(ctx,` 的位置，签名兼容（opts 可选，无需改动），但 ForceCleanTeam 中应传 `WithForce(true)`。

- [x] **Step 5: Commit**

```bash
git add internal/agent_teams/
git commit -m "fix(S9.2): ShutdownMember 添加 ShutdownOption(WithForce)"
```

### 2.2 S9.4: 移除 CancelAllTasks 调用

- [x] **Step 1: 删除 CancelAllTasks 调用**

`team_backend.go` ShutdownMember 方法中的：
```go
// 删除以下行
_, _ = tb.taskManager.CancelAllTasks(ctx, []string{memberName})
```

- [x] **Step 2: 编译+测试**

Run: `go build ./internal/agent_teams/... && go test ./internal/agent_teams/... -v -count=1`
Expected: 编译成功，测试 PASS

- [x] **Step 3: Commit**

```bash
git add internal/agent_teams/tools/team_backend.go
git commit -m "fix(S9.4): ShutdownMember 移除 CancelAllTasks 调用（对齐 Python: shutdown 不取消任务）"
```

### 2.3 S9.7: ApprovePlan 加 ApprovePlanOption

- [x] **Step 1: 添加 ApprovePlanOption 定义**

```go
// ApprovePlanOption ApprovePlan 可选参数
type ApprovePlanOption func(*approvePlanConfig)

// approvePlanConfig ApprovePlan 配置
type approvePlanConfig struct {
    // approved 是否批准（对齐 Python: approved=True）
    approved bool
    // feedback 反馈意见（对齐 Python: feedback=None）
    feedback string
}

// WithApproved 设置是否批准（对齐 Python: approve_plan(approved=False) 可拒绝计划）
func WithApproved(approved bool) ApprovePlanOption {
    return func(c *approvePlanConfig) { c.approved = approved }
}

// WithFeedback 设置反馈意见（对齐 Python: approve_plan(feedback="...")）
func WithFeedback(feedback string) ApprovePlanOption {
    return func(c *approvePlanConfig) { c.feedback = feedback }
}
```

- [x] **Step 2: 修改 ApprovePlan 签名**

L711:
```go
// 修改前
func (tb *TeamBackend) ApprovePlan(ctx context.Context, taskID string) atschema.MemberOpResult
// 修改后
func (tb *TeamBackend) ApprovePlan(ctx context.Context, planID string, opts ...ApprovePlanOption) atschema.MemberOpResult
```

方法体：
```go
cfg := &approvePlanConfig{approved: true} // 默认批准
for _, opt := range opts {
    opt(cfg)
}
err := tb.taskManager.ApprovePlan(ctx, planID, cfg.approved, cfg.feedback, tb.memberName)
```

- [x] **Step 3: 更新调用方和测试**

搜索所有 `ApprovePlan(ctx,` 调用，更新参数名 taskID→planID 和 opts。

- [x] **Step 4: 编译+测试+Commit**

```bash
go build ./internal/agent_teams/... && go test ./internal/agent_teams/... -v -count=1
git add internal/agent_teams/
git commit -m "fix(S9.7): ApprovePlan 添加 ApprovePlanOption(WithApproved/WithFeedback), 参数名改 planID"
```

### 2.4 S9.9: 移除 ForceCleanTeam 的 onTeamCleaned 调用

- [x] **Step 1: 删除 onTeamCleaned 调用**

`team_backend.go` ForceCleanTeam 方法中删除：
```go
// 删除以下行
if tb.onTeamCleaned != nil {
    if err := tb.onTeamCleaned(ctx); err != nil {
        logger.Warn(tbLogComponent).Err(err).Msg("ForceCleanTeam: onTeamCleaned 回调失败")
    }
}
```

- [x] **Step 2: 编译+测试+Commit**

```bash
go build ./internal/agent_teams/... && go test ./internal/agent_teams/... -v -count=1
git add internal/agent_teams/tools/team_backend.go
git commit -m "fix(S9.9): ForceCleanTeam 移除 onTeamCleaned 回调（对齐 Python: force_clean 不触发回调）"
```

### 2.5 S9.10: ForceCleanTeam 修复

- [x] **Step 1: 移除前置状态检查，传 WithForce(true)**

`team_backend.go` ForceCleanTeam 中：
```go
// 修改前
if m.Status != string(atschema.MemberStatusShutdown) {
    result := tb.ShutdownMember(ctx, m.MemberName)
// 修改后
result := tb.ShutdownMember(ctx, m.MemberName, WithForce(true))
```

删除 `if m.Status != string(atschema.MemberStatusShutdown)` 判断，依赖 ShutdownMember 幂等逻辑。

- [x] **Step 2: 编译+测试+Commit**

```bash
go build ./internal/agent_teams/... && go test ./internal/agent_teams/... -v -count=1
git add internal/agent_teams/tools/team_backend.go
git commit -m "fix(S9.10): ForceCleanTeam 传 WithForce(true) + 移除前置状态检查"
```

### 2.6 S9.12: SpawnMember 加 SpawnMemberOption

- [x] **Step 1: 添加 SpawnMemberOption 定义**

```go
// SpawnMemberOption SpawnMember 可选参数
type SpawnMemberOption func(*spawnMemberConfig)

// spawnMemberConfig SpawnMember 配置
type spawnMemberConfig struct {
    // status 成员状态（对齐 Python: status=UNSTARTED）
    status string
    // executionStatus 执行状态（对齐 Python: execution_status=IDLE）
    executionStatus string
    // mode 成员模式（对齐 Python: mode=BUILD_MODE）
    mode string
    // allocation 模型配置分配（对齐 Python: allocation=None）
    allocation *models.Allocation
}

// WithStatus 设置成员状态
func WithStatus(s string) SpawnMemberOption {
    return func(c *spawnMemberConfig) { c.status = s }
}

// WithExecutionStatus 设置执行状态
func WithExecutionStatus(s string) SpawnMemberOption {
    return func(c *spawnMemberConfig) { c.executionStatus = s }
}

// WithMode 设置成员模式
func WithMode(m string) SpawnMemberOption {
    return func(c *spawnMemberConfig) { c.mode = m }
}

// WithAllocation 设置模型配置分配
func WithAllocation(a *models.Allocation) SpawnMemberOption {
    return func(c *spawnMemberConfig) { c.allocation = a }
}
```

- [x] **Step 2: 修改 SpawnMember 签名**

L304:
```go
// 修改前
func (tb *TeamBackend) SpawnMember(ctx context.Context, memberName, displayName, agentCard, role, desc, prompt, modelName string) atschema.MemberOpResult
// 修改后
func (tb *TeamBackend) SpawnMember(ctx context.Context, memberName, displayName, agentCard, role, desc, prompt, modelName string, opts ...SpawnMemberOption) atschema.MemberOpResult
```

方法体中解析 opts 并使用：
```go
cfg := &spawnMemberConfig{
    status:           string(atschema.MemberStatusUnstarted),
    executionStatus:  string(atschema.ExecutionStatusIdle),
    mode:             tb.teammateMode,
}
for _, opt := range opts {
    opt(cfg)
}
```

将 CreateMember 中的硬编码状态替换为 cfg 中的值。

- [x] **Step 3: 更新调用方**

搜索所有 `SpawnMember(ctx,` 调用，确保兼容（opts 可选）。

- [x] **Step 4: 编译+测试+Commit**

```bash
go build ./internal/agent_teams/... && go test ./internal/agent_teams/... -v -count=1
git add internal/agent_teams/
git commit -m "fix(S9.12): SpawnMember 添加 SpawnMemberOption(WithStatus/WithExecutionStatus/WithMode/WithAllocation)"
```

### 2.7 M9.3: Leader 改走 SpawnMember

- [x] **Step 1: 修改 BuildTeam 中 Leader 注册**

`team_backend.go` BuildTeam 中约 L535-537，将 `tb.db.Member().CreateMember(...)` 改为：
```go
result := tb.SpawnMember(ctx, tb.memberName, leaderDisplayName, "", string(atschema.TeamRoleLeader), leaderDesc, "", leaderModelRef,
    WithStatus(string(atschema.MemberStatusBusy)),
    WithExecutionStatus(string(atschema.ExecutionStatusRunning)),
    WithMode(string(atschema.MemberModeBuildMode)),
)
if !result.Success {
    return fmt.Errorf("注册 Leader 失败: %s", result.MemberName)
}
```

注意：需确认 SpawnMember 中 allocation/modelName 的处理逻辑，确保 Leader 的模型配置正确传递。

- [x] **Step 2: 编译+测试+Commit**

```bash
go build ./internal/agent_teams/... && go test ./internal/agent_teams/... -v -count=1
git add internal/agent_teams/tools/team_backend.go
git commit -m "fix(M9.3): BuildTeam Leader 注册改走 SpawnMember（对齐 Python: 统一路径）"
```

---

## Task 3: 10.6.3 StructuredAskUserRail 修复（含目录迁移）

**Files:**
- Move: `internal/swarm/server/rails/structured_ask_user_tool.go` → `internal/swarm/agents/harness/common/rails/structured_ask_user_tool.go`
- Move: `internal/swarm/server/rails/structured_ask_user_rail.go` → `internal/swarm/agents/harness/common/rails/structured_ask_user_rail.go`
- Move: `internal/swarm/server/rails/structured_ask_user_tool_test.go` → `internal/swarm/agents/harness/common/rails/structured_ask_user_tool_test.go`
- Move: `internal/swarm/server/rails/structured_ask_user_rail_test.go` → `internal/swarm/agents/harness/common/rails/structured_ask_user_rail_test.go`
- Modify: `internal/swarm/server/rails/doc.go`（移除条目）
- Create: `internal/swarm/agents/harness/common/rails/doc.go`

### 3.1 目录迁移

- [x] **Step 1: 创建目标目录**

```bash
mkdir -p internal/swarm/agents/harness/common/rails
```

- [x] **Step 2: 移动文件**

```bash
git mv internal/swarm/server/rails/structured_ask_user_tool.go internal/swarm/agents/harness/common/rails/structured_ask_user_tool.go
git mv internal/swarm/server/rails/structured_ask_user_rail.go internal/swarm/agents/harness/common/rails/structured_ask_user_rail.go
git mv internal/swarm/server/rails/structured_ask_user_tool_test.go internal/swarm/agents/harness/common/rails/structured_ask_user_tool_test.go
git mv internal/swarm/server/rails/structured_ask_user_rail_test.go internal/swarm/agents/harness/common/rails/structured_ask_user_rail_test.go
```

- [x] **Step 3: 修改包名**

所有移动文件中 `package rails` → `package rails`（包名不变，同目录下包名一致即可）。

- [x] **Step 4: 更新 import 路径**

搜索所有引用 `uapclaw-go/internal/swarm/server/rails` 中 structured_ask_user 相关的位置，更新为 `uapclaw-go/internal/swarm/agents/harness/common/rails`。

- [x] **Step 5: 更新 doc.go**

- `internal/swarm/server/rails/doc.go`：移除 structured_ask_user 条目
- 创建 `internal/swarm/agents/harness/common/rails/doc.go`

### 3.2 S10.1+S10.2: Schema 重构

- [x] **Step 6: 重写 NewStructuredAskUserTool，自建 EXTENDED_INPUT_PARAMS schema**

`structured_ask_user_tool.go` 中：
```go
func NewStructuredAskUserTool(language, agentID string) (tool.Tool, error) {
    // 自建 schema（对齐 Python: EXTENDED_INPUT_PARAMS_EN/CN），不再从 AskUserMetadataProvider 获取
    inputParams := buildExtendedInputParams(language)
    description := getStructuredDescription(language)
    // ... 构建card和tool
}
```

添加 `buildExtendedInputParams` 函数，一比一复刻 Python 的 `EXTENDED_INPUT_PARAMS_EN/CN`：
- 顶层 `query`（string, required）
- 顶层 `questions`（array, optional）
- questions item: `required: ["question"]`（仅 question 必填）
- options item: `required: ["label"]`（仅 label 必填）

- [x] **Step 7: 编译+测试**

Run: `go build ./internal/swarm/... && go test ./internal/swarm/... -v -count=1`
Expected: 编译成功，测试 PASS

- [x] **Step 8: Commit**

```bash
git add internal/swarm/
git commit -m "fix(S10.1+S10.2+M10.1+M10.2): StructuredAskUserTool 自建 schema(query+questions) + 迁移到 common/rails"
```

---

## Task 4: 10.3.19-20 技能管理修复

**Files:**
- Modify: `internal/swarm/server/runtime/skill/skill_manager.go`
- Create: `internal/swarm/server/runtime/skill/safe_rmtree.go`（safeRmtree 函数）
- Create: `internal/swarm/server/runtime/skill/git_ops.go`（git 操作函数）
- Create: `internal/swarm/server/runtime/skill/remote_import.go`（远程 URL 下载）
- Create: `internal/swarm/server/runtime/skill/plugin_yaml.go`（plugin.yaml 生成）
- Create: `internal/swarm/server/runtime/skill/agent_data.go`（agent-data.json 生成）
- Create: `internal/swarm/server/runtime/skill/proxy_context.go`（代理环境变量上下文）
- Test: 对应的 `_test.go` 文件

> 注意：此 Task 工作量最大，建议拆分为多个子任务由不同 subagent 并行执行。

### 4.1 S10.3.6: 实现 git 操作

- [x] **Step 1: 创建 git_ops.go，实现 gitClone/gitPull/gitGetCommit**

使用 `os/exec` 调用 git 命令：
- `gitClone(ctx, url, dir) error` → `git clone --depth 1 <url> <dir>`
- `gitPull(ctx, dir) error` → `git -C <dir> pull --ff-only`
- `gitGetCommit(dir) (string, error)` → `git -C <dir> rev-parse HEAD`

返回 commit hash。处理命令执行错误、超时（使用 ctx）。

- [x] **Step 2: 替换 skill_manager.go 中的 stub 实现**

将 L2249-2265 的 stub 替换为调用 git_ops.go 中的函数。

- [x] **Step 3: 测试+Commit**

```bash
go test ./internal/swarm/server/runtime/skill/... -v -count=1
git add internal/swarm/server/runtime/skill/
git commit -m "fix(S10.3.6): 实现 gitClone/gitPull/gitGetCommit（os/exec 调用 git 命令）"
```

### 4.2 T10.3.1: 实现 safeRmtree

- [x] **Step 1: 创建 safe_rmtree.go**

```go
// safeRmtree 安全删除目录（对齐 Python: _safe_rmtree）
// 最多 3 次重试，Windows 上修改文件权限，指数退避延迟
func safeRmtree(path string) error {
    var lastErr error
    for attempt := 0; attempt < 3; attempt++ {
        err := os.RemoveAll(path)
        if err == nil {
            return nil
        }
        lastErr = err
        if runtime.GOOS == "windows" {
            // Windows: 递归修改文件权限为可写
            filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
                if err != nil {
                    return nil
                }
                os.Chmod(p, 0o777)
                return nil
            })
        }
        if attempt < 2 {
            time.Sleep(time.Duration(200*(1<<attempt)) * time.Millisecond) // 200ms, 400ms
        }
    }
    return lastErr
}
```

- [x] **Step 2: 替换 skill_manager.go 中的 os.RemoveAll 调用**

搜索所有 `os.RemoveAll` 用于删除技能目录的位置，替换为 `safeRmtree`。

- [x] **Step 3: 测试+Commit**

### 4.3 M10.3.1: marketplace_add 默认 enabled 改为 false

- [x] **Step 1: 修改 HandleSkillsMarketplaceAdd**

L781: `"enabled": true` → `"enabled": false`

添加注释：`// 新增源默认禁用（对齐 Python: enabled=False，避免未经确认就触发远程同步）`

- [x] **Step 2: 测试+Commit**

### 4.4 S10.3.2: 实现 getMirrorSkillsDirs

- [x] **Step 1: 添加 getMirrorSkillsDirs 方法**

```go
// getMirrorSkillsDirs 返回需要镜像同步的 skills 目录（对齐 Python: _get_mirror_skills_dirs）。
// Go 二进制等价 Python package 安装模式，始终返回空切片。
// ⤵️ 回填: 如未来需要源码开发模式支持，需补全 mirror 路径逻辑。
func (sm *SkillManager) getMirrorSkillsDirs() []string {
    return []string{} // Go 二进制 = package 安装模式，无 mirror 目录
}
```

- [x] **Step 2: 在 ClawHub/TeamSkillsHub/SkillNet/Uninstall 四处调用**

每个安装方法中在 copyto 技能目录后添加：
```go
for _, mirrorRoot := range sm.getMirrorSkillsDirs() {
    mirrorDest := filepath.Join(mirrorRoot, safeName, "skill")
    os.RemoveAll(mirrorDest)
    os.MkdirAll(mirrorRoot, 0o755)
    copyDir(skillDir, mirrorDest)
}
```

Uninstall 中在删除技能目录后添加：
```go
for _, mirrorRoot := range sm.getMirrorSkillsDirs() {
    mirrorDest := filepath.Join(mirrorRoot, safeName, "skill")
    safeRmtree(mirrorDest)
}
```

- [x] **Step 3: 测试+Commit**

### 4.5 S10.3.3: 实现 uninstall 内置技能保护

- [x] **Step 1: 在 HandleSkillsUninstall 中添加 builtin 检测**

在删除目录之前添加：
```go
// 内置技能保护（对齐 Python: 检查 builtin 目录，不允许删除内置技能）
builtinDir := getBuiltinSkillsDir()
if builtinDir != "" {
    isBuiltin, builtinPath := sm.isBuiltinSkill(safeName, builtinDir)
    if isBuiltin {
        destAbs, _ := filepath.Abs(dest)
        builtinAbs, _ := filepath.Abs(builtinPath)
        if destAbs == builtinAbs {
            return map[string]any{"success": false, "detail": "内置技能不允许删除"}, nil
        }
    }
}
```

添加 `isBuiltinSkill` 方法：遍历 builtin 目录 → 解析 SKILL.md 匹配技能名。

- [x] **Step 2: 测试+Commit**

### 4.6 S10.3.1: SkillNet 标注 ⤵️ + Install job 状态修复

- [x] **Step 1: Search/Evaluate 添加 ⤵️ 标记**

- [x] **Step 2: Install 修改 job 为 failed 状态**

在 `HandleSkillsSkillnetInstall` 创建 job 后，添加 goroutine 设置 job 为 failed：
```go
go func() {
    time.Sleep(100 * time.Millisecond) // 确保调用方获取到 pending 状态
    job := sm.GetInstallJob(installID)
    if job != nil {
        job["status"] = "failed"
        job["detail"] = "SkillNet 安装尚未实现 ⤵️"
        sm.SetSkillnetInstallJob(installID, job)
    }
}()
```

- [x] **Step 3: 测试+Commit**

### 4.7 S10.3.4: 实现远程 URL 下载

- [x] **Step 1: 创建 remote_import.go**

实现：
- `isHTTPDownloadTarget(url string) bool`
- `assertImportLocalDownloadURLAllowed(url string) error`（白名单校验）
- `importSkillFromRemoteArchive(ctx, downloadURL string, force bool, checksumSHA256 string) (map[string]any, error)`
  - HTTP 下载（net/http）
  - SHA256 校验（crypto/sha256）
  - ZIP/tar.gz 解压（archive/zip, compress/gzip）
  - 复用本地导入逻辑

- [x] **Step 2: 修改 HandleSkillsImportLocal 添加 URL 分支**

在本地路径处理之前添加 URL 检测：
```go
rawPath := toString(params["path"])
if isHTTPDownloadTarget(rawPath) {
    result, err := sm.importSkillFromRemoteArchive(ctx, rawPath, force, checksumSHA256)
    // ...
    return result, err
}
```

- [x] **Step 3: 测试+Commit**

### 4.8 S10.3.5: 实现 plugin.yaml 规范化

- [x] **Step 1: 创建 plugin_yaml.go**

实现：
- `buildTeamSkillsPublishZip(skillDir string) ([]byte, string, error)`
  - 解析 SKILL.md 提取 meta
  - 生成 plugin.yaml
  - 构建规范化 ZIP
  - 计算 SHA256

- [x] **Step 2: 修改 HandleSkillsTeamSkillsHubPublish**

替换直接上传原始 ZIP 为调用 `buildTeamSkillsPublishZip`。

- [x] **Step 3: 测试+Commit**

### 4.9 M10.3.2: 实现 marketplace 缓存清理 + git 同步

- [x] **Step 1: 修改 HandleSkillsMarketplaceRemove**

添加本地缓存目录删除：
```go
repoDir := filepath.Join(sm.workspaceDir, "_marketplace", name)
if dirExists(repoDir) {
    safeRmtree(repoDir)
}
```

- [x] **Step 2: 修改 HandleSkillsMarketplaceToggle**

- 启用时：gitPull（已存在）或 gitClone（不存在）
- 禁用时：safeRmtree 删除本地缓存

- [x] **Step 3: 测试+Commit**

### 4.10 M10.3.3: 实现 Validate 完整校验

- [x] **Step 1: 修改 HandleSkillsTeamSkillsHubValidate**

添加 skill_type 判断 + teamskills 类型的 roles 完整校验。

- [x] **Step 2: 测试+Commit**

### 4.11 M10.3.4: 引入 yaml.v3 完整解析

- [x] **Step 1: 添加依赖**

```bash
go get gopkg.in/yaml.v3
```

- [x] **Step 2: 重写 parseYAMLFrontmatter**

使用 `yaml.Unmarshal` 替代逐行解析。补全默认字段和 tags/allowed_tools 类型转换。

- [x] **Step 3: 测试+Commit**

### 4.12 M10.3.5: matchHost 添加后缀匹配

- [x] **Step 1: 修改 matchHost**

在 `matchHost` 函数中添加 `. 前缀` 规则的处理：
```go
// 后缀匹配（对齐 Python: rule.startswith(".") → host.endswith(rule)）
if strings.HasPrefix(pattern, ".") {
    return strings.HasSuffix(host, pattern)
}
```

- [x] **Step 2: 测试+Commit**

### 4.13 M10.3.6: 实现代理环境变量上下文

- [x] **Step 1: 创建 proxy_context.go**

```go
// skillnetNetworkContext 在 SkillNet 调用期间临时设置代理环境变量，调用结束后恢复。
// 对齐 Python: _skillnet_network_context
func skillnetNetworkContext() func() {
    proxyURL := os.Getenv("FREE_SEARCH_PROXY_URL")
    if proxyURL == "" {
        return func() {} // 无需设置
    }
    // 保存原始值
    origHTTPProxy := os.Getenv("HTTP_PROXY")
    origHTTPSProxy := os.Getenv("HTTPS_PROXY")
    origAllProxy := os.Getenv("ALL_PROXY")
    // 设置代理
    os.Setenv("HTTP_PROXY", proxyURL)
    os.Setenv("HTTPS_PROXY", proxyURL)
    os.Setenv("ALL_PROXY", proxyURL)
    // 返回恢复函数
    return func() {
        os.Setenv("HTTP_PROXY", origHTTPProxy)
        os.Setenv("HTTPS_PROXY", origHTTPSProxy)
        os.Setenv("ALL_PROXY", origAllProxy)
    }
}
```

- [x] **Step 2: 在 SkillNet 搜索/评估/下载调用前使用**

```go
restore := skillnetNetworkContext()
defer restore()
```

- [x] **Step 3: 测试+Commit**

### 4.14 M10.3.7: 安装记录添加 version/commit

- [x] **Step 1: 修改 HandleSkillsClawhubDownload 的 AddInstalledPlugin**

添加 `version` 和 `commit` 字段：
```go
sm.AddInstalledPlugin(map[string]any{
    "name":         skillName,
    "marketplace":  "clawhub",
    "source":       "clawhub",
    "version":      toString(meta["version"]),  // 从 SKILL.md meta 获取
    "commit":       "",                          // 对齐 Python 默认空
    "installed_at": time.Now().Format(time.RFC3339),
})
```

- [x] **Step 2: 测试+Commit**

### 4.15 T10.3.2: 实现 refreshAgentDataIndexes

- [x] **Step 1: 创建 agent_data.go**

实现 `generateAgentDataForWorkspace(workspaceRoot string)` 和 `refreshAgentDataIndexes`：
- 遍历工作区目录收集技能信息
- 生成 `agent-data.json`
- refreshAgentDataIndexes 遍历 sm.skillsDir 的 parent + getMirrorSkillsDirs() 的 parent

- [x] **Step 2: 替换空操作**

- [x] **Step 3: 测试+Commit**

---

## Task 5: 10.6.1-2 Prompt Builder 修复

**Files:**
- Modify: `internal/swarm/agents/harness/code/prompt/code_prompt_builder.go`
- Modify: `internal/swarm/agents/harness/common/prompt/prompt_builder.go`

### 5.1 M10.6.1: 统一品牌名为 "UapClawSwarm"

- [x] **Step 1: 修改 Code Intro 中的品牌名**

`code_prompt_builder.go` BuildCodeIntroSection 中：
- `"JiuwenSwarm"` → `"UapClawSwarm"`

- [x] **Step 2: 修改 identity 节中的品牌名**

`prompt_builder.go` BuildAgentIdentityPrompt 中：
- `"UapClaw"` → `"UapClawSwarm"`
- `".uapclaw"` → `".uapclawswarm"`
- `"UapClaw"` → `"UapClawSwarm"`（英文版）

- [x] **Step 3: 编译+测试+Commit**

```bash
go build ./internal/swarm/... && go test ./internal/swarm/... -v -count=1
git add internal/swarm/agents/harness/
git commit -m "fix(M10.6.1): 统一品牌名为 UapClawSwarm"
```

### 5.2 T10.6.1+T10.6.2: readWorkspaceFile 修复

- [x] **Step 1: 添加 Debug 日志**

`prompt_builder.go` readWorkspaceFile 中，`os.IsNotExist(err)` 分支添加：
```go
logger.Debug(logComponent).
    Str("file_path", filePath).
    Msg("文件不存在")
```

- [x] **Step 2: 添加 TrimSpace**

在返回 content 前添加：
```go
content = strings.TrimSpace(content)
```

- [x] **Step 3: 编译+测试+Commit**

```bash
go build ./internal/swarm/... && go test ./internal/swarm/... -v -count=1
git add internal/swarm/agents/harness/common/prompt/prompt_builder.go
git commit -m "fix(T10.6.1+T10.6.2): readWorkspaceFile 添加 Debug 日志和 TrimSpace"
```
