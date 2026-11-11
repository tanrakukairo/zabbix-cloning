package clone

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/tanrakukairo/zabbix-cloning/internal/config"
	"github.com/tanrakukairo/zabbix-cloning/internal/logx"
	"github.com/tanrakukairo/zabbix-cloning/internal/model"
	"github.com/tanrakukairo/zabbix-cloning/internal/zabbix"
)

func TestDashboardVersionGates(t *testing.T) {
	for _, version := range []string{"7.0.0", "7.2.0", "7.4.0", "8.0.0"} {
		v := zabbix.ParseVersion(version)
		p, err := NewParameters(v)
		if err != nil {
			t.Fatal(err)
		}
		_, method := p.Methods["dashboard"]
		_, rule := p.ImportRules["dashboards"]
		if method != v.AtLeast(8, 0) || rule != method || (p.ConfigExport["dashboard"] == "dashboards") != method {
			t.Fatalf("%s: unexpected dashboard parameters", version)
		}
		for _, master := range []float64{7, 7.2, 7.4, 8} {
			if got := p.ImportSections(master)["dashboard"] != ""; got != (method && master >= 8) {
				t.Fatalf("%s / %.1f: unexpected import section", version, master)
			}
		}
		// No API is needed for old sources or targets, even with dashboard data.
		e := &Engine{Version: v, NewVersion: model.Version{MasterVersion: 7.4}, Dataset: model.Dataset{"dashboard": {{Name: "old"}}}}
		if err := e.ApplyDashboards(context.Background()); err != nil {
			t.Fatal(err)
		}
		if !v.AtLeast(8, 0) {
			e.NewVersion.MasterVersion = 8
			if err := e.ApplyDashboards(context.Background()); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestDashboardConfigurationRoundTrip(t *testing.T) {
	for _, empty := range []bool{false, true} {
		t.Run(map[bool]string{false: "dashboard", true: "empty"}[empty], func(t *testing.T) {
			ctx := context.Background()
			dashboard := model.Object{"name": "Overview", "pages": []any{model.Object{"widgets": []any{model.Object{
				"type": "item", "fields": []any{model.Object{"type": "ITEM", "name": "itemid.0", "value": model.Object{"host": "app", "key": "agent.ping"}}},
			}}}}}
			exported := []any{dashboard}
			ids := []any{"42"}
			if empty {
				exported, ids = []any{}, []any{}
			}
			var calls []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Method string
					Params model.Object
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
				}
				calls = append(calls, req.Method)
				var result any = true
				switch req.Method {
				case "configuration.export":
					if got := objectMap(req.Params["options"])["dashboards"]; !reflect.DeepEqual(got, ids) {
						t.Errorf("export IDs = %#v, want %#v", got, ids)
					}
					source, _ := json.Marshal(model.Object{"zabbix_export": model.Object{"version": "8.0", "dashboards": exported}})
					result = string(source)
				case "configuration.import":
					var document model.Object
					if err := json.Unmarshal([]byte(model.String(req.Params["source"])), &document); err != nil {
						t.Error(err)
					}
					want := model.Object{"version": "8.0", "dashboards": exported}
					got, _ := json.Marshal(document["zabbix_export"])
					encoded, _ := json.Marshal(want)
					if string(got) != string(encoded) {
						t.Errorf("dashboard references changed: %s", got)
					}
					rules := objectMap(objectMap(req.Params["rules"])["dashboards"])
					if rules["createMissing"] != true || rules["updateExisting"] != true {
						t.Errorf("invalid rules: %#v", rules)
					}
				default:
					t.Errorf("unexpected API: %s", req.Method)
				}
				_ = json.NewEncoder(w).Encode(model.Object{"jsonrpc": "2.0", "id": 1, "result": result})
			}))
			defer server.Close()
			api, err := zabbix.New(server.URL, false)
			if err != nil {
				t.Fatal(err)
			}
			logger, err := logx.New("test", "INFO", filepath.Join(t.TempDir(), "dashboard.log"), true)
			if err != nil {
				t.Fatal(err)
			}
			defer logger.Close()
			version := zabbix.ParseVersion("8.0.0")
			params, _ := NewParameters(version)
			params.ConfigExport = map[string]string{"dashboard": "dashboards"}
			local := map[string]*LocalItem{}
			if !empty {
				local["Overview"] = &LocalItem{ID: "42", Name: "Overview", Data: model.Object{"name": "Overview"}}
			}
			e := &Engine{API: api, Log: logger, Config: &config.Config{Role: "master", Quiet: true}, Version: version, Params: params,
				Local: map[string]map[string]*LocalItem{"dashboard": local}}
			if err := e.CreateMasterData(ctx); err != nil {
				t.Fatal(err)
			}
			// Ordinary configuration must defer dashboards, including virtual state.
			e.Local = map[string]map[string]*LocalItem{}
			e.enableDryRunVirtualState()
			if err := e.ApplyConfiguration(ctx); err != nil {
				t.Fatal(err)
			}
			if len(calls) != 1 || len(e.Local["dashboard"]) != 0 {
				t.Fatal("dashboard imported before dependent resources")
			}
			if err := e.ApplyDashboards(ctx); err != nil {
				t.Fatal(err)
			}
			wantCalls := []string{"configuration.export", "configuration.import"}
			if empty {
				wantCalls = wantCalls[:1]
			}
			if !reflect.DeepEqual(calls, wantCalls) || len(e.Local["dashboard"]) != len(exported) {
				t.Fatalf("calls=%v, dashboards=%v", calls, e.Local["dashboard"])
			}
		})
	}
}
