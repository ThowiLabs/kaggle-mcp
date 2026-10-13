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

	"github.com/ThowiLabs/kagssh-go/internal/config"
	"github.com/ThowiLabs/kagssh-go/internal/mcp"
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
		fmt.Println("configuración válida para KagMCP")
		return
	}
	if err := config.SaveSettings(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "guardar preferencias KagMCP:", err)
		os.Exit(1)
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if os.Geteuid() == 0 {
		log.Warn("KagMCP corre como root: las herramientas autorizadas tendrán privilegios del proceso")
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
			log.Error("servicio KagMCP terminó con error", "error", err)
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
	log.Info("KagMCP detenido")
	if failed {
		os.Exit(1)
	}
}
