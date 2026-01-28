// Package main provides the otlp-sim CLI tool for simulating OpenTelemetry traces and logs.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/arloliu/otx/cmd/otlp-sim/engine"
	"github.com/arloliu/otx/cmd/otlp-sim/scenario"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	mode := os.Args[1]
	switch mode {
	case "quick":
		runQuickMode(os.Args[2:])
	case "run":
		runContinuousMode(os.Args[2:])
	case "list":
		listScenarios()
	case "-h", "--help", "help":
		printUsage()
	default:
		_, _ = fmt.Fprintf(os.Stderr, "Unknown mode: %s\n", mode)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`otlp-sim - OpenTelemetry trace/log simulator

Usage:
  otlp-sim <mode> [flags]

Modes:
  quick   Send traces immediately for quick visualization
  run     Simulate real-world timing continuously
  list    List available scenarios

Quick Mode Flags:
  --endpoint     OTLP endpoint (default: localhost:4317)
  --http         Use HTTP instead of gRPC
  --insecure     Skip TLS verification (default: true)
  --scenario     Scenario name (default: payment)
  --count        Number of traces to send (default: 10)
  --logs         Enable log generation
  --service-name Override service name

Continuous Mode Flags:
  --endpoint     OTLP endpoint (default: localhost:4317)
  --http         Use HTTP instead of gRPC
  --insecure     Skip TLS verification (default: true)
  --scenario     Scenario name (default: payment)
  --duration     Total simulation time (default: 1m)
  --rate         Traces per second (default: 1)
  --jitter       Timing variation percentage (default: 20)
  --logs         Enable log generation
  --service-name Override service name

Environment Variables:
  OTEL_EXPORTER_OTLP_ENDPOINT   OTLP endpoint
  OTEL_EXPORTER_OTLP_PROTOCOL   grpc or http
  OTEL_EXPORTER_OTLP_INSECURE   Skip TLS verification
  OTEL_SERVICE_NAME             Default service name

Examples:
  otlp-sim quick --scenario payment --count 5
  otlp-sim run --scenario edge-iot --duration 5m --rate 10
  otlp-sim list`)
}

func runQuickMode(args []string) {
	cfg := newConfig()
	fs := flag.NewFlagSet("quick", flag.ExitOnError)
	cfg.bindCommonFlags(fs)
	fs.IntVar(&cfg.Count, "count", cfg.Count, "Number of traces to send")

	if err := fs.Parse(args); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error parsing flags: %v\n", err)
		return
	}

	cfg.applyEnvOverrides()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := executeQuick(ctx, cfg); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}
}

func runContinuousMode(args []string) {
	cfg := newConfig()
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	cfg.bindCommonFlags(fs)

	fs.DurationVar(&cfg.Duration, "duration", cfg.Duration, "Total simulation time")
	fs.Float64Var(&cfg.Rate, "rate", cfg.Rate, "Traces per second")
	fs.IntVar(&cfg.Jitter, "jitter", cfg.Jitter, "Timing variation percentage")

	if err := fs.Parse(args); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error parsing flags: %v\n", err)
		return
	}

	cfg.applyEnvOverrides()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := executeContinuous(ctx, cfg); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}
}

func listScenarios() {
	fmt.Println(`Available scenarios:

  payment      Online payment system flow
               - 6 services, 8 spans (gateway → payment → fraud/processor)
               - Mix of gRPC, HTTP, async messaging

  edge-iot     Edge device management
               - MQTT → gRPC → Redis/TimescaleDB flow
               - High volume, low latency patterns

  ecommerce    E-commerce order flow
               - Order creation with inventory/pricing checks
               - Database and event bus spans

  health-check Simple connectivity test
               - Single HTTP request span
               - Useful for verifying OTLP connection`)
}

// executeQuick sends traces immediately.
func executeQuick(ctx context.Context, cfg *Config) error {
	s, err := loadScenario(cfg)
	if err != nil {
		return err
	}

	eng, err := engine.New(ctx, engine.Config{
		Endpoint:    cfg.Endpoint,
		UseHTTP:     cfg.UseHTTP,
		Insecure:    cfg.IsInsecure(),
		ServiceName: cfg.ServiceName,
		EnableLogs:  cfg.EnableLogs,
		JitterPct:   0, // No jitter in quick mode
	})
	if err != nil {
		return fmt.Errorf("failed to create engine: %w", err)
	}

	fmt.Printf("Sending %d traces to %s (scenario: %s)\n", cfg.Count, cfg.Endpoint, s.Name)

	for i := range cfg.Count {
		select {
		case <-ctx.Done():
			fmt.Printf("\nInterrupted after %d traces\n", i)
			return nil
		default:
		}

		if err := eng.GenerateTrace(ctx, s); err != nil {
			return fmt.Errorf("failed to generate trace %d: %w", i+1, err)
		}
		fmt.Printf("Trace %d/%d sent\n", i+1, cfg.Count)
	}

	// Allow time for export
	time.Sleep(500 * time.Millisecond)
	fmt.Println("Done!")

	return nil
}

// executeContinuous runs traces at a steady rate for a duration.
func executeContinuous(ctx context.Context, cfg *Config) error {
	s, err := loadScenario(cfg)
	if err != nil {
		return err
	}

	eng, err := engine.New(ctx, engine.Config{
		Endpoint:    cfg.Endpoint,
		UseHTTP:     cfg.UseHTTP,
		Insecure:    cfg.IsInsecure(),
		ServiceName: cfg.ServiceName,
		EnableLogs:  cfg.EnableLogs,
		JitterPct:   cfg.Jitter,
	})
	if err != nil {
		return fmt.Errorf("failed to create engine: %w", err)
	}

	fmt.Printf("Running %s scenario for %v at %.1f traces/sec\n", s.Name, cfg.Duration, cfg.Rate)

	interval := time.Duration(float64(time.Second) / cfg.Rate)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	deadline := time.Now().Add(cfg.Duration)
	traceCount := 0

	for {
		select {
		case <-ctx.Done():
			fmt.Printf("\nInterrupted after %d traces\n", traceCount)
			return nil
		case <-ticker.C:
			if time.Now().After(deadline) {
				fmt.Printf("\nCompleted: sent %d traces\n", traceCount)
				return nil
			}

			if err := eng.GenerateTrace(ctx, s); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "Warning: failed to generate trace: %v\n", err)
				continue
			}
			traceCount++
		}
	}
}

func loadScenario(cfg *Config) (*scenario.Scenario, error) {
	// Try custom YAML file first
	if cfg.ScenarioFile != "" {
		return scenario.LoadFromFile(cfg.ScenarioFile)
	}

	// Look up embedded scenario
	s, ok := scenario.Get(cfg.Scenario)
	if !ok {
		return nil, fmt.Errorf("unknown scenario: %s (use 'otlp-sim list' to see available scenarios)", cfg.Scenario)
	}

	return s, nil
}
