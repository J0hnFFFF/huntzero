package licensing

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/denisbrodbeck/machineid"
)

// MachineFingerprint returns a stable, anonymized identifier for the current
// machine. It combines the OS machine ID with the hostname so that simple
// machine-id resets are not enough to bypass license binding.
func MachineFingerprint() (string, error) {
	id, err := machineid.ID()
	if err != nil {
		// Fall back to a synthetic fingerprint based on hostname and OS.
		id = ""
	}
	host, _ := os.Hostname()
	seed := strings.Join([]string{runtime.GOOS, runtime.GOARCH, id, host}, "|")
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:16]), nil
}

// MustMachineFingerprint returns the fingerprint or panics.
func MustMachineFingerprint() string {
	fp, err := MachineFingerprint()
	if err != nil {
		panic(fmt.Sprintf("machine fingerprint: %v", err))
	}
	return fp
}
