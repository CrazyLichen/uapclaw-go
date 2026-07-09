package stages

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"regexp"
	"sort"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill/skilldev"
)

// ──────────────────────────── 结构体 ────────────────────────────

// optimizationLoopInput 描述优化循环的输入参数封装。
type optimizationLoopInput struct {
	// SkillName 技能名称
	SkillName string
	// SkillBody SKILL.md 正文内容
	SkillBody string
	// CurrentDesc 当前描述
	CurrentDesc string
	// TrainSet 训练集
	TrainSet []skilldev.TriggerEvalQuery
	// TestSet 测试集
	TestSet []skilldev.TriggerEvalQuery
}

// improveDescriptionInput 描述改进步骤的输入参数封装。
type improveDescriptionInput struct {
	// SkillName 技能名称
	SkillName string
	// SkillBody SKILL.md 正文内容
	SkillBody string
	// CurrentDesc 当前描述
	CurrentDesc string
	// TrainResults 训练集评估结果
	TrainResults []map[string]any
	// History 历史迭代
	History []skilldev.DescOptimizeIteration
}

// DescOptimizeStageHandler DESC_OPTIMIZE 阶段：优化 SKILL.md 的 description 以提高触发准确率。
type DescOptimizeStageHandler struct{}

// ──────────────────────────── 常量 ────────────────────────────

// MaxIterations 描述优化最大迭代次数。
const MaxIterations = 5

// HoldoutRatio 测试集保留比例。
const HoldoutRatio = 0.4

// TriggerQueryGenPrompt 触发查询生成 Prompt。
const TriggerQueryGenPrompt = `你是一个 Skill 触发优化专家。根据以下 Skill 的名称和描述，生成 20 个测试查询。

Skill 名称: {skill_name}
当前 Description: {description}

## 要求

### should_trigger=true 的查询（约 10 个）
- 用户确实需要这个 Skill 时会说的话
- 不同表达风格（正式/随意/简短/详细）
- 有些不直接提及 Skill 名称但确实需要其功能
- 包含具体细节（文件路径、个人背景、数据名称等）

### should_trigger=false 的查询（约 10 个）
- 关键词相近但实际不需要这个 Skill 的 **近似场景**
- 相邻领域、歧义措辞、看似相关但应由其他工具处理
- 不要用明显无关的查询（"写斐波那契函数"对 PDF 技能来说太容易区分了）

输出 JSON 数组：
[{"query": "具体的用户查询", "should_trigger": true}, ...]
`

// ImproveDescPrompt 描述改进 Prompt。
const ImproveDescPrompt = `你正在优化一个名为 "{skill_name}" 的 Skill 的 description 字段。
description 出现在模型的 available_skills 列表中，模型仅凭 description 决定是否使用该 Skill。

当前 description：
"{current_description}"

当前得分：{scores_summary}

{failure_details}

{history_section}

## 要求

根据失败案例，写一个更好的 description：
- 从失败中 **泛化**，不要过拟合到具体查询
- 用祈使句（"Use when..." 而非 "This skill does..."）
- 聚焦用户意图而非实现细节
- 让触发场景具体且可区分
- 严格不超过 {max_len} 字符

请在 <new_description> 标签中只输出新的 description 文本：
<new_description>新描述内容</new_description>
`

// ──────────────────────────── 导出函数 ────────────────────────────

// Execute 执行 DESC_OPTIMIZE 阶段逻辑。
func (h *DescOptimizeStageHandler) Execute(_ context.Context, sctx *skilldev.SkillDevContext) (*skilldev.StageResult, error) {
	skillDir := filepathJoin(sctx.Workspace, "skill")
	skillMD := filepathJoin(skillDir, "SKILL.md")

	if _, err := os.Stat(skillMD); os.IsNotExist(err) {
		sctx.Emit(skilldev.SkillDevEventTypeProgress, map[string]any{"message": "未找到 SKILL.md，跳过描述优化"})
		return &skilldev.StageResult{NextStage: skilldev.SkillDevStageCompleted}, nil
	}

	skillName, currentDesc, body := ParseSkillFrontmatter(skillMD)

	// 步骤 1：生成触发测试查询
	sctx.Emit(skilldev.SkillDevEventTypeProgress, map[string]any{"message": "正在生成触发测试查询集..."})
	queries := h.generateTriggerQueries(sctx, skillName, currentDesc)

	// 步骤 2：训练/测试集切分
	trainSet, testSet := SplitEvalSet(queries, HoldoutRatio, 42)

	sctx.Emit(skilldev.SkillDevEventTypeProgress, map[string]any{
		"message": fmt.Sprintf("开始描述优化循环（train=%d, test=%d）", len(trainSet), len(testSet)),
	})

	// 步骤 3：优化循环
	loopInput := optimizationLoopInput{
		SkillName:   skillName,
		SkillBody:   body,
		CurrentDesc: currentDesc,
		TrainSet:    trainSet,
		TestSet:     testSet,
	}
	bestDesc, history := h.optimizationLoop(sctx, loopInput)

	// 步骤 4：写回 SKILL.md
	if bestDesc != "" && bestDesc != currentDesc {
		applyDescription(skillMD, currentDesc, bestDesc)
	}

	// 步骤 5：结果
	result := buildDescOptResult(currentDesc, bestDesc, history, testSet)
	sctx.State.DescOptimizeResult = result

	sctx.Emit(skilldev.SkillDevEventTypeDescOptReady, result)
	return &skilldev.StageResult{NextStage: skilldev.SkillDevStageCompleted}, nil
}

