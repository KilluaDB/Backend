package service

import (
	"context"
	"testing"
	"time"

	"go.uber.org/goleak"
)

func TestOperatorProvisioner_PeriodicMetricsCollector_GoroutineLeak(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	p := newTestProvisioner(t)

	ctx, cancel := context.WithCancel(context.Background())
	
	// Start the collector in a goroutine
	go p.PeriodicMetricsCollector(ctx, 10*time.Millisecond)
	
	// Let it run for a short duration
	time.Sleep(50 * time.Millisecond)
	
	// Cancel the context to stop the collector
	cancel()

	// Wait a moment for goroutine to exit
	time.Sleep(10 * time.Millisecond)
	
	// goleak.VerifyNone at the end of the test will fail if the goroutine leaked
}
