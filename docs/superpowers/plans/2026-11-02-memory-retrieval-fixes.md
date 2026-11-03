# memory 检索三处修复 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 memory 检索的「重建判定 / 向量维度 / 中文命中」三处行为可解释、可自愈：provider 实例变了会重建、维度不匹配会重建表、中文能按词命中。

**Architecture:** 全部改动集中在 `internal/agentcore/memory/lite`（`memory_rail.go` 用的就是这套 lite 实现；`internal/swarm/agents/harness/common/memory` 是另一套并行实现，**不在本计划范围**）。核心是先把「是否需要全量重建」收敛成单一判定函数 `needsReindex() (bool, reason)`，A/B/C 三处都挂在它上面；分词能力通过新文件 `segmenter.go` 提供，索引侧与查询侧共用同一个分词入口，失败则降级为不切分。

**Tech Stack:** Go 1.x + `github.com/mattn/go-sqlite3`（`sqlite_fts5` build tag）+ `github.com/go-ego/gse v1.3.1`（中文分词，`LoadDictEmbed("zh_s")`）。

**依据 spec:** `docs/superpowers/specs/2026-11-02-memory-retrieval-fixes-design.md`

---

## 全局约束（每个 task 都适用）

- 所有 Go 命令都要带 tags：`-tags "sqlite_fts5 test"`（FTS5 用例否则假红）。
- 单测命令模板：`go test -tags "sqlite_fts5 test" ./internal/agentcore/memory/lite/ -run <TestName> -v`
- 每次提交前：`gofmt -l internal/ | grep -v '^$'` 必须为空（用 `$(go env GOROOT)/bin/gofmt`）。
- 注释一律中文（项目规范 1）；文件内声明按「接口/结构体 → 枚举 → 常量 → 全局变量 → 导出函数 → 非导出函数」排列。
- 新增源文件必须登记到同目录 `doc.go` 的文件目录里。
- **当前主工作区可能被别人占用在 `fix/review-2026-09-23` 分支上**：执行本计划前先在独立 worktree 里做（见 Task 0）。

---

## Task 0: 准备隔离工作区

**Files:** 无（环境准备）

- [ ] **Step 1: 从 main 建独立 worktree**

```bash
cd /home/opensource/uap-claw-go
git fetch origin
git worktree add /tmp/wt-memory-fix origin/main
cd /tmp/wt-memory-fix
```

- [ ] **Step 2: 确认基线测试绿**

```bash
cd /tmp/wt-memory-fix
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/memory/lite/ 2>&1 | tail -5
```
Expected: `ok  github.com/uapclaw/uapclaw-go/internal/agentcore/memory/lite`

- [ ] **Step 3: 记录基线行为（用于对照）**

```bash
cd /tmp/wt-memory-fix
grep -n "meta\[\"provider\"\] != m.settings.Provider" internal/agentcore/memory/lite/manager_impl.go
grep -n "INSERT OR REPLACE INTO %s (rowid, id, path, source, text)" internal/agentcore/memory/lite/manager_impl.go
```
Expected: 分别命中 `manager_impl.go:740` 附近与 `indexChunkWithTx` 内的 FTS 写入行 —— 这两处就是 Task 1/5 要改的地方。

---

## Task 1: `needsReindex` 统一判定（A 的核心）

**Files:**
- Modify: `internal/agentcore/memory/lite/manager_impl.go:190`（`Sync` 调用点）、`:737-757`（`shouldFullReindex` 整体替换）
- Test: `internal/agentcore/memory/lite/manager_reindex_test.go`（新建）

- [ ] **Step 1: 写失败测试**

新建 `internal/agentcore/memory/lite/manager_reindex_test.go`：

```go
package lite

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
)

// fakeEmbedProvider 可配置 id/model/dims 的假 provider。
// 存在的意义：needsReindex 必须比对「实例值」，而 settings 里的 provider 是恒定配置值，
// 只有换掉实例才能构造出「配置没变、实例变了」这条真实场景。
type fakeEmbedProvider struct {
	id, model string
	dims      int
}

func (f *fakeEmbedProvider) EmbedQuery(context.Context, string) ([]float64, error) {
	return make([]float64, f.dims), nil
}

func (f *fakeEmbedProvider) EmbedDocuments(context.Context, []string) ([][]float64, error) {
	return nil, nil
}

func (f *fakeEmbedProvider) ID() string    { return f.id }
func (f *fakeEmbedProvider) Model() string { return f.model }
func (f *fakeEmbedProvider) Dims() int     { return f.dims }

// newReindexTestManager 造一个带真实 sqlite（含 meta 表）的 manager。
func newReindexTestManager(t *testing.T) (*memoryIndexManager, func()) {
	t.Helper()
	ws := workspace.NewWorkspace(t.TempDir(), "cn")
	nodePath := ws.GetNodePath("memory")
	if nodePath == nil {
		t.Fatal("Workspace 应包含 memory 节点")
	}
	settings := CreateMemorySettings(*nodePath, nil)
	settings.Sync.Watch = false
	settings.Sync.IntervalMinutes = 0

	mgr, err := GetMemoryIndexManager(MemoryManagerParams{
		AgentID:   "unit_reindex",
		Workspace: ws,
		Settings:  settings,
		NodeName:  "memory",
	})
	if err != nil {
		t.Fatalf("初始化 manager 失败: %v", err)
	}
	m := mgr.(*memoryIndexManager)
	return m, func() {
		_ = m.Close()
		ClearMemoryManagerCache()
	}
}

func TestNeedsReindex_表驱动(t *testing.T) {
	baseMeta := func(over map[string]any) map[string]any {
		meta := map[string]any{
			"provider":    "mock",
			"model":       "mock",
			"chunkTokens": float64(256),
			"segmenter":   "none",
		}
		for k, v := range over {
			meta[k] = v
		}
		return meta
	}

	cases := []struct {
		name       string
		meta       map[string]any // nil = 不写 meta
		providerID string
		providerMd string
		want       bool
		wantReason string
	}{
		{"meta 缺失", nil, "mock", "mock", true, "meta_missing"},
		{"全一致", baseMeta(nil), "mock", "mock", false, ""},
		{"provider 从 mock 换成真模型", baseMeta(nil), "openai_compatible", "text-embedding-v3", true, "provider_changed"},
		{"model 变化", baseMeta(nil), "mock", "other-model", true, "model_changed"},
		{"chunkTokens 变化", baseMeta(map[string]any{"chunkTokens": float64(128)}), "mock", "mock", true, "chunking_changed"},
		{"segmenter 变化", baseMeta(map[string]any{"segmenter": "gse-zh_s@v1.3.1"}), "mock", "mock", true, "segmenter_changed"},
		{"vectorDims 变化", baseMeta(map[string]any{"vectorDims": float64(128)}), "mock", "mock", true, "vector_dims_changed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, cleanup := newReindexTestManager(t)
			defer cleanup()

			m.provider = &fakeEmbedProvider{id: tc.providerID, model: tc.providerMd, dims: 1024}
			if tc.meta != nil {
				if err := m.writeMeta(tc.meta); err != nil {
					t.Fatalf("写 meta 失败: %v", err)
				}
			}

			got, reason := m.needsReindex()
			if got != tc.want || reason != tc.wantReason {
				t.Errorf("needsReindex() = (%v, %q), want (%v, %q)", got, reason, tc.want, tc.wantReason)
			}
		})
	}
}

// TestNeedsReindex_provider为nil 初始化失败时也必须要求重建（并打 Error）
func TestNeedsReindex_provider为nil(t *testing.T) {
	m, cleanup := newReindexTestManager(t)
	defer cleanup()

	m.provider = nil
	got, reason := m.needsReindex()
	if !got || reason != "provider_unavailable" {
		t.Errorf("needsReindex() = (%v, %q), want (true, provider_unavailable)", got, reason)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
cd /tmp/wt-memory-fix
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/memory/lite/ -run 'TestNeedsReindex' -v 2>&1 | tail -10
```
Expected: 编译失败 `m.needsReindex undefined`（函数还不存在）。

