package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"
)

type gatewayError struct {
	Status                int
	RequestID, RetryAfter string
}

func (e *gatewayError) Error() string {
	message := fmt.Sprintf("gateway returned HTTP %d", e.Status)
	if e.RequestID != "" {
		message += "; request ID " + e.RequestID
	}
	if e.RetryAfter != "" {
		message += "; Retry-After " + e.RetryAfter
	}
	return message
}

var safeRequestID = regexp.MustCompile(`^[a-fA-F0-9-]{16,64}$`)
var gatewayHTTP = &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}

func request(key, method, endpoint string, body []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if endpoint == "" || endpoint[0] != '/' {
		return nil, errors.New("invalid gateway path")
	}
	attempts := 1
	if method == "GET" {
		attempts = 3
	}
	for attempt := 0; attempt < attempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, origin+endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, errors.New("invalid request")
		}
		req.Header.Set("User-Agent", "PhasesAI-Setup/"+version)
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		if endpoint == "/v1/messages" {
			req.Header.Set("anthropic-version", "2023-06-01")
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		res, err := gatewayHTTP.Do(req)
		if err != nil {
			return nil, errors.New("network request failed; check connectivity and TLS")
		}
		data, readErr := io.ReadAll(io.LimitReader(res.Body, 2<<20+1))
		res.Body.Close()
		if readErr != nil || len(data) > 2<<20 {
			return nil, errors.New("response unreadable or larger than 2 MiB")
		}
		if res.StatusCode >= 200 && res.StatusCode < 300 {
			return data, nil
		}
		failure := &gatewayError{Status: res.StatusCode}
		if id := res.Header.Get("x-request-id"); safeRequestID.MatchString(id) {
			failure.RequestID = id
		}
		delay := time.Duration(attempt+1) * 250 * time.Millisecond
		if seconds, err := strconv.Atoi(res.Header.Get("Retry-After")); err == nil && seconds >= 0 && seconds <= 5 {
			failure.RetryAfter = strconv.Itoa(seconds)
			delay = time.Duration(seconds) * time.Second
		}
		if attempt+1 >= attempts || (res.StatusCode != 429 && res.StatusCode != 502 && res.StatusCode != 503 && res.StatusCode != 504) {
			return nil, failure
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, errors.New("request deadline exceeded")
		case <-timer.C:
		}
	}
	return nil, errors.New("request failed")
}

type catalogModel struct {
	ID             string   `json:"id"`
	MaxInputTokens int      `json:"max_input_tokens"`
	MaxTokens      int      `json:"max_tokens"`
	EndpointTypes  []string `json:"supported_endpoint_types"`
}

func fetchCatalog() ([]catalogModel, error) {
	var models []catalogModel
	seen := map[string]bool{}
	cursors := map[string]bool{}
	cursor := ""
	for page := 0; page < 100; page++ {
		endpoint := "/v1/models?limit=1000"
		if cursor != "" {
			endpoint += "&after_id=" + url.QueryEscape(cursor)
		}
		data, err := request("", "GET", endpoint, nil)
		if err != nil {
			return nil, err
		}
		var response struct {
			Data    []catalogModel `json:"data"`
			HasMore bool           `json:"has_more"`
			LastID  string         `json:"last_id"`
		}
		if json.Unmarshal(data, &response) != nil {
			return nil, errors.New("invalid model catalog")
		}
		for _, model := range response.Data {
			if model.ID == "" || len(model.ID) > 512 {
				return nil, errors.New("invalid model ID in catalog")
			}
			if !seen[model.ID] {
				seen[model.ID] = true
				models = append(models, model)
			}
		}
		if !response.HasMore {
			if len(models) == 0 {
				return nil, errors.New("catalog is empty")
			}
			return models, nil
		}
		if response.LastID == "" || len(response.Data) == 0 || cursors[response.LastID] {
			return nil, errors.New("catalog pagination did not advance")
		}
		cursor = response.LastID
		cursors[cursor] = true
	}
	return nil, errors.New("catalog exceeded 100 pages")
}
