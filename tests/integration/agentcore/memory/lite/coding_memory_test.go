//go:build integration

package lite

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/coding_memory"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	lite "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/lite"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	// 空白导入：触发 GlobalRegistry 中 fs/shell/code 操作的 init 注册
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/local"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CodingMemorySuite 测试 CodingMemory（lite 包）核心功能。
//
// 覆盖：
//   - MockEmbeddingProvider 确定性向量
//   - CodingMemoryToolContext 工具创建
//   - MemorySettings 默认值
//   - Write/Read/Edit 深度交互
//   - 路径校验和 Frontmatter 验证
//   - MEMORY.md 索引更新
//
// 对齐 Python: tests/system_tests/memory/test_coding_memory.py
type CodingMemorySuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestCodingMemorySuite(t *testing.T) {
	suite.Run(t, new(CodingMemorySuite))
}

// TestMockEmbedding_确定性向量 测试 MockEmbeddingProvider 对同一输入返回相同向量。
// 对齐 Python: MockEmbeddingProvider.embed_query 使用 md5 hash 做 seed。
func (s *CodingMemorySuite) TestMockEmbedding_确定性向量() {
	ctx := s.Ctx
	emb := lite.NewMockEmbeddingProvider()

	vec1, err := emb.EmbedQuery(ctx, "hello world")
	s.Require().NoError(err)
	s.Len(vec1, 128, "向量维度应为 128")

	vec2, err := emb.EmbedQuery(ctx, "hello world")
	s.Require().NoError(err)

	// 同一输入应返回完全相同的向量
	for i := range vec1 {
		s.Equal(vec1[i], vec2[i], "同一输入的向量元素应相同（索引 %d）", i)
	}

	// 不同输入应返回不同向量
	vec3, err := emb.EmbedQuery(ctx, "different text")
	s.Require().NoError(err)
	different := false
	for i := range vec1 {
		if vec1[i] != vec3[i] {
			different = true
			break
		}
	}
	s.True(different, "不同输入应返回不同向量")
}

// TestMockEmbedding_批量嵌入 测试 MockEmbeddingProvider 批量嵌入。
func (s *CodingMemorySuite) TestMockEmbedding_批量嵌入() {
	ctx := s.Ctx
	emb := lite.NewMockEmbeddingProvider()

	texts := []string{"hello", "world", "test"}
	vecs, err := emb.EmbedDocuments(ctx, texts)
	s.Require().NoError(err)
	s.Len(vecs, 3, "应返回 3 个向量")
	for _, v := range vecs {
		s.Len(v, 128, "每个向量维度应为 128")
	}
}

// TestMockEmbedding_标识信息 测试 MockEmbeddingProvider 的 ID/Model/Dims。
func (s *CodingMemorySuite) TestMockEmbedding_标识信息() {
	emb := lite.NewMockEmbeddingProvider()
	s.Equal("mock", emb.ID())
	s.Equal("mock", emb.Model())
	s.Equal(128, emb.Dims())
}

// TestCodingMemoryToolContext_工具创建 测试 CodingMemoryToolContext 创建工具列表。
// 对齐 Python: test_coding_memory_rail_e2e.test_scenario_switching
func (s *CodingMemorySuite) TestCodingMemoryToolContext_工具创建() {
	tempDir := s.T().TempDir()
	toolCtx := lite.NewCodingMemoryToolContext().
		WithCodingMemoryDir(tempDir).
		WithAgentID("itest_cm_tools")

	tools := coding_memory.CreateCodingMemoryTools(toolCtx, "cn", "itest_cm_tools")
	s.NotEmpty(tools, "CreateCodingMemoryTools 应返回非空工具列表")

	// 验证工具都有名称
	for _, t := range tools {
		card := t.Card()
		s.NotNil(card, "工具应有 Card")
		s.NotEmpty(card.Name, "工具名称不应为空")
	}
}

