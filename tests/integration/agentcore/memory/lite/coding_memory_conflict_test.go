//go:build integration

package lite

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	lite "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/lite"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	// 空白导入：触发 GlobalRegistry 中 fs/shell/code 操作的 init 注册
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/local"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CodingMemoryConflictSuite 测试 CodingMemory 冲突解决流程。
//
// 对齐 Python: tests/system_tests/memory/test_coding_memory_conflict.py
// 覆盖 WriteResult 结构验证和基础写入/读取交互。
// 完整的冲突检测（searchSimilar + MemUpdateChecker）需要嵌入和 LLM，此处验证数据结构层和文件 I/O 层。
type CodingMemoryConflictSuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestCodingMemoryConflictSuite(t *testing.T) {
	suite.Run(t, new(CodingMemoryConflictSuite))
}

// newTestToolCtx 创建测试用 CodingMemoryToolContext，注入 LocalSysOperation 提供真实文件 I/O。
func (s *CodingMemoryConflictSuite) newTestToolCtx() *lite.CodingMemoryToolContext {
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
	// SysOperationCard 默认 Mode=OperationModeLocal，GlobalRegistry 中已注册 fs 操作
	sysOp, err := sysop.NewSysOperation(sysop.NewSysOperationCard())
	s.Require().NoError(err, "创建 SysOperation 不应失败")
	s.Require().NotNil(sysOp, "SysOperation 不应为 nil")

	return lite.NewCodingMemoryToolContext().
		WithCodingMemoryDir(cmDir).
		WithWorkspace(ws).
		WithAgentID("itest_cm_conflict").
		WithSysOperation(sysOp)
}

// TestWriteResult_成功写入结构 测试成功写入的 WriteResult 包含必要字段。
// 对齐 Python: test_successful_write_result_structure
func (s *CodingMemoryConflictSuite) TestWriteResult_成功写入结构() {
	ctx := s.Ctx
	toolCtx := s.newTestToolCtx()

	// 带 frontmatter 的合法内容
	result := lite.CodingMemoryWriteWithContext(ctx, toolCtx, "test_structure.md",
		"---\nname: test\ndescription: 测试结构\ntype: user\n---\n测试正文")

	s.Require().NotNil(result)

	// 验证必要字段存在
	s.Contains(result, "success", "结果应包含 success 字段")
	s.Contains(result, "path", "结果应包含 path 字段")
	s.Contains(result, "mode", "结果应包含 mode 字段")

	// 验证 success=true
	success, _ := result["success"].(bool)
	s.True(success, "成功写入时 success 应为 true")

	// 验证 mode 是有效值
	mode, _ := result["mode"].(string)
	s.True(
		mode == "create" || mode == "append" || mode == "skip",
		"mode 应为 create/append/skip 之一，实际: %s", mode,
	)
}

// TestWriteResult_失败写入结构 测试缺少 frontmatter 时返回结构化错误。
// 对齐 Python: test_failed_write_result_structure
func (s *CodingMemoryConflictSuite) TestWriteResult_失败写入结构() {
	ctx := s.Ctx
	toolCtx := s.newTestToolCtx()

	// 无 frontmatter 的非法内容
	result := lite.CodingMemoryWriteWithContext(ctx, toolCtx, "test_fail.md", "No frontmatter here")

	s.Require().NotNil(result)

	success, _ := result["success"].(bool)
	s.False(success, "缺少 frontmatter 时 success 应为 false")

	// 验证包含 error 字段
	s.Contains(result, "error", "失败结果应包含 error 字段")

	// 验证错误信息提及 frontmatter
	errMsg, _ := result["error"].(string)
	s.True(
		strings.Contains(strings.ToLower(errMsg), "frontmatter"),
		"错误信息应提及 frontmatter，实际: %s", errMsg,
	)
}

// TestWriteResult_不完整Frontmatter 测试缺少必要字段时的错误。
func (s *CodingMemoryConflictSuite) TestWriteResult_不完整Frontmatter() {
	ctx := s.Ctx
	toolCtx := s.newTestToolCtx()

	// 缺少 type 字段
	result := lite.CodingMemoryWriteWithContext(ctx, toolCtx, "incomplete.md",
		"---\nname: test\ndescription: d\n---\nbody")

	s.Require().NotNil(result)

	success, _ := result["success"].(bool)
	s.False(success, "不完整 frontmatter 时 success 应为 false")
}

// TestWriteAndRead_基本流程 测试写入后读取的基本交互。
// 对齐 Python: test_conflict_detected_then_read_and_edit 的读写部分
func (s *CodingMemoryConflictSuite) TestWriteAndRead_基本流程() {
	ctx := s.Ctx
	toolCtx := s.newTestToolCtx()

	content := "---\nname: user_role\ndescription: 用户角色\ntype: user\n---\n用户是 Python 开发者，熟悉 Django 和 Flask。"

	// 写入
	writeResult := lite.CodingMemoryWriteWithContext(ctx, toolCtx, "user_role.md", content)
	s.Require().NotNil(writeResult)

	success, _ := writeResult["success"].(bool)
	s.True(success, "写入应成功")

	// 读取
	readResult := lite.CodingMemoryReadWithContext(ctx, toolCtx, "user_role.md", nil, nil)
	s.Require().NotNil(readResult)
	s.True(readResult.Success, "读取应成功")
	s.NotEmpty(readResult.Content, "读取内容不应为空")
}

