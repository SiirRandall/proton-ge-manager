package app

import (
	"fmt"
	"runtime"

	"github.com/SiirRandall/proton-ge-manager/internal/assets"
	"github.com/SiirRandall/proton-ge-manager/internal/ui"

	"fyne.io/fyne/v2"
	fynex "fyne.io/fyne/v2/app"
)

// Run is the entry point used by cmd.
func Run() {
	if runtime.GOOS != "linux" {
		fmt.Println("Note: Proton-GE is primarily for Linux Steam installations. Paths here target Linux layouts.")
	}

	// Create the Fyne app and set icon (embedded).
	a := fynex.NewWithID("com.sirrandall.protonge.manager")
	var icon fyne.Resource
	if len(assets.AppIconBytes) > 0 {
		icon = fyne.NewStaticResource("icon.png", assets.AppIconBytes)
		a.SetIcon(icon)
	}

	w := a.NewWindow("Proton-GE Manager")
	if icon != nil {
		w.SetIcon(icon)
	}
	w.Resize(fyne.NewSize(980, 640))

	// Build and mount the UI.
	ui.Build(w)

	w.ShowAndRun()
}
