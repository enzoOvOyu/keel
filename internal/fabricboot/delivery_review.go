package fabricboot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/enzoOvOyu/keel/internal/fabricdelivery"
	"io"
	"net/http"
	"net/url"
	"time"
)

// ReadDeliveryReview is an optional read-only federation adapter over the same
// operator-managed mTLS gateway. It retrieves existing CMDB acceptance/waiver
// records bound to the exact evidence scope; browser claims are never used.
func (s *ProbeSuite) ReadDeliveryReview(ctx context.Context, project, scopeHash string) (fabricdelivery.Review, error) {
	empty := fabricdelivery.Review{Waivers: []fabricdelivery.Waiver{}, Code: "cmdb_acceptance_adapter_not_configured"}
	if s == nil {
		return empty, nil
	}
	probe, ok := s.targets["network"]
	if !ok {
		return empty, nil
	}
	transport, ok := probe.client.Transport.(*http.Transport)
	if !ok || transport.TLSClientConfig == nil || len(transport.TLSClientConfig.Certificates) == 0 {
		return empty, nil
	}
	if !projectPattern.MatchString(project) || !keaHashPattern.MatchString(scopeHash) {
		return empty, errors.New("invalid delivery review scope")
	}
	origin, e := url.Parse(probe.target.URL)
	if e != nil {
		return empty, e
	}
	origin.Path = "/v1/projects/" + project + "/network-delivery/review"
	origin.RawPath = ""
	origin.RawQuery = ""
	input := struct {
		Version   string `json:"version"`
		ProjectID string `json:"project_id"`
		ScopeHash string `json:"scope_hash"`
	}{"network-delivery-review-request/v1", project, scopeHash}
	body, _ := json.Marshal(input)
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, origin.String(), bytes.NewReader(body))
	if e != nil {
		return empty, e
	}
	req.Header.Set("Content-Type", "application/json")
	client := *probe.client
	client.Timeout = 3 * time.Second
	response, e := client.Do(req)
	if e != nil {
		empty.Code = "cmdb_acceptance_adapter_unavailable"
		return empty, nil
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		empty.Code = "cmdb_acceptance_adapter_unavailable"
		return empty, nil
	}
	raw, e := io.ReadAll(io.LimitReader(response.Body, 262145))
	if e != nil || len(raw) > 262144 {
		return empty, errors.New("delivery review response exceeds limits")
	}
	var reply struct {
		Version       string                `json:"version"`
		ProjectID     string                `json:"project_id"`
		ScopeHash     string                `json:"scope_hash"`
		RequestSHA256 string                `json:"request_sha256"`
		ObservedAt    time.Time             `json:"observed_at"`
		Review        fabricdelivery.Review `json:"review"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&reply) != nil || decoder.Decode(new(any)) != io.EOF {
		return empty, errors.New("delivery review protocol fields are invalid")
	}
	now := time.Now().UTC()
	if reply.Version != "network-delivery-review/v1" || reply.ProjectID != project || reply.ScopeHash != scopeHash || reply.RequestSHA256 != fabricdelivery.BytesDigest(body) || reply.ObservedAt.Before(now.Add(-30*time.Second)) || reply.ObservedAt.After(now.Add(5*time.Second)) || len(reply.Review.Waivers) > 128 {
		return empty, errors.New("delivery review identity, request digest or freshness is invalid")
	}
	for _, w := range reply.Review.Waivers {
		if w.ScopeHash != scopeHash || len(w.ID) > 160 || len(w.Reason) > 2000 || len(w.Compensation) > 2000 || len(w.ApprovalRef) > 300 || w.EvidenceSource != "cmdb" && w.EvidenceSource != "protocol_fixture" {
			return empty, errors.New("delivery waiver scope or provenance is invalid")
		}
	}
	if a := reply.Review.Acceptance; a != nil && (a.ScopeHash != scopeHash || len(a.ID) > 160 || len(a.ApprovalRef) > 300 || len(a.CMDBSnapshotRef) > 300 || a.EvidenceSource != "cmdb" && a.EvidenceSource != "protocol_fixture") {
		return empty, errors.New("delivery acceptance scope or provenance is invalid")
	}
	if reply.Review.Waivers == nil {
		reply.Review.Waivers = []fabricdelivery.Waiver{}
	}
	return reply.Review, nil
}