// TestWriteResult_冲突文件列表 测试 WriteResult 的冲突文件列表序列化。
// 对齐 Python: test_conflict_result_includes_conflicting_files
func (s *CodingMemoryConflictSuite) TestWriteResult_冲突文件列表() {
	// 直接构造 WriteResult 并验证序列化
	wr := &lite.WriteResult{
		Success:          true,
		Path:             "new_file.md",
		Mode:             lite.WriteModeCreate,
		ConflictDetected: true,
		ConflictingFiles: []string{"existing_a.md", "existing_b.md"},
		Note:             "检测到相似内容，请使用 coding_memory_read 查看 existing_a.md",
	}

	dict := wr.ToDict()
	s.Equal(true, dict["conflict_detected"], "conflict_detected 应为 true")
	s.Contains(dict, "conflicting_files", "结果应包含 conflicting_files")

	// 验证 conflicting_files 是非空列表
	files, ok := dict["conflicting_files"].([]string)
	s.True(ok, "conflicting_files 应为 []string 类型")
	s.Len(files, 2, "应有 2 个冲突文件")
	s.Contains(files, "existing_a.md", "应包含 existing_a.md")
	s.Contains(files, "existing_b.md", "应包含 existing_b.md")
}

// TestWriteResult_冲突Note包含读取指令 测试冲突 Note 中包含读取指令。
// 对齐 Python: test_conflict_note_contains_read_instruction
func (s *CodingMemoryConflictSuite) TestWriteResult_冲突Note包含读取指令() {
	wr := &lite.WriteResult{
		Success:          true,
		Path:             "db_settings.md",
		Mode:             lite.WriteModeCreate,
		ConflictDetected: true,
		ConflictingFiles: []string{"db_config.md"},
		Note:             "检测到与 db_config.md 的冲突，请使用 coding_memory_read 查看",
	}

	dict := wr.ToDict()
	s.Contains(dict, "note", "结果应包含 note 字段")

	note, _ := dict["note"].(string)
	s.True(
		strings.Contains(strings.ToLower(note), "read"),
		"冲突 Note 应包含 read 指令，实际: %s", note,
	)
}

// TestWriteResult_冲突Note包含编辑指令 测试冲突 Note 中包含编辑指令。
// 对齐 Python: test_conflict_note_contains_edit_instruction
func (s *CodingMemoryConflictSuite) TestWriteResult_冲突Note包含编辑指令() {
	wr := &lite.WriteResult{
		Success:          true,
		Path:             "python_style.md",
		Mode:             lite.WriteModeCreate,
		ConflictDetected: true,
		ConflictingFiles: []string{"code_style.md"},
		Note:             "检测到与 code_style.md 的冲突，请使用 coding_memory_edit 修改",
	}

	dict := wr.ToDict()
	note, _ := dict["note"].(string)
	s.True(
		strings.Contains(strings.ToLower(note), "edit"),
		"冲突 Note 应包含 edit 指令，实际: %s", note,
	)
}

// TestWriteResult_Skip模式无冲突 测试 Skip 模式不标记 conflict_detected。
// 对齐 Python: test_no_action_needed_for_skip
func (s *CodingMemoryConflictSuite) TestWriteResult_Skip模式无冲突() {
	wr := &lite.WriteResult{
		Success: true,
		Path:    "test_skip.md",
		Mode:    lite.WriteModeSkip,
		Note:    "内容冗余，已跳过",
	}

	dict := wr.ToDict()
	s.Equal(true, dict["success"], "Skip 模式 success 应为 true")

	// Skip 模式不应设置 conflict_detected
	_, hasConflict := dict["conflict_detected"]
	s.False(hasConflict, "Skip 模式不应包含 conflict_detected 字段")
}

// TestWriteResult_冗余内容Note 测试冗余跳过时 Note 包含冗余说明。
// 对齐 Python: test_redundant_content_skip
func (s *CodingMemoryConflictSuite) TestWriteResult_冗余内容Note() {
	wr := &lite.WriteResult{
		Success: true,
		Path:    "login_api.md",
		Mode:    lite.WriteModeSkip,
		Note:    "redundant: 内容与 api_login.md 重复",
	}

	dict := wr.ToDict()
	note, _ := dict["note"].(string)
	s.True(
		strings.Contains(strings.ToLower(note), "redundant"),
		"冗余跳过的 Note 应包含 'redundant'，实际: %s", note,
	)
}
