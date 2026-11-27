package store

import (
	"sort"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// projectStats 项目统计口径（session_count / last_message_at / last_user_message_at）。
// 仅统计该项目的非置顶普通会话（cron_id 为空、channel=web）。
type projectStats struct {
	// SessionCount 会话数
	SessionCount int
	// LastMessageAt 最近消息时间（nil 表示无）
	LastMessageAt any
	// LastUserMessageAt 最近用户消息时间（nil 表示无）
	LastUserMessageAt any
}

// Triple Python 风格 (payload, error, code) 三元组。
type Triple struct {
	// Payload 成功载荷
	Payload map[string]any
	// Error 错误信息（空串表示成功）
	Error string
	// Code 错误码
	Code string
}

// triple 内部别名。
type triple = Triple

// ──────────────────────────── 导出函数 ────────────────────────────

// AttributeSessionProject 返回会话归属的项目 ID：已移除项目返回 ""（调用方跳过），
// 无归属回退默认项目（work_mode=code → default_code，否则 default）。
// Python: project_queries.py attribute_session_project（:40-56）。
func AttributeSessionProject(metadata map[string]any, visibleProjectIDs, removedProjectIDs map[string]bool) string {
	projectID := stringFromAny(metadata["project_id"])
	if projectID != "" {
		if visibleProjectIDs[projectID] {
			return projectID
		}
		if removedProjectIDs[projectID] {
			return ""
		}
	}
	if stringFromAny(metadata["work_mode"]) == WorkModeCode {
		return DefaultProjectIDCode
	}
	return DefaultProjectIDWork
}

// LoadProjectList 构造 Web 项目列表视图（含默认项目与统计，已排序）。
// Python: project_queries.py load_project_list（:415-491）。
func LoadProjectList(params map[string]any) triple {
	filterValue := orDefault(stringFromAny(params["filter"]), "all")
	if filterValue != "all" && filterValue != "pinned" && filterValue != "unpinned" {
		filterValue = "all"
	}
	includeHidden := boolFromAny(params["include_hidden"])
	var workMode string
	if wm, isStr := params["work_mode"].(string); isStr && trimLower(wm) != "" {
		candidate := trimLower(wm)
		if !supportedWorkModes[candidate] {
			return triple{Error: quotedInvalidWorkMode(candidate), Code: "BAD_REQUEST"}
		}
		workMode = candidate
	}

	allProjects := ListProjects(true, true)
	var projects []Project
	for _, p := range allProjects {
		if workMode != "" && orDefault(p.WorkMode, WorkModeWeb) != workMode {
			continue
		}
		projects = append(projects, p)
	}
	visibleProjectIDs, removedProjectIDs := splitProjectIDs(allProjects)
	stats := map[string]*projectStats{}

	statsFor := func(projectID string) *projectStats {
		if s, ok := stats[projectID]; ok {
			return s
		}
		s := &projectStats{}
		stats[projectID] = s
		return s
	}

	for _, session := range CollectAllSessionsMetadata() {
		if stringFromAny(session["channel_id"]) != "web" ||
			boolFromAny(session["pinned"]) || stringFromAny(session["cron_id"]) != "" {
			continue
		}
		owner := AttributeSessionProject(session, visibleProjectIDs, removedProjectIDs)
		if owner == "" {
			continue
		}
		entry := statsFor(owner)
		entry.SessionCount++
		absorbTimestamps(entry, session)
	}

	zero := &projectStats{}

	item := func(project *Project, defaultID string) map[string]any {
		if defaultID != "" {
			return ProjectInfoPayload(nil, WithDefaultID(defaultID), WithStats(statsValue(stats, defaultID, zero)))
		}
		st := zero
		if !project.Hidden {
			st = stats[project.ProjectID]
			if st == nil {
				st = zero
			}
		}
		return ProjectInfoPayload(project, WithStats(statsToAny(st)))
	}

	var defaultItems []map[string]any
	appendDefault := func(defaultID string) {
		defaultItems = append(defaultItems, item(nil, defaultID))
	}
	if workMode == "" || workMode == WorkModeWeb || workMode == WorkModeDesign {
		appendDefault(DefaultProjectIDWork)
	}
	if workMode == "" || workMode == WorkModeCode {
		appendDefault(DefaultProjectIDCode)
	}

	projectAt := func(i int) *Project { return &projects[i] }
	pickVisible := func(p Project) bool { return includeHidden || !p.Hidden }

	var result []map[string]any
	switch filterValue {
	case "pinned":
		for i := range projects {
			if projects[i].Pinned {
				result = append(result, item(projectAt(i), ""))
			}
		}
		sortByPinOrder(result)
	case "unpinned":
		for i := range projects {
			if !projects[i].Pinned && pickVisible(projects[i]) {
				result = append(result, item(projectAt(i), ""))
			}
		}
		sortByUserSortDesc(result)
		result = append(result, defaultItems...)
	default:
		var pinned, unpinned []map[string]any
		for i := range projects {
			if projects[i].Pinned && pickVisible(projects[i]) {
				pinned = append(pinned, item(projectAt(i), ""))
			}
		}
		for i := range projects {
			if !projects[i].Pinned && pickVisible(projects[i]) {
				unpinned = append(unpinned, item(projectAt(i), ""))
			}
		}
		sortByPinOrder(pinned)
		sortByUserSortDesc(unpinned)
		result = append(append(pinned, unpinned...), defaultItems...)
	}
	return triple{Payload: map[string]any{"projects": result}}
}

// LoadProjectInfo 项目详情（含统计）。
// Python: project_queries.py load_project_info（:164-200）。
func LoadProjectInfo(params map[string]any) triple {
	projectID := trimLower(stringFromAny(params["project_id"]))
	if stringFromAny(params["project_id"]) == "" {
		return triple{Error: "project_id is required", Code: "BAD_REQUEST"}
	}
	allProjects := ListProjects(true, true)
	visibleProjectIDs, removedProjectIDs := splitProjectIDs(allProjects)
	stats := &projectStats{}
	for _, session := range CollectAllSessionsMetadata() {
		if stringFromAny(session["channel_id"]) != "web" ||
			boolFromAny(session["pinned"]) || stringFromAny(session["cron_id"]) != "" {
			continue
		}
		if AttributeSessionProject(session, visibleProjectIDs, removedProjectIDs) != projectID {
			continue
		}
		stats.SessionCount++
		absorbTimestamps(stats, session)
	}
	if IsDefaultProjectID(stringFromAny(params["project_id"])) {
		info := ProjectInfoPayload(nil, WithDefaultID(stringFromAny(params["project_id"])), WithStats(statsToAny(stats)))
		return triple{Payload: mergeProjectTopLevel(info)}
	}
	includeHidden := boolFromAny(params["include_hidden"])
	project := GetProjectByID(stringFromAny(params["project_id"]), true)
	if project == nil || (project.Hidden && !includeHidden) {
		return triple{Error: "project not found", Code: "NOT_FOUND"}
	}
	info := ProjectInfoPayload(project, WithStats(statsToAny(stats)))
	return triple{Payload: mergeProjectTopLevel(info)}
}

// LoadPinnedSessions 置顶会话列表（Web 投影）。
// Python: project_queries.py load_pinned_sessions（:202-225）。
func LoadPinnedSessions() triple {
	allProjects := ListProjects(true, true)
	_, removedProjectIDs := splitProjectIDs(allProjects)
	var pinned []map[string]any
	for _, session := range CollectAllSessionsMetadata() {
		channelID := stringFromAny(session["channel_id"])
		if !boolFromAny(session["pinned"]) {
			continue
		}
		if channelID != "web" && !(channelID == "__cron__" && stringFromAny(session["cron_id"]) != "") {
			continue
		}
		if removedProjectIDs[stringFromAny(session["project_id"])] {
			continue
		}
		pinned = append(pinned, session)
	}
	sort.SliceStable(pinned, func(i, j int) bool {
		return intFromAny(pinned[i]["pin_order"]) < intFromAny(pinned[j]["pin_order"])
	})
	out := make([]any, 0, len(pinned))
	for _, session := range pinned {
		out = append(out, ToSessionInfo(session))
	}
	return triple{Payload: map[string]any{"sessions": out}}
}

// LoadProjectSessions 项目下的非置顶普通会话列表。
// Python: project_queries.py load_project_sessions（:293-325）。
func LoadProjectSessions(params map[string]any, _userID string) triple {
	projectID := stringFromAny(params["project_id"])
	if trimLower(projectID) == "" && projectID == "" {
		return triple{Error: "project_id is required", Code: "BAD_REQUEST"}
	}
	limit, offset := parsePage(params)
	allProjects := ListProjects(true, true)
	visibleProjectIDs, removedProjectIDs := splitProjectIDs(allProjects)
	if !IsDefaultProjectID(projectID) {
		project := GetProjectByID(projectID, true)
		if project == nil || project.Hidden {
			return triple{Error: "project not found", Code: "NOT_FOUND"}
		}
	}
	var matched []map[string]any
	for _, session := range CollectAllSessionsMetadata() {
		if boolFromAny(session["pinned"]) || stringFromAny(session["cron_id"]) != "" ||
			stringFromAny(session["channel_id"]) != "web" {
			continue
		}
		if AttributeSessionProject(session, visibleProjectIDs, removedProjectIDs) != projectID {
			continue
		}
		if !sessionMatchesRequestedWorkMode(session, params["work_mode"]) {
			continue
		}
		matched = append(matched, session)
	}
	sortByLastUserMessageDesc(matched)
	total := len(matched)
	page := pageSlice(matched, offset, limit)
	out := make([]any, 0, len(page))
	for _, session := range page {
		out = append(out, ToSessionInfo(session))
	}
	return triple{Payload: map[string]any{"sessions": out, "total": total}}
}

// LoadProjectCronSessions 项目 cron 执行会话列表。
// Python: project_queries.py load_project_cron_sessions（:327-413），
// 含存量 team 会话按目录名回填 cron_id/project_id 的自愈路径。
func LoadProjectCronSessions(params map[string]any, _userID string) triple {
	projectID := stringFromAny(params["project_id"])
	if projectID == "" {
		return triple{Error: "project_id is required", Code: "BAD_REQUEST"}
	}
	cronID := stringFromAny(params["cron_id"])
	limit, offset := parsePage(params)
	allProjects := ListProjects(true, true)
	visibleProjectIDs, removedProjectIDs := splitProjectIDs(allProjects)
	if !IsDefaultProjectID(projectID) {
		project := GetProjectByID(projectID, true)
		if project == nil || project.Hidden {
			return triple{Error: "project not found", Code: "NOT_FOUND"}
		}
	}
	var matched []map[string]any
	for _, session := range CollectAllSessionsMetadata() {
		if boolFromAny(session["pinned"]) {
			continue
		}
		storedCronID := stringFromAny(session["cron_id"])
		sessionID := stringFromAny(session["session_id"])
		// 兜底：目录名符合 cron_*_{job_id} 约定但元数据缺 cron_id 的存量 team
		// 执行会话。命中即回写 cron_id 自愈（sync 首次锁定语义只写空值），
		// project_id 必须随 cron_id 一并回写，否则第二次查询起会话两头都看不到。
		nameMatched := cronID != "" && storedCronID == "" && CronSessionMatchesJob(sessionID, cronID)
		if nameMatched {
			// 已有项目归属必须继续参与过滤；只有未绑定项目才迁移。
			if trimLower(stringFromAny(session["project_id"])) != "" && !cronSessionProjectMatches(
				session, projectID, visibleProjectIDs, removedProjectIDs, cronID) {
				continue
			}
			SyncSessionRequestMetadataBackfill(sessionID, cronID, projectID)
			session = cloneMap(session)
			session["cron_id"] = cronID
		} else if storedCronID == "" {
			continue
		}
		if cronID != "" && stringFromAny(session["cron_id"]) != cronID {
			continue
		}
		if !nameMatched && !cronSessionProjectMatches(session, projectID, visibleProjectIDs, removedProjectIDs, cronID) {
			continue
		}
		matched = append(matched, session)
	}
	sortByLastUserMessageDesc(matched)
	total := len(matched)
	page := pageSlice(matched, offset, limit)
	out := make([]any, 0, len(page))
	for _, session := range page {
		out = append(out, ToSessionInfo(session))
	}
	return triple{Payload: map[string]any{"sessions": out, "total": total}}
}

// ResolveCronBindingControl 控制面 cron 项目绑定解析。
// Python: project_queries.py resolve_cron_binding（:227-243）。
func ResolveCronBindingControl(params map[string]any, channelID string) triple {
	workMode, errCode := ResolveRequestWorkMode(params, orDefault(channelID, "web"))
	if errCode != "" {
		return triple{Error: quotedInvalidWorkModeRaw(params["work_mode"]), Code: "BAD_REQUEST"}
	}
	binding := ResolveCronProjectBinding(params["project_id"], params["project_dir"], workMode)
	if binding.Error != "" {
		code := orDefault(binding.Code, "BAD_REQUEST")
		return triple{Error: binding.Error, Code: code}
	}
	return triple{Payload: map[string]any{"project_id": binding.ProjectID, "work_mode": binding.WorkMode}}
}

// ProjectInfoPayload 项目 info 序列化（对齐 Web 契约）。
// Python: project_queries.py project_info_payload（:88-155）。
func ProjectInfoPayload(project *Project, opts ...ProjectInfoOption) map[string]any {
	cfg := projectInfoConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.stats == nil {
		cfg.stats = map[string]any{
			"session_count": 0, "last_message_at": nil, "last_user_message_at": nil,
		}
	}
	gitDefaults := map[string]any{
		"enabled": false, "repo_root": "", "initialized_by_jiuwenswarm": false,
		"detected_at": float64(0), "status": "disabled", "branch": "",
		"error": "", "error_code": "", "hint": "", "is_dirty": false,
	}
	var rawGit map[string]any
	if project != nil {
		if len(project.Git) > 0 {
			rawGit = project.Git
		}
	}
	git := gitDefaults
	if rawGit != nil {
		git = map[string]any{}
		for k, v := range gitDefaults {
			git[k] = v
		}
		for k, v := range rawGit {
			git[k] = v
		}
	}
	if cfg.defaultID != "" {
		wm := WorkModeWeb
		if cfg.defaultID == DefaultProjectIDCode {
			wm = WorkModeCode
		}
		return map[string]any{
			"project_id": cfg.defaultID, "name": "默认项目", "project_dir": "",
			"pinned": false, "pin_order": 0, "is_default": true, "hidden": false,
			"lifecycle_operation": nil, "execution_blocked": false, "stop_pending": false,
			"work_mode": wm, "git": git,
			"session_count":        cfg.stats["session_count"],
			"last_message_at":      cfg.stats["last_message_at"],
			"last_user_message_at": cfg.stats["last_user_message_at"],
			"created_at":           float64(0), "updated_at": float64(0),
		}
	}
	projection, _ := LifecycleProjectionForProject(project.ProjectID)
	workMode := orDefault(project.WorkMode, WorkModeWeb)
	return map[string]any{
		"project_id": project.ProjectID, "name": project.Name,
		"project_dir": project.ProjectDir, "pinned": project.Pinned,
		"pin_order": project.PinOrder, "is_default": false, "hidden": project.Hidden,
		"lifecycle_operation": projection["lifecycle_operation"],
		"execution_blocked":   projection["execution_blocked"],
		"stop_pending":        projection["stop_pending"],
		"work_mode":           workMode, "git": git,
		"session_count":        cfg.stats["session_count"],
		"last_message_at":      cfg.stats["last_message_at"],
		"last_user_message_at": cfg.stats["last_user_message_at"],
		"created_at":           project.CreatedAt, "updated_at": project.UpdatedAt,
	}
}

// ──────────────────────────── 项目信息选项 ────────────────────────────

// ProjectInfoOption ProjectInfoPayload 函数式选项（对齐 Python 关键字参数）。
type ProjectInfoOption func(*projectInfoConfig)

// projectInfoConfig 选项载体。
type projectInfoConfig struct {
	// defaultID 默认项目 ID（非空时按默认项目形状输出）
	defaultID string
	// stats 统计载荷
	stats map[string]any
}

// WithDefaultID 指定默认项目 ID。
func WithDefaultID(id string) ProjectInfoOption {
	return func(c *projectInfoConfig) { c.defaultID = id }
}

// WithStats 指定统计载荷。
func WithStats(stats map[string]any) ProjectInfoOption {
	return func(c *projectInfoConfig) { c.stats = stats }
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// splitProjectIds拆分 (visible, removed) 项目 ID 集合。
// Python: project_queries.py split_project_ids（:157-162）。
func splitProjectIDs(projects []Project) (map[string]bool, map[string]bool) {
	visible := map[string]bool{}
	removed := map[string]bool{}
	for _, p := range projects {
		if p.Hidden {
			removed[p.ProjectID] = true
		} else {
			visible[p.ProjectID] = true
		}
	}
	return visible, removed
}

// cronSessionProjectMatches cron 会话与所查项目匹配判定。
// Python: project_queries.py cron_session_project_matches（:60-86）：
// 两个虚拟默认项目在带 cron_id 时视为同一桶。
func cronSessionProjectMatches(session map[string]any, projectID string,
	visibleProjectIDs, removedProjectIDs map[string]bool, cronID string) bool {
	actual := AttributeSessionProject(session, visibleProjectIDs, removedProjectIDs)
	if actual == projectID {
		return true
	}
	return cronID != "" && IsDefaultProjectID(projectID) &&
		(actual == DefaultProjectIDWork || actual == DefaultProjectIDCode)
}

// parsePage 宽松分页解析（Python _parse_page :245-267）。
func parsePage(params map[string]any) (int, int) {
	limit := 0
	hasLimit := false
	switch v := params["limit"].(type) {
	case float64:
		if v == float64(int(v)) {
			limit, hasLimit = int(v), true
		}
	case int:
		limit, hasLimit = v, true
	}
	offset := 0
	switch v := params["offset"].(type) {
	case float64:
		if v == float64(int(v)) {
			offset = int(v)
		}
	case int:
		offset = v
	}
	if hasLimit && limit < 1 {
		limit = 1
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

// lastUserMessageTs 最近用户消息排序键（无可用时间戳排最后）。
// Python: project_queries.py _last_user_message_ts（:269-276）。
func lastUserMessageTs(session map[string]any) float64 {
	if v, ok := numericAny(session["last_user_message_at"]); ok {
		return v
	}
	return 0
}

// sessionMatchesRequestedWorkMode 会话是否落在调用方 work_mode 内。
// Python: project_queries.py _session_matches_requested_work_mode（:278-291）：
// 默认项目共享，不过滤会混入 design 画布与 work 会话。
func sessionMatchesRequestedWorkMode(session map[string]any, rawWorkMode any) bool {
	raw, ok := rawWorkMode.(string)
	if !ok || trimLower(raw) == "" {
		return true
	}
	requested := trimLower(raw)
	if !supportedWorkModes[requested] {
		return true
	}
	actual := trimLower(orDefault(stringFromAny(session["work_mode"]), WorkModeWeb))
	return actual == requested
}

// absorbTimestamps 吸收会话的 last_message_at / last_user_message_at 最大值。
func absorbTimestamps(stats *projectStats, session map[string]any) {
	for key, target := range map[string]*any{
		"last_message_at":      &stats.LastMessageAt,
		"last_user_message_at": &stats.LastUserMessageAt,
	} {
		if v, ok := numericAny(session[key]); ok {
			if cur, curOK := numericAny(*target); !curOK || v > cur {
				*target = v
			}
		}
	}
}

// statsValue 取项目统计（无则 zero），转 any 载荷。
func statsValue(stats map[string]*projectStats, projectID string, zero *projectStats) map[string]any {
	s := zero
	if v, ok := stats[projectID]; ok {
		s = v
	}
	return statsToAny(s)
}

// statsToAny 统计结构 → 载荷。
func statsToAny(s *projectStats) map[string]any {
	return map[string]any{
		"session_count": s.SessionCount, "last_message_at": s.LastMessageAt,
		"last_user_message_at": s.LastUserMessageAt,
	}
}

// mergeProjectTopLevel {project: info, **info}（对齐 Python {"project": info, **info}）。
func mergeProjectTopLevel(info map[string]any) map[string]any {
	out := map[string]any{"project": info}
	for k, v := range info {
		out[k] = v
	}
	return out
}

// sortByPinOrder 按 pin_order 升序。
func sortByPinOrder(items []map[string]any) {
	sort.SliceStable(items, func(i, j int) bool {
		return intFromAny(items[i]["pin_order"]) < intFromAny(items[j]["pin_order"])
	})
}

// sortByUserSortDesc 按 last_user_message_at 降序。
func sortByUserSortDesc(items []map[string]any) {
	sort.SliceStable(items, func(i, j int) bool {
		return lastUserMessageTs(items[i]) > lastUserMessageTs(items[j])
	})
}

// sortByLastUserMessageDesc 会话列表按 last_user_message_at 降序（reverse=True）。
func sortByLastUserMessageDesc(items []map[string]any) {
	sort.SliceStable(items, func(i, j int) bool {
		return lastUserMessageTs(items[i]) > lastUserMessageTs(items[j])
	})
}

// pageSlice 分页切片（limit<=0 表示不截断，对齐 Python page 语义）。
func pageSlice(items []map[string]any, offset, limit int) []map[string]any {
	if offset > len(items) {
		offset = len(items)
	}
	end := len(items)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return items[offset:end]
}

// cloneMap 浅拷贝会话元数据。
func cloneMap(src map[string]any) map[string]any {
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

// quotedInvalidWorkMode 非法 work_mode 错误文案。
func quotedInvalidWorkMode(candidate string) string {
	return "invalid work_mode: '" + candidate + "'"
}

// quotedInvalidWorkModeRaw 非法 work_mode 原始值错误文案。
func quotedInvalidWorkModeRaw(raw any) string {
	if s, ok := raw.(string); ok {
		return quotedInvalidWorkMode(s)
	}
	logger.Debug(logger.ComponentAgentServer).Msg("invalid work_mode (non-string)")
	return "invalid work_mode"
}
