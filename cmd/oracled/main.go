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
	delay := 5 * time.Second
	rootCtx := context.Background()

	for {
		ctx, cancel := context.WithCancel(rootCtx)
		dmn := daemon.New(ctx)
		if dmn == nil {
			time.Sleep(delay)
			continue
		}

		select {
		case <-c:
			cancel()
			fmt.Println("Thank you oracle daemon!!")
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
