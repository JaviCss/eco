package httpdoor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type PortInfo struct {
	PID   int    `json:"pid"`
	Port  int    `json:"port"`
	Nonce string `json:"nonce"`
	Token string `json:"token"`
}

func WritePortFile(path string, info PortInfo) error {
	if path == "" {
		return fmt.Errorf("httpdoor: an empty port file path is not a path")
	}
	if info.Port <= 0 || info.Port > 65535 {
		return fmt.Errorf("httpdoor: port %d is out of range (1..65535)", info.Port)
	}
	if info.PID <= 0 {
		return fmt.Errorf("httpdoor: pid %d is out of range", info.PID)
	}
	raw, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("httpdoor: %w", err)
	}
	dir := filepath.Dir(path)
	temporary, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("httpdoor: %w", err)
	}
	temporaryName := temporary.Name()
	cleanup := func() {
		temporary.Close()
		os.Remove(temporaryName)
	}
	if _, err := temporary.Write(raw); err != nil {
		cleanup()
		return fmt.Errorf("httpdoor: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("httpdoor: %w", err)
	}
	if err := temporary.Close(); err != nil {
		os.Remove(temporaryName)
		return fmt.Errorf("httpdoor: %w", err)
	}
	if err := restrictFileToOwner(temporaryName); err != nil {
		os.Remove(temporaryName)
		return err
	}
	if err := os.Rename(temporaryName, path); err != nil {
		os.Remove(temporaryName)
		return fmt.Errorf("httpdoor: %w", err)
	}
	return nil
}

func ReadPortFile(path string) (PortInfo, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return PortInfo{}, fmt.Errorf("httpdoor: %w", err)
	}
	var info PortInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return PortInfo{}, fmt.Errorf("httpdoor: %q is not a port file: %w", filepath.Base(path), err)
	}
	if info.PID <= 0 || info.Port <= 0 || info.Nonce == "" || info.Token == "" {
		return PortInfo{}, fmt.Errorf("httpdoor: %q is an incomplete port file", filepath.Base(path))
	}
	return info, nil
}
