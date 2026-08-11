# 7.2 CodingMemoryTools + 7.5 Frontmatter 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现领域七 7.2（CodingMemoryTools + 通用记忆工具）及前置依赖 7.5（Frontmatter 解析），对齐 Python `openjiuwen/core/memory/lite/` 下对应文件。

**Architecture:** 7.2 的编程记忆工具（coding_memory_tool_ops）是 Agent 与记忆系统的交互层，提供 read/write/edit 三个核心操作。其中 write 最复杂，包含 frontmatter 解析验证、语义相似搜索、文件级锁+索引锁+乐观快照重试的三层并发控制、MEMORY.md 索引更新。通用记忆工具（tool_ops）逻辑简单，直接通过 SysOperation.Fs() 读写。先实现 7.5 frontmatter，再实现通用工具，最后实现编程记忆工具。

**Tech Stack:** Go 1.22+, sync.Mutex, context.Context, SysOperation.Fs() 文件接口, MemoryIndexManager.Search 语义搜索

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| 修改 | `internal/agentcore/memory/lite/frontmatter.go` | 7.5 frontmatter 5 个纯函数 |
| 修改 | `internal/agentcore/memory/lite/tool_ops.go` | 通用记忆工具 6 个函数 |
| 修改 | `internal/agentcore/memory/lite/coding_memory_tool_ops.go` | 编程记忆工具 4 个导出函数 + 6 个非导出函数 |
| 修改 | `internal/agentcore/memory/lite/tools.go` | InitMemoryManagerAsync |
| 修改 | `internal/agentcore/memory/lite/conflict_types.go` | 已修正（WriteMode.String + ToDict 对齐 Python） |
| 修改 | `internal/agentcore/memory/lite/tool_context_base.go` | 已修正（EnsureManager 实现） |
| 修改 | `internal/agentcore/memory/lite/coding_memory_tool_context.go` | 已修正（去掉 NodeName 遮蔽） |
| 修改 | `internal/agentcore/memory/lite/manager_impl.go` | 已修正（添加 IsClosed 方法） |
| 新建 | `internal/agentcore/memory/lite/frontmatter_test.go` | frontmatter 测试 |
| 新建 | `internal/agentcore/memory/lite/tool_ops_test.go` | 通用记忆工具测试 |
| 新建 | `internal/agentcore/memory/lite/coding_memory_tool_ops_test.go` | 编程记忆工具测试 |
| 修改 | `internal/agentcore/memory/lite/doc.go` | 更新文件目录描述 |
| 修改 | `IMPLEMENTATION_PLAN.md` | 更新 7.2 + 7.5 状态 |

---

## 回填标注约定

依赖未实现组件的位置用 `⤵️ 回填: 7.x` 注释标注，后续实现时搜索替换：

| 标注 | 含义 | 出现位置 |
|------|------|---------|
| `⤵️ 回填: 7.8 MemUpdateChecker` | LLM 冲突检测，当前 runChecker 返回空列表 | coding_memory_tool_ops.go |
| `⤵️ 回填: 7.3 CodingMemoryToolContext` | 上下文完整初始化，当前 EnsureManager 已实现 | coding_memory_tool_ops.go |

---

### Task 1: 实现 7.5 frontmatter.go

**Files:**
- 修改: `internal/agentcore/memory/lite/frontmatter.go`
- 新建: `internal/agentcore/memory/lite/frontmatter_test.go`

对齐 Python `openjiuwen/core/memory/lite/frontmatter.py`。5 个纯函数，无外部依赖。

- [ ] **Step 1: 编写 frontmatter_test.go 失败测试**

```go
package lite

import "testing"

// TestParseFrontmatter_正常内容 测试正常 frontmatter 解析
func TestParseFrontmatter_正常内容(t *testing.T) {
	content := "---\nname: test\ndescription: 测试\ntype: user\n---\n正文内容"
	fm := ParseFrontmatter(content)
	if fm == nil {
		t.Fatal("Expected non-nil frontmatter")
	}
	if fm["name"] != "test" {
		t.Errorf("Expected name=test, got %s", fm["name"])
	}
	if fm["description"] != "测试" {
		t.Errorf("Expected description=测试, got %s", fm["description"])
	}
	if fm["type"] != "user" {
		t.Errorf("Expected type=user, got %s", fm["type"])
	}
}

// TestParseFrontmatter_无Frontmatter 测试无 frontmatter
func TestParseFrontmatter_无Frontmatter(t *testing.T) {
	fm := ParseFrontmatter("纯正文内容")
	if fm != nil {
		t.Errorf("Expected nil for content without frontmatter, got %v", fm)
	}
}

// TestParseFrontmatter_无结束标记 测试无结束标记
func TestParseFrontmatter_无结束标记(t *testing.T) {
	fm := ParseFrontmatter("---\nname: test")
	if fm != nil {
		t.Errorf("Expected nil for unclosed frontmatter, got %v", fm)
	}
}

// TestParseFrontmatter_空Frontmatter 测试空 frontmatter
func TestParseFrontmatter_空Frontmatter(t *testing.T) {
	fm := ParseFrontmatter("---\n---\n正文")
	if fm != nil {
		t.Errorf("Expected nil for empty frontmatter, got %v", fm)
	}
}

// TestValidateFrontmatter_合法 测试合法 frontmatter
func TestValidateFrontmatter_合法(t *testing.T) {
	fm := map[string]string{"name": "n", "description": "d", "type": "user"}
	ok, err := ValidateFrontmatter(fm)
	if !ok {
		t.Errorf("Expected valid, got error: %s", err)
	}
}

// TestValidateFrontmatter_缺字段 测试缺少必填字段
func TestValidateFrontmatter_缺字段(t *testing.T) {
	fm := map[string]string{"name": "n", "description": "d"}
	ok, err := ValidateFrontmatter(fm)
	if ok {
		t.Error("Expected invalid for missing type")
	}
	if err == "" {
		t.Error("Expected error message")
	}
}

// TestValidateFrontmatter_非法类型 测试非法 type
func TestValidateFrontmatter_非法类型(t *testing.T) {
	fm := map[string]string{"name": "n", "description": "d", "type": "invalid"}
	ok, err := ValidateFrontmatter(fm)
	if ok {
		t.Error("Expected invalid for bad type")
	}
	if err == "" {
		t.Error("Expected error message")
	}
}

// TestEnrichFrontmatter_创建 测试创建时填充时间戳
func TestEnrichFrontmatter_创建(t *testing.T) {
	fm := map[string]string{"name": "n"}
	result := EnrichFrontmatter(fm, false)
	if _, ok := result["created_at"]; !ok {
		t.Error("Expected created_at to be set")
	}
	if _, ok := result["updated_at"]; !ok {
		t.Error("Expected updated_at to be set")
	}
}

// TestEnrichFrontmatter_编辑 测试编辑时不覆盖 created_at
func TestEnrichFrontmatter_编辑(t *testing.T) {
	fm := map[string]string{"name": "n", "created_at": "2025-01-01"}
	result := EnrichFrontmatter(fm, true)
	if result["created_at"] != "2025-01-01" {
		t.Errorf("Expected created_at preserved, got %s", result["created_at"])
	}
	if _, ok := result["updated_at"]; !ok {
		t.Error("Expected updated_at to be set")
	}
}

// TestRebuildContentWithFrontmatter 测试重建内容
func TestRebuildContentWithFrontmatter(t *testing.T) {
	content := "---\nname: old\n---\n正文内容"
	fm := map[string]string{"name": "new", "type": "user", "description": "d"}
	result := RebuildContentWithFrontmatter(content, fm)
	if result == "" {
		t.Fatal("Expected non-empty result")
	}
	// 应包含 frontmatter
	if len(result) < 10 {
		t.Errorf("Result too short: %s", result)
	}
	// 应包含 body
	if !containsStr(result, "正文内容") {
		t.Errorf("Expected body preserved in result: %s", result)
	}
}

// TestExtractBody 测试提取 body
func TestExtractBody(t *testing.T) {
	content := "---\nname: test\n---\n正文内容"
	body := ExtractBody(content)
	if body != "正文内容" {
		t.Errorf("Expected '正文内容', got '%s'", body)
	}
}

// TestExtractBody_无Frontmatter 测试无 frontmatter 时提取 body
func TestExtractBody_无Frontmatter(t *testing.T) {
	content := "纯正文内容"
	body := ExtractBody(content)
	if body != "纯正文内容" {
		t.Errorf("Expected '纯正文内容', got '%s'", body)
	}
}

// TestExtractBody_空Body 测试空 body
func TestExtractBody_空Body(t *testing.T) {
	content := "---\nname: test\n---\n"
	body := ExtractBody(content)
	if body != "" {
		t.Errorf("Expected empty body, got '%s'", body)
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || searchStr(s, sub))
}

func searchStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/lite/ -run TestParseFrontmatter -v 2>&1 | head -20`
Expected: 测试失败（当前 ParseFrontmatter 返回 nil）

