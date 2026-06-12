//go:build unix

package alerting

import (
	"fmt"
	"time"

	"golang.org/x/sys/unix"
)

// processCPUTime returns the total CPU time (user + system) consumed by the
// current process.
func processCPUTime() (time.Duration, error) {
	var usage unix.Rusage
	if err := unix.Getrusage(unix.RUSAGE_SELF, &usage); err != nil {
		return 0, fmt.Errorf("failed to read process rusage: %w", err)
	}

	return time.Duration(usage.Utime.Nano() + usage.Stime.Nano()), nil
}
