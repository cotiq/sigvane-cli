package commands

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/cotiq/sigvane-cli/internal/sigvane"
	"github.com/cotiq/sigvane-cli/internal/state"
)

func TestInboxPollOnceDrainsUntilEmptyAndAdvancesState(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "sigvane.yaml")
	statePath := filepath.Join(tempDir, "state", "state.json")
	orderLogPath := filepath.Join(tempDir, "order.log")
	t.Setenv("SIGVANE_API_KEY", "test-api-key")
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")

	const githubInboxID = "00000000-0000-7000-8000-000000000001"
	const githubItemID = "00000000-0000-7000-8000-000000000123"
	const billingInboxID = "00000000-0000-7000-8000-000000000002"
	const billingItemID = "00000000-0000-7000-8000-000000000124"
	var mu sync.Mutex
	requestCount := map[string]int{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-api-key" {
			t.Fatalf("Authorization header = %q, want %q", got, "Bearer test-api-key")
		}

		switch r.URL.Path {
		case "/v1/inboxes":
			if r.Method != http.MethodGet {
				t.Fatalf("GET /v1/inboxes method = %s, want GET", r.Method)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `[{"id":"`+githubInboxID+`","slug":"github-repo","provider":"github","createdAt":"2026-04-01T10:00:00Z","updatedAt":"2026-04-01T10:00:00Z"},{"id":"`+billingInboxID+`","slug":"billing-webhooks","provider":"github","createdAt":"2026-04-01T10:01:00Z","updatedAt":"2026-04-01T10:01:00Z"}]`)
		case "/v1/inboxes/" + githubInboxID + "/items":
			if r.Method != http.MethodGet {
				t.Fatalf("GET /v1/inboxes/{id}/items method = %s, want GET", r.Method)
			}
			w.Header().Set("Content-Type", "application/json")
			mu.Lock()
			currentRequest := requestCount["github-repo"]
			requestCount["github-repo"]++
			mu.Unlock()
			switch currentRequest {
			case 0:
				if got := r.URL.Query().Get("cursor"); got != "" {
					t.Fatalf("cursor = %q, want empty cursor on first run", got)
				}
				_, _ = io.WriteString(w, `{"items":[{"id":"`+githubItemID+`","inboxId":"`+githubInboxID+`","inbox":"github-repo","recordedAt":"2026-04-03T10:05:00Z","headers":{"content-type":["application/json"]},"body":"eyJhY3Rpb24iOiJvcGVuZWQifQ==","providerDeliveryId":"delivery-1"}],"nextCursor":"`+githubItemID+`"}`)
			case 1:
				if got := r.URL.Query().Get("cursor"); got != githubItemID {
					t.Fatalf("cursor = %q, want %q after first handled item", got, githubItemID)
				}
				_, _ = io.WriteString(w, `{"items":[],"nextCursor":"`+githubItemID+`"}`)
			default:
				t.Fatalf("unexpected github feed request count %d", currentRequest+1)
			}
		case "/v1/inboxes/" + billingInboxID + "/items":
			if r.Method != http.MethodGet {
				t.Fatalf("GET /v1/inboxes/{id}/items method = %s, want GET", r.Method)
			}
			w.Header().Set("Content-Type", "application/json")
			mu.Lock()
			currentRequest := requestCount["billing-webhooks"]
			requestCount["billing-webhooks"]++
			mu.Unlock()
			switch currentRequest {
			case 0:
				if got := r.URL.Query().Get("cursor"); got != "" {
					t.Fatalf("cursor = %q, want empty cursor on first run", got)
				}
				_, _ = io.WriteString(w, `{"items":[{"id":"`+billingItemID+`","inboxId":"`+billingInboxID+`","inbox":"billing-webhooks","recordedAt":"2026-04-03T10:06:00Z","headers":{"content-type":["application/json"]},"body":"eyJhY3Rpb24iOiJwYWlkIn0=","providerDeliveryId":"delivery-2"}],"nextCursor":"`+billingItemID+`"}`)
			case 1:
				if got := r.URL.Query().Get("cursor"); got != billingItemID {
					t.Fatalf("cursor = %q, want %q after first handled item", got, billingItemID)
				}
				_, _ = io.WriteString(w, `{"items":[],"nextCursor":"`+billingItemID+`"}`)
			default:
				t.Fatalf("unexpected billing feed request count %d", currentRequest+1)
			}
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	writeTestFile(t, configPath, `
version: 1
server:
  url: `+server.URL+`
  api_key: ${SIGVANE_API_KEY}
handlers:
  - inbox: github-repo
    command: ["`+os.Args[0]+`", "-test.run=TestHelperProcess", "--", "append-line", "`+orderLogPath+`", "github-repo"]
    stdin: none
  - inbox: billing-webhooks
    command: ["`+os.Args[0]+`", "-test.run=TestHelperProcess", "--", "append-line", "`+orderLogPath+`", "billing-webhooks"]
    stdin: none
`)

	stdout, stderr, err := executeCommand(
		"inbox",
		"poll",
		"--config", configPath,
		"--once",
		"--state", statePath,
	)
	if err != nil {
		t.Fatalf("inbox poll returned error: %v", err)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty output", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty output", stderr)
	}

	orderLog, err := os.ReadFile(orderLogPath)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", orderLogPath, err)
	}
	if string(orderLog) != "github-repo\nbilling-webhooks\n" {
		t.Fatalf("handler order = %q, want %q", string(orderLog), "github-repo\nbilling-webhooks\n")
	}

	currentState, err := state.Load(statePath)
	if err != nil {
		t.Fatalf("state.Load returned error: %v", err)
	}
	githubEntry := currentState["github-repo"]
	if githubEntry.InboxID != githubInboxID {
		t.Fatalf("github state inbox_id = %q, want %q", githubEntry.InboxID, githubInboxID)
	}
	if githubEntry.LastItemID != githubItemID {
		t.Fatalf("github state last_item_id = %q, want %q", githubEntry.LastItemID, githubItemID)
	}
	billingEntry := currentState["billing-webhooks"]
	if billingEntry.InboxID != billingInboxID {
		t.Fatalf("billing state inbox_id = %q, want %q", billingEntry.InboxID, billingInboxID)
	}
	if billingEntry.LastItemID != billingItemID {
		t.Fatalf("billing state last_item_id = %q, want %q", billingEntry.LastItemID, billingItemID)
	}

	mu.Lock()
	defer mu.Unlock()
	if requestCount["github-repo"] != 2 {
		t.Fatalf("github feed request count = %d, want 2", requestCount["github-repo"])
	}
	if requestCount["billing-webhooks"] != 2 {
		t.Fatalf("billing feed request count = %d, want 2", requestCount["billing-webhooks"])
	}
}

