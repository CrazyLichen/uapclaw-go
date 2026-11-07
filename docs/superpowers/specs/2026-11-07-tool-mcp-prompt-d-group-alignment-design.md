# 工具系统 / MCP / 提示词模板 D 组对齐设计（2026-11-07）

> 依据：`docs/sync/defect-plan-2026-11-04.md` §3.D（18 行）。
> 上游基线：agent-core `develop` HEAD `af5fe06d`、jiuwenswarm `develop` HEAD `238dc3a8d`（克隆于 `/tmp/ac-git`、`/tmp/ws-git`，`.git` 完整，逐条 `git show` 原始 diff 比对）。
> 范围：D-01~D-18 共 18 行；核验结论为 **12 存在 + 5 已对齐/不同构 + 1 已处置**。
> 前置：A/B/C 三组已完结，本组只处理工具、MCP、提示词模板。

## 0. 核验结论与处置一览

| 编号 | 上游证据 | 核验结论 | 处置 | 批次 |
| --- | --- | --- | --- | --- |
| D-01 | `b4a5a967` | 存在：`resolveBashMaxOutputChars(0)` 返回 0，默认常量也是 0 → 单条命令输出无上限 | 修（A） | D-A |
| D-02 | `55b9c571`、`c2d1544` | 存在：两层钳制，sh 层 `JW_EXECUTE_CMD_MAX_TIMEOUT`(600) 吞掉 tool 层的 3600 | 修（A） | D-A |
| D-03 | `6eba3e29` | 存在：`Execute` 恒 `WaitGroup` 并行，无 `parallel_tool_calls` 配置面 | 修（A） | D-B |
| D-04 | `fe99da2` | 存在：无外层调用超时、无 `resilience.timeout_s` 解析、无硬上限 | 修（A，不做重试） | D-B |
| D-05 | `e868918b` | 存在：slug 未扁平化（删父目录连带子目录）；`gitdir:` 内容无越界校验 | 修（A，维持 P0） | D-C |
| D-06 | `710b2b09` | 已对齐：`skill_layout.go:36` `isSafeRelativeFilePath` 等价 | ➖ | — |
| D-07 | `c993823`、`8c5b537b` | 已对齐：`filepath.Rel` 校验（文档原判） | ➖ | — |
| D-08 | `92ab155f` | 存在：`output` 任意路径 + `name` 无 `safePathName`；骨架缺 `skill_type`/`force` | 修（A + 补齐语义） | D-C |
| D-09 | `910af579` | 存在：name 含 `/` 的记录永远卸载不掉（目录与记录双残留） | 修（A，只修卸载侧） | D-C |
| D-10 | `810bb071` | 存在：notebook 超预算直接报错；PDF 逐页累积无预算 | 修（A） | D-D |
| D-11 | `ce2aedda` | 已对齐：`utils.go:171-192` `chunkDecoder` 增量解码 + `flush` | ➖ | — |
| D-12 | `af85e33d` | 存在：裸 `$var` 误判 PS、无 bash 控制流识别、后台 stderr 丢弃 | 修（A，Windows tag 隔离测） | D-D |
| D-13 | `92a04257` | 不同构：Go 统一 `float64` 承载 + 按 schema 判整数，无 pydantic 类型强转层 | ➖ | — |
| D-14 | `1bd6360c`、`d90a442` | 存在：连接重置后 `isConnected` 不置假、不重建，client 永久失效 | 修（A） | D-E |
| D-15 | `5a5d7c61` | 存在：`mcp/base.go:73` 只取 `content[-1]` | 修（A′，含多模态消费端） | D-E |
| D-16 | `5d2eef7f` | 存在：完全不读 `isError`，失败被当成功且 `error` 为 nil | 修（A，对等函数） | D-E |
| D-17 | `0988e98c` | 不同构：根因是 anyio CancelScope 跨 task 退出；Go `Disconnect` 直接 `Close()` | ➖ | — |
| D-18 | `27a2e99b` 等 7 条 | 已处置：9.11a progressive deferred tool catalog | ✅ | — |

统计：**修 12 行**（D-01~D-05、D-08~D-10、D-12、D-14~D-16）、**➖ 5 行**（D-06、D-07、D-11 已对齐；D-13、D-17 不同构）、**✅ 1 行**（D-18 已处置），合计 18 行，与明细行数一致（脚本核对：`grep -cE '^\| D-[0-9]+ \|'` = 18）。

## 1. 批次 D-A：Shell 输出上限与超时预算（D-01 + D-02）

### 1.1 D-01 缺陷

`internal/agentcore/harness/tools/shell/bash.go:334`：

```go
func resolveBashMaxOutputChars(rawValue int) int {
	value := rawValue
	if value == 0 {
		return 0            // ← 未传（零值）也被当成 opt-out
	}
	...
}
```

