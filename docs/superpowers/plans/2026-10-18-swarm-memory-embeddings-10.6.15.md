# Swarm Memory 补齐（10.6.15 Embeddings + 依赖链）实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 `internal/swarm/agents/harness/common/memory/` 包内一比一对齐 Python `jiuwenswarm/agents/harness/common/memory/` 的 types/internal/config/embeddings/manager 五个文件，并回填 adapter 三处调用点。

**Architecture:** swarm 侧独立完整实现（B1 方案）——Python 侧 swarm memory 是 agentcore lite 的完整 fork（零 openjiuwen import），Go 镜像此结构，两包各自对齐各自基线。唯一共享点：vec0 加载器复用 `lite.ResolveVec0Path` + `lite.LoadVec0Extension`（Python 两版都调第三方库 sqlite_vec）。manager 用 workspace_dir 字符串 API + sync.Map INDEX_CACHE；embeddings 用环境变量链 + config.yaml 自查 + Mock 降级；config 用包内私有缓存 + ClearConfigCache。

**Tech Stack:** Go 1.22+, database/sql + mattn/go-sqlite3（含 RETURNING/vec_f32 SQL）, fsnotify 文件监听, net/http 直调（60s 超时无重试）, config.New("").Load()

**设计文档:** `docs/superpowers/specs/2026-10-18-swarm-memory-embeddings-10.6.15-design.md`

**对齐审计要点（全程遵守）:**
1. 初始化顺序不可调换：resolveDBPath → openDatabase → ensureSchema → initializeProvider → loadVectorExtension → Sync("initial") → setupFileWatcher → ensureIntervalSync
2. minScore 陷阱：Python 代码 `get("minScore", 0.7)` 但 dataclass 默认 0.3，实际生效 0.3——Go 按 settings 值实现并注释
3. `updated_at` 语义：Python 用 `asyncio.get_event_loop().time()`（事件循环单调秒），Go 用 `time.Now().Unix()` 等价实现（同为单调递增秒值）
4. Mock 无法复刻 Mersenne Twister：Go 用哈希展开近似，注释说明值不等价但确定性+128维
5. `_is_recent_session_file` 在 Python 中定义但从未被调用——Go 同步实现（保持 1:1）并注释
6. 日志逐条对照 Python logger 调用补齐（规则 3）
7. 编译规则：`export GOPROXY=https://goproxy.cn,direct`；编译前 `pgrep -f 'go (build|test)'` 检查残留；不清理缓存

---

## 文件结构

### 新建文件

| 文件 | 职责 |
|------|------|
| `internal/swarm/agents/harness/common/memory/types.go` | MemorySearchResult/FileEntry/MemoryChunk/FtsStatus/VectorStatus 等 dataclass 对齐 |
| `internal/swarm/agents/harness/common/memory/internal.go` | 纯函数：ListMemoryFiles(递归)/ChunkMarkdown/HashText/BuildFTSQuery/Bm25RankToScore/IsMemoryPath(swarm语义)/CosineSimilarity 等 |
| `internal/swarm/agents/harness/common/memory/config.go` | GetEmbedConfig/MemorySettings/CreateMemorySettings/ResolveModeMemory/IsMemoryEnabled/IsProactiveMemory/GetMemoryMode + 包内配置缓存 |
| `internal/swarm/agents/harness/common/memory/embeddings.go` | EmbeddingProvider 接口/OpenAICompatible(net/http)/Mock/CreateEmbeddingProvider 工厂 |
| `internal/swarm/agents/harness/common/memory/manager.go` | MemoryIndexManager 全套（schema/sync/search/watcher/缓存/Status/Close）+ GetMemoryManager/ClearMemoryManagerCache |
| 上述 5 个文件各配 `_test.go` | 覆盖率 ≥85% |

### 修改文件

| 文件 | 修改内容 |
|------|---------|
| `internal/swarm/agents/harness/common/memory/doc.go` | 补文件目录 |
| `internal/swarm/server/adapter/deep_adapter.go:652-655` | lite.ClearMemoryManagerCache → commmem.ClearMemoryManagerCache + 补 ClearConfigCache |
| `internal/swarm/server/adapter/deep_adapter_rails.go:1419-1540` | 删除私有 getMemoryMode/isMemoryEnabled/isProactiveMemory/resolveModeMemory，调用点改 commmem.*（isBuiltinMemoryAllowed/getMemoryEngine/isExternalMemoryEnabled 保留，属 10.6.16） |
| `IMPLEMENTATION_PLAN.md:711` | 10.6.13-18 状态更新 |

---

## Task 1: types.go + internal.go（纯函数层）

**Files:**
- Create: `internal/swarm/agents/harness/common/memory/types.go`
- Create: `internal/swarm/agents/harness/common/memory/internal.go`
- Test: `internal/swarm/agents/harness/common/memory/internal_test.go`

Python 参考：`/home/opensource/jiuwenswarm-develop/jiuwenswarm/agents/harness/common/memory/types.py`（79行）+ `internal.py`（215行）

- [ ] **Step 1: 写 internal_test.go 失败测试（先测纯函数）**

```go
package memory

import (
	"os"
	"path/filepath"
	"testing"
)

// TestEstimateTokens 对齐 Python estimate_tokens：len//4
func TestEstimateTokens(t *testing.T) {
	if EstimateTokens("") != 0 {
		t.Errorf("空串应为 0")
	}
	if EstimateTokens("12345678") != 2 {
		t.Errorf("8字符应为2 token")
	}
}

// TestListMemoryFiles_递归与根文件 对齐 swarm 语义（与 lite 非递归不同）
func TestListMemoryFiles_递归与根文件(t *testing.T) {
	dir := t.TempDir()
	// MEMORY.md 根文件
	_ = os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("# m"), 0o644)
	// memory/ 子目录递归
	_ = os.MkdirAll(filepath.Join(dir, "memory", "sub"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "memory", "a.md"), []byte("a"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "memory", "sub", "b.md"), []byte("b"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "memory", "c.txt"), []byte("c"), 0o644) // 非 md 排除

	files := ListMemoryFiles(dir, nil)
	if len(files) != 3 {
		t.Fatalf("应找到 3 个文件（MEMORY.md+a.md+sub/b.md），got %v", files)
	}
}

// TestListMemoryFiles_大小写变体 根目录 memory.md 也识别
func TestListMemoryFiles_大小写变体(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "memory.md"), []byte("# m"), 0o644)
	files := ListMemoryFiles(dir, nil)
	if len(files) != 1 {
		t.Fatalf("应找到 memory.md，got %v", files)
	}
}

// TestChunkMarkdown_基本分块 对齐 Python chunk_markdown：target_tokens=4, overlap=0
func TestChunkMarkdown_基本分块(t *testing.T) {
	content := "line1\nline2\nline3\nline4\n"
	chunks := ChunkMarkdown(content, map[string]any{"tokens": 4, "overlap": 0})
	if len(chunks) < 2 {
		t.Fatalf("8字符4行应至少2块，got %d", len(chunks))
	}
	if chunks[0].StartLine != 1 {
		t.Errorf("首块 startLine 应为 1")
	}
}

// TestHashText 对齐 Python sha256 hexdigest[:16]
func TestHashText(t *testing.T) {
	h := HashText("hello")
	if len(h) != 16 {
		t.Fatalf("hash 长度应为 16，got %d", len(h))
	}
	if h != HashText("hello") {
		t.Errorf("确定性失败")
	}
	if h == HashText("world") {
		t.Errorf("不同输入应不同 hash")
	}
}

// TestBuildFTSQuery 对齐 Python：\w+ 分词前 10 个 OR 拼接
func TestBuildFTSQuery(t *testing.T) {
	if BuildFTSQuery("  ") != "" {
		t.Errorf("空查询应为空串")
	}
	q := BuildFTSQuery("hello world")
	if q != `"hello" OR "world"` {
		t.Errorf("got %s", q)
	}
}

// TestBm25RankToScore 对齐 Python：正负 rank 两种公式
func TestBm25RankToScore(t *testing.T) {
	if Bm25RankToScore(0) != 1.0 {
		t.Errorf("rank=0 应为 1.0")
	}
	if Bm25RankToScore(1) != 0.5 {
		t.Errorf("rank=1 应为 0.5")
	}
	if Bm25RankToScore(-1) != 0.5 {
		t.Errorf("rank=-1 应为 0.5（1/(1-(-1))）")
	}
}

// TestIsMemoryPath_swarm语义 对齐 Python：basename ∈ {memory.md, memory} 或路径含 memory 段
func TestIsMemoryPath_swarm语义(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"memory/MEMORY.md", true},      // basename memory.md（小写化）
		{"MEMORY.md", true},              // basename 为 memory（无扩展名场景）
		{"notes/memory/x.txt", true},     // 路径段含 memory
		{"a/b/c.md", false},              // 不含 memory 段
		{`a\memory\d.md`, true},          // 反斜杠归一化
	}
	for _, c := range cases {
		if got := IsMemoryPath(c.path); got != c.want {
			t.Errorf("IsMemoryPath(%q)=%v want %v", c.path, got, c.want)
		}
	}
}

// TestParseEmbedding 三态解析
func TestParseEmbedding(t *testing.T) {
	if ParseEmbedding([]any{1.0, 2.0}) == nil {
		t.Errorf("list 应直接返回")
	}
	if ParseEmbedding(map[string]any{"embedding": []any{1.0}}) == nil {
		t.Errorf("dict.embedding 应递归")
	}
	if ParseEmbedding(map[string]any{"data": []any{map[string]any{"embedding": []any{1.0}}}}) == nil {
		t.Errorf("dict.data[0].embedding 应提取")
	}
	if ParseEmbedding("nope") != nil {
		t.Errorf("无法解析返回 nil")
	}
}

// TestCosineSimilarity 对齐 Python
func TestCosineSimilarity(t *testing.T) {
	if CosineSimilarity([]float64{1, 0}, []float64{1, 0}) != 1.0 {
		t.Errorf("同向应为 1")
	}
	if CosineSimilarity([]float64{1, 0}, []float64{0, 1}) != 0.0 {
		t.Errorf("正交应为 0")
	}
	if CosineSimilarity([]float64{1}, []float64{1, 2}) != 0.0 {
		t.Errorf("维度不等应为 0")
	}
	if CosineSimilarity([]float64{0, 0}, []float64{0, 0}) != 0.0 {
		t.Errorf("零向量应为 0")
	}
}

// TestNormalizeExtraMemoryPaths 相对路径 join workspace
func TestNormalizeExtraMemoryPaths(t *testing.T) {
	got := NormalizeExtraMemoryPaths([]string{"rel", "/abs"}, "/ws")
	if len(got) != 2 || got[0] != filepath.Join("/ws", "rel") || got[1] != "/abs" {
		t.Fatalf("got %v", got)
	}
	if NormalizeExtraMemoryPaths(nil, "/ws") != nil {
		t.Errorf("nil 返回 nil")
	}
}

// TestBuildFileEntry 对齐 Python build_file_entry
func TestBuildFileEntry(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "MEMORY.md")
	_ = os.WriteFile(p, []byte("content"), 0o644)

	entry, err := BuildFileEntry(p, dir)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if entry.Path != "MEMORY.md" {
		t.Errorf("相对路径 got %s", entry.Path)
	}
	if entry.Hash != HashText("content") {
		t.Errorf("hash 应等于内容 hash")
	}
	if entry.Size != 7 {
		t.Errorf("size got %d", entry.Size)
	}
	if entry.MtimeMs <= 0 {
		t.Errorf("mtimeMs 应为正")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway && pgrep -f 'go (build|test)' ; export GOPROXY=https://goproxy.cn,direct && go test ./internal/swarm/agents/harness/common/memory/ -run 'TestEstimate|TestListMemory|TestChunk|TestHash|TestBuildFTS|TestBm25|TestIsMemoryPath|TestParseEmbedding|TestCosine|TestNormalize|TestBuildFile' -v 2>&1 | tail -5
```
Expected: FAIL（`undefined: EstimateTokens` 等编译错误）

- [ ] **Step 3: 写 types.go**

```go
package memory

// ──────────────────────────── 结构体 ────────────────────────────

// MemorySearchResult 记忆搜索结果。对齐 Python MemorySearchResult
type MemorySearchResult struct {
	// ID chunk 标识
	ID string `json:"id"`
	// Path 相对路径
	Path string `json:"path"`
	// Source 来源（memory/sessions）
	Source string `json:"source"`
	// StartLine 起始行
	StartLine int `json:"startLine"`
	// EndLine 结束行
	EndLine int `json:"endLine"`
	// Snippet 文本摘要
	Snippet string `json:"snippet"`
	// Score 相关度分数
	Score float64 `json:"score"`
	// Citation 引用标注
	Citation *string `json:"citation,omitempty"`
}

// MemoryProviderStatus 嵌入提供者状态。对齐 Python MemoryProviderStatus
type MemoryProviderStatus struct {
	// Available 是否可用
	Available bool `json:"available"`
	// Provider 提供者 ID
	Provider *string `json:"provider,omitempty"`
	// Model 模型名
	Model *string `json:"model,omitempty"`
	// Error 错误信息
	Error *string `json:"error,omitempty"`
}

// MemorySyncProgressUpdate 同步进度更新。对齐 Python MemorySyncProgressUpdate
type MemorySyncProgressUpdate struct {
	// Phase 阶段
	Phase string `json:"phase"`
	// Current 当前
	Current int `json:"current"`
	// Total 总数
	Total int `json:"total"`
	// Message 消息
	Message *string `json:"message,omitempty"`
}

// FileEntry 文件条目。对齐 Python FileEntry（MemoryFileEntry/SessionFileEntry 为同结构）
type FileEntry struct {
	// Path 相对路径
	Path string `json:"path"`
	// AbsPath 绝对路径
	AbsPath string `json:"absPath"`
	// Hash 内容 hash
	Hash string `json:"hash"`
	// MtimeMs 修改时间毫秒
	MtimeMs int64 `json:"mtimeMs"`
	// Size 文件大小
	Size int64 `json:"size"`
}

// MemoryChunk 记忆分块。对齐 Python MemoryChunk
type MemoryChunk struct {
	// Text 文本内容
	Text string `json:"text"`
	// StartLine 起始行
	StartLine int `json:"startLine"`
	// EndLine 结束行
	EndLine int `json:"endLine"`
}

// FtsStatus FTS5 全文搜索状态。对齐 Python FtsStatus
type FtsStatus struct {
	// Enabled 是否启用
	Enabled bool `json:"enabled"`
	// Available 是否可用
	Available bool `json:"available"`
	// Error 错误信息
	Error *string `json:"error,omitempty"`
}

// VectorStatus 向量搜索状态。对齐 Python VectorStatus
type VectorStatus struct {
	// Enabled 是否启用
	Enabled bool `json:"enabled"`
	// Available 是否可用
	Available bool `json:"available"`
	// Error 错误信息
	Error *string `json:"error,omitempty"`
	// Dims 维度
	Dims *int `json:"dims,omitempty"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// 记忆来源常量。对齐 Python MemorySource Literal
