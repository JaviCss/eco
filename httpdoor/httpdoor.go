package httpdoor

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/JaviCss/eco/port"
	"github.com/JaviCss/eco/store"
)

const MaxResultBytes = 32 * 1024

const MaxBodyBytesRequest = 64*1024 + 4096

type Config struct {
	Addr     string
	Token    string
	Nonce    string
	PortFile string
}

type Server struct {
	client  port.Port
	cfg     Config
	mu      sync.Mutex
	addr    string
	port    int
	allowed []string
}

type errorJSON struct {
	Error string `json:"error"`
}

type entriesPayload struct {
	Entries   []port.Entry `json:"entries"`
	Truncated bool         `json:"truncated"`
	Omitted   int          `json:"omitted"`
}

type readRequest struct {
	Scope port.Scope `json:"scope"`
	Axis  port.Axis  `json:"axis"`
	Limit int        `json:"limit"`
}

type searchRequest struct {
	Scope port.Scope `json:"scope"`
	Axis  port.Axis  `json:"axis"`
	Query string     `json:"query"`
	Limit int        `json:"limit"`
}

type getRequest struct {
	Scope port.Scope `json:"scope"`
	Axis  port.Axis  `json:"axis"`
	IDs   []string   `json:"ids"`
}

type appendRequest struct {
	Scope port.Scope `json:"scope"`
	Axis  port.Axis  `json:"axis"`
	Entry port.Entry `json:"entry"`
}

type promoteRequest struct {
	IDs []string `json:"ids"`
}

type sourceProvider interface {
	PromoteSource() port.Port
}

func New(client port.Port, cfg Config) (*Server, error) {
	if client == nil {
		return nil, fmt.Errorf("httpdoor: %w: a nil port cannot back a door", port.ErrForbidden)
	}
	if _, _, err := splitAddr(cfg.Addr); err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, fmt.Errorf("httpdoor: %w: an empty token would leave Z2 anonymous", port.ErrForbidden)
	}
	return &Server{client: client, cfg: cfg}, nil
}

func splitAddr(addr string) (string, string, error) {
	host, portNumber, err := net.SplitHostPort(addr)
	if err != nil {
		return "", "", fmt.Errorf("httpdoor: %w: %q is not host:port", port.ErrForbidden, addr)
	}
	if _, err := strconv.Atoi(portNumber); err != nil {
		return "", "", fmt.Errorf("httpdoor: %w: %q does not carry a numeric port", port.ErrForbidden, addr)
	}
	if strings.EqualFold(host, "localhost") {
		return "", "", fmt.Errorf("httpdoor: %w: localhost is not a loopback literal, write 127.0.0.1 or ::1", port.ErrForbidden)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return "", "", fmt.Errorf("httpdoor: %w: %q is not an IP literal", port.ErrForbidden, host)
	}
	if !ip.IsLoopback() {
		return "", "", fmt.Errorf("httpdoor: %w: %q is not a loopback address", port.ErrForbidden, host)
	}
	return host, portNumber, nil
}

func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

func (s *Server) Port() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.port
}

func (s *Server) Serve(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return fmt.Errorf("httpdoor: listen %s: %w", s.cfg.Addr, port.ErrUnavailable)
	}
	tcp, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		listener.Close()
		return fmt.Errorf("httpdoor: %w: the listener is not a TCP address", port.ErrUnavailable)
	}
	srv := &http.Server{Handler: s, ReadHeaderTimeout: 10 * time.Second}
	s.mu.Lock()
	s.addr = listener.Addr().String()
	s.port = tcp.Port
	s.allowed = allowedHosts(tcp.IP, tcp.Port)
	s.mu.Unlock()

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(listener) }()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		shutdownErr := srv.Shutdown(shutdownCtx)
		err := <-serveErr
		if shutdownErr != nil {
			return fmt.Errorf("httpdoor: shutdown: %w", port.ErrUnavailable)
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("httpdoor: serve: %w", port.ErrUnavailable)
		}
		return nil
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("httpdoor: serve: %w", port.ErrUnavailable)
		}
		return nil
	}
}

func allowedHosts(ip net.IP, portNumber int) []string {
	port := strconv.Itoa(portNumber)
	out := []string{net.JoinHostPort(ip.String(), port), net.JoinHostPort("localhost", port)}
	if ip.To4() != nil {
		out = append(out, net.JoinHostPort("127.0.0.1", port))
	} else {
		out = append(out, net.JoinHostPort("::1", port))
	}
	return out
}

