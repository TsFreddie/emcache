//go:build linux

package cache

import (
	"os"
	"syscall"
	"time"
)

func accessTime(info os.FileInfo) time.Time {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return info.ModTime()
	}
	return time.Unix(stat.Atim.Sec, stat.Atim.Nsec)
}
