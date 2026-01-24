// Package grpc provides OpenTelemetry instrumentation for gRPC clients and servers.
//
// # gRPC Server
//
// Use stats handler for gRPC servers:
//
//	server := grpc.NewServer(
//	    grpc.StatsHandler(otxgrpc.ServerHandler()),
//	)
//
// # gRPC Client
//
// Use stats handler for gRPC clients:
//
//	conn, err := grpc.NewClient(target,
//	    grpc.WithStatsHandler(otxgrpc.ClientHandler()),
//	)
package grpc
