package stages

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill/skilldev"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ImproveStageHandler IMPROVE 阶段：Agent 根据用户反馈改进 Skill，随后进入下一轮测试。
type ImproveStageHandler struct{}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ImproveSystemPrompt IMPROVE 阶段 Agent 系统 Prompt。
const ImproveSystemPrompt = `你是一个 Skill 优化专家。根据用户反馈改进 Skill。

当前是第 {iteration} 轮迭代。

用户反馈：
{feedback}

评测报告：
{report}

当前 Skill 内容：
{skill_content}

## 改进哲学（对齐官方 skill-creator 指导）

### 1. 从反馈中泛化，不要过拟合
你在极少数示例上迭代，但 Skill 需要在海量不同场景中表现良好。
不要为特定测试用例添加琐碎的过拟合修改或限制性的 MUST 规则。
尝试理解用户反馈背后的 *根本意图*，将理解注入到指令中。

### 2. 保持精简，删除无效内容
阅读测试的 transcripts（不仅是最终输出）——如果 Skill 让模型在不产出价值的步骤上
浪费大量时间，删除引起这些行为的 Skill 指令并观察效果。

### 3. 解释 why，用心智模型替代死板规则
当今的 LLM 足够智能。与其写 "ALWAYS do X" 或 "NEVER do Y"，
不如解释 *为什么* X 重要、为什么 Y 会导致问题。
让模型理解意图后自主决策，比死板规则更有效、更优雅。

### 4. 发现重复工作 → 捆绑脚本
阅读测试运行的 transcripts，如果所有测试用例都独立编写了类似的辅助脚本
（如 create_docx.py、build_chart.py），这是强烈信号：
应将该脚本写好放入 scripts/，让每次调用直接使用而非重新发明。

### 5. 关注 Benchmark 异常模式
- 某 assertion 在所有配置都 pass → 可能不具区分力，考虑加强或替换
- 某 assertion 在所有配置都 fail → 可能超出能力范围或 assertion 本身有问题
- 高方差 eval → 可能是 flaky 测试或非确定性行为
- with_skill 反而劣于 baseline 的指标 → Skill 可能在某方面产生负面影响

### 6. 先写草稿，再以新鲜眼光审视
写完改进后，以全新视角审视一遍。如果某个持续性问题用当前方法解决不了，
尝试换一种思路——不同的隐喻、不同的工作模式、不同的文件组织方式。
尝试成本低，或许能找到突破口。

请输出改进后的完整文件内容。
`

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// Execute 执行 IMPROVE 阶段逻辑。
func (h *ImproveStageHandler) Execute(ctx context.Context, sctx *skilldev.SkillDevContext) (*skilldev.StageResult, error) {
	if len(sctx.State.FeedbackHistory) == 0 {
		return nil, fmt.Errorf("IMPROVE 阶段缺少反馈历史，请先完成 REVIEW 阶段")
	}

	latestFeedback := make(map[string]any)
	lastEntry := sctx.State.FeedbackHistory[len(sctx.State.FeedbackHistory)-1]
	if fb, ok := lastEntry["feedback"]; ok {
		if m, ok := fb.(map[string]any); ok {
			latestFeedback = m
		}
	}

	report := ""
	if sctx.State.EvalResults != nil {
		if r, ok := sctx.State.EvalResults["report"].(string); ok {
			report = r
		}
	}

	sctx.Emit(skilldev.SkillDevEventTypeProgress, map[string]any{
		"message": fmt.Sprintf("正在根据反馈进行第 %d 轮改进...", sctx.State.Iteration+1),
	})

	if err := h.runImproveAgent(ctx, sctx, latestFeedback, report); err != nil {
		return nil, err
	}

	sctx.State.Iteration++
	sctx.Emit(skilldev.SkillDevEventTypeProgress, map[string]any{
		"message": fmt.Sprintf("改进完成，开始第 %d 轮测试", sctx.State.Iteration),
	})
	return &skilldev.StageResult{NextStage: skilldev.SkillDevStageTestRun}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// runImproveAgent 调用阶段 Agent 根据反馈改写 SKILL.md。
//
// 对齐 Python: ImproveStageHandler._run_improve_agent(ctx, feedback, report)
// 上游该方法仍是占位（直接跳过改进），这里按注释伪代码接入 Agent：
// 拼接 ImproveSystemPrompt（iteration/feedback/report/skill_content）→ 调模型 →
// 剥离围栏 → 保证 frontmatter → 覆盖写回 skill/SKILL.md。
func (h *ImproveStageHandler) runImproveAgent(
	ctx context.Context,
	sctx *skilldev.SkillDevContext,
	feedback map[string]any,
	report string,
) error {
	skillDir := filepath.Join(sctx.Workspace, "skill")
	existing := readSkillFiles(skillDir)

	feedbackJSON, _ := json.Marshal(feedback)
	systemPrompt := ImproveSystemPrompt
	systemPrompt = strings.ReplaceAll(systemPrompt, "{iteration}", strconv.Itoa(sctx.State.Iteration+1))
	systemPrompt = strings.ReplaceAll(systemPrompt, "{feedback}", string(feedbackJSON))
	systemPrompt = strings.ReplaceAll(systemPrompt, "{report}", report)
	systemPrompt = strings.ReplaceAll(systemPrompt, "{skill_content}", existing)

	text, err := sctx.RunStageAgent(
		ctx,
		"improve",
		systemPrompt,
		[]string{"file_read", "file_write"},
		25,
		"根据反馈改进当前技能，输出改进后的完整 SKILL.md 内容（含 frontmatter）。",
	)
	if err != nil {
		return err
	}

	content := skilldev.StripCodeFences(text)
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("IMPROVE 阶段模型未输出改进内容")
	}
	content = ensureImprovedSkillMD(existing, content)

	skillMDPath := filepath.Join(skillDir, "SKILL.md")
	if writeErr := os.WriteFile(skillMDPath, []byte(content), 0o644); writeErr != nil {
		return fmt.Errorf("写入改进后的 SKILL.md 失败: %w", writeErr)
	}

	sctx.Emit(skilldev.SkillDevEventTypeProgress, map[string]any{
		"message":   fmt.Sprintf("第 %d 轮改进已写回 SKILL.md", sctx.State.Iteration+1),
		"iteration": sctx.State.Iteration + 1,
	})
	return nil
}

