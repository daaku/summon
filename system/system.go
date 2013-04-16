package system

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"time"

	"github.com/daaku/go.cmderr"
)

var errNoDiskSpecified = errors.New("no disk specified")

// File System type.
type FSType string

const (
	Ext4  = FSType("ext4")
	Btrfs = FSType("btrfs")
	Vfat  = FSType("vfat")

	tsFormat    = "2006-01-02_15-04"
	btrfsActive = "__active"
)

// Defines a luks encrypted disk.
type EncryptedDisk struct {
	Name     string
	FSType   FSType
	Device   string
	Password string
	Mapper   string
	Dir      string
}

// Initializes the LUKS device.
func (d *EncryptedDisk) LuksFormat(kill chan bool) error {
	cmd := exec.Command(
		"cryptsetup", "luksFormat",
		"--cipher", "aes-xts-plain64",
		"--key-size", "512",
		"--hash", "sha512",
		"--iter-time", "5000",
		"--use-random",
		d.Device,
	)
	cmd.Stdin = bytes.NewBufferString(d.Password)
	if err := run(cmd, kill); err != nil {
		return err
	}
	return nil
}

// Opens the LUKS device.
func (d *EncryptedDisk) LuksOpen(kill chan bool) error {
	cmd := exec.Command("cryptsetup", "open", "--type", "luks", d.Device, d.Name)
	cmd.Stdin = bytes.NewBufferString(d.Password)
	if err := run(cmd, kill); err != nil {
		return err
	}
	return nil
}

// Closes the existing LUKS mapping.
func (d *EncryptedDisk) LuksClose(kill chan bool) error {
	cmd := exec.Command("cryptsetup", "close", d.Name)
	if err := run(cmd, kill); err != nil {
		return err
	}
	return nil
}

// Create the File System.
func (d *EncryptedDisk) MakeFS(kill chan bool) error {
	var bin string
	if d.FSType == Btrfs {
		bin = "mkfs.btrfs"
	}
	if d.FSType == Ext4 {
		bin = "mkfs.ext4"
	}
	if bin == "" {
		return fmt.Errorf("unknown filesystem type: %s", string(d.FSType))
	}

	cmd := exec.Command(
		bin,
		"-L", fmt.Sprintf("%s-root", d.Name),
		d.Mapper,
	)
	if err := run(cmd, kill); err != nil {
		return err
	}

	// for btrfs we ensure creation of an active subvolume
	if d.FSType == Btrfs {
		dir, err := mountBtrfsRoot(d.Mapper, kill)
		if err != nil {
			return err
		}
		defer umountBtrfsRoot(dir, kill)

		activedir := path.Join(dir, btrfsActive)
		scmd := exec.Command("btrfs", "subvolume", "create", activedir)
		if err := run(scmd, kill); err != nil {
			return err
		}
		return nil
	}

	return nil
}

// Mount the File System.
func (d *EncryptedDisk) Mount(kill chan bool) error {
	err := os.MkdirAll(d.Dir, os.FileMode(755))
	if err != nil {
		return err
	}

	options := "noatime"
	if d.FSType == Btrfs {
		options = fmt.Sprintf("%s,compress=lzo,subvol=%s", options, btrfsActive)
	}
	cmd := exec.Command(
		"mount",
		"-t", string(d.FSType),
		"-o", options,
		d.Mapper,
		d.Dir,
	)
	if err := run(cmd, kill); err != nil {
		return err
	}
	return nil
}

// Unmount the File System.
func (d *EncryptedDisk) Umount(kill chan bool) error {
	cmd := exec.Command("umount", d.Dir)
	if err := run(cmd, kill); err != nil {
		return err
	}

	if err := os.Remove(d.Dir); err != nil {
		return err
	}
	return nil
}

// Create a snapshot, if the target File System supports this.
func (d *EncryptedDisk) Snapshot(name string, kill chan bool) error {
	if d.FSType != Btrfs {
		return nil
	}

	dir, err := mountBtrfsRoot(d.Mapper, kill)
	if err != nil {
		return err
	}
	defer umountBtrfsRoot(dir, kill)

	snapdir := path.Join(dir, "__snapshot")
	if err := os.MkdirAll(snapdir, os.FileMode(755)); err != nil {
		return err
	}

	t := time.Now()
	snapname := fmt.Sprintf("%s-%d-%s", t.Format(tsFormat), t.UnixNano(), name)
	scmd := exec.Command(
		"btrfs", "subvolume", "snapshot",
		"-r",
		path.Join(dir, btrfsActive),
		path.Join(snapdir, snapname),
	)
	if err := run(scmd, kill); err != nil {
		return err
	}
	return nil
}

