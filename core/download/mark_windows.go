package download

import (
	"fmt"
	"os"
)

// zoneIdentifier is the mark of the web: the stream that tells the shell a file
// came from the internet zone.
const zoneIdentifier = "[ZoneTransfer]\r\nZoneId=3\r\n"

// Mark writes the Zone.Identifier stream of a received file, so the shell warns
// before it runs the file.
func Mark(path string) error {
	if err := os.WriteFile(path+":Zone.Identifier", []byte(zoneIdentifier), 0o600); err != nil {
		return fmt.Errorf("download: write zone identifier: %w", err)
	}
	return nil
}
