package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// rpcSite 一个方法在某侧的登记点。
type rpcSite struct {
	// Method RPC 方法名
	Method string
	// Site 登记位置（文件:行）
	Site string
	// Handler 上游注册时绑定的 handler 符号（仅上游侧填充）
	Handler string
	// Token 间接注册时从调用处解析出的 req method 常量名（如 PERMISSIONS_RULES_GET）
	Token string
}

// methodPattern 方法名的统一形状：小写点分 2~4 段。
const methodPattern = `([a-z][a-z0-9_]*(?:\.[a-z0-9_]+){1,3})`

var (
	// frontendCallRe 前端包装器调用：call('x.y') / request("x.y") 等。
	frontendCallRe = regexp.MustCompile(`(?:call|request|invoke|rpc|send)\(\s*['"]` + methodPattern + `['"]`)
	// goRegisterRe 本仓注册：Register("x.y", h) 与 RegisterLocalHandler(path, "x.y", h)。
	goRegisterRe = regexp.MustCompile(`\.(?:Register|RegisterLocalHandler)\(\s*(?:[^,()]*,\s*)?["` + "`" + `]` + methodPattern + `["` + "`" + `]`)
	// pyRegisterRe 上游注册：register_method("x.y", h)。
	pyRegisterRe = regexp.MustCompile(`register_method\(\s*["']` + methodPattern + `["']\s*,\s*([A-Za-z_][A-Za-z0-9_]*)`)
	// pyConfigProxyRe 上游 config 代理注册：_register_config_proxy("x.y", _ConfigReq.XXX, ...)。
	pyConfigProxyRe = regexp.MustCompile(`_register_config_proxy\(\s*["']` + methodPattern + `["']`)
	// httpRouteMethodRe HTTP 路由表里的方法名（mapped.py 的第三个字符串、http_routes 的 dispatch 实参）。
	httpRouteMethodRe = regexp.MustCompile(`["']` + methodPattern + `["']`)
	// skipDirs 扫描时跳过的目录（依赖与构建产物）。
	skipDirs = map[string]bool{"node_modules": true, "dist": true, ".git": true}
)

