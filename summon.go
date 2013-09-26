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
		Name string        `goptions:"-n, --name, obligatory, description='system name'"`
		Help goptions.Help `goptions:"-h, --help, description='show this help'"`

		goptions.Verbs
		Create struct {
			FSType     string `goptions:"-f, --fs, obligatory, description='file system'"`
			Disk       string `goptions:"-d, --disk, obligatory, description='target disk'"`
			User       string `goptions:"-u, --user, description='user to set password for'"`
			EnableSwap bool   `goptions:"--enable-swap, description='enable encrypted swap'"`
			EnableOSX  bool   `goptions:"--enable-osx, description='create OS X partitions'"`
			KeepGPT    bool   `goptions:"--keep-gpt, description='keep the existing GPT'"`
		} `goptions:"create"`
		Backup struct {
			goptions.Remainder
		} `goptions:"backup"`
		Exec struct {
			goptions.Remainder
		} `goptions:"exec"`
		NSpawn struct {
			goptions.Remainder
		} `goptions:"nspawn"`
	}{}
	goptions.ParseAndFail(&options)

	sys := system.New(options.Name)
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
		sys.Root.FSType = system.FSType(options.Create.FSType)
		if options.Create.EnableSwap {
			sys.EnableSwap()
		}
		sys.Root.Password = termios.PasswordConfirm(
			fmt.Sprintf("%s disk password: ", sys.Name),
			fmt.Sprintf("confirm %s disk password: ", sys.Name),
		)
		userpass := termios.PasswordConfirm(
			fmt.Sprintf("%s user password: ", sys.Name),
			fmt.Sprintf("confirm %s user password: ", sys.Name),
		)

		if !options.Create.KeepGPT {
			steps = append(steps, Step{Do: sys.GptSetup})
		}

		steps = append(
			steps,
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
			Step{Do: sys.GenEtcHostname},
			Step{Do: sys.GenRefind},
			Step{Do: sys.PostInstall},
			Step{Do: sys.Passwd("root", userpass)},
			Step{Do: sys.Root.Snapshot("as-installed")},
		)
		if options.Create.User != "" {
			steps = append(steps, Step{Do: sys.Passwd(options.Create.User, userpass)})
		}
	case "exec":
		steps = exec(sys, Step{Do: sys.Exec(options.Exec.Remainder)})
	case "backup":
		steps = exec(
			sys,
			Step{Do: sys.Backup(options.Backup.Remainder)},
			Step{Do: sys.Root.Snapshot("backup")},
		)
	case "nspawn":
		args := []string{"systemd-nspawn", "--directory", sys.Root.Dir}
		if len(options.NSpawn.Remainder) == 0 {
			args = append(args, "/usr/bin/bash", "--login")
		} else {
			args = append(args, options.NSpawn.Remainder...)
		}
		steps = exec(sys, Step{Do: sys.Exec(args)})
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
