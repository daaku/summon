package main

import (
	"bitbucket.org/taruti/termios"
	"fmt"
	"github.com/daaku/summon/system"
	"github.com/voxelbrain/goptions"
	"os"
)

type Step struct {
	Do    func() error
	Defer func() error
}

func main() {
	options := struct {
		Name       string        `goptions:"-n, --name, obligatory, description='system name'"`
		FileSystem string        `goptions:"-f, --fs, obligatory, description='file system'"`
		Help       goptions.Help `goptions:"-h, --help, description='show this help'"`

		goptions.Verbs
		Create struct {
			Disk        string `goptions:"-d, --disk, obligatory, description='target disk'"`
			SwapKeyFile string `goptions:"--swap-key-file, description='swap key file'"`
			EnableOSX   bool   `goptions:"--enable-osx, description='enable os x'"`
		} `goptions:"create"`
		Backup struct {
			User string `goptions:"-u, --user, obligatory, description='user to backup'"`
		} `goptions:"backup"`
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
		if options.Create.SwapKeyFile != "" {
			sys.EnableSwap(options.Create.SwapKeyFile)
		}
		sys.Root.Password = termios.PasswordConfirm(
			fmt.Sprintf("%s disk password: ", sys.Name),
			fmt.Sprintf("confirm %s disk password: ", sys.Name),
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
			Step{Do: sys.VirtualFS.Mount, Defer: sys.VirtualFS.Umount},
			Step{Do: sys.InstallSystem},
		)
	case "backup":
		sys.Root.Password = termios.Password(
			fmt.Sprintf("%s disk password: ", sys.Name),
		)
		steps = append(
			steps,
			Step{Do: sys.Root.LuksOpen, Defer: sys.Root.LuksClose},
			Step{Do: sys.Root.Mount, Defer: sys.Root.Umount},
			Step{Do: sys.EFI.Mount, Defer: sys.EFI.Umount},
		)
		break
	}

	if err := run(steps); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(3)
	}
}

func run(steps []Step) error {
	for _, step := range steps {
		if err := step.Do(); err != nil {
			return err
		}
		if step.Defer != nil {
			defer step.Defer()
		}
	}
	return nil
}