func TestInboxPollOnceWithSlugPollsOnlySelectedHandler(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "sigvane.yaml")
	statePath := filepath.Join(tempDir, "state", "state.json")
	orderLogPath := filepath.Join(tempDir, "order.log")
	t.Setenv("SIGVANE_API_KEY", "test-api-key")
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")

	const githubInboxID = "00000000-0000-7000-8000-000000000001"
	const githubItemID = "00000000-0000-7000-8000-000000000123"
	const billingInboxID = "00000000-0000-7000-8000-000000000002"

	var mu sync.Mutex
	requestCount := map[string]int{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-api-key" {
			t.Fatalf("Authorization header = %q, want %q", got, "Bearer test-api-key")
		}

		switch r.URL.Path {
		case "/v1/inboxes":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `[{"id":"`+githubInboxID+`","slug":"github-repo","provider":"github","createdAt":"2026-04-01T10:00:00Z","updatedAt":"2026-04-01T10:00:00Z"},{"id":"`+billingInboxID+`","slug":"billing-webhooks","provider":"github","createdAt":"2026-04-01T10:01:00Z","updatedAt":"2026-04-01T10:01:00Z"}]`)
		case "/v1/inboxes/" + githubInboxID + "/items":
			w.Header().Set("Content-Type", "application/json")
			mu.Lock()
			currentRequest := requestCount["github-repo"]
			requestCount["github-repo"]++
			mu.Unlock()
			switch currentRequest {
			case 0:
				_, _ = io.WriteString(w, `{"items":[{"id":"`+githubItemID+`","inboxId":"`+githubInboxID+`","inbox":"github-repo","recordedAt":"2026-04-03T10:05:00Z","headers":{"content-type":["application/json"]},"body":"eyJhY3Rpb24iOiJvcGVuZWQifQ==","providerDeliveryId":"delivery-1"}],"nextCursor":"`+githubItemID+`"}`)
			case 1:
				_, _ = io.WriteString(w, `{"items":[],"nextCursor":"`+githubItemID+`"}`)
			default:
				t.Fatalf("unexpected github feed request count %d", currentRequest+1)
			}
		case "/v1/inboxes/" + billingInboxID + "/items":
			t.Fatal("billing handler should not be polled when github-repo is explicitly selected")
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	writeTestFile(t, configPath, `
version: 1
server:
  url: `+server.URL+`
  api_key: ${SIGVANE_API_KEY}
handlers:
  - inbox: github-repo
    command: ["`+os.Args[0]+`", "-test.run=TestHelperProcess", "--", "append-line", "`+orderLogPath+`", "github-repo"]
    stdin: none
  - inbox: billing-webhooks
    command: ["`+os.Args[0]+`", "-test.run=TestHelperProcess", "--", "append-line", "`+orderLogPath+`", "billing-webhooks"]
    stdin: none
`)

	_, _, err := executeCommand(
		"inbox",
		"poll",
		"--config", configPath,
		"github-repo",
		"--once",
		"--state", statePath,
	)
	if err != nil {
		t.Fatalf("inbox poll returned error: %v", err)
	}

	orderLog, err := os.ReadFile(orderLogPath)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", orderLogPath, err)
	}
	if string(orderLog) != "github-repo\n" {
		t.Fatalf("handler order = %q, want %q", string(orderLog), "github-repo\n")
	}

	currentState, err := state.Load(statePath)
	if err != nil {
		t.Fatalf("state.Load returned error: %v", err)
	}
	if _, exists := currentState["billing-webhooks"]; exists {
		t.Fatal("billing state should not be written when github-repo is explicitly selected")
	}

	mu.Lock()
	defer mu.Unlock()
	if requestCount["github-repo"] != 2 {
		t.Fatalf("github feed request count = %d, want 2", requestCount["github-repo"])
	}
}

