//go:build linux

package watchers

import "syscall"

func init() {
	// Override statfs with real implementation on Linux
	statfsImpl = statfsLinux
}

var statfsImpl func(string, *StatFS) error

func statfsLinux(path string, stat *StatFS) error {
	var s syscall.Statfs_t
	if err := syscall.Statfs(path, &s); err != nil {
		return err
	}
	stat.Bsize = int64(s.Bsize) // Bsize is int32 on ARMv7, int64 on arm64/amd64
	stat.Blocks = s.Blocks
	stat.Bfree = s.Bfree
	return nil
}
