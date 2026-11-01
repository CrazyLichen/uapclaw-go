package web

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DistSource 前端静态资源来源。
//
// 两种来源（对齐 Python _default_dist_dir 的优先级链 + Go 独有的编译期内嵌兜底）：
//   - 目录来源：用户数据根下的 channels/web/frontend/dist，或 --dist 显式指定
//   - 内嵌来源：编译期 go:embed 的 frontend/dist
type DistSource struct {
	// FS 静态资源根文件系统（已定位到 dist 目录本身）
	FS fs.FS
	// Path 实际使用的目录绝对路径；内嵌来源为空
	Path string
	// Embedded 是否为编译期内嵌来源
	Embedded bool
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DistDirEnv 显式指定前端产物目录的环境变量名。
	//
	// 由 cmd 层的 --dist 选项写入（本仓库既有惯例：--host/--port 也是覆写环境变量），
	// 语义对齐 Python `--dist`（app_web.py L985-991）。
	DistDirEnv = "UAPCLAW_WEB_DIST"

	// placeholderMarker 占位 index.html 的特征串（未构建前端时仓库内提交的占位页）。
	placeholderMarker = "前端构建产物未部署"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolveDist 解析前端静态资源来源。
//
// 优先级（对齐 Python _default_dist_dir，app_web.py L53-66）：
//  1. explicitDir 非空（即 --dist 显式指定）：必须存在且是目录，否则返回 error
//     （Python 为 SystemExit，语义一致：显式指定错即启动失败）；
//  2. 用户数据根 <WorkspaceDir>/channels/web/frontend/dist 存在且是目录 → 用它
//     （Python Priority 1，装好之后可替换前端而无需重编译）；
//  3. 回落到编译期内嵌的 frontend/dist（Go 独有；Python 为包内 dist 兜底）。
//
// 路径基准为 WorkspaceDir()（= Python get_root_dir()：用户数据根，~/.uapclaw）。
func ResolveDist(explicitDir string) (DistSource, error) {
	return resolveDist(pathutil.WorkspaceDir(), explicitDir)
}

// SPAHandler 返回前端静态文件处理器（未命中时回退 index.html）。
//
// 来源按 ResolveDist 的优先级链在**每次构造时**解析一次；解析失败（仅 --dist
// 显式指定才可能失败）时记录 Error 并回落内嵌资源——单进程里前端目录配置错误
// 不应拖垮 AgentServer（与 Python 独立进程 SystemExit 的有意差异，见 doc.go）。
func SPAHandler() http.Handler {
	src, err := ResolveDist(os.Getenv(DistDirEnv))
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("env", DistDirEnv).
			Msg("frontend dist resolve failed, falling back to embedded assets")
		src = embeddedDistSource()
	}
	return src.SPAHandler()
}

// SPAHandler 把本来源包装为带 SPA fallback 的 http.Handler。
func (d DistSource) SPAHandler() http.Handler {
	fileServer := http.FileServer(http.FS(d.FS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqPath := r.URL.Path
		// 非根路径且非 assets 前缀时，文件不存在则回退到 index.html（SPA 路由）
		if reqPath != "/" && !strings.HasPrefix(reqPath, "/assets/") {
			filePath := strings.TrimPrefix(reqPath, "/")
			if _, statErr := fs.Stat(d.FS, filePath); statErr != nil {
				// 用克隆避免改写调用方的请求对象（FileServer 只读 Path）
				r2 := r.Clone(r.Context())
				r2.URL.Path = "/"
				fileServer.ServeHTTP(w, r2)
				return
			}
		}
		fileServer.ServeHTTP(w, r)
	})
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveDist ResolveDist 的可测内核：root 为数据根，explicitDir 为显式目录。
func resolveDist(root, explicitDir string) (DistSource, error) {
	if explicitDir != "" {
		abs, err := filepath.Abs(explicitDir)
		if err != nil {
			return DistSource{}, fmt.Errorf("解析前端目录失败：%w", err)
		}
		info, err := os.Stat(abs)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return DistSource{}, fmt.Errorf("前端目录不存在：%s", abs)
			}
			return DistSource{}, fmt.Errorf("检查前端目录失败：%w", err)
		}
		if !info.IsDir() {
			return DistSource{}, fmt.Errorf("前端路径不是目录：%s", abs)
		}
		return DistSource{FS: os.DirFS(abs), Path: abs}, nil
	}

	userDist := filepath.Join(root, "channels", "web", "frontend", "dist")
	if info, err := os.Stat(userDist); err == nil && info.IsDir() {
		if isPlaceholderDist(os.DirFS(userDist)) {
			logger.Warn(logComponent).
				Str("dist_dir", userDist).
				Msg("user workspace frontend dist is a placeholder build, served as-is")
		}
		return DistSource{FS: os.DirFS(userDist), Path: userDist}, nil
	}

	src := embeddedDistSource()
	if isPlaceholderDist(src.FS) {
		logger.Warn(logComponent).
			Msg("embedded frontend assets are a placeholder build, run `make web-build` before `go build`")
	}
	return src, nil
}

// embeddedDistSource 返回编译期内嵌的前端资源来源。
func embeddedDistSource() DistSource {
	sub, err := fs.Sub(frontendDist, "frontend/dist")
	if err != nil {
		// 嵌入目录在编译期确定，此处不可达；交由 SPAHandler 以空 FS 兜底
		logger.Error(logComponent).
			Err(err).
			Msg("embedded frontend dist unavailable")
		return DistSource{FS: frontendDist, Embedded: true}
	}
	return DistSource{FS: sub, Embedded: true}
}

// isPlaceholderDist 判断 dist 是否为未构建的占位页（仓库内提交的那份）。
func isPlaceholderDist(distFS fs.FS) bool {
	data, err := fs.ReadFile(distFS, "index.html")
	if err != nil {
		return false
	}
	return strings.Contains(string(data), placeholderMarker)
}
