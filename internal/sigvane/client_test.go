package sigvane

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cotiq/sigvane-cli/internal/version"
)

func TestNewClientRejectsInvalidBaseURL(t *testing.T) {
	t.Run("rejects malformed url", func(t *testing.T) {
		_, err := NewClient("://bad", "token", nil)
		if err == nil {
			t.Fatal("expected NewClient to reject malformed base URL")
		}
	})

	t.Run("rejects relative url", func(t *testing.T) {
		_, err := NewClient("/relative", "token", nil)
		if err == nil {
			t.Fatal("expected NewClient to reject relative base URL")
		}
	})
}

func TestNewClientAppliesDefaultRequestTimeout(t *testing.T) {
	client, err := NewClient("https://api.example.com", "token", nil)
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	if client.httpClient.Timeout != DefaultRequestTimeout {
		t.Fatalf("http client timeout = %s, want %s", client.httpClient.Timeout, DefaultRequestTimeout)
	}
}

func TestListInboxesUsesResolvedPathAndHeaders(t *testing.T) {
	const apiKey = "test-api-key"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/inboxes" {
			t.Fatalf("request path = %q, want %q", r.URL.Path, "/v1/inboxes")
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Fatalf("Accept header = %q, want %q", got, "application/json")
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+apiKey {
			t.Fatalf("Authorization header = %q, want %q", got, "Bearer "+apiKey)
		}
		if got := r.Header.Get("User-Agent"); got != "sigvane-cli/"+version.Version {
			t.Fatalf("User-Agent header = %q, want %q", got, "sigvane-cli/"+version.Version)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"id":"inbox-1","slug":"github-repo"}]`)
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/", apiKey, server.Client())
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	inboxes, err := client.ListInboxes(context.Background())
	if err != nil {
		t.Fatalf("ListInboxes returned error: %v", err)
	}
	if len(inboxes) != 1 || inboxes[0].ID != "inbox-1" || inboxes[0].Slug != "github-repo" {
		t.Fatalf("inboxes = %#v, want one decoded inbox", inboxes)
	}
}

func TestListInboxesDecodesPagedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"content":[{"id":"inbox-1","slug":"github-repo"}]}`)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "token", server.Client())
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	inboxes, err := client.ListInboxes(context.Background())
	if err != nil {
		t.Fatalf("ListInboxes returned error: %v", err)
	}
	if len(inboxes) != 1 || inboxes[0].ID != "inbox-1" || inboxes[0].Slug != "github-repo" {
		t.Fatalf("inboxes = %#v, want one decoded inbox", inboxes)
	}
}

func TestListInboxItemsSetsCursorQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/inboxes/inbox-1/items" {
			t.Fatalf("request path = %q, want %q", r.URL.Path, "/v1/inboxes/inbox-1/items")
		}
		if got := r.URL.Query().Get("cursor"); got != "cursor-123" {
			t.Fatalf("cursor query = %q, want %q", got, "cursor-123")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"items":[],"nextCursor":null}`)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "token", server.Client())
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	if _, err := client.ListInboxItems(context.Background(), "inbox-1", "cursor-123"); err != nil {
		t.Fatalf("ListInboxItems returned error: %v", err)
	}
}

func TestClaimTaskPostsKindsAndDecodesTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/tasks/claim" {
			t.Fatalf("request path = %q, want %q", r.URL.Path, "/v1/tasks/claim")
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("Content-Type header = %q, want application/json", got)
		}

		var body struct {
			Kinds []string `json:"kinds"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if len(body.Kinds) != 2 || body.Kinds[0] != "github_pr_review" || body.Kinds[1] != "other" {
			t.Fatalf("kinds = %#v, want configured kind set", body.Kinds)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"task-1","kind":"github_pr_review","payload":{"repository":"cotiq/sigvane","pullRequestNumber":174},"attempts":2,"leaseToken":"lease-token","leaseDeadline":"2026-06-06T12:00:00Z"}`)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "token", server.Client())
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	claim, err := client.ClaimTask(context.Background(), []string{"github_pr_review", "other"})
	if err != nil {
		t.Fatalf("ClaimTask returned error: %v", err)
	}
	if !claim.HasTask {
		t.Fatal("ClaimTask HasTask = false, want true")
	}
	if claim.Task.ID != "task-1" || claim.Task.Kind != "github_pr_review" || claim.Task.Attempts != 2 || claim.Task.LeaseToken != "lease-token" {
		t.Fatalf("task = %#v, want decoded task", claim.Task)
	}
	if string(claim.Task.Payload) != `{"repository":"cotiq/sigvane","pullRequestNumber":174}` {
		t.Fatalf("payload = %s, want raw task payload", claim.Task.Payload)
	}
}

func TestClaimTaskNoContentIsEmptyResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/tasks/claim" {
			t.Fatalf("request path = %q, want %q", r.URL.Path, "/v1/tasks/claim")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "token", server.Client())
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	claim, err := client.ClaimTask(context.Background(), []string{"github_pr_review"})
	if err != nil {
		t.Fatalf("ClaimTask returned error: %v", err)
	}
	if claim.HasTask {
		t.Fatalf("ClaimTask HasTask = true, want false")
	}
}

func TestTaskOutcomeMethodsPostLeaseTokenAndReason(t *testing.T) {
	type requestRecord struct {
		Method string
		Path   string
		Body   map[string]string
	}

	var records []requestRecord
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]string{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		records = append(records, requestRecord{
			Method: r.Method,
			Path:   r.URL.Path,
			Body:   body,
		})
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "token", server.Client())
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	if err := client.CompleteTask(context.Background(), "task-1", "lease-token"); err != nil {
		t.Fatalf("CompleteTask returned error: %v", err)
	}
	if err := client.FailTask(context.Background(), "task-2", "lease-token", "failed"); err != nil {
		t.Fatalf("FailTask returned error: %v", err)
	}
	if err := client.RejectTask(context.Background(), "task-3", "lease-token", "declined"); err != nil {
		t.Fatalf("RejectTask returned error: %v", err)
	}

	want := []requestRecord{
		{Method: http.MethodPost, Path: "/v1/tasks/task-1/complete", Body: map[string]string{"leaseToken": "lease-token"}},
		{Method: http.MethodPost, Path: "/v1/tasks/task-2/fail", Body: map[string]string{"leaseToken": "lease-token", "reason": "failed"}},
		{Method: http.MethodPost, Path: "/v1/tasks/task-3/reject", Body: map[string]string{"leaseToken": "lease-token", "reason": "declined"}},
	}
	if len(records) != len(want) {
		t.Fatalf("request count = %d, want %d", len(records), len(want))
	}
	for index := range want {
		if records[index].Method != want[index].Method || records[index].Path != want[index].Path {
			t.Fatalf("request[%d] = %#v, want %#v", index, records[index], want[index])
		}
		for key, wantValue := range want[index].Body {
			if got := records[index].Body[key]; got != wantValue {
				t.Fatalf("request[%d] body[%q] = %q, want %q", index, key, got, wantValue)
			}
		}
		if len(records[index].Body) != len(want[index].Body) {
			t.Fatalf("request[%d] body = %#v, want %#v", index, records[index].Body, want[index].Body)
		}
	}
}

func TestUnexpectedStatusTrimsAndTruncatesBody(t *testing.T) {
	body := " \n" + strings.Repeat("x", 5000) + "\n "
	resp := &http.Response{
		StatusCode: http.StatusBadRequest,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	err := unexpectedStatus(http.MethodGet, "/v1/inboxes", resp)
	statusErr, ok := err.(*HTTPStatusError)
	if !ok {
		t.Fatalf("unexpectedStatus error type = %T, want *HTTPStatusError", err)
	}
	if statusErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("status code = %d, want %d", statusErr.StatusCode, http.StatusBadRequest)
	}
	if len(statusErr.Body) != 4094 {
		t.Fatalf("body length = %d, want %d after trim", len(statusErr.Body), 4094)
	}
	if strings.HasPrefix(statusErr.Body, " ") || strings.HasSuffix(statusErr.Body, " ") {
		t.Fatalf("body should be trimmed, got %q", statusErr.Body[:8])
	}
}
