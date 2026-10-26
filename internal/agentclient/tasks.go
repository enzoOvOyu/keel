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
	handler := taskrunner.Agent{StateDirectory: dir, ProjectID: c.ProjectID, MachineID: c.MachineID, ScriptsEnabled: c.ScriptsEnabled, MediaCacheEnabled: c.MediaCacheEnabled, CacheInventoryEnabled: c.CacheInventoryEnabled}
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
	seen := map[string]bool{}
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
		if !identityID.MatchString(id) || seen[id] || len(seen) >= 256 || len(request.GetRequestJson()) == 0 || len(request.GetRequestJson()) > 14336 {
			return ErrIdentity
		}
		seen[id] = true
		expires := request.GetExpiresAtUnixMilli()
		if !taskFrameCurrent(expires, time.Now()) {
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