func TestInboxPollHandlerStdinModes(t *testing.T) {
	t.Setenv("SIGVANE_API_KEY", "test-api-key")
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")

	const inboxID = "00000000-0000-7000-8000-000000000001"
	const itemID = "00000000-0000-7000-8000-000000000123"
	const providerDeliveryID = "delivery-1"

	bodyBytes := []byte(`{"action":"opened"}`)
	bodyBase64 := base64.StdEncoding.EncodeToString(bodyBytes)

	tests := []struct {
		name   string
		mode   string
		assert func(t *testing.T, stdin []byte)
	}{
		{
			name: "full item",
			mode: "full_item",
			assert: func(t *testing.T, stdin []byte) {
				t.Helper()

				var item sigvane.InboxItem
				if err := json.Unmarshal(stdin, &item); err != nil {
					t.Fatalf("json.Unmarshal(stdin): %v", err)
				}
				if item.ID != itemID {
					t.Fatalf("item id = %q, want %q", item.ID, itemID)
				}
				if item.InboxID != inboxID {
					t.Fatalf("item inboxId = %q, want %q", item.InboxID, inboxID)
				}
				if item.Inbox != "github-repo" {
					t.Fatalf("item inbox = %q, want %q", item.Inbox, "github-repo")
				}
				if item.RecordedAt != "2026-04-03T10:05:00Z" {
					t.Fatalf("item recordedAt = %q, want %q", item.RecordedAt, "2026-04-03T10:05:00Z")
				}
				if got := item.Headers["content-type"]; len(got) != 1 || got[0] != "application/json" {
					t.Fatalf("item headers[content-type] = %v, want [application/json]", got)
				}
				if item.Body != bodyBase64 {
					t.Fatalf("item body = %q, want %q", item.Body, bodyBase64)
				}
				if item.ProviderDeliveryID == nil || *item.ProviderDeliveryID != providerDeliveryID {
					t.Fatalf("item providerDeliveryId = %v, want %q", item.ProviderDeliveryID, providerDeliveryID)
				}
			},
		},
		{
			name: "body",
			mode: "body",
			assert: func(t *testing.T, stdin []byte) {
				t.Helper()

				if !bytes.Equal(stdin, bodyBytes) {
					t.Fatalf("stdin = %q, want %q", stdin, bodyBytes)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tempDir := t.TempDir()
			configPath := filepath.Join(tempDir, "sigvane.yaml")
			statePath := filepath.Join(tempDir, "state", "state.json")
			stdinPath := filepath.Join(tempDir, "stdin.out")

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("Authorization"); got != "Bearer test-api-key" {
					t.Fatalf("Authorization header = %q, want %q", got, "Bearer test-api-key")
				}

				switch r.URL.Path {
				case "/v1/inboxes":
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, `[{"id":"`+inboxID+`","slug":"github-repo","provider":"github","createdAt":"2026-04-01T10:00:00Z","updatedAt":"2026-04-01T10:00:00Z"}]`)
				case "/v1/inboxes/" + inboxID + "/items":
					w.Header().Set("Content-Type", "application/json")
					switch got := r.URL.Query().Get("cursor"); got {
					case "":
						_, _ = io.WriteString(w, `{"items":[{"id":"`+itemID+`","inboxId":"`+inboxID+`","inbox":"github-repo","recordedAt":"2026-04-03T10:05:00Z","headers":{"content-type":["application/json"]},"body":"`+bodyBase64+`","providerDeliveryId":"`+providerDeliveryID+`"}],"nextCursor":"`+itemID+`"}`)
					case itemID:
						_, _ = io.WriteString(w, `{"items":[],"nextCursor":"`+itemID+`"}`)
					default:
						t.Fatalf("cursor = %q, want empty cursor or %q", got, itemID)
					}
				default:
					t.Fatalf("unexpected request path %q", r.URL.Path)
				}
			}))
			defer server.Close()

			writeTestFile(t, configPath, `
version: 1
server:
  url: `+server.URL+`
  api_key: ${SIGVANE_API_KEY}
handlers:
  - inbox: github-repo
    command: ["`+os.Args[0]+`", "-test.run=TestHelperProcess", "--", "write-stdin", "`+stdinPath+`"]
    stdin: `+tc.mode+`
`)

			stdout, stderr, err := executeCommand(
				"inbox",
				"poll",
				"--config", configPath,
				"--once",
				"--state", statePath,
			)
			if err != nil {
				t.Fatalf("inbox poll returned error: %v", err)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty output", stdout)
			}
			if stderr != "" {
				t.Fatalf("stderr = %q, want empty output", stderr)
			}

			stdin, err := os.ReadFile(stdinPath)
			if err != nil {
				t.Fatalf("ReadFile(%q): %v", stdinPath, err)
			}
			tc.assert(t, stdin)
		})
	}
}