const (
	// MemorySourceMemory 记忆文件来源
	MemorySourceMemory = "memory"
	// MemorySourceSessions 会话记录来源
	MemorySourceSessions = "sessions"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 4: 写 internal.go**

```go
package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ftsWordPattern FTS 分词正则。对齐 Python re.findall(r'\w+')
var ftsWordPattern = regexp.MustCompile(`\w+`)

// memoryFilePattern 会话文件日期模式（对齐 _is_recent_session_file 的正则）
var memoryFilePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\.md$`)

// ──────────────────────────── 导出函数 ────────────────────────────

// EstimateTokens 估算文本 token 数。对齐 Python estimate_tokens：len//4
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	return len(text) / 4
}

// EnsureDir 确保目录存在。对齐 Python ensure_dir
func EnsureDir(path string) error {
	if path == "" {
		return nil
	}
	return os.MkdirAll(path, 0o755)
}

// ListMemoryFiles 列出工作区全部记忆文件。对齐 Python list_memory_files（swarm 语义）：
// 根目录 MEMORY.md/memory.md + memory/ 子目录递归遍历 .md + extraPaths（文件或递归目录）。
// 注意：与 agentcore lite 版（非递归+daily_memory+USER.md）语义不同，本包对齐 swarm 基线。
func ListMemoryFiles(workspaceDir string, extraPaths []string) []string {
	files := make([]string, 0)

	for _, filename := range []string{"MEMORY.md", "memory.md"} {
		fp := filepath.Join(workspaceDir, filename)
		if st, err := os.Stat(fp); err == nil && !st.IsDir() {
			files = append(files, fp)
		}
	}

	memoryDir := filepath.Join(workspaceDir, "memory")
	if st, err := os.Stat(memoryDir); err == nil && st.IsDir() {
		_ = filepath.Walk(memoryDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if !info.IsDir() && strings.HasSuffix(info.Name(), ".md") {
				files = append(files, path)
			}
			return nil
		})
	}

	for _, extra := range extraPaths {
		full := filepath.Join(workspaceDir, extra)
		if st, err := os.Stat(full); err == nil && !st.IsDir() {
			files = append(files, full)
		} else if st != nil && st.IsDir() {
			_ = filepath.Walk(full, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return nil
				}
				if !info.IsDir() && strings.HasSuffix(info.Name(), ".md") {
					files = append(files, path)
				}
				return nil
			})
		}
	}

	// 对齐 Python sorted(set(files))
	seen := make(map[string]struct{}, len(files))
	unique := files[:0]
	for _, f := range files {
		if _, ok := seen[f]; !ok {
			seen[f] = struct{}{}
			unique = append(unique, f)
		}
	}
	sort.Strings(unique)
	return unique
}

// BuildFileEntry 构建文件索引条目。对齐 Python build_file_entry
func BuildFileEntry(absPath, workspaceDir string) (*FileEntry, error) {
	st, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("stat 文件失败: %w", err)
	}
	content, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}
	rel, err := filepath.Rel(workspaceDir, absPath)
	if err != nil {
		rel = absPath
	}
	return &FileEntry{
		Path:    rel,
		AbsPath: absPath,
		Hash:    HashText(string(content)),
		MtimeMs: st.ModTime().UnixMilli(),
		Size:    st.Size(),
	}, nil
}

// ChunkMarkdown 按 token 估算分块。对齐 Python chunk_markdown（逐行累加+尾部回溯 overlap）
func ChunkMarkdown(content string, settings map[string]any) []MemoryChunk {
	targetTokens := 256
	overlap := 32
	if v, ok := settings["tokens"].(int); ok {
		targetTokens = v
	}
	if v, ok := settings["overlap"].(int); ok {
		overlap = v
	}

	lines := strings.Split(content, "\n")
	var chunks []MemoryChunk
	var currentChunk []string
	currentTokens := 0
	startLine := 1

	for i, line := range lines {
		lineTokens := EstimateTokens(line)

		if currentTokens+lineTokens > targetTokens && len(currentChunk) > 0 {
			chunks = append(chunks, MemoryChunk{
				Text:      strings.Join(currentChunk, "\n"),
				StartLine: startLine,
				EndLine:   i, // 对齐 Python endLine = i - 1（i 从 0 起，Python 从 1 起）
			})

			// 尾部回溯构造 overlap
			var overlapLines []string
			overlapTokens := 0
			for j := len(currentChunk) - 1; j >= 0; j-- {
				lt := EstimateTokens(currentChunk[j])
				if overlapTokens+lt > overlap {
					break
				}
				overlapLines = append([]string{currentChunk[j]}, overlapLines...)
				overlapTokens += lt
			}
			currentChunk = overlapLines
			currentTokens = overlapTokens
			startLine = i + 1 - len(overlapLines)
		}

		currentChunk = append(currentChunk, line)
		currentTokens += lineTokens
	}

	if len(currentChunk) > 0 {
		chunks = append(chunks, MemoryChunk{
			Text:      strings.Join(currentChunk, "\n"),
			StartLine: startLine,
			EndLine:   len(lines),
		})
	}
	return chunks
}

// HashText 生成文本 hash。对齐 Python hash_text：sha256 hexdigest[:16]
func HashText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])[:16]
}

// ParseEmbedding 解析 API 响应中的嵌入向量。对齐 Python parse_embedding（list/dict.embedding/dict.data[0].embedding 三态）
func ParseEmbedding(data any) []float64 {
	switch v := data.(type) {
	case []any:
		out := make([]float64, 0, len(v))
		for _, item := range v {
			if f, ok := item.(float64); ok {
				out = append(out, f)
			}
		}
		return out
	case []float64:
		return v
	case map[string]any:
		if emb, ok := v["embedding"]; ok {
			return ParseEmbedding(emb)
		}
		if dataArr, ok := v["data"].([]any); ok && len(dataArr) > 0 {
			if first, ok := dataArr[0].(map[string]any); ok {
				if emb, ok := first["embedding"]; ok {
					return ParseEmbedding(emb)
				}
			}
		}
	}
	return nil
}

// BuildFTSQuery 构造 FTS5 查询。对齐 Python build_fts_query：\w+ 前 10 个 OR 拼接
func BuildFTSQuery(query string) string {
	cleaned := strings.TrimSpace(query)
	if cleaned == "" {
		return ""
	}
	tokens := ftsWordPattern.FindAllString(cleaned, -1)
	if len(tokens) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		quoted = append(quoted, `"`+tok+`"`)
	}
	return strings.Join(quoted, " OR ")
}

// Bm25RankToScore BM25 rank 转相似度分数。对齐 Python bm25_rank_to_score
func Bm25RankToScore(rank float64) float64 {
	if rank >= 0 {
		return 1.0 / (1.0 + rank)
	}
	return 1.0 / (1.0 - rank)
}