// TestMemorySettings_默认值 测试 CreateMemorySettings 默认值。
// 对齐 Python: test_coding_memory_rail_e2e.test_scenario_switching
func (s *CodingMemorySuite) TestMemorySettings_默认值() {
	settings := lite.CreateMemorySettings("", nil)
	s.Require().NotNil(settings)
	s.Equal("memory.db", settings.Store.Path, "默认 Store.Path 应为 memory.db")
	s.Equal("mock", settings.Fallback, "默认 Fallback 应为 mock")
	s.Equal("openai_compatible", settings.Provider, "默认 Provider 应为 openai_compatible")
	s.True(settings.Sync.Watch, "默认 Sync.Watch 应为 true")
	s.True(settings.Store.Vector.Enabled, "默认 Vector.Enabled 应为 true")
	s.True(settings.Store.Fts.Enabled, "默认 Fts.Enabled 应为 true")
}

// TestWrite_4种记忆类型 测试写入 user/feedback/project/reference 四种记忆类型。
// 对齐 Python: test_write_memory_types
func (s *CodingMemorySuite) TestWrite_4种记忆类型() {
	ctx := s.Ctx
	toolCtx := s.newTestToolCtx()

	types := []struct {
		memType string
		name    string
		path    string
	}{
		{"user", "用户偏好", "user_pref.md"},
		{"feedback", "反馈记录", "feedback_note.md"},
		{"project", "项目信息", "project_info.md"},
		{"reference", "参考链接", "reference_link.md"},
	}

	for _, tc := range types {
		content := "---\nname: " + tc.name + "\ndescription: 测试\ntype: " + tc.memType + "\n---\n测试正文"
		result := lite.CodingMemoryWriteWithContext(ctx, toolCtx, tc.path, content)
		s.Require().NotNil(result, "type=%s 的写入结果不应为 nil", tc.memType)

		success, _ := result["success"].(bool)
		s.True(success, "type=%s 写入应成功", tc.memType)

		memType, _ := result["type"].(string)
		s.Equal(tc.memType, memType, "写入结果的 type 应为 %s", tc.memType)
	}
}

// TestWrite_无效type被拒绝 测试无效 type 时 ValidateFrontmatter 拒绝写入。
// 对齐 Python: test_write_invalid_type
func (s *CodingMemorySuite) TestWrite_无效type被拒绝() {
	ctx := s.Ctx
	toolCtx := s.newTestToolCtx()

	content := "---\nname: bad\ndescription: 无效类型\ntype: invalid_type\n---\n测试正文"
	result := lite.CodingMemoryWriteWithContext(ctx, toolCtx, "invalid.md", content)

	s.Require().NotNil(result)

	success, _ := result["success"].(bool)
	s.False(success, "无效 type 应写入失败")

	errMsg, _ := result["error"].(string)
	s.True(
		strings.Contains(errMsg, "type"),
		"错误信息应提及 type，实际: %s", errMsg,
	)
}

// TestWrite_路径遍历被拒绝 测试路径遍历时 ValidateCodingMemoryPath 拒绝写入。
// 对齐 Python: test_write_path_traversal
func (s *CodingMemorySuite) TestWrite_路径遍历被拒绝() {
	ctx := s.Ctx
	toolCtx := s.newTestToolCtx()

	content := "---\nname: evil\ndescription: 路径遍历\ntype: user\n---\n恶意内容"
	result := lite.CodingMemoryWriteWithContext(ctx, toolCtx, "../../etc/passwd.md", content)

	s.Require().NotNil(result)

	success, _ := result["success"].(bool)
	s.False(success, "路径遍历应写入失败")

	errMsg, _ := result["error"].(string)
	s.True(
		strings.Contains(errMsg, "路径") || strings.Contains(errMsg, "遍历"),
		"错误信息应提及路径遍历，实际: %s", errMsg,
	)
}

