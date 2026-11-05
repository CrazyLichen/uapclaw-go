# Web WS 层 RPC 全量对齐实现计划（27-A B0/B1）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把本仓 Web WS 端点在本层承诺范围内对齐上游：47 个 `local` + 2 个 `agent:ok` = **49 个方法补齐**（现有 40 个已一致），同时把该层的文件划分、方法归属、函数名与上游 `gateway/channel_manager/web/` 做成 1:1。**下游缺口 59 条不在本计划接线**，只登记清单。

**Architecture:** 先用 `cmd/rpc-matrix` 把三方（前端 / 本仓 / 上游）方法集合变成可验收矩阵并接进 CI（无白名单），矩阵带**下游状态**列区分"本层能做实"与"需要 AgentServer"；再把现有 5 个 Go 文件按上游职责归位（删自造 `frame`/`RPCDispatcher`，normalize 迁 `gateway` 包，handler 改名到 Python 名）；最后按上游行区相邻性分七批补齐 49 个方法，每批一个提交、单独 CI 四绿。

**实测基线（2026-11-05，`cmd/rpc-matrix` 生成）:** 三方方法 181 = ✅ 一致 40 / ⚠️ 漂移 26 / ❌ 缺 108 / ❓ 前端独有 7；缺口下游分布 `local 47 / agent:ok 2 / agent:const-only 2 / agent:missing 40 / dep 17`。漂移 26 的来源见 spec §1.2（上游删除 1 / 上游走 HTTP 12 / 本仓自造 13）。

**Tech Stack:** Go 1.22（`sqlite_fts5` tag）、gorilla/websocket、标准库 `regexp`、现有 `cmd/integration-audit` 门禁模式、`tests/integration/swarm/gateway` 的 `CliRouteFullChainSuite` 装配。

**上游源码：** `/home/opensource/jiuwenswarm-develop`（`jiuwenswarm` 0.2.5.beta1）。本计划所有「上游 :行号」均指该目录下 `gateway/channel_manager/web/app_web_handlers.py`（除另行注明）。

**移植铁律（规则 9）:** 49 个 handler 一律按上游函数体逐行移植，函数名 = 上游函数名去前导下划线转 lowerCamel，参数顺序 = `(ws, reqID, params, sessionID)`。**不得**为了适配 Go 习惯合并/拆分步骤、改参数语义、换错误码。

**关于「不重抄函数体」:** 本计划不把上游函数体重抄一遍。每个 Task 给出 **上游文件:行号 + 函数名 + 完整注册代码 + 完整测试代码 + 至少一个完整 handler 模板**；其余函数体以该行号为准逐行移植。上游行号即权威实现，不是 TODO 占位。

**进度：Task 1-7b 已完成并入库**（B0 工具 + 两份 sync 文档 + CI 校验；B1a 结构归位：删 frame DTO `b86278c9`、三文件合并 `6e624576`、注册表回挂 WebChannel + normalize 迁 gateway + 30 个 handler 改名 `0909d27a`；B1a 收口：59 个注册点全部转原生 `methodHandler` + 帧级错误 + 分发顺序对齐）。Task 8（config 代理族 10 个）经 2026-11-05 裁定**本期跳过**（原因与拆分方案见该节）；**Task 9（九个 IM 平台配置 18 个）已完成**；**Task 10（health_check ×2 + cron.job.meta + updater ×4）已完成**（updater 按裁定仅保留 handler 回默认）；**Task 11（4 个杂项本地方法）已完成**；**Task 12（external_cli 7 个）已完成**（安装器保留 handler 回默认）；**Task 13（path.select_file）+ Task 14（session 薄代理 2 个）已完成**，后续剩 Task 15 收尾。

**CI 红灯归属（2026-11-05 记录）**：本计划的 B1b 各批自身门禁（Build/Lint/Unit/改动行覆盖）已通过；Integration 仍红在两处**非本计划文件**：

1. `tests/integration/swarm/gateway` 的 `TestCliRouteFullChainSuite/TestChatSendFullChain_*` —— 偶发
   "MockModelClient 未被调用"（整包跑偶发，单套件 `-count=5` 不复现，rerun 绿），属既存 flake，待专门一批治理。
2. `tests/integration/agentcore/evolving` 的 `TestEvolutionConsumptionE2E` —— **自合并 `00d8cbbc`（skill 会话基线 + skill_tool 对齐）
   起稳定失败**：提示词里只剩 `plain-skill`、`skill_tool` 报 `Skill not found: data-processor`，本地 `-count=1` 可稳定复现，
   属 skill rail 刷新/注册链路的回归，归该领域会话处理。

**B1a 收口结论（两处遗留偏差已清零，详见 Task 7b）**：① 删除 `unaryHandler`/`wrapUnary`，59 个注册点一律原生 `methodHandler`，失败帧抬到帧级 `ok=false + error + code`；② 分发顺序改为上游四段式（`local_only` → `handler-before-callback`(`chat.send`) → 回调 → handler）。残留差异见 spec §10（错误文案语言、permissions 本地回退、CI 矩阵上游来源）。

---

## 文件结构（最终态）

```
cmd/rpc-matrix/
├── doc.go              # 包注释（规则 1）
├── main.go             # 入口：-verify / -verify-all / -out
├── scan.go             # 三方扫描器
├── classify.go         # 上游 handler 形态与下游状态判定
├── matrix.go           # 归类 + Markdown 渲染（矩阵 + 缺口清单）
└── scan_test.go        # 扫描/判定/渲染单测
docs/sync/web-rpc-matrix.md            # 生成物（入库）
docs/sync/web-rpc-downstream-gaps.md   # 生成物（入库）
internal/swarm/gateway/channel_manager/web/
├── doc.go              # 文件目录同步
├── web_connect.go      # 对齐 web_connect.py（连接/帧/分发/register_method）
└── app_web_handlers.go # 对齐 app_web_handlers.py（全部 RPC handler + 注册）
internal/swarm/gateway/
├── gateway_server.go   # 已有
└── normalize.go        # 新增：对齐 app_gateway.py:314/:353/:2508
```

删除：`web/frame.go`、`web/frame_test.go`、`web/web_handlers.go`、`web/config_apply.go`、`web/memory_compute.go`、`web/web_normalize.go`（内容并入上表两文件）。

---

## Task 1: B0 扫描器（三方方法集合）

**Files:**
- Create: `cmd/rpc-matrix/doc.go`
- Create: `cmd/rpc-matrix/scan.go`
- Test: `cmd/rpc-matrix/scan_test.go`

- [x] **Step 1: 写失败测试**

`cmd/rpc-matrix/scan_test.go`：

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFile 在临时目录写一个文件（测试助手）。
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// TestScanFrontend_识别包装器调用 验证前端 RPC 方法名提取。
func TestScanFrontend_识别包装器调用(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src", "api.ts"),
		"export const a = () => call('models.list', {});\n"+
			"export const b = () => request('session.pin', {});\n"+
			"const NOT_RPC = 'not.a.method';\n")

	sites, err := scanFrontend(root)
	if err != nil {
		t.Fatalf("scanFrontend: %v", err)
	}
	got := map[string]bool{}
	for _, s := range sites {
		got[s.Method] = true
	}
	if !got["models.list"] || !got["session.pin"] {
		t.Fatalf("未识别到 RPC 方法，实际：%v", got)
	}
	if got["not.a.method"] {
		t.Fatalf("把普通字符串误判为 RPC：%v", got)
	}
}

// TestScanGo_识别注册点 验证 Register / RegisterLocalHandler 两种写法。
func TestScanGo_识别注册点(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "web", "handlers.go"),
		"func f() {\n"+
			"\td.Register(\"config.get\", h1)\n"+
			"\trs.RegisterLocalHandler(\"/tui\", \"session.delete\", h2)\n"+
			"}\n")

	sites, err := scanGo(root)
	if err != nil {
		t.Fatalf("scanGo: %v", err)
	}
	got := map[string]bool{}
	for _, s := range sites {
		got[s.Method] = true
	}
	if !got["config.get"] || !got["session.delete"] {
		t.Fatalf("未识别到注册点，实际：%v", got)
	}
}

// TestScanUpstream_识别两种注册函数 验证 register_method 与 config 代理。
func TestScanUpstream_识别两种注册函数(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "gateway", "h.py"),
		"    channel.register_method(\"models.get\", _models_get)\n"+
			"    _register_config_proxy(\"vendors.list\", _ConfigReq.VENDORS_LIST)\n")

	sites, err := scanUpstream(root)
	if err != nil {
		t.Fatalf("scanUpstream: %v", err)
	}
	got := map[string]bool{}
	for _, s := range sites {
		got[s.Method] = true
	}
	if !got["models.get"] || !got["vendors.list"] {
		t.Fatalf("未识别到上游注册，实际：%v", got)
	}
}
```

- [x] **Step 2: 运行测试确认失败**

Run: `go test ./cmd/rpc-matrix/ -run TestScan -v`
Expected: FAIL，`undefined: scanFrontend` 等编译错误。

- [x] **Step 3: 实现扫描器**

`cmd/rpc-matrix/doc.go`：

```go
// Package main 扫描前端、本仓与上游三方的 Web WS RPC 方法注册，
// 生成 docs/sync/web-rpc-matrix.md 契约矩阵，并在 -verify 模式下作为 CI 门禁。
//
// 文件目录：
//
//	rpc-matrix/
//	├── doc.go      # 包文档
//	├── main.go     # 入口与参数
//	├── scan.go     # 三方扫描器
//	├── matrix.go   # 归类与渲染
//	└── scan_test.go# 扫描单测
//
// 对应 Python 代码：无（本仓工程工具）。
package main
```

`cmd/rpc-matrix/scan.go`：

```go
package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// rpcSite 一个方法在某侧的登记点。
type rpcSite struct {
	// Method RPC 方法名
	Method string
	// Site 登记位置（文件:行）
	Site string
}

// methodPattern 方法名的统一形状：小写点分 2~4 段。
const methodPattern = `([a-z][a-z0-9_]*(?:\.[a-z0-9_]+){1,3})`

var (
	// frontendCallRe 前端包装器调用：call('x.y') / request("x.y") 等。
	frontendCallRe = regexp.MustCompile(`(?:call|request|invoke|rpc|send)\(\s*['"]` + methodPattern + `['"]`)
	// goRegisterRe 本仓注册：Register("x.y", h) 与 RegisterLocalHandler(path, "x.y", h)。
	goRegisterRe = regexp.MustCompile(`\.(?:Register|RegisterLocalHandler)\(\s*(?:[^,()]*,\s*)?["` + "`" + `]` + methodPattern + `["` + "`" + `]`)
	// pyRegisterRe 上游注册：register_method("x.y", h) 与 _register_config_proxy("x.y", ...)。
	pyRegisterRe = regexp.MustCompile(`(?:register_method|_register_config_proxy)\(\s*["']` + methodPattern + `["']`)
)

