package viz

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// watchExts are the file extensions that trigger live-reload on change.
var watchExts = map[string]bool{
	".j2": true, ".html": true, ".yml": true, ".yaml": true, ".css": true, ".js": true,
}

// sseHub manages Server-Sent Event clients for live reload.
type sseHub struct {
	mu      sync.Mutex
	clients []chan struct{}
}

func (h *sseHub) add() chan struct{} {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	h.clients = append(h.clients, ch)
	h.mu.Unlock()
	return ch
}

func (h *sseHub) remove(ch chan struct{}) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, c := range h.clients {
		if c == ch {
			h.clients = append(h.clients[:i], h.clients[i+1:]...)
			return
		}
	}
}

func (h *sseHub) broadcast() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, ch := range h.clients {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// startWatcher watches dir recursively and calls onChange on relevant file changes.
func startWatcher(dir string, onChange func()) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	// Walk to add all subdirectories.
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || !info.IsDir() {
			return nil
		}
		return w.Add(path)
	})

	go func() {
		defer w.Close()
		for {
			select {
			case event, ok := <-w.Events:
				if !ok {
					return
				}
				if watchExts[filepath.Ext(event.Name)] {
					onChange()
				}
			case _, ok := <-w.Errors:
				if !ok {
					return
				}
			}
		}
	}()
	return nil
}

// dashServer is an HTTP server for a shelby-viz dashboard.
type dashServer struct {
	dashPath  string
	shelbyURL string
	port      int
	mock      *MockPipelinesContext // nil means live mode
	hub       *sseHub
}

func (s *dashServer) newRenderer() *Renderer {
	if s.mock != nil {
		return NewSandboxRenderer(s.dashPath, s.mock)
	}
	return NewRenderer(s.dashPath, s.shelbyURL)
}

func (s *dashServer) listenAndServe(liveReload bool) error {
	dir := s.dashPath
	if info, err := os.Stat(dir); err == nil && !info.IsDir() {
		dir = filepath.Dir(dir)
	}

	mux := http.NewServeMux()

	// Static files (runtime.js) served from the embedded builtins/shell directory.
	shellSub, err := fs.Sub(builtinsFS, "builtins/shell")
	if err != nil {
		return fmt.Errorf("viz: static fs: %w", err)
	}
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(shellSub))))

	// Dashboard page.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		dashboard, err := LoadDashboard(s.dashPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		html, err := s.newRenderer().RenderDashboard(dashboard, liveReload)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, html)
	})

	// Widget refresh endpoint.
	mux.HandleFunc("/api/widget/", func(w http.ResponseWriter, r *http.Request) {
		widgetID := r.URL.Path[len("/api/widget/"):]
		dashboard, err := LoadDashboard(s.dashPath)
		if err != nil {
			jsonErr(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var widget *WidgetDef
		for _, wd := range dashboard.Widgets {
			if wd.ID == widgetID {
				widget = wd
				break
			}
		}
		if widget == nil {
			jsonErr(w, "widget not found", http.StatusNotFound)
			return
		}
		html, err := s.newRenderer().RenderWidget(widget)
		if err != nil {
			html = widgetError(err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"html": html})
	})

	// Live-reload SSE endpoint.
	if liveReload {
		if err := startWatcher(dir, s.hub.broadcast); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: file watcher failed: %v\n", err)
		}
		mux.HandleFunc("/api/livereload", s.sseHandler)
	}

	addr := fmt.Sprintf(":%d", s.port)
	fmt.Printf("  shelby viz: http://localhost%s\n", addr)
	return http.ListenAndServe(addr, mux)
}

func (s *dashServer) sseHandler(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	ch := s.hub.add()
	defer s.hub.remove(ch)

	fmt.Fprint(w, "data: connected\n\n")
	flusher.Flush()

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ch:
			fmt.Fprint(w, "data: reload\n\n")
			flusher.Flush()
		case <-heartbeat.C:
			fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}

func jsonErr(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// Serve starts a live dashboard server backed by a Shelby API.
func Serve(dashPath, shelbyURL string, port int, liveReload bool) error {
	srv := &dashServer{
		dashPath:  dashPath,
		shelbyURL: shelbyURL,
		port:      port,
		hub:       &sseHub{},
	}
	return srv.listenAndServe(liveReload)
}

// ServeSandbox starts a dashboard server using mock data from sandbox.yml.
func ServeSandbox(dashPath string, port int, liveReload bool) error {
	mockData, err := LoadSandbox(dashPath)
	if err != nil {
		return fmt.Errorf("viz: load sandbox: %w", err)
	}
	srv := &dashServer{
		dashPath: dashPath,
		port:     port,
		mock:     &MockPipelinesContext{Data: mockData},
		hub:      &sseHub{},
	}
	return srv.listenAndServe(liveReload)
}

// BuildStatic renders the dashboard to a static HTML file in outDir.
func BuildStatic(dashPath, shelbyURL, outDir string) (string, error) {
	dashboard, err := LoadDashboard(dashPath)
	if err != nil {
		return "", err
	}
	r := NewRenderer(dashPath, shelbyURL)
	html, err := r.RenderDashboard(dashboard, false)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(outDir, "index.html")
	return dest, os.WriteFile(dest, []byte(html), 0o644)
}
