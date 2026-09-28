package launcher

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestNewOpener_Default 验证默认配置的初始化行为
func TestNewOpener_Default(t *testing.T) {
	service := NewOpener()
	if service == nil {
		t.Fatal("expected non-nil OpenService instance")
	}

	// 测试关闭行为
	err := service.Close()
	if err != nil {
		t.Fatalf("expected no error on close, got %v", err)
	}
}

// TestOpener_WithMaxConcurrency 验证自定义并发数配置
func TestOpener_WithMaxConcurrency(t *testing.T) {
	// 设置最大并发为 2
	service := NewOpener(WithMaxConcurrency(2))
	defer service.Close()

	opener, ok := service.(*Opener)
	if !ok {
		t.Fatal("expected service to be of type *Opener")
	}

	if cap(opener.sem) != 2 {
		t.Fatalf("expected semaphore capacity to be 2, got %d", cap(opener.sem))
	}
}

// TestOpener_ContextTimeout 验证当并发池满且超时时，请求能被正确取消
func TestOpener_ContextTimeout(t *testing.T) {
	// 最大并发设为 1
	service := NewOpener(WithMaxConcurrency(1))
	defer service.Close()

	// 占满并发槽（传入一个不存在的路径测试其排队和超时逻辑）
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// 模拟阻塞或者直接通过短超时测试限流和 Context 响应
	// 第一个请求（由于open.Start对不存在的文件可能会快速返回错误，我们重点测试限流排队）

	// 为了确保槽位被占用，我们手动填满信号量
	opener := service.(*Opener)
	opener.sem <- struct{}{} // 占满唯一槽位
	defer func() { <-opener.sem }()

	// 此时再发起请求，应该因为拿不到信号量而触发 context timeout
	err := service.Open(ctx, "non-existent-path-for-test")
	if err == nil {
		t.Fatal("expected error due to timeout, got nil")
	}
}

// TestOpener_ClosedService 验证服务关闭后拒绝新请求
func TestOpener_ClosedService(t *testing.T) {
	service := NewOpener()

	// 先关闭
	if err := service.Close(); err != nil {
		t.Fatalf("failed to close service: %v", err)
	}

	// 尝试在关闭后调用 Open
	err := service.Open(context.Background(), "any-path")
	if err != ErrServiceClosed {
		t.Fatalf("expected ErrServiceClosed, got %v", err)
	}

	// 尝试在关闭后调用 OpenAsync
	errCh := service.OpenAsync(context.Background(), "any-path")
	asyncErr := <-errCh
	if asyncErr != ErrServiceClosed {
		t.Fatalf("expected ErrServiceClosed in async call, got %v", asyncErr)
	}
}

// TestOpener_OpenAsync 验证异步调用机制及防协程泄露
func TestOpener_OpenAsync(t *testing.T) {
	service := NewOpener(WithMaxConcurrency(5))
	defer service.Close()

	// 异步调用一个必然会失败或存在的路径（利用不存在的路径快速返回错误验证异步通道闭环）
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errCh := service.OpenAsync(ctx, "invalid_test_path_12345")

	// 验证 channel 是否能正常收到返回值
	select {
	case err := <-errCh:
		// open.Start 对非法路径通常会返回错误，这里确保通道能正确吐出结果且不阻塞
		if err == nil {
			t.Log("warning: open.Start unexpectedly succeeded on an invalid path")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for OpenAsync result")
	}
}

func TestIsAppsFolderTarget(t *testing.T) {
	if !isAppsFolderTarget("shell:AppsFolder\\Microsoft.WindowsStore_8wekyb3d8bbwe!App") {
		t.Fatal("expected AppsFolder target to be recognized")
	}
	if isAppsFolderTarget("C:\\Programs\\app.exe") {
		t.Fatal("ordinary executable must not be treated as an AppsFolder target")
	}
}

// TestOpener_ConcurrencyStress 并发压力与并发限流保护测试
func TestOpener_ConcurrencyStress(t *testing.T) {
	// 限制最大并发为 3
	service := NewOpener(WithMaxConcurrency(3))
	defer service.Close()

	var wg sync.WaitGroup
	concurrency := 10

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()

			// 即使高并发涌入，内部信号量也会将其限制在 3 个并发以内
			_ = service.Open(ctx, "test_stress_path")
		}()
	}

	wg.Wait()
}