- [ ] **Step 3: 实现 `needsReindex` 并替换 `shouldFullReindex`**

`manager_impl.go`：把 `shouldFullReindex`（约 737-757 行）整段替换为下面三个函数：

```go
// readMeta 读取索引元数据。缺失/解析失败返回 error（调用方视为需要重建）。
func (m *memoryIndexManager) readMeta() (map[string]any, error) {
	var value string
	if err := m.db.QueryRow("SELECT value FROM meta WHERE key = ?", metaKey).Scan(&value); err != nil {
		return nil, err
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(value), &meta); err != nil {
		return nil, err
	}
	return meta, nil
}

// needsReindex 统一重建判定 —— A/B/C 三处共用这一入口。
//
// 对齐 Python _should_full_reindex（manager.py:587），但比对的是**实际 provider 实例**
// （self.provider.id），不是 settings 里的配置值：embed 缺 key 时 CreateEmbeddingProvider
// 会静默降级成 mock，若比对配置值，补好 key 后永远不会触发重建。
//
// 返回原因取值：meta_missing / provider_unavailable / provider_changed / model_changed
//             / chunking_changed / segmenter_changed / vector_dims_changed
func (m *memoryIndexManager) needsReindex() (bool, string) {
	meta, err := m.readMeta()
	if err != nil {
		return true, "meta_missing"
	}
	if m.provider == nil {
		logger.Error(logComponent).
			Str("event_type", "memory_reindex_triggered").
			Str("reason", "provider_unavailable").
			Msg("embedding provider unavailable, reindexing text only")
		return true, "provider_unavailable"
	}
	if meta["provider"] != m.provider.ID() {
		return true, "provider_changed"
	}
	if meta["model"] != m.provider.Model() {
		return true, "model_changed"
	}
	if meta["chunkTokens"] != m.settings.Chunking.Tokens {
		return true, "chunking_changed"
	}
	if meta["segmenter"] != segmenterID() {
		return true, "segmenter_changed"
	}
	if dims, ok := metaInt(meta, "vectorDims"); ok && dims != m.provider.Dims() {
		return true, "vector_dims_changed"
	}
	return false, ""
}

// metaInt 从 meta 取整数值（JSON 反序列化后是 float64）。
func metaInt(meta map[string]any, key string) (int, bool) {
	switch v := meta[key].(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	default:
		return 0, false
	}
}
```

同文件 `Sync`（约 190 行）改为：

```go
	needReindex, reason := m.needsReindex()
	if force {
		needReindex, reason = true, "forced"
	}
	if needReindex {
		logger.Info(logComponent).
			Str("event_type", "memory_reindex_triggered").
			Str("reason", reason).
			Msg("performing full index rebuild")
		return m.runReindex(ctx, reason)
	}
```

`runReindex` 增加 `reason` 参数（Task 2 会用到它）：

```go
func (m *memoryIndexManager) runReindex(ctx context.Context, reason string) error {
```

- [ ] **Step 4: 跑测试确认通过**

```bash
cd /tmp/wt-memory-fix
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/memory/lite/ -run 'TestNeedsReindex' -v 2>&1 | tail -15
```
Expected: `TestNeedsReindex_表驱动` 7 个子用例 + `TestNeedsReindex_provider为nil` 全 PASS。

- [ ] **Step 5: 跑整包确认没有破坏其他用例**

```bash
cd /tmp/wt-memory-fix
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/memory/lite/ 2>&1 | tail -5
```
Expected: ok（`shouldFullReindex` 的引用方只有 `Sync`，已一并改掉；若报未使用/未定义，全局 grep 一次 `shouldFullReindex`）。

- [ ] **Step 6: 提交**

```bash
cd /tmp/wt-memory-fix
git add internal/agentcore/memory/lite/manager_impl.go internal/agentcore/memory/lite/manager_reindex_test.go
git commit -m "fix(memory): 重建判定改用实际 provider 实例，并归一为 needsReindex"
```

---

## Task 2: `runReindex` 写实例值 + meta.segmenter

**Files:**
- Modify: `internal/agentcore/memory/lite/manager_impl.go:751-785`（`runReindex`）
- Create: `internal/agentcore/memory/lite/segmenter.go`（先放 `segmenterID` 桩，Task 4 填实现）
- Test: `internal/agentcore/memory/lite/manager_reindex_test.go`（追加）

