package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"time"
	"wind/internal/config"
	"wind/internal/hotkey"
	"wind/internal/icon"
	"wind/internal/launcher"
	"wind/internal/search"
	"wind/internal/ui"

	"fyne.io/fyne/v2"
)

type App struct {
	fyneApp fyne.App

	mainWindow ui.MainWindow
	trayMenu   ui.TrayMenu
	everything search.EverythingService
	hotkey     hotkey.KeyService
	opener     launcher.OpenService
	config     config.CfgService

	ctx             context.Context
	cancel          context.CancelFunc
	hotkeyHandlerID string
	cfg             config.Config
}

func NewApp(fa fyne.App) (*App, error) {
	if fa == nil {
		return nil, fmt.Errorf("fyne app is nil")
	}

	cfgService := config.NewService("")
	if err := cfgService.LoadConfig(); err != nil {
		return nil, fmt.Errorf("加载配置失败, 错误信息: %w", err)
	}
	cfg := cfgService.Get()

	ctx, cancel := context.WithCancel(context.Background())
	a := &App{
		fyneApp:    fa,
		everything: search.NewEverythingService(),
		hotkey:     hotkey.NewKeyServer(),
		opener:     launcher.NewOpener(launcher.WithMaxConcurrency(cfg.Launcher.MaxConcurrency)),
		config:     cfgService,
		ctx:        ctx,
		cancel:     cancel,
		cfg:        cfg,
	}

	// 图标服务目前由全局引擎对外提供，这里集中按配置初始化，后续 UI 要显示图标时不用再关心配置来源。
	icon.InitGlobalEngine(icon.Config{
		CacheCapacity:    cfg.Icon.CacheCapacity,
		DefaultTimeout:   time.Duration(cfg.Icon.DefaultTimeoutMS) * time.Millisecond,
		EnableExtRouting: cfg.Icon.EnableExtRouting,
	})

	a.mainWindow = ui.NewMainWindow(fa, ui.WindowOptions{
		Title:      cfg.Window.Title,
		Width:      cfg.Window.Width,
		Height:     cfg.Window.Height,
		HideOnOpen: cfg.Window.HideOnOpen,
	})
	a.mainWindow.SetCallbacks(ui.Callbacks{
		Search:            a.search,
		Open:              a.open,
		TogglePin:         a.togglePin,
		Quit:              a.Quit,
		GetPinDisplayName: a.config.GetPinDisplayName,
	})

	tray := ui.NewTrayMenu(fa, a.mainWindow, a.Quit)
	if err := tray.StartTrayMenu(); err != nil {
		return nil, err
	}

	return a, nil
}

func (a *App) Run() error {
	if err := a.registerToggleHotkey(); err != nil {
		return err
	}

	go func() {
		if err := a.hotkey.Listen(a.ctx); err != nil {
			log.Printf("热键监听停止, 错误信息: %v", err)
		}
	}()

	if a.cfg.Window.ShowOnStart {
		a.mainWindow.Show()
	}

	a.fyneApp.Run()
	a.Shutdown()
	return nil
}

func (a *App) Quit() {
	a.Shutdown()
	a.fyneApp.Quit()
}

func (a *App) Shutdown() {
	if a.cancel != nil {
		a.cancel()
	}
	if a.hotkeyHandlerID != "" {
		if err := a.hotkey.Unregister(a.hotkeyHandlerID); err != nil {
			if !errors.Is(err, hotkey.ErrHotkeyHandlerNotFound) {
				log.Printf("注销热键失败, 错误信息: %v", err)
			}
		}
		a.hotkeyHandlerID = ""
	}
	if a.opener != nil {
		if err := a.opener.Close(); err != nil && !errors.Is(err, launcher.ErrServiceClosed) {
			log.Printf("close launcher: %v", err)
		}
	}
	if a.config != nil {
		if err := a.config.SaveConfig(); err != nil {
			log.Printf("保存配置失败, 错误信息: %v", err)
		}
	}
}

func (a *App) search(keyword string) []ui.ResultItem {
	if a.everything == nil {
		return nil
	}

	results, err := a.everything.Search(keyword, a.cfg.Search.MaxResults)
	if err != nil {
		log.Printf("search failed: %v", err)
		return nil
	}

	items := make([]ui.ResultItem, len(results))
	for i, result := range results {
		items[i] = ui.ResultItem{
			FullPath: result.FullPath,
			FileName: result.FileName,
			Path:     result.Path,
			Size:     result.Size,
			IsFolder: result.IsFolder,
		}
	}

	return items
}

func (a *App) open(item ui.ResultItem) {
	if a.opener == nil || item.FullPath == "" {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 8*time.Second)
		defer cancel()

		if err := a.opener.Open(ctx, item.FullPath); err != nil {
			log.Printf("打开失败, 错误信息: %v", err)
		}
	}()
}

func (a *App) togglePin(item ui.ResultItem) bool {
	if a.config == nil || item.FullPath == "" {
		return false
	}

	pinned, err := a.config.TogglePin(item.FullPath, pinDisplayName(item))
	if err != nil {
		log.Printf("toggle pin failed: %v", err)
		return false
	}

	a.cfg = a.config.Get()
	if err := a.config.SaveConfig(); err != nil {
		log.Printf("save config failed after toggle pin: %v", err)
	}

	return pinned
}

func (a *App) registerToggleHotkey() error {
	if a.hotkey == nil {
		return nil
	}

	hk, err := hotkey.ParseHotkey(a.cfg.Hotkey.Toggle)
	if err != nil {
		return fmt.Errorf("解析热键 %q: %w", a.cfg.Hotkey.Toggle, err)
	}

	handlerID, err := a.hotkey.Register(a.ctx, hk, func() {
		// 全局热键回调不在 UI 线程，必须交给 Fyne 调度后再操作窗口。
		fyne.Do(func() {
			a.mainWindow.Toggle()
		})
	})
	if err != nil {
		return fmt.Errorf("注册热键 %q: %w", a.cfg.Hotkey.Toggle, err)
	}

	a.hotkeyHandlerID = handlerID
	return nil
}

func pinDisplayName(item ui.ResultItem) string {
	if item.FileName != "" {
		return item.FileName
	}
	if item.FullPath == "" {
		return ""
	}

	name := filepath.Base(item.FullPath)
	if name != "." && name != string(filepath.Separator) {
		return name
	}
	return item.FullPath
}