// scanTree 遍历 root，按 exts 过滤文件，用 re 提取方法名，返回登记点列表。
//
// 返回值 Site 使用相对 root 的路径，便于矩阵里直接读懂。
func scanTree(root string, exts map[string]bool, re *regexp.Regexp) ([]rpcSite, error) {
	var sites []rpcSite
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			name := d.Name()
			// 跳过依赖与构建产物目录，避免把第三方包里的同名串算进来。
			if name == "node_modules" || name == "dist" || name == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if !exts[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("读取 %s: %w", path, err)
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		for i, line := range strings.Split(string(data), "\n") {
			for _, m := range re.FindAllStringSubmatch(line, -1) {
				sites = append(sites, rpcSite{
					Method: m[1],
					Site:   fmt.Sprintf("%s:%d", filepath.ToSlash(rel), i+1),
				})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return sites, nil
}

// scanFrontend 扫描前端源码里的 RPC 调用。
func scanFrontend(root string) ([]rpcSite, error) {
	return scanTree(root, map[string]bool{".ts": true, ".tsx": true, ".js": true, ".jsx": true}, frontendCallRe)
}

// scanGo 扫描本仓的 RPC 注册点。
func scanGo(root string) ([]rpcSite, error) {
	return scanTree(root, map[string]bool{".go": true}, goRegisterRe)
}

// scanUpstream 扫描上游 Python 的 RPC 注册点。
func scanUpstream(root string) ([]rpcSite, error) {
	return scanTree(root, map[string]bool{".py": true}, pyRegisterRe)
}

// groupByMethod 把登记点按方法名聚合（保留首次出现顺序无关，排序由调用方决定）。
func groupByMethod(sites []rpcSite) map[string][]string {
	out := make(map[string][]string, len(sites))
	seen := make(map[string]map[string]bool, len(sites))
	for _, s := range sites {
		if seen[s.Method] == nil {
			seen[s.Method] = map[string]bool{}
		}
		key := s.Method + "@" + s.Site
		if seen[s.Method][key] {
			continue
		}
		seen[s.Method][key] = true
		out[s.Method] = append(out[s.Method], s.Site)
	}
	return out
}
```

- [x] **Step 4: 运行测试确认通过**

Run: `go test ./cmd/rpc-matrix/ -run TestScan -v`
Expected: PASS（3 个用例）。

- [x] **Step 5: 提交**

```bash
git add cmd/rpc-matrix/
git commit -m "feat(rpc-matrix): 三方 RPC 注册扫描器（前端/本仓/上游）"
```

---

## Task 2: B0 矩阵渲染 + `-verify` 门禁 + CI

**Files:**
- Create: `cmd/rpc-matrix/matrix.go`
- Create: `cmd/rpc-matrix/main.go`
- Modify: `.github/workflows/ci.yml`（Build job 增加一步）
- Output: `docs/sync/web-rpc-matrix.md`

- [x] **Step 1: 写失败测试（归类规则）**

追加到 `cmd/rpc-matrix/scan_test.go`：

```go
// TestBuildMatrix_三态归类 验证一致/漂移/缺三种状态。
func TestBuildMatrix_三态归类(t *testing.T) {
	rows := buildMatrix(
		map[string][]string{"models.list": {"a.ts:1"}, "project.list": {"b.ts:2"}},
		map[string][]string{"models.list": {"web.go:9"}, "harness.list": {"web.go:10"}},
		map[string][]string{"models.list": {"h.py:3"}, "project.list": {"h.py:4"}},
	)
	byMethod := map[string]string{}
	for _, r := range rows {
		byMethod[r.Method] = r.Status
	}
	if byMethod["models.list"] != statusOK {
		t.Fatalf("models.list 应为 %s，实际 %s", statusOK, byMethod["models.list"])
	}
	if byMethod["project.list"] != statusMissing {
		t.Fatalf("project.list 应为 %s，实际 %s", statusMissing, byMethod["project.list"])
	}
	if byMethod["harness.list"] != statusDrift {
		t.Fatalf("harness.list 应为 %s，实际 %s", statusDrift, byMethod["harness.list"])
	}
}

// TestMissing_只统计缺的行 验证 -verify 的判据集合。
func TestMissing_只统计缺的行(t *testing.T) {
	rows := []matrixRow{
		{Method: "a.b", Status: statusOK},
		{Method: "c.d", Status: statusMissing},
		{Method: "e.f", Status: statusDrift},
	}
	got := missing(rows)
	if len(got) != 1 || got[0].Method != "c.d" {
		t.Fatalf("missing 应为 [c.d]，实际 %v", got)
	}
}
```

- [x] **Step 2: 运行确认失败**

Run: `go test ./cmd/rpc-matrix/ -run "TestBuildMatrix|TestMissing" -v`
Expected: FAIL，`undefined: buildMatrix`。

- [x] **Step 3: 实现归类与渲染**

`cmd/rpc-matrix/matrix.go`：

```go
package main

import (
	"fmt"
	"sort"
	"strings"
)

const (
	// statusOK 三方一致（上游有、本仓有）。
	statusOK = "✅ 一致"
	// statusDrift 本仓有但上游未注册（自造或上游走 HTTP 映射）。
	statusDrift = "⚠️ 漂移"
	// statusMissing 上游有、本仓无。
	statusMissing = "❌ 缺"
)

// matrixRow 矩阵一行。
type matrixRow struct {
	// Method RPC 方法名
	Method string
	// Frontend 前端引用位置
	Frontend []string
	// Local 本仓注册位置
	Local []string
	// Upstream 上游注册位置
	Upstream []string
	// Status 三态之一
	Status string
}

// buildMatrix 合并三方集合，按方法名聚合后逐行判定状态。
//
// 判定：上游有且有本仓 → statusOK；上游有但无本仓 → statusMissing；
// 上游无但有本仓 → statusDrift（前端是否引用不影响判定）。
func buildMatrix(frontend, local, upstream map[string][]string) []matrixRow {
	methods := map[string]bool{}
	for m := range frontend {
		methods[m] = true
	}
	for m := range local {
		methods[m] = true
	}
	for m := range upstream {
		methods[m] = true
	}

	rows := make([]matrixRow, 0, len(methods))
	for m := range methods {
		row := matrixRow{Method: m, Frontend: frontend[m], Local: local[m], Upstream: upstream[m]}
		switch {
		case len(upstream[m]) > 0 && len(local[m]) > 0:
			row.Status = statusOK
		case len(upstream[m]) > 0:
			row.Status = statusMissing
		default:
			row.Status = statusDrift
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Method < rows[j].Method })
	return rows
}

// missing 返回状态为「缺」的行。
func missing(rows []matrixRow) []matrixRow {
	var out []matrixRow
	for _, r := range rows {
		if r.Status == statusMissing {
			out = append(out, r)
		}
	}
	return out
}

// render 生成 Markdown 矩阵文档。
func render(rows []matrixRow) string {
	var ok, drift int
	for _, r := range rows {
		switch r.Status {
		case statusOK:
			ok++
		case statusDrift:
			drift++
		}
	}
	var b strings.Builder
	b.WriteString("# Web WS RPC 契约矩阵\n\n")
	b.WriteString("> 由 `go run ./cmd/rpc-matrix` 生成，勿手工编辑。\n\n")
	fmt.Fprintf(&b, "- 方法总数：%d（✅ 一致 %d / ⚠️ 漂移 %d / ❌ 缺 %d）\n\n",
		len(rows), ok, drift, len(rows)-ok-drift)
	b.WriteString("| 方法 | 状态 | 前端引用 | 本仓注册 | 上游注册 |\n")
	b.WriteString("|---|---|---|---|---|\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s |\n",
			r.Method, r.Status,
			joinSites(r.Frontend), joinSites(r.Local), joinSites(r.Upstream))
	}
	return b.String()
}

// joinSites 拼接登记位置；为空时输出 —。
func joinSites(sites []string) string {
	if len(sites) == 0 {
		return "—"
	}
	if len(sites) > 3 {
		sites = sites[:3]
	}
	return strings.Join(sites, "<br>")
}
```

- [x] **Step 4: 实现入口**

`cmd/rpc-matrix/main.go`：

```go
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

// main 扫描三方 RPC 注册点，输出矩阵或在 -verify 模式下判定门禁。
func main() {
	var (
		repo      = flag.String("repo", ".", "本仓根目录")
		upstream  = flag.String("upstream", "/home/opensource/jiuwenswarm-develop", "上游 Python 仓库根目录")
		out       = flag.String("out", "docs/sync/web-rpc-matrix.md", "矩阵输出路径")
		verify    = flag.Bool("verify", false, "门禁模式：存在 ❌ 或缺上游目录即失败")
		frontDir  = flag.String("frontend", "internal/swarm/channels/web/frontend", "前端源码目录（相对 repo）")
		goDir     = flag.String("go", "internal/swarm", "本仓扫描目录（相对 repo）")
		upstreamD = flag.String("upstream-dir", "gateway", "上游扫描子目录（相对 upstream）")
	)
	flag.Parse()

	frontendSites, err := scanFrontend(filepath.Join(*repo, *frontDir))
	if err != nil {
		fmt.Fprintf(os.Stderr, "扫描前端失败: %v\n", err)
		os.Exit(2)
	}
	localSites, err := scanGo(filepath.Join(*repo, *goDir))
	if err != nil {
		fmt.Fprintf(os.Stderr, "扫描本仓失败: %v\n", err)
		os.Exit(2)
	}

	upstreamRoot := filepath.Join(*upstream, *upstreamD)
	if _, statErr := os.Stat(upstreamRoot); statErr != nil {
		if *verify {
			fmt.Fprintf(os.Stderr, "上游目录不可用，无法校验矩阵: %v\n", statErr)
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, "警告：上游目录不可用，矩阵仅含前端与本仓两侧: %v\n", statErr)
	}
	var upstreamSites []rpcSite
	if _, statErr := os.Stat(upstreamRoot); statErr == nil {
		upstreamSites, err = scanUpstream(upstreamRoot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "扫描上游失败: %v\n", err)
			os.Exit(2)
		}
	}

	rows := buildMatrix(groupByMethod(frontendSites), groupByMethod(localSites), groupByMethod(upstreamSites))

	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "创建输出目录失败: %v\n", err)
		os.Exit(2)
	}
	if err := os.WriteFile(*out, []byte(render(rows)), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "写矩阵失败: %v\n", err)
		os.Exit(2)
	}

	miss := missing(rows)
	fmt.Printf("矩阵已生成：%s（共 %d 个方法，缺 %d 个）\n", *out, len(rows), len(miss))
	if *verify && len(miss) > 0 {
		fmt.Fprintf(os.Stderr, "存在 %d 个上游已注册但本仓缺失的方法：\n", len(miss))
		for _, r := range miss {
			fmt.Fprintf(os.Stderr, "  ❌ %s（上游 %v）\n", r.Method, r.Upstream)
		}
		os.Exit(1)
	}
}
```

- [x] **Step 5: 运行测试 + 首次生成矩阵**

Run:
```bash
go test ./cmd/rpc-matrix/ -v
go run ./cmd/rpc-matrix -upstream /home/opensource/jiuwenswarm-develop -out docs/sync/web-rpc-matrix.md
```
Expected: 测试 PASS；输出「共 15x 个方法，缺 10x 个」；`docs/sync/web-rpc-matrix.md` 入库。

- [x] **Step 6: 接进 CI（Build job）**

`.github/workflows/ci.yml` 在「验证编译」步骤之后插入（缩进与该 job 其余 step 对齐）：

```yaml
      - name: 校验 Web RPC 契约矩阵
        run: |
          if git clone --depth 1 https://gitcode.com/openJiuwen/jiuwenswarm.git /tmp/upstream; then
            go run ./cmd/rpc-matrix -upstream /tmp/upstream -out docs/sync/web-rpc-matrix.md
            if ! git diff --exit-code -- docs/sync/web-rpc-matrix.md; then
              echo "矩阵已过期：请本地重跑 go run ./cmd/rpc-matrix 并提交 docs/sync/web-rpc-matrix.md"
              exit 1
            fi
          else
            echo "上游不可达，跳过矩阵漂移校验"
          fi
