package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	harnessfactory "github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	runnerpkg "github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// extractionReadFileInput 读取记忆文件工具入参。
type extractionReadFileInput struct {
	// Path 文件相对路径
	Path string `json:"path"`
}

// extractionWriteFileInput 写入记忆文件工具入参。
type extractionWriteFileInput struct {
	// Path 文件相对路径
	Path string `json:"path"`
	// Content 写入内容
	Content string `json:"content"`
}

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

// ExtractionAgentPrompt 提取 agent 系统提示词
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

// BuildExtractionContext 构建提取上下文。
// S-12: 对齐 Python _build_extraction_context（extractor.py L74-104）——
// DB 记录组装为提取 agent 的用户消息（任务记录 + 按时间排序的团队对话）。
func BuildExtractionContext(tasks []*database.TeamTaskBase, messages []*database.TeamMessageBase, tzOffsetHours float64) string {
	tz := time.FixedZone("extract_tz", int(tzOffsetHours*3600))
	parts := []string{"# 本轮团队协作记录\n"}

	if len(tasks) > 0 {
		parts = append(parts, "## 任务记录\n")
		for _, t := range tasks {
			assignee := "未分配"
			if t.Assignee != nil && *t.Assignee != "" {
				assignee = *t.Assignee
			}
			parts = append(parts, fmt.Sprintf("### %s [%s] -> %s", t.Title, t.Status, assignee))
			if t.Content != "" {
				content := t.Content
				// rune 口径截断（Python str[:] 按字符计）
				if runes := []rune(content); len(runes) > taskContentPreviewMax {
					content = string(runes[:taskContentPreviewMax])
				}
				parts = append(parts, content)
			}
			parts = append(parts, "")
		}
	}

	if len(messages) > 0 {
		parts = append(parts, "## 团队对话\n")
		sortedMsgs := make([]*database.TeamMessageBase, len(messages))
		copy(sortedMsgs, messages)
		sort.SliceStable(sortedMsgs, func(i, j int) bool {
			return sortedMsgs[i].Timestamp < sortedMsgs[j].Timestamp
		})
		for _, m := range sortedMsgs {
			ts := time.UnixMilli(m.Timestamp).In(tz).Format("01-02 15:04")
			direction := "-> ?"
			if m.Broadcast {
				direction = "-> 全体"
			} else if m.ToMemberName != nil && *m.ToMemberName != "" {
				direction = "-> " + *m.ToMemberName
			}
			content := m.Content
			if runes := []rune(content); len(runes) > messageContentPreviewMax {
				content = string(runes[:messageContentPreviewMax])
			}
			parts = append(parts, fmt.Sprintf("[%s] %s %s: %s", ts, m.FromMemberName, direction, content))
		}
		parts = append(parts, "")
	}

	return strings.Join(parts, "\n")
}

