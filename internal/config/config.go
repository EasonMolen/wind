package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

const (
	appDirName               = "newwind"
	configFileName           = "config.json"
	linkName                 = "NewWind.lnk"
	defaultUpdateManifestURL = "https://api.github.com/repos/EasonMolen/wind/releases/latest"
)

type Config struct {
	Window   WindowConfig   `json:"window"`
	Hotkey   HotkeyConfig   `json:"hotkey"`
	Search   SearchConfig   `json:"search"`
	Launcher LauncherConfig `json:"launcher"`
	Icon     IconConfig     `json:"icon"`
	Display  DisplayConfig  `json:"display"`
	Update   UpdateConfig   `json:"update"`
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
	MaxConcurrency           int  `json:"maxConcurrency"`
	StartAutomaticallyOnBoot bool `json:"startAutomaticallyOnBoot"`
}

type IconConfig struct {
	CacheCapacity    int  `json:"cacheCapacity"`
	DefaultTimeoutMS int  `json:"defaultTimeoutMs"`
	EnableExtRouting bool `json:"enableExtRouting"`
}

type DisplayConfig struct {
	ShowCharacter bool `json:"showCharacter"`
}

// UpdateConfig intentionally keeps the endpoint out of the settings dialog.
// Release infrastructure controls it, while users only request a check.
type UpdateConfig struct {
	ManifestURL string `json:"manifestUrl"`
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
	MovePin(path string, targetIndex int) error
	PinnedNum() int
	UpdatePinnedName(oldPath, newPath string) error
	IsStartOnBoot() bool
	SetStartOnBoot() error
	UnsetStartOnBoot() error
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
			MaxConcurrency:           10,
			StartAutomaticallyOnBoot: false,
		},
		Icon: IconConfig{
			CacheCapacity:    1024,
			DefaultTimeoutMS: 3000,
			EnableExtRouting: true,
		},
		Display: DisplayConfig{
			ShowCharacter: true,
		},
		Update: UpdateConfig{
			ManifestURL: defaultUpdateManifestURL,
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

	if err = json.Unmarshal(data, &cfg); err != nil {
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

	// 检查是否和已经存在的冲突, 如果冲突了就表明: 用户是取消固定, 应该从map中清除
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

// MovePin 将指定固定项移动到目标索引。targetIndex 使用移动前的显示序号：
// 例如把第 1 项移动到第 3 项的位置，传入 2 即可。
func (s *cfgService) MovePin(path string, targetIndex int) error {
	cleanPath := cleanPinPath(path)
	if cleanPath == "" {
		return errors.New("pin path is empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	sourceIndex := -1
	key := pinKey(cleanPath)
	for i, pin := range s.cfg.Pins {
		if pinKey(pin.Path) == key {
			sourceIndex = i
			break
		}
	}
	if sourceIndex < 0 {
		return errors.New("pin not found")
	}

	pins := append([]Pins(nil), s.cfg.Pins...)
	moved := pins[sourceIndex]
	pins = append(pins[:sourceIndex], pins[sourceIndex+1:]...)
	if targetIndex < 0 {
		targetIndex = 0
	}
	if targetIndex > len(pins) {
		targetIndex = len(pins)
	}
	pins = append(pins, Pins{})
	copy(pins[targetIndex+1:], pins[targetIndex:])
	pins[targetIndex] = moved

	s.cfg.Pins = pins
	s.syncPinMap()
	return nil
}

func (s *cfgService) UpdatePinnedName(oldPath, newPath string) error {
	cleanOldPath := cleanPinPath(oldPath)
	cleanNewPath := cleanPinPath(newPath)
	if cleanNewPath == "" {
		return errors.New("new path is empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	oldKey := pinKey(cleanOldPath)
	for i, pin := range s.cfg.Pins {
		if pinKey(pin.Path) == oldKey {
			s.cfg.Pins[i].Path = cleanNewPath
			s.cfg.Pins[i].Name = filepath.Base(cleanNewPath)
			s.syncPinMap()
			return nil
		}
	}

	return errors.New("pin not found")
}

func (s *cfgService) PinnedNum() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.pinMap)
}

func (s *cfgService) SetStartOnBoot() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	linkPath, err := startupLinkPath()
	if err != nil {
		return err
	}

	if err = os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		return err
	}

	exePath, err := os.Executable()
	if err != nil {
		return err
	}

	go func(link, target string) {
		if e := createShortcut(link, target); e != nil {
			log.Printf("create shortcut failed: %v", e)
			// 或者 s.onError(e)
		}
	}(linkPath, exePath)

	return nil

}

func (s *cfgService) UnsetStartOnBoot() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	linkPath, err := startupLinkPath()
	if err != nil {
		return err
	}
	if err = os.Remove(linkPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *cfgService) IsStartOnBoot() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	linkPath, err := startupLinkPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(linkPath)
	return err == nil
}

func startupLinkPath() (string, error) {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}

	startupDir := filepath.Join(cfgDir, "Microsoft", "Windows", "Start Menu", "Programs", "Startup")
	return filepath.Join(startupDir, linkName), nil
}

func createShortcut(linkPath, target string) error {
	script := fmt.Sprintf(
		`$ws = New-Object -ComObject WScript.Shell; $sc = $ws.CreateShortcut('%s'); $sc.TargetPath = '%s'; $sc.Save()`,
		linkPath, target,
	)
	cmd := exec.Command("powershell", "-NoProfile", "-Command", script)
	return cmd.Run()
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
	if strings.TrimSpace(cfg.Update.ManifestURL) == "" {
		cfg.Update.ManifestURL = def.Update.ManifestURL
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