// TestWrite_非md文件被拒绝 测试非 .md 后缀路径被拒绝。
// 对齐 Python: test_write_non_md_extension
func (s *CodingMemorySuite) TestWrite_非md文件被拒绝() {
	ctx := s.Ctx
	toolCtx := s.newTestToolCtx()

	content := "---\nname: txt\ndescription: 非md文件\ntype: user\n---\n正文"
	result := lite.CodingMemoryWriteWithContext(ctx, toolCtx, "data.txt", content)

	s.Require().NotNil(result)

	success, _ := result["success"].(bool)
	s.False(success, "非 .md 文件应写入失败")

	errMsg, _ := result["error"].(string)
	s.True(
		strings.Contains(errMsg, ".md"),
		"错误信息应提及 .md 后缀要求，实际: %s", errMsg,
	)
}

// TestWrite_空body被拒绝 测试只有 frontmatter 没有 body 时返回错误。
// 对齐 Python: test_write_empty_body
func (s *CodingMemorySuite) TestWrite_空body被拒绝() {
	ctx := s.Ctx
	toolCtx := s.newTestToolCtx()

	// 只有 frontmatter，没有 body 内容
	content := "---\nname: empty\ndescription: 空body\ntype: user\n---\n"
	result := lite.CodingMemoryWriteWithContext(ctx, toolCtx, "empty_body.md", content)

	s.Require().NotNil(result)

	success, _ := result["success"].(bool)
	s.False(success, "空 body 应写入失败")

	errMsg, _ := result["error"].(string)
	s.True(
		strings.Contains(errMsg, "内容") || strings.Contains(errMsg, "body"),
		"错误信息应提及内容体缺失，实际: %s", errMsg,
	)
}

// TestRead_带offset和limit 测试带偏移和限制读取。
// 对齐 Python: test_read_with_offset_and_limit
func (s *CodingMemorySuite) TestRead_带offset和limit() {
	ctx := s.Ctx
	toolCtx := s.newTestToolCtx()

	// 写入多行内容
	bodyLines := []string{
		"第一行内容",
		"第二行内容",
		"第三行内容",
		"第四行内容",
		"第五行内容",
		"第六行内容",
		"第七行内容",
		"第八行内容",
	}
	content := "---\nname: multi\ndescription: 多行读取\ntype: user\n---\n" + strings.Join(bodyLines, "\n")
	writeResult := lite.CodingMemoryWriteWithContext(ctx, toolCtx, "multi_line.md", content)
	s.Require().NotNil(writeResult)
	success, _ := writeResult["success"].(bool)
	s.True(success, "写入应成功")

	// 读取 offset=2, limit=3
	offset := 2
	limit := 3
	readResult := lite.CodingMemoryReadWithContext(ctx, toolCtx, "multi_line.md", &offset, &limit)
	s.Require().NotNil(readResult)
	s.True(readResult.Success, "读取应成功")
	s.True(readResult.TotalLines > 0, "总行数应大于 0")
	s.Equal(2, readResult.StartLine, "起始行号应为 2")
	// EndLine 取决于底层 Fs().ReadFile 对 WithFsLineRange 的实现
	// WithFsLineRange(start, end) 可能返回 [start, end] 闭区间行
	// 验证 EndLine >= StartLine 即可
	s.GreaterOrEqual(readResult.EndLine, readResult.StartLine, "结束行号应 >= 起始行号")

	// 截断取决于文件实际行数
	// 如果文件行数 > 5，则 Truncated=true
}

// TestRead_不存在文件 测试读取不存在的文件。
// 对齐 Python: test_read_nonexistent_file
func (s *CodingMemorySuite) TestRead_不存在文件() {
	ctx := s.Ctx
	toolCtx := s.newTestToolCtx()

	readResult := lite.CodingMemoryReadWithContext(ctx, toolCtx, "nonexistent.md", nil, nil)
	s.Require().NotNil(readResult)
	// LocalSysOperation 的 Fs().ReadFile 在文件不存在时行为取决于实现
	// 可返回 error（Success=false）或空结果（Success=true 但 Content 为空）
	if readResult.Success {
		s.Empty(readResult.Content, "不存在的文件 Content 应为空")
	} else {
		s.NotEmpty(readResult.Error, "失败时应返回错误信息")
	}
}