// IsMemoryPath 判断路径是否为记忆文件。对齐 swarm 语义（Python is_memory_path）：
// basename 小写 ∈ {memory.md, memory}，或路径任一段小写 == "memory"。
// 注意：与 lite 版（.md 后缀判断）语义不同。
func IsMemoryPath(relPath string) bool {
	base := strings.ToLower(filepath.Base(relPath))
	if base == "memory.md" || base == "memory" {
		return true
	}
	parts := strings.Split(strings.ReplaceAll(relPath, `\`, "/"), "/")
	for _, p := range parts {
		if strings.ToLower(p) == "memory" {
			return true
		}
	}
	return false
}

// NormalizeExtraMemoryPaths 归一化额外记忆路径。对齐 Python normalize_extra_memory_paths
func NormalizeExtraMemoryPaths(paths []string, workspaceDir string) []string {
	if len(paths) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(paths))
	for _, p := range paths {
		if filepath.IsAbs(p) {
			normalized = append(normalized, p)
		} else {
			normalized = append(normalized, filepath.Join(workspaceDir, p))
		}
	}
	return normalized
}

// CosineSimilarity 计算余弦相似度。对齐 Python cosine_similarity
func CosineSimilarity(vec1, vec2 []float64) float64 {
	if len(vec1) != len(vec2) {
		return 0.0
	}
	var dot, n1, n2 float64
	for i := range vec1 {
		dot += vec1[i] * vec2[i]
		n1 += vec1[i] * vec1[i]
		n2 += vec2[i] * vec2[i]
	}
	norm1 := math.Sqrt(n1)
	norm2 := math.Sqrt(n2)
	if norm1 < 1e-10 || norm2 < 1e-10 {
		return 0.0
	}
	return dot / (norm1 * norm2)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

注意：ChunkMarkdown 中 Python 循环 `enumerate(lines, 1)`（i 从 1 起，endLine = i-1），Go 循环 i 从 0 起，故 endLine 直接用 `i`（等价 i_py - 1）；末块 endLine 对齐 Python `len(lines)`。

- [ ] **Step 5: 运行测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/swarm/agents/harness/common/memory/ -run 'TestEstimate|TestListMemory|TestChunk|TestHash|TestBuildFTS|TestBm25|TestIsMemoryPath|TestParseEmbedding|TestCosine|TestNormalize|TestBuildFile' -v 2>&1 | tail -5
```
Expected: 全部 PASS

- [ ] **Step 6: 提交**

```bash
cd /home/opensource/uapclaw-gateway && git add internal/swarm/agents/harness/common/memory/types.go internal/swarm/agents/harness/common/memory/internal.go internal/swarm/agents/harness/common/memory/internal_test.go && git commit -m "feat(swarm-memory): 10.6.15 前置 types + internal 纯函数（swarm 语义）"
```

---

## Task 2: config.go（配置层 + 包内缓存）

**Files:**
- Create: `internal/swarm/agents/harness/common/memory/config.go`
- Test: `internal/swarm/agents/harness/common/memory/config_test.go`

Python 参考：`config.py`（259行）。注意：`_resolve_mode_memory/is_memory_enabled/is_proactive_memory/get_memory_mode` 四函数从 deep_adapter_rails.go 迁移为导出版。

- [ ] **Step 1: 写 config_test.go 失败测试**

```go
package memory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
)

// withTestConfig 写临时 config.yaml 并重定向配置读取（测试辅助）
func withTestConfig(t *testing.T, yamlContent string) string {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("写配置失败: %v", err)
	}
	t.Cleanup(ClearConfigCache)
	return cfgPath
}

// TestGetEmbedConfig_读取embed段 对齐 Python get_embed_config
func TestGetEmbedConfig_读取embed段(t *testing.T) {
	cfgPath := withTestConfig(t, `
embed:
  embed_api_key: sk-test
  embed_base_url: https://api.example.com/v1
  embed_model: text-embedding-v3
`)
	cfg, err := config.New(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = cfg // 触发路径存在校验

	got := GetEmbedConfig()
	if got["api_key"] != "sk-test" || got["base_url"] != "https://api.example.com/v1" || got["model"] != "text-embedding-v3" {
		t.Fatalf("got %v", got)
	}
}

// TestGetEmbedConfig_缺省 对齐 Python：embed 段缺失返回空值 map
func TestGetEmbedConfig_缺省(t *testing.T) {
	ClearConfigCache()
	got := GetEmbedConfig()
	if got == nil {
		t.Fatalf("应返回非 nil map")
	}
	if got["api_key"] != "" || got["base_url"] != "" || got["model"] != "" {
		t.Fatalf("缺失时应为空串，got %v", got)
	}
}

// TestMemorySettings_默认值 对齐 Python MemorySettings dataclass 默认值
func TestMemorySettings_默认值(t *testing.T) {
	ClearConfigCache()
	s := CreateMemorySettings(".", nil)
	if s.Provider != "openai_compatible" || s.Model != "text-embedding-v3" || s.Fallback != "mock" {
		t.Errorf("provider 默认值错误: %+v", s)
	}
	if len(s.Sources) != 2 || s.Sources[0] != "memory" || s.Sources[1] != "sessions" {
		t.Errorf("sources 默认值错误")
	}
	if s.Chunking.Tokens != 256 || s.Chunking.Overlap != 32 {
		t.Errorf("chunking 默认值错误")
	}
	if s.Query.MaxResults != 10 || s.Query.MinScore != 0.3 {
		t.Errorf("query 默认值错误（minScore 应为 0.3）")
	}
	if !s.Query.Hybrid.Enabled || s.Query.Hybrid.VectorWeight != 0.7 || s.Query.Hybrid.TextWeight != 0.3 || s.Query.Hybrid.CandidateMultiplier != 2.0 {
		t.Errorf("hybrid 默认值错误")
	}
	if s.Store.Path != "memory.db" || !s.Store.Vector.Enabled || !s.Store.Fts.Enabled {
		t.Errorf("store 默认值错误")
	}
	if !s.Sync.Watch || s.Sync.WatchDebounceMs != 2000 || !s.Sync.OnSearch || !s.Sync.OnSessionStart || s.Sync.IntervalMinutes != 0 {
		t.Errorf("sync 默认值错误")
	}
	if !s.Cache.Enabled || s.Cache.MaxEntries != 10000 {
		t.Errorf("cache 默认值错误")
	}
}

// TestMemorySettings_memory段覆盖 对齐 Python create_memory_settings 读 config memory 段
func TestMemorySettings_memory段覆盖(t *testing.T) {
	withTestConfig(t, `
memory:
  provider: my_provider
  fallback: none
  sources: [memory]
  chunking:
    tokens: 128
    overlap: 16
embed:
  embed_model: embed-model-x
`)
	s := CreateMemorySettings(".", nil)
	if s.Provider != "my_provider" {
		t.Errorf("provider 覆盖失败")
	}
	if s.Fallback != "none" {
		t.Errorf("fallback 覆盖失败")
	}
	if len(s.Sources) != 1 {
		t.Errorf("sources 覆盖失败")
	}
	if s.Chunking.Tokens != 128 || s.Chunking.Overlap != 16 {
		t.Errorf("chunking 覆盖失败")
	}
	if s.Model != "embed-model-x" {
		t.Errorf("model 应取 embed 段 embed_model")
	}
}

// TestResolveModeMemory_三形态 对齐 Python _resolve_mode_memory
func TestResolveModeMemory_三形态(t *testing.T) {
	cfg := map[string]any{
		"modes": map[string]any{
			"agent": map[string]any{
				"plan": map[string]any{"memory": map[string]any{"enabled": true}},
			},
			"code": map[string]any{"memory": map[string]any{"enabled": false}},
		},
	}
	if m := ResolveModeMemory("agent.plan", cfg); m["enabled"] != true {
		t.Errorf("agent.plan 解析失败: %v", m)
	}
	if m := ResolveModeMemory("plan", cfg); m["enabled"] != true {
		t.Errorf("bare plan 应归 agent 命名空间: %v", m)
	}
	if m := ResolveModeMemory("code", cfg); m["enabled"] != false {
		t.Errorf("code 解析失败: %v", m)
	}
	if m := ResolveModeMemory("nonexistent", cfg); len(m) != 0 {
		t.Errorf("不存在应返回空 map")
	}
}

// TestIsMemoryEnabled 对齐 Python is_memory_enabled
func TestIsMemoryEnabled(t *testing.T) {
	cfg := map[string]any{
		"modes": map[string]any{
			"agent": map[string]any{
				"plan": map[string]any{"memory": map[string]any{"enabled": true}},
			},
		},
	}
	if !IsMemoryEnabled("plan", cfg) {
		t.Errorf("plan 应启用")
	}
	if IsMemoryEnabled("code", cfg) {
		t.Errorf("code 未配置应禁用")
	}
}

// TestIsProactiveMemory 对齐 Python is_proactive_memory
func TestIsProactiveMemory(t *testing.T) {
	cfg := map[string]any{
		"modes": map[string]any{
			"agent": map[string]any{
				"plan": map[string]any{"memory": map[string]any{"is_proactive": true}},
			},
		},
	}
	if !IsProactiveMemory("plan", cfg) {
		t.Errorf("plan 应主动")
	}
}

// TestGetMemoryMode 对齐 Python get_memory_mode：cloud 或 local（默认）
func TestGetMemoryMode(t *testing.T) {
	if GetMemoryMode(nil) != "local" {
		t.Errorf("nil config 默认 local")
	}
	if GetMemoryMode(map[string]any{"memory": map[string]any{"mode": "CLOUD "}}) != "cloud" {
		t.Errorf("大小写+空格归一化失败")
	}
	if GetMemoryMode(map[string]any{"memory": map[string]any{"mode": "local"}}) != "local" {
		t.Errorf("local 应返回 local")
	}
}

// TestConfigCache_缓存与清理 对齐 Python _config_cache + clear_config_cache
func TestConfigCache_缓存与清理(t *testing.T) {
	withTestConfig(t, "embed:\n  embed_model: v1\n")
	if GetEmbedConfig()["model"] != "v1" {
		t.Fatalf("首次读取失败")
	}
	// 缓存生效：改文件不刷新（对齐 Python 缓存语义）
	// （此处不实际改文件，验证 ClearConfigCache 可重置）
	ClearConfigCache()
	got := GetEmbedConfig()
	if got == nil {
		t.Fatalf("清缓存后仍可读取")
	}
}
```

注意：GetEmbedConfig 读全局 config.yaml 路径（`pathutil.ConfigFile()`），测试中通过 `SetUserHome` 或接受读真实配置文件。**实现时优先支持注入**：`GetEmbedConfigWithLoader(loader func() (map[string]any, error))` 仅供测试注入，`GetEmbedConfig()` 用默认 loader。这样测试可以完全隔离。

- [ ] **Step 2: 运行测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/swarm/agents/harness/common/memory/ -run 'TestGetEmbedConfig|TestMemorySettings|TestResolveModeMemory|TestIsMemoryEnabled|TestIsProactiveMemory|TestGetMemoryMode|TestConfigCache' -v 2>&1 | tail -5
```
Expected: FAIL（`undefined: GetEmbedConfig` 等）

- [ ] **Step 3: 写 config.go**

```go
package memory

import (
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ChunkingConfig 分块配置。对齐 Python MemorySettings.chunking
type ChunkingConfig struct {
	// Tokens 目标 token 数，默认 256
	Tokens int
	// Overlap 重叠 token 数，默认 32
	Overlap int
}

// HybridConfig 混合搜索配置。对齐 Python MemorySettings.query.hybrid
type HybridConfig struct {
	// Enabled 是否启用混合，默认 true
	Enabled bool
	// VectorWeight 向量权重，默认 0.7
	VectorWeight float64
	// TextWeight 文本权重，默认 0.3
	TextWeight float64
	// CandidateMultiplier 候选倍数，默认 2.0
	CandidateMultiplier float64
}

// QueryConfig 查询配置。对齐 Python MemorySettings.query
type QueryConfig struct {
	// MaxResults 最大结果数，默认 10
	MaxResults int
	// MinScore 最小分数，默认 0.3
	// 注意：Python search() 代码内 get("minScore", 0.7) 是死默认值——
	// settings 默认 0.3 先生效（runReindex 之前的 settings 构造），实际语义 0.3
	MinScore float64
	// Hybrid 混合搜索配置
	Hybrid HybridConfig
}

// StoreConfig 存储配置。对齐 Python MemorySettings.store
type StoreConfig struct {
	// Path 数据库路径（相对 workspace_dir/memory/），默认 "memory.db"
	Path string
	// VectorEnabled 向量搜索开关，默认 true
	VectorEnabled bool
	// FtsEnabled 全文搜索开关，默认 true
	FtsEnabled bool
}

// SyncConfig 同步配置。对齐 Python MemorySettings.sync
type SyncConfig struct {
	// Watch 文件监听开关，默认 true
	Watch bool
	// WatchDebounceMs 监听防抖毫秒，默认 2000
	WatchDebounceMs int
	// OnSearch 搜索前同步，默认 true
	OnSearch bool
	// OnSessionStart 会话开始同步，默认 true
	OnSessionStart bool
	// IntervalMinutes 定时同步分钟，默认 0（禁用）
	IntervalMinutes int
}

// CacheConfig 缓存配置。对齐 Python MemorySettings.cache
type CacheConfig struct {
	// Enabled 是否启用嵌入缓存，默认 true
	Enabled bool
	// MaxEntries 最大条目数，默认 10000
	MaxEntries int
}

// MemorySettings 记忆配置。对齐 Python MemorySettings dataclass
type MemorySettings struct {
	// Provider 提供者类型，默认 "openai_compatible"
	Provider string
	// Model 嵌入模型，默认 "text-embedding-v3"（可被 embed 段覆盖）
	Model string
	// Fallback 回退策略，默认 "mock"
	Fallback string
	// Sources 记忆来源，默认 ["memory", "sessions"]
	Sources []string
	// ExtraPaths 额外记忆路径
	ExtraPaths []string
	// Chunking 分块配置
	Chunking ChunkingConfig
	// Query 查询配置
	Query QueryConfig
	// Store 存储配置
	Store StoreConfig
	// Sync 同步配置
	Sync SyncConfig
	// Cache 缓存配置
	Cache CacheConfig
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// configCache 配置缓存。对齐 Python _config_cache
var configCache map[string]any

// configCacheMu 配置缓存锁
var configCacheMu sync.RWMutex

// configLogComponent 日志组件
var configLogComponent = logger.ComponentAgentServer

// configLoader 配置加载器（默认读全局 config.yaml，测试可注入）
var configLoader = defaultConfigLoader

// ─────────────────── 导出函数 ───────────────────

// ClearConfigCache 清除配置缓存。对齐 Python clear_config_cache
func ClearConfigCache() {
	configCacheMu.Lock()
	configCache = nil
	configCacheMu.Unlock()
}

// GetEmbedConfig 获取嵌入配置。对齐 Python get_embed_config：读 embed 段三键
func GetEmbedConfig() map[string]string {
	cfg := loadConfig()
	embedCfg, _ := cfg["embed"].(map[string]any)
	if embedCfg == nil {
		embedCfg = map[string]any{}
	}
	return map[string]string{
		"api_key":  strVal(embedCfg["embed_api_key"]),
		"base_url": strVal(embedCfg["embed_base_url"]),
		"model":    strVal(embedCfg["embed_model"]),
	}
}

// CreateMemorySettings 创建记忆配置。对齐 Python create_memory_settings：
// 默认值 ← embed 段 model ← config memory 段逐字段覆盖 ← overrides 透传
func CreateMemorySettings(workspaceDir string, overrides map[string]any) *MemorySettings {
	cfg := loadConfig()
	embedCfg := GetEmbedConfig()
	memoryCfg, _ := cfg["memory"].(map[string]any)

	s := &MemorySettings{
		Provider:   "openai_compatible",
		Model:      "text-embedding-v3",
		Fallback:   "mock",
		Sources:    []string{"memory", "sessions"},
		ExtraPaths: nil,
		Chunking:   ChunkingConfig{Tokens: 256, Overlap: 32},
		Query: QueryConfig{
			MaxResults: 10,
			MinScore:   0.3,
			Hybrid:     HybridConfig{Enabled: true, VectorWeight: 0.7, TextWeight: 0.3, CandidateMultiplier: 2.0},
		},
		Store: StoreConfig{Path: "memory.db", VectorEnabled: true, FtsEnabled: true},
		Sync:  SyncConfig{Watch: true, WatchDebounceMs: 2000, OnSearch: true, OnSessionStart: true, IntervalMinutes: 0},
		Cache: CacheConfig{Enabled: true, MaxEntries: 10000},
	}

	// Python: settings.model = embed_config.get("model", settings.model)
	if embedCfg["model"] != "" {
		s.Model = embedCfg["model"]
	}

	if memoryCfg != nil {
		if v, ok := memoryCfg["provider"].(string); ok {
			s.Provider = v
		}
		if v, ok := memoryCfg["fallback"].(string); ok {
			s.Fallback = v
		}
		if v := strSlice(memoryCfg["sources"]); v != nil {
			s.Sources = v
		}
		if v := strSlice(memoryCfg["extraPaths"]); v != nil {
			s.ExtraPaths = v
		}
		if v, ok := memoryCfg["chunking"].(map[string]any); ok {
			if t, ok := v["tokens"].(float64); ok {
				s.Chunking.Tokens = int(t)
			}
			if o, ok := v["overlap"].(float64); ok {
				s.Chunking.Overlap = int(o)
			}
		}
		if v, ok := memoryCfg["query"].(map[string]any); ok {
			if mr, ok := v["maxResults"].(float64); ok {
				s.Query.MaxResults = int(mr)
			}
			if ms, ok := v["minScore"].(float64); ok {
				s.Query.MinScore = ms
			}
			if h, ok := v["hybrid"].(map[string]any); ok {
				if e, ok := h["enabled"].(bool); ok {
					s.Query.Hybrid.Enabled = e
				}
				if w, ok := h["vectorWeight"].(float64); ok {
					s.Query.Hybrid.VectorWeight = w
				}
				if w, ok := h["textWeight"].(float64); ok {
					s.Query.Hybrid.TextWeight = w
				}
				if cm, ok := h["candidateMultiplier"].(float64); ok {
					s.Query.Hybrid.CandidateMultiplier = cm
				}
			}
		}
		if v, ok := memoryCfg["sync"].(map[string]any); ok {
			if w, ok := v["watch"].(bool); ok {
				s.Sync.Watch = w
			}
			if d, ok := v["watchDebounceMs"].(float64); ok {
				s.Sync.WatchDebounceMs = int(d)
			}
			if o, ok := v["onSearch"].(bool); ok {
				s.Sync.OnSearch = o
			}
			if o, ok := v["onSessionStart"].(bool); ok {
				s.Sync.OnSessionStart = o
			}
			if m, ok := v["intervalMinutes"].(float64); ok {
				s.Sync.IntervalMinutes = int(m)
			}
		}
		if v, ok := memoryCfg["cache"].(map[string]any); ok {
			if e, ok := v["enabled"].(bool); ok {
				s.Cache.Enabled = e
			}
			if m, ok := v["maxEntries"].(float64); ok {
				s.Cache.MaxEntries = int(m)
			}
		}
	}

	// Python: store 段特殊处理（overrides["store"] 未提供时构造三键）
	storeOverride := map[string]any{}
	if sc, ok := memoryCfg["store"].(map[string]any); ok {
		if p, ok := sc["path"].(string); ok {
			storeOverride["path"] = p
		}
		if ve, ok := sc["vector"].(map[string]any); ok {
			storeOverride["vector"] = ve
		}
		if fe, ok := sc["fts"].(map[string]any); ok {
			storeOverride["fts"] = fe
		}
	}
	if _, exists := overrides["store"]; !exists && len(storeOverride) > 0 {
		overrides = mapsClone(overrides)
		overrides["store"] = storeOverride
	}

	applyOverrides(s, overrides)
	return s
}

// ResolveModeMemory 定位 modes 下指定 mode 的 memory 块。对齐 Python _resolve_mode_memory。
// 支持 "agent.plan"/"agent.fast"、"plan"/"fast"（归 agent 命名空间）、"code" 三形态。
func ResolveModeMemory(mode string, cfg map[string]any) map[string]any {
	modesCfg, _ := cfg["modes"].(map[string]any)
	if modesCfg == nil {
		return map[string]any{}
	}
	token := strings.TrimSpace(mode)
	var node map[string]any
	if strings.Contains(token, ".") {
		parts := strings.SplitN(token, ".", 2)
		top, _ := modesCfg[parts[0]].(map[string]any)
		if top == nil {
			return map[string]any{}
		}
		node, _ = top[parts[1]].(map[string]any)
	} else if token == "code" {
		node, _ = modesCfg["code"].(map[string]any)
	} else {
		agentNode, _ := modesCfg["agent"].(map[string]any)
		if agentNode == nil {
			return map[string]any{}
		}
		node, _ = agentNode[token].(map[string]any)
	}
	if node == nil {
		return map[string]any{}
	}
	mem, _ := node["memory"].(map[string]any)
	if mem == nil {
		return map[string]any{}
	}
	return mem
}

// IsMemoryEnabled 检查指定 mode 下内置记忆是否启用。对齐 Python is_memory_enabled
func IsMemoryEnabled(mode string, cfg map[string]any) bool {
	mem := ResolveModeMemory(mode, cfg)
	enabled, _ := mem["enabled"].(bool)
	return enabled
}

// IsProactiveMemory 检查指定 mode 下是否主动记忆。对齐 Python is_proactive_memory
func IsProactiveMemory(mode string, cfg map[string]any) bool {
	mem := ResolveModeMemory(mode, cfg)
	proactive, _ := mem["is_proactive"].(bool)
	return proactive
}

// GetMemoryMode 读取 memory.mode：cloud 或 local（默认）。对齐 Python get_memory_mode
func GetMemoryMode(cfg map[string]any) string {
	memCfg, _ := cfg["memory"].(map[string]any)
	if memCfg == nil {
		return "local"
	}
	mode, _ := memCfg["mode"].(string)
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "cloud" {
		return "cloud"
	}
	return "local"
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// defaultConfigLoader 默认配置加载：读全局 config.yaml（带缓存）
func defaultConfigLoader() (map[string]any, error) {
	cfg, err := config.New(pathutil.ConfigFile())
	if err != nil {
		// 对齐 Python: logger.warning(f"Config file not found: {config_path}")
		logger.Warn(configLogComponent).Str("config_path", pathutil.ConfigFile()).Msg("Config file not found")
		return map[string]any{}, nil
	}
	data, err := cfg.Load()
	if err != nil {
		return nil, err
	}
	return data, nil
}

// loadConfig 带缓存加载配置。对齐 Python _load_config（_config_cache 语义）
func loadConfig() map[string]any {
	configCacheMu.RLock()
	if configCache != nil {
		defer configCacheMu.RUnlock()
		return configCache
	}
	configCacheMu.RUnlock()

	data, err := configLoader()
	if err != nil {
		// 对齐 Python：加载失败返回空配置
		logger.Warn(configLogComponent).Err(err).Msg("加载配置失败")
		data = map[string]any{}
	}
	configCacheMu.Lock()
	configCache = data
	configCacheMu.Unlock()
	return data
}

// strVal 安全取字符串
func strVal(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// strSlice 安全取字符串切片
func strSlice(v any) []string {
	switch arr := v.(type) {
	case []string:
		return arr
	case []any:
		out := make([]string, 0, len(arr))
		for _, item := range arr {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

// mapsClone 克隆 map（避免修改调用方 overrides）
func mapsClone(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// applyOverrides 应用 overrides 到 settings。对齐 Python for key, value in overrides.items()
func applyOverrides(s *MemorySettings, overrides map[string]any) {
	if overrides == nil {
		return
	}
	if v, ok := overrides["provider"].(string); ok {
		s.Provider = v
	}
	if v, ok := overrides["model"].(string); ok {
		s.Model = v
	}
	if v, ok := overrides["fallback"].(string); ok {
		s.Fallback = v
	}
	if v := strSlice(overrides["sources"]); v != nil {
		s.Sources = v
	}
	if v := strSlice(overrides["extraPaths"]); v != nil {
		s.ExtraPaths = v
	}
	if v, ok := overrides["chunking"].(map[string]any); ok {
		if t, ok := v["tokens"].(float64); ok {
			s.Chunking.Tokens = int(t)
		}
		if o, ok := v["overlap"].(float64); ok {
			s.Chunking.Overlap = int(o)
		}
	}
	if v, ok := overrides["query"].(map[string]any); ok {
		if mr, ok := v["maxResults"].(float64); ok {
			s.Query.MaxResults = int(mr)
		}
		if ms, ok := v["minScore"].(float64); ok {
			s.Query.MinScore = ms
		}
		if h, ok := v["hybrid"].(map[string]any); ok {
			if e, ok := h["enabled"].(bool); ok {
				s.Query.Hybrid.Enabled = e
			}
			if w, ok := h["vectorWeight"].(float64); ok {
				s.Query.Hybrid.VectorWeight = w
			}
			if w, ok := h["textWeight"].(float64); ok {
				s.Query.Hybrid.TextWeight = w
			}
			if cm, ok := h["candidateMultiplier"].(float64); ok {
				s.Query.Hybrid.CandidateMultiplier = cm
			}
		}
	}
	if v, ok := overrides["store"].(map[string]any); ok {
		if p, ok := v["path"].(string); ok {
			s.Store.Path = p
		}
		if ve, ok := v["vector"].(map[string]any); ok {
			if e, ok := ve["enabled"].(bool); ok {
				s.Store.VectorEnabled = e
			}
		}
		if fe, ok := v["fts"].(map[string]any); ok {
			if e, ok := fe["enabled"].(bool); ok {
				s.Store.FtsEnabled = e
			}
		}
	}
	if v, ok := overrides["sync"].(map[string]any); ok {
		if w, ok := v["watch"].(bool); ok {
			s.Sync.Watch = w
		}
		if d, ok := v["watchDebounceMs"].(float64); ok {
			s.Sync.WatchDebounceMs = int(d)
		}
		if o, ok := v["onSearch"].(bool); ok {
			s.Sync.OnSearch = o
		}
		if o, ok := v["onSessionStart"].(bool); ok {
			s.Sync.OnSessionStart = o
		}
		if m, ok := v["intervalMinutes"].(float64); ok {
			s.Sync.IntervalMinutes = int(m)
		}
	}
	if v, ok := overrides["cache"].(map[string]any); ok {
		if e, ok := v["enabled"].(bool); ok {
			s.Cache.Enabled = e
		}
		if m, ok := v["maxEntries"].(float64); ok {
			s.Cache.MaxEntries = int(m)
		}
	}
}
```

注意：`withTestConfig` 测试辅助需要把 `configLoader` 指向临时文件。在 config_test.go 中添加：

```go
// injectTestConfigLoader 注入测试配置加载器
func injectTestConfigLoader(t *testing.T, yamlContent string) {
	t.Helper()
	orig := configLoader
	configLoader = func() (map[string]any, error) {
		cfg, err := config.New(filepath.Join(t.TempDir(), "config.yaml"))
		if err != nil {
			return nil, err
		}
		_ = os.WriteFile(cfg.Path(), []byte(yamlContent), 0o644)
		data, err := cfg.Load()
		if err != nil {
			return nil, err
		}
		return data, nil
	}
	t.Cleanup(func() {
		configLoader = orig
		ClearConfigCache()
	})
}
```

（若 `config.Config` 无导出 `Path()` 方法，改用包内维护路径变量或在注入函数中直接构造 `config.New(path)` 后 Load——以实际 API 为准，实现时核对 `internal/common/config/config.go`。）

- [ ] **Step 4: 运行测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/swarm/agents/harness/common/memory/ -run 'TestGetEmbedConfig|TestMemorySettings|TestResolveModeMemory|TestIsMemoryEnabled|TestIsProactiveMemory|TestGetMemoryMode|TestConfigCache' -v 2>&1 | tail -5
```
Expected: 全部 PASS

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uapclaw-gateway && git add internal/swarm/agents/harness/common/memory/config.go internal/swarm/agents/harness/common/memory/config_test.go && git commit -m "feat(swarm-memory): config 层（embed 段读取 + MemorySettings + mode 解析 + 包内缓存）"
```

---

## Task 3: embeddings.go（10.6.15 核心）

**Files:**
- Create: `internal/swarm/agents/harness/common/memory/embeddings.go`
- Test: `internal/swarm/agents/harness/common/memory/embeddings_test.go`

Python 参考：`embeddings.py`（165行）。**本任务是 10.6.15 的核心交付物。**

- [ ] **Step 1: 写 embeddings_test.go 失败测试**

```go
package memory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// TestOpenAICompatibleEmbeddingProvider_环境变量链 对齐 Python 构造函数 fallback 链：
// 参数 > EMBED_KEY > EMBED_API_KEY > config embed 段
func TestOpenAICompatibleEmbeddingProvider_环境变量链(t *testing.T) {
	// 清理环境
	for _, k := range []string{"EMBED_KEY", "EMBED_API_KEY", "EMBED_MODEL", "EMBEDDING_MODEL", "EMBED_BASE", "EMBED_BASE_URL"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	ClearConfigCache()

	// 1. 无任何配置：api_key 为空
	p := NewOpenAICompatibleEmbeddingProvider("", "", "")
	if p.APIKey != "" {
		t.Errorf("无配置时 api_key 应为空")
	}

	// 2. EMBED_KEY 优先于 EMBED_API_KEY
	t.Setenv("EMBED_KEY", "key1")
	t.Setenv("EMBED_API_KEY", "key2")
	p = NewOpenAICompatibleEmbeddingProvider("", "", "")
	if p.APIKey != "key1" {
		t.Errorf("EMBED_KEY 应优先，got %s", p.APIKey)
	}

	// 3. 参数优先于环境变量
	p = NewOpenAICompatibleEmbeddingProvider("param-key", "param-model", "param-base")
	if p.APIKey != "param-key" || p.Model != "param-model" || p.BaseURL != "param-base" {
		t.Errorf("参数应最优先: %+v", p)
	}

	// 4. EMBED_MODEL 优先于 EMBEDDING_MODEL
	t.Setenv("EMBED_MODEL", "m1")
	t.Setenv("EMBEDDING_MODEL", "m2")
	p = NewOpenAICompatibleEmbeddingProvider("", "", "")
	if p.Model != "m1" {
		t.Errorf("EMBED_MODEL 应优先，got %s", p.Model)
	}

	// 5. EMBED_BASE 优先于 EMBED_BASE_URL
	t.Setenv("EMBED_BASE", "b1")
	t.Setenv("EMBED_BASE_URL", "b2")
	p = NewOpenAICompatibleEmbeddingProvider("", "", "")
	if p.BaseURL != "b1" {
		t.Errorf("EMBED_BASE 应优先，got %s", p.BaseURL)
	}
}

// TestOpenAICompatibleEmbeddingProvider_baseURL裁剪 对齐 Python：以 /embeddings 结尾裁剪
func TestOpenAICompatibleEmbeddingProvider_baseURL裁剪(t *testing.T) {
	t.Setenv("EMBED_KEY", "k")
	t.Setenv("EMBED_MODEL", "m")
	t.Setenv("EMBED_BASE", "https://api.example.com/v1/embeddings")
	p := NewOpenAICompatibleEmbeddingProvider("", "", "")
	if p.BaseURL != "https://api.example.com/v1" {
		t.Fatalf("裁剪失败: %s", p.BaseURL)
	}
}

// TestOpenAICompatibleEmbeddingProvider_EmbedDocuments 对齐 Python embed_documents：
// POST {base_url}/embeddings + Bearer + encoding_format=float + index 排序 + dims 更新
func TestOpenAICompatibleEmbeddingProvider_EmbedDocuments(t *testing.T) {
	var gotBody map[string]any
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Errorf("路径应为 /embeddings，got %s", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		// 故意乱序返回（index 1 在前）
		_, _ = w.Write([]byte(`{"data":[{"index":1,"embedding":[3.0,4.0]},{"index":0,"embedding":[1.0,2.0]}]}`))
	}))
	defer server.Close()

	t.Setenv("EMBED_KEY", "test-key")
	t.Setenv("EMBED_MODEL", "test-model")
	t.Setenv("EMBED_BASE", server.URL)
	ClearConfigCache()

	p := NewOpenAICompatibleEmbeddingProvider("", "", "")
	if p.Dims() != 1024 {
		t.Errorf("初始 dims 应为 1024")
	}

	emb, err := p.EmbedDocuments(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatalf("EmbedDocuments 失败: %v", err)
	}
	if len(emb) != 2 {
		t.Fatalf("应返回 2 个向量")
	}
	// index 排序：0 在前
	if emb[0][0] != 1.0 || emb[1][0] != 3.0 {
		t.Errorf("应按 index 排序: %v", emb)
	}
	if p.Dims() != 2 {
		t.Errorf("dims 应更新为 2，got %d", p.Dims())
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Bearer 认证失败: %s", gotAuth)
	}
	if gotBody["model"] != "test-model" || gotBody["encoding_format"] != "float" {
		t.Errorf("请求体错误: %v", gotBody)
	}
	inputs := gotBody["input"].([]any)
	if len(inputs) != 2 || inputs[0] != "hello" {
		t.Errorf("input 错误: %v", inputs)
	}
}

// TestOpenAICompatibleEmbeddingProvider_EmbedDocuments_无Key 对齐 Python：无 key 抛 ValueError
func TestOpenAICompatibleEmbeddingProvider_EmbedDocuments_无Key(t *testing.T) {
	for _, k := range []string{"EMBED_KEY", "EMBED_API_KEY"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	ClearConfigCache()
	p := NewOpenAICompatibleEmbeddingProvider("", "", "")
	_, err := p.EmbedDocuments(context.Background(), []string{"x"})
	if err == nil {
		t.Fatalf("无 key 应报错")
	}
}

// TestOpenAICompatibleEmbeddingProvider_EmbedDocuments_非200 对齐 Python：非 200 抛 RuntimeError
func TestOpenAICompatibleEmbeddingProvider_EmbedDocuments_非200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("upstream error"))
	}))
	defer server.Close()
	t.Setenv("EMBED_KEY", "k")
	t.Setenv("EMBED_MODEL", "m")
	t.Setenv("EMBED_BASE", server.URL)
	ClearConfigCache()

	p := NewOpenAICompatibleEmbeddingProvider("", "", "")
	_, err := p.EmbedDocuments(context.Background(), []string{"x"})
	if err == nil {
		t.Fatalf("非 200 应报错")
	}
}

// TestOpenAICompatibleEmbeddingProvider_EmbedQuery 对齐 Python：embed_query 转发 embed_documents 取首个
func TestOpenAICompatibleEmbeddingProvider_EmbedQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[1.5,2.5,3.5]}]}`))
	}))
	defer server.Close()
	t.Setenv("EMBED_KEY", "k")
	t.Setenv("EMBED_MODEL", "m")
	t.Setenv("EMBED_BASE", server.URL)
	ClearConfigCache()

	p := NewOpenAICompatibleEmbeddingProvider("", "", "")
	vec, err := p.EmbedQuery(context.Background(), "q")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(vec) != 3 || vec[0] != 1.5 {
		t.Errorf("got %v", vec)
	}
}

// TestOpenAICompatibleEmbeddingProvider_EmbedQuery_空结果 对齐 Python：embeddings 为空时返回 []
func TestOpenAICompatibleEmbeddingProvider_EmbedQuery_空结果(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()
	t.Setenv("EMBED_KEY", "k")
	t.Setenv("EMBED_MODEL", "m")
	t.Setenv("EMBED_BASE", server.URL)
	ClearConfigCache()

	p := NewOpenAICompatibleEmbeddingProvider("", "", "")
	vec, err := p.EmbedQuery(context.Background(), "q")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(vec) != 0 {
		t.Errorf("空结果应返回空切片，got %v", vec)
	}
}

// TestMockEmbeddingProvider_确定性与维度 对齐 Python：md5 种子 128 维
func TestMockEmbeddingProvider_确定性与维度(t *testing.T) {
	m := NewMockEmbeddingProvider()
	if m.ID() != "mock" || m.Model() != "mock" || m.Dims() != 128 {
		t.Errorf("元数据错误: %s/%s/%d", m.ID(), m.Model(), m.Dims())
	}
	v1, err := m.EmbedQuery(context.Background(), "same text")
	if err != nil {
		t.Fatal(err)
	}
	v2, _ := m.EmbedQuery(context.Background(), "same text")
	v3, _ := m.EmbedQuery(context.Background(), "different")
	if len(v1) != 128 {
		t.Errorf("应为 128 维，got %d", len(v1))
	}
	for i := range v1 {
		if v1[i] != v2[i] {
			t.Errorf("同输入应确定性")
			break
		}
		if v1[i] < -1 || v1[i] > 1 {
			t.Errorf("值应在 [-1,1]")
			break
		}
	}
	sameAsDiff := true
	for i := range v1 {
		if v1[i] != v3[i] {
			sameAsDiff = false
			break
		}
	}
	if sameAsDiff {
		t.Errorf("不同输入应产生不同向量")
	}
}

// TestMockEmbeddingProvider_EmbedDocuments 对齐 Python：逐条转 embed_query
func TestMockEmbeddingProvider_EmbedDocuments(t *testing.T) {
	m := NewMockEmbeddingProvider()
	docs, err := m.EmbedDocuments(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 || len(docs[0]) != 128 || len(docs[1]) != 128 {
		t.Errorf("got %d", len(docs))
	}
}

// TestCreateEmbeddingProvider_mock 对齐 Python：provider=="mock" 直接返回
func TestCreateEmbeddingProvider_mock(t *testing.T) {
	p, err := CreateEmbeddingProvider("mock", "", "mock")
	if err != nil {
		t.Fatal(err)
	}
	if p.ID() != "mock" {
		t.Errorf("应返回 Mock")
	}
}

// TestCreateEmbeddingProvider_有key 对齐 Python：有 embed_key 时返回 OpenAICompatible
func TestCreateEmbeddingProvider_有key(t *testing.T) {
	t.Setenv("EMBED_KEY", "k")
	t.Setenv("EMBED_MODEL", "m")
	t.Setenv("EMBED_BASE", "https://x.example.com")
	ClearConfigCache()
	p, err := CreateEmbeddingProvider("auto", "", "mock")
	if err != nil {
		t.Fatal(err)
	}
	if p.ID() != "openai_compatible" {
		t.Errorf("应返回 OpenAICompatible，got %s", p.ID())
	}
	if p.Model() != "m" {
		t.Errorf("model 应为 m，got %s", p.Model())
	}
}

// TestCreateEmbeddingProvider_降级mock 对齐 Python：无 key + fallback=mock → warn + Mock
func TestCreateEmbeddingProvider_降级mock(t *testing.T) {
	for _, k := range []string{"EMBED_KEY", "EMBED_API_KEY"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	ClearConfigCache()
	p, err := CreateEmbeddingProvider("auto", "", "mock")
	if err != nil {
		t.Fatal(err)
	}
	if p.ID() != "mock" {
		t.Errorf("应降级 Mock，got %s", p.ID())
	}
}

// TestCreateEmbeddingProvider_无key无fallback 对齐 Python：无 key + fallback!=mock → ValueError
func TestCreateEmbeddingProvider_无key无fallback(t *testing.T) {
	for _, k := range []string{"EMBED_KEY", "EMBED_API_KEY"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	ClearConfigCache()
	_, err := CreateEmbeddingProvider("auto", "", "error")
	if err == nil {
		t.Fatalf("应报错")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/swarm/agents/harness/common/memory/ -run 'TestOpenAICompatible|TestMockEmbedding|TestCreateEmbeddingProvider' -v 2>&1 | tail -5
```
Expected: FAIL（`undefined: NewOpenAICompatibleEmbeddingProvider` 等）

- [ ] **Step 3: 写 embeddings.go**

```go
package memory

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EmbeddingProvider 嵌入向量提供者接口。对齐 Python EmbeddingProvider ABC
type EmbeddingProvider interface {
	// EmbedQuery 生成查询向量
	EmbedQuery(ctx context.Context, text string) ([]float64, error)
	// EmbedDocuments 批量生成文档向量
	EmbedDocuments(ctx context.Context, texts []string) ([][]float64, error)
	// ID 提供者标识
	ID() string
	// Model 模型名
	Model() string
	// Dims 向量维度
	Dims() int
}

// OpenAICompatibleEmbeddingProvider OpenAI 兼容嵌入提供者。
// 对齐 Python OpenAICompatibleEmbeddingProvider（支持 DashScope/OpenAI 等）。
// 差异说明：Python 用 httpx.AsyncClient(60s)；Go 用 net/http 60s 超时，均无重试（单次 POST）。
type OpenAICompatibleEmbeddingProvider struct {
	// APIKey 密钥（参数 > EMBED_KEY > EMBED_API_KEY > config embed 段）
	APIKey string
	// Model 模型名（参数 > EMBED_MODEL > EMBEDDING_MODEL > config embed 段）
	Model string
	// BaseURL 基础地址（参数 > EMBED_BASE > EMBED_BASE_URL > config embed 段；/embeddings 后缀裁剪）
	BaseURL string
	// dims 向量维度（初始 1024，首次成功后按实际更新）

	client *http.Client
	dims   int
}

// MockEmbeddingProvider 模拟嵌入提供者（测试用）。对齐 Python MockEmbeddingProvider。
// 差异说明：Python 用 md5 作 random.seed(Mersenne Twister)；Go 无法复刻 PRNG 序列，
// 用 md5 哈希直接展开为 float64（值与 Python 不等价，但保证确定性与 128 维）。
type MockEmbeddingProvider struct {
	id    string
	model string
	dims  int
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// embeddingLogComponent 日志组件
var embeddingLogComponent = logger.ComponentAgentServer

// ──────────────────────────── 导出函数 ────────────────────────────

// NewOpenAICompatibleEmbeddingProvider 构造 OpenAI 兼容提供者。
// 对齐 Python __init__ 的环境变量 fallback 链。
func NewOpenAICompatibleEmbeddingProvider(apiKey, model, baseURL string) *OpenAICompatibleEmbeddingProvider {
	embedConfig := GetEmbedConfig()

	if apiKey == "" {
		apiKey = os.Getenv("EMBED_KEY")
	}
	if apiKey == "" {
		apiKey = os.Getenv("EMBED_API_KEY")
	}
	if apiKey == "" {
		apiKey = embedConfig["api_key"]
	}
	if model == "" {
		model = os.Getenv("EMBED_MODEL")
	}
	if model == "" {
		model = os.Getenv("EMBEDDING_MODEL")
	}
	if model == "" {
		model = embedConfig["model"]
	}
	if baseURL == "" {
		baseURL = os.Getenv("EMBED_BASE")
	}
	if baseURL == "" {
		baseURL = os.Getenv("EMBED_BASE_URL")
	}
	if baseURL == "" {
		baseURL = embedConfig["base_url"]
	}

	// 对齐 Python：base_url 以 /embeddings 结尾时裁剪
	if baseURL != "" && strings.HasSuffix(baseURL, "/embeddings") {
		baseURL = baseURL[:len(baseURL)-len("/embeddings")]
	}

	return &OpenAICompatibleEmbeddingProvider{
		APIKey:  apiKey,
		Model:   model,
		BaseURL: baseURL,
		dims:    1024,
	}
}

// NewMockEmbeddingProvider 构造模拟提供者
func NewMockEmbeddingProvider() *MockEmbeddingProvider {
	return &MockEmbeddingProvider{id: "mock", model: "mock", dims: 128}
}

// CreateEmbeddingProvider 根据配置创建嵌入提供者。对齐 Python create_embedding_provider：
// provider=="mock" 直返；否则查 embed_key（EMBED_KEY > EMBED_API_KEY > config），
// 有则 OpenAICompatible；无且 fallback=="mock" 时 warn + Mock；否则 error。
func CreateEmbeddingProvider(provider, model, fallback string) (EmbeddingProvider, error) {
	if provider == "mock" {
		return NewMockEmbeddingProvider(), nil
	}

	embedConfig := GetEmbedConfig()
	embedKey := os.Getenv("EMBED_KEY")
	if embedKey == "" {
		embedKey = os.Getenv("EMBED_API_KEY")
	}
	if embedKey == "" {
		embedKey = embedConfig["api_key"]
	}

	if embedKey != "" {
		m := model
		if m == "" {
			m = os.Getenv("EMBED_MODEL")
		}
		if m == "" {
			m = os.Getenv("EMBEDDING_MODEL")
		}
		if m == "" {
			m = embedConfig["model"]
		}
		b := os.Getenv("EMBED_BASE")
		if b == "" {
			b = os.Getenv("EMBED_BASE_URL")
		}
		if b == "" {
			b = embedConfig["base_url"]
		}
		return NewOpenAICompatibleEmbeddingProvider(embedKey, m, b), nil
	}

	if fallback == "mock" {
		// 对齐 Python: logger.warning("Embedding API key not found, using mock provider")
		logger.Warn(embeddingLogComponent).Msg("Embedding API key not found, using mock provider")
		return NewMockEmbeddingProvider(), nil
	}

	return nil, fmt.Errorf("Embedding API key not configured. Set embed.api_key in config.yaml or EMBED_API_KEY environment variable.")
}

// EmbedQuery 生成查询向量。对齐 Python embed_query：转发 embed_documents 取首个
func (p *OpenAICompatibleEmbeddingProvider) EmbedQuery(ctx context.Context, text string) ([]float64, error) {
	embeddings, err := p.EmbedDocuments(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(embeddings) == 0 {
		return []float64{}, nil
	}
	return embeddings[0], nil
}

// EmbedDocuments 批量生成文档向量。对齐 Python embed_documents。
func (p *OpenAICompatibleEmbeddingProvider) EmbedDocuments(ctx context.Context, texts []string) ([][]float64, error) {
	if p.APIKey == "" {
		return nil, fmt.Errorf("Embedding API key not configured. Set embed.api_key in config.yaml or EMBED_API_KEY environment variable.")
	}

	if p.client == nil {
		p.client = &http.Client{Timeout: 60 * time.Second}
	}

	reqBody := map[string]any{
		"model":           p.Model,
		"input":           texts,
		"encoding_format": "float",
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+"/embeddings", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Embedding API 请求失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		var sb strings.Builder
		_, _ = sb.WriteString(fmt.Sprintf("Embedding API failed: status=%d", resp.StatusCode))
		return nil, fmt.Errorf("Embedding API failed: %s", sb.String())
	}

	var data struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	// 对齐 Python：sorted(data, key=index)
	sort.SliceStable(data.Data, func(i, j int) bool {
		return data.Data[i].Index < data.Data[j].Index
	})

	embeddings := make([][]float64, 0, len(data.Data))
	for _, item := range data.Data {
		embeddings = append(embeddings, item.Embedding)
	}

	if len(embeddings) > 0 {
		p.dims = len(embeddings[0])
	}
	return embeddings, nil
}

// ID 提供者标识
func (p *OpenAICompatibleEmbeddingProvider) ID() string { return "openai_compatible" }

// Model 模型名
func (p *OpenAICompatibleEmbeddingProvider) Model() string { return p.Model }

// Dims 向量维度
func (p *OpenAICompatibleEmbeddingProvider) Dims() int { return p.dims }

// EmbedQuery 生成模拟向量。对齐 Python：md5 种子 128 维（Go 用哈希展开近似，见结构体注释）
func (m *MockEmbeddingProvider) EmbedQuery(_ context.Context, text string) ([]float64, error) {
	// 取 8 轮 md5（8*16=128 字节）扩展到 128 维
	out := make([]float64, m.dims)
	prefix := ""
	for round := 0; round*16 < m.dims; round++ {
		sum := md5.Sum([]byte(prefix + text))
		prefix = string(sum[:])
		for b := 0; b < 16 && round*16+b < m.dims; b++ {
			v := binary.LittleEndian.Uint16(sum[b*2 : b*2+2])
			out[round*16+b] = float64(v)/65535.0*2.0 - 1.0 // 映射到 [-1, 1]
		}
	}
	return out, nil
}

// EmbedDocuments 批量生成模拟向量。对齐 Python：逐条转 embed_query
func (m *MockEmbeddingProvider) EmbedDocuments(ctx context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, 0, len(texts))
	for _, t := range texts {
		v, err := m.EmbedQuery(ctx, t)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// ID 提供者标识
func (m *MockEmbeddingProvider) ID() string { return m.id }

// Model 模型名
func (m *MockEmbeddingProvider) Model() string { return m.model }

// Dims 向量维度
func (m *MockEmbeddingProvider) Dims() int { return m.dims }

// ──────────────────────────── 非导出函数 ────────────────────────────

// 编译时验证接口满足
var (
	_ EmbeddingProvider = (*OpenAICompatibleEmbeddingProvider)(nil)
	_ EmbeddingProvider = (*MockEmbeddingProvider)(nil)
)

// 引用 math 包避免未使用（Dims 等场景）
var _ = math.Abs
```

注意：`math` 与 `sort` 导入如实际未用到则删除（`var _ = math.Abs` 是占位提示，实现时清理）。

- [ ] **Step 4: 运行测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/swarm/agents/harness/common/memory/ -run 'TestOpenAICompatible|TestMockEmbedding|TestCreateEmbeddingProvider' -v 2>&1 | tail -5
```
Expected: 全部 PASS

- [ ] **Step 5: 提交（10.6.15 核心交付）**

```bash
cd /home/opensource/uapclaw-gateway && git add internal/swarm/agents/harness/common/memory/embeddings.go internal/swarm/agents/harness/common/memory/embeddings_test.go && git commit -m "feat(swarm-memory): 10.6.15 EmbeddingProvider 接口 + OpenAICompatible + Mock + 工厂（环境变量链+降级）"
```

---

## Task 4: manager.go（记忆索引管理器）

**Files:**
- Create: `internal/swarm/agents/harness/common/memory/manager.go`
- Test: `internal/swarm/agents/harness/common/memory/manager_test.go`

Python 参考：`manager.py`（1224行）。这是最大的任务，按内部结构分 5 个子步骤提交。

- [ ] **Step 1: 写 manager_test.go 失败测试（核心行为）**

```go
package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestManager 构造测试 manager（Mock provider + 临时目录）
func newTestManager(t *testing.T, agentID string) *MemoryIndexManager {
	t.Helper()
	dir := t.TempDir()
	settings := CreateMemorySettings(dir, nil)
	mgr, err := GetMemoryManager(context.Background(), agentID, dir, settings)
	if err != nil {
		t.Fatalf("GetMemoryManager 失败: %v", err)
	}
	if mgr == nil {
		t.Fatal("mgr 不应为 nil")
	}
	t.Cleanup(func() { _ = mgr.Close() })
	return mgr
}

// TestGetMemoryManager_缓存幂等 对齐 Python INDEX_CACHE 语义
func TestGetMemoryManager_缓存幂等(t *testing.T) {
	dir := t.TempDir()
	m1, err := GetMemoryManager(context.Background(), "agent-cache-test", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m1.Close() }()
	m2, err := GetMemoryManager(context.Background(), "agent-cache-test", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if m1 != m2 {
		t.Errorf("同 key 应返回缓存实例")
	}
}

// TestGetMemoryManager_ClearCache 对齐 Python clear_memory_manager_cache
func TestGetMemoryManager_ClearCache(t *testing.T) {
	dir := t.TempDir()
	m1, _ := GetMemoryManager(context.Background(), "agent-clear-test", dir, nil)
	if m1 == nil {
		t.Fatal("m1 nil")
	}
	ClearMemoryManagerCache()
	m2, _ := GetMemoryManager(context.Background(), "agent-clear-test", dir, nil)
	if m2 == nil {
		t.Fatal("m2 nil")
	}
	if m1 == m2 {
		t.Errorf("清缓存后应新建实例")
	}
	_ = m1.Close()
	_ = m2.Close()
}

// TestResolveDBPath 对齐 Python _resolve_db_path：
// 绝对路径直返 / workspace_name 前缀去重 / memory/ 子目录保证
func TestResolveDBPath(t *testing.T) {
	m := &MemoryIndexManager{workspaceDir: "/ws/myproj"}
	cases := []struct {
		storePath, want string
	}{
		{"/abs/path/memory.db", "/abs/path/memory.db"},                                       // 绝对路径
		{"myproj/memory.db", "/ws/myproj/memory/memory.db"},                                  // 前缀去重后补 memory/
		{"memory.db", "/ws/myproj/memory/memory.db"},                                         // 补 memory/
		{"memory/custom.db", "/ws/myproj/memory/custom.db"},                                  // 已含 memory/ 不重复
	}
	for _, c := range cases {
		m.settings = &MemorySettings{Store: StoreConfig{Path: c.storePath}}
		if got := m.resolveDBPath(); got != c.want {
			t.Errorf("resolveDBPath(%q)=%q want %q", c.storePath, got, c.want)
		}
	}
}

// TestInitialize_SchemaCreation 对齐 Python _ensure_schema：四表创建
func TestInitialize_SchemaCreation(t *testing.T) {
	mgr := newTestManager(t, "agent-schema")
	s := mgr.Status()
	if !s["available"].(bool) {
		t.Fatalf("应 available")
	}
	// 数据库文件存在
	dbPath := filepath.Join(t.TempDir(), "does-not-exist-check") // 占位，实际用 mgr 内部路径
	_ = dbPath
}

// TestSyncMemoryFiles_增量跳过 对齐 Python：hash 未变跳过重新索引
func TestSyncMemoryFiles_增量跳过(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "memory"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "memory", "MEMORY.md"), []byte("# hello memory"), 0o644)

	mgr, err := GetMemoryManager(context.Background(), "agent-sync-test", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mgr.Close() }()

	s1 := mgr.Status()
	if s1["files"].(int) != 1 {
		t.Fatalf("初始同步应索引 1 个文件: %v", s1["files"])
	}
	if s1["chunks"].(int) < 1 {
		t.Fatalf("应有至少 1 个 chunk")
	}

	// 再 Sync：hash 未变，文件数不变
	if err := mgr.Sync(context.Background(), "manual", false); err != nil {
		t.Fatal(err)
	}
	s2 := mgr.Status()
	if s2["files"].(int) != 1 {
		t.Errorf("增量同步不应改变文件数")
	}
}

// TestSyncMemoryFiles_删除消失文件 对齐 Python _remove_file_from_index
func TestSyncMemoryFiles_删除消失文件(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "memory"), 0o755)
	p := filepath.Join(dir, "memory", "temp.md")
	_ = os.WriteFile(p, []byte("temp content"), 0o644)

	mgr, _ := GetMemoryManager(context.Background(), "agent-del-test", dir, nil)
	defer func() { _ = mgr.Close() }()
	if mgr.Status()["files"].(int) != 1 {
		t.Fatalf("初始应 1 文件")
	}

	_ = os.Remove(p)
	if err := mgr.Sync(context.Background(), "manual", true); err != nil { // force 触发全量
		t.Fatal(err)
	}
	if mgr.Status()["files"].(int) != 0 {
		t.Errorf("消失文件应被清理")
	}
}

