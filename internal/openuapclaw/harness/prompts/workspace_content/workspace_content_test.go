package workspace_content

import (
	"strings"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestWorkspaceHeader_非空 测试工作空间头部常量非空
func TestWorkspaceHeader_非空(t *testing.T) {
	if WorkspaceHeaderCN == "" {
		t.Error("WorkspaceHeaderCN 不应为空")
	}
	if WorkspaceHeaderEN == "" {
		t.Error("WorkspaceHeaderEN 不应为空")
	}
}

// TestImportantFiles_非空 测试重要文件说明常量非空
func TestImportantFiles_非空(t *testing.T) {
	if ImportantFilesCN == "" {
		t.Error("ImportantFilesCN 不应为空")
	}
	if ImportantFilesEN == "" {
		t.Error("ImportantFilesEN 不应为空")
	}
}

// TestContextHeader_非空 测试上下文头部常量非空
func TestContextHeader_非空(t *testing.T) {
	if ContextHeaderCN == "" {
		t.Error("ContextHeaderCN 不应为空")
	}
	if ContextHeaderEN == "" {
		t.Error("ContextHeaderEN 不应为空")
	}
}

// TestDailyMemoryTitle_包含日期占位符 测试每日记忆标题包含日期占位符
func TestDailyMemoryTitle_包含日期占位符(t *testing.T) {
	if !strings.Contains(DailyMemoryTitleCN, "{date}") {
		t.Error("DailyMemoryTitleCN 应包含 {date} 占位符")
	}
	if !strings.Contains(DailyMemoryTitleEN, "{date}") {
		t.Error("DailyMemoryTitleEN 应包含 {date} 占位符")
	}
}

// TestContextFileTitles_键数量 测试上下文文件标题映射键数量
func TestContextFileTitles_键数量(t *testing.T) {
	if len(ContextFileTitlesCN) != 6 {
		t.Errorf("ContextFileTitlesCN 应有 6 个键，实际 %d", len(ContextFileTitlesCN))
	}
	if len(ContextFileTitlesEN) != 6 {
		t.Errorf("ContextFileTitlesEN 应有 6 个键，实际 %d", len(ContextFileTitlesEN))
	}
}

// TestDirectoryDescriptions_键数量 测试目录描述映射键数量
func TestDirectoryDescriptions_键数量(t *testing.T) {
	if len(DirectoryDescriptionsCN) != 12 {
		t.Errorf("DirectoryDescriptionsCN 应有 12 个键，实际 %d", len(DirectoryDescriptionsCN))
	}
	if len(DirectoryDescriptionsEN) != 12 {
		t.Errorf("DirectoryDescriptionsEN 应有 12 个键，实际 %d", len(DirectoryDescriptionsEN))
	}
}

// TestContextFiles_长度 测试固定上下文文件列表长度
func TestContextFiles_长度(t *testing.T) {
	if len(ContextFiles) != 5 {
		t.Errorf("ContextFiles 应有 5 个元素，实际 %d", len(ContextFiles))
	}
}

// TestIdentityMD_非空 测试 IDENTITY.md 模板非空
func TestIdentityMD_非空(t *testing.T) {
	if IdentityMDCN == "" {
		t.Error("IdentityMDCN 不应为空")
	}
	if IdentityMDEN == "" {
		t.Error("IdentityMDEN 不应为空")
	}
}

// TestAgentMD_非空 测试 AGENT.md 模板非空
func TestAgentMD_非空(t *testing.T) {
	if AgentMDCN == "" {
		t.Error("AgentMDCN 不应为空")
	}
	if AgentMDEN == "" {
		t.Error("AgentMDEN 不应为空")
	}
}

// TestSoulMD_非空 测试 SOUL.md 模板非空
func TestSoulMD_非空(t *testing.T) {
	if SoulMDCN == "" {
		t.Error("SoulMDCN 不应为空")
	}
	if SoulMDEN == "" {
		t.Error("SoulMDEN 不应为空")
	}
}

// TestHeartbeatMD_非空 测试 HEARTBEAT.md 模板非空
func TestHeartbeatMD_非空(t *testing.T) {
	if HeartbeatMDCN == "" {
		t.Error("HeartbeatMDCN 不应为空")
	}
	if HeartbeatMDEN == "" {
		t.Error("HeartbeatMDEN 不应为空")
	}
}

// TestSessionMemoryMD_非空 测试会话记忆模板非空
func TestSessionMemoryMD_非空(t *testing.T) {
	if SessionMemoryMDCN == "" {
		t.Error("SessionMemoryMDCN 不应为空")
	}
	if SessionMemoryMDEN == "" {
		t.Error("SessionMemoryMDEN 不应为空")
	}
}

// TestMemoryMD_非空 测试 MEMORY.md 模板非空
func TestMemoryMD_非空(t *testing.T) {
	if MemoryMDCN == "" {
		t.Error("MemoryMDCN 不应为空")
	}
	if MemoryMDEN == "" {
		t.Error("MemoryMDEN 不应为空")
	}
}
