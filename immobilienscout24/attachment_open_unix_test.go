//go:build unix

package immobilienscout24

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A FIFO in place of an attachment's file makes fileSHA256 and readFile fail
// at once, where a blocking open of a FIFO without a writer would hang the
// plan or the apply.
func TestAttachmentFileRefusesAFIFOWithoutBlocking(t *testing.T) {
	name := filepath.Join(t.TempDir(), "living-room.jpg")
	if err := syscall.Mkfifo(name, 0o600); err != nil {
		t.Fatal(err)
	}
	for fn, read := range map[string]func(string) error{
		"fileSHA256": func(n string) error { _, err := fileSHA256(n); return err },
		"readFile":   func(n string) error { _, err := readFile(n); return err },
	} {
		done := make(chan error, 1)
		start := time.Now()
		go func() { done <- read(name) }()
		select {
		case err := <-done:
			if err == nil || !strings.Contains(err.Error(), "is not a regular file") {
				t.Errorf("%s of a FIFO: %v, want an error that it is not a regular file", fn, err)
			}
			t.Logf("%s of a FIFO returned in %v: %v", fn, time.Since(start), err)
		case <-time.After(5 * time.Second):
			// A writer lets the blocked open return, so that the goroutine ends.
			if w, err := os.OpenFile(name, os.O_WRONLY, 0); err == nil {
				_ = w.Close()
			}
			<-done
			t.Errorf("%s of a FIFO blocked for 5 seconds", fn)
		}
	}
}
