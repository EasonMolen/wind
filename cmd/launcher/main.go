package main

import (
	"log"
	coreapp "wind/internal/app"

	fyneapp "fyne.io/fyne/v2/app"
)

func main() {
	fa := fyneapp.NewWithID("wind.newwind")

	application, err := coreapp.NewApp(fa)
	if err != nil {
		log.Fatalf("init app: %v", err)
	}

	if err = application.Run(); err != nil {
		log.Fatalf("run app: %v", err)
	}
}
