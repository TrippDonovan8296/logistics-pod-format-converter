package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	logistics "example.com/logistics-pod-converter"
)

const maxUpload = 20 << 20

type server struct {
	pipeline logistics.Pipeline
}

func main() {
	apiKey := os.Getenv("INFRAI_API_KEY")
	if apiKey == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}
	bucket := envOr("POD_BUCKET", "logistics-pod-files")
	client := logistics.NewInfraiClient(apiKey)
	if err := client.CreateBucket(context.Background(), bucket); err != nil {
		log.Fatalf("create storage bucket: %v", err)
	}

	handler := server{pipeline: logistics.Pipeline{Bucket: bucket, Media: client}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /shipment-events", handler.processShipmentEvent)
	address := envOr("LISTEN_ADDR", ":8080")
	log.Printf("proof-of-delivery converter listening on %s", address)
	log.Fatal(http.ListenAndServe(address, mux))
}

func (s server) processShipmentEvent(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	if err := r.ParseMultipartForm(maxUpload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid multipart request"})
		return
	}
	file, header, err := r.FormFile("proof")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "proof is required"})
		return
	}
	defer file.Close()
	image, err := io.ReadAll(file)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not read proof"})
		return
	}

	result, err := s.pipeline.Process(r.Context(), logistics.ShipmentEvent{
		ShipmentID: r.FormValue("shipment_id"),
		EventID:    r.FormValue("event_id"),
		Kind:       r.FormValue("kind"),
	}, logistics.ProofOfDelivery{Filename: header.Filename, Image: image})
	if err != nil {
		var apiErr *logistics.InfraiError
		if errors.As(err, &apiErr) && apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 {
			writeJSON(w, apiErr.HTTPStatus, map[string]string{"error": apiErr.Message, "code": apiErr.Code})
			return
		}
		if strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "kind must") {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "proof processing failed"})
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write response: %v", err)
	}
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
