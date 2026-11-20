package controller

import (
	"testing"
)

// ──────────────────────────── String 方法测试 ────────────────────────────

func TestMainScope_String(t *testing.T) {
	scope := MainScope{}
	if got := scope.String(); got != "main" {
		t.Errorf("MainScope.String() = %q, want %q", got, "main")
	}
}

func TestDirectSubject_String(t *testing.T) {
	subject := DirectSubject{UserID: "u1"}
	if got := subject.String(); got != "direct:u1" {
		t.Errorf("DirectSubject.String() = %q, want %q", got, "direct:u1")
	}
}

func TestGroupSubject_String(t *testing.T) {
	subject := GroupSubject{GroupID: "g1"}
	if got := subject.String(); got != "group:g1" {
		t.Errorf("GroupSubject.String() = %q, want %q", got, "group:g1")
	}
}

func TestGroupUserSubject_String(t *testing.T) {
	subject := GroupUserSubject{GroupID: "g1", UserID: "u1"}
	if got := subject.String(); got != "group:g1:user:u1" {
		t.Errorf("GroupUserSubject.String() = %q, want %q", got, "group:g1:user:u1")
	}
}

// ──────────────────────────── SessionScope String 测试 ────────────────────────────

func TestSessionScope_String_无Subject(t *testing.T) {
	scope := SessionScope{Scope: MainScope{}}
	if got := scope.String(); got != "main" {
		t.Errorf("SessionScope.String() = %q, want %q", got, "main")
	}
}

func TestSessionScope_String_有DirectSubject(t *testing.T) {
	scope := SessionScope{Scope: MainScope{}, Subject: DirectSubject{UserID: "u1"}}
	if got := scope.String(); got != "main:direct:u1" {
		t.Errorf("SessionScope.String() = %q, want %q", got, "main:direct:u1")
	}
}

func TestSessionScope_String_有GroupSubject(t *testing.T) {
	scope := SessionScope{Scope: MainScope{}, Subject: GroupSubject{GroupID: "g1"}}
	if got := scope.String(); got != "main:group:g1" {
		t.Errorf("SessionScope.String() = %q, want %q", got, "main:group:g1")
	}
}

func TestSessionScope_String_有GroupUserSubject(t *testing.T) {
	scope := SessionScope{Scope: MainScope{}, Subject: GroupUserSubject{GroupID: "g1", UserID: "u1"}}
	if got := scope.String(); got != "main:group:g1:user:u1" {
		t.Errorf("SessionScope.String() = %q, want %q", got, "main:group:g1:user:u1")
	}
}

// ──────────────────────────── ParseSessionScope 测试 ────────────────────────────

func TestParseSessionScope_仅Main(t *testing.T) {
	scope, err := ParseSessionScope("main")
	if err != nil {
		t.Fatalf("ParseSessionScope() 返回错误: %v", err)
	}
	if _, ok := scope.Scope.(MainScope); !ok {
		t.Errorf("ParseSessionScope() Scope 类型错误，期望 MainScope")
	}
	if scope.Subject != nil {
		t.Errorf("ParseSessionScope() Subject 应为 nil")
	}
}

func TestParseSessionScope_DirectSubject(t *testing.T) {
	scope, err := ParseSessionScope("main:direct:u1")
	if err != nil {
		t.Fatalf("ParseSessionScope() 返回错误: %v", err)
	}
	ds, ok := scope.Subject.(DirectSubject)
	if !ok {
		t.Fatalf("ParseSessionScope() Subject 类型错误，期望 DirectSubject")
	}
	if ds.UserID != "u1" {
		t.Errorf("DirectSubject.UserID = %q, want %q", ds.UserID, "u1")
	}
}

func TestParseSessionScope_GroupSubject(t *testing.T) {
	scope, err := ParseSessionScope("main:group:g1")
	if err != nil {
		t.Fatalf("ParseSessionScope() 返回错误: %v", err)
	}
	gs, ok := scope.Subject.(GroupSubject)
	if !ok {
		t.Fatalf("ParseSessionScope() Subject 类型错误，期望 GroupSubject")
	}
	if gs.GroupID != "g1" {
		t.Errorf("GroupSubject.GroupID = %q, want %q", gs.GroupID, "g1")
	}
}

func TestParseSessionScope_GroupUserSubject(t *testing.T) {
	scope, err := ParseSessionScope("main:group:g1:user:u1")
	if err != nil {
		t.Fatalf("ParseSessionScope() 返回错误: %v", err)
	}
	gu, ok := scope.Subject.(GroupUserSubject)
	if !ok {
		t.Fatalf("ParseSessionScope() Subject 类型错误，期望 GroupUserSubject")
	}
	if gu.GroupID != "g1" {
		t.Errorf("GroupUserSubject.GroupID = %q, want %q", gu.GroupID, "g1")
	}
	if gu.UserID != "u1" {
		t.Errorf("GroupUserSubject.UserID = %q, want %q", gu.UserID, "u1")
	}
}

