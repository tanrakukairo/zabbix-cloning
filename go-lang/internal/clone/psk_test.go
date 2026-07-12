package clone

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/t2-f/zabbix-cloning/internal/config"
	"github.com/t2-f/zabbix-cloning/internal/logx"
	"github.com/t2-f/zabbix-cloning/internal/model"
	"github.com/t2-f/zabbix-cloning/internal/zabbix"
)

func TestPSKUpdateParameters(t *testing.T) {
	credential := pskCredential{Identity: "identity", Key: "key"}
	params, update := pskUpdateParameters("hostid", "1", "1", credential)
	if !update || model.Int(params["tls_accept"]) != 2 {
		t.Fatalf("tls_accept=1 must be changed to 2: %#v", params)
	}
	params, update = pskUpdateParameters("hostid", "1", "3", credential)
	if !update || params["tls_accept"] != nil {
		t.Fatalf("tls_accept=3 must be preserved: %#v", params)
	}
	if params["tls_psk_identity"] != "identity" || params["tls_psk"] != "key" {
		t.Fatalf("PSK was not included: %#v", params)
	}
	if params, update = pskUpdateParameters("hostid", "1", "4", credential); update || params != nil {
		t.Fatalf("tls_accept>=4 must be skipped: %#v", params)
	}
}

func TestParsePSKConfiguration(t *testing.T) {
	configuration, err := parsePSKConfiguration(map[string]any{
		"proxy":            map[string]any{"proxy-a": []any{"proxy-id", "proxy-key"}},
		"host":             map[string]any{"host-a": []any{"host-id", "host-key"}},
		"autoregistration": []any{"auto-id", "auto-key"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if configuration.Proxy["proxy-a"].Identity != "proxy-id" || configuration.Host["host-a"].Key != "host-key" || configuration.Autoregistration.Identity != "auto-id" {
		t.Fatalf("unexpected PSK configuration: %#v", configuration)
	}
}

func TestApplyPSKDoesNothingOnMaster(t *testing.T) {
	engine := &Engine{Config: &config.Config{Role: "master", Raw: map[string]any{"psk": "invalid"}}}
	changed, err := engine.ApplyPSK(context.Background())
	if err != nil || changed {
		t.Fatalf("master must not process PSK: changed=%v err=%v", changed, err)
	}
}

func TestApplyPSKUpdatesExistingTargets(t *testing.T) {
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
		var result any = map[string]any{}
		switch current.Method {
		case "proxy.get":
			result = []map[string]any{{"proxyid": "10", "name": "proxy-a", "tls_accept": "3"}}
		case "host.get":
			result = []map[string]any{{"hostid": "20", "host": "host-a", "tls_accept": "1"}}
		case "autoregistration.get":
			result = map[string]any{"tls_accept": "1"}
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
	parameters, err := NewParameters(zabbix.ParseVersion("7.0.21"))
	if err != nil {
		t.Fatal(err)
	}
	engine := &Engine{
		API: api, Log: logger, Params: parameters,
		Config: &config.Config{Raw: map[string]any{"psk": map[string]any{
			"proxy":            map[string]any{"proxy-a": []any{"proxy-id", "proxy-key"}, "missing": []any{"id", "key"}},
			"host":             map[string]any{"host-a": []any{"host-id", "host-key"}},
			"autoregistration": []any{"auto-id", "auto-key"},
		}}},
	}
	changed, err := engine.ApplyPSK(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("PSK updates were not reported")
	}
	wantMethods := []string{"proxy.get", "proxy.update", "host.get", "host.update", "autoregistration.get", "autoregistration.update"}
	if len(requests) != len(wantMethods) {
		t.Fatalf("unexpected request count: got=%d want=%d", len(requests), len(wantMethods))
	}
	for index, method := range wantMethods {
		if requests[index].Method != method {
			t.Fatalf("request %d: got=%s want=%s", index, requests[index].Method, method)
		}
	}
	if requests[1].Params["tls_accept"] != nil {
		t.Fatalf("proxy tls_accept=3 must be preserved: %#v", requests[1].Params)
	}
	if model.Int(requests[3].Params["tls_accept"]) != 2 || model.Int(requests[5].Params["tls_accept"]) != 2 {
		t.Fatalf("tls_accept=1 was not changed to 2: host=%#v autoregistration=%#v", requests[3].Params, requests[5].Params)
	}
}
