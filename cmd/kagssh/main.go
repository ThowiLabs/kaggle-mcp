//go:build linux

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ThowiLabs/kaggle-mcp/internal/config"
	"github.com/ThowiLabs/kaggle-mcp/internal/mcp"
)

var version = "dev"

func main() {
	if len(os.Args) >= 2 && (os.Args[1] == "-sync-credentials" || os.Args[1] == "-git-credential") {
		mode := "sync"
		if os.Args[1] == "-git-credential" {
			mode = "git-credential"
		}
		if err := mcp.RunCredentialCommand(mode); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	check := flag.Bool("check", false, "validar configuración y terminar")
	showVersion := flag.Bool("version", false, "mostrar versión")
	flag.Parse()
	if *showVersion {
		fmt.Println("kagmcp", version)
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	cfg, err := config.FromEnv(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "configuración:", err)
		os.Exit(2)
	}
	if *check {
		fmt.Println("configuración válida para Kaggle MCP")
		return
	}
	if err := config.SaveSettings(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "guardar preferencias Kaggle MCP:", err)
		os.Exit(1)
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if os.Geteuid() == 0 {
		log.Warn("Kaggle MCP corre como root: las herramientas autorizadas tendrán privilegios del proceso")
	}
	results := make(chan error, 1)
	go func() { results <- mcp.Run(ctx, cfg, log) }()
	failed := false
	completed := false
	select {
	case err = <-results:
		completed = true
		if err != nil && !errors.Is(err, context.Canceled) {
			failed = true
			log.Error("servicio Kaggle MCP terminó con error", "error", err)
		}
	case <-ctx.Done():
	}
	stop()
	if !completed {
		deadline := time.NewTimer(12 * time.Second)
		defer deadline.Stop()
		select {
		case <-results:
		case <-deadline.C:
			log.Warn("tiempo de cierre agotado; terminando proceso")
		}
	}
	log.Info("Kaggle MCP detenido")
	if failed {
		os.Exit(1)
	}
}
