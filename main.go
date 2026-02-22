package main

import (
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"sync"
	"syscall" // Para Windows API
	"time"

	webview "github.com/webview/webview_go"
	_ "modernc.org/sqlite"
)

//go:embed frontend/*
var frontendFS embed.FS

type intptr int

type TrackData struct {
	BPM       float64 `json:"bpm"`
	Artist    string  `json:"artist"`
	Title     string  `json:"title"`
	Duration  float64 `json:"duration"`
	Crates    string  `json:"crates"`
	Playlists string  `json:"playlists"`
}

// AppState (Estado global para compartir datos entre el poller y el servidor web)
type AppState struct {
	mu     sync.RWMutex
	tracks []TrackData
}

var appState AppState

func main() {
	log.Println("Iniciando analizador de historial de Mixxx...")

	// --- PASO 1: Encontrar y conectar a la BD ---
	dbPath, err := getMixxxDBPath()
	if err != nil {
		log.Fatalf("Error fatal: No se pudo encontrar la base de datos de Mixxx: %v", err)
	}
	log.Printf("Base de datos encontrada en: %s", dbPath)

	db, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		log.Fatalf("Error al abrir la base de datos: %v", err)
	}
	defer db.Close()

	// --- PASO 2: Iniciar el sondeo de la BD (en segundo plano) ---
	go pollMixxxDB(db)

	// --- PASO 3: Iniciar el servidor web (en segundo plano) ---
	// La ventana webview necesita un servidor web al que apuntar
	subFS, err := fs.Sub(frontendFS, "frontend")
	if err != nil {
		log.Fatalf("Error preparando frontend embebido: %v", err)
	}
	http.Handle("/", http.FileServer(http.FS(subFS)))
	http.HandleFunc("/api/data", serveData)
	go func() {
		log.Println("Iniciando servidor web interno en http://localhost:8080...")
		if err := http.ListenAndServe(":8080", nil); err != nil {
			log.Fatalf("Error al iniciar el servidor web: %v", err)
		}
	}()

	// Esperar un segundo para que el servidor web se inicie
	time.Sleep(1 * time.Second)

	// --- PASO 4: Crear la Ventana Nativa Webview ---
	debug := true // Ponlo en 'true' para depurar
	w := webview.New(debug)
	if w == nil {
		log.Fatal("Falló al crear la ventana webview")
	}
	defer w.Destroy()
	w.SetTitle("Mixxx DJ Buddy")
	w.SetSize(800, 600, webview.HintNone) // Tamaño de la ventana

	// --- ¡NUEVO! Lógica de Overlay (Solo para Windows) ---
	var windowHwnd uintptr
	var setWindowPosProc *syscall.LazyProc

	if runtime.GOOS == "windows" {
		log.Println("Aplicando configuración de Overlay (Always on Top)...")
		hwnd := w.Window()
		if hwnd != nil {
			windowHwnd = uintptr(hwnd)
			user32 := syscall.NewLazyDLL("user32.dll")
			setWindowPosProc = user32.NewProc("SetWindowPos")

			// Constantes de Windows
			HWND_TOPMOST := int32(-1)
			SWP_NOMOVE := 0x0002
			SWP_NOSIZE := 0x0001

			// Set Always on Top by default
			ret, _, err := setWindowPosProc.Call(
				windowHwnd,
				uintptr(HWND_TOPMOST),
				0,
				0,
				0,
				0,
				uintptr(SWP_NOMOVE|SWP_NOSIZE),
			)
			if ret == 0 {
				log.Printf("Error setting 'Always on Top': %v", err)
			}
		} else {
			log.Println("Could not get window handle for overlay.")
		}
	}
	// --- Fin de la lógica de Overlay ---

	// Bind JS-callable function: setAlwaysOnTop(bool)
	w.Bind("setAlwaysOnTop", func(enabled bool) {
		if setWindowPosProc == nil || windowHwnd == 0 {
			return
		}
		var flag int32
		if enabled {
			flag = -1 // HWND_TOPMOST
		} else {
			flag = -2 // HWND_NOTOPMOST
		}
		setWindowPosProc.Call(
			windowHwnd,
			uintptr(flag),
			0, 0, 0, 0,
			uintptr(0x0002|0x0001), // SWP_NOMOVE | SWP_NOSIZE
		)
	})

	w.Navigate("http://localhost:8080")
	w.Run()
}

