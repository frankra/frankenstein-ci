package rest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type ReadClient[Res any] interface {
	GetJSON(ctx context.Context, url string, headers map[string]string) (*Res, error)
}

type client[Res any] struct {
	http.Client
}

func NewReadClient[Res any]() ReadClient[Res] {
	return &client[Res]{
		http.Client{},
	}
}

func (c *client[Res]) GetJSON(ctx context.Context, url string, headers map[string]string) (*Res, error) {
	res := new(Res)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return res, fmt.Errorf("failed to create request, %w", err)
	}
	// spread headers
	for key, value := range headers {
		req.Header.Add(key, value)
	}

	raw, err := c.Do(req)
	if err != nil {
		return res, fmt.Errorf("failed to execute the request, %w", err)
	}
	defer raw.Body.Close()

	if raw.StatusCode != http.StatusOK {
		return res, fmt.Errorf("unexpected status code: %d", raw.StatusCode)
	}

	body, err := io.ReadAll(raw.Body)
	if err != nil {
		return res, fmt.Errorf("failed to read the body, %w", err)
	}

	err = json.Unmarshal(body, res)
	if err != nil {
		return res, fmt.Errorf("failed to unmarshal the body as object, %w", err)
	}

	return res, nil
}
