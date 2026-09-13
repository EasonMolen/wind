package launcher

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/qiniu/open"
)

var (
	ErrServiceClosed = errors.New("launcher: service is closed")
	ErrTimeout       = errors.New("launcher: open operation timed out or canceled")
)

type OpenService interface {
	// Open 同步打开资源，支持 Context 超时控制与并发限流
	Open(ctx context.Context, path string) error
	// OpenWith 使用指定的软件同步打开资源，支持 Context 超时控制
	OpenWith(ctx context.Context, useAppName string, args ...string) error
	// OpenAsync 异步打开资源，执行结果通过非阻塞 Channel 返回
	OpenAsync(ctx context.Context, path string) <-chan error
	// Close 优雅关闭服务，阻止新任务并等待运行中的任务完成
	Close() error
}

type Opener struct {
	sem    chan struct{}  // 信号量，控制最大并发 OS 进程数
	wg     sync.WaitGroup // 追踪正在执行的任务，用于优雅关闭
	mu     sync.RWMutex   // 保护 closed 状态的线程安全
	closed bool
}

// Option 采用函数式配置模式（Functional Options Pattern）
type Option func(*Opener)

// WithMaxConcurrency 设置最大并发打开数，防止并发过高耗尽操作系统资源
func WithMaxConcurrency(max int) Option {
	return func(op *Opener) {
		if max > 0 {
			op.sem = make(chan struct{}, max)
		}
	}
}

func NewOpener(opts ...Option) OpenService {
	op := &Opener{
		sem: make(chan struct{}, 10),
	}

	for _, opt := range opts {
		opt(op)
	}
	return op
}

func (op *Opener) Open(ctx context.Context, path string) error {
	op.mu.RLock()
	if op.closed {
		op.mu.RUnlock()
		return ErrServiceClosed
	}
	op.mu.RUnlock()

	select {
	case op.sem <- struct{}{}:
	case <-ctx.Done():
		return fmt.Errorf("%w: %v", ErrTimeout, ctx.Err())
	}

	op.wg.Add(1)
	defer func() {
		<-op.sem // 执行完毕释放信号量
		op.wg.Done()
	}()

	// 再次检查 Context 状态，避免在信号量排队期间任务已被取消
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrTimeout, err)
	}

	if err := open.Start(path); err != nil {
		return fmt.Errorf("failed to open path [%s]: %w", path, err)
	}

	return nil
}

func (op *Opener) OpenWith(ctx context.Context, appName string, args ...string) error {
	op.mu.RLock()
	if op.closed {
		op.mu.RUnlock()
		return ErrServiceClosed
	}
	op.mu.RUnlock()

	op.wg.Add(1)
	defer func() {
		<-op.sem
		op.wg.Done()
	}()

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrTimeout, err)
	}

	//if err := open.StartWith(path, appName); err != nil {
	//	return fmt.Errorf("failed to open path [%s]: %w with %s", path, err, appName)
	//}
	if err := exec.Command(appName, args...).Start(); err != nil {
		fmt.Errorf("failed to open path [%s]: %w with %s", args[1], err, appName)
	}

	return nil
}

// OpenAsync 异步打开资源，适用于不想阻塞主调用线程的高并发场景
func (op *Opener) OpenAsync(ctx context.Context, path string) <-chan error {
	// 使用缓冲为 1 的 Channel，避免调用方不读取返回值导致协程泄露 (Goroutine Leak)
	errCh := make(chan error, 1)

	go func() {
		defer close(errCh)
		errCh <- op.Open(ctx, path)
	}()

	return errCh
}

// Close 优雅关闭服务
func (op *Opener) Close() error {
	op.mu.Lock()
	if op.closed {
		op.mu.Unlock()
		return ErrServiceClosed
	}
	op.closed = true
	op.mu.Unlock()

	// 等待所有正在执行的 open 动作完毕
	op.wg.Wait()
	return nil
}

func cleaninput(input string) string {
	r := strings.NewReplacer("&", "^&")
	return r.Replace(input)
}
