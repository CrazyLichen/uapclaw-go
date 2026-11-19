// Package mcp 提供 team-mcp stdio server：把团队协作暴露给外部 agent。
//
// 两个场景，由入团描述符的 scope（首次连接时解析）选择：
//
//   - member：被 spawn 的第三方 CLI 作为一等团队成员。server 暴露真团队
//     工具（view_task / claim_task / send_message，CreateTeamTools("teammate")
//     构建产物），外部成员调到与进程内 teammate 完全相同的 TeamTool 实例——
//     同输入 schema、同行为、同渲染文本。入站消息按原生方式到达成员：
//     宿主进程的协调层把它们推进 CLI，所以不暴露拉取工具。server 级
//     instructions 为空：团队 system prompt 在 spawn 时直接注入 CLI。
//   - operator（默认）：通过工具操作与控制团队的外部非成员界面（任务板 +
//     消息 + 名册 + 成员控制）。server 级 instructions 携带控制工作流。
//     operator 没有自己的协调层，所以额外获得 operator 专属 read_inbox
//     拉取工具。
//
// 入口名 uapclawmcpserver（用户拍板品牌改名，沿 D-tel-3 先例）。
// 描述符经 OPENJIWEN_TEAM_JOIN 环境变量读取；session_id / 语言上下文在每次
// 工具调用前重绑（每次调用独立运行）。
//
// 归包偏差 D-15.16-b5：Python 基于 mcp lowlevel Server；Go 用
// mark3labs/mcp-go v0.54.1 server 包承载 stdio。已知偏差：
//   - Python 的 list_tools 在每次请求时从已连接 client 解析；mcp-go 工具集
//     是静态注册，且 scope 在描述符里启动即可知——因此 Go 在启动时**急切
//     connect**（而非惰性 holder），再按 scope 注册工具集（D-15.16-b11）。
//
// Python: openjiuwen/agent_teams/mcp/server.py（308 行）
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/external"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// logComponent 组件常量。
const logComponent = logger.ComponentTeam

// ──────────────────────────── 常量 ────────────────────────────

// ServerName MCP server 标识。
// Python: SERVER_NAME = "openjiuwen-team-member"（server.py:43）
const ServerName = "openjiuwen-team-member"

// operatorInstructions operator scope 的 server 级 instructions——
// 非成员界面驱动团队的控制工作流。（member scope 不带 instructions：
// 团队 system prompt 在 spawn 时直接注入 CLI。）
// Python: OPERATOR_INSTRUCTIONS（server.py:46-53），原文照抄不本地化。
const operatorInstructions = `You operate an OpenJiuWen agent team from outside it (you are not a team
member). Drive the team through these tools: read_inbox to see the mailbox and
task board, create_task to hand out work, update_task to edit / assign /
cancel, send_message to direct members (use "*" to broadcast), list_members to
see the roster, and the task-board tools. Refer to members by name.
`

// readInbox 稳定工具名（operator 专属拉取操作；成员的消息由壳进程推送）。
// Python: READ_INBOX = "read_inbox"（server.py:56）
const readInbox = "read_inbox"

// ──────────────────────────── 结构体 ────────────────────────────

// ClientFactory 生产已连接 ExternalTeamClient 的工厂。
// Python: ClientFactory = Callable[[], Awaitable[ExternalTeamClient]]（server.py:58）
// Go 侧为同步构造（descriptor 从 env 或测试注入）。
type ClientFactory func(ctx context.Context) (*external.ExternalTeamClient, error)

// ──────────────────────────── 导出函数 ────────────────────────────

// ConnectFromEnv 从 OPENJIWEN_TEAM_JOIN 环境变量构建并连接客户端。
// Python: connect_from_env (server.py:60-64)
func ConnectFromEnv(ctx context.Context) (*external.ExternalTeamClient, error) {
	descriptor, err := external.TeamJoinDescriptorFromEnv(nil)
	if err != nil {
		return nil, err
	}
	client := external.NewExternalTeamClient(descriptor)
	if err := client.Connect(ctx); err != nil {
		return nil, err
	}
	return client, nil
}