```

> 上游不可达时只跳过校验，不因克隆失败而红；`-verify` 的 `❌` 判据在 Task 17 收尾时转为强制。

- [x] **Step 7: 提交**

```bash
git add cmd/rpc-matrix/ docs/sync/web-rpc-matrix.md .github/workflows/ci.yml
git commit -m "feat(rpc-matrix): 生成契约矩阵并接入 CI 漂移校验"
```

---

## Task 3: B1a 删除自造 frame DTO

**Files:**
- Delete: `internal/swarm/gateway/channel_manager/web/frame.go`
- Delete: `internal/swarm/gateway/channel_manager/web/frame_test.go`
- Modify: `internal/swarm/gateway/channel_manager/web/doc.go`

- [x] **Step 1: 确认零生产引用**

Run: `grep -rn "web\.Frame\|NewResFrame\|NewEventFrame\|DecodeReqFrame\|ReqFrame{" --include=*.go internal/ tests/ cmd/ | grep -v frame_test.go`
Expected: 无输出（仅 `frame.go`/`frame_test.go` 自身命中）。

- [x] **Step 2: 删除文件并同步 doc.go**

```bash
git rm internal/swarm/gateway/channel_manager/web/frame.go internal/swarm/gateway/channel_manager/web/frame_test.go
```

`doc.go` 中删除 `frame.go` 那一行，并在包注释补一句：

```go
// 说明：上游 Web 通道全程以 dict 传递帧，无 frame DTO；本包同样不定义帧结构体。
```

- [x] **Step 3: 编译与测试**

Run: `CGO_ENABLED=1 go build -tags sqlite_fts5 ./... && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/gateway/...`
Expected: 全绿。

- [x] **Step 4: 提交**

```bash
git add -A internal/swarm/gateway/channel_manager/web/
git commit -m "refactor(web): 删除上游不存在的 frame DTO（规则 8/9）"
```

---

## Task 4: B1a 三文件合并为 `app_web_handlers.go`

**Files:**
- Rename+Modify: `web/web_handlers.go` → `web/app_web_handlers.go`
- Delete: `web/config_apply.go`、`web/memory_compute.go`
- Modify: `web/doc.go`

- [x] **Step 1: 文件改名**

```bash
cd internal/swarm/gateway/channel_manager/web
git mv web_handlers.go app_web_handlers.go
```

- [x] **Step 2: 合并另两个文件的内容**

把 `config_apply.go` 与 `memory_compute.go` 的全部声明按规则 2 的分区顺序合并进 `app_web_handlers.go`：

- 结构体（`ConfigBadRequest`/`ConfigInternalError` 等）→ 合并到文件的结构体区
- 常量/全局变量（如 `configEnvMap` 保持原位）→ 各归其区
- 其余包级函数（`handleConfigSet`/`applyConfigPatch`/`handleMemoryCompute` 等）→ 非导出函数区末尾，保持相互调用顺序不变

合并后删除源文件：

```bash
git rm config_apply.go memory_compute.go
```

- [x] **Step 3: 编译验证（此时不应有行为变化）**

Run: `CGO_ENABLED=1 go build -tags sqlite_fts5 ./... && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/gateway/channel_manager/web/`
Expected: 与 Step 0 相同结果（无新增失败）。

- [x] **Step 4: 同步 doc.go 文件目录**

把 `web_handlers.go`/`config_apply.go`/`memory_compute.go` 三行替换为：

```go
//	web/
//	├── doc.go              # 包文档
//	├── web_connect.go      # 连接、帧收发、方法注册与分发（对齐 web_connect.py）
//	└── app_web_handlers.go # 全部 RPC handler（对齐 app_web_handlers.py）
```

- [x] **Step 5: 提交**

```bash
git add -A internal/swarm/gateway/channel_manager/web/
git commit -m "refactor(web): 三个 handler 文件合并为 app_web_handlers.go（对齐上游单文件）"
```

---

## Task 5: B1a 删 `RPCDispatcher`，注册与分发回挂 `WebChannel`

**Files:**
- Modify: `web/web_connect.go`（新增 `methodHandlers`/`registerMethod`/`invokeMethodHandler`）
- Modify: `web/app_web_handlers.go`（71 个 `d.Register(...)` 改 `wc.registerMethod(...)`，handler 签名改 `(ws, reqID, params, sessionID)`）

**对齐依据：** `web_connect.py:371-400`（`register_method`/`unregister_method`）、`:103-105`（`_HANDLER_BEFORE_CALLBACK_METHODS`/`_LOCAL_ONLY_METHODS`）、`:2180-2231`（分发顺序：local_only → handler-before-callback → on_message 回调 → 路由）。

- [x] **Step 1: 写失败测试**

`web/web_connect_methods_test.go`（新文件）：

```go
package web

import (
	"testing"
)

// TestRegisterMethod_本地方法命中 验证注册后可按名调用且 localOnly 走本地分支。
func TestRegisterMethod_本地方法命中(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{}, nil, nil)
	called := 0
	wc.registerMethod("demo.echo", func(_ wsSender, _ string, params map[string]any, _ string) {
		called++
	}, true)

	if h, ok := wc.methodHandlers["demo.echo"]; !ok || h == nil {
		t.Fatalf("方法未注册")
	}
	if !wc.localOnlyMethods["demo.echo"] {
		t.Fatalf("localOnly 标记未生效")
	}
}

// TestUnregisterMethod_移除 验证反注册同时清掉 localOnly 标记。
func TestUnregisterMethod_移除(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{}, nil, nil)
	wc.registerMethod("demo.echo", func(wsSender, string, map[string]any, string) {}, true)
	wc.unregisterMethod("demo.echo")

	if _, ok := wc.methodHandlers["demo.echo"]; ok {
		t.Fatalf("方法未移除")
	}
	if wc.localOnlyMethods["demo.echo"] {
		t.Fatalf("localOnly 标记未清除")
	}
}
```

同批新增测试助手文件 `web/fake_ws_test.go`（后续所有 batch 复用，勿重复定义）：

```go
package web

import (
	"fmt"
	"sync"
)

// fakeWS 测试用帧记录器，实现 wsSender。
type fakeWS struct {
	// frames 已写出的帧
	frames []map[string]any
	// mu 保护 frames
	mu sync.Mutex
}

// WriteJSON 记录一帧。
func (f *fakeWS) WriteJSON(v any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := v.(map[string]any)
	if !ok {
		return fmt.Errorf("fakeWS 只接受 map 帧，收到 %T", v)
	}
	f.frames = append(f.frames, m)
	return nil
}

// lastFrame 返回最后一帧；无帧时返回 nil。
func (f *fakeWS) lastFrame() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.frames) == 0 {
		return nil
	}
	return f.frames[len(f.frames)-1]
}

// newFakeWS 创建帧记录器。
func newFakeWS() *fakeWS { return &fakeWS{} }
```

**测试构造约定（后续所有 batch 必须遵守）**

1. `NewWebChannel(cfg, nil, nil)` 是**仓库既有惯例**（`web_connect_test.go` 等 20+ 处这样写）：构造函数只把三参存进字段、不 deref，所以传 nil 不是"糊过去"。`nil, nil` 只用于**不碰依赖的功能**（注册表、纯计算、payload 拼装）。
2. handler 需要依赖时**必须给真实现或 fake**，不得靠 nil 让测试"跑过去"——否则就是假绿。
3. 唯一允许传 nil 且真的调 handler 的场景：**测上游自己的"依赖缺失分支"**，此时必须断言上游原文的错误码与文案（例：`channel.*.get_conf` 在 `channel_manager` 为 nil 时回 `SERVICE_UNAVAILABLE` + `"channel manager not available"`，见 `app_web_handlers.py:4367-4377`）。
4. 依赖用窄接口表达（`wsSender`、`channelConfSource`、`modelsConfigStore`…），生产实现是既有类型、测试实现是 fake；这属于 Go 无 duck typing 的语言层偏差，按规则 9.6 在 spec 登记。

- [x] **Step 2: 运行确认失败**

Run: `go test ./internal/swarm/gateway/channel_manager/web/ -run TestRegisterMethod -v`
Expected: FAIL，`undefined: wc.registerMethod`。

- [x] **Step 3: 在 `web_connect.go` 增加注册表**

```go
// wsSender 一帧写出的目标。
//
// 偏差（规则 9.6）：Python 靠 duck typing 直接把 ws 对象传给 handler（调 ws.send_text）；
// Go 需要静态类型，故用只含 WriteJSON 的窄接口等价该形参。生产实现为 *websocket.Conn
// 适配器（见 connSender），测试用 fake 记录帧。
type wsSender interface {
	WriteJSON(v any) error
}

// methodHandler RPC 方法处理器签名。
//
// Python: app_web_handlers.py handler 签名 `(ws, req_id, params, session_id)`；
// 响应由 handler 内部经 sendResponse/sendEvent 直接下发（部分方法多帧）。
type methodHandler func(ws wsSender, reqID string, params map[string]any, sessionID string)

// registerMethod 注册 req method 处理器。
//
// Python: WebChannel.register_method (web_connect.py:371-382)。
func (wc *WebChannel) registerMethod(method string, handler methodHandler, localOnly bool) {
	wc.methodsMu.Lock()
	defer wc.methodsMu.Unlock()
	wc.methodHandlers[method] = handler
	if localOnly {
		wc.localOnlyMethods[method] = true
	}
}

// unregisterMethod 移除方法及其 localOnly 标记。
//
// Python: WebChannel.unregister_method (web_connect.py:384-387)。
func (wc *WebChannel) unregisterMethod(method string) {
	wc.methodsMu.Lock()
	defer wc.methodsMu.Unlock()
	delete(wc.methodHandlers, method)
	delete(wc.localOnlyMethods, method)
}
```

在 `WebChannel` 结构体中补三个字段（放在 `dispatcher` 原位置并删除 `dispatcher`）：

```go
	// methodHandlers RPC 方法注册表（对齐 Python WebChannel._method_handlers）
	methodHandlers map[string]methodHandler
	// localOnlyMethods 仅本地处理、不转发 Agent 的方法集（对齐 _local_only_methods）
	localOnlyMethods map[string]bool
	// methodsMu 保护 methodHandlers/localOnlyMethods
	methodsMu sync.RWMutex
```

构造函数 `NewWebChannel` 中初始化：

```go
	wc := &WebChannel{
		// ...其余字段保持不变
		methodHandlers:   make(map[string]methodHandler),
		localOnlyMethods: make(map[string]bool),
	}
