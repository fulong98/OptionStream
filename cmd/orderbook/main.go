package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/fulong98/OptionStream/internal/config"
	"github.com/fulong98/OptionStream/internal/deribit"
	"github.com/fulong98/OptionStream/internal/kafka"
	"github.com/spf13/cobra"
)

func main() {
	var cmd = &cobra.Command{
		Use:   "orderbook",
		Short: "Collect BTC/USD options orderbook data from Deribit and stream to Kafka",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load configuration
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			// Create context with cancellation
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Initialize Kafka producer
			producer, err := kafka.NewProducer(cfg.Kafka)
			if err != nil {
				return err
			}
			defer producer.Close()

			// Initialize Deribit client
			client := deribit.NewClient(cfg.Deribit, cfg.Kafka)

			// Handle graceful shutdown
			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

			go func() {
				<-sigChan
				cancel()
			}()

			// Start collecting orderbook data
			return client.CollectOrderbook(ctx, producer)
		},
	}

	if err := cmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
