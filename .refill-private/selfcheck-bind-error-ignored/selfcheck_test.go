package selfcheckbinderrorignored

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os/exec"
	"testing"
	"time"
)

func TestSelfcheckFailsWhenItsServerCannotBind(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	fake := &http.Server{Handler: http.HandlerFunc(fakeWorkbench)}
	serveDone := make(chan error, 1)
	go func() { serveDone <- fake.Serve(listener) }()
	t.Cleanup(func() {
		_ = fake.Shutdown(context.Background())
		<-serveDone
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "run", "../../cmd/server", "-addr="+listener.Addr().String(), "-selfcheck")
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("selfcheck 子进程未按期结束: %v", ctx.Err())
	}
	if err == nil {
		t.Fatalf("监听地址已被占用时 selfcheck 仍成功，并检查了其他服务: %s", output)
	}
}

func fakeWorkbench(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	response := map[string]any{}
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api/batches":
		response = map[string]any{"id": "foreign-batch", "version": 1}
	case r.Method == http.MethodPost && r.URL.Path == "/api/batches/foreign-batch/freeze":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["action"] == "precheck" {
			response = map[string]any{"confirmationSummary": "foreign-confirmation"}
		} else {
			response = map[string]any{"version": 2, "plan": map[string]any{"frozenAt": "2020-01-01T00:00:00Z"}}
		}
	case r.Method == http.MethodPost && r.URL.Path == "/api/batches/foreign-batch/rounds":
		response = map[string]any{"version": 3}
	case r.Method == http.MethodPost && r.URL.Path == "/api/batches/foreign-batch/finish":
		response = map[string]any{"version": 4}
	case r.Method == http.MethodPost && r.URL.Path == "/api/batches/foreign-batch/samples":
		response = map[string]any{"recordedSamples": []any{map[string]any{}, map[string]any{}}, "batch": map[string]any{"version": 5}}
	case r.Method == http.MethodGet && r.URL.Path == "/api/batches/foreign-batch/summary":
		response = map[string]any{"reviewEvidence": map[string]any{"reviewToken": "foreign-token"}}
	case r.Method == http.MethodPost && r.URL.Path == "/api/batches/foreign-batch/review":
		response = map[string]any{"version": 6}
	case r.Method == http.MethodPost && r.URL.Path == "/api/batches/foreign-batch/release":
		response = map[string]any{"verificationDigest": "foreign-digest"}
	case r.Method == http.MethodGet && r.URL.Path == "/api/batches/foreign-batch/verify":
		response = map[string]any{"passed": true}
	default:
		w.WriteHeader(http.StatusNotFound)
		response = map[string]any{"error": "unexpected request"}
	}
	_ = json.NewEncoder(w).Encode(response)
}
