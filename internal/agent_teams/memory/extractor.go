package memory

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// taskContentPreviewMax 任务内容预览最大字符数
	taskContentPreviewMax = 2000
	// messageContentPreviewMax 消息内容预览最大字符数
	messageContentPreviewMax = 1000
	// extractionAgentMaxIterations 提取 agent 最大迭代次数
	extractionAgentMaxIterations = 5
)

// ──────────────────────────── 全局变量 ────────────────────────────

const ExtractionAgentPrompt = `你是团队记忆提取 agent。你的工作目录是团队记忆目录，里面可能已有之前提取的记忆文件。

## 你的任务

分析提供的团队协作记录（任务和消息），从中提炼出对未来团队协作有价值的持久记忆，写入记忆文件。

## 工作流程

1. 先用 Read 读取已有的记忆文件（如 TEAM_MEMORY.md），了解已记录的内容
2. 分析新的协作记录，判断哪些信息值得记忆
3. 用 Write/Edit 更新记忆文件：
   - 更新已有记忆条目（如果新信息补充或修正了旧内容）
   - 添加新的记忆条目
   - 删除已过时的条目
   - 合并重复内容

## 提取什么

1. **[decision] 团队决策**: 为什么选择了某个方案、拒绝了哪些替代方案、关键权衡
2. **[lesson] 经验教训**: 什么做法有效、什么导致了返工或问题、值得复用的模式
3. **[member] 成员特长**: 谁擅长什么、谁负责哪个领域、协作模式
4. **[context] 项目背景**: 非代码可推导的业务约束、截止日期、利益相关方要求

## 不要提取什么

- 代码细节、具体文件路径、函数名（可从代码库获取）
- 临时状态、进行中的调试过程
- 原始对话内容的复述（提取的是洞察，不是摘要）
- 任何敏感信息（密钥、凭证、个人隐私）

## 记忆文件格式

TEAM_MEMORY.md 中每条记忆用三级标题 + 类型标签，示例：

    ### [decision] 选择了方案 A 而非 B
    原因是... 权衡是...

    ### [lesson] 并行任务需要先对齐接口
    上次因为没对齐导致返工 2 天...

保持 TEAM_MEMORY.md 在 200 行以内。超出时合并或删除最旧的条目。
如果没有值得提取的新信息，不要修改文件。`

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildExtractionContext 构建提取上下文。⤵️ 回填: 7.2+9.65a
func BuildExtractionContext(tasks []any, messages []any, tzOffsetHours float64) string { return "" }

// CreateExtractionTools 创建提取 agent 限定工具。⤵️ 回填: 7.2
func CreateExtractionTools(teamMemoryDir string, sysOp sysop.SysOperation, teamName string) []any {
	return nil
}

// ExtractTeamMemories 提取团队记忆。⤵️ 回填: 7.2+9.65a
func ExtractTeamMemories(ctx context.Context, teamName string, db database.TeamDatabase, taskMgr *tools.TeamTaskManager, teamMemoryDir string, sysOp sysop.SysOperation, model any, tzOffsetHours float64) error {
	return nil
}