// EFI disk config.
type EFIDisk struct {
	Name   string
	Device string
	Dir    string
}

// Create the EFI file system.
func (d *EFIDisk) MakeFS(kill chan bool) error {
	cmd := exec.Command(
		"mkfs.vfat",
		"-F32",
		"-n", fmt.Sprintf("%s-efi", d.Name),
		d.Device,
	)
	if err := run(cmd, kill); err != nil {
		return err
	}
	return nil
}

// Mount the EFI disk. Create the target directory if necessary.
func (d *EFIDisk) Mount(kill chan bool) error {
	err := os.MkdirAll(d.Dir, os.FileMode(755))
	if err != nil {
		return err
	}

	cmd := exec.Command("mount", "-t", string(Vfat), d.Device, d.Dir)
	if err := run(cmd, kill); err != nil {
		return err
	}
	return nil
}

// Umount the EFI disk. Does not remove the target directory.
func (d *EFIDisk) Umount(kill chan bool) error {
	cmd := exec.Command("umount", d.Dir)
	if err := run(cmd, kill); err != nil {
		return err
	}
	return nil
}

// Swap disk config.
type SwapDisk struct {
	Name    string
	Device  string
	Mapper  string
	KeyFile string
}

// Initializes the LUKS device.
func (d *SwapDisk) LuksFormat(kill chan bool) error {
	if d == nil {
		return nil
	}
	cmd := exec.Command(
		"cryptsetup", "luksFormat",
		"--cipher", "aes-xts-plain64",
		"--key-size", "512",
		"--hash", "sha512",
		"--iter-time", "5000",
		"--use-random",
		"--key-file", d.KeyFile,
		d.Device,
	)
	if err := run(cmd, kill); err != nil {
		return err
	}
	return nil
}

// Opens the LUKS device.
func (d *SwapDisk) LuksOpen(kill chan bool) error {
	if d == nil {
		return nil
	}
	cmd := exec.Command(
		"cryptsetup", "open",
		"--type", "luks",
		"--key-file", d.KeyFile,
		d.Device,
		d.Name,
	)
	if err := run(cmd, kill); err != nil {
		return err
	}
	return nil
}

// Closes the existing LUKS mapping.
func (d *SwapDisk) LuksClose(kill chan bool) error {
	if d == nil {
		return nil
	}
	cmd := exec.Command("cryptsetup", "close", d.Name)
	if err := run(cmd, kill); err != nil {
		return err
	}
	return nil
}

// Create the Swap file system.
func (d *SwapDisk) MakeFS(kill chan bool) error {
	if d == nil {
		return nil
	}
	label := fmt.Sprintf("%s-efi", d.Name)
	cmd := exec.Command("mkswap", "--label", label, d.Mapper)
	if err := run(cmd, kill); err != nil {
		return err
	}
	return nil
}

// Mount this swap.
func (d *SwapDisk) Mount(kill chan bool) error {
	if d == nil {
		return nil
	}
	cmd := exec.Command("swapon", d.Mapper)
	if err := run(cmd, kill); err != nil {
		return err
	}
	return nil
}

// Umount this Swap.
func (d *SwapDisk) Umount(kill chan bool) error {
	if d == nil {
		return nil
	}
	cmd := exec.Command("swapoff", d.Mapper)
	if err := run(cmd, kill); err != nil {
		return err
	}
	return nil
}

var virtualFSs = []string{"dev", "dev/pts", "sys", "proc"}

// Virtual file systems like dev/proc etc.
type VirtualFS struct {
	Dir string
}

// Mount virtual file systems.
func (f *VirtualFS) Mount(kill chan bool) error {
	for _, p := range virtualFSs {
		cmd := exec.Command(
			"mount", "--bind",
			path.Join("/", p),
			path.Join(f.Dir, p),
		)
		if err := run(cmd, kill); err != nil {
			return err
		}
	}
	return nil
}