func (s *Server) hostAllowed(host string) bool {
	if host == "" {
		return false
	}
	s.mu.Lock()
	allowed := s.allowed
	s.mu.Unlock()
	if name, _, err := net.SplitHostPort(host); err == nil && strings.EqualFold(name, "localhost") {
		return true
	}
	for _, candidate := range allowed {
		if strings.EqualFold(candidate, host) {
			return true
		}
	}
	return false
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.hostAllowed(r.Host) {
		writeError(w, http.StatusMisdirectedRequest, "host mismatch")
		return
	}
	if r.Header.Get("Origin") != "" {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if !s.authorized(r) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="eco"`)
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	switch r.URL.Path {
	case "/v1/read", "/v1/search", "/v1/get", "/v1/append", "/v1/promote":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !jsonContentType(r) {
			writeError(w, http.StatusUnsupportedMediaType, "unsupported media type")
			return
		}
	case "/v1/probe":
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
	default:
		writeError(w, http.StatusNotFound, "unknown route")
		return
	}
	s.dispatch(w, r)
}

func jsonContentType(r *http.Request) bool {
	raw := r.Header.Get("Content-Type")
	if raw == "" {
		return false
	}
	media, _, err := mime.ParseMediaType(raw)
	if err != nil {
		return false
	}
	return media == "application/json"
}

func (s *Server) authorized(r *http.Request) bool {
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	got := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	want := strings.TrimSpace(s.cfg.Token)
	if got == "" || want == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func (s *Server) decode(w http.ResponseWriter, r *http.Request, out any) bool {
	limited := http.MaxBytesReader(w, r.Body, MaxBodyBytesRequest)
	raw, err := io.ReadAll(limited)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "payload too large")
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid entry")
		return false
	}
	if len(raw) > MaxBodyBytesRequest {
		writeError(w, http.StatusRequestEntityTooLarge, "payload too large")
		return false
	}
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if err := json.Unmarshal(raw, out); err != nil {
		writeError(w, http.StatusBadRequest, "invalid entry")
		return false
	}
	return true
}

func (s *Server) dispatch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	switch r.URL.Path {
	case "/v1/probe":
		if err := s.client.Probe(ctx); err != nil {
			writePortError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "nonce": s.cfg.Nonce})
	case "/v1/read":
		var req readRequest
		if !s.decode(w, r, &req) {
			return
		}
		entries, err := s.client.Read(ctx, req.Scope, req.Axis, resolveLimit(req.Limit))
		if err != nil {
			writePortError(w, err)
			return
		}
		s.writeEntries(w, entries)
	case "/v1/search":
		var req searchRequest
		if !s.decode(w, r, &req) {
			return
		}
		entries, err := s.client.Search(ctx, req.Scope, req.Axis, req.Query, resolveLimit(req.Limit))
		if err != nil {
			writePortError(w, err)
			return
		}
		s.writeEntries(w, entries)
	case "/v1/get":
		var req getRequest
		if !s.decode(w, r, &req) {
			return
		}
		entries, err := s.client.Get(ctx, req.Scope, req.Axis, req.IDs)
		if err != nil {
			writePortError(w, err)
			return
		}
		s.writeEntries(w, entries)
	case "/v1/append":
		var req appendRequest
		if !s.decode(w, r, &req) {
			return
		}
		stored, err := s.client.Append(ctx, req.Scope, req.Axis, req.Entry)
		if err != nil {
			writePortError(w, err)
			return
		}
		s.writeEntries(w, []port.Entry{stored})
	case "/v1/promote":
		var req promoteRequest
		if !s.decode(w, r, &req) {
			return
		}
		entries, err := store.Promote(ctx, s.promoteSource(), s.client, req.IDs)
		if err != nil {
			writePortError(w, err)
			return
		}
		s.writeEntries(w, entries)
	}
}

func (s *Server) promoteSource() port.Port {
	if provider, ok := s.client.(sourceProvider); ok {
		if source := provider.PromoteSource(); source != nil {
			return source
		}
	}
	return s.client
}

func resolveLimit(limit int) int {
	if limit <= 0 {
		return store.MaxLimit
	}
	return limit
}

func (s *Server) writeEntries(w http.ResponseWriter, entries []port.Entry) {
	payload := encodeEntries(entries)
	writeJSON(w, http.StatusOK, payload)
}

func encodeEntries(entries []port.Entry) entriesPayload {
	full, err := json.Marshal(entriesPayload{Entries: entries})
	if err == nil && len(full) <= MaxResultBytes {
		return entriesPayload{Entries: entries}
	}
	for kept := len(entries) - 1; kept >= 0; kept-- {
		candidate, err := json.Marshal(entriesPayload{Entries: entries[:kept]})
		if err == nil && len(candidate) <= MaxResultBytes {
			return entriesPayload{Entries: entries[:kept], Truncated: true, Omitted: len(entries) - kept}
		}
	}
	return entriesPayload{Truncated: len(entries) > 0, Omitted: len(entries)}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, name string) {
	writeJSON(w, status, errorJSON{Error: name})
}

func writePortError(w http.ResponseWriter, err error) {
	status, name := statusFor(err)
	writeError(w, status, name)
}

func statusFor(err error) (int, string) {
	switch {
	case errors.Is(err, port.ErrUnavailable):
		return http.StatusServiceUnavailable, "unavailable"
	case errors.Is(err, port.ErrNotFound):
		return http.StatusNotFound, "not found"
	case errors.Is(err, port.ErrForbidden):
		return http.StatusForbidden, "forbidden"
	case errors.Is(err, port.ErrInvalidScope):
		return http.StatusBadRequest, "invalid scope"
	case errors.Is(err, port.ErrAxisNotInScope):
		return http.StatusBadRequest, "axis not in scope"
	case errors.Is(err, port.ErrInvalidEntry):
		return http.StatusBadRequest, "invalid entry"
	default:
		return http.StatusServiceUnavailable, "unavailable"
	}
}
