# Proton-GE Manager

**Proton-GE Manager** is a lightweight Linux application for easily installing, updating, and managing Proton-GE builds for Steam.  
It provides a clean graphical interface built with Fyne and removes the need for manual downloads or file extraction.

---

## Features

- Fetches the latest Proton-GE releases from GitHub.
- Installs Proton-GE automatically into Steam’s `compatibilitytools.d` directory.
- Shows both Installed and Available Proton-GE versions.
- Hides versions that are already installed.
- Provides one-click installation and removal.
- Displays a live status log when downloading and extracting.
- Automatically refreshes lists after install/remove actions.
- Detects the Steam install path automatically.
- Works properly in both light and dark desktop themes.

---

## Screenshot

![Proton-GE Manager Screenshot](assets/screenshot.png)

--- 

## Installation

### Download AppImage

Go to the Releases page and download:

```
proton-ge-manager-linux-x86_64.AppImage
```

Then make it executable and run it:

```bash
chmod +x proton-ge-manager-linux-x86_64.AppImage
./proton-ge-manager-linux-x86_64.AppImage
```

---

## Steam Compatibility Tools Location

Proton-GE Manager automatically searches for the correct Proton-GE install directory and supports all common Steam setups on Linux.

The following locations are checked:

- `~/.local/share/Steam/compatibilitytools.d`  
  (Native Steam using XDG paths)

- `~/.steam/steam/compatibilitytools.d`  
  (Legacy Steam layout)

- `~/.steam/root/compatibilitytools.d`  
  (Older legacy path)

- `~/.var/app/com.valvesoftware.Steam/data/Steam/compatibilitytools.d`  
  (Flatpak Steam)

If the chosen directory does not exist, Proton-GE Manager will automatically create it.

---

## Building From Source

Requirements:

- Go 1.22+
- GCC / build-essential
- Fyne dependencies (X11, OpenGL, pkg-config)

Build the application:

```bash
go build -C cmd -o proton-ge-manager
```

Run:

```bash
./proton-ge-manager
```

---

## Why This Exists

Installing Proton-GE manually involves downloading, extracting, and placing directories in the correct location.  
Proton-GE Manager automates all these steps with a simple, intuitive graphical interface.

---

## License

MIT License  
See the `LICENSE` file for details.
