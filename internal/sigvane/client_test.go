package sigvane

import (
	"context"
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
