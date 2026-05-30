package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/owl-corp/hafnium/config"
	"github.com/owl-corp/hafnium/pkg/discord"
	"github.com/owl-corp/hafnium/pkg/github"
	"github.com/owl-corp/hafnium/pkg/keycloak"
	"github.com/owl-corp/hafnium/pkg/sync"
)

var (
	v = viper.New()
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "hafnium",
		Short: "Keycloak to GitHub Sync Daemon",
		Long:  "A standalone daemon that synchronizes Keycloak role memberships with GitHub organization and team memberships.",
		Run:   run,
	}

	config.BindFlags(rootCmd.Flags(), v)

	// Environment variable setup
	v.SetEnvPrefix("HAFNIUM")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) {
	log.Println("Starting Hafnium...")

	cfg, err := config.LoadConfig(v)
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	kcClient := keycloak.NewClient(cfg.Keycloak.URL, cfg.Keycloak.Realm, cfg.Keycloak.Username, cfg.Keycloak.Password)
	ghClient := github.NewClient(cfg.Github.Token, cfg.Github.Org)
	dcClient, err := discord.NewClient(cfg.Discord.Token)
	if err != nil {
		log.Fatalf("Failed to initialize Discord client: %v", err)
	}
	defer dcClient.Close()

	engine := sync.NewEngine(cfg, kcClient, ghClient, dcClient)

	// Metrics server
	go func() {
		log.Printf("Starting metrics server on %s", cfg.Metrics.Addr)
		http.Handle("/metrics", promhttp.Handler())
		if err := http.ListenAndServe(cfg.Metrics.Addr, nil); err != nil {
			log.Printf("Metrics server error: %v", err)
		}
	}()

	// Sync loop
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ticker := time.NewTicker(cfg.Sync.Interval)
	defer ticker.Stop()

	// Run initial sync
	runSync(ctx, engine)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case <-ticker.C:
			runSync(ctx, engine)
		case sig := <-sigChan:
			log.Printf("Received signal %v, shutting down...", sig)
			return
		case <-ctx.Done():
			return
		}
	}
}

func runSync(ctx context.Context, engine *sync.Engine) {
	const maxRetries = 5
	for i := 0; i < maxRetries; i++ {
		log.Printf("Starting sync run (attempt %d/%d)...", i+1, maxRetries)
		if err := engine.Sync(ctx); err != nil {
			log.Printf("Sync error on attempt %d: %v", i+1, err)
			if i < maxRetries-1 {
				log.Println("Retrying in 5 seconds...")
				select {
				case <-time.After(5 * time.Second):
					continue
				case <-ctx.Done():
					return
				}
			}
			continue
		}
		log.Println("Sync run complete.")
		return
	}
	log.Println("Sync failed after max retries.")
}
