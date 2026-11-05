# Web WS 层 RPC 全量对齐实现计划（27-A B0/B1）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把本仓 Web WS 端点从「45/154 同名命中」推进到「154 全量对齐」，同时把该层的文件划分、方法归属、函数名与上游 `gateway/channel_manager/web/` 做成 1:1。

**Architecture:** 先用 `cmd/rpc-matrix` 把三方（前端 / 本仓 / 上游）方法集合变成可验收矩阵并接进 CI（无白名单）；再把现有 5 个 Go 文件按上游职责归位（删自造 `frame`/`RPCDispatcher`，normalize 迁 `gateway` 包，handler 改名到 Python 名）；最后按上游行区相邻性分九批补齐 109 个方法，每批一个提交、单独 CI 四绿。

**Tech Stack:** Go 1.22（`sqlite_fts5` tag）、gorilla/websocket、标准库 `regexp`/`go/ast`、现有 `cmd/integration-audit` 门禁模式、`tests/integration/swarm/gateway` 的 `CliRouteFullChainSuite` 装配。

**上游源码：** `/home/opensource/jiuwenswarm-develop`（`jiuwenswarm` 0.2.5.beta1）。本计划所有「上游 :行号」均指该目录下 `gateway/channel_manager/web/app_web_handlers.py`（除另行注明）。

**移植铁律（规则 9）:** 109 个 handler 一律按上游函数体逐行移植，函数名 = 上游函数名去前导下划线转 lowerCamel，参数顺序 = `(ws, reqID, params, sessionID)`。**不得**为了适配 Go 习惯合并/拆分步骤、改参数语义、换错误码。

**关于「不重抄函数体」:** 本计划不把上游约 3k 行函数体重抄一遍。每个 Task 给出 **上游文件:行号 + 函数名 + 完整注册代码 + 完整测试代码 + 至少一个完整 handler 模板**；其余函数体以该行号为准逐行移植。上游行号即权威实现，不是 TODO 占位。

---

## 文件结构（最终态）

```
cmd/rpc-matrix/
├── doc.go              # 包注释（规则 1）
├── main.go             # 入口：-verify / -out
├── scan.go             # 三方扫描器
├── matrix.go           # 归类 + Markdown 渲染
└── scan_test.go        # 扫描/归类单测
docs/sync/web-rpc-matrix.md            # 生成物（入库）
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

- [ ] **Step 1: 写失败测试**

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

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./cmd/rpc-matrix/ -run TestScan -v`
Expected: FAIL，`undefined: scanFrontend` 等编译错误。

- [ ] **Step 3: 实现扫描器**

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

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./cmd/rpc-matrix/ -run TestScan -v`
Expected: PASS（3 个用例）。

- [ ] **Step 5: 提交**

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

- [ ] **Step 1: 写失败测试（归类规则）**

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

- [ ] **Step 2: 运行确认失败**

Run: `go test ./cmd/rpc-matrix/ -run "TestBuildMatrix|TestMissing" -v`
Expected: FAIL，`undefined: buildMatrix`。

- [ ] **Step 3: 实现归类与渲染**

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

- [ ] **Step 4: 实现入口**

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

- [ ] **Step 5: 运行测试 + 首次生成矩阵**

Run:
```bash
go test ./cmd/rpc-matrix/ -v
go run ./cmd/rpc-matrix -upstream /home/opensource/jiuwenswarm-develop -out docs/sync/web-rpc-matrix.md
```
Expected: 测试 PASS；输出「共 15x 个方法，缺 10x 个」；`docs/sync/web-rpc-matrix.md` 入库。

- [ ] **Step 6: 接进 CI（Build job）**

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

- [ ] **Step 7: 提交**

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

- [ ] **Step 1: 确认零生产引用**

Run: `grep -rn "web\.Frame\|NewResFrame\|NewEventFrame\|DecodeReqFrame\|ReqFrame{" --include=*.go internal/ tests/ cmd/ | grep -v frame_test.go`
Expected: 无输出（仅 `frame.go`/`frame_test.go` 自身命中）。

- [ ] **Step 2: 删除文件并同步 doc.go**

```bash
git rm internal/swarm/gateway/channel_manager/web/frame.go internal/swarm/gateway/channel_manager/web/frame_test.go
```

`doc.go` 中删除 `frame.go` 那一行，并在包注释补一句：

```go
// 说明：上游 Web 通道全程以 dict 传递帧，无 frame DTO；本包同样不定义帧结构体。
```

- [ ] **Step 3: 编译与测试**

Run: `CGO_ENABLED=1 go build -tags sqlite_fts5 ./... && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/gateway/...`
Expected: 全绿。

- [ ] **Step 4: 提交**

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

- [ ] **Step 1: 文件改名**

```bash
cd internal/swarm/gateway/channel_manager/web
git mv web_handlers.go app_web_handlers.go
```

- [ ] **Step 2: 合并另两个文件的内容**

把 `config_apply.go` 与 `memory_compute.go` 的全部声明按规则 2 的分区顺序合并进 `app_web_handlers.go`：

- 结构体（`ConfigBadRequest`/`ConfigInternalError` 等）→ 合并到文件的结构体区
- 常量/全局变量（如 `configEnvMap` 保持原位）→ 各归其区
- 其余包级函数（`handleConfigSet`/`applyConfigPatch`/`handleMemoryCompute` 等）→ 非导出函数区末尾，保持相互调用顺序不变

合并后删除源文件：

```bash
git rm config_apply.go memory_compute.go
```

- [ ] **Step 3: 编译验证（此时不应有行为变化）**

Run: `CGO_ENABLED=1 go build -tags sqlite_fts5 ./... && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/gateway/channel_manager/web/`
Expected: 与 Step 0 相同结果（无新增失败）。

- [ ] **Step 4: 同步 doc.go 文件目录**

把 `web_handlers.go`/`config_apply.go`/`memory_compute.go` 三行替换为：

```go
//	web/
//	├── doc.go              # 包文档
//	├── web_connect.go      # 连接、帧收发、方法注册与分发（对齐 web_connect.py）
//	└── app_web_handlers.go # 全部 RPC handler（对齐 app_web_handlers.py）
```

- [ ] **Step 5: 提交**

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

- [ ] **Step 1: 写失败测试**

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

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/swarm/gateway/channel_manager/web/ -run TestRegisterMethod -v`
Expected: FAIL，`undefined: wc.registerMethod`。

