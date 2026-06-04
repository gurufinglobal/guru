package oracle

import (
	"context"

	"golang.org/x/sync/errgroup"
)

type Runnable interface {
	Run(ctx context.Context) error
}

func Start(ctx context.Context, runnables ...Runnable) error {
	group, runCtx := errgroup.WithContext(ctx)
	for _, runnable := range runnables {
		provider := runnable
		group.Go(func() error {
			return provider.Run(runCtx)
		})
	}

	return group.Wait()
}
