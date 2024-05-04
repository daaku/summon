package summon

// "system" layer
// "config" layer + manifest
// "data" layer + ignores + backups
// "home" mount
// "pkgrepo" mount

import (
	"context"

	"github.com/daaku/errgroup"
)

type Task struct {
	Name      string
	Do, Defer func(context.Context) error
}

func Parallel(name string, tasks ...Task) Task {
	defers := []func(context.Context) error{}
	return Task{
		Name: name,
		Do: func(ctx context.Context) error {
			var eg errgroup.Group
			for _, t := range tasks {
				if t.Do != nil {
					eg.Add(1)
					go func() {
						defer eg.Done()
						eg.Error(t.Do(ctx))
					}()
				}
				if t.Defer != nil {
					defers = append(defers, t.Defer)
				}
			}
			return eg.Wait()
		},
		Defer: func(ctx context.Context) error {
			var eg errgroup.Group
			eg.Add(len(defers))
			for _, f := range defers {
				go func() {
					defer eg.Done()
					eg.Error(f(ctx))
				}()
			}
			return eg.Wait()
		},
	}
}

func Serial(name string, tasks ...Task) Task {
	defers := []func(context.Context) error{}
	return Task{
		Name: name,
		Do: func(ctx context.Context) error {
			for _, t := range tasks {
				if t.Do != nil {
					if err := t.Do(ctx); err != nil {
						return err
					}
				}
				if t.Defer != nil {
					defers = append(defers, t.Defer)
				}
			}
			return nil
		},
		Defer: func(ctx context.Context) error {
			var multiErrors []error
			for _, f := range defers {
				multiErrors = append(multiErrors, f(ctx))
			}
			return errgroup.NewMultiError(multiErrors...)
		},
	}
}

func Run(ctx context.Context, t Task) error {
	if t.Do != nil {
		if err := t.Do(ctx); err != nil {
			return err
		}
	}
	if t.Defer != nil {
		if err := t.Defer(ctx); err != nil {
			return err
		}
	}
	return nil
}

var CheckInternet = Task{
	Name: "Check Internet",
	Do: func(ctx context.Context) error {
		// TODO
		return nil
	},
}
