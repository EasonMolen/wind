package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"time"
	"wind/internal/buildinfo"
	"wind/internal/config"
	"wind/internal/everythingruntime"
	"wind/internal/hotkey"
	"wind/internal/icon"
	"wind/internal/launcher"
	"wind/internal/search"
	"wind/internal/ui"
	"wind/internal/update"

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
	runtime    *everythingruntime.Manager
	updater    update.Checker

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
		everything: search.NewEverythingService(cfg.Search.MaxResults),
		hotkey:     hotkey.NewKeyServer(),
		opener:     launcher.NewOpener(launcher.WithMaxConcurrency(cfg.Launcher.MaxConcurrency)),
		config:     cfgService,
		runtime:    everythingruntime.New(everythingruntime.DefaultExecutablePath()),
		updater: update.Checker{
			CurrentVersion: buildinfo.Version,
			ManifestURL:    cfg.Update.ManifestURL,
		},
		ctx:    ctx,
		cancel: cancel,
		cfg:    cfg,
	}

	// 图标服务目前由全局引擎对外提供，这里集中按配置初始化，后续 UI 要显示图标时不用再关心配置来源。
	iconEngine := icon.NewEngine(icon.Config{
		CacheCapacity:    cfg.Icon.CacheCapacity,
		DefaultTimeout:   time.Duration(cfg.Icon.DefaultTimeoutMS) * time.Millisecond,
		EnableExtRouting: cfg.Icon.EnableExtRouting,
	})

	mainWindowCtx := context.WithoutCancel(a.ctx)
	a.mainWindow = ui.NewMainWindow(mainWindowCtx, fa, iconEngine, ui.WindowOptions{
		Title:         cfg.Window.Title,
		Width:         cfg.Window.Width,
		Height:        cfg.Window.Height,
		HideOnOpen:    cfg.Window.HideOnOpen,
		Categories:    buildSearchCategories(),
		ShowCharacter: cfg.Display.ShowCharacter,
	})
	a.mainWindow.SetCallbacks(ui.Callbacks{
		Search:            a.search,
		CategorySearch:    a.categorySearch,
		Open:              a.open,
		OpenWith:          a.openWith,
		TogglePin:         a.togglePin,
		Quit:              a.Quit,
		UpdateConfig:      a.UpdateConfig,
		GetPinDisplayName: a.config.GetPinDisplayName,
		GetPinnedItems:    a.pinnedItems,
		MovePinnedItem:    a.movePinnedItem,
		PinnedIconsNum:    a.config.PinnedNum,
		GetSettings:       a.settings,
		SaveSettings:      a.saveSettings,
		CheckUpdates:      a.checkUpdates,
	})

	tray := ui.NewTrayMenu(fa, a.mainWindow, a.Quit, a.Cleanup)
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
	a.Cleanup()
	return nil
}

func (a *App) Quit() {
	a.Cleanup()
	a.fyneApp.Quit()
}

