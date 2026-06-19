//go:build !linux

package cache

import (
	"os"
	"time"
)

func accessTime(info os.FileInfo) time.Time {
	return info.ModTime()
}