- [ ] **Step 3: 实现 frontmatter.go**

替换 `internal/agentcore/memory/lite/frontmatter.go` 全部内容：

```go
package lite

import (
	"strings"
	"time"
)

// ──────────────────────────── 常量 ────────────────────────────

// ValidTypes 合法的记忆类型。对齐 Python VALID_TYPES
var ValidTypes = []string{"user", "feedback", "project", "reference"}

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseFrontmatter 解析 --- frontmatter。对齐 Python parse_frontmatter
func ParseFrontmatter(content string) map[string]string {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "---") {
		return nil
	}
	end := strings.Index(content[3:], "---")
	if end == -1 {
		return nil
	}
	result := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(content[3:3+end]), "\n") {
		if idx := strings.Index(line, ":"); idx != -1 {
			key := strings.TrimSpace(line[:idx])
			value := strings.TrimSpace(line[idx+1:])
			result[key] = value
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// ValidateFrontmatter 验证 name/description/type 字段。对齐 Python validate_frontmatter
func ValidateFrontmatter(fm map[string]string) (bool, string) {
	for _, field := range []string{"name", "description", "type"} {
		if fm[field] == "" {
			return false, "缺少必填字段: " + field
		}
	}
	valid := false
	for _, t := range ValidTypes {
		if fm["type"] == t {
			valid = true
			break
		}
	}
	if !valid {
		return false, "type 必须是以下之一: user, feedback, project, reference"
	}
	return true, ""
}

// EnrichFrontmatter 自动填充 created_at/updated_at。对齐 Python enrich_frontmatter
func EnrichFrontmatter(fm map[string]string, isEdit bool) map[string]string {
	today := time.Now().Format("2006-01-02")
	if !isEdit {
		if _, ok := fm["created_at"]; !ok {
			fm["created_at"] = today
		}
	}
	fm["updated_at"] = today
	return fm
}

// RebuildContentWithFrontmatter 用更新后的 frontmatter 重建文件内容。对齐 Python rebuild_content_with_frontmatter
func RebuildContentWithFrontmatter(content string, fm map[string]string) string {
	body := ExtractBody(content)
	var fmLines []string
	fmLines = append(fmLines, "---")
	for key, value := range fm {
		fmLines = append(fmLines, key+": "+value)
	}
	fmLines = append(fmLines, "---")
	parts := []string{strings.Join(fmLines, "\n")}
	if body != "" {
		parts = append(parts, body)
	}
	return strings.Join(parts, "\n\n")
}

// ExtractBody 提取 frontmatter 后的 body 内容。对齐 Python _extract_body
func ExtractBody(content string) string {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "---") {
		return content
	}
	end := strings.Index(content[3:], "---")
	if end == -1 {
		return ""
	}
	return strings.TrimSpace(content[3+end+3:])
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/lite/ -run "TestParseFrontmatter|TestValidateFrontmatter|TestEnrichFrontmatter|TestRebuildContentWithFrontmatter|TestExtractBody" -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/memory/lite/frontmatter.go internal/agentcore/memory/lite/frontmatter_test.go
git commit -m "feat(memory/lite): 实现 7.5 frontmatter 解析/验证/重建"
```

---

### Task 2: 实现 tool_ops.go（通用记忆工具）

**Files:**
- 修改: `internal/agentcore/memory/lite/tool_ops.go`
- 新建: `internal/agentcore/memory/lite/tool_ops_test.go`

对齐 Python `openjiuwen/core/memory/lite/memory_tool_ops.py`。6 个函数，逻辑简单。

- [ ] **Step 1: 编写 tool_ops_test.go 失败测试**

```go
package lite

import (
	"context"
	"testing"
)

// TestValidateMemoryPath_非法穿越 测试目录穿越
func TestValidateMemoryPath_非法穿越(t *testing.T) {
	ws := newTestWorkspace(t)
	ok, _ := ValidateMemoryPath("../etc/passwd", ws)
	if ok {
		t.Error("Expected path traversal to be rejected")
	}
}

// TestValidateMemoryPath_绝对路径 测试绝对路径
func TestValidateMemoryPath_绝对路径(t *testing.T) {
	ws := newTestWorkspace(t)
	ok, _ := ValidateMemoryPath("/etc/passwd", ws)
	if ok {
		t.Error("Expected absolute path to be rejected")
	}
}

// TestValidateMemoryPath_合法路径 测试合法路径
func TestValidateMemoryPath_合法路径(t *testing.T) {
	ws := newTestWorkspace(t)
	ok, resolved := ValidateMemoryPath("test.md", ws)
	if !ok {
		t.Error("Expected valid path")
	}
	if resolved == "" {
		t.Error("Expected resolved path")
	}
}

// TestValidateMemoryPath_UserMD 测试 USER.md 路径解析
func TestValidateMemoryPath_UserMD(t *testing.T) {
	ws := newTestWorkspace(t)
	ok, _ := ValidateMemoryPath("USER.md", ws)
	if !ok {
		t.Error("Expected USER.md to be valid")
	}
}

// TestMemorySearchWithContext_无Manager 测试无 manager
func TestMemorySearchWithContext_无Manager(t *testing.T) {
	ctx := context.Background()
	toolCtx := &MemoryToolContext{}
	result := MemorySearchWithContext(ctx, toolCtx, "test", nil, nil, "")
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if result["disabled"] != true {
		t.Error("Expected disabled=true")
	}
}

// TestReadMemoryWithContext_无Workspace 测试无 workspace
func TestReadMemoryWithContext_无Workspace(t *testing.T) {
	ctx := context.Background()
	toolCtx := &MemoryToolContext{}
	result := ReadMemoryWithContext(ctx, toolCtx, "test.md", nil, nil)
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if result["success"] != false {
		t.Error("Expected success=false")
	}
}

// TestEditMemoryWithContext_无Workspace 测试无 workspace
func TestEditMemoryWithContext_无Workspace(t *testing.T) {
	ctx := context.Background()
	toolCtx := &MemoryToolContext{}
	result := EditMemoryWithContext(ctx, toolCtx, "test.md", "old", "new")
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if result["success"] != false {
		t.Error("Expected success=false")
	}
}

// TestWriteMemoryWithContext_无Workspace 测试无 workspace
func TestWriteMemoryWithContext_无Workspace(t *testing.T) {
	ctx := context.Background()
	toolCtx := &MemoryToolContext{}
	result := WriteMemoryWithContext(ctx, toolCtx, "test.md", "content", false)
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if result["success"] != false {
		t.Error("Expected success=false")
	}
}

// TestLineRangeToFsRead 测试行范围转换
func TestLineRangeToFsRead(t *testing.T) {
	// 无 offset
	result := lineRangeToFsRead(nil, nil)
	if result != nil {
		t.Errorf("Expected nil, got %v", result)
	}
	// offset + limit
	offset := 5
	limit := 10
	result = lineRangeToFsRead(&offset, &limit)
	if result == nil {
		t.Fatal("Expected non-nil")
	}
	if (*result)[0] != 5 || (*result)[1] != 14 {
		t.Errorf("Expected [5, 14], got %v", *result)
	}
	// offset 无 limit
	result = lineRangeToFsRead(&offset, nil)
	if result == nil {
		t.Fatal("Expected non-nil")
	}
	if (*result)[0] != 5 || (*result)[1] != -1 {
		t.Errorf("Expected [5, -1], got %v", *result)
	}
}

// TestViewLines 测试行视图切片
func TestViewLines(t *testing.T) {
	lines := []string{"a", "b", "c", "d", "e"}
	// 全部
	text, total, start, end, truncated := viewLines(lines, nil, nil)
	if total != 5 || text != "a\nb\nc\nd\ne" || truncated {
		t.Errorf("Unexpected: text=%q total=%d start=%d end=%d truncated=%v", text, total, start, end, truncated)
	}
	// offset + limit
	offset := 2
	limit := 2
	text, total, start, end, truncated = viewLines(lines, &offset, &limit)
	if text != "b\nc" || start != 1 || end != 3 || !truncated {
		t.Errorf("Unexpected: text=%q start=%d end=%d truncated=%v", text, start, end, truncated)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/lite/ -run "TestValidateMemoryPath|TestMemorySearch|TestReadMemory|TestWriteMemory|TestEditMemory|TestLineRange|TestViewLines" -v 2>&1 | head -30`
