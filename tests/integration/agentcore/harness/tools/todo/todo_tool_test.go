//go:build integration

package todo

import (
	"testing"

	"github.com/stretchr/testify/suite"
	todo "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/todo"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TodoToolSuite 待办事项工具集成测试套件。
//
// 验证 TodoCreate/TodoList/TodoGet/TodoModify 工具的注册、Schema 定义和描述完整性，
// 不执行真实文件系统读写。
type TodoToolSuite struct {
	isuite.AgentSuite
	// fs mock 文件系统操作
	fs sys_operation.FsOperation
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestTodoToolSuite(t *testing.T) {
	suite.Run(t, new(TodoToolSuite))
}

// SetupSuite 初始化测试套件。
func (s *TodoToolSuite) SetupSuite() {
	s.AgentSuite.SetupSuite()
	s.fs = &stubFsOperation{}
}

// TestTodoToolRegistration 测试待办事项工具注册。
// 对齐 Python: create_todos_tool 返回 4 个工具实例。
func (s *TodoToolSuite) TestTodoToolRegistration() {
	tools, _ := todo.CreateTodosTool("/tmp/test-workspace", s.fs, "cn", "test-agent")
	s.Len(tools, 4, "应创建 4 个待办工具")
}

// TestTodoToolSchema 测试待办事项工具 Schema 定义。
// 对齐 Python: TodoCreateTool.input_schema 包含 tasks 字段。
// 注意：TodoListTool 无输入参数，InputParams 可为空。
func (s *TodoToolSuite) TestTodoToolSchema() {
	tools, _ := todo.CreateTodosTool("/tmp/test-workspace", s.fs, "cn", "test-agent")
	for _, t := range tools {
		card := t.Card()
		s.NotNil(card, "工具 Card 不应为 nil")
		// 验证 Card 基本字段存在
		s.NotEmpty(card.GetName(), "工具名称不应为空")
	}
}

// TestTodoCreateArguments 测试 TodoCreate 工具参数。
// 对齐 Python: TodoCreateTool 的 tasks 字段为必填项。
func (s *TodoToolSuite) TestTodoCreateArguments() {
	tools, _ := todo.CreateTodosTool("/tmp/test-workspace", s.fs, "cn", "test-agent")
	s.Len(tools, 4, "应有 4 个工具")

	// 第一个工具是 TodoCreate
	createTool := tools[0]
	card := createTool.Card()
	s.NotNil(card, "TodoCreate Card 不应为 nil")
	s.NotEmpty(card.GetID(), "TodoCreate ID 不应为空")
}

// TestTodoListArguments 测试 TodoList 工具参数。
// 对齐 Python: TodoListTool 无输入参数。
func (s *TodoToolSuite) TestTodoListArguments() {
	tools, _ := todo.CreateTodosTool("/tmp/test-workspace", s.fs, "cn", "test-agent")
	// 第二个工具是 TodoList
	listTool := tools[1]
	card := listTool.Card()
	s.NotNil(card, "TodoList Card 不应为 nil")
	s.NotEmpty(card.GetID(), "TodoList ID 不应为空")
}

// TestTodoModifyArguments 测试 TodoModify 工具参数。
// 对齐 Python: TodoModifyTool 的 action 字段为必填项。
func (s *TodoToolSuite) TestTodoModifyArguments() {
	tools, _ := todo.CreateTodosTool("/tmp/test-workspace", s.fs, "cn", "test-agent")
	// 第四个工具是 TodoModify
	modifyTool := tools[3]
	card := modifyTool.Card()
	s.NotNil(card, "TodoModify Card 不应为 nil")
	s.NotEmpty(card.GetID(), "TodoModify ID 不应为空")
}

// TestTodoToolDescriptionsNonEmpty 测试待办事项工具描述非空。
// 对齐 Python: 各工具 card.description 非空。
func (s *TodoToolSuite) TestTodoToolDescriptionsNonEmpty() {
	tools, _ := todo.CreateTodosTool("/tmp/test-workspace", s.fs, "cn", "test-agent")
	for _, t := range tools {
		card := t.Card()
		s.NotEmpty(card.GetDescription(), "工具 %s 描述不应为空", card.GetName())
	}
}

// TestMultipleTodoSubToolsCoexist 测试多个待办子工具共存。
// 对齐 Python: create_todos_tool 返回的四个工具 ID 互不重复。
func (s *TodoToolSuite) TestMultipleTodoSubToolsCoexist() {
	tools, _ := todo.CreateTodosTool("/tmp/test-workspace", s.fs, "cn", "test-agent")
	s.Len(tools, 4, "应有 4 个待办工具")

	// 验证所有工具 ID 唯一
	idSet := make(map[string]struct{})
	for _, t := range tools {
		id := t.Card().GetID()
		_, exists := idSet[id]
		s.False(exists, "工具 ID %s 不应重复", id)
		idSet[id] = struct{}{}
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// stubFsOperation 用于测试的桩文件系统操作。
// 嵌入 BaseFsOperation 满足 FsOperation 接口，不执行真实文件读写。
type stubFsOperation struct {
	sys_operation.BaseFsOperation
}

// 编译时验证 stubFsOperation 满足 FsOperation 接口
var _ sys_operation.FsOperation = (*stubFsOperation)(nil)