- [ ] **Step 1: 写失败测试**

追加到 `manager_reindex_test.go`：

```go
// TestRunReindex_meta写实例值 回归：meta 里必须记 mock（实例值），不是 settings 的
// openai_compatible；否则下次启动判定不出「provider 换了」，永远不重建。
func TestRunReindex_meta写实例值(t *testing.T) {
	m, cleanup := newReindexTestManager(t)
	defer cleanup()

	m.provider = &fakeEmbedProvider{id: "mock", model: "mock", dims: 128}
	if err := m.runReindex(context.Background(), "unit"); err != nil {
		t.Fatalf("runReindex 失败: %v", err)
	}

	meta, err := m.readMeta()
	if err != nil {
		t.Fatalf("读 meta 失败: %v", err)
	}
	if meta["provider"] != "mock" || meta["model"] != "mock" {
		t.Errorf("meta 应记实例值 mock/mock，实际 %v/%v", meta["provider"], meta["model"])
	}
	if meta["segmenter"] != segmenterID() {
		t.Errorf("meta.segmenter = %v, want %v", meta["segmenter"], segmenterID())
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
cd /tmp/wt-memory-fix
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/memory/lite/ -run 'TestRunReindex_meta写实例值' -v 2>&1 | tail -8
```
Expected: FAIL —— `meta 应记实例值 mock/mock，实际 openai_compatible/text-embedding-v3`。

- [ ] **Step 3: 新建 `segmenter.go`（只放桩）**

```go
package lite

// ──────────────────────────── 常量 ────────────────────────────

const (
	// segmenterVersion 分词器标识，写入 meta。词典/库升级时改这里 → 触发全量重建。
	segmenterVersion = "gse-zh_s@v1.3.1"
	// maxFTSTokens 关键词检索最多使用的 token 数（AND 连接，过多会让召回过窄）。
	maxFTSTokens = 5
)

// ──────────────────────────── 导出函数 ────────────────────────────

// InitMemorySegmenterAsync 后台预热分词器。Task 4 填入实现。
func InitMemorySegmenterAsync() {}

// ──────────────────────────── 非导出函数 ────────────────────────────

// segmenterID 返回分词器标识；不可用时返回 "none"（写入 meta，避免反复重建）。
func segmenterID() string { return "none" }
```

- [ ] **Step 4: 改 `runReindex` 写实例值**

`manager_impl.go` 的 `runReindex` 末尾：

```go
	providerID, providerModel := "", ""
	if m.provider != nil {
		providerID = m.provider.ID()
		providerModel = m.provider.Model()
	}
	meta := map[string]any{
		"provider":     providerID,
		"model":        providerModel,
		"providerKey":  m.providerKey,
		"chunkTokens":  m.settings.Chunking.Tokens,
		"chunkOverlap": m.settings.Chunking.Overlap,
		"segmenter":    segmenterID(),
	}
	if m.vectorAvailable && m.vectorDims != nil {
		meta["vectorDims"] = *m.vectorDims
	}
	return m.writeMeta(meta)
```

`runReindex` 开头（取完 reason 后）加一行日志，便于排查：

```go
	logger.Info(logComponent).
		Str("event_type", "memory_reindex_triggered").
		Str("reason", reason).
		Msg("running full reindex")
```

- [ ] **Step 5: 跑测试确认通过**

```bash
cd /tmp/wt-memory-fix
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/memory/lite/ -run 'TestRunReindex|TestNeedsReindex' -v 2>&1 | tail -12
```
Expected: 全 PASS。

- [ ] **Step 6: 提交**

```bash
cd /tmp/wt-memory-fix
git add internal/agentcore/memory/lite/manager_impl.go internal/agentcore/memory/lite/segmenter.go internal/agentcore/memory/lite/manager_reindex_test.go
git commit -m "fix(memory): meta 记实际 provider 与分词器标识"
```

---

## Task 3: 向量表维度自愈（B）

**Files:**
- Modify: `internal/agentcore/memory/lite/manager_impl.go:1134-1153`（`ensureVectorTable`）
- Create: `internal/agentcore/memory/lite/vec_table.go`
- Test: `internal/agentcore/memory/lite/vec_table_test.go`（新建）

> 为什么先抽纯函数：`vec0` 扩展只在带 dll 的环境可用（CI 的 linux 无此扩展），
> 直接在 `ensureVectorTable` 里写逻辑无法被单测覆盖。把「解析 + 判定」抽成纯函数即可测。
>
> 与 spec 的偏差（更可靠）：spec 写「与 `meta.vectorDims` 比对」，实现改为**解析 `sqlite_master`
> 里 vec 表的真实维度**。原因：`meta.vectorDims` 是我们自己写的记录值，可能因异常退出而缺失；
> 建表语句才是事实来源。`meta.vectorDims` 保留作 `needsReindex` 的初筛依据。

- [ ] **Step 1: 写失败测试**

新建 `internal/agentcore/memory/lite/vec_table_test.go`：

```go
package lite

import "testing"

func TestParseVecTableDims(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want int
		ok   bool
	}{
		{"标准建表", "CREATE VIRTUAL TABLE chunks_vec USING vec0(embedding float[128])", 128, true},
		{"多维写法", "CREATE VIRTUAL TABLE chunks_vec USING vec0(embedding float[1024], distance_metric=cosine)", 1024, true},
		{"非 vec 表", "CREATE TABLE chunks (id TEXT)", 0, false},
		{"空 SQL", "", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseVecTableDims(tc.sql)
			if got != tc.want || ok != tc.ok {
				t.Errorf("parseVecTableDims(%q) = (%d, %v), want (%d, %v)", tc.sql, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestNeedVecTableRebuild(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want int
		reb  bool
	}{
		{"表不存在 → 需要建", "", 1024, true},
		{"维度一致 → 复用", "CREATE VIRTUAL TABLE chunks_vec USING vec0(embedding float[1024])", 1024, false},
		{"维度变化 → 重建", "CREATE VIRTUAL TABLE chunks_vec USING vec0(embedding float[128])", 1024, true},
		{"解析不出 → 重建", "CREATE VIRTUAL TABLE chunks_vec USING vec0(x)", 1024, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := needVecTableRebuild(tc.sql, tc.want); got != tc.reb {
				t.Errorf("needVecTableRebuild(%q, %d) = %v, want %v", tc.sql, tc.want, got, tc.reb)
			}
		})
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
cd /tmp/wt-memory-fix
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/memory/lite/ -run 'TestParseVecTableDims|TestNeedVecTableRebuild' -v 2>&1 | tail -8
```
Expected: 编译失败 `undefined: parseVecTableDims`。