// CreateExtractionTools 创建提取 agent 限定工具。
// S-12: 对齐 Python _create_extraction_tools（extractor.py L107-191）——
// read/write/list 三个本地函数工具，路径限制在团队记忆目录内
// （禁止 ".." 与绝对路径，filepath.Base 语义收窄）。
func CreateExtractionTools(teamMemoryDir string, sysOp sysop.SysOperation, teamName string) []tool.Tool {
	prefix := "extract." + teamName
	if teamName == "" {
		prefix = "extract"
	}

	// read_memory_file 读取工具：限制路径在团队记忆目录内
	readTool, readErr := tool.NewTool(func(ctx context.Context, in extractionReadFileInput, opts ...tool.ToolOption) (map[string]any, error) {
		if strings.Contains(in.Path, "..") || strings.HasPrefix(in.Path, "/") {
			return map[string]any{"error": "Invalid path"}, nil
		}
		// Python: os.path.join(team_memory_dir, os.path.basename(path))
		fullPath := filepath.Join(teamMemoryDir, filepath.Base(in.Path))
		if sysOp != nil && sysOp.Fs() != nil {
			result, err := sysOp.Fs().ReadFile(ctx, fullPath)
			if err != nil {
				// Python: except → {"content": "", "path": full_path, "note": "file not found"}
				// Go Fs 层错误经 Go error 表达（对应 Python read_file 抛出的意外异常）
				return map[string]any{"content": "", "path": fullPath, "note": "file not found"}, nil
			}
			// Python: result.data 存在（含 error result 内部捕获场景 data 为 None）
			// → hasattr(result, "data") and result.data 判定；error result（文件
			// 未找到等）data 为 nil → 空数据分支（无 note），对齐 Python 语义
			if result != nil && result.Data != nil {
				return map[string]any{"content": result.Data.Content, "path": fullPath}, nil
			}
			// Python: return {"content": "", "path": full_path}（空数据：无 note 键）
			return map[string]any{"content": "", "path": fullPath}, nil
		}
		// sysOp 缺失兜底：等价异常分支口径（带 note，read 侧无本地回退）
		return map[string]any{"content": "", "path": fullPath, "note": "file not found"}, nil
	}, tool.WithToolCard(tool.NewToolCardWithID(
		prefix+".read", "read_memory_file", "读取团队记忆目录下的文件",
		[]*schema.Param{schema.NewStringParam("path", "file path", true)}, nil)))
	if readErr != nil {
		logger.Warn(logComponent).Err(readErr).Msg("[extractor] failed to create read tool")
	}

	// write_memory_file 写入工具：覆盖写且限制路径在团队记忆目录内
	writeTool, writeErr := tool.NewTool(func(ctx context.Context, in extractionWriteFileInput, opts ...tool.ToolOption) (map[string]any, error) {
		if strings.Contains(in.Path, "..") || strings.HasPrefix(in.Path, "/") {
			return map[string]any{"error": "Invalid path"}, nil
		}
		fullPath := filepath.Join(teamMemoryDir, filepath.Base(in.Path))
		if sysOp != nil && sysOp.Fs() != nil {
			if _, err := sysOp.Fs().WriteFile(ctx, fullPath, in.Content, sysop.WithFsCreateIfNotExist(true)); err != nil {
				return map[string]any{"success": false, "error": err.Error()}, nil
			}
			return map[string]any{"success": true, "path": fullPath}, nil
		}
		// 本地文件系统回退（sysOp 缺失时）
		if err := os.WriteFile(fullPath, []byte(in.Content), 0o644); err != nil {
			return map[string]any{"success": false, "error": err.Error()}, nil
		}
		return map[string]any{"success": true, "path": fullPath}, nil
	}, tool.WithToolCard(tool.NewToolCardWithID(
		prefix+".write", "write_memory_file", "写入团队记忆目录下的文件（覆盖）",
		[]*schema.Param{
			schema.NewStringParam("path", "file path", true),
			schema.NewStringParam("content", "content to write", true),
		}, nil)))
	if writeErr != nil {
		logger.Warn(logComponent).Err(writeErr).Msg("[extractor] failed to create write tool")
	}

	// list_memory_files 列出工具：返回团队记忆目录下所有文件名
	// M-16: 对齐 Python _list_files——sys_operation.fs().list_files(dir, recursive=False)
	// 提取非目录项名称；异常/缺失回退 {"files": []}（原实现 os.ReadDir 直读
	// 本地盘，Fs 抽象隔离/远程 sysOp 场景下读不到）
	listTool, listErr := tool.NewTool(func(ctx context.Context, _ struct{}, opts ...tool.ToolOption) (map[string]any, error) {
		if sysOp != nil && sysOp.Fs() != nil {
			// Python: result = await sys_operation.fs().list_files(team_memory_dir, recursive=False)
			if result, err := sysOp.Fs().ListFiles(ctx, teamMemoryDir, sysop.WithFsRecursive(false)); err == nil &&
				result != nil && result.Data != nil {
				files := make([]string, 0, len(result.Data.ListItems))
				for _, item := range result.Data.ListItems {
					// Python: files = [f.name for f in result.data.list_items if not f.is_directory]
					if !item.IsDirectory {
						files = append(files, item.Name)
					}
				}
				return map[string]any{"files": files}, nil
			}
			// Python: except / result 无 data → {"files": []}
			return map[string]any{"files": []string{}}, nil
		}
		// 本地文件系统回退（sysOp 缺失时，与 write 工具兜底口径一致）
		entries, err := os.ReadDir(teamMemoryDir)
		if err != nil {
			return map[string]any{"files": []string{}}, nil
		}
		files := make([]string, 0, len(entries))
		for _, e := range entries {
			if !e.IsDir() {
				files = append(files, e.Name())
			}
		}
		return map[string]any{"files": files}, nil
	}, tool.WithToolCard(tool.NewToolCardWithID(
		prefix+".list", "list_memory_files", "列出团队记忆目录下的所有文件", nil, nil)))
	if listErr != nil {
		logger.Warn(logComponent).Err(listErr).Msg("[extractor] failed to create list tool")
	}

	var tools []tool.Tool
	for _, t := range []tool.Tool{readTool, writeTool, listTool} {
		if t != nil {
			tools = append(tools, t)
		}
	}
	return tools
}

