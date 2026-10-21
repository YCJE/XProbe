//go:build !linux

package collector

// systemSource stub for non-Linux builds: the agent binary compiles everywhere
// (CI, lint) but collection is Linux-only in v1 (design doc §4.1).
type systemSource struct{}

func (systemSource) readFile(string) ([]byte, error) {
	return nil, errUnsupported
}

func (systemSource) statfs(string) (uint64, uint64, error) {
	return 0, 0, errUnsupported
}

func (systemSource) countProcesses() (int, error) {
	return 0, errUnsupported
}
