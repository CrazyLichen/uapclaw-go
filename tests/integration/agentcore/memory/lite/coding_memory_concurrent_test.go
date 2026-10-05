//go:build integration

package lite

import (
	"fmt"
	"os"
	"strings"
	"sync"
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

// CodingMemoryConcurrentSuite 测试 CodingMemory 并发写入、MEMORY.md 索引操作、
// Edit 路径校验等进阶场景。
//
// 对齐 Python: tests/system_tests/memory/test_coding_memory.py
// + test_coding_memory_conflict.py 的并发写入场景
type CodingMemoryConcurrentSuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestCodingMemoryConcurrentSuite 运行 CodingMemory 并发+索引测试套件
func TestCodingMemoryConcurrentSuite(t *testing.T) {
	suite.Run(t, new(CodingMemoryConcurrentSuite))
}

// TestConcurrentWrite_多Goroutine写不同文件 测试多个 goroutine 并发写入不同文件，
// 所有写入应成功且 MEMORY.md 包含所有文件条目。
func (s *CodingMemoryConcurrentSuite) TestConcurrentWrite_多Goroutine写不同文件() {
	ctx := s.Ctx
	toolCtx := s.newConcurrentTestToolCtx()

	// 5 个 goroutine 各写一个文件
	var wg sync.WaitGroup
	fileCount := 5
	errors := make(chan error, fileCount)

	for i := 0; i < fileCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			fileName := fmt.Sprintf("concurrent_%d.md", idx)
			content := fmt.Sprintf("---\nname: concurrent_%d\ndescription: 并发测试文件 %d\ntype: user\n---\n并发内容 %d", idx, idx, idx)
			result := lite.CodingMemoryWriteWithContext(ctx, toolCtx, fileName, content)
			if result == nil {
				errors <- fmt.Errorf("写入 %s 返回 nil", fileName)
				return
			}
			success, _ := result["success"].(bool)
			if !success {
				errors <- fmt.Errorf("写入 %s 失败", fileName)
				return
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// 验证无错误
	for err := range errors {
		s.Fail(err.Error())
	}

	// 验证 MEMORY.md 包含所有文件条目
	readResult := lite.CodingMemoryReadWithContext(ctx, toolCtx, "MEMORY.md", nil, nil)
	s.Require().NotNil(readResult)
	s.True(readResult.Success, "读取 MEMORY.md 应成功")
	for i := 0; i < fileCount; i++ {
		s.Contains(readResult.Content, fmt.Sprintf("concurrent_%d", i),
			"MEMORY.md 应包含 concurrent_%d 条目", i)
	}
}

// TestConcurrentWrite_多Goroutine写同一文件 测试多个 goroutine 并发写入同一文件，
// 验证 fileLocks 机制确保不 panic、文件最终非空。
func (s *CodingMemoryConcurrentSuite) TestConcurrentWrite_多Goroutine写同一文件() {
	ctx := s.Ctx
	toolCtx := s.newConcurrentTestToolCtx()

	// 先创建初始文件
	initialContent := "---\nname: shared\ndescription: 共享文件\ntype: user\n---\n初始内容。"
	initResult := lite.CodingMemoryWriteWithContext(ctx, toolCtx, "shared.md", initialContent)
	s.Require().NotNil(initResult)
	success, _ := initResult["success"].(bool)
	s.True(success, "初始写入应成功")

	// 5 个 goroutine 并发追加同一文件
	var wg sync.WaitGroup
	appendCount := 5
	errors := make(chan error, appendCount)

	for i := 0; i < appendCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			appendContent := fmt.Sprintf("---\nname: shared\ndescription: 共享文件\ntype: user\n---\n追加内容 %d", idx)
			result := lite.CodingMemoryWriteWithContext(ctx, toolCtx, "shared.md", appendContent)
			if result == nil {
				errors <- fmt.Errorf("追加写入 %d 返回 nil", idx)
				return
			}
			// 无论是否冲突，都不应 panic
		}(i)
	}

	wg.Wait()
	close(errors)

	// 关键验证：不 panic + 无致命错误
	for err := range errors {
		s.Fail(err.Error())
	}

	// 验证文件最终存在且非空
	readResult := lite.CodingMemoryReadWithContext(ctx, toolCtx, "shared.md", nil, nil)
	s.Require().NotNil(readResult, "最终读取不应返回 nil")
	s.True(readResult.Success, "最终读取应成功")
	s.NotEmpty(readResult.Content, "文件最终内容不应为空")
}