func TestInboxPollContinuousReloopsImmediatelyThenSleeps(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "sigvane.yaml")
	statePath := filepath.Join(tempDir, "state", "state.json")
	t.Setenv("SIGVANE_API_KEY", "test-api-key")

	const inboxID = "00000000-0000-7000-8000-000000000001"
	const itemID = "00000000-0000-7000-8000-000000000123"
	const pollInterval = 17 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	requestCount := 0
	sleepCalls := 0

	previousSleep := sleepContext
	sleepContext = func(ctx context.Context, d time.Duration) error {
		if d != pollInterval {
			t.Fatalf("sleep duration = %s, want %s", d, pollInterval)
		}
		mu.Lock()
		sleepCalls++
		mu.Unlock()
		cancel()
		return ctx.Err()
	}
	defer func() {
		sleepContext = previousSleep
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-api-key" {
			t.Fatalf("Authorization header = %q, want %q", got, "Bearer test-api-key")
		}

		switch r.URL.Path {
		case "/v1/inboxes":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `[{"id":"`+inboxID+`","slug":"github-repo","provider":"github","createdAt":"2026-04-01T10:00:00Z","updatedAt":"2026-04-01T10:00:00Z"}]`)
		case "/v1/inboxes/" + inboxID + "/items":
			w.Header().Set("Content-Type", "application/json")
			mu.Lock()
			currentRequest := requestCount
			requestCount++
			mu.Unlock()
			switch currentRequest {
			case 0:
				if got := r.URL.Query().Get("cursor"); got != "" {
					t.Fatalf("cursor = %q, want empty cursor on first run", got)
				}
				_, _ = io.WriteString(w, `{"items":[{"id":"`+itemID+`","inboxId":"`+inboxID+`","inbox":"github-repo","recordedAt":"2026-04-03T10:05:00Z","headers":{"content-type":["application/json"]},"body":"eyJhY3Rpb24iOiJvcGVuZWQifQ==","providerDeliveryId":"delivery-1"}],"nextCursor":"`+itemID+`"}`)
			case 1:
				if got := r.URL.Query().Get("cursor"); got != itemID {
					t.Fatalf("cursor = %q, want %q after first handled item", got, itemID)
				}
				_, _ = io.WriteString(w, `{"items":[],"nextCursor":"`+itemID+`"}`)
			default:
				t.Fatalf("unexpected feed request count %d", currentRequest+1)
			}
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	writeTestFile(t, configPath, `
version: 1
server:
  url: `+server.URL+`
  api_key: ${SIGVANE_API_KEY}
  poll_interval: 17s
handlers:
  - inbox: github-repo
    command: ["/usr/bin/true"]
    stdin: none
`)

	_, _, err := executeCommandWithContext(
		ctx,
		"inbox",
		"poll",
		"--config", configPath,
		"--state", statePath,
	)
	if err != nil {
		t.Fatalf("expected graceful shutdown on context cancellation, got error: %v", err)
	}

	currentState, err := state.Load(statePath)
	if err != nil {
		t.Fatalf("state.Load returned error: %v", err)
	}
	entry := currentState["github-repo"]
	if entry.LastItemID != itemID {
		t.Fatalf("state last_item_id = %q, want %q", entry.LastItemID, itemID)
	}

	mu.Lock()
	defer mu.Unlock()
	if requestCount != 2 {
		t.Fatalf("feed request count = %d, want 2", requestCount)
	}
	if sleepCalls != 1 {
		t.Fatalf("sleep call count = %d, want 1", sleepCalls)
	}
}

func TestInboxPollWarnsOnOrphanedStateEntries(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "sigvane.yaml")
	statePath := filepath.Join(tempDir, "state", "state.json")
	t.Setenv("SIGVANE_API_KEY", "test-api-key")

	const inboxID = "00000000-0000-7000-8000-000000000001"

	if err := state.Save(statePath, state.File{
		"orphaned-handler": {
			InboxID:    "00000000-0000-7000-8000-000000000099",
			LastItemID: "00000000-0000-7000-8000-000000000199",
		},
	}); err != nil {
		t.Fatalf("state.Save returned error: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/inboxes":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `[{"id":"`+inboxID+`","slug":"github-repo","provider":"github","createdAt":"2026-04-01T10:00:00Z","updatedAt":"2026-04-01T10:00:00Z"}]`)
		case "/v1/inboxes/" + inboxID + "/items":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"items":[],"nextCursor":null}`)
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	writeTestFile(t, configPath, `
version: 1
server:
  url: `+server.URL+`
  api_key: ${SIGVANE_API_KEY}
handlers:
  - inbox: github-repo
    command: ["/usr/bin/true"]
    stdin: none
`)

	_, stderr, err := executeCommand(
		"inbox",
		"poll",
		"--config", configPath,
		"--once",
		"--state", statePath,
	)
	if err != nil {
		t.Fatalf("inbox poll returned error: %v", err)
	}
	if !strings.Contains(stderr, `warning: ignoring orphaned state entry "orphaned-handler"; no matching handler is present in config. remove it with: sigvane state reset orphaned-handler`) {
		t.Fatalf("stderr = %q, want orphaned handler warning", stderr)
	}
}

func TestInboxPollFailsOnStateInboxIDMismatch(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "sigvane.yaml")
	statePath := filepath.Join(tempDir, "state", "state.json")
	t.Setenv("SIGVANE_API_KEY", "test-api-key")

	const stateInboxID = "00000000-0000-7000-8000-000000000001"
	const resolvedInboxID = "00000000-0000-7000-8000-000000000042"
	itemsPolled := false

	if err := state.Save(statePath, state.File{
		"github-repo": {
			InboxID:    stateInboxID,
			LastItemID: "00000000-0000-7000-8000-000000000123",
		},
	}); err != nil {
		t.Fatalf("state.Save returned error: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/inboxes":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `[{"id":"`+resolvedInboxID+`","slug":"github-repo","provider":"github","createdAt":"2026-04-01T10:00:00Z","updatedAt":"2026-04-01T10:00:00Z"}]`)
		case "/v1/inboxes/" + resolvedInboxID + "/items":
			itemsPolled = true
			t.Fatal("items endpoint should not be called after inbox_id mismatch")
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	writeTestFile(t, configPath, `
version: 1
server:
  url: `+server.URL+`
  api_key: ${SIGVANE_API_KEY}
handlers:
  - inbox: github-repo
    command: ["/usr/bin/true"]
    stdin: none
