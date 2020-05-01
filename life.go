// Package life is a simple place to handle application lifecycle.
// It assumes there are three phases in an application.
//
// 1.) The Init phase (or the setup phase).
// 2.) The Ready phase
// 3.) The Defer phase (or the teardown phase).
//
// Consumers should use OnInit, OnReady, and OnDefer to queue tasks
// for each phase. Once a phase is started, it is locked, and no more
// tasks can be added to it.
//
// Each phase is handled slightly differently. The Init and Defer phases
// kick off tasks synchronously; the Ready phase kicks off tasks asynchronously.
// The Init and Ready phase will abort upon the first failure they encounter;
// the Defer phase will attempt to execute all tasks, even if there are failures.
// The Init and Defer phase timeout after 30 seconds; the Ready phase does not have
// a timeout value. The Ready phase will allow tasks 15 seconds to 'clean up' before
// transitioning to the next phase.
package life

import (
	"context"
	"fmt"
	"golang.org/x/sync/errgroup"
	"log"
	"sync"
	"time"
)

// OnInit queues a task for the Init phase.
// OnInit panics if the init phase has already begun.
func OnInit(f func(context.Context) error) {
	pkg.onInit(f)
}

// OnReady queues a task for the Init phase.
// OnReady panics if the init phase has already begun.
func OnReady(f func(context.Context) error) {
	pkg.onReady(f)
}

// OnDefer queues a task for the Init phase.
// OnDefer panics if the init phase has already begun.
func OnDefer(f func(context.Context) error) {
	pkg.onDefer(f)
}

// Start kicks off the queued tasks.
func Start(ctx context.Context, logger *log.Logger) error {
	return pkg.start(ctx, logger)
}

func newLife(timeout time.Duration) life {
	return life{
		initPhase: phase{
			name:          "init",
			runType:       Sync,
			errorHandling: abortOnError,
			startTime:     zeroTime,
			timeout:       timeout,
		},
		readyPhase: phase{
			name:          "ready",
			runType:       Async,
			errorHandling: abortOnError,
			startTime:     zeroTime,
		},
		deferPhase: phase{
			name:          "deferral",
			runType:       Sync,
			errorHandling: continueOnError,
			startTime:     zeroTime,
			timeout:       timeout,
		},
	}
}

const syncTimeout = time.Second * 30

var (
	zeroTime = time.Unix(0, 0)
	pkg      = newLife(syncTimeout)
)

type life struct {
	lock                              sync.Mutex
	initPhase, readyPhase, deferPhase phase
}

func (l *life) onInit(f func(context.Context) error) {
	l.initPhase.lock.Lock()
	defer l.initPhase.lock.Unlock()
	l.initPhase.appendTask(f)
}

func (l *life) onReady(f func(context.Context) error) {
	l.readyPhase.lock.Lock()
	defer l.readyPhase.lock.Unlock()
	l.readyPhase.appendTask(f)
}

func (l *life) onDefer(f func(context.Context) error) {
	l.deferPhase.lock.Lock()
	defer l.deferPhase.lock.Unlock()
	l.deferPhase.appendTask(f)
}

func (l *life) start(ctx context.Context, logger *log.Logger) error {
	l.lock.Lock()
	defer l.lock.Unlock()

	return runPhases(ctx, logger, []*phase{&l.initPhase, &l.readyPhase, &l.deferPhase})
}

func runPhases(ctx context.Context, logger *log.Logger, phases []*phase) error {
	var result error
	for _, phase := range phases {
		if result != nil {
			if phase.errorHandling == abortOnError {
				logger.Printf("life: skipping %s phase due to prior errors", phase.name)
				continue
			}
		}

		err := phase.run(ctx, logger)
		if err != nil {
			if result == nil {
				result = fmt.Errorf("life: error occurred during %s phase: %w", phase.name, err)
				continue
			}
			logger.Printf("life: error suppress due to previous failure: %v", err)
		}
	}

	return result
}

type syncType int

const (
	Sync syncType = iota
	Async
)

type errorHandling int

const (
	abortOnError errorHandling = iota
	continueOnError
)

type phase struct {
	lock          sync.Mutex
	name          string
	startTime     time.Time
	tasks         []func(ctx context.Context) error
	runType       syncType
	errorHandling errorHandling
	timeout       time.Duration
}

func (phase *phase) panicIfStarted() {
	if phase.startTime != zeroTime {
		panic(fmt.Sprintf("life: %s phase has already started", phase.name))
	}
}

func (phase *phase) appendTask(f func(context.Context) error) {
	phase.panicIfStarted()
	phase.tasks = append(phase.tasks, f)
}

func (phase *phase) run(ctx context.Context, logger *log.Logger) error {
	phase.lock.Lock()
	defer phase.lock.Unlock()

	phase.panicIfStarted()

	phase.startTime = time.Now()
	defer func() {
		logger.Printf("life: %s phase completed in %v", phase.name, time.Now().Sub(phase.startTime))
	}()

	var cancelFunc context.CancelFunc
	if phase.timeout > 0 {
		logger.Printf("life: starting %s phase with timeout: %v", phase.name, phase.timeout)
		ctx, cancelFunc = context.WithTimeout(ctx, phase.timeout)
	} else {
		logger.Printf("life: starting %s phase without timeout", phase.name)
		ctx, cancelFunc = context.WithCancel(ctx)
	}
	defer cancelFunc()

	return phase.runTasks(ctx)
}

func (phase *phase) runTasks(ctx context.Context) error {
	switch t := phase.runType; t {
	case Async:
		return phase.runAsyncTasks(ctx)
	case Sync:
		return phase.runSyncTasks(ctx)
	default:
		panic(fmt.Errorf("life: unexpected syncType: %v", t))
	}
}

func (phase *phase) runSyncTasks(ctx context.Context) error {
	fr := make(chan error)

	go func() {
		defer close(fr)

		for _, f := range phase.tasks {
			err := f(ctx)
			if err != nil {
				fr <- err
				return
			}
		}
	}()

	select {
	case <-ctx.Done():
		return fmt.Errorf("life: abandoning sync tasks in %s phase: %w", phase.name, ctx.Err())
	case err := <-fr:
		return err
	}
}

const asyncCleanupTimeout = time.Second * 15

func (phase *phase) runAsyncTasks(ctx context.Context) error {
	waitErr := make(chan error)

	// This function will return after all tasks have completed successfully, or one has returned an error.
	// If one task returned an error, others will have a canceled context, but may still be running. Once
	// waitErr is closed, then all tasks have completed.
	func(parent context.Context, waitErr chan<- error) {
		g, ctx := errgroup.WithContext(parent)
		for ndx := range phase.tasks {
			g.Go(func() error {
				return phase.tasks[ndx](ctx)
			})
		}

		go func() {
			defer close(waitErr)
			waitErr <- g.Wait()
		}()

		select {
		case <-ctx.Done():
		case <-parent.Done():
		}
	}(ctx, waitErr)

	return func(parent context.Context) error {
		ctx, cancelFunc := context.WithTimeout(parent, asyncCleanupTimeout)
		defer cancelFunc()

		select {
		case <-ctx.Done():
			return fmt.Errorf("life: abandoning async tasks in %s phase: %w", phase.name, context.DeadlineExceeded)
		case err := <-waitErr:
			return err
		}
	}(ctx)
}
