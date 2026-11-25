package web

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCatalogEntriesPythonFixture逐条对拍 与 Python catalog_entries() 全量对拍
// （fixture 由 TestMappedRoutes总量与Python对拍 docstring 同款方法生成，2026-11-25 版本；
// 数量 + (http_method,path,rpc_method,bind_session_param) 逐行一致）。
func TestCatalogEntriesPythonFixture逐条对拍(t *testing.T) {
	data, err := os.ReadFile("testdata/b2_catalog.json")
	if err != nil {
		t.Skipf("跳过对拍：fixture 不可读（%v）", err)
	}
	var pyRows []struct {
		HTTPMethod string `json:"http_method"`
		Path       string `json:"path"`
		RPCMethod  any    `json:"rpc_method"`
		Bind       *bool  `json:"bind_session_param"`
	}
	require.NoError(t, json.Unmarshal(data, &pyRows))
	goRows := catalogEntries()
	require.Len(t, goRows, len(pyRows))

	key := func(r catalogRow) string {
		k := r.HTTPMethod + "|" + r.Path + "|" + r.RPCMethod
		if r.BindParam != nil {
			if *r.BindParam {
				k += "|1"
			} else {
				k += "|0"
			}
		}
		return k
	}
	goSet := map[string]int{}
	for _, row := range goRows {
		goSet[key(row)]++
	}
	for _, row := range pyRows {
		rpc := ""
		if s, ok := row.RPCMethod.(string); ok {
			rpc = s
		}
		k := row.HTTPMethod + "|" + row.Path + "|" + rpc
		if row.Bind != nil {
			if *row.Bind {
				k += "|1"
			} else {
				k += "|0"
			}
		}
		require.Greater(t, goSet[k], 0, "Python catalog 行在 Go 缺失: %s", k)
		goSet[k]--
		if goSet[k] == 0 {
			delete(goSet, k)
		}
	}
	require.Empty(t, goSet, "Go catalog 多出: %v", goSet)
}