`)

	_, _, err := executeCommand(
		"inbox",
		"poll",
		"--config", configPath,
		"--once",
		"--state", statePath,
	)
	if err == nil {
		t.Fatal("expected inbox poll to fail on state/config inbox_id mismatch")
	}
	if !strings.Contains(err.Error(), `handler "github-repo": inbox_id in state (`+stateInboxID+`) does not match resolved inbox id (`+resolvedInboxID+`)`) {
		t.Fatalf("error = %q, want inbox_id mismatch message", err.Error())
	}
	if !strings.Contains(err.Error(), `reset this cursor with: sigvane state reset github-repo`) {
		t.Fatalf("error = %q, want state reset guidance", err.Error())
	}
	if itemsPolled {
		t.Fatal("items endpoint should not be polled after mismatch")
	}
}

func TestInboxPollOnceRetriesTransientFeedErrorsWithBackoff(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "sigvane.yaml")
	statePath := filepath.Join(tempDir, "state", "state.json")
	orderLogPath := filepath.Join(tempDir, "order.log")
	t.Setenv("SIGVANE_API_KEY", "test-api-key")
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")

	const inboxID = "00000000-0000-7000-8000-000000000001"
	const itemID = "00000000-0000-7000-8000-000000000123"

	var mu sync.Mutex
	requestCount := 0
	sleeps := make([]time.Duration, 0, 1)

	previousSleep := sleepContext
	sleepContext = func(_ context.Context, d time.Duration) error {
		sleeps = append(sleeps, d)
		return nil
	}
	defer func() {
		sleepContext = previousSleep
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/inboxes":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `[{"id":"`+inboxID+`","slug":"github-repo","provider":"github","createdAt":"2026-04-01T10:00:00Z","updatedAt":"2026-04-01T10:00:00Z"}]`)
		case "/v1/inboxes/" + inboxID + "/items":
			mu.Lock()
			currentRequest := requestCount
			requestCount++
			mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			switch currentRequest {
			case 0:
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = io.WriteString(w, `rate limited`)
			case 1:
				_, _ = io.WriteString(w, `{"items":[{"id":"`+itemID+`","inboxId":"`+inboxID+`","inbox":"github-repo","recordedAt":"2026-04-03T10:05:00Z","headers":{"content-type":["application/json"]},"body":"eyJhY3Rpb24iOiJvcGVuZWQifQ==","providerDeliveryId":"delivery-1"}],"nextCursor":"`+itemID+`"}`)
			case 2:
				_, _ = io.WriteString(w, `{"items":[],"nextCursor":"`+itemID+`"}`)
			default:
				t.Fatalf("unexpected feed request count %d", currentRequest+1)
			}
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	writeTestFile(t, configPath, `
version: 1
server:
  url: `+server.URL+`
  api_key: ${SIGVANE_API_KEY}
handlers:
  - inbox: github-repo
    command: ["`+os.Args[0]+`", "-test.run=TestHelperProcess", "--", "append-line", "`+orderLogPath+`", "github-repo"]
    stdin: none
`)

	_, stderr, err := executeCommand(
		"inbox",
		"poll",
		"--config", configPath,
		"--once",
		"--state", statePath,
	)
	if err != nil {
		t.Fatalf("inbox poll returned error: %v", err)
	}
	if !strings.Contains(stderr, `warning: transient feed error for inbox "github-repo"`) {
		t.Fatalf("stderr = %q, want transient backoff warning", stderr)
	}

	currentState, err := state.Load(statePath)
	if err != nil {
		t.Fatalf("state.Load returned error: %v", err)
	}
	if currentState["github-repo"].LastItemID != itemID {
		t.Fatalf("state last_item_id = %q, want %q", currentState["github-repo"].LastItemID, itemID)
	}
	if len(sleeps) != 1 || sleeps[0] != time.Second {
		t.Fatalf("backoff sleeps = %#v, want [1s]", sleeps)
	}

	mu.Lock()
	defer mu.Unlock()
	if requestCount != 3 {
		t.Fatalf("feed request count = %d, want 3", requestCount)
	}
}

func TestInboxPollFailsFastOnFatalFeedStatus(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "sigvane.yaml")
	statePath := filepath.Join(tempDir, "state", "state.json")
	t.Setenv("SIGVANE_API_KEY", "test-api-key")

	const inboxID = "00000000-0000-7000-8000-000000000001"
	sleepCalled := false

	previousSleep := sleepContext
	sleepContext = func(_ context.Context, d time.Duration) error {
		sleepCalled = true
		return fmt.Errorf("unexpected sleep for duration %s", d)
	}
	defer func() {
		sleepContext = previousSleep
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/inboxes":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `[{"id":"`+inboxID+`","slug":"github-repo","provider":"github","createdAt":"2026-04-01T10:00:00Z","updatedAt":"2026-04-01T10:00:00Z"}]`)
		case "/v1/inboxes/" + inboxID + "/items":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `invalid cursor`)
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	writeTestFile(t, configPath, `
version: 1
server:
  url: `+server.URL+`
  api_key: ${SIGVANE_API_KEY}
handlers:
  - inbox: github-repo
    command: ["/usr/bin/true"]
    stdin: none
