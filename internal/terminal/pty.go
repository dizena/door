package terminal

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

type wsMessage struct {
	Type string `json:"type"` // "input" | "resize"
	Data string `json:"data,omitempty"`
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
}

// Handler upgrades an authenticated HTTP request into an interactive PTY WebSocket session.
type Handler struct {
	workDir string
	logger  *slog.Logger
}

func NewHandler(workDir string, logger *slog.Logger) *Handler {
	return &Handler{workDir: workDir, logger: logger}
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		u, err := url.Parse(origin)
		if err != nil {
			return false
		}
		return strings.EqualFold(u.Host, r.Host)
	},
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Warn("terminal websocket upgrade failed", "err", err)
		return
	}
	defer conn.Close()

	shell := detectShell()
	cmd := exec.CommandContext(r.Context(), shell)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	if h.workDir != "" {
		if info, statErr := os.Stat(h.workDir); statErr == nil && info.IsDir() {
			cmd.Dir = h.workDir
		}
	}

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 100, Rows: 30})
	if err != nil {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("Failed to start shell: "+err.Error()+"\r\n"))
		return
	}
	defer func() {
		_ = ptmx.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	var writeMu sync.Mutex
	writeWS := func(data []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteMessage(websocket.TextMessage, data)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			n, readErr := ptmx.Read(buf)
			if n > 0 {
				if err := writeWS(buf[:n]); err != nil {
					return
				}
			}
			if readErr != nil {
				if readErr != io.EOF {
					_ = writeWS([]byte("\r\n[PTY disconnected]\r\n"))
				}
				return
			}
		}
	}()

	for {
		msgType, raw, err := conn.ReadMessage()
		if err != nil {
			break
		}
		if msgType != websocket.TextMessage && msgType != websocket.BinaryMessage {
			continue
		}

		var msg wsMessage
		if len(raw) > 0 && raw[0] == '{' && json.Unmarshal(raw, &msg) == nil && msg.Type != "" {
			switch msg.Type {
			case "resize":
				if msg.Cols > 0 && msg.Rows > 0 {
					_ = pty.Setsize(ptmx, &pty.Winsize{Cols: msg.Cols, Rows: msg.Rows})
				}
			case "input":
				if msg.Data != "" {
					_, _ = ptmx.Write([]byte(msg.Data))
				}
			}
			continue
		}
		_, _ = ptmx.Write(raw)
	}

	_ = ptmx.Close()
	<-done
}

func detectShell() string {
	for _, candidate := range []string{"/bin/bash", "/bin/zsh", "/bin/sh"} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "sh"
}
