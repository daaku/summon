package summon

// "system" layer
// "config" layer + manifest
// "data" layer + ignores + backups
// "home" mount
// "pkgrepo" mount

// asgard
// - no encrypted disks
// - no unlocked home
// boe
// - luks encrypted root
// - bootable live backup
// marvin
// - encrypted home

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/daaku/errgroup"
	"github.com/kballard/go-shellquote"
)

func Shellf(format string, a ...any) (string, []string, error) {
	c := fmt.Sprintf(format, a...)
	parts, err := shellquote.Split(c)
	if err != nil {
		return "", nil, err
	}
	return parts[0], parts[1:], nil
}

func MustCmdf(ctx context.Context, format string, a ...any) *exec.Cmd {
	name, args, err := Shellf(format, a...)
	if err != nil {
		panic(err)
	}
	return exec.CommandContext(ctx, name, args...)
}

func VerboseRun(cmd *exec.Cmd) error {
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("error running command: %q: %v\n%s", cmd, err, out)
	}
	return nil
}

func Runf(ctx context.Context, format string, a ...any) error {
	name, args, err := Shellf(format, a...)
	if err != nil {
		return err
	}
	return VerboseRun(exec.CommandContext(ctx, name, args...))
}

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
