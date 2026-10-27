package agentclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/enzoOvOyu/keel/internal/agentpki"
	"github.com/enzoOvOyu/keel/internal/monitor"
)

const monitorQueueLimit = 64

var monitorQueueMu sync.Mutex
var errMonitorQueueFull = errors.New("monitor queue capacity exhausted")

// All queue operations are serialized by a private no-symlink directory lock.
// Frames are immutable and removed only after their exact durable ingest ACK.
func monitorQueueDirectory(c Config) (string, error) {
	path := filepath.Join(c.StateDirectory, "monitor")
	if e := os.Mkdir(path, 0700); e != nil && !os.IsExist(e) {
		return "", ErrIdentity
	}
	info, e := os.Lstat(path)
	if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0700 {
		return "", ErrIdentity
	}
	if st, ok := info.Sys().(*syscall.Stat_t); !ok || st.Uid != uint32(os.Geteuid()) {
		return "", ErrIdentity
	}
	return path, nil
}
func monitorQueueFiles(path string) ([]string, error) {
	entries, e := os.ReadDir(path)
	if e != nil {
		return nil, e
	}
	names := []string{}
	for _, entry := range entries {
		n := entry.Name()
		if strings.HasPrefix(n, "frame-") && strings.HasSuffix(n, ".json") {
			if !entry.Type().IsRegular() || len(n) < len("frame-")+20+1+1+len(".json") {
				return nil, ErrIdentity
			}
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names, nil
}
func monitorQueuePeek(c Config) ([]byte, error) {
	monitorQueueMu.Lock()
	defer monitorQueueMu.Unlock()
	path, e := monitorQueueDirectory(c)
	if e != nil {
		return nil, e
	}
	unlock, e := agentpki.LockIdentity(path)
	if e != nil {
		return nil, e
	}
	defer unlock()
	names, e := monitorQueueFiles(path)
	if e != nil || len(names) == 0 {
		return nil, e
	}
	raw, e := agentpki.ReadFile(filepath.Join(path, names[0]), monitor.MaxFrameBytes, true)
	if e != nil {
		return nil, e
	}
	if _, e = monitor.DecodeFrame(raw, time.Now()); e != nil {
		return nil, ErrIdentity
	}
	return raw, nil
}
func monitorQueueAck(c Config, raw []byte, id string) error {
	monitorQueueMu.Lock()
	defer monitorQueueMu.Unlock()
	if len(raw) == 0 {
		return nil
	}
	frame, e := monitor.DecodeFrame(raw, time.Now())
	if e != nil || frame.ID != id {
		return ErrIdentity
	}
	path, e := monitorQueueDirectory(c)
	if e != nil {
		return e
	}
	unlock, e := agentpki.LockIdentity(path)
	if e != nil {
		return e
	}
	defer unlock()
	names, e := monitorQueueFiles(path)
	if e != nil || len(names) == 0 {
		return ErrIdentity
	}
	previous, e := agentpki.ReadFile(filepath.Join(path, names[0]), monitor.MaxFrameBytes, true)
	if e != nil || string(previous) != string(raw) {
		return ErrIdentity
	}
	if e = os.Remove(filepath.Join(path, names[0])); e != nil {
		return e
	}
	dir, e := os.Open(path)
	if e != nil {
		return e
	}
	defer dir.Close()
	return dir.Sync()
}
func monitorQueueAppend(c Config, frame monitor.Frame) error {
	monitorQueueMu.Lock()
	defer monitorQueueMu.Unlock()
	path, e := monitorQueueDirectory(c)
	if e != nil {
		return e
	}
	unlock, e := agentpki.LockIdentity(path)
	if e != nil {
		return e
	}
	defer unlock()
	names, e := monitorQueueFiles(path)
	if e != nil {
		return e
	}
	gapPath := filepath.Join(path, "overflow.json")
	if len(names) >= monitorQueueLimit {
		if _, e = os.Lstat(gapPath); os.IsNotExist(e) {
			raw, _ := json.Marshal(time.Now().UTC())
			if e = agentpki.WriteNew(gapPath, raw); e != nil {
				return e
			}
		} else if e != nil {
			return e
		}
		return errMonitorQueueFull // caller stops sampling and retries after ACK drain.
	}
	var gapRaw []byte
	if _, e = os.Lstat(gapPath); e == nil {
		gapRaw, e = agentpki.ReadFile(gapPath, 128, true)
		if e != nil {
			return e
		}
		var start time.Time
		if json.Unmarshal(gapRaw, &start) != nil {
			return ErrIdentity
		}
		end := time.Now().UTC()
		frame.GapStart = &start
		frame.GapEnd = &end
	} else if !os.IsNotExist(e) {
		return e
	}
	raw, e := json.Marshal(frame)
	if e != nil || len(raw) > monitor.MaxFrameBytes {
		return ErrIdentity
	}
	if _, e = monitor.DecodeFrame(raw, time.Now()); e != nil {
		return e
	}
	// Time is display/order metadata only; a random frame ID prevents replacement.
	order := time.Now().UnixNano()
	if len(names) > 0 {
		previous, e := strconv.ParseInt(strings.TrimPrefix(names[len(names)-1], "frame-")[:20], 10, 64)
		if e != nil {
			return ErrIdentity
		}
		if order <= previous {
			order = previous + 1
		}
	}
	name := fmt.Sprintf("frame-%020d-%s.json", order, frame.ID)
	if e = agentpki.WriteNew(filepath.Join(path, name), raw); e != nil {
		return e
	}
	if len(gapRaw) > 0 {
		if e = os.Remove(gapPath); e != nil {
			return e
		}
	}
	return nil
}
func monitorSaveAssignment(c Config, assignment monitor.Assignment) error {
	monitorQueueMu.Lock()
	defer monitorQueueMu.Unlock()
	if assignment.Version != monitor.Version || assignment.Policy.Validate() != nil || assignment.PayloadBytesPerSecond < 1024 || assignment.PayloadBytesPerSecond > 50000 || assignment.BurstBytes != monitor.MaxFrameBytes || assignment.InitialJitterMillis < 0 || assignment.InitialJitterMillis >= 10000 {
		return ErrIdentity
	}
	path, e := monitorQueueDirectory(c)
	if e != nil {
		return e
	}
	unlock, e := agentpki.LockIdentity(path)
	if e != nil {
		return e
	}
	defer unlock()
	target := filepath.Join(path, "assignment.json")
	raw, _ := json.Marshal(assignment)
	if _, e = os.Lstat(target); os.IsNotExist(e) {
		return agentpki.WriteNew(target, raw)
	}
	if e != nil {
		return e
	}
	previous, e := agentpki.ReadFile(target, 16384, true)
	if e != nil {
		return e
	}
	if string(previous) == string(raw) {
		return nil
	}
	return agentpki.ReplaceIdentity(target, previous, raw)
}
func monitorLoadAssignment(c Config) (*monitor.Assignment, error) {
	path, e := monitorQueueDirectory(c)
	if e != nil {
		return nil, e
	}
	target := filepath.Join(path, "assignment.json")
	if _, e = os.Lstat(target); os.IsNotExist(e) {
		return nil, nil
	}
	raw, e := agentpki.ReadFile(target, 16384, true)
	if e != nil {
		return nil, e
	}
	var assignment monitor.Assignment
	if json.Unmarshal(raw, &assignment) != nil || assignment.Policy.Validate() != nil {
		return nil, ErrIdentity
	}
	return &assignment, nil
}
