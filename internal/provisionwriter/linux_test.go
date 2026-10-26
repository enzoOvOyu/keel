//go:build linux

package provisionwriter

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/enzoOvOyu/keel/internal/provisionstorage"

	"golang.org/x/sys/unix"
)

func TestWriterPrivilegedAdaptersRejectOrdinaryHostBeforeIO(t *testing.T) {
	if os.Getpid() == 1 {
		t.Skip("ordinary process guard test")
	}
	if _, err := NewLinux(context.Background(), LinuxInputs{}); err == nil {
		t.Fatal("host opened installation adapter")
	}
	if handled, code := RunToolChild([]string{"--keel-writer-tool", "mkfs.ext4"}); !handled || code != 125 {
		t.Fatal("host activated internal tool boundary")
	}
	if handled, _ := RunToolChild([]string{"--once"}); handled {
		t.Fatal("unrelated CLI was consumed")
	}
	if _, err := newLoop(nil, provisionstorage.Partition{}, 512); err == nil {
		t.Fatal("host allocated loop mapping")
	}
}

func TestWriterFixedToolArgumentsCannotIncludeArchiveCommands(t *testing.T) {
	for _, operation := range []string{"unsquashfs", "mkfs.ext4", "mkfs.vfat"} {
		c := toolRequest{Operation: operation, Sector: 4096, Size: 512 << 20, RootUUID: "21234567-89ab-4cde-a012-3456789abcde", FATID: "01234567"}
		if operation == "mkfs.ext4" {
			c.Size = 32 << 30
		}
		argv, env, err := toolArgv(c)
		if err != nil || len(argv) == 0 || argv[0] != operation || !strings.Contains(strings.Join(argv, " "), "/proc/self/fd/5") || strings.Contains(strings.Join(argv, " "), "/dev/") || !strings.Contains(strings.Join(env, " "), "MKE2FS_CONFIG=/etc/mke2fs.conf") {
			t.Fatalf("unsafe argv: %v %v %v", argv, env, err)
		}
		if operation == "unsquashfs" && (!strings.Contains(strings.Join(argv, " "), "-strict-errors") || strings.Contains(strings.Join(argv, " "), "-force")) {
			t.Fatal("extractor can overwrite a preexisting tree")
		}
	}
	if _, _, err := toolArgv(toolRequest{Operation: "shell"}); err == nil {
		t.Fatal("arbitrary command reached tool boundary")
	}
}

func TestWriterRequiresInstalledFilesystemDriversBeforePermit(t *testing.T) {
	good := []byte("nodev\tproc\nnodev\ttmpfs\n\text4\n\tvfat\n")
	if !requiredFilesystems(good) {
		t.Fatal("registered required drivers rejected")
	}
	for _, remove := range []string{"proc", "tmpfs", "ext4", "vfat"} {
		if requiredFilesystems([]byte(strings.Replace(string(good), remove, "other", 1))) {
			t.Fatalf("missing %s driver admitted", remove)
		}
	}
}

func TestWriterOwnedPathsAndSymlinksStayBelowTargetRoot(t *testing.T) {
	rootDirectory := t.TempDir()
	outside := t.TempDir()
	root, err := openMountRoot(rootDirectory)
	if err != nil {
		if os.Geteuid() != 0 {
			t.Skip("owned root requires root")
		}
		t.Fatal(err)
	}
	defer root.Close()
	if os.Symlink(outside, rootDirectory+"/escape") != nil {
		t.Fatal("symlink setup")
	}
	for _, path := range []string{"../outside", "/etc/passwd", "escape/write", "a/../write"} {
		if err := writeOwned(context.Background(), root, path, strings.NewReader("x"), 1, 0600, false); err == nil {
			t.Fatalf("escaped root through %s", path)
		}
	}
	if err := writeOwned(context.Background(), root, "safe/seed", strings.NewReader("fixed"), 5, 0600, false); err != nil {
		t.Fatal(err)
	}
	if err := writeOwned(context.Background(), root, "safe/seed", strings.NewReader("again"), 5, 0600, false); err == nil {
		t.Fatal("silently replaced an existing seed")
	}
	fd, err := ownedOpen(root, "safe/seed", unix.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	unix.Close(fd)
	if _, err := openOwnedRead(root, "escape/secret", 1024); err == nil {
		t.Fatal("followed image-provided symlink")
	}
}
