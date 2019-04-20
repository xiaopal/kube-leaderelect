// +build !linux

package subreaper

import (
	"context"
)

//Pause func
func Pause() {
	return
}

//Resume func
func Resume() {
	return
}

//IsPaused func
func IsPaused() bool {
	return false
}

//Start func
func Start(ctx context.Context) {
	return
}
