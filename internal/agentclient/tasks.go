package agentclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"

	pb "github.com/enzoOvOyu/keel/internal/agentproto"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

// A delivery deadline protects first execution from buffered frames that arrive
// after the control-plane lease expired. It does not shorten admitted work.
func taskFrameCurrent(deadline int64, now time.Time) bool {
	return deadline > now.UnixMilli() && deadline <= now.Add(35*time.Second).UnixMilli()
}

// taskStream is subordinate to exactly one authenticated heartbeat connection.
// It never resubmits work after a reconnect; queries retrieve durable receipts.
func (c Config) taskStream(ctx context.Context, pair tls.Certificate, connectionID string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	dir, e := taskrunner.PrepareAgentState(c.StateDirectory)
	if e != nil {
		return ErrIdentity
	}
	handler := taskrunner.Agent{ArtifactTransfersEnabled: c.ArtifactTransfersEnabled, StateDirectory: dir, ProjectID: c.ProjectID, MachineID: c.MachineID, ScriptsEnabled: c.ScriptsEnabled, MediaCacheEnabled: c.MediaCacheEnabled, CacheInventoryEnabled: c.CacheInventoryEnabled}
	connection, e := c.connect([]tls.Certificate{pair})
	if e != nil {
		return ErrIdentity
	}
	defer connection.Close()
	stream, e := pb.NewAgentIdentityClient(connection).Tasks(ctx)
	if e != nil {
		return e
	}
	defer stream.CloseSend()
	if e = stream.Send(&pb.TaskReport{ConnectionId: connectionID}); e != nil {
		return e
	}
	hello, e := stream.Recv()
	if e != nil {
		return e
	}
	if hello.GetRequestId() != "" || len(hello.GetRequestJson()) != 0 || hello.GetExpiresAtUnixMilli() != 0 {
		return ErrIdentity
	}
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	slots := make(chan struct{}, 8)
	reports := make(chan *pb.TaskReport, 8)
	sendErrors := make(chan error, 1)
	workers.Add(1)
	go func() {
		defer workers.Done()
		for {
			select {
			case report := <-reports:
				if err := stream.Send(report); err != nil {
					sendErrors <- err
					cancel()
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	seen := taskRequestWindow{active: map[string]bool{}, recent: map[string]bool{}}
	for {
		request, err := stream.Recv()
		if err != nil {
			select {
			case sendErr := <-sendErrors:
				return sendErr
			default:
				return err
			}
		}
		id := request.GetRequestId()
		if !identityID.MatchString(id) || !seen.admit(id) || len(request.GetRequestJson()) == 0 || len(request.GetRequestJson()) > 14336 {
			return ErrIdentity
		}
		expires := request.GetExpiresAtUnixMilli()
		if !taskFrameCurrent(expires, time.Now()) {
			seen.complete(id)
			select {
			case reports <- &pb.TaskReport{ConnectionId: connectionID, RequestId: id, Error: "unknown"}:
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}
		d := json.NewDecoder(bytes.NewReader(request.GetRequestJson()))
		d.DisallowUnknownFields()
		var command taskrunner.Request
		var extra any
		if d.Decode(&command) != nil || d.Decode(&extra) != io.EOF {
			return ErrIdentity
		}
		select {
		case slots <- struct{}{}:
		default:
			return ErrIdentity
		}
		workers.Add(1)
		go func(id string, expires int64, command taskrunner.Request) {
			defer workers.Done()
			defer seen.complete(id)
			defer func() { <-slots }()
			report := &pb.TaskReport{ConnectionId: connectionID, RequestId: id}
			if !taskFrameCurrent(expires, time.Now()) {
				report.Error = "unknown"
				select {
				case reports <- report:
				case <-ctx.Done():
				}
				return
			}
			observed, err := handler.Handle(ctx, command)
			if err != nil {
				report.Error = "unknown"
				if errors.Is(err, taskrunner.ErrRejected) {
					report.Error = "rejected"
				}
			} else {
				raw, e := json.Marshal(observed)
				if e != nil || len(raw) > 14336 {
					report.Error = "unknown"
				} else {
					report.ObservationJson = raw
				}
			}
			select {
			case reports <- report:
			case <-ctx.Done():
			}
		}(id, expires, command)
	}
}

// Frame IDs deduplicate transport responses, not write authority. Keep active IDs
// and the last 256 completed frames; durable command/fence/checkpoint validation
// remains authoritative even when a very old frame falls outside this window.
type taskRequestWindow struct {
	mu     sync.Mutex
	active map[string]bool
	recent map[string]bool
	order  []string
}

func (w *taskRequestWindow) admit(id string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.active[id] || w.recent[id] {
		return false
	}
	w.active[id] = true
	return true
}
func (w *taskRequestWindow) complete(id string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.active, id)
	if w.recent[id] {
		return
	}
	if len(w.order) == 256 {
		delete(w.recent, w.order[0])
		w.order = w.order[1:]
	}
	w.order = append(w.order, id)
	w.recent[id] = true
}
