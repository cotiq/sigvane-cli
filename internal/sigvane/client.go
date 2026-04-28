// Package sigvane contains a minimal HTTP client for the Sigvane APIs used by the CLI.
package sigvane

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/cotiq/sigvane-cli/internal/version"
)

// Client talks to the Sigvane management and inbox feed APIs.
type Client struct {
	baseURL    *url.URL
	apiKey     string
	httpClient *http.Client
}

// Inbox is the subset of inbox metadata the CLI needs for slug resolution.
type Inbox struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
}

// InboxItem is the published low-level inbox item contract returned by the feed API.
type InboxItem struct {
	ID                 string              `json:"id"`
	InboxID            string              `json:"inboxId"`
	Inbox              string              `json:"inbox"`
	RecordedAt         string              `json:"recordedAt"`
	Headers            map[string][]string `json:"headers"`
	Body               string              `json:"body"`
	ProviderDeliveryID *string             `json:"providerDeliveryId"`
}

// FeedResponse is one page from the inbox feed API.
type FeedResponse struct {
	Items []InboxItem `json:"items"`
}

// HTTPStatusError reports a non-2xx HTTP response from the Sigvane API.
type HTTPStatusError struct {
	Method     string
	Path       string
	StatusCode int
	Body       string
}

func (e *HTTPStatusError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("%s %s returned %d", e.Method, e.Path, e.StatusCode)
	}

	return fmt.Sprintf("%s %s returned %d: %s", e.Method, e.Path, e.StatusCode, e.Body)
}

// NewClient constructs a Sigvane API client.
func NewClient(baseURL string, apiKey string, httpClient *http.Client) (*Client, error) {
	parsedBaseURL, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base URL %q: %w", baseURL, err)
	}
	if parsedBaseURL.Scheme == "" || parsedBaseURL.Host == "" {
		return nil, fmt.Errorf("base URL %q must be absolute", baseURL)
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	return &Client{
		baseURL:    parsedBaseURL,
		apiKey:     apiKey,
		httpClient: httpClient,
	}, nil
}

// ListInboxes returns the authenticated account's inboxes.
func (c *Client) ListInboxes(ctx context.Context) ([]Inbox, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/v1/inboxes", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list inboxes: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, unexpectedStatus(http.MethodGet, "/v1/inboxes", resp)
	}

	inboxes, err := decodeInboxList(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("decode inbox list response: %w", err)
	}

	return inboxes, nil
}

func decodeInboxList(reader io.Reader) ([]Inbox, error) {
	var raw json.RawMessage
	if err := json.NewDecoder(reader).Decode(&raw); err != nil {
		return nil, err
	}

	var response struct {
		Content []Inbox `json:"content"`
	}
	if err := json.Unmarshal(raw, &response); err == nil && response.Content != nil {
		return response.Content, nil
	}

	var inboxes []Inbox
	if err := json.Unmarshal(raw, &inboxes); err != nil {
		return nil, err
	}

	return inboxes, nil
}

// ListInboxItems fetches one feed page for an inbox, optionally resuming after a cursor.
func (c *Client) ListInboxItems(ctx context.Context, inboxID string, cursor string) (FeedResponse, error) {
	query := url.Values{}
	if cursor != "" {
		query.Set("cursor", cursor)
	}

	path := fmt.Sprintf("/v1/inboxes/%s/items", inboxID)
	req, err := c.newRequest(ctx, http.MethodGet, path, query)
	if err != nil {
		return FeedResponse{}, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return FeedResponse{}, fmt.Errorf("list inbox items for %q: %w", inboxID, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return FeedResponse{}, unexpectedStatus(http.MethodGet, path, resp)
	}

	var feed FeedResponse
	if err := json.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return FeedResponse{}, fmt.Errorf("decode inbox feed response: %w", err)
	}

	return feed, nil
}

func (c *Client) newRequest(ctx context.Context, method string, path string, query url.Values) (*http.Request, error) {
	endpoint := c.baseURL.ResolveReference(&url.URL{
		Path:     path,
		RawQuery: query.Encode(),
	})

	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build request %s %s: %w", method, endpoint.String(), err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("User-Agent", "sigvane-cli/"+version.Version)

	return req, nil
}

func unexpectedStatus(method string, path string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return &HTTPStatusError{
		Method:     method,
		Path:       path,
		StatusCode: resp.StatusCode,
		Body:       strings.TrimSpace(string(body)),
	}
}
