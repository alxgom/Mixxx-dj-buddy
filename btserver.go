package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
	"encoding/base64"
	"io"

	"github.com/dhowden/tag"
	"golang.org/x/image/draw"
)

// ── Data types ────────────────────────────────────────────────────────────────

type TrackInfo struct {
	Artist string  `json:"artist"`
	Title  string  `json:"title"`
	BPM    float64 `json:"bpm"`
	HasArt bool    `json:"has_art"`
}

type trackUpdate struct {
	info TrackInfo
	art  []byte // resized+compressed JPEG for BT, nil = no art
}

type BTAppState struct {
	mu      sync.RWMutex
	track   TrackInfo
	artData []byte // full-resolution, for HTTP
	artMime string
	running bool
}

var (
	btState BTAppState

	clientsMu sync.Mutex
	clients   = make(map[chan trackUpdate]struct{})
	
	// Channels for managing the server lifecycle
	btStopChan chan struct{}
)

// ── HTTP endpoints ────────────────────────────────────────────────────────────

func serveCurrentBTTrack(w http.ResponseWriter, r *http.Request) {
	btState.mu.RLock()
	t := btState.track
	btState.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(t)
}

func serveCoverArt(w http.ResponseWriter, r *http.Request) {
	btState.mu.RLock()
	data, mime := btState.artData, btState.artMime
	btState.mu.RUnlock()
	if len(data) == 0 {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(data)
}

// ── Set Current Track ─────────────────────────────────────────────────────────

func SetCurrentBTTrack(artist, title string, bpm float64, filePath string) {
	btState.mu.RLock()
	currentArtist := btState.track.Artist
	currentTitle := btState.track.Title
	btState.mu.RUnlock()

	// Only update if it actually changed
	if artist == currentArtist && title == currentTitle {
		return
	}

	log.Printf("BT: New track → %s – %s", artist, title)

	rawArt, artMime := extractCoverArt(filePath)
	var btArt []byte
	if len(rawArt) > 0 {
		btArt = prepareArtForBT(rawArt)
		log.Printf("BT: art %d bytes (raw)", len(btArt))
	}

	info := TrackInfo{
		Artist: artist,
		Title:  title,
		BPM:    bpm,
		HasArt: len(rawArt) > 0,
	}

	btState.mu.Lock()
	btState.track = info
	btState.artData = rawArt
	btState.artMime = artMime
	btState.mu.Unlock()

	upd := trackUpdate{info: info, art: btArt}
	
	clientsMu.Lock()
	for ch := range clients {
		select {
		case ch <- upd:
		default:
			select {
			case <-ch:
			default:
			}
			ch <- upd
		}
	}
	clientsMu.Unlock()
}

// ── Image resize ──────────────────────────────────────────────────────────────

func prepareArtForBT(data []byte) []byte {
	if len(data) == 0 {
		return nil
	}
	
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		log.Printf("resize: decode failed: %v", err)
		return nil
	}
	
	dst := image.NewRGBA(image.Rect(0, 0, 600, 600))
	draw.BiLinear.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Src, nil)
	
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 80}); err != nil {
		log.Printf("resize: jpeg encode failed: %v", err)
		return nil
	}
	
	return buf.Bytes()
}

// ── OGG/Opus cover art extractor ─────────────────────────────────────────────

func readOggPage(r io.Reader) (segments [][]byte, err error) {
	sig := make([]byte, 4)
	if _, err = io.ReadFull(r, sig); err != nil {
		return
	}
	if string(sig) != "OggS" {
		return nil, fmt.Errorf("not an OGG page")
	}
	hdr := make([]byte, 23)
	if _, err = io.ReadFull(r, hdr); err != nil {
		return
	}
	nsegs := int(hdr[22])
	table := make([]byte, nsegs)
	if _, err = io.ReadFull(r, table); err != nil {
		return
	}
	segments = make([][]byte, nsegs)
	for i, sz := range table {
		seg := make([]byte, sz)
		if _, err = io.ReadFull(r, seg); err != nil {
			return
		}
		segments[i] = seg
	}
	return
}

