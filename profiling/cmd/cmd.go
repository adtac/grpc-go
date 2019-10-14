package main

import (
	"sort"
	"strings"
	"time"
	"io"
	"context"
	"fmt"
	"flag"
	"os"
	"encoding/gob"
	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal/profiling"
	"google.golang.org/grpc/profiling/proto"
	pspb "google.golang.org/grpc/profiling/proto/service"
	ptpb "google.golang.org/grpc/profiling/proto/tags"
)

var address = flag.String("address", "", "address of your remote target")
var timeout = flag.Int("timeout", 0, "network operations timeout in seconds to remote target (0 indicates unlimited)")

var storeSnapshotFile = flag.String("store-snapshot", "", "connect to remote target and store a profiling snapshot locally for offline processing")

var enable = flag.Bool("enable", false, "enable profiling in remote target")
var disable = flag.Bool("disable", false, "disable profiling in remote target")

var loadSnapshotFile = flag.String("load-snapshot", "", "load a local profiling snapshot for offline processing")
var listAll = flag.Bool("list-all", false, "list profiles of all kinds raw")
var listMessages = flag.Bool("list-messages", false, "list message profiles raw")
var showPercent = flag.Bool("show-percent", false, "show percent of overall for timer components")
var delimiter = flag.String("delimiter", "\t", "delimiter to use between timer components")

func parseArgs() error {
	flag.Parse()

	if *address == "" && *loadSnapshotFile == "" {
		return fmt.Errorf("you must provide either -address or -load-snapshot")
	}

	if *address != "" && *loadSnapshotFile != "" {
		return fmt.Errorf("you may not provide both -address and -load-snapshot")
	}

	if *enable && *disable {
		return fmt.Errorf("you may not -enable and -disable profiling in a remote target at the same time")
	}

	if *address == "" {
		if *enable || *disable || *storeSnapshotFile != "" {
			return fmt.Errorf("cannot do that with a local snapshot file, need a remote target")
		}
	} else {
		if !*enable && !*disable && *storeSnapshotFile == "" {
			return fmt.Errorf("what should I do after connecting to the remote target?")
		}
	}

	return nil
}

func setEnabled(ctx context.Context, c pspb.ProfilingClient, enabled bool) {
	resp, err := c.SetEnabled(ctx, &pspb.SetEnabledRequest{Enabled: enabled})
	if err != nil {
		grpclog.Printf("error calling SetEnabled: %v\n", err)
		return
	}

	if resp.Success {
		grpclog.Printf("successfully set enabled = %v", enabled)
	}
}

type snapshot struct {
	MessageStats []*profiling.Stat
}

func storeSnapshot(ctx context.Context, c pspb.ProfilingClient, f string) {
	grpclog.Infof("creating %s", f)
	file, err := os.Create(f)
	if err != nil {
		grpclog.Errorf("cannot create %s: %v", f, err)
		return
	}

	grpclog.Infof("making RPC call to retrieve message stats from remote target")
	stream, err := c.GetMessageStats(ctx, &pspb.GetMessageStatsRequest{})
	if err != nil {
		grpclog.Errorf("error calling GetMessageStats: %v\n", err)
		return
	}

	s := &snapshot{MessageStats: make([]*profiling.Stat, 0)}

	for {
		resp, err := stream.Recv()

		if err == io.EOF {
			grpclog.Infof("recvd EOF, last")
			break
		}

		if err != nil {
			grpclog.Errorf("error recv: %v", err)
			return
		}

		stat := proto.StatProtoToStat(resp)
		s.MessageStats = append(s.MessageStats, stat)
	}

	grpclog.Infof("writing to %s", f)
	encoder := gob.NewEncoder(file)
	encoder.Encode(s)

	file.Close()
	grpclog.Infof("successfully wrote profiling snapshot to %s", f)
}

func getTimerNano(timer *profiling.Timer) int64 {
	return int64(timer.End.Sub(timer.Begin))
}

func prettifyTimerTag(prefix string, timer *profiling.Timer) string {
	return strings.ReplaceAll(ptpb.TimerTag_name[int32(timer.TimerTag)], prefix, "")
}

func prettifyStatTag(prefix string, stat *profiling.Stat) string {
	return strings.ToLower(strings.ReplaceAll(ptpb.StatTag_name[int32(stat.StatTag)], prefix, ""))
}

func listMessageStat(stat *profiling.Stat) {
	overallNano := getTimerNano(stat.Timers[0])

	fmt.Printf("%v%s", prettifyStatTag("", stat), *delimiter)
	fmt.Printf("@%d.%09d%s", stat.Timers[0].Begin.Unix(), stat.Timers[0].Begin.Nanosecond(), *delimiter)
	fmt.Printf("O=%d%s", overallNano, *delimiter)

	var others int64 = 0
	for i := 1; i < len(stat.Timers); i++ {
		nano := getTimerNano(stat.Timers[i])
		others += nano
		fmt.Printf("%s=%d", prettifyTimerTag("MESSAGE_", stat.Timers[i])[:1], nano)
		if *showPercent {
			fmt.Printf("(%d%%)", (100*nano)/overallNano)
		}
		fmt.Printf("%s", *delimiter)
	}
	if *showPercent {
		fmt.Printf("U=%d(%d%%)%s", overallNano - others, (100*(overallNano - others)) / overallNano, *delimiter)
	}
	fmt.Printf("\n")
}

func listAllMessages(stats []*profiling.Stat) {
	fmt.Printf("legend: O=overall, U=unaccounted, H=headers, T=transport, C=compression, E=encoding\n")
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].Timers[0].Begin.Before(stats[j].Timers[0].Begin)
	})
	for _, stat := range stats {
		listMessageStat(stat)
	}
}

func process(s *snapshot) {
	if *listAll {
		listAllMessages(s.MessageStats)
	} else if *listMessages {
		listAllMessages(s.MessageStats)
	} else {
		fmt.Printf("no action specified\n")
	}
}

func loadSnapshot(f string) {
	grpclog.Infof("loading %s", f)
	file, err := os.Open(f)
	if err != nil {
		grpclog.Errorf("cannot open %s: %v", f, err)
		return
	}

	s := &snapshot{}

	decoder := gob.NewDecoder(file)
	if err = decoder.Decode(s); err != nil {
		grpclog.Errorf("cannot decode %s: %v", f, err)
		return
	}

	process(s)
}

func main() {
	if err := parseArgs(); err != nil {
		grpclog.Errorf("error parsing flags: %v\n", err)
		return
	}

	if *address != "" {
		ctx := context.Background()
		var cancel context.CancelFunc
		if *timeout > 0 {
			ctx, cancel = context.WithTimeout(context.Background(), time.Duration(*timeout) * time.Second)
			defer cancel()
		}

		grpclog.Infof("dialing %s", *address)
		cc, err := grpc.Dial(*address, grpc.WithInsecure())
		if err != nil {
			grpclog.Printf("dial error: %v", err)
			return
		}
		defer cc.Close()

		c := pspb.NewProfilingClient(cc)

		if *enable {
			setEnabled(ctx, c, true)
			return
		}

		if *disable {
			setEnabled(ctx, c, false)
			return
		}

		if *storeSnapshotFile != "" {
			storeSnapshot(ctx, c, *storeSnapshotFile)
		}
	} else {
		loadSnapshot(*loadSnapshotFile)
	}
}
