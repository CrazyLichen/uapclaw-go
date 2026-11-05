package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// 下游状态取值。
const (
	// downLocal 上游在网关本地实现，本层补齐即可真跑。
	downLocal = "local"
	// downAgentOK 上游薄代理，本仓已有 req method 常量与 AgentServer 处理器。
	downAgentOK = "agent:ok"
	// downAgentConst 上游薄代理，本仓有常量但 AgentServer 无处理器。
	downAgentConst = "agent:const-only"
	// downAgentMissing 上游薄代理，本仓既无常量也无处理器。
	downAgentMissing = "agent:missing"
	// downDep 依赖尚未迁移的子系统。
	downDep = "dep"
)

var (
	// reqMethodTokenRe 上游 proxy 调用里的 req method 常量名。
	reqMethodTokenRe = regexp.MustCompile(`ReqMethod\.([A-Z][A-Z0-9_]+)`)
	// pyDefRe 上游 handler 定义行（支持嵌套缩进）。
	pyDefRe = regexp.MustCompile(`^\s*(?:async\s+)?def\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	// pyAssignRe 上游 handler 用工厂赋值的情形（如 _project_git_status = _make_...）。
	pyAssignRe = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*_make_[A-Za-z0-9_]+\(`)
)

// depOverrides 依赖未迁移子系统的方法前缀 → 说明。
//
// 这些方法在上游也是代理或本地实现，但底层能力不在本仓，需等对应章节。
var depOverrides = map[string]string{
	"heartbeat.job.":  "缺 heartbeat job store",
	"openai_account.": "23.6 OpenAI 账号 OAuth",
	"task.asr.":       "ASR 子系统",
	"3rdagent.":       "web_proxy 子系统",
}

// upstreamSource 上游源码索引：handler 符号 → 函数体窗口。
type upstreamSource struct {
	// bodies handler 符号 → 函数体前若干行
	bodies map[string]string
}

// loadUpstreamSource 读取上游 gateway 下全部 py 文件，建立 handler 符号到函数体窗口的索引。
//
// 窗口取「def 行起，到下一个 def/赋值工厂为止，最多 80 行」——代理调用总出现在
// 函数体前部，足以覆盖判定所需信息；此处不追求完整 AST。
func loadUpstreamSource(root string) (*upstreamSource, error) {
	src := &upstreamSource{bodies: map[string]string{}}
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if strings.ToLower(filepath.Ext(path)) != ".py" {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("读取 %s: %w", path, readErr)
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			name := ""
			if m := pyDefRe.FindStringSubmatch(line); m != nil {
				name = m[1]
			} else if m := pyAssignRe.FindStringSubmatch(line); m != nil {
				name = m[1]
			}
			if name == "" {
				continue
			}
			end := i + 1
			for end < len(lines) && end < i+80 {
				if end > i+1 && (pyDefRe.MatchString(lines[end]) || pyAssignRe.MatchString(lines[end])) {
					break
				}
				end++
			}
			if _, exists := src.bodies[name]; !exists {
				src.bodies[name] = strings.Join(lines[i:end], "\n")
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return src, nil
}

// goCapabilities 本仓下游能力索引：req method 常量与 AgentServer 分支。
type goCapabilities struct {
	// constants req_method.go 中出现的常量标识符
	constants map[string]bool
	// serverCases handle_message.go 中出现的 case 标识符
	serverCases map[string]bool
}

// loadGoCapabilities 读取本仓 req method 常量表与 AgentServer 分发 switch。
func loadGoCapabilities(repo string) (*goCapabilities, error) {
	caps := &goCapabilities{constants: map[string]bool{}, serverCases: map[string]bool{}}
	constFile := filepath.Join(repo, "internal/swarm/schema/req_method.go")
	data, err := os.ReadFile(constFile)
	if err != nil {
		return nil, fmt.Errorf("读取 %s: %w", constFile, err)
	}
	for _, m := range regexp.MustCompile(`(ReqMethod[A-Za-z0-9]+)\s+ReqMethod\s*=`).FindAllStringSubmatch(string(data), -1) {
		caps.constants[m[1]] = true
	}

	serverFile := filepath.Join(repo, "internal/swarm/server/handle_message.go")
	data, err = os.ReadFile(serverFile)
	if err != nil {
		return nil, fmt.Errorf("读取 %s: %w", serverFile, err)
	}
	for _, m := range regexp.MustCompile(`case\s+schema\.(ReqMethod[A-Za-z0-9]+)`).FindAllStringSubmatch(string(data), -1) {
		caps.serverCases[m[1]] = true
	}
	return caps, nil
}

// snakeToCamel 把上游常量名转成本仓 req method 标识符后缀。
//
// 例：SESSION_GET_METADATA → SessionGetMetadata；HARNESS_PACKAGES_EXPORT → HarnessPackagesExport。
func snakeToCamel(s string) string {
	parts := strings.Split(strings.ToLower(s), "_")
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]))
		b.WriteString(p[1:])
	}
	return b.String()
}

