package discovery

import (
	"sync"
	"testing"
	"time"

	"github.com/yyyar/gobetween/config"
	"github.com/yyyar/gobetween/core"
)

func TestStopCompletedDiscovery(t *testing.T) {
	d := NewStaticDiscovery(staticConfig("127.0.0.1:80")).(*Discovery)
	d.Start()
	select {
	case <-d.Discover():
	case <-time.After(2 * time.Second):
		t.Fatal("discovery did not publish")
	}
	// All callers must return, even after the one-shot worker has exited.
	var callers sync.WaitGroup
	for i := 0; i < 5; i++ {
		callers.Add(1)
		go func() {
			defer callers.Done()
			d.Stop()
		}()
	}
	stopped := make(chan struct{})
	go func() {
		callers.Wait()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("stopping completed discovery blocked")
	}
}

func TestDiscoveryStopWaitsForFetch(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	exited := make(chan struct{})
	d := &Discovery{
		cfg: config.DiscoveryConfig{Interval: "1s"},
		fetch: func(config.DiscoveryConfig) (*[]core.Backend, error) {
			close(entered)
			<-release
			close(exited)
			return &[]core.Backend{}, nil
		},
	}
	d.Start()
	<-entered
	stopped := make(chan struct{})
	go func() {
		d.Stop()
		close(stopped)
	}()
	// The cancellation request must not be mistaken for completed cleanup.
	select {
	case <-d.stop:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("Stop did not request cancellation")
	}
	select {
	case <-stopped:
		close(release)
		t.Fatal("Stop returned while fetch was still running")
	default:
	}
	close(release)
	select {
	case <-stopped:
		select {
		case <-exited:
		default:
			t.Fatal("Stop returned before fetch finished")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not wait for fetch completion")
	}
}
