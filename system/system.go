package system

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"time"

	"github.com/daaku/cmderr"
)

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
func (d *EncryptedDisk) LuksFormat() error {
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
	if out, err := cmd.CombinedOutput(); err != nil {
		return cmderr.New(out, err)
	}
	return nil
}

// Opens the LUKS device.
func (d *EncryptedDisk) LuksOpen() error {
	cmd := exec.Command("cryptsetup", "open", "--type", "luks", d.Device, d.Name)
	cmd.Stdin = bytes.NewBufferString(d.Password)
	if out, err := cmd.CombinedOutput(); err != nil {
		return cmderr.New(out, err)
	}
	return nil
}

// Closes the existing LUKS mapping.
func (d *EncryptedDisk) LuksClose() error {
	cmd := exec.Command("cryptsetup", "open", d.Name)
	if out, err := cmd.CombinedOutput(); err != nil {
		return cmderr.New(out, err)
	}
	return nil
}

// Create the File System.
func (d *EncryptedDisk) MakeFS() error {
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
	if out, err := cmd.CombinedOutput(); err != nil {
		return cmderr.New(out, err)
	}

	// for btrfs we ensure creation of an active subvolume
	if d.FSType == Btrfs {
		dir, err := mountBtrfsRoot(d.Mapper)
		if err != nil {
			return err
		}
		defer umountBtrfsRoot(dir)

		activedir := path.Join(dir, btrfsActive)
		scmd := exec.Command("btrfs", "subvolume", "create", activedir)
		if out, err := scmd.CombinedOutput(); err != nil {
			return cmderr.New(out, err)
		}
		return nil
	}

	return nil
}

// Mount the File System.
func (d *EncryptedDisk) Mount() error {
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
	if out, err := cmd.CombinedOutput(); err != nil {
		return cmderr.New(out, err)
	}
	return nil
}

// Unmount the File System.
func (d *EncryptedDisk) Umount() error {
	cmd := exec.Command("umount", d.Dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return cmderr.New(out, err)
	}

	if err := os.Remove(d.Dir); err != nil {
		return err
	}
	return nil
}

// Create a snapshot, if the target File System supports this.
func (d *EncryptedDisk) Snapshot(name string) error {
	if d.FSType != Btrfs {
		return nil
	}

	dir, err := mountBtrfsRoot(d.Mapper)
	if err != nil {
		return err
	}
	defer umountBtrfsRoot(dir)

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
	if out, err := scmd.CombinedOutput(); err != nil {
		return cmderr.New(out, err)
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
func (d *EFIDisk) MakeFS() error {
	cmd := exec.Command(
		"mkfs.vfat",
		"-F32",
		"-n", fmt.Sprintf("%s-efi", d.Name),
		d.Device,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return cmderr.New(out, err)
	}
	return nil
}

// Mount the EFI disk. Create the target directory if necessary.
func (d *EFIDisk) Mount() error {
	err := os.MkdirAll(d.Dir, os.FileMode(755))
	if err != nil {
		return err
	}

	cmd := exec.Command("mount", "-o", string(Vfat), d.Device, d.Dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return cmderr.New(out, err)
	}
	return nil
}

// Umount the EFI disk. Does not remove the target directory.
func (d *EFIDisk) Umount() error {
	cmd := exec.Command("umount", d.Dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return cmderr.New(out, err)
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
func (d *SwapDisk) LuksFormat() error {
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
	if out, err := cmd.CombinedOutput(); err != nil {
		return cmderr.New(out, err)
	}
	return nil
}

// Opens the LUKS device.
func (d *SwapDisk) LuksOpen() error {
	cmd := exec.Command(
		"cryptsetup", "open",
		"--type", "luks",
		"--key-file", d.KeyFile,
		d.Device,
		d.Name,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return cmderr.New(out, err)
	}
	return nil
}

// Closes the existing LUKS mapping.
func (d *SwapDisk) LuksClose() error {
	cmd := exec.Command("cryptsetup", "open", d.Name)
	if out, err := cmd.CombinedOutput(); err != nil {
		return cmderr.New(out, err)
	}
	return nil
}

// Create the Swap file system.
func (d *SwapDisk) MakeFS() error {
	label := fmt.Sprintf("%s-efi", d.Name)
	cmd := exec.Command("mkswap", "--label", label, d.Mapper)
	if out, err := cmd.CombinedOutput(); err != nil {
		return cmderr.New(out, err)
	}
	return nil
}

// Mount this swap.
func (d *SwapDisk) Mount() error {
	if out, err := exec.Command("swapon", d.Mapper).CombinedOutput(); err != nil {
		return cmderr.New(out, err)
	}
	return nil
}

// Umount this Swap.
func (d *SwapDisk) Umount() error {
	cmd := exec.Command("swapoff", d.Mapper)
	if out, err := cmd.CombinedOutput(); err != nil {
		return cmderr.New(out, err)
	}
	return nil
}

var virtualFSs = []string{"dev", "dev/pts", "sys", "proc"}

// Virtual file systems like dev/proc etc.
type VirtualFS struct {
	Dir string
}

// Mount virtual file systems.
func (f *VirtualFS) Mount() error {
	for _, p := range virtualFSs {
		cmd := exec.Command(
			"mount", "-bind",
			path.Join("/", p),
			path.Join(f.Dir, p),
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			return cmderr.New(out, err)
		}
	}
	return nil
}

// Umount virtual file systems.
func (f *VirtualFS) Umount() error {
	for i := len(virtualFSs) - 1; i >= 0; i = i - 1 {
		p := virtualFSs[i]
		cmd := exec.Command("umount", path.Join(f.Dir, p))
		if out, err := cmd.CombinedOutput(); err != nil {
			return cmderr.New(out, err)
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
func (c *Config) GptSetup() error {
	return nil
}

// Install system.
func (c *Config) InstallSystem() error {
	return nil
}

func mountBtrfsRoot(device string) (string, error) {
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
	if out, err := mcmd.CombinedOutput(); err != nil {
		return "", cmderr.New(out, err)
	}
	return dir, nil
}

func umountBtrfsRoot(dir string) error {
	cmd := exec.Command("umount", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return cmderr.New(out, err)
	}
	if err := os.Remove(dir); err != nil {
		return err
	}
	return nil
}
