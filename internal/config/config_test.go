package config

import "testing"

func TestMovePinPersistsRequestedOrder(t *testing.T) {
	configPath := t.TempDir() + "/config.json"
	service := NewService(configPath)
	service.Set(Config{Pins: []Pins{
		{Path: `C:\Apps\first.exe`, Name: "first"},
		{Path: `C:\Apps\second.exe`, Name: "second"},
		{Path: `C:\Apps\third.exe`, Name: "third"},
	}})

	if err := service.MovePin(`C:\Apps\first.exe`, 2); err != nil {
		t.Fatalf("MovePin returned error: %v", err)
	}
	if err := service.SaveConfig(); err != nil {
		t.Fatalf("SaveConfig returned error: %v", err)
	}

	reloaded := NewService(configPath)
	if err := reloaded.LoadConfig(); err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	pins := reloaded.Get().Pins
	want := []string{`C:\Apps\second.exe`, `C:\Apps\third.exe`, `C:\Apps\first.exe`}
	if len(pins) != len(want) {
		t.Fatalf("got %d pins, want %d", len(pins), len(want))
	}
	for i, path := range want {
		if pins[i].Path != path {
			t.Fatalf("pin %d = %q, want %q", i, pins[i].Path, path)
		}
	}
}
