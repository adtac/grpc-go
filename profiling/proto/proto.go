package proto

import (
	"time"
	"google.golang.org/grpc/internal/profiling"
	pspb "google.golang.org/grpc/profiling/proto/service"
)

func timerToTimerProto(timer *profiling.Timer) *pspb.TimerProto {
	return &pspb.TimerProto{
		TimerTag: timer.TimerTag,
		BeginSec: timer.Begin.Unix(),
		BeginNsec: int32(timer.Begin.Nanosecond()),
		EndSec: timer.End.Unix(),
		EndNsec: int32(timer.End.Nanosecond()),
	}
}

func StatToStatProto(stat *profiling.Stat) *pspb.StatProto {
	statProto := &pspb.StatProto{StatTag: stat.StatTag, TimerProtos: make([]*pspb.TimerProto, 0)}
	for _, t := range stat.Timers {
		statProto.TimerProtos = append(statProto.TimerProtos, timerToTimerProto(t))
	}

	return statProto
}

func timerProtoToTimer(timerProto *pspb.TimerProto) *profiling.Timer {
	return &profiling.Timer{
		TimerTag: timerProto.TimerTag,
		Begin: time.Unix(timerProto.BeginSec, int64(timerProto.BeginNsec)),
		End: time.Unix(timerProto.EndSec, int64(timerProto.EndNsec)),
	}
}

func StatProtoToStat(statProto *pspb.StatProto) *profiling.Stat {
	s := &profiling.Stat{StatTag: statProto.StatTag, Timers: make([]*profiling.Timer, 0)}
	for _, timerProto := range statProto.TimerProtos {
		s.Timers = append(s.Timers, timerProtoToTimer(timerProto))
	}

	return s
}