// pollMixxxDB consulta la BD cada 5 segundos
func pollMixxxDB(db *sql.DB) {
	for {
		rowsScanned := 0

		// Esta consulta busca la playlist más reciente por fecha Y que tenga formato de fecha
		// y luego obtiene los tracks de esa playlist en orden.
		query := `
			SELECT T.bpm, T.artist, T.title, T.duration,
			  COALESCE(
			    (SELECT GROUP_CONCAT(C.name, ', ')
			     FROM crate_tracks CT
			     JOIN crates C ON CT.crate_id = C.id
			     WHERE CT.track_id = T.id), ''
			  ) AS crates,
			  COALESCE(
			    (SELECT GROUP_CONCAT(P2.name, ', ')
			     FROM PlaylistTracks PT2
			     JOIN Playlists P2 ON PT2.playlist_id = P2.id
			     WHERE PT2.track_id = T.id
			       AND P2.name NOT GLOB '????-??-??'
			       AND P2.name NOT GLOB '????-??-?? (*'
			       AND P2.name NOT GLOB '????-??-?? #*'
			       AND P2.hidden = 0), ''
			  ) AS playlists
			FROM library T
			JOIN PlaylistTracks PT ON T.id = PT.track_id
			WHERE PT.playlist_id = (
				SELECT P.id
				FROM Playlists P
				WHERE (P.name GLOB '????-??-??' OR P.name GLOB '????-??-?? (*)' OR P.name GLOB '????-??-?? #*')
				ORDER BY P.date_created DESC
				LIMIT 1
			)
			ORDER BY
			  PT."position" ASC;
		`
		rows, err := db.Query(query)
		if err != nil {
			log.Printf("Error al consultar la BD: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		var currentTracks []TrackData
		for rows.Next() {
			rowsScanned++
			var bpm float64
			var artist sql.NullString
			var title sql.NullString
			var duration sql.NullFloat64
			var crates sql.NullString
			var playlists sql.NullString
			if err := rows.Scan(&bpm, &artist, &title, &duration, &crates, &playlists); err != nil {
				log.Printf("Error scanning row: %v", err)
				continue
			}

			if bpm > 0 {
				art := "Unknown Artist"
				if artist.Valid && artist.String != "" {
					art = artist.String
				}
				tit := "Unknown Title"
				if title.Valid && title.String != "" {
					tit = title.String
				}
				dur := 0.0
				if duration.Valid {
					dur = duration.Float64
				}
				crt := ""
				if crates.Valid {
					crt = crates.String
				}
				pls := ""
				if playlists.Valid {
					pls = playlists.String
				}
				currentTracks = append(currentTracks, TrackData{
					BPM:       bpm,
					Artist:    art,
					Title:     tit,
					Duration:  dur,
					Crates:    crt,
					Playlists: pls,
				})
			}
		}
		rows.Close()

		if len(currentTracks) > 0 {
			log.Printf("Datos actualizados. %d tracks encontrados con BPM > 0.", len(currentTracks))
		} else if rowsScanned > 0 {
			log.Println("Tracks encontrados, pero todos tienen 0 BPM. Esperando a que Mixxx los analice.")
		} else {
			log.Println("Esperando datos de historial (no se encontraron playlists con formato de fecha)...")
		}

		// Actualizar el estado global de forma segura
		appState.mu.Lock()
		appState.tracks = currentTracks
		appState.mu.Unlock()

		time.Sleep(2 * time.Second)
	}
}

// serveData es el endpoint de la API /api/data
func serveData(w http.ResponseWriter, r *http.Request) {
	appState.mu.RLock()
	tracksToJS := appState.tracks
	appState.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(tracksToJS); err != nil {
		log.Printf("Error al codificar JSON: %v", err)
		http.Error(w, "Error interno", http.StatusInternalServerError)
	}
}

// getMixxxDBPath intenta encontrar el archivo mixxxdb.sqlite
func getMixxxDBPath() (string, error) {
	usr, err := user.Current()
	if err != nil {
		return "", err
	}
	homeDir := usr.HomeDir

	var paths []string

	switch runtime.GOOS {
	case "darwin": // macOS
		paths = append(paths, filepath.Join(homeDir, "Library/Application Support/Mixxx/mixxxdb.sqlite"))
	case "linux":
		paths = append(paths, filepath.Join(homeDir, ".mixxx/mixxxdb.sqlite"))
	case "windows":
		// os.Getenv("LOCALAPPDATA") usualmente es C:\Users\<usuario>\AppData\Local
		paths = append(paths, filepath.Join(os.Getenv("LOCALAPPDATA"), "Mixxx/mixxxdb.sqlite"))
	}

	// Probar todas las rutas candidatas
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			// El archivo existe
			return path, nil
		}
	}

	return "", fmt.Errorf("no se encontró mixxxdb.sqlite en las rutas estándar")
}
