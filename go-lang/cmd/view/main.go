package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/t2-f/zabbix-cloning/internal/config"
	viewcmd "github.com/t2-f/zabbix-cloning/internal/view"
)

func main() { os.Exit(run()) }
func run() int {
	cfg, err := config.Parse(os.Args[1:], "view")
	if errors.Is(err, config.ErrHelp) {
		fmt.Print(config.ViewHelp())
		return 0
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprint(os.Stderr, config.ViewHelp())
		return 2
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := viewcmd.Run(ctx, cfg, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
