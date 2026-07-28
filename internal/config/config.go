package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	appDirName     = "newwind"
	configFileName = "config.json"
)

type Config struct {
	Window   WindowConfig   `json:"window"`
	Hotkey   HotkeyConfig   `json:"hotkey"`
	Search   SearchConfig   `json:"search"`
	Launcher LauncherConfig `json:"launcher"`
	Icon     IconConfig     `json:"icon"`
	Pins     []Pins         `json:"pins"`
}

type WindowConfig struct {
	Title       string  `json:"title"`
	Width       float32 `json:"width"`
	Height      float32 `json:"height"`
	ShowOnStart bool    `json:"showOnStart"`
	HideOnOpen  bool    `json:"hideOnOpen"`
}

type HotkeyConfig struct {
	Toggle string `json:"toggle"`
}

type SearchConfig struct {
	MaxResults int `json:"maxResults"`
}

type LauncherConfig struct {
	MaxConcurrency int `json:"maxConcurrency"`
}

type IconConfig struct {
	CacheCapacity    int  `json:"cacheCapacity"`
	DefaultTimeoutMS int  `json:"defaultTimeoutMs"`
	EnableExtRouting bool `json:"enableExtRouting"`
}

type Pins struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

type CfgService interface {
	LoadConfig() error
	SaveConfig() error
	Get() Config
	Set(Config)
	Path() string
	GetPinDisplayName(path string) (string, bool)
	TogglePin(path string, name string) (bool, error)
}

type cfgService struct {
	path   string
	cfg    Config
	pinMap map[string]string
	mu     sync.RWMutex
}

func NewService(path string) CfgService {
	if path == "" {
		path = defaultConfigPath()
	}

	return &cfgService{
		path:   path,
		cfg:    DefaultConfig(),
		pinMap: map[string]string{},
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
		Pins: []Pins{},
	}
}

func (s *cfgService) LoadConfig() error {
	cfg := DefaultConfig()

	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.cfg = normalize(cfg)
		s.syncPinMap()
		return s.saveLocked()
	}
	if err != nil {
		return err
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = normalize(cfg)
	s.syncPinMap()
	return nil
}

func (s *cfgService) SaveConfig() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

func (s *cfgService) saveLocked() error {
	s.cfg = normalize(s.cfg)
	s.syncPinMap()

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
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

func (s *cfgService) Set(cfg Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = normalize(cfg)
	s.syncPinMap()
}

func (s *cfgService) Path() string {
	return s.path
}

func (s *cfgService) GetPinDisplayName(path string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	name, ok := s.pinMap[pinKey(path)]
	return name, ok
}

func (s *cfgService) TogglePin(path string, name string) (bool, error) {
	cleanPath := cleanPinPath(path)
	if cleanPath == "" {
		return false, errors.New("pin path is empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := pinKey(cleanPath)
	for i, pin := range s.cfg.Pins {
		if pinKey(pin.Path) != key {
			continue
		}

		s.cfg.Pins = append(s.cfg.Pins[:i], s.cfg.Pins[i+1:]...)
		s.syncPinMap()
		return false, nil
	}

	displayName := strings.TrimSpace(name)
	if displayName == "" {
		displayName = filepath.Base(cleanPath)
	}
	if displayName == "" {
		displayName = cleanPath
	}

	s.cfg.Pins = append(s.cfg.Pins, Pins{
		Path: cleanPath,
		Name: displayName,
	})
	s.syncPinMap()
	return true, nil
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

	cfg.Pins = normalizePins(cfg.Pins)
	return cfg
}

func (s *cfgService) syncPinMap() {
	pinMap := make(map[string]string, len(s.cfg.Pins))
	for _, pin := range s.cfg.Pins {
		pinMap[pinKey(pin.Path)] = pin.Name
	}
	s.pinMap = pinMap
}

func normalizePins(pins []Pins) []Pins {
	if len(pins) == 0 {
		return []Pins{}
	}

	normalized := make([]Pins, 0, len(pins))
	seen := make(map[string]struct{}, len(pins))
	for _, pin := range pins {
		path := cleanPinPath(pin.Path)
		if path == "" {
			continue
		}

		key := pinKey(path)
		if _, exists := seen[key]; exists {
			continue
		}

		name := strings.TrimSpace(pin.Name)
		if name == "" {
			name = filepath.Base(path)
		}
		if name == "" {
			name = path
		}

		normalized = append(normalized, Pins{
			Path: path,
			Name: name,
		})
		seen[key] = struct{}{}
	}

	return normalized
}

func cleanPinPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

func pinKey(path string) string {
	path = cleanPinPath(path)
	if path == "" {
		return ""
	}
	if os.PathSeparator == '\\' {
		return strings.ToLower(path)
	}
	return path
}
