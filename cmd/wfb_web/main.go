// wfb_web - Web-based video player and stats dashboard for WFB
//
// Usage:
//
//	wfb_web --http :8080 --video :5600
//
// Then open http://localhost:8080 in a browser (Safari recommended for HEVC).
//
// Note: This standalone tool only receives video via UDP. For full stats,
// use the embedded web server in wfb_server.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/lian/wfb-go/pkg/server/web"
)

func main() {
	httpAddr := flag.String("http", ":8080", "HTTP server address")
	videoAddr := flag.String("video", ":5600", "UDP address to receive video")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	server := web.NewServer(web.Config{
		Addr:        *httpAddr,
		VideoSource: *videoAddr,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("Shutting down...")
		cancel()
	}()

	log.Printf("Starting WFB Web UI on %s (video from %s)", *httpAddr, *videoAddr)
	log.Printf("Open http://localhost%s in your browser (Safari recommended for HEVC)", *httpAddr)

	if err := server.Run(ctx); err != nil && err != context.Canceled {
		log.Fatal(err)
	}
}
