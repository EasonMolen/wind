package hotkey

import (
	"context"
	"errors"
	"strings"
)

var (
	ErrInvalidHotKey         = errors.New("invalid hotkey format")
	ErrEmptyHotKey           = errors.New("hotkey cannot be empty")
	ErrUnknownKey            = errors.New("unknown key in hotkey definition")
	ErrHotkeyAlreadyBound    = errors.New("hotkey already bound")
	ErrHotkeyHandlerNotFound = errors.New("hotkey handler not found")
)

// Windows / 通用 OS 修饰键常量定义
const (
	ModAlt     uint32 = 0x0001
	ModControl uint32 = 0x0002
	ModShift   uint32 = 0x0004
	ModWin     uint32 = 0x0008
)

// KeyMap 常见虚拟键码 (Virtual Key Codes) 映射表
var KeyMap = map[string]uint32{
	// 字母 (A-Z)
	"a": 0x41, "b": 0x42, "c": 0x43, "d": 0x44, "e": 0x45, "f": 0x46, "g": 0x47,
	"h": 0x48, "i": 0x49, "j": 0x4A, "k": 0x4B, "l": 0x4C, "m": 0x4D, "n": 0x4E,
	"o": 0x4F, "p": 0x50, "q": 0x51, "r": 0x52, "s": 0x53, "t": 0x54, "u": 0x55,
	"v": 0x56, "w": 0x57, "x": 0x58, "y": 0x59, "z": 0x5A,

	// 数字 (0-9)
	"0": 0x30, "1": 0x31, "2": 0x32, "3": 0x33, "4": 0x34,
	"5": 0x35, "6": 0x36, "7": 0x37, "8": 0x38, "9": 0x39,

	// 功能键 (F1-F12)
	"f1": 0x70, "f2": 0x71, "f3": 0x72, "f4": 0x73, "f5": 0x74, "f6": 0x75,
	"f7": 0x76, "f8": 0x77, "f9": 0x78, "f10": 0x79, "f11": 0x7A, "f12": 0x7B,

	// 特殊常用键
	"space": 0x20, "enter": 0x0D, "return": 0x0D, "tab": 0x09, "esc": 0x1B,
	"escape": 0x1B, "backspace": 0x08, "delete": 0x2E, "del": 0x2E,
	"up": 0x26, "down": 0x28, "left": 0x25, "right": 0x27,
}

type Hotkey struct {
	Modifiers uint32
	Key       uint32
}

type KeyService interface {
	// Register 注册一个快捷键及其对应的触发回调函数
	// 返回一个 handlerID (便于后续取消) 或全局唯一的 error (如按键冲突 ErrHotkeyAlreadyBound)
	Register(ctx context.Context, hk Hotkey, handler func()) (handlerID string, err error)

	// Unregister 取消指定已注册的快捷键
	Unregister(id string) error

	// UnregisterAll 清空当前服务实例注册的所有快捷键 (常用于页面切换或组件销毁)
	UnregisterAll() error

	// Listen 开始监听热键事件 (通常是阻塞式的事件循环，或在后台 goroutine 运行)
	// 传入 context 用于控制监听服务的停止
	Listen(ctx context.Context) error

	// IsRegistered 检查某个快捷键是否已经被当前程序占用
	IsRegistered(hk Hotkey) bool

	// IsSystemRegister 检查某个快捷键是否已经被当前系统或其它应用占用
	IsSystemRegister(hk Hotkey) bool
}

func ParseHotkey(hotkey string) (Hotkey, error) {
	var h Hotkey

	// 去除首发空白，防空
	hotkey = strings.TrimSpace(hotkey)
	if hotkey == "" {
		return h, ErrInvalidHotKey
	}

	// 按"+"拆分组合键
	rawTokens := strings.Split(hotkey, "+")
	var tokens []string
	for _, t := range rawTokens {
		trimmed := strings.ToLower(strings.TrimSpace(t))
		if trimmed != "" {
			tokens = append(tokens, trimmed)
		}
	}
	if len(tokens) == 0 {
		return h, ErrEmptyHotKey
	}

	// 最后一个 token 必须是“主按键”（如 A, F5, Space 等）
	mainKeyStr := tokens[len(tokens)-1]

	// 前面的 tokens 均为"修饰键"
	modifierTokens := tokens[:len(tokens)-1]

	// 解析修饰键 (允许多个修饰键，如 Ctrl+Alt+Shift)
	for _, mod := range modifierTokens {
		switch mod {
		case "alt":
			h.Modifiers |= ModAlt
		case "control", "ctrl":
			h.Modifiers |= ModControl
		case "shift":
			h.Modifiers |= ModShift
		case "win", "super":
			h.Modifiers |= ModWin
		default:
			return h, ErrUnknownKey
		}
	}

	// 解析主按键
	vkCode, ok := parseMainKey(mainKeyStr)
	if !ok {
		return h, ErrUnknownKey
	}
	h.Key = vkCode
	return h, nil
}

func parseMainKey(mainKeyStr string) (uint32, bool) {
	// 先查表 (处理 F1-F12, Space, Enter 等)
	if vk, found := KeyMap[mainKeyStr]; found {
		return vk, true
	}

	if len(mainKeyStr) == 1 {
		char := mainKeyStr[0]
		if char >= 'a' && char <= 'z' {
			return uint32(char - 'a' + 'A'), true // 转大写 VK 码
		}
		if char >= '0' && char <= '9' {
			return uint32(char), true
		}
	}

	return 0, false
}