`)

	_, _, err := executeCommand(
		"inbox",
		"poll",
		"--config", configPath,
		"--once",
		"--state", statePath,
	)
	if err == nil {
		t.Fatal("expected inbox poll to fail on fatal feed status")
	}
	if !strings.Contains(err.Error(), `returned 400: invalid cursor`) {
		t.Fatalf("error = %q, want fatal feed status message", err.Error())
	}
	if sleepCalled {
		t.Fatal("backoff sleep should not run for fatal feed status")
	}
}

func TestInboxPollLogsHandlerFailureAndPreservesCursor(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "sigvane.yaml")
	statePath := filepath.Join(tempDir, "state", "state.json")
	orderLogPath := filepath.Join(tempDir, "order.log")
	t.Setenv("SIGVANE_API_KEY", "test-api-key")
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")

	const githubInboxID = "00000000-0000-7000-8000-000000000001"
	const githubItemID = "00000000-0000-7000-8000-000000000123"
	const billingInboxID = "00000000-0000-7000-8000-000000000002"
	const billingItemID = "00000000-0000-7000-8000-000000000124"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	requestCount := map[string]int{}
	sleepCalls := 0

	previousSleep := sleepContext
	sleepContext = func(ctx context.Context, d time.Duration) error {
		if d != 5*time.Second {
			t.Fatalf("sleep duration = %s, want %s", d, 5*time.Second)
		}
		sleepCalls++
		cancel()
		return ctx.Err()
	}
	defer func() {
		sleepContext = previousSleep
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/inboxes":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `[{"id":"`+githubInboxID+`","slug":"github-repo","provider":"github","createdAt":"2026-04-01T10:00:00Z","updatedAt":"2026-04-01T10:00:00Z"},{"id":"`+billingInboxID+`","slug":"billing-webhooks","provider":"github","createdAt":"2026-04-01T10:01:00Z","updatedAt":"2026-04-01T10:01:00Z"}]`)
		case "/v1/inboxes/" + githubInboxID + "/items":
			w.Header().Set("Content-Type", "application/json")
			mu.Lock()
			currentRequest := requestCount["github-repo"]
			requestCount["github-repo"]++
			mu.Unlock()

			switch currentRequest {
			case 0:
				if got := r.URL.Query().Get("cursor"); got != "" {
					t.Fatalf("github cursor = %q, want empty cursor on first iteration", got)
				}
				_, _ = io.WriteString(w, `{"items":[{"id":"`+githubItemID+`","inboxId":"`+githubInboxID+`","inbox":"github-repo","recordedAt":"2026-04-03T10:05:00Z","headers":{"content-type":["application/json"]},"body":"eyJhY3Rpb24iOiJvcGVuZWQifQ==","providerDeliveryId":"delivery-1"}],"nextCursor":"`+githubItemID+`"}`)
			case 1:
				if got := r.URL.Query().Get("cursor"); got != "" {
					t.Fatalf("github cursor = %q, want empty cursor after failed handler replay", got)
				}
				_, _ = io.WriteString(w, `{"items":[{"id":"`+githubItemID+`","inboxId":"`+githubInboxID+`","inbox":"github-repo","recordedAt":"2026-04-03T10:05:00Z","headers":{"content-type":["application/json"]},"body":"eyJhY3Rpb24iOiJvcGVuZWQifQ==","providerDeliveryId":"delivery-1"}],"nextCursor":"`+githubItemID+`"}`)
			default:
				t.Fatalf("unexpected github feed request count %d", currentRequest+1)
			}
		case "/v1/inboxes/" + billingInboxID + "/items":
			w.Header().Set("Content-Type", "application/json")
			mu.Lock()
			currentRequest := requestCount["billing-webhooks"]
			requestCount["billing-webhooks"]++
			mu.Unlock()

			switch currentRequest {
			case 0:
				if got := r.URL.Query().Get("cursor"); got != "" {
					t.Fatalf("billing cursor = %q, want empty cursor on first iteration", got)
				}
				_, _ = io.WriteString(w, `{"items":[{"id":"`+billingItemID+`","inboxId":"`+billingInboxID+`","inbox":"billing-webhooks","recordedAt":"2026-04-03T10:06:00Z","headers":{"content-type":["application/json"]},"body":"eyJhY3Rpb24iOiJwYWlkIn0=","providerDeliveryId":"delivery-2"}],"nextCursor":"`+billingItemID+`"}`)
			case 1:
				if got := r.URL.Query().Get("cursor"); got != billingItemID {
					t.Fatalf("billing cursor = %q, want %q after successful first iteration", got, billingItemID)
				}
				_, _ = io.WriteString(w, `{"items":[],"nextCursor":"`+billingItemID+`"}`)
			default:
				t.Fatalf("unexpected billing feed request count %d", currentRequest+1)
			}
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	writeTestFile(t, configPath, `
version: 1
server:
  url: `+server.URL+`
  api_key: ${SIGVANE_API_KEY}
handlers:
  - inbox: github-repo
    command: ["`+os.Args[0]+`", "-test.run=TestHelperProcess", "--", "exit", "1"]
    stdin: none
  - inbox: billing-webhooks
    command: ["`+os.Args[0]+`", "-test.run=TestHelperProcess", "--", "append-line", "`+orderLogPath+`", "billing-webhooks"]
    stdin: none
