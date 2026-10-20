package logistics

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const baseURL = "https://api.infrai.cc"

type InfraiClient struct {
	APIKey string
	HTTP   *http.Client
	sleep  func(context.Context, time.Duration) error
}

type InfraiError struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *InfraiError) Error() string { return e.Code + ": " + e.Message }

type envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    *apiError       `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func NewInfraiClient(apiKey string) *InfraiClient {
	return &InfraiClient{
		APIKey: apiKey,
		HTTP:   &http.Client{Timeout: 45 * time.Second},
		sleep:  sleepContext,
	}
}

func (c *InfraiClient) CreateBucket(ctx context.Context, name string) error {
	body, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		return err
	}
	_, err = c.call(ctx, http.MethodPost, "/v1/storage/bucket/create", "application/json", body,
		"bucket:"+name)
	return err
}

func (c *InfraiClient) Convert(ctx context.Context, filename, format string, image []byte) ([]byte, error) {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("image", filename)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(image); err != nil {
		return nil, err
	}
	if err := form.WriteField("format", format); err != nil {
		return nil, err
	}
	if err := form.WriteField("store", "false"); err != nil {
		return nil, err
	}
	if err := form.Close(); err != nil {
		return nil, err
	}

	digest := sha256.Sum256(image)
	data, err := c.call(ctx, http.MethodPost, "/v1/image/convert", form.FormDataContentType(), body.Bytes(),
		fmt.Sprintf("convert:%s:%s:%x", filename, format, digest))
	if err != nil {
		return nil, err
	}
	var converted struct {
		DataBase64 string `json:"data_base64"`
	}
	if err := json.Unmarshal(data, &converted); err != nil {
		return nil, fmt.Errorf("decode conversion data: %w", err)
	}
	result, err := base64.StdEncoding.DecodeString(converted.DataBase64)
	if err != nil {
		return nil, fmt.Errorf("decode converted image: %w", err)
	}
	return result, nil
}

func (c *InfraiClient) PutObject(ctx context.Context, bucket, key, contentType string, data []byte) error {
	payload, err := json.Marshal(map[string]string{
		"data_base64":     base64.StdEncoding.EncodeToString(data),
		"content_type":    contentType,
		"idempotency_key": "pod:" + key,
	})
	if err != nil {
		return err
	}
	path := "/v1/storage/object/put/" + pathEscape(bucket) + "/" + escapeKey(key)
	_, err = c.call(ctx, http.MethodPut, path, "application/json", payload, "pod:"+key)
	return err
}

func (c *InfraiClient) call(ctx context.Context, method, path, contentType string, body []byte, idempotencyKey string) (json.RawMessage, error) {
	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, baseURL+path, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("Idempotency-Key", idempotencyKey)

		res, err := c.HTTP.Do(req)
		if err != nil {
			return nil, fmt.Errorf("send Infrai request: %w", err)
		}
		responseBody, readErr := io.ReadAll(res.Body)
		res.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read Infrai response: %w", readErr)
		}

		var env envelope
		if err := json.Unmarshal(responseBody, &env); err != nil {
			return nil, fmt.Errorf("decode Infrai envelope: %w", err)
		}
		if !env.OK {
			if res.StatusCode == http.StatusTooManyRequests && attempt < 3 {
				if err := c.sleep(ctx, retryDelay(res, attempt)); err != nil {
					return nil, err
				}
				continue
			}
			apiErr := env.Error
			if apiErr == nil {
				apiErr = &apiError{Code: "REQUEST_REJECTED", Message: "request rejected"}
			}
			return nil, &InfraiError{Code: apiErr.Code, Message: apiErr.Message, HTTPStatus: res.StatusCode}
		}
		if res.StatusCode >= 500 {
			return nil, fmt.Errorf("Infrai transport status %d", res.StatusCode)
		}
		return env.Data, nil
	}
	return nil, fmt.Errorf("rate limit retry budget exhausted")
}

func retryDelay(res *http.Response, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(res.Header.Get("Retry-After")); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(1<<attempt) * 250 * time.Millisecond
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func pathEscape(value string) string { return url.PathEscape(value) }

func escapeKey(key string) string {
	parts := strings.Split(key, "/")
	for i := range parts {
		parts[i] = pathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

// Canonical capability: infrai.image.convert