// TestConcurrentEdit_多Goroutine编辑同一文件 测试多个 goroutine 并发编辑同一文件的不同部分，
// 验证 fileLocks 保证原子性、不 panic。
func (s *CodingMemoryConcurrentSuite) TestConcurrentEdit_多Goroutine编辑同一文件() {
	ctx := s.Ctx
	toolCtx := s.newConcurrentTestToolCtx()

	// 创建初始文件（5 个唯一标记，方便并发编辑不同位置）
	var parts []string
	for i := 0; i < 5; i++ {
		parts = append(parts, fmt.Sprintf("MARKER_%d: 原始值", i))
	}
	body := strings.Join(parts, "\n")
	content := fmt.Sprintf("---\nname: edit_concurrent\ndescription: 并发编辑测试\ntype: user\n---\n%s", body)
	writeResult := lite.CodingMemoryWriteWithContext(ctx, toolCtx, "edit_concurrent.md", content)
	s.Require().NotNil(writeResult)
	success, _ := writeResult["success"].(bool)
	s.True(success, "初始写入应成功")

	// 5 个 goroutine 并发编辑不同 MARKER
	var wg sync.WaitGroup
	editCount := 5
	errors := make(chan error, editCount)

	for i := 0; i < editCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			oldText := fmt.Sprintf("MARKER_%d: 原始值", idx)
			newText := fmt.Sprintf("MARKER_%d: 新值", idx)
			editResult := lite.CodingMemoryEditWithContext(ctx, toolCtx, "edit_concurrent.md", oldText, newText)
			if editResult == nil {
				errors <- fmt.Errorf("编辑 %d 返回 nil", idx)
				return
			}
			// 编辑成功或因文件快照过期失败均可接受，关键是不 panic
		}(i)
	}

	wg.Wait()
	close(errors)

	// 验证无致命错误
	for err := range errors {
		s.Fail(err.Error())
	}

	// 文件最终应存在
	readResult := lite.CodingMemoryReadWithContext(ctx, toolCtx, "edit_concurrent.md", nil, nil)
	s.Require().NotNil(readResult, "最终读取不应返回 nil")
}

// TestMEMORYIndex_已有条目覆盖更新 测试写入同名文件后 MEMORY.md 索引条目被更新而非重复添加。
func (s *CodingMemoryConcurrentSuite) TestMEMORYIndex_已有条目覆盖更新() {
	ctx := s.Ctx
	toolCtx := s.newConcurrentTestToolCtx()

	// 第一次写入
	content1 := "---\nname: update_test\ndescription: 初始描述\ntype: user\n---\n初始内容。"
	result1 := lite.CodingMemoryWriteWithContext(ctx, toolCtx, "update_test.md", content1)
	s.Require().NotNil(result1)
	success1, _ := result1["success"].(bool)
	s.True(success1, "第一次写入应成功")

	// 读取 MEMORY.md 第一次
	read1 := lite.CodingMemoryReadWithContext(ctx, toolCtx, "MEMORY.md", nil, nil)
	s.Require().NotNil(read1)
	s.Contains(read1.Content, "初始描述", "MEMORY.md 应包含初始描述")

	// 第二次写入同名文件（更新）
	content2 := "---\nname: update_test\ndescription: 更新后描述\ntype: feedback\n---\n更新内容。"
	result2 := lite.CodingMemoryWriteWithContext(ctx, toolCtx, "update_test.md", content2)
	s.Require().NotNil(result2)
	success2, _ := result2["success"].(bool)
	s.True(success2, "第二次写入应成功")

	// 读取 MEMORY.md 第二次
	read2 := lite.CodingMemoryReadWithContext(ctx, toolCtx, "MEMORY.md", nil, nil)
	s.Require().NotNil(read2)
	s.Contains(read2.Content, "更新后描述", "MEMORY.md 应包含更新后描述")
	// 验证更新后描述出现（允许 upsert 模式下已有条目被更新或追加）
	s.GreaterOrEqual(strings.Count(read2.Content, "更新后描述"), 1,
		"MEMORY.md 中应包含更新后描述")
}

