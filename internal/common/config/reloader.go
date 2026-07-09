package config

import (
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Reloader 监听配置文件变更并触发回调。
//
// 设计要点：
//   - 监听配置文件所在目录（而非文件本身），因为部分编辑器保存时是 rename 操作
//   - 500ms 防抖，避免编辑器保存时多次触发
//   - 回调内传入新配置数据，调用方自行决定如何应用
type Reloader struct {
	watcher  *fsnotify.Watcher
	config   *Config
	mu       sync.Mutex
	stopCh   chan struct{}
	debounce time.Duration          // 防抖间隔
	handlers []func(map[string]any) // 变更回调列表
	done     chan struct{}          // 监听协程退出信号
	started  bool                  // 是否已启动
}

// ──────────────────────────── 常量 ────────────────────────────
const (
	// DefaultDebounce 默认防抖间隔。
	DefaultDebounce = 500 * time.Millisecond
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewReloader 创建配置热重载器。
//
// cfg: 要监听的配置管理器。
// 监听配置文件所在目录，文件变更时自动调用 cfg.Reload() 并触发回调。
func NewReloader(cfg *Config) (*Reloader, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("创建文件监听器失败: %w", err)
	}

	return &Reloader{
		watcher:  watcher,
		config:   cfg,
		debounce: DefaultDebounce,
		stopCh:   make(chan struct{}),
		done:     make(chan struct{}),
	}, nil
}

// OnReload 注册配置变更回调。
// 回调函数接收重载后的新配置数据。
func (r *Reloader) OnReload(fn func(map[string]any)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers = append(r.handlers, fn)
}

// Start 启动文件监听。
// 监听配置文件所在目录，目录下文件变更时触发重载。
func (r *Reloader) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.started {
		return nil
	}

	// 监听配置文件所在目录
	dir := filepath.Dir(r.config.Path())
	if err := r.watcher.Add(dir); err != nil {
		return fmt.Errorf("监听配置目录失败: %w", err)
	}

	r.started = true
	go r.watchLoop()

	log.Printf("[config] 已启动配置热重载监听，目录: %s", dir)
	return nil
}

// Stop 停止文件监听。
func (r *Reloader) Stop() error {
	r.mu.Lock()
	wasStarted := r.started
	r.mu.Unlock()

	if !wasStarted {
		// 未启动时直接关闭 watcher 即可，不需要等待 done
		_ = r.watcher.Close()
		return nil
	}

	close(r.stopCh)
	_ = r.watcher.Close()
	// 等待监听协程退出
	<-r.done
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// watchLoop 监听文件变更事件。
func (r *Reloader) watchLoop() {
	defer close(r.done)

	// 防抖定时器
	var timer *time.Timer
	configFile := filepath.Base(r.config.Path())

	for {
		select {
		case <-r.stopCh:
			if timer != nil {
				timer.Stop()
			}
			return

		case event, ok := <-r.watcher.Events:
			if !ok {
				return
			}
			// 只关注目标配置文件的 Write/Create/Rename 事件
			if filepath.Base(event.Name) != configFile {
				continue
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) {
				// 防抖：重置定时器
				if timer != nil {
					timer.Stop()
				}
				timer = time.AfterFunc(r.debounce, func() {
					r.reload()
				})
			}

		case _, ok := <-r.watcher.Errors:
			if !ok {
				return
			}
			// 监听错误，记录日志但继续运行
			log.Printf("[config] 文件监听错误，继续运行")
		}
	}
}

// reload 执行配置重载并触发回调。
func (r *Reloader) reload() {
	if err := r.config.Reload(); err != nil {
		log.Printf("[config] 配置热重载失败: %v", err)
		return
	}

	log.Printf("[config] 配置热重载成功")

	// 获取最新配置数据（深拷贝，避免外部修改影响内部状态）
	r.config.mu.RLock()
	dataCopy := deepCopyMap(r.config.data)
	r.config.mu.RUnlock()

	// 触发所有回调
	r.mu.Lock()
	handlers := make([]func(map[string]any), len(r.handlers))
	copy(handlers, r.handlers)
	r.mu.Unlock()

	for _, fn := range handlers {
		fn(dataCopy)
	}
}
