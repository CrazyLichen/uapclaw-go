package controller

// ──────────────────────────── 结构体 ────────────────────────────

// SessionScopeFactory 会话作用域工厂，提供创建常见 SessionScope 实例的静态方法。
// 简化内置 scope 和 subject 组合的创建，同时支持自定义扩展。
// 对应 Python: openjiuwen/core/session/session_controller/scope_factory.py (SessionScopeFactory)
type SessionScopeFactory struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// CreateMain 创建仅含主域的会话作用域（无 Subject）。
// 字符串表示："main"
func (SessionScopeFactory) CreateMain() SessionScope {
	return SessionScope{Scope: MainScope{}}
}

// CreateDirect 创建私聊场景的会话作用域。
// 字符串表示："main:direct:{userID}"
func (SessionScopeFactory) CreateDirect(userID string) SessionScope {
	return SessionScope{Scope: MainScope{}, Subject: DirectSubject{UserID: userID}}
}

// CreateGroup 创建群聊场景的会话作用域。
// 字符串表示："main:group:{groupID}"
func (SessionScopeFactory) CreateGroup(groupID string) SessionScope {
	return SessionScope{Scope: MainScope{}, Subject: GroupSubject{GroupID: groupID}}
}

// CreateGroupUser 创建群内用户视角的会话作用域。
// 字符串表示："main:group:{groupID}:user:{userID}"
func (SessionScopeFactory) CreateGroupUser(groupID, userID string) SessionScope {
	return SessionScope{Scope: MainScope{}, Subject: GroupUserSubject{GroupID: groupID, UserID: userID}}
}

// CreateCustom 使用自定义 Scope 和 Subject 创建会话作用域。
func (SessionScopeFactory) CreateCustom(scope Scope, subject Subject) SessionScope {
	return SessionScope{Scope: scope, Subject: subject}
}

// FromString 从字符串解析会话作用域，委托给 ParseSessionScope。
func (SessionScopeFactory) FromString(keyStr string) (SessionScope, error) {
	return ParseSessionScope(keyStr)
}
