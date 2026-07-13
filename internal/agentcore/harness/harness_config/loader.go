package harness_config

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"text/template"

	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 结构体 ────────────────────────────

type ResolvedSection struct {
	// Name 段名称
	Name string
	// Priority 优先级
	Priority int
	// Content 按语言索引的内容 {lang: text}
	Content map[string]string
}

type ResolvedFileSection struct {
	// Filename 文件名，如 "AGENT.md"
	Filename string
	// Content 按语言索引的内容 {language: text}
	Content map[string]string
}

type ResolvedHarnessConfig struct {
	// Config 解析后的 HarnessConfig（已校验）
	Config *HarnessConfig
	// SystemPrompt identity 段的内容，映射到 DeepAgentConfig.SystemPrompt
	SystemPrompt *string
	// ExtraSections 非 identity 内联段 → builder.add_section()
	ExtraSections []ResolvedSection
	// FileSections 文件型段 → 由 HarnessConfigBuilder 写入工作空间
	FileSections []ResolvedFileSection
	// SourcePath harness_config.yaml 文件的绝对路径
	SourcePath string
}

type HarnessConfigLoader struct{}

// ──────────────────────────── 全局变量 ────────────────────────────

var varPlaceholderRegexp = regexp.MustCompile(`\{\{\s*(\w+)\s*\}\}`)

// ──────────────────────────── 导出函数 ────────────────────────────

func (HarnessConfigLoader) Load(path string, params map[string]any, workspaceRoot ...string) (*ResolvedHarnessConfig, error) {
	absPath, err := absPath(path)
	if err != nil {
		return nil, fmt.Errorf("获取配置文件绝对路径失败: %w", err)
	}

	raw, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("读取 harness_config 文件失败: %w", err)
	}

	var cfg HarnessConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("解析 harness_config YAML 失败: %w", err)
	}

	setDefaults(&cfg)

	if err := ValidateHarnessConfig(&cfg); err != nil {
		return nil, fmt.Errorf("harness_config 校验失败: %w", err)
	}

	// 构建有效参数
	effectiveParams := make(map[string]any)
	for k, v := range params {
		effectiveParams[k] = v
	}
	if _, ok := effectiveParams["workspace_root"]; !ok {
		if len(workspaceRoot) > 0 && workspaceRoot[0] != "" {
			effectiveParams["workspace_root"] = workspaceRoot[0]
		} else {
			effectiveParams["workspace_root"] = dirOf(absPath)
		}
	}

	language := cfg.Language
	var systemPrompt *string
	var extraSections []ResolvedSection
	var fileSections []ResolvedFileSection

	if cfg.Prompts != nil {
		for _, sec := range cfg.Prompts.Sections {
			rawContent := normalizeContent(sec.Content)

			// 对每种语言渲染模板
			rendered := make(map[string]string, len(rawContent))
			for lang, text := range rawContent {
				renderedText, renderErr := renderTemplate(text, effectiveParams)
				if renderErr != nil {
					return nil, fmt.Errorf("渲染模板失败（段 %s，语言 %s）: %w", sec.Name, lang, renderErr)
				}
				rendered[lang] = renderedText
			}

			if sec.File != nil {
				// 文件型段 → 写入工作空间
				fileSections = append(fileSections, ResolvedFileSection{
					Filename: *sec.File,
					Content:  rendered,
				})
			} else if sec.Name == "identity" {
				// identity 段 → DeepAgentConfig.SystemPrompt
				if text, ok := rendered[language]; ok && text != "" {
					systemPrompt = &text
				} else if text, ok := rendered["cn"]; ok && text != "" {
					systemPrompt = &text
				} else if text, ok := rendered["en"]; ok && text != "" {
					systemPrompt = &text
				}
			} else {
				// 自定义内联段 → add_section()
				priority := DefaultSectionPriority
				if sec.Priority != nil {
					priority = *sec.Priority
				}
				extraSections = append(extraSections, ResolvedSection{
					Name:     sec.Name,
					Priority: priority,
					Content:  rendered,
				})
			}
		}
	}

	return &ResolvedHarnessConfig{
		Config:        &cfg,
		SystemPrompt:  systemPrompt,
		ExtraSections: extraSections,
		FileSections:  fileSections,
		SourcePath:    absPath,
	}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func normalizeContent(content any) map[string]string {
	if content == nil {
		return map[string]string{}
	}
	switch v := content.(type) {
	case string:
		return map[string]string{"cn": v, "en": v}
	case map[string]string:
		result := make(map[string]string, len(v))
		for k, val := range v {
			result[k] = val
		}
		return result
	case map[string]any:
		result := make(map[string]string, len(v))
		for k, val := range v {
			if s, ok := val.(string); ok {
				result[k] = s
			}
		}
		return result
	default:
		return map[string]string{}
	}
}

func renderTemplate(text string, params map[string]any) (string, error) {
	if text == "" || !varPlaceholderRegexp.MatchString(text) {
		return text, nil
	}

	// 将 {{ var }} 转为 {{ .Var }} 格式
	converted := varPlaceholderRegexp.ReplaceAllString(text, `{{ .${1} }}`)

	// 准备模板数据：首字母大写以匹配 .Var 访问
	templateData := make(map[string]any, len(params))
	for k, v := range params {
		templateData[capitalize(k)] = v
	}

	tmpl, err := template.New("harness").Option("missingkey=error").Parse(converted)
	if err != nil {
		return "", fmt.Errorf("解析模板失败: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, templateData); err != nil {
		// 模板执行失败时回退到简单替换
		return simpleSubstitute(text, params), nil
	}

	return buf.String(), nil
}

func simpleSubstitute(text string, params map[string]any) string {
	return varPlaceholderRegexp.ReplaceAllStringFunc(text, func(match string) string {
		submatch := varPlaceholderRegexp.FindStringSubmatch(match)
		if len(submatch) < 2 {
			return match
		}
		key := submatch[1]
		if val, ok := params[key]; ok {
			return fmt.Sprintf("%v", val)
		}
		return match
	})
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	c := s[0]
	if c >= 'a' && c <= 'z' {
		c -= 32
	}
	return string(c) + s[1:]
}

func absPath(path string) (string, error) {
	if len(path) > 0 && path[0] == '/' {
		return path, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return wd + "/" + path, nil
}

func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[:i]
		}
	}
	return "."
}