func assembleOggPackets(r io.Reader, n int) ([][]byte, error) {
	var packets [][]byte
	var cur []byte
	for len(packets) < n {
		segs, err := readOggPage(r)
		if err != nil {
			return packets, err
		}
		for _, seg := range segs {
			cur = append(cur, seg...)
			if len(seg) < 255 {
				packets = append(packets, cur)
				cur = nil
				if len(packets) >= n {
					return packets, nil
				}
			}
		}
	}
	return packets, nil
}

func parseVorbisComments(data []byte) (imgData []byte, mime string) {
	if len(data) < 4 {
		return
	}
	vl := int(binary.LittleEndian.Uint32(data[:4]))
	data = data[4:]
	if len(data) < vl {
		return
	}
	data = data[vl:]
	if len(data) < 4 {
		return
	}
	n := int(binary.LittleEndian.Uint32(data[:4]))
	data = data[4:]
	for i := 0; i < n; i++ {
		if len(data) < 4 {
			return
		}
		cl := int(binary.LittleEndian.Uint32(data[:4]))
		data = data[4:]
		if len(data) < cl {
			return
		}
		comment := string(data[:cl])
		data = data[cl:]
		if strings.HasPrefix(strings.ToUpper(comment), "METADATA_BLOCK_PICTURE=") {
			b64 := comment[len("METADATA_BLOCK_PICTURE="):]
			raw, err := base64.StdEncoding.DecodeString(b64)
			if err != nil {
				return
			}
			return parseFLACPicture(raw)
		}
	}
	return
}

func parseFLACPicture(d []byte) ([]byte, string) {
	if len(d) < 8 {
		return nil, ""
	}
	d = d[4:]
	mimeLen := int(binary.BigEndian.Uint32(d[:4]))
	d = d[4:]
	if len(d) < mimeLen {
		return nil, ""
	}
	mime := string(d[:mimeLen])
	d = d[mimeLen:]
	if len(d) < 4 {
		return nil, ""
	}
	descLen := int(binary.BigEndian.Uint32(d[:4]))
	d = d[4:]
	if len(d) < descLen+16+4 {
		return nil, ""
	}
	d = d[descLen+16:]
	imgLen := int(binary.BigEndian.Uint32(d[:4]))
	d = d[4:]
	if len(d) < imgLen {
		return nil, ""
	}
	return d[:imgLen], mime
}

func extractCoverArtOpus(filePath string) ([]byte, string) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, ""
	}
	defer f.Close()
	pkts, err := assembleOggPackets(f, 2)
	if err != nil && len(pkts) < 2 {
		return nil, ""
	}
	if len(pkts) < 2 {
		return nil, ""
	}
	tags := pkts[1]
	if len(tags) < 8 || string(tags[:8]) != "OpusTags" {
		return nil, ""
	}
	data, mime := parseVorbisComments(tags[8:])
	return data, mime
}

func extractCoverArt(filePath string) ([]byte, string) {
	ext := strings.ToLower(filepath.Ext(filePath))
	if ext == ".opus" {
		return extractCoverArtOpus(filePath)
	}
	f, err := os.Open(filePath)
	if err != nil {
		return nil, ""
	}
	defer f.Close()
	m, err := tag.ReadFrom(f)
	if err != nil {
		return nil, ""
	}
	pic := m.Picture()
	if pic == nil || len(pic.Data) == 0 {
		return nil, ""
	}
	mime := pic.MIMEType
	if mime == "" {
		mime = "image/jpeg"
	}
	return pic.Data, mime
}

// ── Bluetooth Server Management ───────────────────────────────────────────────

func StartBTServer() {
	btState.mu.Lock()
	if btState.running {
		btState.mu.Unlock()
		return
	}
	btState.running = true
	btStopChan = make(chan struct{})
	btState.mu.Unlock()

	go runBluetoothServer()
}

