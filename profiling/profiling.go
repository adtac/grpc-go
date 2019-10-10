package profiling

import (
	"time"
)

type connectionProfiling struct {
	resolveEnter time.Time
	resolveExit  time.Time
}