// scanTree 遍历 root，按 exts 过滤文件，用 re 提取方法名，返回登记点列表。
//
// keep 为 nil 表示全部文件参与；非 nil 时按相对路径过滤（用于只扫 HTTP 路由表）。
// 返回值 Site 使用相对 root 的路径，便于矩阵里直接读懂。
func scanTree(root string, exts map[string]bool, re *regexp.Regexp, keep func(rel string) bool) ([]rpcSite, error) {
	var sites []rpcSite
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if !exts[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		relSlash := filepath.ToSlash(rel)
		if keep != nil && !keep(relSlash) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("读取 %s: %w", path, err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			for _, m := range re.FindAllStringSubmatch(line, -1) {
				site := rpcSite{Method: m[1], Site: fmt.Sprintf("%s:%d", relSlash, i+1)}
				if len(m) > 2 {
					site.Handler = m[2]
				}
				sites = append(sites, site)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return sites, nil
}

// skipTestSites 过滤掉测试文件里的登记点（测试夹具不是生产契约）。
func skipTestSites(sites []rpcSite) []rpcSite {
	out := sites[:0]
	for _, s := range sites {
		if strings.HasSuffix(strings.SplitN(s.Site, ":", 2)[0], "_test.go") ||
			strings.Contains(s.Site, "/tests/") || strings.Contains(s.Site, "test_") {
			continue
		}
		out = append(out, s)
	}
	return out
}

// scanFrontend 扫描前端源码里的 RPC 调用。
func scanFrontend(root string) ([]rpcSite, error) {
	return scanTree(root, map[string]bool{".ts": true, ".tsx": true, ".js": true, ".jsx": true}, frontendCallRe, nil)
}

// scanGo 扫描本仓的 RPC 注册点（排除测试文件，避免把夹具当契约）。
func scanGo(root string) ([]rpcSite, error) {
	sites, err := scanTree(root, map[string]bool{".go": true}, goRegisterRe, nil)
	if err != nil {
		return nil, err
	}
	return skipTestSites(sites), nil
}

// scanUpstream 扫描上游 Python 的 WS 注册点（直接 register_method + 间接注册助手）。
//
// registrars 为「函数体内会调 register_method 的本地助手名」，如 `_register_perm`；
// 这些助手的方法名字面量出现在调用处，必须单独扫，否则会误判成"上游没有"。
func scanUpstream(root string, registrars map[string]bool) ([]rpcSite, error) {
	sites, err := scanTree(root, map[string]bool{".py": true}, pyRegisterRe, nil)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(sites))
	for _, s := range sites {
		seen[s.Method] = true
	}

	if len(registrars) > 0 {
		names := make([]string, 0, len(registrars))
		for name := range registrars {
			names = append(names, regexp.QuoteMeta(name))
		}
		// 助手调用：NAME("x.y", _PermReq.XXX, ...) —— 第一段参数是方法名，
		// 第二段里的 `ReqMethod|_PermReq|_ConfigReq.XXX` 是代理目标常量。
		helperRe := regexp.MustCompile(`(?:` + strings.Join(names, "|") + `)\(\s*["']` + methodPattern + `["']\s*,\s*([^\n]*)`)
		helperSites, helperErr := scanTree(root, map[string]bool{".py": true}, helperRe, nil)
		if helperErr != nil {
			return nil, helperErr
		}
		for _, s := range helperSites {
			if seen[s.Method] {
				continue
			}
			// 间接注册：scanTree 把第二段参数塞进了 Handler 字段，这里转成 req method 常量。
			s.Token = reqTokenFromExpr(s.Handler)
			s.Handler = ""
			sites = append(sites, s)
			seen[s.Method] = true
		}
	}

	// 表驱动注册：`rsi_methods = [("x.y", ReqMethod.X), ...]` —— 方法名与常量成对出现。
	tableRe := regexp.MustCompile(`["']` + methodPattern + `["']\s*,\s*[A-Za-z_]*Req\.([A-Z][A-Z0-9_]*)`)
	tableSites, tableErr := scanTree(root, map[string]bool{".py": true}, tableRe, nil)
	if tableErr != nil {
		return nil, tableErr
	}
	for _, s := range tableSites {
		if seen[s.Method] {
			continue
		}
		// scanTree 把常量名放进了 Handler 字段。
		s.Token = s.Handler
		s.Handler = ""
		sites = append(sites, s)
		seen[s.Method] = true
	}
	return sites, nil
}

// reqTokenRe 从表达式里取 req method 常量名（ReqMethod.X / _PermReq.X / _HarnessReq.X …）。
var reqTokenRe = regexp.MustCompile(`[A-Za-z_]*Req\.([A-Z][A-Z0-9_]*)`)

// reqTokenFromExpr 从参数字符串里提取 req method 常量名；无法提取时返回空串。
func reqTokenFromExpr(expr string) string {
	if m := reqTokenRe.FindStringSubmatch(expr); m != nil {
		return m[1]
	}
	return ""
}

// scanConfigProxyMethods 返回上游用 `_register_config_proxy` 注册的方法集合。
//
// 这类方法在单用户部署下走网关本地 handler，只有 AgentOS 多用户才代理，
// 下游判定必须归为 local，不能按代理处理。
func scanConfigProxyMethods(root string) (map[string]bool, error) {
	sites, err := scanTree(root, map[string]bool{".py": true}, pyConfigProxyRe, nil)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(sites))
	for _, s := range sites {
		out[s.Method] = true
	}
	return out, nil
}

// scanUpstreamHTTP 扫描上游以 HTTP 路由暴露的方法名（不参与 WS 契约，只用于解释漂移）。
func scanUpstreamHTTP(root string) ([]rpcSite, error) {
	return scanTree(root, map[string]bool{".py": true}, httpRouteMethodRe, func(rel string) bool {
		return strings.Contains(rel, "http_routes/") || strings.HasSuffix(rel, "web_http_routes.py")
	})
}

// groupByMethod 把登记点按方法名聚合，站点列表去重。
func groupByMethod(sites []rpcSite) map[string][]string {
	out := make(map[string][]string, len(sites))
	seen := make(map[string]map[string]bool, len(sites))
	for _, s := range sites {
		if seen[s.Method] == nil {
			seen[s.Method] = map[string]bool{}
		}
		if seen[s.Method][s.Site] {
			continue
		}
		seen[s.Method][s.Site] = true
		out[s.Method] = append(out[s.Method], s.Site)
	}
	return out
}

// upstreamRef 上游某个方法的注册信息。
type upstreamRef struct {
	// Handler 直接注册时的 handler 符号
	Handler string
	// Token 间接注册时从调用处解析出的 req method 常量名
	Token string
}

// upstreamHandlers 返回 方法名 → 上游注册信息。
func upstreamHandlers(sites []rpcSite) map[string]upstreamRef {
	out := make(map[string]upstreamRef, len(sites))
	for _, s := range sites {
		ref := out[s.Method]
		if s.Handler != "" {
			ref.Handler = s.Handler
		}
		if s.Token != "" {
			ref.Token = s.Token
		}
		out[s.Method] = ref
	}
	return out
}
