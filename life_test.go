package life

import (
	"context"
	"errors"
	"io/ioutil"
	"log"
	"testing"
	"time"
)

func TestStart(t *testing.T) {
	logger := log.New(ioutil.Discard, log.Prefix(), log.Flags())

	t.Run("empty life returns without error", func(t *testing.T) {
		ctx, _ := context.WithTimeout(context.Background(), time.Second*5)
		l := newLife(time.Second)
		err := l.start(ctx, logger)
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("onInit is called", func(t *testing.T) {
		ctx, _ := context.WithTimeout(context.Background(), time.Second*5)
		l := newLife(time.Second)

		var calls int
		l.onInit(func(ctx context.Context) error {
			calls++
			return nil
		})

		err := l.start(ctx, logger)
		if err != nil {
			t.Fatal(err)
		}

		if calls != 1 {
			t.Errorf("got %d calls, want %d", calls, 1)
		}
	})

	t.Run("onReady is called", func(t *testing.T) {
		ctx, _ := context.WithTimeout(context.Background(), time.Second*5)
		l := newLife(time.Second)

		var calls int
		l.onReady(func(ctx context.Context) error {
			calls++
			return nil
		})

		err := l.start(ctx, logger)
		if err != nil {
			t.Fatal(err)
		}

		if calls != 1 {
			t.Errorf("got %d calls, want %d", calls, 1)
		}
	})

	t.Run("onDefer is called", func(t *testing.T) {
		ctx, _ := context.WithTimeout(context.Background(), time.Second*5)
		l := newLife(time.Second)

		var calls int
		l.onDefer(func(ctx context.Context) error {
			calls++
			return nil
		})

		err := l.start(ctx, logger)
		if err != nil {
			t.Fatal(err)
		}

		if calls != 1 {
			t.Errorf("got %d calls, want %d", calls, 1)
		}
	})

	t.Run("onReady is skipped on failure", func(t *testing.T) {
		ctx, _ := context.WithTimeout(context.Background(), time.Second*5)
		l := newLife(time.Second)

		l.onInit(func(ctx context.Context) error {
			return errors.New("fake init error")
		})

		var calls int
		l.onReady(func(ctx context.Context) error {
			calls++
			return nil
		})

		err := l.start(ctx, logger)
		if err == nil {
			t.Errorf("expected error, got %v", err)
		}

		if calls != 0 {
			t.Errorf("got %d calls, want %d", calls, 0)
		}
	})

	t.Run("onDefer is called even on failure", func(t *testing.T) {
		ctx, _ := context.WithTimeout(context.Background(), time.Second*5)
		l := newLife(time.Second)

		l.onReady(func(ctx context.Context) error {
			return errors.New("fake ready error")
		})

		var calls int
		l.onDefer(func(ctx context.Context) error {
			calls++
			return nil
		})

		err := l.start(ctx, logger)
		if err == nil {
			t.Errorf("expected error, got %v", err)
		}

		if calls != 1 {
			t.Errorf("got %d calls, want %d", calls, 1)
		}
	})

	t.Run("onDefer failure is swallowed upon previous failure", func(t *testing.T) {
		ctx, _ := context.WithTimeout(context.Background(), time.Second*5)
		l := newLife(time.Second)

		want := errors.New("fake ready error")
		l.onReady(func(ctx context.Context) error {
			return want
		})

		var calls int
		l.onDefer(func(ctx context.Context) error {
			calls++
			return errors.New("fake defer error")
		})

		got := l.start(ctx, logger)
		if !errors.Is(got, want) {
			t.Errorf("want %v, got %v", want, errors.Unwrap(got))
		}

		if calls != 1 {
			t.Errorf("got %d calls, want %d", calls, 1)
		}
	})

	t.Run("sync phases timeout correctly", func(t *testing.T) {
		if testing.Short() {
			t.SkipNow()
		}

		ctx, _ := context.WithTimeout(context.Background(), time.Second*5)
		l := newLife(time.Second)

		l.onInit(func(ctx context.Context) error {
			time.Sleep(time.Second * 10)
			return nil
		})

		err := l.start(ctx, logger)
		if want := context.DeadlineExceeded; !errors.Is(err, want) {
			t.Errorf("expected %v, got %v", want, err)
		}
	})
}
