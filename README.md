# Mixxx DJ Buddy

A lightweight, always-on-top desktop overlay for DJs using [Mixxx](https://mixxx.org/). It reads the Mixxx SQLite database in real-time to display a live BPM chart, track history, and — with the integrated Bluetooth server — streams cover art and metadata directly to a wireless e-ink display.

![Mixxx DJ Buddy Icon](icon.png)

## Features

- **Live BPM Chart:** Real-time D3.js visualization of BPM across your set, rendered inside a native WebView window.
- **Track History List:** Scrollable list of all tracks played in the current session.
- **Always on Top:** Stays over your DJ software. Togglable in settings.
- **Bluetooth E-Reader Broadcast:** An integrated Bluetooth RFCOMM server that sends cover art and track metadata wirelessly to connected Android devices (e.g., a rooted Likebook e-reader).
- **Cover Art Preview:** Click the Image icon in the top bar to see exactly what's being sent to the e-reader in a floating "Now Playing" modal.
- **Themes & Layout:** Dark mode by default, with a "Jazzy" light theme. Switch between split/chart/list layouts.
- **Persistent Settings:** All toggles (theme, layout, BT server state, etc.) are saved in `localStorage`.

## Window Icon

The app window uses a custom vinyl record SVG icon embedded directly into the `.exe` via `go-winres`.

## Build

```powershell
cd "c:\DEV\Mixxx dj buddy"

# Generate Windows resources (icon embedded into .exe)
go-winres make

# Build the final executable
go build -ldflags="-H windowsgui" -o mixxx-dj-buddy.exe .
```

## Run (Development)

```powershell
go run .
```

## Bluetooth E-Reader Setup

1. Toggle the **broadcast icon** (Wi-Fi waves) in the top-right of the UI to start the Bluetooth RFCOMM server on Channel 4.
2. The icon lights up in the theme color when the server is active. Your state is saved — if you leave it on, it will auto-start on the next launch.
3. The **image icon** opens a preview modal showing the current track's cover art, artist, and title — exactly what the e-reader will display.

## Dependencies

| Package | Purpose |
|---|---|
| `github.com/webview/webview_go` | Native desktop WebView window |
| `modernc.org/sqlite` | Pure-Go SQLite driver (no CGo needed for DB) |
| `github.com/dhowden/tag` | Audio tag / cover art extraction |
| `golang.org/x/image/draw` | High-quality image resizing for Bluetooth transfer |
| `github.com/srwiley/oksvg` + `rasterx` | SVG rendering for icon generation |
