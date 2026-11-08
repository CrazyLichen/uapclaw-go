package database

import (
	"encoding/json"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Team 团队信息模型。
// Python: Team (openjiuwen/agent_teams/tools/models.py)
// 静态表 team_info 的行模型。
type Team struct {
	// TeamName 团队名称（主键）
	TeamName string `json:"team_name" gorm:"primaryKey"`
	// DisplayName 显示名称
	DisplayName string `json:"display_name"`
	// LeaderMemberName Leader 成员名
	LeaderMemberName string `json:"leader_member_name"`
	// Desc 团队描述
	Desc string `json:"desc,omitempty"`
	// Prompt 团队提示词
	Prompt string `json:"prompt,omitempty"`
	// Created 创建时间（毫秒时间戳）
	Created int64 `json:"created"`
	// UpdatedAt 更新时间（毫秒时间戳，仅 roster 变更时 bump）
	UpdatedAt int64 `json:"updated_at,omitempty"`
}

// TeamMember 团队成员模型。
// Python: TeamMember (openjiuwen/agent_teams/tools/models.py)
// 静态表 team_member 的行模型，复合主键 (member_name, team_name)。
type TeamMember struct {
	// MemberName 成员名称（主键）
	MemberName string `json:"member_name" gorm:"primaryKey"`
	// TeamName 团队名称（主键，外键 team_info.team_name）
	TeamName string `json:"team_name" gorm:"primaryKey"`
	// DisplayName 显示名称
	DisplayName string `json:"display_name"`
	// Desc 成员描述
	Desc string `json:"desc,omitempty"`
	// AgentCard Agent 卡片 JSON 字符串（存储 AgentCard.model_dump_json()）
	AgentCard string `json:"agent_card"`
	// Status 成员状态（MemberStatus 枚举值）
	Status string `json:"status"`
	// ExecutionStatus 执行状态（ExecutionStatus 枚举值）
	ExecutionStatus string `json:"execution_status,omitempty"`
	// Mode 成员模式（MemberMode 枚举值：build_mode / plan_mode）
	Mode string `json:"mode"`
	// Role 成员角色（TeamRole 枚举值：leader / teammate / human_agent）
	Role string `json:"role"`
	// Prompt 成员专属提示词
	Prompt string `json:"prompt,omitempty"`
	// ModelRefJSON 模型引用 JSON（{"model_id": str, "model_name": str}）
	ModelRefJSON string `json:"model_ref_json,omitempty"`
	// UpdatedAt 更新时间（毫秒时间戳，仅 roster 变更时 bump，status 变更不 bump）
	UpdatedAt int64 `json:"updated_at,omitempty"`
}

// TeamTaskBase 任务行模型。
// Python: TeamTaskBase (openjiuwen/agent_teams/tools/database/task_dao.py)
// 动态表 team_task_<session_suffix> 的行模型。
type TeamTaskBase struct {
	// TaskID 任务唯一标识
	TaskID string `json:"task_id" gorm:"column:task_id;primaryKey"`
	// TeamName 团队名称
	TeamName string `json:"team_name" gorm:"column:team_name"`
	// Title 任务标题
	Title string `json:"title" gorm:"column:title"`
	// Content 任务内容
	Content string `json:"content" gorm:"column:content"`
	// Status 任务状态（TaskStatus 枚举值）
	Status string `json:"status" gorm:"column:status"`
	// Assignee 认领人/分配人（nil 表示未分配，对齐 Python: Optional[str] = Field(nullable=True)）
	Assignee *string `json:"assignee,omitempty" gorm:"column:assignee"`
	// Reviewer 验收人 JSON 文本（nil = 无验收门）
	// Python: reviewer: Optional[str] = Field(default=None, nullable=True)（models.py L145）
	// 格式 [{"reviewer_id":"bob","type":"verifier"}]；空表示完成时直接 COMPLETED，
	// 非空则完成后先进 IN_REVIEW 走验收门（F_59/F_62）
	Reviewer *string `json:"reviewer,omitempty" gorm:"column:reviewer"`
	// Revision 正文修订号，每次 title/content 编辑 +1（F_129）
	// Python: revision: int = Field(default=0, nullable=False)（models.py L182）
	Revision int64 `json:"revision" gorm:"column:revision;not null;default:0"`
	// ObservedRevision 认领人最后看到的修订号（F_129）
	// Python: observed_revision: int = Field(default=0, nullable=False)（models.py L183）
	// revision > observed_revision 即「改过但未读」，是独立于任何通知标志的常驻信号
	ObservedRevision int64 `json:"observed_revision" gorm:"column:observed_revision;not null;default:0"`
	// ChangeNotes 编辑说明 JSON 列表 [{"revision","note","by"}]（F_129 / D8）
	// Python: change_notes: Optional[str] = Field(default=None, nullable=True)（models.py L189）
	// 随 revision bump 一同追加，作为审计历史保存
	ChangeNotes *string `json:"change_notes,omitempty" gorm:"column:change_notes"`
	// UpdatedAt 更新时间（毫秒时间戳）
	UpdatedAt int64 `json:"updated_at,omitempty" gorm:"column:updated_at"`
}

// TeamTaskDependencyBase 依赖边模型。
// Python: TeamTaskDependencyBase (openjiuwen/agent_teams/tools/database/task_dao.py)
// 动态表 team_task_dependency_<session_suffix> 的行模型。
type TeamTaskDependencyBase struct {
	// TaskID 下游任务ID（被阻塞的任务）
	TaskID string `json:"task_id" gorm:"column:task_id;primaryKey"`
	// DependsOnID 上游任务ID（阻塞源）
	DependsOnID string `json:"depends_on_task_id" gorm:"column:depends_on_task_id;primaryKey"`
	// TeamName 团队名称
	TeamName string `json:"team_name" gorm:"column:team_name"`
	// Resolved 依赖是否已解决（上游完成/取消时标记为 true）
	Resolved bool `json:"resolved" gorm:"column:resolved"`
}

// NewTaskSpec 图变更管线中待插入的新任务规范。
// Python: NewTaskSpec (openjiuwen/agent_teams/tools/database/task_dao.py)
type NewTaskSpec struct {
	// TaskID 任务唯一标识
	TaskID string
	// Title 任务标题
	Title string
	// Content 任务内容
	Content string
	// InitialStatus 初始状态
	InitialStatus string
}

// EdgeSpec 依赖边规范（管线输入）。
// 方向语义：TaskID 依赖 DependsOnID（TaskID 被 DependsOnID 阻塞）。
// Python: 的 (task_id, depends_on_task_id) 边方向。
type EdgeSpec struct {
	// TaskID 下游任务ID（被阻塞的任务）
	TaskID string
	// DependsOnID 上游任务ID（阻塞源）
	DependsOnID string
}

// TeamMessageBase 消息行模型。
// Python: TeamMessageBase (openjiuwen/agent_teams/tools/models.py)
// 动态表 team_message_<session_suffix> 的行模型。
type TeamMessageBase struct {
	// MessageID 消息唯一标识（主键）
	MessageID string `json:"message_id" gorm:"column:message_id;primaryKey"`
	// TeamName 团队名称
	TeamName string `json:"team_name" gorm:"column:team_name"`
	// FromMemberName 发送者
	FromMemberName string `json:"from_member_name" gorm:"column:from_member_name"`
	// ToMemberName 接收者（nil 表示广播消息，对齐 Python: Optional[str] = Field(nullable=True)）
	ToMemberName *string `json:"to_member_name,omitempty" gorm:"column:to_member_name"`
	// Content 消息内容
	Content string `json:"content" gorm:"column:content"`
	// Timestamp 毫秒时间戳
	Timestamp int64 `json:"timestamp" gorm:"column:timestamp"`
	// Broadcast 是否广播消息
	Broadcast bool `json:"broadcast" gorm:"column:broadcast"`
	// IsRead 直发消息的已读标记。广播消息为 nil（广播已读状态由 MessageReadStatusBase 管理）。
	// Python: is_read = None if broadcast else is_read
	// nil = 广播消息（不参与直发已读判定）
	// *false = 直发未读, *true = 直发已读
	IsRead *bool `json:"is_read" gorm:"column:is_read"`
}

// MessageReadStatusBase 广播已读水位模型。
// Python: MessageReadStatusBase (openjiuwen/agent_teams/tools/models.py)
// 动态表 message_read_status_<session_suffix> 的行模型。
// 每个 member 每个团队一条记录，存储已读的最新广播消息的 timestamp。
type MessageReadStatusBase struct {
	// MemberName 成员名（主键）
	MemberName string `json:"member_name" gorm:"column:member_name;primaryKey"`
	// TeamName 团队名（主键）
	TeamName string `json:"team_name" gorm:"column:team_name;primaryKey"`
	// ReadAt 已读水位时间戳（该成员已读的最新广播消息的 timestamp）
	ReadAt *int64 `json:"read_at" gorm:"column:read_at"`
}

// GraphMutationResult 图变更操作结果。
// Python: GraphMutationResult (openjiuwen/agent_teams/tools/database/task_dao.py)
type GraphMutationResult struct {
	// Ok 操作是否成功
	Ok bool
	// Reason 失败原因（Ok=false 时）
	Reason string
	// RefreshedTasks 状态刷新产出的任务列表（对齐 Python: refreshed_tasks: list[TeamTaskBase]）
	RefreshedTasks []*TeamTaskBase
}

// CancelAllTasksResult 批量取消任务的返回结果。
// Python: cancel_all_tasks() → {"cancelled_tasks": [...], "unblocked_tasks": [...]}
type CancelAllTasksResult struct {
	// Cancelled 被取消的任务列表
	Cancelled []*TeamTaskBase
	// Unblocked 被解除阻塞的任务列表（从 BLOCKED→PENDING 的任务，对齐 Python: unblocked_tasks）
	Unblocked []*TeamTaskBase
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// TeamDynamicTablePrefixes 动态表名前缀（用于识别和清理）。
	// Python: TEAM_DYNAMIC_TABLE_PREFIXES
	TeamDynamicTablePrefixes = [...]string{
		"team_task_dependency_",
		"team_task_",
		"team_message_",
		"message_read_status_",
	}
	// TeamStaticTablesToClear 需要清空的静态表名。
	// Python: TEAM_STATIC_TABLES_TO_CLEAR
	TeamStaticTablesToClear = [...]string{
		"team_info",
		"team_member",
	}
)

// TableName 指定 Team 对应的数据库表名
func (Team) TableName() string { return "team_info" }

// TableName 指定 TeamMember 对应的数据库表名
func (TeamMember) TableName() string { return "team_member" }

// ──────────────────────────── 导出函数 ────────────────────────────

// BoolPtr 返回 bool 的指针。
func BoolPtr(v bool) *bool { return &v }

// Int64Ptr 返回 int64 的指针。
func Int64Ptr(v int64) *int64 { return &v }

// StringPtr 返回 string 的指针。
func StringPtr(v string) *string { return &v }

// Reviewers 解析 reviewer 列为成员名列表。
//
// 新格式元素是对象 {"reviewer_id":"bob","type":"verifier"}；字符串元素同样接受
// （宽松，对应上游 reviewers() 对旧格式的自动升级语义）。空值 / 非 JSON / 非列表
// 一律返回空，语义是「无验收门」，完成路径按直接 COMPLETED 处理。
//
// 偏差 D-2（Q2 裁决）：生成侧只写新对象格式（Go 无历史数据，旧字符串格式永远
// 不会产生），但解析侧保留字符串分支以对齐上游容错行为。
//
// Python: TeamTaskBase.reviewers() (models.py L193-219)
func (t *TeamTaskBase) Reviewers() []string {
	if t == nil || t.Reviewer == nil || *t.Reviewer == "" {
		return nil
	}
	var parsed []any
	if err := json.Unmarshal([]byte(*t.Reviewer), &parsed); err != nil {
		return nil
	}
	ids := make([]string, 0, len(parsed))
	for _, entry := range parsed {
		switch v := entry.(type) {
		case string:
			if v != "" {
				ids = append(ids, v)
			}
		case map[string]any:
			if id, ok := v["reviewer_id"].(string); ok && id != "" {
				ids = append(ids, id)
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return ids
}

// HasReviewers 该任务是否配了验收人（决定完成时是否走 IN_REVIEW）。
// Python: 调用点 if task.reviewers():（team_context.py L899）
func (t *TeamTaskBase) HasReviewers() bool { return len(t.Reviewers()) > 0 }