Expected: 编译失败（函数签名不匹配）

- [ ] **Step 3: 实现 tool_ops.go**

替换 `internal/agentcore/memory/lite/tool_ops.go` 全部内容：

```go
package lite

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// dailyMemoryPattern 日期文件名正则。对齐 Python: re.match(r"^\d{4}-\d{2}-\d{2}\.md$", basename)
var dailyMemoryPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\.md$`)

// ──────────────────────────── 导出函数 ────────────────────────────

// ValidateMemoryPath 验证路径在 memory 目录内。对齐 Python validate_memory_path
func ValidateMemoryPath(path string, ws *workspace.Workspace) (bool, string) {
	if ws == nil {
		return false, "Workspace not initialized"
	}
	if strings.Contains(path, "..") || strings.HasPrefix(path, "/") {
		return false, "Invalid path: directory traversal not allowed"
	}
	basename := filepath.Base(path)
	memoryDir := ""
	if nodePath := ws.GetNodePath("memory"); nodePath != nil {
		memoryDir = *nodePath
	}
	var resolvedPath string
	switch {
	case basename == "USER.md":
		if userPath := ws.GetNodePath("USER.md"); userPath != nil {
			resolvedPath = *userPath
		}
	case basename == "MEMORY.md":
		memoryRel := ws.GetDirectory("MEMORY.md")
		if memoryDir != "" && memoryRel != "" {
			resolvedPath = filepath.Join(memoryDir, memoryRel)
		}
	case dailyMemoryPattern.MatchString(basename):
		dailyRel := ws.GetDirectory("daily_memory")
		if memoryDir != "" && dailyRel != "" {
			resolvedPath = filepath.Join(memoryDir, dailyRel, basename)
		}
	default:
		if memoryDir != "" {
			resolvedPath = filepath.Join(memoryDir, basename)
		}
	}
	if resolvedPath == "" {
		return false, fmt.Sprintf("Cannot resolve path: %s", path)
	}
	return true, resolvedPath
}

// MemorySearchWithContext 语义搜索记忆。对齐 Python memory_search_with_context
func MemorySearchWithContext(ctx context.Context, toolCtx *MemoryToolContext, query string, maxResults *int, minScore *float64, sessionKey string) map[string]any {
	if toolCtx == nil {
		return map[string]any{"results": []map[string]any{}, "disabled": true, "error": "Memory manager not available"}
	}
	if !toolCtx.EnsureManager() {
		return map[string]any{"results": []map[string]any{}, "disabled": true, "error": "Memory manager not available"}
	}
	if toolCtx.Manager == nil {
		return map[string]any{"results": []map[string]any{}, "disabled": true, "error": "Memory manager not initialized"}
	}
	opts := make(map[string]any)
	if maxResults != nil {
		opts["max_results"] = *maxResults
	}
	if minScore != nil {
		opts["min_score"] = *minScore
	}
	if sessionKey != "" {
		opts["session_key"] = sessionKey
	}
	results, err := toolCtx.Manager.Search(ctx, query, opts)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).Err(err).Msg("Memory search failed")
		return map[string]any{"results": []map[string]any{}, "disabled": true, "error": err.Error()}
	}
	// 添加 citation 字段。对齐 Python: r["citation"] = f"{r['path']}#L{r['start_line']}"
	for _, r := range results {
		startLine, _ := r["start_line"].(int)
		endLine, _ := r["end_line"].(int)
		path, _ := r["path"].(string)
		if path != "" {
			if startLine == endLine {
				r["citation"] = fmt.Sprintf("%s#L%d", path, startLine)
			} else {
				r["citation"] = fmt.Sprintf("%s#L%d-L%d", path, startLine, endLine)
			}
		}
	}
	status := toolCtx.Manager.Status()
	return map[string]any{
		"results":  results,
		"provider": status["provider"],
		"model":    status["model"],
		"disabled": false,
	}
}

// MemoryGetWithContext 获取记忆文件内容。对齐 Python memory_get_with_context
func MemoryGetWithContext(ctx context.Context, toolCtx *MemoryToolContext, path string, fromLine *int, lines *int) map[string]any {
	ws := toolCtx.Workspace
	if ws == nil {
		return map[string]any{"path": path, "text": "", "disabled": true, "error": "Workspace not initialized"}
	}
	isValid, result := ValidateMemoryPath(path, ws)
	if !isValid {
		return map[string]any{"path": path, "text": "", "disabled": true, "error": result}
	}
	resolvedPath := result
	if toolCtx == nil {
		return map[string]any{"path": resolvedPath, "text": "", "disabled": true, "error": "Memory manager not available"}
	}
	if !toolCtx.EnsureManager() {
		return map[string]any{"path": resolvedPath, "text": "", "disabled": true, "error": "Memory manager not available"}
	}
	if toolCtx.Manager == nil {
		return map[string]any{"path": resolvedPath, "text": "", "disabled": true, "error": "Memory manager not initialized"}
	}
	rf, err := toolCtx.Manager.ReadFile(ctx, resolvedPath, fromLine, lines)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).Err(err).Msg("Memory get failed")
		return map[string]any{"path": resolvedPath, "text": "", "disabled": true, "error": err.Error()}
	}
	rf["disabled"] = false
	return rf
}

// ReadMemoryWithContext 读取记忆文件。对齐 Python read_memory_with_context
func ReadMemoryWithContext(ctx context.Context, toolCtx *MemoryToolContext, path string, offset *int, limit *int) map[string]any {
	ws := toolCtx.Workspace
	if ws == nil {
		return map[string]any{"success": false, "path": path, "content": "", "error": "Workspace not initialized"}
	}
	isValid, result := ValidateMemoryPath(path, ws)
	if !isValid {
		return map[string]any{"success": false, "path": path, "content": "", "error": result}
	}
	fullPath := result
	sysOp := toolCtx.SysOperation
	if sysOp == nil {
		logger.Error(logger.ComponentAgentCore).Msg("Read memory failed, no available sys_operation")
		return map[string]any{"success": false, "path": path, "error": "Read failed, no available sys_operation."}
	}
	// 对齐 Python: 使用 line_range 读取
	fsOpts := []sysop.FsOption{}
	if offset != nil {
		if limit != nil {
			fsOpts = append(fsOpts, sysop.WithFsLineRange(*offset, *offset+*limit-1))
		} else {
			fsOpts = append(fsOpts, sysop.WithFsLineRange(*offset, -1))
		}
	}
	readResult, err := sysOp.Fs().ReadFile(ctx, fullPath, fsOpts...)
	if err != nil {
		return map[string]any{"success": false, "path": path, "content": "", "error": err.Error()}
	}
	var content string
	if readResult != nil && readResult.Data != nil {
		content = readResult.Data.Content
	}
	lineList := strings.Split(content, "\n")
	body, nTotal, startIdx, endIdx, truncated := viewLines(lineList, offset, limit)
	return map[string]any{
		"success":    true,
		"path":       fullPath,
		"content":    body,
		"totalLines": nTotal,
		"start_line": startIdx + 1,
		"end_line":   endIdx,
		"truncated":  truncated,
	}
}

