package window

import "syscall"

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