```

- [x] **Step 4: 迁移 71 个 handler 的签名与注册**

统一规则（逐条机械替换，不改变函数体逻辑）。**注意保持上游的闭包形态**：上游 handler 是 `_register_web_handlers(bind)` 内的嵌套函数，捕获 `channel`/`agent_client` 等，Go 侧同样是闭包（`func(ws wsSender, ...)` 形式，捕获 `wc` 与依赖），**不改成 WebChannel 的方法**（否则方法归属与上游不一致，违反规则 9.2）：

1. `func handleXxx(dep ...) RPCHandlerFunc { return func(ctx context.Context, params map[string]any, sessionID string) (map[string]any, error) {` →
   `func xxx(wc *WebChannel, dep ...) methodHandler { return func(ws wsSender, reqID string, params map[string]any, sessionID string) {`
2. 返回 `payload, nil` → `wc.sendResponse(ws, reqID, true, payload, "", "")`
3. 返回 `nil, fmt.Errorf(...)` + 调用方按 code 转换 → 直接在原地 `wc.sendResponse(ws, reqID, false, nil, err.Error(), WsErrInternalError)` 并 `return`
4. `stubHandler("x", payload)` → `func(ws wsSender, reqID string, _ map[string]any, _ string) { wc.sendResponse(ws, reqID, true, payload, "", "") }`
5. `d.Register("m", h)` → `wc.registerMethod("m", h, false)`；`chat.send` 等需先本地处理后转发的方法注册 `localOnly=false`，与上游 `_HANDLER_BEFORE_CALLBACK_METHODS` 对齐（`chat.send` 是 handler-before-callback 而非 local_only）

`WebChannel` 需补一个响应发送器（若已有 `sendResponse` 则复用，不再新增）：

```go
// connSender 把 *websocket.Conn 适配成 wsSender（生产路径）。
type connSender struct {
	// conn 底层连接
	conn *websocket.Conn
}

// WriteJSON 写一帧 JSON。
func (s connSender) WriteJSON(v any) error { return s.conn.WriteJSON(v) }

// sendResponse 向客户端回一帧 res。
//
// Python: WebChannel.send_response (web_connect.py:6xx)。
func (wc *WebChannel) sendResponse(ws wsSender, reqID string, ok bool, payload map[string]any, errMsg, code string) {
	frame := map[string]any{"type": "res", "id": reqID, "ok": ok}
	if payload != nil {
		frame["payload"] = payload
	}
	if errMsg != "" {
		frame["error"] = errMsg
	}
	if code != "" {
		frame["code"] = code
	}
	if err := ws.WriteJSON(frame); err != nil {
		logger.Warn(logComponent).Err(err).Str("req_id", reqID).Msg("写 res 帧失败")
	}
}
```

`ws.WriteJSON` 由 `wsSender` 接口提供；生产路径用 `connSender{conn}` 包装现有 `*websocket.Conn`（多协程并发写仍走既有 `wswrite` 包，`connSender` 只在单帧回复路径使用）。

- [x] **Step 5: 分发顺序对齐 `web_connect.py:2180-2231`**

`web_connect.go` 中原 `wc.dispatcher.Dispatch(...)` 调用点改为：

```go
	// Python: handler = self._method_handlers.get(method)
	wc.methodsMu.RLock()
	handler, hasHandler := wc.methodHandlers[method]
	isLocalOnly := wc.localOnlyMethods[method]
	wc.methodsMu.RUnlock()

	if isLocalOnly {
		if !hasHandler {
			wc.sendResponse(ws, reqID, false, nil, "unknown method: "+method, WsErrMethodNotFound)
			return
		}
		handler(ws, reqID, params, sessionID)
		return
	}
	handlerAlreadyCalled := false
	if handlerBeforeCallback(method) && hasHandler {
		handler(ws, reqID, params, sessionID)
		handlerAlreadyCalled = true
	}
	handledByCallback := wc.deliverToCallback(msg)
	if handledByCallback || handlerAlreadyCalled {
		return
	}
	if hasHandler {
		handler(ws, reqID, params, sessionID)
		return
	}
	wc.sendResponse(ws, reqID, false, nil, "unknown method: "+method, WsErrMethodNotFound)
```

配套新增：

```go
// handlerBeforeCallbackMethods 需在 on_message 回调之前执行的方法集。
//
// Python: _HANDLER_BEFORE_CALLBACK_METHODS = frozenset({ReqMethod.CHAT_SEND.value})。
var handlerBeforeCallbackMethods = map[string]bool{"chat.send": true}

// handlerBeforeCallback 判断方法是否需在回调前执行。
func handlerBeforeCallback(method string) bool { return handlerBeforeCallbackMethods[method] }
```

> 现有 `ForwardNoLocalHandlerMethods` 等表在 Task 6 一并迁走；本步先保证同一顺序语义。

- [x] **Step 6: 运行测试**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/gateway/... ./internal/swarm/channels/...`
Expected: 全绿（handler 级测试改为直接调用 `xxx(...)(fakeWS, "rid", params, "sid")` 或经 `methodHandlers` 表）。

- [x] **Step 7: 提交**

```bash
git add -A internal/swarm/gateway/channel_manager/web/
git commit -m "refactor(web): 删除 RPCDispatcher，注册与分发回挂 WebChannel（对齐 register_method）"
```

---

## Task 6: B1a normalize/forward 归位到 `gateway` 包

**Files:**
- Create: `internal/swarm/gateway/normalize.go`
- Delete: `web/web_normalize.go`
- Modify: `web/web_connect.go`（吸收 `BuildUserMessage`/`parseMode`）
- Modify: `internal/swarm/gateway/gateway_server.go`（改调用点）

**对齐依据：** `app_gateway.py:314`（`_normalize_gateway_message`）、`:353`（`_normalize_and_forward_message`）、`:2508`（调用点）。

- [x] **Step 1: 写失败测试**

`internal/swarm/gateway/normalize_test.go`：

```go
package gateway

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// TestNormalizeGatewayMessage_内容补query 验证 content → query 回填与 chat.resume 折叠。
func TestNormalizeGatewayMessage_内容补query(t *testing.T) {
	msg := &schema.Message{ReqMethod: schema.ReqMethodChatSend, Params: map[string]any{"content": "hi"}}
	out := NormalizeGatewayMessage(msg)
	if out.Params["query"] != "hi" {
		t.Fatalf("query 未回填：%v", out.Params)
	}

	resume := &schema.Message{ReqMethod: schema.ReqMethodChatResume, Params: map[string]any{}}
	out = NormalizeGatewayMessage(resume)
	if out.ReqMethod != schema.ReqMethodChatCancel {
		t.Fatalf("chat.resume 应折叠为 chat.cancel，实际 %v", out.ReqMethod)
	}
	if out.Params["intent"] != "resume" {
		t.Fatalf("intent 未设置：%v", out.Params)
	}
}
```

- [x] **Step 2: 运行确认失败**

Run: `go test ./internal/swarm/gateway/ -run TestNormalizeGatewayMessage_内容补query -v`
Expected: FAIL，`undefined: NormalizeGatewayMessage`。

- [x] **Step 3: 迁移实现**

把 `web/web_normalize.go` 中以下符号整体搬到 `internal/swarm/gateway/normalize.go`，包名改 `gateway`，函数体不动：

- `NormalizeGatewayMessage`
- `MakeNormAndForward`（含 `NormAndForwardFunc` 类型）
- `ForwardReqMethods`、`ForwardNoLocalHandlerMethods`
- 其内部依赖的私有助手（如 `rawModeText` 等）

搬完后：

```go
// normalize.go 包内引用调整：把 schema.XXX 保持原样；sendEvent 等 web 包内类型改为参数传入。
```

- [x] **Step 4: `BuildUserMessage`/`parseMode` 留在 web 包**

这两个符号移到 `web/web_connect.go`（上游属 `handle_connection` 内联构造 + `_parse_mode`），签名与实现不变。

- [x] **Step 5: 改调用点**

`internal/swarm/gateway/gateway_server.go` 里对 `web.NormalizeGatewayMessage` / `web.MakeNormAndForward` 的调用改为包内 `NormalizeGatewayMessage` / `MakeNormAndForward`。

- [x] **Step 6: 测试与清理**

Run:
```bash
grep -rn "ForwardReqMethods\|ForwardNoLocalHandlerMethods" --include=*.go internal/swarm/gateway/channel_manager/web/
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/gateway/...
```
Expected: grep 无输出；测试全绿。

- [x] **Step 7: 提交**

```bash
git add -A internal/swarm/gateway/
git commit -m "refactor(gateway): normalize/forward 迁回 gateway 包（对齐 app_gateway.py 归属）"
```

---

## Task 7: B1a 71 个 handler 改名到 Python 名

**Files:**
- Modify: `web/app_web_handlers.go`
- Modify: 全部 `web/*_test.go`

**规则：** Go 名 = 上游 handler 函数名去前导下划线转 lowerCamel（`_config_get` → `configGet`，`_hb_job_list` → `hbJobList`）。

- [x] **Step 1: 生成改名清单**

Run: `grep -n "^func (wc \*WebChannel)" internal/swarm/gateway/channel_manager/web/app_web_handlers.go`
按上表逐条对照上游函数名列出映射，写入提交说明。

- [x] **Step 2: 机械改名（示例 8 条，其余同规则）**

| 现名 | 新名（上游） | 上游位置 |
|---|---|---|
| `configGet` | `configGet` | `:2262` `_config_get` |
| `configSet` | `configSet` | `:2xxx` `_config_set` |
| `modelsList` | `modelsList` | `:2446` `_models_list` |
| `sessionList` | `sessionList` | `:2993` `_session_list` |
| `cronJobList` | `cronJobList` | `:6xxx` `_cron_job_list` |
| `heartbeatGetConf` | `heartbeatGetConf` | `:4xxx` `_heartbeat_get_conf` |
| `permissionsOwnerScopesGet` | `permissionsOwnerScopesGet` | `:5xxx` `_permissions_owner_scopes_get` |
| `memoryCompute` | `memoryCompute` | `:3995` `_memory_compute` |

> 现名是 `handleConfigGet` 这类自创前缀时一律改为上游名；已经是上游名的保持不动。

- [x] **Step 3: 批量替换**

```bash
cd internal/swarm/gateway/channel_manager/web
# 逐个函数名替换（示例，按 Step 1 清单补全）
sed -i 's/\bhandleConfigGet\b/configGet/g; s/\bhandleConfigSet\b/configSet/g; s/\bhandleModelsList\b/modelsList/g' app_web_handlers.go *_test.go
```

- [x] **Step 4: 验证无自创前缀残留**

Run: `grep -rn "handle[A-Z]" --include=*.go internal/swarm/gateway/channel_manager/web/ | grep -v "_test.go"`
Expected: 无输出。

- [x] **Step 5: 覆盖率门禁核对（纯搬移批必做）**

Run: `bash scripts/check-changed-line-coverage.sh`
Expected: 改动行覆盖率 ≥ 85%（含 5% 容差）。未达线时**在同批补测试**（把原测试文件改指向新名字后仍应真实执行到这些行），不做整段豁免——审查 M-13 已点名"把整段入口代码登记掉（1382 行）"不可接受。

- [x] **Step 6: 测试与提交**

```bash
CGO_ENABLED=1 go build -tags sqlite_fts5 ./... && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/gateway/...
git add -A internal/swarm/gateway/channel_manager/web/
git commit -m "refactor(web): handler 函数名对齐上游 Python 名（规则 9.2）"
```

---

## Task 7b: B1a 收口 —— 原生 handler + 分发顺序（已完成）

**背景：** Task 7 结束时留了两处偏差：① 59 个注册点全部经 `wrapUnary` 把 `(payload, error)` 适配成 `methodHandler`，失败信息塞在 payload 内（`{ok:false,error,code}`）而帧级 `ok` 恒为 true；② 分发顺序是「回调优先 → 本地 handler」，与上游相反。两处都在本 Task 清零（用户裁定：一次性全转 native + 分发顺序同批改）。

**Files:**
- Modify: `web/web_connect.go`、`web/app_web_handlers.go`
- Test: `web/web_dispatch_order_test.go`（新增）、`web/testhelper_dispatch_test.go`、全部 `web/*_test.go`
- Fix: `tests/integration/swarm/gateway/hot_reload/hot_reload_test.go`（Task 5 删 `RPCDispatcher` 后遗留的 `Dispatch` 调用）、`cmd/rpc-matrix/main.go`

- [x] **Step 1: 帧形状对齐上游 `send_response`**

`web_connect.go` 的 `sendResponse` 改为 `web_connect.py:487-507` 的形状：`payload` 恒存在（nil 落 `{}`）；`error`/`code` 只在 `ok=false` 时写出；`error` 为空时兜底 `"request failed"`。

- [x] **Step 2: 删除 `unaryHandler` / `wrapUnary`，59 个注册点全转原生**

每个 handler 改成 `func xxx(wc *WebChannel, deps...) methodHandler`，内部 `return func(ws wsSender, reqID string, params map[string]any, sessionID string)`；所有失败分支改 `wc.sendResponse(ws, reqID, false, nil, errMsg, code)`，成功分支 `wc.sendResponse(ws, reqID, true, payload, "", "")`。`stubHandler` 去掉无用的方法名参数（`stubHandler(wc, payload)`）。顺手对齐三处文案/键形状：`config.set`/`config.save_all`/`memory.forbidden.set`/`permissions.owner_scopes.set` 的 params 校验回上游英文；`history.get` 补 `cursor`/`limit` 透传（`app_web_handlers.py:4165-4176`）；`chat.user_answer` 的 `request_id` 仅非空时透出（`:4151-4156`）。

- [x] **Step 3: handler 异常兜底**

新增 `invokeMethodHandler`（对齐 `web_connect.py:624-678`）：`recover` 后记诊断日志、回 `INTERNAL_ERROR` 帧、返回 false；原 `invokeMethod` 删除。

- [x] **Step 4: 分发顺序四段式**

`handleWebSocket` 读循环改为上游顺序：`local_only`（空集）→ `handler-before-callback`（`chat.send`，见全局变量 `handlerBeforeCallbackMethods`）→ 回调转发 → 本地 handler / `unknown method`。`chat.send` 的 ack 在回调前**立即写出**（`writeCollectedFrames`），不再攒到回调结束后。

- [x] **Step 5: 测试迁移与新增**

`callHandler` / `errorCode` 测试助手取代旧 `(payload, error)` 直调；失败断言由 `result["ok"]==false`/`result["code"]` 改为 `require.Error` + `errorCode(err)`。新增 `web_dispatch_order_test.go` 11 例：帧形状 3 例、异常兜底 2 例、分发顺序 6 例（trail 记录同 goroutine 上的执行次序，不靠 socket 时序）。

- [x] **Step 6: 修 Task 5 遗留的集成测试断点**

`hot_reload` 集成用例改用真实 WS 连接（`httptest` + `HandleWebSocket` + 一帧 `config.save_all`）验证热重载回调。

- [x] **Step 7: 撤掉 CI 的矩阵校验步骤（用户裁定）**

`ci.yml` Build job 删除「校验 Web RPC 契约矩阵」：CI 克隆的上游 HEAD 已无 `gateway/`（代码在 `jiuwenswarm/…` 下，且 tag `0.2.5.beta1` 的文件与计划基准快照行数不同），该步骤只能拿降级矩阵误报"已过期"。矩阵与缺口清单改为**本地命令**（`go run ./cmd/rpc-matrix -upstream <快照>`），漂移不再由 CI 拦截，登记在 spec §10。

- [x] **Step 8: 验收**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/...
CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/gateway/...
go run ./cmd/rpc-matrix -upstream /home/opensource/jiuwenswarm-develop
go run ./cmd/integration-audit -verify
```

---

## Task 8: B1b-1 config 代理族（10 个，`local`）——⤵️ 本期跳过（2026-11-05 裁定）

> **状态：跳过，不实施。** 下面保留原设计供后续独立计划使用；本期继续 Task 9。
>
> **跳过原因（读快照源码后的更正）：**
>
> 1. **原设计前提有误。** 本 Task 原写"`_register_config_proxy` 在单用户下走 `local_handler`"。快照实际判定（`app_web_handlers.py:5582-5610`）是
>    `if local_handler is not None and is_enterprise() and use_local_handler:` —— **个人版恒走 E2A**，只有企业版 + 无 client / `RuntimeRoutedAgentClient` /
>    legacy 共享目录才用本地 handler。`_proxy_config_request` 的 docstring 说"单用户共享目录走本地"，与代码判定自相矛盾，原设计照 docstring 抄错了。
> 2. **E2A 薄代理在本仓是断路。** `internal/` 下 grep `models.upsert` / `model_groups.*` / `vendors.*` / `session.selection.set` 均 0 命中，
>    `schema.ReqMethod` 里也没有 `MODELS_*`；转过去 AgentServer 只会回 `unknown method`。所以这 10 个只能本地实现，绕不开模型目录子系统。
> 3. **"10 个薄 handler"其实是整套子系统移植**（约 1,200 行 Python）：
>
>    | 上游文件 | 行数 | 作用 |
>    |---|---|---|
>    | `gateway/common/model_selection.py` | 76 | `ModelSelection` 校验（type/id + 引用语义） |
>    | `gateway/common/model_errors.py` | 30 | `MODEL_SELECTION_NOT_FOUND` / `_REFERENCED` 错误码与载荷 |
>    | `gateway/common/model_catalog.py` | 238 | `list_public_groups` / `get_public_model_detail` / `find_references` |
>    | `gateway/common/config.py` | ~150 | `upsert_model_resource` / `upsert_model_group_resource` / `delete_model_resource` |
>    | `gateway/common/model_vendor_registry.py` | 593 | 厂商预设（plan/endpoint/鉴权/模型域），依赖 core llm 枚举 + 火山端点 |
>    | `app_web_handlers.py` | ~180 | `_model_resource_rpc` / `_vendors_list` / `_vendors_fetch_models`（含远端 HTTP 拉取与回退） |
>
> 4. **可切分。** `model_catalog` 不依赖 `model_vendor_registry`（前者只依赖 config + selection + errors），所以恢复时可拆两批：
>    **8a** = `models.*` + `model_groups.*` + `session.selection.set`（约 500 行，新建 `gateway/modelcatalog` 子包）；
>    **8b** = `vendors.list` / `vendors.fetch_models`（约 700 行，含远端拉取与回退语义）。
>
> **对验收口径的影响：** 矩阵里这 10 个仍是 `❌ 缺`（`local`），Task 15 的"本层承诺范围 ❌ 归零"改为"除本 Task 10 个之外归零"。
> 恢复条件：另立计划，先移植 8a 的数据层再分批接 handler；不得用"E2A 薄代理"假接线（会变成绿但不通）。

**Files:**
- Modify: `web/app_web_handlers.go`
- Test: `web/app_web_handlers_models_test.go`

**方法清单（上游 `_register_config_proxy`，单用户走本地配置读写）:**

| 方法 | 上游 | Go 名 |
|---|---|---|
| `models.get` | `:5643` | `modelsGet` |
| `models.upsert` | `:5647` | `modelsUpsert` |
| `models.delete` | `:5649` | `modelsDelete` |
| `models.references` | `:5651` | `modelsReferences` |
| `model_groups.list` | `:5653` | `modelGroupsList` |
| `model_groups.upsert` | `:5655` | `modelGroupsUpsert` |
| `model_groups.delete` | `:5657` | `modelGroupsDelete` |
| `session.selection.set` | `:5659` | `sessionSelectionSet` |
| `vendors.list` | `:5677` | `vendorsList` |
| `vendors.fetch_models` | `:5678` | `vendorsFetchModels` |

- ⤵️ **Step 1: 读上游实现**

Run: `sed -n '5504,5700p' /home/opensource/jiuwenswarm-develop/gateway/channel_manager/web/app_web_handlers.py`
要点：`_register_config_proxy(name, req_method, local_handler)` 在**单用户**下走 `local_handler`；AgentOS 多用户才走 `_proxy_config_request`（E2A）。本仓只有单用户路径 → 只实现 local_handler 语义，多用户分支在函数注释标 ⤵️。

- ⤵️ **Step 2: 写失败测试**

```go
// TestModelsFamily_注册齐全 验证 10 个方法都已注册。
func TestModelsFamily_注册齐全(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{}, nil, nil)
	registerModelsFamily(wc, fakeModelsConfigStore{})
	for _, m := range []string{
		"models.get", "models.upsert", "models.delete", "models.references",
		"model_groups.list", "model_groups.upsert", "model_groups.delete",
		"session.selection.set", "vendors.list", "vendors.fetch_models",
	} {
		if _, ok := wc.methodHandlers[m]; !ok {
			t.Errorf("方法未注册: %s", m)
		}
	}
}

// TestModelsGet_返回配置 验证正向路径 payload 透出。
func TestModelsGet_返回配置(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{}, nil, nil)
	registerModelsFamily(wc, fakeModelsConfigStore{
		models: map[string]any{"models": []any{map[string]any{"name": "qwen-max"}}},
	})
	ws := newFakeWS()
	wc.methodHandlers["models.get"](ws, "rid", map[string]any{}, "sid")
	if ws.lastFrame()["ok"] != true {
		t.Fatalf("预期成功，实际 %v", ws.lastFrame())
	}
}

// fakeModelsConfigStore 测试用配置读写。
type fakeModelsConfigStore struct {
	// models GetModels 返回值
	models map[string]any
}

// GetModels 返回预置模型清单。
func (f fakeModelsConfigStore) GetModels() (map[string]any, error) { return f.models, nil }

// 其余方法在需要断言的用例中按需实现，未实现的返回零值。
```

> `modelsConfigStore` 接口其余方法（`UpsertModel` 等）在 fake 上补空实现即可，编译期由接口约束保证签名一致。

- ⤵️ **Step 3: 实现（模板，其余 9 个同构）**

```go
// registerModelsFamily 注册 model / model_group / vendor / session 选择族方法。
//
// Python: app_web_handlers.py:5643-5678（_register_config_proxy 段）。
// 与上游一致：handler 是 _register_web_handlers(bind) 内的闭包，捕获 channel 与
// 配置读写函数，因此这里用闭包而非 WebChannel 方法。
func registerModelsFamily(wc *WebChannel, cfg modelsConfigStore) {
	wc.registerMethod("models.get", modelsGet(wc, cfg), false)
	wc.registerMethod("models.upsert", modelsUpsert(wc, cfg), false)
	wc.registerMethod("models.delete", modelsDelete(wc, cfg), false)
	wc.registerMethod("models.references", modelsReferences(wc, cfg), false)
	wc.registerMethod("model_groups.list", modelGroupsList(wc, cfg), false)
	wc.registerMethod("model_groups.upsert", modelGroupsUpsert(wc, cfg), false)
	wc.registerMethod("model_groups.delete", modelGroupsDelete(wc, cfg), false)
	wc.registerMethod("session.selection.set", sessionSelectionSet(wc, cfg), false)
	wc.registerMethod("vendors.list", vendorsList(wc, cfg), false)
	wc.registerMethod("vendors.fetch_models", vendorsFetchModels(wc, cfg), false)
}

// modelsConfigStore 配置代理族所需的读写面（生产实现复用 config_apply 内的读写函数）。
type modelsConfigStore interface {
	// GetModels 读取模型清单与默认项
	GetModels() (map[string]any, error)
	// UpsertModel 新增/更新单个模型
	UpsertModel(params map[string]any) (map[string]any, error)
	// DeleteModel 删除单个模型
	DeleteModel(name string) (map[string]any, error)
	// ModelReferences 查询模型被引用情况
	ModelReferences(name string) (map[string]any, error)
	// ListModelGroups 列出模型分组
	ListModelGroups() (map[string]any, error)
	// UpsertModelGroup 新增/更新模型分组
	UpsertModelGroup(params map[string]any) (map[string]any, error)
	// DeleteModelGroup 删除模型分组
	DeleteModelGroup(id string) (map[string]any, error)
	// SetSessionSelection 写入会话级模型选择
	SetSessionSelection(params map[string]any) (map[string]any, error)
	// ListVendors 列出厂商
	ListVendors() (map[string]any, error)
	// FetchVendorModels 拉取厂商模型列表
	FetchVendorModels(params map[string]any) (map[string]any, error)
}

// ModelsGet 读取模型配置。
//
// Python: _register_config_proxy("models.get", _ConfigReq.MODELS_GET, <local handler>)。
// 移植要求：返回 payload 的键名与上游 local handler 完全一致。
func modelsGet(wc *WebChannel, cfg modelsConfigStore) methodHandler {
	return func(ws wsSender, reqID string, _ map[string]any, _ string) {
		payload, err := cfg.GetModels()
		if err != nil {
			wc.sendResponse(ws, reqID, false, nil, err.Error(), WsErrInternalError)
			return
		}
		wc.sendResponse(ws, reqID, true, payload, "", "")
	}
}
```

> 其余 9 个 `make*` 同构：只把接口方法与错误分支换成上游对应函数体；`params` 取值键名逐字对齐上游。

- ⤵️ **Step 4: 跑单测**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/gateway/channel_manager/web/ -run "TestModels" -v`
Expected: PASS。

- ⤵️ **Step 5: 更新矩阵并提交**

```bash
go run ./cmd/rpc-matrix -upstream /home/opensource/jiuwenswarm-develop
git add -A internal/swarm/gateway/channel_manager/web/ docs/sync/
git commit -m "feat(web): 补齐 models/model_groups/vendors/session.selection 方法族（10 个）"
```

---

## Task 9: B1b-2 九个 IM 平台配置（18 个，`local`）

**Files:**
- Modify: `web/app_web_handlers.go`
- Test: `web/app_web_handlers_channel_conf_test.go`

**方法清单：** `feishu / xiaoyi / telegram / dingtalk / whatsapp / discord / slack / wecom / wechat` 各一对 `channel.<p>.get_conf` / `channel.<p>.set_conf`（上游 `:5844-5861`）。

- [x] **Step 1: 读上游 9 组实现**

```bash
cd /home/opensource/jiuwenswarm-develop/gateway/channel_manager/web
sed -n '4360,4420p;5780,5870p' app_web_handlers.py
grep -n "_channel_.*_set_conf" -A 20 app_web_handlers.py | head -60
```
要点（`_channel_feishu_get_conf` 原文）：`cm is None` → `ok=False`、`error="channel manager not available"`、`code="SERVICE_UNAVAILABLE"`。

- [x] **Step 2: 写失败测试（注册 + nil 分支 + 正向）**

```go
// TestChannelConf_九平台齐全 验证 9 平台 × 2 方法全部注册。
func TestChannelConf_九平台齐全(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{}, nil, nil)
	registerChannelConfFamily(wc, nil)
	for _, p := range channelConfPlatforms {
		for _, suffix := range []string{"get_conf", "set_conf"} {
			if _, ok := wc.methodHandlers["channel."+p+"."+suffix]; !ok {
				t.Errorf("方法未注册: channel.%s.%s", p, suffix)
			}
		}
	}
}

// TestChannelConf_无CM时回SERVICE_UNAVAILABLE 验证依赖缺失分支与上游文案一致。
//
// 传 nil 是**被测语义**（上游 app_web_handlers.py:4367-4377 的真实分支）。
func TestChannelConf_无CM时回SERVICE_UNAVAILABLE(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{}, nil, nil)
	registerChannelConfFamily(wc, nil)
	ws := newFakeWS()
	wc.methodHandlers["channel.feishu.get_conf"](ws, "rid", map[string]any{}, "sid")
	frame := ws.lastFrame()
	if frame["ok"] != false || frame["code"] != WsErrServiceUnavailable {
		t.Fatalf("预期 SERVICE_UNAVAILABLE，实际 %v", frame)
	}
	if frame["error"] != "channel manager not available" {
		t.Fatalf("文案与上游不一致：%v", frame["error"])
	}
}
```

- [x] **Step 3: 用平台表驱动实现**

```go
// channelConfSource 渠道配置读写面（Go 对上游 duck-typed `cm.get_conf/set_conf` 的等价表达）。
type channelConfSource interface {
	// GetConf 读取指定渠道的原始配置
	GetConf(name string) (map[string]any, error)
	// SetConf 写回指定渠道配置
	SetConf(name string, conf map[string]any) error
}

// channelConfPlatforms 支持 get_conf/set_conf 的 IM 平台清单。
//
// Python: app_web_handlers.py:5844-5861 逐个平台注册。
var channelConfPlatforms = []string{"feishu", "xiaoyi", "telegram", "dingtalk", "whatsapp", "discord", "slack", "wecom", "wechat"}

// registerChannelConfFamily 注册九个 IM 平台的配置读写方法。
func registerChannelConfFamily(wc *WebChannel, cm channelConfSource) {
	for _, p := range channelConfPlatforms {
		platform := p
		wc.registerMethod("channel."+platform+".get_conf", func(ws wsSender, reqID string, params map[string]any, sid string) {
			channelConfGet(wc, cm, platform, ws, reqID)
		}, false)
		wc.registerMethod("channel."+platform+".set_conf", func(ws wsSender, reqID string, params map[string]any, sid string) {
			channelConfSet(wc, cm, platform, ws, reqID, params)
		}, false)
	}
}

// channelConfGet 读取单个 IM 平台的配置。
//
// Python: _channel_feishu_get_conf 等 (app_web_handlers.py:4365-...)。分支顺序照抄：
// 先判 cm 缺失（SERVICE_UNAVAILABLE + "channel manager not available"），
// 再取配置、按平台归一化、最后回 payload。
func channelConfGet(wc *WebChannel, cm channelConfSource, platform string, ws wsSender, reqID string) {
	if cm == nil {
		logger.Warn(logComponent).Str("platform", platform).Str("req_id", reqID).
			Msg("channel manager not available")
		wc.sendResponse(ws, reqID, false, nil, "channel manager not available", WsErrServiceUnavailable)
		return
	}
	raw, err := cm.GetConf(platform)
	if err != nil {
		wc.sendResponse(ws, reqID, false, nil, err.Error(), WsErrInternalError)
		return
	}
	wc.sendResponse(ws, reqID, true, raw, "", "")
}
```

> 若上游某平台 handler 体确有差异（feishu 归一 `apps`、wechat 多 login 分支），在该平台分支里照抄，不得为了 DRY 合并语义。

- [x] **Step 4: 测试 + 矩阵 + 提交**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/gateway/channel_manager/web/ -run TestChannelConf -v
go run ./cmd/rpc-matrix -upstream /home/opensource/jiuwenswarm-develop
git add -A && git commit -m "feat(web): 补齐九个 IM 平台配置读写 18 个方法"
```

---

**实施记录（2026-11-05 完成）：**

- 18 个方法在 `registerChannelConfFamily` 里**逐条字面量注册**（不再用 7 平台 stub 循环）：一是与上游 18 条
  `register_method` 一一对应，二是 `cmd/rpc-matrix` 是字面量扫描，循环注册会让矩阵看不到（本批踩过一次，缺口数不降）。
- 普通平台（telegram/dingtalk/whatsapp/discord/slack/wecom/wechat）：`cm.SetConf(platform, params)` → 回读 →
  `updateChannelInConfig`（逐键并入 `channels.<p>`，round-trip 写回）→ 成功则 `clearAgentConfigCache`。
- feishu/xiaoyi：`params` 必须含 `apps`（缺失按上游 `KeyError` 兜成 `INTERNAL_ERROR`，文案 `'apps'`）；
  归一化补 `_FEISHU_APP_DEFAULTS` / `_XIAOYI_APP_DEFAULTS` → `mergeAppsByID` → `cm.SetConf({apps})` →
  `replaceChannelSubsectionWithCleanup`（替换 `apps` 并清掉旧平铺键，`keepKeys={apps,send_file_allowed}`）。
- 清缓存时机按上游差异保留：feishu 在响应之后（Python `finally`），xiaoyi 在响应之前。
- 归一化的已知语义（与上游同构）：合并是 `{**existing, **normalized}`，缺省表里有的键以归一化结果为准（含空串），
  只有缺省表之外的运行时字段（`last_chat_id` / `bot_open_id`）才真正保留——`_merge_apps_by_id` 的注释与实际效果不符，
  本仓照实际行为移植，测试 `TestChannelConf_feishu_set_按app_id合并保留运行时字段` 固定这一口径。
- 偏差：`get_conf` 上游有 `try/except → INTERNAL_ERROR`，Go 的 `cm.GetConf` 无错误返回，故无该分支。
- 矩阵：`local` 缺口 47 → 29（本批 18 个归零）。


---

## Task 10: B1b-3 health_check / cron / updater（7 个，`local`）

**Files:**
- Modify: `web/app_web_handlers.go`
- Test: `web/app_web_handlers_cron_health_test.go`

| 方法 | 上游 | Go 名 |
|---|---|---|
| `health_check.get_conf` | `:5839` | `healthCheckGetConf` |
| `health_check.set_conf` | `:5840` | `healthCheckSetConf` |
| `cron.job.meta` | `:5865` | `cronJobMeta` |
| `updater.upgrade` | `:5835` | `updaterUpgrade` |
| `updater.get_conf` | `:5836` | `updaterGetConf` |
| `updater.reset_source` | `:5837` | `updaterResetSource` |
| `updater.set_conf` | `:5838` | `updaterSetConf` |

- [x] **Step 1: 读上游**

Run: `sed -n '4252,4270p;5820,5870p' /home/opensource/jiuwenswarm-develop/gateway/channel_manager/web/app_web_handlers.py`
要点（`_health_check_get_conf` 原文）：服务为 None → `ok=False, error="health_check service not available", code="SERVICE_UNAVAILABLE"`；异常 → `code="INTERNAL_ERROR"`。

- [x] **Step 2: 写失败测试**

```go
// TestHealthCheck_缺服务时返回SERVICE_UNAVAILABLE 验证错误码与文案对齐上游。
func TestHealthCheck_缺服务时返回SERVICE_UNAVAILABLE(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{}, nil, nil)
	ws := newFakeWS()
	registerHealthCheckFamily(wc, nil)
	wc.methodHandlers["health_check.get_conf"](ws, "rid", map[string]any{}, "sid")
	frame := ws.lastFrame()
	if frame["ok"] != false || frame["code"] != WsErrServiceUnavailable {
		t.Fatalf("错误码不符：%v", frame)
	}
	if frame["error"] != "health_check service not available" {
		t.Fatalf("文案不符：%v", frame["error"])
	}
}
```

- [x] **Step 3: 实现并注册 7 个方法**

`health_check.*` 复用既有 `HeartbeatConfService`（Go 已有 `GetHeartbeatConf`/`SetHeartbeatConf`，上游 `hb.get_health_check_conf()` 同源，别名接入即可）；`cron.job.meta` 在 `CronJobController` 上补 `Meta() (map[string]any, error)` 并在 controller 实现（上游 `_cron_job_meta` 返回静态元数据）；`updater.*` 在 `UpdaterService` 接口上补 conf 面（`GetConf`/`SetConf`/`ResetSource`/`Upgrade`）。

- [x] **Step 4: 删除自造与漂移名**

```bash
grep -rn '"cron.job.disable"\|"cron.job.enable"\|"cron.job.run"\|"cron.job.status"\|"updater.cancel"\|"updater.install"\|"heartbeat.get_path"' --include=*.go internal/
```
逐处删除注册与 handler，测试同步删断言。

- [x] **Step 5: 测试 + 矩阵 + 提交**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/gateway/...
go run ./cmd/rpc-matrix -upstream /home/opensource/jiuwenswarm-develop
git add -A && git commit -m "feat(web): 补齐 health_check/cron.meta/updater 7 个方法；删除 7 个自造漂移名"
```

---

**实施记录（2026-11-05 完成；updater 按用户裁定只保留 handler 回默认）：**

- `health_check.get_conf` / `set_conf`：与 `heartbeat.*` **共用同一对 handler**（上游 5839-5842 就是把两个名字注册到同一对函数）。
  随之对齐三点：① get 的不可用文案改为 `"health_check service not available"`（上游两个注册名共用同一 handler，因此
  `heartbeat.get_conf` 也是这个文案；set 仍为 `"heartbeat service not available"`，上游原文即不对称）；
  ② set 补**飞书目标校验**（`target=="feishu"` 时要求每个 app 都有 `last_chat_id`/`chat_id`，否则回
  `error=code="feishuNoTarget"`，`app_web_handlers.py:4290-4325`）；③ 写回段从 `heartbeat.*` 改为 **`health_check.*`**
  （`update_health_check_in_config`，config.py:451-463），启动读侧改为「优先 health_check、回退 heartbeat」。
  ④ 启动时执行 `config.MigrateLegacyHeartbeatProbeConfig()`（`config.py:466-493`）：只搬 `every/target/active_hours`、
  目标段 setdefault 优先、`heartbeat.jobs` 不动、搬空写空 mapping；失败只 Warn（对齐 `app_gateway.py:192-206`），随后 `cfg.Reload()`。
- `cron.job.meta`：`CronJobController` 加 `JobMetadata()`，`gateway/cron` 落 `CronJobMetadata()`
  （`runtime/cron/models.py:172-180`）：modes 22 个（21 字面量 + `team.plan.normal`/`team.plan.code`，按 Python
  `sorted()` 字典序）、`default_mode=agent`、default/default_team/max timeout = 3600/3600/259200。
- `updater.get_conf/set_conf/reset_source/upgrade`：**只保留 handler，回默认**（用户裁定）。三个配置面回
  `defaultUpdaterRuntimeConfig()`（`UpdaterService.get_runtime_config()` 的默认形态：gitcode/pypi、openJiuwen/jiuwenswarm、
  timeout 20、pypi 镜像、空 token），`upgrade` 回 `{"state":"idle"}`。连同既有 `updater.check/download/get_status` 三个
  stub，`updater.*` 全族在本仓是"能应答但不执行"——已登记 spec §10，恢复需移植 `gateway/common/updater.py`(578 行)。
- `Step 4 删除自造名`：`cron.job.disable/enable/run/status`、`updater.cancel/install`、`heartbeat.get_path` 在
  `385eb614` 已删，本批 grep 复核 0 命中。
- 矩阵：`local` 缺口 29 → 22（本批 7 个）。


---

## Task 11: B1b-4 杂项本地方法（4 个，`local`）

**Files:**
- Modify: `web/app_web_handlers.go`
- Test: `web/app_web_handlers_misc_test.go`

| 方法 | 上游 | Go 名 | 说明 |
|---|---|---|---|
| `connection.status` | `:5676` | `ConnectionStatus` | 返回 `agent_ready` + `protocol_version` |
| `commands.list` | `:5775` | `CommandsList` | 静态命令清单，本地返回 |
| `config.moa_status` | `:5637` | `configMoaStatus` | 复用 config 代理 |
| `chat.swarmflow_reply` | `:5818` | `ChatSwarmflowReply` | 空 ack（上游即 `{"accepted": true, "session_id": ...}`） |

- [x] **Step 1: 读上游**

Run: `sed -n '4010,4020p;4158,4165p;5637,5684p' /home/opensource/jiuwenswarm-develop/gateway/channel_manager/web/app_web_handlers.py`

- [x] **Step 2: 写失败测试**

```go
// TestConnectionStatus_返回就绪字段 验证 payload 与上游一致。
func TestConnectionStatus_返回就绪字段(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{}, nil, nil)
	ws := newFakeWS()
	connectionStatus(wc, nil)(ws, "rid", map[string]any{}, "sid")
	p := ws.lastFrame()["payload"].(map[string]any)
	if p["protocol_version"] != "1.0" {
		t.Fatalf("protocol_version 不符：%v", p)
	}
	if _, ok := p["agent_ready"]; !ok {
		t.Fatalf("缺 agent_ready")
	}
}

// TestChatSwarmflowReply_回accepted 验证空 ack 形状。
func TestChatSwarmflowReply_回accepted(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{}, nil, nil)
	ws := newFakeWS()
	chatSwarmflowReply(wc)(ws, "rid", map[string]any{}, "sid-1")
	p := ws.lastFrame()["payload"].(map[string]any)
	if p["accepted"] != true || p["session_id"] != "sid-1" {
		t.Fatalf("ack 形状不符：%v", p)
	}
}
```

- [x] **Step 3: 实现四个闭包 + 注册**

```go
// ConnectionStatus 返回 Gateway / AgentServer 就绪状态。
//
// Python: _connection_status (app_web_handlers.py:5676-5684)。
func connectionStatus(wc *WebChannel, agentClient *routing.AgentClient) methodHandler {
	return func(ws wsSender, reqID string, _ map[string]any, _ string) {
		ready := agentClient != nil && agentClient.ServerReady()
		wc.sendResponse(ws, reqID, true, map[string]any{
			"agent_ready":      ready,
			"protocol_version": "1.0",
		}, "", "")
	}
}

// ChatSwarmflowReply 回空 ack，由三层路由继续转发给 agent adapter。
//
// Python: _chat_swarmflow_reply (app_web_handlers.py:4158-4163)。
func chatSwarmflowReply(wc *WebChannel) methodHandler {
	return func(ws wsSender, reqID string, _ map[string]any, sessionID string) {
		wc.sendResponse(ws, reqID, true, map[string]any{
			"accepted":   true,
			"session_id": sessionID,
		}, "", "")
	}
}
```

- [x] **Step 4: 测试 + 矩阵 + 提交**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/gateway/channel_manager/web/ -v
go run ./cmd/rpc-matrix -upstream /home/opensource/jiuwenswarm-develop
git add -A && git commit -m "feat(web): 补齐 connection/commands/moa/swarmflow 4 个本地方法"
```

---

**实施记录（2026-11-05 完成）：**

- `connection.status`：`{agent_ready: agentClient != nil && ServerReady(), protocol_version: "1.0"}`
  （`app_web_handlers.py:5676-5684`）。
- `commands.list`：`command_parser` 侧补 `ListBuiltinCommands`（`slash_command.py:483-592`）——
  5 条内置命令（fork/compact/plan/goal/persist）的静态元数据表 + `description` 由
  `description_i18n["zh"]` 派生 + `work_mode` 过滤（上游 5 条的 `available_modes` 全为 null，
  故实际不过滤，但过滤逻辑保留）。
- `config.moa_status`：上游经 E2A 到 AgentServer `moa_status()`（`moa_runtime.py:106-119`），
  后者无可用 model group 时固定回 `{"configured": false}`。本仓 catalog 未移植（Task 8 跳过），
  **固定回这个默认 DTO**，面板显示 MoA 未启用、不误报可用。
- `chat.swarmflow_reply`：`{accepted: true, session_id: 连接会话}`（`:4158-4163`），转发由分发层完成。
- 矩阵：`local` 缺口 22 → 18。


---

## Task 12: B1b-5 external_cli（7 个，`local`）

**Files:**
- Modify: `web/app_web_handlers.go`
- Test: `web/app_web_handlers_external_cli_test.go`

| 方法 | 上游 |
|---|---|
| `external_cli.detect` | `:5496` → `_external_cli_detect` (`:2268`) |
| `external_cli.validate_paths` | `:5497` → `:2280` |
| `external_cli.prune_path_histories` | `:5498` → `:2304` |
| `external_cli.codex_install_status` | `:5499` → `:2327` |
| `external_cli.install_status` | `:5500` → `:2330` |
| `external_cli.install` | `:5501` → `:2342` |
| `external_cli.custom_agent.remove` | `:5502` → `:2361` |

- [x] **Step 1: 读上游**

Run: `sed -n '2268,2380p' /home/opensource/jiuwenswarm-develop/gateway/channel_manager/web/app_web_handlers.py`

- [x] **Step 2: 写失败测试**

```go
// TestExternalCLI_注册齐全 验证 7 个方法已注册。
func TestExternalCLI_注册齐全(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{}, nil, nil)
	registerExternalCLIFamily(wc, nil)
	for _, m := range []string{
		"external_cli.detect", "external_cli.validate_paths", "external_cli.prune_path_histories",
		"external_cli.codex_install_status", "external_cli.install_status",
		"external_cli.install", "external_cli.custom_agent.remove",
	} {
		if _, ok := wc.methodHandlers[m]; !ok {
			t.Errorf("方法未注册: %s", m)
		}
	}
}
```

- [x] **Step 3: 7 个 handler 按上游 1:1 移植 + 注册**

安装器与状态轮询是 gateway 本地能力（上游注释明确），依赖 `exec` 与本地文件；测试只做参数校验分支与注册，真安装不测。

- [x] **Step 4: 测试 + 矩阵 + 提交**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/gateway/channel_manager/web/ -run TestExternalCLI -v
go run ./cmd/rpc-matrix -upstream /home/opensource/jiuwenswarm-develop
git add -A && git commit -m "feat(web): 补齐 external_cli 7 个方法"
```