func (a *App) Cleanup() {
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

func (a *App) search(keyword string) ([]ui.ResultItem, error) {
	if a.everything == nil {
		return nil, nil
	}

	results, err := a.queryEverything(func() ([]search.ResultSearch, error) {
		return a.everything.Search(keyword)
	})
	if err != nil {
		log.Printf("search failed: %v", err)
		return nil, err
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

	return items, nil
}

func (a *App) categorySearch(keyword, category string) ([]ui.ResultItem, error) {
	if a.everything == nil {
		return nil, nil
	}

	results, err := a.queryEverything(func() ([]search.ResultSearch, error) {
		return a.everything.CategorySearch(keyword, category)
	})
	if err != nil {
		log.Printf("categorySearch failed: %v", err)
		return nil, err
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

	return items, nil
}

// queryEverything retries once the portable runtime is requested. The SDK DLL
// only proxies IPC, so a successful process start still needs a short wait for
// Everything to create its IPC endpoint and load its index.
func (a *App) queryEverything(query func() ([]search.ResultSearch, error)) ([]search.ResultSearch, error) {
	results, err := query()
	if !errors.Is(err, search.ErrIPCUnavailable) || a.runtime == nil {
		return results, err
	}

	startCtx, cancel := context.WithTimeout(a.ctx, 3*time.Second)
	defer cancel()
	if startErr := a.runtime.Start(startCtx); startErr != nil {
		return nil, fmt.Errorf("Everything 未运行且内置运行时无法启动: %w", startErr)
	}

	for attempt := 0; attempt < 8; attempt++ {
		select {
		case <-a.ctx.Done():
			return nil, a.ctx.Err()
		case <-time.After(400 * time.Millisecond):
		}

		results, err = query()
		if !errors.Is(err, search.ErrIPCUnavailable) {
			return results, err
		}
	}
	return nil, fmt.Errorf("Everything 正在启动或建立索引，请稍后重试: %w", err)
}

func (a *App) open(itemFullPath string) {
	if a.opener == nil || itemFullPath == "" {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 8*time.Second)
		defer cancel()

		if err := a.opener.Open(ctx, itemFullPath); err != nil {
			log.Printf("打开失败, 错误信息: %v", err)
		}
	}()
}

func (a *App) openWith(useAppName string, args ...string) {
	if a.opener == nil || args == nil {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 8*time.Second)
		defer cancel()

		if err := a.opener.OpenWith(ctx, useAppName, args...); err != nil {
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
	if err = a.config.SaveConfig(); err != nil {
		log.Printf("save config failed after toggle pin: %v", err)
	}

	return pinned
}

func (a *App) pinnedItems() []ui.ResultItem {
	if len(a.cfg.Pins) == 0 {
		return nil
	}

	items := make([]ui.ResultItem, 0, len(a.cfg.Pins))
	for _, pin := range a.cfg.Pins {
		if pin.Path == "" {
			continue
		}

		items = append(items, ui.ResultItem{
			FullPath: pin.Path,
			FileName: pin.Name,
		})
	}

	return items
}

func (a *App) movePinnedItem(path string, targetIndex int) bool {
	if a.config == nil {
		return false
	}
	if err := a.config.MovePin(path, targetIndex); err != nil {
		log.Printf("调整固定项顺序失败: %v", err)
		return false
	}
	a.cfg = a.config.Get()
	if err := a.config.SaveConfig(); err != nil {
		log.Printf("保存固定项顺序失败: %v", err)
		return false
	}
	return true
}

func (a *App) UpdateConfig(oldPath, newPath string) error {
	if err := a.config.UpdatePinnedName(oldPath, newPath); err != nil {
		log.Printf("向配置文件中更新pin内容的路径失败, 旧路径为:%s, 新路径为:%s. : %v", oldPath, newPath, err)
		return err
	}

	a.cfg = a.config.Get()
	if err := a.config.SaveConfig(); err != nil {
		log.Printf("保存固定项路径失败: %v", err)
	}

	return nil
}

func (a *App) settings() ui.Settings {
	cfg := a.config.Get()
	return ui.Settings{
		ToggleHotkey:  cfg.Hotkey.Toggle,
		ShowOnStart:   cfg.Window.ShowOnStart,
		StartOnBoot:   cfg.Launcher.StartAutomaticallyOnBoot,
		HideOnOpen:    cfg.Window.HideOnOpen,
		ShowCharacter: cfg.Display.ShowCharacter,
		MaxResults:    cfg.Search.MaxResults,
		Version:       buildinfo.Version,
	}
}

func (a *App) checkUpdates(ctx context.Context) (ui.UpdateStatus, error) {
	result, err := a.updater.Check(ctx)
	if err != nil {
		return ui.UpdateStatus{}, err
	}
	return ui.UpdateStatus{
		CurrentVersion: result.CurrentVersion,
		LatestVersion:  result.LatestVersion,
		Available:      result.Available,
		Notes:          result.Notes,
	}, nil
}

func (a *App) saveSettings(settings ui.Settings) error {
	if _, err := hotkey.ParseHotkey(settings.ToggleHotkey); err != nil {
		return fmt.Errorf("唤起热键无效: %w", err)
	}
	if settings.MaxResults < 10 || settings.MaxResults > 200 {
		return fmt.Errorf("最大搜索结果必须是 10 到 200 之间的整数")
	}

	previous := a.config.Get()
	updated := previous
	updated.Hotkey.Toggle = settings.ToggleHotkey
	updated.Window.ShowOnStart = settings.ShowOnStart
	updated.Launcher.StartAutomaticallyOnBoot = settings.StartOnBoot
	updated.Window.HideOnOpen = settings.HideOnOpen
	updated.Display.ShowCharacter = settings.ShowCharacter
	updated.Search.MaxResults = settings.MaxResults

	if updated.Hotkey.Toggle != previous.Hotkey.Toggle {
		if err := a.replaceToggleHotkey(previous.Hotkey.Toggle, updated.Hotkey.Toggle); err != nil {
			return err
		}
	}
	if updated.Launcher.StartAutomaticallyOnBoot != previous.Launcher.StartAutomaticallyOnBoot {
		if err := a.setStartOnBoot(updated.Launcher.StartAutomaticallyOnBoot); err != nil {
			if updated.Hotkey.Toggle != previous.Hotkey.Toggle {
				_ = a.replaceToggleHotkey(updated.Hotkey.Toggle, previous.Hotkey.Toggle)
			}
			return err
		}
	}

	a.config.Set(updated)
	if err := a.config.SaveConfig(); err != nil {
		a.config.Set(previous)
		if updated.Launcher.StartAutomaticallyOnBoot != previous.Launcher.StartAutomaticallyOnBoot {
			_ = a.setStartOnBoot(previous.Launcher.StartAutomaticallyOnBoot)
		}
		if updated.Hotkey.Toggle != previous.Hotkey.Toggle {
			_ = a.replaceToggleHotkey(updated.Hotkey.Toggle, previous.Hotkey.Toggle)
		}
		return fmt.Errorf("保存设置: %w", err)
	}
	a.cfg = a.config.Get()
	if a.everything != nil {
		a.everything.SetMaxResults(settings.MaxResults)
	}
	a.mainWindow.ApplySettings(settings)
	return nil
}

func (a *App) setStartOnBoot(enabled bool) error {
	if a.config == nil {
		return errors.New("配置服务不可用")
	}
	if enabled {
		if err := a.config.SetStartOnBoot(); err != nil {
			return fmt.Errorf("创建开机自启动快捷方式: %w", err)
		}
		return nil
	}
	if err := a.config.UnsetStartOnBoot(); err != nil {
		return fmt.Errorf("移除开机自启动快捷方式: %w", err)
	}
	return nil
}

func (a *App) replaceToggleHotkey(previous, value string) error {
	if a.hotkeyHandlerID != "" {
		if err := a.hotkey.Unregister(a.hotkeyHandlerID); err != nil && !errors.Is(err, hotkey.ErrHotkeyHandlerNotFound) {
			return fmt.Errorf("注销旧热键: %w", err)
		}
		a.hotkeyHandlerID = ""
	}

	hk, err := hotkey.ParseHotkey(value)
	if err != nil {
		return err
	}
	handlerID, err := a.hotkey.Register(a.ctx, hk, func() {
		fyne.Do(func() { a.mainWindow.Toggle() })
	})
	if err != nil {
		previousHotkey, previousErr := hotkey.ParseHotkey(previous)
		if previousErr == nil {
			if previousID, restoreErr := a.hotkey.Register(a.ctx, previousHotkey, func() {
				fyne.Do(func() { a.mainWindow.Toggle() })
			}); restoreErr == nil {
				a.hotkeyHandlerID = previousID
			}
		}
		return fmt.Errorf("注册热键 %q: %w", value, err)
	}
	a.hotkeyHandlerID = handlerID
	return nil
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

func buildSearchCategories() []ui.SearchCategory {
	categories := make([]ui.SearchCategory, 0, len(search.CategoryDefinitions())+1)
	categories = append(categories, ui.SearchCategory{
		ID:    "",
		Label: "全部",
	})

	for _, category := range search.CategoryDefinitions() {
		categories = append(categories, ui.SearchCategory{
			ID:    category.ID,
			Label: category.Label,
		})
	}

	return categories
}
