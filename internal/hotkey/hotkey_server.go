package hotkey

import (
	"context"
	"fmt"
	"sync"

	gohk "golang.design/x/hotkey"
)

type binding struct {
	id      string
	hotkey  Hotkey
	hkObj   *gohk.Hotkey
	handler func()
}

type Server struct {
	mu       sync.RWMutex        // 读写锁 保护 bindings map 的读写
	probeMu  sync.Mutex          // 探针锁 专门保护 OS 试探注册过程，避免并发试探互殴
	bindings map[string]*binding // key: handlerID
}

func NewKeyServer() KeyServer {
	return &Server{
		bindings: make(map[string]*binding),
	}
}

// Register 将自定义 Hotkey 转换为 golang.design/x/hotkey 并进行注册
func (s *Server) Register(ctx context.Context, hk Hotkey, handler func()) (handlerID string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := s.getHandlerID(hk)

	// 防重注册检查
	if _, exists := s.bindings[id]; exists {
		return "", ErrHotkeyAlreadyBound
	}

	// 映射修饰键(Modifiers)
	mods := convertModifiers(hk.Modifiers)

	// 映射主按键(Key)
	mainKey := gohk.Key(hk.Key)

	// 创建系统的Hotkey实体
	hkObj := gohk.New(mods, mainKey)

	if err = hkObj.Register(); err != nil {
		return "", fmt.Errorf("failed to register with OS: %w", err)
	}

	b := &binding{
		id:      id,
		hotkey:  hk,
		hkObj:   hkObj,
		handler: handler,
	}

	s.bindings[id] = b

	// 开启专属监听协程
	go func(b *binding) {
		for {
			select {
			case <-ctx.Done():
				return
			case <-b.hkObj.Keydown():
				if b.handler != nil {
					b.handler()
				}
			}
		}

	}(b)

	return id, nil
}

// Unregister 卸载指定热键
func (s *Server) Unregister(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.unregisterLocked(id)
}

// UnregisterAll 清空所有热键
func (s *Server) UnregisterAll() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var firstErr error
	for id := range s.bindings {
		if err := s.unregisterLocked(id); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Listen 启动热键监听循环
func (s *Server) Listen(ctx context.Context) error {
	// 阻塞等待上下文结束，退出时统一释放OS资源
	<-ctx.Done()

	// 收到退出信号,统一向OS注销所有的热键
	return s.UnregisterAll()
}

func (s *Server) IsRegistered(hk Hotkey) bool {
	s.mu.RLock() // 开启读锁，允许多个协程同时查询，但阻止并发写入
	defer s.mu.RUnlock()

	id := s.getHandlerID(hk)
	_, exists := s.bindings[id]
	return exists // 直接返回 bool，极为简洁
}

func (s *Server) IsSystemRegister(hk Hotkey) bool {

	if s.IsRegistered(hk) {
		return true
	}

	// 尝试临时向操作系统注册一次

	// 映射修饰键(Modifiers)
	mods := convertModifiers(hk.Modifiers)

	// 映射主按键(Key)
	mainKey := gohk.Key(hk.Key)

	s.probeMu.Lock()
	defer s.probeMu.Unlock()

	// 创建系统的Hotkey实体
	hkObj := gohk.New(mods, mainKey)

	if err := hkObj.Register(); err != nil {
		// 注册失败，说明被系统或其他程序抢占了
		return true
	}

	// 上一步的试探注册成功,说明快捷键可用,要立刻注销还原,保持OS干净
	_ = hkObj.Unregister()
	return false
}

func (s *Server) getHandlerID(hk Hotkey) string {
	return fmt.Sprintf("hk_%d_%d", hk.Modifiers, hk.Key)
}

// convertModifiers 将自定义的 Modifiers 位掩码转换为 golang.design/x/hotkey 识别的修饰键切片
func convertModifiers(mods uint32) []gohk.Modifier {
	var result []gohk.Modifier

	// 这里的 ModAlt, ModControl 等对应上一轮定义的常量
	if mods&ModAlt != 0 {
		result = append(result, gohk.ModAlt)
	}
	if mods&ModControl != 0 {
		result = append(result, gohk.ModCtrl)
	}
	if mods&ModShift != 0 {
		result = append(result, gohk.ModShift)
	}
	if mods&ModWin != 0 {
		result = append(result, gohk.ModWin)
	}

	return result
}

func (s *Server) unregisterLocked(id string) error {
	b, exists := s.bindings[id]
	if !exists {
		return ErrHotkeyHandlerNotFound
	}

	// 注销 OS 层面的热键绑定
	if err := b.hkObj.Unregister(); err != nil {
		return fmt.Errorf("failed to unregister hotkey: %w", err)
	}

	delete(s.bindings, id)
	return nil
}
