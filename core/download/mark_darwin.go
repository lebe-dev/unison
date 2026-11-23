package download

import (
	"errors"
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
// the user before it runs the file or an app inside it. A volume without
// extended attributes cannot carry the mark, which does not stop the transfer.
func Mark(path string) error {
	value := fmt.Sprintf("0081;%08x;Unison;%s", uint32(time.Now().Unix()), strings.ToUpper(uuid.NewString()))
	err := unix.Setxattr(path, quarantineAttribute, []byte(value), 0)
	if errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EOPNOTSUPP) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("download: set quarantine attribute: %w", err)
	}
	return nil
}
