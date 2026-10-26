// Package agentrunner multiplexes transient requests over authenticated Agent
// streams. It has no durable queue; Execution/outbox/attempts own delivery state.
package agentrunner

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"sync"
	"time"

	pb "github.com/enzoOvOyu/keel/internal/agentproto"
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var id = regexp.MustCompile(`^[0-9a-f]{32}$`)
var authorityID = regexp.MustCompile(`^[0-9a-f]{64}$`)

type response struct {
	observation taskrunner.Observation
	err         error
}
type pending struct {
	ctx     context.Context
	request taskrunner.Request
	result  chan response
}
type session struct {
	connection, credential string
	project, machine       string
	binding                taskrunner.Binding
	requests               chan *pb.TaskRequest
	done                   chan struct{}
	mu                     sync.Mutex
	pending                map[string]pending
}
type Broker struct {
	store     *platform.Store
	mu        sync.Mutex
	authority string
	sessions  map[string]*session
}

func New(store *platform.Store) *Broker {
	return &Broker{store: store, sessions: map[string]*session{}}
}
func (b *Broker) ConfigureAuthority(authority string) error {
	if !authorityID.MatchString(authority) {
		return taskrunner.ErrRejected
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.authority != "" && b.authority != authority {
		return taskrunner.ErrRejected
	}
	b.authority = authority
	return nil
}
func (b *Broker) Binding(project, machine string) (taskrunner.Binding, error) {
	b.mu.Lock()
	authority := b.authority
	if authority != "" {
		// Dispatchers can use only their own transport pool. This also avoids a
		// nested DB transaction while execution admission holds its identity locks.
		for _, session := range b.sessions {
			if session.project == project && session.machine == machine {
				binding := session.binding
				b.mu.Unlock()
				return binding, nil
			}
		}
		b.mu.Unlock()
		return taskrunner.Binding{}, taskrunner.ErrUnknown
	}
	b.mu.Unlock()
	// The owner CLI has no authority/listener; it reads current task connection
	// metadata for admission but can never dispatch through this empty pool.
	ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	c, _, e := b.store.ActiveAgentTaskBinding(ctx, project, machine)
	if e != nil {
		return taskrunner.Binding{}, e
	}
	return platform.AgentCertificateBinding(c), nil
}
func (b *Broker) Do(ctx context.Context, r taskrunner.Request) (taskrunner.Observation, error) {
	var zero taskrunner.Observation
	if r.Validate() != nil || (r.Command.Operation != "host.identity" && r.Command.Operation != "host.wait" && r.Command.Operation != "script.run" && r.Command.Operation != "host.cache.inventory" && r.Command.Operation != "host.artifact.inventory" && r.Command.Operation != "media.stage") {
		return zero, taskrunner.ErrRejected
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(r.Command.TimeoutMS)*time.Millisecond+5*time.Second)
	defer cancel()
	c, connection, e := b.store.ActiveAgentTaskBinding(ctx, r.Command.ProjectID, r.Command.MachineID)
	if e != nil {
		return zero, taskrunner.ErrUnknown
	}
	if e = b.store.AgentRequestAllowed(ctx, r, c); e != nil {
		return zero, e
	}
	b.mu.Lock()
	s := b.sessions[c.ID]
	authority := b.authority
	b.mu.Unlock()
	if s == nil || s.connection != connection || authority != c.AuthorityID {
		return zero, taskrunner.ErrUnknown
	}
	expiry, e := b.store.AgentRequestExpiry(ctx, r)
	if e != nil {
		return zero, e
	}
	raw, e := json.Marshal(r)
	if e != nil || len(raw) > 14336 {
		return zero, taskrunner.ErrRejected
	}
	requestID := platform.RandomID()
	result := make(chan response, 1)
	s.mu.Lock()
	if len(s.pending) >= 32 {
		s.mu.Unlock()
		return zero, taskrunner.ErrUnknown
	}
	s.pending[requestID] = pending{ctx: ctx, request: r, result: result}
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.pending, requestID); s.mu.Unlock() }()
	select {
	case s.requests <- &pb.TaskRequest{RequestId: requestID, RequestJson: raw, ExpiresAtUnixMilli: expiry.UnixMilli()}:
	case <-s.done:
		return zero, taskrunner.ErrUnknown
	case <-ctx.Done():
		return zero, taskrunner.ErrUnknown
	}
	select {
	case out := <-result:
		return out.observation, out.err
	case <-s.done:
		return zero, taskrunner.ErrUnknown
	case <-ctx.Done():
		return zero, taskrunner.ErrUnknown
	}
}
func decode(raw []byte, out any) error {
	if len(raw) == 0 || len(raw) > 16384 {
		return taskrunner.ErrRejected
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil {
		return taskrunner.ErrRejected
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return taskrunner.ErrRejected
	}
	return nil
}
func unavailable(e error) error {
	if errors.Is(e, platform.ErrAgentDenied) {
		return status.Error(codes.PermissionDenied, "Agent task identity unavailable")
	}
	if errors.Is(e, platform.ErrAgentConnectionBusy) {
		return status.Error(codes.ResourceExhausted, "Agent task connection already active")
	}
	return status.Error(codes.Unavailable, "Agent task identity storage unavailable")
}

// Serve is called only after gRPC has verified the registered client cert. A
// task stream additionally owns an existing live heartbeat connection ID.
func (b *Broker) Serve(stream grpc.BidiStreamingServer[pb.TaskReport, pb.TaskRequest], cert *x509.Certificate, authority string) error {
	ctx, cancel := context.WithTimeout(stream.Context(), 2*time.Minute)
	defer cancel()
	type incoming struct {
		report *pb.TaskReport
		err    error
	}
	received := make(chan incoming, 1)
	go func() {
		for {
			r, e := stream.Recv()
			select {
			case received <- incoming{r, e}:
			case <-ctx.Done():
				return
			}
			if e != nil {
				return
			}
		}
	}()
	var first *pb.TaskReport
	select {
	case in := <-received:
		if in.err != nil {
			return unavailable(platform.ErrAgentDenied)
		}
		first = in.report
	case <-ctx.Done():
		return status.Error(codes.DeadlineExceeded, "Agent task stream ended")
	case <-time.After(5 * time.Second):
		return status.Error(codes.DeadlineExceeded, "Agent task hello timed out")
	}
	if !id.MatchString(first.GetConnectionId()) || first.GetRequestId() != "" || len(first.GetObservationJson()) != 0 || first.GetError() != "" {
		return unavailable(platform.ErrAgentDenied)
	}
	connection, sessionID := first.GetConnectionId(), platform.RandomID()
	work, stop := context.WithTimeout(ctx, 3*time.Second)
	c, e := b.store.OpenAgentTaskSession(work, cert, authority, connection, sessionID)
	stop()
	if e != nil {
		return unavailable(e)
	}
	s := &session{project: c.ProjectID, machine: c.MachineID, binding: platform.AgentCertificateBinding(c), connection: connection, credential: c.ID, requests: make(chan *pb.TaskRequest, 32), done: make(chan struct{}), pending: map[string]pending{}}
	b.mu.Lock()
	if b.sessions[c.ID] != nil {
		b.mu.Unlock()
		cleanup, done := context.WithTimeout(context.Background(), 3*time.Second)
		defer done()
		_ = b.store.CloseAgentTaskSession(cleanup, connection, sessionID)
		return unavailable(platform.ErrAgentConnectionBusy)
	}
	b.sessions[c.ID] = s
	b.mu.Unlock()
	defer func() {
		close(s.done)
		b.mu.Lock()
		if b.sessions[c.ID] == s {
			delete(b.sessions, c.ID)
		}
		b.mu.Unlock()
		cleanup, done := context.WithTimeout(context.Background(), 3*time.Second)
		defer done()
		_ = b.store.CloseAgentTaskSession(cleanup, connection, sessionID)
	}()
	// A single sender serializes all frames. Its context is bounded even if the
	// peer stops reading; returning the handler cancels the underlying gRPC stream.
	type sendCall struct {
		frame  *pb.TaskRequest
		result chan error
	}
	sends := make(chan sendCall)
	go func() {
		for {
			select {
			case call := <-sends:
				err := stream.Send(call.frame)
				call.result <- err
				if err != nil {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	sendFrame := func(frame *pb.TaskRequest) error {
		work, stop := context.WithTimeout(ctx, 3*time.Second)
		defer stop()
		result := make(chan error, 1)
		select {
		case sends <- sendCall{frame, result}:
		case <-work.Done():
			return status.Error(codes.Unavailable, "Agent task send deadline exceeded")
		}
		select {
		case err := <-result:
			if err != nil {
				return status.Error(codes.Unavailable, "Agent task transport closed")
			}
			return nil
		case <-work.Done():
			return status.Error(codes.Unavailable, "Agent task send deadline exceeded")
		}
	}
	if e = sendFrame(&pb.TaskRequest{}); e != nil {
		return e
	}
	check := time.NewTicker(2 * time.Second)
	defer check.Stop()
	for {
		select {
		case <-ctx.Done():
			return status.Error(codes.DeadlineExceeded, "Agent task connection window ended")
		case <-check.C:
			work, stop := context.WithTimeout(ctx, 3*time.Second)
			_, e = b.store.VerifyAgentTaskConnection(work, cert, authority, connection)
			stop()
			if e != nil {
				return unavailable(e)
			}
		case frame := <-s.requests:
			s.mu.Lock()
			pendingRequest, present := s.pending[frame.GetRequestId()]
			s.mu.Unlock()
			if !present || pendingRequest.ctx.Err() != nil || frame.GetExpiresAtUnixMilli() <= time.Now().UnixMilli() {
				continue
			}
			// Revalidate at send time, including current execution authority. Queueing a
			// transient frame never grants permission to send after a kill or revocation.
			var request taskrunner.Request
			if decode(frame.GetRequestJson(), &request) != nil {
				return unavailable(platform.ErrAgentDenied)
			}
			work, stop := context.WithTimeout(ctx, 3*time.Second)
			current, err := b.store.VerifyAgentTaskConnection(work, cert, authority, connection)
			if err == nil {
				err = b.store.AgentRequestAllowed(work, request, current)
			}
			stop()
			if err != nil {
				s.mu.Lock()
				p, exists := s.pending[frame.GetRequestId()]
				s.mu.Unlock()
				if exists {
					select {
					case p.result <- response{err: taskrunner.ErrUnknown}:
					default:
					}
				}
				continue
			}
			if e = sendFrame(frame); e != nil {
				return e
			}
		case in := <-received:
			if in.err != nil {
				return status.Error(codes.Unavailable, "Agent task stream closed")
			}
			r := in.report
			if r.GetConnectionId() != connection || !id.MatchString(r.GetRequestId()) {
				return unavailable(platform.ErrAgentDenied)
			}
			s.mu.Lock()
			p, exists := s.pending[r.GetRequestId()]
			s.mu.Unlock()
			if !exists {
				continue
			} // Late response after deadline cannot revive an attempt.
			work, stop := context.WithTimeout(ctx, 3*time.Second)
			_, err := b.store.VerifyAgentTaskConnection(work, cert, authority, connection)
			stop()
			if err != nil {
				return unavailable(err)
			}
			var out response
			switch r.GetError() {
			case "":
				if decode(r.GetObservationJson(), &out.observation) != nil || out.observation.Validate(p.request.Command) != nil {
					return unavailable(platform.ErrAgentDenied)
				}
			case "unknown", "rejected":
				if len(r.GetObservationJson()) != 0 {
					return unavailable(platform.ErrAgentDenied)
				}
				out.err = taskrunner.ErrUnknown
				if r.GetError() == "rejected" {
					out.err = taskrunner.ErrRejected
				}
			default:
				return unavailable(platform.ErrAgentDenied)
			}
			select {
			case p.result <- out:
			default:
			}
		}
	}
}

var _ taskrunner.Runner = (*Broker)(nil)
