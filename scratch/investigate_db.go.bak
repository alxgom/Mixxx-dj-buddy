package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"runtime"

	_ "modernc.org/sqlite"
)

func main() {
	dbPath, err := getMixxxDBPath()
	if err != nil {
		log.Fatal(err)
	}

	db, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	query := `
		SELECT T.bpm, T.artist, T.title, T.duration, PT."position", T.id
		FROM library T
		JOIN PlaylistTracks PT ON T.id = PT.track_id
		WHERE PT.playlist_id = (
			SELECT P.id
			FROM Playlists P
			WHERE (P.name GLOB '????-??-??' OR P.name GLOB '????-??-?? (*)' OR P.name GLOB '????-??-?? #*')
			ORDER BY P.date_created DESC
			LIMIT 1
		)
		ORDER BY PT."position" ASC;
	`
	rows, err := db.Query(query)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	fmt.Println("Pos | BPM    | Artist - Title (ID)")
	fmt.Println("------------------------------------")
	for rows.Next() {
		var bpm float64
		var artist, title sql.NullString
		var duration float64
		var position int
		var id int
		if err := rows.Scan(&bpm, &artist, &title, &duration, &position, &id); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%3d | %6.2f | %s - %s (%d)\n", position, bpm, artist.String, title.String, id)
	}
}

func getMixxxDBPath() (string, error) {
	usr, err := user.Current()
	if err != nil {
		return "", err
	}
	homeDir := usr.HomeDir
	var paths []string
	switch runtime.GOOS {
	case "darwin":
		paths = append(paths, filepath.Join(homeDir, "Library/Application Support/Mixxx/mixxxdb.sqlite"))
	case "linux":
		paths = append(paths, filepath.Join(homeDir, ".mixxx/mixxxdb.sqlite"))
	case "windows":
		paths = append(paths, filepath.Join(os.Getenv("LOCALAPPDATA"), "Mixxx/mixxxdb.sqlite"))
	}
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("no se encontró mixxxdb.sqlite")
}