// ensureImprovedSkillMD 保证改进结果带 frontmatter。
//
// 模型只输出正文时，沿用原 SKILL.md 的 frontmatter，避免校验失败又把流程打回 GENERATE。
func ensureImprovedSkillMD(existing, improved string) string {
	if strings.HasPrefix(strings.TrimSpace(improved), "---") {
		return improved
	}
	fm := extractFrontmatterBlock(existing)
	if fm == "" {
		return improved
	}
	return fm + "\n" + improved
}

// extractFrontmatterBlock 提取 SKILL.md 的 frontmatter 原文（含首尾 ---）。
func extractFrontmatterBlock(content string) string {
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, "---") {
		return ""
	}
	rest := trimmed[3:]
	if idx := strings.Index(rest, "\n---"); idx >= 0 {
		return "---" + rest[:idx+4]
	}
	return ""
}

// readSkillFiles 读取当前 skill 目录下所有文件内容。
func readSkillFiles(skillDir string) string {
	var parts []string
	entries, err := walkFilesSorted(skillDir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		data, err := os.ReadFile(entry)
		if err != nil {
			continue
		}
		rel, _ := filepath.Rel(skillDir, entry)
		parts = append(parts, fmt.Sprintf("=== %s ===\n%s", rel, string(data)))
	}
	return strings.Join(parts, "\n\n")
}

// walkFilesSorted 递归获取目录下所有文件路径（排序）。
func walkFilesSorted(dir string) ([]string, error) {
	var files []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}
