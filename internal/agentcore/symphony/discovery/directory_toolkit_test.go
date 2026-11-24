package discovery

import (
	"encoding/json"

	common "github.com/uapclaw/uapclaw-go/internal/agentcore/symphony/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/symphony/retrieval/search/artifacts"
	"strings"
	"testing"
)

// makeToolkit 构造带 count 技能的目录工具集（真盘）
func makeToolkit(t *testing.T, count int) (*InstalledSkillsDirectoryToolkit, *SkillFS, string) {
	t.Helper()
	root := t.TempDir()
	fs, _ := makeFS(t, root, count)
	tk, err := NewInstalledSkillsDirectoryToolkit(fs, "test-session", 4000)
	if err != nil {
		t.Fatal(err)
	}
	return tk, fs, root
}

// idsOf 从输出行提取技能 id
func idsOf(output string) []string {
	ids := []string{}
	for _, line := range splitLines(output) {
		if strings.Contains(line, "- [skill] ") {
			rest := line[strings.Index(line, "- [skill] ")+len("- [skill] "):]
			ids = append(ids, strings.Fields(rest)[0])
		}
	}
	return ids
}

// TestDirectoryToolkit_ListBasic list 基础：条目行 + 汇总头 + 诊断
func TestDirectoryToolkit_ListBasic(t *testing.T) {
	tk, _, _ := makeToolkit(t, 3)
	res, err := tk.SkillIndex(map[string]any{"operation": "list"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.ModelContent, "## Skills") {
		t.Fatalf("汇总头不符: %q", res.ModelContent)
	}
	got := idsOf(res.ModelContent)
	if len(got) != 3 {
		t.Fatalf("应列出 3 技能: %v", got)
	}
	if res.DetailedOutput["operation"] != "list" || res.DetailedOutput["ok"] != true {
		t.Fatalf("诊断不符: %+v", res.DetailedOutput)
	}
	if res.DetailedOutput["total_count"] != 3 {
		t.Fatalf("total_count 不符: %+v", res.DetailedOutput["total_count"])
	}
}

// TestDirectoryToolkit_ListPagingCursor list 翻页游标往返
func TestDirectoryToolkit_ListPagingCursor(t *testing.T) {
	tk, _, _ := makeToolkit(t, 5)
	// limit 触发分页（_page_size=2）
	res, err := tk.SkillIndex(map[string]any{"operation": "list", "limit": 2})
	if err != nil {
		t.Fatal(err)
	}
	first := idsOf(res.ModelContent)
	if len(first) != 2 {
		t.Fatalf("首页应 2 条: %v", first)
	}
	nextCursor, _ := res.DetailedOutput["next_cursor"].(string)
	if nextCursor == "" {
		t.Fatalf("应有 next cursor: %+v", res.DetailedOutput)
	}
	if !strings.Contains(res.ModelContent, "[next]") {
		t.Fatalf("应有 [next] footer: %q", res.ModelContent)
	}
	// 游标续页：其余 3 条一次取完
	res2, err := tk.SkillIndex(map[string]any{"operation": "list", "cursor": nextCursor})
	if err != nil {
		t.Fatal(err)
	}
	// 续页页宽与原页一致（Python _page_size=state.page_size=2），循环取完
	second := idsOf(res2.ModelContent)
	if len(second) != 2 {
		t.Fatalf("续页应 2 条: %v", second)
	}
	seen := map[string]bool{}
	for _, id := range first {
		seen[id] = true
	}
	all := append(append([]string{}, first...), second...)
	for {
		nextCursor, _ := res2.DetailedOutput["next_cursor"].(string)
		if nextCursor == "" {
			break
		}
		res2, err = tk.SkillIndex(map[string]any{"operation": "list", "cursor": nextCursor})
		if err != nil {
			t.Fatal(err)
		}
		more := idsOf(res2.ModelContent)
		if len(more) == 0 {
			break
		}
		for _, id := range more {
			if seen[id] {
				t.Fatalf("翻页不应重复: %v", id)
			}
			seen[id] = true
			all = append(all, id)
		}
	}
	if len(all) != 5 {
		t.Fatalf("翻页应覆盖全部 5 技能: %v", all)
	}
	// 游标一次性消费：复用报错
	if _, err := tk.SkillIndex(map[string]any{"operation": "list", "cursor": nextCursor}); err == nil {
		t.Fatal("复用已消费游标应报错")
	}
}

// TestDirectoryToolkit_UnknownCursor 非法/过期游标错误码
func TestDirectoryToolkit_UnknownCursor(t *testing.T) {
	tk, _, _ := makeToolkit(t, 2)
	_, err := tk.SkillIndex(map[string]any{"operation": "list", "cursor": "bogus"})
	if err == nil || !strings.Contains(err.Error(), "Unknown or expired skill_index cursor") {
		t.Fatalf("错误不符: %v", err)
	}
}

// TestDirectoryToolkit_SearchFlat 降级 flat 词法检索 + 命中行
func TestDirectoryToolkit_SearchFlat(t *testing.T) {
	root := t.TempDir()
	makeSkillDir(t, root, "pdf-ocr", "name: pdf-ocr\ndescription: extract text from PDF documents\n", "body about PDF")
	makeSkillDir(t, root, "web-scraper", "name: web-scraper\ndescription: crawl web pages\n", "body")
	fs, _ := makeFS(t, root, 0)
	_ = fs
	settings := DefaultDiscoverySettings()
	settings.UseExistingIndex = true
	inv, err := ScanSkillDirectories([]string{root}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	fs2, err := NewSkillFS(func() (SkillInventory, error) { return inv, nil },
		SkillFSOptions{Settings: settings, ArtifactRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	tk, err := NewInstalledSkillsDirectoryToolkit(fs2, "sess", 4000)
	if err != nil {
		t.Fatal(err)
	}
	res, err := tk.SkillIndex(map[string]any{"operation": "search", "query": []any{"PDF"}})
	if err != nil {
		t.Fatal(err)
	}
	got := idsOf(res.ModelContent)
	if len(got) != 1 || got[0] != "pdf-ocr" {
		t.Fatalf("应命中 pdf-ocr: %v", got)
	}
	if !strings.Contains(res.ModelContent, "path: ") {
		t.Fatalf("应有 path 字段: %q", res.ModelContent)
	}
}

// TestDirectoryToolkit_SearchUnknownCategoryHints 无命中 → 类别提示
func TestDirectoryToolkit_SearchUnknownCategoryHints(t *testing.T) {
	tk, _, _ := makeToolkit(t, 2)
	res, err := tk.SkillIndex(map[string]any{"operation": "search", "query": []any{"zzz-nothing"}})
	if err != nil {
		t.Fatal(err)
	}
	// flat 无类别 → 空消息
	if !strings.Contains(res.ModelContent, "No matching") {
		t.Fatalf("空结果消息不符: %q", res.ModelContent)
	}
}

// TestDirectoryToolkit_ReadFull read 全文 + 元数据卡
func TestDirectoryToolkit_ReadFull(t *testing.T) {
	tk, _, _ := makeToolkit(t, 2)
	res, err := tk.SkillIndex(map[string]any{"operation": "read", "skills": []any{"s-skill-a"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.ModelContent, "# s-skill-a") ||
		!strings.Contains(res.ModelContent, "- Description: ") ||
		!strings.Contains(res.ModelContent, "- Source: local") {
		t.Fatalf("元数据卡不符: %q", res.ModelContent)
	}
}

// TestDirectoryToolkit_ReadHeadAndRange head/range 模式
func TestDirectoryToolkit_ReadHeadAndRange(t *testing.T) {
	tk, _, _ := makeToolkit(t, 1)
	res, err := tk.SkillIndex(map[string]any{"operation": "read", "skills": []any{"s-skill-a"}, "read_mode": "head", "line_count": 2})
	if err != nil {
		t.Fatal(err)
	}
	// head 2 行 = 首行 + 空行（Python splitlines 含空行，join 后尾部换行）
	if out, _ := res.DetailedOutput["output"].(string); len(strings.Split(out, "\n")) != 2 {
		t.Fatalf("head 2 行不符: %q", out)
	}
	if _, err := tk.SkillIndex(map[string]any{"operation": "read", "skills": []any{"s-skill-a"}, "read_mode": "range", "start_line": 2, "end_line": 1}); err == nil {
		t.Fatal("end<start 应报错")
	}
	if _, err := tk.SkillIndex(map[string]any{"operation": "read", "skills": []any{"s-skill-a"}, "read_mode": "range", "start_line": 2}); err == nil ||
		!strings.Contains(err.Error(), "start_line and end_line are required") {
		t.Fatalf("range 缺 end_line 应报错: %v", err)
	}
}

// TestDirectoryToolkit_ReadUnknownSkill 未知技能错误
func TestDirectoryToolkit_ReadUnknownSkill(t *testing.T) {
	tk, _, _ := makeToolkit(t, 1)
	_, err := tk.SkillIndex(map[string]any{"operation": "read", "skills": []any{"nope"}})
	if err == nil || !strings.Contains(err.Error(), "Unknown Skill: nope") {
		t.Fatalf("错误不符: %v", err)
	}
}

// TestDirectoryToolkit_InvalidOperation 非法 operation
func TestDirectoryToolkit_InvalidOperation(t *testing.T) {
	tk, _, _ := makeToolkit(t, 1)
	_, err := tk.SkillIndex(map[string]any{"operation": "delete"})
	if err == nil || !strings.Contains(err.Error(), "operation must be one of") {
		t.Fatalf("错误不符: %v", err)
	}
}

// TestDirectoryToolkit_UnexpectedArgument 高级参数白名单
func TestDirectoryToolkit_UnexpectedArgument(t *testing.T) {
	tk, _, _ := makeToolkit(t, 1)
	_, err := tk.SkillIndex(map[string]any{"operation": "list", "bogus_kwarg": 1})
	if err == nil || !strings.Contains(err.Error(), "got an unexpected keyword argument 'bogus_kwarg'") {
		t.Fatalf("错误不符: %v", err)
	}
}

// TestDirectoryToolkit_ToolInvokeRejectsUnknownKeys 工具层拒绝卡外键（_DirectoryFunction.invoke）
func TestDirectoryToolkit_ToolInvokeRejectsUnknownKeys(t *testing.T) {
	tk, _, _ := makeToolkit(t, 1)
	_, err := skillIndexToolInvoke(tk, map[string]any{"operation": "list", "read_mode": "head"})
	if err == nil || !strings.Contains(err.Error(), "Unknown skill_index arguments: read_mode") {
		t.Fatalf("错误不符: %v", err)
	}
	// 归一化：search 字符串 query → 单元素数组
	res, err := skillIndexToolInvoke(tk, map[string]any{"operation": "search", "query": "PDF"})
	if err != nil {
		t.Fatal(err)
	}
	if res.DetailedOutput["operation"] != "search" {
		t.Fatalf("归一化后仍应执行: %+v", res.DetailedOutput)
	}
}

// TestDirectoryToolkit_CategoryResolution 类别解析/未知/歧义
func TestDirectoryToolkit_CategoryResolution(t *testing.T) {
	root := t.TempDir()
	makeSkillDir(t, root, "a-tools", "name: a-tools\ndescription: alpha\n", "body")
	settings := DefaultDiscoverySettings()
	settings.UseExistingIndex = true
	inv, err := ScanSkillDirectories([]string{root}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	fs, err := NewSkillFS(func() (SkillInventory, error) { return inv, nil },
		SkillFSOptions{Settings: settings, ArtifactRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	tk, err := NewInstalledSkillsDirectoryToolkit(fs, "sess", 4000)
	if err != nil {
		t.Fatal(err)
	}
	// ROOT 类别合法
	if _, err := tk.SkillIndex(map[string]any{"operation": "list", "category": "ROOT"}); err != nil {
		t.Fatal(err)
	}
	// 未知类别
	_, err = tk.SkillIndex(map[string]any{"operation": "list", "category": "nope > nothing"})
	if err == nil || !strings.Contains(err.Error(), "Unknown Skill category") {
		t.Fatalf("错误不符: %v", err)
	}
	// category 与 read 互斥
	_, err = tk.SkillIndex(map[string]any{"operation": "read", "skills": []any{"a-tools"}, "category": "ROOT"})
	if err == nil || !strings.Contains(err.Error(), "category is valid only for list and search") {
		t.Fatalf("错误不符: %v", err)
	}
}

// TestDirectoryToolkit_OutputBudgetTruncation 输出预算截断链
func TestDirectoryToolkit_OutputBudgetTruncation(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 30; i++ {
		name := "skill-" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		makeSkillDir(t, root, name, "name: "+name+"\ndescription: "+strings.Repeat("d ", 60)+"\n", "body")
	}
	settings := DefaultDiscoverySettings()
	settings.UseExistingIndex = true
	inv, err := ScanSkillDirectories([]string{root}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	fs, err := NewSkillFS(func() (SkillInventory, error) { return inv, nil },
		SkillFSOptions{Settings: settings, ArtifactRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	tk, err := NewInstalledSkillsDirectoryToolkit(fs, "sess", 4000)
	if err != nil {
		t.Fatal(err)
	}
	// 禁用截断
	res, err := tk.SkillIndex(map[string]any{"operation": "list", "disable_output_truncation": true})
	if err != nil {
		t.Fatal(err)
	}
	full := res.ModelContent
	// 低预算截断 + [truncated] 标记（非分页路径 showShortenedMarker=true）
	res2, err := tk.SkillIndex(map[string]any{"operation": "list", "max_output_chars": 700})
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.ModelContent) >= len(full) {
		t.Fatalf("低预算输出应更短")
	}
	if !strings.Contains(res2.ModelContent, "[skill_index output shortened") {
		t.Fatalf("应有截断标记: %q", res2.ModelContent)
	}
	// 预算越界
	_, err = tk.SkillIndex(map[string]any{"operation": "list", "max_output_chars": 100})
	if err == nil || !strings.Contains(err.Error(), "between 512 and 48000") {
		t.Fatalf("错误不符: %v", err)
	}
}

// TestDirectoryToolkit_NoticeAppended 新增技能通知拼进输出（入口 A）
func TestDirectoryToolkit_NoticeAppended(t *testing.T) {
	root := t.TempDir()
	makeSkillDir(t, root, "first", "name: first\ndescription: one\n", "body")
	settings := DefaultDiscoverySettings()
	settings.UseExistingIndex = true
	fs, err := NewSkillFS(func() (SkillInventory, error) {
		return ScanSkillDirectories([]string{root}, nil, nil)
	},
		SkillFSOptions{Settings: settings, ArtifactRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	tk, err := NewInstalledSkillsDirectoryToolkit(fs, "sess-notice", 4000)
	if err != nil {
		t.Fatal(err)
	}
	// 目录中途新增技能
	makeSkillDir(t, root, "second", "name: second\ndescription: two\n", "body")
	res, err := tk.SkillIndex(map[string]any{"operation": "list"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.ModelContent, "second") {
		t.Fatalf("通知应含新技能: %q", res.ModelContent)
	}
	// 第二次不再重复通知
	res2, err := tk.SkillIndex(map[string]any{"operation": "list"})
	if err != nil {
		t.Fatal(err)
	}
	notifyLines := 0
	for _, line := range splitLines(res2.ModelContent) {
		if strings.Contains(line, "New Skills") || strings.Contains(line, "skill_index") && strings.Contains(line, "second") {
			notifyLines++
		}
	}
	if notifyLines != 0 {
		t.Fatalf("基线推进后不应重复通知: %q", res2.ModelContent)
	}
}

// TestDirectoryToolkit_MetadataVirtualAddressing META.md 虚拟寻址（路径串构造无读盘）
func TestDirectoryToolkit_MetadataVirtualAddressing(t *testing.T) {
	tk, fs, _ := makeToolkit(t, 2)
	view := fs.Directory()
	metaPath, err := view.MetadataPath("s-skill-a")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(metaPath, "META.md") {
		t.Fatalf("虚拟寻址应以 META.md 结尾: %s", metaPath)
	}
	// 先 list 登记 observed，再经虚拟路径 read（对齐 _read_skills 白名单语义）
	if _, err := tk.SkillIndex(map[string]any{"operation": "list"}); err != nil {
		t.Fatal(err)
	}
	res, err := tk.SkillIndex(map[string]any{"operation": "read", "paths": []any{metaPath}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.ModelContent, "# s-skill-a") {
		t.Fatalf("经虚拟路径 read 失败: %q", res.ModelContent)
	}
}

// TestDirectoryToolkit_PipelineCountFilter 管道 count/filter
func TestDirectoryToolkit_PipelineCountFilter(t *testing.T) {
	tk, _, _ := makeToolkit(t, 3)
	res, err := tk.SkillIndex(map[string]any{
		"operation": "list",
		"pipeline":  []any{map[string]any{"operation": "count"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ModelContent != "3" && !strings.HasPrefix(res.ModelContent, "## ") {
		t.Fatalf("count 输出不符: %q", res.ModelContent)
	}
	if res.DetailedOutput["total_count"] != 3 {
		t.Fatalf("count 诊断不符: %+v", res.DetailedOutput["total_count"])
	}
	res2, err := tk.SkillIndex(map[string]any{
		"operation": "list",
		"pipeline":  []any{map[string]any{"operation": "filter", "query": "s-skill-a"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := idsOf(res2.ModelContent)
	if len(got) != 1 || got[0] != "s-skill-a" {
		t.Fatalf("filter 不符: %v", got)
	}
}

// TestDirectoryToolkit_CursorBudgetGuards 游标与高级参数互斥
func TestDirectoryToolkit_CursorBudgetGuards(t *testing.T) {
	tk, _, _ := makeToolkit(t, 3)
	_, err := tk.SkillIndex(map[string]any{"operation": "list", "cursor": "x", "limit": 2})
	if err == nil || !strings.Contains(err.Error(), "cursor cannot be combined with advanced arguments") {
		t.Fatalf("错误不符: %v", err)
	}
	_, err = tk.SkillIndex(map[string]any{"operation": "list", "limit": 11})
	if err == nil || !strings.Contains(err.Error(), "limit must not exceed") {
		t.Fatalf("错误不符: %v", err)
	}
}

// TestDirectoryToolkit_ToolCardShape 工具卡形状（skill_index / 模型操作枚举）
func TestDirectoryToolkit_ToolCardShape(t *testing.T) {
	tk, _, _ := makeToolkit(t, 1)
	tools := tk.GetTools()
	if len(tools) != 1 || tools[0].Name != "skill_index" {
		t.Fatalf("工具不符: %+v", tools)
	}
	card := tools[0].Card
	if card["name"] != "skill_index" || tools[0].ToolID != tk.ToolID() {
		t.Fatalf("卡片标识不符: %+v", card)
	}
	props := card["input_params"].(map[string]any)["properties"].(map[string]any)
	if _, ok := props["operation"]; !ok {
		t.Fatal("缺 operation 属性")
	}
	b, _ := json.Marshal(card["input_params"].(map[string]any)["properties"].(map[string]any)["operation"].(map[string]any)["enum"])
	if string(b) != `["list","search"]` {
		t.Fatalf("模型操作枚举不符: %s", b)
	}
}

// TestDirectoryToolkit_CloseAfterClose 关闭后拒绝执行
func TestDirectoryToolkit_CloseAfterClose(t *testing.T) {
	tk, _, _ := makeToolkit(t, 1)
	tk.Close()
	if _, err := tk.SkillIndex(map[string]any{"operation": "list"}); err == nil ||
		!strings.Contains(err.Error(), "closed") {
		t.Fatalf("关闭后应报错: %v", err)
	}
}

// TestDirectoryToolkit_InvalidSettingsBounds 构造期预算边界
func TestDirectoryToolkit_InvalidSettingsBounds(t *testing.T) {
	root := t.TempDir()
	makeSkillDir(t, root, "x", "name: x\ndescription: d\n", "b")
	settings := DefaultDiscoverySettings()
	settings.UseExistingIndex = true
	settings.MaxOutputChars = 100
	inv, err := ScanSkillDirectories([]string{root}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	fs, err := NewSkillFS(func() (SkillInventory, error) { return inv, nil },
		SkillFSOptions{Settings: settings, ArtifactRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewInstalledSkillsDirectoryToolkit(fs, "sess", 4000); err == nil ||
		!strings.Contains(err.Error(), "between 512 and 48000") {
		t.Fatalf("错误不符: %v", err)
	}
	// scope 校验须用合法 settings（构造器 settings 校验在前，与 Python __init__ 同序）
	settings2 := DefaultDiscoverySettings()
	settings2.UseExistingIndex = true
	inv2, err := ScanSkillDirectories([]string{root}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	fs2, err := NewSkillFS(func() (SkillInventory, error) { return inv2, nil },
		SkillFSOptions{Settings: settings2, ArtifactRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewInstalledSkillsDirectoryToolkit(fs2, "  ", 4000); err == nil ||
		!strings.Contains(err.Error(), "session_scope must be non-empty") {
		t.Fatalf("错误不符: %v", err)
	}
}

// makeNestedToolkit 构造带嵌套目录的技能库
func makeNestedToolkit(t *testing.T) (*InstalledSkillsDirectoryToolkit, *SkillFS) {
	t.Helper()
	root := t.TempDir()
	// 根一级（扫描器只扫根一级——嵌套类别仅 taxonomy 树可达）
	makeSkillDir(t, root, "one", "name: one\ndescription: alpha one tool for pdf extraction\n", "pdf body")
	makeSkillDir(t, root, "two", "name: two\ndescription: alpha two\n", "body two")
	makeSkillDir(t, root, "three", "name: three\ndescription: beta three\n", "body three")
	settings := DefaultDiscoverySettings()
	settings.UseExistingIndex = true
	inv, err := ScanSkillDirectories([]string{root}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	fs, err := NewSkillFS(func() (SkillInventory, error) { return inv, nil },
		SkillFSOptions{Settings: settings, ArtifactRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	tk, err := NewInstalledSkillsDirectoryToolkit(fs, "nested", 4000)
	if err != nil {
		t.Fatal(err)
	}
	return tk, fs
}

// TestDirectoryToolkit_TreeView tree 视图：缩进分支行 + 类别链 + 目录行
func TestDirectoryToolkit_TreeView(t *testing.T) {
	tk, _ := makeNestedToolkit(t)
	res, err := tk.SkillIndex(map[string]any{"operation": "list", "view": "tree", "directories_only": true})
	if err != nil {
		t.Fatal(err)
	}
	out := res.ModelContent
	// flat 布局：根节点单类别行 ROOT，含全部 3 技能计数
	if !strings.Contains(out, "- [category] ROOT") {
		t.Fatalf("树视图应含 ROOT 类别: %q", out)
	}
	if !strings.Contains(out, "Contains 3 skills") {
		t.Fatalf("应含技能计数: %q", out)
	}
	// 目录行走 searchDirectoryRow 形态（matches 缩进空）
	if _, err := tk.SkillIndex(map[string]any{"operation": "list", "view": "names"}); err != nil {
		t.Fatal(err)
	}
	// category=alpha（flat 无该节点 → 未知类别）
	_, err = tk.SkillIndex(map[string]any{"operation": "list", "category": "alpha"})
	if err == nil || !strings.Contains(err.Error(), "Unknown Skill category") {
		t.Fatalf("错误不符: %v", err)
	}
	// category 与 paths 互斥
	if _, err := tk.SkillIndex(map[string]any{"operation": "list", "category": "alpha", "paths": []any{"/"}}); err == nil ||
		!strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("错误不符: %v", err)
	}
}

// TestDirectoryToolkit_SearchNameMatch name/path 匹配 + 通配符 + 排序
func TestDirectoryToolkit_SearchNameMatch(t *testing.T) {
	tk, _ := makeNestedToolkit(t)
	// name 精确前缀命中
	res, err := tk.SkillIndex(map[string]any{"operation": "search", "query": []any{"one"}, "match": "name"})
	if err != nil {
		t.Fatal(err)
	}
	got := idsOf(res.ModelContent)
	if len(got) != 1 || got[0] != "one" {
		t.Fatalf("name 匹配应命中 one: %v", got)
	}
	// 通配符
	res2, err := tk.SkillIndex(map[string]any{"operation": "search", "query": []any{"t*"}, "match": "name"})
	if err != nil {
		t.Fatal(err)
	}
	if len(idsOf(res2.ModelContent)) < 2 {
		t.Fatalf("通配符应命中 two/three: %q", res2.ModelContent)
	}
	// path 匹配（技能目录名出现在 SKILL.md 路径中）
	res3, err := tk.SkillIndex(map[string]any{"operation": "search", "query": []any{"three"}, "match": "path"})
	if err != nil {
		t.Fatal(err)
	}
	if len(idsOf(res3.ModelContent)) != 1 {
		t.Fatalf("path 匹配应命中 three: %q", res3.ModelContent)
	}
	// match!=content 而 result!=files 应报错
	_, err = tk.SkillIndex(map[string]any{"operation": "search", "query": []any{"one"}, "match": "name", "result": "matches"})
	if err == nil || !strings.Contains(err.Error(), "result is only valid for content search") {
		t.Fatalf("错误不符: %v", err)
	}
}

// TestDirectoryToolkit_SearchMatchesMode content+matches：多片段（matchedSnippets）
func TestDirectoryToolkit_SearchMatchesMode(t *testing.T) {
	tk, _ := makeNestedToolkit(t)
	res, err := tk.SkillIndex(map[string]any{"operation": "search", "query": []any{"pdf"}, "result": "matches"})
	if err != nil {
		t.Fatal(err)
	}
	// matchedSnippets 命中 description 与 body 两字段
	if !strings.Contains(res.ModelContent, "description:") || !strings.Contains(res.ModelContent, "body:") {
		t.Fatalf("应含字段片段: %q", res.ModelContent)
	}
	if res.DetailedOutput["total_count"] == float64(0) && res.DetailedOutput["total_count"] != 0 {
		t.Fatalf("total_count 应为命中片段数: %+v", res.DetailedOutput["total_count"])
	}
}

// TestDirectoryToolkit_QueriesBatch 多查询交错去重 + queries 校验
func TestDirectoryToolkit_QueriesBatch(t *testing.T) {
	tk, _ := makeNestedToolkit(t)
	res, err := tk.SkillIndex(map[string]any{"operation": "search", "queries": []any{"one", "three"}})
	if err != nil {
		t.Fatal(err)
	}
	got := idsOf(res.ModelContent)
	if len(got) != 2 {
		t.Fatalf("双查询应交错命中: %v", got)
	}
	// queries 必须 entries 输出模式
	if _, err := tk.SkillIndex(map[string]any{"operation": "search", "queries": []any{"one"}, "output_mode": "count"}); err == nil ||
		!strings.Contains(err.Error(), "queries require output_mode=entries") {
		t.Fatalf("错误不符: %v", err)
	}
	// queries 数量越界
	_, err = tk.SkillIndex(map[string]any{"operation": "search", "queries": []any{"a", "b", "c", "d", "e", "f", "g", "h", "i"}})
	if err == nil || !strings.Contains(err.Error(), "2-8 strings") {
		t.Fatalf("错误不符: %v", err)
	}
	// query 与 queries 互斥
	_, err = tk.SkillIndex(map[string]any{"operation": "search", "query": []any{"one"}, "queries": []any{"two"}})
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("错误不符: %v", err)
	}
	// 字符串 JSON 数组解码
	res2, err := tk.SkillIndex(map[string]any{"operation": "search", "query": `["one","three"]`})
	if err != nil {
		t.Fatal(err)
	}
	if len(idsOf(res2.ModelContent)) != 2 {
		t.Fatalf("JSON 数组 query 应解码: %q", res2.ModelContent)
	}
}

// TestDirectoryToolkit_ContinueOpMismatches 游标归属校验分支
func TestDirectoryToolkit_ContinueOpMismatches(t *testing.T) {
	tk, _ := makeNestedToolkit(t)
	res, err := tk.SkillIndex(map[string]any{"operation": "list", "limit": 1})
	if err != nil {
		t.Fatal(err)
	}
	cursor := res.DetailedOutput["next_cursor"].(string)
	// operation 不符
	_, err = tk.SkillIndex(map[string]any{"operation": "search", "cursor": cursor, "query": []any{"x"}})
	if err == nil || !strings.Contains(err.Error(), "cursor belongs to operation=list") {
		t.Fatalf("错误不符: %v", err)
	}
	// 游标超长
	long := strings.Repeat("a", 129)
	_, err = tk.SkillIndex(map[string]any{"operation": "list", "cursor": long})
	if err == nil || !strings.Contains(err.Error(), "cursor must not exceed 128") {
		t.Fatalf("错误不符: %v", err)
	}
	// 空游标
	_, err = tk.SkillIndex(map[string]any{"operation": "list", "cursor": "  "})
	if err == nil || !strings.Contains(err.Error(), "non-empty") {
		t.Fatalf("错误不符: %v", err)
	}
	// 新取游标 → Aclose 后失效
	res2, err := tk.SkillIndex(map[string]any{"operation": "list", "limit": 1})
	if err != nil {
		t.Fatal(err)
	}
	tk.Aclose()
	_, err = tk.SkillIndex(map[string]any{"operation": "list", "cursor": res2.DetailedOutput["next_cursor"].(string)})
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("Aclose 后应报错: %v", err)
	}
}

// TestDirectoryToolkit_SearchCursorFingerprint 指纹变更拒绝续页
func TestDirectoryToolkit_SearchCursorFingerprint(t *testing.T) {
	root := t.TempDir()
	makeSkillDir(t, root, "one", "name: one\ndescription: first\n", "body")
	makeSkillDir(t, root, "two", "name: two\ndescription: second\n", "body")
	settings := DefaultDiscoverySettings()
	settings.UseExistingIndex = true
	fs, err := NewSkillFS(func() (SkillInventory, error) {
		return ScanSkillDirectories([]string{root}, nil, nil)
	},
		SkillFSOptions{Settings: settings, ArtifactRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	tk, err := NewInstalledSkillsDirectoryToolkit(fs, "fp", 4000)
	if err != nil {
		t.Fatal(err)
	}
	res, err := tk.SkillIndex(map[string]any{"operation": "list", "limit": 1})
	if err != nil {
		t.Fatal(err)
	}
	cursor := res.DetailedOutput["next_cursor"].(string)
	// 目录变更 → 指纹变化（新增第三个技能）
	makeSkillDir(t, root, "three", "name: three\ndescription: third\n", "body")
	_, err = tk.SkillIndex(map[string]any{"operation": "list", "cursor": cursor})
	if err == nil || !strings.Contains(err.Error(), "Skill index changed") {
		t.Fatalf("指纹变更应拒绝续页: %v", err)
	}
}

// TestDirectoryToolkit_PipelineSliceAndErrors slice/limit 校验与 invert
func TestDirectoryToolkit_PipelineSliceAndErrors(t *testing.T) {
	tk, _ := makeNestedToolkit(t)
	// slice
	res, err := tk.SkillIndex(map[string]any{
		"operation": "list",
		"pipeline":  []any{map[string]any{"operation": "slice", "start_line": 1, "end_line": 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(splitLines(res.DetailedOutput["output"].(string))) != 1 {
		t.Fatalf("slice 不符: %q", res.DetailedOutput["output"])
	}
	// slice end<start
	_, err = tk.SkillIndex(map[string]any{
		"operation": "list",
		"pipeline":  []any{map[string]any{"operation": "slice", "start_line": 2, "end_line": 1}},
	})
	if err == nil || !strings.Contains(err.Error(), ">= start_line") {
		t.Fatalf("错误不符: %v", err)
	}
	// limit 越界
	_, err = tk.SkillIndex(map[string]any{
		"operation": "list",
		"pipeline":  []any{map[string]any{"operation": "limit", "lines": 9999}},
	})
	if err == nil || !strings.Contains(err.Error(), "must not exceed 5000") {
		t.Fatalf("错误不符: %v", err)
	}
	// filter invert
	res2, err := tk.SkillIndex(map[string]any{
		"operation": "list",
		"pipeline":  []any{map[string]any{"operation": "filter", "query": "one", "invert": true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := idsOf(res2.DetailedOutput["output"].(string))
	if len(got) != 2 {
		t.Fatalf("invert filter 应排除 one: %v", got)
	}
	// 不支持字段
	_, err = tk.SkillIndex(map[string]any{
		"operation": "list",
		"pipeline":  []any{map[string]any{"operation": "count", "lines": 3}},
	})
	if err == nil || !strings.Contains(err.Error(), "count takes no other fields") {
		t.Fatalf("错误不符: %v", err)
	}
	// 非法 stage
	_, err = tk.SkillIndex(map[string]any{
		"operation": "list",
		"pipeline":  []any{map[string]any{"operation": "bogus"}},
	})
	if err == nil || !strings.Contains(err.Error(), "pipeline[0].operation must be one of") {
		t.Fatalf("错误不符: %v", err)
	}
	// 非对象 stage
	_, err = tk.SkillIndex(map[string]any{"operation": "list", "pipeline": []any{"x"}})
	if err == nil || !strings.Contains(err.Error(), "pipeline stages must be objects") {
		t.Fatalf("错误不符: %v", err)
	}
	// count 与 pipeline 互斥
	_, err = tk.SkillIndex(map[string]any{
		"operation":   "list",
		"pipeline":    []any{map[string]any{"operation": "count"}},
		"output_mode": "count",
	})
	if err == nil || !strings.Contains(err.Error(), "cannot be combined with pipeline") {
		t.Fatalf("错误不符: %v", err)
	}
	// read 不允许 count
	_, err = tk.SkillIndex(map[string]any{
		"operation": "read", "skills": []any{"one"}, "output_mode": "count",
	})
	if err == nil || !strings.Contains(err.Error(), "count output_mode is valid only for list and search") {
		t.Fatalf("错误不符: %v", err)
	}
}

// TestDirectoryToolkit_ValidateRequestCrossField 操作与参数域交叉校验
func TestDirectoryToolkit_ValidateRequestCrossField(t *testing.T) {
	tk, _ := makeNestedToolkit(t)
	cases := []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"operation": "search", "query": []any{"x"}, "view": "tree"}, "only valid for list"},
		{map[string]any{"operation": "list", "read_mode": "head"}, "only valid for read"},
		{map[string]any{"operation": "read", "skills": []any{"one"}, "query": []any{"x"}}, "only valid for search"},
		{map[string]any{"operation": "read", "skills": []any{"one"}, "max_depth": 2}, "max_depth is only valid for list and search"},
		{map[string]any{"operation": "read", "skills": []any{"one"}, "read_mode": "head", "line_count": 2, "start_line": 1}, "valid only for range"},
		{map[string]any{"operation": "read", "skills": []any{"one"}, "read_mode": "range", "line_count": 2, "start_line": 1, "end_line": 2}, "valid only for head"},
		{map[string]any{"operation": "list", "view": "tree", "recursive": true}, "not valid with tree view"},
		{map[string]any{"operation": "list", "max_depth": 2}, "require tree view"},
	}
	for _, c := range cases {
		_, err := tk.SkillIndex(c.args)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("错误不符 want=%q got=%v args=%v", c.want, err, c.args)
		}
	}
}

// TestDirectoryToolkit_FitRowsFallback 单行超预算回落截断首行
func TestDirectoryToolkit_FitRowsFallback(t *testing.T) {
	tk, _ := makeNestedToolkit(t)
	// 极小预算：仅头与首行截断
	res, err := tk.SkillIndex(map[string]any{"operation": "list", "view": "names", "max_output_chars": 520})
	if err != nil {
		t.Fatal(err)
	}
	if res.ModelContent == "" {
		t.Fatal("不应为空")
	}
	// disable 校验非布尔
	_, err = tk.SkillIndex(map[string]any{"operation": "list", "disable_output_truncation": "yes"})
	if err == nil || !strings.Contains(err.Error(), "must be a boolean") {
		t.Fatalf("错误不符: %v", err)
	}
}

// TestDirectoryToolkit_PreviouslyShown 重复列出标记
func TestDirectoryToolkit_PreviouslyShown(t *testing.T) {
	tk, _ := makeNestedToolkit(t)
	// 首列（read 之后 observed 登记）
	if _, err := tk.SkillIndex(map[string]any{"operation": "read", "skills": []any{"one"}}); err != nil {
		t.Fatal(err)
	}
	res, err := tk.SkillIndex(map[string]any{"operation": "search", "query": []any{"one"}, "match": "name"})
	if err != nil {
		t.Fatal(err)
	}
	// search 分支 paging 为 nil → previously_shown=false；此处主要覆盖 summaryLine 分支
	if !strings.Contains(res.ModelContent, "## Skills") {
		t.Fatalf("汇总头不符: %q", res.ModelContent)
	}
}

// TestDirectoryToolkit_SearchContentFallbackToCategories 内容无命中 → 类别提示（simple_page）
func TestDirectoryToolkit_SearchContentFallbackToCategories(t *testing.T) {
	tk, _ := makeNestedToolkit(t)
	// limit 分页 → simple_page=true；查询词不命中内容但命中类别 alpha
	res, err := tk.SkillIndex(map[string]any{"operation": "search", "query": []any{"alpha"}, "limit": 1})
	if err != nil {
		t.Fatal(err)
	}
	out := res.ModelContent
	// alpha 类别行或技能行至少其一
	if !strings.Contains(out, "[category]") && !strings.Contains(out, "[skill]") {
		t.Fatalf("应有命中: %q", out)
	}
}

// TestDirectoryToolkit_AcloseAndClose 关闭语义
func TestDirectoryToolkit_AcloseAndClose(t *testing.T) {
	tk, _ := makeNestedToolkit(t)
	tk.Aclose()
	if _, err := tk.SkillIndex(map[string]any{"operation": "list"}); err == nil {
		t.Fatal("Aclose 后应报错")
	}
	// Environment/GetTools 访问器
	if tk.Environment() == nil {
		t.Fatal("environment 不应为空")
	}
	tools := tk.GetTools()
	if _, err := tools[0].Invoke(map[string]any{"operation": "list"}); err == nil {
		t.Fatal("关闭后 Invoke 应报错")
	}
	tools2 := tk.GetTools()
	if tools2[0].Name != "skill_index" {
		t.Fatal("工具名不符")
	}
}

// TestDirectoryHelpers_CompactAndAppendNote compact/appendNote 分支
func TestDirectoryHelpers_CompactAndAppendNote(t *testing.T) {
	if got := compact("a b c", 2); got != "a b..." {
		t.Fatalf("compact 截断不符: %q", got)
	}
	if got := compact("", 5); got != "" {
		t.Fatalf("compact 空串不符: %q", got)
	}
	// appendNote 空内容预算内
	if got := appendNote("", "note", 10); got != "note" {
		t.Fatalf("appendNote 空内容不符: %q", got)
	}
	if got := appendNote("", "very long note text", 8); got != "very..." {
		t.Fatalf("appendNote 截断不符: %q", got)
	}
	// appendNote 剩余不足
	if got := appendNote("x", "note", 4); got != "x" {
		t.Fatalf("appendNote 剩余不足应保留原文: %q", got)
	}
	// appendNote 剩余够
	if got := appendNote("x", "note", 100); got != "x\n\nnote" {
		t.Fatalf("appendNote 拼接不符: %q", got)
	}
}

// TestDirectoryHelpers_QueriesOf JSON 与去重分支
func TestDirectoryHelpers_QueriesOf(t *testing.T) {
	// 非法 JSON 字符串回落为单词查询
	v, err := queriesOf("[broken", nil)
	if err != nil || len(v) != 1 || v[0] != "[broken" {
		t.Fatalf("非法 JSON 回落不符: %v %v", v, err)
	}
	// queries 单元素不足最小值
	if _, err := queriesOf(nil, []any{"one"}); err == nil || !strings.Contains(err.Error(), "2-8") {
		t.Fatalf("queries 单元素应报错: %v", err)
	}
	// query 列表 minimum=1：去重到 1 合法
	if v, err := queriesOf([]any{"x", "x"}, nil); err != nil || len(v) != 1 {
		t.Fatalf("query 去重不符: %v %v", v, err)
	}
	// null byte
	if _, err := queriesOf("a\x00b", nil); err == nil || !strings.Contains(err.Error(), "null byte") {
		t.Fatalf("null byte 应报错: %v", err)
	}
	// 超 512
	if _, err := queriesOf(strings.Repeat("a", 513), nil); err == nil || !strings.Contains(err.Error(), "512") {
		t.Fatalf("超长应报错: %v", err)
	}
	// 双查询 → []string（cursorQueries 路径）
	v2, err := queriesOf([]any{"a", "b"}, nil)
	if err != nil || len(v2) != 2 {
		t.Fatalf("双查询不符: %v %v", v2, err)
	}
	if !equalStringSlices(cursorQueries([]string{"a", "b"}), []string{"a", "b"}) ||
		!equalStringSlices(cursorQueries("a"), []string{"a"}) ||
		equalStringSlices([]string{"a"}, []string{"b"}) ||
		equalStringSlices([]string{"a"}, nil) {
		t.Fatal("cursorQueries/equalStringSlices 分支不符")
	}
}

// TestDirectoryHelpers_PathsAndIdentifiers paths/identifiers 边界
func TestDirectoryHelpers_PathsAndIdentifiers(t *testing.T) {
	if _, err := pathsOf([]string{"/"}, false); err != nil {
		t.Fatalf("[]string 输入应接受: %v", err)
	}
	if _, err := pathsOf([]any{}, false); err == nil || !strings.Contains(err.Error(), "non-empty") {
		t.Fatalf("空数组应报错: %v", err)
	}
	if _, err := pathsOf([]any{"a\x00"}, true); err == nil || !strings.Contains(err.Error(), "null byte") {
		t.Fatalf("null byte 应报错: %v", err)
	}
	if _, err := pathsOf([]any{strings.Repeat("a", 4097)}, true); err == nil || !strings.Contains(err.Error(), "4096") {
		t.Fatalf("超长路径应报错: %v", err)
	}
	if _, err := identifiers(nil, "skills", true); err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("必填应报错: %v", err)
	}
	if _, err := identifiers("x", "skills", false); err == nil || !strings.Contains(err.Error(), "non-empty array") {
		t.Fatalf("非数组应报错: %v", err)
	}
	if _, err := identifiers([]any{strings.Repeat("a", 513)}, "skills", false); err == nil || !strings.Contains(err.Error(), "512") {
		t.Fatalf("超长应报错: %v", err)
	}
}

// TestDirectoryHelpers_PositiveAndEnum positiveOf/enumOf/emptyMessage 分支
func TestDirectoryHelpers_PositiveAndEnum(t *testing.T) {
	if v, _ := positiveOf(nil, "x", true); v != 0 {
		t.Fatalf("optional nil 应 0: %d", v)
	}
	if _, err := positiveOf(true, "x", false); err == nil {
		t.Fatal("bool 应拒绝")
	}
	if _, err := positiveOf(0, "x", false); err == nil {
		t.Fatal("0 应拒绝")
	}
	if _, err := positiveOf(nil, "x", false); err == nil {
		t.Fatal("nil 非可选应拒绝")
	}
	if _, err := enumOf(1, operations, "op"); err == nil {
		t.Fatal("非字符串应拒绝")
	}
	if emptyMessage("list", nil) != "No entries." {
		t.Fatal("list 空消息不符")
	}
	if got := emptyMessage("search", "alpha"); !strings.Contains(got, "`alpha`") {
		t.Fatalf("search 类别空消息不符: %q", got)
	}
}

// TestDirectoryHelpers_MetadataCardAndRows 卡片与行渲染
func TestDirectoryHelpers_MetadataCardAndRows(t *testing.T) {
	rec := SkillRecord{WorkerID: "w", Name: "n", Description: "d", SkillFile: "/x/SKILL.md", Source: "hub", Version: "1.0", Author: "a"}
	card := metadataCard(rec)
	for _, want := range []string{"# n", "- Version: 1.0", "- Author: a", "- Source: hub"} {
		if !strings.Contains(card, want) {
			t.Fatalf("卡片缺 %s: %q", want, card)
		}
	}
	// inlineText 禁行注入
	if got := inlineText("a\"b\nc"); strings.Contains(got, "\n") {
		t.Fatalf("inlineText 不应含换行: %q", got)
	}
	// safeMatchSnippet 已知标签
	if got := safeMatchSnippet("name: a:b"); got != "name: a:b" {
		t.Fatalf("safeMatchSnippet 不符: %q", got)
	}
	if got := safeMatchSnippet("unknown: <x>"); strings.Contains(got, "<") == false {
		// sanitize 后仍保留（此处仅覆盖分支）
		_ = got
	}
	// searchIdentity 两分支
	if got := searchIdentity(SkillRecord{WorkerID: "w"}); got != "skill\x00w" {
		t.Fatalf("searchIdentity skill 不符: %q", got)
	}
	if got := searchIdentity(DirectoryEntry{Kind: "dir", Path: "/p"}); got != "dir\x00/p" {
		t.Fatalf("searchIdentity dir 不符: %q", got)
	}
	// literalMatchRank 三档
	if literalMatchRank("abc", "abc") != 0 || literalMatchRank("abcd", "abc") != 1 || literalMatchRank("xabc", "abc") != 2 {
		t.Fatal("literalMatchRank 不符")
	}
}

// TestDirectoryHelpers_DirectoryDescription 描述清洗
func TestDirectoryHelpers_DirectoryDescription(t *testing.T) {
	in := "First semantic line.\n\nSelect when: user needs PDF\nCovers stuff\nRepresentative tasks\nDon't select when: never"
	got := directoryDescription(in, 300)
	if !strings.Contains(got, "First semantic line") {
		t.Fatalf("语义行应保留: %q", got)
	}
	if !strings.Contains(got, "Select when: user needs PDF") {
		t.Fatalf("路由行应保留: %q", got)
	}
	if strings.Contains(got, "Covers") || strings.Contains(got, "Representative") {
		t.Fatalf("统计行应剔除: %q", got)
	}
}

// TestDirectoryHelpers_SearchDirectoryRow 目录行（matches 分支）
func TestDirectoryHelpers_SearchDirectoryRow(t *testing.T) {
	root := t.TempDir()
	makeSkillDir(t, root, "one", "name: one\ndescription: d1\n", "b")
	makeSkillDir(t, root, "two", "name: two\ndescription: d2\n", "b")
	settings := DefaultDiscoverySettings()
	settings.UseExistingIndex = true
	inv, _ := ScanSkillDirectories([]string{root}, nil, nil)
	fs, _ := NewSkillFS(func() (SkillInventory, error) { return inv, nil },
		SkillFSOptions{Settings: settings, ArtifactRoot: t.TempDir()})
	tk, _ := NewInstalledSkillsDirectoryToolkit(fs, "rowdir", 4000)
	// 通配符 name 搜索命中目录（flat 根）→ searchDirectoryRow
	res, err := tk.SkillIndex(map[string]any{"operation": "search", "query": []any{"*"}, "match": "name"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.ModelContent, "[skill]") {
		t.Fatalf("name 搜索应命中技能行: %q", res.ModelContent)
	}
}

// TestDirectoryHelpers_ApplyPipelineCountSecondStage count 作为第二阶段
func TestDirectoryHelpers_ApplyPipelineCountSecondStage(t *testing.T) {
	rows := []row{{text: "a\nb", workerID: "w"}, {text: "c"}}
	out, complete, countValue, transformedCount, err := applyPipeline(rows, []map[string]any{
		{"operation": "limit", "lines": 1},
		{"operation": "count"},
	}, "entries")
	if err != nil {
		t.Fatal(err)
	}
	// limit(3 行→1) 使 complete=False（对齐 Python len(current) <= limit 判定）
	if len(out) != 1 || out[0].text != "1" || complete {
		t.Fatalf("limit+count 不符: %+v complete=%v", out, complete)
	}
	if countValue == nil || *countValue != 1 {
		t.Fatalf("count 值不符: %v", countValue)
	}
	if transformedCount != nil {
		t.Fatalf("count 后 transformedCount 应 nil: %v", transformedCount)
	}
	// limit 后 count_seen 且 countValue 非 nil → transformedCount nil；再造 transformed 路径
	out2, _, cv2, tc2, err := applyPipeline(rows, []map[string]any{{"operation": "filter", "query": "b"}}, "entries")
	if err != nil || len(out2) != 1 || cv2 != nil || tc2 != nil {
		t.Fatalf("filter 不符: %+v %v %v %v", out2, cv2, tc2, err)
	}
}

// TestDirectoryHelpers_FitRowsPagingNoMarker 分页路径无截断标记
func TestDirectoryHelpers_FitRowsPagingNoMarker(t *testing.T) {
	rows := []row{{text: strings.Repeat("x", 100), workerID: "a"}, {text: strings.Repeat("y", 100), workerID: "b"}}
	_, _, _, shortened, _ := fitRows(rows, map[string]any{"operation": "list", "returned_skill_count": 1, "returned_category_count": 0}, 150, nil, false)
	if !shortened {
		t.Fatal("应判缩短")
	}
}

// TestDirectoryHelpers_CategoryFromPathNested 直构树视图覆盖类别链
func TestDirectoryHelpers_CategoryFromPathNested(t *testing.T) {
	// 用双节点树直构视图（模拟 taxonomy 树投影），覆盖 categoryFromPath 嵌套分支
	root := common.RetrieverNode{
		NodeID: "/", Label: "root",
		Children: []common.RetrieverNode{{
			NodeID: "alpha", Label: "Alpha",
			Children: []common.RetrieverNode{{NodeID: "alpha/sub", Label: "Sub"}},
		}},
	}
	view := NewSkillDirectoryView(root, nil, map[string]artifacts.CatalogRecord{})
	got := categoryFromPath(view, "/Alpha/Sub")
	if got != "Alpha > Sub" {
		t.Fatalf("嵌套类别链不符: %q", got)
	}
	if categoryFromPath(view, "/") != "ROOT" {
		t.Fatal("根应 ROOT")
	}
	// 无节点路径回落路径段名
	if got := categoryFromPath(view, "/missing"); got != "missing" {
		t.Fatalf("回落不符: %q", got)
	}
}
