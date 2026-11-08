package clone

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/tanrakukairo/zabbix-cloning/internal/config"
	"github.com/tanrakukairo/zabbix-cloning/internal/logx"
	"github.com/tanrakukairo/zabbix-cloning/internal/model"
	"github.com/tanrakukairo/zabbix-cloning/internal/zabbix"
)

func TestProxyPermissionsUseTargetIDs(t *testing.T) {
	engine := &Engine{Local: map[string]map[string]*LocalItem{
		"proxy":      {"Remote": {Name: "Remote", ID: "13"}},
		"proxygroup": {"Edge": {Name: "Edge", ID: "20"}},
	}}
	engine.rebuildIDReplace()
	data := model.Object{
		"proxies":      []any{model.Object{"proxyid": "13"}},
		"proxy_groups": []any{model.Object{"proxy_groupid": "20"}},
		"apm_filters":  []any{model.Object{"key": "service.name", "value": "backend", "signal_type": 0}},
	}
	if err := engine.storeProxyPermissions(data); err != nil {
		t.Fatal(err)
	}
	engine.Local["proxy"]["Remote"].ID = "35"
	engine.Local["proxygroup"]["Edge"].ID = "42"
	if err := engine.resolveProxyPermissions(data); err != nil {
		t.Fatal(err)
	}
	if objects(data["proxies"])[0]["proxyid"] != "35" ||
		objects(data["proxy_groups"])[0]["proxy_groupid"] != "42" ||
		objects(data["apm_filters"])[0]["value"] != "backend" {
		t.Fatalf("invalid permission payload: %#v", data)
	}
	missing := model.Object{"proxy_groups": []any{"Missing"}}
	if err := engine.resolveProxyPermissions(missing); err == nil {
		t.Fatal("a missing target must not silently change group permissions")
	}
	legacy := model.Object{"proxy_mode": 1}
	if err := engine.resolveProxyPermissions(legacy); err != nil || len(legacy) != 1 {
		t.Fatalf("legacy snapshot unexpectedly changed: %#v, %v", legacy, err)
	}
}

func TestAPMSelectorsRequireZabbix80(t *testing.T) {
	for _, version := range []string{"7.0.32", "7.2.15", "7.4.16", "8.0.0"} {
		parsed := zabbix.ParseVersion(version)
		params, err := NewParameters(parsed)
		if err != nil {
			t.Fatal(err)
		}
		for method, selectors := range map[string][]string{
			"proxy": {"selectApm"}, "usergroup": {"selectProxies", "selectProxyGroups", "selectApmFilters"},
		} {
			for _, key := range selectors {
				if _, present := params.Methods[method].Options[key]; present != parsed.AtLeast(8, 0) {
					t.Fatalf("%s: unexpected %s selector presence %t", version, key, present)
				}
			}
		}
	}
}

func TestSAMLDirectoryUsesTargetCredentials(t *testing.T) {
	for _, tc := range []struct {
		version string
		update  bool
	}{
		{"7.4.16", false},
		{"8.0.0", false},
		{"8.0.0", true},
	} {
		t.Run(tc.version+map[bool]string{false: "/create", true: "/update"}[tc.update], func(t *testing.T) {
			method := "userdirectory.create"
			local := map[string]*LocalItem{}
			if tc.update {
				method = "userdirectory.update"
				local[samlDirectoryName] = &LocalItem{ID: "57"}
			}
			requests := make(chan model.Object, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Method string
					Params model.Object
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				if request.Method != method {
					t.Errorf("method = %q, want %q", request.Method, method)
				}
				requests <- request.Params
				_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":{"userdirectoryids":["57"]},"id":1}`))
			}))
			defer server.Close()
			api, err := zabbix.New(server.URL, false)
			if err != nil {
				t.Fatal(err)
			}
			source := model.Object{"idp_type": 2, "sp_private_key": "old-key", "sp_certificate_hash": "source-hash"}
			engine := &Engine{
				API: api, Version: zabbix.ParseVersion(tc.version), Params: &Parameters{},
				Config:  &config.Config{Raw: map[string]any{"saml_certificates": model.Object{"sp_private_key": "target-key"}}},
				Dataset: model.Dataset{"userdirectory": {{Data: source}}},
				Local:   map[string]map[string]*LocalItem{"userdirectory": local},
			}
			if err := engine.ApplySAMLDirectory(context.Background()); err != nil {
				t.Fatal(err)
			}
			select {
			case request := <-requests:
				if _, present := request["sp_certificate_hash"]; present {
					t.Fatal("source hash sent to the target")
				}
				if key := model.String(request["sp_private_key"]); (tc.version == "8.0.0" && key != "target-key") ||
					(tc.version != "8.0.0" && key != "") {
					t.Fatal("incorrect SAML credential payload")
				}
				if tc.update && request["userdirectoryid"] != "57" {
					t.Fatal("existing SAML directory ID not used")
				}
			default:
				t.Fatal("SAML request not received")
			}
		})
	}
}

