package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	tool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	ptools "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	sections "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	skillpkg "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/skills"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ListSkillTool 列出所有可用技能或通过 LLM 路由筛选相关技能。
//
// 通过 getSkills 回调获取当前已启用技能列表。
// 当 query 为空时返回所有技能；当 query 非空且 listSkillModel 可用时，
// 使用 LLM 路由筛选与任务相关的技能；当 listSkillModel 不可用时回退返回全部技能。
//
// 对应 Python: ListSkillTool (openjiuwen/harness/tools/skills/list_skill.py)
type ListSkillTool struct {
	// card 工具配置卡片
	card *tool.ToolCard
	// getSkills 返回当前已启用技能列表的回调函数
	getSkills func() []*skillpkg.Skill
	// listSkillModel 用于技能路由的 LLM 模型（可选）
	listSkillModel *llm.Model
	// language 语言标识（"cn"/"en"）
	language string
}

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewListSkillTool 创建 ListSkillTool 实例。
//
// 对应 Python: ListSkillTool.__init__(get_skills, list_skill_model, language, agent_id)
func NewListSkillTool(
	getSkills func() []*skillpkg.Skill,
	listSkillModel *llm.Model,
	language string,
	agentID string,
) *ListSkillTool {
	card, _ := ptools.BuildToolCard("list_skill", "ListSkillTool", language, nil, agentID)

	return &ListSkillTool{
		card:           card,
		getSkills:      getSkills,
		listSkillModel: listSkillModel,
		language:       language,
	}
}

// Invoke 执行 list_skill 工具，列出技能或路由筛选技能。
//
// 一比一复刻 Python ListSkillTool.invoke()：
//  1. query 为空 → 返回所有技能（mode="all"）
//  2. query 非空 + listSkillModel 为 nil → 返回全部技能（mode="all", fallback 消息）
//  3. query 非空 + listSkillModel 可用 → routeSkills 路由筛选（mode="filtered"）
//  4. 异常 → success=false, error=err.Error()
//
// 对应 Python: ListSkillTool.invoke(inputs, **kwargs)
func (t *ListSkillTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	query := ""
	if v, ok := inputs["query"]; ok {
		query = fmt.Sprintf("%v", v)
	}
	query = trimSpace(query)

	// 一比一复刻 Python: try-except 整体异常捕获
	tryResult, tryErr := t.invokeCore(ctx, query)
	if tryErr != nil {
		logger.Error(logComponent).
			Str("query", query).
			Err(tryErr).
			Msg("ListSkillTool 执行异常")
		return map[string]any{
			"success": false,
			"error":   tryErr.Error(),
		}, nil
	}
	return tryResult, nil
}

// Stream ListSkillTool 不支持流式调用。
//
// 对应 Python: ListSkillTool.stream(inputs, **kwargs) — if False: yield None
func (t *ListSkillTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.NewErrStreamNotSupported(t.card.ID)
}

// Card 返回工具配置卡片。
func (t *ListSkillTool) Card() *tool.ToolCard { return t.card }

// ──────────────────────────── 非导出函数 ────────────────────────────

// invokeCore Invoke 的核心逻辑，将 Python 的 try-except 拆分为独立函数便于测试。
//
// 一比一复刻 Python ListSkillTool.invoke() 三路分支：
//  1. 无 query → all
//  2. 有 query + 无 model → fallback all
//  3. 有 query + 有 model → filtered
func (t *ListSkillTool) invokeCore(ctx context.Context, query string) (map[string]any, error) {
	if query == "" {
		// 一比一复刻 Python: if not query → ToolOutput(success=True, data={"skills": ..., "mode": "all"})
		logger.Info(logComponent).
			Str("mode", "all").
			Msg("ListSkillTool 无 query，返回全部技能")
		return map[string]any{
			"success": true,
			"data": map[string]any{
				"skills": t.dumpAllSkills(),
				"mode":   "all",
			},
		}, nil
	}

	if t.listSkillModel == nil {
		// 一比一复刻 Python: if list_skill_model is None → fallback all with message
		logger.Info(logComponent).
			Str("query", query).
			Str("mode", "all_fallback").
			Msg("ListSkillTool 无模型配置，回退返回全部技能")
		return map[string]any{
			"success": true,
			"data": map[string]any{
				"skills":  t.dumpAllSkills(),
				"mode":    "all",
				"message": "list_skill_model is not configured, fallback to all skills.",
			},
		}, nil
	}

	// 一比一复刻 Python: route_skills → select_skills_by_names → filtered result
	selectedNames, routeErr := t.routeSkills(ctx, query)
	if routeErr != nil {
		logger.Error(logComponent).
			Str("query", query).
			Err(routeErr).
			Msg("ListSkillTool 路由技能失败")
		return nil, routeErr
	}

	selectedSkills := t.selectSkillsByNames(selectedNames)

	logger.Info(logComponent).
		Str("query", query).
		Str("mode", "filtered").
		Int("selected_count", len(selectedSkills)).
		Msg("ListSkillTool 路由筛选技能成功")

	// 一比一复刻 Python: selected_skill_names = [skill.name for skill in selected_skills]
	selectedNamesResult := make([]string, 0, len(selectedSkills))
	for _, s := range selectedSkills {
		selectedNamesResult = append(selectedNamesResult, s.Name)
	}

	return map[string]any{
		"success": true,
		"data": map[string]any{
			"skills":            t.dumpSkills(selectedSkills),
			"mode":              "filtered",
			"selected_skill_names": selectedNamesResult,
		},
	}, nil
}

// dumpAllSkills 转换所有当前已启用技能为可序列化字典列表。
//
// 一比一复刻 Python: ListSkillTool._dump_all_skills()
func (t *ListSkillTool) dumpAllSkills() []map[string]any {
	allSkills := t.getSkills()
	if allSkills == nil {
		allSkills = []*skillpkg.Skill{}
	}
	return t.dumpSkills(allSkills)
}