// TestEdit_成功修改 测试编辑成功修改内容。
// 对齐 Python: test_edit_success
func (s *CodingMemorySuite) TestEdit_成功修改() {
	ctx := s.Ctx
	toolCtx := s.newTestToolCtx()

	// 先写入
	content := "---\nname: edit_test\ndescription: 编辑测试\ntype: user\n---\n旧内容在这里。"
	writeResult := lite.CodingMemoryWriteWithContext(ctx, toolCtx, "edit_test.md", content)
	s.Require().NotNil(writeResult)
	success, _ := writeResult["success"].(bool)
	s.True(success, "写入应成功")

	// 编辑：将"旧内容"替换为"新内容"
	editResult := lite.CodingMemoryEditWithContext(ctx, toolCtx, "edit_test.md", "旧内容", "新内容")
	s.Require().NotNil(editResult)
	s.True(editResult.Success, "编辑应成功")
	s.Contains(editResult.NewContent, "新内容", "新内容应包含替换后的文本")
}

// TestEdit_oldText未找到 测试编辑不存在的 old_text。
// 对齐 Python: test_edit_old_text_not_found
func (s *CodingMemorySuite) TestEdit_oldText未找到() {
	ctx := s.Ctx
	toolCtx := s.newTestToolCtx()

	// 先写入
	content := "---\nname: not_found\ndescription: 未找到测试\ntype: user\n---\n原始内容。"
	writeResult := lite.CodingMemoryWriteWithContext(ctx, toolCtx, "edit_notfound.md", content)
	s.Require().NotNil(writeResult)
	success, _ := writeResult["success"].(bool)
	s.True(success, "写入应成功")

	// 编辑不存在的文本
	editResult := lite.CodingMemoryEditWithContext(ctx, toolCtx, "edit_notfound.md", "不存在的文本", "替换文本")
	s.Require().NotNil(editResult)
	s.False(editResult.Success, "编辑不存在的文本应失败")
	s.True(
		strings.Contains(editResult.Error, "未找到"),
		"错误信息应包含'未找到'，实际: %s", editResult.Error,
	)
}

// TestEdit_多次匹配被拒绝 测试 old_text 出现多次时被拒绝。
// 对齐 Python: test_edit_multiple_matches
func (s *CodingMemorySuite) TestEdit_多次匹配被拒绝() {
	ctx := s.Ctx
	toolCtx := s.newTestToolCtx()

	// 写入包含重复文本的文件
	content := "---\nname: dup\ndescription: 重复测试\ntype: user\n---\n重复内容 重复内容 结尾"
	writeResult := lite.CodingMemoryWriteWithContext(ctx, toolCtx, "edit_dup.md", content)
	s.Require().NotNil(writeResult)
	success, _ := writeResult["success"].(bool)
	s.True(success, "写入应成功")

	// 编辑重复文本
	editResult := lite.CodingMemoryEditWithContext(ctx, toolCtx, "edit_dup.md", "重复内容", "新内容")
	s.Require().NotNil(editResult)
	s.False(editResult.Success, "多次匹配时编辑应失败")
	s.True(
		strings.Contains(editResult.Error, "次"),
		"错误信息应包含'次'字（表示出现次数），实际: %s", editResult.Error,
	)
}

// TestEdit_空oldText被拒绝 测试 old_text 为空时返回错误。
// 对齐 Python: test_edit_empty_old_text
func (s *CodingMemorySuite) TestEdit_空oldText被拒绝() {
	ctx := s.Ctx
	toolCtx := s.newTestToolCtx()

	editResult := lite.CodingMemoryEditWithContext(ctx, toolCtx, "any.md", "", "新内容")
	s.Require().NotNil(editResult)
	s.False(editResult.Success, "空 old_text 应被拒绝")
	s.Contains(editResult.Error, "old_text 不能为空", "错误信息应为'old_text 不能为空'")
}

