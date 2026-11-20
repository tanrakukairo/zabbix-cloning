package clone

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/tanrakukairo/zabbix-cloning/internal/config"
	"github.com/tanrakukairo/zabbix-cloning/internal/logx"
	"github.com/tanrakukairo/zabbix-cloning/internal/model"
	"github.com/tanrakukairo/zabbix-cloning/internal/zabbix"
)

func TestAPMDataSourceAndProxyApplyLate(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(map[bool]string{false: "HTTP", true: "dry-run"}[dryRun], func(t *testing.T) {
			type request struct {
				Method string
				Params model.Object
			}
			var requests []request
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req request
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
				}
				requests = append(requests, req)
				_ = json.NewEncoder(w).Encode(model.Object{"jsonrpc": "2.0", "id": 1, "result": model.Object{"proxyids": []any{"99"}}})
			}))
			defer server.Close()
			api, err := zabbix.New(server.URL, false)
			if err != nil {
				t.Fatal(err)
			}
			api.SetDryRun(dryRun)
			logger, err := logx.New("test", "INFO", "", true)
			if err != nil {
				t.Fatal(err)
			}
			defer logger.Close()
			version := zabbix.ParseVersion("8.0.0")
			params, _ := NewParameters(version)
			params.Global = []string{"settings"}
			params.Mid = []string{"proxy"}
			settings := model.Object{"default_lang": "ja_JP", "apm_global_db": model.Object{
				"status": 1, "url": "https://clickhouse.example:8443", "authentication_type": 0, "username": "zc", "password": "source-secret", "db": "otel", "ssl_verify_peer": 1, "ssl_verify_host": 1,
			}}
			e := &Engine{API: api, Log: logger, Params: params, Version: version, NewVersion: model.Version{MasterVersion: 8},
				Config: &config.Config{DryRun: true, Raw: map[string]any{"settings": model.Object{"apm_global_db": model.Object{"password": "target-secret"}}}},
				Local:  map[string]map[string]*LocalItem{"proxy": {"edge": {ID: "99", Name: "edge", Data: model.Object{}}}},
				Dataset: model.Dataset{"proxy": {{Name: "edge", Data: model.Object{"name": "edge", "operating_mode": 0, "apm": model.Object{
					"data_collection_status": 1, "max_messages_per_second": 123, "additional_resource_attributes": []any{model.Object{"key": "cluster", "value": "prod", "signal_type": 0}},
				}}}}},
			}
			e.prepareMasterItem("settings", settings)
			if objectMap(settings["apm_global_db"])["password"] != nil {
				t.Fatal("source password stored")
			}
			e.Dataset["settings"] = []model.StoreItem{{Data: settings}}
			e.enableDryRunVirtualState()
			ctx := context.Background()
			if err := e.ApplyGlobalSettings(ctx); err != nil {
				t.Fatal(err)
			}
			if err := e.ApplyAPISection(ctx, "MID"); err != nil {
				t.Fatal(err)
			}
			if !dryRun {
				if len(requests) != 2 || requests[0].Params["apm_global_db"] != nil || requests[1].Params["apm"] != nil {
					t.Fatalf("APM applied too early: %v", requests)
				}
			}
			if err := e.ApplyAPMSettings(ctx); err != nil {
				t.Fatal(err)
			}
			if !dryRun {
				if len(requests) != 4 || requests[2].Method != "settings.update" || requests[3].Method != "proxy.update" {
					t.Fatalf("incorrect order: %v", requests)
				}
				db := objectMap(requests[2].Params["apm_global_db"])
				if db["password"] != "target-secret" || db["url"] != "https://clickhouse.example:8443" || db["db"] != "otel" {
					t.Fatalf("partial credentials override lost fields: %#v", db)
				}
				if requests[3].Params["proxyid"] != "99" || model.Int(objectMap(requests[3].Params["apm"])["max_messages_per_second"]) != 123 {
					t.Fatalf("invalid proxy APM: %#v", requests[3])
				}
			} else {
				if len(requests) != 0 || api.DryRunMethods()["settings.update"] != 2 || api.DryRunMethods()["proxy.update"] != 2 {
					t.Fatal("dry-run did not plan APM without sending requests")
				}
			}
			if model.Int(objectMap(e.Local["proxy"]["edge"].Data["apm"])["data_collection_status"]) != 1 {
				t.Fatal("virtual proxy state not updated")
			}
			if objectMap(e.Local["settings"]["apm_global_db"].Data["apm_global_db"])["db"] != "otel" {
				t.Fatal("virtual data source not updated")
			}
			if objectMap(settings["apm_global_db"])["password"] != nil {
				t.Fatal("store mutated by credentials override")
			}
		})
	}
}

