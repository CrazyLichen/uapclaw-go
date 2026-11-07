# 缺陷 D 组（工具 / MCP / 提示词）对齐实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 `docs/sync/defect-plan-2026-11-04.md` §3.D 中核验为「存在」的 12 项缺陷（D-01~05、D-08~10、D-12、D-14~16）按上游语义逐项对齐。

**Architecture:** 五个独立批次。D-A（shell 输出上限与超时预算）产出 `*int` 语义与 `getCallTimeout` 钩子，D-B（工具执行语义）消费该钩子实现外层调用超时；D-C（技能与 worktree 路径）、D-D（读取预算与 shell 检测）、D-E（MCP 结果与重连）互不依赖，可并行。每批次内部按 TDD：先写失败测试 → 最小实现 → 通过 → 提交。

**Tech Stack:** Go 1.22+，`github.com/modelcontextprotocol/go-sdk`（MCP），`github.com/ledongthuc/pdf`（PDF），标准库 `os/exec`。

**设计依据：** `docs/superpowers/specs/2026-11-07-tool-mcp-prompt-d-group-alignment-design.md`

---

## 文件结构

| 文件 | 职责 | 批次 |
| --- | --- | --- |
| `internal/agentcore/harness/tools/shell/bash.go` | Bash 工具输入解析、`*int` 输出上限 | D-A |
| `internal/agentcore/harness/tools/shell/bash_stream.go` | Bash 流式输入解析 | D-A |
| `internal/agentcore/harness/tools/shell/powershell.go` | PowerShell 输入解析 | D-A |
| `internal/agentcore/harness/tools/shell/powershell_stream.go` | PowerShell 流式输入解析 | D-A |
| `internal/agentcore/harness/tools/shell/call_timeout.go` | **新建**：`getCallTimeout` 的 Bash/PS 实现 | D-A |
| `internal/agentcore/sys_operation/local/shell_operation.go` | 删除 sh 层超时钳制 | D-A |
| `internal/agentcore/harness/prompts/tools/bash.go` | 提示词文案 + schema `minimum` | D-A |
| `internal/agentcore/harness/prompts/tools/powershell.go` | 同上 | D-A |
| `internal/agentcore/foundation/tool/base.go` | `Tool` 接口加 `GetCallTimeout` | D-B |
| `internal/agentcore/single_agent/ability/tool_timeout.go` | **新建**：三层超时预算解析 | D-B |
| `internal/agentcore/single_agent/ability/ability_manager.go` | 并行开关 + 外层超时包裹 | D-B |
| `internal/agentcore/single_agent/config/agent_config.go` | `ParallelToolCalls` 字段 | D-B |
| `internal/agentcore/single_agent/agents/react_invoke.go` | 透传开关 | D-B |
| `internal/agentcore/harness/schema/config.go` | `DeepAgentConfig`/`SubAgentConfig` 字段 | D-B |
| `internal/agentcore/harness/tools/worktree/slug.go` | slug 扁平化 + 校验收紧 | D-C |
| `internal/agentcore/harness/tools/worktree/git.go` | `gitdir`/`commondir` 越界校验 | D-C |
| `internal/swarm/server/runtime/skill/skill_manager.go` | teamskillshub init 路径语义 + 卸载 slug 兜底 | D-C |
| `internal/agentcore/harness/tools/filesystem/read_file.go` | notebook/PDF token 预算 | D-D |
| `internal/agentcore/sys_operation/local/shell_helpers.go` | PS 赋值判定 + bash 控制流识别 | D-D |
| `internal/agentcore/sys_operation/local/utils.go` | 后台 stderr 排空 | D-D |
| `internal/agentcore/foundation/tool/mcp/base.go` | 多块抽取 + 状态保留 + `MCPToolResult` | D-E |
| `internal/agentcore/foundation/tool/mcp/types/types.go` | `IncludeImageContent` 字段 | D-E |
| `internal/agentcore/foundation/tool/mcp/client/reconnect.go` | **新建**：可重连判定与重试 | D-E |
| `internal/agentcore/foundation/tool/mcp/client/sse_client.go` | 接入重连 | D-E |
| `internal/agentcore/foundation/tool/mcp/client/streamable_http_client.go` | 接入重连 | D-E |
| `internal/agentcore/single_agent/agents/react_invoke.go` | 多模态消费端结构体分支 + 文案 | D-E |

---

## 批次 D-A：Shell 输出上限与超时预算

### Task A1: `resolveBashMaxOutputChars` 改 `*int` 语义

**Files:**
- Modify: `internal/agentcore/harness/tools/shell/bash.go:30-55`（常量）、`:334-355`（函数）
- Test: `internal/agentcore/harness/tools/shell/bash_test.go`

- [ ] **Step 1: 写失败测试**

在 `bash_test.go` 末尾追加：

```go
// TestResolveBashMaxOutputChars_指针语义 验证 nil/0/正数三种输入的分支
func TestResolveBashMaxOutputChars_指针语义(t *testing.T) {
	zero := 0
	small := 100
	normal := 5000
	huge := 999999

	cases := []struct {
		name string
		raw  *int
		want int
	}{
		{"未传取默认 20000", nil, 20000},
		{"显式 0 表示不限制", &zero, 0},
		{"小于 200 钳到 200", &small, 200},
		{"正常值原样", &normal, 5000},
		{"超上限钳到默认上限", &huge, 20000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveBashMaxOutputChars(tc.raw); got != tc.want {
				t.Fatalf("resolveBashMaxOutputChars = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestResolveBashMaxOutputChars_环境变量上限 验证 BASH_TOOL_MAX_OUTPUT_CHARS 参与钳制
func TestResolveBashMaxOutputChars_环境变量上限(t *testing.T) {
	t.Setenv("BASH_TOOL_MAX_OUTPUT_CHARS", "3000")
	v := 9999
	if got := resolveBashMaxOutputChars(&v); got != 3000 {
		t.Fatalf("env cap not applied: got %d, want 3000", got)
	}
	// 环境变量不影响 nil 的默认值（默认值也要被钳制）
	if got := resolveBashMaxOutputChars(nil); got != 3000 {
		t.Fatalf("env cap not applied to default: got %d, want 3000", got)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/agentcore/harness/tools/shell/ -run 'TestResolveBashMaxOutputChars_指针语义|TestResolveBashMaxOutputChars_环境变量上限' -v`
Expected: 编译失败 `cannot use tc.raw (variable of type *int) as int value in argument to resolveBashMaxOutputChars`

- [ ] **Step 3: 改常量与函数**

`bash.go:52-54` 处常量：

```go
	// bashDefaultMaxOutputChars 未传 max_output_chars 时的默认最大输出字符数。
	// Python: BashTool._resolve_max_output_chars default=20000
	bashDefaultMaxOutputChars = 20000
```

`bash.go` 的 `resolveBashMaxOutputChars` 整体替换为：

```go
// resolveBashMaxOutputChars 解析并钳制最大输出字符数。
// Python: BashTool._resolve_max_output_chars (bash/_tool.py L113-126)
//
// rawValue 为 nil 表示调用方未传该参数，取默认值（并同样受环境变量钳制）；
// 显式传 0 表示不限制（opt-out），不做任何钳制；正数取 [200, maxChars]。
func resolveBashMaxOutputChars(rawValue *int) int {
	maxChars := bashDefaultMaxOutputChars
	if v := os.Getenv("BASH_TOOL_MAX_OUTPUT_CHARS"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			maxChars = parsed
		}
	}
	if maxChars < 200 {
		maxChars = 200
	}

	value := bashDefaultMaxOutputChars
	if rawValue != nil {
		value = *rawValue
	}
	// 显式 0：调用方主动放弃上限
	if rawValue != nil && value == 0 {
		return 0
	}
	if value < 200 {
		value = 200
	}
	if value > maxChars {
		value = maxChars
	}
	return value
}
```

- [ ] **Step 4: 改调用点与字段**

`bash.go:34-35` 字段：

```go
	// MaxOutputChars 最大输出字符数；nil 表示未传（默认 20000），显式 0 表示不限制
	MaxOutputChars *int `json:"max_output_chars"`
```

`bash.go:87` 调用点无需改动（`input.MaxOutputChars` 已是 `*int`）。

- [ ] **Step 5: 运行测试确认通过**

Run: `go test ./internal/agentcore/harness/tools/shell/ -run 'TestResolveBashMaxOutputChars' -v`
Expected: PASS（含既有的 `TestResolveBashMaxOutputChars_零值` 需同步改成 `&zero`，见 Step 6）

- [ ] **Step 6: 修既有测试的调用签名**

`bash_test.go` 中所有 `resolveBashMaxOutputChars(数字)` 改成传指针，例如：

```go
	if got := resolveBashMaxOutputChars(&zero); got != 0 {
```

注意：原 `TestResolveBashMaxOutputChars_零值` 断言 `0 → 0`，语义仍是「显式 0 不限制」，保留。

- [ ] **Step 7: 编译整包并提交**

Run: `go build ./... && go test ./internal/agentcore/harness/tools/shell/`

```bash
git add internal/agentcore/harness/tools/shell/bash.go internal/agentcore/harness/tools/shell/bash_test.go
git commit -m "fix(shell): max_output_chars 改 *int 语义，默认 20000 防止输出撑爆上下文（D-01）"
```

---

### Task A2: `BashStreamInput` 同步改 `*int`

**Files:**
- Modify: `internal/agentcore/harness/tools/shell/bash_stream.go:28-33`

- [ ] **Step 1: 写失败测试**

在 `bash_test.go` 追加（流式工具的输入解析与具名工具共用同一函数，这里只验证字段类型被正确解析）：

```go
// TestBashStreamInput_指针字段 验证流式输入结构体的 MaxOutputChars 可区分未传与显式 0
func TestBashStreamInput_指针字段(t *testing.T) {
	var absent BashStreamInput
	if err := json.Unmarshal([]byte(`{"command":"echo hi"}`), &absent); err != nil {
		t.Fatal(err)
	}
	if absent.MaxOutputChars != nil {
		t.Fatalf("未传时应为 nil，实际 %v", *absent.MaxOutputChars)
	}
	if got := resolveBashMaxOutputChars(absent.MaxOutputChars); got != 20000 {
		t.Fatalf("未传应取默认 20000，实际 %d", got)
	}

	var explicitZero BashStreamInput
	if err := json.Unmarshal([]byte(`{"command":"echo hi","max_output_chars":0}`), &explicitZero); err != nil {
		t.Fatal(err)
	}
	if explicitZero.MaxOutputChars == nil || *explicitZero.MaxOutputChars != 0 {
		t.Fatal("显式 0 应被解析为指向 0 的指针")
	}
	if got := resolveBashMaxOutputChars(explicitZero.MaxOutputChars); got != 0 {
		t.Fatalf("显式 0 应为不限制，实际 %d", got)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/harness/tools/shell/ -run TestBashStreamInput_指针字段 -v`
Expected: 编译失败（`absent.MaxOutputChars != nil` 类型不匹配 int）

- [ ] **Step 3: 改字段**

`bash_stream.go:30-31`：

```go
	// MaxOutputChars 最大输出字符数；nil 表示未传（默认 20000），显式 0 表示不限制
	MaxOutputChars *int `json:"max_output_chars"`
```

- [ ] **Step 4: 补 json import 并跑测试**

Run: `go test ./internal/agentcore/harness/tools/shell/ -run TestBashStreamInput_指针字段 -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/tools/shell/bash_stream.go internal/agentcore/harness/tools/shell/bash_test.go
git commit -m "fix(shell): 流式 bash 输入同步改 *int 语义（D-01）"
```

---

### Task A3: PowerShell 两处同步改 `*int`

**Files:**
- Modify: `internal/agentcore/harness/tools/shell/powershell.go:28-48`、`:294-320`
- Modify: `internal/agentcore/harness/tools/shell/powershell_stream.go:26-30`
- Test: `internal/agentcore/harness/tools/shell/powershell_test.go`

- [ ] **Step 1: 写失败测试**

`powershell_test.go` 追加：

```go
// TestResolvePSMaxOutputChars_指针语义 验证 nil/0/正数三种输入
func TestResolvePSMaxOutputChars_指针语义(t *testing.T) {
	zero := 0
	small := 100
	normal := 5000

	cases := []struct {
		name string
		raw  *int
		want int
	}{
		{"未传取默认 20000", nil, 20000},
		{"显式 0 表示不限制", &zero, 0},
		{"小于 200 钳到 200", &small, 200},
		{"正常值原样", &normal, 5000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolvePSMaxOutputChars(tc.raw); got != tc.want {
				t.Fatalf("resolvePSMaxOutputChars = %d, want %d", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/harness/tools/shell/ -run TestResolvePSMaxOutputChars_指针语义 -v`
Expected: 编译失败（参数类型不匹配）

- [ ] **Step 3: 改常量、函数、两处字段**

`powershell.go:45-47`：

```go
	// psDefaultMaxOutputChars 未传 max_output_chars 时的默认最大输出字符数。
	// Python: PowerShellTool._resolve_max_output_chars default=20000
	psDefaultMaxOutputChars = 20000
```

`powershell.go:294-320` 整体替换：

```go
// resolvePSMaxOutputChars 解析并钳制 PowerShell 最大输出字符数。
// Python: PowerShellTool._resolve_max_output_chars (powershell/_tool.py L94-108)
//
// rawValue 为 nil 表示未传，取默认值（同样受环境变量钳制）；
// 显式 0 表示不限制；正数取 [200, maxChars]。
func resolvePSMaxOutputChars(rawValue *int) int {
	maxChars := psDefaultMaxOutputChars
	if v := os.Getenv("POWER_SHELL_TOOL_MAX_OUTPUT_CHARS"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			maxChars = parsed
		}
	}
	if maxChars < 200 {
		maxChars = 200
	}

	value := psDefaultMaxOutputChars
	if rawValue != nil {
		value = *rawValue
	}
	if rawValue != nil && value == 0 {
		return 0
	}
	if value < 200 {
		value = 200
	}
	if value > maxChars {
		value = maxChars
	}
	return value
}
```

`powershell.go:30-31` 与 `powershell_stream.go:28-29` 字段统一为：

```go
	// MaxOutputChars 最大输出字符数；nil 表示未传（默认 20000），显式 0 表示不限制
	MaxOutputChars *int `json:"max_output_chars"`
```

- [ ] **Step 4: 修既有测试签名**

`powershell_test.go` 中 `resolvePSMaxOutputChars(0)` / `(5000)` 等改传指针，例如：

```go
	if got := resolvePSMaxOutputChars(&zero); got != 0 {
```

- [ ] **Step 5: 跑测试并提交**

Run: `go build ./... && go test ./internal/agentcore/harness/tools/shell/`
Expected: PASS

```bash
git add internal/agentcore/harness/tools/shell/powershell.go internal/agentcore/harness/tools/shell/powershell_stream.go internal/agentcore/harness/tools/shell/powershell_test.go
git commit -m "fix(shell): powershell 与流式输入同步改 *int 语义（D-01）"
```

---

### Task A4: 提示词文案与 schema `minimum`

**Files:**
- Modify: `internal/agentcore/harness/prompts/tools/bash.go:110`、`:129`
- Modify: `internal/agentcore/harness/prompts/tools/powershell.go:220`、`:236`
- Test: `internal/agentcore/harness/prompts/tools/tools_test.go`（已存在，追加用例）

- [ ] **Step 1: 写失败测试**

追加到 `tools_test.go`（该文件已存在）：

```go
// TestBashInputParams_MaxOutputCharsSchema 验证 max_output_chars 的文案与 minimum 约束
func TestBashInputParams_MaxOutputCharsSchema(t *testing.T) {
	params := (&BashMetadataProvider{}).GetInputParams("cn")
	props, _ := params["properties"].(map[string]any)
	field, ok := props["max_output_chars"].(map[string]any)
	if !ok {
		t.Fatal("max_output_chars 属性缺失")
	}
	if min, ok := field["minimum"].(int); !ok || min != 0 {
		t.Fatalf("minimum 应为 0，实际 %v", field["minimum"])
	}
	desc, _ := field["description"].(string)
	if !strings.Contains(desc, "默认 20000") {
		t.Fatalf("描述应说明默认 20000，实际 %q", desc)
	}
	if !strings.Contains(desc, "显式传 0 表示不限制") {
		t.Fatalf("描述应说明显式 0 不限制，实际 %q", desc)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/harness/prompts/tools/ -run TestBashInputParams_MaxOutputCharsSchema -v`
Expected: FAIL，`minimum 应为 0，实际 <nil>`

- [ ] **Step 3: 改文案与 schema**

`bash.go:110`：

```go
		"max_output_chars":  {"cn": "最大输出字符数，默认 20000：超出部分会被截断并写入临时文件（结果中会给出文件路径，可按需读取），防止超大输出撑爆上下文；显式传 0 表示不限制（谨慎使用）", "en": "Max output characters; defaults to 20000: output beyond this is truncated and the full content is written to a temp file (its path is returned in the result for on-demand reading), preventing oversized output from flooding context. Pass 0 explicitly to disable the limit (use with care)"},
```

`bash.go:129`：

```go
			"max_output_chars":  map[string]any{"type": "integer", "minimum": 0, "description": desc("max_output_chars")},
```

`powershell.go:220`：

