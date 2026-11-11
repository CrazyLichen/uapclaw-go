package processcli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
)

// 本文件是 channels/process_cli/session_entry.py 的 Go 对应物。
//
// Python: channels/process_cli/session_entry.py（139 行）
//
// 职责：会话查询。直接读 Runtime 的会话目录（readOnly=true，不写回元数据），
// 全程不启动 Runtime。

// ──────────────────────────── 常量 ────────────────────────────

const (
	// sessionSearchMaxLength 搜索串上限
	// Python: session_entry.py:46 len(args.search) > 200
	sessionSearchMaxLength = 200
	// sessionPageMinLimit 每页最小条数
	sessionPageMinLimit = 1
	// sessionPageMaxLimit 每页最大条数
	sessionPageMaxLimit = 200
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ValidateSessionQuery 校验会话查询的参数组合。
// Python: session_entry.py:24-48 _validate_query
//
// 六条规则：查询触发条件、分页参数只配 latest 排除、limit 范围、
// offset 非负、search 长度、不得混用任务参数。
func ValidateSessionQuery(args *AppArgs) error {
	if !args.ListSessions && !args.LatestSession {
		if args.SearchSet || args.LimitSet || args.OffsetSet {
			return fmt.Errorf("--search、--limit、--offset 仅用于会话查询")
		}
		return nil
	}
	// Python: :40 args.prompt 与其它任务参数都不得出现
	if args.Prompt != "" ||
		args.Model != "" || args.APIBase != "" || args.APIKeyEnv != "" || args.ModelProvider != "" ||
		args.QueryJSON != "" || args.RunJSON != "" || args.RunJSONL ||
		args.Cwd != "" || args.ProjectDir != "" || len(args.TrustedDirs) > 0 ||
		args.OutputExplicit || args.TimeoutSet || args.ShowReasoning || args.ShowTools || args.Debug {
		return fmt.Errorf("会话查询仅支持 --search、--limit、--offset，不能混用任务参数")
	}
	// Python: :41-42 latest 不支持分页
	if args.LatestSession && (args.LimitSet || args.OffsetSet) {
		return fmt.Errorf("--latest-session 仅支持 --search，不支持分页参数")
	}
	// Python: :44-45 limit 范围
	if args.LimitSet && (args.Limit < sessionPageMinLimit || args.Limit > sessionPageMaxLimit) {
		return fmt.Errorf("--limit 必须在 1 到 200 之间")
	}
	// Python: :46 offset 非负
	if args.OffsetSet && args.Offset < 0 {
		return fmt.Errorf("--offset 必须是非负整数")
	}
	// Python: :47-48 search 长度
	if args.SearchSet && len([]rune(args.Search)) > sessionSearchMaxLength {
		return fmt.Errorf("--search 不能超过 200 个字符")
	}
	return nil
}

// ExecuteSessionQuery 校验后复用 Runtime 目录查询（元数据写回关闭）。
// Python: session_entry.py:100-139 execute_session_query
//
// 返回进程退出码。全程不启动 Runtime。
func ExecuteSessionQuery(args *AppArgs) int {
	if err := ValidateSessionQuery(args); err != nil {
		fmt.Fprintf(os.Stderr, "uapclaw-process: 错误：%v\n", err)
		return exitArgError
	}

	streams, restore, diagErr := CaptureDiagnostics(true, false)
	if diagErr == nil && restore != nil {
		defer restore()
	}
	stdout := io.Writer(os.Stdout)
	stderr := io.Writer(os.Stderr)
	logPath := ""
	if streams != nil {
		stdout = streams.Stdout
		stderr = streams.Stderr
		logPath = streams.LogPath
	}

	limit := 20
	if args.LimitSet {
		limit = args.Limit
	}
	if args.LatestSession {
		limit = 1
	}
	offset := 0
	if args.OffsetSet {
		offset = args.Offset
	}

	result, err := swruntime.ListSessions(swruntime.SessionListInput{
		ChannelID: processCliChannelID,
		Search:    args.Search,
		Limit:     limit,
		Offset:    offset,
	}, true, true)
	if err != nil {
		detail := "请检查数据目录与文件访问权限"
		if logPath != "" {
			detail = fmt.Sprintf("诊断日志：%s", logPath)
		}
		fmt.Fprintf(stderr, "会话查询失败；%s\n", detail)
		return 1
	}

	// Python: :117-121 latest 只输出完整 ID
	if args.LatestSession {
		if len(result.Sessions) == 0 {
			fmt.Fprintf(stderr, "未找到匹配的 Process CLI 会话。\n")
			return 1
		}
		fmt.Fprintf(stdout, "%s\n", result.Sessions[0].SessionID)
		return 0
	}

	// Python: :122-126 无结果且总数为 0
	if len(result.Sessions) == 0 && result.Total == 0 {
		fmt.Fprint(stdout, "没有匹配的 Process CLI 会话。\n")
		return 0
	}
	fmt.Fprint(stdout, renderSessions(result))
	return 0
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// displayText 把控制字符替换成空格以避免破坏表格。
// Python: session_entry.py:52-55 _display_text
func displayText(value string) string {
	var builder strings.Builder
	for _, r := range value {
		if r < 32 || (r >= 127 && r < 160) {
			builder.WriteRune(' ')
			continue
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

// displayTime 把 Unix 秒时间戳渲染为本地时间；非法值返回 "-"。
// Python: session_entry.py:58-66 _display_time
func displayTime(value float64) string {
	if value <= 0 {
		return "-"
	}
	seconds := int64(value)
	if seconds <= 0 {
		return "-"
	}
	return time.Unix(seconds, 0).Local().Format("2006-01-02T15:04:05-07:00")
}

// renderSessions 渲染会话表格（表头 + 行 + 汇总）。
// Python: session_entry.py:69-97 _render_sessions
func renderSessions(result swruntime.SessionListResult) string {
	header := []string{"会话ID", "最近消息时间", "模式", "标题", "项目目录"}
	rows := make([][]string, 0, len(result.Sessions)+1)
	rows = append(rows, header)
	for _, item := range result.Sessions {
		rows = append(rows, []string{
			item.SessionID,
			displayTime(item.LastMessageAt),
			item.Mode,
			displayText(item.Title),
			displayText(item.ProjectDir),
		})
	}

	// Python: :79-83 前四列按显示宽度对齐，最后一列不填充
	columnWidths := make([]int, len(header)-1)
	for column := range columnWidths {
		width := 0
		for _, row := range rows {
			if w := displayWidth(row[column]); w > width {
				width = w
			}
		}
		columnWidths[column] = width
	}

	var builder strings.Builder
	for _, row := range rows {
		cells := make([]string, 0, len(row)-1)
		for column := 0; column < len(row)-1; column++ {
			padding := columnWidths[column] - displayWidth(row[column])
			if padding < 0 {
				padding = 0
			}
			cells = append(cells, row[column]+strings.Repeat(" ", padding))
		}
		cells = append(cells, row[len(row)-1])
		builder.WriteString(strings.Join(cells, "  "))
		builder.WriteString("\n")
	}
	builder.WriteString(fmt.Sprintf(
		"本页 %d 条；匹配共 %d 条；offset=%d；limit=%d\n",
		len(result.Sessions), result.Total, result.Offset, result.Limit))
	return builder.String()
}
