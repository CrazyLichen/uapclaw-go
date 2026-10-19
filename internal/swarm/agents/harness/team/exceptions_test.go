package team

import (
	"errors"
	"testing"
)

// TestTeamError_错误消息 各异常类型 Error() 输出与 TeamError 基类匹配。
func TestTeamError_错误消息(t *testing.T) {
	cases := []struct {
		err error
		msg string
	}{
		{NewTeamError("base"), "TeamError: base"},
		{NewTeamCreateError("create failed"), "TeamCreateError: create failed"},
		{NewTeamRecoverError("recover failed"), "TeamRecoverError: recover failed"},
		{NewTeamInteractError("interact failed"), "TeamInteractError: interact failed"},
		{NewTeamConfigError("bad config"), "TeamConfigError: bad config"},
		{NewTeamMonitorError("monitor failed"), "TeamMonitorError: monitor failed"},
		{NewTeamSessionError("session failed"), "TeamSessionError: session failed"},
		{NewTeamStorageError("storage failed"), "TeamStorageError: storage failed"},
	}
	for _, tc := range cases {
		if tc.err.Error() != tc.msg {
			t.Errorf("Error() = %q, want %q", tc.err.Error(), tc.msg)
		}
		// 所有子类同时匹配 TeamError 基类（Unwrap 链，等价 Python isinstance）
		var base *TeamError
		if !errors.As(tc.err, &base) {
			t.Errorf("errors.As TeamError base failed for %T", tc.err)
		}
	}
}

// TestTeamError_子类型精确匹配 errors.As 各子类型独立区分。
func TestTeamError_子类型精确匹配(t *testing.T) {
	if _, ok := any(NewTeamCreateError("x")).(*TeamCreateError); !ok {
		t.Error("NewTeamCreateError should return *TeamCreateError")
	}
	if _, ok := any(NewTeamRecoverError("x")).(*TeamRecoverError); !ok {
		t.Error("NewTeamRecoverError should return *TeamRecoverError")
	}
	if _, ok := any(NewTeamInteractError("x")).(*TeamInteractError); !ok {
		t.Error("NewTeamInteractError should return *TeamInteractError")
	}
	if _, ok := any(NewTeamConfigError("x")).(*TeamConfigError); !ok {
		t.Error("NewTeamConfigError should return *TeamConfigError")
	}
	if _, ok := any(NewTeamMonitorError("x")).(*TeamMonitorError); !ok {
		t.Error("NewTeamMonitorError should return *TeamMonitorError")
	}
	if _, ok := any(NewTeamSessionError("x")).(*TeamSessionError); !ok {
		t.Error("NewTeamSessionError should return *TeamSessionError")
	}
	if _, ok := any(NewTeamStorageError("x")).(*TeamStorageError); !ok {
		t.Error("NewTeamStorageError should return *TeamStorageError")
	}
	// 各子类不匹配兄弟类型
	var ce *TeamConfigError
	if errors.As(error(NewTeamCreateError("x")), &ce) {
		t.Error("TeamCreateError should not match TeamConfigError")
	}
}
