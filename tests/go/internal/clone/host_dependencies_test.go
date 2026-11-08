package clone

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tanrakukairo/zabbix-cloning/internal/config"
	"github.com/tanrakukairo/zabbix-cloning/internal/logx"
	"github.com/tanrakukairo/zabbix-cloning/internal/model"
	"github.com/tanrakukairo/zabbix-cloning/internal/zabbix"
)

func hostConditionAction(host string) model.Object {
	return model.Object{"eventsource": 0, "filter": model.Object{"conditions": []any{model.Object{"conditiontype": 1, "value": host}}}}
}

func TestMissingHostReferences(t *testing.T) {
	e := &Engine{Local: map[string]map[string]*LocalItem{"host": {"present": {ID: "42"}}}}
	for _, tc := range []struct {
		name, method string
		data         model.Object
		want         []string
	}{
		{"condition", "action", hostConditionAction("missing"), []string{"missing"}},
		{"existing", "action", hostConditionAction("present"), []string{}},
		{"source ID is not target ID", "action", hostConditionAction("42"), []string{"42"}},
		{"group only", "action", model.Object{"filter": model.Object{"conditions": []any{model.Object{"conditiontype": 0, "value": "group"}}}}, []string{}},
		{"maintenance mixed", "maintenance", model.Object{"hosts": []any{"present", "missing"}, "hostgroups": []any{"group"}}, []string{"missing"}},
		{"maintenance object", "maintenance", model.Object{"hosts": []any{model.Object{"name": "missing"}}}, []string{"missing"}},
		{"maintenance group only", "maintenance", model.Object{"hostgroups": []any{"group"}}, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := e.missingHostReferences(tc.method, tc.data); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
	for _, section := range []string{"operations", "recovery_operations", "update_operations"} {
		data := model.Object{section: []any{model.Object{"opcommand_hst": []any{model.Object{"hostid": "present"}, model.Object{"hostid": "__CURRENT_HOST__"}, model.Object{"hostid": 0}, model.Object{"hostid": "missing"}, model.Object{"hostid": "missing"}}}}}
		if got := e.missingHostReferences("action", data); !reflect.DeepEqual(got, []string{"missing"}) {
			t.Fatalf("%s: %v", section, got)
		}
	}
}

func TestMissingHostSettingsSkipAndContinueAfterHostFailure(t *testing.T) {
	for _, role := range []string{"worker", "replica"} {
		t.Run(role, func(t *testing.T) {
			var calls []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Method string       `json:"method"`
					Params model.Object `json:"params"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				calls = append(calls, request.Method+":"+model.String(request.Params["name"]))
				response := map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{}}
				if request.Method == "host.create" {
					response["error"] = map[string]any{"code": -32602, "message": "Invalid params", "data": "host rejected"}
					delete(response, "result")
				}
				if request.Method == "action.create" {
					condition := objects(objectMap(request.Params["filter"])["conditions"])[0]
					if model.String(condition["value"]) != "42" {
						t.Errorf("unresolved host reference: %v", condition)
					}
				}
				_ = json.NewEncoder(w).Encode(response)
			}))
			defer server.Close()
			api, err := zabbix.New(server.URL, false)
			if err != nil {
				t.Fatal(err)
			}
			logPath := filepath.Join(t.TempDir(), "apply.log")
			logger, err := logx.New("test", "INFO", logPath, true)
			if err != nil {
				t.Fatal(err)
			}
			defer logger.Close()
			params, err := NewParameters(zabbix.ParseVersion("7.0.0"))
			if err != nil {
				t.Fatal(err)
			}
			params.Post = []string{"action", "maintenance"}
			skipped := hostConditionAction("missing")
			skipped["name"] = "keep-action"
			valid := hostConditionAction("present")
			valid["name"] = "valid-action"
			maintenance := model.Object{"name": "keep-maintenance", "hosts": []any{"present", "missing"}, "hostgroups": []any{"group"}}
			e := &Engine{API: api, Log: logger, Params: params, Config: &config.Config{Role: role, Node: "worker-a", Workers: 1, Quiet: true, DryRun: true, DeleteAPI: true},
				Local:   map[string]map[string]*LocalItem{"host": {"present": {ID: "42", Name: "present"}}, "action": {"keep-action": {ID: "5", Name: "keep-action", Data: model.Object{"marker": "unchanged"}}}, "maintenance": {"keep-maintenance": {ID: "6", Name: "keep-maintenance", Data: model.Object{"marker": "unchanged"}}}},
				Dataset: model.Dataset{"host": {{Name: "missing", Data: model.Object{"host": "missing", "name": "missing", "tags": []any{model.Object{"tag": "ZC_WORKER", "value": "worker-a"}}}}}, "action": {{Name: "keep-action", Data: skipped}, {Name: "valid-action", Data: valid}}, "maintenance": {{Name: "keep-maintenance", Data: maintenance}}},
			}
			// Retain virtual target state while exercising real requests to the fake API.
			e.enableDryRunVirtualState()
			if err := e.ApplyHosts(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := e.ApplyAPISection(context.Background(), "POST"); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(calls, []string{"host.create:missing", "action.create:valid-action"}) {
				t.Fatalf("unexpected calls: %v", calls)
			}
			for method, name := range map[string]string{"action": "keep-action", "maintenance": "keep-maintenance"} {
				if e.Local[method][name].Data["marker"] != "unchanged" {
					t.Fatal("skipped setting changed")
				}
			}
			data, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			for _, value := range []string{"keep-action: SKIP, missing target hosts: missing", "keep-maintenance: SKIP, missing target hosts: missing", "skip:1"} {
				if !strings.Contains(string(data), value) {
					t.Errorf("log missing %q", value)
				}
			}
		})
	}
}

func TestUnrelatedActionErrorStillFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "error": map[string]any{"code": -32602, "message": "Invalid params", "data": "invalid formula"}})
	}))
	defer server.Close()
	e := preflightEngine(t, false)
	e.Config.Initialize = false
	e.Params.Post = []string{"action"}
	api, err := zabbix.New(server.URL, false)
	if err != nil {
		t.Fatal(err)
	}
	e.API = api
	e.Dataset = model.Dataset{"action": {{Name: "invalid", Data: model.Object{"name": "invalid", "eventsource": 0}}}}
	if err := e.ApplyAPISection(context.Background(), "POST"); err == nil || !strings.Contains(err.Error(), "invalid formula") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMaintenanceStoresTechnicalHostName(t *testing.T) {
	e := &Engine{}
	data := model.Object{"active_till": model.NowUnix() + 3600, "timeperiods": []any{model.Object{"timeperiod_type": 1}}, "hosts": []any{model.Object{"host": "technical", "name": "Display name"}}}
	if !e.prepareMasterItem("maintenance", data) {
		t.Fatal("maintenance rejected")
	}
	if !reflect.DeepEqual(data["hosts"], []any{"technical"}) {
		t.Fatalf("hosts = %v", data["hosts"])
	}
}