---

**实施记录（2026-11-05 完成；安装器按"保留 handler 回默认"口径）：**

- 真实现 3 个（不依赖安装子系统）：
  - `external_cli.detect`：内置种类 `claude`/`codex`；显式路径 → 必须存在且非目录（`directory`）、
    Windows 脚本后缀 `.bat/.cmd/.ps1` → `windows_script`；无路径 → PATH 搜索；再跑 `--version`
    用 `(\d+\.\d+\.\d+)` 取版本，`ok` / `warning`（解析不出）/ `missing` / `unavailable` 四态对齐上游。
  - `external_cli.validate_paths`：单次最多 50 条，三值 `true/false/null`（`null`=无法判定，
    调用方只应据 `false` 清历史），空串跳过。
  - `external_cli.prune_path_histories`：按 `modes.team.jiuwen_team.external_cli_path_histories.<agent>`
    做「既有历史 ∩ removePaths」删除、去重保序、无变化不写盘、删空写空 mapping。
- 回默认 4 个：`codex_install_status` / `install_status` / `install` / `custom_agent.remove`
  —— 安装器子系统 `gateway/common/external_cli_runtime.py`（1,121 行）+ `external_providers.py`（70 行）
  未移植；`install_status` 保留参数校验（未知种类 BAD_REQUEST），其余回 `{installing:false, installed:false,
  state:"idle", ...}` 默认快照。**偏差已在 spec §10 登记**（面板可渲染、不会真装）。
