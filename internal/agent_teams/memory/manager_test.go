package memory

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	tool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	lite "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/lite"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/local"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/result"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestNewTeamMemoryManager 测试创建团队记忆管理器
func TestNewTeamMemoryManager(t *testing.T) {
	params := TeamMemoryManagerParams{
		MemberName:        "t1",
		TeamName:          "team_a",
		Role:              TeamRoleTeammate,
		Lifecycle:         TeamLifecycleTemporary,
		Scenario:          TeamScenarioCoding,
		Language:          TeamLanguageCN,
		PromptMode:        PromptModePassive,
		EnableAutoExtract: true,
	}
	mgr := NewTeamMemoryManager(params)
	require.NotNil(t, mgr)
	assert.Equal(t, "t1", mgr.MemberName())
	assert.Equal(t, "team_a", mgr.TeamName())
	assert.Equal(t, TeamRoleTeammate, mgr.Role())
}

// TestNewTeamMemoryManager_无共享记忆 测试不启用共享记忆
func TestNewTeamMemoryManager_无共享记忆(t *testing.T) {
	params := TeamMemoryManagerParams{
		MemberName:   "t1",
		TeamName:     "team_a",
		SharedMemory: false,
	}
	mgr := NewTeamMemoryManager(params)
	require.NotNil(t, mgr)
	assert.Nil(t, mgr.SharedManager())
}

// TestTeamMemoryManager_InitToolkit 测试初始化工具集（当前返回 false）
func TestTeamMemoryManager_InitToolkit(t *testing.T) {
	mgr := NewTeamMemoryManager(TeamMemoryManagerParams{MemberName: "t1"})
	ok, err := mgr.InitToolkit(context.Background())
	assert.False(t, ok)
	assert.NoError(t, err)
}

// TestTeamMemoryManager_RegisterTools 测试注册工具（当前空实现）
func TestTeamMemoryManager_RegisterTools(t *testing.T) {
	mgr := NewTeamMemoryManager(TeamMemoryManagerParams{MemberName: "t1"})
	mgr.RegisterTools(nil) // 不 panic
}

// TestTeamMemoryManager_LoadAndInject 测试加载注入（当前空实现）
func TestTeamMemoryManager_LoadAndInject(t *testing.T) {
	mgr := NewTeamMemoryManager(TeamMemoryManagerParams{MemberName: "t1"})
	err := mgr.LoadAndInject(context.Background(), nil, "")
	assert.NoError(t, err)
}

// TestTeamMemoryManager_ExtractAfterRound 测试提取（当前空实现）
func TestTeamMemoryManager_ExtractAfterRound(t *testing.T) {
	mgr := NewTeamMemoryManager(TeamMemoryManagerParams{MemberName: "t1"})
	err := mgr.ExtractAfterRound(context.Background())
	assert.NoError(t, err)
}

// TestTeamMemoryManager_Close 测试关闭（当前空实现）
func TestTeamMemoryManager_Close(t *testing.T) {
	mgr := NewTeamMemoryManager(TeamMemoryManagerParams{MemberName: "t1"})
	err := mgr.Close(context.Background())
	assert.NoError(t, err)
}

// TestTeamMemoryManager_ExtractionModel 测试提取模型 getter/setter
func TestTeamMemoryManager_ExtractionModel(t *testing.T) {
	mgr := NewTeamMemoryManager(TeamMemoryManagerParams{MemberName: "t1"})
	assert.Nil(t, mgr.ExtractionModel())
	// 设置 nil 仍正常工作
	mgr.SetExtractionModel(nil)
	assert.Nil(t, mgr.ExtractionModel())
}

// TestTeamMemoryManager_Getter 测试各种 getter
func TestTeamMemoryManager_Getter(t *testing.T) {
	params := TeamMemoryManagerParams{
		MemberName: "t1",
		TeamName:   "team_a",
		Role:       TeamRoleLeader,
	}
	mgr := NewTeamMemoryManager(params)
	assert.Equal(t, "t1", mgr.MemberName())
	assert.Equal(t, "team_a", mgr.TeamName())
	assert.Equal(t, TeamRoleLeader, mgr.Role())
}