- [ ] **Step 3: 新建 `vec_table.go`**

```go
package lite

import (
	"regexp"
	"strconv"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// vecTableDimsRe 从 vec0 建表语句里抓 float[N] 的维度。
var vecTableDimsRe = regexp.MustCompile(`float\[\s*(\d+)\s*\]`)

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseVecTableDims 解析 vec0 建表语句里的向量维度。
//
// 为什么要解析 sql：vec0 的维度写在 CREATE 语句里，没有系统表可查；
// 而 manager 进程内的 vectorDims 在新进程里是空的，读到旧表时无法发现维度不匹配。
func parseVecTableDims(sql string) (int, bool) {
	m := vecTableDimsRe.FindStringSubmatch(sql)
	if len(m) != 2 {
		return 0, false
	}
	dims, err := strconv.Atoi(m[1])
	if err != nil || dims <= 0 {
		return 0, false
	}
	return dims, true
}

// needVecTableRebuild 判定是否必须丢弃现有 vec 表重建。
//
// 表不存在 → 需要建；维度一致 → 复用；维度不一致或解析不出 → 重建。
func needVecTableRebuild(existingSQL string, want int) bool {
	dims, ok := parseVecTableDims(existingSQL)
	if !ok {
		return true
	}
	return dims != want
}
```

- [ ] **Step 4: 跑测试确认通过**

```bash
cd /tmp/wt-memory-fix
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/memory/lite/ -run 'TestParseVecTableDims|TestNeedVecTableRebuild' -v 2>&1 | tail -12
```
Expected: 8 个子用例全 PASS。

- [ ] **Step 5: 把判定接进 `ensureVectorTable`**

`manager_impl.go` 的 `ensureVectorTable` 整段替换为：

```go
// ensureVectorTable 确保 vec0 表存在且维度正确。对齐并修正 Python _ensure_vector_table。
//
// 与旧实现的差异：旧实现只比进程内的 vectorDims，新进程读到旧维度表时
// `CREATE VIRTUAL TABLE IF NOT EXISTS` 是空操作 → 新维度向量写不进去。
// 这里改为从 sqlite_master 解析真实表维度（parseVecTableDims），不一致就丢表重建。
func (m *memoryIndexManager) ensureVectorTable(dims int) bool {
	if !m.vectorAvailable {
		return false
	}
	if m.vectorDims != nil && *m.vectorDims == dims {
		return true
	}

	var existingSQL string
	_ = m.db.QueryRow(
		"SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?", vectorTable,
	).Scan(&existingSQL)

	oldDims, _ := parseVecTableDims(existingSQL)
	if needVecTableRebuild(existingSQL, dims) {
		if existingSQL != "" {
			_, _ = m.db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", vectorTable))
			logger.Warn(logComponent).
				Str("event_type", "memory_vector_table_recreated").
				Int("old_dims", oldDims).
				Int("new_dims", dims).
				Msg("vector table dims mismatch, recreating")
		}
		if _, err := m.db.Exec(fmt.Sprintf("CREATE VIRTUAL TABLE IF NOT EXISTS %s USING vec0(embedding float[%d])", vectorTable, dims)); err != nil {
			m.vectorAvailable = false
			m.vectorError = err.Error()
			return false
		}
	}

	m.vectorDims = &dims
	return true
}
```

- [ ] **Step 6: 跑整包 + 提交**

```bash
cd /tmp/wt-memory-fix
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/memory/lite/ 2>&1 | tail -5
git add internal/agentcore/memory/lite/vec_table.go internal/agentcore/memory/lite/vec_table_test.go internal/agentcore/memory/lite/manager_impl.go
git commit -m "fix(memory): vec0 维度不匹配时按 sqlite_master 真实维度重建"
```

---

## Task 4: gse 依赖 + `segmenter.go` 实现

**Files:**
- Modify: `internal/agentcore/memory/lite/segmenter.go`（把桩换成实现）
- Test: `internal/agentcore/memory/lite/segmenter_test.go`（新建）
- Modify: `go.mod` / `go.sum`（`go get` 产生）

- [ ] **Step 1: 加依赖**

```bash
cd /tmp/wt-memory-fix
export GOPROXY=https://goproxy.cn,direct
go get github.com/go-ego/gse@v1.3.1
```
Expected: `go.mod` 新增 `require github.com/go-ego/gse v1.3.1`。

- [ ] **Step 2: 写失败测试**

新建 `internal/agentcore/memory/lite/segmenter_test.go`：

```go
package lite

import (
	"errors"
	"sync"
	"testing"
)

// TestSegmentForFTS_过滤规则 用真实词典验证：空格/标点被过滤、英文小写化、停用词被过滤。
func TestSegmentForFTS_过滤规则(t *testing.T) {
	seg, err := ensureSegmenter()
	if err != nil || seg == nil {
		t.Skipf("分词器不可用，跳过: %v", err)
	}

	got := segmentForFTS("VDS15800 组网")
	want := []string{"vds15800", "组网"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("segmentForFTS = %v, want %v", got, want)
	}

	tokens := segmentForFTS("这个设备的组网方式")
	for _, tok := range tokens {
		if tok == "的" || tok == "，" || tok == " " {
			t.Errorf("停用词/标点未过滤: %v", tokens)
		}
	}
	if len(tokens) == 0 {
		t.Error("正常中文句子不应切出空结果")
	}
}

// TestSegmentForFTS_不可用返回nil 分词器加载失败时必须返回 nil（调用方据此降级）
func TestSegmentForFTS_不可用返回nil(t *testing.T) {
	prevFn, prevOnce, prevInst := segmenterLoadFn, segOnce, segInst
	defer func() { segmenterLoadFn, segOnce, segInst = prevFn, prevOnce, prevInst }()

	segmenterLoadFn = func() (segmenterHandle, error) { return nil, errors.New("dict load failed") }
	segOnce = sync.Once{}
	segInst = nil

	if got := segmentForFTS("组网"); got != nil {
		t.Errorf("降级时应返回 nil，实际 %v", got)
	}
	if id := segmenterID(); id != "none" {
		t.Errorf("降级时 segmenterID = %q, want none", id)
	}
}

// TestJoinSegments 验证写进 FTS 的文本形态
func TestJoinSegments(t *testing.T) {
	if got := joinSegments([]string{"a", "b"}); got != "a b" {
		t.Errorf("joinSegments = %q, want %q", got, "a b")
	}
	if got := joinSegments(nil); got != "" {
		t.Errorf("joinSegments(nil) = %q, want 空串", got)
	}
}
```

