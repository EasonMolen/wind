package window

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"

	"fyne.io/fyne/v2"
	"github.com/jasonlovesdoggo/displayindex"
)

type RECT struct {
	Left, Top, Right, Bottom int32
}

const (
	SWP_NOZORDER      = 0x0004
	MDT_EFFECTIVE_DPI = 0

	GWL_STYLE   = ^uintptr(15) // -16
	GWL_EXSTYLE = ^uintptr(19) // -20
)

type MONITORINFO struct {
	CbSize    uint32
	RcMonitor RECT
	RcWork    RECT
	DwFlags   uint32
}

type WindowAnchor int

const (
	// 九宫格基础对齐（客户区角/边与屏幕角/边对齐）

	AnchorTopLeft WindowAnchor = iota
	AnchorTopCenter
	AnchorTopRight
	AnchorMiddleLeft
	AnchorCenter
	AnchorMiddleRight
	AnchorBottomLeft
	AnchorBottomCenter
	AnchorBottomRight

	// 自定义分数位置（客户区特定边框在屏幕特定分数处，另一方向居中）

	AnchorTopThirdCenter      // 客户区上边框在屏幕垂直 1/3 处，水平居中
	AnchorTopQuarterCenter    // 客户区上边框在屏幕垂直 1/4 处，水平居中
	AnchorBottomThirdCenter   // 客户区下边框在屏幕垂直 2/3 处，水平居中
	AnchorBottomQuarterCenter // 客户区下边框在屏幕垂直 3/4 处，水平居中
	AnchorLeftThirdCenter     // 客户区左边框在屏幕水平 1/3 处，垂直居中
	AnchorRightThirdCenter    // 客户区右边框在屏幕水平 2/3 处，垂直居中
)

// MoveWindowToAnchor 将 Fyne 窗口移动到目标屏幕的指定锚点位置
// 参数说明：
//
//	win: Fyne 窗口实例（必须已 Show）
//	screenIndex: 目标显示器索引，-1 表示自动获取鼠标所在屏幕
//	anchor: 位置锚点（如 AnchorTopThirdCenter）
//	customW, customH: 可选，手动指定逻辑尺寸。若传入 <=0，则自动从 win.Canvas().Size() 获取
func MoveWindowToAnchor(win fyne.Window, screenIndex int, anchor WindowAnchor, customW ...float32) error {
	// 1. 获取逻辑尺寸（优先使用自定义，否则从 Canvas 读取）
	var logicalW, logicalH float32
	if len(customW) >= 2 && customW[0] > 0 && customW[1] > 0 {
		logicalW, logicalH = customW[0], customW[1]
	} else {
		size := win.Canvas().Size()
		logicalW, logicalH = size.Width, size.Height
		if logicalW <= 0 || logicalH <= 0 {
			// 极端兜底：若 Canvas 还没渲染好，使用默认值
			logicalW, logicalH = 760, 520
		}
	}

	// 2. 【核心】内部自动获取原生窗口句柄
	hwnd := getNativeWindowHandle(win)
	if hwnd == 0 {
		// 兜底：通过标题查找（确保窗口已创建且标题唯一）
		titlePtr, _ := syscall.UTF16PtrFromString(win.Title())
		hwnd, _, _ = procFindWindow.Call(0, uintptr(unsafe.Pointer(titlePtr)))
	}
	if hwnd == 0 {
		return errors.New("failed to get native window handle (HWND)")
	}

	// 3. 获取目标显示器信息（复用之前的逻辑）
	_, monitorRect, dpi, err := getTargetMonitorInfo(screenIndex)
	if err != nil {
		return err
	}
	if dpi == 0 {
		dpi = 96
	}

	// 4. 计算客户区物理尺寸
	clientPhysW := int32(float32(logicalW) * float32(dpi) / 96.0)
	clientPhysH := int32(float32(logicalH) * float32(dpi) / 96.0)

	// 5. 计算完整窗口物理尺寸和偏移量
	fullW, fullH, offsetX, offsetY, err := calcWindowPhysicalMetrics(hwnd, clientPhysW, clientPhysH, dpi)
	if err != nil {
		return err
	}

	// 6. 计算客户区目标左上角 (物理坐标)
	targetClientX, targetClientY := calcClientAnchorPosition(monitorRect, clientPhysW, clientPhysH, anchor)

	// 7. 反推窗口左上角并调用 SetWindowPos
	targetX := targetClientX - offsetX
	targetY := targetClientY - offsetY

	ret, _, _ := procSetWindowPos.Call(
		hwnd,
		0,
		uintptr(targetX),
		uintptr(targetY),
		uintptr(fullW),
		uintptr(fullH),
		SWP_NOZORDER,
	)
	if ret == 0 {
		return errors.New("SetWindowPos failed")
	}
	return nil
}

