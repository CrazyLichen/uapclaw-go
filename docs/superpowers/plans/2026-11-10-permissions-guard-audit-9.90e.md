# 9.90e permissions 护栏与审计 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 落地 permissions 审计旁路、规则落盘补全、流事件中断分流与会话隔离，并顺带修正 config 层架构下沉与 owner_scopes 命名/逻辑偏差。

**Architecture:** 按依赖链分 12 个 Task 推进。先补 persist 的隐藏前置件 `MergeFileGuardPathRule`，再补 persist 5 函数，然后把 config 层 11 函数迁回 `internal/common/config/` 并收敛 RPC 分发，接着落地 `audit.go` 与 `permission_interaction.go`，用它们替换 9.90d-2 留下的 17 处 `⤵️ 9.90e` 桩，最后接 `stream_event_rail` 的隔离与分流、对齐 `owner_scopes` 命名。

**Tech Stack:** Go 1.x、`github.com/gofrs/flock`（配置持久化锁）、`testify` 断言、`CGO_ENABLED=1` + `-tags "sqlite_fts5 test"` 构建标签。

**Spec:** `docs/superpowers/specs/2026-11-10-permissions-guard-audit-9.90e-design.md`

---

## 文件结构总览

### 新建

| 文件 | 职责 |
|---|---|
| `internal/common/config/permissions.go` | permissions 配置读写 11 函数（对齐 `jiuwenswarm/common/config.py` L1199-1500） |
| `internal/common/config/permissions_test.go` | 上述 11 函数单测 |
| `internal/swarm/agents/harness/common/rails/permissions/audit.go` | 审计事件发出（对齐 `audit.py` 65 行） |
| `internal/swarm/agents/harness/common/rails/permissions/audit_test.go` | 审计单测 |
| `internal/swarm/agents/harness/common/rails/permissions/permission_interaction.go` | 权限交互判据（对齐 `permission_interaction.py` 54 行） |
| `internal/swarm/agents/harness/common/rails/permissions/permission_interaction_test.go` | 判据单测 |
| `tests/integration/swarm/permissions/guard_audit_test.go` | 审计-落盘-重放 / 中断分流 / 隔离恢复 三场景 |

### 修改

| 文件 | 改动 |
|---|---|
| `internal/agentcore/harness/security/patterns.go` | 新增 `MergeFileGuardPathRule`（约 90 行） |
| `internal/agentcore/harness/security/patterns_test.go` | 新增对应测试 |
| `internal/swarm/agents/harness/common/rails/permissions/permissions_persist.go` | 补 5 函数 + 1 哨兵错误 |
| `internal/swarm/agents/harness/common/rails/permissions/permissions_persist_test.go` | 新增测试 |
| `internal/swarm/agents/harness/common/rails/permissions/permissions_config_rpc.go` | 从 23 函数收敛为 1 入口 + 3 私有 |
| `internal/swarm/agents/harness/common/rails/permissions/permissions_config_rpc_test.go` | 重写为 11 分支测试 |
| `internal/swarm/agents/harness/common/rails/permissions/auto_permission/*.go` | 17 处 `⤵️ 9.90e` 桩回填 |
| `internal/swarm/agents/harness/common/rails/permissions/auto_permission/lifecycle.go` | `PersistentAuditWriter any` → 具体类型 |
| `internal/swarm/agents/harness/common/rails/permissions/policy_eval.go` | `send_file` 路径守卫接线 |
| `internal/swarm/agents/harness/common/rails/stream_event_rail.go` | 隔离双写 + 中断分流 |
| `internal/swarm/agents/harness/common/rails/stream_event_rail_test.go` | 隔离测试 |
| `internal/swarm/schema/permission.go` | `SetupPermissionContext` / `CleanupPermissionContext` / `CurrentPermissionOwnerScope` |
| `internal/swarm/schema/permission_test.go` | 对应测试 |
| `internal/swarm/schema/agent.go` | 删 `PermissionContext` 字段与 `WithAgentPermissionContext` |
| `internal/swarm/server/adapter/deep_adapter.go` | 2 处调用改 `SetupPermissionContext` |
| `internal/swarm/server/adapter/deep_adapter_rails.go` | 1 处补 `CurrentPermissionOwnerScope` 使用（可选） |
| `internal/swarm/agents/harness/common/rails/permissions/doc.go` | 文件树补 `audit.go` / `permission_interaction.go` |
| `internal/common/config/doc.go` | 文件树补 `permissions.go` |
| `internal/swarm/agents/harness/common/rails/permissions/auto_permission/doc.go` | 更新 `runtime_bridge.go` 描述 |
| `IMPLEMENTATION_PLAN.md` | 9.90e 行标 ✅ |

### 删除

| 符号 | 位置 |
|---|---|
| `GetPermissionsTools` 等 11 个 | `permissions_config_rpc.go` |
| `loadLiveConfig` / `readYAMLData` / `writeYAMLData` / `validateToolsMap` / `normalizeRuleTools` / `normalizeRuleSeverityAction` / `randomHex` | `permissions_config_rpc.go` |
| `NewPermissionContextFromRequest` | `swarm/schema/permission.go` |
| `WithAgentPermissionContext` | `swarm/schema/agent.go` |
| `AgentRequest.PermissionContext` 字段 | `swarm/schema/agent.go` |

---

## 关键背景：执行前必读

### B1. 两个常量必须一字不差

```go
// 会话隔离标记键。字面量必须与 Python 完全一致，否则跨语言共享的会话状态读不到。
const PermissionRuntimeQuarantinedKey = "_jiuwenswarm_permission_runtime_quarantined_v1"
```

品牌前缀 `jiuwenswarm` **不映射**为 `uapclawswarm`——它是已落盘的持久化键名，改了就读不到历史数据。

### B2. 已有先例：cleanup 用空闭包

项目里 `BindRootDecisionContext` 和 `BindCommandExecution` 都是这个形态：

```go
func BindRootDecisionContext(ctx context.Context, dc *RootDecisionContext) (context.Context, func()) {
	return context.WithValue(ctx, rootContextKey{}, dc), func() {}
}
```

注释统一写「Go 的 context 不可变，丢弃派生 ctx 即恢复上一层绑定」。`SetupPermissionContext` 照此办理。

### B3. 测试命令固定前缀

```bash
export GOPROXY=https://goproxy.cn,direct
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" <包路径>
```

**不带 `-tags "sqlite_fts5 test"` 会导致 memory 包 FTS 用例假失败。**

### B4. 提交纪律

工作区可能有其他任务的未提交改动。**禁止 `git commit -am` / `git add -A`**，必须显式 `git add <文件清单>`，提交后 `git show --stat HEAD` 核对。

---

## Task 1: `MergeFileGuardPathRule`（隐藏前置件）

Python：`openjiuwen/harness/security/permission_engine/approve/persist_rule_merge.py:259-329`（71 行）。
这是 `_merge_file_guard_path_into_permissions` 的硬依赖，Go 侧零落地。

**Files:**
- Modify: `internal/agentcore/harness/security/patterns.go`（在 `MergeFileGuardAccessAllows` 之后追加）
- Test: `internal/agentcore/harness/security/patterns_test.go`

- [ ] **Step 1: 写失败测试**

在 `patterns_test.go` 末尾追加：

```go
// TestMergeFileGuardPathRule_新建条目 验证首次写入 file_guard.paths。
func TestMergeFileGuardPathRule_新建条目(t *testing.T) {
	perms := map[string]any{}

	merged, wrote := MergeFileGuardPathRule(perms, "/data/ws", "allow", "allow", "ask", "prefix")

	if !wrote {
		t.Fatal("wrote = false, want true")
	}
	fg, _ := merged["file_guard"].(map[string]any)
	if fg == nil {
		t.Fatal("file_guard missing")
	}
	if fg["enabled"] != true {
		t.Errorf("enabled = %v, want true", fg["enabled"])
	}
	paths, _ := fg["paths"].([]any)
	if len(paths) != 1 {
		t.Fatalf("paths len = %d, want 1", len(paths))
	}
	entry, _ := paths[0].(map[string]any)
	if entry["path"] != "/data/ws" {
		t.Errorf("path = %v", entry["path"])
	}
	if entry["read"] != "allow" || entry["write"] != "allow" || entry["exec"] != "ask" {
		t.Errorf("axes = %v/%v/%v", entry["read"], entry["write"], entry["exec"])
	}
	if entry["match"] != "prefix" {
		t.Errorf("match = %v", entry["match"])
	}
}

// TestMergeFileGuardPathRule_路径归一化 验证反斜杠转正斜杠并去尾斜杠。
func TestMergeFileGuardPathRule_路径归一化(t *testing.T) {
	merged, wrote := MergeFileGuardPathRule(map[string]any{}, `C:\data\ws\`, "allow", "allow", "ask", "prefix")
	if !wrote {
		t.Fatal("wrote = false")
	}
	fg, _ := merged["file_guard"].(map[string]any)
	paths, _ := fg["paths"].([]any)
	entry, _ := paths[0].(map[string]any)
	if entry["path"] != "C:/data/ws" {
		t.Errorf("path = %v, want C:/data/ws", entry["path"])
	}
}

// TestMergeFileGuardPathRule_空路径不写入 验证归一化后为空则返回 false。
func TestMergeFileGuardPathRule_空路径不写入(t *testing.T) {
	perms := map[string]any{"file_guard": map[string]any{"enabled": false}}
	merged, wrote := MergeFileGuardPathRule(perms, "   /   ", "allow", "allow", "ask", "prefix")
	if wrote {
		t.Error("wrote = true, want false for empty path")
	}
	fg, _ := merged["file_guard"].(map[string]any)
	if fg["enabled"] != false {
		t.Error("empty path should not flip enabled")
	}
}

// TestMergeFileGuardPathRule_同路径按轴升级 验证已有条目的轴升级。
func TestMergeFileGuardPathRule_同路径按轴升级(t *testing.T) {
	perms := map[string]any{
		"file_guard": map[string]any{
			"enabled": true,
			"paths": []any{
				map[string]any{
					"path":  "/data/ws",
					"read":  "ask",
					"write": "ask",
					"exec":  "ask",
					"match": "prefix",
					"note":  "保留我",
				},
			},
		},
	}

	merged, wrote := MergeFileGuardPathRule(perms, "/data/ws", "allow", "ask", "ask", "prefix")

	if !wrote {
		t.Fatal("wrote = false, want true（read 轴从 ask 升到 allow）")
	}
	fg, _ := merged["file_guard"].(map[string]any)
	paths, _ := fg["paths"].([]any)
	if len(paths) != 1 {
		t.Fatalf("paths len = %d, want 1（不应新增条目）", len(paths))
	}
	entry, _ := paths[0].(map[string]any)
	if entry["read"] != "allow" {
		t.Errorf("read = %v, want allow", entry["read"])
	}
	if entry["write"] != "ask" {
		t.Errorf("write = %v, want ask（不降级）", entry["write"])
	}
	if entry["note"] != "保留我" {
		t.Error("existing 其他字段必须保留")
	}
}

// TestMergeFileGuardPathRule_无变更返回false 验证幂等。
func TestMergeFileGuardPathRule_无变更返回false(t *testing.T) {
	perms := map[string]any{
		"file_guard": map[string]any{
			"enabled": true,
			"paths": []any{
				map[string]any{"path": "/data/ws", "read": "allow", "write": "allow", "exec": "ask", "match": "prefix"},
			},
		},
	}

	_, wrote := MergeFileGuardPathRule(perms, "/data/ws", "allow", "allow", "ask", "prefix")

	if wrote {
		t.Error("wrote = true, want false（完全一致时不应报告变更）")
	}
}

// TestMergeFileGuardPathRule_不改动入参 验证深拷贝语义。
func TestMergeFileGuardPathRule_不改动入参(t *testing.T) {
	perms := map[string]any{}
	_, _ = MergeFileGuardPathRule(perms, "/data/ws", "allow", "allow", "ask", "prefix")

	if _, exists := perms["file_guard"]; exists {
		t.Error("入参被修改，Python 用 deepcopy 不应改动原对象")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/security/ -run TestMergeFileGuardPathRule -v
```

预期：编译失败 `undefined: MergeFileGuardPathRule`。

- [ ] **Step 3: 实现**

在 `patterns.go` 的 `MergeFileGuardAccessAllows` 之后追加（保持文件中 `// ──── 导出函数 ────` 区块内）：

```go
// MergeFileGuardPathRule 在 permissions 副本上合并一条 file_guard.paths 规则。
// Python: merge_file_guard_path_rule (persist_rule_merge.py L259-329)
//
// 返回 (merged, wroteAny)：merged 是深拷贝后的新 permissions，wroteAny 表示是否真有变更。
// 传入的 permissions 不被修改（Python 用 deepcopy）。
func MergeFileGuardPathRule(
	permissions map[string]any,
	path string,
	read string,
	write string,
	execAction string,
	match string,
) (map[string]any, bool) {
	pathNorm := strings.ReplaceAll(path, "\\", "/")
	pathNorm = strings.TrimRight(pathNorm, "/")
	if pathNorm == "" {
		return deepCopyPermissions(permissions), false
	}

	perms := deepCopyPermissions(permissions)
	fg, _ := perms["file_guard"].(map[string]any)
	if fg == nil {
		fg = map[string]any{}
		perms["file_guard"] = fg
	}
	fg["enabled"] = true
	paths, _ := fg["paths"].([]any)
	if paths == nil {
		paths = []any{}
		fg["paths"] = paths
	}

	for i, raw := range paths {
		existing, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		existingPath := ""
		if s, ok := existing["path"].(string); ok {
			existingPath = strings.ReplaceAll(s, "\\", "/")
			existingPath = strings.TrimRight(existingPath, "/")
		}
		if existingPath != pathNorm {
			continue
		}
		mergedRead := escalateAxisTowardAllow(existing["read"], read)
		mergedWrite := escalateAxisTowardAllow(existing["write"], write)
		mergedExec := escalateAxisTowardAllow(existing["exec"], execAction)
		mergedMatch := match
		if s, ok := existing["match"].(string); ok && s != "" {
			mergedMatch = s
		}
		unchanged := fmt.Sprint(existing["read"]) == mergedRead &&
			fmt.Sprint(existing["write"]) == mergedWrite &&
			fmt.Sprint(existing["exec"]) == mergedExec
		if existingMatch, ok := existing["match"].(string); ok {
			if existingMatch != mergedMatch {
				unchanged = false
			}
		} else if mergedMatch != "prefix" {
			unchanged = false
		}
		if unchanged {
			return perms, false
		}
		next := map[string]any{}
		for k, v := range existing {
			next[k] = v
		}
		next["path"] = pathNorm
		next["read"] = mergedRead
		next["write"] = mergedWrite
		next["exec"] = mergedExec
		next["match"] = mergedMatch
		paths[i] = next
		logger.Info(logComponent).
			Str("path", pathNorm).
			Str("read", mergedRead).
			Str("write", mergedWrite).
			Str("exec", mergedExec).
			Msg("permission.merge.file_guard")
		return perms, true
	}

	paths = append(paths, map[string]any{
		"path":  pathNorm,
		"read":  read,
		"write": write,
		"exec":  execAction,
		"match": match,
	})
	fg["paths"] = paths
	logger.Info(logComponent).
		Str("path", pathNorm).
		Str("read", read).
		Str("write", write).
		Str("exec", execAction).
		Msg("permission.merge.file_guard")
	return perms, true
}
```

追加到 `// ──── 非导出函数 ────` 区块：

```go
// deepCopyPermissions 深拷贝 permissions 字典。
// Python: copy.deepcopy(permissions)
func deepCopyPermissions(source map[string]any) map[string]any {
	if source == nil {
		return map[string]any{}
	}
	return utils.DeepCopyMap(source)
}

// escalateAxisTowardAllow 按轴向 allow 升级，不降级。
// Python: _escalate_axis_toward_allow(existing, requested)
//
// 严重度：deny(2) > ask(1) > allow(0)。取两者中更宽松的那个。
func escalateAxisTowardAllow(existing any, requested string) string {
	existingStr := ""
	if s, ok := existing.(string); ok {
		existingStr = strings.TrimSpace(s)
	}
	if existingStr == "" {
		return requested
	}
	if axisSeverity(existingStr) <= axisSeverity(requested) {
		return existingStr
	}
	return requested
}

// axisSeverity 返回权限轴严重度，未知值按最严处理。
func axisSeverity(level string) int {
	switch strings.ToLower(level) {
	case "allow":
		return 0
	case "ask":
		return 1
	default:
		return 2
	}
}
```

**检查点**：若 `patterns.go` 已有 `deepCopyPermissions` / `axisSeverity` 同名函数，改用已有实现，不要重复定义。先跑：

```bash
grep -n "func deepCopyPermissions\|func axisSeverity\|func escalateAxisTowardAllow" internal/agentcore/harness/security/patterns.go
```

- [ ] **Step 4: 运行测试确认通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/security/ -run TestMergeFileGuardPathRule -v
```

预期：6 个用例全部 PASS。

- [ ] **Step 5: 检查覆盖率**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -coverprofile=/tmp/p1.out ./internal/agentcore/harness/security/ && go tool cover -func=/tmp/p1.out | grep MergeFileGuardPathRule
```

预期：`MergeFileGuardPathRule` ≥ 85%。

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/harness/security/patterns.go internal/agentcore/harness/security/patterns_test.go
git commit -m "feat(security): MergeFileGuardPathRule 文件守卫路径合并（74 行，9.90e 前置件）

