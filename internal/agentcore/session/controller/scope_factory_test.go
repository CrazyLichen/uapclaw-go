package controller

import "testing"

func TestScopeFactory_CreateMain(t *testing.T) {
	f := SessionScopeFactory{}
	scope := f.CreateMain()
	if got := scope.String(); got != "main" {
		t.Errorf("CreateMain().String() = %q, want %q", got, "main")
	}
	if scope.Subject != nil {
		t.Errorf("CreateMain() Subject 应为 nil")
	}
}

func TestScopeFactory_CreateDirect(t *testing.T) {
	f := SessionScopeFactory{}
	scope := f.CreateDirect("u1")
	if got := scope.String(); got != "main:direct:u1" {
		t.Errorf("CreateDirect().String() = %q, want %q", got, "main:direct:u1")
	}
}

func TestScopeFactory_CreateGroup(t *testing.T) {
	f := SessionScopeFactory{}
	scope := f.CreateGroup("g1")
	if got := scope.String(); got != "main:group:g1" {
		t.Errorf("CreateGroup().String() = %q, want %q", got, "main:group:g1")
	}
}

func TestScopeFactory_CreateGroupUser(t *testing.T) {
	f := SessionScopeFactory{}
	scope := f.CreateGroupUser("g1", "u1")
	if got := scope.String(); got != "main:group:g1:user:u1" {
		t.Errorf("CreateGroupUser().String() = %q, want %q", got, "main:group:g1:user:u1")
	}
}

func TestScopeFactory_FromString(t *testing.T) {
	f := SessionScopeFactory{}
	scope, err := f.FromString("main:direct:u1")
	if err != nil {
		t.Fatalf("FromString() 返回错误: %v", err)
	}
	expected := f.CreateDirect("u1")
	if scope.String() != expected.String() {
		t.Errorf("FromString() = %q, want %q", scope.String(), expected.String())
	}
}