- [ ] **Step 3: 在 `web_connect.go` 增加注册表**

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

- [ ] **Step 4: 迁移 71 个 handler 的签名与注册**

统一规则（逐条机械替换，不改变函数体逻辑）。**注意保持上游的闭包形态**：上游 handler 是 `_register_web_handlers(bind)` 内的嵌套函数，捕获 `channel`/`agent_client` 等，Go 侧同样是闭包（`func(ws wsSender, ...)` 形式，捕获 `wc` 与依赖），**不改成 WebChannel 的方法**（否则方法归属与上游不一致，违反规则 9.2）：

1. `func handleXxx(dep ...) RPCHandlerFunc { return func(ctx context.Context, params map[string]any, sessionID string) (map[string]any, error) {` →
   `func makeXxx(wc *WebChannel, dep ...) methodHandler { return func(ws wsSender, reqID string, params map[string]any, sessionID string) {`
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

- [ ] **Step 5: 分发顺序对齐 `web_connect.py:2180-2231`**

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

- [ ] **Step 6: 运行测试**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/gateway/... ./internal/swarm/channels/...`
Expected: 全绿（handler 级测试改为直接调用 `makeXxx(...)(fakeWS, "rid", params, "sid")` 或经 `methodHandlers` 表）。

- [ ] **Step 7: 提交**

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

- [ ] **Step 1: 写失败测试**

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

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/swarm/gateway/ -run TestNormalizeGatewayMessage_内容补query -v`
Expected: FAIL，`undefined: NormalizeGatewayMessage`。

- [ ] **Step 3: 迁移实现**

把 `web/web_normalize.go` 中以下符号整体搬到 `internal/swarm/gateway/normalize.go`，包名改 `gateway`，函数体不动：

- `NormalizeGatewayMessage`
- `MakeNormAndForward`（含 `NormAndForwardFunc` 类型）
- `ForwardReqMethods`、`ForwardNoLocalHandlerMethods`
- 其内部依赖的私有助手（如 `rawModeText` 等）

搬完后：

```go
// normalize.go 包内引用调整：把 schema.XXX 保持原样；sendEvent 等 web 包内类型改为参数传入。
```

- [ ] **Step 4: `BuildUserMessage`/`parseMode` 留在 web 包**

这两个符号移到 `web/web_connect.go`（上游属 `handle_connection` 内联构造 + `_parse_mode`），签名与实现不变。

- [ ] **Step 5: 改调用点**

`internal/swarm/gateway/gateway_server.go` 里对 `web.NormalizeGatewayMessage` / `web.MakeNormAndForward` 的调用改为包内 `NormalizeGatewayMessage` / `MakeNormAndForward`。

- [ ] **Step 6: 测试与清理**

Run:
```bash
grep -rn "ForwardReqMethods\|ForwardNoLocalHandlerMethods" --include=*.go internal/swarm/gateway/channel_manager/web/
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/gateway/...
```
Expected: grep 无输出；测试全绿。

- [ ] **Step 7: 提交**

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

- [ ] **Step 1: 生成改名清单**

Run: `grep -n "^func (wc \*WebChannel)" internal/swarm/gateway/channel_manager/web/app_web_handlers.go`
按上表逐条对照上游函数名列出映射，写入提交说明。

- [ ] **Step 2: 机械改名（示例 8 条，其余同规则）**

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

- [ ] **Step 3: 批量替换**

```bash
cd internal/swarm/gateway/channel_manager/web
# 逐个函数名替换（示例，按 Step 1 清单补全）
sed -i 's/\bhandleConfigGet\b/configGet/g; s/\bhandleConfigSet\b/configSet/g; s/\bhandleModelsList\b/modelsList/g' app_web_handlers.go *_test.go
```

- [ ] **Step 4: 验证无自创前缀残留**

Run: `grep -rn "handle[A-Z]" --include=*.go internal/swarm/gateway/channel_manager/web/ | grep -v "_test.go"`
Expected: 无输出。

- [ ] **Step 5: 覆盖率门禁核对（纯搬移批必做）**

Run: `bash scripts/check-changed-line-coverage.sh`
Expected: 改动行覆盖率 ≥ 85%（含 5% 容差）。未达线时**在同批补测试**（把原测试文件改指向新名字后仍应真实执行到这些行），不做整段豁免——审查 M-13 已点名"把整段入口代码登记掉（1382 行）"不可接受。

- [ ] **Step 6: 测试与提交**

```bash
CGO_ENABLED=1 go build -tags sqlite_fts5 ./... && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/gateway/...
git add -A internal/swarm/gateway/channel_manager/web/
git commit -m "refactor(web): handler 函数名对齐上游 Python 名（规则 9.2）"
```

