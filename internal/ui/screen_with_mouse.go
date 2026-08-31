package ui

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
	"wind/internal/window"

	"fyne.io/fyne/v2"
	"github.com/jasonlovesdoggo/displayindex"
)

var (
	user32                       = syscall.NewLazyDLL("user32.dll")
	shcore                       = syscall.NewLazyDLL("shcore.dll")
	procFindWindow               = user32.NewProc("FindWindowW")
	procSetWindowPos             = user32.NewProc("SetWindowPos")
	procGetWindowLongPtr         = user32.NewProc("GetWindowLongPtrW")
	procAdjustWindowRectExForDpi = user32.NewProc("AdjustWindowRectExForDpi")
	procEnumDisplayMonitors      = user32.NewProc("EnumDisplayMonitors")
	procGetMonitorInfoW          = user32.NewProc("GetMonitorInfoW")
	procGetDpiForMonitor         = shcore.NewProc("GetDpiForMonitor")
)

const (
	SWP_NOZORDER      = 0x0004
	MDT_EFFECTIVE_DPI = 0

	GWL_STYLE   = ^uintptr(15) // -16
	GWL_EXSTYLE = ^uintptr(19) // -20
)

type RECT struct {
	Left, Top, Right, Bottom int32
}

type MONITORINFO struct {
	CbSize    uint32
	RcMonitor RECT
	RcWork    RECT
	DwFlags   uint32
}

// Show 显示窗口并确保内容中心对齐到鼠标所在屏幕的物理中心
func (w *mainWindow) Show() {
	w.visible.Store(true)
	w.window.Show()

	if runtime.GOOS == "windows" {
		//w.alignToActiveScreenCenter() // 直接调用，不再嵌套 fyne.Do
		currentDisplayIndex, err := displayindex.CurrentDisplayIndex()
		if err != nil {
			return
		}
		err = window.MoveWindowToAnchor(w.window, currentDisplayIndex, window.AnchorBottomThirdCenter)
		if err != nil {
			return
		}
	} else {
		w.window.CenterOnScreen()
	}

	w.window.RequestFocus()
	w.FocusSearch()
}

// alignToActiveScreenCenter 立即获取句柄并移动窗口
func (w *mainWindow) alignToActiveScreenCenter() {
	// 1. 立即尝试获取窗口句柄（窗口刚 Show，句柄通常已就绪）
	hwnd := getNativeWindowHandle(w.window)
	if hwnd == 0 {
		// 后备：通过标题查找（标题唯一，窗口已创建）
		titlePtr, _ := syscall.UTF16PtrFromString(w.window.Title())
		hwnd, _, _ = procFindWindow.Call(0, uintptr(unsafe.Pointer(titlePtr)))
		if hwnd == 0 {
			// 实在获取不到，回退到 Fyne 默认居中
			w.window.CenterOnScreen()
			return
		}
	}

	// 2. 使用 displayindex 获取鼠标所在屏幕索引
	screenIndex, err := displayindex.CurrentDisplayIndex()
	if err != nil {
		w.window.CenterOnScreen()
		return
	}

	// 3. 枚举所有显示器，获取 HMONITOR 列表
	monitors := getAllMonitorHandles()
	if screenIndex < 0 || screenIndex >= len(monitors) {
		w.window.CenterOnScreen()
		return
	}
	hMonitor := monitors[screenIndex]

	// 4. 获取该显示器的物理矩形和 DPI
	monitorRect, err := getMonitorRect(hMonitor)
	if err != nil {
		w.window.CenterOnScreen()
		return
	}
	dpi := getDpiForMonitor(hMonitor)
	if dpi == 0 {
		dpi = 96
	}

	// 5. 获取窗口当前的逻辑尺寸（Fyne Canvas 大小）
	logicalWidth, logicalHeight := w.getLogicalSize()
	if logicalWidth <= 0 || logicalHeight <= 0 {
		// 后备：使用窗口当前物理尺寸反推逻辑尺寸
		var rect RECT
		procGetWindowRect := user32.NewProc("GetWindowRect")
		procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rect)))
		logicalWidth = float32(rect.Right-rect.Left) * 96.0 / float32(dpi)
		logicalHeight = float32(rect.Bottom-rect.Top) * 96.0 / float32(dpi)
		if logicalWidth <= 0 {
			logicalWidth = 760
		}
		if logicalHeight <= 0 {
			logicalHeight = 520
		}
	}

	// 6. 计算客户区物理尺寸
	clientPhysicalWidth := int32(float64(logicalWidth) * float64(dpi) / 96.0)
	clientPhysicalHeight := int32(float64(logicalHeight) * float64(dpi) / 96.0)

	// 7. 计算整个窗口（含标题栏/边框）的物理尺寸
	style := getWindowLongPtr(hwnd, GWL_STYLE)
	exStyle := getWindowLongPtr(hwnd, GWL_EXSTYLE)
	rect := RECT{0, 0, clientPhysicalWidth, clientPhysicalHeight}
	procAdjustWindowRectExForDpi.Call(
		uintptr(unsafe.Pointer(&rect)),
		style,
		0,
		exStyle,
		uintptr(dpi),
	)
	windowPhysicalWidth := rect.Right - rect.Left
	windowPhysicalHeight := rect.Bottom - rect.Top

	// 8. 计算目标左上角，使客户区中心与屏幕中心对齐
	clientOffsetX := (windowPhysicalWidth - clientPhysicalWidth) / 2
	clientOffsetY := (windowPhysicalHeight - clientPhysicalHeight) / 2

	screenCenterX := monitorRect.Left + (monitorRect.Right-monitorRect.Left)/2
	screenCenterY := monitorRect.Top + (monitorRect.Bottom-monitorRect.Top)/2

	targetX := screenCenterX - clientOffsetX - clientPhysicalWidth/2
	targetY := screenCenterY - clientOffsetY - clientPhysicalHeight/2

	// 9. 一步设置位置和大小（不隐藏窗口）
	procSetWindowPos.Call(
		hwnd,
		0,
		uintptr(targetX),
		uintptr(targetY),
		uintptr(windowPhysicalWidth),
		uintptr(windowPhysicalHeight),
		SWP_NOZORDER,
	)
}