func TestAPMDataSourceNormalization(t *testing.T) {
	for _, tc := range []struct {
		name        string
		input, want model.Object
	}{
		{"disabled", model.Object{"status": 0, "url": "https://old", "authentication_type": 0, "password": "old", "db": "old"}, model.Object{"status": 0}},
		{"no auth HTTP", model.Object{"status": 1, "url": "http://ch", "authentication_type": 1, "username": "old", "password": "old", "ssl_verify_peer": 1, "ssl_verify_host": 0, "db": ""}, model.Object{"status": 1, "url": "http://ch", "authentication_type": 1, "db": ""}},
		{"no peer verification", model.Object{"status": 1, "url": "https://ch", "ssl_verify_peer": 0, "ssl_verify_host": 0}, model.Object{"status": 1, "url": "https://ch", "ssl_verify_peer": 0}},
		{"empty password", model.Object{"status": 1, "authentication_type": 0, "password": ""}, model.Object{"status": 1, "authentication_type": 0, "password": ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			normalizeAPMDataSource(tc.input)
			if !reflect.DeepEqual(tc.input, tc.want) {
				t.Fatalf("got %#v, want %#v", tc.input, tc.want)
			}
		})
	}
}

func TestAPMOldDataAndVersionsDoNotChangeConfiguration(t *testing.T) {
	for _, version := range []string{"7.0.0", "7.2.0", "7.4.0", "8.0.0"} {
		e := &Engine{Version: zabbix.ParseVersion(version), Dataset: model.Dataset{"proxy": {{Name: "legacy", Data: model.Object{"name": "legacy"}}}}}
		if err := e.ApplyAPMSettings(context.Background()); err != nil {
			t.Fatalf("%s: %v", version, err)
		}
		if version != "8.0.0" {
			e.apmSettings = model.Object{"apm_global_db": model.Object{"status": 1}}
			if err := e.ApplyAPMSettings(context.Background()); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestAPMPasswordOnlyUsesTargetSecret(t *testing.T) {
	for _, tc := range []struct {
		name     string
		override model.Object
		want     any
		present  bool
	}{
		{"no secret", model.Object{}, nil, false},
		{"target secret", model.Object{"password": "configured-secret"}, "configured-secret", true},
		{"explicit empty secret", model.Object{"password": ""}, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := &Engine{Version: zabbix.ParseVersion("8.0.0"), Config: &config.Config{Raw: map[string]any{
				"settings": model.Object{"apm_global_db": tc.override},
			}}, Local: map[string]map[string]*LocalItem{"settings": {"apm_global_db": {Data: model.Object{
				"apm_global_db": model.Object{"password": "target-api-secret"},
			}}}}}
			// Legacy store data may still contain API-returned credentials.
			source := model.Object{
				"status": 1, "url": "https://ch", "authentication_type": 0, "username": "zc", "password": "legacy-store-secret",
			}
			_, err := e.prepareSettingsUpdate(model.Object{"apm_global_db": source})
			if err != nil {
				t.Fatal(err)
			}
			db := objectMap(e.apmSettings["apm_global_db"])
			password, present := db["password"]
			if present != tc.present || password != tc.want {
				t.Fatal("API/store password used instead of target secret")
			}
			if db["url"] != "https://ch" || db["username"] != "zc" {
				t.Fatal("secret override lost data-source settings")
			}
			if source["password"] != "legacy-store-secret" {
				t.Fatal("stored data object mutated while preparing the update")
			}
		})
	}
}

func TestAPMDataSourceFailurePreventsProxyCollectionUpdate(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(model.Object{"jsonrpc": "2.0", "id": 1, "error": model.Object{"code": -32602, "message": "Invalid params", "data": "data source rejected"}})
	}))
	defer server.Close()
	api, err := zabbix.New(server.URL, false)
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{API: api, Version: zabbix.ParseVersion("8.0.0"), apmSettings: model.Object{"apm_global_db": model.Object{"status": 1}},
		Dataset: model.Dataset{"proxy": {{Name: "edge", Data: model.Object{"apm": model.Object{"data_collection_status": 1}}}}}}
	if err := e.ApplyAPMSettings(context.Background()); err == nil || calls != 1 || len(e.apmSettings) == 0 {
		t.Fatal("proxy collection must not change after data-source failure")
	}
}
