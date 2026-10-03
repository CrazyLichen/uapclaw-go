package dreaming

import (
	"fmt"
	"strings"
	"testing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestGetPrompt_所有模式语言组合 验证所有 4 种组合的提示词模板都能获取
func TestGetPrompt_所有模式语言组合(t *testing.T) {
	tests := []struct {
		mode     string
		language string
	}{
		{"code", "zh"},
		{"code", "en"},
		{"agent", "zh"},
		{"agent", "en"},
	}
	for _, tt := range tests {
		result := GetPrompt(tt.mode, tt.language)
		if result == "" {
			t.Errorf("GetPrompt(%q, %q) 返回空字符串", tt.mode, tt.language)
		}
	}
}

// TestGetPrompt_回退到默认值 验证未知组合回退到 code/zh
func TestGetPrompt_回退到默认值(t *testing.T) {
	result := GetPrompt("unknown", "fr")
	defaultPrompt := promptMap[[2]string{"code", "zh"}]
	if result != defaultPrompt {
		t.Errorf("GetPrompt('unknown', 'fr') 应回退到 code/zh，实际不同")
	}
}

// TestGetSysMsg_所有模式语言组合 验证所有 4 种组合的系统消息都能获取
func TestGetSysMsg_所有模式语言组合(t *testing.T) {
	tests := []struct {
		mode     string
		language string
		expected string
	}{
		{"code", "zh", "你是技术经验提取助手。严格输出 JSON 数组。"},
		{"code", "en", "You are a technical knowledge extractor. Output JSON array strictly."},
		{"agent", "zh", "你是记忆整理助手。严格输出 JSON 数组。"},
		{"agent", "en", "You are a memory organizer. Output JSON array strictly."},
	}
	for _, tt := range tests {
		result := GetSysMsg(tt.mode, tt.language)
		if result != tt.expected {
			t.Errorf("GetSysMsg(%q, %q) = %q, 期望 %q", tt.mode, tt.language, result, tt.expected)
		}
	}
}

// TestGetSysMsg_回退到默认值 验证未知组合回退到 code/zh
func TestGetSysMsg_回退到默认值(t *testing.T) {
	result := GetSysMsg("unknown", "fr")
	expected := sysMsgMap[[2]string{"code", "zh"}]
	if result != expected {
		t.Errorf("GetSysMsg('unknown', 'fr') 应回退到 code/zh")
	}
}

// TestPromptCodeZH_包含关键段落 验证 code/zh 提示词包含所有关键段落
func TestPromptCodeZH_包含关键段落(t *testing.T) {
	segments := []string{
		"你是技术经验提取助手",
		"值得提取",
		"严禁提取",
		"自检三问",
		"已有知识库",
		"对话记录",
		"JSON 数组",
		"title",
		"content",
	}
	for _, seg := range segments {
		if !strings.Contains(promptCodeZH, seg) {
			t.Errorf("promptCodeZH 缺少关键段落: %q", seg)
		}
	}
}

// TestPromptAgentZH_包含关键段落 验证 agent/zh 提示词包含所有关键段落
func TestPromptAgentZH_包含关键段落(t *testing.T) {
	segments := []string{
		"记忆整理助手",
		"提取标准",
		"已有记忆",
		"对话记录",
		"JSON 数组",
		"title",
		"content",
		"宁缺毋滥",
	}
	for _, seg := range segments {
		if !strings.Contains(promptAgentZH, seg) {
			t.Errorf("promptAgentZH 缺少关键段落: %q", seg)
		}
	}
}

// TestPromptCodeEN_包含关键段落 验证 code/en 提示词包含所有关键段落
func TestPromptCodeEN_包含关键段落(t *testing.T) {
	segments := []string{
		"technical experience extraction assistant",
		"Worth Extracting",
		"Strictly Forbidden",
		"Self-check Three Questions",
		"Existing Knowledge Base",
		"Conversation Log",
		"JSON array",
		"title",
		"content",
	}
	for _, seg := range segments {
		if !strings.Contains(promptCodeEN, seg) {
			t.Errorf("promptCodeEN 缺少关键段落: %q", seg)
		}
	}
}

// TestPromptAgentEN_包含关键段落 验证 agent/en 提示词包含所有关键段落
func TestPromptAgentEN_包含关键段落(t *testing.T) {
	segments := []string{
		"memory organization assistant",
		"Extraction Criteria",
		"Existing Memories",
		"Conversation Log",
		"JSON array",
		"title",
		"content",
		"self-contained",
	}
	for _, seg := range segments {
		if !strings.Contains(promptAgentEN, seg) {
			t.Errorf("promptAgentEN 缺少关键段落: %q", seg)
		}
	}
}

// TestPromptTemplate_格式化占位符 验证提示词模板可以通过 fmt.Sprintf 格式化
func TestPromptTemplate_格式化占位符(t *testing.T) {
	existing := "已有知识"
	compressed := "对话内容"
	maxItems := 5

	// 测试所有 4 个模板
	templates := []struct {
		name string
		tmpl string
	}{
		{"promptCodeZH", promptCodeZH},
		{"promptAgentZH", promptAgentZH},
		{"promptCodeEN", promptCodeEN},
		{"promptAgentEN", promptAgentEN},
	}

	for _, tt := range templates {
		result := fmt.Sprintf(tt.tmpl, existing, compressed, maxItems)
		if !strings.Contains(result, existing) {
			t.Errorf("%s: 格式化后缺少 existing_knowledge", tt.name)
		}
		if !strings.Contains(result, compressed) {
			t.Errorf("%s: 格式化后缺少 compressed_session", tt.name)
		}
		if !strings.Contains(result, "5") {
			t.Errorf("%s: 格式化后缺少 max_items", tt.name)
		}
	}
}

// TestPromptMap_条目完整 验证 promptMap 包含所有 4 个条目
func TestPromptMap_条目完整(t *testing.T) {
	expected := [][2]string{
		{"code", "zh"},
		{"code", "en"},
		{"agent", "zh"},
		{"agent", "en"},
	}
	for _, key := range expected {
		if _, ok := promptMap[key]; !ok {
			t.Errorf("promptMap 缺少 key: %v", key)
		}
	}
	if len(promptMap) != 4 {
		t.Errorf("promptMap 应有 4 个条目，实际 %d 个", len(promptMap))
	}
}

// TestSysMsgMap_条目完整 验证 sysMsgMap 包含所有 4 个条目
func TestSysMsgMap_条目完整(t *testing.T) {
	expected := [][2]string{
		{"code", "zh"},
		{"code", "en"},
		{"agent", "zh"},
		{"agent", "en"},
	}
	for _, key := range expected {
		if _, ok := sysMsgMap[key]; !ok {
			t.Errorf("sysMsgMap 缺少 key: %v", key)
		}
	}
	if len(sysMsgMap) != 4 {
		t.Errorf("sysMsgMap 应有 4 个条目，实际 %d 个", len(sysMsgMap))
	}
}

// TestPromptCodeZH_对齐Python 验证 code/zh 提示词与 Python _PROMPT_CODE 一致
func TestPromptCodeZH_对齐Python(t *testing.T) {
	// Python 中的关键特征字符串
	pythonSegments := []string{
		"绝大多数对话沉淀不了知识，输出 [] 是常态，不要凑数",
		"踩坑根因：症状 X，常规思路 W 为何不通，真正根因 Y，解法 Z",
		"被验证的非显而易见行为",
		`设计决策的"为什么"`,
		"项目隐含约定",
		"AI 助手自身运行机制",
		"通用编程常识",
		"对话流水账",
		"静态可查事实",
		"一次性任务状态",
		"一周后做类似任务",
		"后者一律丢弃",
		"能否写成",
		"注：仅含用户提问和助手最终回复，无工具调用细节",
	}
	for _, seg := range pythonSegments {
		if !strings.Contains(promptCodeZH, seg) {
			t.Errorf("promptCodeZH 与 Python 不一致，缺少: %q", seg)
		}
	}
}

// TestPromptAgentZH_对齐Python 验证 agent/zh 提示词与 Python _PROMPT_AGENT 一致
func TestPromptAgentZH_对齐Python(t *testing.T) {
	pythonSegments := []string{
		"记忆整理助手",
		"脱离原始对话语境后仍然可读、可用",
		"用户明确表达的偏好",
		"用户的个人背景信息",
		"用户纠正助手的地方",
		"反复出现的交互模式",
		"一次性的事务请求",
		"对话中的寒暄、确认、感谢",
		"宁缺毋滥",
	}
	for _, seg := range pythonSegments {
		if !strings.Contains(promptAgentZH, seg) {
			t.Errorf("promptAgentZH 与 Python 不一致，缺少: %q", seg)
		}
	}
}