- [ ] **Step 3: 跑测试确认失败**

```bash
cd /tmp/wt-memory-fix
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/memory/lite/ -run 'TestSegmentForFTS|TestJoinSegments' -v 2>&1 | tail -8
```
Expected: 编译失败（`segmenterLoadFn`/`ensureSegmenter`/`segmentForFTS`/`joinSegments` 未定义）。

- [ ] **Step 4: 写实现（替换 `segmenter.go` 全文）**

```go
package lite

import (
	"strings"
	"sync"
	"unicode"

	"github.com/go-ego/gse"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// segmenterHandle 分词器最小契约：测试可注入假的实现来验证降级路径。
type segmenterHandle interface {
	// Cut 分词，hmm=true 时启用 HMM 新词发现
	Cut(str string, hmm ...bool) []string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// segmenterVersion 分词器标识，写入 meta。词典/库升级时改这里 → 触发全量重建。
	segmenterVersion = "gse-zh_s@v1.3.1"
	// maxFTSTokens 关键词检索最多使用的 token 数（AND 连接，过多会让召回过窄）。
	maxFTSTokens = 5
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// segOnce 保证词典只加载一次（实测 843ms）
	segOnce sync.Once
	// segInst 分词器实例；nil 表示不可用
	segInst segmenterHandle
	// segmenterLoadFn 加载函数（测试可替换）
	segmenterLoadFn = loadSegmenter
)

// ──────────────────────────── 导出函数 ────────────────────────────

// InitMemorySegmenterAsync 后台预热分词器，避免 843ms 落在用户第一次搜索上。
func InitMemorySegmenterAsync() {
	go func() { _, _ = ensureSegmenter() }()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ensureSegmenter 惰性加载分词器，失败返回 (nil, err) 且不再重试。
func ensureSegmenter() (segmenterHandle, error) {
	var loadErr error
	segOnce.Do(func() {
		seg, err := segmenterLoadFn()
		if err != nil {
			loadErr = err
			logger.Error(logComponent).
				Str("event_type", "memory_segmenter_unavailable").
				Err(err).
				Msg("memory segmenter unavailable, falling back to unsegmented text")
			return
		}
		segInst = seg
	})
	if segInst == nil && loadErr == nil {
		loadErr = errSegmenterUnavailable
	}
	return segInst, loadErr
}

// loadSegmenter 加载 gse 内置简体词典 + 停用词表。
//
// 只指定 "zh_s"：gse 会把 jp/繁/简/idf 四份词典一起 embed 进二进制（约 36MB），
// 但运行时只加载简体这一份。
func loadSegmenter() (segmenterHandle, error) {
	var seg gse.Segmenter
	if err := seg.LoadDictEmbed("zh_s"); err != nil {
		return nil, err
	}
	// 停用词表可选：失败不阻断分词
	_ = seg.LoadStopEmbed()
	return &seg, nil
}

// segmenterID 返回分词器标识；不可用时 "none"（写入 meta，避免反复重建）。
func segmenterID() string {
	seg, err := ensureSegmenter()
	if err != nil || seg == nil {
		return "none"
	}
	return segmenterVersion
}

// segmentForFTS 分词入口 —— 索引侧与查询侧必须共用，否则 token 对不齐。
//
// 处理链：gse.Cut → 小写 → 去掉空白/纯标点 → 去重保序。
// 分词器不可用时返回 nil，调用方降级为「原文 + 正则切分 + OR」。
func segmentForFTS(text string) []string {
	seg, err := ensureSegmenter()
	if err != nil || seg == nil {
		return nil
	}
	raw := seg.Cut(text, true)
	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, tok := range raw {
		tok = strings.ToLower(strings.TrimSpace(tok))
		if !isContentToken(tok) {
			continue
		}
		if _, dup := seen[tok]; dup {
			continue
		}
		seen[tok] = struct{}{}
		out = append(out, tok)
	}
	return out
}

// joinSegments 把分词结果拼成写进 FTS 的文本形态。
func joinSegments(tokens []string) string {
	return strings.Join(tokens, " ")
}

// isContentToken 判断 token 是否含字母或数字（滤掉空格、标点）。
func isContentToken(tok string) bool {
	if tok == "" {
		return false
	}
	for _, r := range tok {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}
```

`errors` 加到 import，并在「全局变量」区块内、`segOnce` 之前声明：

```go
	// errSegmenterUnavailable 分词器不可用（已降级）
	errSegmenterUnavailable = errors.New("memory segmenter unavailable")
```

- [ ] **Step 5: 跑测试确认通过**

```bash
cd /tmp/wt-memory-fix
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/memory/lite/ -run 'TestSegmentForFTS|TestJoinSegments' -v 2>&1 | tail -12
```
Expected: 3 个用例 PASS（`TestSegmentForFTS_过滤规则` 首次运行约 0.8s，词典只加载一次）。

- [ ] **Step 6: 跑整包 + 登记 doc.go + 提交**

```bash
cd /tmp/wt-memory-fix
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/memory/lite/ 2>&1 | tail -5
```
把 `segmenter.go` 与 `vec_table.go` 加进 `internal/agentcore/memory/lite/doc.go` 的文件目录（格式照抄现有条目）。

