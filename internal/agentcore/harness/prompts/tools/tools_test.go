package tools

import (
	"strings"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestValidateToolMetadata_所有工具 校验所有已注册工具的双语元数据完整性
func TestValidateToolMetadata_所有工具(t *testing.T) {
	providers := AllProviders()
	if len(providers) == 0 {
		t.Fatal("AllProviders() 返回空，说明没有工具注册到全局注册表")
	}

	// 不需要严格校验的工具（空 description 或空参数 schema 是允许的）
	skipValidate := map[string]bool{
		"list_skill": true, // description 为空
		"skill_tool": true, // description 为空
	}

	for _, provider := range providers {
		name := provider.GetName()
		t.Run(name, func(t *testing.T) {
			if skipValidate[name] {
				t.Skipf("工具 %s 跳过 ValidateToolMetadata 校验", name)
			}
			if err := ValidateToolMetadata(provider); err != nil {
				t.Errorf("ValidateToolMetadata(%s) 失败: %v", name, err)
			}
		})
	}
}

// TestGetToolProvider_已注册工具 查找已注册的工具提供者
func TestGetToolProvider_已注册工具(t *testing.T) {
	names := []string{
		"bash", "code", "ask_user",
		"read_file", "write_file", "edit_file", "glob", "list_files", "grep",
		"audio_transcription", "audio_question_answering", "audio_metadata",
		"list_skill", "load_tools", "search_tools", "skill_tool",
		"switch_mode", "enter_plan_mode", "exit_plan_mode",
		"coding_memory_read", "coding_memory_write", "coding_memory_edit",
		"cron",
		"enter_worktree", "exit_worktree",
		"lsp",
		"list_mcp_resources", "read_mcp_resource",
		"memory_search", "memory_get", "write_memory", "edit_memory", "read_memory",
		"powershell",
		"sessions_list", "sessions_spawn", "sessions_cancel",
		"task_tool",
		"todo_create", "todo_list", "todo_modify", "todo_get",
		"video_understanding",
		"image_ocr", "visual_question_answering",
		"free_search", "paid_search", "fetch_webpage",
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			provider, ok := GetToolProvider(name)
			if !ok {
				t.Errorf("GetToolProvider(%q) 未找到", name)
				return
			}
			if provider.GetName() != name {
				t.Errorf("GetName() = %q, 期望 %q", provider.GetName(), name)
			}
		})
	}
}

// TestGetToolProvider_未注册工具 查找不存在的工具应返回 false
func TestGetToolProvider_未注册工具(t *testing.T) {
	_, ok := GetToolProvider("nonexistent_tool")
	if ok {
		t.Error("GetToolProvider(\"nonexistent_tool\") 不应找到")
	}
}

// TestAllProviders_非空 AllProviders 应返回非空列表
func TestAllProviders_非空(t *testing.T) {
	providers := AllProviders()
	if len(providers) == 0 {
		t.Error("AllProviders() 不应返回空列表")
	}
}

// TestToolMetadata_双语描述 所有工具的双语描述应非空
func TestToolMetadata_双语描述(t *testing.T) {
	providers := AllProviders()
	for _, provider := range providers {
		name := provider.GetName()
		t.Run(name, func(t *testing.T) {
			for _, lang := range []string{"cn", "en"} {
				desc := provider.GetDescription(lang)
				// list_skill 和 skill_tool 的 description 允许为空
				if name == "list_skill" || name == "skill_tool" {
					return
				}
				if strings.TrimSpace(desc) == "" {
					t.Errorf("GetDescription(%q) 返回空", lang)
				}
			}
		})
	}
}

// TestToolMetadata_参数Schema 结构校验
func TestToolMetadata_参数Schema(t *testing.T) {
	providers := AllProviders()
	for _, provider := range providers {
		name := provider.GetName()
		t.Run(name, func(t *testing.T) {
			for _, lang := range []string{"cn", "en"} {
				schema := provider.GetInputParams(lang)
				if schema["type"] != "object" {
					t.Errorf("GetInputParams(%q)[\"type\"] = %v, 期望 \"object\"", lang, schema["type"])
				}
				if _, ok := schema["properties"]; !ok {
					t.Errorf("GetInputParams(%q) 缺少 \"properties\" 键", lang)
				}
				if _, ok := schema["required"]; !ok {
					t.Errorf("GetInputParams(%q) 缺少 \"required\" 键", lang)
				}
			}
		})
	}
}

