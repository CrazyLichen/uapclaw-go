package main

// 10.4.6 ACP CLI 冒烟入口测试。

import (
	"errors"
	"fmt"
	"testing"
)

// TestExitCodeFor 退出码解析全分支（0/1/2/130/未知/包装）。
func TestExitCodeFor(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"配置错", &acpChatExitError{code: 2}, 2},
		{"会话错", &acpChatExitError{code: 1}, 1},
		{"中断", &acpChatExitError{code: 130}, 130},
		{"包装后仍可取码", fmt.Errorf("wrap: %w", &acpChatExitError{code: 2}), 2},
		{"普通错误回落 1", errors.New("boom"), 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := exitCodeFor(tc.err); got != tc.want {
				t.Errorf("exitCodeFor = %d, want %d", got, tc.want)
			}
		})
	}
}
