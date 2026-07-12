package clone

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tanrakukairo/zabbix-cloning/internal/config"
	"github.com/tanrakukairo/zabbix-cloning/internal/logx"
	"github.com/tanrakukairo/zabbix-cloning/internal/model"
	"github.com/tanrakukairo/zabbix-cloning/internal/zabbix"
)

func TestApplySecretGlobalMacrosCreatesAndUpdates(t *testing.T) {
	type request struct {
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
		ID     uint64         `json:"id"`
	}
	var requests []request
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, httpRequest *http.Request) {
		var current request
		if err := json.NewDecoder(httpRequest.Body).Decode(&current); err != nil {
			t.Error(err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		requests = append(requests, current)
		result := any(map[string]any{"globalmacroids": []string{"1"}})
		if current.Method == "usermacro.get" {
			result = []map[string]any{{"globalmacroid": "1", "macro": "{$EXISTING}", "type": "0"}}
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "result": result, "id": current.ID})
	}))
	defer server.Close()

	api, err := zabbix.New(server.URL, false)
	if err != nil {
		t.Fatal(err)
	}
	logger, err := logx.New("test", "INFO", "", true)
	if err != nil {
		t.Fatal(err)
	}
	engine := &Engine{
		API: api,
		Log: logger,
		Config: &config.Config{Raw: map[string]any{"secret_globalmacro": []any{
			map[string]any{"macro": "{$EXISTING}", "value": "updated"},
			map[string]any{"macro": "{$NEW}", "value": "created"},
		}}},
	}
	if err := engine.applySecretGlobalMacros(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 3 {
		t.Fatalf("unexpected request count: %d", len(requests))
	}
	if requests[0].Method != "usermacro.get" || requests[1].Method != "usermacro.updateglobal" || requests[2].Method != "usermacro.createglobal" {
		t.Fatalf("unexpected methods: %#v", requests)
	}
	if model.Int(requests[1].Params["type"]) != 1 || model.String(requests[1].Params["value"]) != "updated" {
		t.Fatalf("unexpected update parameters: %#v", requests[1].Params)
	}
	if model.Int(requests[2].Params["type"]) != 1 || model.String(requests[2].Params["macro"]) != "{$NEW}" {
		t.Fatalf("unexpected create parameters: %#v", requests[2].Params)
	}
}

func TestParseSecretGlobalMacrosRejectsInvalidValues(t *testing.T) {
	tests := []any{
		"invalid",
		[]any{"invalid"},
		[]any{map[string]any{"value": "missing macro"}},
		[]any{map[string]any{"macro": "{$MISSING_VALUE}"}},
		[]any{
			map[string]any{"macro": "{$DUPLICATE}", "value": "one"},
			map[string]any{"macro": "{$DUPLICATE}", "value": "two"},
		},
	}
	for _, value := range tests {
		if _, err := parseSecretGlobalMacros(value); err == nil {
			t.Fatalf("expected an error for %#v", value)
		}
	}
}

func TestDirectMasterDoesNotApplyTargetSecrets(t *testing.T) {
	cfg := (&config.Config{Raw: map[string]any{}, StoreType: "direct"}).DirectMaster()
	if !cfg.DirectSource {
		t.Fatal("direct master must be marked as an internal source")
	}
}
