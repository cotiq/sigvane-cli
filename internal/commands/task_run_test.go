package commands

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTaskRunOnceDrainsTasksAndCompletes(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "sigvane.yaml")
	stdinPath := filepath.Join(tempDir, "stdin.json")
	t.Setenv("SIGVANE_API_KEY", "test-api-key")
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")

	const taskID = "00000000-0000-7000-8000-000000000174"
	const leaseToken = "lease-token"

	var mu sync.Mutex
	claimRequests := 0
	completeRequests := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-api-key" {
			t.Fatalf("Authorization header = %q, want %q", got, "Bearer test-api-key")
		}

		switch r.URL.Path {
		case "/v1/tasks/claim":
			if r.Method != http.MethodPost {
				t.Fatalf("claim method = %s, want POST", r.Method)
			}

			var body struct {
				Kinds []string `json:"kinds"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode claim body: %v", err)
			}
			if len(body.Kinds) != 1 || body.Kinds[0] != "github_pr_review" {
				t.Fatalf("claim kinds = %#v, want github_pr_review", body.Kinds)
			}

			mu.Lock()
			currentRequest := claimRequests
			claimRequests++
			mu.Unlock()
			if currentRequest == 0 {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"id":"`+taskID+`","kind":"github_pr_review","payload":{"repository":"cotiq/sigvane","pullRequestNumber":174},"attempts":1,"leaseToken":"`+leaseToken+`","leaseDeadline":"2026-06-06T12:00:00Z"}`)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case "/v1/tasks/" + taskID + "/complete":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode complete body: %v", err)
			}
			if body["leaseToken"] != leaseToken {
				t.Fatalf("complete leaseToken = %q, want %q", body["leaseToken"], leaseToken)
			}
			if len(body) != 1 {
				t.Fatalf("complete body = %#v, want only leaseToken", body)
			}
			mu.Lock()
			completeRequests++
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
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
tasks:
  - kind: github_pr_review
    command: ["`+os.Args[0]+`", "-test.run=TestHelperProcess", "--", "write-stdin", "`+stdinPath+`"]
`)

	stdout, stderr, err := executeCommand("task", "run", "--config", configPath, "--once")
	if err != nil {
		t.Fatalf("task run returned error: %v", err)
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
	if string(stdin) != `{"repository":"cotiq/sigvane","pullRequestNumber":174}` {
		t.Fatalf("handler stdin = %s, want task payload only", stdin)
	}
	if strings.Contains(string(stdin), leaseToken) {
		t.Fatalf("handler stdin must not contain lease token, got %s", stdin)
	}

	mu.Lock()
	defer mu.Unlock()
	if claimRequests != 2 {
		t.Fatalf("claim request count = %d, want 2", claimRequests)
	}
	if completeRequests != 1 {
		t.Fatalf("complete request count = %d, want 1", completeRequests)
	}
}

func TestTaskRunReportsRejectAndFailOutcomes(t *testing.T) {
	tests := []struct {
		name        string
		handlerArgs string
		outcomePath string
		wantReason  string
		wantStderr  string
	}{
		{
			name:        "reject exit code",
			handlerArgs: `"stderr-and-exit", "draft pull request\n", "79"`,
			outcomePath: "/reject",
			wantReason:  "draft pull request",
			wantStderr:  "draft pull request\n",
		},
		{
			name:        "fail exit code with fallback reason",
			handlerArgs: `"exit", "1"`,
			outcomePath: "/fail",
			wantReason:  "handler exited with code 1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tempDir := t.TempDir()
			configPath := filepath.Join(tempDir, "sigvane.yaml")
			t.Setenv("SIGVANE_API_KEY", "test-api-key")
			t.Setenv("GO_WANT_HELPER_PROCESS", "1")

			const taskID = "00000000-0000-7000-8000-000000000174"
			const leaseToken = "lease-token"

			claimRequests := 0
			outcomeRequests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/tasks/claim":
					claimRequests++
					if claimRequests == 1 {
						w.Header().Set("Content-Type", "application/json")
						_, _ = io.WriteString(w, `{"id":"`+taskID+`","kind":"github_pr_review","payload":{"repository":"cotiq/sigvane","pullRequestNumber":174},"attempts":1,"leaseToken":"`+leaseToken+`","leaseDeadline":"2026-06-06T12:00:00Z"}`)
						return
					}
					w.WriteHeader(http.StatusNoContent)
				case "/v1/tasks/" + taskID + tc.outcomePath:
					outcomeRequests++
					var body map[string]string
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatalf("decode outcome body: %v", err)
					}
					if body["leaseToken"] != leaseToken {
						t.Fatalf("outcome leaseToken = %q, want %q", body["leaseToken"], leaseToken)
					}
					if body["reason"] != tc.wantReason {
						t.Fatalf("outcome reason = %q, want %q", body["reason"], tc.wantReason)
					}
					w.WriteHeader(http.StatusOK)
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
tasks:
  - kind: github_pr_review
    command: ["`+os.Args[0]+`", "-test.run=TestHelperProcess", "--", `+tc.handlerArgs+`]
`)

			_, stderr, err := executeCommand("task", "run", "--config", configPath, "--once")
			if err != nil {
				t.Fatalf("task run returned error: %v", err)
			}
			if stderr != tc.wantStderr {
				t.Fatalf("stderr = %q, want %q", stderr, tc.wantStderr)
			}
			if outcomeRequests != 1 {
				t.Fatalf("outcome request count = %d, want 1", outcomeRequests)
			}
		})
	}
}

