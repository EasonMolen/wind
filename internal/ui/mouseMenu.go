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
	content  *fyne.Container
	itemPath string                        // 当前项的路径，用于复制
	window   fyne.Window                   // 当前项的路径，用于复制
	openWith OpenWithFunc                  // 用于实现“资源管理器打开”
	OnTapped func()                        // 左键回调函数
	OnRename func(oldPath, newPath string) // 重命名后的回调函数
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

// TappedSecondary 捕获右键点击事件
func (m *mouseMenuItemWidget) TappedSecondary(pe *fyne.PointEvent) {
	// 创建右键菜单
	menu := fyne.NewMenu("",
		fyne.NewMenuItem("使用资源管理器打开", func() {
			m.openWith("explorer", "/select,", m.itemPath)
		}),
		fyne.NewMenuItem("复制文件", func() {
			go func(itemPath string) {
				if err := m.copyFileToClipboard(itemPath); err != nil {
					fmt.Printf("复制文件失败：%v", err.Error())
					return
				}
			}(m.itemPath)
		}),
		fyne.NewMenuItem("复制文件名", func() {
			fileName := filepath.Base(m.itemPath)
			m.window.Clipboard().SetContent(fileName)
		}),
		fyne.NewMenuItem("复制文件路径", func() {
			// 点击复制时，将路径写入剪贴板
			m.window.Clipboard().SetContent(m.itemPath)
		}),
		fyne.NewMenuItem("重命名", func() {
			m.rename()
		}),
	)

	// 在鼠标位置弹出菜单
	widget.ShowPopUpMenuAtPosition(menu, m.window.Canvas(), pe.AbsolutePosition)
}

func (m *mouseMenuItemWidget) Tapped(pe *fyne.PointEvent) {
	if m.OnTapped != nil {
		m.OnTapped() //触发回调
	}
}

func (m *mouseMenuItemWidget) copyFileToClipboard(path string) error {
	// 使用 powershell 的 Set-Clipboard 命令
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

func (m *mouseMenuItemWidget) rename() {
	oldPath := m.itemPath

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
			// 提示
			dialog.ShowError(fmt.Errorf("新名称不能为空"), m.window)
			return
		}

		newPath := filepath.Join(dir, newName)

		if err := os.Rename(oldPath, newPath); err != nil {
			// 提示
			dialog.ShowError(fmt.Errorf("重命名失败:\n%v", err), m.window)
			return
		}

		m.itemPath = newPath

		if m.OnRename != nil {
			m.OnRename(oldPath, newPath)
		}

		dialog.ShowInformation("成功", "文件已重命名", m.window)

	}, m.window)

	d.Resize(fyne.NewSize(400, 150))

	d.Show()

	m.window.Canvas().Focus(entry)
}