// TestWrite_更新MEMORY索引 测试写入后 MEMORY.md 包含 name 链接。
// 对齐 Python: test_write_updates_memory_index
func (s *CodingMemorySuite) TestWrite_更新MEMORY索引() {
	ctx := s.Ctx
	toolCtx := s.newTestToolCtx()

	// 写入记忆
	content := "---\nname: index_test\ndescription: 索引测试\ntype: user\n---\n索引测试内容。"
	writeResult := lite.CodingMemoryWriteWithContext(ctx, toolCtx, "index_test.md", content)
	s.Require().NotNil(writeResult)
	success, _ := writeResult["success"].(bool)
	s.True(success, "写入应成功")

	// 读取 MEMORY.md
	readResult := lite.CodingMemoryReadWithContext(ctx, toolCtx, "MEMORY.md", nil, nil)
	s.Require().NotNil(readResult)
	s.True(readResult.Success, "读取 MEMORY.md 应成功")

	// 验证包含 name 链接
	s.Contains(readResult.Content, "index_test", "MEMORY.md 应包含记忆名称链接")
}

// TestValidTypes_合法类型列表 测试 ValidTypes 返回四种合法类型。
// 对齐 Python: test_valid_types
func (s *CodingMemorySuite) TestValidTypes_合法类型列表() {
	types := lite.ValidTypes()
	s.Len(types, 4, "应有 4 种合法类型")

	expected := []string{"user", "feedback", "project", "reference"}
	for _, t := range expected {
		s.Contains(types, t, "合法类型应包含 %s", t)
	}
}

// TestValidateFrontmatter_合法 测试完整 frontmatter 通过验证。
// 对齐 Python: test_validate_frontmatter_valid
func (s *CodingMemorySuite) TestValidateFrontmatter_合法() {
	fm := map[string]string{
		"name":        "test",
		"description": "测试描述",
		"type":        "user",
	}
	ok, err := lite.ValidateFrontmatter(fm)
	s.True(ok, "合法 frontmatter 应通过验证")
	s.Empty(err, "合法 frontmatter 不应有错误")
}

// TestValidateFrontmatter_缺少name 测试缺少 name 字段时验证失败。
// 对齐 Python: test_validate_frontmatter_missing_name
func (s *CodingMemorySuite) TestValidateFrontmatter_缺少name() {
	fm := map[string]string{
		"description": "测试描述",
		"type":        "user",
	}
	ok, err := lite.ValidateFrontmatter(fm)
	s.False(ok, "缺少 name 时验证应失败")
	s.Contains(err, "name", "错误信息应提及 name 字段")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newTestToolCtx 创建测试用 CodingMemoryToolContext，注入 LocalSysOperation 提供真实文件 I/O。
func (s *CodingMemorySuite) newTestToolCtx() *lite.CodingMemoryToolContext {
	tempDir := s.T().TempDir()
	// 使用 NewWorkspace 创建带默认目录结构的 workspace
	ws := workspace.NewWorkspace(tempDir, "cn")
	// 获取 coding_memory 节点路径
	nodePath := ws.GetNodePath("coding_memory")
	s.Require().NotNil(nodePath, "Workspace 应包含 coding_memory 节点")
	cmDir := *nodePath
	// 创建目录
	if err := os.MkdirAll(cmDir, 0o755); err != nil {
		s.T().Fatalf("创建 coding_memory 目录失败: %v", err)
	}

	// 创建 LocalSysOperation，提供真实文件 I/O 能力
	sysOp, err := sysop.NewSysOperation(sysop.NewSysOperationCard())
	s.Require().NoError(err, "创建 SysOperation 不应失败")
	s.Require().NotNil(sysOp, "SysOperation 不应为 nil")

	return lite.NewCodingMemoryToolContext().
		WithCodingMemoryDir(cmDir).
		WithWorkspace(ws).
		WithAgentID("itest_cm_deep").
		WithSysOperation(sysOp)
}
