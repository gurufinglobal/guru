package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/GPTx-global/guru-v2/oralce/config"
	"github.com/GPTx-global/guru-v2/oralce/daemon"
)

func main() {
	config.Load()

	ctx, cancel := context.WithCancel(context.Background())
	daemon := daemon.NewDaemon(ctx)
	daemon.Start()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	<-c
	cancel()
	daemon.Stop()
}
