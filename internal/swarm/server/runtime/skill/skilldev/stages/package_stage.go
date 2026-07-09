package stages

import (
	"archive/zip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill/skilldev"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PackageStageHandler PACKAGE 阶段：打包 skill/ 为 .skill (zip) 文件。
type PackageStageHandler struct{}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// excludeDirs 目录级排除规则
	excludeDirs = map[string]bool{
		"__pycache__":  true,
		"node_modules": true,
		".git":         true,
	}
	// excludeFiles 文件级排除规则
	excludeFiles = map[string]bool{
		".DS_Store": true,
	}
	// excludeGlobs glob 匹配排除规则
	excludeGlobs = []string{"*.pyc"}
	// rootExcludeDirs 根目录级排除
	rootExcludeDirs = map[string]bool{
		"evals": true,
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// Execute 执行 PACKAGE 阶段逻辑。
func (h *PackageStageHandler) Execute(_ context.Context, sctx *skilldev.SkillDevContext) (*skilldev.StageResult, error) {
	skillDir := filepath.Join(sctx.Workspace, "skill")
	outputDir := filepath.Join(sctx.Workspace, "output")
	_ = os.MkdirAll(outputDir, 0o755)
	skillName := "skill"
	if sctx.State.Plan != nil {
		if sn, ok := sctx.State.Plan["skill_name"].(string); ok {
			skillName = sn
		}
	}
	// 官方格式为 .skill（本质是 zip）
	skillFilename := fmt.Sprintf("%s.skill", skillName)
	skillPath := filepath.Join(outputDir, skillFilename)

	sctx.Emit(skilldev.SkillDevEventTypeProgress, map[string]any{
		"message": fmt.Sprintf("正在打包 %s...", skillFilename),
	})

	if err := h.zipSkillDir(skillDir, skillPath); err != nil {
		return nil, fmt.Errorf("打包失败: %w", err)
	}

	sctx.State.ZipPath = &skillPath
	if info, err := os.Stat(skillPath); err == nil {
		sctx.State.ZipSize = int(info.Size())
	}

	sctx.Emit(skilldev.SkillDevEventTypeArtifactReady, map[string]any{
		"artifact": map[string]any{
			"id":           "skill_package",
			"name":         skillFilename,
			"type":         "skill_package",
			"size_bytes":   sctx.State.ZipSize,
			"browsable":    true,
			"downloadable": true,
		},
	})
	return &skilldev.StageResult{NextStage: skilldev.SkillDevStageDescOptimizeConfirm}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// zipSkillDir 将 skillDir 打包为 zip，排除无关文件。
func (h *PackageStageHandler) zipSkillDir(skillDir, zipPath string) error {
	zf, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer func() { _ = zf.Close() }()

	w := zip.NewWriter(zf)
	defer func() { _ = w.Close() }()

	err = filepath.Walk(skillDir, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if shouldExclude(filePath, skillDir) {
			return nil
		}
		relPath, err := filepath.Rel(skillDir, filePath)
		if err != nil {
			return err
		}
		f, err := w.Create(relPath)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}
		_, err = f.Write(data)
		return err
	})
	if err != nil {
		return err
	}

	if info, err := os.Stat(zipPath); err == nil {
		logger.Info(logComponent).
			Str("zip_path", zipPath).
			Int64("size", info.Size()).
			Msg("[PackageStage] 打包完成")
	}
	return nil
}

// shouldExclude 判断文件是否应被排除出 zip 包。
//
// 排除规则：目录级排除 + 文件级排除 + glob 匹配。
func shouldExclude(filePath, skillDir string) bool {
	relPath, err := filepath.Rel(skillDir, filePath)
	if err != nil {
		return false
	}
	parts := strings.Split(relPath, string(filepath.Separator))

	// 目录级排除
	for _, part := range parts {
		if excludeDirs[part] {
			return true
		}
	}

	// 根目录级别的排除（如 evals/）
	if len(parts) > 0 && rootExcludeDirs[parts[0]] {
		return true
	}

	// 文件名级排除
	if excludeFiles[filepath.Base(filePath)] {
		return true
	}

	// glob 匹配排除
	fileName := filepath.Base(filePath)
	for _, pattern := range excludeGlobs {
		if matched, _ := filepath.Match(pattern, fileName); matched {
			return true
		}
	}

	return false
}