// Umount virtual file systems.
func (f *VirtualFS) Umount(kill chan bool) error {
	for i := len(virtualFSs) - 1; i >= 0; i = i - 1 {
		p := virtualFSs[i]
		cmd := exec.Command("umount", path.Join(f.Dir, p))
		if err := run(cmd, kill); err != nil {
			return err
		}
	}
	return nil
}

// Defines a system.
type Config struct {
	Name      string
	Disk      string
	Root      *EncryptedDisk
	EFI       *EFIDisk
	Swap      *SwapDisk
	VirtualFS *VirtualFS
	EnableOSX bool
}

// Create a new config based on standard naming rules.
func New(name string) *Config {
	rootName := fmt.Sprintf("%s-root", name)
	efiName := fmt.Sprintf("%s-efi", name)
	dir := path.Join("/mnt", name)
	return &Config{
		Name: name,
		Root: &EncryptedDisk{
			Name:   rootName,
			Device: path.Join("/dev/disk/by-partlabel", rootName),
			Mapper: path.Join("/dev/mapper", rootName),
			Dir:    dir,
		},
		EFI: &EFIDisk{
			Name:   efiName,
			Device: path.Join("/dev/disk/by-partlabel", efiName),
			Dir:    path.Join("/mnt", name, "boot/efi"),
		},
		VirtualFS: &VirtualFS{
			Dir: dir,
		},
	}
}

// Enable a swap disk.
func (c *Config) EnableSwap(keyFile string) {
	name := fmt.Sprintf("%s-swap", c.Name)
	c.Swap = &SwapDisk{
		Name:    name,
		KeyFile: keyFile,
		Device:  path.Join("/dev/disk/by-partlabel", name),
		Mapper:  path.Join("/dev/mapper", name),
	}
}

// Create GPT for system.
func (c *Config) GptSetup(kill chan bool) error {
	if c.Disk == "" {
		return errNoDiskSpecified
	}

	zcmd := exec.Command("sgdisk", "--zap-all", c.Disk)
	if err := run(zcmd, kill); err != nil {
		return err
	}

	part := 0
	entry := func(size, typecode, name string) []string {
		part = part + 1
		return []string{
			"--new", fmt.Sprintf("%d:0:%s", part, size),
			"--typecode", fmt.Sprintf("%d:%s", part, typecode),
			"--change-name", fmt.Sprintf("%d:%s", part, name),
		}
	}

	var args []string
	efisize := "+64M"
	if c.EnableOSX {
		efisize = "+256M"
	}
	args = append(args, entry(efisize, "ef00", c.EFI.Name)...)
	if c.EnableOSX {
		args = append(args, entry("+30G", "af00", c.label("osx"))...)
		args = append(args, entry("+620M", "ab00", c.label("recovery"))...)
	}
	if c.Swap != nil {
		args = append(args, entry("+4G", "8200", c.Swap.Name)...)
	}
	args = append(args, entry("0", "8300", c.Root.Name)...)
	args = append(args, c.Disk)

	ccmd := exec.Command("sgdisk", args...)
	if err := run(ccmd, kill); err != nil {
		return err
	}

	max := time.Second * 2
	sleep := time.Millisecond * 50
	current := time.Millisecond
	for {
		_, err := os.Stat(c.Root.Device)
		if err == nil {
			break
		}
		if os.IsNotExist(err) {
			time.Sleep(sleep)
		} else {
			return err
		}
		if current > max {
			return fmt.Errorf("failed to find %s", c.Root.Device)
		}
		current = current + sleep
	}

	return nil
}

// Install system.
func (c *Config) InstallFileSystem(kill chan bool) error {
	dirs := []string{"var/lib/pacman", "var/cache/pacman/pkg"}
	for _, d := range dirs {
		full := path.Join(c.Root.Dir, d)
		if err := os.MkdirAll(full, os.FileMode(755)); err != nil {
			return err
		}
	}

	cmd := exec.Command(
		"pacman",
		"--refresh",
		"--root", c.Root.Dir,
		"--asdeps",
		"--noconfirm",
		"--quiet",
		"--sync",
		"filesystem",
	)
	if err := run(cmd, kill); err != nil {
		return err
	}
	return nil
}