func TestTaskRunRetriesTransientClaimAndOutcomeErrors(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "sigvane.yaml")
	runLogPath := filepath.Join(tempDir, "runs.log")
	t.Setenv("SIGVANE_API_KEY", "test-api-key")
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")

	const taskID = "00000000-0000-7000-8000-000000000174"
	var sleeps []time.Duration
	previousSleep := sleepContext
	sleepContext = func(_ context.Context, d time.Duration) error {
		sleeps = append(sleeps, d)
		return nil
	}
	defer func() {
		sleepContext = previousSleep
	}()

	claimRequests := 0
	outcomeRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/tasks/claim":
			claimRequests++
			if claimRequests == 1 {
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = io.WriteString(w, "rate limited")
				return
			}
			if claimRequests == 2 {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"id":"`+taskID+`","kind":"github_pr_review","payload":{},"attempts":1,"leaseToken":"lease-token","leaseDeadline":"2026-06-06T12:00:00Z"}`)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case "/v1/tasks/" + taskID + "/complete":
			outcomeRequests++
			if outcomeRequests == 1 {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = io.WriteString(w, "try again")
				return
			}
			w.WriteHeader(http.StatusOK)
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
tasks:
  - kind: github_pr_review
    command: ["`+os.Args[0]+`", "-test.run=TestHelperProcess", "--", "append-line", "`+runLogPath+`", "ran"]
`)

	_, stderr, err := executeCommand("task", "run", "--config", configPath, "--once")
	if err != nil {
		t.Fatalf("task run returned error: %v", err)
	}
	if !strings.Contains(stderr, "warning: transient task claim error") {
		t.Fatalf("stderr = %q, want transient claim warning", stderr)
	}
	if !strings.Contains(stderr, `warning: transient task outcome error for task "`+taskID+`"`) {
		t.Fatalf("stderr = %q, want transient outcome warning", stderr)
	}

	runLog, err := os.ReadFile(runLogPath)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", runLogPath, err)
	}
	if string(runLog) != "ran\n" {
		t.Fatalf("run log = %q, want one handler run", string(runLog))
	}
	if claimRequests != 3 {
		t.Fatalf("claim request count = %d, want 3", claimRequests)
	}
	if outcomeRequests != 2 {
		t.Fatalf("outcome request count = %d, want 2", outcomeRequests)
	}
	if len(sleeps) != 2 || sleeps[0] != time.Second || sleeps[1] != time.Second {
		t.Fatalf("backoff sleeps = %#v, want [1s 1s]", sleeps)
	}
}

