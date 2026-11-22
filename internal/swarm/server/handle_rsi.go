package server

// AgentServer RSI 分发接线：rsi.* 前缀统一走 RsiAgentServerHandlers
//（服务域/推送见 runtime/rsi 包 + rsihandlers 分发层）。
// 对应 Python：agent_ws_server.py _handle_rsi_request / _get_rsi_handlers /
// _rsi_harness_refs_provider（约 13960-14230 行）。
//
// ⚠️ 偏差 D-9-2：RSI_PROVIDER_MODE / RSI_USE_MOCK_PROVIDER 环境变量分支删除
//（方案 C 仅 HARNESS 轨，mock provider 不做），懒装配仅 real 路径。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	rsi "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/rsi"
	rsihandlers "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/rsihandlers"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// rsiHandlers RSI 服务域分发句柄（懒加载，见 getRSIHandlers）。
var (
	rsiHandlersInstance *rsihandlers.AgentServerHandlers
	rsiHarnessProvider  rsi.HarnessProviderContract
	rsiWiringMu         sync.Mutex
)

// ──────────────────────────── 导出函数 ────────────────────────────

// HandleRSIRequest 处理任意 rsi.* unary method（含 Harness 安装）。
// 懒装配 RSI 服务域（每进程一次）并接线：
//   - harness_refs 快照提供方 = RSI active version，然后 controlled generic fallback；
//   - send_push 包装 = gateway_push 出口（E2A server_push 语义，零改动）。
func (s *AgentServer) HandleRSIRequest(ctx context.Context, request *schema.AgentRequest) {
	var resp *schema.AgentResponse
	// 实验入口开关（对齐 Python gateway rsi.enabled：关→入口拒绝）。
	if !rsi.RsiEnabledFromConfig(nil) && !rsi.EnvFallbackEnabled() {
		s.writeResponse(request.RequestID, request.ChannelID, rsiErrorResponse(
			request, "RSI 实验入口未开启", "RSI_DISABLED"))
		return
	}
	handlers, err := s.getRSIHandlers(ctx)
	if err != nil {
		logger.Error(rsiLogComponent).Err(err).Msg("rsi handlers bootstrap failed")
		resp = rsiErrorResponse(request, err.Error(), "INTERNAL_ERROR")
	} else {
		reqMap := map[string]any{
			"req_method": string(request.ReqMethod),
			"session_id": request.SessionID,
			"params":     decodeParams(request.Params),
		}
		result := handlers.HandleAsync(ctx, reqMap)
		if boolOf(result["ok"]) {
			resp = &schema.AgentResponse{
				RequestID: request.RequestID,
				ChannelID: request.ChannelID,
				OK:        true,
				Payload:   payloadMap(result["payload"]),
			}
		} else {
			resp = &schema.AgentResponse{
				RequestID: request.RequestID,
				ChannelID: request.ChannelID,
				OK:        false,
				Payload: map[string]any{
					"error": valueOf(result["error"]),
					"code":  valueOf(result["code"]),
				},
			}
		}
	}
	s.writeResponse(request.RequestID, request.ChannelID, resp)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getRSIHandlers 懒装配 RSI 服务域 + 分发句柄（每进程一次）。
// 装配序：build context → register HARNESS adapter + HarnessProvider →
// bind installer 广播 → handlers 构造（构造期 recover_workspace + 绑推送）。
func (s *AgentServer) getRSIHandlers(ctx context.Context) (*rsihandlers.AgentServerHandlers, error) {
	rsiWiringMu.Lock()
	defer rsiWiringMu.Unlock()
	if rsiHandlersInstance != nil {
		return rsiHandlersInstance, nil
	}
	tasksRoot := rsiWorkspaceTasksRoot()
	context := rsi.BuildRsiServiceContext(tasksRoot, true, false)
	// real 模式：HarnessProvider 真身（驱动的就是 17.4d 迭代引擎）。
	if rsiHarnessProvider == nil {
		rsiHarnessProvider = rsi.NewHarnessProvider(tasksRoot, context.ModelResolver)
	}
	context.RegisterHarnessProvider(rsiHarnessProvider)
	context.BindHarnessInstaller(func(old, new map[string]any) map[string]any {
		return s.AgentManager().BroadcastRSIHarnessChange(ctx, old, new)
	})
	refsProvider := func(params map[string]any) (string, error) {
		return rsiHarnessRefsProvider(params)
	}
	handlers := rsihandlers.NewAgentServerHandlers(context,
		func(msg map[string]any) error {
			return s.gatewayPushTransportFunc()(msg)
		},
		refsProvider, "web")
	rsiHandlersInstance = handlers
	return handlers, nil
}

// rsiHarnessRefsProvider 显式已装 Plugin 或激活 Harness 的三级解析
// （对齐 Python _rsi_harness_refs_provider；浏览器任意路径不在此解析）。
func rsiHarnessRefsProvider(params map[string]any) (string, error) {
	requestedID := strings.TrimSpace(valueOf(mapGet(params, "package_id")))
	if requestedID != "" {
		// 与 chat.send 同源 Plugin registry；不静默以 H0 顶替显式选择。
		// ⚠️ D-9-8 偏差登记：Python extension_package_manager.is_plugin_allowed/
		// resolve_plugin_dir 注册表 Go 侧无等价实现——以 Agent 工作区 plugins
		// 目录定位（安装即注册的既有形态），拒绝未安装与 MCP 依赖插件。
		dir := filepath.Join(workspace.AgentWorkspaceDir(), "plugins", requestedID)
		if !dirExistsSimple(dir) {
			return "", rsi.NewInvalidHarness(fmtInvalidPlugin(requestedID))
		}
		manifest := filepath.Join(dir, "manifest.json")
		data, err := os.ReadFile(manifest)
		if err != nil {
			return "", rsi.NewInvalidHarness(err.Error())
		}
		var payload map[string]any
		if err := json.Unmarshal(data, &payload); err != nil {
			return "", rsi.NewInvalidHarness(err.Error())
		}
		if payload == nil || payload["mcps"] != nil {
			return "", rsi.NewInvalidHarness("RSI isolated evaluation does not yet support Plugin MCP dependencies")
		}
		abs, _ := filepath.Abs(dir)
		return abs, nil
	}
	// 第二级：RSI 激活版本。
	activationRoot := rsiWorkspaceTasksRoot()
	activation := rsi.NewHarnessActivationStore(activationRoot)
	if active := activation.ResolveActiveRuntimePath(); active != "" {
		return active, nil
	}
	// 第三级：配置根 / 工作区 generic registry（initial_harness_refs.yaml 是
	// 受信基线输入：物化器在引擎看到之前把它复制进任务目录）。
	configured := strings.TrimSpace(os.Getenv("RSI_HARNESS_ROOT"))
	harnessRoot := ""
	if configured != "" {
		harnessRoot = expandTilde(configured)
	} else if home := os.Getenv("HOME"); home != "" {
		harnessRoot = filepath.Join(home, ".uapclawswarm", "rsi", "harness")
	}
	if harnessRoot != "" {
		initialRefs := filepath.Join(harnessRoot, "initial_harness_refs.yaml")
		if fileExistsSimple(initialRefs) {
			return initialRefs, nil
		}
	}
	// auto_harness packages 激活表（D-9-3：17.4e 跳过后恒为空表分支）。
	return "", nil
}

// ──────────────────────────── 小助手 ────────────────────────────

// decodeParams 参数 JSON → map。
func decodeParams(raw json.RawMessage) map[string]any {
	params := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &params)
	}
	return params
}