// SplitEvalSet 按 should_trigger 分层切分 train/test。
func SplitEvalSet(queries []skilldev.TriggerEvalQuery, holdout float64, seed int64) ([]skilldev.TriggerEvalQuery, []skilldev.TriggerEvalQuery) {
	rng := rand.New(rand.NewSource(seed))

	var trigger, noTrigger []skilldev.TriggerEvalQuery
	for _, q := range queries {
		if q.ShouldTrigger {
			trigger = append(trigger, q)
		} else {
			noTrigger = append(noTrigger, q)
		}
	}
	rng.Shuffle(len(trigger), func(i, j int) { trigger[i], trigger[j] = trigger[j], trigger[i] })
	rng.Shuffle(len(noTrigger), func(i, j int) { noTrigger[i], noTrigger[j] = noTrigger[j], noTrigger[i] })

	nT := max(1, int(float64(len(trigger))*holdout))
	nNT := max(1, int(float64(len(noTrigger))*holdout))

	test := make([]skilldev.TriggerEvalQuery, 0)
	test = append(test, trigger[:min(nT, len(trigger))]...)
	test = append(test, noTrigger[:min(nNT, len(noTrigger))]...)

	train := make([]skilldev.TriggerEvalQuery, 0)
	train = append(train, trigger[min(nT, len(trigger)):]...)
	train = append(train, noTrigger[min(nNT, len(noTrigger)):]...)

	return train, test
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// filepathJoin 拼接文件路径。
func filepathJoin(parts ...string) string {
	s := parts[0]
	for _, p := range parts[1:] {
		s = s + "/" + p
	}
	return s
}

// generateTriggerQueries 调用 Agent 生成 ~20 个触发测试查询。
//
// 待实现: 接入 create_stage_agent
func (h *DescOptimizeStageHandler) generateTriggerQueries(_ *skilldev.SkillDevContext, skillName, _ string) []skilldev.TriggerEvalQuery {
	// 待实现:
	// agent, err := sctx.CreateStageAgent("desc_opt_gen", prompt, ...)
	// output := await agent.run(...)
	// parsed := json.loads(output)
	// return parsed

	logger.Warn(logComponent).Msg("[DescOptimize] generateTriggerQueries 待接入 Agent")
	return []skilldev.TriggerEvalQuery{
		{Query: fmt.Sprintf("帮我用 %s 完成一个任务", skillName), ShouldTrigger: true},
		{Query: "帮我写一个排序算法", ShouldTrigger: false},
	}
}

// optimizationLoop 运行 eval → improve 循环，返回 (bestDescription, history)。
func (h *DescOptimizeStageHandler) optimizationLoop(sctx *skilldev.SkillDevContext, input optimizationLoopInput) (string, []skilldev.DescOptimizeIteration) {
	currentDesc := input.CurrentDesc
	trainSet := input.TrainSet
	testSet := input.TestSet
	history := make([]skilldev.DescOptimizeIteration, 0)

	for i := 1; i <= MaxIterations; i++ {
		sctx.Emit(skilldev.SkillDevEventTypeProgress, map[string]any{
			"message": fmt.Sprintf("描述优化第 %d/%d 轮...", i, MaxIterations),
		})

		// 评估 train + test
		trainResults := h.evalDescription(sctx, currentDesc, trainSet)
		var testResults []map[string]any
		if len(testSet) > 0 {
			testResults = h.evalDescription(sctx, currentDesc, testSet)
		}

		trainPassed := countPassed(trainResults)
		iteration := skilldev.DescOptimizeIteration{
			Iteration:   i,
			Description: currentDesc,
			TrainPassed: trainPassed,
			TrainTotal:  len(trainSet),
		}
		if testResults != nil {
			tp := countPassed(testResults)
			tt := len(testSet)
			iteration.TestPassed = &tp
			iteration.TestTotal = &tt
		}
		history = append(history, iteration)

		// 全部通过则提前退出
		if trainPassed == len(trainSet) {
			break
		}

		// 最后一轮不再改进
		if i == MaxIterations {
			break
		}

		// 改进 description
		improveInput := improveDescriptionInput{
			SkillName:    input.SkillName,
			SkillBody:    input.SkillBody,
			CurrentDesc:  currentDesc,
			TrainResults: trainResults,
			History:      history,
		}
		currentDesc = h.improveDescription(sctx, improveInput)
	}

	// 选 test score 最高的（防过拟合）
	if len(testSet) > 0 && len(history) > 0 {
		best := history[0]
		for _, hi := range history {
			if hi.TestPassed != nil && (best.TestPassed == nil || *hi.TestPassed > *best.TestPassed) {
				best = hi
			}
		}
		return best.Description, history
	}
	if len(history) > 0 {
		best := history[0]
		for _, hi := range history {
			if hi.TrainPassed > best.TrainPassed {
				best = hi
			}
		}
		return best.Description, history
	}
	return currentDesc, history
}

// evalDescription 对每个 query，调用模型判断当前 description 是否会触发。
//
// 待实现: 接入 create_stage_agent 实际评估
func (h *DescOptimizeStageHandler) evalDescription(_ *skilldev.SkillDevContext, _ string, queries []skilldev.TriggerEvalQuery) []map[string]any {
	// 待实现: 接入 create_stage_agent 实际评估
	logger.Warn(logComponent).Msg("[DescOptimize] evalDescription 待接入 Agent")
	results := make([]map[string]any, 0, len(queries))
	for _, q := range queries {
		results = append(results, map[string]any{
			"query":          q.Query,
			"should_trigger": q.ShouldTrigger,
			"triggered":      q.ShouldTrigger, // 占位：假设全部正确
			"pass":           true,
		})
	}
	return results
}

// improveDescription 调用模型基于失败案例改进 description。
//
// 待实现: 接入 create_stage_agent
func (h *DescOptimizeStageHandler) improveDescription(_ *skilldev.SkillDevContext, input improveDescriptionInput) string {
	// 待实现: 实际调用 Agent
	logger.Warn(logComponent).Msg("[DescOptimize] improveDescription 待接入 Agent")
	return input.CurrentDesc
}

// applyDescription 替换 SKILL.md frontmatter 中的 description 字段。
func applyDescription(skillMD, _, newDesc string) {
	data, err := os.ReadFile(skillMD)
	if err != nil {
		return
	}
	content := string(data)

	fmRe := regexp.MustCompile(`(?s)^(---\n)(.*?)(\n---)`)
	match := fmRe.FindStringSubmatch(content)
	if match == nil {
		return
	}

	frontmatter := match[2]
	// 替换 description 行（简单场景：单行 description: xxx）
	descRe := regexp.MustCompile(`(description:\s*).*`)
	newFM := descRe.ReplaceAllString(frontmatter, "${1}"+newDesc)

	newContent := match[1] + newFM + match[3] + content[len(match[0]):]
	_ = os.WriteFile(skillMD, []byte(newContent), 0o644)
}

// countPassed 统计评估结果中通过的个数。
func countPassed(results []map[string]any) int {
	count := 0
	for _, r := range results {
		if p, ok := r["pass"].(bool); ok && p {
			count++
		}
	}
	return count
}

// buildDescOptResult 构建描述优化结果。
func buildDescOptResult(originalDesc, bestDesc string, history []skilldev.DescOptimizeIteration, testSet []skilldev.TriggerEvalQuery) map[string]any {
	bestIter := findBestIteration(history, len(testSet) > 0)

	bestScore := "N/A"
	if bestIter != nil {
		if len(testSet) > 0 && bestIter.TestPassed != nil {
			bestScore = fmt.Sprintf("%d/%d", *bestIter.TestPassed, *bestIter.TestTotal)
		} else {
			bestScore = fmt.Sprintf("%d/%d", bestIter.TrainPassed, bestIter.TrainTotal)
		}
	}

	historyDicts := make([]map[string]any, 0, len(history))
	for _, h := range history {
		historyDicts = append(historyDicts, h.ToDict())
	}

	return map[string]any{
		"original_description": originalDesc,
		"best_description":     bestDesc,
		"best_score":           bestScore,
		"iterations_run":       len(history),
		"history":              historyDicts,
	}
}

// findBestIteration 找到最佳迭代。
func findBestIteration(history []skilldev.DescOptimizeIteration, hasTestSet bool) *skilldev.DescOptimizeIteration {
	if len(history) == 0 {
		return nil
	}
	best := &history[0]
	for i := range history {
		h := &history[i]
		if hasTestSet {
			if h.TestPassed != nil && (best.TestPassed == nil || *h.TestPassed > *best.TestPassed) {
				best = h
			}
		} else {
			if h.TrainPassed > best.TrainPassed {
				best = h
			}
		}
	}
	return best
}

// sortedStringKeys 返回 map 的已排序键列表。
func sortedStringKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