```go
		"max_output_chars": {"cn": "最大输出字符数，默认 20000：超出部分会被截断并写入临时文件（结果中会给出文件路径，可按需读取），防止超大输出撑爆上下文；显式传 0 表示不限制（谨慎使用）", "en": "Max output characters; defaults to 20000: output beyond this is truncated and the full content is written to a temp file (its path is returned in the result for on-demand reading), preventing oversized output from flooding context. Pass 0 explicitly to disable the limit (use with care)"},
```

`powershell.go:236`：

```go
			"max_output_chars": map[string]any{"type": "integer", "minimum": 0, "description": d("max_output_chars")},
```

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/agentcore/harness/prompts/tools/ -v -run MaxOutputChars`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/prompts/tools/bash.go internal/agentcore/harness/prompts/tools/powershell.go internal/agentcore/harness/prompts/tools/bash_test.go
git commit -m "fix(prompts): max_output_chars 文案对齐上游并加 minimum 约束（D-01）"
```

---

### Task A5: 删除 sh 层框架超时钳制

**Files:**
- Modify: `internal/agentcore/sys_operation/local/shell_operation.go:146-159`、`:310-325`
- Test: `internal/agentcore/sys_operation/local/shell_operation_test.go`

- [ ] **Step 1: 写失败测试**

`shell_operation_test.go` 追加：

```go
// TestExecuteCmd_不截断显式超时 验证 sh 层不再用 JW_EXECUTE_CMD_MAX_TIMEOUT 砍小显式预算
func TestExecuteCmd_不截断显式超时(t *testing.T) {
	// 即便环境变量设成很小的值，显式 timeout 也不应被缩短
	t.Setenv("JW_EXECUTE_CMD_MAX_TIMEOUT", "5")

	shellOp := NewLocalOperation().Shell()
	start := time.Now()
	_, err := shellOp.ExecuteCmd(context.Background(), "sleep 6",
		sys_operation.WithShellTimeout(30))
	if err != nil {
		t.Fatalf("ExecuteCmd error: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 6*time.Second {
		t.Fatalf("命令被提前中断（耗时 %v），说明 sh 层仍在钳制 timeout", elapsed)
	}
}
```

> 真实签名：`ExecuteCmd(ctx, command string, opts ...Option)`，超时通过 `sys_operation.WithShellTimeout(30)` 传（见 `local_operation_test.go:132`）。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/sys_operation/local/ -run TestExecuteCmd_不截断显式超时 -v`
Expected: FAIL，`命令被提前中断（耗时 5.xxxs）`

- [ ] **Step 3: 删除两处钳制**

`shell_operation.go:146-158`（`ExecuteCmd` 内）整段替换为：

```go
	// 调用方持有执行预算，不在此处缩短（上游 55b9c571 裁决）
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = defaultExecCmdTimeout
	}
```

`shell_operation.go:310-324`（流式 `ExecuteCmdStream` 内）整段替换为同样内容：

```go
	// 与 ExecuteCmd 一致：保留调用方给定的执行预算
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = defaultExecCmdTimeout
	}
```

同时删除 `:311` 的注释行 `// 框架层超时上限，对齐 Python JW_EXECUTE_CMD_MAX_TIMEOUT`。

- [ ] **Step 4: 确认无残留引用**

Run: `grep -rn "JW_EXECUTE_CMD_MAX_TIMEOUT" internal/`
Expected: 无输出

- [ ] **Step 5: 跑测试并提交**

Run: `go build ./... && go test ./internal/agentcore/sys_operation/local/ -run TestExecuteCmd`
Expected: PASS

```bash
git add internal/agentcore/sys_operation/local/shell_operation.go internal/agentcore/sys_operation/local/shell_operation_test.go
git commit -m "fix(sys_operation): 删除框架层超时上限，显式 timeout 不再被砍到 600s（D-02）"
```

---

### Task A6: `getCallTimeout` 钩子的 Bash/PS 实现

**Files:**
- Create: `internal/agentcore/harness/tools/shell/call_timeout.go`
- Test: `internal/agentcore/harness/tools/shell/call_timeout_test.go`

> 本任务只产出函数，`Tool` 接口的挂载在 Task B1 完成（接口属 D-B 语义）。先单独可测。

- [ ] **Step 1: 写失败测试**

新建 `call_timeout_test.go`：

```go
package shell

import "testing"

// TestBashCallTimeout_前台加宽限 验证前台命令取得"执行预算 + 30s"
func TestBashCallTimeout_前台加宽限(t *testing.T) {
	timeout := 300
	got := bashCallTimeout(&BashInput{Timeout: timeout, RunInBackground: false}, 300)
	if got != 330 {
		t.Fatalf("前台应为 300+30=330，实际 %v", got)
	}
}

// TestBashCallTimeout_后台不宽限 验证后台模式原样返回默认预算
func TestBashCallTimeout_后台不宽限(t *testing.T) {
	got := bashCallTimeout(&BashInput{Timeout: 300, RunInBackground: true}, 300)
	if got != 300 {
		t.Fatalf("后台应原样返回 300，实际 %v", got)
	}
}

// TestBashCallTimeout_超时钳制后再加宽限 验证宽限基于钳制后的值
func TestBashCallTimeout_超时钳制后再加宽限(t *testing.T) {
	got := bashCallTimeout(&BashInput{Timeout: 99999}, 300)
	if got != 3600+30 {
		t.Fatalf("超时先钳到 3600 再加 30，实际 %v", got)
	}
}

// TestPSCallTimeout_前台加宽限 验证 PowerShell 同构
func TestPSCallTimeout_前台加宽限(t *testing.T) {
	// 后台模式原样返回 defaultTimeout（后台命令立即返回 PID，不需要宽限）
	got := psCallTimeout(&PowerShellInput{Timeout: 120, Background: false}, 300)
	if got != 150 {
		t.Fatalf("前台应为 120+30=150，实际 %v", got)
	}
}
```

> PowerShell 的超时解析函数是 `resolvePSTimeout(rawValue int) int`（`powershell.go:274`），`psCallTimeout` 直接调用它。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/harness/tools/shell/ -run 'CallTimeout' -v`
Expected: 编译失败 `undefined: bashCallTimeout`

- [ ] **Step 3: 写实现**

新建 `call_timeout.go`：

```go
package shell

// ──────────────────────────── 常量 ────────────────────────────