// 获取显示器信息（支持 -1 自动获取鼠标所在屏）
func getTargetMonitorInfo(screenIndex int) (uintptr, RECT, uint32, error) {
	monitors := getAllMonitorHandles()
	if len(monitors) == 0 {
		return 0, RECT{}, 0, errors.New("no monitor found")
	}

	idx := screenIndex
	if idx < 0 {
		// 自动获取鼠标所在屏幕索引
		var err error
		idx, err = displayindex.CurrentDisplayIndex() // 沿用你原有的库
		if err != nil {
			idx = 0 // 默认主屏
		}
	}
	if idx >= len(monitors) {
		return 0, RECT{}, 0, fmt.Errorf("screen index %d out of range", idx)
	}

	hMonitor := monitors[idx]
	rect, err := getMonitorRect(hMonitor)
	if err != nil {
		return 0, RECT{}, 0, err
	}
	dpi := getDpiForMonitor(hMonitor)
	return hMonitor, rect, dpi, nil
}

// 计算完整窗口物理尺寸和客户区偏移量
func calcWindowPhysicalMetrics(hwnd uintptr, clientW, clientH int32, dpi uint32) (fullW, fullH, offsetX, offsetY int32, err error) {
	style := getWindowLongPtr(hwnd, GWL_STYLE)
	exStyle := getWindowLongPtr(hwnd, GWL_EXSTYLE)

	rect := RECT{0, 0, clientW, clientH}
	ret, _, _ := procAdjustWindowRectExForDpi.Call(
		uintptr(unsafe.Pointer(&rect)),
		style,
		0,
		exStyle,
		uintptr(dpi),
	)
	if ret == 0 {
		return 0, 0, 0, 0, errors.New("AdjustWindowRectExForDpi failed")
	}

	fullW = rect.Right - rect.Left
	fullH = rect.Bottom - rect.Top
	offsetX = (fullW - clientW) / 2
	offsetY = (fullH - clientH) / 2
	return fullW, fullH, offsetX, offsetY, nil
}

// 根据锚点计算客户区左上角坐标 (物理像素)
func calcClientAnchorPosition(monitorRect RECT, clientW, clientH int32, anchor WindowAnchor) (int32, int32) {
	left, top := monitorRect.Left, monitorRect.Top
	right, bottom := monitorRect.Right, monitorRect.Bottom
	monW := right - left
	monH := bottom - top

	var targetX, targetY int32

	switch anchor {
	// ---------- 九宫格 ----------
	case AnchorTopLeft:
		targetX, targetY = left, top
	case AnchorTopCenter:
		targetX, targetY = left+monW/2-clientW/2, top
	case AnchorTopRight:
		targetX, targetY = right-clientW, top
	case AnchorMiddleLeft:
		targetX, targetY = left, top+monH/2-clientH/2
	case AnchorCenter:
		targetX, targetY = left+monW/2-clientW/2, top+monH/2-clientH/2
	case AnchorMiddleRight:
		targetX, targetY = right-clientW, top+monH/2-clientH/2
	case AnchorBottomLeft:
		targetX, targetY = left, bottom-clientH
	case AnchorBottomCenter:
		targetX, targetY = left+monW/2-clientW/2, bottom-clientH
	case AnchorBottomRight:
		targetX, targetY = right-clientW, bottom-clientH

	// ---------- 自定义分数位置（水平居中/垂直居中） ----------
	case AnchorTopThirdCenter:
		targetX = left + monW/2 - clientW/2
		targetY = top + int32(float32(monH)/3.0)
	case AnchorTopQuarterCenter:
		targetX = left + monW/2 - clientW/2
		targetY = top + int32(float32(monH)/4.0)
	case AnchorBottomThirdCenter:
		targetX = left + monW/2 - clientW/2
		targetY = top + int32(float32(monH)*2.0/3.0) - clientH // 客户区下边框对齐
	case AnchorBottomQuarterCenter:
		targetX = left + monW/2 - clientW/2
		targetY = top + int32(float32(monH)*3.0/4.0) - clientH
	case AnchorLeftThirdCenter:
		targetX = left + int32(float32(monW)/3.0)
		targetY = top + monH/2 - clientH/2
	case AnchorRightThirdCenter:
		targetX = left + int32(float32(monW)*2.0/3.0) - clientW
		targetY = top + monH/2 - clientH/2

	default:
		// 保底居中
		targetX, targetY = left+monW/2-clientW/2, top+monH/2-clientH/2
	}
	return targetX, targetY
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