// WriteMemoryWithContext 写入/追加记忆文件。对齐 Python write_memory_with_context
func WriteMemoryWithContext(ctx context.Context, toolCtx *MemoryToolContext, path string, content string, appendMode bool) map[string]any {
	ws := toolCtx.Workspace
	if ws == nil {
		return map[string]any{"success": false, "path": path, "error": "Workspace not initialized"}
	}
	isValid, result := ValidateMemoryPath(path, ws)
	if !isValid {
		return map[string]any{"success": false, "path": path, "error": result}
	}
	resolvedPath := result
	sysOp := toolCtx.SysOperation
	if sysOp == nil {
		logger.Error(logger.ComponentAgentCore).Msg("Memory write failed, no available sys_operation")
		return map[string]any{"success": false, "path": path, "error": "Memory write failed, no available sys_operation."}
	}
	fsOpts := []sysop.FsOption{
		sysop.WithFsCreateIfNotExist(true),
		sysop.WithFsAppend(appendMode),
	}
	if appendMode {
		prepend := true
		fsOpts = append(fsOpts, sysop.WithFsPrependNewline(prepend))
	}
	writeResult, err := sysOp.Fs().WriteFile(ctx, resolvedPath, content, fsOpts...)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).Err(err).Str("path", resolvedPath).Msg("Write failed")
		return map[string]any{"success": false, "path": path, "error": err.Error()}
	}
	fileExisted := false
	if writeResult != nil && writeResult.Data != nil && writeResult.Data.Size > 0 {
		fileExisted = true
	}
	action := "Wrote"
	if appendMode {
		action = "Appended to"
	}
	logger.Info(logger.ComponentAgentCore).Str("action", action).Str("path", resolvedPath).Msg("Memory file written")
	return map[string]any{
		"success":     true,
		"path":        resolvedPath,
		"fullPath":    resolvedPath,
		"appended":    appendMode,
		"fileExisted": fileExisted,
	}
}

