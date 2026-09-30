//go:build !unix

package immobilienscout24

import "os"

// openNonBlocking opens a file for reading. Outside unix it cannot ask for
// O_NONBLOCK; openFile still refuses anything but a regular file.
func openNonBlocking(name string) (*os.File, error) {
	return os.Open(name)
}
