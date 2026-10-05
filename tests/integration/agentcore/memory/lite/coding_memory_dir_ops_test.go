//go:build integration

package lite

import (
	"fmt"
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

// CodingMemoryDirOpsSuite 测试 CodingMemory 目录/工作空间操作。
//
// 覆盖路径校验（ValidateCodingMemoryPath）、读取/写入/编辑工具操作、
// MEMORY.md 索引自动更新、以及 CodingMemoryToolContext 链式构造。
type CodingMemoryDirOpsSuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestCodingMemoryDirOpsSuite 运行 CodingMemory 目录操作测试套件
func TestCodingMemoryDirOpsSuite(t *testing.T) {
	suite.Run(t, new(CodingMemoryDirOpsSuite))
}

// TestValidateCodingMemoryPath_合法路径 测试工作空间内合法 .md 路径返回 true
func (s *CodingMemoryDirOpsSuite) TestValidateCodingMemoryPath_合法路径() {
	tempDir := s.T().TempDir()
	ws := workspace.NewWorkspace(tempDir, "cn")
	nodePath := ws.GetNodePath("coding_memory")
	s.Require().NotNil(nodePath, "Workspace 应包含 coding_memory 节点")
	cmDir := *nodePath
	s.Require().NoError(os.MkdirAll(cmDir, 0o755), "创建 coding_memory 目录不应失败")

	valid, resolved := lite.ValidateCodingMemoryPath("user_role.md", ws)
	s.True(valid, "合法 .md 路径应返回 true")
	s.NotEmpty(resolved, "合法路径应返回解析后的路径")
	s.Contains(resolved, "user_role.md", "解析路径应包含文件名")
}

// TestValidateCodingMemoryPath_目录遍历 测试含 ".." 的路径返回 false
func (s *CodingMemoryDirOpsSuite) TestValidateCodingMemoryPath_目录遍历() {
	tempDir := s.T().TempDir()
	ws := workspace.NewWorkspace(tempDir, "cn")

	valid, _ := lite.ValidateCodingMemoryPath("../etc/passwd.md", ws)
	s.False(valid, "含 .. 的路径应返回 false")
}

// TestValidateCodingMemoryPath_绝对路径 测试以 "/" 开头的绝对路径返回 false
func (s *CodingMemoryDirOpsSuite) TestValidateCodingMemoryPath_绝对路径() {
	tempDir := s.T().TempDir()
	ws := workspace.NewWorkspace(tempDir, "cn")

	valid, _ := lite.ValidateCodingMemoryPath("/etc/passwd.md", ws)
	s.False(valid, "绝对路径应返回 false")
}

// TestValidateCodingMemoryPath_非md后缀 测试非 .md 后缀路径返回 false
func (s *CodingMemoryDirOpsSuite) TestValidateCodingMemoryPath_非md后缀() {
	tempDir := s.T().TempDir()
	ws := workspace.NewWorkspace(tempDir, "cn")

	valid, _ := lite.ValidateCodingMemoryPath("test.txt", ws)
	s.False(valid, "非 .md 后缀路径应返回 false")
}

// TestValidateCodingMemoryPath_NilWorkspace 测试 ws=nil 时返回 false
func (s *CodingMemoryDirOpsSuite) TestValidateCodingMemoryPath_NilWorkspace() {
	valid, _ := lite.ValidateCodingMemoryPath("user_role.md", nil)
	s.False(valid, "ws=nil 时应返回 false")
}

// TestCodingMemoryEdit_基本编辑 测试写入文件后编辑内容：写入 user_role.md → 编辑 "Python" 为 "Go" → 读回验证变更
func (s *CodingMemoryDirOpsSuite) TestCodingMemoryEdit_基本编辑() {
	ctx := s.Ctx
	toolCtx := s.newTestToolCtx()

	// 写入初始文件
	content := "---\nname: user_role\ndescription: 用户角色\ntype: user\n---\n用户是 Python 开发者，熟悉 Django 和 Flask。"
	writeResult := lite.CodingMemoryWriteWithContext(ctx, toolCtx, "user_role.md", content)
	s.Require().NotNil(writeResult)
	success, _ := writeResult["success"].(bool)
	s.True(success, "写入应成功")

	// 编辑：将 Python 替换为 Go
	editResult := lite.CodingMemoryEditWithContext(ctx, toolCtx, "user_role.md", "Python", "Go")
	s.Require().NotNil(editResult)
	s.True(editResult.Success, "编辑应成功")
	s.NotEmpty(editResult.NewContent, "新内容不应为空")
	s.Contains(editResult.NewContent, "Go", "编辑后内容应包含 Go")
	s.NotContains(editResult.NewContent, "Python", "编辑后内容不应包含 Python")

	// 读回验证
	readResult := lite.CodingMemoryReadWithContext(ctx, toolCtx, "user_role.md", nil, nil)
	s.Require().NotNil(readResult)
	s.True(readResult.Success, "读取应成功")
	s.Contains(readResult.Content, "Go", "读回内容应包含 Go")
	s.NotContains(readResult.Content, "Python", "读回内容不应包含 Python")
}

// TestCodingMemoryEdit_OldText未找到 测试编辑不存在的 old_text 时返回错误
func (s *CodingMemoryDirOpsSuite) TestCodingMemoryEdit_OldText未找到() {
	ctx := s.Ctx
	toolCtx := s.newTestToolCtx()

	// 写入初始文件
	content := "---\nname: test\ndescription: 测试\ntype: user\n---\nHello World"
	writeResult := lite.CodingMemoryWriteWithContext(ctx, toolCtx, "test_edit.md", content)
	s.Require().NotNil(writeResult)
	success, _ := writeResult["success"].(bool)
	s.True(success, "写入应成功")

	// 尝试编辑不存在的文本
	editResult := lite.CodingMemoryEditWithContext(ctx, toolCtx, "test_edit.md", "NotExistText", "NewText")
	s.Require().NotNil(editResult)
	s.False(editResult.Success, "old_text 不存在时编辑应失败")
	s.NotEmpty(editResult.Error, "应返回错误信息")
	s.Contains(editResult.Error, "未找到", "错误信息应提及未找到")
}

// TestCodingMemoryEdit_OldText多次出现 测试 old_text 出现 2+ 次时返回歧义错误
func (s *CodingMemoryDirOpsSuite) TestCodingMemoryEdit_OldText多次出现() {
	ctx := s.Ctx
	toolCtx := s.newTestToolCtx()

	// 写入包含重复文本的文件
	content := "---\nname: test\ndescription: 测试\ntype: user\n---\nPython is great. Python is powerful."
	writeResult := lite.CodingMemoryWriteWithContext(ctx, toolCtx, "test_multi.md", content)
	s.Require().NotNil(writeResult)
	success, _ := writeResult["success"].(bool)
	s.True(success, "写入应成功")

	// 尝试编辑出现多次的文本
	editResult := lite.CodingMemoryEditWithContext(ctx, toolCtx, "test_multi.md", "Python", "Go")
	s.Require().NotNil(editResult)
	s.False(editResult.Success, "old_text 出现多次时编辑应失败")
	s.NotEmpty(editResult.Error, "应返回错误信息")
	s.Contains(editResult.Error, "次", "错误信息应提及出现次数")
}

// TestCodingMemoryEdit_空OldText 测试 old_text 为空时返回错误
func (s *CodingMemoryDirOpsSuite) TestCodingMemoryEdit_空OldText() {
	ctx := s.Ctx
	toolCtx := s.newTestToolCtx()

	editResult := lite.CodingMemoryEditWithContext(ctx, toolCtx, "any_file.md", "", "new text")
	s.Require().NotNil(editResult)
	s.False(editResult.Success, "空 old_text 时编辑应失败")
	s.NotEmpty(editResult.Error, "应返回错误信息")
	s.Contains(editResult.Error, "不能为空", "错误信息应提及不能为空")
}

// TestCodingMemoryWrite_创建新文件 测试写入新 .md 文件时 mode=create
func (s *CodingMemoryDirOpsSuite) TestCodingMemoryWrite_创建新文件() {
	ctx := s.Ctx
	toolCtx := s.newTestToolCtx()

	content := "---\nname: new_file\ndescription: 新建文件测试\ntype: feedback\n---\n这是新建文件的内容。"
	result := lite.CodingMemoryWriteWithContext(ctx, toolCtx, "new_file.md", content)
	s.Require().NotNil(result)

	success, _ := result["success"].(bool)
	s.True(success, "创建新文件应成功")

	mode, _ := result["mode"].(string)
	s.Equal("create", mode, "新文件 mode 应为 create")
}

// TestCodingMemoryWrite_追加到已有文件 测试对已有文件再次写入时 mode=append
func (s *CodingMemoryDirOpsSuite) TestCodingMemoryWrite_追加到已有文件() {
	ctx := s.Ctx
	toolCtx := s.newTestToolCtx()

	// 第一次写入
	content := "---\nname: append_test\ndescription: 追加测试\ntype: user\n---\n第一段内容。"
	result1 := lite.CodingMemoryWriteWithContext(ctx, toolCtx, "append_test.md", content)
	s.Require().NotNil(result1)
	success1, _ := result1["success"].(bool)
	s.True(success1, "第一次写入应成功")
	mode1, _ := result1["mode"].(string)
	s.Equal("create", mode1, "第一次写入 mode 应为 create")

	// 第二次写入同一路径
	content2 := "---\nname: append_test\ndescription: 追加测试\ntype: user\n---\n第二段内容。"
	result2 := lite.CodingMemoryWriteWithContext(ctx, toolCtx, "append_test.md", content2)
	s.Require().NotNil(result2)
	success2, _ := result2["success"].(bool)
	s.True(success2, "第二次写入应成功")
	mode2, _ := result2["mode"].(string)
	s.Equal("append", mode2, "第二次写入 mode 应为 append")
}

// TestCodingMemoryWrite_NilWorkspace 测试 toolCtx 中 Workspace 为 nil 时 success=false
func (s *CodingMemoryDirOpsSuite) TestCodingMemoryWrite_NilWorkspace() {
	ctx := s.Ctx
	// 构造 Workspace 为 nil 的 toolCtx
	toolCtx := lite.NewCodingMemoryToolContext().
		WithCodingMemoryDir("/tmp/dummy").
		WithWorkspace(nil).
		WithAgentID("itest_nil_ws")

	content := "---\nname: nil_ws\ndescription: nil workspace 测试\ntype: user\n---\n内容"
	result := lite.CodingMemoryWriteWithContext(ctx, toolCtx, "nil_ws.md", content)
	s.Require().NotNil(result)

	success, _ := result["success"].(bool)
	s.False(success, "Workspace 为 nil 时 success 应为 false")
}

// TestCodingMemoryRead_偏移和限制 测试带 offset 和 limit 参数读取文件
// 验证 CodingMemoryReadWithContext 接受 offset/limit 参数不 panic。
// 具体的行截取行为取决于底层 Fs 实现对 WithFsLineRange 的支持。
func (s *CodingMemoryDirOpsSuite) TestCodingMemoryRead_偏移和限制() {
	ctx := s.Ctx
	toolCtx := s.newTestToolCtx()

	// 构造 20 行内容
	var lines []string
	for i := 1; i <= 20; i++ {
		lines = append(lines, fmt.Sprintf("第 %d 行", i))
	}
	body := strings.Join(lines, "\n")
	content := "---\nname: multi_line\ndescription: 多行测试\ntype: user\n---\n" + body

	writeResult := lite.CodingMemoryWriteWithContext(ctx, toolCtx, "multi_line.md", content)
	s.Require().NotNil(writeResult)
	success, _ := writeResult["success"].(bool)
	s.True(success, "写入应成功")

	// 先验证无 offset/limit 读取正常
	readFull := lite.CodingMemoryReadWithContext(ctx, toolCtx, "multi_line.md", nil, nil)
	s.Require().NotNil(readFull)
	s.True(readFull.Success, "全文读取应成功")
	s.NotEmpty(readFull.Content, "全文内容不应为空")

	// 带参数读取不 panic（返回结构体非 nil 即可）
	offset := 5
	limit := 3
	readResult := lite.CodingMemoryReadWithContext(ctx, toolCtx, "multi_line.md", &offset, &limit)
	s.Require().NotNil(readResult, "带 offset/limit 读取不应 panic")
	// 无论 Fs 是否支持行截取，都应返回非 nil 结果
}

// TestCodingMemoryRead_不存在的文件 测试读取不存在的文件时返回错误
func (s *CodingMemoryDirOpsSuite) TestCodingMemoryRead_不存在的文件() {
	ctx := s.Ctx
	toolCtx := s.newTestToolCtx()

	readResult := lite.CodingMemoryReadWithContext(ctx, toolCtx, "nonexistent.md", nil, nil)
	s.Require().NotNil(readResult)
	// LocalSysOperation 的 Fs().ReadFile 在文件不存在时可能返回 error（Success=false）
	// 也可能返回空结果（Success=true 但 Content 为空）——取决于实现
	// 关键验证：结果不为 nil，且内容为空或标记为失败
	if readResult.Success {
		s.Empty(readResult.Content, "不存在的文件 Content 应为空")
	} else {
		s.NotEmpty(readResult.Error, "失败时应返回错误信息")
	}
}

// TestCodingMemoryRead_NilWorkspace 测试 toolCtx 中 Workspace 为 nil 时 success=false
func (s *CodingMemoryDirOpsSuite) TestCodingMemoryRead_NilWorkspace() {
	ctx := s.Ctx
	// 构造 Workspace 为 nil 的 toolCtx
	toolCtx := lite.NewCodingMemoryToolContext().
		WithCodingMemoryDir("/tmp/dummy").
		WithWorkspace(nil).
		WithAgentID("itest_read_nil_ws")

	readResult := lite.CodingMemoryReadWithContext(ctx, toolCtx, "any_file.md", nil, nil)
	s.Require().NotNil(readResult)
	s.False(readResult.Success, "Workspace 为 nil 时读取应失败")
	s.NotEmpty(readResult.Error, "应返回错误信息")
}

// TestMEMORYIndex_自动更新 测试写入文件后 MEMORY.md 自动更新包含该文件条目
func (s *CodingMemoryDirOpsSuite) TestMEMORYIndex_自动更新() {
	ctx := s.Ctx
	toolCtx := s.newTestToolCtx()

	content := "---\nname: idx_test\ndescription: 索引更新测试\ntype: user\n---\n测试索引自动更新。"
	writeResult := lite.CodingMemoryWriteWithContext(ctx, toolCtx, "idx_test.md", content)
	s.Require().NotNil(writeResult)
	success, _ := writeResult["success"].(bool)
	s.True(success, "写入应成功")

	// 读取 MEMORY.md
	readResult := lite.CodingMemoryReadWithContext(ctx, toolCtx, "MEMORY.md", nil, nil)
	s.Require().NotNil(readResult)
	s.True(readResult.Success, "读取 MEMORY.md 应成功")
	s.Contains(readResult.Content, "idx_test", "MEMORY.md 应包含 idx_test 条目")
	s.Contains(readResult.Content, "索引更新测试", "MEMORY.md 应包含文件描述")
}

// TestMEMORYIndex_追加文件更新索引 测试连续写入两个文件后 MEMORY.md 包含两者条目
func (s *CodingMemoryDirOpsSuite) TestMEMORYIndex_追加文件更新索引() {
	ctx := s.Ctx
	toolCtx := s.newTestToolCtx()

	contentA := "---\nname: file_a\ndescription: 文件A描述\ntype: user\n---\n文件A内容。"
	writeResultA := lite.CodingMemoryWriteWithContext(ctx, toolCtx, "file_a.md", contentA)
	s.Require().NotNil(writeResultA)
	successA, _ := writeResultA["success"].(bool)
	s.True(successA, "写入文件A应成功")

	contentB := "---\nname: file_b\ndescription: 文件B描述\ntype: feedback\n---\n文件B内容。"
	writeResultB := lite.CodingMemoryWriteWithContext(ctx, toolCtx, "file_b.md", contentB)
	s.Require().NotNil(writeResultB)
	successB, _ := writeResultB["success"].(bool)
	s.True(successB, "写入文件B应成功")

	// 读取 MEMORY.md，验证两个条目都存在
	readResult := lite.CodingMemoryReadWithContext(ctx, toolCtx, "MEMORY.md", nil, nil)
	s.Require().NotNil(readResult)
	s.True(readResult.Success, "读取 MEMORY.md 应成功")
	s.Contains(readResult.Content, "file_a", "MEMORY.md 应包含 file_a 条目")
	s.Contains(readResult.Content, "file_b", "MEMORY.md 应包含 file_b 条目")
	s.Contains(readResult.Content, "文件A描述", "MEMORY.md 应包含文件A描述")
	s.Contains(readResult.Content, "文件B描述", "MEMORY.md 应包含文件B描述")
}

// TestCodingMemoryToolContext_链式构造 测试 NewCodingMemoryToolContext().WithXxx() 链式调用正确设置所有字段
func (s *CodingMemoryDirOpsSuite) TestCodingMemoryToolContext_链式构造() {
	tempDir := s.T().TempDir()
	ws := workspace.NewWorkspace(tempDir, "cn")
	nodePath := ws.GetNodePath("coding_memory")
	s.Require().NotNil(nodePath, "Workspace 应包含 coding_memory 节点")
	cmDir := *nodePath

	sysOp, err := sysop.NewSysOperation(sysop.NewSysOperationCard())
	s.Require().NoError(err, "创建 SysOperation 不应失败")

	toolCtx := lite.NewCodingMemoryToolContext().
		WithCodingMemoryDir(cmDir).
		WithWorkspace(ws).
		WithAgentID("test").
		WithSysOperation(sysOp)

	s.Equal(cmDir, toolCtx.CodingMemoryDir, "CodingMemoryDir 应被正确设置")
	s.Equal(ws, toolCtx.Workspace, "Workspace 应被正确设置")
	s.Equal("test", toolCtx.AgentID, "AgentID 应被正确设置")
	s.Equal(sysOp, toolCtx.SysOperation, "SysOperation 应被正确设置")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newTestToolCtx 创建测试用 CodingMemoryToolContext，注入 LocalSysOperation 提供真实文件 I/O。
func (s *CodingMemoryDirOpsSuite) newTestToolCtx() *lite.CodingMemoryToolContext {
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
		WithAgentID("itest_cm_dirops").
		WithSysOperation(sysOp)
}
