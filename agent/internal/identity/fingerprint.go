// Package identity builds the host fingerprint that binds an agent token to
// the machine it was enrolled on. It is a signal, not a replacement for the
// token: an attacker who can read the agent config already has the token.
package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"
)

// Compute returns SHA-256(salt | cpu model | primary MAC | GOOS). The
// install_salt makes cloned templates differ from one another.
func Compute(salt, cpuModel, mac, goos string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{salt, cpuModel, mac, goos}, "|")))
	return hex.EncodeToString(sum[:])
}

func Fingerprint(salt string) (string, error) {
	if strings.TrimSpace(salt) == "" {
		return "", fmt.Errorf("install_salt is empty")
	}
	cpu, err := cpuModel()
	if err != nil {
		return "", err
	}
	return Compute(salt, cpu, primaryMAC(), runtime.GOOS), nil
}

func cpuModel() (string, error) {
	b, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return "", fmt.Errorf("read /proc/cpuinfo: %w", err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "model name", "Hardware", "Processor":
			if model := strings.TrimSpace(value); model != "" {
				return model, nil
			}
		}
	}
	return "unknown", nil
}

func primaryMAC() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "none"
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		if len(iface.HardwareAddr) == 0 {
			continue
		}
		return iface.HardwareAddr.String()
	}
	return "none"
}