func StopBTServer() {
	btState.mu.Lock()
	if !btState.running {
		btState.mu.Unlock()
		return
	}
	btState.running = false
	if btStopChan != nil {
		close(btStopChan)
	}
	btState.mu.Unlock()
	log.Println("BT Server stopped.")
}

// ── Bluetooth RFCOMM server (Windows Winsock2) ────────────────────────────────

const (
	afBTH         = 32
	sockStream    = 1
	bthRFCOMM     = 3
	invalidSocket = ^uintptr(0)
)

type sockaddrBTH struct {
	addressFamily  uint16
	btAddrBytes    [8]byte
	serviceClassId [16]byte
	portBytes      [4]byte
}

func makeSockaddrBTH(addr uint64, port uint32) sockaddrBTH {
	var s sockaddrBTH
	s.addressFamily = afBTH
	binary.LittleEndian.PutUint64(s.btAddrBytes[:], addr)
	binary.LittleEndian.PutUint32(s.portBytes[:], port)
	return s
}

func (s *sockaddrBTH) getPort() uint32 {
	return binary.LittleEndian.Uint32(s.portBytes[:])
}

type wsaData struct {
	Version      uint16
	HighVersion  uint16
	MaxSockets   uint16
	MaxUdpDg     uint16
	VendorInfo   uintptr
	Description  [257]byte
	SystemStatus [129]byte
}

type btRadioInfo struct {
	dwSize          uint32
	address         uint64
	szName          [248]uint16
	ulClassofDevice uint32
	lmpSubversion   uint16
	manufacturer    uint16
}

type btFindRadioParams struct {
	dwSize uint32
}

func getLocalBTAddress() uint64 {
	bth := syscall.NewLazyDLL("bthprops.cpl")
	findFirst := bth.NewProc("BluetoothFindFirstRadio")
	getInfo   := bth.NewProc("BluetoothGetRadioInfo")
	findClose := bth.NewProc("BluetoothFindRadioClose")

	params := btFindRadioParams{dwSize: 4}
	var radioHandle uintptr
	findHandle, _, _ := findFirst.Call(
		uintptr(unsafe.Pointer(&params)),
		uintptr(unsafe.Pointer(&radioHandle)),
	)
	if findHandle == 0 {
		return 0
	}
	defer findClose.Call(findHandle)
	defer syscall.CloseHandle(syscall.Handle(radioHandle))

	var info btRadioInfo
	info.dwSize = uint32(unsafe.Sizeof(info))
	r, _, _ := getInfo.Call(radioHandle, uintptr(unsafe.Pointer(&info)))
	if r != 0 {
		return 0
	}
	return info.address
}

