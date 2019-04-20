package appctx

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

//Interface interface
type Interface interface {
	Context() context.Context
	EndContext()
	End()
	WaitGroup() *sync.WaitGroup
}

type appctx struct {
	ctx    context.Context
	endCtx func()
	wg     *sync.WaitGroup
}

func (a *appctx) Context() context.Context {
	return a.ctx
}

func (a *appctx) EndContext() {
	a.endCtx()
}

func (a *appctx) End() {
	defer a.wg.Wait()
	a.EndContext()
}

func (a *appctx) WaitGroup() *sync.WaitGroup {
	return a.wg
}

//Start func
func Start() Interface {
	logger := log.New(os.Stderr, "[appctx] ", log.Flags())
	interruptChan := make(chan os.Signal, 2)
	signal.Notify(interruptChan, syscall.SIGINT, syscall.SIGTERM)
	ctx, endCtx := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}
	go func() {
		wg.Add(1)
		defer wg.Done()
		select {
		case sig := <-interruptChan:
			logger.Printf("signal %v", sig)
			endCtx()
		case <-ctx.Done():
		}
		signal.Stop(interruptChan)
	}()
	return &appctx{ctx, endCtx, wg}
}
