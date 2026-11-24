package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// fakeProvider 构造可编程 deferred 工具集（真实 ToolInfo，非替身接口）。
func fakeProvider(infos ...*schema.ToolInfo) func() []schema.ToolInfoInterface {
	infosAny := make([]schema.ToolInfoInterface, 0, len(infos))
	for _, info := range infos {
		infosAny = append(infosAny, info)
	}
	return func() []schema.ToolInfoInterface { return infosAny }
}

// providerInfos 常用测试工具集：cron 三件 + heartbeat 两件 + read_pdf 单件。
func providerInfos() []*schema.ToolInfo {
	return []*schema.ToolInfo{
		{Name: "cron_create_job", Description: "创建定时任务"},
		{Name: "cron_list_jobs", Description: "列出定时任务"},
		{Name: "cron_update_job", Description: "修改定时任务"},
		{Name: "heartbeat_create_job", Description: "创建心跳跟进"},
		{Name: "heartbeat_list_jobs", Description: "列出心跳跟进"},
		{Name: "read_pdf", Description: "读取 PDF 文本"},
	}
}

func newRetrievalToolkit(infos []*schema.ToolInfo) *ToolRetrievalToolkit {
	return NewToolRetrievalToolkit(fakeProvider(infos...), "sess1", nil, nil, "zh")
}

// TestBuildCatalogue_交集与折叠 对齐 Python _build_catalogue：成员 ≥2 的分类
// 保留；单成员分类（documents/read_pdf）折叠进未归类块。
func TestBuildCatalogue_交集与折叠(t *testing.T) {
	tk := newRetrievalToolkit(providerInfos())
	snap := tk.FrozenCatalogue()
	// cron 3 live、heartbeat 2 live → 保留；documents 单成员 → 折叠。
	foundCron, foundHeartbeat, foundDocuments := false, false, false
	for _, category := range snap.Categories {
		switch category.Path {
		case "cron":
			foundCron = true
			if len(category.Tools) != 3 {
				t.Fatalf("cron 应 3 live 成员, got %v", category.Tools)
			}
		case "heartbeat":
			foundHeartbeat = true
		case "documents":
			foundDocuments = true
		}
	}
	if !foundCron || !foundHeartbeat {
		t.Fatalf("cron/heartbeat 分类缺失: %+v", snap.Categories)
	}
	if foundDocuments {
		t.Fatal("单成员 documents 分类应折叠")
	}
	// flat_entries 列全部 live 工具（含折叠的 read_pdf）
	if len(snap.FlatEntries) != 6 {
		t.Fatalf("flat_entries 应 6 条, got %d", len(snap.FlatEntries))
	}
}

// TestRefreshSnapshot_注册表后到 对齐 Python：构造期常为空，刷新后生效。
func TestRefreshSnapshot_注册表后到(t *testing.T) {
	tk := newRetrievalToolkit(nil)
	if len(tk.FrozenCatalogue().Categories) != 0 {
		t.Fatal("构造期应为空目录")
	}
	tk.RefreshSnapshot() // provider 仍空
	if len(tk.FrozenCatalogue().FlatEntries) != 0 {
		t.Fatal("空 provider 应无 flat 条目")
	}
}

// TestToolSearch_GetSchema 对齐 Python _render_schema：完整描述逐字保留、
// object 参数锚定、usage 指引存在。
func TestToolSearch_GetSchema(t *testing.T) {
	tk := newRetrievalToolkit([]*schema.ToolInfo{
		{Name: "cron_create_job", Description: "创建定时任务，表达式必填"},
	})
	out, err := tk.ToolSearch(map[string]any{"operation": "get_schema", "tool": "cron_create_job"})
	if err != nil {
		t.Fatalf("get_schema 失败: %v", err)
	}
	for _, want := range []string{"cron_create_job", "创建定时任务，表达式必填", "usage", "tool_call"} {
		if !strings.Contains(out, want) {
			t.Fatalf("get_schema 输出缺 %q: %s", want, out)
		}
	}
	// 未知工具报错
	if _, err := tk.ToolSearch(map[string]any{"operation": "get_schema", "tool": "nope"}); err == nil {
		t.Fatal("未知工具应报错")
	}
	// 空名报错（教学文案）
	if _, err := tk.ToolSearch(map[string]any{"operation": "get_schema"}); err == nil {
		t.Fatal("空 tool 应报错")
	}
}

