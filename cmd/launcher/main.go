package main

import (
	_ "embed"
	"log"
	_ "net/http/pprof"
	"os"
	"wind/assets"
	coreapp "wind/internal/app"

	"fyne.io/fyne/v2"
	fyneapp "fyne.io/fyne/v2/app"
)

func main() {

	fa := fyneapp.NewWithID("wind.newwind")
	fa.SetIcon(fyne.NewStaticResource("appIcon", assets.IconData))

	application, err := coreapp.NewApp(fa)
	if err != nil {
		log.Fatalf("init app: %v", err)
	}

	if err = application.Run(); err != nil {
		log.Fatalf("run app: %v", err)
	}
}

func loadResourceFromFile(path string) fyne.Resource {

	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	return fyne.NewStaticResource("wind", bytes)
}