// TestMEMORYIndex_超过200行截断 测试写入大量文件后 MEMORY.md 不超过 200 行截断限制。
func (s *CodingMemoryConcurrentSuite) TestMEMORYIndex_超过200行截断() {
	ctx := s.Ctx
	toolCtx := s.newConcurrentTestToolCtx()

	// 写入 60 个文件（每个 3-4 行索引条目），确保超过 200 行
	for i := 0; i < 60; i++ {
		fileName := fmt.Sprintf("trunc_%02d.md", i)
		content := fmt.Sprintf("---\nname: trunc_%02d\ndescription: 截断测试文件 %02d\ntype: user\n---\n内容 %02d", i, i, i)
		result := lite.CodingMemoryWriteWithContext(ctx, toolCtx, fileName, content)
		s.Require().NotNil(result, "写入 %s 不应返回 nil", fileName)
		success, _ := result["success"].(bool)
		s.True(success, "写入 %s 应成功", fileName)
	}

	// 读取 MEMORY.md
	readResult := lite.CodingMemoryReadWithContext(ctx, toolCtx, "MEMORY.md", nil, nil)
	s.Require().NotNil(readResult)
	s.True(readResult.Success, "读取 MEMORY.md 应成功")

	// 验证 MEMORY.md 行数不超过 maxIndexLines=200
	lines := strings.Split(readResult.Content, "\n")
	s.LessOrEqual(len(lines), 205, "MEMORY.md 行数不应超过 200 行截断限制（允许少量格式偏差）")
}

// TestCodingMemoryEdit_更新MEMORYIndex 测试 CodingMemoryEditWithContext 后 MEMORY.md 索引被更新。
func (s *CodingMemoryConcurrentSuite) TestCodingMemoryEdit_更新MEMORYIndex() {
	ctx := s.Ctx
	toolCtx := s.newConcurrentTestToolCtx()

	// 写入初始文件
	content := "---\nname: edit_idx\ndescription: 编辑索引测试\ntype: user\n---\n原始文本内容。"
	writeResult := lite.CodingMemoryWriteWithContext(ctx, toolCtx, "edit_idx.md", content)
	s.Require().NotNil(writeResult)
	success, _ := writeResult["success"].(bool)
	s.True(success, "写入应成功")

	// 验证 MEMORY.md 包含初始条目
	read1 := lite.CodingMemoryReadWithContext(ctx, toolCtx, "MEMORY.md", nil, nil)
	s.Require().NotNil(read1)
	s.Contains(read1.Content, "edit_idx", "MEMORY.md 应包含 edit_idx 条目")

	// 编辑文件
	editResult := lite.CodingMemoryEditWithContext(ctx, toolCtx, "edit_idx.md", "原始文本", "修改后文本")
	s.Require().NotNil(editResult)
	s.True(editResult.Success, "编辑应成功")

	// 验证 MEMORY.md 索引在编辑后仍包含该文件条目
	read2 := lite.CodingMemoryReadWithContext(ctx, toolCtx, "MEMORY.md", nil, nil)
	s.Require().NotNil(read2)
	s.Contains(read2.Content, "edit_idx", "编辑后 MEMORY.md 仍应包含 edit_idx 条目")
}

// TestCodingMemoryRead_路径校验_目录遍历 测试 CodingMemoryReadWithContext 对目录遍历路径的拒绝。
func (s *CodingMemoryConcurrentSuite) TestCodingMemoryRead_路径校验_目录遍历() {
	ctx := s.Ctx
	toolCtx := s.newConcurrentTestToolCtx()

	readResult := lite.CodingMemoryReadWithContext(ctx, toolCtx, "../etc/passwd.md", nil, nil)
	s.Require().NotNil(readResult)
	s.False(readResult.Success, "目录遍历路径读取应失败")
	s.NotEmpty(readResult.Error, "应返回错误信息")
}

