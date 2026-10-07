package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/JaviCss/eco/httpdoor"
	"github.com/JaviCss/eco/port"
	"github.com/JaviCss/eco/store"
)

type serveClient struct {
	*store.Store
}

func (c serveClient) PromoteSource() port.Port {
	return c.Store
}

func randomHex(bytes int) (string, error) {
	raw := make([]byte, bytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("eco: %w: %v", port.ErrUnavailable, err)
	}
	return hex.EncodeToString(raw), nil
}

func serveVerb(args []string) {
	v := parseFlags("serve", args)
	if strings.TrimSpace(v.userDB) == "" || strings.TrimSpace(v.projectDB) == "" {
		refuse("serve", "--user-db and --project-db are required")
	}
	if v.parentPID <= 0 {
		refuse("serve", "--parent-pid is required: eco serve dies with the process that launched it")
	}
	if strings.TrimSpace(v.portFile) == "" {
		refuse("serve", "--port-file is required")
	}
	addr := strings.TrimSpace(v.addr)
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	token, err := randomHex(32)
	if err != nil {
		fail("serve", err)
	}
	nonce, err := randomHex(16)
	if err != nil {
		fail("serve", err)
	}
	s := openStore(v.userDB, v.projectDB, "http", store.ProfileHuman)
	client := serveClient{Store: s}
	srv, err := httpdoor.New(client, httpdoor.Config{Addr: addr, Token: token, Nonce: nonce})
	if err != nil {
		s.Close()
		fail("serve", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx) }()
	portNumber := waitForPort(srv, 10*time.Second)
	if portNumber == 0 {
		cancel()
		serveErr := <-done
		s.Close()
		if serveErr != nil {
			fail("serve", serveErr)
		}
		fail("serve", fmt.Errorf("eco: %w: the listener never came up", port.ErrUnavailable))
	}
	if err := httpdoor.WritePortFile(v.portFile, httpdoor.PortInfo{
		PID:   os.Getpid(),
		Port:  portNumber,
		Nonce: nonce,
		Token: token,
	}); err != nil {
		cancel()
		<-done
		s.Close()
		fail("serve", err)
	}
	go waitForParent(v.parentPID, cancel)
	serveErr := <-done
	removeErr := os.Remove(v.portFile)
	s.Close()
	cancel()
	if serveErr != nil {
		fail("serve", serveErr)
	}
	if removeErr != nil {
		fail("serve", fmt.Errorf("eco: %w: the port file did not go away", port.ErrUnavailable))
	}
	os.Exit(0)
}

func waitForPort(srv *httpdoor.Server, limit time.Duration) int {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if bound := srv.Port(); bound > 0 {
			return bound
		}
		time.Sleep(5 * time.Millisecond)
	}
	return 0
}

func probeOverPortFile(path string) {
	info, err := httpdoor.ReadPortFile(path)
	if err != nil {
		fail("probe", fmt.Errorf("eco: %w: %v", port.ErrUnavailable, err))
	}
	target := "http://127.0.0.1:" + strconv.Itoa(info.Port) + "/v1/probe"
	request, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		fail("probe", fmt.Errorf("eco: %w: %v", port.ErrUnavailable, err))
	}
	request.Header.Set("Authorization", "Bearer "+info.Token)
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		fail("probe", fmt.Errorf("eco: %w: %v", port.ErrUnavailable, err))
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		fail("probe", fmt.Errorf("eco: %w: the door answered %d", port.ErrUnavailable, response.StatusCode))
	}
	var payload struct {
		OK    bool   `json:"ok"`
		Nonce string `json:"nonce"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		fail("probe", fmt.Errorf("eco: %w: %v", port.ErrUnavailable, err))
	}
	if payload.Nonce != info.Nonce {
		fail("probe", fmt.Errorf("eco: %w: another process answers on port %d", port.ErrUnavailable, info.Port))
	}
	printJSON(map[string]any{"status": "ok", "pid": info.PID, "port": info.Port, "nonce": payload.Nonce})
}