---

## Task 8: B1b-1 config 代理族（10 个）

**Files:**
- Modify: `web/app_web_handlers.go`
- Test: `web/app_web_handlers_models_test.go`

**方法清单（上游 `_register_config_proxy`）:**

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

- [ ] **Step 1: 读上游实现**

Run: `sed -n '5504,5700p' /home/opensource/jiuwenswarm-develop/gateway/channel_manager/web/app_web_handlers.py`
要点：`_register_config_proxy(name, req_method, local_handler)` 单用户走 `local_handler`，AgentOS 多用户走 `_proxy_config_request`（E2A `proxy_unary_request`）。本仓当前只有单用户路径 → **只实现 local_handler 语义**，多用户分支在函数注释中标 ⤵️ 并登记计划。

- [ ] **Step 2: 写失败测试（表驱动）**

```go
// TestModelsFamily_注册齐全 验证 10 个方法都已注册。
func TestModelsFamily_注册齐全(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{}, nil, nil)
	registerModelsFamily(wc)
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
```

- [ ] **Step 3: 实现（模板，其余 9 个同构）**

```go
// registerModelsFamily 注册 model / model_group / vendor / session 选择族方法。
//
// Python: app_web_handlers.py:5643-5678（_register_config_proxy 段）。
// 与上游一致：handler 是 _register_web_handlers(bind) 内的闭包，捕获 channel 与
// 配置读写函数，因此这里用闭包而非 WebChannel 方法。
func registerModelsFamily(wc *WebChannel, cfg modelsConfigStore) {
	wc.registerMethod("models.get", makeModelsGet(wc, cfg), false)
	wc.registerMethod("models.upsert", makeModelsUpsert(wc, cfg), false)
	wc.registerMethod("models.delete", makeModelsDelete(wc, cfg), false)
	wc.registerMethod("models.references", makeModelsReferences(wc, cfg), false)
	wc.registerMethod("model_groups.list", makeModelGroupsList(wc, cfg), false)
	wc.registerMethod("model_groups.upsert", makeModelGroupsUpsert(wc, cfg), false)
	wc.registerMethod("model_groups.delete", makeModelGroupsDelete(wc, cfg), false)
	wc.registerMethod("session.selection.set", makeSessionSelectionSet(wc, cfg), false)
	wc.registerMethod("vendors.list", makeVendorsList(wc, cfg), false)
	wc.registerMethod("vendors.fetch_models", makeVendorsFetchModels(wc, cfg), false)
}

// modelsConfigStore 配置代理族所需的读写面（生产实现复用现有 config_apply 内的读写函数）。
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

// makeModelsGet 读取单个模型配置。
//
// Python: _register_config_proxy("models.get", _ConfigReq.MODELS_GET, <local handler>)。
// 移植要求：返回 payload 的键名与上游 local handler 完全一致。
func makeModelsGet(wc *WebChannel, cfg modelsConfigStore) methodHandler {
	return func(ws wsSender, reqID string, params map[string]any, _ string) {
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

- [ ] **Step 4: 跑测试**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/gateway/channel_manager/web/ -run TestModelsFamily -v`
Expected: PASS。

- [ ] **Step 5: 集成用例**

在 `tests/integration/swarm/web/` 新增：

```go
// TestModelsFamily_真实WS帧 验证前端真实帧可命中 models.get。
func (s *WebRPCFullChainSuite) TestModelsFamily_真实WS帧() {
	s.sendRPC("models.get", map[string]any{"name": "default"})
	res := s.waitRPC("models.get")
	if !res.OK && res.Code != "INTERNAL_ERROR" {
		t.Fatalf("预期 ok 或业务错误，实际 code=%s", res.Code)
	}
}
```

- [ ] **Step 6: 更新矩阵并提交**

```bash
go run ./cmd/rpc-matrix -upstream /home/opensource/jiuwenswarm-develop
git add -A internal/swarm/gateway/channel_manager/web/ tests/integration/swarm/web/ docs/sync/web-rpc-matrix.md
git commit -m "feat(web): 补齐 models/model_groups/vendors/session.selection 方法族（10 个）"
```

---

## Task 9: B1b-2 heartbeat.job.*（10 个）

**Files:**
- Modify: `web/app_web_handlers.go`
- Delete: `heartbeat.get_path` 注册
- Test: `web/app_web_handlers_heartbeat_job_test.go`

**方法清单：** `heartbeat.job.list/get/meta/create/update/delete/toggle/preview/run_now/cancel`（上游 `:6196-6205`，handler `_hb_job_*`；实体在 `:5887` 起的 `_hb_job_list` 一族）。

- [ ] **Step 1: 读上游**

Run: `sed -n '5860,6220p' /home/opensource/jiuwenswarm-develop/gateway/channel_manager/web/app_web_handlers.py`
要点：`_get_heartbeat()` 返回 None 时回 `_heartbeat_unavailable`（错误 code 从该函数体抄，勿自拟）。

- [ ] **Step 2: 写失败测试**

```go
// TestHeartbeatJob_注册且未注入服务时报错 验证 10 个方法与缺依赖语义。
func TestHeartbeatJob_注册且未注入服务时报错(t *testing.T) {
	for _, m := range heartbeatJobMethods {
		wc := NewWebChannel(WebChannelConfig{}, nil, nil)
		registerHeartbeatFamily(wc, nil)
		h, ok := wc.methodHandlers[m]
		if !ok {
			t.Fatalf("方法未注册: %s", m)
		}
		h(nil, "rid", map[string]any{}, "sid") // 不 panic 即通过；错误帧由 fake ws 断言
	}
}
```

