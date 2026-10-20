package logistics

import (
	"context"
	"testing"
)

type recordingMedia struct {
	format string
	put    string
}

func (m *recordingMedia) Convert(_ context.Context, _ string, format string, _ []byte) ([]byte, error) {
	m.format = format
	return []byte("converted-pod"), nil
}

func (m *recordingMedia) PutObject(_ context.Context, bucket, key, contentType string, data []byte) error {
	m.put = bucket + "|" + key + "|" + contentType + "|" + string(data)
	return nil
}

func TestProcessSelectsFormatAndModelsShipmentState(t *testing.T) {
	tests := []struct {
		name          string
		kind          string
		wantFormat    string
		wantState     string
		wantException string
		wantKey       string
	}{
		{
			name: "delivered proof becomes webp", kind: "delivered", wantFormat: "webp",
			wantState: "pod_ready", wantKey: "shipments/SHP-204/events/EVT-9/doorstep.webp",
		},
		{
			name: "exception evidence becomes avif", kind: "delivery_exception", wantFormat: "avif",
			wantState: "exception_evidence_ready", wantException: "delivery_exception",
			wantKey: "shipments/SHP-204/events/EVT-9/doorstep.avif",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			media := &recordingMedia{}
			pipeline := Pipeline{Bucket: "logistics-pod", Media: media}
			got, err := pipeline.Process(context.Background(), ShipmentEvent{
				ShipmentID: "SHP-204", EventID: "EVT-9", Kind: tt.kind,
			}, ProofOfDelivery{Filename: "mobile/doorstep.jpg", Image: []byte("jpeg")})
			if err != nil {
				t.Fatal(err)
			}
			if got.Format != tt.wantFormat || got.State != tt.wantState || got.Exception != tt.wantException {
				t.Fatalf("result = %#v", got)
			}
			if got.ObjectKey != tt.wantKey {
				t.Fatalf("object key = %q, want %q", got.ObjectKey, tt.wantKey)
			}
			wantPut := "logistics-pod|" + tt.wantKey + "|image/" + tt.wantFormat + "|converted-pod"
			if media.format != tt.wantFormat || media.put != wantPut {
				t.Fatalf("conversion format = %q, storage write = %q", media.format, media.put)
			}
		})
	}
}

func TestProcessRejectsUntrackedEventBeforeConversion(t *testing.T) {
	media := &recordingMedia{}
	_, err := (Pipeline{Bucket: "logistics-pod", Media: media}).Process(context.Background(), ShipmentEvent{
		ShipmentID: "SHP-204", EventID: "EVT-9", Kind: "in_transit",
	}, ProofOfDelivery{Filename: "doorstep.jpg", Image: []byte("jpeg")})
	if err == nil || media.format != "" || media.put != "" {
		t.Fatalf("expected policy rejection before API calls, err=%v media=%#v", err, media)
	}
}
