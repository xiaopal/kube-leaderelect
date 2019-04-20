// +build linux

package subreaper

import (
	"context"
	"log"
	"math"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
)

const maxPauseCount = math.MaxInt32 / 2

var (
	logger           = log.New(os.Stderr, "[children-reaper] ", log.Flags())
	pauseCount int32 = maxPauseCount
	pauseChan        = make(chan int32, 0)
)

//Pause func
func Pause() {
	if c := atomic.AddInt32(&pauseCount, 1); c == 1 {
		if pauseChan != nil {
			pauseChan <- 1
		}
	}
}

//Resume func
func Resume() {
	if c := atomic.AddInt32(&pauseCount, -1); c <= 0 {
		if pauseChan != nil {
			pauseChan <- 0
		}
		for c < 0 && !atomic.CompareAndSwapInt32(&pauseCount, c, 0) {
			c = atomic.LoadInt32(&pauseCount)
		}
	}
}

//IsPaused func
func IsPaused() bool {
	return atomic.LoadInt32(&pauseCount) > 0
}

//Start func
func Start(ctx context.Context) {
	childChan := make(chan os.Signal, 10)
	leapChildren := func() {
		var wstatus syscall.WaitStatus
		for {
			if pid, _ := syscall.Wait4(-1, &wstatus, syscall.WNOHANG, nil); pid > 0 {
				logger.Printf("reap child pid %d, exitted %d", pid, wstatus.ExitStatus())
				continue
			}
			return
		}
	}
	atomic.StoreInt32(&pauseCount, 0)
	go func() {
		signal.Notify(childChan, syscall.SIGCHLD)
		defer func() {
			atomic.StoreInt32(&pauseCount, maxPauseCount)
			signal.Stop(childChan)
			close(childChan)
			close(pauseChan)
		}()
		for {
			select {
			case <-pauseChan:
				if !IsPaused() {
					leapChildren()
				}
			case <-childChan:
				if !IsPaused() {
					leapChildren()
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}