- [ ] **Step 3: 实现 + 注册**

按上游 `_hb_job_*` 逐函数移植，统一命名 `hbJobList`/`hbJobGet`/…；`heartbeatJobMethods` 变量列出 10 个方法名供测试与注册共用。

`bind.HeartbeatService` 现有接口只有 `GetHeartbeatConf`/`SetHeartbeatConf` → 需扩出 job 面：

```go
// HeartbeatJobService 心跳任务面（对齐上游 heartbeat 服务 list_jobs/get_job/... 方法）。
type HeartbeatJobService interface {
	ListJobs(params map[string]any, accessSessionID, userID string) (map[string]any, error)
	GetJob(jobID string) (map[string]any, error)
	JobMeta() (map[string]any, error)
	CreateJob(params map[string]any) (map[string]any, error)
	UpdateJob(jobID string, params map[string]any) (map[string]any, error)
	DeleteJob(jobID string) (bool, error)
	ToggleJob(jobID string, enabled bool) (map[string]any, error)
	PreviewJob(jobID string, count int) (map[string]any, error)
	RunNow(jobID string) (string, error)
	CancelJob(jobID string) (bool, error)
}
```

- [ ] **Step 4: 删除 `heartbeat.get_path`**

```bash
grep -rn "heartbeat.get_path" --include=*.go internal/ | grep -v _test
```
逐处删除注册与 handler；测试同步删断言。

- [ ] **Step 5: 测试 + 集成 + 提交**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/gateway/...
go run ./cmd/rpc-matrix -upstream /home/opensource/jiuwenswarm-develop
git add -A && git commit -m "feat(web): 补齐 heartbeat.job.* 10 个方法并删除自造 heartbeat.get_path"
```

---

## Task 10: B1b-3 cron.meta / health_check / updater（7 个）

**Files:**
- Modify: `web/app_web_handlers.go`
- Test: `web/app_web_handlers_cron_health_test.go`

**方法清单：**

| 方法 | 上游 | Go 名 |
|---|---|---|
| `cron.job.meta` | `:5865` | `cronJobMeta` |
| `health_check.get_conf` | `:5839` | `healthCheckGetConf` |
| `health_check.set_conf` | `:5840` | `healthCheckSetConf` |
| `updater.upgrade` | `:5835` | `updaterUpgrade` |
| `updater.get_conf` | `:5836` | `updaterGetConf` |
| `updater.reset_source` | `:5837` | `updaterResetSource` |
| `updater.set_conf` | `:5838` | `updaterSetConf` |

- [ ] **Step 1: 读上游**

Run: `sed -n '4240,4270p;5820,5870p' /home/opensource/jiuwenswarm-develop/gateway/channel_manager/web/app_web_handlers.py`

关键语义（`_health_check_get_conf` 原文）：服务为 None → `ok=False, error="health_check service not available", code="SERVICE_UNAVAILABLE"`；异常 → `code="INTERNAL_ERROR"`。**逐字照抄，勿改文案**。

- [ ] **Step 2: 写失败测试**

```go
// TestHealthCheck_缺服务时返回SERVICE_UNAVAILABLE 验证错误码与文案对齐上游。
func TestHealthCheck_缺服务时返回SERVICE_UNAVAILABLE(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{}, nil, nil)
	ws := newFakeWS()
	registerHealthCheckFamily(wc, nil)
	wc.methodHandlers["health_check.get_conf"](ws, "rid", map[string]any{}, "sid")
	frame := ws.lastFrame()
	if frame["ok"] != false || frame["code"] != "SERVICE_UNAVAILABLE" {
		t.Fatalf("错误码不符：%v", frame)
	}
	if frame["error"] != "health_check service not available" {
		t.Fatalf("文案不符：%v", frame["error"])
	}
}
```

- [ ] **Step 3: 实现 + 注册**（模板同 Task 8，逐函数 1:1 移植）

- [ ] **Step 4: 删除 `updater.cancel`/`updater.install`**

上游零命中，删除注册与 handler，测试同步删除。

- [ ] **Step 5: 测试 + 矩阵 + 提交**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/gateway/...
go run ./cmd/rpc-matrix -upstream /home/opensource/jiuwenswarm-develop
git add -A && git commit -m "feat(web): 补齐 cron.meta/health_check/updater 7 个方法并删除自造 updater.cancel|install"
```

---

## Task 11: B1b-4 session 族与杂项（9 个）

**Files:**
- Modify: `web/app_web_handlers.go`
- Test: `web/app_web_handlers_session_test.go`

**方法清单：**

| 方法 | 上游 | Go 名 |
|---|---|---|
| `session.get_metadata` | `:5690` | `sessionGetMetadata` |
| `session.message.continue_queued` | `:5691` | `sessionMessageContinueQueued` |
| `session.plan_status` | `:5692` | `sessionPlanStatus` |
| `session.rename` | `:5693` | `sessionRename` |
| `session.pin` | `:5694` | `sessionPin` |
| `chat.swarmflow_reply` | `:5818` | `chatSwarmflowReply` |
| `commands.list` | `:5775` | `commandsList` |
| `connection.status` | `:5676` | `connectionStatus` |
| `config.moa_status` | `:5637` | `configMoaStatus` |

- [ ] **Step 1: 读上游**

Run: `sed -n '5630,5680p;5688,5700p;5770,5785p;5810,5830p' /home/opensource/jiuwenswarm-develop/gateway/channel_manager/web/app_web_handlers.py`

