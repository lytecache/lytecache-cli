package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestProgramStartDelegatesToStartFunc(t *testing.T) {
	called := false
	p := &Program{StartFunc: func() error { called = true; return nil }}
	if err := p.Start(nil); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("Start did not call StartFunc")
	}
}

func TestProgramStartPropagatesError(t *testing.T) {
	wantErr := errors.New("boom")
	p := &Program{StartFunc: func() error { return wantErr }}
	if err := p.Start(nil); !errors.Is(err, wantErr) {
		t.Errorf("Start() = %v, want %v", err, wantErr)
	}
}

func TestProgramStopPassesABoundedContext(t *testing.T) {
	var gotDeadline time.Time
	var hadDeadline bool
	p := &Program{
		ShutdownTimeout: 50 * time.Millisecond,
		StopFunc: func(ctx context.Context) error {
			gotDeadline, hadDeadline = ctx.Deadline()
			return nil
		},
	}
	if err := p.Stop(nil); err != nil {
		t.Fatal(err)
	}
	if !hadDeadline {
		t.Fatal("expected StopFunc's context to carry a deadline")
	}
	if time.Until(gotDeadline) > 50*time.Millisecond {
		t.Errorf("deadline %v is further out than the configured 50ms ShutdownTimeout", gotDeadline)
	}
}

func TestProgramStopDefaultsShutdownTimeout(t *testing.T) {
	var hadDeadline bool
	p := &Program{StopFunc: func(ctx context.Context) error {
		_, hadDeadline = ctx.Deadline()
		return nil
	}}
	if err := p.Stop(nil); err != nil {
		t.Fatal(err)
	}
	if !hadDeadline {
		t.Error("expected a default deadline when ShutdownTimeout is unset")
	}
}
