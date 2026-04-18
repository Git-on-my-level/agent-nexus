package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"agent-nexus-tools-anx-http-record/internal/proxy"
	"agent-nexus-tools-anx-http-record/internal/recorder"
)

func main() {
	cfg, err := parseFlags()
	if err != nil {
		log.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Dir(cfg.outputPath), 0o755); err != nil {
		log.Fatalf("create output directory: %v", err)
	}
	outputFile, err := os.Create(cfg.outputPath)
	if err != nil {
		log.Fatalf("open output file: %v", err)
	}
	defer outputFile.Close()

	jsonl := recorder.NewJSONLWriter(outputFile)
	handler, err := proxy.NewHandler(proxy.Options{
		Upstream:      cfg.upstream,
		Recorder:      jsonl,
		MaxBodyBytes:  cfg.maxBodyBytes,
		MutationsOnly: cfg.mutationsOnly,
		Logger:        log.Default(),
	})
	if err != nil {
		log.Fatal(err)
	}

	server := &http.Server{
		Addr:              cfg.listenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("anx-http-record listening on http://%s and forwarding to %s", cfg.listenAddr, cfg.upstream)
	log.Printf("writing JSONL recording to %s", cfg.outputPath)

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Printf("received %s, shutting down", sig)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Fatalf("shutdown server: %v", err)
		}
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}
}

type config struct {
	listenAddr    string
	outputPath    string
	upstream      *url.URL
	maxBodyBytes  int64
	mutationsOnly bool
}

func parseFlags() (config, error) {
	var cfg config
	var upstreamRaw string

	flag.StringVar(&cfg.listenAddr, "listen", "127.0.0.1:8010", "listen address for the recording proxy")
	flag.StringVar(&upstreamRaw, "upstream", "", "required upstream base URL, for example http://127.0.0.1:8000")
	flag.StringVar(&cfg.outputPath, "output", "", "required JSONL output path")
	flag.Int64Var(&cfg.maxBodyBytes, "max-body-bytes", 1<<20, "max bytes of request or response body to retain per exchange")
	flag.BoolVar(&cfg.mutationsOnly, "mutations-only", false, "record only POST, PUT, PATCH, and DELETE exchanges")
	flag.Parse()

	if upstreamRaw == "" {
		return cfg, fmt.Errorf("--upstream is required")
	}
	if cfg.outputPath == "" {
		return cfg, fmt.Errorf("--output is required")
	}
	if cfg.maxBodyBytes < 0 {
		return cfg, fmt.Errorf("--max-body-bytes must be >= 0")
	}

	upstream, err := url.Parse(upstreamRaw)
	if err != nil {
		return cfg, fmt.Errorf("parse --upstream: %w", err)
	}
	if upstream.Scheme != "http" && upstream.Scheme != "https" {
		return cfg, fmt.Errorf("--upstream must use http or https")
	}
	if upstream.Host == "" {
		return cfg, fmt.Errorf("--upstream host is required")
	}
	cfg.upstream = upstream
	return cfg, nil
}