// TestSearch_混合检索 对齐 Python search：mock 向量 + FTS 关键词
func TestSearch_混合检索(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "memory"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "memory", "notes.md"), []byte("# Project Alpha\n\nWe decided to use golang for the gateway service."), 0o644)

	mgr, _ := GetMemoryManager(context.Background(), "agent-search-test", dir, nil)
	defer func() { _ = mgr.Close() }()

	results, err := mgr.Search(context.Background(), "golang gateway", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatalf("FTS 应命中 'golang'/'gateway'")
	}
	r := results[0]
	if r["path"] != "memory/notes.md" && r["path"] != filepath.Join("memory", "notes.md") {
		t.Errorf("path 错误: %v", r["path"])
	}
	if r["snippet"] == "" {
		t.Errorf("snippet 不应为空")
	}
}

// TestSearch_minScore_默认值 对齐 Python 陷阱：实际生效 0.3（settings 默认）
func TestSearch_minScore_默认值(t *testing.T) {
	// mock provider 相似度不高时，0.3 阈值下的行为
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "memory"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "memory", "a.md"), []byte("apple banana"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "memory", "b.md"), []byte("zebra xyz"), 0o644)

	mgr, _ := GetMemoryManager(context.Background(), "agent-minscore-test", dir, nil)
	defer func() { _ = mgr.Close() }()

	// minScore=0.99：过滤掉低分结果
	results, _ := mgr.Search(context.Background(), "apple", map[string]any{"minScore": 0.99, "hybrid": map[string]any{"enabled": false}})
	for _, r := range results {
		if r["score"].(float64) < 0.99 {
			t.Errorf("应全部 >= 0.99: %v", r["score"])
		}
	}
}