// BuildServer 构建 stdio MCP server，按入团描述符的 scope 分型。
// scope 为 "member" 时暴露真 teammate 工具；其余（含 operator）暴露
// operator 工具集并携带控制工作流 instructions。
// 偏差 D-15.16-b11：启动时急切 connect + 静态工具注册（Python 为惰性
// holder + 每请求 list_tools；scope 两者同源自描述符）。
// Python: build_server (server.py:186-262)
func BuildServer(ctx context.Context, clientFactory ClientFactory, scopeOverride string) (*mcpserver.MCPServer, *external.ExternalTeamClient, error) {
	resolvedScope := scopeOverride
	if resolvedScope == "" {
		descriptor, err := external.TeamJoinDescriptorFromEnv(nil)
		if err != nil {
			return nil, nil, err
		}
		resolvedScope = descriptor.Scope
	}
	instructions := ""
	if resolvedScope != "member" {
		instructions = operatorInstructions
	}
	logger.Info(logComponent).Str("scope", resolvedScope).Msg("team MCP server scope")

	client, err := clientFactory(ctx)
	if err != nil {
		return nil, nil, err
	}

	opts := []mcpserver.ServerOption{}
	if instructions != "" {
		opts = append(opts, mcpserver.WithInstructions(instructions))
	}
	opts = append(opts, mcpserver.WithToolCapabilities(false))
	server := mcpserver.NewMCPServer(ServerName, "1.0.0", opts...)

	if resolvedScope == "member" {
		// 只暴露真 teammate 工具——入站消息由宿主进程协调层推给成员，
		// operator 专属的 read_inbox 拉取工具被有意排除。
		// Python: list_tools 的 member 分支（server.py:243-258）
		for name, tl := range client.Tools() {
			toolName := name
			toolImpl := tl
			card := toolImpl.Card()
			if card == nil {
				continue
			}
			mcpTool := teamToolToMCP(card.Name, card.Description, card.InputParams)
			server.AddTool(mcpTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return callTeamTool(ctx, client, toolName, request)
			})
		}
		return server, client, nil
	}

	// operator scope：显式工具定义 + 经 client 操作分发。
	// Python: _operator_tool_defs 注册 + call_tool 的 operator 分支（server.py:120-182, 260-277）
	for _, def := range operatorToolDefs() {
		server.AddTool(def.tool, def.handler(ctx, client))
	}
	return server, client, nil
}

// Serve 在 stdio 上运行 server 直到客户端断开。
// Python: _serve + main (server.py:281-293)
func Serve(ctx context.Context, clientFactory ClientFactory, scopeOverride string, stdin, stdout interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
}) error {
	server, _, err := BuildServer(ctx, clientFactory, scopeOverride)
	if err != nil {
		return err
	}
	return mcpserver.NewStdioServer(server).Listen(ctx, stdin, stdout)
}