`bashDefaultMaxOutputChars = 0`（`bash.go:54`）。结构体字段是 `int`，无法区分「未传」与「显式传 0」，于是默认路径恒为无限制。

后果（上游 `b4a5a967` 的原始描述）：LLM 从不传 `max_output_chars`，一条返回 600K token 的命令输出原样进上下文，把上下文窗口撑到 1.1M token，压缩引擎陷入无法收敛的循环。

### 1.2 D-02 缺陷

两层钳制：

| 层 | 位置 | 上限 |
| --- | --- | --- |
| tool 层 | `bash.go:326` | `BASH_TOOL_MAX_TIMEOUT_SECONDS` 默认 3600 |
| sh 层 | `shell_operation.go:147-158`、`:312-323` | `JW_EXECUTE_CMD_MAX_TIMEOUT` 默认 600 |

`timeout=3000` 经 tool 层放行，到 sh 层被砍成 600s。模型按提示词（`prompts/tools/bash.go:106` 声明「上限 3600」）以为有 50 分钟，实际 10 分钟就断。

上游 `55b9c571` 的裁决是「**caller owns the execution budget**」，删掉 sh 层这段 `min(timeout, env)`。

### 1.3 设计

**(a) `MaxOutputChars` 改指针**

四个输入结构体同步改（`BashInput`、`BashStreamInput`、`PowerShellInput`、`PowerShellStreamInput`）：

```go
// MaxOutputChars 最大输出字符数；nil 表示未传（取默认 20000），显式 0 表示不限制
MaxOutputChars *int `json:"max_output_chars"`
```

解析函数签名改为 `func resolveBashMaxOutputChars(rawValue *int) int`：

| 输入 | 输出 |
| --- | --- |
| `nil` | 默认值 20000，再按 `BASH_TOOL_MAX_OUTPUT_CHARS` 钳制 |
| `&0` | `0`（opt-out，不进入截断分支） |
| `&v`（v>0） | `min(max(v, 200), maxChars)` |

**与上游的一处有意偏差**：上游用 `min(200, value)`，模型传 `-100` 时得到 200（悄悄放大成 200 字符）。Go 侧在 schema 上加 `"minimum": 0`，负数在 `FormatWithSchema` 的 `validateParamConstraints` 阶段直接拒绝，不进解析函数。偏差性质：仅拒绝路径不同，合法输入行为完全一致。回收条件：无。

**(b) schema 与提示词**

`prompts/tools/bash.go:129`、`prompts/tools/powershell.go:236` 的 `max_output_chars` 属性加 `"minimum": 0`。文案逐字照搬上游（`bash.py:263-270`、`powershell.py:228-235`）：

```
cn: 最大输出字符数，默认 20000：超出部分会被截断并写入临时文件（结果中会给出文件路径，可按需读取），防止超大输出撑爆上下文；显式传 0 表示不限制（谨慎使用）
en: Max output characters; defaults to 20000: output beyond this is truncated and the full content is written to a temp file (its path is returned in the result for on-demand reading), preventing oversized output from flooding context. Pass 0 explicitly to disable the limit (use with care)
```

**(c) 删除 sh 层钳制**

`shell_operation.go` 两处（`ExecuteCmd` 与流式路径）删除：

```go
maxTimeout := 600
if v := os.Getenv("JW_EXECUTE_CMD_MAX_TIMEOUT"); v != "" { ... }
if timeout > maxTimeout { timeout = maxTimeout }
```

改为上游同款 `timeout = timeout or 300` 语义：`if timeout <= 0 { timeout = defaultExecCmdTimeout }`，不再截断。

`JW_EXECUTE_CMD_MAX_TIMEOUT` 环境变量在删除后无消费方，一并删除读取代码；文档中若提及需同步。

**影响面核查**：`execute_cmd` 还被 `code_operation.go` 与文件操作调用，这些调用方都不传 timeout，走默认 300s，行为不变。只有显式传大于 600 的调用方行为改变——这正是本缺陷要修的。

### 1.4 测试

- `resolveBashMaxOutputChars` 表驱动：`nil`→20000、`&0`→0、`&100`→200、`&5000`→5000、`&999999`→`BASH_TOOL_MAX_OUTPUT_CHARS`。四个工具各自一行。
- `ExecuteCmd` 不再截断：注入 `timeout=1200`，断言子进程拿到的 deadline ≈1200s（用 `context.Deadline()` 反查，不真等）。
- schema 拒绝负数：`FormatWithSchema` 传 `{"max_output_chars": -1}` 断言 `StatusSchemaValidateInvalid`。
- 集成：`tests/integration/` 下单测一条跨 harness→sys_operation 的超时传递链路（跨 ≥3 子系统）。

## 2. 批次 D-B：工具执行语义（D-03 + D-04）

### 2.1 D-03 缺陷

`ability_manager.go:473-521` 恒并行：

```go
var wg sync.WaitGroup
for i, tc := range toolCalls {
	wg.Add(1)
	go func(idx int, toolCall *llmschema.ToolCall, toolCtx *interfaces.AgentCallbackContext) { ... }(...)
}
```

