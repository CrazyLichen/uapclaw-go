package runtime

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 测试替身 ────────────────────────────

// fakeCard 工具卡替身。
type fakeCard struct{ name string }

// ToolName 返回工具名。
func (c fakeCard) ToolName() string { return c.name }

// fakeInfo 工具信息替身。
type fakeInfo struct{ name string }

// ToolName 返回工具名。
func (i fakeInfo) ToolName() string { return i.name }

// fakeCall 工具调用替身。
type fakeCall struct{ name string }

// ToolName 返回工具名。
func (c fakeCall) ToolName() string { return c.name }

// fakeToolManager 工具管理器替身。
type fakeToolManager struct {
	// cards 发现结果
	cards []ToolCard
	// infos 信息结果
	infos []ToolInfo
	// listErr 信息查询错误
	listErr error
	// executed 已执行的工具调用
	executed any
}

// List 返回预设工具卡。
func (m *fakeToolManager) List() []ToolCard { return m.cards }

// ListToolInfo 返回预设信息。
func (m *fakeToolManager) ListToolInfo() ([]ToolInfo, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.infos, nil
}

// Execute 记录并返回成功。
func (m *fakeToolManager) Execute(_ any, toolCall any, _ any) (any, error) {
	m.executed = toolCall
	return "executed", nil
}

// fakeHolder 白名单持有者替身。
type fakeHolder struct {
	// allowed 已安装白名单
	allowed map[string]struct{}
}

// ToolAllowlist 返回已安装白名单。
func (h *fakeHolder) ToolAllowlist() map[string]struct{} { return h.allowed }

// SetToolAllowlist 记录白名单。
func (h *fakeHolder) SetToolAllowlist(allowed map[string]struct{}) { h.allowed = allowed }

// ──────────────────────────── 提示词 ────────────────────────────

// TestApplyToolPolicyPrompt_空名单追加禁用说明 上游语义：名单为空才追加提示。
// Python: tool_allowlist.py:20-26
func TestApplyToolPolicyPrompt_空名单追加禁用说明(t *testing.T) {
	got := ApplyToolPolicyPrompt("base", nil)
	require.Contains(t, got, "All tools are disabled for this Agent")
	require.Contains(t, got, "Do not call or claim access")
	require.Contains(t, got, "Answer using only the conversation and your knowledge")

	empty := ApplyToolPolicyPrompt("base", []string{})
	require.Contains(t, empty, "All tools are disabled for this Agent")
}

// TestApplyToolPolicyPrompt_非空名单原样返回 名单非空时不追加任何内容。
// Python: tool_allowlist.py:20-21 if tool_names: return system_prompt
func TestApplyToolPolicyPrompt_非空名单原样返回(t *testing.T) {
	got := ApplyToolPolicyPrompt("base prompt", []string{"read_file"})
	require.Equal(t, "base prompt", got)
	require.NotContains(t, got, "All tools are disabled")
}

// TestApplyToolPolicyPrompt_保留原提示词 追加而非替换。
// Python: tool_allowlist.py:22 system_prompt + (...)
func TestApplyToolPolicyPrompt_保留原提示词(t *testing.T) {
	got := ApplyToolPolicyPrompt("原有系统提示", nil)
	require.Contains(t, got, "原有系统提示")
	require.Contains(t, got, "All tools are disabled")
}

// ──────────────────────────── 安装校验 ────────────────────────────

// TestInstallToolAllowlist_拒绝星号 显式白名单不得含 "*"。
// Python: tool_allowlist.py:33-34
func TestInstallToolAllowlist_拒绝星号(t *testing.T) {
	manager := &fakeToolManager{}
	_, err := InstallToolAllowlist(manager, &fakeHolder{}, []string{"*", "read_file"})
	require.ErrorIs(t, err, ErrToolAllowlistWildcard)
}

// TestInstallToolAllowlist_拒绝重复安装 已安装后再次安装报错。
// Python: tool_allowlist.py:35-36
func TestInstallToolAllowlist_拒绝重复安装(t *testing.T) {
	manager := &fakeToolManager{}
	holder := &fakeHolder{allowed: map[string]struct{}{"read_file": {}}}
	_, err := InstallToolAllowlist(manager, holder, []string{"write_file"})
	require.ErrorIs(t, err, ErrToolAllowlistInstalled)
}

// TestInstallToolAllowlist_空名单可安装 空名单是合法的（表示全部禁用）。
// Python: tool_allowlist.py:32 allowed = frozenset(tool_names)（空集合法）
func TestInstallToolAllowlist_空名单可安装(t *testing.T) {
	manager := &fakeToolManager{cards: []ToolCard{fakeCard{"read_file"}}}
	wrapped, err := InstallToolAllowlist(manager, &fakeHolder{}, nil)
	require.NoError(t, err)
	require.NotNil(t, wrapped)
	require.Empty(t, wrapped.List(), "空名单下所有工具卡被过滤")
}

// ──────────────────────────── 过滤行为 ────────────────────────────

