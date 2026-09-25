package ui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// Settings is the deliberately small, user-facing subset of application
// configuration. Advanced engine knobs remain in config.json.
type Settings struct {
	ToggleHotkey  string
	ShowOnStart   bool
	StartOnBoot   bool
	HideOnOpen    bool
	ShowCharacter bool
	MaxResults    int
	Version       string
}

type UpdateStatus struct {
	CurrentVersion string
	LatestVersion  string
	Available      bool
	Notes          string
}

func (w *mainWindow) showSettings() {
	if w.callbacks.GetSettings == nil || w.callbacks.SaveSettings == nil {
		w.status.SetText("设置服务没有配置")
		return
	}

	settings := w.callbacks.GetSettings()
	hotkey := widget.NewEntry()
	hotkey.SetText(settings.ToggleHotkey)
	maxResults := widget.NewEntry()
	maxResults.SetText(strconv.Itoa(settings.MaxResults))
	showOnStart := widget.NewCheck("启动时显示窗口", nil)
	showOnStart.SetChecked(settings.ShowOnStart)
	startAutomaticallyOnBoot := widget.NewCheck("开机自启动", nil)
	startAutomaticallyOnBoot.SetChecked(settings.StartOnBoot)
	hideOnOpen := widget.NewCheck("打开结果后隐藏窗口", nil)
	hideOnOpen.SetChecked(settings.HideOnOpen)
	showCharacter := widget.NewCheck("固定项显示名称", nil)
	showCharacter.SetChecked(settings.ShowCharacter)
	version := widget.NewLabel(settings.Version)
	checkUpdates := widget.NewButton("检查更新", func() {
		if w.callbacks.CheckUpdates == nil {
			dialog.ShowInformation("检查更新", "此版本尚未配置更新源。", w.window)
			return
		}
		go func() {
			status, err := w.callbacks.CheckUpdates(context.Background())
			fyne.Do(func() {
				if err != nil {
					dialog.ShowError(err, w.window)
					return
				}
				if !status.Available {
					dialog.ShowInformation("检查更新", "已是最新版本（"+status.CurrentVersion+"）。", w.window)
					return
				}
				message := "发现新版本 " + status.LatestVersion + "。下载与安装将在发布源配置完成后启用。"
				if status.Notes != "" {
					message += "\n\n" + status.Notes
				}
				dialog.ShowInformation("发现更新", message, w.window)
			})
		}()
	})

	form := dialog.NewForm("设置", "保存", "取消", []*widget.FormItem{
		widget.NewFormItem("唤起热键", hotkey),
		widget.NewFormItem("最大搜索结果", maxResults),
		widget.NewFormItem("", showOnStart),
		widget.NewFormItem("", startAutomaticallyOnBoot),
		widget.NewFormItem("", hideOnOpen),
		widget.NewFormItem("", showCharacter),
		widget.NewFormItem("当前版本", version),
		widget.NewFormItem("", checkUpdates),
	}, func(confirmed bool) {
		if !confirmed {
			return
		}
		limit, err := strconv.Atoi(strings.TrimSpace(maxResults.Text))
		if err != nil || limit < 10 || limit > 200 {
			dialog.ShowError(fmt.Errorf("最大搜索结果必须是 10 到 200 之间的整数"), w.window)
			return
		}

		updated := Settings{
			ToggleHotkey:  strings.TrimSpace(hotkey.Text),
			ShowOnStart:   showOnStart.Checked,
			StartOnBoot:   startAutomaticallyOnBoot.Checked,
			HideOnOpen:    hideOnOpen.Checked,
			ShowCharacter: showCharacter.Checked,
			MaxResults:    limit,
		}
		if err = w.callbacks.SaveSettings(updated); err != nil {
			dialog.ShowError(err, w.window)
			return
		}
		w.status.SetText("设置已保存")
	}, w.window)
	form.Resize(w.window.Canvas().Size())
	form.Show()
}