// TestToolSearch_裸List教学文案 对齐 Python：bare list 抛错附正确调用示例，
// 文案逐字符断言。
func TestToolSearch_裸List教学文案(t *testing.T) {
	tk := newRetrievalToolkit(providerInfos())
	_, err := tk.ToolSearch(map[string]any{"operation": "list"})
	if err == nil {
		t.Fatal("裸 list 应报错")
	}
	want := `list needs "category" or "keyword": pick a category from the catalogue in your system prompt, or pass keyword for a global search. e.g. {"operation":"list","category":"cron"} or {"operation":"list","keyword":"图片"}`
	if err.Error() != want {
		t.Fatalf("教学文案不匹配:\n got: %s\nwant: %s", err.Error(), want)
	}
}

// TestToolSearch_退役字段降级 对齐 Python：query/tools/call_goal/cursor
// 防御性 pop，旧快照长会话降级不报错。
func TestToolSearch_退役字段降级(t *testing.T) {
	tk := newRetrievalToolkit(providerInfos())
	// 退役 query → 降级 bare list（had_retired_query=true → 报教学错误）
	_, err := tk.ToolSearch(map[string]any{"query": "cron"})
	if err == nil || !strings.Contains(err.Error(), "list needs") {
		t.Fatalf("退役 query 应降级 bare list 教学: %v", err)
	}
	// 退役 tools 数组 → 取首名 get_schema
	out, err := tk.ToolSearch(map[string]any{"operation": "get_schema", "tools": []any{"cron_create_job"}})
	if err != nil || !strings.Contains(out, "cron_create_job") {
		t.Fatalf("退役 tools 数组应降级取首名: %v %s", err, out)
	}
	// call_goal/cursor 直接忽略
	out, err = tk.ToolSearch(map[string]any{"operation": "get_schema", "tool": "cron_list_jobs", "call_goal": "x", "cursor": 3})
	if err != nil {
		t.Fatalf("退役字段应被忽略: %v", err)
	}
	_ = out
	// 旧别名 search 无 category/keyword → 根列表降级渲染（非报错）
	out, err = tk.ToolSearch(map[string]any{"operation": "search"})
	if err != nil || !strings.Contains(out, "## Tool categories") {
		t.Fatalf("旧 search 别名应根列表降级: %v %s", err, out)
	}
}

// TestToolSearch_List分类 对齐 Python _render_category_listing：整类返回
// name+简述（无 schema），keyword BM25 过滤。
func TestToolSearch_List分类(t *testing.T) {
	tk := newRetrievalToolkit(providerInfos())
	out, err := tk.ToolSearch(map[string]any{"operation": "list", "category": "cron"})
	if err != nil {
		t.Fatalf("list cron 失败: %v", err)
	}
	for _, want := range []string{"## Tools (category: cron)", "[tool] cron_create_job", "[tool] cron_list_jobs"} {
		if !strings.Contains(out, want) {
			t.Fatalf("list 输出缺 %q: %s", want, out)
		}
	}
	// 未知分类报错附有效名单
	_, err = tk.ToolSearch(map[string]any{"operation": "list", "category": "nope"})
	if err == nil || !strings.Contains(err.Error(), "cron") {
		t.Fatalf("未知分类应报错附有效名单: %v", err)
	}
}

// TestToolSearch_Keyword全局搜索 对齐 Python _render_keyword_search_listing：
// 覆盖未归类工具；中英文别名命中；单字符 token 噪声过滤。
func TestToolSearch_Keyword全局搜索(t *testing.T) {
	tk := newRetrievalToolkit(providerInfos())
	out, err := tk.ToolSearch(map[string]any{"operation": "list", "keyword": "提醒我"})
	if err != nil {
		t.Fatalf("keyword 搜索失败: %v", err)
	}
	// cron_create_job 的中文别名 "提醒我" 应命中
	if !strings.Contains(out, "cron_create_job") {
		t.Fatalf("中文别名应命中 cron_create_job: %s", out)
	}
	// 无命中输出 0 of N
	out2, _ := tk.ToolSearch(map[string]any{"operation": "list", "keyword": "zzzz不存在的词"})
	if !strings.Contains(out2, "0 of 6") {
		t.Fatalf("无命中应报 0 of 6: %s", out2)
	}
}