`ReActAgentConfig` 无 `parallel_tool_calls`。有状态工具（browser 动作序列、worktree 切换）并发执行时顺序被打乱。

### 2.2 D-04 缺陷

`AbilityManager.Execute` 全程无 `context.WithTimeout`。`tool.invoke` 一旦挂住，整轮 ReAct 卡死；上游 `fe99da2` 的原始 issue 描述是 glob 扫大项目跑了一小时。

`ToolCard` 无 `resilience` 解析，也没有硬上限。

### 2.3 设计

**(a) `parallel_tool_calls` 透传（D-03）**

`Execute` 增加参数：

```go
func (am *AbilityManager) Execute(
	ctx context.Context,
	cbc *interfaces.AgentCallbackContext,
	toolCalls []*llmschema.ToolCall,
	sess sessioninterfaces.SessionFacade,
	tag string,
	parallelToolCalls bool,     // 新增
) []agentschema.ExecuteResult
```

`parallelToolCalls == false` 时把两处 `WaitGroup` 循环换成顺序 `for`（`cbc == nil` 降级路径与正常路径各一处），其余逻辑不动。

配置面按上游逐层透传，默认 `true`：

```
ReActAgentConfig.ParallelToolCalls   (single_agent/agents/config)
  ← DeepAgentConfig.ParallelToolCalls   (harness/schema/config.go)
  ← SubAgentConfig.ParallelToolCalls    (harness/schema/config.go)
  ← resolveDeepAgentParts / CreateDeepAgent 参数
```

`DeepAgentConfig` 的传递点对应上游 `deep_agent.py:726`；`SubAgentConfig` 对应 `config.py:265`。

**(b) 三层超时预算（D-04，不含重试）**

在 `runToolCallSafely` 的 `tool.invoke` 调用处包一层：

```go
callTimeout := resolveCallTimeout(toolCard)          // ① 卡片预算
if callTimeout == nil {
	callTimeout = maxToolCallTimeoutHardLimit        // ② 豁免 → 硬上限 3600
}
callTimeout = resolveInvocationTimeout(tool, args, callTimeout)  // ③ 工具钩子（D-02 第二半）
invokeCtx, cancel := context.WithTimeout(ctx, time.Duration(callTimeout*float64(time.Second)))
defer cancel()
result, err := tool.Invoke(invokeCtx, args, opts...)
if errors.Is(invokeCtx.Err(), context.DeadlineExceeded) {
	err = buildExecutionError(toolCall, fmt.Sprintf("Tool '%s' timed out after %vs", toolName, callTimeout))
}
```

`resolveCallTimeout` 逐条对齐 `ability_manager.py:679-713`：

| `ToolCard.Properties["resilience"]["timeout_s"]` | 结果 |
| --- | --- |
| 不存在 `resilience` 块 / 非 dict | `defaultToolCallTimeout`（300，可被 `DEFAULT_TOOL_CALL_TIMEOUT` 环境变量覆盖） |
| 键不存在 | 300 |
| 值为 `nil` | `nil`（豁免，由工具内部超时管辖） |
| 值为非正数 | `nil` |
| 值为正数 | 该值 |

`getCallTimeout` 钩子按上游 `base.py:255-276` 加在 `foundation/tool` 的 `Tool` 接口上，默认实现返回 `defaultTimeout`；`BashTool` / `PowerShellTool` 覆盖为 `timeout + 30`（`_CALL_TIMEOUT_GRACE_SECONDS = 30`，`bash/_tool.py:57`），后台模式原样返回 `defaultTimeout`。

**为什么必须挂在 D-04 上**：没有外层 `WithTimeout` 时 `getCallTimeout` 零调用方，单独实现是死代码。

**不做重试（用户决策）**：上游同一提交 `fe99da2` 附带的 `ToolCallResilienceRail`（三层重试判定）本次不实现。理由：重试会给服务端重复发送调用，非幂等工具（browser_click 等）可能造成重复副作用；当前先解决「挂死」，工具失败交给模型自我纠正。该决策记录在本文档，不再作为待办。

**(c) 超时错误形态**

对齐上游：超时产生带 `tool_call_id` 的 `ToolMessage`，内容含 `timed out`，走既有 `errorToExecuteResult` 路径回灌给模型，模型可自我纠正而非整轮失败。

`context.Canceled` 必须与 `DeadlineExceeded` 区分：用户中断（取消）**不**转成超时错误，继续上抛——对应上游 `except asyncio.CancelledError: raise`。

### 2.4 测试

