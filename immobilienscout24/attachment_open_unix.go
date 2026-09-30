//go:build unix

package immobilienscout24

import (
	"os"
	"syscall"
)

// openNonBlocking opens a file for reading with O_NONBLOCK, so that the open
// of a FIFO without a writer returns at once instead of waiting for one. The
// flag does not change how a regular file reads.
func openNonBlocking(name string) (*os.File, error) {
	return os.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