对齐 Python merge_file_guard_path_rule (persist_rule_merge.py L259-329)。
permissions_persist 的 _merge_file_guard_path_into_permissions 依赖此函数。"
git show --stat HEAD
```

---

## Task 2: `permissions_persist.go` 补 5 函数 + 1 哨兵错误

Python：`permissions_persist.py` L81/99/151/155/195/300。

**Files:**
- Modify: `internal/swarm/agents/harness/common/rails/permissions/permissions_persist.go`
- Test: `internal/swarm/agents/harness/common/rails/permissions/permissions_persist_test.go`

- [ ] **Step 1: 写失败测试（哨兵错误 + 3 个简单函数）**

在 `permissions_persist_test.go` 末尾追加：

```go
// TestBuildCommandAllowPattern 验证命令通配符构造。
func TestBuildCommandAllowPattern(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"npm test", "npm test *"},
		{"  npm test  ", "npm test *"},
		{"ls", "ls *"},
	}
	for _, c := range cases {
		if got := BuildCommandAllowPattern(c.in); got != c.want {
			t.Errorf("BuildCommandAllowPattern(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestNormalizeExactAccesses_合法输入 验证归一化与去重保序。
func TestNormalizeExactAccesses_合法输入(t *testing.T) {
	in := [][2]string{
		{"/data/a/", "READ"},
		{`C:\data\b\`, "write"},
		{"/data/a", "read"},
	}
	got, ok := NormalizeExactAccesses(in)
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2（去重后）", len(got))
	}
	if got[0][0] != "/data/a" || got[0][1] != "read" {
		t.Errorf("got[0] = %v", got[0])
	}
	if got[1][0] != "C:/data/b" || got[1][1] != "write" {
		t.Errorf("got[1] = %v", got[1])
	}
}

// TestNormalizeExactAccesses_非法输入 验证任一不合格整体失败。
func TestNormalizeExactAccesses_非法输入(t *testing.T) {
	cases := map[string][][2]string{
		"空 path":    {{"", "read"}},
		"非法 action": {{"/data/a", "delete"}},
		"path 全是斜杠": {{"///", "read"}},
	}
	for name, in := range cases {
		if _, ok := NormalizeExactAccesses(in); ok {
			t.Errorf("%s: ok = true, want false", name)
		}
	}
}

// TestNormalizeExactAccesses_空切片 验证空输入返回空且 true。
func TestNormalizeExactAccesses_空切片(t *testing.T) {
	got, ok := NormalizeExactAccesses(nil)
	if !ok {
		t.Fatal("ok = false, want true（空输入合法）")
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

// TestMergeFileGuardPathIntoPermissions 验证原地改写语义。
func TestMergeFileGuardPathIntoPermissions(t *testing.T) {
	perms := map[string]any{}
	wrote := MergeFileGuardPathIntoPermissions(perms, "/data/ws", "allow", "allow", "ask")
	if !wrote {
		t.Fatal("wrote = false, want true")
	}
	fg, _ := perms["file_guard"].(map[string]any)
	if fg == nil {
		t.Fatal("file_guard 必须写回同一 map 引用（Python 用 clear+update）")
	}
	paths, _ := fg["paths"].([]any)
	if len(paths) != 1 {
		t.Fatalf("paths len = %d, want 1", len(paths))
	}
}

// TestErrPermissionApprovalInvalidated_可被errors.Is识别 验证哨兵语义。
func TestErrPermissionApprovalInvalidated_可被errors.Is识别(t *testing.T) {
	wrapped := fmt.Errorf("%w: %s", ErrPermissionApprovalInvalidated, "approval_accesses_changed")
	if !errors.Is(wrapped, ErrPermissionApprovalInvalidated) {
		t.Error("errors.Is 识别失败")
	}
}
```

- [ ] **Step 2: 运行确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/agents/harness/common/rails/permissions/ -run "TestBuildCommandAllowPattern|TestNormalizeExactAccesses|TestMergeFileGuardPathIntoPermissions|TestErrPermissionApprovalInvalidated" -v
```

预期：编译失败，`undefined: BuildCommandAllowPattern` 等。

- [ ] **Step 3: 实现 5 个函数 + 哨兵**

在 `permissions_persist.go` 的 `// ──── 全局变量 ────` 区块加：

```go
// ErrPermissionApprovalInvalidated 一次审批已不满足当前宿主安全检查。
// Python: class PermissionApprovalInvalidated(ValueError) (permissions_persist.py L151)
var ErrPermissionApprovalInvalidated = errors.New("permission approval invalidated")
```

在 `// ──── 导出函数 ────` 区块追加：

```go
// BuildCommandAllowPattern 构建匹配完整命令的通配符模式。
// Python: build_command_allow_pattern (permissions_persist.py L99-101)
func BuildCommandAllowPattern(cmd string) string {
	return strings.TrimSpace(cmd) + " *"
}

// MergeFileGuardPathIntoPermissions 写入 / 更新一条 file_guard.paths。
// Python: _merge_file_guard_path_into_permissions (permissions_persist.py L81-96)
//
// Python 用 clear()+update() 原地改同一 dict 供调用方 dump；Go 的 map 引用语义一致。
func MergeFileGuardPathIntoPermissions(
	permissions map[string]any,
	pathNorm string,
	read string,
	write string,
	execAction string,
) bool {
	merged, wrote := security.MergeFileGuardPathRule(permissions, pathNorm, read, write, execAction, "prefix")
	for key := range permissions {
		delete(permissions, key)
	}
	for key, value := range merged {
		permissions[key] = value
	}
	return wrote
}

// NormalizeExactAccesses 归一化精确访问集合。
// Python: _normalize_exact_accesses (permissions_persist.py L300-320)
//
// 返回 (normalized, ok)：ok=false 表示输入非法，调用方应整体拒绝。
func NormalizeExactAccesses(accesses [][2]string) ([][2]string, bool) {
	normalized := make([][2]string, 0, len(accesses))
	seen := make(map[[2]string]struct{}, len(accesses))
	for _, access := range accesses {
		pathNorm := strings.ReplaceAll(access[0], "\\", "/")
		pathNorm = strings.TrimRight(pathNorm, "/")
		actionNorm := strings.ToLower(strings.TrimSpace(access[1]))
		if pathNorm == "" {
			return nil, false
		}
		switch actionNorm {
		case "read", "write", "exec":
		default:
			return nil, false
		}
		item := [2]string{pathNorm, actionNorm}
		if _, dup := seen[item]; dup {
			continue
		}
		seen[item] = struct{}{}
		normalized = append(normalized, item)
	}
	return normalized, true
}
```

**注意 import**：需要 `errors`、`strings`、`internal/agentcore/harness/security`（检查是否已导入，避免循环依赖——`security` 包不依赖 `rails/permissions`，安全）。

- [ ] **Step 4: 运行确认通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/agents/harness/common/rails/permissions/ -run "TestBuildCommandAllowPattern|TestNormalizeExactAccesses|TestMergeFileGuardPathIntoPermissions|TestErrPermissionApprovalInvalidated" -v
```

预期：全部 PASS。

- [ ] **Step 5: 写 `CheckApprovalAccesses` 的失败测试**

追加到 `permissions_persist_test.go`：

```go
// TestCheckApprovalAccesses_deny抛错 验证策略 deny 时抛哨兵。
func TestCheckApprovalAccesses_deny抛错(t *testing.T) {
	perms := map[string]any{
		"tools": map[string]any{"bash": "deny"},
	}
	_, _, err := CheckApprovalAccesses(perms, "bash", map[string]any{"command": "ls"}, "")
	if !errors.Is(err, ErrPermissionApprovalInvalidated) {
		t.Fatalf("err = %v, want ErrPermissionApprovalInvalidated", err)
	}
}

// TestCheckApprovalAccesses_allow返回级别 验证正常路径。
func TestCheckApprovalAccesses_allow返回级别(t *testing.T) {
	perms := map[string]any{
		"tools": map[string]any{"bash": "allow"},
	}
	level, asks, err := CheckApprovalAccesses(perms, "bash", map[string]any{"command": "ls"}, "")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if level != "allow" {
		t.Errorf("level = %v, want allow", level)
	}
	_ = asks
}

// TestCheckApprovalAccesses_sendFile不可评估抛错 验证 unevaluable 分支。
func TestCheckApprovalAccesses_sendFile不可评估抛错(t *testing.T) {
	perms := map[string]any{}
	_, _, err := CheckApprovalAccesses(perms, SendFileToolName, map[string]any{}, "")
	if err == nil {
		t.Fatal("err = nil, want invalidated（send_file 缺 abs_file_path_list 应 unevaluable）")
	}
	if !errors.Is(err, ErrPermissionApprovalInvalidated) {
		t.Fatalf("err = %v, want ErrPermissionApprovalInvalidated", err)
	}
}
```

- [ ] **Step 6: 运行确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/agents/harness/common/rails/permissions/ -run TestCheckApprovalAccesses -v
```

预期：`undefined: CheckApprovalAccesses`。

- [ ] **Step 7: 实现 `CheckApprovalAccesses`**

```go
// CheckApprovalAccesses 用 Core 的完整策略与宿主访问投影校验一次审批。
// Python: _check_approval_accesses (permissions_persist.py L155-193)
//
// 返回 (level, askAccesses, err)。err 非 nil 时一定是
// ErrPermissionApprovalInvalidated（语义：当前策略已不允许）。
func CheckApprovalAccesses(
	permissions map[string]any,
	name string,
	args map[string]any,
	root string,
) (string, map[[2]string]struct{}, error) {
	engine := security.NewPermissionEngine(permissions, root, nil)
	access := CurrentNativePathAccess(name, args)

	includeExternal := access == nil
	level := engine.EvaluateGlobalPolicyDirectly(name, args, includeExternal)

	checker := fileguard.BuildFileGuardChecker(engine.Config(), root, nil)
	guardName, guardArgs := name, args
	if access != nil {
		guardName, guardArgs = access.GuardTool, access.GuardArgs
	}

	if access != nil && checker != nil {
		if pathResult := checker.Evaluate(guardName, guardArgs); pathResult != nil {
			if level == "" {
				level = pathResult.Permission
			} else {
				level = security.Strictest(level, pathResult.Permission)
			}
		}
	}

	asks := map[[2]string]struct{}{}
	if checker != nil {
		for _, ask := range checker.CollectAskAccesses(guardName, guardArgs) {
			asks[[2]string{ask.Path, ask.Action}] = struct{}{}
		}
	}

	if name == SendFileToolName {
		result := NewSendFilePathGuardEvaluator().Evaluate(permissions, root, nil, args["abs_file_path_list"])
		if result.Status == "unevaluable" || result.Level == "deny" {
			return "", nil, fmt.Errorf("%w: %s", ErrPermissionApprovalInvalidated, result.Reason)
		}
		for _, ask := range result.AskAccesses {
			asks[ask] = struct{}{}
		}
	}

	if level == "deny" {
		return "", nil, fmt.Errorf("%w: %s", ErrPermissionApprovalInvalidated, "current_policy_denied")
	}
	return level, asks, nil
}
```

**签名校正**（执行时按实际代码核对，勿盲抄）：
- `security.NewPermissionEngine` 的实际签名
- `engine.EvaluateGlobalPolicyDirectly` 的参数与返回（可能返回 `(level, reason)`）
- `checker.Evaluate` 的返回结构字段名（`Permission` / `Level`）
- `SendFilePathGuardEvaluator.Evaluate` 的参数顺序（已在 9.90d-1 落地，见 `send_file_path_guard.go:124`）
- `security.PathAction` 字段名（`Path` / `Action`）

先跑：

```bash
grep -n "func NewPermissionEngine\|func (e \*PermissionEngine) EvaluateGlobalPolicyDirectly\|func (c \*FileGuardChecker) Evaluate\|func (e \*SendFilePathGuardEvaluator) Evaluate\|type PathAction" -r internal/agentcore/harness/security/ internal/swarm/agents/harness/common/rails/permissions/
```

- [ ] **Step 8: 运行确认通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/agents/harness/common/rails/permissions/ -run TestCheckApprovalAccesses -v
```

预期：3 个用例 PASS。

- [ ] **Step 9: 写 `PersistExactPermissionAllowRule` 的失败测试**

```go
// TestPersistExactPermissionAllowRule_非法scope 验证空 name 抛错。
func TestPersistExactPermissionAllowRule_非法scope(t *testing.T) {
	err := PersistExactPermissionAllowRule(ExactAllowOptions{
		ToolName:        "",
		ToolArgs:        map[string]any{},
		RaiseOnInvalid:  true,
	})
	if !errors.Is(err, ErrPermissionApprovalInvalidated) {
		t.Fatalf("err = %v, want ErrPermissionApprovalInvalidated", err)
	}
}

// TestPersistExactPermissionAllowRule_sessionOnly缺sessionID 验证参数校验。
func TestPersistExactPermissionAllowRule_sessionOnly缺sessionID(t *testing.T) {
	err := PersistExactPermissionAllowRule(ExactAllowOptions{
		ToolName:       "bash",
		ToolArgs:       map[string]any{"command": "ls"},
		SessionOnly:    true,
		SessionID:      "",
		RaiseOnInvalid: true,
	})
	if !errors.Is(err, ErrPermissionApprovalInvalidated) {
		t.Fatalf("err = %v, want ErrPermissionApprovalInvalidated", err)
	}
}

// TestPersistExactPermissionAllowRule_validateOnly不写盘 验证只校验不落盘。
func TestPersistExactPermissionAllowRule_validateOnly不写盘(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("JIUWENSWARM_DATA_DIR", dir)

	err := PersistExactPermissionAllowRule(ExactAllowOptions{
		ToolName:      "bash",
		ToolArgs:      map[string]any{"command": "ls"},
		WorkspaceRoot: dir,
		ValidateOnly:  true,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("validateOnly 不应写盘，实际产生 %d 个文件", len(entries))
	}
}

// TestPersistExactPermissionAllowRule_raiseOnInvalid为false返回nil 验证静默模式。
func TestPersistExactPermissionAllowRule_raiseOnInvalid为false返回nil(t *testing.T) {
	err := PersistExactPermissionAllowRule(ExactAllowOptions{
		ToolName:       "",
		RaiseOnInvalid: false,
	})
	if err != nil {
		t.Fatalf("err = %v, want nil（raise_on_invalid=false 应静默）", err)
	}
}
```

- [ ] **Step 10: 运行确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/agents/harness/common/rails/permissions/ -run TestPersistExactPermissionAllowRule -v
```

预期：`undefined: PersistExactPermissionAllowRule`。

- [ ] **Step 11: 实现 `PersistExactPermissionAllowRule`**

先定义选项结构体（放 `// ──── 结构体 ────` 区块）：

```go
// ExactAllowOptions 一次精确放行落盘的参数。
// Python: persist_exact_permission_allow_rule 的形参与关键字参数
type ExactAllowOptions struct {
	// ToolName 工具名
	ToolName string
	// ToolArgs 工具参数（dict 或 JSON 字符串）
	ToolArgs map[string]any
	// AskAccesses 审批时确认的访问集合
	AskAccesses [][2]string
	// SessionID 会话标识，sessionOnly 时必填
	SessionID string
	// WorkspaceRoot 工作区根，空则取默认工作区
	WorkspaceRoot string
	// SessionOnly 仅写 session 层（false 写 user 层）
	SessionOnly bool
	// AutoMode 自动审查模式
	AutoMode bool
	// ValidateOnly 只校验不写盘
	ValidateOnly bool
	// RaiseOnInvalid 无效时返回错误（false 则静默返回 nil）
	RaiseOnInvalid bool
	// ExpectedGlobalPermissions 期望的全局配置快照，非空时比对
	ExpectedGlobalPermissions map[string]any
}
```

实现（`// ──── 导出函数 ────` 区块）：

```go
// PersistExactPermissionAllowRule 校验一次审批并保存标准的 User 或 Session 增量。
// Python: persist_exact_permission_allow_rule (permissions_persist.py L195-299)
func PersistExactPermissionAllowRule(opts ExactAllowOptions) error {
	err := persistExactPermissionAllowRule(opts)
	if err == nil {
		return nil
	}
	if !opts.RaiseOnInvalid {
		return nil
	}
	if errors.Is(err, ErrPermissionApprovalInvalidated) {
		return err
	}
	return fmt.Errorf("%w: %s", ErrPermissionApprovalInvalidated, "approval_validation_failed")
}
```

非导出主体（`// ──── 非导出函数 ────` 区块）按 Python 14 步顺序：

```go
// persistExactPermissionAllowRule 的 14 步主体。
func persistExactPermissionAllowRule(opts ExactAllowOptions) error {
	// 1-3. 参数归一与校验
	name := strings.TrimSpace(opts.ToolName)
	args := normalizeToolArgs(opts.ToolArgs)
	accesses, ok := NormalizeExactAccesses(opts.AskAccesses)
	if name == "" || !ok {
		return fmt.Errorf("%w: %s", ErrPermissionApprovalInvalidated, "invalid_approval_scope")
	}
	if opts.SessionOnly && opts.SessionID == "" {
		return fmt.Errorf("%w: %s", ErrPermissionApprovalInvalidated, "invalid_approval_scope")
	}
	root := opts.WorkspaceRoot
	if root == "" {
		root = workspace.WorkspaceDir()
	}

	// 4. 存储锁 + 三层读取
	unlock, err := PermissionStorageLock(opts.SessionID)
	if err != nil {
		return err
	}
	defer unlock()

	globalPerms, user, session, err := ReadPermissionLayersLocked(opts.SessionID, nil)
	if err != nil {
		return err
	}

	// 5. installed_policy_changed 检查
	if opts.ExpectedGlobalPermissions != nil {
		globalPerms = config.ResolveEnvVars(globalPerms, nil).(map[string]any)
		if !opts.ValidateOnly && !reflect.DeepEqual(globalPerms, opts.ExpectedGlobalPermissions) {
			return fmt.Errorf("%w: %s", ErrPermissionApprovalInvalidated, "installed_policy_changed")
		}
	}

	// 6. baseline 计算
	compose := func(userLayer, sessionLayer map[string]any) map[string]any {
		return ComposeHostEffectivePermissions(globalPerms, userLayer, sessionLayer, opts.SessionID, opts.AutoMode)
	}
	effective := compose(user, session)

	// 7. 当前访问必须被审批覆盖
	currentLevel, currentAsks, err := CheckApprovalAccesses(effective, name, args, root)
	if err != nil {
		return err
	}
	if !isSubsetOf(currentAsks, accesses) {
		return fmt.Errorf("%w: %s", ErrPermissionApprovalInvalidated, "approval_accesses_changed")
	}

	// 8. 只校验模式提前返回
	if opts.ValidateOnly {
		return nil
	}

	// 9. durable 检查：user 侧授权必须在本 session 已有授权之外仍成立
	baseline := effective
	if !opts.SessionOnly {
		baseline = compose(user, map[string]any{})
	}
	if !opts.AutoMode && !opts.SessionOnly && currentLevel == "allow" {
		durableLevel, durableAsks, err := CheckApprovalAccesses(baseline, name, args, root)
		if err != nil {
			return err
		}
		if durableLevel == "allow" && len(durableAsks) == 0 {
			return nil
		}
	}
	_, targetAsks, err := CheckApprovalAccesses(baseline, name, args, root)
	if err != nil {
		return err
	}
	if !isSubsetOf(targetAsks, accesses) {
		return fmt.Errorf("%w: %s", ErrPermissionApprovalInvalidated, "approval_target_scope_changed")
	}

	// 10. 目标层增量
	current := user
	if opts.SessionOnly {
		current = session
	}
	updated := copyPermissionsMap(current)

	level, _ := security.EvaluateTieredPolicy(baseline, name, args)
	if level == security.PermissionLevelAsk {
		merged, applied := security.MergePermissionAllowRuleIntoPermissions(baseline, name, args)
		if !applied {
			return fmt.Errorf("%w: %s", ErrPermissionApprovalInvalidated, "approval_rule_unavailable")
		}
		mergedTools, _ := merged["tools"].(map[string]any)
		baselineTools, _ := baseline["tools"].(map[string]any)
		if !reflect.DeepEqual(mergedTools[name], baselineTools[name]) {
			allow := toStringSlice(updated["allow_tools"])
			if !containsString(allow, name) {
				allow = append(allow, name)
			}
			updated["allow_tools"] = toAnySlice(allow)
		}
		previous := toAnySliceOfMaps(baseline["approval_overrides"])
		for _, item := range toAnySliceOfMaps(merged["approval_overrides"]) {
			if containsMap(previous, item) {
				continue
			}
			existing := toAnySlice(updated["approval_overrides"])
			existing = append(existing, item)
			updated["approval_overrides"] = existing
		}
	}

	// 11. 路径授权
	if len(targetAsks) > 0 {
		accessesList := make([]security.PathAction, 0, len(targetAsks))
		for key := range targetAsks {
			accessesList = append(accessesList, security.PathAction{Path: key[0], Action: key[1]})
		}
		sort.Slice(accessesList, func(i, j int) bool {
			if accessesList[i].Path != accessesList[j].Path {
				return accessesList[i].Path < accessesList[j].Path
			}
			return accessesList[i].Action < accessesList[j].Action
		})
		next, applied := security.MergeFileGuardAccessAllows(updated, accessesList)
		if !applied {
			return fmt.Errorf("%w: %s", ErrPermissionApprovalInvalidated, "approval_paths_unavailable")
		}
		updated = next
	}

	// 12. 最终校验：写完必须真能 ALLOW
	proposed := compose(updated, map[string]any{})
	if opts.SessionOnly {
		proposed = compose(user, updated)
	}
	proposedLevel, proposedAsks, err := CheckApprovalAccesses(proposed, name, args, root)
	if err != nil {
		return err
	}
	if proposedLevel != "allow" || len(proposedAsks) > 0 {
		return fmt.Errorf("%w: %s", ErrPermissionApprovalInvalidated, "approval_not_covered")
	}

	// 13. 幂等返回
	if reflect.DeepEqual(updated, current) {
		return nil
	}

	// 14. 写盘
	if opts.SessionOnly {
		if !SaveSessionPermissionsLocked(opts.SessionID, updated) {
			logger.Error(logComponent).Msg("permission storage failed")
			return nil
		}
		return nil
	}
	if !SaveUserPermissionsLocked(updated) {
		logger.Error(logComponent).Msg("permission storage failed")
		return nil
	}
	return nil
}
```

辅助函数（`// ──── 非导出函数 ────`）：

```go
// isSubsetOf 判断 asks 是否为 accesses 的子集。
func isSubsetOf(asks map[[2]string]struct{}, accesses [][2]string) bool {
	set := make(map[[2]string]struct{}, len(accesses))
	for _, access := range accesses {
		set[access] = struct{}{}
	}
	for ask := range asks {
		if _, ok := set[ask]; !ok {
			return false
		}
	}
	return true
}

// copyPermissionsMap 浅拷贝一层 permissions 字典。
// Python: deepcopy(current)——Go 侧增量只改顶层键，浅拷贝足够。
func copyPermissionsMap(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
```

**重要**：`toStringSlice` / `toAnySlice` / `containsString` / `toAnySliceOfMaps` / `containsMap` 若包内已有同类助手，直接复用。先查：

```bash
grep -n "func toStringSlice\|func toAnySlice\|func containsString\|func toAnySliceOfMaps\|func containsMap" internal/swarm/agents/harness/common/rails/permissions/*.go
```

- [ ] **Step 12: 运行确认通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/agents/harness/common/rails/permissions/ -run TestPersistExactPermissionAllowRule -v
```

预期：4 个用例 PASS。

- [ ] **Step 13: 全包回归 + 覆盖率**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -coverprofile=/tmp/p2.out ./internal/swarm/agents/harness/common/rails/permissions/
go tool cover -func=/tmp/p2.out | grep -E "BuildCommandAllowPattern|NormalizeExactAccesses|MergeFileGuardPathIntoPermissions|CheckApprovalAccesses|PersistExactPermissionAllowRule"
```

预期：每个函数 ≥ 85%，包整体不低于改动前。

- [ ] **Step 14: 提交**

```bash
git add internal/swarm/agents/harness/common/rails/permissions/permissions_persist.go internal/swarm/agents/harness/common/rails/permissions/permissions_persist_test.go
git commit -m "feat(permissions): persist 补 5 函数 + PermissionApprovalInvalidated

对齐 Python permissions_persist.py：
- build_command_allow_pattern (L99)
- _normalize_exact_accesses (L300)
- _merge_file_guard_path_into_permissions (L81)
- _check_approval_accesses (L155)
- persist_exact_permission_allow_rule (L195，14 步)

go 用哨兵错误 + %w 包装替代 Python 的 ValueError 子类。"
git show --stat HEAD
```

---

## Task 3: config 层 11 函数

Python：`jiuwenswarm/common/config.py` L1199-1500。

**关键**：现有 `GetPermissionsTools` / `GetPermissionsRules` 读路��错误（漏 `["permissions"]` 层），本 Task 一并修正。

**Files:**
- Create: `internal/common/config/permissions.go`
- Create: `internal/common/config/permissions_test.go`
- Modify: `internal/common/config/doc.go`

- [ ] **Step 1: 检查 config 层现有符号避免重名**

```bash
grep -n "^func " internal/common/config/config.go internal/common/config/sections.go internal/common/config/values.go | grep -i permission
```

预期：无 `Permission` 相关导出函数（spec 已核实为 0 命中）。若有，改用 `Permissions` 前缀规划命名。

- [ ] **Step 2: 写失败测试**

新建 `internal/common/config/permissions_test.go`：

```go
package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
)

// setupPermissionsConfig 在临时目录写入 permissions 配置并加载。
func setupPermissionsConfig(t *testing.T, yaml string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("JIUWENSWARM_CONFIG_PATH", path)
	config.SetConfigPath(path)
}

// TestGetPermissionsTools_读到permissions节点 验证读取路径正确（bug 回归）。
func TestGetPermissionsTools_读到permissions节点(t *testing.T) {
	setupPermissionsConfig(t, `
permissions:
  tools:
    bash: allow
    write_file: ask
`)
	got := config.GetPermissionsTools()
	tools, _ := got["tools"].(map[string]any)
	if tools == nil {
		t.Fatal("tools 为 nil，说明读错了配置路径（应为 permissions.tools 而非顶层 tools）")
	}
	if tools["bash"] != "allow" {
		t.Errorf("bash = %v, want allow", tools["bash"])
	}
	if tools["write_file"] != "ask" {
		t.Errorf("write_file = %v, want ask", tools["write_file"])
	}
}

// TestGetPermissionsTools_空配置返回空map 验证兜底。
func TestGetPermissionsTools_空配置返回空map(t *testing.T) {
	setupPermissionsConfig(t, "permissions: {}\n")
	got := config.GetPermissionsTools()
	tools, _ := got["tools"].(map[string]any)
	if tools == nil {
		t.Fatal("tools 为 nil")
	}
	if len(tools) != 0 {
		t.Errorf("tools len = %d, want 0", len(tools))
	}
}

// TestGetPermissionsRules_读到permissions节点 验证读取路径正确（bug 回归）。
func TestGetPermissionsRules_读到permissions节点(t *testing.T) {
	setupPermissionsConfig(t, `
permissions:
  rules:
    - id: r1
      tools: [bash]
      action: deny
`)
	got := config.GetPermissionsRules()
	rules, _ := got["rules"].([]any)
	if len(rules) != 1 {
		t.Fatalf("rules len = %d, want 1（应为 permissions.rules 而非顶层 rules）", len(rules))
	}
	first, _ := rules[0].(map[string]any)
	if first["id"] != "r1" {
		t.Errorf("id = %v, want r1", first["id"])
	}
}

// TestGetPermissionsRules_过滤非dict项 验证只保留 dict。
func TestGetPermissionsRules_过滤非dict项(t *testing.T) {
	setupPermissionsConfig(t, `
permissions:
  rules:
    - id: r1
    - plain_string
    - id: r2
`)
	got := config.GetPermissionsRules()
	rules, _ := got["rules"].([]any)
	if len(rules) != 2 {
		t.Errorf("rules len = %d, want 2（字符串项应被过滤）", len(rules))
	}
}

// TestUpdatePermissionsToolInConfig_合并单键 验证写入与读回一致。
func TestUpdatePermissionsToolInConfig_合并单键(t *testing.T) {
	setupPermissionsConfig(t, "permissions:\n  tools:\n    bash: ask\n")

	got, err := config.UpdatePermissionsToolInConfig("write_file", "allow")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	tools, _ := got["tools"].(map[string]any)
	if tools["write_file"] != "allow" {
		t.Errorf("返回 tools.write_file = %v, want allow", tools["write_file"])
	}
	if tools["bash"] != "ask" {
		t.Errorf("已有键 bash 应保留，实际 %v", tools["bash"])
	}

	// 读回验证落盘
	reloaded := config.GetPermissionsTools()
	reloadedTools, _ := reloaded["tools"].(map[string]any)
	if reloadedTools["write_file"] != "allow" {
		t.Error("配置未落盘")
	}
}

// TestUpdatePermissionsToolInConfig_空名字报错 验证参数校验。
func TestUpdatePermissionsToolInConfig_空名字报错(t *testing.T) {
	setupPermissionsConfig(t, "permissions: {}\n")
	if _, err := config.UpdatePermissionsToolInConfig("  ", "allow"); err == nil {
		t.Fatal("err = nil, want error（空工具名应报错）")
	}
}

// TestDeletePermissionsToolInConfig_键不存在返回false 验证幂等。
func TestDeletePermissionsToolInConfig_键不存在返回false(t *testing.T) {
	setupPermissionsConfig(t, "permissions:\n  tools:\n    bash: ask\n")
	ok, err := config.DeletePermissionsToolInConfig("nonexistent")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if ok {
		t.Error("ok = true, want false")
	}
}

// TestDeletePermissionsToolInConfig_删除成功 验证删除。
func TestDeletePermissionsToolInConfig_删除成功(t *testing.T) {
	setupPermissionsConfig(t, "permissions:\n  tools:\n    bash: ask\n    ls: allow\n")
	ok, err := config.DeletePermissionsToolInConfig("bash")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}
	reloaded := config.GetPermissionsTools()
	tools, _ := reloaded["tools"].(map[string]any)
	if _, exists := tools["bash"]; exists {
		t.Error("bash 应已删除")
	}
	if tools["ls"] != "allow" {
		t.Error("ls 应保留")
	}
}

// TestReplacePermissionsToolsInConfig_整表替换 验证替换语义。
func TestReplacePermissionsToolsInConfig_整表替换(t *testing.T) {
	setupPermissionsConfig(t, "permissions:\n  tools:\n    bash: ask\n    ls: allow\n")
	err := config.ReplacePermissionsToolsInConfig(map[string]any{"curl": "deny"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	reloaded := config.GetPermissionsTools()
	tools, _ := reloaded["tools"].(map[string]any)
	if len(tools) != 1 {
		t.Fatalf("tools len = %d, want 1（整表替换）", len(tools))
	}
	if tools["curl"] != "deny" {
		t.Errorf("curl = %v, want deny", tools["curl"])
	}
}

// TestReplacePermissionsToolsInConfig_非法级别报错 验证校验。
func TestReplacePermissionsToolsInConfig_非法级别报错(t *testing.T) {
	setupPermissionsConfig(t, "permissions: {}\n")
	if err := config.ReplacePermissionsToolsInConfig(map[string]any{"bash": "maybe"}); err == nil {
		t.Fatal("err = nil, want error（maybe 不是合法级别）")
	}
}

// TestCreatePermissionsRuleInConfig_生成id 验证创建。
func TestCreatePermissionsRuleInConfig_生成id(t *testing.T) {
	setupPermissionsConfig(t, "permissions:\n  rules: []\n")
	got, err := config.CreatePermissionsRuleInConfig(map[string]any{
		"tools":  []any{"bash"},
		"action": "deny",
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if id, _ := got["id"].(string); id == "" {
		t.Error("id 为空，应自动生成")
	}
}

// TestDeletePermissionsRuleInConfig_按id删除 验证删除。
func TestDeletePermissionsRuleInConfig_按id删除(t *testing.T) {
	setupPermissionsConfig(t, `
permissions:
  rules:
    - id: r1
      tools: [bash]
    - id: r2
      tools: [ls]
`)
	ok, err := config.DeletePermissionsRuleInConfig("r1")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}
	got := config.GetPermissionsRules()
	rules, _ := got["rules"].([]any)
	if len(rules) != 1 {
		t.Fatalf("rules len = %d, want 1", len(rules))
	}
	remaining, _ := rules[0].(map[string]any)
	if remaining["id"] != "r2" {
		t.Errorf("remaining id = %v, want r2", remaining["id"])
	}
}

// TestGetPermissionsApprovalOverrides_读取节点 验证路径。
func TestGetPermissionsApprovalOverrides_读取节点(t *testing.T) {
	setupPermissionsConfig(t, `
permissions:
  approval_overrides:
    - id: o1
      tools: [bash]
`)
	got := config.GetPermissionsApprovalOverrides()
	overrides, _ := got["approval_overrides"].([]any)
	if len(overrides) != 1 {
		t.Fatalf("overrides len = %d, want 1", len(overrides))
	}
}

// TestDeletePermissionsApprovalOverrideInConfig_按id删除 验证删除。
func TestDeletePermissionsApprovalOverrideInConfig_按id删除(t *testing.T) {
	setupPermissionsConfig(t, `
permissions:
  approval_overrides:
    - id: o1
      tools: [bash]
`)
	ok, err := config.DeletePermissionsApprovalOverrideInConfig("o1")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}
	got := config.GetPermissionsApprovalOverrides()
	overrides, _ := got["approval_overrides"].([]any)
	if len(overrides) != 0 {
		t.Errorf("overrides len = %d, want 0", len(overrides))
	}
}
```

- [ ] **Step 3: 运行确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/common/config/ -run "TestGetPermissions|TestUpdatePermissions|TestDeletePermissions|TestReplacePermissions|TestCreatePermissions" -v
```

预期：编译失败，`undefined: config.GetPermissionsTools` 等。

- [ ] **Step 4: 实现**

新建 `internal/common/config/permissions.go`。**执行时先读 `jiuwenswarm/common/config.py` L1199-1500 的原文**，逐函数一比一翻译，不要凭记忆写。骨架：

```go
package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// permissionsSectionKey permissions 配置节键
	permissionsSectionKey = "permissions"
	// toolsSectionKey tools 子节键
	toolsSectionKey = "tools"
	// rulesSectionKey rules 子节键
	rulesSectionKey = "rules"
	// approvalOverridesSectionKey approval_overrides 子节键
	approvalOverridesSectionKey = "approval_overrides"
	// ownerScopesSectionKey owner_scopes 子节键
	ownerScopesSectionKey = "owner_scopes"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// errEmptyToolName 工具名为空
	errEmptyToolName = errors.New("tool name must be non-empty")
	// errEmptyRuleID 规则 id 为空
	errEmptyRuleID = errors.New("rule id must be non-empty")
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GetPermissionsTools 返回 permissions.tools（原始结构，可能含 legacy dict）。
// Python: get_permissions_tools (config.py L1239-1246)
func GetPermissionsTools() map[string]any {
	cfg := currentConfigMap()
	perms, _ := cfg[permissionsSectionKey].(map[string]any)
	tools, _ := perms[toolsSectionKey].(map[string]any)
	if tools == nil {
		return map[string]any{toolsSectionKey: map[string]any{}}
	}
	copied := make(map[string]any, len(tools))
	for key, value := range tools {
		copied[key] = value
	}
	return map[string]any{toolsSectionKey: copied}
}

// ReplacePermissionsToolsInConfig 整表替换 permissions.tools。
// Python: replace_permissions_tools_in_config (config.py L1248-1259)
func ReplacePermissionsToolsInConfig(tools any) error {
	normalized, err := ValidateToolsMap(tools)
	if err != nil {
		return err
	}
	return UpdateConfig(func(data map[string]any) map[string]any {
		perms, _ := data[permissionsSectionKey].(map[string]any)
		if perms == nil {
			perms = map[string]any{}
			data[permissionsSectionKey] = perms
		}
		perms[toolsSectionKey] = normalized
		return data
	})
}

// UpdatePermissionsToolInConfig 合并单条工具级别到 permissions.tools 并写回。
// Python: update_permissions_tool_in_config (config.py L1261-1292)
func UpdatePermissionsToolInConfig(toolName string, level any) (map[string]any, error) {
	name := strings.TrimSpace(toolName)
	if name == "" {
		return nil, errEmptyToolName
	}
	piece, err := ValidateToolsMap(map[string]any{name: level})
	if err != nil {
		return nil, err
	}
	var result map[string]any
	err = UpdateConfig(func(data map[string]any) map[string]any {
		perms, _ := data[permissionsSectionKey].(map[string]any)
		if perms == nil {
			perms = map[string]any{}
			data[permissionsSectionKey] = perms
		}
		existing, _ := perms[toolsSectionKey].(map[string]any)
		merged := make(map[string]any, len(existing)+1)
		for key, value := range existing {
			merged[key] = value
		}
		merged[name] = piece[name]
		perms[toolsSectionKey] = merged
		result = map[string]any{toolsSectionKey: merged}
		return data
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// DeletePermissionsToolInConfig 从 permissions.tools 删除一个键。
// Python: delete_permissions_tool_in_config (config.py L1294-1342)
func DeletePermissionsToolInConfig(toolName string) (bool, error) {
	name := strings.TrimSpace(toolName)
	if name == "" {
		return false, errEmptyToolName
	}
	deleted := false
	err := UpdateConfig(func(data map[string]any) map[string]any {
		perms, _ := data[permissionsSectionKey].(map[string]any)
		if perms == nil {
			return data
		}
		tools, _ := perms[toolsSectionKey].(map[string]any)
		if tools == nil {
			return data
		}
		keyToDelete := ""
		for key := range tools {
			if strings.TrimSpace(fmt.Sprint(key)) == name {
				keyToDelete = key
				break
			}
		}
		if keyToDelete == "" {
			return data
		}
		delete(tools, keyToDelete)
		deleted = true
		return data
	})
	if err != nil {
		return false, err
	}
	return deleted, nil
}

// GetPermissionsRules 返回 permissions.rules 列表。
// Python: get_permissions_rules (config.py L1344-1351)
func GetPermissionsRules() map[string]any {
	cfg := currentConfigMap()
	perms, _ := cfg[permissionsSectionKey].(map[string]any)
	rules, _ := perms[rulesSectionKey].([]any)
	filtered := make([]any, 0, len(rules))
	for _, rule := range rules {
		if _, ok := rule.(map[string]any); ok {
			filtered = append(filtered, rule)
		}
	}
	return map[string]any{rulesSectionKey: filtered}
}

// GetPermissionsApprovalOverrides 返回 permissions.approval_overrides。
// Python: get_permissions_approval_overrides (config.py L1353-1360)
func GetPermissionsApprovalOverrides() map[string]any {
	cfg := currentConfigMap()
	perms, _ := cfg[permissionsSectionKey].(map[string]any)
	overrides, _ := perms[approvalOverridesSectionKey].([]any)
	filtered := make([]any, 0, len(overrides))
	for _, item := range overrides {
		if _, ok := item.(map[string]any); ok {
			filtered = append(filtered, item)
		}
	}
	return map[string]any{approvalOverridesSectionKey: filtered}
}

// CreatePermissionsRuleInConfig 创建一条 permissions.rules 规则。
// Python: create_permissions_rule_in_config (config.py L1362-1399)
func CreatePermissionsRuleInConfig(rule map[string]any) (map[string]any, error) {
	created := make(map[string]any, len(rule)+1)
	for key, value := range rule {
		created[key] = value
	}
	if id, _ := created["id"].(string); strings.TrimSpace(id) == "" {
		created["id"] = randomHex(8)
	}
	err := UpdateConfig(func(data map[string]any) map[string]any {
		perms, _ := data[permissionsSectionKey].(map[string]any)
		if perms == nil {
			perms = map[string]any{}
			data[permissionsSectionKey] = perms
		}
		rules, _ := perms[rulesSectionKey].([]any)
		perms[rulesSectionKey] = append(rules, created)
		return data
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// UpdatePermissionsRuleInConfig 按 ruleID 打补丁。
// Python: update_permissions_rule_in_config (config.py L1401-1453)
func UpdatePermissionsRuleInConfig(ruleID string, patch map[string]any) (map[string]any, error) {
	id := strings.TrimSpace(ruleID)
	if id == "" {
		return nil, errEmptyRuleID
	}
	var updated map[string]any
	err := UpdateConfig(func(data map[string]any) map[string]any {
		perms, _ := data[permissionsSectionKey].(map[string]any)
		if perms == nil {
			return data
		}
		rules, _ := perms[rulesSectionKey].([]any)
		for index, raw := range rules {
			rule, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if existing, _ := rule["id"].(string); existing != id {
				continue
			}
			next := make(map[string]any, len(rule)+len(patch))
			for key, value := range rule {
				next[key] = value
			}
			for key, value := range patch {
				next[key] = value
			}
			next["id"] = id
			rules[index] = next
			updated = next
			break
		}
		perms[rulesSectionKey] = rules
		return data
	})
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, fmt.Errorf("rule not found: %s", id)
	}
	return updated, nil
}

// DeletePermissionsRuleInConfig 按 ruleID 删除。
// Python: delete_permissions_rule_in_config (config.py L1455-1478)
func DeletePermissionsRuleInConfig(ruleID string) (bool, error) {
	id := strings.TrimSpace(ruleID)
	if id == "" {
		return false, errEmptyRuleID
	}
	deleted := false
	err := UpdateConfig(func(data map[string]any) map[string]any {
		perms, _ := data[permissionsSectionKey].(map[string]any)
		if perms == nil {
			return data
		}
		rules, _ := perms[rulesSectionKey].([]any)
		kept := make([]any, 0, len(rules))
		for _, raw := range rules {
			rule, ok := raw.(map[string]any)
			if ok {
				if existing, _ := rule["id"].(string); existing == id {
					deleted = true
					continue
				}
			}
			kept = append(kept, raw)
		}
		perms[rulesSectionKey] = kept
		return data
	})
	if err != nil {
		return false, err
	}
	return deleted, nil
}

// DeletePermissionsApprovalOverrideInConfig 按 overrideID 删除。
// Python: delete_permissions_approval_override_in_config (config.py L1480-1500)
func DeletePermissionsApprovalOverrideInConfig(overrideID string) (bool, error) {
	id := strings.TrimSpace(overrideID)
	if id == "" {
		return false, errors.New("override id must be non-empty")
	}
	deleted := false
	err := UpdateConfig(func(data map[string]any) map[string]any {
		perms, _ := data[permissionsSectionKey].(map[string]any)
		if perms == nil {
			return data
		}
		overrides, _ := perms[approvalOverridesSectionKey].([]any)
		kept := make([]any, 0, len(overrides))
		for _, raw := range overrides {
			item, ok := raw.(map[string]any)
			if ok {
				if existing, _ := item["id"].(string); existing == id {
					deleted = true
					continue
				}
			}
			kept = append(kept, raw)
		}
		perms[approvalOverridesSectionKey] = kept
		return data
	})
	if err != nil {
		return false, err
	}
	return deleted, nil
}

// UpdatePermissionsOwnerScopesInConfig 更新 permissions.owner_scopes。
// Python: update_permissions_owner_scopes_in_config (config.py L1199-1237)
func UpdatePermissionsOwnerScopesInConfig(ownerScopes map[string]any) error {
	return UpdateConfig(func(data map[string]any) map[string]any {
		perms, _ := data[permissionsSectionKey].(map[string]any)
		if perms == nil {
			perms = map[string]any{}
			data[permissionsSectionKey] = perms
		}
		perms[ownerScopesSectionKey] = ownerScopes
		return data
	})
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ValidateToolsMap 校验并归一化 tools 映射。
// Python: _validate_tools_map
//
// 值仅允许 allow / ask / deny，或 legacy {"*": level}。
func ValidateToolsMap(tools any) (map[string]any, error) {
	raw, ok := tools.(map[string]any)
	if !ok {
		return nil, errors.New("tools must be object")
	}
	normalized := make(map[string]any, len(raw))
	for key, value := range raw {
		name := strings.TrimSpace(fmt.Sprint(key))
		if name == "" {
			continue
		}
		level, err := normalizeToolLevel(value)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", err, name)
		}
		normalized[name] = level
	}
	return normalized, nil
}

// normalizeToolLevel 归一化单个工具的级别。
func normalizeToolLevel(value any) (any, error) {
	// legacy {"*": level}
	if mapping, ok := value.(map[string]any); ok {
		star, _ := mapping["*"].(string)
		return normalizeToolLevel(star)
	}
	level := strings.ToLower(strings.TrimSpace(fmt.Sprint(value)))
	switch level {
	case "allow", "ask", "deny":
		return level, nil
	default:
		return nil, errors.New("invalid tool level")
	}
}

// currentConfigMap 返回当前生效的配置字典。
// Python: get_config() or {}
func currentConfigMap() map[string]any {
	return GetConfigSnapshot()
}

// randomHex 生成 n 字节的随机十六进制串。
func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	return hex.EncodeToString(buf)
}
```

**必须核对的既有函数**（勿重复定义）：

```bash
grep -n "func UpdateConfig\|func GetConfigSnapshot\|func SetConfigPath\|func get_config\|func LoadConfig" -r internal/common/config/
```

若 `UpdateConfig` 的签名不同（例如无返回值或接收 `func(map[string]any) error`），按实际改写。若 `GetConfigSnapshot` 不存在，改用现有读取入口。

- [ ] **Step 5: 运行确认通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/common/config/ -run "TestGetPermissions|TestUpdatePermissions|TestDeletePermissions|TestReplacePermissions|TestCreatePermissions" -v
```

预期：全部 PASS。

- [ ] **Step 6: 覆盖率**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -coverprofile=/tmp/p3.out ./internal/common/config/ && go tool cover -func=/tmp/p3.out | grep -i permission
```

预期：11 个函数均 ≥ 85%。

- [ ] **Step 7: 更新 doc.go 并提交**

在 `internal/common/config/doc.go` 文件树加一行：

```
//	├── permissions.go    # permissions 配置读写（对齐 common/config.py L1199-1500）
```

```bash
git add internal/common/config/permissions.go internal/common/config/permissions_test.go internal/common/config/doc.go
git commit -m "feat(config): permissions 配置读写 11 函数

对齐 Python jiuwenswarm/common/config.py L1199-1500。
这批函数原先被错误实现在 rails/permissions 包内（违反规则 12.1-1 架构层次）。

同时修正两处读取路径 bug：
- GetPermissionsTools 原读 cfg["tools"]，应读 cfg["permissions"]["tools"]
- GetPermissionsRules 原读 cfg["rules"]，应读 cfg["permissions"]["rules"]
读写不对称导致前端权限工具列表读到空数据。"
git show --stat HEAD
```

---

## Task 4: RPC 分发重构 + 删 17 个旧符号

**Files:**
- Modify: `internal/swarm/agents/harness/common/rails/permissions/permissions_config_rpc.go`
- Modify: `internal/swarm/agents/harness/common/rails/permissions/permissions_config_rpc_test.go`

- [ ] **Step 1: 记录重构前的测试基线**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/agents/harness/common/rails/permissions/ -run TestDispatchPermissionsConfigRequest -v 2>&1 | tail -20
```

记录当前用例数与结果，重构后必须全部仍通过。

- [ ] **Step 2: 查出所有外部调用点**

```bash
grep -rn "permissions.GetPermissionsTools\|permissions.GetPermissionsRules\|permissions.CreatePermissionsRuleInConfig\|permissions.UpdatePermissionsRuleInConfig\|permissions.DeletePermissionsRuleInConfig\|permissions.DeletePermissionsApprovalOverrideInConfig\|permissions.GetPermissionsApprovalOverrides\|permissions.UpdatePermissionsOwnerScopesInConfig\|permissions.ReplacePermissionsToolsInConfig\|permissions.UpdatePermissionsToolInConfig\|permissions.DeletePermissionsToolInConfig" --include=*.go internal/ tests/ cmd/ | grep -v "_test.go" | grep -v "permissions_config_rpc.go"
```

预期：无输出，或输出很少。**若有，逐个改为 `configutil.` 调用**。

- [ ] **Step 3: 写新的分发测试**

重写 `permissions_config_rpc_test.go`。保留原有 11 分支测试的断言语义，改为测 `DispatchPermissionsConfigRequest`：

```go
// TestDispatchPermissionsConfigRequest_工具读取 验证 PERMISSIONS_TOOLS_GET。
func TestDispatchPermissionsConfigRequest_工具读取(t *testing.T) {
	ok, payload := DispatchPermissionsConfigRequest("permissions.tools.get", map[string]any{})
	if !ok {
		t.Fatalf("ok = false, payload = %v", payload)
	}
	if _, exists := payload["tools"]; !exists {
		t.Error("payload 缺 tools 键")
	}
}

// TestDispatchPermissionsConfigRequest_未知方法 验证兜底。
func TestDispatchPermissionsConfigRequest_未知方法(t *testing.T) {
	ok, payload := DispatchPermissionsConfigRequest("permissions.unknown", map[string]any{})
	if ok {
		t.Fatal("ok = true, want false（未知方法应失败）")
	}
	if payload["code"] != "BAD_REQUEST" {
		t.Errorf("code = %v, want BAD_REQUEST", payload["code"])
	}
}

// TestDispatchPermissionsConfigRequest_参数校验错误文案 验证与 Python 一致。
func TestDispatchPermissionsConfigRequest_参数校验错误文案(t *testing.T) {
	cases := []struct {
		method  string
		params  map[string]any
		wantErr string
	}{
		{"permissions.tools.set", map[string]any{"tools": "not_a_map"}, "tools must be object"},
		{"permissions.tools.update", map[string]any{}, "tool is required"},
		{"permissions.tools.update", map[string]any{"tool": "bash"}, "level is required"},
	}
	for _, c := range cases {
		ok, payload := DispatchPermissionsConfigRequest(c.method, c.params)
		if ok {
			t.Errorf("%s: ok = true, want false", c.method)
			continue
		}
		if payload["error"] != c.wantErr {
			t.Errorf("%s: error = %v, want %q", c.method, payload["error"], c.wantErr)
		}
	}
}
```

**方法名映射**：`ReqMethod` 的字符串值需从现有代码确认。查：

```bash
grep -rn "PERMISSIONS_TOOLS_GET\|permissions.tools.get" --include=*.go internal/swarm/schema/ | head
```

- [ ] **Step 4: 运行确认新旧测试并存通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/agents/harness/common/rails/permissions/ -run TestDispatchPermissionsConfigRequest -v
```

- [ ] **Step 5: 改写 `permissions_config_rpc.go`**

目标结构（**保持 1 个导出入口 + 3 个私有**）：

```go
package permissions

import (
	"github.com/uapclaw/uapclaw-go/internal/common/config"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	reqMethodPermissionsToolsGet                 = "permissions.tools.get"
	reqMethodPermissionsToolsSet                 = "permissions.tools.set"
	reqMethodPermissionsToolsUpdate              = "permissions.tools.update"
	reqMethodPermissionsToolsUpdateUser          = "permissions.tools.update_user"
	reqMethodPermissionsToolsDelete              = "permissions.tools.delete"
	reqMethodPermissionsRulesGet                 = "permissions.rules.get"
	reqMethodPermissionsRulesCreate              = "permissions.rules.create"
	reqMethodPermissionsRulesUpdate              = "permissions.rules.update"
	reqMethodPermissionsRulesDelete              = "permissions.rules.delete"
	reqMethodPermissionsApprovalOverridesGet     = "permissions.approval_overrides.get"
	reqMethodPermissionsApprovalOverridesDelete  = "permissions.approval_overrides.delete"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// DispatchPermissionsConfigRequest 执行一条 permissions 配置 RPC。
// Python: dispatch_permissions_config_request (permissions_config_rpc.py L58-221)
func DispatchPermissionsConfigRequest(reqMethod string, params map[string]any) (bool, map[string]any) {
	switch reqMethod {
	case reqMethodPermissionsToolsGet:
		return dispatchPermissionsToolsGet()
	case reqMethodPermissionsToolsSet:
		return dispatchPermissionsToolsSet(params)
	case reqMethodPermissionsToolsUpdate:
		return dispatchPermissionsToolsUpdate(params)
	case reqMethodPermissionsToolsUpdateUser:
		return dispatchPermissionsToolsUpdateUser(params)
	case reqMethodPermissionsToolsDelete:
		return dispatchPermissionsToolsDelete(params)
	case reqMethodPermissionsRulesGet:
		return dispatchPermissionsRulesGet()
	case reqMethodPermissionsRulesCreate:
		return dispatchPermissionsRulesCreate(params)
	case reqMethodPermissionsRulesUpdate:
		return dispatchPermissionsRulesUpdate(params)
	case reqMethodPermissionsRulesDelete:
		return dispatchPermissionsRulesDelete(params)
	case reqMethodPermissionsApprovalOverridesGet:
		return dispatchPermissionsApprovalOverridesGet()
	case reqMethodPermissionsApprovalOverridesDelete:
		return dispatchPermissionsApprovalOverridesDelete(params)
	default:
		return false, permissionsConfigErr("unknown method", "BAD_REQUEST")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// permissionsConfigErr 构造失败载荷。
// Python: _err (permissions_config_rpc.py L38-46)
func permissionsConfigErr(message, code string) map[string]any {
	return map[string]any{"error": message, "code": code}
}

// permissionsConfigOK 构造成功载荷。
// Python: _ok (permissions_config_rpc.py L48-56)
func permissionsConfigOK(payload map[string]any) (bool, map[string]any) {
	if payload == nil {
		payload = map[string]any{}
	}
	return true, payload
}

// dispatchPermissionsToolsGet 处理 permissions.tools.get。
// Python: permissions_config_rpc.py L78-95
func dispatchPermissionsToolsGet() (bool, map[string]any) {
	effective := ComposeHostEffectivePermissions(
		LoadGlobalPermissions(),
		LoadUserPermissions(),
		LoadSessionPermissions(""),
		"",
		false,
	)
	tools, _ := effective["tools"].(map[string]any)
	if tools == nil {
		tools = map[string]any{}
	}
	copied := make(map[string]any, len(tools))
	for key, value := range tools {
		copied[key] = value
	}
	return permissionsConfigOK(map[string]any{"tools": copied})
}

// dispatchPermissionsToolsSet 处理 permissions.tools.set。
// Python: permissions_config_rpc.py L96-128
func dispatchPermissionsToolsSet(params map[string]any) (bool, map[string]any) {
	rawTools, exists := params["tools"]
	if !exists {
		return false, permissionsConfigErr("tools must be object", "BAD_REQUEST")
	}
	tools, ok := rawTools.(map[string]any)
	if !ok {
		return false, permissionsConfigErr("tools must be object", "BAD_REQUEST")
	}
	deny, ask, allow := []string{}, []string{}, []string{}
	for name, raw := range tools {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		level := ""
		if mapping, ok := raw.(map[string]any); ok {
			level, _ = mapping["*"].(string)
		} else {
			level = fmt.Sprint(raw)
		}
		switch strings.ToLower(strings.TrimSpace(level)) {
		case "deny":
			deny = append(deny, trimmed)
		case "ask":
			ask = append(ask, trimmed)
		case "allow":
			allow = append(allow, trimmed)
		}
	}
	saved := UpdateUserPermissions(func(overlay map[string]any) map[string]any {
		overlay["deny_tools"] = toAnySlice(deny)
		overlay["ask_tools"] = toAnySlice(ask)
		overlay["allow_tools"] = toAnySlice(allow)
		return overlay
	})
	if !saved {
		return false, permissionsConfigErr("failed to save user permissions", "BAD_REQUEST")
	}
	return permissionsConfigOK(map[string]any{"ok": true})
}

// dispatchPermissionsToolsUpdate 处理 permissions.tools.update。
// Python: permissions_config_rpc.py L129-139
func dispatchPermissionsToolsUpdate(params map[string]any) (bool, map[string]any) {
	tool := ""
	if s, ok := params["tool"].(string); ok {
		tool = strings.TrimSpace(s)
	}
	if tool == "" {
		if s, ok := params["name"].(string); ok {
			tool = strings.TrimSpace(s)
		}
	}
	if tool == "" {
		return false, permissionsConfigErr("tool is required", "BAD_REQUEST")
	}
	level, exists := params["level"]
	if !exists {
		return false, permissionsConfigErr("level is required", "BAD_REQUEST")
	}
	payload, err := config.UpdatePermissionsToolInConfig(tool, level)
	if err != nil {
		return false, permissionsConfigErr(err.Error(), "BAD_REQUEST")
	}
	return permissionsConfigOK(payload)
}

// 其余 7 个 dispatch 函数按 Python 对应行号逐个翻译，结构同上。
// 每个函数：参数校验 → 委托 config 层 → 构造载荷。
```

**剩下 7 个 dispatch 函数必须完整实现，不可留空**。执行时按 `permissions_config_rpc.py` 对应行号逐个翻译：

| 函数 | Python 行 |
|---|---|
| `dispatchPermissionsToolsUpdateUser` | L140-156 |
| `dispatchPermissionsToolsDelete` | L157-167 |
| `dispatchPermissionsRulesGet` | L168-170 |
| `dispatchPermissionsRulesCreate` | L171-179 |
| `dispatchPermissionsRulesUpdate` | L180-189 |
| `dispatchPermissionsRulesDelete` | L190-197 |
| `dispatchPermissionsApprovalOverridesGet` | L198-200 |
| `dispatchPermissionsApprovalOverridesDelete` | L201-... |

- [ ] **Step 6: 删除旧符号**

删除以下（用 `Edit` 逐个删，或重写整个文件后确认无残留）：

`GetPermissionsTools` / `ReplacePermissionsToolsInConfig` / `UpdatePermissionsToolInConfig` / `DeletePermissionsToolInConfig` / `GetPermissionsRules` / `GetPermissionsApprovalOverrides` / `UpdatePermissionsOwnerScopesInConfig` / `CreatePermissionsRuleInConfig` / `UpdatePermissionsRuleInConfig` / `DeletePermissionsRuleInConfig` / `DeletePermissionsApprovalOverrideInConfig` / `loadLiveConfig` / `readYAMLData` / `writeYAMLData` / `validateToolsMap` / `normalizeRuleTools` / `normalizeRuleSeverityAction` / `randomHex` / `errPayload` / `toolNameFromParams` / `ParseParams`（若属于 config 职责）。

**保留**：`DispatchPermissionsConfigRequest` 及 3 个私有 + 所有 dispatch* 函数。

- [ ] **Step 7: 编译 + 验证锚点**

```bash
go build ./... && go vet ./...
grep -rn "func GetPermissionsTools\|func GetPermissionsRules\|func CreatePermissionsRuleInConfig" --include=*.go internal/swarm/
```

预期：编译通过；grep 无输出（已全部迁到 `internal/common/config/`）。

- [ ] **Step 8: 全包回归**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/agents/harness/common/rails/permissions/... ./internal/common/config/
```

预期：全绿。

- [ ] **Step 9: 提交**

```bash
git add internal/swarm/agents/harness/common/rails/permissions/permissions_config_rpc.go internal/swarm/agents/harness/common/rails/permissions/permissions_config_rpc_test.go
git commit -m "refactor(permissions): config_rpc 从 23 函数收敛为 1 入口 + 3 私有

对齐 Python permissions_config_rpc.py 结构：单个
dispatch_permissions_config_request 内按 11 个 ReqMethod 分发，
业务逻辑全部委托 config 层。

原先 11 个配置读写函数实现在 rails 包内，等于把 config 层职责
下沉到 rails 层，违反规则 12.1-1（Python 在 A 层做的事 Go 必须还在 A 层）。
这 11 个函数已在上一提交迁到 internal/common/config/permissions.go，
本次删除旧实现。

文件行数：745 → 约 250。"
git show --stat HEAD
```

---

## Task 5: `audit.go`（审计旁路）

Python：`audit.py` 65 行。

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/permissions/audit.go`
- Create: `internal/swarm/agents/harness/common/rails/permissions/audit_test.go`

- [ ] **Step 1: 写失败测试**

```go
package permissions

import (
	"os"
	"path/filepath"
	"testing"
)

// newAuditFacts 构造测试用决策事实。
//
// ⚠️ ToolDecisionFacts 没有 ToolName / ToolCategory 顶层字段，
// 工具名与分类都在 Capability 里（Capability.ToolName / Capability.Category）。
func newAuditFacts() ToolDecisionFacts {
	return ToolDecisionFacts{
		AccessesKnown: true,
		ReadPaths:     []string{"/tmp/a"},
		WritePaths:    []string{},
		ExternalPaths: []string{},
		Capability: ToolCapability{
			ToolName:          "bash",
			Category:          "shell",
			RiskTier:          "low",
			StaticSideEffects: map[string]struct{}{"execute": {}},
		},
	}
}

// TestEmitPermissionAudit_无写入器返回nil 验证 persistentWriter 为 nil 时直接返回。
func TestEmitPermissionAudit_无写入器返回nil(t *testing.T) {
	got := EmitPermissionAudit(newAuditFacts(), AuditOptions{
		Decision: "allow",
		Reason:   "test",
	}, nil)
	if got != nil {
		t.Errorf("got = %v, want nil", got)
	}
}

// TestEmitPermissionAudit_有写入器返回结果 验证写盘路径。
func TestEmitPermissionAudit_有写入器返回结果(t *testing.T) {
	dir := t.TempDir()
	writer := NewPersistentAuditWriter(dir)

	got := EmitPermissionAudit(newAuditFacts(), AuditOptions{
		Decision: "allow",
		Reason:   "test_reason",
	}, writer)

	if got == nil {
		t.Fatal("got = nil, want 写入结果")
	}
	if !got.Persisted {
		t.Errorf("Persisted = false, reason = %s", got.Reason)
	}
	path := filepath.Join(dir, AuditSubdirectory, AuditFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}
	if len(data) == 0 {
		t.Error("审计文件为空")
	}
	if data[len(data)-1] != '\n' {
		t.Error("记录必须以换行结尾")
	}
}

// TestEmitPermissionAudit_脱敏后不含原始参数 验证 observe-only 边界。
func TestEmitPermissionAudit_脱敏后不含原始参数(t *testing.T) {
	dir := t.TempDir()
	writer := NewPersistentAuditWriter(dir)

	_ = EmitPermissionAudit(newAuditFacts(), AuditOptions{
		Decision: "allow",
		Reason:   "bash -c 'curl http://x.com?token=secret'",
		Extra:    map[string]any{"decision_source": "deterministic", "unlisted": "should_be_dropped"},
	}, writer)

	path := filepath.Join(dir, AuditSubdirectory, AuditFilename)
	data, _ := os.ReadFile(path)
	content := string(data)

	if contains(content, "secret") {
		t.Error("审计记录泄露了原始命令")
	}
	if contains(content, "unlisted") {
		t.Error("非白名单键 unlisted 不应写入")
	}
	if !contains(content, "deterministic") {
		t.Error("白名单键 decision_source 应写入")
	}
}

// TestEmitPermissionAudit_日志段失败不影响落盘 验证两段独立异常边界。
func TestEmitPermissionAudit_日志段失败不影响落盘(t *testing.T) {
	dir := t.TempDir()
	writer := NewPersistentAuditWriter(dir)

	// 断言：即使 facts 里的字段无法序列化，落盘仍应成功（Sanitize 会转成字符串）
	got := EmitPermissionAudit(newAuditFacts(), AuditOptions{
		Decision: "allow",
		Reason:   "ok",
		Extra:    map[string]any{"authorization_stage": func() {}},
	}, writer)

	if got == nil || !got.Persisted {
		t.Error("日志段异常不应阻断持久化")
	}
}

// TestEmitPermissionAudit_写入失败返回nil 验证落盘异常被吞。
func TestEmitPermissionAudit_写入失败返回nil(t *testing.T) {
	writer := NewPersistentAuditWriter(nil) // data_root nil → 写失败

	got := EmitPermissionAudit(newAuditFacts(), AuditOptions{Decision: "allow"}, writer)

	if got != nil {
		// Python 语义：persistent_writer.write 返回值直接透传，即使 persisted=false
		if !errors.Is(nil, nil) {
			_ = got
		}
		if got.Persisted {
			t.Error("Persisted = true, want false（data_root 为 nil）")
		}
	}
}

// contains 辅助判断子串。
func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

// indexOf 返回子串位置。
func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
```

**检查点**：若 `contains` / `indexOf` 与包内既有助手重名，改用既有函数或 `strings.Contains`。

- [ ] **Step 2: 运行确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/agents/harness/common/rails/permissions/ -run TestEmitPermissionAudit -v
```

预期：`undefined: EmitPermissionAudit`。

- [ ] **Step 3: 实现**

新建 `audit.go`：

```go
// Package permissions 的审计旁路实现。
//
// 本文件对齐 Python jiuwenswarm/agents/harness/common/rails/permissions/audit.py（65 行）。
// 审计是 observe-only：任何失败都不得改变权限判定结果。
package permissions

import (
	"encoding/json"
	"sort"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AuditOptions 一次审计事件的决策事实。
// Python: emit_permission_audit 的关键字参数
type AuditOptions struct {
	// Decision 决策结果（allow / ask / deny）
	Decision string
	// Reason 决策原因码
	Reason string
	// Degraded 是否处于降级状态
	Degraded bool
	// GrantID 授权标识，空串不写入记录
	GrantID string
	// GrantReason 授权原因，空串不写入记录
	GrantReason string
	// Extra 白名单附加字段，非白名单键被丢弃
	Extra map[string]any
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// EmitPermissionAudit 发出自动权限审计事件，不含原始工具参数。
// Python: emit_permission_audit (audit.py L21-65)
//
// 两段独立的异常边界：日志段与持久化段互不牵连，任一段失败都返回 nil，
// 绝不让审计改变权限判定结果（observe-only）。
func EmitPermissionAudit(
	facts ToolDecisionFacts,
	opts AuditOptions,
	persistentWriter *PersistentAuditWriter,
) *PersistentAuditWriteResult {
	emitAuditLog(facts, opts)
	if persistentWriter == nil {
		return nil
	}
	return writePersistentAudit(persistentWriter, facts, opts)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// emitAuditLog 记审计日志。
// Python: audit.py L33-50 的第一段 try/except
//
// 序列化或处理器异常一律吞掉——审计失败不应产生噪声。
func emitAuditLog(facts ToolDecisionFacts, opts AuditOptions) {
	defer func() {
		// Python: except Exception: pass
		_ = recover()
	}()
	event := BuildSanitizedAuditFields(
		facts,
		opts.Decision,
		opts.Reason,
		opts.Degraded,
		opts.GrantID,
		opts.GrantReason,
		opts.Extra,
	)
	encoded, err := marshalSortedCompact(event)
	if err != nil {
		return
	}
	logger.Info(logComponent).
		Msg("permission_audit " + encoded)
}

// writePersistentAudit 落盘审计记录。
// Python: audit.py L51-65 的第二段 try/except
func writePersistentAudit(
	writer *PersistentAuditWriter,
	facts ToolDecisionFacts,
	opts AuditOptions,
) *PersistentAuditWriteResult {
	defer func() {
		_ = recover()
	}()
	result := writer.Write(
		facts,
		opts.Decision,
		opts.Reason,
		opts.Degraded,
		opts.GrantID,
		opts.GrantReason,
		opts.Extra,
	)
	return &result
}

// marshalSortedCompact 序列化为键有序、无空格的 JSON。
// Python: json.dumps(event, sort_keys=True, separators=(",", ":"))
//
// Go 的 encoding/json 对 map[string]any 天然按键名字典序输出，
// 与 sort_keys=True 一致；这里再显式归一一次，避免嵌套类型差异。
func marshalSortedCompact(value any) (string, error) {
	normalized := normalizeForSortedJSON(value)
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// normalizeForSortedJSON 递归归一化为可稳定序列化的结构。
func normalizeForSortedJSON(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		result := make(map[string]any, len(typed))
		for _, key := range keys {
			result[key] = normalizeForSortedJSON(typed[key])
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = normalizeForSortedJSON(item)
		}
		return result
	case []string:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = item
		}
		return result
	default:
		return value
	}
}
```

**已核实的既有签名**（写计划时已查，无需再查）：

```go
// persistent_audit.go L139
func NewPersistentAuditWriter(dataRoot any) *PersistentAuditWriter

// persistent_audit.go L173 —— 位置参数，不是选项结构体
func (w *PersistentAuditWriter) Write(
	facts ToolDecisionFacts,
	decision string,
	reason string,
	degraded bool,
	grantID string,
	grantReason string,
	extra map[string]any,
) (result PersistentAuditWriteResult)   // ⚠️ 返回值不是指针

// persistent_audit.go L265 —— 同样是位置参数
func BuildSanitizedAuditFields(
	facts ToolDecisionFacts,
	decision string,
	reason string,
	degraded bool,
	grantID string,
	grantReason string,
	extra map[string]any,
) map[string]any
```

`logComponent` 定义在 `owner_scopes.go:51`：

```go
var logComponent = logger.ComponentPermissions
```

**因此上面代码里的两处调用已按位置参数写好**，`Write` 返回的是**值**，需取地址后返回：

```go
result := writer.Write(facts, opts.Decision, opts.Reason, opts.Degraded, opts.GrantID, opts.GrantReason, opts.Extra)
return &result
```

`strings` 包若未用到则删除 import。

- [ ] **Step 4: 运行确认通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/agents/harness/common/rails/permissions/ -run TestEmitPermissionAudit -v
```

预期：5 个用例 PASS。

- [ ] **Step 5: 覆盖率 + 提交**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -coverprofile=/tmp/p5.out ./internal/swarm/agents/harness/common/rails/permissions/ && go tool cover -func=/tmp/p5.out | grep -i audit
```

`EmitPermissionAudit` ≥ 85%。

```bash
git add internal/swarm/agents/harness/common/rails/permissions/audit.go internal/swarm/agents/harness/common/rails/permissions/audit_test.go
git commit -m "feat(permissions): audit.go 审计旁路（65 行）

对齐 Python audit.py。两段独立异常边界：
- 日志段失败不影响持久化
- 持久化失败不影响日志
两段异常一律吞掉，绝不改变权限判定结果（observe-only）。

依赖 9.90d-1 已落地的 persistent_audit.go（BuildSanitizedAuditFields /
PersistentAuditWriter）。"
git show --stat HEAD
```

---

## Task 6: `permission_interaction.go`（权限交互判据）

Python：`permission_interaction.py` 54 行。

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/permissions/permission_interaction.go`
- Create: `internal/swarm/agents/harness/common/rails/permissions/permission_interaction_test.go`

- [ ] **Step 1: 写失败测试**

```go
package permissions

import (
	"reflect"
	"testing"
)

// TestContainsPermissionInteraction_顶层键命中 验证判据 3。
func TestContainsPermissionInteraction_顶层键命中(t *testing.T) {
	value := map[string]any{"tool_invocation_key": "abc"}
	if !ContainsPermissionInteraction(value) {
		t.Error("got false, want true（顶层 tool_invocation_key）")
	}
}

// TestContainsPermissionInteraction_metadata键命中 验证判据 1。
func TestContainsPermissionInteraction_metadata键命中(t *testing.T) {
	value := map[string]any{
		"metadata": map[string]any{"tool_invocation_key": "abc"},
	}
	if !ContainsPermissionInteraction(value) {
		t.Error("got false, want true（metadata.tool_invocation_key）")
	}
}

// TestContainsPermissionInteraction_manualApproval为true命中 验证判据 2。
func TestContainsPermissionInteraction_manualApproval为true命中(t *testing.T) {
	value := map[string]any{
		"metadata": map[string]any{"manual_approval_supported": true},
	}
	if !ContainsPermissionInteraction(value) {
		t.Error("got false, want true（manual_approval_supported == true）")
	}
}

// TestContainsPermissionInteraction_manualApproval为数字1不命中 验证 is 语义。
func TestContainsPermissionInteraction_manualApproval为数字1不命中(t *testing.T) {
	value := map[string]any{
		"metadata": map[string]any{"manual_approval_supported": 1},
	}
	if ContainsPermissionInteraction(value) {
		t.Error("got true, want false（Python 用 is，1 不等于 True）")
	}
}

// TestContainsPermissionInteraction_嵌套list命中 验证递归。
func TestContainsPermissionInteraction_嵌套list命中(t *testing.T) {
	value := map[string]any{
		"state": []any{
			map[string]any{"foo": "bar"},
			map[string]any{"metadata": map[string]any{"tool_invocation_key": "k"}},
		},
	}
	if !ContainsPermissionInteraction(value) {
		t.Error("got false, want true（列表深层命中）")
	}
}

// TestContainsPermissionInteraction_无关结构不命中 验证普通中断不误判。
func TestContainsPermissionInteraction_无关结构不命中(t *testing.T) {
	value := map[string]any{
		"result_type": "interrupt",
		"state": []any{
			map[string]any{"question": "选哪个？", "options": []any{"A", "B"}},
		},
	}
	if ContainsPermissionInteraction(value) {
		t.Error("got true, want false（普通问答不含权限字段）")
	}
}

// TestContainsPermissionInteraction_循环引用不死循环 验证 id 去重。
func TestContainsPermissionInteraction_循环引用不死循环(t *testing.T) {
	cyclic := map[string]any{}
	cyclic["self"] = cyclic

	done := make(chan bool, 1)
	go func() {
		ContainsPermissionInteraction(cyclic)
		done <- true
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("循环引用导致死循环")
	}
}

// TestContainsPermissionInteraction_超深嵌套failSafe 验证扫满 128 层返回 true。
func TestContainsPermissionInteraction_超深嵌套failSafe(t *testing.T) {
	var value any = map[string]any{"leaf": "x"}
	for i := 0; i < 200; i++ {
		value = map[string]any{"next": value}
	}
	if !ContainsPermissionInteraction(value) {
		t.Error("got false, want true（扫满 128 层未完成应按有权限交互处理）")
	}
}

// TestContainsPermissionInteraction_结构体载体命中 验证对象分支。
func TestContainsPermissionInteraction_结构体载体命中(t *testing.T) {
	value := permissionInteractionCarrier{
		Metadata: map[string]any{"tool_invocation_key": "k"},
	}
	if !ContainsPermissionInteraction(value) {
		t.Error("got false, want true（带 Metadata() 的对象）")
	}
}

// permissionInteractionCarrier 测试用载体。
type permissionInteractionCarrier struct {
	Metadata map[string]any
}

// Metadata 返回元数据。
func (c permissionInteractionCarrier) Metadata() map[string]any { return c.Metadata }

// TestContainsPermissionInteraction_基本类型不命中 验证标量。
func TestContainsPermissionInteraction_基本类型不命中(t *testing.T) {
	for _, value := range []any{"text", 42, 3.14, true, nil} {
		if ContainsPermissionInteraction(value) {
			t.Errorf("%v（%v）got true, want false", value, reflect.TypeOf(value))
		}
	}
}
```

需要 import `time`。

- [ ] **Step 2: 运行确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/agents/harness/common/rails/permissions/ -run TestContainsPermissionInteraction -v
```

预期：`undefined: ContainsPermissionInteraction`。

- [ ] **Step 3: 实现**

新建 `permission_interaction.go`：

```go
// Package permissions 的权限交互判据实现。
//
// 本文件对齐 Python jiuwenswarm/agents/harness/common/rails/permissions/permission_interaction.py（54 行）。
// 用于流事件层判断一条嵌套消息里是否携带宿主权限定位信息，决定走
// 「权限中断快照撤销」还是「普通中断投影」。
package permissions

import (
	"reflect"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// PermissionRuntimeQuarantinedKey 会话隔离标记在 session state 中的键。
	// Python: PERMISSION_RUNTIME_QUARANTINED_KEY (permission_interaction.py L10-12)
	//
	// 键名含 v1 后缀用于将来平滑迁移；字面量必须与 Python 一字不差，
	// 否则跨语言共享的会话状态读不到。品牌前缀 jiuwenswarm 不映射。
	PermissionRuntimeQuarantinedKey = "_jiuwenswarm_permission_runtime_quarantined_v1"

	// permissionInteractionScanLimit 扫描循环硬上限。
	// Python: permission_interaction.py L20 `for _ in range(128)`
	permissionInteractionScanLimit = 128

	// toolInvocationKeyField 权限请求身份指纹字段名
	toolInvocationKeyField = "tool_invocation_key"

	// manualApprovalSupportedField 人工审批标志字段名
	manualApprovalSupportedField = "manual_approval_supported"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// permissionInteractionNestedFields 对象载体上待下钻的字段名。
// Python: permission_interaction.py L47 `for name in ("payload", "value", "state")`
var permissionInteractionNestedFields = []string{"payload", "value", "state"}

// ──────────────────────────── 导出函数 ────────────────────────────

// ContainsPermissionInteraction 判断一个嵌套值是否携带宿主权限定位信息。
// Python: contains_permission_interaction (permission_interaction.py L15-51)
//
// 三条判据（短路顺序不可调换）：
//  1. metadata 含 tool_invocation_key
//  2. metadata.manual_approval_supported 严格等于 true
//  3. 对象顶层含 tool_invocation_key
//
// 扫满 128 层仍未完成时返回 true（fail-safe）：无法确认干净就按有权限交互处理。
func ContainsPermissionInteraction(value any) bool {
	pending := []any{value}
	seen := make(map[uintptr]struct{}, 16)

	for i := 0; i < permissionInteractionScanLimit; i++ {
		if len(pending) == 0 {
			return false
		}
		item := pending[len(pending)-1]
		pending = pending[:len(pending)-1]

		if identity, ok := valueIdentity(item); ok {
			if _, dup := seen[identity]; dup {
				continue
			}
			seen[identity] = struct{}{}
		}

		switch typed := item.(type) {
		case map[string]any:
			if mappingHasPermissionKey(typed["metadata"]) {
				return true
			}
			if _, exists := typed[toolInvocationKeyField]; exists {
				return true
			}
			for _, nested := range typed {
				pending = append(pending, nested)
			}
		case []any:
			for _, nested := range typed {
				pending = append(pending, nested)
			}
		default:
			if mappingHasPermissionKey(readObjectMetadata(item)) {
				return true
			}
			for _, field := range permissionInteractionNestedFields {
				if nested := readObjectField(item, field); nested != nil {
					pending = append(pending, nested)
				}
			}
		}
	}
	return len(pending) > 0
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// mappingHasPermissionKey 判断一个值是否为携带权限定位信息的映射。
// Python: permission_interaction.py L29-34 / L41-46
func mappingHasPermissionKey(value any) bool {
	mapping, ok := value.(map[string]any)
	if !ok {
		return false
	}
	if _, exists := mapping[toolInvocationKeyField]; exists {
		return true
	}
	flag, ok := mapping[manualApprovalSupportedField].(bool)
	return ok && flag
}

// readObjectMetadata 读取对象载体上的 metadata。
// Python: getattr(item, "metadata", None)
func readObjectMetadata(item any) any {
	return readObjectField(item, "metadata")
}

// readObjectField 通过反射读取对象字段或调用同名方法。
// Python: getattr(item, name, None)
func readObjectField(item any, name string) any {
	if item == nil {
		return nil
	}
	value := reflect.ValueOf(item)
	method := value.MethodByName(exportedName(name))
	if method.IsValid() && method.Type().NumIn() == 0 && method.Type().NumOut() == 1 {
		result := method.Call(nil)
		if len(result) == 1 && !result[0].IsNil() {
			return result[0].Interface()
		}
		return nil
	}
	if value.Kind() == reflect.Ptr {
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return nil
	}
	field := value.FieldByName(exportedName(name))
	if !field.IsValid() || !field.CanInterface() {
		return nil
	}
	return field.Interface()
}

// exportedName 把 snake_case 字段名转成 Go 导出名。
// 例：tool_invocation_key → ToolInvocationKey
func exportedName(name string) string {
	parts := strings.Split(name, "_")
	var builder strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}
		builder.WriteString(strings.ToUpper(part[:1]))
		builder.WriteString(part[1:])
	}
	return builder.String()
}

// valueIdentity 取值的身份标识用于去重。
// Python: id(item)
//
// 偏差 D-1：Go 的 any 装箱后取不到原地址，仅对 map / slice / ptr 可取。
// 取不到时返回 ok=false，该次不做去重（128 层上限已能兜住绝大多数循环）。
func valueIdentity(value any) (uintptr, bool) {
	if value == nil {
		return 0, false
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Map, reflect.Slice, reflect.Ptr:
		if reflected.IsNil() {
			return 0, false
		}
		return reflected.Pointer(), true
	default:
		return 0, false
	}
}
```

需 import `strings`。

**核对既有符号**：`logComponent` 是否已在包内定义（`audit.go` 也用它）。查：

```bash
grep -rn "const logComponent" internal/swarm/agents/harness/common/rails/permissions/
```

- [ ] **Step 4: 运行确认通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/agents/harness/common/rails/permissions/ -run TestContainsPermissionInteraction -v
```

预期：11 个用例 PASS。

- [ ] **Step 5: 覆盖率 + 提交**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -coverprofile=/tmp/p6.out ./internal/swarm/agents/harness/common/rails/permissions/ && go tool cover -func=/tmp/p6.out | grep -i "ContainsPermissionInteraction\|mappingHasPermissionKey\|exportedName\|valueIdentity"
```

全部 ≥ 85%。

```bash
git add internal/swarm/agents/harness/common/rails/permissions/permission_interaction.go internal/swarm/agents/harness/common/rails/permissions/permission_interaction_test.go
git commit -m "feat(permissions): permission_interaction 权限交互判据（54 行）

对齐 Python permission_interaction.py。三条判据（短路顺序不可换）：
1. metadata.tool_invocation_key 存在
2. metadata.manual_approval_supported 严格等于 true（1 不算）
3. 对象顶层 tool_invocation_key 存在

128 层扫描上限 + 扫满返回 len(pending)>0（fail-safe，无法确认干净
就按有权限交互处理）。

偏差 D-1：Python 的 id(item) 去重，Go 仅对 map/slice/ptr 可取地址。"
git show --stat HEAD
```

---

## Task 7: 17 处 `⤵️ 9.90e` 桩回填

**Files:**
- Modify: `internal/swarm/agents/harness/common/rails/permissions/auto_permission/runtime_bridge.go`
- Modify: `internal/swarm/agents/harness/common/rails/permissions/auto_permission/domain_grant_audit.go`
- Modify: `internal/swarm/agents/harness/common/rails/permissions/auto_permission/reviewer_execution.go`
- Modify: `internal/swarm/agents/harness/common/rails/permissions/auto_permission/reviewer_override_gate.go`
- Modify: `internal/swarm/agents/harness/common/rails/permissions/auto_permission/reviewer_override_consume.go`
- Modify: `internal/swarm/agents/harness/common/rails/permissions/auto_permission/deny_response.go`
- Modify: `internal/swarm/agents/harness/common/rails/permissions/auto_permission/manual_authorization.go`
- Modify: `internal/swarm/agents/harness/common/rails/permissions/auto_permission/deterministic.go`
- Modify: `internal/swarm/agents/harness/common/rails/permissions/auto_permission/artifact_tracking_capture.go`
- Modify: `internal/swarm/agents/harness/common/rails/permissions/auto_permission/lifecycle.go`
- Modify: `internal/swarm/agents/harness/common/rails/permissions/policy_eval.go`
- Test: 各文件对应 `_test.go`

- [ ] **Step 1: 列出全部待回填点**

```bash
grep -rn "⤵️ 9.90e" --include=*.go internal/
```

预期 17 处（spec §4.4 清单）。逐个记录文件:行号。

- [ ] **Step 2: 改 `lifecycle.go` 的字段类型**

```go
// PersistentAuditWriter 持久化审计写入器。
// Python: self.persistent_audit_writer
//
// 9.90e 前为 any（audit.py 未落地），现改为具体类型。
persistentAuditWriter *permissions.PersistentAuditWriter
```

同时改：
- `AutoPermissionRailOptions.PersistentAuditWriter` 字段类型 → `*permissions.PersistentAuditWriter`
- `initLifecycle` 的赋值
- `UpdateConfig` 里 `r.persistentAuditWriter.(*permissions.PersistentAuditWriter)` 的类型断言改为直接使用
- `lifecycle_test.go` 里 `permissions.NewPersistentAuditWriter(nil)` 的用法（已是具体类型，不用改）

- [ ] **Step 3: 改 `runtime_bridge.go` 两个核心桩**

```go
// auditRecord 一次审计记录。
// Python: _emit_audit 的 **kwargs
type auditRecord struct {
	// Decision 决策结果
	Decision string
	// Reason 决策原因码
	Reason string
	// Degraded 是否降级
	Degraded bool
	// Extra 白名单附加字段
	Extra map[string]any
}

// emitAudit 发出审计事件。
// Python: AutoPermissionRuntimeBridgeMixin._emit_audit (runtime_bridge.py L62-67)
func (r *AutoPermissionRail) emitAudit(facts permissions.ToolDecisionFacts, record auditRecord) *permissions.PersistentAuditWriteResult {
	return permissions.EmitPermissionAudit(facts, permissions.AuditOptions{
		Decision: record.Decision,
		Reason:   record.Reason,
		Degraded: record.Degraded,
		Extra:    record.Extra,
	}, r.persistentAuditWriter)
}

// emitPermissionResultObservation 观察一次待人工审批或最终 rail 结果。
// Python: AutoPermissionRuntimeBridgeMixin._emit_permission_result_observation (runtime_bridge.py L69-...)
func (r *AutoPermissionRail) emitPermissionResultObservation(
	facts permissions.ToolDecisionFacts,
	result any,
	sessionID string,
	toolCallID string,
	decisionSource string,
) {
	outcome := permissionTerminalOutcome(result)
	source := permissionTerminalDecisionSource(decisionSource)
	if outcome == "" {
		r.emitAudit(facts, auditRecord{
			Decision: permissions.AskLevel,
			Reason:   "permission_manual_wait",
			Degraded: false,
			Extra: map[string]any{
				"record_kind":          "permission_pending",
				"decision_source":      source,
				"authorization_stage":  "manual_approval",
				"stage_outcome":        "manual_wait",
			},
		})
		return
	}
	r.emitAudit(facts, auditRecord{
		Decision: outcome,
		Reason:   "permission_terminal",
		Extra: map[string]any{
			"record_kind":          "permission_terminal",
			"authorization_outcome": outcome,
			"decision_source":      source,
		},
	})
	_ = sessionID
	_ = toolCallID
}
```

**必须核对**：`permissionTerminalOutcome` / `permissionTerminalDecisionSource` 是否已在包内存在（Python `runtime_bridge.py` 有这两个私有函数）。若不存在，需一并实现。查：

```bash
grep -rn "func permissionTerminalOutcome\|func permissionTerminalDecisionSource\|askLevel\|AskLevel" internal/swarm/agents/harness/common/rails/permissions/
```

`AskLevel` 常量在 `auto_decision.go`（Python `ASK_LEVEL`），确认导出状态。

- [ ] **Step 4: 改其余 12 处桩调用**

每处形态一致：把 `})  // ⤵️ 9.90e 回填桩体` 换成真实 `r.emitAudit(facts, auditRecord{...})`，字段按各自 Python 原文填。逐文件处理：

```bash
grep -n "⤵️ 9.90e" internal/swarm/agents/harness/common/rails/permissions/auto_permission/domain_grant_audit.go
grep -n "⤵️ 9.90e" internal/swarm/agents/harness/common/rails/permissions/auto_permission/reviewer_execution.go
grep -n "⤵️ 9.90e" internal/swarm/agents/harness/common/rails/permissions/auto_permission/reviewer_override_gate.go
grep -n "⤵️ 9.90e" internal/swarm/agents/harness/common/rails/permissions/auto_permission/reviewer_override_consume.go
grep -n "⤵️ 9.90e" internal/swarm/agents/harness/common/rails/permissions/auto_permission/deny_response.go
grep -n "⤵️ 9.90e" internal/swarm/agents/harness/common/rails/permissions/auto_permission/manual_authorization.go
grep -n "⤵️ 9.90e" internal/swarm/agents/harness/common/rails/permissions/auto_permission/deterministic.go
grep -n "⤵️ 9.90e" internal/swarm/agents/harness/common/rails/permissions/auto_permission/artifact_tracking_capture.go
```

对应 Python 文件逐个核对 `_emit_audit(...)` 的实参：

| Go 文件 | Python 源 | 审计点数 |
|---|---|---|
| `domain_grant_audit.go` | `_auto_permission/domain_grant_audit.py` | 3 |
| `reviewer_execution.go` | `_auto_permission/reviewer_execution.py` | 4 |
| `reviewer_override_gate.go` | `_auto_permission/reviewer_override_gate.py` | 1 |
| `reviewer_override_consume.go` | `_auto_permission/reviewer_override_consume.py` | 1 |
| `deny_response.go` | `_auto_permission/deny_response.py` | 1 |
| `manual_authorization.go` | `_auto_permission/manual_authorization.py` | 1 |
| `deterministic.go` | `_auto_permission/deterministic.py` | 1 |
| `artifact_tracking_capture.go` | `_auto_permission/artifact_tracking_capture.py` | 1（偏差 V-16：派生 ctx 回传） |

**关键**：`artifact_tracking_capture.go` 那处不是审计桩，是「派生 ctx 需回传给调用方才能真正生效」（偏差 V-16）。按 Python `artifact_tracking_capture.py` 的实现处理 ctx 传递。

- [ ] **Step 5: `policy_eval.go` 的 send_file 接线**

现有：

```go
// send_file 路径守卫：⤵️ 9.90e 回填
// SendFilePathGuardEvaluator 未实现，path_result 恒 nil。
// 9.90e 落地 send_file_path_guard.py（315 行）后在此构造并调用。
var pathResult *SendFilePathGuardResult
if normalizedToolName == SendFileToolName {
	pathResult = e.sendFilePathGuardResult(effectiveConfig, toolArgs, engine)
}
```

`SendFilePathGuardEvaluator` 已在 9.90d-1 落地，只需删掉 3 行注释并确认 `sendFilePathGuardResult` 已实现。查：

```bash
grep -n "func (e \*OpenJiuwenPolicyEvaluator) sendFilePathGuardResult" internal/swarm/agents/harness/common/rails/permissions/policy_eval.go
```

若无实现，按 Python `policy_eval.py` 对应段补。

- [ ] **Step 6: 验证锚点清空**

```bash
grep -rn "⤵️ 9.90e" --include=*.go internal/
```

预期：无输出。

- [ ] **Step 7: 全包回归**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/agents/harness/common/rails/permissions/... 
```

预期：全绿。若某些测试断言「桩返回 nil」（如 `runtime_bridge_test.go:39` 的 `want nil（⤵️ 9.90e 回填）`），需改为断言真实审计行为。

- [ ] **Step 8: 提交**

```bash
git add internal/swarm/agents/harness/common/rails/permissions/
git commit -m "feat(permissions): 17 处 ⤵️ 9.90e 桩全部回填

audit.go 落地后，把 9.90d-2 保留的 no-op 桩换成真实 EmitPermissionAudit 调用：
- runtime_bridge.go 2 处（emitAudit / emitPermissionResultObservation）
- domain_grant_audit.go 3 处
- reviewer_execution.go 4 处
- reviewer_override_gate.go / reviewer_override_consume.go / deny_response.go
  / manual_authorization.go / deterministic.go 各 1 处
- artifact_tracking_capture.go（偏差 V-16 派生 ctx 回传）
- lifecycle.go persistentAuditWriter 字段 any → 具体类型
- policy_eval.go send_file 路径守卫接线

验证：grep -rn '⤵️ 9.90e' internal/ 输出为空。"
git show --stat HEAD
```

---

## Task 8: `stream_event_rail.go` 会话隔离与中断分流

Python：`stream_event_rail.py` L317 / L675-701 / L771 / L811-820 / L916。

**Files:**
- Modify: `internal/swarm/agents/harness/common/rails/stream_event_rail.go`
- Test: `internal/swarm/agents/harness/common/rails/stream_event_rail_test.go`

**背景**：工作区已有未提交改动（`rootPermissionQueue` 字段 + setter）。本 Task 在同一改动内接完，不单独提交那部分。

- [ ] **Step 1: 写失败测试**

在 `stream_event_rail_test.go` 追加：

```go
// fakeSessionFacade 测试用 session。
type fakeSessionFacade struct {
	state map[string]any
}

// UpdateState 更新状态。
func (f *fakeSessionFacade) UpdateState(data map[string]any) {
	if f.state == nil {
		f.state = map[string]any{}
	}
	for key, value := range data {
		f.state[key] = value
	}
}

// GetState 读取状态。
func (f *fakeSessionFacade) GetState(key state.StateKey) (any, error) {
	if f.state == nil {
		return nil, nil
	}
	return f.state[key.String()], nil
}

// TestQuarantineSession_双写 验证内存与 session state 都写入。
func TestQuarantineSession_双写(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	session := &fakeSessionFacade{}

	rail.QuarantineSession("sess-1", session)

	if !isQuarantinedInMemory(rail, "sess-1") {
		t.Error("内存集合未写入")
	}
	if session.state[permissions.PermissionRuntimeQuarantinedKey] != true {
		t.Error("session state 未写入隔离标记")
	}
}

// TestRaiseIfQuarantined_内存命中 验证进程内判定。
func TestRaiseIfQuarantined_内存命中(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	rail.QuarantineSession("sess-1", nil)

	err := rail.RaiseIfQuarantined("sess-1", nil)
	if err == nil {
		t.Fatal("err = nil, want 隔离错误")
	}
}

// TestRaiseIfQuarantined_持久化命中后回填内存 验证重启恢复场景。
func TestRaiseIfQuarantined_持久化命中后回填内存(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	session := &fakeSessionFacade{state: map[string]any{
		permissions.PermissionRuntimeQuarantinedKey: true,
	}}

	err := rail.RaiseIfQuarantined("sess-1", session)
	if err == nil {
		t.Fatal("err = nil, want 隔离错误（从 session state 发现）")
	}
	if !isQuarantinedInMemory(rail, "sess-1") {
		t.Error("从持久化发现后应回填内存集合（后续走快速路径）")
	}
}

// TestRaiseIfQuarantined_未隔离返回nil 验证正常路径。
func TestRaiseIfQuarantined_未隔离返回nil(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	session := &fakeSessionFacade{}

	if err := rail.RaiseIfQuarantined("sess-1", session); err != nil {
		t.Errorf("err = %v, want nil", err)
	}
}

// TestRaiseIfQuarantined_空sessionID归一为default 验证 sid 归一。
func TestRaiseIfQuarantined_空sessionID归一为default(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	rail.QuarantineSession("", nil)

	if err := rail.RaiseIfQuarantined("  ", nil); err == nil {
		t.Error("err = nil, want 隔离错误（空串与空白应归一到 default）")
	}
}

// isQuarantinedInMemory 读取内存集合（测试用，需与实现字段名一致）。
func isQuarantinedInMemory(rail *JiuClawStreamEventRail, sessionID string) bool {
	rail.mu.Lock()
	defer rail.mu.Unlock()
	_, exists := rail.quarantinedSessions[rail.sidKey(sessionID)]
	return exists
}
```

- [ ] **Step 2: 运行确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/agents/harness/common/rails/ -run "TestQuarantineSession|TestRaiseIfQuarantined" -v
```

预期：`undefined: QuarantineSession`。

- [ ] **Step 3: 实现**

字段（`JiuClawStreamEventRail` 结构体内）：

```go
// quarantinedSessions 已隔离会话集合（进程内快速判定）。
// Python: _quarantined_sessions (stream_event_rail.py L317)
quarantinedSessions map[string]struct{}
```

构造函数里初始化：

```go
quarantinedSessions: make(map[string]struct{}),
```

方法（导出函数区块，放在 `CleanupSession` 之后）：

```go
// QuarantineSession 永久隔离已损坏的运行时，直到所属会话被销毁。
// Python: quarantine_session (stream_event_rail.py L675-684)
//
// 双写：进程内集合（快速判定）+ 会话状态（跨重启）。
func (r *JiuClawStreamEventRail) QuarantineSession(
	sessionID string,
	session sessioninterfaces.SessionFacade,
) {
	sid := r.sidKey(sessionID)
	r.mu.Lock()
	r.quarantinedSessions[sid] = struct{}{}
	r.mu.Unlock()

	if session != nil {
		session.UpdateState(map[string]any{
			permissions.PermissionRuntimeQuarantinedKey: true,
		})
	}
	r.abort(sid)
}

// RaiseIfQuarantined 在 fail-closed 清理失败后拒绝新工作。
// Python: raise_if_quarantined (stream_event_rail.py L686-701)
func (r *JiuClawStreamEventRail) RaiseIfQuarantined(
	sessionID string,
	session sessioninterfaces.SessionFacade,
) error {
	sid := r.sidKey(sessionID)

	persisted := false
	if session != nil {
		if value, err := session.GetState(state.StringKey(permissions.PermissionRuntimeQuarantinedKey)); err == nil {
			persisted, _ = value.(bool)
		}
	}

	r.mu.Lock()
	_, inMemory := r.quarantinedSessions[sid]
	if inMemory || persisted {
		r.quarantinedSessions[sid] = struct{}{}
	}
	r.mu.Unlock()

	if inMemory || persisted {
		return fmt.Errorf("session %s is quarantined", sid)
	}
	return nil
}
```

**必须核对的既有方法**：
- `r.sidKey(...)` — 对应 Python `_sid_key`。查现有实现名：

```bash
grep -n "func (r \*JiuClawStreamEventRail) sidKey\|func (r \*JiuClawStreamEventRail) abort" internal/swarm/agents/harness/common/rails/stream_event_rail.go
```

Python `_sid_key` 是 `str(session_id or "").strip() or "default"`。若无 `sidKey` 则新增：

```go
// sidKey 归一化每会话状态键。
// Python: _sid_key (stream_event_rail.py L524-534)
func (r *JiuClawStreamEventRail) sidKey(sessionID string) string {
	trimmed := strings.TrimSpace(sessionID)
	if trimmed == "" {
		return "default"
	}
	return trimmed
}
```

- [ ] **Step 4: 接 `beforeInvoke` 的检查**

在 `beforeInvoke` 里，`sid` 解析之后、处理输入之前：

```go
sid := resolvedSID // 沿用现有变量
// Python: L771 self.raise_if_quarantined(sid, ctx.session)
if err := r.RaiseIfQuarantined(sid, session); err != nil {
	return err
}
```

- [ ] **Step 5: 接 `cancelInterruptionSnapshot` 的隔离**

现有函数（对应 Python `_cancel_interruption_snapshot`）末尾的异常出口加隔离：

```go
func (r *JiuClawStreamEventRail) cancelInterruptionSnapshot(...) {
	session := cbc.Session()
	sid := r.resolveSID(cbc, session)

	succeeded := false
	defer func() {
		if succeeded {
			return
		}
		// Python: except BaseException: self.quarantine_session(sid, session); raise
		// Go 无 BaseException，panic 与 error 两路都要覆盖（偏差 D-2）。
		r.QuarantineSession(sid, session)
	}()

	session.UpdateState(map[string]any{interruptionKey: nil})
	// ...原有清理逻辑...
	succeeded = true
}
```

**注意**：`defer` 中的 `recover()` 需谨慎——若原函数在 panic 时应让 panic 继续传播，写法为：

```go
defer func() {
	if recovered := recover(); recovered != nil {
		r.QuarantineSession(sid, session)
		panic(recovered) // 重新抛出，保持原语义
	}
	if !succeeded {
		r.QuarantineSession(sid, session)
	}
}()
```

- [ ] **Step 6: 改中断分流**

现有 `projectCompleteInterruption`（对应 Python `_project_complete_interruption`）：

```go
func (r *JiuClawStreamEventRail) projectCompleteInterruption(cbc *sainterfaces.AgentCallbackContext) error {
	queue := r.RootPermissionQueue()
	sid := r.resolveSID(cbc, cbc.Session())
	_ = sid

	if queue == nil {
		// Python: L818
		if permissions.ContainsPermissionInteraction(cbc.Inputs().Result()) {
			return r.cancelInterruptionSnapshot(cbc, nil, "permission_queue_unavailable")
		}
		if r.projectInterruptionSnapshots {
			return r.projectOrdinaryInterruption(cbc)
		}
	}
	// ...原有 queue 非 nil 的路径...
	return nil
}
```

**核对 `cbc.Inputs().Result()` 的实际访问方式**：Go 的 `AgentCallbackContext` 通过 `Inputs()` 返回 `EventInputs` 接口，取 `result` 需类型断言。查现有代码：

```bash
grep -n "cbc.Inputs()" internal/swarm/agents/harness/common/rails/stream_event_rail.go | head
```

按现有模式取。

- [ ] **Step 7: 运行确认通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/agents/harness/common/rails/ -run "TestQuarantineSession|TestRaiseIfQuarantined" -v
```

预期：5 个用例 PASS。

- [ ] **Step 8: 全包回归 + 覆盖率**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -coverprofile=/tmp/p8.out ./internal/swarm/agents/harness/common/rails/
go tool cover -func=/tmp/p8.out | grep -E "QuarantineSession|RaiseIfQuarantined|sidKey"
```

≥ 85%。

- [ ] **Step 9: 提交**

```bash
git add internal/swarm/agents/harness/common/rails/stream_event_rail.go internal/swarm/agents/harness/common/rails/stream_event_rail_test.go
git commit -m "feat(rails): stream_event_rail 会话隔离与中断分流

对齐 Python stream_event_rail.py：
- quarantine_session (L675-684)：双写（进程内 Set + session state）
- raise_if_quarantined (L686-701)：任一为真即隔离，从持久化发现后回填内存
- before_invoke (L771)：新请求先查隔离
- _cancel_interruption_snapshot (L916)：清理失败即隔离
- _project_complete_interruption (L811-820)：queue 缺失时用
  ContainsPermissionInteraction 决定走撤销还是普通投影

偏差 D-2：Python except BaseException 捕获 CancelledError，
Go 用 defer + recover 覆盖 panic 与 error 两路。"
git show --stat HEAD
```

---

## Task 9: `owner_scopes` 命名对齐与逻辑补齐

**Files:**
- Modify: `internal/swarm/schema/permission.go`
- Modify: `internal/swarm/schema/permission_test.go`
- Modify: `internal/swarm/schema/agent.go`
- Modify: `internal/swarm/server/adapter/deep_adapter.go`

**重要修正**：`PermissionContextFromCtx` **保留**（对应 Python `TOOL_PERMISSION_CONTEXT.get()`，3 处调用方都需要完整对象）。只新增 `CurrentPermissionOwnerScope`。

- [ ] **Step 1: 写失败测试**

在 `internal/swarm/schema/permission_test.go` 追加：

```go
// TestSetupPermissionContext_avatarMode建上下文 验证 avatar 分支。
func TestSetupPermissionContext_avatarMode建上下文(t *testing.T) {
	ctx, cleanup := SetupPermissionContext(context.Background(), "chan-1", map[string]any{
		"avatar_mode":       true,
		"principal_user_id": "u1",
	})
	defer cleanup()

	got := PermissionContextFromCtx(ctx)
	if got == nil {
		t.Fatal("got nil, want 上下文（avatar_mode=true）")
	}
	if got.PrincipalUserID != "u1" {
		t.Errorf("PrincipalUserID = %q", got.PrincipalUserID)
	}
	if !got.AvatarMode {
		t.Error("AvatarMode = false")
	}
}

// TestSetupPermissionContext_principalUserID非空建上下文 验证缺口回归。
func TestSetupPermissionContext_principalUserID非空建上下文(t *testing.T) {
	ctx, cleanup := SetupPermissionContext(context.Background(), "chan-1", map[string]any{
		"principal_user_id": "u1",
	})
	defer cleanup()

	got := PermissionContextFromCtx(ctx)
	if got == nil {
		t.Fatal("got nil, want 上下文（principal_user_id 非空时也要建，用于 workspace authority）")
	}
	if got.PrincipalUserID != "u1" {
		t.Errorf("PrincipalUserID = %q", got.PrincipalUserID)
	}
	if got.AvatarMode {
		t.Error("AvatarMode = true, want false")
	}
}

// TestSetupPermissionContext_enableMemory为false建上下文 验证记忆禁用分支。
func TestSetupPermissionContext_enableMemory为false建上下文(t *testing.T) {
	ctx, cleanup := SetupPermissionContext(context.Background(), "chan-1", map[string]any{
		"enable_memory": false,
	})
	defer cleanup()

	got := PermissionContextFromCtx(ctx)
	if got == nil {
		t.Fatal("got nil, want 上下文（enable_memory=false）")
	}
	if got.EnableMemory {
		t.Error("EnableMemory = true, want false")
	}
}

// TestSetupPermissionContext_无相关字段不建上下文 验证原行为不变。
func TestSetupPermissionContext_无相关字段不建上下文(t *testing.T) {
	ctx, cleanup := SetupPermissionContext(context.Background(), "chan-1", map[string]any{})
	defer cleanup()

	if got := PermissionContextFromCtx(ctx); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

// TestCleanupPermissionContext_不panic 验证空闭包语义。
func TestCleanupPermissionContext_不panic(t *testing.T) {
	_, cleanup := SetupPermissionContext(context.Background(), "chan-1", map[string]any{
		"avatar_mode": true,
	})
	CleanupPermissionContext(cleanup)
	CleanupPermissionContext(nil)
}

// TestCurrentPermissionOwnerScope_格式 验证 principal:{channel}:{uid}。
func TestCurrentPermissionOwnerScope_格式(t *testing.T) {
	ctx, cleanup := SetupPermissionContext(context.Background(), "web", map[string]any{
		"principal_user_id": "u42",
	})
	defer cleanup()

	if got := CurrentPermissionOwnerScope(ctx); got != "principal:web:u42" {
		t.Errorf("got %q, want principal:web:u42", got)
	}
}

// TestCurrentPermissionOwnerScope_无principalUser返回空 验证空值。
func TestCurrentPermissionOwnerScope_无principalUser返回空(t *testing.T) {
	ctx, cleanup := SetupPermissionContext(context.Background(), "web", map[string]any{
		"avatar_mode": true,
	})
	defer cleanup()

	if got := CurrentPermissionOwnerScope(ctx); got != "" {
		t.Errorf("got %q, want 空串（无 principal_user_id）", got)
	}
}

// TestCurrentPermissionOwnerScope_channel为空回落default 验证回落。
func TestCurrentPermissionOwnerScope_channel为空回落default(t *testing.T) {
	ctx, cleanup := SetupPermissionContext(context.Background(), "", map[string]any{
		"principal_user_id": "u42",
	})
	defer cleanup()

	if got := CurrentPermissionOwnerScope(ctx); got != "principal:default:u42" {
		t.Errorf("got %q, want principal:default:u42", got)
	}
}
```

- [ ] **Step 2: 运行确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/schema/ -run "TestSetupPermissionContext|TestCleanupPermissionContext|TestCurrentPermissionOwnerScope" -v
```

预期：`undefined: SetupPermissionContext`。

- [ ] **Step 3: 实现三个函数，删 `NewPermissionContextFromRequest`**

在 `permission.go` 的导出函数区块，把 `NewPermissionContextFromRequest` 整个替换为：

```go
// SetupPermissionContext 从请求 metadata 构造 PermissionContext 并注入派生 ctx。
// Python: setup_permission_context (owner_scopes.py L61-97)
//
// Python 返回 contextvars.Token 供 cleanup_permission_context 回滚；
// Go 的 context.WithValue 派生链不可变，丢弃派生 ctx 即恢复上一层绑定，
// 故 cleanup 为空闭包（偏差 D-3，与 BindRootDecisionContext 同惯例）。
//
// 三个建上下文的条件（任一满足）：
//  1. avatar_mode = true
//  2. principal_user_id 非空（workspace authority 需要）
//  3. enable_memory = false（禁用记忆）
func SetupPermissionContext(
	ctx context.Context,
	channelID string,
	metadata map[string]any,
) (context.Context, func()) {
	if metadata == nil {
		metadata = map[string]any{}
	}

	avatarMode, _ := metadata["avatar_mode"].(bool)
	principalUserID, _ := metadata["principal_user_id"].(string)
	principalUserID = strings.TrimSpace(principalUserID)

	memoryDisabled := false
	if value, ok := metadata["enable_memory"]; ok {
		if flag, isBool := value.(bool); isBool && !flag {
			memoryDisabled = true
		}
	}

	if !avatarMode && principalUserID == "" && !memoryDisabled {
		return context.WithValue(ctx, toolPermContextKey{}, nil), func() {}
	}

	permCtx := NewPermissionContextFromDict(metadata)
	permCtx.AvatarMode = avatarMode
	if strings.TrimSpace(permCtx.ChannelID) == "" {
		permCtx.ChannelID = channelID
	}
	return context.WithValue(ctx, toolPermContextKey{}, permCtx), func() {}
}

// CleanupPermissionContext 清理权限上下文。
// Python: cleanup_permission_context (owner_scopes.py L99-102)
//
// Go 的 cleanup 是空闭包（见 SetupPermissionContext 说明），本函数仅保持
// 与 Python 的调用形态一致，调用方仍然可以显式成对书写。
func CleanupPermissionContext(cleanup func()) {
	if cleanup != nil {
		cleanup()
	}
}

// CurrentPermissionOwnerScope 返回可信的请求 owner 令牌，用于工作区溯源。
// Python: current_permission_owner_scope (owner_scopes.py L104-114)
//
// 格式 principal:{channel}:{uid}；无 principal_user_id 时返回空串。
func CurrentPermissionOwnerScope(ctx context.Context) string {
	permCtx := PermissionContextFromCtx(ctx)
	if permCtx == nil {
		return ""
	}
	principalUserID := strings.TrimSpace(permCtx.PrincipalUserID)
	if principalUserID == "" {
		return ""
	}
	channelID := strings.TrimSpace(permCtx.ChannelID)
	if channelID == "" {
		channelID = "default"
	}
	return "principal:" + channelID + ":" + principalUserID
}
```

**保留**：`PermissionContextFromCtx`（对应 `.get()`）、`WithPermissionContextValue`（底层注入手段）。

- [ ] **Step 4: 改 `deep_adapter.go` 两处调用**

`deep_adapter.go:1056` 与 `:1351`：

```go
// 改前
permCtx := schema.NewPermissionContextFromRequest(req.ChannelID, req.Metadata)
if permCtx != nil {
	ctx = schema.WithPermissionContextValue(ctx, permCtx)
} else if req.PermissionContext != nil {
	ctx = schema.WithPermissionContextValue(ctx, req.PermissionContext)
}

// 改后
ctx, cleanupPermCtx := schema.SetupPermissionContext(ctx, req.ChannelID, req.Metadata)
defer schema.CleanupPermissionContext(cleanupPermCtx)
```

- [ ] **Step 5: 删 `WithAgentPermissionContext` 与字段**

`internal/swarm/schema/agent.go`：

1. 删 `PermissionContext *PermissionContext \`json:"permission_context,omitempty"\`` 字段（L45 附近）
2. 删 `WithAgentPermissionContext` 函数（L190 附近）

- [ ] **Step 6: 编译驱动修完全部引用**

```bash
go build ./... 2>&1 | head -30
```

按报错逐个改：测试里的 `NewPermissionContextFromRequest` 全部换成 `SetupPermissionContext`，断言随之调整。

- [ ] **Step 7: 验证锚点 + 全包回归**

```bash
grep -rn "NewPermissionContextFromRequest\|WithAgentPermissionContext" --include=*.go internal/ tests/ cmd/
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/schema/ ./internal/swarm/server/adapter/ ./internal/swarm/agents/harness/common/rails/ ./internal/swarm/agents/harness/common/rails/permissions/
```

预期：grep 无输出；测试全绿。

- [ ] **Step 8: 提交**

```bash
git add internal/swarm/schema/permission.go internal/swarm/schema/permission_test.go internal/swarm/schema/agent.go internal/swarm/server/adapter/deep_adapter.go
git commit -m "refactor(schema): owner_scopes 命名对齐 + principal_user_id 缺口补齐

命名（规则 12.1-4，禁止改名）：
- NewPermissionContextFromRequest → SetupPermissionContext
- 新增 CleanupPermissionContext（空闭包，偏差 D-3）
- 新增 CurrentPermissionOwnerScope（Python 原名）
- 保留 PermissionContextFromCtx（对应 TOOL_PERMISSION_CONTEXT.get()，
  avatar_rail / deep_adapter_rails 3 处需要完整对象）

逻辑缺口（安全影响）：
Python 的 setup_permission_context 在 principal_user_id 非空时也建上下文
（用于 workspace authority）。Go 版只判断 avatar_mode 与 enable_memory，
导致普通 IM 场景下 permCtx 为 nil → permissionSceneHook 直接返回 →
owner_scopes 权限完全不生效。

同时删除零调用死代码：
- WithAgentPermissionContext（全仓 0 调用）
- AgentRequest.PermissionContext 字段

验收：grep -rn 'NewPermissionContextFromRequest' = 0"
git show --stat HEAD
```

---

## Task 10: 集成测试三场景

**Files:**
- Create: `tests/integration/swarm/permissions/guard_audit_test.go`

- [ ] **Step 1: 确认目录存在**

```bash
ls tests/integration/swarm/permissions/ 2>/dev/null || echo "需创建"
```

9.90d-2 已建目录。若不存在则创建。

- [ ] **Step 2: 写集成测试**

```go
//go:build integration

package permissions_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
)

// GuardAuditSuite 审计-落盘-重放集成测试套件。
type GuardAuditSuite struct {
	suite.Suite
	dataDir string
}

// SetupTest 准备临时数据目录。
func (s *GuardAuditSuite) SetupTest() {
	s.dataDir = s.T().TempDir()
	s.T().Setenv("JIUWENSWARM_DATA_DIR", s.dataDir)
}

// Test审计落盘重放 验证一次允许后规则进 YAML，第二次直接命中 allow。
func (s *GuardAuditSuite) Test审计落盘重放() {
	writer := permissions.NewPersistentAuditWriter(s.dataDir)
	facts := permissions.ToolDecisionFacts{
		AccessesKnown: true,
		Capability: permissions.ToolCapability{
			ToolName: "bash",
			Category: "shell",
			RiskTier: "low",
		},
	}

	// 第一次：允许 + 审计
	result := permissions.EmitPermissionAudit(facts, permissions.AuditOptions{
		Decision: "allow",
		Reason:   "reviewer_approved",
		Extra:    map[string]any{"decision_source": "llm_reviewer"},
	}, writer)
	s.Require().NotNil(result)
	s.Require().True(result.Persisted)

	// 审计文件存在且含记录
	auditPath := filepath.Join(s.dataDir, permissions.AuditSubdirectory, permissions.AuditFilename)
	content, err := os.ReadFile(auditPath)
	s.Require().NoError(err)
	s.Require().Contains(string(content), "reviewer_approved")
	s.Require().Contains(string(content), "llm_reviewer")

	// 落盘规则
	err = permissions.PersistExactPermissionAllowRule(permissions.ExactAllowOptions{
		ToolName:      "bash",
		ToolArgs:      map[string]any{"command": "npm test"},
		WorkspaceRoot: s.dataDir,
		RaiseOnInvalid: true,
	})
	s.Require().NoError(err)

	// 第二次：规则已生效
	tools := config.GetPermissionsTools()
	toolsMap, _ := tools["tools"].(map[string]any)
	s.Require().Contains(toolsMap, "bash")
}

// Test中断分流 验证 queue 缺失时按判据分流。
func (s *GuardAuditSuite) Test中断分流() {
	withPermission := map[string]any{
		"result_type": "interrupt",
		"state": []any{
			map[string]any{
				"metadata": map[string]any{"tool_invocation_key": "k1"},
			},
		},
	}
	s.Require().True(permissions.ContainsPermissionInteraction(withPermission))

	ordinary := map[string]any{
		"result_type": "interrupt",
		"state": []any{
			map[string]any{"question": "选哪个？"},
		},
	}
	s.Require().False(permissions.ContainsPermissionInteraction(ordinary))
}

// Test隔离恢复 验证内存清空后仍从 session state 判定隔离。
func (s *GuardAuditSuite) Test隔离恢复() {
	session := newIntegrationSession()
	rail := permissions.NewStreamEventRailForTest()

	rail.QuarantineSession("sess-1", session)
	s.Require().Error(rail.RaiseIfQuarantined("sess-1", session))

	// 模拟进程重启：新建 rail（内存为空）
	freshRail := permissions.NewStreamEventRailForTest()
	s.Require().Error(freshRail.RaiseIfQuarantined("sess-1", session),
		"重启后仍应从 session state 恢复隔离判定")
}

// TestGuardAuditSuite 运行套件。
func TestGuardAuditSuite(t *testing.T) {
	suite.Run(t, new(GuardAuditSuite))
}
```

**注意**：`NewStreamEventRailForTest` 与 `newIntegrationSession` 需要在实现侧提供。若不便暴露，改为在 `stream_event_rail` 包内写该用例（同包测试可访问未导出符号），集成测试只保留前两个场景。

- [ ] **Step 3: 跑集成审计门禁**

```bash
go run ./cmd/integration-audit -verify
```

预期：通过。若跨度不足被标灰区，按规则 7 补跨子系统调用（本目录已跨 `agentcore/harness/security` + `common/config` + `swarm/.../permissions` + `swarm/.../rails`，满足 ≥3）。

- [ ] **Step 4: 运行集成测试**

```bash
CGO_ENABLED=1 go test -tags "integration sqlite_fts5 test" ./tests/integration/swarm/permissions/ -v
```

预期：全绿。

- [ ] **Step 5: 提交**

```bash
git add tests/integration/swarm/permissions/guard_audit_test.go
git commit -m "test(permissions): 9.90e 集成三场景

1. 审计-落盘-重放：允许 → 审计 JSONL 有记录 → 规则进配置 → 二次可读
2. 中断分流：含权限交互走撤销路径，普通问答走投影路径
3. 隔离恢复：进程重启（内存清空）后仍从 session state 判定隔离"
git show --stat HEAD
```

---

## Task 11: 文档同步与计划表回填

**Files:**
- Modify: `internal/swarm/agents/harness/common/rails/permissions/doc.go`
- Modify: `internal/swarm/agents/harness/common/rails/permissions/auto_permission/doc.go`
- Modify: `internal/common/config/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 `permissions/doc.go` 文件树**

在文件目录区块补：

```
//	├── audit.go                            # 审计旁路：observe-only 事件发出（9.90e）
//	├── permission_interaction.go           # 权限交互判据：中断分流用（9.90e）
```

- [ ] **Step 2: 更新 `auto_permission/doc.go`**

`runtime_bridge.go` 的描述从「no-op 桩」改为真实行为：

```
//	├── runtime_bridge.go # 判定结果到运行时行为的桥接（Task 29，审计已接 9.90e）
```

- [ ] **Step 3: 更新 `common/config/doc.go`**

```
//	├── permissions.go    # permissions 配置读写 11 函数（对齐 common/config.py L1199-1500）
```

- [ ] **Step 4: 回填 `IMPLEMENTATION_PLAN.md`**

9.90e 行：`☐` → `✅`，内容改为完成摘要：

```markdown
| 9.90e | ✅ | permissions 护栏与审计 | ✅ **9 工作项全部落地**：`audit.go`(65) + `permission_interaction.go`(54) + persist 5 函数(340) + config 层 11 函数(300) + RPC 收敛(745→250) + `MergeFileGuardPathRule`(74) + 17 处 ⤵️ 桩回填 + stream_event_rail 隔离分流(250) + owner_scopes 对齐(120)。**顺带修 3 个缺陷**：①`GetPermissionsTools`/`GetPermissionsRules` 读错配置路径（漏 `["permissions"]` 层，前端权限页读到空数据）；②`merge_file_guard_path_rule` 零落地（隐藏前置件）；③`SetupPermissionContext` 漏 `principal_user_id` 分支（普通 IM 场景 owner_scopes 不生效）。**登记 5 项偏差** D-1~D-5（id 去重/BaseException/ContextVar token/文件归属/异常类型）。Spec: `docs/superpowers/specs/2026-11-10-permissions-guard-audit-9.90e-design.md`；Plan: `docs/superpowers/plans/2026-11-10-permissions-guard-audit-9.90e.md` |
```

- [ ] **Step 5: 删除已回填的 ⤵️ 标记**

检查计划表里指向 9.90e 的 `⤵️`：

```bash
grep -n "9.90e" IMPLEMENTATION_PLAN.md
```

9.90b 行的「⤵️ 遗留：`policy_eval` 的 send_file 分支（待 9.90e）」改为已回填。9.90d-2 行的 V-8 相关标记同改。

- [ ] **Step 6: 提交**

```bash
git add internal/swarm/agents/harness/common/rails/permissions/doc.go internal/swarm/agents/harness/common/rails/permissions/auto_permission/doc.go internal/common/config/doc.go IMPLEMENTATION_PLAN.md
git commit -m "docs(permissions): 9.90e 文档同步 + 计划表回填

- permissions/doc.go：补 audit.go / permission_interaction.go
- auto_permission/doc.go：runtime_bridge 不再是 no-op 桩
- common/config/doc.go：补 permissions.go
- IMPLEMENTATION_PLAN.md：9.90e 标 ✅，回填 3 个顺带修正的缺陷"
git show --stat HEAD
```

---

## Task 12: 全量验收

- [ ] **Step 1: 锚点全部清空**

```bash
grep -rn "⤵️ 9.90e" --include=*.go internal/ ; echo "exit=$?"
grep -rn "NewPermissionContextFromRequest\|WithAgentPermissionContext" --include=*.go internal/ tests/ cmd/ ; echo "exit=$?"
grep -rn "func GetPermissionsTools\|func GetPermissionsRules" --include=*.go internal/swarm/ ; echo "exit=$?"
```

预期：三条全部无输出。

- [ ] **Step 2: 编译与静态检查**

```bash
go build ./... && go vet ./...
```

- [ ] **Step 3: 全量回归**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./... 2>&1 | tail -40
```

预期：零 FAIL。

- [ ] **Step 4: 覆盖率**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -coverprofile=/tmp/final.out \
  ./internal/swarm/agents/harness/common/rails/permissions/... \
  ./internal/common/config/... \
  ./internal/agentcore/harness/security/... \
  ./internal/swarm/schema/...
go tool cover -func=/tmp/final.out | grep -E "audit.go|permission_interaction.go|permissions.go|patterns.go|permission.go" | head -30
```

预期：新增函数均 ≥ 85%。

- [ ] **Step 5: 集成审计门禁**

```bash
go run ./cmd/integration-audit -verify
```

- [ ] **Step 6: 推送**

```bash
git push
```

按规则 11，本地验证通过 + 推送即完成，不追踪远端 CI。

---

## 自审记录

### 覆盖检查

| Spec 章节 | 对应 Task |
|---|---|
| §4.1 audit.go | Task 5 |
| §4.2 permission_interaction.go | Task 6 |
| §4.3 stream_event_rail 隔离分流 | Task 8 |
| §4.4 17 处桩回填 | Task 7 |
| §4.5 persist 5 函数 | Task 2 |
| §4.6 MergeFileGuardPathRule | Task 1 |
| §4.7 config 层 11 函数 | Task 3 |
| §4.8 RPC 分发重构 | Task 4 |
| §4.9 owner_scopes 对齐 | Task 9 |
| §6 偏差 D-1~D-5 | Task 1（D-1 Task 6、D-2 Task 8、D-3/D-4 Task 9、D-5 Task 2） |
| §7 删除清单 | Task 4（11 函数）+ Task 9（schema 符号） |
| §8 测试策略 | 各 Task 内 + Task 10 |
| §9 执行顺序 | Task 1-12 即按此排 |

### 类型一致性

- `AuditOptions` — Task 5 定义，Task 7 使用 ✓
- `PersistentAuditWriteResult` — 9.90d-1 已存在，Task 5/7 使用 ✓
- `ExactAllowOptions` — Task 2 定义并使用 ✓
- `ErrPermissionApprovalInvalidated` — Task 2 定义，同 Task 使用 ✓
- `PermissionRuntimeQuarantinedKey` — Task 6 定义，Task 8 使用 ✓
- `ContainsPermissionInteraction` — Task 6 定义，Task 8 使用 ✓
- `auditRecord` — Task 7 定义，同 Task 使用 ✓
- `SetupPermissionContext` / `CleanupPermissionContext` / `CurrentPermissionOwnerScope` — Task 9 定义并使用 ✓

### 已核实（写计划时查过，执行时可直接用）

1. `BuildSanitizedAuditFields` / `PersistentAuditWriter.Write` 都是**位置参数**，`Write` 返回值（非指针）— Task 5 代码已按此写
2. `logComponent = logger.ComponentPermissions` 在 `owner_scopes.go:51`
3. `ToolDecisionFacts` 无顶层 `ToolName`/`ToolCategory`，都在 `Capability` 里；`StaticSideEffects` 是 `map[string]struct{}` — Task 5/10 代码已按此写
4. `PersistentAuditWriteResult` 是值类型（`Persisted`/`Degraded`/`Reason`/`Path` 四字段）
5. `SessionFacade.GetState` 收 `state.StateKey`，用 `state.StringKey("...")` 构造

### 待执行时核对的签名（已在各 Task 内标注 grep 命令）

1. `security.NewPermissionEngine` / `EvaluateGlobalPolicyDirectly` / `FileGuardChecker.Evaluate` / `SendFilePathGuardEvaluator.Evaluate` / `PathAction` 字段名（Task 2）
2. `UpdateConfig` / `GetConfigSnapshot` 的实际形态（Task 3）
3. `ReqMethod` 的字符串值（Task 4）
4. `sidKey` / `abort` 现有实现（Task 8）
5. `cbc.Inputs().Result()` 的访问方式（Task 8）
6. `permissionTerminalOutcome` / `permissionTerminalDecisionSource` / `AskLevel` 是否已存在（Task 7）