// EditMemoryWithContext 编辑记忆文件。对齐 Python edit_memory_with_context
func EditMemoryWithContext(ctx context.Context, toolCtx *MemoryToolContext, path string, oldText string, newText string) map[string]any {
	ws := toolCtx.Workspace
	if ws == nil {
		return map[string]any{"success": false, "path": path, "error": "Workspace not initialized"}
	}
	isValid, result := ValidateMemoryPath(path, ws)
	if !isValid {
		return map[string]any{"success": false, "path": path, "error": result}
	}
	resolvedPath := result
	sysOp := toolCtx.SysOperation
	if sysOp == nil {
		logger.Error(logger.ComponentAgentCore).Msg("Edit failed, no available sys_operation")
		return map[string]any{"success": false, "path": path, "error": "Edit failed, no available sys_operation."}
	}
	readResult, err := sysOp.Fs().ReadFile(ctx, resolvedPath)
	if err != nil || readResult == nil || readResult.Data == nil {
		return map[string]any{"success": false, "path": path, "error": fmt.Sprintf("failed to read file: %s", path)}
	}
	content := readResult.Data.Content
	if !strings.Contains(content, oldText) {
		return map[string]any{
			"success": false,
			"path":    path,
			"error":   "old_text not found in file. Use read_memory tool to check exact content.",
		}
	}
	occurrences := strings.Count(content, oldText)
	if occurrences > 1 {
		return map[string]any{
			"success": false,
			"path":    path,
			"error":   fmt.Sprintf("old_text appears %d times in file. Be more specific.", occurrences),
		}
	}
	newContent := strings.Replace(content, oldText, newText, 1)
	_, err = sysOp.Fs().WriteFile(ctx, resolvedPath, newContent,
		sysop.WithFsCreateIfNotExist(true),
		sysop.WithFsPrependNewline(false),
		sysop.WithFsAppendNewline(false),
	)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).Err(err).Str("path", resolvedPath).Msg("Edit write failed")
		return map[string]any{"success": false, "path": path, "error": err.Error()}
	}
	logger.Info(logger.ComponentAgentCore).Str("path", resolvedPath).Msg("Edited file")
	return map[string]any{
		"success":   true,
		"path":      resolvedPath,
		"replaced":  oldText,
		"new_text":  newText,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// lineRangeToFsRead 映射 offset/limit 到 FsOperation 的 LineRange。对齐 Python _line_range_to_fs_read
func lineRangeToFsRead(firstLine *int, lineCap *int) *[2]int {
	if firstLine == nil {
		return nil
	}
	if lineCap != nil {
		return &[2]int{*firstLine, *firstLine + *lineCap - 1}
	}
	return &[2]int{*firstLine, -1}
}

// viewLines 行视图切片。对齐 Python _view_lines
// firstLine 是 1-based；返回 (excerpt, total, start_idx, end_idx, truncated)
func viewLines(allLines []string, firstLine *int, lineCap *int) (string, int, int, int, bool) {
	total := len(allLines)
	startIdx := 0
	if firstLine != nil {
		startIdx = *firstLine - 1
		if startIdx < 0 {
			startIdx = 0
		}
	}
	endIdx := total
	if lineCap != nil {
		endIdx = startIdx + *lineCap
		if endIdx > total {
			endIdx = total
		}
	}
	if startIdx > total {
		startIdx = total
	}
	if endIdx > total {
		endIdx = total
	}
	text := strings.Join(allLines[startIdx:endIdx], "\n")
	truncated := lineCap != nil && endIdx < total
	return text, total, startIdx, endIdx, truncated
}

// newTestWorkspace 创建测试用 workspace。测试辅助函数
func newTestWorkspace(t *testing.T) *workspace.Workspace {
	t.Helper()
	dir := t.TempDir()
	memoryDir := filepath.Join(dir, "memory")
	if err := os.MkdirAll(memoryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ws := &workspace.Workspace{}
	// 通过反射或简单方式设置路径——测试中 ValidateMemoryPath 需要有效的 workspace
	// 实际测试中可能需要更复杂的 setup
	return ws
}
```

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/lite/ -run "TestValidateMemoryPath|TestMemorySearch|TestReadMemory|TestWriteMemory|TestEditMemory|TestLineRange|TestViewLines" -v 2>&1 | head -40`
Expected: 大部分 PASS（workspace 相关的测试可能需要调整 setup）

- [ ] **Step 5: 修正测试辅助函数并确认通过**

根据实际编译结果调整 `newTestWorkspace` 和测试，确保所有测试通过。

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/memory/lite/tool_ops.go internal/agentcore/memory/lite/tool_ops_test.go
git commit -m "feat(memory/lite): 实现 7.2 通用记忆工具 tool_ops"
```

---

### Task 3: 实现 coding_memory_tool_ops.go（编程记忆工具）

**Files:**
- 修改: `internal/agentcore/memory/lite/coding_memory_tool_ops.go`
- 新建: `internal/agentcore/memory/lite/coding_memory_tool_ops_test.go`

对齐 Python `openjiuwen/core/memory/lite/coding_memory_tool_ops.py`。最复杂的部分，包含三层并发控制。

- [ ] **Step 1: 编写 coding_memory_tool_ops_test.go 基础测试**

```go
package lite

import (
	"context"
	"testing"
)

// TestValidateCodingMemoryPath_非法穿越 测试目录穿越
func TestValidateCodingMemoryPath_非法穿越(t *testing.T) {
	ws := newTestWorkspace(t)
	ok, _ := ValidateCodingMemoryPath("../etc/passwd", ws)
	if ok {
		t.Error("Expected path traversal to be rejected")
	}
}

// TestValidateCodingMemoryPath_绝对路径 测试绝对路径
func TestValidateCodingMemoryPath_绝对路径(t *testing.T) {
	ws := newTestWorkspace(t)
	ok, _ := ValidateCodingMemoryPath("/etc/passwd", ws)
	if ok {
		t.Error("Expected absolute path to be rejected")
	}
}

// TestValidateCodingMemoryPath_非MD文件 测试非 .md 后缀
func TestValidateCodingMemoryPath_非MD文件(t *testing.T) {
	ws := newTestWorkspace(t)
	ok, _ := ValidateCodingMemoryPath("test.txt", ws)
	if ok {
		t.Error("Expected non-md file to be rejected")
	}
}

// TestValidateCodingMemoryPath_无Workspace 测试无 workspace
func TestValidateCodingMemoryPath_无Workspace(t *testing.T) {
	ok, _ := ValidateCodingMemoryPath("test.md", nil)
	if ok {
		t.Error("Expected nil workspace to be rejected")
	}
}

// TestCodingMemoryReadWithContext_无Workspace 测试无 workspace
func TestCodingMemoryReadWithContext_无Workspace(t *testing.T) {
	ctx := context.Background()
	toolCtx := &CodingMemoryToolContext{}
	result := CodingMemoryReadWithContext(ctx, toolCtx, "test.md", nil, nil)
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if result["success"] != false {
		t.Error("Expected success=false")
	}
}

// TestCodingMemoryWriteWithContext_无Workspace 测试无 workspace
func TestCodingMemoryWriteWithContext_无Workspace(t *testing.T) {
	ctx := context.Background()
	toolCtx := &CodingMemoryToolContext{}
	result := CodingMemoryWriteWithContext(ctx, toolCtx, "test.md", "content")
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if result["success"] != false {
		t.Error("Expected success=false")
	}
}

// TestCodingMemoryWriteWithContext_无Frontmatter 测试无 frontmatter
func TestCodingMemoryWriteWithContext_无Frontmatter(t *testing.T) {
	ctx := context.Background()
	toolCtx := &CodingMemoryToolContext{
		LiteMemoryToolContextBase: LiteMemoryToolContextBase{
			Workspace: newTestWorkspace(t),
		},
	}
	result := CodingMemoryWriteWithContext(ctx, toolCtx, "test.md", "no frontmatter")
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if result["success"] != false {
		t.Error("Expected success=false for missing frontmatter")
	}
}

// TestCodingMemoryEditWithContext_无Workspace 测试无 workspace
func TestCodingMemoryEditWithContext_无Workspace(t *testing.T) {
	ctx := context.Background()
	toolCtx := &CodingMemoryToolContext{}
	result := CodingMemoryEditWithContext(ctx, toolCtx, "test.md", "old", "new")
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if result["success"] != false {
		t.Error("Expected success=false")
	}
}

// TestCodingMemoryEditWithContext_空OldText 测试空 old_text
func TestCodingMemoryEditWithContext_空OldText(t *testing.T) {
	ctx := context.Background()
	toolCtx := &CodingMemoryToolContext{
		LiteMemoryToolContextBase: LiteMemoryToolContextBase{
			Workspace: newTestWorkspace(t),
		},
	}
	result := CodingMemoryEditWithContext(ctx, toolCtx, "test.md", "", "new")
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if result["success"] != false {
		t.Error("Expected success=false for empty old_text")
	}
}

// TestResolveMemoryDir 测试解析记忆目录
func TestResolveMemoryDir(t *testing.T) {
	// 有 CodingMemoryDir
	ctx := &CodingMemoryToolContext{
		CodingMemoryDir: "/custom/dir",
	}
	result := resolveMemoryDir(ctx, "/resolved/path/test.md")
	if result != "/custom/dir" {
		t.Errorf("Expected /custom/dir, got %s", result)
	}
	// 无 CodingMemoryDir，从 resolved path 推导
	ctx2 := &CodingMemoryToolContext{}
	result2 := resolveMemoryDir(ctx2, "/memory/dir/test.md")
	if result2 != "/memory/dir" {
		t.Errorf("Expected /memory/dir, got %s", result2)
	}
}

// TestGetFileLock 测试文件锁获取
func TestGetFileLock(t *testing.T) {
	lock1 := getFileLock("/test/path1.md")
	lock2 := getFileLock("/test/path1.md")
	lock3 := getFileLock("/test/path2.md")
	if lock1 != lock2 {
		t.Error("Expected same lock for same path")
	}
	if lock1 == lock3 {
		t.Error("Expected different lock for different path")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/lite/ -run "TestValidateCodingMemoryPath|TestCodingMemoryRead|TestCodingMemoryWrite|TestCodingMemoryEdit|TestResolveMemoryDir|TestGetFileLock" -v 2>&1 | head -30`
Expected: 编译失败（函数签名不匹配）

- [ ] **Step 3: 实现 coding_memory_tool_ops.go**

替换 `internal/agentcore/memory/lite/coding_memory_tool_ops.go` 全部内容：

```go
package lite

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// maxIndexLines MEMORY.md 索引最大行数。对齐 Python MAX_INDEX_LINES
	maxIndexLines = 200
	// maxConflictRetries 乐观并发最大重试次数。对齐 Python _MAX_CONFLICT_RETRIES
	maxConflictRetries = 2
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// fileLocks 文件级锁注册表。对齐 Python _file_locks
	fileLocks = make(map[string]*sync.Mutex)
	// fileLocksMu 保护 fileLocks 注册表的互斥锁。对齐 Python _file_locks_init_lock
	fileLocksMu sync.Mutex
	// memoryIndexLock MEMORY.md 索引锁。对齐 Python _memory_index_lock
	memoryIndexLock sync.Mutex
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ValidateCodingMemoryPath 验证路径在 coding_memory 目录内。对齐 Python validate_coding_memory_path
func ValidateCodingMemoryPath(path string, ws *workspace.Workspace) (bool, string) {
	if ws == nil {
		return false, "Workspace not initialized"
	}
	if strings.Contains(path, "..") || strings.HasPrefix(path, "/") {
		return false, "Invalid path: directory traversal not allowed"
	}
	if !strings.HasSuffix(path, ".md") {
		return false, "Path must end with .md"
	}
	memoryDir := ""
	if nodePath := ws.GetNodePath("coding_memory"); nodePath != nil {
		memoryDir = *nodePath
	}
	if memoryDir == "" {
		return false, "coding_memory node not configured"
	}
	resolved := filepath.Join(memoryDir, filepath.Base(path))
	return true, resolved
}

// CodingMemoryReadWithContext 读取 coding_memory 文件。对齐 Python coding_memory_read_with_context
func CodingMemoryReadWithContext(ctx context.Context, toolCtx *CodingMemoryToolContext, path string, offset *int, limit *int) map[string]any {
	ws := toolCtx.Workspace
	if ws == nil {
		return map[string]any{"success": false, "path": path, "content": "", "error": "Workspace not initialized"}
	}
	isValid, result := ValidateCodingMemoryPath(path, ws)
	if !isValid {
		return map[string]any{"success": false, "path": path, "content": "", "error": result}
	}
	fullPath := result
	sysOp := toolCtx.SysOperation
	if sysOp == nil {
		logger.Error(logger.ComponentAgentCore).Msg("Read memory failed, no available sys_operation")
		return map[string]any{"success": false, "path": path, "error": "Read failed, no available sys_operation."}
	}
	// 对齐 Python: 使用 line_range 读取
	fsOpts := []sysop.FsOption{}
	if offset != nil {
		if limit != nil {
			fsOpts = append(fsOpts, sysop.WithFsLineRange(*offset, *offset+*limit-1))
		} else {
			fsOpts = append(fsOpts, sysop.WithFsLineRange(*offset, -1))
		}
	}
	readResult, err := sysOp.Fs().ReadFile(ctx, fullPath, fsOpts...)
	if err != nil {
		return map[string]any{"success": false, "path": path, "content": "", "error": err.Error()}
	}
	var content string
	if readResult != nil && readResult.Data != nil {
		content = readResult.Data.Content
	}
	rows := strings.Split(content, "\n")
	n := len(rows)
	fromIdx := 0
	if offset != nil {
		fromIdx = *offset - 1
		if fromIdx < 0 {
			fromIdx = 0
		}
	}
	toIdx := n
	if limit != nil {
		toIdx = fromIdx + *limit
		if toIdx > n {
			toIdx = n
		}
	}
	return map[string]any{
		"success":    true,
		"path":       fullPath,
		"content":    strings.Join(rows[fromIdx:toIdx], "\n"),
		"totalLines": n,
		"start_line": fromIdx + 1,
		"end_line":   toIdx,
		"truncated":  limit != nil && toIdx < n,
	}
}

// CodingMemoryWriteWithContext 写入 coding_memory 文件。对齐 Python coding_memory_write_with_context
func CodingMemoryWriteWithContext(ctx context.Context, toolCtx *CodingMemoryToolContext, path string, content string) map[string]any {
	if toolCtx == nil {
		return map[string]any{"success": false, "path": path, "error": "Workspace not initialized"}
	}
	ws := toolCtx.Workspace
	if ws == nil {
		return map[string]any{"success": false, "path": path, "error": "Workspace not initialized"}
	}
	isValid, resolved := ValidateCodingMemoryPath(path, ws)
	if !isValid {
		return map[string]any{"success": false, "path": path, "error": resolved}
	}
	// 对齐 Python: frontmatter 解析验证
	fm := ParseFrontmatter(content)
	if fm == nil {
		return map[string]any{"success": false, "path": path, "error": "must contain frontmatter(name/description/type)"}
	}
	if ok, err := ValidateFrontmatter(fm); !ok {
		return map[string]any{"success": false, "path": path, "error": err}
	}
	fm = EnrichFrontmatter(fm, false)
	content = RebuildContentWithFrontmatter(content, fm)
	body := ExtractBody(content)
	if body == "" {
		return map[string]any{"success": false, "path": path, "error": "no content body"}
	}
	basename := filepath.Base(resolved)
	memoryDir := resolveMemoryDir(toolCtx, resolved)

	// 对齐 Python: 乐观并发 — 冲突检测在锁外运行，快照验证在锁内
	var conflictResult map[string]any
	for attempt := 0; attempt < maxConflictRetries; attempt++ {
		snapshot := snapshotMemoryFiles(toolCtx, memoryDir)
		fileExists := false
		for _, name := range snapshot {
			if name == basename {
				fileExists = true
				break
			}
		}

		if !fileExists {
			// 创建模式: 搜索相似文件
			conflictResult = map[string]any{"conflict_detected": false, "conflicting_files": []string{}}
			similarFiles := searchSimilar(toolCtx, body, basename, 5, 0.75)
			if len(similarFiles) > 0 {
				conflicting := make([]string, 0, len(similarFiles))
				for name := range similarFiles {
					conflicting = append(conflicting, name)
				}
				conflictResult["conflict_detected"] = true
				conflictResult["conflicting_files"] = conflicting
				conflictResult["note"] = fmt.Sprintf(
					"Conflicts with: %s. Use coding_memory_read to review, then coding_memory_edit to update.",
					strings.Join(conflicting, ", "),
				)
			}
			// ⤵️ 回填: 7.8 MemUpdateChecker — LLM 冗余判断，当前跳过 SKIP 逻辑
		} else {
			// 追加模式: 搜索自身 + 相似文件
			conflictResult = prepareAppendMode(toolCtx, resolved, basename, body, fm)
			// 检查是否 SKIP（当前 searchSimilar 不做 SKIP，仅 conflict 检测）
		}

		// 文件级锁保护实际写入 + 快照验证
		fileLock := getFileLock(resolved)
		snapshotStale := false
		fileLock.Lock()
		currentSnapshot := snapshotMemoryFiles(toolCtx, memoryDir)
		if !snapshotEqual(currentSnapshot, snapshot) {
			logger.Info(logger.ComponentAgentCore).
				Int("attempt", attempt+1).
				Msg("Snapshot stale, retrying conflict detection")
			snapshotStale = true
		} else {
			sysOp := toolCtx.SysOperation
			if sysOp == nil {
				fileLock.Unlock()
				return map[string]any{"success": false, "path": path, "error": "no available sys_operation"}
			}
			if !fileExists {
				_, err := sysOp.Fs().WriteFile(ctx, resolved, content, sysop.WithFsCreateIfNotExist(true))
				if err != nil {
					fileLock.Unlock()
					return map[string]any{"success": false, "path": path, "error": err.Error()}
				}
			} else {
				appendToExistingFile(ctx, toolCtx, resolved, body, fm)
			}
		}
		fileLock.Unlock()

		if snapshotStale {
			continue
		}

		// 更新 MEMORY.md 索引（索引有自己的锁，不需要在文件锁内运行）
		upsertMemoryIndex(ctx, toolCtx, memoryDir, basename, fm)

		writeMode := WriteModeCreate
		if fileExists {
			writeMode = WriteModeAppend
		}
		wr := &WriteResult{
			Success: true,
			Path:    resolved,
			Mode:    writeMode,
			Type:    fm["type"],
		}
		// 合并冲突检测结果
		if cd, ok := conflictResult["conflict_detected"].(bool); ok && cd {
			wr.ConflictDetected = true
		}
		if cf, ok := conflictResult["conflicting_files"].([]string); ok {
			wr.ConflictingFiles = cf
		}
		if note, ok := conflictResult["note"].(string); ok {
			wr.Note = note
		}
		return wr.ToDict()
	}

	// 超过重试次数，降级为无快照验证写入
	logger.Warn(logger.ComponentAgentCore).
		Int("max_retries", maxConflictRetries).
		Msg("Exceeded max conflict retries, writing without snapshot validation")

	fileLock := getFileLock(resolved)
	fileLock.Lock()
	currentSnapshot := snapshotMemoryFiles(toolCtx, memoryDir)
	fileExistsNow := false
	for _, name := range currentSnapshot {
		if name == basename {
			fileExistsNow = true
			break
		}
	}
	sysOp := toolCtx.SysOperation
	if sysOp != nil {
		if !fileExistsNow {
			_, _ = sysOp.Fs().WriteFile(ctx, resolved, content, sysop.WithFsCreateIfNotExist(true))
		} else {
			appendToExistingFile(ctx, toolCtx, resolved, body, fm)
		}
	}
	fileLock.Unlock()

	upsertMemoryIndex(ctx, toolCtx, memoryDir, basename, fm)

	writeMode := WriteModeCreate
	if fileExistsNow {
		writeMode = WriteModeAppend
	}
	wr := &WriteResult{
		Success: true,
		Path:    resolved,
		Mode:    writeMode,
		Type:    fm["type"],
	}
	if cd, ok := conflictResult["conflict_detected"].(bool); ok && cd {
		wr.ConflictDetected = true
	}
	if cf, ok := conflictResult["conflicting_files"].([]string); ok {
		wr.ConflictingFiles = cf
	}
	return wr.ToDict()
}

// CodingMemoryEditWithContext 编辑 coding_memory 文件。对齐 Python coding_memory_edit_with_context
func CodingMemoryEditWithContext(ctx context.Context, toolCtx *CodingMemoryToolContext, path string, oldText string, newText string) map[string]any {
	if oldText == "" {
		return map[string]any{"success": false, "error": "old_text cannot be empty"}
	}
	if toolCtx == nil {
		return map[string]any{"success": false, "error": "Workspace not initialized"}
	}
	ws := toolCtx.Workspace
	if ws == nil {
		return map[string]any{"success": false, "error": "Workspace not initialized"}
	}
	isValid, resolved := ValidateCodingMemoryPath(path, ws)
	if !isValid {
		return map[string]any{"success": false, "error": resolved}
	}
	memoryDir := resolveMemoryDir(toolCtx, resolved)
	sysOp := toolCtx.SysOperation
	if sysOp == nil {
		return map[string]any{"success": false, "error": "no available sys_operation"}
	}

	// 文件级锁保护 read-then-write
	fileLock := getFileLock(resolved)
	fileLock.Lock()
	readResult, err := sysOp.Fs().ReadFile(ctx, resolved)
	if err != nil || readResult == nil || readResult.Data == nil {
		fileLock.Unlock()
		return map[string]any{"success": false, "error": fmt.Sprintf("failed to read file: %s", path)}
	}
	content := readResult.Data.Content
	occurrences := strings.Count(content, oldText)
	if occurrences == 0 {
		fileLock.Unlock()
		return map[string]any{"success": false, "error": "old_text not found in file"}
	}
	if occurrences > 1 {
		fileLock.Unlock()
		return map[string]any{"success": false, "error": fmt.Sprintf("old_text appears %d times, please be more specific", occurrences)}
	}
	newContent := strings.Replace(content, oldText, newText, 1)
	_, err = sysOp.Fs().WriteFile(ctx, resolved, newContent, sysop.WithFsCreateIfNotExist(true))
	fileLock.Unlock()
	if err != nil {
		logger.Error(logger.ComponentAgentCore).Err(err).Msg("coding_memory_edit failed")
		return map[string]any{"success": false, "error": err.Error()}
	}

	// 更新 MEMORY.md 索引
	fm := ParseFrontmatter(newContent)
	if fm != nil {
		if ok, _ := ValidateFrontmatter(fm); ok {
			upsertMemoryIndex(ctx, toolCtx, memoryDir, filepath.Base(resolved), fm)
		}
	}

	return map[string]any{"success": true, "path": resolved, "new_content": newContent}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveMemoryDir 解析 coding_memory 目录。对齐 Python _resolve_memory_dir
func resolveMemoryDir(ctx *CodingMemoryToolContext, resolvedPath string) string {
	if ctx != nil && ctx.CodingMemoryDir != "" {
		return ctx.CodingMemoryDir
	}
	return filepath.Dir(resolvedPath)
}

// searchSimilar 语义搜索相似记忆。对齐 Python _search_similar
// ⤵️ 回填: 7.8 MemUpdateChecker — 当前仅基于搜索结果，不做 LLM 冗余判断
func searchSimilar(toolCtx *CodingMemoryToolContext, body string, excludePath string, topK int, threshold float64) map[string]string {
	oldMemories := make(map[string]string)
	if toolCtx == nil || toolCtx.Manager == nil {
		return oldMemories
	}
	results, err := toolCtx.Manager.Search(context.Background(), body, map[string]any{"max_results": topK})
	if err != nil {
		return oldMemories
	}
	memoryDir := toolCtx.CodingMemoryDir
	for _, r := range results {
		score, _ := r["score"].(float64)
		path, _ := r["path"].(string)
		if score <= threshold || path == "MEMORY.md" || path == excludePath {
			continue
		}
		// 读取文件内容
		sysOp := toolCtx.SysOperation
		if sysOp == nil {
			continue
		}
		fullPath := filepath.Join(memoryDir, path)
		readResult, err := sysOp.Fs().ReadFile(context.Background(), fullPath)
		if err != nil || readResult == nil || readResult.Data == nil {
			continue
		}
		oldBody := ExtractBody(readResult.Data.Content)
		if oldBody != "" {
			oldMemories[path] = oldBody
		}
	}
	return oldMemories
}

// runChecker 调用 MemUpdateChecker 执行 LLM 冲突检测。
// ⤵️ 回填: 7.8 MemUpdateChecker — 当前返回空列表，不做 LLM 判断
func runChecker(manager MemoryIndexManager, newID string, newBody string, oldMemories map[string]string) []any {
	// TODO: 7.8 实现 MemUpdateChecker 后替换
	return nil
}

// prepareAppendMode 准备追加模式并返回冲突检测结果。对齐 Python _prepare_append_mode
func prepareAppendMode(toolCtx *CodingMemoryToolContext, resolved string, basename string, body string, fm map[string]string) map[string]any {
	result := map[string]any{"conflict_detected": false, "conflicting_files": []string{}}

	// 构建旧记忆：自身文件 + 其他相似文件
	oldMemories := make(map[string]string)

	// 读取自身文件
	sysOp := toolCtx.SysOperation
	if sysOp != nil {
		readResult, err := sysOp.Fs().ReadFile(context.Background(), resolved)
		if err == nil && readResult != nil && readResult.Data != nil {
			existingBody := ExtractBody(readResult.Data.Content)
			if existingBody != "" {
				oldMemories["__self__"] = existingBody
			}
		}
	}

	// 其他相似文件
	other := searchSimilar(toolCtx, body, basename, 5, 0.75)
	for k, v := range other {
		oldMemories[k] = v
	}

	// ⤵️ 回填: 7.8 MemUpdateChecker — LLM 冗余/冲突判断
	// 当前仅基于 searchSimilar 结果判断冲突
	if len(other) > 0 {
		conflicting := make([]string, 0, len(other))
		for name := range other {
			conflicting = append(conflicting, name)
		}
		result["conflict_detected"] = true
		result["conflicting_files"] = conflicting
		result["note"] = fmt.Sprintf(
			"Conflicts with: %s. Use coding_memory_read to review, then coding_memory_edit to update.",
			strings.Join(conflicting, ", "),
		)
	}

	return result
}

// snapshotMemoryFiles 快照目录下的 .md 文件列表（排除 MEMORY.md）。对齐 Python _snapshot_memory_files
func snapshotMemoryFiles(toolCtx *CodingMemoryToolContext, memoryDir string) []string {
	if memoryDir == "" || toolCtx.SysOperation == nil {
		return nil
	}
	entries, err := os.ReadDir(memoryDir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}
		if strings.EqualFold(name, "memory.md") {
			continue
		}
		names = append(names, name)
	}
	return names
}

// snapshotEqual 比较两个快照是否相等
func snapshotEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	aMap := make(map[string]bool, len(a))
	for _, s := range a {
		aMap[s] = true
	}
	for _, s := range b {
		if !aMap[s] {
			return false
		}
	}
	return true
}

// getFileLock 获取文件级锁。对齐 Python _get_file_lock
func getFileLock(path string) *sync.Mutex {
	fileLocksMu.Lock()
	defer fileLocksMu.Unlock()
	if lock, ok := fileLocks[path]; ok {
		return lock
	}
	lock := &sync.Mutex{}
	fileLocks[path] = lock
	return lock
}

// upsertMemoryIndex 增量更新 MEMORY.md 索引。对齐 Python _upsert_memory_index
func upsertMemoryIndex(ctx context.Context, toolCtx *CodingMemoryToolContext, memoryDir string, filename string, fm map[string]string) {
	memoryIndexLock.Lock()
	defer memoryIndexLock.Unlock()

	sysOp := toolCtx.SysOperation
	if sysOp == nil {
		return
	}
	if fm["name"] == "" || fm["description"] == "" {
		return
	}

	indexPath := filepath.Join(memoryDir, "MEMORY.md")
	newEntry := fmt.Sprintf("- [%s](%s) — %s", fm["name"], filename, fm["description"])

	var lines []string
	readResult, err := sysOp.Fs().ReadFile(ctx, indexPath)
	if err == nil && readResult != nil && readResult.Data != nil {
		lines = strings.Split(readResult.Data.Content, "\n")
	}

	found := false
	for i, line := range lines {
		if strings.Contains(line, fmt.Sprintf("](%s)", filename)) {
			lines[i] = newEntry
			found = true
			break
		}
	}
	if !found {
		lines = append([]string{newEntry}, lines...)
	}

	// 限制行数
	if len(lines) > maxIndexLines {
		lines = lines[:maxIndexLines]
	}
	newContent := strings.Join(lines, "\n")
	_, _ = sysOp.Fs().WriteFile(ctx, indexPath, newContent, sysop.WithFsCreateIfNotExist(true))
}

// appendToExistingFile 追加内容到已有文件并更新 frontmatter。对齐 Python _append_to_existing_file
func appendToExistingFile(ctx context.Context, toolCtx *CodingMemoryToolContext, resolved string, body string, fm map[string]string) {
	sysOp := toolCtx.SysOperation
	if sysOp == nil {
		logger.Error(logger.ComponentAgentCore).Msg("appendToExistingFile: sys_operation is nil")
		return
	}

	// 追加 body
	_, err := sysOp.Fs().WriteFile(ctx, resolved, "\n\n"+body,
		sysop.WithFsAppend(true),
		sysop.WithFsCreateIfNotExist(false),
	)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).Err(err).Str("path", resolved).Msg("追加内容失败")
		return
	}

	// 更新 frontmatter updated_at
	readResult, err := sysOp.Fs().ReadFile(ctx, resolved)
	if err != nil || readResult == nil || readResult.Data == nil {
		return
	}
	fmParsed := ParseFrontmatter(readResult.Data.Content)
	if fmParsed != nil {
		fmParsed = EnrichFrontmatter(fmParsed, true)
		updatedContent := RebuildContentWithFrontmatter(readResult.Data.Content, fmParsed)
		_, _ = sysOp.Fs().WriteFile(ctx, resolved, updatedContent, sysop.WithFsCreateIfNotExist(false))
	}
}

// readFileSafe 安全读取文件内容。对齐 Python _read_file_safe
func readFileSafe(ctx context.Context, toolCtx *CodingMemoryToolContext, filepath string) string {
	sysOp := toolCtx.SysOperation
	if sysOp == nil {
		return ""
	}
	readResult, err := sysOp.Fs().ReadFile(ctx, filepath)
	if err != nil || readResult == nil || readResult.Data == nil {
		return ""
	}
	return readResult.Data.Content
}
```

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/lite/ -run "TestValidateCodingMemoryPath|TestCodingMemoryRead|TestCodingMemoryWrite|TestCodingMemoryEdit|TestResolveMemoryDir|TestGetFileLock" -v 2>&1 | head -40`
Expected: 大部分 PASS（workspace 相关测试可能需要调整）

- [ ] **Step 5: 修正测试并确认通过**

根据实际编译结果调整测试代码，确保所有测试通过。

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/memory/lite/coding_memory_tool_ops.go internal/agentcore/memory/lite/coding_memory_tool_ops_test.go
git commit -m "feat(memory/lite): 实现 7.2 编程记忆工具 coding_memory_tool_ops"
```

---

### Task 4: 实现 tools.go（InitMemoryManagerAsync）

**Files:**
- 修改: `internal/agentcore/memory/lite/tools.go`

对齐 Python `coding_memory_tools.py` + `memory_tools.py` 的 `init_memory_manager_async`。

- [ ] **Step 1: 实现 tools.go**

替换 `internal/agentcore/memory/lite/tools.go` 全部内容：

```go
package lite

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// InitMemoryManagerAsync 初始化通用记忆管理器。对齐 Python coding_memory_tools.init_memory_manager_async + memory_tools.init_memory_manager_async
func InitMemoryManagerAsync(ctx context.Context, ws *workspace.Workspace, agentID string, embeddingConfig *embedding.EmbeddingConfig, sysOp sysop.SysOperation) (MemoryIndexManager, error) {
	if !IsMemoryEnabled() {
		logger.Info(logger.ComponentAgentCore).Msg("Memory system is disabled")
		return nil, nil
	}
	memoryDir := ""
	if nodePath := ws.GetNodePath("memory"); nodePath != nil {
		memoryDir = *nodePath
	}
	settings := CreateMemorySettings(memoryDir, nil)
	params := MemoryManagerParams{
		AgentID:         agentID,
		Workspace:       ws,
		Settings:        settings,
		EmbeddingConfig: embeddingConfig,
		SysOperation:    sysOp,
		NodeName:        "memory",
	}
	mgr, err := GetMemoryIndexManager(params)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).Err(err).Msg("Failed to initialize memory manager")
		return nil, err
	}
	logger.Info(logger.ComponentAgentCore).Str("memory_dir", memoryDir).Msg("Memory manager initialized")
	return mgr, nil
}

