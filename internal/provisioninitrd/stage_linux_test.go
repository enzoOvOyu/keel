//go:build linux

package provisioninitrd

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type denyStageSource struct{ calls int }

func (s *denyStageSource) CopyArtifact(context.Context, string, int64, io.Writer) error {
	s.calls++
	return ErrUnavailable
}

func TestStageInstallationRefusesOrdinaryHostBeforeAnyAction(t *testing.T) {
	source := &denyStageSource{}
	if _, err := StageInstallation(context.Background(), Input{}, &Files{}, source, strings.Repeat("a", 64)); err == nil || source.calls != 0 {
		t.Fatal("host staging bypassed PID1/initramfs boundary")
	}
	var stage *StagedInstallation
	if stage.RootFSFile() != nil || stage.TargetUKIFile() != nil || stage.ToolFile("unsquashfs") != nil || stage.RuntimeSHA256() != "" || stage.Close() != nil {
		t.Fatal("nil stage exposed usable input")
	}
}

func TestStageBoundedOutputDoesNotWriteOverflowBytes(t *testing.T) {
	var output bytes.Buffer
	w := &stageBoundedWriter{writer: &output, limit: 4}
	if n, err := w.Write([]byte("1234")); err != nil || n != 4 {
		t.Fatal(n, err)
	}
	if n, err := w.Write([]byte("5")); err == nil || n != 0 || output.String() != "1234" || !w.overflow {
		t.Fatal("overflow altered output")
	}
}

func TestInitrdRootFSDownloadIsExactFixedAndSingleAttempt(t *testing.T) {
	for _, mode := range []string{"exact", "redirect", "partial", "encoding", "short", "wrong_length"} {
		t.Run(mode, func(t *testing.T) {
			hits := 0
			files, _ := tlsFiles(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits++
				if !strings.HasSuffix(r.URL.Path, "/rootfs") || r.Header.Get("Range") != "" || r.Header.Get("Accept-Encoding") != "identity" {
					t.Error("unexpected artifact request")
				}
				switch mode {
				case "redirect":
					http.Redirect(w, r, "https://outside.invalid/rootfs", http.StatusFound)
					return
				case "partial":
					w.Header().Set("Content-Range", "bytes 0-95/96")
					w.WriteHeader(http.StatusPartialContent)
				case "encoding":
					w.Header().Set("Content-Encoding", "gzip")
				case "short":
					w.Header().Set("Content-Length", "96")
					io.WriteString(w, "short")
					return
				case "wrong_length":
					io.WriteString(w, strings.Repeat("x", 97))
					return
				}
				io.WriteString(w, strings.Repeat("x", 96))
			}))
			var output bytes.Buffer
			err := files.CopyArtifact(context.Background(), "rootfs", 96, &output)
			if (err == nil) != (mode == "exact") || hits != 1 {
				t.Fatalf("download status %v hits %d", err, hits)
			}
			for _, name := range []string{"../rootfs", "rootfs?x=1", "kernel", "https://other.invalid/rootfs"} {
				if files.CopyArtifact(context.Background(), name, 96, &output) == nil {
					t.Fatal("arbitrary artifact admitted")
				}
			}
			if hits != 1 {
				t.Fatal("invalid names issued requests")
			}
		})
	}
}
