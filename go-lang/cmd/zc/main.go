package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/tanrakukairo/zabbix-cloning/internal/clone"
	"github.com/tanrakukairo/zabbix-cloning/internal/config"
	"github.com/tanrakukairo/zabbix-cloning/internal/logx"
)

func main() { os.Exit(run()) }
func run() int {
	cfg, err := config.Parse(os.Args[1:], "clone")
	if errors.Is(err, config.ErrHelp) {
		fmt.Print(config.CloneHelp())
		return 0
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprint(os.Stderr, config.CloneHelp())
		return 2
	}
	logger, err := logx.New(cfg.LogName, cfg.LogLevel, cfg.LogFile, cfg.Quiet)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer logger.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := clone.Run(ctx, cfg, logger); err != nil {
		logger.Errorf("[ABORT] %v", err)
		return 1
	}
	return 0
}
