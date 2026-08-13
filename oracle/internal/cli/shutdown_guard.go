package cli

import (
	"sync"
	"time"
)

type shutdownGuard struct {
	timeout     time.Duration
	stopSignals func()
	hardExit    func(int)
	done        chan struct{}

	mu        sync.Mutex
	timer     *time.Timer
	started   bool
	completed bool
	doneOnce  sync.Once
}

func newShutdownGuard(timeout time.Duration, stopSignals func(), hardExit func(int)) *shutdownGuard {
	return &shutdownGuard{
		timeout:     timeout,
		stopSignals: stopSignals,
		hardExit:    hardExit,
		done:        make(chan struct{}),
	}
}

func (g *shutdownGuard) Start() {
	g.mu.Lock()
	if g.started || g.completed {
		g.mu.Unlock()
		return
	}
	g.started = true
	g.timer = time.AfterFunc(g.timeout, func() {
		g.mu.Lock()
		if g.completed {
			g.mu.Unlock()
			return
		}
		g.hardExit(1)
		g.mu.Unlock()
	})
	g.mu.Unlock()
	g.stopSignals()
}

func (g *shutdownGuard) Complete() {
	g.mu.Lock()
	g.completed = true
	if g.timer != nil {
		g.timer.Stop()
	}
	g.mu.Unlock()
	g.doneOnce.Do(func() { close(g.done) })
}

func (g *shutdownGuard) Done() <-chan struct{} {
	return g.done
}