func TestTaskRunAbortsWithoutOutcomeWhenHandlerCannotStart(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "sigvane.yaml")
	t.Setenv("SIGVANE_API_KEY", "test-api-key")

	const taskID = "00000000-0000-7000-8000-000000000174"
	claimRequests := 0
	outcomeRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/tasks/claim":
			claimRequests++
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"`+taskID+`","kind":"github_pr_review","payload":{},"attempts":1,"leaseToken":"lease-token","leaseDeadline":"2026-06-06T12:00:00Z"}`)
		case "/v1/tasks/" + taskID + "/complete", "/v1/tasks/" + taskID + "/fail", "/v1/tasks/" + taskID + "/reject":
			outcomeRequests++
			t.Fatalf("outcome endpoint should not be called when handler cannot start: %s", r.URL.Path)
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
tasks:
  - kind: github_pr_review
    command: ["/path/to/missing/sigvane-test-handler"]
`)

	_, _, err := executeCommand("task", "run", "--config", configPath, "--once")
	if err == nil {
		t.Fatal("expected task run to abort when handler cannot start")
	}
	if !strings.Contains(err.Error(), `run task handler for kind "github_pr_review"`) {
		t.Fatalf("error = %q, want handler start failure context", err.Error())
	}
	if claimRequests != 1 {
		t.Fatalf("claim request count = %d, want 1", claimRequests)
	}
	if outcomeRequests != 0 {
		t.Fatalf("outcome request count = %d, want 0", outcomeRequests)
	}
}

func TestTaskRunOutcomeConflictWarnsAndDoesNotRerun(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "sigvane.yaml")
	runLogPath := filepath.Join(tempDir, "runs.log")
	t.Setenv("SIGVANE_API_KEY", "test-api-key")
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")

	const taskID = "00000000-0000-7000-8000-000000000174"
	claimRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/tasks/claim":
			claimRequests++
			if claimRequests == 1 {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"id":"`+taskID+`","kind":"github_pr_review","payload":{},"attempts":1,"leaseToken":"lease-token","leaseDeadline":"2026-06-06T12:00:00Z"}`)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case "/v1/tasks/" + taskID + "/complete":
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, "stale lease")
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
tasks:
  - kind: github_pr_review
    command: ["`+os.Args[0]+`", "-test.run=TestHelperProcess", "--", "append-line", "`+runLogPath+`", "ran"]
`)

	_, stderr, err := executeCommand("task", "run", "--config", configPath, "--once")
	if err != nil {
		t.Fatalf("task run returned error: %v", err)
	}
	if !strings.Contains(stderr, `warning: task "`+taskID+`" outcome "complete" was not applied because the lease was stale or already resolved`) {
		t.Fatalf("stderr = %q, want outcome conflict warning", stderr)
	}

	runLog, err := os.ReadFile(runLogPath)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", runLogPath, err)
	}
	if string(runLog) != "ran\n" {
		t.Fatalf("run log = %q, want one handler run", string(runLog))
	}
}

func TestTaskRunShutdownReportsFinishedTaskOutcome(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "sigvane.yaml")
	startedPath := filepath.Join(tempDir, "handler.started")
	termPath := filepath.Join(tempDir, "handler.term")
	t.Setenv("SIGVANE_API_KEY", "test-api-key")
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")

	const taskID = "00000000-0000-7000-8000-000000000174"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	completeRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/tasks/claim":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"`+taskID+`","kind":"github_pr_review","payload":{},"attempts":1,"leaseToken":"lease-token","leaseDeadline":"2026-06-06T12:00:00Z"}`)
		case "/v1/tasks/" + taskID + "/complete":
			completeRequests++
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode complete body: %v", err)
			}
			if body["leaseToken"] != "lease-token" {
				t.Fatalf("complete leaseToken = %q, want lease-token", body["leaseToken"])
			}
			w.WriteHeader(http.StatusOK)
		case "/v1/tasks/" + taskID + "/fail", "/v1/tasks/" + taskID + "/reject":
			t.Fatalf("unexpected outcome endpoint for finished task: %s", r.URL.Path)
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
  shutdown_grace_period: 2s
tasks:
  - kind: github_pr_review
    command: ["`+os.Args[0]+`", "-test.run=TestHelperProcess", "--", "wait-for-term-and-exit", "`+startedPath+`", "`+termPath+`", "10ms", "0"]
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

	_, _, err := executeCommandWithContext(ctx, "task", "run", "--config", configPath, "--once")
	if err != nil {
		t.Fatalf("expected graceful shutdown on context cancellation, got error: %v", err)
	}
	if _, err := os.Stat(termPath); err != nil {
		t.Fatalf("expected handler to observe shutdown signal, Stat(%q): %v", termPath, err)
	}
	if completeRequests != 1 {
		t.Fatalf("complete request count = %d, want 1", completeRequests)
	}
}
