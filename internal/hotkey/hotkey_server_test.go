package hotkey

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.design/x/hotkey/mainthread"
)

// 测试用的快捷键定义
var (
	testHotkeyA = Hotkey{Modifiers: ModControl | ModAlt | ModShift, Key: 0x77} // Ctrl + Alt + Shift + F8
	testHotkeyB = Hotkey{Modifiers: ModControl | ModAlt | ModShift, Key: 0x78} // Ctrl + Alt + Shift + F9
)

func TestMain(m *testing.M) {
	mainthread.Init(func() {
		var code int
		mainthread.Call(func() {
			code = m.Run()
		})
		os.Exit(code)
	})
}

// 1. 测试动态 Register 与 IsRegistered
func TestServer_RegisterAndIsRegistered(t *testing.T) {
	server := NewKeyServer()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer server.UnregisterAll() // 避免泄漏给下一个 Test

	// 1.1 初始未注册
	if server.IsRegistered(testHotkeyA) {
		t.Fatal("expected testHotkeyA NOT to be registered initially")
	}

	// 1.2 动态注册并向 OS 绑定
	id, err := server.Register(ctx, testHotkeyA, func() {})
	if err != nil {
		t.Fatalf("unexpected error during Register: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty handlerID")
	}

	// 1.3 验证内部状态
	if !server.IsRegistered(testHotkeyA) {
		t.Fatal("expected testHotkeyA to be registered")
	}

	// 1.4 测试防重注册
	_, err = server.Register(ctx, testHotkeyA, func() {})
	if !errors.Is(err, ErrHotkeyAlreadyBound) {
		t.Fatalf("expected ErrHotkeyAlreadyBound, got: %v", err)
	}
}

// 2. 测试 Unregister 与 UnregisterAll
func TestServer_Unregister(t *testing.T) {
	server := NewKeyServer()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer server.UnregisterAll()

	idA, err := server.Register(ctx, testHotkeyA, func() {})
	if err != nil {
		t.Fatalf("failed to register hotkeyA: %v", err)
	}

	_, err = server.Register(ctx, testHotkeyB, func() {})
	if err != nil {
		t.Fatalf("failed to register hotkeyB: %v", err)
	}

	// 2.1 单个注销 testHotkeyA
	if err := server.Unregister(idA); err != nil {
		t.Fatalf("unexpected error on Unregister: %v", err)
	}

	if server.IsRegistered(testHotkeyA) {
		t.Error("expected testHotkeyA to be unregistered")
	}
	if !server.IsRegistered(testHotkeyB) {
		t.Error("expected testHotkeyB to STILL be registered")
	}

	// 2.2 测试注销不存在的 ID
	if err := server.Unregister("invalid_id"); !errors.Is(err, ErrHotkeyHandlerNotFound) {
		t.Errorf("expected ErrHotkeyHandlerNotFound, got: %v", err)
	}

	// 2.3 批量注销所有
	if err := server.UnregisterAll(); err != nil {
		t.Fatalf("unexpected error on UnregisterAll: %v", err)
	}

	if server.IsRegistered(testHotkeyB) {
		t.Error("expected testHotkeyB to be unregistered after UnregisterAll")
	}
}

// 3. 测试 IsSystemRegister 探针功能
func TestServer_IsSystemRegister(t *testing.T) {
	server := NewKeyServer()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer server.UnregisterAll()

	// 3.1 内部已注册 -> 应返回 true
	_, err := server.Register(ctx, testHotkeyA, func() {})
	if err != nil {
		t.Fatalf("failed to register hotkeyA: %v", err)
	}

	if !server.IsSystemRegister(testHotkeyA) {
		t.Error("expected IsSystemRegister to return true for locally bound hotkey")
	}

	// 3.2 未注册 -> 通过 OS 探测
	_ = server.IsSystemRegister(testHotkeyB)
}

// 4. 测试 Listen 阻塞等待与 Context 优雅退出
func TestServer_ListenLifecycle(t *testing.T) {
	server := NewKeyServer()
	ctx, cancel := context.WithCancel(context.Background())

	// 注册一个热键
	_, err := server.Register(ctx, testHotkeyA, func() {})
	if err != nil {
		t.Fatalf("failed to register hotkey: %v", err)
	}

	listenErrChan := make(chan error, 1)

	// 启动 Listen (后台阻塞等待)
	go func() {
		listenErrChan <- server.Listen(ctx)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-listenErrChan:
		if err != nil {
			t.Fatalf("Listen returned error upon cancellation: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Listen timeout: failed to exit cleanly after context cancelled")
	}

	if server.IsRegistered(testHotkeyA) {
		t.Error("expected Listen to unregister all hotkeys on context cancellation")
	}
}

// 5. 并发安全性测试
func TestServer_ConcurrentOperations(t *testing.T) {
	server := NewKeyServer()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer server.UnregisterAll()

	var wg sync.WaitGroup
	var successCount int32

	for i := 0; i < 50; i++ {
		wg.Add(2)
		hk := Hotkey{Modifiers: ModControl | ModAlt, Key: uint32(0x41 + (i % 26))}

		go func(h Hotkey) {
			defer wg.Done()
			if _, err := server.Register(ctx, h, func() {}); err == nil {
				atomic.AddInt32(&successCount, 1)
			}
		}(hk)

		go func(h Hotkey) {
			defer wg.Done()
			_ = server.IsRegistered(h)
			_ = server.IsSystemRegister(h)
		}(hk)
	}

	wg.Wait()

	if atomic.LoadInt32(&successCount) == 0 {
		t.Error("expected at least some hotkeys to register successfully")
	}
}