const (
	// callTimeoutGraceSeconds 外层调用预算在内层执行预算之上额外给的清理宽限（秒）。
	// 内层超时要来得及 kill 进程树并排空管道；外层若与内层同时到点，
	// 会把还在收尾的调用直接砍掉，导致 ExitCode 与已产出内容丢失。
	// Python: _CALL_TIMEOUT_GRACE_SECONDS = 30 (bash/_tool.py:57)
	callTimeoutGraceSeconds = 30
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// bashCallTimeout 返回 Bash 工具的外层调用预算。
// Python: BashTool.get_call_timeout (bash/_tool.py:151-168)
//
// 前台命令 = 钳制后的执行预算 + 清理宽限；后台命令立即返回 PID，不需要宽限。
func bashCallTimeout(input *BashInput, defaultTimeout float64) float64 {
	if input == nil {
		return defaultTimeout
	}
	if input.RunInBackground {
		return defaultTimeout
	}
	return float64(resolveBashTimeout(input.Timeout)) + callTimeoutGraceSeconds
}

// psCallTimeout 返回 PowerShell 工具的外层调用预算。
// Python: PowerShellTool.get_call_timeout (powershell/_tool.py:130-147)
func psCallTimeout(input *PowerShellInput, defaultTimeout float64) float64 {
	if input == nil {
		return defaultTimeout
	}
	if input.Background {
		return defaultTimeout
	}
	return float64(resolvePSTimeout(input.Timeout)) + callTimeoutGraceSeconds
}
```

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/agentcore/harness/tools/shell/ -run 'CallTimeout' -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/tools/shell/call_timeout.go internal/agentcore/harness/tools/shell/call_timeout_test.go
git commit -m "feat(shell): 新增外层调用预算钩子实现（执行预算+30s 清理宽限，D-02）"
```

---

## 批次 D-B：工具执行语义

### Task B1: `Tool` 接口加 `GetCallTimeout`

**Files:**
- Modify: `internal/agentcore/foundation/tool/base.go:18-31`（接口）、新增默认实现
- Modify: `internal/agentcore/foundation/tool/lifecycle_tool.go`（包装器透传，**关键**）
- Test: `internal/agentcore/foundation/tool/base_test.go`、`lifecycle_tool_test.go`

> **为什么必须改 `LifecycleTool`**：`ability_manager.go:919` 实际执行的是 `lt := tool.NewLifecycleTool(t)`。若包装器不实现 `GetCallTimeout`，接口方法会落到内嵌的 `Tool` 字段上——那是 `BaseCallTimeout`，`BashTool` 的 `+30s` 永远不生效。

- [ ] **Step 1: 写失败测试**

`base_test.go` 追加：

```go
// fakeTool 实现了 Tool 的最小测试替身
type fakeTool struct{}

func (f *fakeTool) Card() *ToolCard { return NewToolCard("fake", "", nil, nil) }
func (f *fakeTool) Invoke(ctx context.Context, inputs map[string]any, opts ...ToolOption) (map[string]any, error) {
	return nil, nil
}
func (f *fakeTool) Stream(ctx context.Context, inputs map[string]any, opts ...ToolOption) (<-chan StreamChunk, error) {
	return nil, nil
}

// TestTool_DefaultGetCallTimeout 验证默认实现原样返回 defaultTimeout
func TestTool_DefaultGetCallTimeout(t *testing.T) {
	var tl Tool = &fakeTool{}
	if got := tl.GetCallTimeout(map[string]any{}, 123.0); got != 123.0 {
		t.Fatalf("默认实现应返回 123，实际 %v", got)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/foundation/tool/ -run TestTool_DefaultGetCallTimeout -v`
Expected: 编译失败 `tl.GetCallTimeout undefined`

- [ ] **Step 3: 接口加方法 + 提供默认实现**

`base.go` 的 `Tool` 接口追加：

```go
	// GetCallTimeout 返回本次调用的外层预算（秒）。
	//
	// 内部已有执行期限的工具可覆盖此方法，让外层调用预算 = 执行预算 + 有界清理时间。
	// 默认实现原样返回 defaultTimeout，即沿用卡片级超时策略。
	//
	// Python: Tool.get_call_timeout (foundation/tool/base.py:255-276)
	GetCallTimeout(inputs map[string]any, defaultTimeout float64) float64
```

`base.go` 文件末尾追加默认实现的导出类型：

```go
// ──────────────────────────── 导出函数 ────────────────────────────

// BaseCallTimeout 是 GetCallTimeout 的默认实现，供不覆盖该钩子的工具嵌入或代理调用。
//
// 嵌入 BaseToolBehavior 的工具无需实现此能力；显式实现 Tool 接口的工具
// 应在 GetCallTimeout 中委托到本函数。
//
// Python: Tool.get_call_timeout 的基类实现
func BaseCallTimeout(_ map[string]any, defaultTimeout float64) float64 {
	return defaultTimeout
}
```

- [ ] **Step 4: 逐个实现者补齐**

Run: `grep -rn "var _ Tool\b\|var _ Tool =" --include=*.go internal/ | grep -v _test`

对每个实现者补一个方法。无特殊语义的统一委托：

```go
// GetCallTimeout 沿用卡片级超时策略。
func (t *XxxTool) GetCallTimeout(inputs map[string]any, defaultTimeout float64) float64 {
	return BaseCallTimeout(inputs, defaultTimeout)
}
```

测试替身（`fakeTool`、各 `_test.go` 里的桩）同样补齐，避免 `go vet ./...` 失败。

- [ ] **Step 5: 跑测试与 vet**

Run: `go build ./... && go vet ./... && go test ./internal/agentcore/foundation/tool/ -run TestTool_DefaultGetCallTimeout -v`
Expected: PASS

- [ ] **Step 6: 包装器透传 + 写失败测试**

`lifecycle_tool_test.go` 追加：

```go
// stubCallTimeoutTool 覆盖 GetCallTimeout 的测试替身
type stubCallTimeoutTool struct{ *fakeTool }

func (s *stubCallTimeoutTool) GetCallTimeout(_ map[string]any, _ float64) float64 { return 777 }

// TestLifecycleTool_透传GetCallTimeout 验证包装器不吞掉内层工具的预算钩子
//
// ability_manager 实际执行的是 LifecycleTool；若它不透传，
// BashTool 的"执行预算+30s"永远不生效。
func TestLifecycleTool_透传GetCallTimeout(t *testing.T) {
	inner := &stubCallTimeoutTool{fakeTool: &fakeTool{}}
	lt := NewLifecycleTool(inner)
	if got := lt.GetCallTimeout(map[string]any{}, 300); got != 777 {
		t.Fatalf("应透传内层的 777，实际 %v", got)
	}
}
```

Run: `go test ./internal/agentcore/foundation/tool/ -run TestLifecycleTool_透传GetCallTimeout -v`
Expected: FAIL，`应透传内层的 777，实际 300`

- [ ] **Step 7: 实现透传**

`lifecycle_tool.go` 追加方法（委托到被包装的工具，而非内嵌字段）：

```go
// GetCallTimeout 透传被包装工具的外层预算钩子。
//
// 必须显式委托：包装器若依赖内嵌 Tool 字段的方法，
// 会命中外层默认实现而吞掉 BashTool/PowerShellTool 的宽限。
// Python: LifecycleTool 无对应包装，等价于 Tool.get_call_timeout 的动态派发
func (l *LifecycleTool) GetCallTimeout(inputs map[string]any, defaultTimeout float64) float64 {
	if l.inner == nil {
		return BaseCallTimeout(inputs, defaultTimeout)
	}
	return l.inner.GetCallTimeout(inputs, defaultTimeout)
}
```

- [ ] **Step 8: 跑测试**

Run: `go build ./... && go test ./internal/agentcore/foundation/tool/ -run 'TestTool_DefaultGetCallTimeout|TestLifecycleTool_透传GetCallTimeout' -v`
Expected: PASS

- [ ] **Step 9: 提交**

```bash
git add internal/agentcore/foundation/tool/base.go internal/agentcore/foundation/tool/lifecycle_tool.go internal/agentcore/foundation/tool/*_test.go
git commit -m "feat(tool): Tool 接口新增 GetCallTimeout 钩子，包装器透传到内层（D-02/D-04）"
```

---

### Task B2: Bash / PowerShell 覆盖 `GetCallTimeout`

**Files:**
- Modify: `internal/agentcore/harness/tools/shell/bash.go`、`powershell.go`

- [ ] **Step 1: 写失败测试**

`call_timeout_test.go` 追加：

```go
// TestBashTool_GetCallTimeout 验证工具实例的钩子委托到 bashCallTimeout
func TestBashTool_GetCallTimeout(t *testing.T) {
	tl := NewBashTool(nil, "cn", "", PermissionConfig{})
	got := tl.GetCallTimeout(map[string]any{"command": "echo hi", "timeout": float64(300)}, 300)
	if got != 330 {
		t.Fatalf("应为 330，实际 %v", got)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/harness/tools/shell/ -run TestBashTool_GetCallTimeout -v`
Expected: FAIL（BashTool 尚未实现接口方法，编译失败）

- [ ] **Step 3: 实现方法**

> 注意：当前工具通过 `NewTool[I,O]` 注册，返回的是 `*InvokeFunction[I,O]` 而非具名类型。实施时先确认 Bash 工具的注册方式：

Run: `sed -n '60,90p' internal/agentcore/harness/tools/shell/bash.go`

若返回 `tool.Tool` 且底层是 `*InvokeFunction[BashInput, map[string]any]`，则在 `InvokeFunction` 上加可选的超时钩子字段：

```go
// 在 invoke_function.go 的 InvokeFunction 结构中追加：
	// callTimeout 可选的外层预算钩子；nil 表示沿用卡片策略
	callTimeout func(inputs map[string]any, defaultTimeout float64) float64
```

```go
// GetCallTimeout 委托可选的钩子；未设置时沿用卡片策略。
func (f *InvokeFunction[I, O]) GetCallTimeout(inputs map[string]any, defaultTimeout float64) float64 {
	if f.callTimeout != nil {
		return f.callTimeout(inputs, defaultTimeout)
	}
	return BaseCallTimeout(inputs, defaultTimeout)
}
```

并在 `tool_func.go` 增加选项：

```go
// WithCallTimeout 为工具注册外层预算钩子（inputs 为原始参数 map）。
func WithCallTimeout(fn func(inputs map[string]any, defaultTimeout float64) float64) ToolFuncOption {
	return func(o *toolFuncConfig) { o.callTimeout = fn }
}
```

`toolFuncConfig` 加字段 `callTimeout func(inputs map[string]any, defaultTimeout float64) float64`，`NewTool` 中把它写进 `InvokeFunction`。

`bash.go` 注册处加：

```go
	tool.WithCallTimeout(func(inputs map[string]any, defaultTimeout float64) float64 {
		var in BashInput
		if b, err := json.Marshal(inputs); err == nil {
			_ = json.Unmarshal(b, &in)
		}
		return bashCallTimeout(&in, defaultTimeout)
	}),
```

`powershell.go` 同构，调 `psCallTimeout`。

- [ ] **Step 4: 跑测试**

Run: `go build ./... && go test ./internal/agentcore/harness/tools/shell/ -run 'CallTimeout' -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/foundation/tool/invoke_function.go internal/agentcore/foundation/tool/tool_func.go internal/agentcore/harness/tools/shell/bash.go internal/agentcore/harness/tools/shell/powershell.go internal/agentcore/harness/tools/shell/call_timeout_test.go
git commit -m "feat(shell): bash/powershell 覆盖 GetCallTimeout，外层多留 30s 清理（D-02）"
```

---

### Task B3: 三层超时预算解析

**Files:**
- Create: `internal/agentcore/single_agent/ability/tool_timeout.go`
- Test: `internal/agentcore/single_agent/ability/tool_timeout_test.go`

- [ ] **Step 1: 写失败测试**

新建 `tool_timeout_test.go`：

```go
package ability

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
)

// TestResolveCallTimeout_无resilience块 验证缺省返回 300
func TestResolveCallTimeout_无resilience块(t *testing.T) {
	card := tool.NewToolCard("t", "", nil, nil)
	if got := resolveCallTimeout(card); got == nil || *got != defaultToolCallTimeout {
		t.Fatalf("应为 300，实际 %v", got)
	}
}

// TestResolveCallTimeout_显式正数 验证正数原样返回
func TestResolveCallTimeout_显式正数(t *testing.T) {
	card := tool.NewToolCard("t", "", nil, map[string]any{
		"resilience": map[string]any{"timeout_s": float64(42)},
	})
	got := resolveCallTimeout(card)
	if got == nil || *got != 42 {
		t.Fatalf("应为 42，实际 %v", got)
	}
}

// TestResolveCallTimeout_显式nil 验证 nil 表示豁免
func TestResolveCallTimeout_显式nil(t *testing.T) {
	card := tool.NewToolCard("t", "", nil, map[string]any{
		"resilience": map[string]any{"timeout_s": nil},
	})
	if got := resolveCallTimeout(card); got != nil {
		t.Fatalf("应为 nil（豁免），实际 %v", *got)
	}
}

// TestResolveCallTimeout_非正数 验证 0 与负数表示豁免
func TestResolveCallTimeout_非正数(t *testing.T) {
	for _, v := range []float64{0, -1} {
		card := tool.NewToolCard("t", "", nil, map[string]any{
			"resilience": map[string]any{"timeout_s": v},
		})
		if got := resolveCallTimeout(card); got != nil {
			t.Fatalf("timeout_s=%v 应为 nil（豁免），实际 %v", v, *got)
		}
	}
}

// TestResolveCallTimeout_键不存在 验证只有 resilience 块无 timeout_s 时取 300
func TestResolveCallTimeout_键不存在(t *testing.T) {
	card := tool.NewToolCard("t", "", nil, map[string]any{
		"resilience": map[string]any{"max_attempts": 3},
	})
	got := resolveCallTimeout(card)
	if got == nil || *got != defaultToolCallTimeout {
		t.Fatalf("应为 300，实际 %v", got)
	}
}

// TestEffectiveCallTimeout_豁免走硬上限 验证 nil 预算被提升为 3600
func TestEffectiveCallTimeout_豁免走硬上限(t *testing.T) {
	if got := effectiveCallTimeout(nil); got != maxToolCallTimeoutHardLimit {
		t.Fatalf("应为 3600，实际 %v", got)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/single_agent/ability/ -run 'CallTimeout' -v`
Expected: 编译失败 `undefined: resolveCallTimeout`

- [ ] **Step 3: 写实现**

新建 `tool_timeout.go`：

```go
package ability

import (
	"os"
	"strconv"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
)

// ──────────────────────────── 常量 ────────────────────────────

// defaultToolCallTimeout 卡片未声明预算时的默认外层调用预算（秒）。
// Python: DEFAULT_TOOL_CALL_TIMEOUT (ability_manager.py:241)
const defaultToolCallTimeout = 300.0

// maxToolCallTimeoutHardLimit 豁免工具的硬上限（秒），保证豁免也不会无限挂死。
// Python: MAX_TOOL_CALL_TIMEOUT_HARD_LIMIT (ability_manager.py:245-247)
const maxToolCallTimeoutHardLimit = 3600.0

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveCallTimeout 解析卡片级外层调用预算。
// Python: AbilityManager._resolve_call_timeout (ability_manager.py:679-713)
//
// 解析顺序：
//  1. ToolCard.Properties["resilience"]["timeout_s"] 显式声明：
//     nil 或非正数 → 返回 nil（豁免，由工具内部超时管辖）；正数 → 该值
//  2. 无 resilience 块 / 无 timeout_s 键 / 类型非法 → defaultToolCallTimeout
//
// 返回值 nil 表示豁免，调用方须用 effectiveCallTimeout 提升为硬上限。
func resolveCallTimeout(card *tool.ToolCard) *float64 {
	fallback := defaultToolCallTimeout
	if v := os.Getenv("DEFAULT_TOOL_CALL_TIMEOUT"); v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			fallback = parsed
		}
	}

	if card == nil || card.Properties == nil {
		return &fallback
	}
	resilience, ok := card.Properties["resilience"].(map[string]any)
	if !ok {
		return &fallback
	}
	declared, hasKey := resilience["timeout_s"]
	if !hasKey {
		return &fallback
	}
	if declared == nil {
		return nil
	}
	value, ok := toFloat(declared)
	if !ok {
		return &fallback
	}
	if value <= 0 {
		return nil
	}
	return &value
}

// effectiveCallTimeout 把豁免（nil）提升为硬上限。
func effectiveCallTimeout(budget *float64) float64 {
	if budget == nil {
		return maxToolCallTimeoutHardLimit
	}
	return *budget
}

// resolveInvocationTimeout 应用工具的输入感知预算钩子。
// Python: AbilityManager._resolve_invocation_timeout (ability_manager.py:715-729)
func resolveInvocationTimeout(tl tool.Tool, inputs map[string]any, defaultTimeout float64) float64 {
	if tl == nil {
		return defaultTimeout
	}
	return tl.GetCallTimeout(inputs, defaultTimeout)
}

// toFloat 把任意数值转成 float64。
func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case string:
		parsed, err := strconv.ParseFloat(n, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}
```

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/agentcore/single_agent/ability/ -run 'CallTimeout' -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/single_agent/ability/tool_timeout.go internal/agentcore/single_agent/ability/tool_timeout_test.go
git commit -m "feat(ability): 工具调用三层超时预算解析（resilience/默认/硬上限，D-04）"
```

---

### Task B4: `Execute` 加并行开关

**Files:**
- Modify: `internal/agentcore/single_agent/ability/ability_manager.go:473-521`
- Modify: 调用方 `react_invoke.go:1088`、`progressive.go:567`、`p2p_ability_manager.go:100/139/190`
- Test: `internal/agentcore/single_agent/ability/ability_manager_test.go`

- [ ] **Step 1: 写失败测试**

`ability_manager_test.go` 追加：

```go
// TestExecute_ParallelFalse_串行 验证关闭并行后工具按序执行
func TestExecute_ParallelFalse_串行(t *testing.T) {
	var mu sync.Mutex
	var order []string

	am := NewAbilityManager()
	for _, name := range []string{"a", "b"} {
		n := name
		tl, err := tool.NewTool(
			func(ctx context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
				mu.Lock()
				order = append(order, n+".enter")
				mu.Unlock()
				time.Sleep(30 * time.Millisecond)
				mu.Lock()
				order = append(order, n+".exit")
				mu.Unlock()
				return map[string]any{"ok": true}, nil
			},
			tool.WithToolName(n),
		)
		if err != nil {
			t.Fatal(err)
		}
		am.Add(tl)
	}

	calls := []*llmschema.ToolCall{
		{ID: "1", Name: "a", Arguments: "{}"},
		{ID: "2", Name: "b", Arguments: "{}"},
	}
	am.Execute(context.Background(), nil, calls, nil, "", false)

	want := []string{"a.enter", "a.exit", "b.enter", "b.exit"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("串行顺序不符：got %v, want %v", order, want)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/single_agent/ability/ -run TestExecute_ParallelFalse_串行 -v`
Expected: 编译失败（`Execute` 参数数量不匹配）

- [ ] **Step 3: 改 `Execute` 签名与实现**

`ability_manager.go:473-479` 签名追加参数：

```go
func (am *AbilityManager) Execute(
	ctx context.Context,
	cbc *interfaces.AgentCallbackContext,
	toolCalls []*llmschema.ToolCall,
	sess sessioninterfaces.SessionFacade,
	tag string,
	parallelToolCalls bool,
) []agentschema.ExecuteResult {
```

`:485-499`（cbc == nil 降级路径）的 `WaitGroup` 循环替换为：

```go
	if cbc == nil {
		am.mu.RLock()
		results := make([]agentschema.ExecuteResult, len(toolCalls))
		if parallelToolCalls {
			var wg sync.WaitGroup
			for i, tc := range toolCalls {
				wg.Add(1)
				go func(idx int, toolCall *llmschema.ToolCall) {
					defer wg.Done()
					results[idx] = am.runToolCallSafely(ctx, nil, toolCall, sess, tag)
				}(i, tc)
			}
			wg.Wait()
		} else {
			// Python: ability_manager.py:1292-1299 串行分支
			for i, tc := range toolCalls {
				results[i] = am.runToolCallSafely(ctx, nil, tc, sess, tag)
			}
		}
		am.mu.RUnlock()
		return results
	}
```

`:511-521`（正常路径）替换为：

```go
	if parallelToolCalls {
		var wg sync.WaitGroup
		for i, tc := range toolCalls {
			wg.Add(1)
			go func(idx int, toolCall *llmschema.ToolCall, toolCtx *interfaces.AgentCallbackContext) {
				defer wg.Done()
				results[idx] = am.runToolCallSafely(ctx, toolCtx, toolCall, sess, tag)
			}(i, tc, toolCtxs[i])
		}
		am.mu.RUnlock()
		wg.Wait()
	} else {
		// 串行：单 goroutine 顺序执行，避免有状态工具的动作交错
		for i, tc := range toolCalls {
			results[i] = am.runToolCallSafely(ctx, toolCtxs[i], tc, sess, tag)
		}
		am.mu.RUnlock()
	}
```

> 注意 `am.mu.RUnlock()` 的位置：原代码里它在 `wg.Wait()` 之前释放，串行分支必须在循环之后释放。上面两段已按此排布，实施时保持。

- [ ] **Step 4: 改所有调用方**

| 文件:行 | 改法 |
| --- | --- |
| `react_invoke.go:1088` | `am.Execute(ctx, cbc, toolCalls, sess, "", a.parallelToolCalls())` |
| `progressive.go:567` | `..., sess, "", true)` |
| `p2p_ability_manager.go:100` | `..., tag, true)` |
| `p2p_ability_manager.go:139` | `..., tag, true)` |
| `p2p_ability_manager.go:190` | `..., sess, "", true)` |

`react_invoke.go` 追加辅助方法：

```go
// parallelToolCalls 返回本次工具批量是否并行执行。
// Python: react_agent.py:2460 self._config.parallel_tool_calls
func (a *ReActAgent) parallelToolCalls() bool {
	if a.config == nil {
		return true
	}
	return a.config.ParallelToolCalls
}
```

- [ ] **Step 5: 跑测试与编译**

Run: `go build ./... && go test ./internal/agentcore/single_agent/ability/ -run TestExecute -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add -u internal/agentcore/single_agent/ internal/agentcore/harness/rails/progressive.go internal/agentcore/multi_agent/teams/hierarchical_msgbus/p2p_ability_manager.go
git commit -m "feat(ability): Execute 支持串行执行开关 parallelToolCalls（D-03）"
```

---

### Task B5: `parallel_tool_calls` 配置透传

**Files:**
- Modify: `internal/agentcore/single_agent/config/agent_config.go:18-55`
- Modify: `internal/agentcore/harness/schema/config.go:129-131`（DeepAgentConfig）、`:133-141`（SubAgentConfig）
- Test: `internal/agentcore/single_agent/config/agent_config_test.go`

- [ ] **Step 1: 写失败测试**

`agent_config_test.go` 追加：

```go
// TestReActAgentConfig_DefaultParallel 验证零值语义为 true（默认并行）
func TestReActAgentConfig_DefaultParallel(t *testing.T) {
	cfg := &ReActAgentConfig{}
	if !cfg.ParallelToolCallsOrDefault() {
		t.Fatal("零值应视为并行（默认 true）")
	}
	cfg.ParallelToolCalls = ptrBool(false)
	if cfg.ParallelToolCallsOrDefault() {
		t.Fatal("显式 false 应为串行")
	}
}
```

> `ParallelToolCalls` 用 `*bool` 是为了区分「未配置」与「显式 false」。Go 的 bool 零值是 false，直接用 bool 会让未配置的配置项变成串行，与上游默认 true 相反。

（测试文件末尾追加辅助函数）

```go
func ptrBool(b bool) *bool { return &b }
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/single_agent/config/ -run TestReActAgentConfig_DefaultParallel -v`
Expected: 编译失败 `cfg.ParallelToolCallsOrDefault undefined`

- [ ] **Step 3: 加字段与访问器**

`agent_config.go` 的 `ReActAgentConfig` 末尾追加：

```go
	// ParallelToolCalls 是否并行执行同一批工具调用；nil 表示未配置，按默认 true 处理。
	//
	// 有状态工具（浏览器动作序列、worktree 切换）被并行调用会打乱动作顺序，
	// 子代理可通过 SubAgentConfig 单独关闭。
	// Python: ReActAgentConfig.parallel_tool_calls (react_agent.py:332)
	ParallelToolCalls *bool `json:"parallel_tool_calls,omitempty"`
```

文件末尾追加导出方法：

```go
// ParallelToolCallsOrDefault 返回是否并行执行工具调用，未配置时为 true。
// Python: ReActAgentConfig.parallel_tool_calls default=True
func (c *ReActAgentConfig) ParallelToolCallsOrDefault() bool {
	if c == nil || c.ParallelToolCalls == nil {
		return true
	}
	return *c.ParallelToolCalls
}
```

- [ ] **Step 4: harness 两处配置加字段**

`config.go` 的 `DeepAgentConfig`（`:129-132` 附近）追加：

```go
	// ParallelToolCalls 是否并行执行工具调用；nil 表示未配置，按默认 true 处理。
	// Python: DeepAgentConfig.parallel_tool_calls (harness/schema/config.py:236)
	ParallelToolCalls *bool `json:"parallel_tool_calls,omitempty"`
```

`SubAgentConfig`（`:133-141` 附近）追加同样字段：

```go
	// ParallelToolCalls 是否并行执行工具调用；nil 表示未配置，按默认 true 处理。
	// Python: SubAgentConfig.parallel_tool_calls (harness/schema/config.py:265)
	ParallelToolCalls *bool `json:"parallel_tool_calls,omitempty"`
```

- [ ] **Step 5: 在 `react_invoke.parallelToolCalls` 里消费**

把 Task B4 的辅助方法改为读取配置：

```go
func (a *ReActAgent) parallelToolCalls() bool {
	if a.config == nil {
		return true
	}
	return a.config.ParallelToolCallsOrDefault()
}
```

- [ ] **Step 6: 跑测试**

Run: `go build ./... && go test ./internal/agentcore/single_agent/config/ -run TestReActAgentConfig_DefaultParallel -v`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/single_agent/config/agent_config.go internal/agentcore/single_agent/config/agent_config_test.go internal/agentcore/harness/schema/config.go internal/agentcore/single_agent/agents/react_invoke.go
git commit -m "feat(config): parallel_tool_calls 配置面透传，默认 true（D-03）"
```

---

### Task B6: 外层调用超时包裹

**Files:**
- Modify: `internal/agentcore/single_agent/ability/ability_manager.go`（`runToolCallSafely` 及其调用的 invoke 点）
- Test: `internal/agentcore/single_agent/ability/ability_manager_test.go`

- [ ] **Step 1: 写失败测试**

`ability_manager_test.go` 追加：

```go
// TestExecute_工具超时产出可恢复ToolMessage 验证超时不整轮失败，而是回灌 tool_message
func TestExecute_工具超时产出可恢复ToolMessage(t *testing.T) {
	am := NewAbilityManager()
	tl, err := tool.NewTool(
		func(ctx context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
			time.Sleep(2 * time.Second)
			return map[string]any{"ok": true}, nil
		},
		tool.WithToolName("slow"),
		tool.WithCard(tool.NewToolCard("slow", "", nil, map[string]any{
			"resilience": map[string]any{"timeout_s": 0.1},
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	am.Add(tl)

	results := am.Execute(context.Background(), nil,
		[]*llmschema.ToolCall{{ID: "c1", Name: "slow", Arguments: "{}"}}, nil, "", true)
	if len(results) != 1 {
		t.Fatalf("期望 1 条结果，实际 %d", len(results))
	}
	if results[0].ToolMsg == nil {
		t.Fatal("超时必须产出 ToolMessage，否则 assistant 的 tool_calls 悬空")
	}
	if !strings.Contains(results[0].ToolMsg.Content, "timed out") {
		t.Fatalf("超时文案应含 timed out，实际 %q", results[0].ToolMsg.Content)
	}
	if results[0].ToolMsg.ToolCallID != "c1" {
		t.Fatalf("tool_call_id 应为 c1，实际 %q", results[0].ToolMsg.ToolCallID)
	}
}

// TestExecute_取消不转超时 验证用户中断不被当成工具超时
func TestExecute_取消不转超时(t *testing.T) {
	am := NewAbilityManager()
	tl, _ := tool.NewTool(
		func(ctx context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
		tool.WithToolName("blk"),
	)
	am.Add(tl)

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	results := am.Execute(ctx, nil,
		[]*llmschema.ToolCall{{ID: "c2", Name: "blk", Arguments: "{}"}}, nil, "", true)
	if len(results) == 1 && results[0].ToolMsg != nil &&
		strings.Contains(results[0].ToolMsg.Content, "timed out") {
		t.Fatal("取消不应被渲染成工具超时")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/single_agent/ability/ -run 'TestExecute_工具超时|TestExecute_取消不转超时' -v`
