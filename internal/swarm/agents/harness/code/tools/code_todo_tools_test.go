package tools

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCodeTodoCardBuilder_四个工具卡片 验证名称、描述与必填入参。
func TestCodeTodoCardBuilder_四个工具卡片(t *testing.T) {
	for _, name := range []string{"todo_create", "todo_list", "todo_get", "todo_modify"} {
		card := CodeTodoCardBuilder(name, "TodoCreateTool", "cn", "agent-1")
		require.NotNil(t, card, "工具 %s 应生成卡片", name)
		require.Equal(t, name, card.Name)
		require.Equal(t, "TodoCreateTool_agent-1", card.ID)
		require.NotEmpty(t, card.Description)
	}

	create := CodeTodoCardBuilder("todo_create", "TodoCreateTool", "cn", "")
	require.NotNil(t, create)
	require.Contains(t, create.ID, "TodoCreateTool_")
	require.Contains(t, create.Description, "Create a todo list for the current session")
	require.NotEmpty(t, create.InputParams)

	list := CodeTodoCardBuilder("todo_list", "TodoListTool", "cn", "a")
	require.Contains(t, list.Description, "List all todos for the current session")

	get := CodeTodoCardBuilder("todo_get", "TodoGetTool", "cn", "a")
	require.Contains(t, get.Description, "Get full details for one todo by id")

	modify := CodeTodoCardBuilder("todo_modify", "TodoModifyTool", "cn", "a")
	require.Contains(t, modify.Description, "Update todo items for the current session")
	require.Contains(t, modify.Description, "insert_after")
}

// TestCodeTodoCardBuilder_未知工具返回nil 验证回落语义。
func TestCodeTodoCardBuilder_未知工具返回nil(t *testing.T) {
	require.Nil(t, CodeTodoCardBuilder("todo_unknown", "X", "cn", "a"))
}
