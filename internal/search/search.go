// Package search asks a SearXNG instance (its JSON API) for web pages, the
// first step of a deal hunt: SearXNG queries several search engines at once
// and needs no API key.
package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Result is one hit.
type Result struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Content string `json:"content"`
	Engine  string `json:"engine"`
	Image   string `json:"thumbnail"`
}

// Client talks to one SearXNG.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// New creates a client; an empty base URL means search is off.
func New(base string) *Client {
	return &Client{BaseURL: strings.TrimRight(strings.TrimSpace(base), "/"), HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// Configured reports whether an address is set.
func (c *Client) Configured() bool { return c != nil && c.BaseURL != "" }

// Search runs one query. lang is a SearXNG language ("de", "en", "all"…).
func (c *Client) Search(ctx context.Context, q, lang string) ([]Result, error) {
	if !c.Configured() {
		return nil, errors.New("no search engine is set up")
	}
	v := url.Values{"q": {q}, "format": {"json"}, "safesearch": {"0"}}
	if lang != "" {
		v.Set("language", lang)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/search?"+v.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusForbidden {
		return nil, errors.New("the search engine refused JSON (enable `formats: [html, json]` in SearXNG's settings.yml)")
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search engine HTTP %d", res.StatusCode)
	}
	var out struct {
		Results []Result `json:"results"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("search engine answer: %w", err)
	}
	return out.Results, nil
}