// MainFromEnv 生产入口：从进程环境读描述符并 serve over stdio。
// Python: main (server.py:287-290)
func MainFromEnv() {
	if err := Serve(context.Background(), ConnectFromEnv, "", os.Stdin, os.Stdout); err != nil {
		logger.Error(logComponent).Err(err).Msg("team MCP server exited with error")
		os.Exit(1)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// operatorToolDef 一个 operator 工具的定义与处理器。
type operatorToolDef struct {
	// tool MCP 工具定义
	tool mcp.Tool
	// handler 调用处理器
	handler func(ctx context.Context, client *external.ExternalTeamClient) mcpserver.ToolHandlerFunc
}

// strSchema {"type": "string"} 属性。
// Python: _STR（server.py:110）
func strSchema() map[string]any {
	return map[string]any{"type": "string"}
}

// rawSchema 把手写 JSON schema 转成 RawInputSchema。
func rawSchema(schemaMap map[string]any) mcp.Tool {
	raw, _ := json.Marshal(schemaMap)
	return mcp.Tool{RawInputSchema: raw}
}

// operatorToolDefs operator（外部团队控制）scope 的工具定义集。
// 逐条对齐 Python: _operator_tool_defs (server.py:113-180)
func operatorToolDefs() []operatorToolDef {
	defs := []operatorToolDef{}

	// read_inbox：operator 专属收件箱拉取工具（无原生对应物）。
	// Python: _read_inbox_tool (server.py:66-78)
	{
		tool := rawSchema(map[string]any{
			"type":       "object",
			"properties": map[string]any{},
			"required":   []string{},
		})
		tool.Name = readInbox
		tool.Description = "Pull your unread messages (direct + broadcast) and the current " +
			"task board, rendered as text. Reading marks messages read. Call " +
			"it at the start of a turn and whenever you expect new work; an " +
			"empty inbox is normal."
		defs = append(defs, operatorToolDef{tool: tool, handler: func(ctx context.Context, client *external.ExternalTeamClient) mcpserver.ToolHandlerFunc {
			return func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				text, err := client.ReadInbox(ctx, true)
				if err != nil {
					return mcp.NewToolResultErrorFromErr("read_inbox failed", err), nil
				}
				return mcp.NewToolResultText(text), nil
			}
		}})
	}

	// send_message / create_task / update_task / list_tasks / claimable_tasks /
	// get_task / claim_task / complete_task / list_members（server.py:119-180）
	defs = append(defs,
		opTool("send_message",
			`Send a direct message to a member (use "*" to broadcast to the team).`,
			map[string]any{"to": strSchema(), "content": strSchema()},
			[]string{"to", "content"},
			func(ctx context.Context, client *external.ExternalTeamClient, args map[string]any) (string, error) {
				messageID, err := client.SendMessage(ctx, argsString(args, "to"), argsString(args, "content"))
				if err != nil {
					return "", err
				}
				ok := messageID != nil
				return jsonMarshal(map[string]any{"ok": ok, "message_id": messageID}), nil
			}),
		opTool("create_task",
			"Create a team task members can claim. Optionally pin an id and dependency ids.",
			map[string]any{
				"title":        strSchema(),
				"content":      strSchema(),
				"task_id":      strSchema(),
				"dependencies": map[string]any{"type": "array", "items": strSchema()},
			},
			[]string{"title", "content"},
			func(ctx context.Context, client *external.ExternalTeamClient, args map[string]any) (string, error) {
				result, err := client.CreateTask(ctx, argsString(args, "title"), argsString(args, "content"),
					argsStringPtr(args, "task_id"), argsStrings(args, "dependencies"))
				if err != nil {
					return "", err
				}
				taskID := ""
				if result.Task != nil {
					taskID = result.Task.TaskID
				}
				return jsonMarshal(map[string]any{"ok": true, "reason": result.Reason, "task_id": taskID}), nil
			}),
		opTool("update_task",
			`Edit a task's title and/or content (pending / blocked tasks only).`,
			map[string]any{"task_id": strSchema(), "title": strSchema(), "content": strSchema()},
			[]string{"task_id"},
			func(ctx context.Context, client *external.ExternalTeamClient, args map[string]any) (string, error) {
				result, err := client.UpdateTask(ctx, argsString(args, "task_id"), argsStringPtr(args, "title"), argsStringPtr(args, "content"))
				if err != nil {
					return "", err
				}
				return jsonMarshal(map[string]any{"ok": result.OK}), nil
			}),
		opTool("list_tasks",
			"List team tasks, optionally filtered by status.",
			map[string]any{"status": strSchema()},
			nil,
			func(ctx context.Context, client *external.ExternalTeamClient, args map[string]any) (string, error) {
				tasks, err := client.ListTasks(ctx, argsString(args, "status"))
				if err != nil {
					return "", err
				}
				rows := make([]map[string]any, 0, len(tasks))
				for _, t := range tasks {
					rows = append(rows, map[string]any{
						"task_id": t.TaskID, "title": t.Title, "status": t.Status, "assignee": t.Assignee,
					})
				}
				return jsonMarshal(rows), nil
			}),
		opTool("claimable_tasks",
			"List pending tasks available to claim.",
			map[string]any{},
			nil,
			func(ctx context.Context, client *external.ExternalTeamClient, args map[string]any) (string, error) {
				tasks, err := client.ClaimableTasks(ctx)
				if err != nil {
					return "", err
				}
				rows := make([]map[string]any, 0, len(tasks))
				for _, t := range tasks {
					rows = append(rows, map[string]any{"task_id": t.TaskID, "title": t.Title})
				}
				return jsonMarshal(rows), nil
			}),
		opTool("get_task",
			`Get one task's full detail, or null if it does not exist.`,
			map[string]any{"task_id": strSchema()},
			[]string{"task_id"},
			func(ctx context.Context, client *external.ExternalTeamClient, args map[string]any) (string, error) {
				detail, err := client.GetTask(ctx, argsString(args, "task_id"))
				if err != nil {
					return "", err
				}
				if detail == nil {
					return "null", nil
				}
				return jsonMarshal(detail), nil
			}),
		opTool("claim_task",
			"Claim a pending task.",
			map[string]any{"task_id": strSchema()},
			[]string{"task_id"},
			func(ctx context.Context, client *external.ExternalTeamClient, args map[string]any) (string, error) {
				result, err := client.ClaimTask(ctx, argsString(args, "task_id"))
				if err != nil {
					return "", err
				}
				return jsonMarshal(map[string]any{"ok": result.OK, "reason": result.Reason}), nil
			}),
		opTool("complete_task",
			"Complete a claimed task.",
			map[string]any{"task_id": strSchema()},
			[]string{"task_id"},
			func(ctx context.Context, client *external.ExternalTeamClient, args map[string]any) (string, error) {
				result, err := client.CompleteTask(ctx, argsString(args, "task_id"))
				if err != nil {
					return "", err
				}
				return jsonMarshal(map[string]any{"ok": result.OK, "reason": result.Reason}), nil
			}),
		opTool("list_members",
			`List the team's members.`,
			map[string]any{},
			nil,
			func(ctx context.Context, client *external.ExternalTeamClient, args map[string]any) (string, error) {
				members, err := client.ListMembers(ctx)
				if err != nil {
					return "", err
				}
				rows := make([]map[string]any, 0, len(members))
				for _, m := range members {
					rows = append(rows, map[string]any{
						"member_name": m.MemberName, "role": m.Role, "status": m.Status,
					})
				}
				return jsonMarshal(rows), nil
			}),
	)
	return defs
}

// opTool 组装一个 operator 工具定义。
func opTool(name, description string, properties map[string]any, required []string, dispatch func(ctx context.Context, client *external.ExternalTeamClient, args map[string]any) (string, error)) operatorToolDef {
	schemaMap := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schemaMap["required"] = required
	}
	tool := rawSchema(schemaMap)
	tool.Name = name
	tool.Description = description
	return operatorToolDef{tool: tool, handler: func(ctx context.Context, client *external.ExternalTeamClient) mcpserver.ToolHandlerFunc {
		return func(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			text, err := dispatch(ctx, client, request.GetArguments())
			if err != nil {
				return mcp.NewToolResultErrorFromErr(name+" failed", err), nil
			}
			return mcp.NewToolResultText(text), nil
		}
	}}
}

