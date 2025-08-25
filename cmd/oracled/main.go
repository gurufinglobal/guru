package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/GPTx-global/guru-v2/oralce/config"
	"github.com/GPTx-global/guru-v2/oralce/daemon"
)

func main() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	config.Load()
	rootCtx := context.Background()
	delay := 5 * time.Second

	for {
		ctx, cancel := context.WithCancel(rootCtx)
		dmn := daemon.New(ctx)

		if dmn == nil {
			time.Sleep(delay)
			continue
		}

		select {
		case <-c:
			fmt.Println("Thank you oracle daemon!!")
			cancel()
			os.Exit(0)
		case <-dmn.Fatal():
			cancel()
			time.Sleep(delay)
			dmn = nil
			runtime.GC()
			continue
		}
	}
}