// payloadMap any → map（非 map 归一空表）。
func payloadMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}

// boolOf any → bool。
func boolOf(v any) bool {
	b, _ := v.(bool)
	return b
}

// valueOf any → string。
func valueOf(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// mapGet map 取键。
func mapGet(m map[string]any, key string) any {
	if m == nil {
		return nil
	}
	return m[key]
}

// rsiErrorResponse 错误响应组装。
func rsiErrorResponse(request *schema.AgentRequest, errMsg, code string) *schema.AgentResponse {
	return &schema.AgentResponse{
		RequestID: request.RequestID,
		ChannelID: request.ChannelID,
		OK:        false,
		Payload:   map[string]any{"error": errMsg, "code": code},
	}
}

// gatewayPushTransportFunc Gateway 下行推送出口（复用 AgentServer.SendPush，
// 对齐 adapter.SetDefaultSendPushFunc 注入形态；避免 server→gateway_push 环）。
func (s *AgentServer) gatewayPushTransportFunc() func(map[string]any) error {
	return func(msg map[string]any) error {
		return s.SendPush(context.Background(), msg)
	}
}

// rsiWorkspaceTasksRoot RSI 任务根（工作区 .uapclawswarm/rsi/tasks）。
func rsiWorkspaceTasksRoot() string {
	home := os.Getenv("HOME")
	if home == "" {
		home = "."
	}
	root := filepath.Join(home, ".uapclawswarm", "rsi", "tasks")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return root
	}
	return root
}

// expandTilde ~ 展开。
func expandTilde(p string) string {
	if strings.HasPrefix(p, "~") {
		home := os.Getenv("HOME")
		if home == "" {
			home = "."
		}
		return filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	return p
}

// fileExistsSimple 文件存在。
func fileExistsSimple(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// dirExistsSimple 目录存在。
func dirExistsSimple(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// fmtInvalidPlugin 插件不可用文案。
func fmtInvalidPlugin(id string) string {
	return "Plugin is not installed: " + id
}

// rsiLogComponent RSI 接线日志组件。
const rsiLogComponent = logger.ComponentAgentServer
