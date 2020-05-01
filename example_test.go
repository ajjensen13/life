package life_test

import (
	"context"
	"fmt"
	"github.com/ajjensen13/life"
	"io/ioutil"
	"log"
	"time"
)

// Demonstrates how to queue tasks to execute during various phases of the
// application's lifecycle.
func ExampleStart() {
	// Queue some initialization work
	life.OnInit(func(ctx context.Context) error {
		fmt.Println("setup complete")

		// Once the initialization work has completed, queue the cleanup work
		defer life.OnDefer(func(ctx context.Context) error {
			fmt.Println("shutdown complete")
			return nil
		})

		// Queue the work to execute during the ready phase when the initialization succeeds
		life.OnReady(func(ctx context.Context) error {
			time.Sleep(time.Second)
			fmt.Println("ready complete")
			return nil
		})
		return nil
	})

	err := life.Start(context.Background(), log.New(ioutil.Discard, log.Prefix(), log.Flags()))
	if err != nil {
		panic(err)
	}
	fmt.Println("program complete")

	// Output:
	// setup complete
	// ready complete
	// shutdown complete
	// program complete
}