- `reference_version`：上游读 `claude_agent_sdk._cli_version` / `openai-codex` 包元数据，本仓无这些依赖 →
  改用**常量表**（`claude=0.2.115`、`codex=0.144.4`，取上游 `uv.lock` 锁定版本），并按上游语义实现
  `parseVersionTuple` + `compareVersionTuple`：探测版本 < 参考版本 → `status=warning` +
  `"version X may be incompatible with expected version Y"`。**上游升版需同步该表**。
- 仍保留的偏差：`use_builtin` 的托管 CLI 解析依赖 managed runtime（未移植）→ 退回 PATH 搜索。
- 矩阵：`local` 缺口 18 → 11。


---

## Task 13: B1b-6 `path.select_file`（1 个，`local`）

**Files:**
- Modify: `web/app_web_handlers.go`
- Test: `web/app_web_handlers_path_test.go`

> `path.select_files` 与 `path.select_directory` 在上游是薄代理（`PATH_SELECT_*` → AgentServer），属下游缺口，**不在本期**。

- [x] **Step %d: 读上游**

Run: `sed -n '3948,4010p' /home/opensource/jiuwenswarm-develop/gateway/channel_manager/web/app_web_handlers.py`
要点：先校验 `initial_dir`/`initial_path` 为字符串（非法回 `BAD_REQUEST`），再走本地文件选择。

