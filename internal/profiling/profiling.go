package profiling

import (
	"sync/atomic"
	"time"
)

var profilingEnabled uint32

func IsEnabled() bool {
	return atomic.LoadUint32(&profilingEnabled) > 0
}

func SetEnabled(enabled bool) {
	if enabled {
		atomic.StoreUint32(&profilingEnabled, 1)
	} else {
		atomic.StoreUint32(&profilingEnabled, 0)
	}
}

type Timer struct {
	TimerTag string
	Begin time.Time
	End time.Time
}

func newTimer(timerTag string) (*Timer) {
	return &Timer{TimerTag: timerTag}
}

func (t *Timer) Ingress() {
	t.Begin = time.Now()
}

func (t *Timer) Egress() {
	t.End = time.Now()
}

type Stat struct {
	StatTag string
	Timers []*Timer
}

func NewStat(statTag string) *Stat {
	return &Stat{StatTag: statTag, Timers: make([]*Timer, 0)}
}

func (stat *Stat) NewTimer(timerTag string) *Timer {
	timer := newTimer(timerTag)
	stat.Timers = append(stat.Timers, timer)
	return timer
}

var MessageStats *CircularBuffer

func InitStats(bufsize uint32) (err error) {
	MessageStats, err = NewCircularBuffer(bufsize)
	if err != nil {
		return
	}

	return
}
