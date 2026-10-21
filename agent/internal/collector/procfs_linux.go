//go:build linux

package collector

import (
	"fmt"
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)

// systemSource reads procfs and statfs on Linux.
type systemSource struct{}

func (systemSource) readFile(name string) ([]byte, error) {
	return os.ReadFile(name)
}

func (systemSource) statfs(path string) (totalBytes, availBytes uint64, err error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, 0, fmt.Errorf("statfs %s: %w", path, err)
	}
	bs := uint64(st.Bsize)
	return st.Blocks * bs, st.Bavail * bs, nil
}

func (systemSource) countProcesses() (int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(e.Name()); err == nil {
			n++
		}
	}
	return n, nil
}
