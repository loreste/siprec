//go:build !unix

package alerting

import (
	"fmt"
	"time"
)

// processCPUTime is unavailable on this platform, so CPU-based alert rules
// report the metric as unavailable instead of evaluating fake data.
func processCPUTime() (time.Duration, error) {
	return 0, fmt.Errorf("process cpu time not supported on this platform")
}