func runBluetoothServer() {
	dll := syscall.NewLazyDLL("ws2_32.dll")

	wsaStartup  := dll.NewProc("WSAStartup")
	socketProc  := dll.NewProc("socket")
	bindProc    := dll.NewProc("bind")
	listenProc  := dll.NewProc("listen")
	acceptProc  := dll.NewProc("accept")
	closeProc   := dll.NewProc("closesocket")
	gsnProc     := dll.NewProc("getsockname")
	
	// Optional: we need to interrupt accept() if we stop the server.
	// For simplicity, we just set a read timeout or use a select. 
	// Given raw syscalls, we'll let it block until the process exits or we find a way to close the socket from StopBTServer.

	var wsd wsaData
	r, _, _ := wsaStartup.Call(0x0202, uintptr(unsafe.Pointer(&wsd)))
	if r != 0 {
		log.Printf("BT: WSAStartup failed (code %d)", r)
		return
	}

	sock, _, _ := socketProc.Call(afBTH, sockStream, bthRFCOMM)
	if sock == invalidSocket {
		log.Println("BT: socket() failed")
		return
	}
	defer closeProc.Call(sock)

	// In Go, if we close the socket from another thread, accept() will unblock and fail.
	// We handle this by capturing the server socket so StopBTServer could theoretically close it.
	// For now, we will simply close it on return.

	localBTAddr := getLocalBTAddress()
	if localBTAddr == 0 {
		log.Println("BT: Could not get local BT radio address")
		return
	}

	const rfcommChannel = 4
	addr := makeSockaddrBTH(localBTAddr, rfcommChannel)
	addrSize := uintptr(unsafe.Sizeof(addr))

	r, _, _ = bindProc.Call(
		sock,
		uintptr(unsafe.Pointer(&addr)),
		addrSize,
	)
	if r != 0 {
		log.Println("BT: bind() failed")
		return
	}

	addrLen := uint32(unsafe.Sizeof(addr))
	gsnProc.Call(sock, uintptr(unsafe.Pointer(&addr)), uintptr(unsafe.Pointer(&addrLen)))
	
	log.Printf("BT: Server ready on channel %d", addr.getPort())
	listenProc.Call(sock, 4)

	// Goroutine to handle stop signals and close socket to unblock accept()
	go func() {
		<-btStopChan
		closeProc.Call(sock)
	}()

	for {
		client, _, _ := acceptProc.Call(sock, 0, 0)
		if client == invalidSocket {
			// Check if we were told to stop
			btState.mu.RLock()
			running := btState.running
			btState.mu.RUnlock()
			if !running {
				return
			}
			time.Sleep(time.Second)
			continue
		}
		log.Println("BT: Client connected!")
		go handleBTClient(client, dll)
	}
}

func btWrite(sendProc *syscall.LazyProc, sock uintptr, data []byte) error {
	sent := 0
	for sent < len(data) {
		n, _, _ := sendProc.Call(
			sock,
			uintptr(unsafe.Pointer(&data[sent])),
			uintptr(len(data)-sent),
			0,
		)
		if n == ^uintptr(0) {
			return fmt.Errorf("send error")
		}
		sent += int(n)
	}
	return nil
}

func btSendUpdate(sendProc *syscall.LazyProc, sock uintptr, upd trackUpdate) error {
	jsonBytes, _ := json.Marshal(upd.info)
	msg1 := make([]byte, 5+len(jsonBytes))
	msg1[0] = 0x01
	binary.BigEndian.PutUint32(msg1[1:5], uint32(len(jsonBytes)))
	copy(msg1[5:], jsonBytes)
	if err := btWrite(sendProc, sock, msg1); err != nil {
		return err
	}

	artLen := uint32(len(upd.art))
	msg2hdr := []byte{0x02, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(msg2hdr[1:], artLen)
	if err := btWrite(sendProc, sock, msg2hdr); err != nil {
		return err
	}
	if artLen > 0 {
		if err := btWrite(sendProc, sock, upd.art); err != nil {
			return err
		}
	}
	return nil
}

func handleBTClient(sock uintptr, dll *syscall.LazyDLL) {
	sendProc  := dll.NewProc("send")
	closeProc := dll.NewProc("closesocket")
	defer closeProc.Call(sock)
	defer log.Println("BT: client disconnected")

	btState.mu.RLock()
	upd := trackUpdate{info: btState.track}
	if len(btState.artData) > 0 {
		upd.art = prepareArtForBT(btState.artData)
	}
	btState.mu.RUnlock()

	ch := make(chan trackUpdate, 1)
	clientsMu.Lock()
	clients[ch] = struct{}{}
	clientsMu.Unlock()

	defer func() {
		clientsMu.Lock()
		delete(clients, ch)
		clientsMu.Unlock()
	}()

	if err := btSendUpdate(sendProc, sock, upd); err != nil {
		return
	}

	heartbeat := time.NewTicker(5 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-btStopChan:
			return
		case upd := <-ch:
			if err := btSendUpdate(sendProc, sock, upd); err != nil {
				return
			}
		case <-heartbeat.C:
			if err := btWrite(sendProc, sock, []byte{0x00}); err != nil {
				return
			}
		}
	}
}
