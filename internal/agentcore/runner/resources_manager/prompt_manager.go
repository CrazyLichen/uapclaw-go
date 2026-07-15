package resources_manager

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/prompt"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// PromptMgr Prompt 资源管理器，使用 ThreadSafeDict 存储 PromptTemplate。
// 不继承 AbstractManager，因为 PromptTemplate 不需要 provider 延迟加载模式。
//
// 对应 Python: PromptMgr (openjiuwen/core/runner/resources_manager/prompt_manager.py)
type PromptMgr struct {
	// repo Prompt 模板存储
	repo *ThreadSafeDict[string, *prompt.PromptTemplate]
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPromptMgr 创建 Prompt 资源管理器。
func NewPromptMgr() *PromptMgr {
	return &PromptMgr{
		repo: NewThreadSafeDict[string, *prompt.PromptTemplate](),
	}
}

// AddPrompt 添加 Prompt 模板。
// 验证 templateID 和 template 非空后存入 repo。
//
// 对应 Python: PromptMgr.add_prompt(template_id, template)
func (m *PromptMgr) AddPrompt(templateID string, template *prompt.PromptTemplate) error {
	if templateID == "" {
		return exception.BuildError(exception.StatusResourceIDValueInvalid,
			exception.WithParam("resource_type", "prompt"),
			exception.WithParam("reason", "template id is empty"),
		)
	}
	if template == nil {
		return exception.BuildError(exception.StatusResourceValueInvalid,
			exception.WithParam("resource_type", "prompt"),
			exception.WithParam("reason", "prompt template is nil"),
		)
	}

	m.repo.Set(templateID, template)

	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "PROMPT_ADD_SUCCESS").
		Str("template_id", templateID).
		Msg("添加 Prompt 模板成功")
	return nil
}

// AddPrompts 批量添加 Prompt 模板。
//
// 对应 Python: PromptMgr.add_prompts(templates)
func (m *PromptMgr) AddPrompts(templates []PromptEntry) {
	for _, entry := range templates {
		if entry.ID == "" || entry.Template == nil {
			logger.Error(logger.ComponentAgentCore).
				Str("event_type", "PROMPT_ADD_ERROR").
				Str("template_id", entry.ID).
				Msg("批量添加 Prompt 跳过无效条目")
			continue
		}
		m.repo.Set(entry.ID, entry.Template)
		logger.Info(logger.ComponentAgentCore).
			Str("event_type", "PROMPT_ADD_SUCCESS").
			Str("template_id", entry.ID).
			Msg("批量添加 Prompt 模板成功")
	}
}

// RemovePrompt 移除 Prompt 模板，返回被移除的模板。
//
// 对应 Python: PromptMgr.remove_prompt(template_id)
func (m *PromptMgr) RemovePrompt(templateID string) (*prompt.PromptTemplate, error) {
	if templateID == "" {
		return nil, exception.BuildError(exception.StatusResourceIDValueInvalid,
			exception.WithParam("resource_type", "prompt"),
			exception.WithParam("reason", "template id is empty"),
		)
	}

	template := m.repo.Pop(templateID)
	if template == nil {
		return nil, exception.BuildError(exception.StatusResourceGetError,
			exception.WithParam("resource_id", templateID),
			exception.WithParam("resource_type", "prompt"),
			exception.WithParam("reason", fmt.Sprintf("prompt template %s not found", templateID)),
		)
	}

	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "PROMPT_REMOVE_SUCCESS").
		Str("template_id", templateID).
		Msg("移除 Prompt 模板成功")
	return template, nil
}

// GetPrompt 获取 Prompt 模板。
// 验证 templateID 非空后从 repo 读取。
//
// 对应 Python: PromptMgr.get_prompt(template_id)
func (m *PromptMgr) GetPrompt(templateID string) (*prompt.PromptTemplate, error) {
	if templateID == "" {
		return nil, exception.BuildError(exception.StatusResourceIDValueInvalid,
			exception.WithParam("resource_type", "prompt"),
			exception.WithParam("reason", "template id is empty"),
		)
	}

	template := m.repo.Get(templateID)
	if template == nil {
		return nil, exception.BuildError(exception.StatusResourceGetError,
			exception.WithParam("resource_id", templateID),
			exception.WithParam("resource_type", "prompt"),
			exception.WithParam("reason", fmt.Sprintf("prompt template %s not found", templateID)),
		)
	}

	return template, nil
}
