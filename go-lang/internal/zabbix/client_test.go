package zabbix

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseVersion(t *testing.T) {
	version := ParseVersion("7.0.21")
	if version.Major != 7 || version.Minor != 0 || version.Patch != 21 {
		t.Fatalf("unexpected version: %#v", version)
	}
	if !version.AtLeast(6, 4) || version.Float() != 7.0 {
		t.Fatalf("unexpected comparison: %#v", version)
	}
}

func TestDryRunSkipsMutationRequests(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestCount++
		_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "result": []any{}, "id": 1})
	}))
	defer server.Close()

	client, err := New(server.URL, false)
	if err != nil {
		t.Fatal(err)
	}
	client.SetDryRun(true)
	mutations := []string{
		"host.create", "host.update", "host.delete",
		"usermacro.createglobal", "usermacro.updateglobal", "configuration.import",
	}
	for _, method := range mutations {
		if _, err := client.Call(context.Background(), method, map[string]any{}); err != nil {
			t.Fatal(err)
		}
	}
	if requestCount != 0 {
		t.Fatalf("dry run sent mutation requests: %d", requestCount)
	}
	if _, err := client.Call(context.Background(), "host.get", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if requestCount != 1 {
		t.Fatalf("read request was not sent: %d", requestCount)
	}
	counts := client.DryRunMethods()
	for _, method := range mutations {
		if counts[method] != 1 {
			t.Fatalf("mutation was not recorded: %s=%d", method, counts[method])
		}
	}
}

func TestMutationMethodClassification(t *testing.T) {
	for _, method := range []string{"host.create", "host.update", "host.delete", "task.create", "usermacro.createglobal", "usermacro.updateglobal", "configuration.import"} {
		if !isMutationMethod(method) {
			t.Fatalf("mutation method was not detected: %s", method)
		}
	}
	for _, method := range []string{"host.get", "configuration.export", "apiinfo.version", "user.login"} {
		if isMutationMethod(method) {
			t.Fatalf("read method was classified as mutation: %s", method)
		}
	}
}
