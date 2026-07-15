package stages

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill/skilldev"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InitStageHandler INIT 阶段：解析请求参数，准备工作区。
type InitStageHandler struct{}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// Execute 执行 INIT 阶段逻辑。
func (h *InitStageHandler) Execute(_ context.Context, sctx *skilldev.SkillDevContext) (*skilldev.StageResult, error) {
	sctx.Emit(skilldev.SkillDevEventTypeProgress, map[string]any{"message": "正在初始化工作区..."})

	// 判断任务模式
	sctx.State.Mode = skilldev.DetermineTaskMode(sctx.State.Input)
	logger.Info(logComponent).
		Str("task_id", sctx.TaskID).
		Str("mode", string(sctx.State.Mode)).
		Msg("[InitStage] 任务模式判断完成")

	// 工作区已由 Pipeline 的 ensureLocal 创建，此处直接使用
	resourcesDir := filepath.Join(sctx.Workspace, "resources")
	skillDir := filepath.Join(sctx.Workspace, "skill")

	// 解析上传的资源文件
	resources, _ := sctx.State.Input["resources"].([]any)
	if len(resources) > 0 {
		sctx.Emit(skilldev.SkillDevEventTypeProgress, map[string]any{
			"message": fmt.Sprintf("正在解析 %d 个资源文件...", len(resources)),
		})
		texts := h.extractResources(resources, resourcesDir)
		sctx.State.ReferenceTexts = texts
	}

	// 解析已有 skill 包（修改/升级场景）
	if _, ok := sctx.State.Input["existing_skill"]; ok {
		existingSkill, _ := sctx.State.Input["existing_skill"].(map[string]any)
		if existingSkill != nil {
			sctx.Emit(skilldev.SkillDevEventTypeProgress, map[string]any{"message": "正在解析已有 Skill 包..."})
			skillMD := h.extractExistingSkill(existingSkill, skillDir)
			sctx.State.ExistingSkillMD = skillMD
		}
	}

	sctx.Emit(skilldev.SkillDevEventTypeProgress, map[string]any{"message": "初始化完成，准备生成开发计划"})
	return &skilldev.StageResult{NextStage: skilldev.SkillDevStagePlan}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// extractResources 解析资源文件列表，提取纯文本内容。
//
// 支持格式：.zip（解压）/ .docx / .pdf / .txt / .md
// 待实现: 实现各格式的文本提取逻辑
func (h *InitStageHandler) extractResources(resources []any, destDir string) []string {
	texts := make([]string, 0)
	for _, res := range resources {
		resMap, ok := res.(map[string]any)
		if !ok {
			continue
		}
		name, _ := resMap["name"].(string)
		if name == "" {
			name = "unknown"
		}
		contentB64, _ := resMap["content_base64"].(string)
		raw, err := base64.StdEncoding.DecodeString(contentB64)
		if err != nil {
			logger.Warn(logComponent).
				Str("name", name).
				Err(err).
				Msg("[InitStage] 资源文件 base64 解码失败")
			continue
		}
		filePath := filepath.Join(destDir, name)
		if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
			logger.Warn(logComponent).
				Str("name", name).
				Err(err).
				Msg("[InitStage] 创建资源目录失败")
			continue
		}
		if err := os.WriteFile(filePath, raw, 0o644); err != nil {
			logger.Warn(logComponent).
				Str("name", name).
				Err(err).
				Msg("[InitStage] 写入资源文件失败")
			continue
		}
		text := parseFileToText(filePath)
		if text != "" {
			texts = append(texts, text)
		}
	}
	return texts
}

// extractExistingSkill 解压已有 skill.zip，提取 SKILL.md 内容。
//
// 待实现: 实现 zip 解压逻辑
func (h *InitStageHandler) extractExistingSkill(_ map[string]any, _ string) *string {
	// 待实现:
	// 待实现：解码已有技能内容 raw, err := base64.StdEncoding.DecodeString(existingSkill["content_base64"].(string))
	// ...
	// 用 archive/zip 解压到 destDir
	// 读取 SKILL.md 内容
	logger.Warn(logComponent).Msg("[InitStage] extractExistingSkill 尚未实现")
	return nil
}

// parseFileToText 将文件解析为纯文本。
//
// 待实现: 实现各格式的解析逻辑：
//   - .docx → docx 解析
//   - .pdf  → pdf 解析
//   - .txt / .md → 直接读取
//   - .zip  → 解压后递归处理
func parseFileToText(filePath string) string {
	suffix := strings.ToLower(filepath.Ext(filePath))
	if suffix == ".txt" || suffix == ".md" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return ""
		}
		return string(data)
	}
	// 待实现: 其他格式
	logger.Warn(logComponent).
		Str("suffix", suffix).
		Msg("[InitStage] 暂不支持的文件格式")
	return ""
}