// TestInstallToolAllowlist_过滤发现结果 只保留白名单内工具卡。
// Python: tool_allowlist.py:42-43 limited_list
func TestInstallToolAllowlist_过滤发现结果(t *testing.T) {
	manager := &fakeToolManager{cards: []ToolCard{
		fakeCard{"read_file"}, fakeCard{"write_file"}, fakeCard{"shell"},
	}}
	wrapped, err := InstallToolAllowlist(manager, &fakeHolder{}, []string{"read_file", "shell"})
	require.NoError(t, err)

	cards := wrapped.List()
	require.Len(t, cards, 2)
	require.Equal(t, "read_file", cards[0].ToolName())
	require.Equal(t, "shell", cards[1].ToolName())
}

// TestInstallToolAllowlist_过滤工具信息 只保留白名单内工具信息。
// Python: tool_allowlist.py:45-47 limited_list_tool_info
func TestInstallToolAllowlist_过滤工具信息(t *testing.T) {
	manager := &fakeToolManager{infos: []ToolInfo{
		fakeInfo{"read_file"}, fakeInfo{"write_file"},
	}}
	wrapped, err := InstallToolAllowlist(manager, &fakeHolder{}, []string{"write_file"})
	require.NoError(t, err)

	infos, err := wrapped.ListToolInfo()
	require.NoError(t, err)
	require.Len(t, infos, 1)
	require.Equal(t, "write_file", infos[0].ToolName())
}

// TestInstallToolAllowlist_信息查询错误透传 原始错误原样返回。
// Python: tool_allowlist.py:46 infos = await original_list_tool_info(...)（异常传播）
func TestInstallToolAllowlist_信息查询错误透传(t *testing.T) {
	sentinel := errors.New("info boom")
	manager := &fakeToolManager{listErr: sentinel}
	wrapped, err := InstallToolAllowlist(manager, &fakeHolder{}, []string{"read_file"})
	require.NoError(t, err)

	_, err = wrapped.ListToolInfo()
	require.ErrorIs(t, err, sentinel)
}

// ──────────────────────────── 执行拒绝 ────────────────────────────

// TestExecute_白名单内工具放行 放行时交给原始执行。
// Python: tool_allowlist.py:56 return await original_execute(...)
func TestExecute_白名单内工具放行(t *testing.T) {
	manager := &fakeToolManager{}
	wrapped, err := InstallToolAllowlist(manager, &fakeHolder{}, []string{"read_file"})
	require.NoError(t, err)

	call := fakeCall{"read_file"}
	got, err := wrapped.Execute(nil, call, nil)
	require.NoError(t, err)
	require.Equal(t, "executed", got)
	require.Equal(t, call, manager.executed)
}

// TestExecute_单个越界调用被拒 不在白名单的单个调用报错且不执行。
// Python: tool_allowlist.py:52-55
func TestExecute_单个越界调用被拒(t *testing.T) {
	manager := &fakeToolManager{}
	wrapped, err := InstallToolAllowlist(manager, &fakeHolder{}, []string{"read_file"})
	require.NoError(t, err)

	_, err = wrapped.Execute(nil, fakeCall{"shell"}, nil)
	require.ErrorIs(t, err, ErrToolAllowlistDenied)
	require.Contains(t, err.Error(), "shell")
	require.Nil(t, manager.executed, "拒绝时不得执行")
}

// TestExecute_批量调用任一越界即全拒 列表中有一个越界就整体拒绝。
// Python: tool_allowlist.py:52-56 calls = tool_call if isinstance(list) else [...]
func TestExecute_批量调用任一越界即全拒(t *testing.T) {
	manager := &fakeToolManager{}
	wrapped, err := InstallToolAllowlist(manager, &fakeHolder{}, []string{"read_file"})
	require.NoError(t, err)

	calls := []ToolCall{fakeCall{"read_file"}, fakeCall{"shell"}, fakeCall{"write_file"}}
	_, err = wrapped.Execute(nil, calls, nil)
	require.ErrorIs(t, err, ErrToolAllowlistDenied)
	require.Contains(t, err.Error(), "shell")
	require.Contains(t, err.Error(), "write_file")
	require.Nil(t, manager.executed)
}

// TestExecute_批量调用全部合法放行 全部在白名单内时执行。
func TestExecute_批量调用全部合法放行(t *testing.T) {
	manager := &fakeToolManager{}
	wrapped, err := InstallToolAllowlist(manager, &fakeHolder{}, []string{"read_file", "shell"})
	require.NoError(t, err)

	calls := []ToolCall{fakeCall{"read_file"}, fakeCall{"shell"}}
	_, err = wrapped.Execute(nil, calls, nil)
	require.NoError(t, err)
	require.NotNil(t, manager.executed)
}

// TestInstallToolAllowlist_记录白名单标记 安装后 holder 记下白名单。
// Python: tool_allowlist.py:61 manager.jiuwenswarm_tool_allowlist = allowed
func TestInstallToolAllowlist_记录白名单标记(t *testing.T) {
	holder := &fakeHolder{}
	_, err := InstallToolAllowlist(&fakeToolManager{}, holder, []string{"read_file", "shell"})
	require.NoError(t, err)
	require.NotNil(t, holder.ToolAllowlist())
	require.Len(t, holder.ToolAllowlist(), 2)
	_, ok := holder.ToolAllowlist()["read_file"]
	require.True(t, ok)
}
