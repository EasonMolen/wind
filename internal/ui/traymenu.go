package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
)

type TrayMenu struct {
	desk       desktop.App
	mainWindow MainWindow
	quit       func()
}

func NewTrayMenu(app fyne.App, win MainWindow, quit func()) *TrayMenu {
	t := &TrayMenu{
		mainWindow: win,
		quit:       quit,
	}

	// 安全断言 desktop.App
	if desk, ok := app.(desktop.App); ok {
		t.desk = desk
	}

	return t
}

func (t *TrayMenu) StartTrayMenu() error {
	// 如果不是桌面环境（不支持托盘），优雅退出
	if t.desk == nil {
		return nil
	}

	fyne.Do(func() {
		// 构建右键菜单，关联 MainWindow 的行为
		showItem := fyne.NewMenuItem("显示窗口", func() {
			t.mainWindow.Show()
		})

		hideItem := fyne.NewMenuItem("隐藏窗口", func() {
			t.mainWindow.Hide()
		})

		// 补充：增加彻底退出程序的菜单项
		quitItem := fyne.NewMenuItem("优雅退出", func() {
			t.quit()
		})

		// 组合菜单
		trayMenu := fyne.NewMenu("Wind Launcher",
			showItem,
			hideItem,
			fyne.NewMenuItemSeparator(), // 加一条分隔线，区分显示控制和程序退出
			quitItem,
		)
		t.desk.SetSystemTrayMenu(trayMenu)

		// 设置托盘图标
		t.desk.SetSystemTrayIcon(theme.SearchIcon())

		// 4. (Fyne 2.7+) 关联窗口：实现单击托盘图标自动 Toggle (显示/隐藏) 窗口
		if t.mainWindow != nil && t.mainWindow.Window() != nil {
			t.desk.SetSystemTrayWindow(t.mainWindow.Window())
		}
	})

	return nil
}
