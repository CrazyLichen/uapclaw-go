package tools

import (
	"context"
	"testing"
)

// TestAsyncToolHost_接口形状 验证接口可被最小实现满足。
func TestAsyncToolHost_接口形状(t *testing.T) {
	var _ AsyncToolHost = (*fakeAsyncHost)(nil)
}

// fakeAsyncHost 最小实现，仅用于编译期形状校验。
type fakeAsyncHost struct{}

func (f *fakeAsyncHost) LaunchAsyncTool(string, func(context.Context) (any, error), AsyncToolLaunchOptions) error {
	return nil
}
func (f *fakeAsyncHost) SessionID() string                           { return "" }
func (f *fakeAsyncHost) SetAsyncToolOutputDirResolver(func() string) {}