```bash
cd /tmp/wt-memory-fix
git add go.mod go.sum internal/agentcore/memory/lite/segmenter.go internal/agentcore/memory/lite/segmenter_test.go internal/agentcore/memory/lite/doc.go
git commit -m "feat(memory): 引入 gse 中文分词，提供索引/查询共用的分词入口"
```

---

## Task 5: 索引侧写入分词文本

**Files:**
- Modify: `internal/agentcore/memory/lite/manager_impl.go:993-1001`（`indexChunkWithTx` 的 FTS 写入）
- Test: `internal/agentcore/memory/lite/manager_search_zh_test.go`（新建）

- [ ] **Step 1: 写失败测试（走真实 sqlite + FTS）**

新建 `internal/agentcore/memory/lite/manager_search_zh_test.go`：

```go
package lite

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestSearch_中文关键词命中 端到端验证「索引写分词文本 → 查询分词 → 命中」。
// 向量侧关掉（TextWeight=1/VectorWeight=0），排除 embedding 干扰。
func TestSearch_中文关键词命中(t *testing.T) {
	if id := segmenterID(); id == "none" {
		t.Skip("分词器不可用，跳过中文检索用例")
	}

	m, cleanup := newReindexTestManager(t)
	defer cleanup()

	m.settings.Query.Hybrid.Enabled = true
	m.settings.Query.Hybrid.VectorWeight = 0
	m.settings.Query.Hybrid.TextWeight = 1
	m.settings.Query.MinScore = 0

	dir := filepath.Dir(m.dbPath)
	content := "# 记录\n\nVDS15800 支持手动组网模式，默认关闭。\n"
	if err := os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("写 MEMORY.md 失败: %v", err)
	}

	if err := m.Sync(context.Background(), "unit", true); err != nil {
		t.Fatalf("同步失败: %v", err)
	}

	results, err := m.Search(context.Background(), "VDS15800 组网", SearchOpts{MinScore: 0})
	if err != nil {
		t.Fatalf("检索失败: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("中文查询应命中至少一条（索引侧未写分词文本？）")
	}
	if results[0].Path != "MEMORY.md" {
		t.Errorf("命中文件 = %q, want MEMORY.md", results[0].Path)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
cd /tmp/wt-memory-fix
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/memory/lite/ -run 'TestSearch_中文关键词命中' -v 2>&1 | tail -10
```
Expected: FAIL `中文查询应命中至少一条` —— 因为 FTS 里存的还是原文，`"组网"` 匹配不到 `"组网模式"`。

- [ ] **Step 3: 改 `indexChunkWithTx` 的 FTS 写入**

把该函数里的 FTS 段替换为：

```go
	// FTS5 全文搜索索引写入。
	// 写入的是**分词后**的文本（空格拼接），原文仍留在 chunks.text 供 snippet/展示；
	// FTS 表是 contentless，文本本来就是我们自己填的，可以分离。分词不可用时写原文。
	if m.ftsAvailable && rowid > 0 {
		ftsText := joinSegments(segmentForFTS(chunk.Text))
		if ftsText == "" {
			ftsText = chunk.Text
		}
		_, err := db.Exec(fmt.Sprintf("INSERT OR REPLACE INTO %s (rowid, id, path, source, text) VALUES (?, ?, ?, ?, ?)", ftsTable),
			rowid, chunkID, filePath, source, ftsText)
		if err != nil {
			logger.Debug(logComponent).Err(err).Msg("failed to insert FTS5")
		}
	}
```

- [ ] **Step 4: 跑测试确认通过**

```bash
cd /tmp/wt-memory-fix
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/memory/lite/ -run 'TestSearch_中文关键词命中' -v 2>&1 | tail -10
```
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
cd /tmp/wt-memory-fix
git add internal/agentcore/memory/lite/manager_impl.go internal/agentcore/memory/lite/manager_search_zh_test.go
git commit -m "feat(memory): 索引侧写入分词文本，chunks 保留原文"
```

---

## Task 6: 查询侧 `BuildFTSQuery` 改 AND + 截断 + 降级 OR

**Files:**
- Modify: `internal/agentcore/memory/lite/internal.go:110-124`（`BuildFTSQuery`）
- Test: `internal/agentcore/memory/lite/internal_test.go`（追加）

- [ ] **Step 1: 写失败测试**

追加到 `internal_test.go`：

```go
func TestBuildFTSQuery_AND与截断(t *testing.T) {
	if segmenterID() == "none" {
		t.Skip("分词器不可用，跳过")
	}

	got := BuildFTSQuery("VDS15800 组网")
	want := `"vds15800" AND "组网"`
	if got != want {
		t.Errorf("BuildFTSQuery = %q, want %q", got, want)
	}

	// 超过 5 个 token 截断
	got = BuildFTSQuery("一 二 三 四 五 六 七")
	if n := strings.Count(got, "AND"); n != maxFTSTokens-1 {
		t.Errorf("截断后应有 %d 个 AND，实际 %d（query=%q）", maxFTSTokens-1, n, got)
	}
}

func TestBuildFTSQuery_空查询(t *testing.T) {
	if got := BuildFTSQuery("   "); got != "" {
		t.Errorf("空查询应返回空串，实际 %q", got)
	}
	if got := BuildFTSQuery("，。！"); got != "" {
		t.Errorf("纯标点应返回空串，实际 %q", got)
	}
}