// TestEmbeddingCache 对齐 Python _get_embedding 四键缓存
func TestEmbeddingCache(t *testing.T) {
	dir := t.TempDir()
	mgr0, _ := GetMemoryManager(context.Background(), "agent-embedcache-test", dir, nil)
	_ = mgr0.Close() // 只为触发 db 创建
	// 缓存行为通过搜索路径间接验证（provider mock 确定性，无外部副作用）
	_ = os.MkdirAll(filepath.Join(dir, "memory"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "memory", "x.md"), []byte("cache test content"), 0o644)
	mgr, _ := GetMemoryManager(context.Background(), "agent-embedcache-test2", dir, nil)
	defer func() { _ = mgr.Close() }()
	if _, err := mgr.Search(context.Background(), "cache test", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	// 二次搜索应命中缓存（不报错即通过）
	if _, err := mgr.Search(context.Background(), "cache test", map[string]any{}); err != nil {
		t.Fatal(err)
	}
}

// TestSyncSessionFiles 对齐 Python _sync_session_files：sessions/*.jsonl 索引
func TestSyncSessionFiles(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "sessions"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "sessions", "2026-01-01.jsonl"), []byte(`{"role":"user","content":"hello session"}`), 0o644)

	mgr, _ := GetMemoryManager(context.Background(), "agent-session-test", dir, nil)
	defer func() { _ = mgr.Close() }()
	s := mgr.Status()
	// sources 默认含 sessions
	found := false
	if counts, ok := s["sourceCounts"].([]map[string]any); ok {
		for _, c := range counts {
			if c["source"] == "sessions" && c["files"].(int) >= 1 {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("sessions 应被索引: %v", s["sourceCounts"])
	}
}

// TestFileWatcher_防抖同步 对齐 Python _setup_file_watcher + _schedule_watch_sync
func TestFileWatcher_防抖同步(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "memory"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "memory", "watch.md"), []byte("initial"), 0o644)

	mgr, _ := GetMemoryManager(context.Background(), "agent-watch-test", dir, nil)
	defer func() { _ = mgr.Close() }()

	// watcher 1s 后初始化 + 2s 防抖，等待后验证
	_ = os.WriteFile(filepath.Join(dir, "memory", "watch.md"), []byte("updated content"), 0o644)

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		results, _ := mgr.Search(context.Background(), "updated content", map[string]any{"minScore": 0.0, "hybrid": map[string]any{"enabled": false}})
		if len(results) > 0 {
			return // watcher 触发同步成功
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Errorf("watcher 未在 8s 内触发同步")
}