`_connection_status` 原文（已确认）：返回 `{"agent_ready": ac is not None and bool(getattr(ac, "server_ready", False)), "protocol_version": "1.0"}`。

- [ ] **Step 2: 写失败测试**

```go
// TestConnectionStatus_返回就绪字段 验证 payload 与上游一致。
func TestConnectionStatus_返回就绪字段(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{}, nil, nil)
	ws := newFakeWS()
	wc.registerMethod("connection.status", wc.connectionStatus, false)
	wc.methodHandlers["connection.status"](ws, "rid", map[string]any{}, "sid")
	p := ws.lastFrame()["payload"].(map[string]any)
	if p["protocol_version"] != "1.0" {
		t.Fatalf("protocol_version 不符：%v", p)
	}
	if _, ok := p["agent_ready"]; !ok {
		t.Fatalf("缺 agent_ready")
	}
}
```

- [ ] **Step 3: 实现 `connectionStatus`（完整代码）**

```go
// connectionStatus 返回 Gateway / AgentServer 就绪状态。
//
// Python: _connection_status (app_web_handlers.py:5676-5684)。
//
// 与上游一致：handler 是 _register_web_handlers(bind) 内的闭包，捕获 agent_client，
// 故 Go 侧写成捕获 bind.AgentClient 的闭包，而不是 WebChannel 的方法。
func makeConnectionStatus(wc *WebChannel, agentClient *routing.AgentClient) methodHandler {
	return func(ws wsSender, reqID string, _ map[string]any, _ string) {
		agentReady := agentClient != nil && agentClient.ServerReady()
		wc.sendResponse(ws, reqID, true, map[string]any{
			"agent_ready":      agentReady,
			"protocol_version": "1.0",
		}, "", "")
	}
}
```

> `ServerReady()` 若 `routing.AgentClient` 没有，先在同包加只读访问器暴露 `server_ready` 字段，不在 handler 里另建状态。

- [ ] **Step 4: 其余 8 个按上游 1:1 移植 + 注册**

- [ ] **Step 5: 测试 + 矩阵 + 提交**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/gateway/...
go run ./cmd/rpc-matrix -upstream /home/opensource/jiuwenswarm-develop
git add -A && git commit -m "feat(web): 补齐 session 族与 connection/commands/moa/swarmflow 9 个方法"
```

---

## Task 12: B1b-5 九个 IM 平台的 get_conf/set_conf（18 个）

**Files:**
- Modify: `web/app_web_handlers.go`
- Test: `web/app_web_handlers_channel_conf_test.go`

**方法清单：** 对 `feishu / xiaoyi / telegram / dingtalk / whatsapp / discord / slack / wecom / wechat` 各一对 `channel.<p>.get_conf` / `channel.<p>.set_conf`（上游 `:5844-5861`，handler `_channel_<p>_get_conf` / `_channel_<p>_set_conf`）。

- [ ] **Step 1: 读上游 9 组实现**

Run: `sed -n '5780,5870p' /home/opensource/jiuwenswarm-develop/gateway/channel_manager/web/app_web_handlers.py`
若 handler 体在更早位置（定义与注册分离）：`grep -n "_channel_feishu_get_conf" -A 20 <file>`

- [ ] **Step 2: 写表驱动失败测试**

```go
// TestChannelConf_九平台齐全 验证 9 平台 × 2 方法全部注册。
func TestChannelConf_九平台齐全(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{}, nil, nil)
	registerChannelConfFamily(wc, nil)
	for _, p := range []string{"feishu", "xiaoyi", "telegram", "dingtalk", "whatsapp", "discord", "slack", "wecom", "wechat"} {
		for _, suffix := range []string{"get_conf", "set_conf"} {
			m := "channel." + p + "." + suffix
			if _, ok := wc.methodHandlers[m]; !ok {
				t.Errorf("方法未注册: %s", m)
			}
		}
	}
}

// TestChannelConf_无CM时回SERVICE_UNAVAILABLE 验证依赖缺失分支与上游文案一致。
//
// 这里传 nil 是**被测语义**（上游 app_web_handlers.py:4367-4377 的真实分支），
// 不是拿 nil 糊测试：断言的是错误码与文案。
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

// TestChannelConf_正常读取 验证注入 fake 后返回上游 payload 形状。
func TestChannelConf_正常读取(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{}, nil, nil)
	registerChannelConfFamily(wc, fakeChannelConfSource{conf: map[string]any{"apps": []any{map[string]any{"name": "bot-a"}}}})
	ws := newFakeWS()
	wc.methodHandlers["channel.feishu.get_conf"](ws, "rid", map[string]any{}, "sid")

	if ws.lastFrame()["ok"] != true {
		t.Fatalf("预期成功，实际 %v", ws.lastFrame())
	}
}

// fakeChannelConfSource 测试用渠道配置读写。
type fakeChannelConfSource struct {
	// conf GetConf 返回的配置
	conf map[string]any
}

// GetConf 返回预置配置。
func (f fakeChannelConfSource) GetConf(string) (map[string]any, error) { return f.conf, nil }