// 辅助函数：获取窗口逻辑尺寸
func (w *mainWindow) getLogicalSize() (float32, float32) {
	size := w.window.Canvas().Size()
	return size.Width, size.Height
}

// 获取原生窗口句柄（Fyne 窗口可能实现 NativeWindow 接口）
func getNativeWindowHandle(win fyne.Window) uintptr {
	type nativeWindow interface {
		NativeWindow() uintptr
	}
	if nw, ok := win.(nativeWindow); ok {
		return nw.NativeWindow()
	}
	return 0
}

// 枚举所有显示器的 HMONITOR，返回顺序通常与 displayindex 一致
func getAllMonitorHandles() []uintptr {
	var monitors []uintptr
	callback := syscall.NewCallback(func(hMonitor, hdc, lprcMonitor, lParam uintptr) uintptr {
		monitors = append(monitors, hMonitor)
		return 1 // 继续枚举
	})
	procEnumDisplayMonitors.Call(0, 0, callback, 0)
	return monitors
}

// 通过 HMONITOR 获取显示器物理矩形
func getMonitorRect(hMonitor uintptr) (RECT, error) {
	var info MONITORINFO
	info.CbSize = uint32(unsafe.Sizeof(info))
	ret, _, _ := procGetMonitorInfoW.Call(hMonitor, uintptr(unsafe.Pointer(&info)))
	if ret == 0 {
		return RECT{}, fmt.Errorf("GetMonitorInfoW failed")
	}
	return info.RcMonitor, nil
}

// 通过 HMONITOR 获取显示器 DPI
func getDpiForMonitor(hMonitor uintptr) uint32 {
	var dpiX, dpiY uint32
	ret, _, _ := procGetDpiForMonitor.Call(
		hMonitor,
		MDT_EFFECTIVE_DPI,
		uintptr(unsafe.Pointer(&dpiX)),
		uintptr(unsafe.Pointer(&dpiY)),
	)
	if ret != 0 {
		return 0
	}
	return dpiX
}

// 获取窗口样式
func getWindowLongPtr(hwnd uintptr, index uintptr) uintptr {
	val, _, _ := procGetWindowLongPtr.Call(hwnd, index)
	return val
}
