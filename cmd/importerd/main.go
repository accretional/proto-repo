// Command importerd serves the repo.Importer gRPC service.
package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	pb "github.com/accretional/proto-repo/genpb"
	"github.com/accretional/proto-repo/importer"
	"github.com/accretional/proto-repo/subcommands"
	"google.golang.org/grpc"
)

func main() {
	addr := flag.String("addr", ":7777", "listen address")
	scratchDir := flag.String("scratch-dir", "./scratch", "directory to clone repos into")
	flag.Parse()

	if err := os.MkdirAll(*scratchDir, 0o755); err != nil {
		log.Fatalf("mkdir scratch: %v", err)
	}

	lis, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("listen %s: %v", *addr, err)
	}
	s := grpc.NewServer()
	imp, err := importer.New(*scratchDir)
	if err != nil {
		log.Fatalf("importer.New: %v", err)
	}
	pb.RegisterImporterServer(s, imp)
	sc, err := subcommands.New(*scratchDir)
	if err != nil {
		log.Fatalf("subcommands.New: %v", err)
	}
	pb.RegisterSubCommandsServer(s, sc)

	// Trap SIGINT/SIGTERM and ask the gRPC server to drain — in-flight
	// unary calls and streaming responses finish before Serve returns.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		log.Printf("shutdown signal received, draining...")
		s.GracefulStop()
	}()

	log.Printf("importerd listening on %s (scratch=%s)", *addr, *scratchDir)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
	log.Printf("importerd stopped cleanly")
}
