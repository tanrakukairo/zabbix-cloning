package clone

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tanrakukairo/zabbix-cloning/internal/config"
	"github.com/tanrakukairo/zabbix-cloning/internal/logx"
	"github.com/tanrakukairo/zabbix-cloning/internal/model"
	"github.com/tanrakukairo/zabbix-cloning/internal/zabbix"
)

type preflightStore struct {
	dryRunStore
	load func() (model.Dataset, error)
}

func (s *preflightStore) Load(context.Context, model.Version) (model.Dataset, error) { return s.load() }

func preflightEngine(t *testing.T, full bool) *Engine {
	t.Helper()
	api, err := zabbix.New("http://127.0.0.1:1", false)
	if err != nil {
		t.Fatal(err)
	}
	api.SetDryRun(true)
	logger, err := logx.New("test", "INFO", "", true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	version := zabbix.ParseVersion("7.0.21")
	params, err := NewParameters(version)
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{API: api, Log: logger, Version: version, Params: params,
		Config:   &config.Config{Role: "replica", DryRun: true, Initialize: true, InitializeFull: full},
		Versions: []model.Version{{VersionID: "source", MasterVersion: 7}},
		Local: map[string]map[string]*LocalItem{
			"action": {"existing": {ID: "10", Name: "existing", Data: model.Object{"name": "existing"}}},
			"host":   {"existing": {ID: "20", Name: "existing", Data: model.Object{"host": "existing"}}},
		},
	}
	e.enableDryRunVirtualState()
	return e
}

func TestReplicaLoadsBeforeInitialize(t *testing.T) {
	for _, full := range []bool{false, true} {
		for _, fail := range []bool{false, true} {
			t.Run(fmt.Sprintf("full=%v/fail=%v", full, fail), func(t *testing.T) {
				e := preflightEngine(t, full)
				loaded := false
				e.Store = &preflightStore{load: func() (model.Dataset, error) {
					loaded = true
					if len(e.API.DryRunMethods()) != 0 || e.Local["action"]["existing"] == nil {
						t.Fatal("target mutated before loading")
					}
					if fail {
						return nil, fmt.Errorf("corrupt source")
					}
					return model.Dataset{}, nil
				}}
				if err := e.prepareReplica(context.Background()); err != nil {
					t.Fatal(err)
				}
				err := e.loadAndPrepareReplica(context.Background(), nil)
				if !loaded {
					t.Fatal("source was not loaded")
				}
				if fail {
					if err == nil || len(e.API.DryRunMethods()) != 0 {
						t.Fatalf("failed source changed target: %v, %v", err, e.API.DryRunMethods())
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if e.Local["action"]["existing"] != nil {
					t.Fatal("initialization did not run after load")
				}
				if full && e.Local["host"]["existing"] != nil {
					t.Fatal("full initialization did not run")
				}
			})
		}
	}
}

func TestReplicaRejectsOldStoreBeforeInitialize(t *testing.T) {
	e := preflightEngine(t, true)
	e.Versions[0].MasterVersion = 6.4
	e.Store = &preflightStore{load: func() (model.Dataset, error) { t.Fatal("old store should not load"); return nil, nil }}
	if err := e.loadAndPrepareReplica(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "7.0") {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(e.API.DryRunMethods()) != 0 {
		t.Fatal("old source changed target")
	}
}

func TestDirectSourceIsValidatedBeforeInitialize(t *testing.T) {
	for _, mode := range []string{"unavailable", "newer", "success"} {
		t.Run(mode, func(t *testing.T) {
			e := preflightEngine(t, true)
			if mode == "success" {
				e.Version = zabbix.ParseVersion("7.4.0")
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if len(e.API.DryRunMethods()) != 0 {
					t.Error("target mutated before direct source finished loading")
				}
				if mode == "unavailable" {
					http.Error(w, "unavailable", http.StatusServiceUnavailable)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": `{"zabbix_export":{"version":"7.4"}}`})
			}))
			defer server.Close()
			api, err := zabbix.New(server.URL, false)
			if err != nil {
				t.Fatal(err)
			}
			direct := &Engine{API: api, Log: e.Log, Version: zabbix.ParseVersion("7.4.0"), Config: &config.Config{Role: "master", TemplateSeparate: 1}, Params: &Parameters{Methods: map[string]MethodSpec{}}, Local: map[string]map[string]*LocalItem{}}
			if mode == "unavailable" {
				direct.Params.Methods["host"] = MethodSpec{ID: "hostid", Name: "host"}
			}
			err = e.loadAndPrepareReplica(context.Background(), direct)
			if mode == "success" {
				if err != nil {
					t.Fatal(err)
				}
				if e.Local["host"]["existing"] != nil || e.API.DryRunMethods()["host.delete"] != 1 {
					t.Fatal("direct source did not initialize target after successful loading")
				}
				return
			}
			if err == nil {
				t.Fatal("expected direct source error")
			}
			if len(e.API.DryRunMethods()) != 0 || e.Local["host"]["existing"] == nil {
				t.Fatal("failed direct source changed target")
			}
		})
	}
}

func TestNewRejectsOldServerBeforeAuthentication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		if request.Method != "apiinfo.version" {
			t.Errorf("unexpected call: %s", request.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": "6.4.0"})
	}))
	defer server.Close()
	_, err := New(context.Background(), &config.Config{Endpoint: server.URL}, nil)
	if err == nil || !strings.Contains(err.Error(), "7.0") {
		t.Fatalf("unexpected error: %v", err)
	}
}