- [x] **Step %d: 写失败测试**

```go
// TestPathSelectFile_非法参数回BAD_REQUEST 验证参数校验分支。
func TestPathSelectFile_非法参数回BAD_REQUEST(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{}, nil, nil)
	ws := newFakeWS()
	wc.registerMethod("path.select_file", PathSelectFile(wc), false)
	wc.methodHandlers["path.select_file"](ws, "rid", map[string]any{"initial_dir": 123}, "sid")
	if ws.lastFrame()["code"] != WsErrBadRequest {
		t.Fatalf("预期 BAD_REQUEST，实际 %v", ws.lastFrame())
	}
}
```

- [x] **Step %d: 实现 + 注册**

- [x] **Step %d: 测试 + 矩阵 + 提交**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/gateway/channel_manager/web/ -run TestPathSelectFile -v
go run ./cmd/rpc-matrix -upstream /home/opensource/jiuwenswarm-develop
git add -A && git commit -m "feat(web): 补齐 path.select_file（本地选择分支）"
```

---

**实施记录（2026-11-05 完成）：**

- 参数校验三条与上游一致：`initial_dir`/`initial_path`/`title` 非字符串 → `BAD_REQUEST`（各自专属文案）；
  `params` 非对象按空对象处理（上游 `if not isinstance(params, dict): params = {}`）。
- 起始目录：`initial_path` 优先取其父目录（`Path(initial_path).parent`），否则用 `initial_dir`（strip）。
- 原生选择器：Python 走 `directory_picker.select_file_native`（tkinter/zenity）；Go 侧按平台尝试
  **osascript（macOS）/ zenity → kdialog（Linux）**，都不可用 → `UNSUPPORTED`（与上游 headless 下
  picker 抛异常同码），Windows 构建直接 `UNSUPPORTED`（无原生实现，已登记 spec §10）。
- 取消（选择器无输出）→ `{path: null, cancelled: true}`；选中 → `{path: 绝对路径, cancelled: false}`。
- 测试注入点：`selectFileNativeFn`（包级变量，生产值即 `selectFileNative`），避免测试依赖真实对话框。


---

## Task 14: B1b-7 session 薄代理（2 个，`agent:ok`）

**Files:**
- Modify: `web/app_web_handlers.go`
- Test: `web/app_web_handlers_session_proxy_test.go`

| 方法 | 上游 | 代理 req method | 本仓下游 |
|---|---|---|---|
| `session.rename` | `:5693` | `SESSION_RENAME` | `ReqMethodSessionRename` 已有分支 |
| `session.plan_status` | `:5692` | `SESSION_PLAN_STATUS` | `ReqMethodSessionPlanStatus` 已有分支 |

- [x] **Step 1: 读上游**

Run: `sed -n '3060,3140p' /home/opensource/jiuwenswarm-develop/gateway/channel_manager/web/app_web_handlers.py`
要点：两个都是 `proxy_unary_request(..., req_method=ReqMethod.SESSION_*)` 薄代理，携带 `session_id` 与 `params`。

- [x] **Step 2: 写失败测试**

```go
// TestSessionProxy_注册齐全 验证两个代理方法已注册。
func TestSessionProxy_注册齐全(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{}, nil, nil)
	registerSessionProxyFamily(wc, nil)
	for _, m := range []string{"session.rename", "session.plan_status"} {
		if _, ok := wc.methodHandlers[m]; !ok {
			t.Errorf("方法未注册: %s", m)
		}
	}
}

