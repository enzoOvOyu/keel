//go:build linux

package provisioninitrd

import (
	"context"
	"encoding/json"

	"github.com/enzoOvOyu/keel/internal/provisionboot"
)

func (h *HTTPFiles) IssueWritePermit(ctx context.Context, raw []byte) (*provisionboot.WritePermit, error) {
	body, err := h.postBoot(ctx, "write-permit", "", raw)
	if err != nil {
		return nil, err
	}
	var out provisionboot.WritePermit
	if json.Unmarshal(body, &out) != nil {
		return nil, ErrUnavailable
	}
	return &out, nil
}

func (h *HTTPFiles) CheckWritePermit(ctx context.Context, raw []byte) (*provisionboot.WriteAuthorization, error) {
	body, err := h.postBoot(ctx, "write-check", "", raw)
	if err != nil {
		return nil, err
	}
	var out provisionboot.WriteAuthorization
	if json.Unmarshal(body, &out) != nil {
		return nil, ErrUnavailable
	}
	return &out, nil
}

func (h *HTTPFiles) ReportWriteReceipt(ctx context.Context, raw []byte) (*provisionboot.BootAcknowledgment, error) {
	body, err := h.postBoot(ctx, "write-receipt", "", raw)
	if err != nil {
		return nil, err
	}
	var out provisionboot.BootAcknowledgment
	if json.Unmarshal(body, &out) != nil {
		return nil, ErrUnavailable
	}
	return &out, nil
}