Expected: FAIL（`results[0].ToolMsg` 为 nil 或超时未触发）

- [ ] **Step 3: 在 invoke 点包 context**

`ability_manager.go:927` 的 `result, err := lt.Invoke(ctx, toolArgs, invokeOpts...)` 替换为：

```go
	// 外层调用预算：卡片声明 → 默认 300 → 豁免提升为 3600 → 工具钩子（bash/ps 再加 30s 清理宽限）
	// Python: ability_manager.py:1755-1773（注册路径）/ :1901-1907（回退路径）
	callTimeout := resolveInvocationTimeout(lt, toolArgs, effectiveCallTimeout(resolveCallTimeout(t.Card())))
	invokeCtx, cancelInvoke := context.WithTimeout(ctx, time.Duration(callTimeout*float64(time.Second)))
	defer cancelInvoke()

	result, err := lt.Invoke(invokeCtx, toolArgs, invokeOpts...)
	if err != nil && errors.Is(invokeCtx.Err(), context.DeadlineExceeded) {
		// context.Canceled 保持原样，只有真正的 deadline 才转成工具超时
		err = NewAbilityExecutionError(
			exception.StatusToolLocalFunctionExecutionError,
			toolCall.ID,
			fmt.Sprintf("Tool '%s' timed out after %vs", toolName, callTimeout),
			exception.WithParam("tool_name", toolName),
		)
	}
```

`:1206` 的 `result, invokeErr := lt.Invoke(ctx, toolArgs, tool.WithToolSession(sess))`（回退路径）同样包裹，`card` 取该处持有的工具卡片。

- [ ] **Step 4: 确认超时错误已产出 ToolMessage**

**无需额外代码**：`runToolCallSafely`（`ability_manager.go:605-609`）对 error 走 `errorToExecuteResult`，而 `errorToExecuteResult`（`ability_types.go:139-152`）对任何非 `ToolInterruptException` 的 error 都会用 `llmschema.NewToolMessage(toolCallID, err.Error())` 补 `ToolMsg`；若 err 是 `*AbilityExecutionError` 且带 `ToolMessage`，则用其自带的。`NewAbilityExecutionError` 构造的是 `AbilityExecutionError`，其 `ToolMessage` 由构造函数按 `toolCallID` + msg 填充，满足「含 timed out 且 tool_call_id 正确」。

用 Task 的测试直接验证这条链路即可，不改代码。

- [ ] **Step 5: 跑测试**

Run: `go test ./internal/agentcore/single_agent/ability/ -run 'TestExecute_' -v`
Expected: PASS（含既有用例不回归）

- [ ] **Step 6: 全量回归**

Run: `go build ./... && go vet ./... && go test ./internal/agentcore/single_agent/... ./internal/agentcore/harness/...`
Expected: 全 PASS

- [ ] **Step 7: 提交**

```bash
git add -u internal/agentcore/single_agent/ability/
git commit -m "fix(ability): 工具调用包裹外层超时，超时产出可恢复 tool_message（D-04）"
```

---

## 批次 D-C：技能与 worktree 路径安全

### Task C1: slug 扁平化

**Files:**
- Modify: `internal/agentcore/harness/tools/worktree/slug.go:47-68`
- Test: `internal/agentcore/harness/tools/worktree/slug_test.go`

- [ ] **Step 1: 写失败测试**

`slug_test.go` 追加：

```go
// TestWorktreePathFor_嵌套slug扁平化 验证 user/feature 落盘为 user+feature
// 理由：嵌套目录在删除父 worktree 时会连带删掉子 worktree
func TestWorktreePathFor_嵌套slug扁平化(t *testing.T) {
	got := WorktreePathFor("/ws", "user/feat")
	want := filepath.Join("/ws", ".worktrees", "user+feat")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestWorktreePathFor_拒绝穿越 验证 ../escape 不被扁平化绕过
func TestWorktreePathFor_拒绝穿越(t *testing.T) {
	if got := WorktreePathFor("/ws", "../escape"); got != "" {
		t.Fatalf("穿越 slug 应返回空路径，实际 %q", got)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/harness/tools/worktree/ -run 'TestWorktreePathFor_' -v`
Expected: FAIL，`got "/ws/.worktrees/user/feat", want "/ws/.worktrees/user+feat"`

- [ ] **Step 3: 加扁平化函数并改两个函数**

`slug.go` 追加：

```go
// flattenSlug 把已校验的 slug 扁平化为单层路径段。
// Python: _flatten_slug (worktree/slug.py:42-52)
//
// 形如 "user/feature" 的嵌套 slug 对用户友好，但映射到嵌套目录时，
// 删除父 worktree 会连带删掉其下所有子 worktree。
func flattenSlug(slug string) string {
	return strings.ReplaceAll(slug, "/", "+")
}
```

`WorktreeBranchName` 改为：

```go
func WorktreeBranchName(slug string) string {
	if err := ValidateSlug(slug); err != nil {
		return ""
	}
	return "worktree-" + flattenSlug(slug)
}
```

`WorktreePathFor` 改为：

```go
// WorktreePathFor 计算 worktree 目录路径。
// Python: worktree_path_for(base_dir, slug) (worktree/slug.py:70-90)
//
// 路径格式：{baseDir}/.worktrees/{flattened-slug}；slug 非法时返回空串。
func WorktreePathFor(baseDir, slug string) string {
	if err := ValidateSlug(slug); err != nil {
		return ""
	}
	return filepath.Join(WorktreesDir(baseDir), flattenSlug(slug))
}
```

- [ ] **Step 4: 修既有测试**

`slug_test.go` 中原断言 `{base}/.worktrees/{slug}` 的用例同步改扁平化期望。

Run: `go test ./internal/agentcore/harness/tools/worktree/ -run 'TestWorktreePathFor|TestWorktreeBranchName' -v`
Expected: PASS

- [ ] **Step 5: 全包回归**

