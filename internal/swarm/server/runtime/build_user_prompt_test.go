package runtime

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildUserPrompt_中文基本格式(t *testing.T) {
	result := BuildUserPrompt("你好", nil, "web", "zh", nil, nil)
	assert.Contains(t, result, "你收到一条消息：")
	assert.Contains(t, result, `"content":"你好"`)
	assert.Contains(t, result, `"source":"web"`)
	assert.Contains(t, result, `"preferred_response_language":"zh"`)
	assert.Contains(t, result, `"type":"user input"`)
}

func TestBuildUserPrompt_英文基本格式(t *testing.T) {
	result := BuildUserPrompt("hello", nil, "web", "en", nil, nil)
	assert.Contains(t, result, "You receive a new message:")
	assert.Contains(t, result, `"content":"hello"`)
	assert.Contains(t, result, `"source":"web"`)
}

func TestBuildUserPrompt_cron模式(t *testing.T) {
	result := BuildUserPrompt("查询状态", nil, "cron", "zh", nil, nil)
	assert.Contains(t, result, "必须输出查询到的内容")
	assert.Contains(t, result, `"source":"system"`)
	assert.Contains(t, result, `"type":"cron"`)
}

func TestBuildUserPrompt_heartbeat模式(t *testing.T) {
	result := BuildUserPrompt("心跳检查", nil, "heartbeat", "zh", nil, nil)
	assert.Contains(t, result, `"source":"system"`)
	assert.Contains(t, result, `"type":"heartbeat"`)
}

func TestBuildUserPrompt_带files(t *testing.T) {
	files := map[string]any{"file1.py": "changed"}
	result := BuildUserPrompt("修改了文件", files, "web", "zh", nil, nil)
	assert.Contains(t, result, `"files_updated_by_user"`)
}

func TestBuildUserPrompt_带trustedDirs(t *testing.T) {
	dirs := []string{"/home/user/project"}
	result := BuildUserPrompt("hello", nil, "web", "en", dirs, nil)
	assert.Contains(t, result, `"trusted_dirs"`)
}

func TestBuildUserPrompt_带interactionContext(t *testing.T) {
	metadata := map[string]any{"interaction_context": "之前的对话上下文"}
	result := BuildUserPrompt("继续", nil, "web", "zh", nil, metadata)
	// interaction_context 作为前缀出现在 prompt 开头
	assert.True(t, strings.HasPrefix(result, "\n之前的对话上下文\n\n"))
}

func TestBuildUserPrompt_含时区和时间戳(t *testing.T) {
	result := BuildUserPrompt("test", nil, "web", "zh", nil, nil)
	assert.Contains(t, result, `"timezone":"Asia/Shanghai"`)
	assert.Contains(t, result, `"timestamp"`)
}

func TestBuildUserPrompt_skillsUseSlashCommand(t *testing.T) {
	result := BuildUserPrompt("/skills use web_search, 搜索Go语言", nil, "web", "zh", nil, nil)
	assert.Contains(t, result, `"skills_to_use"`)
	// query 部分应被替换为 "搜索Go语言"
	assert.Contains(t, result, `"content":"搜索Go语言"`)
}

func TestBuildUserPrompt_输出为合法JSON(t *testing.T) {
	result := BuildUserPrompt("test content", nil, "web", "zh", nil, nil)
	// 找到第一个 { 开头，提取 JSON 部分
	idx := strings.Index(result, "{")
	if idx >= 0 {
		jsonStr := result[idx:]
		var parsed map[string]any
		err := json.Unmarshal([]byte(jsonStr), &parsed)
		require.NoError(t, err, "BuildUserPrompt 输出应该是合法 JSON")
	}
}