func TestInterfacesReplaceAtomically(t *testing.T) {
	for _, mode := range []string{"success", "failure", "dry-run", "unchanged"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body model.Object
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					return
				}
				calls++
				if body["method"] != "host.update" {
					t.Errorf("removed API called: %v", body["method"])
				}
				params := objectMap(body["params"])
				rows := objects(params["interfaces"])
				if model.String(params["hostid"]) != "100" || len(rows) != 4 {
					t.Errorf("unexpected payload: %#v", params)
				}
				ids := map[string]model.Object{}
				for _, row := range rows {
					ids[model.String(row["interfaceid"])] = row
				}
				if ids["1"] == nil || ids["2"] == nil || ids["4"] == nil || ids[""] == nil || ids["3"] != nil {
					t.Errorf("interface IDs not preserved: %#v", rows)
				}
				if model.String(ids["1"]["ip"]) != "10.0.0.9" || model.String(objectMap(ids["1"]["details"])["retries"]) != "{$RETRIES}" {
					t.Errorf("update lost: %#v", ids["1"])
				}
				response := model.Object{"jsonrpc": "2.0", "id": body["id"], "result": model.Object{"hostids": []string{"100"}}}
				if mode == "failure" {
					delete(response, "result")
					response["error"] = model.Object{"code": -32602, "message": "Invalid params.", "data": "Interface is used by items."}
				}
				_ = json.NewEncoder(w).Encode(response)
			}))
			defer server.Close()
			api, err := zabbix.New(server.URL, false)
			if err != nil {
				t.Fatal(err)
			}
			api.SetDryRun(mode == "dry-run")
			logger, err := logx.New("test", "CRITICAL", "", true)
			if err != nil {
				t.Fatal(err)
			}
			defer logger.Close()
			engine := &Engine{API: api, Version: zabbix.ParseVersion("8.0.0")}
			work := hostInterfaceWork{
				Host:     hostPlan{ID: "100", Name: "test"},
				Existing: []model.Object{{"interfaceid": "1"}, {"interfaceid": "2"}, {"interfaceid": "3"}, {"interfaceid": "4"}},
				Plans: []interfacePlan{
					{Function: "update", Target: model.Object{"interfaceid": "1"}, Update: model.Object{"type": 2, "ip": "10.0.0.9", "details": model.Object{"retries": "{$RETRIES}"}}},
					{Function: "skip", Target: model.Object{"interfaceid": "2"}, Update: model.Object{"type": 1}},
					{Function: "create", Update: model.Object{"type": 4, "main": 1, "port": "12345"}},
				},
				Deletes: []model.Object{{"interfaceid": "3"}},
			}
			if mode == "unchanged" {
				work.Plans = work.Plans[1:2]
				work.Deletes = nil
			}
			progress := newApplyProgress(logger, true, "interfaces", len(work.Plans)+len(work.Deletes))
			engine.applyHostInterfaceWork(context.Background(), work, progress, &sync.Mutex{})
			wantCalls := 1
			if mode == "dry-run" || mode == "unchanged" {
				wantCalls = 0
			}
			if calls != wantCalls {
				t.Fatalf("calls = %d, want %d", calls, wantCalls)
			}
			if mode == "failure" && progress.counts["failed"] != 4 {
				t.Fatalf("failure not reported: %#v", progress.counts)
			}
			if mode != "failure" && progress.counts["failed"] != 0 {
				t.Fatalf("unexpected failure: %#v", progress.counts)
			}
			if mode == "dry-run" && api.DryRunMethods()["host.update"] != 1 {
				t.Fatal("mutation not recorded in dry run")
			}
		})
	}
}

func TestAction80ProxyGroupRoundTrip(t *testing.T) {
	params, err := NewParameters(zabbix.ParseVersion("8.0.0"))
	if err != nil {
		t.Fatal(err)
	}
	condition := model.Object{"conditiontype": 29, "value": "15"}
	data := model.Object{"eventsource": 1, "filter": model.Object{"conditions": []any{condition}}}
	engine := &Engine{Params: params, Config: &config.Config{Role: "master"}, IDReplace: map[string]map[string]any{"proxygroup": {"15": "proxy-group"}}}
	if !engine.prepareMasterItem("action", data) || condition["value"] != "proxy-group" {
		t.Fatalf("source ID not converted: %#v", condition)
	}
	engine.Config.Role = "worker"
	engine.IDReplace = map[string]map[string]any{"proxygroup": {"proxy-group": "99"}}
	if !engine.normalizeAction("test", data) || condition["value"] != "99" {
		t.Fatalf("target ID not resolved: %#v", condition)
	}
}

func TestRegexpDescriptionVersionGate(t *testing.T) {
	for _, version := range []string{"7.0.0", "7.4.0", "8.0.0"} {
		params, err := NewParameters(zabbix.ParseVersion(version))
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, field := range toAnyList(params.Methods["regexp"].Options["output"]) {
			found = found || field == "description"
		}
		if found != (version == "8.0.0") {
			t.Errorf("%s description selected = %v", version, found)
		}
	}
}

func TestRole80PreservesLegacyAPIAccess(t *testing.T) {
	for _, test := range []struct {
		name         string
		source       float64
		access, mode int
		methods      []any
		wantMode     int
	}{
		{"legacy all", 7.4, 1, 1, []any{}, 0},
		{"native denied", 8, 1, 1, []any{}, 1},
		{"disabled", 7.4, 0, 1, []any{}, 1},
		{"restricted", 7.4, 1, 1, []any{"host.get"}, 1},
		{"deny list", 7.4, 1, 0, []any{"host.delete"}, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			engine := &Engine{Version: zabbix.ParseVersion("8.0.0"), NewVersion: model.Version{MasterVersion: test.source}}
			rules := model.Object{"api.access": test.access, "api.mode": test.mode, "api": test.methods}
			data := model.Object{"rules": rules}
			if !engine.prepareSpecialAPIItem("role", "test", data) {
				t.Fatal("role skipped")
			}
			if model.Int(rules["api.mode"]) != test.wantMode || model.Int(rules["api.access"]) != test.access {
				t.Fatalf("unexpected rules: %#v", rules)
			}
		})
	}
}
