package logistics

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

type MediaStore interface {
	Convert(context.Context, string, string, []byte) ([]byte, error)
	PutObject(context.Context, string, string, string, []byte) error
}

type ShipmentEvent struct {
	ShipmentID string `json:"shipment_id"`
	EventID    string `json:"event_id"`
	Kind       string `json:"kind"`
}

type ProofOfDelivery struct {
	Filename string `json:"filename"`
	Image    []byte `json:"-"`
}

type ProcessingResult struct {
	ShipmentID string `json:"shipment_id"`
	EventID    string `json:"event_id"`
	State      string `json:"state"`
	Format     string `json:"format"`
	ObjectKey  string `json:"object_key"`
	Exception  string `json:"exception,omitempty"`
}

type Pipeline struct {
	Bucket string
	Media  MediaStore
}

func (p Pipeline) Process(ctx context.Context, event ShipmentEvent, pod ProofOfDelivery) (ProcessingResult, error) {
	if event.ShipmentID == "" || event.EventID == "" || pod.Filename == "" || len(pod.Image) == 0 {
		return ProcessingResult{}, fmt.Errorf("shipment_id, event_id, filename, and image are required")
	}

	format, state, exception, err := conversionPolicy(event.Kind)
	if err != nil {
		return ProcessingResult{}, err
	}
	converted, err := p.Media.Convert(ctx, pod.Filename, format, pod.Image)
	if err != nil {
		return ProcessingResult{}, fmt.Errorf("convert proof of delivery: %w", err)
	}

	stem := strings.TrimSuffix(filepath.Base(pod.Filename), filepath.Ext(pod.Filename))
	key := fmt.Sprintf("shipments/%s/events/%s/%s.%s", event.ShipmentID, event.EventID, stem, format)
	if err := p.Media.PutObject(ctx, p.Bucket, key, "image/"+format, converted); err != nil {
		return ProcessingResult{}, fmt.Errorf("store proof of delivery: %w", err)
	}

	return ProcessingResult{
		ShipmentID: event.ShipmentID,
		EventID:    event.EventID,
		State:      state,
		Format:     format,
		ObjectKey:  key,
		Exception:  exception,
	}, nil
}

func conversionPolicy(kind string) (format, state, exception string, err error) {
	switch kind {
	case "delivered":
		return "webp", "pod_ready", "", nil
	case "delivery_exception":
		return "avif", "exception_evidence_ready", "delivery_exception", nil
	default:
		return "", "", "", fmt.Errorf("kind must be delivered or delivery_exception")
	}
}