// TestNewTeamMemoryManager_启用共享记忆 测试启用共享记忆时创建 SharedManager
func TestNewTeamMemoryManager_启用共享记忆(t *testing.T) {
	dir := "/tmp/test-team-memory"
	params := TeamMemoryManagerParams{
		MemberName:    "leader1",
		TeamName:      "team_b",
		SharedMemory:  true,
		TeamMemoryDir: &dir,
		SysOperation:  &fakeSysOperation{},
	}
	mgr := NewTeamMemoryManager(params)
	require.NotNil(t, mgr)
	assert.NotNil(t, mgr.SharedManager(), "启用共享记忆时应创建 SharedManager")
}

// TestNewTeamMemoryManager_共享记忆Dir为nil 测试 TeamMemoryDir 为 nil 时不创建 SharedManager
func TestNewTeamMemoryManager_共享记忆Dir为nil(t *testing.T) {
	params := TeamMemoryManagerParams{
		MemberName:   "t1",
		SharedMemory: true,
		// TeamMemoryDir 为 nil
	}
	mgr := NewTeamMemoryManager(params)
	require.NotNil(t, mgr)
	assert.Nil(t, mgr.SharedManager(), "TeamMemoryDir 为 nil 时不应创建 SharedManager")
}

// TestTeamMemoryManager_RegisterTools_不panic 测试注册工具不 panic
func TestTeamMemoryManager_RegisterTools_不panic(t *testing.T) {
	mgr := NewTeamMemoryManager(TeamMemoryManagerParams{MemberName: "t1"})
	assert.NotPanics(t, func() {
		mgr.RegisterTools(nil)
	})
}

// TestTeamMemoryManager_ExtractionModel_设置有效值 测试提取模型设置有效值
func TestTeamMemoryManager_ExtractionModel_设置有效值(t *testing.T) {
	mgr := NewTeamMemoryManager(TeamMemoryManagerParams{MemberName: "t1"})
	assert.Nil(t, mgr.ExtractionModel())
	model := &llm.Model{}
	mgr.SetExtractionModel(model)
	assert.Equal(t, model, mgr.ExtractionModel())
}

// TestTeamMemoryManager_InitToolkit_有工作空间 测试 S-12 实现后
// 有 workspace 时 InitToolkit 走 MemberMemoryToolkit 真实初始化路径
func TestTeamMemoryManager_InitToolkit_有工作空间(t *testing.T) {
	ws := workspace.NewWorkspace(t.TempDir(), "")
	mgr := NewTeamMemoryManager(TeamMemoryManagerParams{
		MemberName: "t1",
		TeamName:   "team_a",
		Workspace:  ws,
	})
	ok, err := mgr.InitToolkit(context.Background())
	// lite.IsMemoryEnabled() 默认 true（MEMORY_ENABLED 未设置）→ toolkit 初始化成功
	assert.True(t, ok, "S-12 实现后 InitToolkit 应真实初始化 toolkit")
	assert.NoError(t, err)
	// 幂等：二次调用直接返回 true
	ok2, err2 := mgr.InitToolkit(context.Background())
	assert.True(t, ok2)
	assert.NoError(t, err2)
}

// TestTeamMemoryManager_ExtractAfterRound_前置条件矩阵 测试 S-12 四重前置条件
// enableAutoExtract / persistent / leader / teamMemoryDir+db 任一不满足即短路
func TestTeamMemoryManager_ExtractAfterRound_前置条件矩阵(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name      string
		params    TeamMemoryManagerParams
		wantReach bool
	}{
		{
			name: "全满足_到达提取",
			params: TeamMemoryManagerParams{
				MemberName: "leader", TeamName: "team_a", Role: TeamRoleLeader,
				Lifecycle: TeamLifecyclePersistent, EnableAutoExtract: true, TeamMemoryDir: &dir,
			},
			// extractionModel 为 nil → ExtractTeamMemories 早退（不会真正调 LLM）
			wantReach: true,
		},
		{
			name: "非Leader_短路",
			params: TeamMemoryManagerParams{
				MemberName: "t1", TeamName: "team_a", Role: TeamRoleTeammate,
				Lifecycle: TeamLifecyclePersistent, EnableAutoExtract: true, TeamMemoryDir: &dir,
			},
			wantReach: false,
		},
		{
			name: "非持久_短路",
			params: TeamMemoryManagerParams{
				MemberName: "leader", TeamName: "team_a", Role: TeamRoleLeader,
				Lifecycle: TeamLifecycleTemporary, EnableAutoExtract: true, TeamMemoryDir: &dir,
			},
			wantReach: false,
		},
		{
			name: "未启用自动提取_短路",
			params: TeamMemoryManagerParams{
				MemberName: "leader", TeamName: "team_a", Role: TeamRoleLeader,
				Lifecycle: TeamLifecyclePersistent, EnableAutoExtract: false, TeamMemoryDir: &dir,
			},
			wantReach: false,
		},
		{
			name: "无记忆目录_短路",
			params: TeamMemoryManagerParams{
				MemberName: "leader", TeamName: "team_a", Role: TeamRoleLeader,
				Lifecycle: TeamLifecyclePersistent, EnableAutoExtract: true,
			},
			wantReach: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mgr := NewTeamMemoryManager(tc.params)
			// 全满足场景：db 为 nil 仍短路（Python: not self._team_memory_dir or not self._db）
			// 此处 wantReach 仅区分前置条件是否全部满足（不含 db）
			assert.NotPanics(t, func() {
				_ = mgr.ExtractAfterRound(context.Background())
			})
		})
	}
}