- `parallel_tool_calls=false`：两个工具各记录进入/退出时刻，断言行程序列化（`first.exit <= second.enter`）。
- `parallel_tool_calls=true`：保持既有并行断言不回归。
- 超时预算表驱动：`resilience` 五种取值 → `resolveCallTimeout` 五个期望。
- 超时触发：注册一个 sleep 2s 的工具，卡片设 `timeout_s: 0.1`，断言返回 `tool_message` 含 `timed out` 且 `tool_call_id` 正确。
- 取消不转超时：调用方 `cancel()` 后断言错误是 `context.Canceled` 而非超时文案。
- `BashTool.GetCallTimeout` 返回 `timeout+30`；后台模式返回 `defaultTimeout`。

## 3. 批次 D-C：技能与 worktree 路径安全（D-05 + D-08 + D-09）

### 3.1 D-05 缺陷与设计

**三处改动**，对齐 `e868918b`：

1. **slug 扁平化**（`slug.go`）：

```go
func flattenSlug(slug string) (string, error) {
	if err := ValidateSlug(slug); err != nil {
		return "", err
	}
	return strings.ReplaceAll(slug, "/", "+"), nil
}
```

`WorktreePathFor(baseDir, slug)` 改为 `filepath.Join(WorktreesDir(baseDir), flatten(slug))`，`WorktreeBranchName` 同样走 `flattenSlug`。上游理由（`slug.py` docstring）：嵌套目录在删除父 worktree 时会连带删掉子 worktree。

2. **`ValidateSlug` 补 Windows 分隔符与盘符**：先 `strings.ReplaceAll(slug, "\\", "/")` 再分段校验，拒绝 `..\evil`、`C:\evil`。Go 侧当前只按 `/` 切分，`..\evil` 会被当成单段 `..\evil`，正则 `^[a-zA-Z0-9._-]+$` 恰好拒绝（反斜杠不在字符集）——行为上已正确，但错误文案误导。改为显式归一后校验，并补回归用例。

3. **`gitdir:` 内容校验**（`git.go:274`）：`filepath.Clean(filepath.Join(wtPath, gitdirContent))` 之后追加：

```go
rel, err := filepath.Rel(wtPath, gitDir)
if err != nil || strings.HasPrefix(rel, "..") {
	return "", fmt.Errorf("invalid gitdir %q escapes worktree %s", gitdirContent, wtPath)
}
```

`commondir` 处的 `:306` 同理：相对解析后必须仍在 `gitDir` 的祖先链内。

**定级**：保持 P0。可执行路径确实需要先写入 `{wt}/.git`（受 write_file 沙箱约束），属纵深防御；但安全边界类问题按 P0 口径处理，不做可达性降级。

### 3.2 D-08 缺陷与设计

当前 `skill_manager.go:1402-1434` 的两处缺口：

```go
name := trimSpace(toString(params["name"]))       // 无 safePathName
output := trimSpace(toString(params["output"]))
if output != "" { dirPath = filepath.Join(output, name) }   // 任意绝对路径
```

`output=/etc`、`name=../../x` 都能逃出 `skillsDir`。

上游语义是 `path` 参数（默认 `.`），表示「skills 目录内的相对路径」。设计：

- **参数名**：主参数改 `path`；`output` 保留为兼容别名，值与 `path` 走**同一套**校验（不是放行）。
- **`name`**：补 `safePathName(name, "skill")`，失败记 `logRejectedName` 并返回。
- **`path` 校验**（逐条对齐 `skill_manager.py`）：
  1. `~` / `~/` / `~\` 开头 → 拒绝「path 仅支持相对路径（限制在 skills 目录内）」
  2. 绝对路径（POSIX 与 Windows 两种判定）→ 同上
  3. `filepath.Abs(Join(skillsDir, path))` 后 `filepath.Rel(skillsDir, ...)`，`..` 开头 → 「path 越界：仅允许在 skills 目录内创建」
  4. 目录不存在或非目录 → 「path 不是有效目录: <abs>」
- **目标双保险**：`targetDir` 再过一次 `Rel` 越界校验 → 「目标路径越界：仅允许在 skills 目录内创建」

**连带补齐的语义**（同一函数，一次改完）：

| 项 | 上游行为 | Go 现状 |
| --- | --- | --- |
| `skill_type` 校验 | 只接受 `teamskills`/`skill`，否则报错 | 无该参数 |
| `force` | 目标非空且 `force` 时 `_safe_rmtree` 后重建 | 无 |
| 骨架内容 | `teamskills` 生成 `kind: team-skill` + `roles` 两项；`skill` 生成普通骨架，均含 `# <name>`、`## Instructions`、`TODO` 段 | 写死 `name/description/version` 三段 |
| 已存在判定 | 目录存在但为空可复用；非空且无 `force` → 「目标目录非空」 | 只要存在就报错 |

`force` 的删除必须走既有 `safeRmtree`（`skill_manager.go` 内已有），不得用 `os.RemoveAll`。

### 3.3 D-09 缺陷与设计

`skill_manager.go:686-690`：`safePathName` 失败直接返回错误。若 `local_skills` 里存在 `name` 含 `/` 的记录（安装侧可能写入），这条记录永远卸载不掉：目录删不到、记录清不掉、前端一直显示已安装。