// dumpSkills 将技能对象列表转为可序列化字典列表。
//
// 一比一复刻 Python: ListSkillTool._dump_skills(skills):
//   skill_dict = skill.asdict(include_directory=True)
//   skill_dict["skill_md_path"] = str(Path(skill.directory) / "SKILL.md")
func (t *ListSkillTool) dumpSkills(skills []*skillpkg.Skill) []map[string]any {
	results := make([]map[string]any, 0, len(skills))
	for _, s := range skills {
		skillDict := s.AsDict(true)
		// 一比一复刻 Python: skill_dict["skill_md_path"] = str(Path(skill.directory) / "SKILL.md")
		skillDict["skill_md_path"] = filepath.Join(s.Directory, "SKILL.md")
		results = append(results, skillDict)
	}
	return results
}

// routeSkills 通过 LLM 路由筛选与查询相关的技能名称。
//
// 一比一复刻 Python: ListSkillTool._route_skills(query):
//   payload = self._dump_all_skills()
//   response = await self.list_skill_model.invoke(messages=[...])
//   content = getattr(response, "content", "") or ""
//   return self._parse_selected_skill_names(content)
func (t *ListSkillTool) routeSkills(ctx context.Context, query string) ([]string, error) {
	payload := t.dumpAllSkills()
	payloadJSON, jsonErr := json.Marshal(payload)
	if jsonErr != nil {
		logger.Error(logComponent).
			Str("query", query).
			Err(jsonErr).
			Msg("ListSkillTool 序列化技能列表失败")
		return nil, fmt.Errorf("序列化技能列表失败: %w", jsonErr)
	}

	// 一比一复刻 Python 提示词，不允许自行翻译
	systemPrompt := sections.GetListSkillSystemPrompt(t.language)

	// 一比一复刻 Python: UserMessage(content=(f"User task:\n{query}\n\nAvailable skills:\n{json.dumps(payload, ...)}\n\nReturn only ..."))
	userContent := fmt.Sprintf(
		"User task:\n%s\n\nAvailable skills:\n%s\n\nReturn only the names of the skills that are relevant to the task.",
		query,
		string(payloadJSON),
	)

	msgs := model_clients.NewMessagesParam(
		llmschema.NewSystemMessage(systemPrompt),
		llmschema.NewUserMessage(userContent),
	)

	response, invokeErr := t.listSkillModel.Invoke(ctx, msgs)
	if invokeErr != nil {
		logger.Error(logComponent).
			Str("query", query).
			Err(invokeErr).
			Msg("ListSkillTool LLM 路由调用失败")
		return nil, fmt.Errorf("LLM 路由调用失败: %w", invokeErr)
	}

	content := ""
	if response != nil {
		content = response.Content.Text()
	}

	return parseSelectedSkillNames(content), nil
}

// parseSelectedSkillNames 解析 LLM 输出中的技能名称列表。
//
// 一比一复刻 Python: ListSkillTool._parse_selected_skill_names(content):
//   1. strip → 空 → []
//   2. 去除 ``` 代码块包裹
//   3. json.loads → data.get("skills", []) → filter empty strings
func parseSelectedSkillNames(content string) []string {
	text := trimSpace(content)
	if text == "" {
		return []string{}
	}

	// 一比一复刻 Python: if text.startswith("```") → 去除首尾代码块标记
	if strings.HasPrefix(text, "```") {
		lines := strings.Split(text, "\n")
		if len(lines) > 0 {
			lines = lines[1:]
		}
		if len(lines) > 0 && trimSpace(lines[len(lines)-1]) == "```" {
			lines = lines[:len(lines)-1]
		}
		text = trimSpace(strings.Join(lines, "\n"))
	}

	// 一比一复刻 Python: json.loads → data.get("skills", [])
	var data map[string]any
	if unmarshalErr := json.Unmarshal([]byte(text), &data); unmarshalErr != nil {
		logger.Warn(logComponent).
			Str("raw_content", content).
			Err(unmarshalErr).
			Msg("ListSkillTool JSON 解析失败")
		return []string{}
	}

	skillsRaw, ok := data["skills"]
	if !ok {
		return []string{}
	}
	skillsList, ok := skillsRaw.([]any)
	if !ok {
		return []string{}
	}

	// 一比一复刻 Python: [str(item).strip() for item in skills if str(item).strip()]
	result := make([]string, 0, len(skillsList))
	for _, item := range skillsList {
		s := trimSpace(fmt.Sprintf("%v", item))
		if s != "" {
			result = append(result, s)
		}
	}
	return result
}

// selectSkillsByNames 按名称列表选择技能对象。
//
// 一比一复刻 Python: ListSkillTool._select_skills_by_names(names):
//   if not names → []
//   skill_map = {skill.name: skill for skill in self.get_skills() or []}
//   selected = [skill_map.get(name) for name in names if skill_map.get(name) is not None]
func (t *ListSkillTool) selectSkillsByNames(names []string) []*skillpkg.Skill {
	if len(names) == 0 {
		return []*skillpkg.Skill{}
	}

	allSkills := t.getSkills()
	if allSkills == nil {
		allSkills = []*skillpkg.Skill{}
	}

	// 一比一复刻 Python: skill_map = {skill.name: skill for skill in skills}
	skillMap := make(map[string]*skillpkg.Skill, len(allSkills))
	for _, s := range allSkills {
		skillMap[s.Name] = s
	}

	selected := make([]*skillpkg.Skill, 0, len(names))
	for _, name := range names {
		s, ok := skillMap[name]
		if ok {
			selected = append(selected, s)
		}
	}
	return selected
}
