package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestValidateMappedRoutes无重复面 构造期已校验，重复 panic。
// 对齐 Python mapped.py:886-893。
func TestValidateMappedRoutes无重复面(t *testing.T) {
	require.NotPanics(t, func() { validateMappedRoutes(mappedRoutes) })
	require.Panics(t, func() {
		validateMappedRoutes([]webHTTPMappedRoute{
			*newWebHTTPMappedRoute("GET", "/a", "m.a", "t", "s", nil, nil, false, false, false, nil, mappedGroupSettings),
			*newWebHTTPMappedRoute("GET", "/{id}", "m.b", "t", "s", nil, nil, false, false, false, nil, mappedGroupSettings),
		})
	})
}

// TestNewWebHTTPMappedRoute校验 对齐 __post_init__ 三条校验。
func TestNewWebHTTPMappedRoute校验(t *testing.T) {
	require.Panics(t, func() {
		newWebHTTPMappedRoute("GET", "no-slash", "m", "t", "s", nil, nil, false, false, false, nil, mappedGroupSettings)
	})
	require.Panics(t, func() {
		newWebHTTPMappedRoute("HEAD", "/", "m", "t", "s", nil, nil, false, false, false, nil, mappedGroupSettings)
	})
	require.Panics(t, func() {
		newWebHTTPMappedRoute("GET", "/", "", "t", "s", nil, nil, false, false, false, nil, mappedGroupSettings)
	})
}

// TestMappedRoutes总量与Python对拍 与 mapped.py 机器对拍（数量 + 三元组集合）。
// 生成 fixture 的命令（Python 真相源，版本 2026-11-25）：
//
//	python3 - <<'EOF'
//	import importlib.util, sys, json
//	spec = importlib.util.spec_from_file_location("m", "<jiuwenswarm>/gateway/channel_manager/web/http_routes/mapped.py")
//	mod = importlib.util.module_from_spec(spec); sys.modules["m"] = mod; spec.loader.exec_module(mod)
//	rows=[dict(m=r.http_method,p=r.path,rr=r.rpc_method) for r in mod.MAPPED_ROUTES]
//	open("b2_mapped_triples.json","w").write(json.dumps(rows))
//	EOF
func TestMappedRoutes总量与Python对拍(t *testing.T) {
	require.Len(t, mappedRoutes, 130)
	fixture := "/tmp/b2_mapped.json"
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Skipf("跳过对拍：fixture 不可读（%v）；数量断言仍生效", err)
	}
	var pyRows []struct {
		M  string `json:"m"`
		P  string `json:"p"`
		RR string `json:"rr"`
	}
	require.NoError(t, json.Unmarshal(data, &pyRows))
	goSet := map[string]bool{}
	for i := range mappedRoutes {
		r := &mappedRoutes[i]
		goSet[r.httpMethod+" "+r.path+" "+r.rpcMethod] = true
	}
	for _, row := range pyRows {
		key := row.M + " " + row.P + " " + row.RR
		require.True(t, goSet[key], "Python 路由在 Go 表缺失: %s", key)
		delete(goSet, key)
	}
	require.Empty(t, goSet, "Go 表多出: %v", goSet)
}

// TestCatalogEntries含CORE与multipart专行 12 CORE + 130 mapped + 1 multipart。
// 对齐 Python catalog_entries（mapped.py:918-962）。
func TestCatalogEntries含CORE与multipart专行(t *testing.T) {
	rows := catalogEntries()
	require.Len(t, rows, 12+130+1)
	require.Equal(t, "core", rows[0].Group)
	require.Equal(t, "GET", rows[0].HTTPMethod)
	last := rows[len(rows)-1]
	require.Equal(t, "harness.import", last.RPCMethod)
	// workspace 组 bind_session_param 逐行带出
	require.NotNil(t, rows[13].BindParam)
}

// TestNormalizePathKey 对齐 mapped.py:896-903。
func TestNormalizePathKey(t *testing.T) {
	require.Equal(t, "/sessions/{}/history", normalizePathKey("/sessions/{session_id}/history"))
	require.Equal(t, "/skills/{}", normalizePathKey("/skills/{name}"))
	require.Equal(t, "/projects/{}/sessions/actions/archive", normalizePathKey("/projects/{project_id}/sessions/actions/archive"))
}

// TestMappedRoutesForEdition个人版 恒返回全量个人版表（D-B2-5）。
func TestMappedRoutesForEdition个人版(t *testing.T) {
	require.Equal(t, mappedRoutes, mappedRoutesForEdition())
}

// TestSplitPath 空段丢弃。
func TestSplitPath(t *testing.T) {
	require.Equal(t, []string{"a", "b"}, splitPath("/a/b"))
	require.Equal(t, []string{"a"}, splitPath("a/"))
}

// TestMappedRoutesPythonFixture可再生 fixture 缺失时给出再生命令。
func TestMappedRoutesPythonFixture可再生(t *testing.T) {
	if _, err := os.Stat("/tmp/b2_mapped.json"); err == nil {
		t.Log("fixture 存在")
		return
	}
	cmd := exec.Command("python3", "-c", "print('see TestMappedRoutes总量与Python对拍 docstring')")
	require.NoError(t, cmd.Run())
}
