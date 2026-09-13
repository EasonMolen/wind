package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

type mouseMenuItemWidget struct {
	widget.BaseWidget
	content *fyne.Container
	item    *ResultItem // 完整的item内容
	//itemPath  string                        // 当前项的路径，用于复制
	window    fyne.Window                   // 当前项的路径，用于复制
	openWith  OpenWithFunc                  // 用于实现“资源管理器打开”
	OnTapped  func()                        // 左键回调函数
	OnRename  func(oldPath, newPath string) // 重命名后的回调函数
	TogglePin TogglePinFunc                 // 切换固定图标
}

func newMouseMenuItemWidget(content *fyne.Container, win fyne.Window, openFunc OpenWithFunc) *mouseMenuItemWidget {
	r := &mouseMenuItemWidget{
		content:  content,
		window:   win,
		openWith: openFunc,
	}
	r.ExtendBaseWidget(r)
	return r
}

// CreateRenderer 渲染内部包裹的内容
func (m *mouseMenuItemWidget) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(m.content)
}

func (m *mouseMenuItemWidget) TappedSecondary(pe *fyne.PointEvent) {
	ShowItemContextMenu(
		m.window,
		m.item.FullPath,
		m.openWith,
		func(oldPath, newPath string) {
			m.item.FullPath = newPath
			if m.OnRename != nil {
				m.OnRename(oldPath, newPath)
			}
		},
		m.TogglePin,
		pe.AbsolutePosition,
		m.item,
	)
}

func (m *mouseMenuItemWidget) Tapped(pe *fyne.PointEvent) {
	if m.OnTapped != nil {
		m.OnTapped() //触发回调
	}
}

// ShowItemContextMenu 在指定位置弹出针对某个文件路径的右键菜单
func ShowItemContextMenu(
	window fyne.Window,
	itemPath string,
	openWith OpenWithFunc,
	onRename func(oldPath, newPath string),
	TogglePin TogglePinFunc,
	pos fyne.Position,
	item *ResultItem,
) {
	menu := fyne.NewMenu("",
		fyne.NewMenuItem("使用资源管理器打开", func() {
			openWith("explorer", "/select,", itemPath)
		}),
		fyne.NewMenuItem("复制文件", func() {
			go func(p string) {
				if err := CopyFileToClipboard(p); err != nil {
					fmt.Printf("复制文件失败：%v", err.Error())
				}
			}(itemPath)
		}),
		fyne.NewMenuItem("复制文件名", func() {
			window.Clipboard().SetContent(filepath.Base(itemPath))
		}),
		fyne.NewMenuItem("复制文件路径", func() {
			window.Clipboard().SetContent(itemPath)
		}),
		fyne.NewMenuItem("重命名", func() {
			ShowRenameDialog(window, itemPath, onRename)
		}),
		fyne.NewMenuItem("取消固定", func() {
			if item != nil {
				TogglePin(*item)
			}
		}),
	)

	widget.ShowPopUpMenuAtPosition(menu, window.Canvas(), pos)
}

// CopyFileToClipboard 使用 powershell 把文件复制到剪贴板
func CopyFileToClipboard(path string) error {
	// -NoProfile: 加快启动速度
	// -WindowStyle Hidden: 隐藏黑框闪烁
	// -Command: 执行的具体脚本
	// -LiteralPath: 将其作为绝对字面路径处理，防止带有 [] 等符号的路径报错
	cmd := exec.Command(
		"powershell",
		"-NoProfile",
		"-WindowStyle", "Hidden",
		"-Command", "Set-Clipboard -LiteralPath $env:CLIP_TARGET_FILE",
	)

	// 利用环境变量安全传递路径，彻底杜绝引号转义、空格截断的问题
	cmd.Env = append(os.Environ(), "CLIP_TARGET_FILE="+path)

	// 使Windows底层不创建控制台窗口
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true,
	}

	// 执行命令并等待完成
	return cmd.Run()
}

// ShowRenameDialog 弹出重命名对话框
func ShowRenameDialog(window fyne.Window, oldPath string, onRename func(oldPath, newPath string)) {
	dir, oldName := filepath.Split(oldPath)

	entry := widget.NewEntry()
	entry.SetText(oldName)

	items := []*widget.FormItem{
		widget.NewFormItem("新名称:", entry),
	}

	d := dialog.NewForm("重命名", "确认", "取消", items, func(confirm bool) {
		if !confirm {
			return
		}

		newName := entry.Text

		if newName == "" || oldName == newName {
			dialog.ShowError(fmt.Errorf("新名称不能为空"), window)
			return
		}

		newPath := filepath.Join(dir, newName)

		if err := os.Rename(oldPath, newPath); err != nil {
			dialog.ShowError(fmt.Errorf("重命名失败:\n%v", err), window)
			return
		}

		if onRename != nil {
			onRename(oldPath, newPath)
		}

		dialog.ShowInformation("成功", "文件已重命名", window)

	}, window)

	d.Resize(fyne.NewSize(400, 150))
	d.Show()

	window.Canvas().Focus(entry)
}