// TestSessionProxy_无客户端时回SERVICE_UNAVAILABLE 验证代理缺客户端的分支。
func TestSessionProxy_无客户端时回SERVICE_UNAVAILABLE(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{}, nil, nil)
	ws := newFakeWS()
	registerSessionProxyFamily(wc, nil)
	wc.methodHandlers["session.rename"](ws, "rid", map[string]any{"title": "x"}, "sid")
	if ws.lastFrame()["ok"] == true {
		t.Fatalf("无 AgentServer 客户端时应失败，实际 %v", ws.lastFrame())
	}
}
```

- [x] **Step 3: 实现薄代理**

```go
// sessionProxyClient 会话类薄代理所需的单请求转发面。
type sessionProxyClient interface {
	// Unary 转发一次单请求并返回回包
	Unary(reqMethod string, params map[string]any, sessionID string) (ok bool, payload map[string]any, errMsg, code string)
}

// registerSessionProxyFamily 注册 session 族薄代理方法。
//
// Python: _session_rename / _session_plan_status（app_web_handlers.py:3060-3140），
// 都是 proxy_unary_request 薄代理；本仓 AgentServer 已有对应分支，故只需转发。
func registerSessionProxyFamily(wc *WebChannel, client sessionProxyClient) {
	wc.registerMethod("session.rename", SessionProxy(wc, client, "session.rename", "SESSION_RENAME"), false)
	wc.registerMethod("session.plan_status", SessionProxy(wc, client, "session.plan_status", "SESSION_PLAN_STATUS"), false)
}

// SessionProxy 构造单个薄代理 handler。
func SessionProxy(wc *WebChannel, client sessionProxyClient, label, reqMethod string) methodHandler {
	return func(ws wsSender, reqID string, params map[string]any, sessionID string) {
		if client == nil {
			logger.Warn(logComponent).Str("label", label).Str("req_id", reqID).
				Msg("agent client not available")
			wc.sendResponse(ws, reqID, false, nil, "agent client not available", WsErrServiceUnavailable)
			return
		}
		ok, payload, errMsg, code := client.Unary(reqMethod, params, sessionID)
		if !ok {
			wc.sendResponse(ws, reqID, false, payload, errMsg, code)
			return
		}
		wc.sendResponse(ws, reqID, true, payload, "", "")
	}
}
```

- [x] **Step 4: 测试 + 矩阵 + 提交**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/gateway/channel_manager/web/ -run TestSessionProxy -v
go run ./cmd/rpc-matrix -upstream /home/opensource/jiuwenswarm-develop
git add -A && git commit -m "feat(web): 补齐 session.rename/plan_status 两个薄代理方法"
```

---

**实施记录（2026-11-05 完成）：**

- `session.rename` / `session.plan_status` 统一由 `sessionProxyForward(wc, agentClient, method)` 承担：
  构造 E2A 信封（`request_id`=帧 ID、`channel_id`=`wc.ChannelID()`、`session_id`、`req_method`、`params`）
  后 `ac.SendRequest`（对齐 `proxy_unary_request`，e2a_proxy.py:99-175）。
- 客户端不可用（nil 或 `ServerReady()==false`）→ `SERVICE_UNAVAILABLE` + 上游原文
  `"AgentServer is unavailable"`；转发异常 → `INTERNAL_ERROR`；失败回包的 `code` 缺失时用
  `BAD_REQUEST`（上游 `default_error_code`）。
- 测试：注册齐全 + 无客户端分支；E2A 成功/失败分支需真实 AgentServer 客户端（进程边界），
  已在 `.coverage-exempt` 登记行级豁免（同 `permissions.*` E2A 分支口径）。
- **与本文 Step 3 草稿的偏差（2026-11-05 裁定保留）**：草稿定义了 `sessionProxyClient` 接口，
  实际直接用具体类型 `*routing.AgentClient`（web 包本就 import routing，`routing` 不 import web，
  **无循环依赖**；上游 `proxy_unary_request` 也是直接吃 client 对象，没有这层接口）。代价是
  E2A 三分支不可注入假客户端，靠行级豁免；换取的是不多一层 Python 不存在的抽象（规则 9）。
- 矩阵：`local` 缺口 10 → 9，`agent:ok` 缺口 7 → 5（两个会话代理接上）。


---

## Task 15: 收尾 —— 承诺范围归零、门禁转强制、计划登记

**Files:**
- Modify: `.github/workflows/ci.yml`（`-verify` 转强制）
- Modify: `IMPLEMENTATION_PLAN.md`（27-A、27.x 状态与 ⤵️/⤴️）
- Modify: `docs/superpowers/specs/2026-11-05-web-ws-rpc-alignment-design.md`（偏差回收记录）

- [x] **Step 1: 确认承诺范围无 ❌**

Run: `go run ./cmd/rpc-matrix -verify -upstream /home/opensource/jiuwenswarm-develop`
Expected: 退出码 0。说明：`-verify` 只看 `local` + `agent:ok`；下游缺口仍会打印在缺口清单里，不判失败。全部清零后再改用 `-verify-all`。

- [x] **Step 2: CI 转强制**

把 Task 2 里的步骤改为（保持上游不可达时跳过）：

```yaml
      - name: 校验 Web RPC 契约矩阵
        run: |
          if git clone --depth 1 https://gitcode.com/openJiuwen/jiuwenswarm.git /tmp/upstream; then
            go run ./cmd/rpc-matrix -verify -upstream /tmp/upstream
            git diff --exit-code -- docs/sync/web-rpc-matrix.md docs/sync/web-rpc-downstream-gaps.md
          else
            echo "上游不可达，跳过矩阵校验"
          fi
```

- [x] **Step 3: 计划登记**

`IMPLEMENTATION_PLAN.md`：
- 27-A 的 B0、B1 行 → ✅，写明"本层承诺 49/49 落地，下游缺口 59 条见 `docs/sync/web-rpc-downstream-gaps.md`"
- 27.1-27.10 中受本计划影响的行补记方法数
- 新增偏差/回填行（按缺口清单分组）：`project.*` 20 → 27.x；`topic_starters.*`/`prompt_suggestion.*` 10 → recommendation；`session.get_metadata/pin/continue_queued` 3 → 27.6；`path.select_files/select_directory`/`media.*`/`document.*`/`file.upload_chunk` 7 → 27.8；`harness.import/export` 2 → AgentServer；`heartbeat.job.*` 10 → 24.x/11.11；`openai_account.*` 6 → 23.6；`task.asr.transcribe` 1 → 23.9
- 24.13 行状态由 🔄 → ✅（代码已在 `36f85699` 落地，本步仅补记）

- [x] **Step 4: 全量验收**

```bash
export GOPROXY=https://goproxy.cn,direct
CGO_ENABLED=1 go build -tags sqlite_fts5 ./...
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/...
CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/web/...
go run ./cmd/rpc-matrix -verify -upstream /home/opensource/jiuwenswarm-develop
go run ./cmd/integration-audit -verify
```
Expected: 全部通过。

- [x] **Step 5: 提交并观察 CI**

```bash
git add -A
git commit -m "chore(web): 本层承诺 49/49 落地，-verify 转 CI 强制；计划登记 27-A/24.13 与 59 条下游缺口"
git push
gh run list --limit 3
```
Expected: Build / Lint / Unit Test / Integration Test 四 job SUCCESS。

---

## 附：后续独立计划（不在本计划内）

- **59 条下游缺口**：见 `docs/sync/web-rpc-downstream-gaps.md`（const-only 2 / agent:missing 40 / dep 17）。每组需在对应章节补 req method 常量 + AgentServer 分支 + 数据层，属 27.x / 23.x / 24.x 范围。
- **入站主流程对齐**：24.13 剩余两步（`_resolve_external_channel_session`、非流式 team 拒绝）+ 审查 M-07 六步（`prepare_im_session_input`、`session_input` 校验归一、`session_busy`/`steer`、`godview` 注册）。改 `forward_loop.go`/`message_handler`，与 WebChannel RPC 表面无交集，须单独 spec。
- **27-A B2~B8 整域模块**：HTTP 路由/鉴权、文件与目录面板、git diff watcher、分享导出、轨迹诊断、MCP/沙箱 OAuth（28-33 人日）。

**实施记录（2026-11-05 完成，两处口径已按实际裁定调整）：**

- **Step 1（承诺范围归零）**：实际口径改为 **「除 Task 8 的 10 个之外归零」**。矩阵现状 196 方法、漂移 0：
  `local` 缺 10（全部是 Task 8 那批 `models.*`/`model_groups.*`/`vendors.*`/`session.selection.set`），
  `agent:ok` 缺 5（AgentServer 侧补齐），其余 82 条为下游缺口清单。因此 `-verify` 仍会红在 Task 8 那 10 个上，
  **本期不启用 `-verify` 门禁**。
- **Step 2（CI 转强制）**：**不执行**。用户 2026-11-05 裁定删除 CI 的「校验 Web RPC 契约矩阵」步骤
  （CI 克隆的上游 HEAD 已无 `gateway/`，只会拿降级矩阵误报过期），矩阵改为本地命令。
- **Step 3（计划登记）**：`IMPLEMENTATION_PLAN.md` 27-A 的 B0/B1 已改 ✅，并补一段收口口径说明
  （矩阵方法数、剩余缺口归属、`updater.*`/`external_cli.*` 回默认的登记位置）。B2-B11 未动。
- **Step 4（全量验收）**：`go build`、`go test ./internal/swarm/...`、矩阵重算、`integration-audit -verify`
  均已跑通；集成测试的 `TestEvolutionConsumptionE2E` 与 `TestCliRouteFullChainSuite` 两处红灯归属见
  本文件开头的「CI 红灯归属」段（非本计划文件）。

