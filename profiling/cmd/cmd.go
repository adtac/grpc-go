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
var messageFilter = flag.String("message-filter", "", "filter for message stats of this type")

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

func getTimerTimestamp(t time.Time) string {
	return fmt.Sprintf("%d.%09d", t.Unix(), t.Nanosecond())
}

type hierNode struct {
	name string
	level int
	timers []*profiling.Timer
	childTimers []*profiling.Timer
	subtrees []*hierNode
	index map[string]int
	parent *hierNode
}

func newHierNode(name string, level int, parent *hierNode) *hierNode {
	return &hierNode{
		name: name,
		level: level,
		timers: make([]*profiling.Timer, 0),
		childTimers: make([]*profiling.Timer, 0),
		subtrees: make([]*hierNode, 0),
		index: make(map[string]int),
		parent: parent,
	}
}

func recursiveMessageStatList(cur *hierNode) {
	if cur.level >= 0 {
		for i := 0; i < cur.level; i++ {
			fmt.Printf("  ")
		}

		fmt.Printf("%s", cur.name)
		written := 2*cur.level + len(cur.name)

		if len(cur.timers) > 0 {
			for i := 0; i < 32 - written; i++ {
				fmt.Printf(" ")
			}
			var nano, childNano int64
			for _, timer := range cur.childTimers {
				childNano += getTimerNano(timer)
			}
			for _, timer := range cur.timers {
				nano += getTimerNano(timer)
			}
			fmt.Printf("%d\t%d", nano, childNano)
			if *showPercent {
				fmt.Printf("\t~ %d%%", (100*childNano) / nano)
			}
			fmt.Printf("\t @")
			for i, timer := range cur.timers {
				fmt.Printf("%s-%s", getTimerTimestamp(timer.Begin), getTimerTimestamp(timer.End))
				if i < len(cur.timers) - 1 {
					fmt.Printf(",")
				}
			}
		}
		fmt.Printf("\n")
	}

	for _, node := range cur.subtrees {
		recursiveMessageStatList(node)
	}
}

func listMessageStat(stat *profiling.Stat) {
	if len(stat.Timers) == 0 {
		return
	}

	fmt.Printf("%v\n", stat.StatTag)

	root := newHierNode("", -1, nil)

	for i := 0; i < len(stat.Timers); i++ {
		splitTags := strings.Split(stat.Timers[i].TimerTag, "/")
		cur := root
		for level, splitTag := range splitTags {
			if _, ok := cur.index[splitTag]; !ok {
				cur.index[splitTag] = len(cur.subtrees)
				cur.subtrees = append(cur.subtrees, newHierNode(splitTag, level, cur))
			}

			if level == len(splitTags) - 1 {
				cur.subtrees[cur.index[splitTag]].timers = append(cur.subtrees[cur.index[splitTag]].timers, stat.Timers[i])
			} else {
				cur.subtrees[cur.index[splitTag]].childTimers = append(cur.subtrees[cur.index[splitTag]].childTimers, stat.Timers[i])
			}

			cur = cur.subtrees[cur.index[splitTag]]
		}
	}

	recursiveMessageStatList(root)
	fmt.Printf("\n")
}

func listAllMessages(stats []*profiling.Stat) {
	sort.Slice(stats, func(i, j int) bool {
		if len(stats[j].Timers) == 0 {
			return true
		} else if len(stats[i].Timers) == 0 {
			return false
		}
		return stats[i].Timers[0].Begin.Before(stats[j].Timers[0].Begin)
	})
	for _, stat := range stats {
		if *messageFilter == "" || *messageFilter == stat.StatTag {
			listMessageStat(stat)
		}
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
