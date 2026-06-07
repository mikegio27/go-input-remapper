package engine

import (
	"sync"
	"time"

	evdev "github.com/mikegio27/go-evdev"
)

// outputSink is the subset of *evdev.VirtualDevice the scheduler and run loop
// write through. Defining it as an interface lets tests substitute a recorder.
type outputSink interface {
	Write(evdev.InputEvent) error
	Sync() error
}

// scheduler runs macros off the read loop so their delays never backpressure
// input. Each macro runs in its own goroutine; writes to the shared sink (from
// both macro goroutines and the run loop) are serialized by mu so frames never
// interleave. sleep is injectable for deterministic tests.
type scheduler struct {
	out   outputSink
	mu    *sync.Mutex
	sleep func(time.Duration)
	after func(time.Duration) <-chan time.Time // interruptible wait between repeats; injectable for tests
	wg    sync.WaitGroup
}

func newScheduler(out outputSink, mu *sync.Mutex) *scheduler {
	return &scheduler{out: out, mu: mu, sleep: time.Sleep, after: time.After}
}

// fire launches a macro asynchronously and returns immediately.
func (s *scheduler) fire(m *compiledMacro) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.run(m)
	}()
}

// fireRepeat launches a repeating macro asynchronously. It runs the macro, then
// waits interval and runs it again, until stop is closed or it has run maxCount
// times (maxCount <= 0 means unbounded). The wait is interruptible by stop so a
// released trigger or a torn-down device stops it promptly. onDone (if non-nil)
// runs when the goroutine exits, so the caller can drop its bookkeeping for a
// repeat that ended on its own. Returns immediately.
func (s *scheduler) fireRepeat(m *compiledMacro, interval time.Duration, maxCount int, stop <-chan struct{}, onDone func()) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if onDone != nil {
			defer onDone()
		}
		for n := 0; maxCount <= 0 || n < maxCount; n++ {
			select {
			case <-stop:
				return
			default:
			}
			s.run(m)
			select {
			case <-stop:
				return
			case <-s.after(interval):
			}
		}
	}()
}

// run executes a macro's steps in order: pause for each step's delay, then emit
// its events as individual synced frames. It is synchronous, so tests can call
// it directly and assert the emitted sequence.
func (s *scheduler) run(m *compiledMacro) {
	for _, step := range m.steps {
		if step.delay > 0 {
			s.sleep(step.delay)
		}
		if len(step.events) == 0 {
			continue
		}
		s.mu.Lock()
		for _, ev := range step.events {
			if err := s.out.Write(ev); err != nil {
				s.mu.Unlock()
				return // sink gone (device torn down); stop the macro
			}
			if err := s.out.Sync(); err != nil {
				s.mu.Unlock()
				return
			}
		}
		s.mu.Unlock()
	}
}

// wait blocks until all in-flight macros finish.
func (s *scheduler) wait() { s.wg.Wait() }
