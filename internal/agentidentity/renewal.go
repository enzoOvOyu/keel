package agentidentity

import (
	"context"
	"time"

	pb "github.com/enzoOvOyu/keel/internal/agentproto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) Renew(ctx context.Context, in *pb.RenewRequest) (*pb.EnrollResponse, error) {
	if !s.renewalEnabled {
		return nil, status.Error(codes.Unimplemented, "Agent certificate renewal is disabled")
	}
	cert, e := certificate(ctx)
	if e != nil {
		return nil, denied
	}
	if !s.reserve() {
		return nil, status.Error(codes.ResourceExhausted, "Agent certificate renewal temporarily unavailable")
	}
	defer func() { <-s.slots }()
	work, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, e := s.store.RenewAgentCertificate(work, s.authority, cert, in.GetCsrPem())
	if e != nil {
		return nil, storeError(e)
	}
	return &pb.EnrollResponse{CredentialId: out.CredentialID, CertificatePem: []byte(out.CertificatePEM), CaCertificatePem: s.authority.PEM, ExpiresAt: out.ExpiresAt.Format(time.RFC3339)}, nil
}