// InitCodingMemoryManagerAsync 初始化编程记忆管理器。对齐 Python coding_memory_tools.init_memory_manager_async
func InitCodingMemoryManagerAsync(ctx context.Context, ws *workspace.Workspace, agentID string, embeddingConfig *embedding.EmbeddingConfig, sysOp sysop.SysOperation) (MemoryIndexManager, error) {
	if !IsMemoryEnabled() {
		logger.Info(logger.ComponentAgentCore).Msg("Memory system is disabled")
		return nil, nil
	}
	cmDir := ""
	if nodePath := ws.GetNodePath("coding_memory"); nodePath != nil {
		cmDir = *nodePath
	}
	settings := CreateMemorySettings(cmDir, nil)
	params := MemoryManagerParams{
		AgentID:         agentID,
		Workspace:       ws,
		Settings:        settings,
		EmbeddingConfig: embeddingConfig,
		SysOperation:    sysOp,
		NodeName:        "coding_memory",
	}
	mgr, err := GetMemoryIndexManager(params)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).Err(err).Msg("Failed to initialize Coding Memory manager")
		return nil, fmt.Errorf("初始化 Coding Memory 管理器失败: %w", err)
	}
	logger.Info(logger.ComponentAgentCore).Str("cm_dir", cmDir).Msg("Coding Memory manager initialized")
	return mgr, nil
}
```

- [ ] **Step 2: 运行编译检查**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/memory/lite/ 2>&1`
Expected: 编译通过

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/memory/lite/tools.go
git commit -m "feat(memory/lite): 实现 7.2 InitMemoryManagerAsync + InitCodingMemoryManagerAsync"
```

---

### Task 5: 更新 doc.go 和 IMPLEMENTATION_PLAN.md

**Files:**
- 修改: `internal/agentcore/memory/lite/doc.go`
- 修改: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 doc.go 文件目录描述**

更新 `internal/agentcore/memory/lite/doc.go` 中的文件描述，去掉 `⤵️ 7.2` 标注（已实现的函数），保留 `⤵️ 7.3` 标注：

```
//	lite/
//	├── doc.go                       # 包文档
//	├── config.go                    # MemorySettings + IsMemoryEnabled + CreateMemorySettings
//	├── types.go                     # MemoryChunk 数据类
//	├── internal.go                  # 纯计算工具函数（FTS5 查询构建、BM25 分数转换等）
//	├── frontmatter.go               # frontmatter 解析/验证/重建
//	├── conflict_types.go            # WriteMode + WriteResult + ToDict（对齐 Python）
//	├── embeddings.go                # EmbeddingProvider 接口 + Mock + baseEmbeddingAdapter
//	├── vec_loader.go                # vec0.so 加载器（ResolveVec0Path + LoadVec0Extension）
//	├── manager.go                   # MemoryIndexManager 接口 + Params + 导出函数
//	├── manager_impl.go              # MemoryIndexManager 实现（Initialize + Sync + Search + Close）
//	├── tool_context_base.go         # LiteMemoryToolContextBase + EnsureManager
//	├── tool_context.go              # MemoryToolContext
//	├── coding_memory_tool_context.go # CodingMemoryToolContext
//	├── tool_ops.go                  # memory_search/read/write/edit_with_context
//	├── coding_memory_tool_ops.go    # coding_memory_read/write/edit_with_context + 并发控制 + 索引更新
//	└── tools.go                     # InitMemoryManagerAsync + InitCodingMemoryManagerAsync
```

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md**

将 7.2 和 7.5 的状态从 `☐` 改为 `✅`：

```
| 7.2 | ✅ | CodingMemoryTools | 编程记忆工具（读写搜索） | `openjiuwen/core/memory/lite/coding_memory_tools.py` |
| 7.5 | ✅ | Frontmatter 解析 | YAML frontmatter 读写 | `openjiuwen/core/memory/lite/frontmatter.py` |
```

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/memory/lite/doc.go IMPLEMENTATION_PLAN.md
git commit -m "docs(memory/lite): 更新 doc.go 和实现计划 7.2+7.5 状态"
```

