package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const (
	appDirName     = "newwind"
	configFileName = "config.json"
)

// Config 是整个程序的用户配置入口。
// app 包只依赖这里的结构，不把具体服务的内部配置散落在 UI 或入口函数里。
type Config struct {
	Window   WindowConfig   `json:"window"`
	Hotkey   HotkeyConfig   `json:"hotkey"`
	Search   SearchConfig   `json:"search"`
	Launcher LauncherConfig `json:"launcher"`
	Icon     IconConfig     `json:"icon"`
}

type WindowConfig struct {
	Title       string  `json:"title"`
	Width       float32 `json:"width"`
	Height      float32 `json:"height"`
	ShowOnStart bool    `json:"showOnStart"`
	HideOnOpen  bool    `json:"hideOnOpen"`
}

type HotkeyConfig struct {
	// Toggle 是显示/隐藏主窗口的全局热键，例如 "Alt+Space"。
	Toggle string `json:"toggle"`
}

type SearchConfig struct {
	// MaxResults 限制 Everything 单次返回数量，避免 UI 被大量结果阻塞。
	MaxResults int `json:"maxResults"`
}

type LauncherConfig struct {
	// MaxConcurrency 限制同时打开文件/目录的数量。
	MaxConcurrency int `json:"maxConcurrency"`
}

type IconConfig struct {
	CacheCapacity    int  `json:"cacheCapacity"`
	DefaultTimeoutMS int  `json:"defaultTimeoutMs"`
	EnableExtRouting bool `json:"enableExtRouting"`
}

type CfgService interface {
	LoadConfig() error
	SaveConfig() error
	Get() Config
	Set(Config)
	Path() string
}

type cfgService struct {
	path string
	cfg  Config
}

func NewService(path string) CfgService {
	if path == "" {
		path = defaultConfigPath()
	}

	return &cfgService{
		path: path,
		cfg:  DefaultConfig(),
	}
}

func DefaultConfig() Config {
	return Config{
		Window: WindowConfig{
			Title:       "NewWind",
			Width:       760,
			Height:      520,
			ShowOnStart: true,
			HideOnOpen:  true,
		},
		Hotkey: HotkeyConfig{
			Toggle: "Alt+Space",
		},
		Search: SearchConfig{
			MaxResults: 50,
		},
		Launcher: LauncherConfig{
			MaxConcurrency: 10,
		},
		Icon: IconConfig{
			CacheCapacity:    1024,
			DefaultTimeoutMS: 3000,
			EnableExtRouting: true,
		},
	}
}

func (s *cfgService) LoadConfig() error {
	cfg := DefaultConfig()

	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.cfg = cfg
		return s.SaveConfig()
	}
	if err != nil {
		return err
	}

	if err = json.Unmarshal(data, &cfg); err != nil {
		return err
	}

	s.cfg = normalize(cfg)
	return nil
}

func (s *cfgService) SaveConfig() error {
	s.cfg = normalize(s.cfg)

	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.path, append(data, '\n'), 0644)
}

func (s *cfgService) Get() Config {
	return s.cfg
}

func (s *cfgService) Set(cfg Config) {
	s.cfg = normalize(cfg)
}

func (s *cfgService) Path() string {
	return s.path
}

func defaultConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return filepath.Join(".", configFileName)
	}
	return filepath.Join(dir, appDirName, configFileName)
}

func normalize(cfg Config) Config {
	def := DefaultConfig()

	if cfg.Window.Title == "" {
		cfg.Window.Title = def.Window.Title
	}
	if cfg.Window.Width <= 0 {
		cfg.Window.Width = def.Window.Width
	}
	if cfg.Window.Height <= 0 {
		cfg.Window.Height = def.Window.Height
	}
	if cfg.Hotkey.Toggle == "" {
		cfg.Hotkey.Toggle = def.Hotkey.Toggle
	}
	if cfg.Search.MaxResults <= 0 {
		cfg.Search.MaxResults = def.Search.MaxResults
	}
	if cfg.Launcher.MaxConcurrency <= 0 {
		cfg.Launcher.MaxConcurrency = def.Launcher.MaxConcurrency
	}
	if cfg.Icon.CacheCapacity <= 0 {
		cfg.Icon.CacheCapacity = def.Icon.CacheCapacity
	}
	if cfg.Icon.DefaultTimeoutMS <= 0 {
		cfg.Icon.DefaultTimeoutMS = def.Icon.DefaultTimeoutMS
	}

	return cfg
}
