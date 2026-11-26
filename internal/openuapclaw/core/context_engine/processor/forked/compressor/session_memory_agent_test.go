// T13 收尾批：agent_edit 执行体策略验证（受控 AbilityManager 只放行
// edit_file 且目标=notes 路径；配置注入与快照）。
// 对应 Python：session_memory_agent.py:63 (execute) / :122 (_is_allowed_edit_path)。
package compressor

import (
	"context"
	"os"
	"strings"
	"testing"

	llm_schema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	saagents "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/agents"
	saconfig "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/config"
	agentschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
	_ "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/sys_operation/local"
	filesystem "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/tools/filesystem"
)

// newPolicyManagerForTest 构造装配完成的受控管理器（真实 sysop 沙箱 + EditFileTool）。
func newPolicyManagerForTest(t *testing.T) (*SessionMemoryAgent, *SessionMemoryAbilityManager, string) {
	t.Helper()
	ws := t.TempDir()
	sma := NewSessionMemoryAgent(
		nil, nil, nil, nil,
		NewSessionMemoryAgentConfig(),
		ws,
	)
	// 真实 ReActAgent（configureAgent 会 SetAbilityManager/读卡片）
	agent := saagents.NewReActAgent(
		agentschema.NewAgentCard(agentschema.WithAgentName("session_memory_update_agent_test")),
		saconfig.NewReActAgentConfig(),
	)
	sma.configureAgent(agent)
	mgr := sma.AbilityManager()
	if mgr == nil {
		t.Fatal("configureAgent 后应产出受控管理器")
	}
	return sma, mgr, ws
}

// notesCall 构造指向 notes 文件的 edit_file 调用
func notesCall(id string, notesPath string) *llm_schema.ToolCall {
	return llm_schema.NewToolCall(id, "edit_file",
		`{"file_path":"`+notesPath+`","old_string":"hello","new_string":"hello edited"}`)
}

// TestAbilityManager_拒绝非edit_file 只有 edit_file 可执行。
// Python: session_memory_agent.py:74-84。
func TestAbilityManager_拒绝非edit_file(t *testing.T) {
	_, mgr, ws := newPolicyManagerForTest(t)
	mgr.SetAllowedNotesPath(ws + "/memory.md")

	calls := []*llm_schema.ToolCall{
		llm_schema.NewToolCall("c1", "read_file", `{"file_path":"x"}`),
	}
	results := mgr.Execute(context.Background(), nil, calls, nil, "", true)
	if len(results) != 1 {
		t.Fatalf("应返回 1 条结果, got %d", len(results))
	}
	if results[0].ToolMsg == nil ||
		!strings.Contains(results[0].ToolMsg.Content.Text(), "rejected by SessionMemory policy") {
		t.Errorf("应携带策略拒绝消息: %+v", results[0].ToolMsg)
	}
	if len(mgr.rejectedToolCalls) != 1 || mgr.rejectedToolCalls[0]["reason"] != "only edit_file is executable" {
		t.Errorf("应记账拒绝原因: %+v", mgr.rejectedToolCalls)
	}
}

// TestAbilityManager_路径白名单 edit_file 目标必须是待写笔记（绝对路径等值）。
// Python: session_memory_agent.py:122 (_is_allowed_edit_path)。
func TestAbilityManager_路径白名单(t *testing.T) {
	_, mgr, ws := newPolicyManagerForTest(t)
	notes := ws + "/memory.md"
	mgr.SetAllowedNotesPath(notes)

	if !mgr.isAllowedEditPath(map[string]any{"file_path": notes}) {
		t.Error("笔记路径应放行")
	}
	if mgr.isAllowedEditPath(map[string]any{"file_path": ws + "/other.md"}) {
		t.Error("非笔记路径应拒绝")
	}
	if mgr.isAllowedEditPath(map[string]any{}) {
		t.Error("无 file_path 应拒绝")
	}
	// JSON 字符串形态入参
	if !mgr.isAllowedEditPath(`{"file_path":"` + notes + `"}`) {
		t.Error("JSON 字符串形态应解析放行")
	}
	// 未设置白名单 → 全拒
	mgr2 := NewSessionMemoryAbilityManager("owner")
	if mgr2.isAllowedEditPath(map[string]any{"file_path": notes}) {
		t.Error("未设置 allowed_notes_path 应全拒")
	}
}

// TestAbilityManager_DebugSnapshot 快照承载模型工具与允许路径。
// Python: session_memory_agent.py:48 (debug_snapshot)。
func TestAbilityManager_DebugSnapshot(t *testing.T) {
	_, mgr, ws := newPolicyManagerForTest(t)
	mgr.SetModelTools(nil)
	mgr.SetAllowedNotesPath(ws + "/memory.md")
	snap := mgr.DebugSnapshot()
	if snap["allowed_notes_path"] != ws+"/memory.md" {
		t.Errorf("快照应含允许路径: %+v", snap)
	}
}

// TestSessionMemoryAgent_装配能力集 configureRequest 后工具信息与白名单就绪。
func TestSessionMemoryAgent_装配能力集(t *testing.T) {
	sma, _, ws := newPolicyManagerForTest(t)
	if err := sma.ConfigureRequest(nil, ws+"/memory.md"); err != nil {
		t.Fatal(err)
	}
	if sma.AbilityManager().allowedNotesPath != ws+"/memory.md" {
		t.Errorf("白名单应注入: %q", sma.AbilityManager().allowedNotesPath)
	}
	// 未装配时 ConfigureRequest 报错
	bare := NewSessionMemoryAgent(nil, nil, nil, nil, nil, "")
	if err := bare.ConfigureRequest(nil, "x"); err == nil {
		t.Error("未 configureAgent 时应报错")
	}
}

// TestAbilityManager_放行路径真实编辑 放行的 edit_file 经真实 sysop/EditFileTool
// 修改笔记文件——挂账①的实物闭环验证。
func TestAbilityManager_放行路径真实编辑(t *testing.T) {
	sma, mgr, ws := newPolicyManagerForTest(t)
	notes := ws + "/memory.md"
	if err := os.WriteFile(notes, []byte("hello world\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = sma
	mgr.SetAllowedNotesPath(notes)
	// 生产装配：SessionMemoryAgentUpdater.primeNotesFn 预填充读状态
	// （对齐 Python _prime_notes_file_as_read——先读后写防护放行）
	if stat, err := os.Stat(notes); err == nil {
		filesystem.SetFileReadState(notes, &filesystem.FileReadState{
			MtimeNS:   stat.ModTime().UnixNano(),
			SizeBytes: stat.Size(),
			Content:   "hello world\n",
		})
	}

	results := mgr.Execute(context.Background(), nil,
		[]*llm_schema.ToolCall{notesCall("c-edit", notes)}, nil, "", true)
	if len(results) != 1 {
		t.Fatalf("应返回 1 条结果, got %d", len(results))
	}
	data, err := os.ReadFile(notes)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "hello edited") {
		t.Errorf("笔记应被真实编辑, 当前内容: %q, result=%+v", string(data), results[0])
	}
}