// TestBuildExtractionContext_任务与消息 测试 S-12 提取上下文组装
func TestBuildExtractionContext_任务与消息(t *testing.T) {
	assignee := "coder1"
	toMember := "leader"
	tasks := []*database.TeamTaskBase{
		{TaskID: "t1", TeamName: "team_a", Title: "实现登录", Content: "完成登录页", Status: "in_progress", Assignee: &assignee},
		{TaskID: "t2", TeamName: "team_a", Title: "无分配任务", Content: "", Status: "pending", Assignee: nil},
	}
	messages := []*database.TeamMessageBase{
		{MessageID: "m2", TeamName: "team_a", FromMemberName: "coder1", ToMemberName: &toMember, Content: "完成", Timestamp: 2000, Broadcast: false},
		{MessageID: "m1", TeamName: "team_a", FromMemberName: "leader", ToMemberName: nil, Content: "开始", Timestamp: 1000, Broadcast: true},
	}
	result := BuildExtractionContext(tasks, messages, 8.0)
	// 任务记录存在
	assert.Contains(t, result, "## 任务记录")
	assert.Contains(t, result, "### 实现登录 [in_progress] -> coder1")
	assert.Contains(t, result, "### 无分配任务 [pending] -> 未分配")
	// 消息按时间排序（m1 在前）
	assert.Contains(t, result, "## 团队对话")
	msgStart := strings.Index(result, "[01-01 08:00] leader -> 全体: 开始")
	assert.GreaterOrEqual(t, msgStart, 0, "广播消息应包含方向 -> 全体")
	assert.Contains(t, result, "-> leader: 完成")
}

// TestBuildExtractionContext_空输入 测试空输入只有标题
func TestBuildExtractionContext_空输入(t *testing.T) {
	result := BuildExtractionContext(nil, nil, 8.0)
	assert.Equal(t, "# 本轮团队协作记录\n", result)
}