// TestToolMetadata_默认语言回退 非法语言应回退到 cn
func TestToolMetadata_默认语言回退(t *testing.T) {
	providers := AllProviders()
	for _, provider := range providers {
		name := provider.GetName()
		t.Run(name, func(t *testing.T) {
			descCn := provider.GetDescription("cn")
			descFallback := provider.GetDescription("xyz")
			if descCn != descFallback {
				t.Errorf("GetDescription(\"xyz\") 未回退到 cn: got %q, want %q", descFallback, descCn)
			}
			schemaCn := provider.GetInputParams("cn")
			schemaFallback := provider.GetInputParams("xyz")
			if len(schemaCn) != len(schemaFallback) {
				t.Errorf("GetInputParams(\"xyz\") 未回退到 cn: schema 长度不一致")
			}
		})
	}
}

// TestBashMetadataProvider bash 工具专项测试
func TestBashMetadataProvider(t *testing.T) {
	provider := &BashMetadataProvider{}
	if provider.GetName() != "bash" {
		t.Errorf("GetName() = %q, 期望 \"bash\"", provider.GetName())
	}
	descCn := provider.GetDescription("cn")
	if !strings.Contains(descCn, "Shell") {
		t.Error("中文描述应包含 'Shell'")
	}
	descEn := provider.GetDescription("en")
	if !strings.Contains(descEn, "bash") {
		t.Error("英文描述应包含 'bash'")
	}
	params := provider.GetInputParams("cn")
	props, _ := params["properties"].(map[string]any)
	if _, ok := props["command"]; !ok {
		t.Error("参数 schema 缺少 'command' 属性")
	}
	if _, ok := props["timeout"]; !ok {
		t.Error("参数 schema 缺少 'timeout' 属性")
	}
}

// TestFilesystemMetadataProvider 文件系统工具专项测试
func TestFilesystemMetadataProvider(t *testing.T) {
	t.Run("read_file", func(t *testing.T) {
		p := &ReadFileMetadataProvider{}
		if p.GetName() != "read_file" {
			t.Errorf("GetName() = %q, 期望 \"read_file\"", p.GetName())
		}
		params := p.GetInputParams("cn")
		props, _ := params["properties"].(map[string]any)
		if _, ok := props["file_path"]; !ok {
			t.Error("read_file schema 缺少 'file_path'")
		}
	})
	t.Run("edit_file", func(t *testing.T) {
		p := &EditFileMetadataProvider{}
		if p.GetName() != "edit_file" {
			t.Errorf("GetName() = %q, 期望 \"edit_file\"", p.GetName())
		}
		params := p.GetInputParams("cn")
		props, _ := params["properties"].(map[string]any)
		for _, key := range []string{"file_path", "old_string", "new_string"} {
			if _, ok := props[key]; !ok {
				t.Errorf("edit_file schema 缺少 '%s'", key)
			}
		}
	})
}

// TestAskUserMetadataProvider ask_user 工具专项测试
func TestAskUserMetadataProvider(t *testing.T) {
	p := &AskUserMetadataProvider{}
	if p.GetName() != "ask_user" {
		t.Errorf("GetName() = %q, 期望 \"ask_user\"", p.GetName())
	}
	params := p.GetInputParams("cn")
	props, _ := params["properties"].(map[string]any)
	questions, _ := props["questions"].(map[string]any)
	if questions == nil {
		t.Error("ask_user schema 缺少 'questions' 属性")
	}
	if questions["type"] != "array" {
		t.Error("questions 属性 type 应为 'array'")
	}
}