---

### Task 6: 编译验证 + 全量测试

**Files:** 无修改

- [ ] **Step 1: 编译整个 memory/lite 包**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/memory/lite/ 2>&1`
Expected: 编译通过，无错误

- [ ] **Step 2: 运行 memory/lite 包全部测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/lite/ -v -cover 2>&1 | tail -40`
Expected: 所有测试通过，覆盖率 ≥ 85%（排除 build tag 隔离的代码）

- [ ] **Step 3: 检查是否有依赖包编译问题**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/... 2>&1`
Expected: 编译通过

- [ ] **Step 4: 最终提交（如有遗漏修复）**

```bash
git add -A
git commit -m "fix(memory/lite): 7.2+7.5 实现最终修复"
```

---

## 自审清单

1. **Spec 覆盖度**：
   - ✅ 7.5 frontmatter：5 个函数全部实现
   - ✅ 7.2 tool_ops：6 个函数全部实现
   - ✅ 7.2 coding_memory_tool_ops：4 个导出函数 + 6 个非导出函数全部实现
   - ✅ 7.2 tools.go：InitMemoryManagerAsync + InitCodingMemoryManagerAsync
   - ✅ 修正项：WriteResult.ToDict / CodingMemoryToolContext / EnsureManager / IsClosed
   - ⤵️ 回填标注：7.8 MemUpdateChecker（runChecker 返回空）、7.3 上下文完整初始化

2. **Placeholder 扫描**：无 TBD/TODO（除标注的 `⤵️ 回填: 7.8`）

3. **类型一致性**：所有函数签名与 Python 对齐，WriteMode.String() 返回字符串与 ToDict() 一致