// TestStatus_结构完整 对齐 Python status() 字段
func TestStatus_结构完整(t *testing.T) {
	mgr := newTestManager(t, "agent-status")
	s := mgr.Status()
	for _, key := range []string{"available", "provider", "model", "files", "chunks", "sourceCounts", "dirty", "fts", "vector", "cache"} {
		if _, ok := s[key]; !ok {
			t.Errorf("status 缺少 %s", key)
		}
	}
	if s["provider"] != "mock" && s["provider"] != "openai_compatible" {
		t.Errorf("provider 异常: %v", s["provider"])
	}
}

// TestReadFile 对齐 Python read_file：行区间读取
func TestReadFile(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "memory"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "memory", "lines.md"), []byte("l1\nl2\nl3\nl4\nl5"), 0o644)

	mgr, _ := GetMemoryManager(context.Background(), "agent-readfile-test", dir, nil)
	defer func() { _ = mgr.Close() }()

	res, err := mgr.ReadFile(context.Background(), filepath.Join("memory", "lines.md"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res["totalLines"] != 5 {
		t.Errorf("totalLines got %v", res["totalLines"])
	}

	from := 2
	lines := 2
	res, err = mgr.ReadFile(context.Background(), filepath.Join("memory", "lines.md"), &from, &lines)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res["text"].(string), "l2") || !strings.Contains(res["text"].(string), "l3") {
		t.Errorf("行区间读取错误: %v", res["text"])
	}
	if strings.Contains(res["text"].(string), "l1") || strings.Contains(res["text"].(string), "l4") {
		t.Errorf("越界行: %v", res["text"])
	}
}

