package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/danmo-ai/danmo-novel/core/serve"
)

func main() {
	core, err := serve.New(serve.Options{})
	if err != nil {
		log.Fatal(err)
	}
	if err := core.Start(); err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- core.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			log.Fatal(err)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := core.Stop(shutdownCtx); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}
}
