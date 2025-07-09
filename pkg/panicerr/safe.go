package panicerr

import (
	"context"
	"github.com/sourcegraph/conc/panics"
)

// Safe wraps a function that returns an error, catching any panics and returning them as an error.
func Safe(fn func() error) func() error {
	return func() error {
		var (
			catcher panics.Catcher
			err     error
		)
		catcher.Try(func() {
			err = fn()
		})
		if err != nil {
			return err
		}
		return catcher.Recovered().AsError()
	}
}

// SafeContext wraps a function that takes a context and returns an error.
func SafeContext(fn func(context.Context) error) func(context.Context) error {
	return func(ctx context.Context) error {
		var (
			catcher panics.Catcher
			err     error
		)
		catcher.Try(func() {
			err = fn(ctx)
		})
		if err != nil {
			return err
		}
		return catcher.Recovered().AsError()
	}
}