// Install system.
func (c *Config) InstallSystem(kill chan bool) error {
	pcmd := exec.Command(
		"pacman",
		"--root", c.Root.Dir,
		"--asdeps",
		"--noconfirm",
		"--quiet",
		"--sync",
		"lib32-mesa-libgl",
		"ttf-dejavu",
		"mesa-libgl",
		"libreoffice-en-US",
	)
	if err := run(pcmd, kill); err != nil {
		return err
	}

	f := "etc/systemd/system/getty.target.wants/getty@tty1.service"
	if err := os.Remove(path.Join(c.Root.Dir, f)); err != nil {
		return err
	}

	rcmd := exec.Command(
		"pacman",
		"--root", c.Root.Dir,
		"--noconfirm",
		"--quiet",
		"--sync",
		fmt.Sprintf("%s-system", c.Name),
	)
	if err := run(rcmd, kill); err != nil {
		return err
	}
	return nil
}

// Post install steps.
func (c *Config) PostInstall(kill chan bool) error {
	cmds := [][]string{
		[]string{c.Root.Dir, "/usr/bin/pacman-key", "--init"},
		[]string{c.Root.Dir, "/usr/bin/pacman-key", "--populate", "archlinux"},
		[]string{c.Root.Dir, "/usr/bin/locale-gen"},
		[]string{c.Root.Dir, "/usr/bin/mandb", "--quiet"},
		[]string{c.Root.Dir, "/usr/bin/mkinitcpio", "-p", "linux"},
	}

	for _, cmd := range cmds {
		if err := run(exec.Command("chroot", cmd...), kill); err != nil {
			return err
		}
	}
	return nil
}

// Setup password.
func (c *Config) Passwd(user, pass string) func(kill chan bool) error {
	return func(kill chan bool) error {
		cmd := exec.Command("chroot", c.Root.Dir, "/usr/bin/passwd", user)
		cmd.Stdin = bytes.NewBufferString(pass + "\n" + pass + "\n")
		if err := run(cmd, kill); err != nil {
			return err
		}
		return nil
	}
}

// Execute a command. Will connect stdin, stdout & stderr thru.
func (c *Config) Exec(args []string) func(kill chan bool) error {
	return func(kill chan bool) error {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			return err
		}

		ec := make(chan error)
		go func() {
			if err := cmd.Wait(); err != nil {
				ec <- err
				return
			}
			ec <- nil
		}()
		select {
		case <-kill:
			e1 := cmd.Process.Kill()
			e2 := <-ec
			if e2 != nil {
				return e2
			}
			return e1
		case err := <-ec:
			return err
		}
		panic("not reached")
	}
}

func (c *Config) label(thing string) string {
	return fmt.Sprintf("%s-%s", c.Name, thing)
}

func mountBtrfsRoot(device string, kill chan bool) (string, error) {
	dir, err := ioutil.TempDir("", path.Base(device))
	if err != nil {
		return "", err
	}

	mcmd := exec.Command(
		"mount",
		"-t", string(Btrfs),
		"-o", "noatime,compress=lzo",
		device,
		dir,
	)
	if err := run(mcmd, kill); err != nil {
		return "", err
	}
	return dir, nil
}

func umountBtrfsRoot(dir string, kill chan bool) error {
	cmd := exec.Command("umount", dir)
	if err := run(cmd, kill); err != nil {
		return err
	}
	if err := os.Remove(dir); err != nil {
		return err
	}
	return nil
}

func run(cmd *exec.Cmd, kill chan bool) error {
	if cmd.Stdout != nil {
		return errors.New("summon: Stdout already set")
	}
	if cmd.Stderr != nil {
		return errors.New("summon: Stderr already set")
	}
	var b bytes.Buffer
	cmd.Stdout = &b
	cmd.Stderr = &b
	if err := cmd.Start(); err != nil {
		return err
	}

	ec := make(chan error)
	go func() {
		if err := cmd.Wait(); err != nil {
			ec <- cmderr.New(cmd, b.Bytes(), err)
			return
		}
		ec <- nil
	}()
	select {
	case <-kill:
		e1 := cmd.Process.Kill()
		e2 := <-ec
		if e2 != nil {
			return e2
		}
		return e1
	case err := <-ec:
		return err
	}
	panic("not reached")
}