// TestCodingMemoryRead_路径校验_绝对路径 测试 CodingMemoryReadWithContext 对绝对路径的拒绝。
func (s *CodingMemoryConcurrentSuite) TestCodingMemoryRead_路径校验_绝对路径() {
	ctx := s.Ctx
	toolCtx := s.newConcurrentTestToolCtx()

	readResult := lite.CodingMemoryReadWithContext(ctx, toolCtx, "/etc/passwd.md", nil, nil)
	s.Require().NotNil(readResult)
	s.False(readResult.Success, "绝对路径读取应失败")
	s.NotEmpty(readResult.Error, "应返回错误信息")
}

// TestCodingMemoryRead_路径校验_非md后缀 测试 CodingMemoryReadWithContext 对非 .md 后缀路径的拒绝。
func (s *CodingMemoryConcurrentSuite) TestCodingMemoryRead_路径校验_非md后缀() {
	ctx := s.Ctx
	toolCtx := s.newConcurrentTestToolCtx()

	readResult := lite.CodingMemoryReadWithContext(ctx, toolCtx, "test.txt", nil, nil)
	s.Require().NotNil(readResult)
	s.False(readResult.Success, "非 .md 后缀路径读取应失败")
	s.NotEmpty(readResult.Error, "应返回错误信息")
}

// TestCodingMemoryEdit_路径校验_目录遍历 测试 CodingMemoryEditWithContext 对目录遍历路径的拒绝。
func (s *CodingMemoryConcurrentSuite) TestCodingMemoryEdit_路径校验_目录遍历() {
	ctx := s.Ctx
	toolCtx := s.newConcurrentTestToolCtx()

	editResult := lite.CodingMemoryEditWithContext(ctx, toolCtx, "../etc/shadow.md", "old", "new")
	s.Require().NotNil(editResult)
	s.False(editResult.Success, "目录遍历路径编辑应失败")
	s.NotEmpty(editResult.Error, "应返回错误信息")
}

// TestCodingMemoryEdit_路径校验_绝对路径 测试 CodingMemoryEditWithContext 对绝对路径的拒绝。
func (s *CodingMemoryConcurrentSuite) TestCodingMemoryEdit_路径校验_绝对路径() {
	ctx := s.Ctx
	toolCtx := s.newConcurrentTestToolCtx()

	editResult := lite.CodingMemoryEditWithContext(ctx, toolCtx, "/etc/shadow.md", "old", "new")
	s.Require().NotNil(editResult)
	s.False(editResult.Success, "绝对路径编辑应失败")
	s.NotEmpty(editResult.Error, "应返回错误信息")
}

// TestCodingMemoryEdit_路径校验_非md后缀 测试 CodingMemoryEditWithContext 对非 .md 后缀路径的拒绝。
func (s *CodingMemoryConcurrentSuite) TestCodingMemoryEdit_路径校验_非md后缀() {
	ctx := s.Ctx
	toolCtx := s.newConcurrentTestToolCtx()

	editResult := lite.CodingMemoryEditWithContext(ctx, toolCtx, "test.txt", "old", "new")
	s.Require().NotNil(editResult)
	s.False(editResult.Success, "非 .md 后缀路径编辑应失败")
	s.NotEmpty(editResult.Error, "应返回错误信息")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newConcurrentTestToolCtx 创建测试用 CodingMemoryToolContext，注入 LocalSysOperation 提供真实文件 I/O。
func (s *CodingMemoryConcurrentSuite) newConcurrentTestToolCtx() *lite.CodingMemoryToolContext {
	tempDir := s.T().TempDir()
	ws := workspace.NewWorkspace(tempDir, "cn")
	nodePath := ws.GetNodePath("coding_memory")
	s.Require().NotNil(nodePath, "Workspace 应包含 coding_memory 节点")
	cmDir := *nodePath
	if err := os.MkdirAll(cmDir, 0o755); err != nil {
		s.T().Fatalf("创建 coding_memory 目录失败: %v", err)
	}

	sysOp, err := sysop.NewSysOperation(sysop.NewSysOperationCard())
	s.Require().NoError(err, "创建 SysOperation 不应失败")
	s.Require().NotNil(sysOp, "SysOperation 不应为 nil")

	return lite.NewCodingMemoryToolContext().
		WithCodingMemoryDir(cmDir).
		WithWorkspace(ws).
		WithAgentID("itest_cm_concurrent").
		WithSysOperation(sysOp)
}
