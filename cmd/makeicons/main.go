package main

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
)

// The vinyl SVG — same as in the app, no animation, just static paths
const vinylSVG = `<?xml version="1.0" encoding="utf-8"?>
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 512 512" width="512" height="512">
  <!-- Outer grooves / record body -->
  <path fill="#222222" d="M256,0C114.837,0,0,114.843,0,256s114.837,256,256,256s256-114.843,256-256S397.163,0,256,0z
    M149.75,362.25c6.521,6.516,6.521,17.087,0,23.609c-6.522,6.522-17.086,6.522-23.609,0
    C91.5,351.223,72.424,305.109,72.424,256s19.076-95.223,53.718-129.859
    c6.521-6.521,17.087-6.521,23.609,0s6.521,17.092,0,23.609
    c-28.326,28.331-43.935,66.065-43.935,106.25S121.424,333.919,149.75,362.25z
    M256,339.478c-46.032,0-83.478-37.446-83.478-83.478s37.446-83.478,83.478-83.478
    s83.478,37.446,83.478,83.478S302.032,339.478,256,339.478z
    M385.859,385.859c-6.522,6.522-17.086,6.522-23.609,0
    c-6.521-6.521-6.521-17.092,0-23.609c28.326-28.331,43.935-66.065,43.935-106.25
    s-15.608-77.919-43.935-106.25c-6.521-6.516-6.521-17.087,0-23.609
    c6.521-6.521,17.087-6.521,23.609,0
    c34.641,34.636,53.718,80.75,53.718,129.859S420.5,351.223,385.859,385.859z" />
  <!-- Center label (purple/magenta) -->
  <path fill="#8b008b" d="M256,205.913c-27.619,0-50.087,22.468-50.087,50.087
    s22.468,50.087,50.087,50.087s50.087-22.468,50.087-50.087S283.619,205.913,256,205.913z
    M256,272.696c-9.22,0-16.696-7.475-16.696-16.696c0-9.22,7.475-16.696,16.696-16.696
    c9.22,0,16.696,7.475,16.696,16.696C272.696,265.22,265.22,272.696,256,272.696z" />
</svg>`

func renderSVG(svgStr string, width, height int, outPath string) error {
	icon, err := oksvg.ReadIconStream(bytes.NewBufferString(svgStr))
	if err != nil {
		return fmt.Errorf("ReadIconStream: %w", err)
	}
	icon.SetTarget(0, 0, float64(width), float64(height))

	rgba := image.NewRGBA(image.Rect(0, 0, width, height))
	icon.Draw(rasterx.NewDasher(width, height, rasterx.NewScannerGV(width, height, rgba, rgba.Bounds())), 1)

	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, rgba)
}

func main() {
	targets := []struct {
		dir  string
		size int
	}{
		// Vinyl placeholder for cover art area (large, used as ImageView src)
		{"../../../mixxx-bt-display/app/src/main/res/drawable", 600},
		// App launcher icons (mipmap densities)
		{"../../../mixxx-bt-display/app/src/main/res/mipmap-mdpi", 48},
		{"../../../mixxx-bt-display/app/src/main/res/mipmap-hdpi", 72},
		{"../../../mixxx-bt-display/app/src/main/res/mipmap-xhdpi", 96},
		{"../../../mixxx-bt-display/app/src/main/res/mipmap-xxhdpi", 144},
		{"../../../mixxx-bt-display/app/src/main/res/mipmap-xxxhdpi", 192},
	}

	// The display-in-screen icon SVG (for launcher)
	displaySVG := `<?xml version="1.0" encoding="utf-8"?>
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 512 512" width="512" height="512">
  <rect x="40" y="20" width="432" height="472" rx="30" ry="30" fill="#333333" />
  <rect x="60" y="60" width="392" height="392" rx="10" ry="10" fill="#ffffff" />
  <g transform="translate(100, 100) scale(0.609)">
    <path fill="#222222" d="M256,0C114.837,0,0,114.843,0,256s114.837,256,256,256s256-114.843,256-256S397.163,0,256,0z
      M149.75,362.25c6.521,6.516,6.521,17.087,0,23.609c-6.522,6.522-17.086,6.522-23.609,0
      C91.5,351.223,72.424,305.109,72.424,256s19.076-95.223,53.718-129.859
      c6.521-6.521,17.087-6.521,23.609,0s6.521,17.092,0,23.609
      c-28.326,28.331-43.935,66.065-43.935,106.25S121.424,333.919,149.75,362.25z
      M256,339.478c-46.032,0-83.478-37.446-83.478-83.478s37.446-83.478,83.478-83.478
      s83.478,37.446,83.478,83.478S302.032,339.478,256,339.478z
      M385.859,385.859c-6.522,6.522-17.086,6.522-23.609,0
      c-6.521-6.521-6.521-17.092,0-23.609c28.326-28.331,43.935-66.065,43.935-106.25
      s-15.608-77.919-43.935-106.25c-6.521-6.516-6.521-17.087,0-23.609
      c6.521-6.521,17.087-6.521,23.609,0
      c34.641,34.636,53.718,80.75,53.718,129.859S420.5,351.223,385.859,385.859z" />
    <path fill="#8b008b" d="M256,205.913c-27.619,0-50.087,22.468-50.087,50.087
      s22.468,50.087,50.087,50.087s50.087-22.468,50.087-50.087S283.619,205.913,256,205.913z
      M256,272.696c-9.22,0-16.696-7.475-16.696-16.696c0-9.22,7.475-16.696,16.696-16.696
      c9.22,0,16.696,7.475,16.696,16.696C272.696,265.22,265.22,272.696,256,272.696z" />
  </g>
</svg>`

	// Generate vinyl placeholder
	drawableDir := "../../../mixxx-bt-display/app/src/main/res/drawable"
	os.MkdirAll(drawableDir, 0755)
	if err := renderSVG(vinylSVG, 600, 600, drawableDir+"/vinyl_placeholder.png"); err != nil {
		fmt.Println("Error vinyl_placeholder.png:", err)
	} else {
		fmt.Println("Generated vinyl_placeholder.png (600x600)")
	}

	// Generate launcher icons
	for _, t := range targets[1:] {
		os.MkdirAll(t.dir, 0755)
		out := t.dir + "/ic_launcher.png"
		if err := renderSVG(displaySVG, t.size, t.size, out); err != nil {
			fmt.Printf("Error %s: %v\n", out, err)
		} else {
			fmt.Printf("Generated %s (%dx%d)\n", out, t.size, t.size)
		}
	}

	// ── Windows exe icon (vinyl record) ──────────────────────────────────────
	// go-winres reads these PNGs from winres/ and embeds them in the .ico.
	// 256px → desktop/explorer large icon view
	// 16px  → taskbar, title bar, alt-tab
	winresDir := "../../winres"
	for _, ic := range []struct {
		size int
		name string
	}{
		{256, "icon.png"},
		{16, "icon16.png"},
	} {
		out := winresDir + "/" + ic.name
		if err := renderSVG(vinylSVG, ic.size, ic.size, out); err != nil {
			fmt.Printf("Error %s: %v\n", out, err)
		} else {
			fmt.Printf("Generated %s (%dx%d)\n", out, ic.size, ic.size)
		}
	}

	fmt.Println("Done.")
}