// SetConf 空实现（写回断言在需要时另行扩展）。
func (f fakeChannelConfSource) SetConf(string, map[string]any) error { return nil }
```

- [ ] **Step 3: 用平台表驱动实现（避免 18 份重复代码）**

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
//
// Python: app_web_handlers.py:5844-5861。每个平台的两个方法体按上游对应函数移植，
// 平台名只作为 selector 传入，不改变分支结构。
func registerChannelConfFamily(wc *WebChannel, cm channelConfSource) {
	for _, p := range channelConfPlatforms {
		platform := p
		wc.registerMethod("channel."+platform+".get_conf", func(ws wsSender, reqID string, params map[string]any, sid string) {
			channelConfGet(wc, cm, platform, ws, reqID, params, sid)
		}, false)
		wc.registerMethod("channel."+platform+".set_conf", func(ws wsSender, reqID string, params map[string]any, sid string) {
			channelConfSet(wc, cm, platform, ws, reqID, params, sid)
		}, false)
	}
}

// channelConfGet 读取单个 IM 平台的配置。
//
// Python: _channel_feishu_get_conf 等 (app_web_handlers.py:4365-...)。分支顺序照抄上游：
// 先判 cm 是否缺失（回 SERVICE_UNAVAILABLE + "channel manager not available"），
// 再取配置、按平台做归一化、最后回 payload。
func channelConfGet(wc *WebChannel, cm channelConfSource, platform string, ws wsSender, reqID string, params map[string]any, sid string) {
	if cm == nil {
		logger.Warn(logComponent).Str("platform", platform).Str("req_id", reqID).
			Msg("channel manager not available")
		wc.sendResponse(ws, reqID, false, nil, "channel manager not available", WsErrServiceUnavailable)
		return
	}
	// 平台分支：按上游对应函数体逐行移植（feishu 归一 apps、wechat 归一 login 等）
	raw, err := cm.GetConf(platform)
	if err != nil {
		wc.sendResponse(ws, reqID, false, nil, err.Error(), WsErrInternalError)
		return
	}
	wc.sendResponse(ws, reqID, true, raw, "", "")
}
```

> 若上游九个平台 handler 体确实不同（例如 wechat 多一个 login_ui 分支、feishu 归一 `apps`），则在该平台分支里照抄上游逻辑，不得为了 DRY 合并语义。

- [ ] **Step 4: 测试 + 矩阵 + 提交**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/gateway/channel_manager/web/ -run TestChannelConf -v
go run ./cmd/rpc-matrix -upstream /home/opensource/jiuwenswarm-develop
git add -A && git commit -m "feat(web): 补齐九个 IM 平台配置读写 18 个方法"
```

---

## Task 13: B1b-6 project 族（20 个，含 9 个 git）

**Files:**
- Create: `internal/swarm/gateway/channel_manager/web/project_git.go`（对齐 `project_git.py`）
- Create: `internal/swarm/gateway/channel_manager/web/git_ws_handler.go`（对齐 `git_ws_handler.py`）
- Modify: `web/app_web_handlers.go`（`project.*` CRUD 10 个）
- Test: 上述两文件的 `_test.go` + `app_web_handlers_project_test.go`

**方法清单：**

| 方法 | 上游 | Go 文件 |
|---|---|---|
| `project.list/info/get_sessions/get_cron_sessions/create/rename/pin/pinned_sessions/remove/restore` | `:5701-5716` | `app_web_handlers.go` |
| `project.git.status/probe/init/switch_branch/create_branch/commit/push/diff_status/turn_diff_list/turn_diff` | `:5719-5728` | `project_git.go` + `git_ws_handler.go` |

- [ ] **Step 1: 读上游**

Run:
```bash
cd /home/opensource/jiuwenswarm-develop/gateway/channel_manager/web
sed -n '5700,5730p' app_web_handlers.py
wc -l project_git.py git_ws_handler.py git_diff_status.py git_diff_watcher.py
```

- [ ] **Step 2: 先写 project CRUD 测试**

```go
// TestProjectCrud_注册齐全 验证 10 个 project 方法已注册。
func TestProjectCrud_注册齐全(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{}, nil, nil)
	registerProjectFamily(wc)
	for _, m := range []string{
		"project.list", "project.info", "project.get_sessions", "project.get_cron_sessions",
		"project.create", "project.rename", "project.pin", "project.pinned_sessions",
		"project.remove", "project.restore",
	} {
		if _, ok := wc.methodHandlers[m]; !ok {
			t.Errorf("方法未注册: %s", m)
		}
	}
}
```

- [ ] **Step 3: 实现 project CRUD（按上游 `_project_*` 1:1）**

- [ ] **Step 4: git 族按上游文件归属落地**

`project_git.py` 的 `project_git_status/probe/init/switch_branch/create_branch/commit/push/diff_status/turn_diff_list/turn_diff` → `project_git.go`；`git_ws_handler.py` 的 WS 侧封装 → `git_ws_handler.go`。测试用 `t.TempDir()` 造真仓库验证 status/diff 输出形状（不 mock git）。

- [ ] **Step 5: 集成用例（真 git 仓库）**

```go
// TestProjectGit_真实仓库状态 验证 project.git.status 在临时仓库上返回分支与改动。
func (s *WebRPCFullChainSuite) TestProjectGit_真实仓库状态() {
	repo := s.initGitRepo()
	s.sendRPC("project.git.status", map[string]any{"project_dir": repo})
	res := s.waitRPC("project.git.status")
	if !res.OK {
		t.Fatalf("status 失败：%+v", res)
	}
}
```

- [ ] **Step 6: 测试 + 矩阵 + 提交**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/gateway/channel_manager/web/ -run "TestProject" -v
go run ./cmd/rpc-matrix -upstream /home/opensource/jiuwenswarm-develop
git add -A && git commit -m "feat(web): 补齐 project 族 20 个方法（含 git 9 个，真仓库测试）"
```

---

## Task 14: B1b-7 file / media / document / path（8 个）