`)

	_, stderr, err := executeCommandWithContext(
		ctx,
		"inbox",
		"poll",
		"--config", configPath,
		"--state", statePath,
	)
	if err != nil {
		t.Fatalf("expected graceful shutdown on context cancellation, got error: %v", err)
	}
	if !strings.Contains(stderr, `warning: handler "github-repo" failed for inbox item "`+githubItemID+`"`) {
		t.Fatalf("stderr = %q, want handler failure warning", stderr)
	}

	orderLog, err := os.ReadFile(orderLogPath)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", orderLogPath, err)
	}
	if string(orderLog) != "billing-webhooks\n" {
		t.Fatalf("handler order log = %q, want %q", string(orderLog), "billing-webhooks\n")
	}

	currentState, err := state.Load(statePath)
	if err != nil {
		t.Fatalf("state.Load returned error: %v", err)
	}
	if _, exists := currentState["github-repo"]; exists {
		t.Fatal("github state should not advance after handler failure")
	}
	if currentState["billing-webhooks"].LastItemID != billingItemID {
		t.Fatalf("billing state last_item_id = %q, want %q", currentState["billing-webhooks"].LastItemID, billingItemID)
	}
	if sleepCalls != 1 {
		t.Fatalf("sleep call count = %d, want 1", sleepCalls)
	}

	mu.Lock()
	defer mu.Unlock()
	if requestCount["github-repo"] != 2 {
		t.Fatalf("github feed request count = %d, want 2", requestCount["github-repo"])
	}
	if requestCount["billing-webhooks"] != 2 {
		t.Fatalf("billing feed request count = %d, want 2", requestCount["billing-webhooks"])
	}
}

func TestInboxPollShutdownSkipsRemainingHandlers(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "sigvane.yaml")
	statePath := filepath.Join(tempDir, "state", "state.json")
	t.Setenv("SIGVANE_API_KEY", "test-api-key")

	const handlerAInboxID = "00000000-0000-7000-8000-000000000001"
	const handlerBInboxID = "00000000-0000-7000-8000-000000000002"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-api-key" {
			t.Fatalf("Authorization header = %q, want %q", got, "Bearer test-api-key")
		}
		switch r.URL.Path {
		case "/v1/inboxes":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `[`+
				`{"id":"`+handlerAInboxID+`","slug":"handler-a","provider":"github","createdAt":"2026-04-01T10:00:00Z","updatedAt":"2026-04-01T10:00:00Z"},`+
				`{"id":"`+handlerBInboxID+`","slug":"handler-b","provider":"github","createdAt":"2026-04-01T10:01:00Z","updatedAt":"2026-04-01T10:01:00Z"}`+
				`]`)
		case "/v1/inboxes/" + handlerAInboxID + "/items":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"items":[],"nextCursor":null}`)
			go cancel()
		case "/v1/inboxes/" + handlerBInboxID + "/items":
			t.Errorf("handler-b should not be polled after context cancellation between handlers")
			w.WriteHeader(http.StatusInternalServerError)
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	writeTestFile(t, configPath, `
version: 1
server:
  url: `+server.URL+`
  api_key: ${SIGVANE_API_KEY}
handlers:
  - inbox: handler-a
    command: ["/usr/bin/true"]
    stdin: none
  - inbox: handler-b
    command: ["/usr/bin/true"]
    stdin: none
`)

	_, _, err := executeCommandWithContext(
		ctx,
		"inbox",
		"poll",
		"--config", configPath,
		"--state", statePath,
	)
	if err != nil {
		t.Fatalf("expected graceful shutdown on context cancellation, got error: %v", err)
	}
}

func TestInboxPollShutdownLetsHandlerExitWithinGracePeriod(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "sigvane.yaml")
	statePath := filepath.Join(tempDir, "state", "state.json")
	startedPath := filepath.Join(tempDir, "handler.started")
	termPath := filepath.Join(tempDir, "handler.term")
	t.Setenv("SIGVANE_API_KEY", "test-api-key")
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")

	const inboxID = "00000000-0000-7000-8000-000000000001"
	const itemID = "00000000-0000-7000-8000-000000000123"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-api-key" {
			t.Fatalf("Authorization header = %q, want %q", got, "Bearer test-api-key")
		}

		switch r.URL.Path {
		case "/v1/inboxes":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `[{"id":"`+inboxID+`","slug":"github-repo","provider":"github","createdAt":"2026-04-01T10:00:00Z","updatedAt":"2026-04-01T10:00:00Z"}]`)
		case "/v1/inboxes/" + inboxID + "/items":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"items":[{"id":"`+itemID+`","inboxId":"`+inboxID+`","inbox":"github-repo","recordedAt":"2026-04-03T10:05:00Z","headers":{"content-type":["application/json"]},"body":"eyJhY3Rpb24iOiJvcGVuZWQifQ==","providerDeliveryId":"delivery-1"}],"nextCursor":"`+itemID+`"}`)
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	writeTestFile(t, configPath, `
version: 1
server:
  url: `+server.URL+`
  api_key: ${SIGVANE_API_KEY}
  shutdown_grace_period: 200ms
handlers:
  - inbox: github-repo
    command: ["`+os.Args[0]+`", "-test.run=TestHelperProcess", "--", "wait-for-term-and-exit", "`+startedPath+`", "`+termPath+`", "50ms", "0"]
    stdin: none