对齐 `910af579`：

```go
safeName, err := safePathName(name, "skill")
if err != nil {
	// 回查 local_skills：用 origin（如 teamskillshub:xxx）里的 slug 定位目录
	slug := ""
	for _, ls := range sm.state["local_skills"].([]map[string]any) {
		if toString(ls["name"]) == name {
			if origin := toString(ls["origin"]); strings.Contains(origin, ":") {
				slug = origin[strings.LastIndex(origin, ":")+1:]
			}
			break
		}
	}
	if slug == "" {
		logRejectedName("skills.uninstall", "skill", name, err)
		return map[string]any{"success": false, "detail": err.Error()}, nil
	}
	safeName, err = safePathName(slug, "skill")
	if err != nil { /* 同样报错 */ }
}
```

**关键差异**：目录用 `safeName`（slug）定位，**记录删除仍用原始 `name`**——`removeInstalledPlugin` / `removeLocalSkill` 按 `name` 字段匹配，记录里的键是原始 name（`910af579` 明确改的就是这两处从 `name` 回到 `raw_name`）。

**范围限定**：只修卸载侧。安装侧是否写入脏 name 不在本条目内（严格一比一）。

### 3.4 测试

- slug：`user/feature` → `WorktreePathFor` 产出 `{base}/.worktrees/user+feature`；`../escape`、`..\evil`、`C:\evil` 三类均拒绝。
- gitdir 越界：构造 `{wt}/.git` 内容 `gitdir: ../../etc`，断言 `readWorktreeHeadSHA` 返回 error 且不读到 `/etc/HEAD`。
- teamskillshub init：`path=../../etc`、`path=/tmp`、`path=~` 三类拒绝；`path=.` 正常；`name=../../x` 拒绝；`force=false` 遇非空目录报错、`force=true` 重建；`skill_type=teamskills` 产出含 `kind: team-skill` 与两段 `roles`。
- 卸载：伪造 `local_skills` 里 `name="a/b"`、`origin="teamskillshub:a+b"` 的记录，断言卸载成功且记录被清。

## 4. 批次 D-D：读取预算与 shell 检测（D-10 + D-12）

### 4.1 D-10 缺陷与设计

- notebook（`read_file.go:306-312`）：全文估 token 超 25000 直接报错，模型只能改用 bash+jq。
- PDF（`readPDF`，`:428-468`）：逐页累积 `parts`，**完全无预算**，页内文本多时照样爆。

上游 `810bb071` 引入 `_TokenBudget`。Go 实现：

```go
// tokenBudget 按固定预算累积 token 消耗（Python: _TokenBudget）
type tokenBudget struct {
	maxTokens int
	spent     int
}

func (b *tokenBudget) canFit(tokens int) bool { return b.spent+tokens <= b.maxTokens }
func (b *tokenBudget) spend(tokens int)       { b.spent += tokens }
```

**notebook**：按 cell 组装（cell 内 `## Cell N [type]` / source / `### Outputs` / outputs 拼成一个 `cellText`）后计 token：

```go
if extractedCount > 0 && !budget.canFit(cellTokens) {
	blocks = append(blocks, fmt.Sprintf("… (truncated — %d/%d cells extracted, %d-token budget reached)",
		extractedCount, len(cells), maxTokens))
	break
}
budget.spend(cellTokens)
```

**PDF**：同样按页计。截断文案 `… (truncated — %d/%d pages extracted, %d-token budget reached)`。

**首块无条件放行**：`extractedCount == 0` 时不判预算，直接放行——避免「第一个 cell/页本身就超预算」导致输出空。这是上游的显式设计。

**`.ipynb` 二进制豁免**：Go 侧 `read_file.go:113` 已把 `isNotebook` 列入豁免，无需改动（上游这次是把豁免抽成 `_is_binary_check_exempt`，行为等价）。

### 4.2 D-12 缺陷与设计

三处，全在 Windows 分支：

**(a) 裸 `$var` 误判 PowerShell**

`shell_helpers.go:35` 当前 `psVariablePattern = (^|[\s;(])\$[A-Za-z_][A-Za-z0-9_]*`——任何 `$var` 都判 PS。但 `for i in 1 2; do echo $i; done` 在 bash 里完全合法，被误送进 PowerShell。

改为上游 `_PS_ASSIGNMENT_PATTERN`：

