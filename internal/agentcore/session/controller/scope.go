package controller

import (
	"fmt"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 接口 ────────────────────────────

// Scope 隔离边界接口，定义数据隔离的基本边界。
// 每个 Scope 对应独立的存储命名空间，用于区分不同隔离策略。
// 对应 Python: openjiuwen/core/session/session_controller/scope.py (Scope)
type Scope interface {
	// String 转为字符串表示，用于序列化和存储键生成
	fmt.Stringer
}

// Subject 会话参与者接口，在 Scope 内进一步细分数据隔离。
// 不同 Subject 类型对应不同的会话场景（私聊、群聊、群内用户）。
// 对应 Python: openjiuwen/core/session/session_controller/scope.py (Subject)
type Subject interface {
	// String 转为字符串表示
	fmt.Stringer
}

// MainScope 主域，系统内置默认域。
// 用于不涉及额外租户或应用级隔离的通用场景，字符串表示为固定值 "main"。
// 对应 Python: openjiuwen/core/session/session_controller/scope.py (MainScope)
type MainScope struct{}

// DirectSubject 私聊参与者，用于一对一私聊场景，数据隔离到特定用户。
// 字符串格式："direct:{user_id}"
// 对应 Python: openjiuwen/core/session/session_controller/scope.py (DirectSubject)
type DirectSubject struct {
	// UserID 用户唯一标识
	UserID string
}

// GroupSubject 群聊参与者，用于群聊场景，群成员共享会话上下文。
// 字符串格式："group:{group_id}"
// 对应 Python: openjiuwen/core/session/session_controller/scope.py (GroupSubject)
type GroupSubject struct {
	// GroupID 群组唯一标识
	GroupID string
}

// GroupUserSubject 群内用户参与者，用于群聊中特定用户的隔离视角。
// 字符串格式："group:{group_id}:user:{user_id}"
// 对应 Python: openjiuwen/core/session/session_controller/scope.py (GroupUserSubject)
type GroupUserSubject struct {
	// GroupID 群组标识
	GroupID string
	// UserID 用户标识
	UserID string
}

// SessionScope 会话作用域，由 Scope 和可选 Subject 组成，定义数据隔离的边界。
// 同一 Agent 下不同 SessionScope 的数据完全隔离。
// 对应 Python: openjiuwen/core/session/session_controller/scope.py (SessionScope)
type SessionScope struct {
	// Scope 隔离边界对象
	Scope Scope
	// Subject 可选的参与者对象，用于在 Scope 内进一步细分隔离
	Subject Subject
}

// SessionScopeKey 全局唯一键，标识特定 Agent 下特定 SessionScope 的会话集合。
// 格式："agent:{agent_id}:{session_scope}"
// 对应 Python: openjiuwen/core/session/session_controller/scope.py (SessionScopeKey)
type SessionScopeKey struct {
	// AgentID Agent 唯一标识
	AgentID string
	// SessionScope 会话作用域
	SessionScope SessionScope
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── MainScope 方法 ────────────────────────────

// String 返回固定值 "main"
func (MainScope) String() string { return "main" }

// ──────────────────────────── DirectSubject 方法 ────────────────────────────

// String 返回 "direct:{UserID}" 格式的字符串
func (d DirectSubject) String() string {
	return "direct:" + d.UserID
}

// ──────────────────────────── GroupSubject 方法 ────────────────────────────

// String 返回 "group:{GroupID}" 格式的字符串
func (g GroupSubject) String() string {
	return "group:" + g.GroupID
}

// ──────────────────────────── GroupUserSubject 方法 ────────────────────────────

// String 返回 "group:{GroupID}:user:{UserID}" 格式的字符串
func (g GroupUserSubject) String() string {
	return "group:" + g.GroupID + ":user:" + g.UserID
}

// ──────────────────────────── SessionScope 方法 ────────────────────────────

// String 返回会话作用域的字符串表示。
// 无 Subject 时返回 "{scope}"，有 Subject 时返回 "{scope}:{subject}"。
func (s SessionScope) String() string {
	if s.Subject != nil {
		return s.Scope.String() + ":" + s.Subject.String()
	}
	return s.Scope.String()
}

// ──────────────────────────── SessionScopeKey 方法 ────────────────────────────

// String 返回全局唯一键的字符串表示，格式 "agent:{AgentID}:{SessionScope}"
func (k SessionScopeKey) String() string {
	return "agent:" + k.AgentID + ":" + k.SessionScope.String()
}

// ParseSessionScope 从字符串解析 SessionScope。
// 解析规则：不含 ":" 则整个字符串作为 scope；含 ":" 则第一段为 scope，其余为 subject。
// 对应 Python: SessionScope.from_string()
func ParseSessionScope(keyStr string) (SessionScope, error) {
	parts := strings.SplitN(keyStr, ":", 2)
	scopeStr := parts[0]
	subjectStr := ""
	if len(parts) > 1 {
		subjectStr = parts[1]
	}

	// 解析 Scope
	var scope Scope
	switch scopeStr {
	case "main":
		scope = MainScope{}
	default:
		return SessionScope{}, fmt.Errorf("未知 scope: %q", scopeStr)
	}

	// 解析 Subject
	var subject Subject
	if subjectStr != "" {
		if strings.HasPrefix(subjectStr, "direct:") {
			userID := strings.TrimPrefix(subjectStr, "direct:")
			if userID == "" {
				return SessionScope{}, fmt.Errorf("DirectSubject 的 UserID 不能为空")
			}
			subject = DirectSubject{UserID: userID}
		} else if strings.HasPrefix(subjectStr, "group:") && strings.Contains(subjectStr, ":user:") {
			parts := strings.Split(subjectStr, ":")
			if len(parts) != 4 || parts[0] != "group" || parts[2] != "user" {
				return SessionScope{}, fmt.Errorf("GroupUserSubject 格式错误，期望 'group:{group_id}:user:{user_id}'，得到 %q", subjectStr)
			}
			if parts[1] == "" || parts[3] == "" {
				return SessionScope{}, fmt.Errorf("GroupUserSubject 的 GroupID 和 UserID 不能为空")
			}
			subject = GroupUserSubject{GroupID: parts[1], UserID: parts[3]}
		} else if strings.HasPrefix(subjectStr, "group:") {
			groupID := strings.TrimPrefix(subjectStr, "group:")
			if groupID == "" {
				return SessionScope{}, fmt.Errorf("GroupSubject 的 GroupID 不能为空")
			}
			subject = GroupSubject{GroupID: groupID}
		} else {
			return SessionScope{}, fmt.Errorf("未知 subject 格式: %q", subjectStr)
		}
	}

	return SessionScope{Scope: scope, Subject: subject}, nil
}

// ParseSessionScopeKey 从字符串解析 SessionScopeKey。
// 字符串必须以 "agent:" 开头，后跟 agentID 和 SessionScope 字符串。
// 对应 Python: SessionScopeKey.from_string()
func ParseSessionScopeKey(keyStr string) (SessionScopeKey, error) {
	if !strings.HasPrefix(keyStr, "agent:") {
		return SessionScopeKey{}, fmt.Errorf("SessionScopeKey 必须以 'agent:' 开头")
	}
	rest := strings.TrimPrefix(keyStr, "agent:")
	parts := strings.SplitN(rest, ":", 2)
	if len(parts) < 1 || parts[0] == "" {
		return SessionScopeKey{}, fmt.Errorf("SessionScopeKey 缺少 agentID")
	}
	agentID := parts[0]
	sessionScopeStr := ""
	if len(parts) > 1 {
		sessionScopeStr = parts[1]
	}
	sessionScope, err := ParseSessionScope(sessionScopeStr)
	if err != nil {
		return SessionScopeKey{}, fmt.Errorf("解析 SessionScope 失败: %w", err)
	}
	return SessionScopeKey{AgentID: agentID, SessionScope: sessionScope}, nil
}
