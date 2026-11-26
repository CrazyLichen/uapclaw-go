package visualization

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestToMermaidPNG_真实链 httptest 承载 mermaid.ink 端点（规则 3.3 网络端点边界 mock）
func TestToMermaidPNG_真实链(t *testing.T) {
	var gotPath string
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, "PNGBYTES")
	}))
	defer srv.Close()
	t.Setenv("MERMAID_INK_SERVER", srv.URL)

	d := NewDrawable()
	d.addNode("a", &fakeBranchSource{})
	script, err := d.ToMermaid("", false, false)
	require.NoError(t, err)
	png, err := d.ToMermaidPNG("", false)
	require.NoError(t, err)
	assert.Equal(t, "PNGBYTES", string(png))
	assert.True(t, strings.HasPrefix(gotPath, "/img/"))
	wantB64 := base64.URLEncoding.EncodeToString([]byte(script))
	assert.Equal(t, "/img/"+wantB64, gotPath)
	assert.Equal(t, "format=png", gotQuery)
}

// TestToMermaidSVG_尾部问号 query 空仍带 ?（__main__.py:201-206 svg_url 拼接）
func TestToMermaidSVG_尾部问号(t *testing.T) {
	var gotReqURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReqURI = r.URL.RequestURI()
		_, _ = io.WriteString(w, "<svg/>")
	}))
	defer srv.Close()
	t.Setenv("MERMAID_INK_SERVER", srv.URL)

	d := NewDrawable()
	d.addNode("a", &fakeBranchSource{})
	svg, err := d.ToMermaidSVG("", false)
	require.NoError(t, err)
	assert.Equal(t, "<svg/>", string(svg))
	assert.True(t, strings.HasSuffix(gotReqURI, "?"))
}

// TestToMermaidPNG_服务错误 非2xx → error 含 status（MermaidError 复刻）
func TestToMermaidPNG_服务错误(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	t.Setenv("MERMAID_INK_SERVER", srv.URL)
	d := NewDrawable()
	d.addNode("a", &fakeBranchSource{})
	_, err := d.ToMermaidPNG("", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "502")
}

// TestTruncateDetails Details 段 200 字符截断（__main__.py:119-123）
func TestTruncateDetails(t *testing.T) {
	assert.Equal(t, "abc", truncateDetails([]byte("abc")))
	long := strings.Repeat("x", 300)
	got := truncateDetails([]byte(long))
	assert.Len(t, got, 203)
	assert.True(t, strings.HasSuffix(got, "..."))
}

// TestToMermaidPNG_参数错误透传 expand 非法时错误直接上抛不触网
func TestToMermaidPNG_参数错误透传(t *testing.T) {
	t.Setenv("MERMAID_INK_SERVER", "http://127.0.0.1:1") // 不可达端口
	d := NewDrawable()
	d.addNode("a", &fakeBranchSource{})
	_, err := d.ToMermaidPNG("", 1.5)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expand_subgraph")
}