// TestBuildFTSQuery_降级回OR 分词器不可用时退回正则切分 + OR
func TestBuildFTSQuery_降级回OR(t *testing.T) {
	prevFn, prevOnce, prevInst := segmenterLoadFn, segOnce, segInst
	defer func() { segmenterLoadFn, segOnce, segInst = prevFn, prevOnce, prevInst }()
	segmenterLoadFn = func() (segmenterHandle, error) { return nil, errors.New("offline") }
	segOnce = sync.Once{}
	segInst = nil

	got := BuildFTSQuery("VDS15800 组网")
	want := `"VDS15800" OR "组网"`
	if got != want {
		t.Errorf("降级 BuildFTSQuery = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
cd /tmp/wt-memory-fix
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/memory/lite/ -run 'TestBuildFTSQuery' -v 2>&1 | tail -12
```
Expected: FAIL —— 现状是 `"VDS15800" OR "组网"`，与 AND 断言不符。

- [ ] **Step 3: 改 `BuildFTSQuery`**

`internal.go` 整段替换：

```go
// BuildFTSQuery 把查询串转成 FTS5 MATCH 表达式。
//
// 优先走分词（索引侧同样分词，两侧必须一致）：
//   - 多 token 用 AND（文本侧只占 0.3 权重，语义召回交给向量侧，这里要精确）
//   - 最多 maxFTSTokens 个 token（过多会让 AND 召回过窄）
//
// 分词器不可用时降级为旧行为：正则切分 + OR（中文没有整词 token，用 AND 会全空）。
func BuildFTSQuery(query string) string {
	cleaned := strings.TrimSpace(query)
	if cleaned == "" {
		return ""
	}

	tokens := segmentForFTS(cleaned)
	op := " AND "
	if len(tokens) == 0 {
		tokens = regexp.MustCompile(`[\p{L}\p{N}_]+`).FindAllString(cleaned, maxFTSTokens)
		op = " OR "
	}
	if len(tokens) == 0 {
		return ""
	}
	if len(tokens) > maxFTSTokens {
		tokens = tokens[:maxFTSTokens]
	}

	parts := make([]string, len(tokens))
	for i, tok := range tokens {
		parts[i] = `"` + tok + `"`
	}
	return strings.Join(parts, op)
}
```

- [ ] **Step 4: 跑测试确认通过**

```bash
cd /tmp/wt-memory-fix
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/memory/lite/ -run 'TestBuildFTSQuery|TestSearch_中文关键词命中' -v 2>&1 | tail -14
```
Expected: 全 PASS（含中文检索端到端用例）。

- [ ] **Step 5: 提交**

```bash
cd /tmp/wt-memory-fix
git add internal/agentcore/memory/lite/internal.go internal/agentcore/memory/lite/internal_test.go
git commit -m "feat(memory): 关键词检索改 AND 连接并截断前 5 个 token，降级回 OR"
```

---

## Task 7: 可观测性收尾（日志字段 + doc.go + 调用 InitMemorySegmenterAsync）

**Files:**
- Modify: `internal/agentcore/memory/lite/manager_impl.go`（`initializeProvider` 的 info 日志）
- Modify: `internal/agentcore/memory/lite/segmenter.go`（确认事件名）
- Modify: `internal/agentcore/harness/deep_agent.go` 或进程启动处（调用 `lite.InitMemorySegmenterAsync()`）
- Test: 无新增（日志断言成本高于价值）

- [ ] **Step 1: `embedding provider initialized` 改打实例值**

`initializeProvider` 里那条日志：

```go
	providerID, providerModel := "", ""
	if m.provider != nil {
		providerID = m.provider.ID()
		providerModel = m.provider.Model()
	}
	logger.Info(logComponent).
		Str("provider", providerID).
		Str("model", providerModel).
		Msg("embedding provider initialized")
```

- [ ] **Step 2: 预热分词器**

改 `internal/agentcore/harness/rails/memory/memory_rail.go` 的 `Init`（该文件已 import lite，不会引入循环依赖）：在 `Init` 函数体的**第一行**插入下面两行，函数其余语句保持不动。

```go
	// 中文分词词典加载约 843ms，后台预热，避免落在用户第一次 memory_search 上
	lite.InitMemorySegmenterAsync()
```

- [ ] **Step 3: 编译 + 跑整包**

```bash
cd /tmp/wt-memory-fix
go build ./... 2>&1 | head -5
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/memory/lite/ ./internal/agentcore/harness/ 2>&1 | tail -5
```
Expected: 无输出（build 干净）+ 两个包 ok。

- [ ] **Step 4: 提交**

```bash
cd /tmp/wt-memory-fix
git add internal/agentcore/memory/lite/manager_impl.go internal/agentcore/harness/factory.go
git commit -m "chore(memory): provider 日志打实例值，进程启动预热分词器"
```

---

## Task 8: 集成用例 3 条 + 门禁复跑

**Files:**
- Modify: `tests/integration/agentcore/harness/rails/memory/memory_rail_test.go`（追加 3 个用例）

- [ ] **Step 1: 写集成用例（中文端到端）**

追加到 `MemoryRailSuite`：

```go
// TestMemoryRail_中文检索命中 端到端：写入中文记忆 → memory_search 命中。
// embedding 用 httptest 词袋，避免依赖真实 API。
func (s *MemoryRailSuite) TestMemoryRail_中文检索命中() {
	srv := newBagOfWordsEmbeddingServer(s.T())
	defer srv.Close()

	rail := memory.NewMemoryRail(&apiEmbedding.EmbeddingConfig{
		ModelName: "bag",
		BaseURL:   srv.URL,
		APIKey:    "test",
	}, false)

	root := s.T().TempDir()
	ws := workspace.NewWorkspace(root, "cn")
	s.Require().NoError(os.MkdirAll(root, 0o755))
	sysOp, err := sys_operation.NewSysOperation(sys_operation.NewSysOperationCard(
		sys_operation.WithSysOpMode(sys_operation.OperationModeLocal),
		sys_operation.WithSysOpWorkConfig(&sys_operation.LocalWorkConfig{RestrictToSandbox: true}),
	))
	s.Require().NoError(err)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("ok"))
	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Workspace:    ws,
		SysOperation: sysOp,
		Rails:        []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	initCtx, err := agent.EnsureInitialized(s.Ctx)
	s.Require().NoError(err)

	writeTool := s.toolByName(agent, "write_memory")
	wres, err := writeTool.Invoke(initCtx, map[string]any{
		"path":    "MEMORY.md",
		"content": "- VDS15800 支持手动组网模式，默认关闭\n",
	})
	s.Require().NoError(err)
	s.Equal(true, wres["success"], "写入应成功: %+v", wres)

	searchTool := s.toolByName(agent, "memory_search")
	sres, err := searchTool.Invoke(initCtx, map[string]any{"query": "VDS15800 组网", "min_score": 0})
	s.Require().NoError(err)
	results, ok := sres["results"].([]any)
	s.Require().True(ok, "results 应为数组: %+v", sres)
	s.NotEmpty(results, "中文查询应命中（分词/索引/token 对齐任一环节坏了都会空）")
}
```

需要的辅助函数（同文件末尾）：

```go
// newBagOfWordsEmbeddingServer 词袋 embedding：把文本按空白/标点切词后做确定性哈希向量。
// 对齐先例 tests/integration/agentcore/harness/rails/memory/coding_memory_rail_recall_test.go。
func newBagOfWordsEmbeddingServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input any    `json:"input"`
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		texts := []string{}
		switch v := req.Input.(type) {
		case string:
			texts = append(texts, v)
		case []any:
			for _, item := range v {
				if s2, ok := item.(string); ok {
					texts = append(texts, s2)
				}
			}
		}
		data := make([]map[string]any, 0, len(texts))
		for i, text := range texts {
			data = append(data, map[string]any{"index": i, "embedding": bagVector(text, 64)})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
}

// bagVector 64 维词袋向量（归一化），保证同一 token 落在同一维度。
func bagVector(text string, dims int) []float64 {
	vec := make([]float64, dims)
	for _, tok := range strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		h := fnv.New32a()
		_, _ = h.Write([]byte(strings.ToLower(tok)))
		vec[int(h.Sum32())%dims]++
	}
	return vec
}
```

- [ ] **Step 2: 跑集成用例**

```bash
cd /tmp/wt-memory-fix
CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/agentcore/harness/rails/memory/ -run 'TestMemoryRailSuite/TestMemoryRail_中文检索命中' -v 2>&1 | tail -12
```
Expected: PASS。若失败先看是分词没生效（`segmenterID()=="none"`）还是 embedding 端点未被调用。

- [ ] **Step 3: 写降级用例**

```go
// TestMemoryRail_分词降级仍可英文检索 分词器不可用时退回「正则切分 + OR」，
// 记忆检索整体仍可用（只是中文退化为仅靠向量召回）。
func (s *MemoryRailSuite) TestMemoryRail_分词降级仍可英文检索() {
	s.T().Setenv("UAPCLAW_DISABLE_SEGMENTER", "1")

	srv := newBagOfWordsEmbeddingServer(s.T())
	defer srv.Close()

	rail := memory.NewMemoryRail(&apiEmbedding.EmbeddingConfig{
		ModelName: "bag",
		BaseURL:   srv.URL,
		APIKey:    "test",
	}, false)

	root := s.T().TempDir()
	ws := workspace.NewWorkspace(root, "cn")
	s.Require().NoError(os.MkdirAll(root, 0o755))
	sysOp, err := sys_operation.NewSysOperation(sys_operation.NewSysOperationCard(
		sys_operation.WithSysOpMode(sys_operation.OperationModeLocal),
		sys_operation.WithSysOpWorkConfig(&sys_operation.LocalWorkConfig{RestrictToSandbox: true}),
	))
	s.Require().NoError(err)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("ok"))
	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Workspace:     ws,
		SysOperation:  sysOp,
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	initCtx, err := agent.EnsureInitialized(s.Ctx)
	s.Require().NoError(err)

	writeTool := s.toolByName(agent, "write_memory")
	wres, err := writeTool.Invoke(initCtx, map[string]any{
		"path":    "MEMORY.md",
		"content": "- VDS15800 支持手动组网模式\n",
	})
	s.Require().NoError(err)
	s.Equal(true, wres["success"], "写入应成功: %+v", wres)

	searchTool := s.toolByName(agent, "memory_search")
	sres, err := searchTool.Invoke(initCtx, map[string]any{"query": "VDS15800", "min_score": 0})
	s.Require().NoError(err)
	results, ok := sres["results"].([]any)
	s.Require().True(ok, "results 应为数组: %+v", sres)
	s.NotEmpty(results, "降级后英文/数字 token 仍应命中（正则切分 + OR）")
}
```

> 说明（与 spec 的偏差）：spec 里的集成第 2 条「配置变化自愈」不再单独写集成用例 ——
> A/B 的判定与重建在 Task 1/3 已经用**真实 sqlite + 真实 meta 表**覆盖（`manager_reindex_test.go`、
> `vec_table_test.go`），再走一遍 rail 装配不增加信息量；rail 层只保留「中文命中」与「降级」两条。

- [ ] **Step 4: 给降级用例提供注入点**

`segmenter.go` 的 `loadSegmenter` 开头加：

```go
	// 降级开关：集成测试与线上紧急规避（分词出问题时）共用
	if os.Getenv("UAPCLAW_DISABLE_SEGMENTER") == "1" {
		return nil, errSegmenterUnavailable
	}
