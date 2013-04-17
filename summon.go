package main

import (
	"bitbucket.org/taruti/termios"
	"fmt"
	"github.com/daaku/summon/system"
	"github.com/voxelbrain/goptions"
	"os"
	"os/signal"
	"syscall"
)

type Step struct {
	Do    func(kill chan bool) error
	Defer func(kill chan bool) error
}

func (s Step) LoggedDefer(kill chan bool) {
	if s.Defer == nil {
		return
	}
	if err := s.Defer(kill); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}

func main() {
	options := struct {
		Name   string        `goptions:"-n, --name, obligatory, description='system name'"`
		FSType string        `goptions:"-f, --fs, obligatory, description='file system'"`
		Help   goptions.Help `goptions:"-h, --help, description='show this help'"`

		goptions.Verbs
		Create struct {
			Disk        string `goptions:"-d, --disk, obligatory, description='target disk'"`
			User        string `goptions:"-u, --user, description='user to set password for'"`
			SwapKeyFile string `goptions:"--swap-key-file, description='swap key file (swap disabled by default)'"`
			EnableOSX   bool   `goptions:"--enable-osx, description='create OS X partitions'"`
		} `goptions:"create"`
		Exec struct {
			goptions.Remainder
		} `goptions:"exec"`
	}{}
	goptions.ParseAndFail(&options)

	sys := system.New(options.Name)
	sys.Root.FSType = system.FSType(options.FSType)
	var steps []Step

	switch options.Verbs {
	case "":
		fmt.Fprintln(os.Stderr, "a verb must be specified")
		goptions.PrintHelp()
		os.Exit(2)
	default:
		fmt.Fprintf(os.Stderr, "invalid verb: %v\n", options.Verbs)
		goptions.PrintHelp()
		os.Exit(2)
	case "create":
		sys.EnableOSX = options.Create.EnableOSX
		sys.Disk = options.Create.Disk
		if options.Create.SwapKeyFile != "" {
			sys.EnableSwap(options.Create.SwapKeyFile)
		}
		sys.Root.Password = termios.PasswordConfirm(
			fmt.Sprintf("%s disk password: ", sys.Name),
			fmt.Sprintf("confirm %s disk password: ", sys.Name),
		)
		userpass := termios.PasswordConfirm(
			fmt.Sprintf("%s user password: ", sys.Name),
			fmt.Sprintf("confirm %s user password: ", sys.Name),
		)
		steps = append(
			steps,
			Step{Do: sys.GptSetup},
			Step{Do: sys.Root.LuksFormat},
			Step{Do: sys.Root.LuksOpen, Defer: sys.Root.LuksClose},
			Step{Do: sys.Root.MakeFS},
			Step{Do: sys.Root.Mount, Defer: sys.Root.Umount},
			Step{Do: sys.Swap.LuksFormat},
			Step{Do: sys.Swap.LuksOpen, Defer: sys.Swap.LuksClose},
			Step{Do: sys.Swap.MakeFS},
			Step{Do: sys.EFI.MakeFS},
			Step{Do: sys.EFI.Mount, Defer: sys.EFI.Umount},
			Step{Do: sys.InstallFileSystem},
			Step{Do: sys.VirtualFS.Mount, Defer: sys.VirtualFS.Umount},
			Step{Do: sys.InstallSystem},
			Step{Do: sys.PostInstall},
			Step{Do: sys.Passwd("root", userpass)},
			Step{Do: sys.Root.Snapshot("as-installed")},
		)
		if options.Create.User != "" {
			steps = append(steps, Step{Do: sys.Passwd(options.Create.User, userpass)})
		}
	case "exec":
		steps = append(
			steps,
			exec(sys, Step{Do: sys.Exec(options.Exec.Remainder)})...,
		)
		break
	}

	if err := run(steps); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(3)
	}
}

func exec(sys *system.Config, steps ...Step) []Step {
	sys.Root.Password = termios.Password(
		fmt.Sprintf("%s disk password: ", sys.Name),
	)
	r := []Step{
		Step{Do: sys.Root.LuksOpen, Defer: sys.Root.LuksClose},
		Step{Do: sys.Root.Mount, Defer: sys.Root.Umount},
		Step{Do: sys.EFI.Mount, Defer: sys.EFI.Umount},
	}
	return append(r, steps...)
}

func run(steps []Step) error {
	ec := make(chan error)
	kill := make(chan bool)
	deferKill := make(chan bool)

	go func() {
		ec <- func() error {
			for _, step := range steps {
				if err := step.Do(kill); err != nil {
					return err
				}
				defer step.LoggedDefer(deferKill)
			}
			return nil
		}()
	}()

	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGINT)
	select {
	case <-sig:
		close(kill)
		return <-ec
	case err := <-ec:
		return err
	}
	panic("not reached")
}