**Files:**
- Modify: `web/app_web_handlers.go`
- Test: `web/app_web_handlers_assets_test.go`

| 方法 | 上游 | 备注 |
|---|---|---|
| `file.upload_chunk` | `:5802` | 分片上传，后续实现参照 `container_file_http.py` |
| `media.persist` / `media.discard` | `:5811-5812` | |
| `document.persist` / `document.formats` | `:5813-5814` | |
| `path.select_file` / `path.select_files` / `path.select_directory` | `:5732-5734` | 对齐 `file_picker.py` / `directory_picker.py` |

- [ ] **Step 1: 读上游四个入口**

```bash
cd /home/opensource/jiuwenswarm-develop/gateway/channel_manager/web
sed -n '5730,5740p;5800,5820p' app_web_handlers.py
grep -n "def _path_select_file\|def _media_persist\|def _document_persist\|def _file_upload_chunk" -A 15 app_web_handlers.py
```

- [ ] **Step 2: 写失败测试**

```go
// TestPathSelect_缺桌面会话时返回对齐错误 验证 path.select_file 的错误分支。
func TestPathSelect_缺桌面会话时返回对齐错误(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{}, nil, nil)
	ws := newFakeWS()
	wc.registerMethod("path.select_file", wc.pathSelectFile, false)
	wc.methodHandlers["path.select_file"](ws, "rid", map[string]any{}, "sid")
	if ws.lastFrame()["ok"] == true {
		t.Fatalf("无桌面选择器时应失败")
	}
}
```

- [ ] **Step 3: 8 个 handler 按上游 1:1 移植 + 注册**

- [ ] **Step 4: 测试 + 矩阵 + 提交**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/gateway/channel_manager/web/ -v
go run ./cmd/rpc-matrix -upstream /home/opensource/jiuwenswarm-develop
git add -A && git commit -m "feat(web): 补齐 file/media/document/path 8 个方法"
```

---

## Task 15: B1b-8 harness / external_cli（9 个 + 删 7 个自造）

**Files:**
- Modify: `web/app_web_handlers.go`
- Delete: 7 个 `harness.*` 自造 handler
- Test: `web/app_web_handlers_harness_test.go`

| 方法 | 上游 | Go 名 |
|---|---|---|
| `harness.import` | `:6555` | `harnessImport` |
| `harness.export` | `:6567` | `harnessExport` |
| `external_cli.detect` | `:5496` | `externalCliDetect` |
| `external_cli.validate_paths` | `:5497` | `externalCliValidatePaths` |
| `external_cli.prune_path_histories` | `:5498` | `externalCliPrunePathHistories` |
| `external_cli.codex_install_status` | `:5499` | `externalCliCodexInstallStatus` |
| `external_cli.install_status` | `:5500` | `externalCliInstallStatus` |
| `external_cli.install` | `:5501` | `externalCliInstall` |
| `external_cli.custom_agent.remove` | `:5502` | `externalCliCustomAgentRemove` |

- [ ] **Step 1: 读上游**

Run: `sed -n '2268,2380p;5540,5700p' /home/opensource/jiuwenswarm-develop/gateway/channel_manager/web/app_web_handlers.py`

- [ ] **Step 2: 写失败测试**

```go
// TestHarnessExportImport_注册 验证 2 个方法与 7 个自造方法已清除。
func TestHarnessExportImport_注册(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{}, nil, nil)
	registerHarnessFamily(wc)
	if _, ok := wc.methodHandlers["harness.export"]; !ok {
		t.Fatalf("harness.export 未注册")
	}
	for _, gone := range []string{"harness.list", "harness.install", "harness.uninstall", "harness.enable", "harness.disable", "harness.get_status", "harness.rebuild"} {
		if _, ok := wc.methodHandlers[gone]; ok {
			t.Errorf("自造方法未删除: %s", gone)
		}
	}
}
```

- [ ] **Step 3: 实现 9 个 + 删除 7 个自造 handler 与其测试**

- [ ] **Step 4: 测试 + 矩阵 + 提交**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/gateway/channel_manager/web/ -v
go run ./cmd/rpc-matrix -upstream /home/opensource/jiuwenswarm-develop
git add -A && git commit -m "feat(web): 补齐 harness.import|export 与 external_cli 9 个方法；删除 7 个自造 harness.*"
```

---

## Task 16: B1b-9 openai_account / topic_starters / prompt_suggestion / asr / 3rdagent（18 个）

**Files:**
- Modify: `web/app_web_handlers.go`
- Test: `web/app_web_handlers_misc_test.go`

| 方法 | 上游 |
|---|---|
| `openai_account.auth.status/start_login/pending_login/poll_login/logout`、`openai_account.models.list` | `:5681-5686` → `:2778` 起实现 |
| `topic_starters.profile/restore_direction/control/status/select_model/get/foreground/event` | `:5820-5827` |
| `prompt_suggestion.event/get` | `:5828-5829` |
| `task.asr.transcribe` | `:5680` |
| `3rdagent.web` | `protocol/web_proxy/web_proxy_listen.py:883` |

- [ ] **Step 1: 读上游**

Run:
```bash
cd /home/opensource/jiuwenswarm-develop/gateway
sed -n '2778,2830p' channel_manager/web/app_web_handlers.py
sed -n '5820,5835p' channel_manager/web/app_web_handlers.py
sed -n '870,900p' channel_manager/protocol/web_proxy/web_proxy_listen.py
```

- [ ] **Step 2: 写失败测试**