// TestClose_幂等 对齐 Python close：closed 标记 + 缓存清理
func TestClose_幂等(t *testing.T) {
	dir := t.TempDir()
	mgr, _ := GetMemoryManager(context.Background(), "agent-close-test", dir, nil)
	if err := mgr.Close(); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Close(); err != nil {
		t.Errorf("二次 Close 应幂等")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/swarm/agents/harness/common/memory/ -run 'TestGetMemoryManager|TestResolveDBPath|TestInitialize|TestSync|TestSearch|TestEmbeddingCache|TestFileWatcher|TestStatus|TestReadFile|TestClose' -v 2>&1 | tail -5
```
Expected: FAIL（`undefined: GetMemoryManager` 等）

- [ ] **Step 3: 写 manager.go（完整实现，核心结构如下——实现时逐函数对照 manager.py L56-1224）**

```go
package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	_ "github.com/mattn/go-sqlite3" // sqlite3 驱动
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/lite"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryIndexManager 记忆索引管理器。对齐 Python MemoryIndexManager（swarm 版）。
// 差异说明（vs agentcore lite 版）：本类用 workspaceDir 字符串 API；
// ListMemoryFiles 递归遍历 + 根目录 MEMORY.md；provider 构造走环境变量链。
type MemoryIndexManager struct {
	// AgentID Agent 标识
	AgentID string
	// WorkspaceDir 工作区目录
	WorkspaceDir string
	// Settings 记忆配置
	Settings *MemorySettings

	mu sync.Mutex

	db     *sql.DB
	dbPath string

	provider    EmbeddingProvider
	providerKey string

	dirty           bool
	sessionsDirty   bool
	sessionsDirtyFiles map[string]struct{}
	sessionWarm     map[string]struct{}
	closed          bool

	ftsEnabled   bool
	ftsAvailable bool
	ftsError     string
	vectorEnabled   bool
	vectorAvailable bool
	vectorError     string
	vectorDims      *int

	cacheEnabled bool

	watcher            *fsnotify.Watcher
	watcherInitialized bool
	watchTimer         *time.Timer
	watchMu            sync.Mutex

	intervalCancel context.CancelFunc

	// 会话增量脚手架字段。对齐 Python _session_pending_files/_session_deltas（Python 声明未用，Go 同步）
	sessionPendingFiles map[string]struct{}
	sessionDeltas       map[string]*SessionDeltaState
	sessionTimer        *time.Timer
}

// SessionDeltaState 会话增量状态。对齐 Python SessionDeltaState
type SessionDeltaState struct {
	LastSize       int
	PendingBytes   int
	PendingMessages int
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// metaKey 元数据键
	metaKey = "memory_index_meta_v1"
	// snippetMaxChars 摘要最大字符数
	snippetMaxChars = 700
	// vectorTable vec0 虚拟表
	vectorTable = "chunks_vec"
	// ftsTable FTS5 虚拟表
	ftsTable = "chunks_fts"
	// embeddingCacheTable 嵌入缓存表
	embeddingCacheTable = "embedding_cache"
	// sessionDirtyDebounceMs 会话脏防抖
	sessionDirtyDebounceMs = 5000
)

// ──────────────────────────── 全局变量 ────────────────────────────

// indexCache 管理器缓存。对齐 Python INDEX_CACHE
var indexCache sync.Map

// managerLogComponent 日志组件
var managerLogComponent = logger.ComponentAgentServer

// ──────────────────────────── 导出函数 ────────────────────────────

// GetMemoryManager 获取或创建记忆管理器。对齐 Python get_memory_manager
func GetMemoryManager(ctx context.Context, agentID, workspaceDir string, settings *MemorySettings) (*MemoryIndexManager, error) {
	if settings == nil {
		settings = CreateMemorySettings(workspaceDir, nil)
	}
	return getOrCreateManager(ctx, agentID, workspaceDir, settings)
}

// ClearMemoryManagerCache 清除管理器缓存。对齐 Python clear_memory_manager_cache
func ClearMemoryManagerCache() {
	indexCache.Range(func(key, _ any) bool {
		indexCache.Delete(key)
		return true
	})
}

// Initialize 初始化管理器。对齐 Python _initialize（顺序不可调换）：
// resolveDBPath → openDatabase → ensureSchema → initializeProvider →
// loadVectorExtension → Sync("initial") → setupFileWatcher → ensureIntervalSync
func (m *MemoryIndexManager) Initialize(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.dbPath = m.resolveDBPath()
	if err := EnsureDir(filepath.Dir(m.dbPath)); err != nil {
		return fmt.Errorf("确保数据库目录失败: %w", err)
	}

	db, err := sql.Open("sqlite3", m.dbPath+"?_busy_timeout=5000&_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return fmt.Errorf("打开数据库失败: %w", err)
	}
	db.SetMaxOpenConns(1) // 对齐 Python sqlite3 单连接语义
	m.db = db

	if err := m.ensureSchema(); err != nil {
		return fmt.Errorf("建 schema 失败: %w", err)
	}

	if err := m.initializeProvider(); err != nil {
		return err
	}

	if err := m.loadVectorExtension(); err != nil {
		m.vectorAvailable = false
		m.vectorError = err.Error()
		// 对齐 Python: logger.warning(f"Failed to load sqlite-vec extension: {e}")
		logger.Warn(managerLogComponent).Err(err).Msg("Failed to load sqlite-vec extension")
	}

	m.mu.Unlock()
	err = m.Sync(ctx, "initial", false)
	m.mu.Lock()
	if err != nil {
		// 对齐 Python：初始同步失败仅记录（不中断初始化）
		logger.Warn(managerLogComponent).Err(err).Msg("初始同步失败")
	}

	if m.Settings.Sync.Watch {
		m.setupFileWatcher()
	}
	m.ensureIntervalSync(ctx)

	// 对齐 Python: logger.info(f"Memory manager initialized for agent: {self.agent_id}")
	logger.Info(managerLogComponent).Str("agent_id", m.AgentID).Msg("Memory manager initialized for agent")
	return nil
}

// Sync 同步索引。对齐 Python sync
func (m *MemoryIndexManager) Sync(ctx context.Context, reason string, force bool) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	if force || m.shouldFullReindex() {
		// 对齐 Python: logger.info(f"Running full reindex (reason: {reason or 'unknown'})...")
		logger.Info(managerLogComponent).Str("reason", reason).Msg("Running full reindex")
		return m.runReindex(ctx)
	}

	// 对齐 Python: logger.debug(f"Memory sync (reason: {reason or 'unknown'})...")
	logger.Debug(managerLogComponent).Str("reason", reason).Msg("Memory sync")

	if containsSource(m.Settings.Sources, "memory") && m.dirty {
		if err := m.syncMemoryFiles(ctx); err != nil {
			return err
		}
		m.dirty = false
	}

	if containsSource(m.Settings.Sources, "sessions") {
		if err := m.syncSessionFiles(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Search 混合搜索。对齐 Python search（注意 minScore 实际生效 settings 默认 0.3，
// 代码内 get("minScore", 0.7) 的 0.7 是永不生效的死默认值）。
// 返回 []map[string]any：对齐 Python dict（下游 memory_search 会追加 citation 字段）。
func (m *MemoryIndexManager) Search(ctx context.Context, query string, opts map[string]any) ([]map[string]any, error) {
	// 对齐 Python: if self.settings.sync.get("onSearch", True) and self.dirty
	if m.Settings.Sync.OnSearch && m.dirty {
		if err := m.Sync(ctx, "search", false); err != nil {
			logger.Warn(managerLogComponent).Err(err).Msg("Memory sync failed (search)")
		}
	}

	cleaned := strings.TrimSpace(query)
	if cleaned == "" {
		return []map[string]any{}, nil
	}

	minScore := m.Settings.Query.MinScore
	if v, ok := opts["minScore"].(float64); ok {
		minScore = v
	}
	maxResults := m.Settings.Query.MaxResults
	if v, ok := opts["maxResults"].(float64); ok {
		maxResults = int(v)
	} else if v, ok := opts["maxResults"].(int); ok {
		maxResults = v
	}
	hybridEnabled := m.Settings.Query.Hybrid.Enabled
	candidateMultiplier := m.Settings.Query.Hybrid.CandidateMultiplier

	candidates := int(math_Min(200, math_Max(1, float64(maxResults)*candidateMultiplier)))

	var keywordResults []map[string]any
	if hybridEnabled && m.ftsAvailable {
		keywordResults = m.searchKeyword(cleaned, candidates)
	}

	queryVec := m.embedQueryWithTimeout(ctx, cleaned)
	hasVector := len(queryVec) > 0
	for _, v := range queryVec {
		if v != 0 {
			hasVector = true
			break
		}
	}
	if len(queryVec) == 0 {
		hasVector = false
	}

	var vectorResults []map[string]any
	if hasVector {
		vectorResults = m.searchVector(queryVec, candidates)
	}

	if !hybridEnabled {
		out := make([]map[string]any, 0, len(vectorResults))
		for _, r := range vectorResults {
			if r["score"].(float64) >= minScore {
				out = append(out, r)
			}
		}
		return out[:math_MinInt(len(out), maxResults)], nil
	}

	merged := mergeHybridResults(vectorResults, keywordResults,
		m.Settings.Query.Hybrid.VectorWeight, m.Settings.Query.Hybrid.TextWeight)

	out := make([]map[string]any, 0, len(merged))
	for _, r := range merged {
		if r["score"].(float64) >= minScore {
			out = append(out, r)
		}
	}
	return out[:math_MinInt(len(out), maxResults)], nil
}

// ReadFile 读取文件内容。对齐 Python read_file
func (m *MemoryIndexManager) ReadFile(ctx context.Context, relPath string, fromLine, lines *int) (map[string]any, error) {
	full := filepath.Join(m.WorkspaceDir, relPath)
	content, err := os.ReadFile(full)
	if err != nil {
		return nil, fmt.Errorf("File not found: %s", relPath)
	}
	allLines := strings.Split(string(content), "\n")
	total := len(allLines)

	start, end := 0, total
	if fromLine != nil {
		start = fromLine - 1
		if start < 0 {
			start = 0
		}
		end = total
		if lines != nil {
			end = start + *lines
			if end > total {
				end = total
			}
		}
	}
	selected := allLines[start:end]
	from := 1
	if fromLine != nil {
		from = *fromLine
	}
	return map[string]any{
		"path":       relPath,
		"text":       strings.Join(selected, "\n"),
		"totalLines": total,
		"fromLine":   from,
		"toLine":     from + len(selected) - 1,
	}, nil
}

// Status 系统状态报告。对齐 Python status()
func (m *MemoryIndexManager) Status() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.db == nil {
		return map[string]any{"available": false}
	}

	var fileCount, chunkCount int
	_ = m.db.QueryRow("SELECT COUNT(*) FROM files").Scan(&fileCount)
	_ = m.db.QueryRow("SELECT COUNT(*) FROM chunks").Scan(&chunkCount)

	rows, err := m.db.Query("SELECT source, COUNT(*) FROM files GROUP BY source")
	if err != nil {
		return map[string]any{"available": false}
	}
	defer func() { _ = rows.Close() }()
	var sourceCounts []map[string]any
	for rows.Next() {
		var src string
		var cnt int
		if err := rows.Scan(&src, &cnt); err == nil {
			sourceCounts = append(sourceCounts, map[string]any{"source": src, "files": cnt})
		}
	}

	provider, model := "", ""
	if m.provider != nil {
		provider = m.provider.ID()
		model = m.provider.Model()
	}

	return map[string]any{
		"available":    true,
		"provider":     provider,
		"model":        model,
		"files":        fileCount,
		"chunks":       chunkCount,
		"sourceCounts": sourceCounts,
		"dirty":        m.dirty,
		"fts": map[string]any{
			"enabled": m.ftsEnabled, "available": m.ftsAvailable, "error": m.ftsError,
		},
		"vector": map[string]any{
			"enabled": m.vectorEnabled, "available": m.vectorAvailable,
			"error": m.vectorError, "dims": m.vectorDims,
		},
		"cache": map[string]any{
			"enabled": m.cacheEnabled, "entries": m.cacheEntryCount(),
		},
	}
}

// Close 关闭管理器。对齐 Python close（幂等）
func (m *MemoryIndexManager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	m.mu.Unlock()

	if m.intervalCancel != nil {
		m.intervalCancel()
	}
	m.watchMu.Lock()
	if m.watchTimer != nil {
		m.watchTimer.Stop()
	}
	m.watchMu.Unlock()
	if m.watcher != nil {
		_ = m.watcher.Close()
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.db != nil {
		_ = m.db.Close()
	}
	indexCache.Delete(m.cacheKey())
	// 对齐 Python: logger.info("Memory manager closed")
	logger.Info(managerLogComponent).Msg("Memory manager closed")
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getOrCreateManager 幂等获取。对齐 Python MemoryIndexManager.get + INDEX_CACHE
func getOrCreateManager(ctx context.Context, agentID, workspaceDir string, settings *MemorySettings) (*MemoryIndexManager, error) {
	key := agentID + ":" + workspaceDir
	if cached, ok := indexCache.Load(key); ok {
		mgr := cached.(*MemoryIndexManager)
		if !mgr.closed {
			return mgr, nil
		}
	}

	m := &MemoryIndexManager{
		AgentID:      agentID,
		WorkspaceDir: workspaceDir,
		Settings:     settings,
		dirty:        true,
		ftsEnabled:   settings.Store.FtsEnabled,
		vectorEnabled: settings.Store.VectorEnabled,
		cacheEnabled: settings.Cache.Enabled,
	}

	if err := m.Initialize(ctx); err != nil {
		// 对齐 Python: logger.error(f"Failed to initialize memory manager: {e}")
		logger.Error(managerLogComponent).Err(err).Msg("Failed to initialize memory manager")
		return nil, err
	}
	indexCache.Store(key, m)
	return m, nil
}

// cacheKey 缓存键。对齐 Python f"{agent_id}:{workspace_dir}"
func (m *MemoryIndexManager) cacheKey() string {
	return m.AgentID + ":" + m.WorkspaceDir
}

// resolveDBPath 解析数据库路径。对齐 Python _resolve_db_path
func (m *MemoryIndexManager) resolveDBPath() string {
	storePath := m.Settings.Store.Path
	if filepath.IsAbs(storePath) {
		return storePath
	}
	workspaceName := filepath.Base(m.WorkspaceDir)
	if strings.HasPrefix(storePath, workspaceName+"/") || strings.HasPrefix(storePath, workspaceName+`\`) {
		storePath = storePath[len(workspaceName)+1:]
	}
	if !strings.HasPrefix(storePath, "memory/") && !strings.HasPrefix(storePath, `memory\`) {
		storePath = filepath.Join("memory", storePath)
	}
	return filepath.Join(m.WorkspaceDir, storePath)
}

// ensureSchema 建表。对齐 Python _ensure_schema（meta/files/chunks/embedding_cache + FTS5 contentless_delete）
func (m *MemoryIndexManager) ensureSchema() error {
	// …… 实现时对照 manager.py L183-276 逐表 CREATE（含 FTS5 contentless_delete=1 虚拟表，
	// 失败时 ftsAvailable=false + ftsError 记录 + warn 日志，对齐 Python try/except）
	return nil // 占位——实现时补全，见下方"实现要点"
}

// initializeProvider 初始化提供者。对齐 Python _initialize_provider
func (m *MemoryIndexManager) initializeProvider() error {
	provider, err := CreateEmbeddingProvider(m.Settings.Provider, m.Settings.Model, m.Settings.Fallback)
	if err != nil {
		// 对齐 Python: logger.error(f"Failed to initialize embedding provider: {e}")
		logger.Error(managerLogComponent).Err(err).Msg("Failed to initialize embedding provider")
		return err
	}
	m.provider = provider
	m.providerKey = provider.ID() + ":" + provider.Model()
	// 对齐 Python: logger.info(f"Embedding provider: {self.provider.id} / {self.provider.model}")
	logger.Info(managerLogComponent).Str("provider", provider.ID()).Str("model", provider.Model()).Msg("Embedding provider")
	return nil
}

// loadVectorExtension 加载 vec0 扩展。对齐 Python _load_vector_extension（复用 lite vec loader）
func (m *MemoryIndexManager) loadVectorExtension() error {
	if !m.vectorEnabled || m.db == nil {
		return nil
	}
	vecPath := lite.ResolveVec0Path()
	if _, err := os.Stat(vecPath); err != nil {
		return fmt.Errorf("vec0 不存在: %s", vecPath)
	}
	conn, err := m.db.Conn(context.Background())
	if err != nil {
		return fmt.Errorf("获取连接失败: %w", err)
	}
	defer func() { _ = conn.Close() }()
	err = conn.Raw(func(driverConn any) error {
		dc, ok := driverConn.(sqlite3.SQLiteConn别名见实现说明)
		_ = ok
		return lite.LoadVec0Extension(dcAsLiteConn, vecPath)
	})
	if err != nil {
		return fmt.Errorf("加载 vec0 扩展失败: %w", err)
	}
	m.vectorAvailable = true
	// 对齐 Python: logger.info("sqlite-vec extension loaded successfully")
	logger.Info(managerLogComponent).Msg("sqlite-vec extension loaded successfully")
	return nil
}

// …… 以下函数实现时对照 manager.py 对应行号逐个补全（签名固定，函数体按 Python 一比一）：
//
// shouldFullReindex() bool                     — L495-522（meta 三比对 provider/model/chunkTokens）
// runReindex(ctx) error                        — L524-543（sync + writeMeta 含 vectorDims）
// isRecentSessionFile(filename) bool           — L545-572（Python 定义未调用，Go 同步实现）
// syncMemoryFiles(ctx) error                   — L574-604（ListMemoryFiles + hash 跳过 + 删除消失）
// syncSessionFiles(ctx) error                  — L606-639（sessions/*.jsonl）
// indexFile(ctx, entry, source) error          — L641-668（读文件 → ChunkMarkdown → 三表写 + commit/rollback）
// indexChunk(ctx, path, source, chunk) error   — L670-713（RETURNING rowid + FTS + vec_f32）
// removeFileFromIndex(path)                    — L715-742（vector/fts/chunks/files 四表清理）
// getEmbedding(ctx, text) []float64            — L744-779（四键缓存查 + miss 写回）
// searchVector(vec, limit) []map[string]any    — L842-912（vec_distance_cosine，score=max(0,1-d/2)）
// searchVectorFallback(vec, limit)             — L914-962（内存余弦，向量归一化后逐行算）
// searchKeyword(query, limit)                  — L964-1024（FTS5 MATCH + bm25_rank_to_score）
// buildSourceFilter() string                   — L1026-1035（SQL IN 拼接，repr 单引号）
// mergeHybridResults(v, k, vw, tw)             — L1037-1068（by_id 加权合并）
// embedQueryWithTimeout(ctx, q) []float64      — L1070-1083（60s 超时，失败返回空）
// writeMeta(meta)                              — L1119-1125
// cacheEntryCount() int                        — L1173-1179
// setupFileWatcher()                           — L339-409（fsnotify，watchPaths: 根MEMORY.md/memory目录/extraPaths，1s 延迟初始化）
// watchLoop()                                  — 事件循环 + IsMemoryPath 过滤 + scheduleWatchSync
// scheduleWatchSync()                          — L416-447（防抖 watchDebounceMs）
// ensureIntervalSync(ctx)                      — L449-468（intervalMinutes goroutine）
```

**实现要点（对照 Python 行号逐一落实，本步必须全部完成）：**

1. **ensureSchema**（L183-276）：meta/files/chunks 三张普通表 + embedding_cache 表，全部 `CREATE TABLE IF NOT EXISTS`；FTS5 表用 `contentless_delete=1` 语法：`CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(id UNINDEXED, path UNINDEXED, source UNINDEXED, text, content='', contentless_delete=1)`；FTS 创建失败（编译期无 FTS5 支持时）→ `ftsAvailable=false` + warn（对齐 Python try/except）
2. **indexChunk**（L670-713）：`INSERT OR REPLACE INTO chunks (...) VALUES (...) RETURNING rowid`（mattn/go-sqlite3 支持 RETURNING）；FTS 写入用 `(rowid, id, path, source, text)`；vector 写入 `INSERT OR REPLACE INTO chunks_vec (rowid, embedding) VALUES (?, vec_f32(?))`；`vectorToBlob` 用 `math.Float32bits` + binary.LittleEndian.PutUint32 打包（对齐 Python struct.pack float32）
3. **searchVector**（L842-912）：先 `SELECT rowid,... FROM chunks WHERE {source_filter}` 建 chunk_map，再 `SELECT rowid, vec_distance_cosine(embedding, vec_f32(?)) as distance FROM chunks_vec WHERE rowid IN (...) ORDER BY distance LIMIT ?`；score = max(0, 1-d/2)；异常时降级 searchVectorFallback（对齐 Python try/except + debug 日志）
4. **searchVectorFallback**（L914-962）：查询归一化 → 逐行 blob_to_vector + cosine → 排序截断
5. **searchKeyword**（L964-1024）：`BuildFTSQuery` → FTS MATCH → `bm25_rank_to_score(rank)`；chunk_map 同上
6. **buildSourceFilter**（L1026-1035）：`source IN ('memory', 'sessions')`（对齐 Python repr 单引号）
7. **mergeHybridResults**（L1037-1068）：by_id 合并，`score = vw*vector + tw*text`，排序
8. **getEmbedding**（L744-779）：`SELECT embedding FROM embedding_cache WHERE provider=? AND model=? AND provider_key=? AND hash=?`；miss 时 provider.EmbedQuery + INSERT OR REPLACE（updated_at 用 time.Now().Unix()）
9. **syncMemoryFiles**（L574-604）：ListMemoryFiles → BuildFileEntry → `SELECT hash FROM files WHERE path=? AND source='memory'` 未变跳过 → indexFile；最后删除 active_paths 之外的记录
10. **setupFileWatcher**（L339-409）：watchPaths = 根目录 MEMORY.md/memory.md（存在且非 symlink）+ memory 目录（递归）+ extraPaths；文件监听父目录（非递归）、目录递归监听；1s 后 watcherInitialized=true
11. **embedQueryWithTimeout**（L1070-1083）：`context.WithTimeout(ctx, 60*time.Second)`；超时 warn "Embedding query timed out" 返回 nil；错误 error 返回 nil
12. **status 中的 vectorDims**：`*int` 类型（nil 表示未建表）
13. **close**（L1181-1209）：停 intervalCancel/watchTimer/sessionTimer → 关 watcher → 关 db → 清 INDEX_CACHE

- [ ] **Step 4: 运行测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/swarm/agents/harness/common/memory/ -run 'TestGetMemoryManager|TestResolveDBPath|TestInitialize|TestSync|TestSearch|TestEmbeddingCache|TestFileWatcher|TestStatus|TestReadFile|TestClose' -v 2>&1 | tail -8
```
Expected: 全部 PASS（vec0 缺失时自动走内存余弦 fallback，不影响测试）

注意：`math_Min/math_Max/math_MinInt` 是计划伪码示意，实现时直接用 Go 1.21+ 内建 `min/max` 或写辅助函数，命名自定但不能与导入冲突。

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uapclaw-gateway && git add internal/swarm/agents/harness/common/memory/manager.go internal/swarm/agents/harness/common/memory/manager_test.go && git commit -m "feat(swarm-memory): MemoryIndexManager 全量实现（sync/search/watcher/缓存，对齐 swarm 版 manager.py）"
```

---

## Task 5: 回填 adapter 三处 + doc.go + 计划文档

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter.go:652-655`
- Modify: `internal/swarm/server/adapter/deep_adapter_rails.go:1419-1540`（删除四个私有函数，调用点改 commmem）
- Modify: `internal/swarm/agents/harness/common/memory/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md:711`

- [ ] **Step 1: 修改 deep_adapter.go ReloadAgentConfig**

在 `internal/swarm/server/adapter/deep_adapter.go` 中：

1. import 块添加：
```go
commmem "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/memory"
```
若 `lite` import 仅剩此处使用则删除（检查文件内其他 `lite.` 引用）。

2. L652-655 处替换：
```go
	// Python: clear_config_cache() + clear_memory_manager_cache()
	// ✅ 已回填：ClearConfigCache + ClearMemoryManagerCache（对齐 Python import 源 jiuwenswarm...memory）
	commmem.ClearConfigCache()
	commmem.ClearMemoryManagerCache()
```

- [ ] **Step 2: 修改 deep_adapter_rails.go 迁移四函数**

在 `internal/swarm/server/adapter/deep_adapter_rails.go` 中：

1. import 块添加：
```go
commmem "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/memory"
```

2. **删除**四个私有函数（约 L1461-1540 区域）：`getMemoryMode`、`isMemoryEnabled`、`isProactiveMemory`、`resolveModeMemory`（函数体已迁移到 swarm memory config.go 导出版）。
   ⚠️ **保留** `isBuiltinMemoryAllowed`、`getMemoryEngine`、`isExternalMemoryEnabled`（属 external_memory_config.py，10.6.16 范围）。

3. 调用点替换（保持行为不变，仅换调用源）：
   - L690: `if getMemoryMode(d.configCache) == "local"` → `if commmem.GetMemoryMode(d.configCache) == "local"`
   - L692: `builtinOn := isBuiltinMemoryAllowed(d.configCache) && isMemoryEnabled(mode, d.configCache)` → `builtinOn := isBuiltinMemoryAllowed(d.configCache) && commmem.IsMemoryEnabled(mode, d.configCache)`
   - L698: `curProactive := isProactiveMemory(mode, d.configCache)` → `curProactive := commmem.IsProactiveMemory(mode, d.configCache)`
   - L805: `isProactive := isProactiveMemory(mode, d.configCache)` → `isProactive := commmem.IsProactiveMemory(mode, d.configCache)`
   
   若同文件还有 `resolveModeMemory` 的其他调用点（用 grep 核实），一并替换为 `commmem.ResolveModeMemory`。

- [ ] **Step 3: 编译 + 全量回归 adapter 包**

```bash
cd /home/opensource/uapclaw-gateway && pgrep -f 'go (build|test)' ; export GOPROXY=https://goproxy.cn,direct && go build ./internal/swarm/... && go test ./internal/swarm/server/adapter/ 2>&1 | tail -5
```
Expected: BUILD OK + adapter 全部测试 PASS（回填是等价替换，不应有行为变化）

- [ ] **Step 4: 更新 doc.go**

`internal/swarm/agents/harness/common/memory/doc.go` 全文替换为：

```go
// Package memory 提供 Swarm 侧的记忆相关功能。
//
// 本包对齐 Python jiuwenswarm/agents/harness/common/memory/，
// 是 agentcore lite 记忆包的产品侧完整 fork：workspace_dir 字符串 API、
// 配置自查（config.yaml embed/memory 段 + 环境变量链）、Mock 降级、
// INDEX_CACHE 全局缓存。消费者为 memory_rpc（TUI 通道）。
// 与 internal/agentcore/memory/lite/ 各自对齐各自 Python 基线；
// 唯一共享点：vec0 扩展加载器（lite.ResolveVec0Path/LoadVec0Extension，
// 对应 Python 两包共用第三方库 sqlite_vec）。
//
// 文件目录：
//
//	memory/
//	├── doc.go          # 包文档
//	├── types.go        # 记忆数据类型（MemorySearchResult/FileEntry/MemoryChunk 等）
//	├── internal.go     # 纯函数（递归 ListMemoryFiles/ChunkMarkdown/FTS 构建等，swarm 语义）
//	├── config.go       # 配置（GetEmbedConfig/MemorySettings/mode 解析 + 包内缓存）
//	├── embeddings.go   # EmbeddingProvider 接口 + OpenAICompatible + Mock + 工厂（10.6.15）
//	├── manager.go      # MemoryIndexManager（sync/混合检索/文件监听/嵌入缓存）
//	├── forbidden.go    # 记忆禁止配置与提示词生成
//	└── dreaming/       # Dreaming 记忆整理（Sweeper 业务管线）
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/memory/
package memory
```

- [ ] **Step 5: 更新 IMPLEMENTATION_PLAN.md**

`IMPLEMENTATION_PLAN.md` L711 行：

```markdown
| 10.6.13-18 | 🔄 | Swarm Memory | Config✅(swarm版config.go+包内缓存+ClearConfigCache回填)/Dreaming✅/Embeddings✅(Provider接口+OpenAICompatible+Mock+工厂，含manager/internal/types依赖链补齐)/External/Forbidden✅/RPC | `jiuwenswarm/agents/harness/common/memory/` |
```

同时检查 L671（10.3.2 行）中 ClearMemoryManagerCache 相关注记如需更新指向则同步修改。

- [ ] **Step 6: 全量验证覆盖率 ≥85%**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test -cover ./internal/swarm/agents/harness/common/memory/... 2>&1 | tail -3
```
Expected: `ok ... coverage: 8x.x%`（≥85%，不足则补测试：重点查 manager.go 的 intervalSync/close/watchLoop 分支）

- [ ] **Step 7: 提交**

```bash
cd /home/opensource/uapclaw-gateway && git add -A && git commit -m "refactor(swarm-memory): 回填 adapter 三处调用点（ClearConfigCache+ClearMemoryManagerCache 指向 swarm 包 + 四函数迁移导出）+ doc/计划文档同步"
```

---

## 验证清单（完成标准）

- [ ] `go build ./internal/...` 通过
- [ ] `go test ./internal/swarm/agents/harness/common/memory/... -cover` ≥85%
- [ ] `go test ./internal/swarm/server/adapter/` 全部通过（回填无行为变化）
- [ ] Python 对齐抽查：初始化顺序 / minScore=0.3 / provider_key 格式 / 四键缓存 / index 排序 / vec0 降级路径
- [ ] IMPLEMENTATION_PLAN.md 10.6.13-18 已更新
- [ ] 每个函数注释含 Python 行号对照