// TestMemoryMetadataProvider 记忆工具专项测试
func TestMemoryMetadataProvider(t *testing.T) {
	t.Run("memory_search", func(t *testing.T) {
		p := &MemorySearchMetadataProvider{}
		if p.GetName() != "memory_search" {
			t.Errorf("GetName() = %q", p.GetName())
		}
		params := p.GetInputParams("en")
		props, _ := params["properties"].(map[string]any)
		if _, ok := props["query"]; !ok {
			t.Error("memory_search schema 缺少 'query'")
		}
	})
	t.Run("write_memory", func(t *testing.T) {
		p := &WriteMemoryMetadataProvider{}
		if p.GetName() != "write_memory" {
			t.Errorf("GetName() = %q", p.GetName())
		}
		params := p.GetInputParams("cn")
		props, _ := params["properties"].(map[string]any)
		for _, key := range []string{"path", "content", "append"} {
			if _, ok := props[key]; !ok {
				t.Errorf("write_memory schema 缺少 '%s'", key)
			}
		}
	})
}

// TestTodoMetadataProvider todo 工具专项测试
func TestTodoMetadataProvider(t *testing.T) {
	t.Run("todo_create", func(t *testing.T) {
		p := &TodoCreateMetadataProvider{}
		if p.GetName() != "todo_create" {
			t.Errorf("GetName() = %q", p.GetName())
		}
		params := p.GetInputParams("cn")
		props, _ := params["properties"].(map[string]any)
		if _, ok := props["tasks"]; !ok {
			t.Error("todo_create schema 缺少 'tasks'")
		}
	})
	t.Run("todo_modify", func(t *testing.T) {
		p := &TodoModifyMetadataProvider{}
		if p.GetName() != "todo_modify" {
			t.Errorf("GetName() = %q", p.GetName())
		}
		params := p.GetInputParams("en")
		props, _ := params["properties"].(map[string]any)
		if _, ok := props["action"]; !ok {
			t.Error("todo_modify schema 缺少 'action'")
		}
	})
}

// TestWebToolsMetadataProvider 联网搜索工具专项测试
func TestWebToolsMetadataProvider(t *testing.T) {
	t.Run("free_search", func(t *testing.T) {
		p := &FreeSearchMetadataProvider{}
		if p.GetName() != "free_search" {
			t.Errorf("GetName() = %q", p.GetName())
		}
	})
	t.Run("paid_search", func(t *testing.T) {
		p := &PaidSearchMetadataProvider{}
		if p.GetName() != "paid_search" {
			t.Errorf("GetName() = %q", p.GetName())
		}
		params := p.GetInputParams("cn")
		props, _ := params["properties"].(map[string]any)
		if _, ok := props["provider"]; !ok {
			t.Error("paid_search schema 缺少 'provider'")
		}
	})
	t.Run("fetch_webpage", func(t *testing.T) {
		p := &FetchWebpageMetadataProvider{}
		if p.GetName() != "fetch_webpage" {
			t.Errorf("GetName() = %q", p.GetName())
		}
		params := p.GetInputParams("en")
		props, _ := params["properties"].(map[string]any)
		if _, ok := props["url"]; !ok {
			t.Error("fetch_webpage schema 缺少 'url'")
		}
	})
}

// TestValidateToolMetadata_结构不一致 校验 cn/en schema 不一致时报错
func TestValidateToolMetadata_结构不一致(t *testing.T) {
	// 构造一个 cn/en properties key 不一致的 provider
	provider := &testBadProvider{}
	err := ValidateToolMetadata(provider)
	if err == nil {
		t.Error("期望 ValidateToolMetadata 报错，但返回 nil")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// testBadProvider 用于测试 ValidateToolMetadata 的错误路径
type testBadProvider struct{}

func (p *testBadProvider) GetName() string                       { return "test_bad" }
func (p *testBadProvider) GetDescription(language string) string { return "test" }
func (p *testBadProvider) GetInputParams(language string) map[string]any {
	if language == "cn" {
		return map[string]any{
			"type": "object",
			"properties": map[string]any{
				"foo": map[string]any{"type": "string", "description": "测试"},
			},
			"required": []any{"foo"},
		}
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"bar": map[string]any{"type": "string", "description": "test"},
		},
		"required": []any{"bar"},
	}
}