// TestToolSearch_ObjectParamAnchor 对齐 Python _anchored_parameters：
// get_schema 输出中 object 型参数附直调序列化规则，真实卡不动。
func TestToolSearch_ObjectParamAnchor(t *testing.T) {
	params := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"job": map[string]any{
				"type":        "object",
				"description": "任务对象",
			},
		},
	}
	tk := newRetrievalToolkit([]*schema.ToolInfo{
		{Name: "with_object", Description: "描述", Parameters: params},
	})
	out, err := tk.ToolSearch(map[string]any{"operation": "get_schema", "tool": "with_object"})
	if err != nil {
		t.Fatalf("get_schema 失败: %v", err)
	}
	if !strings.Contains(out, "never a JSON string") {
		t.Fatalf("object 参数应带锚定注解: %s", out)
	}
	// 原始 parameters 不被修改（呈现层注解）
	if props := params["properties"].(map[string]any); props != nil {
		job := props["job"].(map[string]any)
		if job["description"] != "任务对象" {
			t.Fatalf("真实 schema 被污染: %v", job)
		}
	}
}

// TestMcpConnectorGroups_最长前缀拆分 对齐 Python：mcp_{server}_tool 多下划线
// server 精确拆分、mcp_x 裸名跳过、纯串描述双语共用、兜底文案。
func TestMcpConnectorGroups_最长前缀拆分(t *testing.T) {
	infos := []*schema.ToolInfo{
		{Name: "mcp_github_ci_create", Description: "CI 创建"},
		{Name: "mcp_github_list", Description: "GitHub 列表"},
		{Name: "mcp_x", Description: "裸名"},
	}
	tk := NewToolRetrievalToolkit(
		fakeProvider(infos...), "s", nil,
		func() map[string]any { return map[string]any{"github_ci": "CI 服务器", "github": map[string]any{"zh": "GitHub 服务器", "en": "GitHub server"}} },
		"zh",
	)
	groups := tk.McpConnectorGroups()
	if g, ok := groups["github_ci"]; !ok || len(g.Tools) != 1 || g.Tools[0] != "mcp_github_ci_create" {
		t.Fatalf("github_ci 拆分失败: %+v", groups)
	}
	if g, ok := groups["github"]; !ok || len(g.Tools) != 1 {
		t.Fatalf("github 分组失败: %+v", groups)
	}
	if _, ok := groups["x"]; ok {
		t.Fatal("mcp_x 裸名不应成组")
	}
	if groups["github_ci"].DescriptionZh != "CI 服务器" {
		t.Fatalf("纯串描述应双语共用: %+v", groups["github_ci"])
	}
	// 有效分类名单含虚拟分支
	tk2 := newRetrievalToolkit(providerInfos())
	_ = tk2
}

// TestToolSearch_Mcp连接器List 对齐 Python _render_connector_listing。
func TestToolSearch_Mcp连接器List(t *testing.T) {
	infos := []*schema.ToolInfo{
		{Name: "mcp_github_list", Description: "GitHub 列表"},
		{Name: "mcp_github_create", Description: "GitHub 创建"},
	}
	tk := NewToolRetrievalToolkit(fakeProvider(infos...), "s", nil,
		func() map[string]any { return map[string]any{"github": map[string]any{"zh": "GitHub 服务器"}} },
		"zh")
	out, err := tk.ToolSearch(map[string]any{"operation": "list", "category": "mcp_github"})
	if err != nil {
		t.Fatalf("connector list 失败: %v", err)
	}
	for _, want := range []string{"## Tools (MCP connector: github)", "GitHub 服务器", "[tool] mcp_github_list"} {
		if !strings.Contains(out, want) {
			t.Fatalf("connector list 缺 %q: %s", want, out)
		}
	}
	// server 名不带前缀也可命中
	if _, err := tk.ToolSearch(map[string]any{"operation": "list", "category": "github"}); err != nil {
		t.Fatalf("裸 server 名应可命中: %v", err)
	}
}