// ExtractTeamMemories 提取团队记忆。
// S-12: 对齐 Python extract_team_memories（extractor.py L194-245）——
// Forked Agent 提炼：拉取任务+消息 → 无数据早退 → 建目录 → 构建上下文 →
// create_deep_agent + Runner.run_agent 执行提取。model 为 nil 时不执行。
func ExtractTeamMemories(
	ctx context.Context,
	teamName string,
	db database.TeamDatabase,
	taskMgr *tools.TeamTaskManager,
	teamMemoryDir string,
	sysOp sysop.SysOperation,
	model *llm.Model,
	tzOffsetHours float64,
) error {
	// Python: required_components 任一为 None → return
	if sysOp == nil || teamMemoryDir == "" || model == nil || db == nil || taskMgr == nil {
		return nil
	}

	// 失败仅告警不中断（Python: except → logger.warning）
	if err := func() (err error) {
		defer func() {
			if rec := recover(); rec != nil {
				err = fmt.Errorf("panic: %v", rec)
			}
		}()

		tasks, taskErr := taskMgr.ListTasks(ctx, "")
		if taskErr != nil {
			return fmt.Errorf("list tasks failed: %w", taskErr)
		}
		messages, msgErr := db.Message().GetTeamMessages(ctx, teamName, "")
		if msgErr != nil {
			return fmt.Errorf("get team messages failed: %w", msgErr)
		}

		// Python: if not tasks and not messages: return
		if len(tasks) == 0 && len(messages) == 0 {
			return nil
		}

		// Python: os.makedirs(team_memory_dir, exist_ok=True)
		if err := os.MkdirAll(teamMemoryDir, 0o755); err != nil {
			return err
		}

		contextStr := BuildExtractionContext(tasks, messages, tzOffsetHours)

		// Python: extraction_tools = _create_extraction_tools(...)
		extractionTools := CreateExtractionTools(teamMemoryDir, sysOp, teamName)

		// Python: agent = create_deep_agent(model, system_prompt=..., tools=...,
		//   max_iterations=EXTRACTION_AGENT_MAX_ITERATIONS, enable_task_loop=False)
		agent, agentErr := harnessfactory.CreateDeepAgent(ctx, hconfig.CreateDeepAgentParams{
			Model:          model,
			SystemPrompt:   ExtractionAgentPrompt,
			ToolInstances:  extractionTools,
			MaxIterations:  extractionAgentMaxIterations,
			EnableTaskLoop: false,
		})
		if agentErr != nil {
			return fmt.Errorf("create extraction agent failed: %w", agentErr)
		}

		// Python: await Runner.run_agent(agent, {"query": ...})
		_, runErr := runnerpkg.RunAgent(ctx, runnerpkg.ByAgent(agent), map[string]any{
			"query": fmt.Sprintf("请分析以下团队 %s 的协作记录并提取记忆：\n\n%s", teamName, contextStr),
		}, runnerpkg.SessionRef{}, nil, nil)
		if runErr != nil {
			return fmt.Errorf("extraction agent run failed: %w", runErr)
		}

		logger.Info(logComponent).
			Str("team_name", teamName).
			Msg("[extractor] Extraction agent completed")
		return nil
	}(); err != nil {
		logger.Warn(logComponent).
			Str("team_name", teamName).
			Err(err).
			Msg("[extractor] extract_team_memories failed")
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
