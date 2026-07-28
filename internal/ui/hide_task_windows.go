//go:build windows

package ui

import (
	"fmt"
	"os"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32                    = syscall.NewLazyDLL("user32.dll")
	procEnumWindows           = user32.NewProc("EnumWindows")
	procGetWindowTextW        = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW  = user32.NewProc("GetWindowTextLengthW")
	procGetWindowThreadProcId = user32.NewProc("GetWindowThreadProcessId")
	procGetWindowLongPtrW     = user32.NewProc("GetWindowLongPtrW")
	procGetWindowLongW        = user32.NewProc("GetWindowLongW")
	procSetWindowLongPtrW     = user32.NewProc("SetWindowLongPtrW")
	procSetWindowLongW        = user32.NewProc("SetWindowLongW")
	procSetWindowPos          = user32.NewProc("SetWindowPos")
)

const (
	GWL_EXSTYLE      = -20
	WS_EX_APPWINDOW  = 0x00040000 // 任务栏强制显示图标标记
	WS_EX_TOOLWINDOW = 0x00000080 // 工具窗口标记（任务栏不显示）

	SWP_NOSIZE       = 0x0001
	SWP_NOMOVE       = 0x0002
	SWP_NOZORDER     = 0x0004
	SWP_FRAMECHANGED = 0x0020
)

// HideFromTaskbar 生产级任务栏隐藏实现（兼容 Splash 窗口）
func HideFromTaskbar(title string) error {
	currentPID := uint32(os.Getpid())
	var targetHWND uintptr

	// 轮询等待，解决 Fyne/GLFW 异步创建窗口句柄的时间差问题（最多尝试 20 次，共 500ms）
	for i := 0; i < 20; i++ {
		targetHWND = findAppWindowByPID(title, currentPID)
		if targetHWND != 0 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	if targetHWND == 0 {
		return fmt.Errorf("未能找到属于当前进程(PID: %d)的有效 UI 窗口", currentPID)
	}

	// 读取原扩展样式 (自适应 32/64 位及负数转换)
	style := getWindowLong(targetHWND, GWL_EXSTYLE)

	// 剥离 APPWINDOW 属性，附加 TOOLWINDOW 属性
	style = (style &^ WS_EX_APPWINDOW) | WS_EX_TOOLWINDOW

	// 写回新样式
	setWindowLong(targetHWND, GWL_EXSTYLE, style)

	// 强制刷新窗口框架，使 DWM (桌面窗口管理器) 立即应用更改
	procSetWindowPos.Call(targetHWND, 0, 0, 0, 0, 0,
		uintptr(SWP_NOMOVE|SWP_NOSIZE|SWP_NOZORDER|SWP_FRAMECHANGED))

	return nil
}

// findAppWindowByPID 双重匹配：优先匹配标题，匹配不到时自动提取当前进程的有效主窗口
func findAppWindowByPID(targetTitle string, targetPID uint32) uintptr {
	var exactMatch uintptr
	var fallbackMatch uintptr

	cb := syscall.NewCallback(func(hwnd uintptr, lparam uintptr) uintptr {
		var winPID uint32
		procGetWindowThreadProcId.Call(hwnd, uintptr(unsafe.Pointer(&winPID)))

		// 1. 严格限制为当前进程，杜绝误伤系统其他同名窗口
		if winPID != targetPID {
			return 1 // 继续枚举下一个
		}

		length, _, _ := procGetWindowTextLengthW.Call(hwnd)
		var winTitle string
		if length > 0 {
			buf := make([]uint16, length+1)
			procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(length+1))
			winTitle = syscall.UTF16ToString(buf)
		}

		// 2. 过滤掉 GLFW 底层共享上下文等内部辅助隐藏窗口及系统输入法窗口
		if winTitle == "GLFW3 Shared Context" || winTitle == "Default IME" || winTitle == "MSCTFIME UI" {
			return 1
		}

		// 3. 优先级一：如果标题精确匹配，直接选定并停止枚举
		if targetTitle != "" && winTitle == targetTitle {
			exactMatch = hwnd
			return 0
		}

		// 4. 优先级二：记录当前进程下第一个合法的 UI 窗口作为兜底
		//（由于 Splash 窗口的底层标题通常为 "" 或 "Fyne Application"，这里能完美捕获到它）
		if fallbackMatch == 0 {
			fallbackMatch = hwnd
		}

		return 1
	})

	procEnumWindows.Call(cb, 0)

	if exactMatch != 0 {
		return exactMatch
	}
	return fallbackMatch
}

// getWindowLong 兼容 32/64 位，同时处理 -20 负数转换
func getWindowLong(hwnd uintptr, index int) uintptr {
	idx := uintptr(int32(index))
	if procGetWindowLongPtrW.Find() == nil {
		ret, _, _ := procGetWindowLongPtrW.Call(hwnd, idx)
		return ret
	}
	ret, _, _ := procGetWindowLongW.Call(hwnd, idx)
	return ret
}

// setWindowLong 兼容 32/64 位写入
func setWindowLong(hwnd uintptr, index int, value uintptr) {
	idx := uintptr(int32(index))
	if procSetWindowLongPtrW.Find() == nil {
		procSetWindowLongPtrW.Call(hwnd, idx, value)
		return
	}
	procSetWindowLongW.Call(hwnd, idx, value)
}