`)

	go func() {
		for {
			info, err := os.Stat(startedPath)
			if err == nil && !info.IsDir() {
				cancel()
				return
			}
			if err != nil && !os.IsNotExist(err) {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	_, stderr, err := executeCommandWithContext(
		ctx,
		"inbox",
		"poll",
		"--config", configPath,
		"--once",
		"--state", statePath,
	)
	if err != nil {
		t.Fatalf("expected graceful shutdown within grace period, got error: %v", err)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty output", stderr)
	}

	currentState, err := state.Load(statePath)
	if err != nil {
		t.Fatalf("state.Load returned error: %v", err)
	}
	if currentState["github-repo"].LastItemID != itemID {
		t.Fatalf("state last_item_id = %q, want %q", currentState["github-repo"].LastItemID, itemID)
	}
	if _, err := os.Stat(termPath); err != nil {
		t.Fatalf("expected handler to observe shutdown signal, Stat(%q): %v", termPath, err)
	}
}

func TestInboxPollShutdownFailsWhenHandlerExceedsGracePeriod(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "sigvane.yaml")
	statePath := filepath.Join(tempDir, "state", "state.json")
	startedPath := filepath.Join(tempDir, "handler.started")
	termPath := filepath.Join(tempDir, "handler.term")
	t.Setenv("SIGVANE_API_KEY", "test-api-key")
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")

	const inboxID = "00000000-0000-7000-8000-000000000001"
	const itemID = "00000000-0000-7000-8000-000000000123"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-api-key" {
			t.Fatalf("Authorization header = %q, want %q", got, "Bearer test-api-key")
		}

		switch r.URL.Path {
		case "/v1/inboxes":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `[{"id":"`+inboxID+`","slug":"github-repo","provider":"github","createdAt":"2026-04-01T10:00:00Z","updatedAt":"2026-04-01T10:00:00Z"}]`)
		case "/v1/inboxes/" + inboxID + "/items":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"items":[{"id":"`+itemID+`","inboxId":"`+inboxID+`","inbox":"github-repo","recordedAt":"2026-04-03T10:05:00Z","headers":{"content-type":["application/json"]},"body":"eyJhY3Rpb24iOiJvcGVuZWQifQ==","providerDeliveryId":"delivery-1"}],"nextCursor":"`+itemID+`"}`)
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	writeTestFile(t, configPath, `
version: 1
server:
  url: `+server.URL+`
  api_key: ${SIGVANE_API_KEY}
  shutdown_grace_period: 50ms
handlers:
  - inbox: github-repo
    command: ["`+os.Args[0]+`", "-test.run=TestHelperProcess", "--", "ignore-term-and-sleep", "`+startedPath+`", "`+termPath+`", "250ms"]
    stdin: none
`)

	go func() {
		for {
			info, err := os.Stat(startedPath)
			if err == nil && !info.IsDir() {
				cancel()
				return
			}
			if err != nil && !os.IsNotExist(err) {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	_, stderr, err := executeCommandWithContext(
		ctx,
		"inbox",
		"poll",
		"--config", configPath,
		"--once",
		"--state", statePath,
	)
	if err == nil {
		t.Fatal("expected shutdown timeout error")
	}
	if !strings.Contains(err.Error(), `shutdown timed out waiting 50ms for handler "github-repo" to exit`) {
		t.Fatalf("error = %q, want shutdown timeout message", err.Error())
	}
	if strings.Contains(stderr, `warning: handler "github-repo" failed`) {
		t.Fatalf("stderr = %q, want no handler failure warning during shutdown timeout", stderr)
	}

	currentState, err := state.Load(statePath)
	if err != nil {
		t.Fatalf("state.Load returned error: %v", err)
	}
	if _, exists := currentState["github-repo"]; exists {
		t.Fatal("state should not advance when handler exceeds shutdown grace period")
	}
	if _, err := os.Stat(termPath); err != nil {
		t.Fatalf("expected handler to observe shutdown signal, Stat(%q): %v", termPath, err)
	}
}

func TestHelperProcess(_ *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	args := os.Args
	separator := -1
	for i, arg := range args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator == -1 || len(args) <= separator+2 {
		os.Exit(2)
	}

	commandName := args[separator+1]
	switch commandName {
	case "exit":
		exitCode := args[separator+2]
		if exitCode == "1" {
			os.Exit(1)
		}
		os.Exit(9)
	case "append-line":
		outputPath := args[separator+2]
		line := args[separator+3] + "\n"
		file, err := os.OpenFile(outputPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			os.Exit(6)
		}
		if _, err := io.WriteString(file, line); err != nil {
			_ = file.Close()
			os.Exit(7)
		}
		if err := file.Close(); err != nil {
			os.Exit(8)
		}
		os.Exit(0)
	case "write-stdin":
		outputPath := args[separator+2]
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			os.Exit(3)
		}
		if err := os.WriteFile(outputPath, data, 0o600); err != nil {
			os.Exit(4)
		}
		os.Exit(0)
	case "mkdir":
		path := args[separator+2]
		if err := os.MkdirAll(path, 0o700); err != nil {
			os.Exit(10)
		}
		os.Exit(0)
	case "mkdir-and-sleep":
		path := args[separator+2]
		duration, err := time.ParseDuration(args[separator+3])
		if err != nil {
			os.Exit(11)
		}
		if err := os.MkdirAll(path, 0o700); err != nil {
			os.Exit(10)
		}
		time.Sleep(duration)
		os.Exit(0)
	case "wait-for-term-and-exit":
		startedPath := args[separator+2]
		termPath := args[separator+3]
		duration, err := time.ParseDuration(args[separator+4])
		if err != nil {
			os.Exit(12)
		}
		exitCode := args[separator+5]
		termSignals := make(chan os.Signal, 1)
		signal.Notify(termSignals, syscall.SIGTERM)
		defer signal.Stop(termSignals)
		if err := os.WriteFile(startedPath, []byte("started\n"), 0o600); err != nil {
			os.Exit(13)
		}
		<-termSignals
		if err := os.WriteFile(termPath, []byte("term\n"), 0o600); err != nil {
			os.Exit(14)
		}
		time.Sleep(duration)
		if exitCode == "0" {
			os.Exit(0)
		}
		os.Exit(1)
	case "ignore-term-and-sleep":
		startedPath := args[separator+2]
		termPath := args[separator+3]
		duration, err := time.ParseDuration(args[separator+4])
		if err != nil {
			os.Exit(15)
		}
		termSignals := make(chan os.Signal, 1)
		signal.Notify(termSignals, syscall.SIGTERM)
		defer signal.Stop(termSignals)
		if err := os.WriteFile(startedPath, []byte("started\n"), 0o600); err != nil {
			os.Exit(16)
		}
		<-termSignals
		if err := os.WriteFile(termPath, []byte("term\n"), 0o600); err != nil {
			os.Exit(17)
		}
		time.Sleep(duration)
		os.Exit(0)
	default:
		os.Exit(5)
	}
}

func writeTestFile(t *testing.T, path string, contents string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimPrefix(contents, "\n")), 0o600); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}
