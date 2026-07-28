package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"crm-prospect-simulator/backend/bootstrap"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	application, cfg, err := bootstrap.Build(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err := application.DB.Close(); err != nil {
			log.Printf("close database: %v", err)
		}
	}()

	listenErr := make(chan error, 1)
	go func() {
		listenErr <- application.Fiber.Listen(":" + cfg.Port)
	}()

	signalCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-listenErr:
		return err
	case <-signalCtx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := application.Fiber.ShutdownWithContext(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown server: %w", err)
		}
		if err := <-listenErr; err != nil {
			return fmt.Errorf("listen: %w", err)
		}
		return nil
	}
}
