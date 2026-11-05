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

// scanUpstream 扫描上游 Python 的 WS 注册点（含 config 代理），并带回 handler 符号。
func scanUpstream(root string) ([]rpcSite, error) {
	sites, err := scanTree(root, map[string]bool{".py": true}, pyRegisterRe, nil)
	if err != nil {
		return nil, err
	}
	proxies, err := scanTree(root, map[string]bool{".py": true}, pyConfigProxyRe, nil)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(sites))
	for _, s := range sites {
		seen[s.Method] = true
	}
	for _, p := range proxies {
		if seen[p.Method] {
			continue
		}
		// config 代理没有独立 handler 符号，标记为 proxy 由 classify 判定。
		p.Handler = ""
		sites = append(sites, p)
		seen[p.Method] = true
	}
	return sites, nil
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

// upstreamHandlers 返回 方法名 → 上游 handler 符号（无符号时为空串）。
func upstreamHandlers(sites []rpcSite) map[string]string {
	out := make(map[string]string, len(sites))
	for _, s := range sites {
		if s.Handler != "" {
			out[s.Method] = s.Handler
		} else if _, ok := out[s.Method]; !ok {
			out[s.Method] = ""
		}
	}
	return out
}