Run: `go test ./internal/agentcore/harness/tools/worktree/`
Expected: PASS（`tools.go:287`、`manager.go:463`、`backend.go:140` 的调用方需对空串做 error 处理，若编译或测试失败则补齐）

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/harness/tools/worktree/slug.go internal/agentcore/harness/tools/worktree/slug_test.go
git commit -m "fix(worktree): slug 扁平化，避免删父目录连带删子 worktree（D-05）"
```

---

### Task C2: `ValidateSlug` 拒绝 Windows 分隔符与盘符

**Files:**
- Modify: `internal/agentcore/harness/tools/worktree/slug.go:32-45`
- Test: `internal/agentcore/harness/tools/worktree/slug_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestValidateSlug_Windows路径 验证反斜杠与盘符被拒绝
// Python: test_path_traversal_windows_separator / test_windows_drive_path_rejected
func TestValidateSlug_Windows路径(t *testing.T) {
	for _, bad := range []string{`..\evil`, `C:\evil`, `a\b`, `..\\..\\x`} {
		if err := ValidateSlug(bad); err == nil {
			t.Fatalf("%q 应被拒绝", bad)
		}
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/harness/tools/worktree/ -run TestValidateSlug_Windows路径 -v`
Expected: FAIL（`a\b` 当前会被正则拒绝，但 `C:\evil` 的错误原因不对；若全部通过则说明仅需补错误文案，跳到 Step 4）

- [ ] **Step 3: 归一化后校验**

`ValidateSlug` 改为：

```go
// ValidateSlug 校验 worktree slug 安全性。
// Python: validate_slug (worktree/slug.py:20-40)
//
// 拒绝路径遍历、绝对路径、Windows 分隔符与盘符、shell 元字符和超长名称。
func ValidateSlug(slug string) error {
	if len(slug) > MaxSlugLength {
		return fmt.Errorf("invalid worktree name: must be %d characters or fewer (got %d)", MaxSlugLength, len(slug))
	}
	// 反斜杠在 Windows 上也是分隔符，先归一化再分段校验，
	// 否则 `..\evil` 会被当成单个段名而给出误导性的错误
	normalized := strings.ReplaceAll(slug, "\\", "/")
	for _, segment := range strings.Split(normalized, "/") {
		if segment == "." || segment == ".." {
			return fmt.Errorf("invalid worktree name %q: must not contain \".\" or \"..\" path segments", slug)
		}
		if !validSlugSegment.MatchString(segment) {
			return fmt.Errorf("invalid worktree name %q: each segment must be non-empty and contain only letters, digits, dots, underscores, and dashes", slug)
		}
	}
	return nil
}
```

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/agentcore/harness/tools/worktree/ -run TestValidateSlug -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/tools/worktree/slug.go internal/agentcore/harness/tools/worktree/slug_test.go
git commit -m "fix(worktree): slug 校验归一化反斜杠并拒盘符（D-05）"
```

---

### Task C3: `gitdir` / `commondir` 越界校验

**Files:**
- Modify: `internal/agentcore/harness/tools/worktree/git.go:259-313`
- Test: `internal/agentcore/harness/tools/worktree/git_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestReadWorktreeHeadSHA_拒绝gitdir越界 验证 .git 指向外部目录时返回错误
func TestReadWorktreeHeadSHA_拒绝gitdir越界(t *testing.T) {
	wt := t.TempDir()
	outside := t.TempDir()
	// 外部目录放一个看似合法的 HEAD，若校验缺失就会被读到
	if err := os.WriteFile(filepath.Join(outside, "HEAD"), []byte("0123456789012345678901234567890123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(wt, outside)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+rel+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sha, err := readWorktreeHeadSHA(wt)
	if err == nil {
		t.Fatalf("越界 gitdir 应返回错误，实际读到 sha=%q", sha)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/harness/tools/worktree/ -run TestReadWorktreeHeadSHA_拒绝gitdir越界 -v`
Expected: FAIL，`越界 gitdir 应返回错误，实际读到 sha="0123..."`

- [ ] **Step 3: 加校验函数并接入**

`git.go` 追加：

```go
// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveDirWithin 把相对路径 rel 解析到 base 下，并要求结果不逃出 base。
//
// 用于 .git 文件的 gitdir 与 commondir 内容：这两处都是文件内容而非
// 用户直接输入，被篡改后可能指向 worktree 之外的目录。
func resolveDirWithin(base, rel, label string) (string, error) {
	resolved := filepath.Clean(filepath.Join(base, rel))
	relBack, err := filepath.Rel(base, resolved)
	if err != nil || relBack == ".." || strings.HasPrefix(relBack, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid %s %q escapes %s", label, rel, base)
	}
	return resolved, nil
}
```

`readWorktreeHeadSHA` 中 `:274` 处替换为：

```go
	gitDir, err := resolveDirWithin(wtPath, strings.TrimSpace(strings.TrimPrefix(content, "gitdir:")), "gitdir")
	if err != nil {
		return "", err
	}
```

`:305-306` 处（commondir 解析）替换为：

```go
	common := strings.TrimSpace(string(commonData))
	commonAbs, err := resolveDirWithin(gitDir, common, "commondir")
	if err != nil {
		return "", err
	}
```

> 注意：`commondir` 的基准是 `gitDir`，而上游 worktree 的 commondir 常写成 `../..`（指向主仓库 `.git`）。这属于**合法**的向上跳。因此 commondir 的校验不能禁止 `..`，只能要求「解析后仍是一个目录」。实施时改为：

```go
	commonAbs := filepath.Clean(filepath.Join(gitDir, common))
	if info, statErr := os.Stat(commonAbs); statErr != nil || !info.IsDir() {
		return "", fmt.Errorf("invalid commondir %q", common)
	}
```

`gitdir` 则必须用 `resolveDirWithin`（worktree 的 `.git` 必须指向 worktree 自身目录之内或与之同级的 git 目录；按上游语义，gitdir 内容相对 worktree 解析后须落在 `wtPath` 下）。

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/agentcore/harness/tools/worktree/ -run 'TestReadWorktreeHeadSHA' -v`
Expected: PASS

- [ ] **Step 5: 全包回归（覆盖真实 worktree 路径）**

Run: `go test ./internal/agentcore/harness/tools/worktree/ -v`
Expected: PASS；若既有 gitdir 用例用的是 `../../.git/worktrees/x` 这类形式上跳的路径，需按 Step 3 注释调整判据为「解析结果必须是目录且 basename 非空」，并在测试里补真实布局用例。

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/harness/tools/worktree/git.go internal/agentcore/harness/tools/worktree/git_test.go
git commit -m "fix(worktree): gitdir/commondir 内容加越界校验（D-05）"
```

---

### Task C4: teamskillshub init 路径语义与骨架

**Files:**
- Modify: `internal/swarm/server/runtime/skill/skill_manager.go:1400-1434`
- Test: `internal/swarm/server/runtime/skill/skill_manager_test.go`

- [ ] **Step 1: 写失败测试**

`skill_manager_test.go` 追加：

```go
// TestHandleSkillsTeamSkillsHubInit_拒绝路径穿越 验证 path 越界与 name 非法均被拒
func TestHandleSkillsTeamSkillsHubInit_拒绝路径穿越(t *testing.T) {
	root := t.TempDir()
	sm := NewSkillManagerForTest(root)

	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"绝对路径", map[string]any{"name": "s1", "path": "/tmp"}, "path 仅支持相对路径"},
		{"上跳越界", map[string]any{"name": "s1", "path": "../../etc"}, "path 越界"},
		{"家目录", map[string]any{"name": "s1", "path": "~/x"}, "path 仅支持相对路径"},
		{"name 带斜杠", map[string]any{"name": "../../x"}, "无效"},
		{"output 别名同样校验", map[string]any{"name": "s1", "output": "/etc"}, "path 仅支持相对路径"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := sm.HandleSkillsTeamSkillsHubInit(context.Background(), tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if got["success"] != false {
				t.Fatalf("应失败，实际 %v", got)
			}
			if detail, _ := got["detail"].(string); !strings.Contains(detail, tc.want) {
				t.Fatalf("detail 应含 %q，实际 %q", tc.want, detail)
			}
		})
	}
}

// TestHandleSkillsTeamSkillsHubInit_teamskills骨架 验证 teamskills 类型产出 kind 与 roles
func TestHandleSkillsTeamSkillsHubInit_teamskills骨架(t *testing.T) {
	root := t.TempDir()
	sm := NewSkillManagerForTest(root)

	got, err := sm.HandleSkillsTeamSkillsHubInit(context.Background(), map[string]any{
		"name": "demo", "skill_type": "teamskills",
	})
	if err != nil || got["success"] != true {
		t.Fatalf("应成功，实际 %v err=%v", got, err)
	}
	body, err := os.ReadFile(filepath.Join(root, "skills", "demo", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "kind: team-skill") {
		t.Fatalf("teamskills 骨架应含 kind: team-skill，实际:\n%s", body)
	}
	if strings.Count(string(body), "- id: role_") != 2 {
		t.Fatalf("应有 2 个 roles，实际:\n%s", body)
	}
}

// TestHandleSkillsTeamSkillsHubInit_force覆盖 验证非空目录需 force 才重建
func TestHandleSkillsTeamSkillsHubInit_force覆盖(t *testing.T) {
	root := t.TempDir()
	sm := NewSkillManagerForTest(root)
	target := filepath.Join(root, "skills", "demo")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "junk.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, _ := sm.HandleSkillsTeamSkillsHubInit(context.Background(), map[string]any{"name": "demo"})
	if got["success"] != false || !strings.Contains(got["detail"].(string), "非空") {
		t.Fatalf("无 force 应报目标目录非空，实际 %v", got)
	}

	got, _ = sm.HandleSkillsTeamSkillsHubInit(context.Background(), map[string]any{"name": "demo", "force": true})
	if got["success"] != true {
		t.Fatalf("force=true 应重建成功，实际 %v", got)
	}
	if _, err := os.Stat(filepath.Join(target, "junk.txt")); !os.IsNotExist(err) {
		t.Fatal("force 后旧文件应被清除")
	}
}
```

> `NewSkillManagerForTest` 按该包既有构造方式替换（`grep -n "func NewSkillManager" internal/swarm/server/runtime/skill/skill_manager.go`）。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/swarm/server/runtime/skill/ -run TestHandleSkillsTeamSkillsHubInit -v`
Expected: FAIL（`path` 参数未被识别，仍按 `output` 走；teamskills 骨架缺 `kind`）

- [ ] **Step 3: 重写 init 函数前半段**

`skill_manager.go:1402-1434` 整体替换：

```go
func (sm *SkillManager) HandleSkillsTeamSkillsHubInit(ctx context.Context, params map[string]any) (map[string]any, error) {
	// ── 参数解析 ──
	nameRaw := trimSpace(toString(params["name"]))
	if nameRaw == "" {
		return map[string]any{"success": false, "detail": "缺少参数: name"}, nil
	}
	name, err := safePathName(nameRaw, "skill")
	if err != nil {
		logRejectedName("skills.teamskillshub.init", "skill", nameRaw, err)
		return map[string]any{"success": false, "detail": err.Error()}, nil
	}

	// path 为主参数；output 为兼容别名，走同一套校验（不能放行任意绝对路径）
	parentRaw := trimSpace(toString(params["path"]))
	if parentRaw == "" {
		parentRaw = trimSpace(toString(params["output"]))
	}
	if parentRaw == "" {
		parentRaw = "."
	}

	skillType := strings.ToLower(trimSpace(toString(params["skill_type"])))
	if skillType == "" {
		skillType = strings.ToLower(trimSpace(toString(params["plugin_type"])))
	}
	if skillType == "" {
		skillType = strings.ToLower(trimSpace(toString(params["type"])))
	}
	if skillType == "" {
		skillType = "teamskills"
	}
	if skillType != "teamskills" && skillType != "skill" {
		return map[string]any{"success": false, "detail": "type 仅支持: teamskills 或 skill"}, nil
	}
	force := toBool(params["force"])

	// ── path 边界收敛：仅允许 skills 目录内的相对路径 ──
	// Python: skill_manager.py handle_skills_team_skills_hub_init
	if parentRaw == "~" || strings.HasPrefix(parentRaw, "~/") || strings.HasPrefix(parentRaw, `~\`) {
		return map[string]any{"success": false, "detail": "path 仅支持相对路径（限制在 skills 目录内）"}, nil
	}
	if filepath.IsAbs(parentRaw) || isWindowsAbsPath(parentRaw) {
		return map[string]any{"success": false, "detail": "path 仅支持相对路径（限制在 skills 目录内）"}, nil
	}
	skillsRoot := pathutil.ResolveSymlinksLenient(sm.skillsDir)
	parentDir := pathutil.ResolveSymlinksLenient(filepath.Join(sm.skillsDir, parentRaw))
	if !pathutil.IsPathWithin(skillsRoot, parentDir) {
		return map[string]any{"success": false, "detail": "path 越界：仅允许在 skills 目录内创建"}, nil
	}
	if !dirExists(parentDir) {
		return map[string]any{"success": false, "detail": fmt.Sprintf("path 不是有效目录: %s", parentDir)}, nil
	}

	// target 双保险
	targetDir := pathutil.ResolveSymlinksLenient(filepath.Join(parentDir, name))
	if !pathutil.IsPathWithin(skillsRoot, targetDir) {
		return map[string]any{"success": false, "detail": "目标路径越界：仅允许在 skills 目录内创建"}, nil
	}

	// ── 已存在处理 ──
	if dirExists(targetDir) {
		if entries, readErr := os.ReadDir(targetDir); readErr != nil || len(entries) > 0 {
			if !force {
				return map[string]any{"success": false, "detail": fmt.Sprintf("目标目录非空: %s", targetDir)}, nil
			}
			if err := safeRmtree(targetDir); err != nil {
				return map[string]any{"success": false, "detail": "清理目标目录失败: " + err.Error()}, nil
			}
		}
	}

	// ── 建目录与骨架 ──
	if err := os.MkdirAll(filepath.Join(targetDir, "tools"), 0o755); err != nil {
		return map[string]any{"success": false, "detail": "创建目录失败: " + err.Error()}, nil
	}
	if err := os.MkdirAll(filepath.Join(targetDir, "data"), 0o755); err != nil {
		return map[string]any{"success": false, "detail": "创建目录失败: " + err.Error()}, nil
	}
	if err := os.WriteFile(filepath.Join(targetDir, "SKILL.md"), []byte(buildSkillScaffold(name, skillType)), 0o644); err != nil {
		return map[string]any{"success": false, "detail": "写入 SKILL.md 失败: " + err.Error()}, nil
	}

	return map[string]any{"success": true, "path": targetDir}, nil
}
```

追加两个助手：

```go
// buildSkillScaffold 生成 SKILL.md 骨架。
// Python: handle_skills_team_skills_hub_init 内的 content 分支
func buildSkillScaffold(name, skillType string) string {
	if skillType == "teamskills" {
		return "---\n" +
			"name: " + name + "\n" +
			`description: "TODO: describe this team skill."` + "\n" +
			"kind: team-skill\n" +
			"roles:\n" +
			"  - id: role_01\n" +
			"    purpose: role_01_purpose\n" +
			"  - id: role_02\n" +
			"    purpose: role_02_purpose\n" +
			"---\n\n" +
			"# " + name + "\n\n" +
			"## Instructions\n\n" +
			"TODO: add step-by-step guidance.\n"
	}
	return "---\n" +
		"name: " + name + "\n" +
		`description: "TODO: describe this skill."` + "\n" +
		"---\n\n" +
		"# " + name + "\n\n" +
		"## Instructions\n\n" +
		"TODO: add step-by-step guidance.\n"
}

// isWindowsAbsPath 判断是否 Windows 绝对路径（跨平台判定，便于在 Linux 上测试）。
func isWindowsAbsPath(p string) bool {
	if len(p) >= 2 && p[1] == ':' {
		return true
	}
	return strings.HasPrefix(p, `\\`) || strings.HasPrefix(p, "//")
}
```

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/swarm/server/runtime/skill/ -run TestHandleSkillsTeamSkillsHubInit -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/server/runtime/skill/skill_manager.go internal/swarm/server/runtime/skill/skill_manager_test.go
git commit -m "fix(skill): teamskillshub init 收敛 path 到 skills 目录并补齐骨架语义（D-08）"
```

---

### Task C5: 技能卸载 slug 兜底

**Files:**
- Modify: `internal/swarm/server/runtime/skill/skill_manager.go:681-725`
- Test: `internal/swarm/server/runtime/skill/skill_manager_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestHandleSkillsUninstall_不安全name用slug兜底 验证含 / 的记录可被卸载
func TestHandleSkillsUninstall_不安全name用slug兜底(t *testing.T) {
	root := t.TempDir()
	sm := NewSkillManagerForTest(root)

	// 模拟历史安装写入的脏记录：name 含 /，目录实际用扁平化的 slug
	dir := filepath.Join(root, "skills", "a+b")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sm.state["local_skills"] = []map[string]any{
		{"name": "a/b", "origin": "teamskillshub:a+b"},
	}

	got, err := sm.HandleSkillsUninstall(context.Background(), map[string]any{"name": "a/b"})
	if err != nil {
		t.Fatal(err)
	}
	if got["success"] != true {
		t.Fatalf("应卸载成功，实际 %v", got)
	}
	if dirExists(dir) {
		t.Fatal("目录应被删除")
	}
	local, _ := sm.state["local_skills"].([]map[string]any)
	for _, s := range local {
		if toString(s["name"]) == "a/b" {
			t.Fatal("记录应被清除（按原始 name 匹配）")
		}
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/swarm/server/runtime/skill/ -run TestHandleSkillsUninstall_不安全name用slug兜底 -v`
Expected: FAIL，`应卸载成功，实际 map[detail:... success:false]`

- [ ] **Step 3: 改卸载逻辑**

`skill_manager.go:686-690` 替换为：

```go
	safeName, err := safePathName(name, "skill")
	if err != nil {
		// name 含不安全字符（如 /）：从 local_skills 的 origin 提取 slug 定位目录。
		// 记录本身仍按原始 name 删除——记录里的键就是原始 name。
		// Python: handle_skills_uninstall (skill_manager.py:1885-1905)
		slug := ""
		if local, ok := sm.state["local_skills"].([]map[string]any); ok {
			for _, ls := range local {
				if toString(ls["name"]) == name {
					if origin := toString(ls["origin"]); strings.Contains(origin, ":") {
						slug = origin[strings.LastIndex(origin, ":")+1:]
					}
					break
				}
			}
		}
		if slug == "" {
			logRejectedName("skills.uninstall", "skill", name, err)
			return map[string]any{"success": false, "detail": err.Error()}, nil
		}
		resolved, slugErr := safePathName(slug, "skill")
		if slugErr != nil {
			logRejectedName("skills.uninstall", "skill", name, err)
			return map[string]any{"success": false, "detail": err.Error()}, nil
		}
		safeName = resolved
	}
```

`:720-721` 两处删除记录改为传原始 `name`：

```go
	sm.removeInstalledPlugin(name)
	sm.removeLocalSkill(name)
```

> 注意：`sm.mu.Lock()` 在 `:692`，而 Step 3 的兜底查表读 `sm.state` 发生在加锁**之前**。把 Step 3 的兜底代码块移到 `sm.mu.Lock(); defer sm.mu.Unlock()` 之后，避免数据竞争。

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/swarm/server/runtime/skill/ -run TestHandleSkillsUninstall -v`
Expected: PASS

- [ ] **Step 5: race 检测**

Run: `go test -race ./internal/swarm/server/runtime/skill/ -run TestHandleSkillsUninstall`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/swarm/server/runtime/skill/skill_manager.go internal/swarm/server/runtime/skill/skill_manager_test.go
git commit -m "fix(skill): 卸载不安全 name 时用 origin slug 兜底定位目录（D-09）"
```

---

## 批次 D-D：读取预算与 shell 检测

### Task D1: notebook token 预算截断

**Files:**
- Modify: `internal/agentcore/harness/tools/filesystem/read_file.go:289-374`
- Test: `internal/agentcore/harness/tools/filesystem/read_file_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestReadNotebook_超预算截断 验证超预算时截断并给出提示，而不是整体报错
func TestReadNotebook_超预算截断(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`{"cells":[`)
	for i := 0; i < 60; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		body := strings.Repeat("x", 4000) // 每 cell 约 1000 token
		sb.WriteString(`{"cell_type":"code","source":["` + body + `"]}`)
	}
	sb.WriteString(`]}`)

	dir := t.TempDir()
	path := filepath.Join(dir, "big.ipynb")
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	op := newTestLocalOperation(t, dir)
	got, err := readNotebook(context.Background(), op, path)
	if err != nil {
		t.Fatalf("超预算应截断而非报错，实际 %v", err)
	}
	content, _ := got["content"].(string)
	if !strings.Contains(content, "cells extracted") {
		t.Fatalf("应含截断提示，实际长度 %d", len(content))
	}
	if !strings.Contains(content, "-token budget reached") {
		t.Fatalf("应含 budget reached 提示")
	}
}

// TestReadNotebook_首块无条件放行 验证第一个 cell 超预算也输出
func TestReadNotebook_首块无条件放行(t *testing.T) {
	huge := strings.Repeat("y", 200000) // 约 50000 token，单块就超预算
	body := `{"cells":[{"cell_type":"code","source":["` + huge + `"]}]}`
	dir := t.TempDir()
	path := filepath.Join(dir, "one.ipynb")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	op := newTestLocalOperation(t, dir)
	got, err := readNotebook(context.Background(), op, path)
	if err != nil {
		t.Fatalf("首块应无条件放行，实际 %v", err)
	}
	content, _ := got["content"].(string)
	if len(content) < 100000 {
		t.Fatalf("首块内容应被完整输出，实际长度 %d", len(content))
	}
	if strings.Contains(content, "truncated") {
		t.Fatal("单块不应触发截断提示")
	}
}
```

> `newTestLocalOperation` 按该包既有测试构造方式替换。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/harness/tools/filesystem/ -run 'TestReadNotebook_' -v`
Expected: FAIL，`超预算应截断而非报错，实际 file content (...) exceeds maximum allowed tokens`

- [ ] **Step 3: 加预算类型并改写组装循环**

`read_file.go` 追加类型（放结构体区块）：

```go
// tokenBudget 按固定预算累积 token 消耗，用于渐进式抽取时的截断判断。
// Python: _TokenBudget (filesystem.py:255-270)
type tokenBudget struct {
	maxTokens int
	spent     int
}

// canFit 判断再消耗 tokens 后是否仍在预算内。
func (b *tokenBudget) canFit(tokens int) bool { return b.spent+tokens <= b.maxTokens }

// spend 记入已消耗 token。
func (b *tokenBudget) spend(tokens int) { b.spent += tokens }
```

`readNotebook` 中删除 `:315-323` 的全文 token 报错块（保留字节大小检查），并把组装循环（`:341-368`）替换为：

```go
	budget := &tokenBudget{maxTokens: maxTokens}
	var blocks []string
	extractedCount := 0
	for idx, cell := range cells {
		cellType := "unknown"
		if ctRaw, ok := cell["cell_type"]; ok {
			var ct string
			if json.Unmarshal(ctRaw, &ct) == nil {
				cellType = ct
			}
		}

		cellLines := []string{fmt.Sprintf("## Cell %d [%s]", idx+1, cellType)}
		if source := extractNotebookString(cell, "source"); source != "" {
			cellLines = append(cellLines, strings.TrimRight(source, "\n"))
		}
		if outputsRaw, hasOutputs := cell["outputs"]; hasOutputs {
			var outputs []map[string]json.RawMessage
			if json.Unmarshal(outputsRaw, &outputs) == nil && len(outputs) > 0 {
				cellLines = append(cellLines, "### Outputs")
				for _, out := range outputs {
					if text := extractOutputText(out); text != "" {
						cellLines = append(cellLines, strings.TrimRight(text, "\n"))
					}
				}
			}
		}

		cellText := strings.Join(cellLines, "\n")
		cellTokens := estimateTokens(cellText)
		// 首块无条件放行：避免第一个 cell 自身超预算导致输出为空
		if extractedCount > 0 && !budget.canFit(cellTokens) {
			blocks = append(blocks, fmt.Sprintf(
				"… (truncated — %d/%d cells extracted, %d-token budget reached)",
				extractedCount, len(cells), maxTokens))
			break
		}
		budget.spend(cellTokens)
		blocks = append(blocks, cellText)
		extractedCount++
	}
```

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/agentcore/harness/tools/filesystem/ -run 'TestReadNotebook_' -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/tools/filesystem/read_file.go internal/agentcore/harness/tools/filesystem/read_file_test.go
git commit -m "fix(filesystem): notebook 读取按 cell 做 token 预算截断（D-10）"
```

---

### Task D2: PDF token 预算截断

**Files:**
- Modify: `internal/agentcore/harness/tools/filesystem/read_file.go:428-470`
- Test: `internal/agentcore/harness/tools/filesystem/read_file_test.go`

- [ ] **Step 1: 写失败测试**

PDF 需要真实文件，用 `ledongthuc/pdf` 无法直接生成。改为验证**截断判定函数**的独立逻辑：

```go
// TestTokenBudget_语义 验证预算累加与首块放行判据
func TestTokenBudget_语义(t *testing.T) {
	b := &tokenBudget{maxTokens: 100}
	if !b.canFit(100) {
		t.Fatal("恰好等于预算应可容纳")
	}
	if b.canFit(101) {
		t.Fatal("超出预算不应可容纳")
	}
	b.spend(60)
	if !b.canFit(40) {
		t.Fatal("剩余 40 应可容纳")
	}
	if b.canFit(41) {
		t.Fatal("剩余 40 不应容纳 41")
	}
}

// TestReadPDF_逐页预算 验证按页截断文案（用最小合法 PDF 构造 3 页）
func TestReadPDF_逐页预算(t *testing.T) {
	pdfBytes := buildMinimalPDF(t, 3)
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.pdf")
	if err := os.WriteFile(path, pdfBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	op := newTestLocalOperation(t, dir)
	got, err := readPDF(context.Background(), op, path, "1-3")
	if err != nil {
		t.Fatalf("readPDF error: %v", err)
	}
	content, _ := got["content"].(string)
	if !strings.Contains(content, "## Page 1") {
		t.Fatalf("应至少输出首页，实际 %q", content)
	}
}
```

（`buildMinimalPDF` 是测试助手，需在测试文件里实现一个最小 PDF 生成器；实施时若构造代价过高，可改为只测 `tokenBudget` 与「截断文案模板」两个纯函数，并在测试注释里写明未覆盖真实 PDF 分页路径的原因。）

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/harness/tools/filesystem/ -run 'TestTokenBudget_语义' -v`
Expected: PASS（Task D1 已实现 `tokenBudget`）；`TestReadPDF_逐页预算` 若 PDF 助手未就绪则编译失败

- [ ] **Step 3: 改写按页提取循环**

`read_file.go:436-468` 替换为：

```go
	// 按页提取文本，按 token 预算截断
	budget := &tokenBudget{maxTokens: maxTokens}
	var parts []string
	extractedCount := 0
	for pageNo := startPg; pageNo <= endPg; pageNo++ {
		page := reader.Page(pageNo)
		pageText := ""
		if !page.V.IsNull() {
			for _, name := range page.Fonts() {
				if _, ok := fonts[name]; !ok {
					f := page.Font(name)
					fonts[name] = &f
				}
			}
			extracted, err := page.GetPlainText(fonts)
			if err != nil {
				logger.Debug(logComponent).
					Str("file_path", filePath).
					Int("page_no", pageNo).
					Err(err).
					Msg("ReadFileTool failed to extract PDF page text")
			} else {
				pageText = extracted
			}
		}

		pageBlock := strings.TrimRight(fmt.Sprintf("## Page %d\n%s", pageNo, pageText), " \t\n\r")
		pageTokens := estimateTokens(pageBlock)
		// 首块无条件放行
		if extractedCount > 0 && !budget.canFit(pageTokens) {
			parts = append(parts, fmt.Sprintf(
				"… (truncated — %d/%d pages extracted, %d-token budget reached)",
				extractedCount, pageCount, maxTokens))
			break
		}
		budget.spend(pageTokens)
		parts = append(parts, pageBlock)
		extractedCount++
	}
```

> `pageCount` 是既有变量（`endPg - startPg + 1`），与上游文案里的总页数一致。

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/agentcore/harness/tools/filesystem/ -run 'TestReadPDF|TestTokenBudget' -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/tools/filesystem/read_file.go internal/agentcore/harness/tools/filesystem/read_file_test.go
git commit -m "fix(filesystem): PDF 读取按页做 token 预算截断（D-10）"
```

---

### Task D3: PowerShell 赋值判定与 bash 控制流识别

**Files:**
- Modify: `internal/agentcore/sys_operation/local/shell_helpers.go:33-35`、`:74-92`、`:320-335`
- Modify: `internal/agentcore/sys_operation/local/shell_operation.go:812-830`
- Test: `internal/agentcore/sys_operation/local/shell_helpers_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestLooksLikePowerShell_裸变量不误判 验证 $i 这类 bash 也合法的语法不单独判 PS
func TestLooksLikePowerShell_裸变量不误判(t *testing.T) {
	if LooksLikePowerShell("for i in 1 2; do echo $i; done") {
		t.Fatal("裸 $i 不应单独判为 PowerShell")
	}
}

// TestLooksLikePowerShell_赋值判PS 验证 $x = 1 是 PowerShell 独有
func TestLooksLikePowerShell_赋值判PS(t *testing.T) {
	for _, cmd := range []string{`$x = 1`, `$name = "a"`, `$n += 2`} {
		if !LooksLikePowerShell(cmd) {
			t.Fatalf("%q 应判为 PowerShell", cmd)
		}
	}
}

// TestLooksLikePowerShell_比较不误判 验证 $x == 1 不被当赋值
func TestLooksLikePowerShell_比较不误判(t *testing.T) {
	if LooksLikePowerShell(`if [ "$x" == 1 ]; then echo ok; fi`) {
		t.Fatal("比较运算不应判为 PowerShell 赋值")
	}
}

// TestLooksLikePosix_控制流识别 验证 for/while/if 结构判为 POSIX
func TestLooksLikePosix_控制流识别(t *testing.T) {
	cases := []string{
		"for i in a b; do echo $i; done",
		"while true; do sleep 1; done",
		"if [ -f x ]; then echo y; fi",
	}
	for _, cmd := range cases {
		if !LooksLikePosix(cmd) {
			t.Fatalf("%q 应判为 POSIX", cmd)
		}
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/sys_operation/local/ -run 'TestLooksLike' -v`
Expected: FAIL，`裸 $i 不应单独判为 PowerShell`

- [ ] **Step 3: 加赋值模式与控制流模式**

`shell_helpers.go:33-35` 处追加：

```go
// psAssignmentPattern PowerShell 赋值模式（`$name = ...`）。
// 裸 `$var` 在 bash 与 PowerShell 中都合法，不能单独作为判定依据；
// `$name =` 带赋值才是 PowerShell 独有的写法。
// Python: _PS_ASSIGNMENT_PATTERN (shell_operation.py:44)
var psAssignmentPattern = regexp.MustCompile(`(^|[\s;(])\$[A-Za-z_][A-Za-z0-9_]*\s*[-+*/]?=(?!=)`)

// bashControlFlowPatterns bash 控制流结构（for/while/until/if），用于 POSIX 判定。
// Python: _BASH_CONTROL_FLOW_PATTERNS (shell_operation.py:53-64)
var bashControlFlowPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?is)(?:^|[;\r\n])\s*for\s+(?:[A-Za-z_][A-Za-z0-9_]*\s+in\b|\(\().*?(?:;|[\r\n])\s*do\b.*?(?:;|[\r\n])\s*done\b`),
	regexp.MustCompile(`(?is)(?:^|[;\r\n])\s*(?:while|until)\b.*?(?:;|[\r\n])\s*do\b.*?(?:;|[\r\n])\s*done\b`),
	regexp.MustCompile(`(?is)(?:^|[;\r\n])\s*if\b.*?(?:;|[\r\n])\s*then\b.*?(?:;|[\r\n])\s*fi\b`),
}
```

`LooksLikePowerShell` 的 `psVariablePattern` 判定改为 `psAssignmentPattern`：

```go
	if psAssignmentPattern.MatchString(command) {
		return true
	}
```

`LooksLikePosix` 开头加控制流判定：

```go
func LooksLikePosix(command string) bool {
	for _, pattern := range bashControlFlowPatterns {
		if pattern.MatchString(command) {
			return true
		}
	}
	for _, segment := range SplitShellSegments(command) {
		base := SegmentBaseCommand(segment)
		if posixCommands[base] {
			return true
		}
	}
	return false
}
```

> Go 的 RE2 不支持 `(?!=)` 负向前瞻。`(?!=)` 在 RE2 里会被当作「字面 `(?`」而编译失败。改用等价写法：匹配 `=` 后再排除 `==`。把 `psAssignmentPattern` 写成：

```go
var psAssignmentPattern = regexp.MustCompile(`(^|[\s;(])\$[A-Za-z_][A-Za-z0-9_]*\s*[-+*/]?=([^=]|$)`)
```

- [ ] **Step 4: auto 分支下沉裸变量判定**

`shell_operation.go:812-830` 的 Windows auto 分支，在 `LooksLikePosix` 之后、`cmd` 兜底之前插入：

```go
			// 裸 `$name` 在 bash 与 PowerShell 中共享，只有 POSIX 判定未命中时才按 PowerShell 处理
			if psVariablePattern.MatchString(command) {
				exe := AvailablePowerShell()
				return []string{exe, "-NoProfile", "-NonInteractive", "-Command", command}, false, "powershell", nil
			}
```

- [ ] **Step 5: 跑测试**

Run: `go test ./internal/agentcore/sys_operation/local/ -run 'TestLooksLike' -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/sys_operation/local/shell_helpers.go internal/agentcore/sys_operation/local/shell_operation.go internal/agentcore/sys_operation/local/shell_helpers_test.go
git commit -m "fix(sys_operation): 修正 PowerShell 裸变量误判并补 bash 控制流识别（D-12）"
```

---

### Task D4: Windows auto 分支平台测试（build tag 隔离）

**Files:**
- Create: `internal/agentcore/sys_operation/local/shell_operation_windows_test.go`
- Test: 本文件自身

- [ ] **Step 1: 写测试**

```go
//go:build windows

package local

import (
	"testing"

	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// TestResolveExecutionPlan_Auto选择 验证 Windows auto 分支的 shell 选择顺序
//
// 仅在 Windows 上运行：auto 分支的判定依赖 runtime.GOOS。
// 本地或发布前手工执行：go test -run TestResolveExecutionPlan_Auto选择 ./internal/agentcore/sys_operation/local/
func TestResolveExecutionPlan_Auto选择(t *testing.T) {
	op := newTestShellOperation(t)

	cases := []struct {
		command string
		want    string
	}{
		{"for i in 1 2; do echo $i; done", "bash"},
		{"$x = 1; echo $x", "powershell"},
		{"dir", "cmd"},
	}
	for _, tc := range cases {
		t.Run(tc.command, func(t *testing.T) {
			_, _, shellName, err := op.resolveExecutionPlan(tc.command, sysop.ShellTypeAuto)
			if err != nil {
				t.Fatal(err)
			}
			if shellName != tc.want {
				t.Fatalf("shell 选中 %q，期望 %q", shellName, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: 确认 Linux 上被跳过**

Run: `go test ./internal/agentcore/sys_operation/local/ -run TestResolveExecutionPlan_Auto选择 -v`
Expected: `no tests to run`（build tag 生效）

- [ ] **Step 3: 确认文件编译无语法错**

Run: `GOOS=windows go vet ./internal/agentcore/sys_operation/local/`
Expected: 无输出

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/sys_operation/local/shell_operation_windows_test.go
git commit -m "test(sys_operation): Windows auto shell 选择用例（build tag 隔离，D-12）"
```

---

### Task D5: 后台 stderr 保留与排空

**Files:**
- Modify: `internal/agentcore/sys_operation/local/utils.go:466-527`（`Background`）
- Test: `internal/agentcore/sys_operation/local/local_operation_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestBackground_EarlyExit含stderr诊断 验证后台进程早退时错误信息带 stderr
func TestBackground_EarlyExit含stderr诊断(t *testing.T) {
	h := NewAsyncProcessHandler(
		exec.Command("sh", "-c", "echo 'boom: missing dep' >&2; exit 3"),
		1024, "utf-8", 5)

	_, err := h.Background(2.0)
	if err == nil {
		t.Fatal("非零退出应返回错误")
	}
	if !strings.Contains(err.Error(), "boom: missing dep") {
		t.Fatalf("错误信息应包含 stderr 内容，实际 %q", err.Error())
	}
}

// TestDrainBackgroundStderr_上限 验证超长 stderr 被截断并标注
func TestDrainBackgroundStderr_上限(t *testing.T) {
	h := NewAsyncProcessHandler(
		exec.Command("sh", "-c", "head -c 20000 /dev/zero | tr '\\0' 'x' >&2; sleep 0.2"),
		1024, "utf-8", 5)
	h.cmd.Stderr, _ = os.Pipe()
	if err := h.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	got := h.drainBackgroundStderr(0.5, 8192)
	if len(got) > 8300 {
		t.Fatalf("保留量应受 8192 限制，实际 %d", len(got))
	}
	if !strings.Contains(got, "[stderr truncated]") {
		t.Fatalf("超限应标注截断，实际尾部 %q", tail(got, 80))
	}
	_ = h.cmd.Wait()
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/sys_operation/local/ -run 'TestBackground_EarlyExit含stderr诊断' -v`
Expected: FAIL，`错误信息应包含 stderr 内容，实际 "进程提前退出，退出码 3"`

- [ ] **Step 3: 改 `Background` 并加排空函数**

`utils.go:474-477` 替换为：

```go
	// 后台模式：stdout 丢弃；stderr 保留管道，早退时能拿到诊断，且持续排空避免管道写满阻塞
	h.cmd.Stdin = nil
	h.cmd.Stdout = nil
	var stderrPipe io.ReadCloser
	if pipe, err := h.cmd.StderrPipe(); err == nil {
		stderrPipe = pipe
	}
```

在 `SetProcessGroup` 之前的 `Start()` 之后启动排空协程，并在早退分支使用其内容：

```go
	// 早退检测期间同步排空并保留前 8KB stderr
	type stderrResult struct{ msg string }
	stderrCh := make(chan stderrResult, 1)
	if stderrPipe != nil {
		go func() { stderrCh <- stderrResult{msg: drainStderr(stderrPipe, 8192)} }()
	}
```

非零退出分支追加：

```go
				msg := fmt.Sprintf("进程提前退出，退出码 %d", exitErr.ExitCode())
				if stderrPipe != nil {
					select {
					case sr := <-stderrCh:
						if sr.msg != "" {
							msg += ": " + sr.msg
						}
					case <-time.After(500 * time.Millisecond):
					}
				}
				return pid, fmt.Errorf("%s", msg)
```

grace 超时分支（进程仍在跑）不需要等 stderr，但要把管道排空协程遗留的输出丢掉，避免 goroutine 泄漏——`drainStderr` 在进程退出时会自然返回。

新增函数：

```go
// drainStderr 排空 stderr 管道并保留前 limit 字节，超限追加截断标记。
// Python: AsyncProcessHandler._drain_background_stderr (utils.py:312-332)
func drainStderr(r io.Reader, limit int) string {
	var retained bytes.Buffer
	truncated := false
	buf := make([]byte, 1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			remaining := limit - retained.Len()
			if remaining > 0 {
				if n <= remaining {
					retained.Write(buf[:n])
				} else {
					retained.Write(buf[:remaining])
					truncated = true
				}
			} else {
				truncated = true
			}
		}
		if err != nil {
			break
		}
	}
	msg := strings.TrimSpace(retained.String())
	if truncated {
		if msg != "" {
			return msg + "\n[stderr truncated]"
		}
		return "[stderr truncated]"
	}
	return msg
}
```

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/agentcore/sys_operation/local/ -run 'TestBackground|TestDrainBackgroundStderr' -v`
Expected: PASS

- [ ] **Step 5: 全包 race 回归**

Run: `go test -race ./internal/agentcore/sys_operation/local/`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/sys_operation/local/utils.go internal/agentcore/sys_operation/local/local_operation_test.go
git commit -m "fix(sys_operation): 后台模式保留并排空 stderr，早退时可诊断（D-12）"
```

---

## 批次 D-E：MCP

### Task E1: `McpServerConfig` 加 `IncludeImageContent`

**Files:**
- Modify: `internal/agentcore/foundation/tool/mcp/types/types.go:47-66`、`:124-141`
- Test: `internal/agentcore/foundation/tool/mcp/types/types_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestMcpServerConfig_IncludeImageContent 验证默认 false 且可通过选项开启
func TestMcpServerConfig_IncludeImageContent(t *testing.T) {
	cfg := NewMcpServerConfig("s", "cmd", "stdio")
	if cfg.IncludeImageContent {
		t.Fatal("默认应为 false")
	}
	cfg = NewMcpServerConfig("s", "cmd", "stdio", WithIncludeImageContent(true))
	if !cfg.IncludeImageContent {
		t.Fatal("WithIncludeImageContent(true) 应生效")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/foundation/tool/mcp/types/ -run TestMcpServerConfig_IncludeImageContent -v`
Expected: 编译失败 `undefined: WithIncludeImageContent`

- [ ] **Step 3: 加字段与选项**

`types.go` 的 `McpServerConfig` 末尾追加：

```go
	// IncludeImageContent 是否把 MCP 返回的图片块桥接为多模态工具结果数据。
	//
	// 默认 false：图片退化为文本占位符（`[image content: image/png, N base64 chars]`）。
	// 仅对模型必须看到图片的服务端开启（如 cua-driver 截图）。
	// Python: McpServerConfig.include_image_content (mcp/base.py:114-121)
	IncludeImageContent bool
```

选项区块追加：

```go
// WithIncludeImageContent 设置是否把图片块桥接为多模态数据。
func WithIncludeImageContent(include bool) McpServerConfigOption {
	return func(c *McpServerConfig) { c.IncludeImageContent = include }
}
```

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/agentcore/foundation/tool/mcp/types/ -run TestMcpServerConfig_IncludeImageContent -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/foundation/tool/mcp/types/types.go internal/agentcore/foundation/tool/mcp/types/types_test.go
git commit -m "feat(mcp): McpServerConfig 新增 include_image_content 开关（D-15）"
```

---

### Task E2: 多块抽取

**Files:**
- Modify: `internal/agentcore/foundation/tool/mcp/base.go:59-115`
- Test: `internal/agentcore/foundation/tool/mcp/base_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestExtractMCPToolResultContent_多文本块拼接 验证不再只取最后一块
func TestExtractMCPToolResultContent_多文本块拼接(t *testing.T) {
	raw := map[string]any{
		"isError": false,
		"content": []any{
			map[string]any{"type": "text", "text": "ok"},
			map[string]any{"type": "text", "text": "detail"},
		},
	}
	got := ExtractMCPToolResultContent(raw)
	if s, _ := got.(string); s != "ok\n\ndetail" {
		t.Fatalf("应为 ok\\n\\ndetail，实际 %#v", got)
	}
}

// TestExtractMCPToolResultContent_图片占位符 验证默认把图片转成文本占位符
func TestExtractMCPToolResultContent_图片占位符(t *testing.T) {
	raw := map[string]any{
		"isError": false,
		"content": []any{
			map[string]any{"type": "text", "text": "clicked"},
			map[string]any{"type": "image", "mimeType": "image/png", "data": "AAAA"},
		},
	}
	got := ExtractMCPToolResultContent(raw)
	s, _ := got.(string)
	if !strings.Contains(s, "clicked") || !strings.Contains(s, "[image content: image/png, 4 base64 chars]") {
		t.Fatalf("应含文本与图片占位符，实际 %q", s)
	}
}

// TestExtractMCPToolResultContent_单块保持类型 验证单块 data 直接返回而非字符串化
func TestExtractMCPToolResultContent_单块保持类型(t *testing.T) {
	raw := map[string]any{
		"isError": false,
		"content": []any{map[string]any{"type": "resource", "data": map[string]any{"k": "v"}}},
	}
	got := ExtractMCPToolResultContent(raw)
	if _, ok := got.(map[string]any); !ok {
		t.Fatalf("单块 data 应原样返回 map，实际 %#v", got)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/foundation/tool/mcp/ -run TestExtractMCPToolResultContent -v`
Expected: FAIL，`应为 ok\n\ndetail，实际 "detail"`

- [ ] **Step 3: 重写抽取函数**

`base.go:59-115` 的 `ExtractMCPToolResultContent` 整体替换（`preserveMCPToolResultStatus` 在 Task E3 加，本步先留空实现等价于直通）：

```go
// ExtractMCPToolResultContent 从 MCP CallToolResult 抽取紧凑结果。
// Python: extract_mcp_tool_result_content (mcp/base.py:56-134)
//
// 保留全部内容块：文本块按顺序拼接，多块结果不再只剩最后一块。
// 图片块默认转文本占位符；服务端开启 IncludeImageContent 时产出多模态数据。
func ExtractMCPToolResultContent(toolResult any, includeImageContent bool, toolName string) any {
	resultMap, ok := toolResult.(map[string]any)
	if !ok {
		return nil
	}
	contentRaw, hasContent := resultMap["content"]
	contentList, _ := contentRaw.([]any)
	if !hasContent || len(contentList) == 0 {
		return nil
	}

	var textParts []string
	type imageBlock struct{ mimeType, data string }
	var images []imageBlock

	for _, itemRaw := range contentList {
		item, ok := itemRaw.(map[string]any)
		if !ok {
			if len(contentList) == 1 {
				return fmt.Sprintf("%v", itemRaw)
			}
			textParts = append(textParts, fmt.Sprintf("%v", itemRaw))
			continue
		}

		if text, ok := item["text"].(string); ok {
			textParts = append(textParts, text)
			continue
		}

		if data, hasData := item["data"]; hasData {
			mimeType, _ := item["mimeType"].(string)
			if mimeType == "" {
				mimeType, _ = item["mime_type"].(string)
			}
			mimeType = strings.ToLower(mimeType)
			if strings.HasPrefix(mimeType, "image/") {
				if includeImageContent {
					images = append(images, imageBlock{mimeType: mimeType, data: fmt.Sprintf("%v", data)})
				} else {
					textParts = append(textParts, fmt.Sprintf("[image content: %s, %d base64 chars]",
						mimeType, len(fmt.Sprintf("%v", data))))
				}
				continue
			}
			if len(contentList) == 1 {
				return data
			}
			textParts = append(textParts, fmt.Sprintf("%v", data))
			continue
		}

		// 其他情况：排除空值与 data 字段后序列化
		cleaned := make(map[string]any, len(item))
		for k, v := range item {
			if k == "data" || v == nil {
				continue
			}
			cleaned[k] = v
		}
		if len(contentList) == 1 {
			return cleaned
		}
		if jsonBytes, err := json.Marshal(cleaned); err == nil {
			textParts = append(textParts, string(jsonBytes))
		} else {
			textParts = append(textParts, fmt.Sprintf("%v", item))
		}
	}

	text := strings.Join(textParts, "\n\n")
	if len(images) == 0 {
		return text
	}

	// 多模态：产出与 Python McpToolResult.data 同构的结构
	note := fmt.Sprintf("%d image(s) attached as multimodal input.", len(images))
	content := note
	if text != "" {
		content = text + "\n\n" + note
	}
	items := make([]any, 0, len(images))
	for _, img := range images {
		items = append(items, map[string]any{
			"type":        "image",
			"source":      "mcp",
			"source_path": toolName,
			"mime_type":   img.mimeType,
			"data_url":    "data:" + img.mimeType + ";base64," + img.data,
		})
	}
	return map[string]any{"content": content, "multimodal": items}
}
```

- [ ] **Step 4: 改结构体与调用点**

`MCPTool` 当前只有 `card` 与 `mcpClient` 两个字段（`base.go:20-23`），不持有配置。加一个字段：

```go
// MCPTool MCP 工具实现。
type MCPTool struct {
	card      *types.McpToolCard
	mcpClient types.McpClient
	// includeImageContent 是否把图片块桥接为多模态数据（来自 McpServerConfig）
	includeImageContent bool
}
```

`NewMCPTool` 签名追加参数（调用方只有装配层，改动面小）：

```go
// NewMCPTool 创建 MCP 工具实例。
// includeImageContent 来自 McpServerConfig.IncludeImageContent。
func NewMCPTool(mcpClient types.McpClient, card *types.McpToolCard, includeImageContent bool) (*MCPTool, error) {
	if mcpClient == nil {
		return nil, exception.BuildError(
			exception.StatusToolMcpClientNotSupported,
			exception.WithParam("card", card.String()),
		)
	}
	return &MCPTool{card: card, mcpClient: mcpClient, includeImageContent: includeImageContent}, nil
}
```

`Invoke` 中改为：

```go
	extracted := ExtractMCPToolResultContent(result, t.includeImageContent, t.card.Name)
```

Run: `grep -rn "NewMCPTool(" --include=*.go internal/ | grep -v _test` 逐个补第三个参数（从 `McpServerConfig.IncludeImageContent` 取）。

- [ ] **Step 5: 跑测试**

Run: `go test ./internal/agentcore/foundation/tool/mcp/ -run TestExtractMCPToolResultContent -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/foundation/tool/mcp/base.go internal/agentcore/foundation/tool/mcp/base_test.go
git commit -m "fix(mcp): 抽取保留全部内容块，图片按开关产出占位符或模态数据（D-15）"
```

---

### Task E3: `preserveMCPToolResultStatus` 状态保留

**Files:**
- Modify: `internal/agentcore/foundation/tool/mcp/base.go`
- Test: `internal/agentcore/foundation/tool/mcp/base_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestPreserveMCPToolResultStatus_成功值不改 验证 isError=false 时内容一字节不变
func TestPreserveMCPToolResultStatus_成功值不改(t *testing.T) {
	content, err := preserveMCPToolResultStatus(false, "ok")
	if err != nil {
		t.Fatalf("成功路径不应返回 error，实际 %v", err)
	}
	if content != "ok" {
		t.Fatalf("成功值应原样返回，实际 %#v", content)
	}
}

// TestPreserveMCPToolResultStatus_失败转错误 验证 isError=true 时产出错误并保留文本
func TestPreserveMCPToolResultStatus_失败转错误(t *testing.T) {
	_, err := preserveMCPToolResultStatus(true, "Permission denied")
	if err == nil {
		t.Fatal("isError=true 应返回 error")
	}
	if !strings.Contains(err.Error(), "Permission denied") {
		t.Fatalf("错误应保留原文，实际 %q", err.Error())
	}
}

// TestPreserveMCPToolResultStatus_空内容 验证无文本时给出兜底文案
func TestPreserveMCPToolResultStatus_空内容(t *testing.T) {
	_, err := preserveMCPToolResultStatus(true, "")
	if err == nil || !strings.Contains(err.Error(), "MCP tool reported an error.") {
		t.Fatalf("空内容应给兜底文案，实际 %v", err)
	}
}

// TestExtractMCPToolResultContent_六个返回点都过状态 验证各路径的 isError 都被保留
func TestExtractMCPToolResultContent_六个返回点都过状态(t *testing.T) {
	cases := map[string]any{
		"无content":  map[string]any{"isError": true},
		"单块data":   map[string]any{"isError": true, "content": []any{map[string]any{"data": "raw"}}},
		"单块other":  map[string]any{"isError": true, "content": []any{map[string]any{"type": "x", "foo": "bar"}}},
		"多块文本":     map[string]any{"isError": true, "content": []any{map[string]any{"text": "a"}, map[string]any{"text": "b"}}},
		"单块文本":     map[string]any{"isError": true, "content": []any{map[string]any{"text": "only"}}},
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := extractMCPToolResult(raw, false, "t")
			if err == nil {
				t.Fatalf("isError=true 应返回 error，实际 got=%#v", got)
			}
		})
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/foundation/tool/mcp/ -run 'TestPreserve|TestExtractMCPToolResultContent_六个返回点' -v`
Expected: 编译失败 `undefined: preserveMCPToolResultStatus`

- [ ] **Step 3: 加核心函数并改造抽取入口**

`base.go` 新增：

```go
// preserveMCPToolResultStatus 按 MCP 的 isError 给抽取结果打成功/失败标。
// Python: preserve_mcp_tool_result_status (mcp/base.py:36-53)
//
// isError 不为真时原样返回 content（成功值一个字节都不改）；
// isError 为真时把抽取到的文本作为错误信息返回，让上层按失败处理。
func preserveMCPToolResultStatus(isError bool, content any) (any, error) {
	if !isError {
		return content, nil
	}
	msg := ""
	if s, ok := content.(string); ok {
		msg = strings.TrimSpace(s)
	}
	if msg == "" {
		msg = "MCP tool reported an error."
	}
	return content, exception.BuildError(
		exception.StatusToolMcpExecutionError,
		exception.WithParam("method", "invoke"),
		exception.WithParam("reason", msg),
	)
}
```

把 `ExtractMCPToolResultContent` 拆成「纯抽取」+「状态包裹」两层，让 6 个返回点都能穿过状态判定：

```go
// extractMCPToolResult 纯抽取：只负责算出 content，不判断 isError。
func extractMCPToolResult(toolResult any, includeImageContent bool, toolName string) (any, error) {
	isError, _ := toolResult.(map[string]any)["isError"].(bool)
	content := extractMCPToolResultContentRaw(toolResult, includeImageContent, toolName)
	return preserveMCPToolResultStatus(isError, content)
}
```

将 Task E2 的实现体改名为 `extractMCPToolResultContentRaw`，`ExtractMCPToolResultContent` 保留为兼容入口：

```go
// ExtractMCPToolResultContent 抽取 MCP 结果（不含 isError 状态）。
//
// Deprecated: 需要按 isError 判定成功/失败时用 extractMCPToolResult。
func ExtractMCPToolResultContent(toolResult any, includeImageContent bool, toolName string) any {
	content, _ := extractMCPToolResult(toolResult, includeImageContent, toolName)
	return content
}
```

`Invoke` 中改为：

```go
	extracted, extractErr := extractMCPToolResult(result, includeImageContent, t.card.Name)
	if extractErr != nil {
		return map[string]any{"result": extracted, "__mcp_result__": &MCPToolResult{
			Success: false,
			Data:    map[string]any{"result": extracted},
			Err:     extractErr.Error(),
		}}, extractErr
	}
	if mm, isMultimodal := extracted.(map[string]any); isMultimodal {
		if _, hasMM := mm["multimodal"]; hasMM {
			return map[string]any{"__mcp_result__": &MCPToolResult{
				Success: true,
				Data:    mm,
			}}, nil
		}
	}
	return map[string]any{"result": extracted}, nil
```

并新增结构体（`base.go` 结构体区块）：

```go
// MCPToolResult 携带多模态数据或失败状态的 MCP 工具结果。
//
// 与 harness 的 ToolOutput 形状（success/data/error）保持鸭子类型兼容，
// 使 react agent 的多模态管线无需 core 反向导入 harness。
// Python: McpToolResult (mcp/base.py:145-157)
type MCPToolResult struct {
	// Success 是否成功
	Success bool
	// Data 结果数据，多模态时含 content 与 multimodal
	Data map[string]any
	// Err 失败原因
	Err string
}
```

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/agentcore/foundation/tool/mcp/ -run 'TestPreserve|TestExtractMCPToolResultContent' -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/foundation/tool/mcp/base.go internal/agentcore/foundation/tool/mcp/base_test.go
git commit -m "fix(mcp): 保留 isError 状态，失败不再被当成功（D-16）"
```

---

### Task E4: ability 层识别 `MCPToolResult`

**Files:**
- Modify: `internal/agentcore/single_agent/ability/ability_manager.go`（`runToolCallSafely` 结果处理段）
- Test: `internal/agentcore/single_agent/ability/ability_manager_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestExecute_MCP多模态结果装入Result 验证 __mcp_result__ 被拆出结构体放进 Result
func TestExecute_MCP多模态结果装入Result(t *testing.T) {
	am := NewAbilityManager()
	tl, _ := tool.NewTool(
		func(ctx context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
			return map[string]any{"__mcp_result__": &mcptool.MCPToolResult{
				Success: true,
				Data: map[string]any{
					"content": "shot",
					"multimodal": []any{map[string]any{
						"type": "image", "source": "mcp", "source_path": "t",
						"mime_type": "image/png", "data_url": "data:image/png;base64,AA",
					}},
				},
			}}, nil
		},
		tool.WithToolName("mcp_t"),
	)
	am.Add(tl)

	results := am.Execute(context.Background(), nil,
		[]*llmschema.ToolCall{{ID: "m1", Name: "mcp_t", Arguments: "{}"}}, nil, "", true)
	if len(results) != 1 {
		t.Fatalf("期望 1 条结果，实际 %d", len(results))
	}
	if _, ok := results[0].Result.(*mcptool.MCPToolResult); !ok {
		t.Fatalf("Result 应为 *MCPToolResult，实际 %T", results[0].Result)
	}
	if results[0].ToolMsg == nil || !strings.Contains(results[0].ToolMsg.Content, "shot") {
		t.Fatalf("ToolMsg 应渲染 data.content，实际 %v", results[0].ToolMsg)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/single_agent/ability/ -run TestExecute_MCP多模态结果装入Result -v`
Expected: FAIL，`Result 应为 *MCPToolResult，实际 map[string]interface{}`

- [ ] **Step 3: 在结果处理段加拆包与渲染**

在 `ability_manager.go` 的结果处理循环里，`res.Result` 赋值处的紧邻位置加：

```go
			// MCP 多模态/失败结果用 __mcp_result__ 承载结构体，
			// 拆出后 Result 装结构体本体（对齐 Python 原样透传 McpToolResult），
			// ToolMsg 文本走 runk 的 render_for_llm 等价逻辑。
			if m, ok := res.Result.(map[string]any); ok {
				if wrapped, has := m["__mcp_result__"].(*mcptool.MCPToolResult); has {
					res.Result = wrapped
					if res.ToolMsg == nil {
						res.ToolMsg = llmschema.NewToolMessage(toolCallIDOf(toolCall), renderMCPResultForLLM(wrapped))
					}
				}
			}
```

新增渲染函数（放 `internal/agentcore/single_agent/ability/mcp_result.go`，**新建文件**）：

```go
package ability

import (
	"encoding/json"

	mcptool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp"
)

// ──────────────────────────── 常量 ────────────────────────────

// emptySuccessText 工具成功但无文本可读时的兜底文案。
// Python: EMPTY_SUCCESS_TEXT (foundation/tool/schema.py)
const emptySuccessText = "Tool completed."

// ──────────────────────────── 非导出函数 ────────────────────────────

// renderMCPResultForLLM 把 MCP 结果渲染成模型可见文本。
// Python: MCPTool.render_for_llm (mcp/base.py:227-234)
//
// invoke 把普通结果包成 {"result": value}，模型读的是 value 本身而非包装；
// 多模态结果读 data.content。
func renderMCPResultForLLM(r *mcptool.MCPToolResult) string {
	if r == nil {
		return ""
	}
	if r.Data != nil {
		if content, ok := r.Data["content"].(string); ok && content != "" {
			return content
		}
		if inner, ok := r.Data["result"]; ok {
			if text := renderPayloadText(inner); text != "" {
				return text
			}
		}
	}
	if r.Err != "" {
		return r.Err
	}
	return emptySuccessText
}

// renderPayloadText 把任意 payload 渲染成文本。
// Python: render_payload_text
//
// 字符串原样返回；其余类型 JSON 序列化（结构化结果给模型读 JSON 而非 Go 字面量）。
func renderPayloadText(payload any) string {
	switch v := payload.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(encoded)
	}
}
```

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/agentcore/single_agent/ability/ -run 'TestExecute_MCP多模态结果装入Result|TestRenderMCPResultForLLM' -v`
Expected: PASS

（补一条渲染用例到 `mcp_result_test.go`）

```go
// TestRenderMCPResultForLLM 验证三种渲染分支
func TestRenderMCPResultForLLM(t *testing.T) {
	// data.content 优先
	if got := renderMCPResultForLLM(&mcptool.MCPToolResult{
		Success: true,
		Data:    map[string]any{"content": "hello"},
	}); got != "hello" {
		t.Fatalf("应取 content，实际 %q", got)
	}

	// 无 content 时渲染 data.result
	if got := renderMCPResultForLLM(&mcptool.MCPToolResult{
		Success: true,
		Data:    map[string]any{"result": map[string]any{"k": "v"}},
	}); got != `{"k":"v"}` {
		t.Fatalf("应 JSON 渲染 result，实际 %q", got)
	}

	// 失败且无数据时用 Err
	if got := renderMCPResultForLLM(&mcptool.MCPToolResult{Err: "boom"}); got != "boom" {
		t.Fatalf("应返回 Err，实际 %q", got)
	}
}
```

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/single_agent/ability/
git commit -m "feat(ability): 拆分 __mcp_result__ 装入 Result 并渲染模型可见文本（D-15/D-16）"
```

---

### Task E5: 消费端识别结构体与文案

**Files:**
- Modify: `internal/agentcore/single_agent/agents/react_invoke.go:1108-1195`
- Test: `internal/agentcore/single_agent/agents/react_invoke_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestIterMultimodalImageItems_识别MCP结构体 验证结构体形态的结果也能取到图片
func TestIterMultimodalImageItems_识别MCP结构体(t *testing.T) {
	res := &mcptool.MCPToolResult{
		Success: true,
		Data: map[string]any{
			"multimodal": []any{map[string]any{
				"type": "image", "source": "mcp", "source_path": "shot.png",
				"mime_type": "image/png", "data_url": "data:image/png;base64,AA",
			}},
		},
	}
	items := iterMultimodalImageItems(res)
	if len(items) != 1 {
		t.Fatalf("应取到 1 张图，实际 %d", len(items))
	}
	if items[0]["source"] != "mcp" {
		t.Fatalf("source 应为 mcp，实际 %v", items[0]["source"])
	}
}

// TestBuildMultimodal_来源文案 验证文案使用 source 而非硬编码 read_file
func TestBuildMultimodal_来源文案(t *testing.T) {
	a := &ReActAgent{}
	results := []agentschema.ExecuteResult{{Result: map[string]any{
		"data": map[string]any{"multimodal": []any{map[string]any{
			"type": "image", "source": "mcp", "source_path": "shot.png",
			"data_url": "data:image/png;base64,AA",
		}}},
	}}}
	msg := a.buildMultimodalToolResultsMessage(results)
	if msg == nil {
		t.Fatal("应构建出多模态消息")
	}
	if !strings.Contains(fmt.Sprint(msg), "Image loaded from mcp: shot.png") {
		t.Fatalf("文案应含 Image loaded from mcp: shot.png，实际 %v", msg)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/single_agent/agents/ -run 'TestIterMultimodalImageItems_识别MCP结构体|TestBuildMultimodal_来源文案' -v`
Expected: FAIL

- [ ] **Step 3: 改消费端**

`iterMultimodalImageItems` 开头加结构体分支：

```go
func iterMultimodalImageItems(toolResult any) []map[string]any {
	var data map[string]any
	switch v := toolResult.(type) {
	case *mcptool.MCPToolResult:
		data = v.Data
	case map[string]any:
		data, _ = v["data"].(map[string]any)
	}
	if data == nil {
		return nil
	}
	multimodalItems, _ := data["multimodal"].([]any)
	...
```

`buildMultimodalToolResultsMessage` 的文案改为：

```go
		for _, item := range iterMultimodalImageItems(r.Result) {
			source, _ := item["source"].(string)
			if source == "" {
				source = "tool"
			}
			sourcePath, _ := item["source_path"].(string)
			if sourcePath == "" {
				sourcePath = "unknown image"
			}
			dataURL, _ := item["data_url"].(string)
			loadedPaths = append(loadedPaths, sourcePath)
			parts = append(parts,
				llmschema.ContentPart{
					Type: "text",
					Text: fmt.Sprintf("Image loaded from %s: %s", source, sourcePath),
				},
				...)
```

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/agentcore/single_agent/agents/ -run 'TestIterMultimodalImageItems|TestBuildMultimodal' -v`
Expected: PASS

- [ ] **Step 5: 全量回归**

Run: `go build ./... && go vet ./... && go test ./internal/agentcore/...`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/single_agent/agents/react_invoke.go internal/agentcore/single_agent/agents/react_invoke_test.go
git commit -m "fix(agents): 多模态消费端识别 MCP 结构体并按 source 渲染来源（D-15）"
```

---

### Task E6: MCP 重连模块

**Files:**
- Create: `internal/agentcore/foundation/tool/mcp/client/reconnect.go`
- Test: `internal/agentcore/foundation/tool/mcp/client/reconnect_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestIsReconnectableError 验证只把"请求未完成"的连接类错误判为可重连
func TestIsReconnectableError(t *testing.T) {
	reconnectable := []string{
		"connection reset by peer",
		"broken pipe",
		"unexpected EOF",
		"transport closed",
		"session terminated",
		"connection refused",
	}
	for _, msg := range reconnectable {
		if !isReconnectableError(errors.New(msg)) {
			t.Fatalf("%q 应判为可重连", msg)
		}
	}

	notReconnectable := []error{
		errors.New("invalid params"),
		errors.New("permission denied"),
		context.Canceled,
		context.DeadlineExceeded,
	}
	for _, err := range notReconnectable {
		if isReconnectableError(err) {
			t.Fatalf("%v 不应判为可重连", err)
		}
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/foundation/tool/mcp/client/ -run TestIsReconnectableError -v`
Expected: 编译失败

- [ ] **Step 3: 写实现**

新建 `reconnect.go`：

```go
package client

import (
	"context"
	"errors"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// reconnectableMarkers 表示请求未完成的连接类错误特征（小写比较）。
// Python: client/reconnect.py 的错误分类
var reconnectableMarkers = []string{
	"connection reset",
	"broken pipe",
	"unexpected eof",
	"transport closed",
	"session terminated",
	"connection refused",
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isReconnectableError 判断错误是否属于连接层中断（请求未送达或连接被重置）。
//
// 只有这类错误才允许透明重试：业务错误（isError）、超时、取消、参数错误
// 重试都会造成重复副作用或掩盖真实失败。
func isReconnectableError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, marker := range reconnectableMarkers {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// reconnectableClient 可重连客户端的协作面。
type reconnectableClient interface {
	reconnect(ctx context.Context) error
}

// callWithReconnect 执行 fn；命中连接类错误时重建连接并重试一次。
//
// 只重试一次，避免无限循环；重连失败时返回原始错误，不掩盖首个失败。
// Python: client/reconnect.py retry-after-reconnect 语义
func callWithReconnect(
	ctx context.Context,
	c reconnectableClient,
	serverName string,
	fn func() (any, error),
) (any, error) {
	result, err := fn()
	if err == nil || !isReconnectableError(err) {
		return result, err
	}

	logger.Warn(logComponent).
		Str("server_name", serverName).
		Err(err).
		Msg("MCP connection lost, reconnecting and retrying once")

	if rerr := c.reconnect(ctx); rerr != nil {
		logger.Error(logComponent).
			Str("server_name", serverName).
			Err(rerr).
			Msg("MCP reconnect failed, returning original error")
		return nil, err
	}

	return fn()
}
```

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/agentcore/foundation/tool/mcp/client/ -run TestIsReconnectableError -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/foundation/tool/mcp/client/reconnect.go internal/agentcore/foundation/tool/mcp/client/reconnect_test.go
git commit -m "feat(mcp): 新增连接类错误判定与单次重连重试（D-14）"
```

---

### Task E7: SSE 客户端接入重连

**Files:**
- Modify: `internal/agentcore/foundation/tool/mcp/client/sse_client.go:325-360`
- Test: `internal/agentcore/foundation/tool/mcp/client/sse_client_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestSseClient_CallTool断线重连 验证连接重置后重建并重试成功
func TestSseClient_CallTool断线重连(t *testing.T) {
	var calls int
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			// 第一次直接断开 TCP
			if hj, ok := w.(http.Hijacker); ok {
				conn, _, _ := hj.Hijack()
				_ = conn.Close()
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"ok"}]}}`))
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c := NewSseClient(types.NewMcpServerConfig("s", srv.URL, "sse"))
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Disconnect(context.Background())

	got, err := c.CallTool(context.Background(), "t", map[string]any{})
	if err != nil {
		t.Fatalf("重连后应成功，实际 %v", err)
	}
	if got == nil {
		t.Fatal("应返回结果")
	}
	if calls < 2 {
		t.Fatalf("应至少发两次请求（含重试），实际 %d", calls)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/foundation/tool/mcp/client/ -run TestSseClient_CallTool断线重连 -v`
Expected: FAIL，`重连后应成功，实际 ...`

- [ ] **Step 3: 加 reconnect 方法并包 CallTool**

`sse_client.go` 追加：

```go
// reconnect 重建底层 client 与连接。
func (c *SseClient) reconnect(ctx context.Context) error {
	if c.mcpClient != nil {
		_ = c.mcpClient.Close()
		c.mcpClient = nil
	}
	c.isConnected = false
	return c.Connect(ctx)
}
```

`CallTool` 调用块替换为：

```go
	result, err := callWithReconnect(ctx, c, c.serverName, func() (any, error) {
		resp, callErr := c.mcpClient.CallTool(ctx, mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      toolName,
				Arguments: arguments,
			},
		})
		if callErr != nil {
			return nil, callErr
		}
		return callToolResultToMap(resp), nil
	})
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("server_name", c.serverName).
			Str("tool_name", toolName).
			Msg("SSE client failed to call tool")
		return nil, exception.BuildError(
			exception.StatusToolMcpExecutionError,
			exception.WithParam("method", "CallTool"),
			exception.WithParam("reason", err.Error()),
			exception.WithParam("card", toolName),
		)
	}
	return result, nil
```

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/agentcore/foundation/tool/mcp/client/ -run TestSseClient_CallTool断线重连 -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/foundation/tool/mcp/client/sse_client.go internal/agentcore/foundation/tool/mcp/client/sse_client_test.go
git commit -m "fix(mcp): SSE 客户端连接重置后重连并重试（D-14）"
```

---

### Task E8: streamable-http 客户端接入重连

**Files:**
- Modify: `internal/agentcore/foundation/tool/mcp/client/streamable_http_client.go:301-343`
- Test: `internal/agentcore/foundation/tool/mcp/client/streamable_http_client_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestStreamableHTTP_CallTool断线重连 验证服务重启后重连并重试成功
func TestStreamableHTTP_CallTool断线重连(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			if hj, ok := w.(http.Hijacker); ok {
				conn, _, _ := hj.Hijack()
				_ = conn.Close()
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"ok"}]}}`))
	}))
	defer srv.Close()

	c := NewStreamableHttpClient(types.NewMcpServerConfig("s", srv.URL, "streamable-http"))
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Disconnect(context.Background())

	if _, err := c.CallTool(context.Background(), "t", map[string]any{}); err != nil {
		t.Fatalf("重连后应成功，实际 %v", err)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/foundation/tool/mcp/client/ -run TestStreamableHTTP_CallTool断线重连 -v`
Expected: FAIL

- [ ] **Step 3: 加 reconnect 并包 CallTool**

同 Task E7 的结构：新增 `reconnect(ctx)`（关闭 `c.client`、置 `isConnected=false`、调 `Connect`），`CallTool` 的调用块用 `callWithReconnect` 包裹，错误分支保留原有日志与返回。

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/agentcore/foundation/tool/mcp/client/ -run TestStreamableHTTP_CallTool断线重连 -v`
Expected: PASS

- [ ] **Step 5: 全包 race 回归**

Run: `go test -race ./internal/agentcore/foundation/tool/mcp/...`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/foundation/tool/mcp/client/streamable_http_client.go internal/agentcore/foundation/tool/mcp/client/streamable_http_client_test.go
git commit -m "fix(mcp): streamable-http 客户端服务重启后重连重试（D-14）"
```

---

## 收口任务

### Task Z1: 更新缺陷计划文档状态

**Files:**
- Modify: `docs/sync/defect-plan-2026-11-04.md`（§0 摘要、§2 总览、§3.D 明细、§4 批次一）

- [ ] **Step 1: 更新 §3.D 状态列**

把 D-01~D-05、D-08~D-10、D-12、D-14~D-16 的状态从 ✅/🔍 改为 `✅ 已修（<提交号>）`，并在 §3.D 表下追加一节「D 组处置决定（2026-11-07）」，写明：

- 12 项修复的提交号
- 5 项 ➖ 的判据（D-06/D-07/D-11 已对齐；D-13/D-17 不同构）
- 设计文档与计划文档路径

- [ ] **Step 2: 重算计数**

Run: `grep -cE '^\| D-[0-9]+ \|' docs/sync/defect-plan-2026-11-04.md`
把 §0 摘要里的「已核验 53 行」与 §2 总览表按实际重算（用脚本统计，不心算）。

- [ ] **Step 3: 更新 §4 批次一**

P0 待办从 29 行减去 D-01/04/05/06/08/10 中已修的行，重算待办数。

- [ ] **Step 4: 提交**

```bash
git add docs/sync/defect-plan-2026-11-04.md
git commit -m "docs(defect-plan): D 组 12 项修复完成回填状态与计数"
```

---

### Task Z2: 集成测试

**Files:**
- Create: `tests/integration/tools/d_group_alignment_test.go`（含 `//go:build integration`）

- [ ] **Step 1: 写集成用例**

跨 ≥3 个 internal 二级子系统（`single_agent/ability` + `harness/tools/shell` + `sys_operation/local`）：

```go
//go:build integration

package tools

import (
	"context"
	"testing"
	"time"

	abilitypkg "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/ability"
	shelltools "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/shell"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/local"
)

// DGroupSuite D 组对齐集成套件。
//
// 跨 harness/tools/shell + single_agent/ability + sys_operation/local 三个
// internal 二级子系统，覆盖 shell 预算从工具层到执行层的真实传导。
type DGroupSuite struct {
	suite.AgentSuite
}

// runBash 通过 AbilityManager 执行一次 bash 工具调用，返回 ToolMsg 文本。
func (s *DGroupSuite) runBash(command string, timeout int) string {
	s.T().Helper()

	op := local.NewLocalOperation(
		local.WithWorkDir(s.T().TempDir()),
	)
	bashTool := shelltools.NewBashTool(op, "cn", "", shelltools.PermissionConfig{})

	am := abilitypkg.NewAbilityManager()
	am.Add(bashTool)

	args, _ := json.Marshal(map[string]any{"command": command, "timeout": timeout})
	results := am.Execute(context.Background(), nil,
		[]*llmschema.ToolCall{{ID: "c1", Name: "bash", Arguments: string(args)}},
		nil, "", true)
	s.Require().Len(results, 1, "应产出一条执行结果")
	s.Require().NotNil(results[0].ToolMsg, "应产出 ToolMessage")
	return results[0].ToolMsg.Content
}

// TestDGroup_ShellTimeoutEndToEnd 验证工具层预算真实传导到子进程执行层
//
// 覆盖 D-02：bash 解析的 timeout 经 ability 外层预算到 sys_operation 进程超时，
// 三层不再互相截断（历史缺陷：tool 层放行 3000s，sh 层砍成 600s）。
func (s *DGroupSuite) TestDGroup_ShellTimeoutEndToEnd() {
	start := time.Now()
	content := s.runBash("sleep 8; echo done", 30)
	elapsed := time.Since(start)

	s.Less(elapsed.Seconds(), 20.0, "命令应按 timeout 正常跑完，不应被额外钳制提前中断")
	s.Contains(content, "done", "命令应真正执行完毕")
}

// TestDGroup_MaxOutputCharsDefault 验证默认 20000 上限真实生效
//
// 覆盖 D-01：默认值从 0 改为 20000，大输出被截断并落盘。
func (s *DGroupSuite) TestDGroup_MaxOutputCharsDefault() {
	content := s.runBash("seq 1 200000", 30)
	s.Less(len(content), 100000, "默认上限应截断输出")
	s.Contains(content, "openjiuwen", "截断结果应给出落盘文件路径")
}

// TestDGroup_ToolCallTimeoutRecoverable 验证工具超时后模型可自我纠正
//
// 覆盖 D-04：超时产出 tool_message 而非整轮失败。
func (s *DGroupSuite) TestDGroup_ToolCallTimeoutRecoverable() {
	am := abilitypkg.NewAbilityManager()
	slow, err := tool.NewTool(
		func(ctx context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
			time.Sleep(3 * time.Second)
			return map[string]any{"ok": true}, nil
		},
		tool.WithToolName("slow_tool"),
		tool.WithCard(tool.NewToolCard("slow_tool", "", nil, map[string]any{
			"resilience": map[string]any{"timeout_s": 0.2},
		})),
	)
	s.Require().NoError(err)
	am.Add(slow)

	results := am.Execute(context.Background(), nil,
		[]*llmschema.ToolCall{{ID: "t1", Name: "slow_tool", Arguments: "{}"}}, nil, "", true)
	s.Require().Len(results, 1)
	s.Require().NotNil(results[0].ToolMsg)
	s.Contains(results[0].ToolMsg.Content, "timed out")
	s.Equal("t1", results[0].ToolMsg.ToolCallID)
}

// TestDGroupSuite 运行 D 组集成套件。
func TestDGroupSuite(t *testing.T) {
	suite.Run(t, new(DGroupSuite))
}
```

> 实现要点：
> - `AgentSuite`（`tests/integration/suite/agent_suite.go`）提供 `GetResourceMgr()` 与 `TearDownTest` 清理，但**没有** `RunBash`，所以自建 `runBash` 辅助方法。
> - `NewBashTool(op, language, agentID, permConfig)` 与 `local.NewLocalOperation(...)` 的实参形态以本任务 Step 1 编译报错为准微调——这两个签名在 A 批次已确认存在。
> - 该目录 `tests/integration/tools/` 尚不存在，Step 1 一并创建目录与 `doc.go`（`Package tools 提供 D 组缺陷对齐的跨子系统集成测试。`）。

- [ ] **Step 2: 本地跑集成套件**

Run: `go test -tags=integration ./tests/integration/tools/ -race -v`
Expected: PASS

- [ ] **Step 3: 跑审计门禁**

Run: `go run ./cmd/integration-audit -verify`
Expected: 该目录不落入灰区（跨 ≥3 子系统）

- [ ] **Step 4: 提交并推送**

```bash
git add tests/integration/tools/d_group_alignment_test.go
git commit -m "test(integration): D 组 shell 上限与工具超时端到端用例"
git push
```

---

## 验收清单

每批次完成后：

- [ ] `go build ./...`
- [ ] `go vet ./...`
- [ ] 受影响包 `go test -race`
- [ ] 受影响集成套件 `go test -tags=integration ./tests/integration/<包>/ -race`
- [ ] 改动行覆盖率 ≥ 85%

全部完成后：

- [ ] D 组 12 项状态回填 `docs/sync/defect-plan-2026-11-04.md`
- [ ] 计数用脚本重算，不心算
- [ ] `docs/superpowers/plans/` 本计划的 checkbox 全部勾选