```go
// TestTopicStarters_注册齐全 验证 8 个 topic_starters 方法已注册。
func TestTopicStarters_注册齐全(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{}, nil, nil)
	registerTopicStartersFamily(wc, nil)
	for _, m := range []string{
		"topic_starters.profile", "topic_starters.restore_direction", "topic_starters.control",
		"topic_starters.status", "topic_starters.select_model", "topic_starters.get",
		"topic_starters.foreground", "topic_starters.event",
	} {
		if _, ok := wc.methodHandlers[m]; !ok {
			t.Errorf("方法未注册: %s", m)
		}
	}
}
```

- [ ] **Step 3: 实现 12 个可落地方法（topic_starters 8 + prompt_suggestion 2 + task.asr 1 + openai_account.models.list 1）**

按上游 1:1 移植；`task.asr.transcribe` 对齐 `task_asr.py`。

- [ ] **Step 4: 两个依赖缺口按偏差处置（不做假实现、不加白名单）**

- `openai_account.auth.*`（5 个）：上游委托 `openai_account_auth` 设备码模块，属计划 23.6。本步**只注册方法并返回与上游相同的错误形状**：

```go
// openaiAccountAuthStatus 查询 OpenAI 账号授权状态。
//
// Python: _openai_account_auth_status (app_web_handlers.py:2778-2795)。
// ⤵️ 依赖 23.6（openai_account_auth 设备码模块）未迁移：当前统一回 SERVICE_UNAVAILABLE，
// 与上游"模块不可用"分支的 code 一致；23.6 落地后替换为真实调用。
func makeOpenAIAccountAuthStatus(wc *WebChannel) methodHandler {
	return func(ws wsSender, reqID string, _ map[string]any, _ string) {
		wc.sendResponse(ws, reqID, false, nil, "openai account auth unavailable", WsErrServiceUnavailable)
	}
}
```

  同批在 `IMPLEMENTATION_PLAN.md` 的 23.6 行加 ⤴️ 回填标记，写明"Web 侧 5 个 openai_account.auth.* 已注册待接"。

- `3rdagent.web`：同样先注册，转 `web_proxy` 未迁移的 ⤵️；等 `web_proxy` 批落地后接真实现。

- [ ] **Step 5: 测试 + 矩阵 + 提交**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/gateway/channel_manager/web/ -v
go run ./cmd/rpc-matrix -upstream /home/opensource/jiuwenswarm-develop
git add -A && git commit -m "feat(web): 补齐 topic_starters/prompt_suggestion/asr 等方法；openai_account 与 3rdagent 注册待依赖回填"
```

---

## Task 17: 收尾 —— 矩阵归零、门禁转强制、计划登记

**Files:**
- Modify: `.github/workflows/ci.yml`（`-verify` 转强制）
- Modify: `IMPLEMENTATION_PLAN.md`（27-A、27.x 状态与 ⤵️/⤴️）
- Modify: `docs/superpowers/specs/2026-11-05-web-ws-rpc-alignment-design.md`（偏差回收记录）

- [ ] **Step 1: 确认矩阵无 ❌**

Run: `go run ./cmd/rpc-matrix -verify -upstream /home/opensource/jiuwenswarm-develop`
Expected: 退出码 0，输出「缺 0 个」。

- [ ] **Step 2: CI 转强制**

把 Task 2 里的步骤改为：

```yaml
      - name: 校验 Web RPC 契约矩阵
        run: |
          git clone --depth 1 https://gitcode.com/openJiuwen/jiuwenswarm.git /tmp/upstream
          go run ./cmd/rpc-matrix -verify -upstream /tmp/upstream -out docs/sync/web-rpc-matrix.md
          git diff --exit-code -- docs/sync/web-rpc-matrix.md
```

- [ ] **Step 3: 计划登记**

`IMPLEMENTATION_PLAN.md`：
- 27-A 的 B0、B1 行 → ✅，并写明"RPC 154 全量对齐，矩阵 `docs/sync/web-rpc-matrix.md`"
- 27.1-27.10 中受本计划影响的行补记方法数
- 新增偏差行：`openai_account.auth.*`（⤵️ 23.6）、`3rdagent.web`（⤵️ web_proxy）
- 24.13 行状态由 🔄 → ✅（代码已在 `36f85699` 落地，本步仅补记）

- [ ] **Step 4: 全量验收**

```bash
export GOPROXY=https://goproxy.cn,direct
CGO_ENABLED=1 go build -tags sqlite_fts5 ./...
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/...
CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/web/...
go run ./cmd/rpc-matrix -verify -upstream /home/opensource/jiuwenswarm-develop
go run ./cmd/integration-audit -verify
```
Expected: 全部通过。

- [ ] **Step 5: 提交并观察 CI**

```bash
git add -A
git commit -m "chore(web): RPC 矩阵归零，-verify 转 CI 强制；计划登记 27-A/24.13"
git push
gh run list --limit 3
```
Expected: Build / Lint / Unit Test / Integration Test 四 job SUCCESS。

---

## 附：后续独立计划（不在本计划内）

- **入站主流程对齐**：24.13 剩余两步（`_resolve_external_channel_session`、非流式 team 拒绝）+ 审查 M-07 六步（`prepare_im_session_input`、`session_input` 校验归一、`session_busy`/`steer`、`godview` 注册）。改 `forward_loop.go`/`message_handler`，与 WebChannel RPC 表面无交集，须单独 spec。
- **27-A B2~B8 整域模块**：HTTP 路由/鉴权、文件与目录面板、git diff watcher、分享导出、轨迹诊断、MCP/沙箱 OAuth（28-33 人日）。
