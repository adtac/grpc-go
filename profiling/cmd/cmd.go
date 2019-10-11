package main

import (
	"time"
	"io"
	"context"
	"fmt"
	"flag"
	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/profiling/metrics"
	"google.golang.org/grpc/profiling/proto"
	pspb "google.golang.org/grpc/profiling/proto/service"
	// ptpb "google.golang.org/grpc/profiling/proto/service"
)

var address = flag.String("address", "", "address of your remote target")
var timeout = flag.Int("timeout", 0, "network operations timeout in seconds to remote target (0 indicates unlimited)")
var storeSnapshotFile = flag.String("store-snapshot", "", "connect to remote target and store a profiling snapshot locally for offline processing")
var enable = flag.Bool("enable", false, "enable profiling in remote target")
var disable = flag.Bool("disable", false, "disable profiling in remote target")
var loadSnapshotFile = flag.String("load-snapshot", "", "load a local profiling snapshot for offline processing")

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
	MessageStats []*metrics.Stat
}

func storeSnapshot(ctx context.Context, c pspb.ProfilingClient, f string) {
	grpclog.Infof("making RPC call to retrieve message stats from remote target")
	stream, err := c.GetMessageStats(ctx, &pspb.GetMessageStatsRequest{})
	if err != nil {
		grpclog.Errorf("error calling SetEnabled: %v\n", err)
		return
	}

	s := &snapshot{MessageStats: make([]*metrics.Stat, 0)}

	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			grpclog.Infof("recvd EOF, last")
			return
		}
		if err != nil {
			grpclog.Errorf("error recv: %v", err)
			return
		}

		grpclog.Infof("%v", resp)
		s.MessageStats = append(s.MessageStats, proto.StatProtoToStat(resp))
	}
}

func loadSnapshot(f string) {
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