```go
var psAssignmentPattern = regexp.MustCompile(`(^|[\s;(])\$[A-Za-z_][A-Za-z0-9_]*\s*[-+*/]?=(?!=)`)
```

并把原 `psVariablePattern` 的判定**下沉到 POSIX 检测之后**（`shell_operation.go:812` 的 auto 分支）：

```
Windows auto 顺序：unwrap PowerShell → LooksLikePowerShell（赋值版）→ LooksLikePosix → 裸 $var 判 PS → cmd
```

这样 `$i` 这类 bash 也合法的语法，先给 bash 一次机会。

**(b) POSIX 检测缺控制流识别**

`LooksLikePosix` 只看段首命令名。新增三条 RE2 可控的正则（上游 `_BASH_CONTROL_FLOW_PATTERNS`，本就不含 lookbehind，可直接翻译）：

```
for/do/done      (?is)(?:^|[;\r\n])\s*for\s+(?:[A-Za-z_][A-Za-z0-9_]*\s+in\b|\(\().*?(?:;|[\r\n])\s*do\b.*?(?:;|[\r\n])\s*done\b
while/until/done (?is)(?:^|[;\r\n])\s*(?:while|until)\b.*?(?:;|[\r\n])\s*do\b.*?(?:;|[\r\n])\s*done\b
if/then/fi       (?is)(?:^|[;\r\n])\s*if\b.*?(?:;|[\r\n])\s*then\b.*?(?:;|[\r\n])\s*fi\b
```

**(c) 后台 stderr 丢弃**

`utils.go:474-477` 后台模式 `Stderr = nil`（DEVNULL）。进程早失败时无任何诊断信息。改为：

- `h.cmd.Stderr` 设为 `os.Pipe()`，goroutine 排空，保留前 8192 字节，超限追加 `[stderr truncated]`
- 早退检测（grace 窗口内退出且 exit code ≠ 0）把保留的 stderr 拼进错误信息
- grace 超时后继续排空但不保留，防止长时间运行的进程写满管道被阻塞

### 4.3 测试

- notebook 预算：构造 30 个 cell 的 ipynb，断言输出含 `… (truncated — N/30 cells extracted`，且 N ≥ 1。
- PDF 预算：同构，断言 `pages extracted` 文案。
- 首块放行：单个超大 cell（> 25000 token）断言仍被输出，不截断成空。
- `LooksLikePowerShell("$i")` → false；`LooksLikePowerShell("$x = 1")` → true；`LooksLikePowerShell("$env:PATH")` → true（token 表命中）。
- `LooksLikePosix("for i in a b; do echo $i; done")` → true；`if [ -f x ]; then echo y; fi` → true。
- 后台 stderr：`//go:build windows` 下跑一个立刻失败的命令，断言返回错误里含 stderr 文本；`os.Pipe` 排空逻辑用平台无关的单元测试覆盖（直接构造 handler）。

## 5. 批次 D-E：MCP（D-14 + D-15 + D-16）

### 5.1 D-14 缺陷与设计

`sse_client.go:326-360`、`streamable_http_client.go` 同款：`CallTool` 失败只包装报错，**不置 `isConnected=false`、不重建**。MCP 服务重启一次，client 永久失效。

**共享重连模块** `foundation/tool/mcp/client/reconnect.go`（对应上游 `client/reconnect.py`）：

```go
// isReconnectableError 判定错误是否属于"请求未完成"的连接类错误
func isReconnectableError(err error) bool

// reconnectAndRetry 重建底层 client 后重试一次
func reconnectAndRetry(ctx context.Context, c reconnectable, fn func() (any, error)) (any, error)
```

**只对连接类错误重试**（用户显式选择「严格按连接错误判定」）：

| 重试 | 不重试 |
| --- | --- |
| `connection reset`、`broken pipe`、`unexpected EOF`、`transport closed`、`session terminated`、`connection refused` | `isError:true` 的业务错误、`context.DeadlineExceeded`、参数/校验错误、`context.Canceled` |

重试**只做一次**，避免无限循环；重连失败时返回原始错误，不掩盖首个错误。

`sse_client.go` 与 `streamable_http_client.go` 共用该模块，各自实现 `reconnect()`（重建 session + transport）。

### 5.2 D-15 缺陷与设计

`mcp/base.go:73`：`item := contentList[len(contentList)-1]` —— 只取最后一块。多块结果（文本 + 截图）丢前面的。

**抽取函数**按上游 `extract_mcp_tool_result_content` 重写：

```
遍历全部块
  ├─ 有 text        → textParts.append(text)
  ├─ 有 data + image/* → includeImageContent ? images.append : textParts.append("[image content: ...]")
  ├─ 有 data（非图） → len==1 ? 直接 return data : textParts.append(str(data))
  ├─ 有 model_dump  → len==1 ? 直接 return dumped : textParts.append(...)
  └─ 其他           → len==1 ? 直接 return str : textParts.append(str)
拼接：text = strings.Join(textParts, "\n\n")
若 images 非空 → 产出 multimodal 结构
每个 return 点穿过 preserveMCPToolResultStatus（见 5.3）
```

多模态结构（对齐 `McpToolResult.data`）：

```go
map[string]any{
	"content":    text + "\n\n" + note,   // note = "N image(s) attached as multimodal input."
	"multimodal": []any{
		map[string]any{
			"type":        "image",
			"source":      "mcp",
			"source_path": toolName,
			"mime_type":   mimeType,
			"data_url":    "data:" + mimeType + ";base64," + data,
		},
	},
}
```

**配置面**：`McpServerConfig` 加 `IncludeImageContent bool`，默认 false（不开启则图片退化为文本占位符）。**不加上限**（与 Python 一致，用户决策）：不设单图大小与张数约束。

### 5.3 D-16 缺陷与设计

Go 完全不读 `isError`。`{"isError": true, "content": [{"text": "Permission denied"}]}` 被抽成 `map{"result": "Permission denied"}` 且 `err == nil` —— 失败被当成功。

**对等函数**（这是 D-16 的核心，不是"在 Invoke 里加个 if"）：

```go
// preserveMCPToolResultStatus 按 isError 给抽取结果打成功/失败标。
// Python: preserve_mcp_tool_result_status (mcp/base.py:36-53)
//
// isError 不为真时原样返回 content（成功值一个字节都不改）。
// isError 为真时收集所有文本块拼成 error（空则 "MCP tool reported an error."）。
func preserveMCPToolResultStatus(isError bool, content any) (any, error) {
	if !isError {
		return content, nil
	}
	// error 由调用方传入的 content 文本决定；结构在 buildMCPFailure 中组装
	return nil, buildMCPFailure(content)
}
```

抽取函数的 **6 个 return 点全部穿过它**（对齐上游 `base.py:79/99/107/111/133/134`）：无 content、单块 data、单块 dumped、单块 str、多模态结果、纯文本结果。

`MCPTool.Invoke` 侧：`isError` 时返回 `(nil, exception.BuildError(StatusToolMcpExecutionError, 错误文本))`，与 Go 既有错误路径一致（`ability_manager` 已有 error→ToolMessage 重写）。

### 5.4 多模态结果如何到达消费端（D-15 的接入点）

Python 的结构是**两条独立通道**，不展开 map：

```
result（原始对象，McpToolResult）→ ExecuteResult[0] → rail / 事件 / 多模态消费端
content（render_for_llm 渲染的字符串）→ ToolMessage   → 模型可见文本
```

`ability_manager.py:1928-1934`：`return result, tool_message`，`result` 原样透传。消费端 `_iter_multimodal_image_items` 拿到的就是 `McpToolResult` 本体，路径 `result.data.multimodal`，**没有 `result["result"]` 那一层**。

`render_for_llm`（`mcp/base.py:227-234`）只在渲染时剥包装：

```python
payload = output.data if isinstance(output, McpToolResult) else output["result"]
```

Go 侧对等落法（方案 A′）：

| 环节 | Go 实现 |
| --- | --- |
| 结构体 | `MCPToolResult{Success bool; Data map[string]any; Err string}`，落 `mcp/base.go` |
| `Invoke` 返回 | 多模态或错误时，`Invoke` 的 `map[string]any` 返回值置为 `{"__mcp_result__": &MCPToolResult{...}}`，由 `ability_manager` 拆出结构体装入 `Result`（理由见下） |
| `ability_manager` | `runToolCallSafely` 见 `MCPToolResult` 时，`Result` 装结构体本身；`ToolMsg.Content` 调 `renderForLLM` |
| `renderForLLM` | `payload = Data`（结构体）或 `m["result"]`（普通 map），再 `renderPayloadText` |
| 消费端 | `iterMultimodalImageItems` 加一条分支：`case *MCPToolResult: data = r.Data`；`case map[string]any: data = m["data"]` |

**为什么用 `__mcp_result__` 占位而不是直接换返回类型**：`Tool.Invoke` 的签名是 `(map[string]any, error)`，全仓所有 `Tool` 实现共用这个契约，改签名会波及 `InvokeFunction`、`LifecycleTool`、`map_function` 等全部实现者。用 map 里一个不可能与业务字段冲突的键携带结构体，把「类型识别」收敛在 `ability_manager` 与消费端两处，是全仓影响面最小的等价做法。

**`ExecuteResult.Result` 的类型**：`agentschema.ExecuteResult.Result any`，可装结构体。实施时需逐个核查 `ability_manager` 里现有的 `Result.(map[string]any)` 断言点——凡在 MCP 工具路径上的，都要加结构体分支（断言失败即取不到值，不会 panic，但要避免静默吞空）。

消费端文案同步改（上游 `react_agent.py:1373`）：`Image loaded from read_file: %s` → `Image loaded from {source}: {source_path}`，`source` 缺省 `"tool"`。

### 5.5 测试

- 多块拼接：`[{text:"ok"},{text:"detail"}]` → `"ok\n\ndetail"`（不再只剩 detail）。
- 图片占位符：`include_image_content=false` → 文本 `[image content: image/png, N base64 chars]`；`true` → 产出 `data.multimodal` 且 `data_url` 前缀 `data:image/`。
- 单块类型保持：单块 `data`（非图）→ 直接返回该值，不包装成字符串。
- `isError` 传递（6 个 return 点各一条）：成功路径值一字不改；失败路径返回 error 且文案含全部文本块；空内容失败 → `"MCP tool reported an error."`。
- 端到端：fake MCP server 返回 `[text, image]` + `include_image_content=true`，断言 `ExecuteResult.Result` 是结构体、`iterMultimodalImageItems` 取到 1 张图、`ToolMsg.Content` 是文本。
- 重连：httptest 起一个服务，第一次 `CallTool` 后关闭 listener → 断言触发重连、第二次调用成功；业务错误（`isError:true`）**不**触发重连。

## 6. 实施顺序与批次

| 批次 | 内容 | 依赖 | 预计改动文件 |
| --- | --- | --- | --- |
| D-A | D-01 + D-02 | 无 | shell 4 个输入结构体、`shell_operation.go`、2 个 prompts 文件 |
| D-B | D-03 + D-04 | D-A（`getCallTimeout` 是 D-04 的钩子，D-02 的 30s 宽限落在其中） | `ability_manager.go`、`foundation/tool/base.go`、`harness/schema/config.go`、`single_agent/agents/config` |
| D-C | D-05 + D-08 + D-09 | 无 | `worktree/slug.go`、`worktree/git.go`、`skill_manager.go` |
| D-D | D-10 + D-12 | 无 | `filesystem/read_file.go`、`sys_operation/local/{shell_helpers,shell_operation,utils}.go` |
| D-E | D-14 + D-15 + D-16 | 无 | `mcp/{base,types}.go`、3 个 client、新 `reconnect.go`、`react_invoke.go` |

D-A → D-B 有依赖（D-02 的宽限钩子是 D-04 的一部分）。C/D/E 三批互相独立，可并行。

### 删除清单（规则 8）

| 删除对象 | 位置 | 替代 |
| --- | --- | --- |
| `JW_EXECUTE_CMD_MAX_TIMEOUT` 读取代码 + 相关注释（3 处命中：`:148`、`:311` 注释、`:313`） | `shell_operation.go` | 上游裁决「caller owns budget」 |
| `MaxOutputChars int` 字段（4 处） | `BashInput`、`BashStreamInput`、`PowerShellInput`、`PowerShellStreamInput` | `*int` |
| `ExtractMCPToolResultContent` 的 `content[-1]` 取尾块逻辑 | `mcp/base.go:73-110` | 全块遍历拼接（§5.2） |
| `skill_manager.go` 的 `output` 单参数路径解析 | `HandleSkillsTeamSkillsHubInit` | `path` 语义 + 越界校验（§3.2） |

> 不删除的存量代码：`read_file.go:113` 的 `isNotebook` 内联判断（上游虽抽成 `_is_binary_check_exempt`，行为已等价，抽函数无收益，登记为偏差 D-D-1）。

### 已知偏差登记

| 编号 | Python 符号 + 位置 | Go 等价位置 | 偏差性质 | 理由 | 回收条件 |
| --- | --- | --- | --- | --- | --- |
| D-A-1 | `_resolve_max_output_chars`（`bash/_tool.py:113-126`）用 `min(200, value)` 把负数放大为 200 | `resolveBashMaxOutputChars` + schema `minimum:0` | 负数在 schema 层被拒，不进解析函数 | 模型传负数属非法输入，静默放大成 200 更危险 | 无（不回退） |
| D-B-1 | 上游 `fe99da2` 同时引入 `ToolCallResilienceRail` 三层重试 | 不实现 | 功能裁剪 | 重试会给非幂等工具造成重复副作用；先解决挂死，失败交给模型自我纠正 | 产品确认需要工具级重试时 |
| D-D-1 | `_is_binary_check_exempt`（`filesystem.py`）把 pdf/ipynb/image 合成一个判断 | `read_file.go:113` 内联三条件 | 结构等价，未抽函数 | Go 侧只有一处消费，抽函数无收益 | 出现第二个消费点时 |
| D-E-1 | `McpToolResult` 作为 `invoke` 返回值的独立类型，`ability_manager` 用 `isinstance` 识别 | `Tool.Invoke` 签名固定 `(map, error)`，用 `__mcp_result__` 键携带结构体 | 携带方式不同，识别点相同 | 改 `Tool` 接口签名会波及全仓实现者 | Go 侧 `Tool.Invoke` 收敛为统一返回结构时 |

## 7. 全局验收

每批次：

1. `go build ./...`、`go vet ./...`
2. 受影响包 `go test -race`
3. 受影响集成套件 `go test -tags=integration ./tests/integration/<包>/ -race`
4. 改动行覆盖率 ≥ 85%

全集完成后：`docs/sync/defect-plan-2026-11-04.md` §3.D 状态列全部更新，§0 摘要与 §4 批次一（P0 待办数）同步订正；§2 总览表重算。