```
（`os` 加到 import）

- [ ] **Step 5: 跑集成 + 全量回归 + 门禁**

```bash
cd /tmp/wt-memory-fix
CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/agentcore/harness/rails/memory/ -v 2>&1 | tail -20
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/... 2>&1 | grep -v "^ok\|no test files" | head -5
CGO_ENABLED=1 go test -mod=mod -tags "sqlite_fts5 test integration" -coverprofile=/tmp/integ.out ./tests/integration/... 2>&1 | grep -v "^ok" | head -5
bash scripts/check-changed-line-coverage.sh /tmp/integ.out origin/main 5 .integration-coverage-exempt | tail -8
```
Expected: 集成改动行门禁 `通过`；若 `memory/lite/segmenter.go` 报 0%，说明没有集成用例真正驱动分词（检查 `TestMemoryRail_中文检索命中` 是否被 `-run` 过滤掉）。

- [ ] **Step 6: 提交并推送**

```bash
cd /tmp/wt-memory-fix
git add tests/integration/agentcore/harness/rails/memory/ internal/agentcore/memory/lite/segmenter.go
git commit -m "test(integration): 中文检索命中与分词降级链路"
git push origin HEAD:main
```
推送后按项目规则 6 追 CI：`gh run list --limit 3` → `gh run watch <id>`，四个 job 全绿才算完成。

---

## 范围外（明确不做）

- `internal/swarm/agents/harness/common/memory`（另一套并行实现，被 adapter/avatar/memoryrpc 使用）：本计划不动；如果线上走的是这条链路，需要单独开 spec。
- Python 侧同步改造（分词 / AND / 维度自愈）：Go 独有的分叉，已登记在 spec「风险与偏差」。