func TestParseSessionScope_未知Scope(t *testing.T) {
	_, err := ParseSessionScope("unknown")
	if err == nil {
		t.Errorf("ParseSessionScope() 期望返回错误，但返回 nil")
	}
}

func TestParseSessionScope_未知Subject(t *testing.T) {
	_, err := ParseSessionScope("main:other:value")
	if err == nil {
		t.Errorf("ParseSessionScope() 期望返回错误，但返回 nil")
	}
}

// ──────────────────────────── ParseSessionScopeKey 测试 ────────────────────────────

func TestParseSessionScopeKey_Direct(t *testing.T) {
	key, err := ParseSessionScopeKey("agent:a1:main:direct:u1")
	if err != nil {
		t.Fatalf("ParseSessionScopeKey() 返回错误: %v", err)
	}
	if key.AgentID != "a1" {
		t.Errorf("AgentID = %q, want %q", key.AgentID, "a1")
	}
	ds, ok := key.SessionScope.Subject.(DirectSubject)
	if !ok {
		t.Fatalf("Subject 类型错误，期望 DirectSubject")
	}
	if ds.UserID != "u1" {
		t.Errorf("DirectSubject.UserID = %q, want %q", ds.UserID, "u1")
	}
}

func TestParseSessionScopeKey_仅Main(t *testing.T) {
	key, err := ParseSessionScopeKey("agent:a1:main")
	if err != nil {
		t.Fatalf("ParseSessionScopeKey() 返回错误: %v", err)
	}
	if key.AgentID != "a1" {
		t.Errorf("AgentID = %q, want %q", key.AgentID, "a1")
	}
	if key.SessionScope.Subject != nil {
		t.Errorf("Subject 应为 nil")
	}
}

func TestParseSessionScopeKey_缺少Agent前缀(t *testing.T) {
	_, err := ParseSessionScopeKey("invalid")
	if err == nil {
		t.Errorf("ParseSessionScopeKey() 期望返回错误，但返回 nil")
	}
}

func TestParseSessionScopeKey_缺少AgentID(t *testing.T) {
	_, err := ParseSessionScopeKey("agent:")
	if err == nil {
		t.Errorf("ParseSessionScopeKey() 期望返回错误，但返回 nil")
	}
}

// ──────────────────────────── 等值比较测试 ────────────────────────────

func TestSessionScope_等值比较(t *testing.T) {
	scope1 := SessionScope{Scope: MainScope{}, Subject: DirectSubject{UserID: "u1"}}
	scope2 := SessionScope{Scope: MainScope{}, Subject: DirectSubject{UserID: "u1"}}
	if scope1.String() != scope2.String() {
		t.Errorf("相同 SessionScope 的 String() 应相等")
	}
}

func TestSessionScopeKey_等值比较(t *testing.T) {
	key1 := SessionScopeKey{AgentID: "a1", SessionScope: SessionScope{Scope: MainScope{}}}
	key2 := SessionScopeKey{AgentID: "a1", SessionScope: SessionScope{Scope: MainScope{}}}
	if key1.String() != key2.String() {
		t.Errorf("相同 SessionScopeKey 的 String() 应相等")
	}
}

// ──────────────────────────── 往返测试 ────────────────────────────

func TestSessionScope_往返解析(t *testing.T) {
	cases := []SessionScope{
		{Scope: MainScope{}},
		{Scope: MainScope{}, Subject: DirectSubject{UserID: "u1"}},
		{Scope: MainScope{}, Subject: GroupSubject{GroupID: "g1"}},
		{Scope: MainScope{}, Subject: GroupUserSubject{GroupID: "g1", UserID: "u1"}},
	}
	for _, original := range cases {
		parsed, err := ParseSessionScope(original.String())
		if err != nil {
			t.Errorf("ParseSessionScope(%q) 返回错误: %v", original.String(), err)
			continue
		}
		if parsed.String() != original.String() {
			t.Errorf("往返不匹配: original=%q, parsed=%q", original.String(), parsed.String())
		}
	}
}

func TestSessionScopeKey_往返解析(t *testing.T) {
	cases := []SessionScopeKey{
		{AgentID: "a1", SessionScope: SessionScope{Scope: MainScope{}}},
		{AgentID: "a1", SessionScope: SessionScope{Scope: MainScope{}, Subject: DirectSubject{UserID: "u1"}}},
	}
	for _, original := range cases {
		parsed, err := ParseSessionScopeKey(original.String())
		if err != nil {
			t.Errorf("ParseSessionScopeKey(%q) 返回错误: %v", original.String(), err)
			continue
		}
		if parsed.String() != original.String() {
			t.Errorf("往返不匹配: original=%q, parsed=%q", original.String(), parsed.String())
		}
	}
}
