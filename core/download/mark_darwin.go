package download

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

// quarantineAttribute is the extended attribute Gatekeeper checks before it
// opens a file.
const quarantineAttribute = "com.apple.quarantine"

// Mark sets the quarantine attribute on a received file, so the system asks
// the user before it runs the file or an app inside it.
func Mark(path string) error {
	value := fmt.Sprintf("0081;%08x;Unison;%s", time.Now().Unix(), strings.ToUpper(uuid.NewString()))
	if err := unix.Setxattr(path, quarantineAttribute, []byte(value), 0); err != nil {
		return fmt.Errorf("download: set quarantine attribute: %w", err)
	}
	return nil
}