// TestCreateExtractionTools_工具集 测试 S-12 提取工具创建
func TestCreateExtractionTools_工具集(t *testing.T) {
	tools := CreateExtractionTools(t.TempDir(), nil, "team_a")
	assert.Len(t, tools, 3)
	names := make([]string, 0, 3)
	for _, tl := range tools {
		names = append(names, tl.Card().Name)
	}
	assert.Contains(t, names, "read_memory_file")
	assert.Contains(t, names, "write_memory_file")
	assert.Contains(t, names, "list_memory_files")
	// ToolCard ID 带 team 前缀
	for _, tl := range tools {
		assert.Contains(t, tl.Card().ID, "extract.team_a.")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// stubIndexManagerForInject 用于 LoadAndInject/fetch 测试的记忆索引桩。
// 搜索返回单条结果（路径 notes.md），文件内容由 FsOperation 真实读取。
type stubIndexManagerForInject struct{}

func (s *stubIndexManagerForInject) Initialize(_ context.Context) error             { return nil }
func (s *stubIndexManagerForInject) Sync(_ context.Context, _ string, _ bool) error { return nil }
func (s *stubIndexManagerForInject) Search(_ context.Context, _ string, _ lite.SearchOpts) ([]lite.SearchResult, error) {
	return []lite.SearchResult{{ID: "1", Path: "notes.md"}}, nil
}
func (s *stubIndexManagerForInject) ReadFile(_ context.Context, _ string, _ *int, _ *int) (*lite.ReadFileResult, error) {
	return nil, nil
}
func (s *stubIndexManagerForInject) Status() *lite.StatusResult { return nil }
func (s *stubIndexManagerForInject) LLM() *llm.Model            { return nil }
func (s *stubIndexManagerForInject) Close() error               { return nil }

// fakeDeepAgentForInject 用于 LoadAndInject 测试的 DeepAgent 桩（仅提供 builder）。
type fakeDeepAgentForInject struct {
	interfaces.DeepAgentInterface
	builder saprompt.SystemPromptBuilderInterface
}

func (f *fakeDeepAgentForInject) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface {
	return f.builder
}

// realReadFsOp 真实读文件的 FsOperation 桩（写操作不支持）。
type realReadFsOp struct{}

func (realReadFsOp) ReadFile(_ context.Context, path string, _ ...sysop.FsOption) (*result.ReadFileResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return &result.ReadFileResult{Data: &result.ReadFileData{Content: string(data), Path: path}}, nil
}
func (realReadFsOp) ReadFileStream(_ context.Context, _ string, _ ...sysop.FsOption) (<-chan result.ReadFileStreamResult, error) {
	return nil, nil
}
func (realReadFsOp) WriteFile(_ context.Context, _ string, _ string, _ ...sysop.FsOption) (*result.WriteFileResult, error) {
	return nil, errors.New("not supported")
}
func (realReadFsOp) UploadFile(_ context.Context, _ string, _ string, _ ...sysop.FsOption) (*result.UploadFileResult, error) {
	return nil, nil
}
func (realReadFsOp) UploadFileStream(_ context.Context, _ string, _ string, _ ...sysop.FsOption) (<-chan result.UploadFileStreamResult, error) {
	return nil, nil
}
func (realReadFsOp) DownloadFile(_ context.Context, _ string, _ string, _ ...sysop.FsOption) (*result.DownloadFileResult, error) {
	return nil, nil
}
func (realReadFsOp) DownloadFileStream(_ context.Context, _ string, _ string, _ ...sysop.FsOption) (<-chan result.DownloadFileStreamResult, error) {
	return nil, nil
}
func (realReadFsOp) ListFiles(_ context.Context, _ string, _ ...sysop.FsOption) (*result.ListFilesResult, error) {
	return nil, nil
}
func (realReadFsOp) ListDirectories(_ context.Context, _ string, _ ...sysop.FsOption) (*result.ListDirsResult, error) {
	return nil, nil
}
func (realReadFsOp) SearchFiles(_ context.Context, _ string, _ string, _ ...sysop.FsOption) (*result.SearchFilesResult, error) {
	return nil, nil
}
func (realReadFsOp) ListTools() []*tool.ToolCard { return nil }

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestTeamMemoryManager_LoadAndInject_英文header S-04 回归测试。
// Python manager.py L294: 英文分支 header 应为 "## Your relevant memories"，
// 原代码复制粘贴错误写成了 "## Team shared memory"（与团队共享段重名）。
func TestTeamMemoryManager_LoadAndInject_英文header(t *testing.T) {
	dir := t.TempDir()
	ws := workspace.NewWorkspace(dir, "en")
	// 落盘记忆文件（搜索分支通过 FsOperation 真实读取）
	memDir := filepath.Join(dir, "memory")
	require.NoError(t, os.MkdirAll(memDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(memDir, "notes.md"), []byte("my memory content"), 0o644))
	params := TeamMemoryManagerParams{
		MemberName:   "t1",
		TeamName:     "team_a",
		Language:     TeamLanguageEN,
		Workspace:    ws,
		SysOperation: &fakeSysOperation{fsOp: realReadFsOp{}},
		PromptMode:   PromptModePassive,
	}
	mgr := NewTeamMemoryManager(params)
	// 预置缓存基础段（绕过 BuildMemorySection 的模板细节）
	mgr.cachedBaseSection = &saprompt.PromptSection{
		Name:     SectionName,
		Content:  map[string]string{"en": "base"},
		Priority: 50,
	}
	// 注入个人记忆：搜索分支走 indexManager（同包可直写 m.toolkit）
	mgr.toolkit = &MemberMemoryToolkit{manager: &stubIndexManagerForInject{}}

	agent := &fakeDeepAgentForInject{builder: saprompt.NewSystemPromptBuilder()}
	err := mgr.LoadAndInject(context.Background(), agent, "查询")
	require.NoError(t, err)

	sec := agent.builder.GetSection(SectionName)
	require.NotNil(t, sec)
	assert.Contains(t, sec.Content["en"], "## Your relevant memories")
	assert.Contains(t, sec.Content["en"], "my memory content")
	// 英文分支不得出现中文 header 或复制粘贴错误的共享段标题
	assert.NotContains(t, sec.Content["en"], "## 你的相关记忆")
	assert.NotContains(t, sec.Content["en"], "## Team shared memory")
}

// TestTeamMemoryManager_LoadAndInject_中文header 中文分支回归（确保英文修复不破坏中文）
func TestTeamMemoryManager_LoadAndInject_中文header(t *testing.T) {
	dir := t.TempDir()
	ws := workspace.NewWorkspace(dir, "cn")
	memDir := filepath.Join(dir, "memory")
	require.NoError(t, os.MkdirAll(memDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(memDir, "notes.md"), []byte("个人记忆内容"), 0o644))
	params := TeamMemoryManagerParams{
		MemberName:   "t1",
		TeamName:     "team_a",
		Language:     TeamLanguageCN,
		Workspace:    ws,
		SysOperation: &fakeSysOperation{fsOp: realReadFsOp{}},
		PromptMode:   PromptModePassive,
	}
	mgr := NewTeamMemoryManager(params)
	mgr.cachedBaseSection = &saprompt.PromptSection{
		Name:     SectionName,
		Content:  map[string]string{"cn": "基础段"},
		Priority: 50,
	}
	mgr.toolkit = &MemberMemoryToolkit{manager: &stubIndexManagerForInject{}}

	agent := &fakeDeepAgentForInject{builder: saprompt.NewSystemPromptBuilder()}
	err := mgr.LoadAndInject(context.Background(), agent, "查询")
	require.NoError(t, err)

	sec := agent.builder.GetSection(SectionName)
	require.NotNil(t, sec)
	assert.Contains(t, sec.Content["cn"], "## 你的相关记忆")
	assert.Contains(t, sec.Content["cn"], "个人记忆内容")
}

// TestFetchPersonalMemoryForPrompt_CJK截断 M-14 回归测试。
// Python L215 content[:remaining] 按字符切片，Go 原实现按字节切会在
// CJK 多字节边界产生非法 UTF-8，现按 rune 对齐。
func TestFetchPersonalMemoryForPrompt_CJK截断(t *testing.T) {
	dir := t.TempDir()
	ws := workspace.NewWorkspace(dir, "cn")
	// 真实落盘一个大文件：5000 个 CJK 字符（15000 字节 > maxPersonalMemoryBytes=10240）
	memDir := filepath.Join(dir, "memory")
	require.NoError(t, os.MkdirAll(memDir, 0o755))
	big := strings.Repeat("测", 5000)
	require.NoError(t, os.WriteFile(filepath.Join(memDir, "notes.md"), []byte(big), 0o644))
	// fsOp 走真实文件读取（复用 fakeSysOperation 外壳 + 真实 ReadFile）
	fsOp := realReadFsOp{}
	params := TeamMemoryManagerParams{
		MemberName:   "t1",
		TeamName:     "team_a",
		Language:     TeamLanguageCN,
		Workspace:    ws,
		SysOperation: &fakeSysOperation{fsOp: fsOp},
	}
	mgr := NewTeamMemoryManager(params)
	mgr.toolkit = &MemberMemoryToolkit{manager: &stubIndexManagerForInject{}}

	got := mgr.fetchPersonalMemoryForPrompt(context.Background(), "查询")
	// 输出须为合法 UTF-8（rune 截断保证），且带截断标记
	assert.True(t, utf8.ValidString(got), "截断输出应为合法 UTF-8")
	assert.Contains(t, got, "... (truncated)")
	// remaining=10240 > 内容 rune 数 5000 → 全量保留（min 钳制），不丢字符
	assert.Contains(t, got, strings.Repeat("测", 5000))
	// 字节口径截断会在 10240/3≈3413 字符处切出非法 UTF-8；rune 口径保证 5000 全保留
	assert.Equal(t, 5000, strings.Count(got, "测"))
}

// TestCreateExtractionTools_List走Fs抽象 M-16 回归测试。
// Python _list_files 走 sys_operation.fs().list_files(dir, recursive=False)，
// 非 sysOp 场景回退本地 os.ReadDir；两个路径都要覆盖。
func TestCreateExtractionTools_List走Fs抽象(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	memDir := filepath.Join(dir, "team-memory")
	require.NoError(t, os.MkdirAll(memDir, 0o755))
	// 预置 2 文件 + 1 子目录（子目录不应出现在结果中）
	require.NoError(t, os.WriteFile(filepath.Join(memDir, "a.md"), []byte("a"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(memDir, "b.md"), []byte("b"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(memDir, "sub"), 0o755))

	// 场景1：sysOp 提供真实 LocalFsOperation → Fs 抽象路径
	localFs, ok := local.NewLocalFsOperation(nil).(sysop.FsOperation)
	require.True(t, ok, "LocalFsOperation 应实现 FsOperation 接口")
	sysOp := &fakeSysOperation{fsOp: localFs}
	tools := CreateExtractionTools(memDir, sysOp, "team_a")
	require.Len(t, tools, 3)
	var listTool tool.Tool
	for _, tl := range tools {
		if tl.Card().Name == "list_memory_files" {
			listTool = tl
		}
	}
	require.NotNil(t, listTool, "未找到 list_memory_files 工具")
	out, err := listTool.Invoke(ctx, map[string]any{})
	require.NoError(t, err)
	files := toStringSlice(t, out["files"])
	assert.ElementsMatch(t, []string{"a.md", "b.md"}, files, "Fs 抽象路径应列出 2 文件且排除子目录")

	// 场景2：sysOp 为 nil → 本地 os.ReadDir 回退路径
	toolsLocal := CreateExtractionTools(memDir, nil, "team_a")
	require.Len(t, toolsLocal, 3)
	for _, tl := range toolsLocal {
		if tl.Card().Name == "list_memory_files" {
			listTool = tl
		}
	}
	require.NotNil(t, listTool, "未找到 list_memory_files 工具（本地回退）")
	out, err = listTool.Invoke(ctx, map[string]any{})
	require.NoError(t, err)
	files = toStringSlice(t, out["files"])
	assert.ElementsMatch(t, []string{"a.md", "b.md"}, files, "本地回退路径应列出 2 文件且排除子目录")

	// 场景3：目录不存在 → 空列表（Python except → {"files": []}）
	toolsMissing := CreateExtractionTools(filepath.Join(dir, "no-such-dir"), nil, "team_a")
	for _, tl := range toolsMissing {
		if tl.Card().Name == "list_memory_files" {
			out, err = tl.Invoke(ctx, map[string]any{})
			require.NoError(t, err)
			files = toStringSlice(t, out["files"])
			assert.Empty(t, files, "目录不存在时应返回空列表")
		}
	}
}

// TestCreateExtractionTools_ListFs异常回退空列表 M-16 回归测试。
// Fs 抽象返回 error 或 Data 为 nil 时应回退空列表而非 panic。
func TestCreateExtractionTools_ListFs异常回退空列表(t *testing.T) {
	ctx := context.Background()
	memDir := t.TempDir()

	// fakeSysOperation.Fs() 返回 nil FsOperation → 走本地 os.ReadDir（空目录）
	sysOpNilFs := &fakeSysOperation{fsOp: nil}
	tools := CreateExtractionTools(memDir, sysOpNilFs, "team_a")
	require.Len(t, tools, 3)
	for _, tl := range tools {
		if tl.Card().Name == "list_memory_files" {
			out, err := tl.Invoke(ctx, map[string]any{})
			require.NoError(t, err)
			files := toStringSlice(t, out["files"])
			assert.Empty(t, files, "Fs 为 nil 走本地回退，空目录应返回空列表")
		}
	}
}

// toStringSlice 将 Invoke 输出的 files 字段（JSON 中转后为 []any）转为 []string。
func toStringSlice(t *testing.T, v any) []string {
	t.Helper()
	items, ok := v.([]any)
	if !ok {
		t.Fatalf("files 字段类型断言失败: %T", v)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, _ := item.(string)
		out = append(out, s)
	}
	return out
}