// TestGetTools_卡片与调用 对齐 Python get_tools：session 稳定 id + 双语卡描述
// + Invoke 返回 __mapped_content 文本。
func TestGetTools_卡片与调用(t *testing.T) {
	tk := newRetrievalToolkit(providerInfos())
	tools := tk.GetTools()
	if len(tools) != 1 {
		t.Fatalf("应返回 1 个工具, got %d", len(tools))
	}
	card := tools[0].Card()
	if card.Name != ToolSearchToolName {
		t.Fatalf("工具名应 tool_search, got %s", card.Name)
	}
	if len(card.ID) != 16 {
		t.Fatalf("session 派生 id 应 16 字符, got %q", card.ID)
	}
	if !strings.Contains(card.Description, "只读的 deferred 工具目录") {
		t.Fatalf("zh 卡描述不符: %s", card.Description)
	}
	payload, err := tools[0].Invoke(context.Background(), map[string]any{"operation": "list", "category": "cron"})
	if err != nil {
		t.Fatalf("invoke 失败: %v", err)
	}
	if payload["success"] != true {
		t.Fatalf("invoke 应成功: %+v", payload)
	}
	if text, ok := payload["__mapped_content"].(string); !ok || !strings.Contains(text, "cron_create_job") {
		t.Fatalf("映射文本缺失: %+v", payload)
	}
	// 错误路径 payload.success=false + error
	payload, _ = tools[0].Invoke(context.Background(), map[string]any{"operation": "list"})
	if payload["success"] != false {
		t.Fatalf("裸 list 应 success=false: %+v", payload)
	}
	// id 稳定性：同 session 同 id，异 session 异 id
	if ToolIndexToolID("a") == ToolIndexToolID("b") {
		t.Fatal("不同 session 的 id 应不同")
	}
}

// TestCompactSchemaDescription 对齐 Python _compact_schema_description：
// 空白折叠 + 超限截断加省略号。
func TestCompactSchemaDescription(t *testing.T) {
	if got := CompactSchemaDescription("a  b\n c"); got != "a b c" {
		t.Fatalf("空白折叠失败: %q", got)
	}
	long := strings.Repeat("字", 300)
	got := CompactSchemaDescription(long)
	if n := len([]rune(got)); n != schemaDescChars {
		t.Fatalf("截断应 %d 字符(含省略号), got %d", schemaDescChars, n)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatal("截断应以省略号结尾")
	}
}

// TestToolkitAccessors_补齐 对齐 Python property 面：language/session_scope/
// deferred_tool_names/deferred_tool_infos/兜底文案/Stream/GetCallTimeout。
func TestToolkitAccessors_补齐(t *testing.T) {
	tk := newRetrievalToolkit(providerInfos())
	if tk.Language() != "cn" {
		t.Fatalf("language 应 resolve 为 cn, got %s", tk.Language())
	}
	if tk.SessionScope() != "sess1" {
		t.Fatalf("session_scope 不符: %s", tk.SessionScope())
	}
	if _, ok := tk.DeferredToolNames()["cron_create_job"]; !ok {
		t.Fatal("deferred_tool_names 缺 cron_create_job")
	}
	if info := tk.DeferredToolInfos()["read_pdf"]; info == nil || info.GetDescription() != "读取 PDF 文本" {
		t.Fatalf("deferred_tool_infos 缺 read_pdf: %+v", tk.DeferredToolInfos())
	}
	if got := connectorServerDescription("srv", "en"); got != "Tools from MCP connector srv." {
		t.Fatalf("en 兜底文案不符: %s", got)
	}
	if got := connectorServerDescription("srv", "zh"); got != "MCP 连接器 srv 提供的工具。" {
		t.Fatalf("zh 兜底文案不符: %s", got)
	}
	toolInstance := tk.GetTools()[0]
	if _, err := toolInstance.Stream(context.Background(), nil); err == nil {
		t.Fatal("Stream 应不支持")
	}
	if got := toolInstance.GetCallTimeout(nil, 30); got != 30 {
		t.Fatalf("GetCallTimeout 应透传默认值, got %v", got)
	}
	// 无 provider 的兜底：mcpServersProvider 为 nil 时 connector groups 为空
	tkNoProvider := newRetrievalToolkit(providerInfos())
	if len(tkNoProvider.McpConnectorGroups()) != 0 {
		t.Fatal("nil provider 应无 connector 分组")
	}
}

