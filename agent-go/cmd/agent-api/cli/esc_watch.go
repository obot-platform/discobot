package cli

import "context"

type escWatcher struct {
	done chan struct{}
}

func startEscWatch(ctx context.Context, cancel context.CancelFunc) *escWatcher {
	watcher := &escWatcher{done: make(chan struct{})}
	go func() {
		defer close(watcher.done)
		watchEscDuringTurn(ctx, cancel)
	}()
	return watcher
}

func (w *escWatcher) Wait() {
	if w == nil {
		return
	}
	<-w.done
}