// proxyHelperRe 代理工厂调用（如 _make_git_proxy_handler）。
var proxyHelperRe = regexp.MustCompile(`_make_[a-z0-9_]*proxy[a-z0-9_]*`)

// isProxyHandlerBody 判断上游 handler 函数体是否是薄代理。
//
// 上游有四类代理写法，缺一不可识别：
//  1. `proxy_unary_request(...)` —— 统一薄代理
//  2. `fetch_agent_unary(...)` —— 早期代理助手（project.remove 等）
//  3. `_forward_recommendation(...)` —— recommendation 族固定转发
//  4. `_make_git_proxy_handler(...)` —— git 族代理工厂
func isProxyHandlerBody(body string) bool {
	switch {
	case strings.Contains(body, "proxy_unary_request"),
		strings.Contains(body, "fetch_agent_unary"),
		strings.Contains(body, "_forward_recommendation"):
		return true
	default:
		return proxyHelperRe.MatchString(body)
	}
}

// classifyDownstream 判定某方法的下游状态。
//
// 判定顺序：依赖覆盖表 → 上游无 handler（config 代理族，本地语义）→
// 上游函数体含代理调用 → 查本仓常量/处理器；否则视为网关本地实现。
func classifyDownstream(method, handler string, src *upstreamSource, caps *goCapabilities) (string, string) {
	for prefix, note := range depOverrides {
		if strings.HasPrefix(method, prefix) {
			return downDep, note
		}
	}
	if handler == "" {
		return downLocal, "config 代理族（单用户走本地配置读写）"
	}
	body, ok := src.bodies[handler]
	if !ok {
		return downLocal, "handler 未定位（按本地实现处理）"
	}

	isProxy := isProxyHandlerBody(body)
	forwardRecommendation := strings.Contains(body, "_forward_recommendation")
	if !isProxy {
		return downLocal, "网关本地实现"
	}

	token := ""
	if m := reqMethodTokenRe.FindStringSubmatch(body); m != nil {
		token = m[1]
	} else if forwardRecommendation {
		// _forward_recommendation 内部固定使用 RECOMMENDATION 方法。
		token = "RECOMMENDATION"
	}
	if token == "" {
		return downAgentMissing, "代理目标 req method 未识别"
	}

	ident := "ReqMethod" + snakeToCamel(token)
	note := "代理 " + token
	switch {
	case caps.constants[ident] && caps.serverCases[ident]:
		return downAgentOK, note
	case caps.constants[ident]:
		return downAgentConst, note + "（有常量，AgentServer 无分支）"
	default:
		return downAgentMissing, note + "（本仓无该 req method）"
	}
}

// collectDownstream 为每个方法计算下游状态，返回 方法名 → (状态, 说明)。
func collectDownstream(methods map[string]string, src *upstreamSource, caps *goCapabilities) map[string]downstream {
	out := make(map[string]downstream, len(methods))
	for method, handler := range methods {
		status, note := classifyDownstream(method, handler, src, caps)
		out[method] = downstream{Status: status, Note: note}
	}
	return out
}

// downstream 下游状态与说明。
type downstream struct {
	// Status 状态取值见 down* 常量
	Status string
	// Note 人读说明
	Note string
}
