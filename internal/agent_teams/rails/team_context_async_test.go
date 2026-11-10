package rails_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/rails"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

// fakeAsyncToolHost 最小 AsyncToolHost 实现。
type fakeAsyncToolHost struct{}

func (f *fakeAsyncToolHost) LaunchAsyncTool(string, func(context.Context) (any, error), tools.AsyncToolLaunchOptions) error {
	return nil
}
func (f *fakeAsyncToolHost) SessionID() string                           { return "" }
func (f *fakeAsyncToolHost) SetAsyncToolOutputDirResolver(func() string) {}

// TestGetAsyncToolHost_可取回与缺失 验证句柄读取、缺失、nil ctx 与类型不符。
func TestGetAsyncToolHost_可取回与缺失(t *testing.T) {
	host := &fakeAsyncToolHost{}
	ctx := &hschema.BuildContext{Extras: map[string]any{tools.TeamHandleAsyncToolHost: host}}
	assert.Same(t, host, rails.GetAsyncToolHost(ctx))

	assert.Nil(t, rails.GetAsyncToolHost(&hschema.BuildContext{Extras: map[string]any{}}))
	assert.Nil(t, rails.GetAsyncToolHost(nil))

	wrong := &hschema.BuildContext{Extras: map[string]any{tools.TeamHandleAsyncToolHost: "wrong"}}
	assert.Nil(t, rails.GetAsyncToolHost(wrong))
}
