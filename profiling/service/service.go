package service

import (
	"context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"

	"google.golang.org/grpc/profiling/metrics"
	ppb "google.golang.org/grpc/profiling/proto"
	pspb "google.golang.org/grpc/profiling/proto/service"
)

type ProfilingConfig struct {
	Enabled bool
	SampleCount uint32
	Server *grpc.Server
}

func registerService(s *grpc.Server) {
	grpclog.Infof("registering profiling service")
	pspb.RegisterProfilingServer(s, &profilingServer{})
}

func Init(pc *ProfilingConfig) (err error) {
	err = metrics.InitStats(pc.SampleCount)
	if err != nil {
		return
	}

	registerService(pc.Server)

	// Do this last after everything has been initialised and allocated.
	metrics.SetEnabled(pc.Enabled)

	return
}

type profilingServer struct {}

func (s *profilingServer) SetEnabled(ctx context.Context, req *pspb.SetEnabledRequest) (ser *pspb.SetEnabledResponse, err error) {
	grpclog.Infof("processing SetEnabled (%v)", req.Enabled)
	metrics.SetEnabled(req.Enabled)

	ser = &pspb.SetEnabledResponse{Success: true}
	err = nil
	return
}

func (s *profilingServer) GetMessageStats(req *pspb.GetMessageStatsRequest, stream pspb.Profiling_GetMessageStatsServer) (err error) {
	grpclog.Infof("processing stream request for message stats")
	results := metrics.MessageStats.Drain()
	grpclog.Infof("message stats size: %v records", len(results))
	for i := 0; i < len(results); i++ {
		if err = stream.Send(ppb.StatToStatProto(results[i].(*metrics.Stat))); err != nil {
			return
		}
	}
	metrics.MessageStats.Drain()

	return
}