// callTeamTool member scope：按名字取真 TeamTool 并以成员身份调用，
// 返回其 LLM 可读渲染文本。
// Python: call_tool 的 member 分支（server.py:260-271）——
// tool.invoke(arguments, member_name=..., display_name=...) 后
// tool.render_for_llm(result)。Go 侧渲染等价物是 WrapInvokeWithLogging
// 已注入的 __mapped_content（D-15.16-b8），缺失时走默认规则渲染。
func callTeamTool(ctx context.Context, client *external.ExternalTeamClient, name string, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	toolImpl, ok := client.Tools()[name]
	if !ok {
		return mcp.NewToolResultText(fmt.Sprintf("Unknown tool: %s", name)), nil
	}
	output, err := toolImpl.Invoke(ctx, request.GetArguments())
	if err != nil {
		// Python: except Exception → "Internal error: {exc}"（server.py:273-276），
		// 绝不冒成 MCP 协议错误。
		return mcp.NewToolResultText(fmt.Sprintf("Internal error: %s", err)), nil
	}
	if mapped, ok := output[tools.MappedContentKey].(string); ok && mapped != "" {
		return mcp.NewToolResultText(mapped), nil
	}
	data, _ := output["data"].(map[string]any)
	success, _ := output["success"].(bool)
	if success {
		if content, ok := data["content"].(string); ok && content != "" {
			return mcp.NewToolResultText(content), nil
		}
		return mcp.NewToolResultText("(empty result)"), nil
	}
	if errText, ok := output["error"].(string); ok && errText != "" {
		return mcp.NewToolResultText(errText), nil
	}
	return mcp.NewToolResultText("(empty failure)"), nil
}

// teamToolToMCP 把 TeamTool 卡片转成 MCP 工具定义。
// Python: list_tools 的 member 分支投影（server.py:243-258）——
// name/description/inputSchema 直出 card 裸形状。
func teamToolToMCP(name, description string, params []*commonschema.Param) mcp.Tool {
	schemaMap := commonschema.ToJSONSchemaMap(params)
	raw, _ := json.Marshal(schemaMap)
	return mcp.Tool{Name: name, Description: description, RawInputSchema: raw}
}

// jsonMarshal 紧凑序列化；失败退化为空对象文本。
func jsonMarshal(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// argsString 从 MCP 参数取字符串；缺失返回空串。
func argsString(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

// argsStringPtr 从 MCP 参数取可选字符串。
func argsStringPtr(args map[string]any, key string) *string {
	if v, ok := args[key].(string); ok && v != "" {
		return &v
	}
	return nil
}

// argsStrings 从 MCP 参数取字符串列表。
func argsStrings(args map[string]any, key string) []string {
	raw, ok := args[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