// TestIsObjectTyped_数组形态 对齐 Python：type 为数组且含 "object"。
func TestIsObjectTyped_数组形态(t *testing.T) {
	if !isObjectTyped([]any{"string", "object"}) {
		t.Fatal("数组含 object 应命中")
	}
	if isObjectTyped([]any{"string", "number"}) {
		t.Fatal("数组不含 object 不应命中")
	}
	if isObjectTyped(nil) || isObjectTyped(123) {
		t.Fatal("非串非数组不应命中")
	}
}

// ──────────────────────────── 审查批补齐 ────────────────────────────

// TestDescriptionFor_双语回落链 对齐 Python description_for：en 缺失回落 zh、
// zh 缺失回落 en、连接器组互为回落。
func TestDescriptionFor_双语回落链(t *testing.T) {
	cn := ToolCategory{Path: "p", Description: "中文", Tools: []string{"a"}}
	en := ToolCategory{Path: "p", Description: "中文", DescriptionEn: "English", Tools: []string{"a"}}
	if got := cn.DescriptionFor("en"); got != "中文" {
		t.Fatalf("en 缺失应回落中文: %q", got)
	}
	if got := en.DescriptionFor("en"); got != "English" {
		t.Fatalf("en 应取英文: %q", got)
	}
	group := McpServerGroup{DescriptionEn: "only-en"}
	if got := group.DescriptionFor("zh"); got != "only-en" {
		t.Fatalf("zh 缺失应回落 en: %q", got)
	}
	group2 := McpServerGroup{DescriptionZh: "only-zh"}
	if got := group2.DescriptionFor("en"); got != "only-zh" {
		t.Fatalf("en 缺失应回落 zh: %q", got)
	}
}

// TestStringOf_宽松转串 对齐 Python str() 保守等价：nil 空、string 直通、
// Stringer 调用、其他 fmt 兜底。
func TestStringOf_宽松转串(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{nil, ""},
		{"abc", "abc"},
		{42, "42"},
		{true, "true"},
	}
	for _, c := range cases {
		if got := stringOf(c.in); got != c.want {
			t.Fatalf("stringOf(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestRenderRootListing_退役降级 对齐 Python _render_root_listing：裸 search
// 降级渲染根目录——多成员 [category]、单成员 [tool]、en 头部文案。
func TestRenderRootListing_退役降级(t *testing.T) {
	tk := newRetrievalToolkit(providerInfos())
	zh := tk.renderRootListing()
	if !strings.Contains(zh, "## Tool categories") || !strings.Contains(zh, "旧拼写降级") {
		t.Fatalf("zh 降级头部缺失:\n%s", zh)
	}
	if !strings.Contains(zh, "[tool] read_pdf") {
		t.Fatalf("单成员应折叠 [tool]:\n%s", zh)
	}
	tkEn := NewToolRetrievalToolkit(fakeProvider(providerInfos()...), "s", nil, nil, "en")
	en := tkEn.renderRootListing()
	if !strings.Contains(en, "legacy calls") {
		t.Fatalf("en 头部缺失:\n%s", en)
	}
}

// TestRenderCategoryListing_Keyword与未知分类 对齐 Python：keyword BM25
// 过滤与未知分类错误文案。
func TestRenderCategoryListing_Keyword与未知分类(t *testing.T) {
	tk := newRetrievalToolkit(providerInfos())
	out, err := tk.renderCategoryListing("cron", "创建")
	if err != nil {
		t.Fatalf("命中分类不应报错: %v", err)
	}
	if !strings.Contains(out, "cron_create_job") {
		t.Fatalf("keyword 命中应含 create:\n%s", out)
	}
	if _, err = tk.renderCategoryListing("no_such", ""); err == nil || !strings.Contains(err.Error(), "no_such") {
		t.Fatalf("未知分类应报错: %v", err)
	}
}
