package service

import (
	"context"
	"time"

	kservice "github.com/kardianos/service"
)

// DefaultShutdownTimeout bounds how long Program.Stop's context stays
// valid, mirroring the foreground `lytecache ui` path's own graceful
// shutdown budget (see cmd_ui.go's runUI).
const DefaultShutdownTimeout = 10 * time.Second

// Program adapts a generic start/stop pair to kardianos/service.Interface,
// deliberately generic (no dependency on package ui or package main,
// which this package -- as a real subpackage -- cannot import anyway):
// cmd_service.go supplies StartFunc/StopFunc as closures over the actual
// UI server construction in cmd_ui.go, so the server-startup code path
// stays the single one described in the plan -- this type only adapts its
// calling convention to what kardianos/service expects.
//
// StartFunc must not block (kardianos/service requires Start to return
// quickly) -- it should launch the real work in a goroutine and return.
type Program struct {
	StartFunc func() error
	StopFunc  func(ctx context.Context) error

	// ShutdownTimeout bounds StopFunc's context. Zero means
	// DefaultShutdownTimeout.
	ShutdownTimeout time.Duration
}

// Start implements kservice.Interface. It must not block, per
// kardianos/service's contract, so it just delegates to StartFunc.
func (p *Program) Start(_ kservice.Service) error {
	return p.StartFunc()
}

// Stop implements kservice.Interface, calling StopFunc with a
// context bounded by ShutdownTimeout (or DefaultShutdownTimeout).
func (p *Program) Stop(_ kservice.Service) error {
	timeout := p.ShutdownTimeout
	if timeout <= 0 {
		timeout = DefaultShutdownTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return p.StopFunc(ctx)
}
