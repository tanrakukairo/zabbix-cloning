package clone

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/t2-f/zabbix-cloning/internal/config"
	"github.com/t2-f/zabbix-cloning/internal/logx"
	"github.com/t2-f/zabbix-cloning/internal/model"
	"github.com/t2-f/zabbix-cloning/internal/store"
	"github.com/t2-f/zabbix-cloning/internal/zabbix"
)

const (
	uniqueTag    = "ZC_UUID"
	versionMacro = "{$ZC_VERSION}"
	superUser    = "Admin"
	superGroup   = "Zabbix administrators"
)

type LocalItem struct {
	ID   string
	Name string
	Data model.Object
}

type Engine struct {
	Config     *config.Config
	Log        *logx.Logger
	API        *zabbix.Client
	Version    zabbix.Version
	Params     *Parameters
	Store      store.Store
	Versions   []model.Version
	Dataset    model.Dataset
	Local      map[string]map[string]*LocalItem
	IDReplace  map[string]map[string]any
	NewVersion model.Version

	dryRunVirtual  bool
	dryRunSequence int
}

func New(ctx context.Context, cfg *config.Config, logger *logx.Logger) (*Engine, error) {
	api, err := zabbix.New(cfg.Endpoint, cfg.SelfCert)
	if err != nil {
		return nil, err
	}
	api.SetDryRun(cfg.DryRun)
	if err := api.CheckServerName(ctx, cfg.Node, cfg.Token, cfg.User, cfg.Password); err != nil {
		return nil, err
	}
	if err := api.Authenticate(ctx, cfg.Token, cfg.User, cfg.Password); err != nil {
		return nil, err
	}
	version, err := api.Version(ctx)
	if err != nil {
		return nil, err
	}
	params, err := NewParameters(version)
	if err != nil {
		return nil, err
	}
	engine := &Engine{Config: cfg, Log: logger, API: api, Version: version, Params: params, Local: map[string]map[string]*LocalItem{}, IDReplace: map[string]map[string]any{}}
	if cfg.StoreType != "direct" {
		engine.Store, err = store.Open(ctx, cfg)
		if err != nil {
			return nil, err
		}
	}
	return engine, nil
}

func (e *Engine) Close() error {
	if e.Store != nil {
		return e.Store.Close()
	}
	return nil
}
func (e *Engine) IsMaster() bool  { return e.Config.Role == "master" }
func (e *Engine) IsReplica() bool { return e.Config.Role == "replica" }

func (e *Engine) FirstProcess(ctx context.Context) error {
	if e.Store != nil {
		versions, err := e.Store.Versions(ctx, "")
		if err != nil {
			return fmt.Errorf("get versions: %w", err)
		}
		e.Versions = versions
		if !e.IsMaster() && len(versions) == 0 {
			return fmt.Errorf("no versions in datastore")
		}
	}
	if !e.Config.DirectSource {
		if err := e.applySecretGlobalMacros(ctx); err != nil {
			return fmt.Errorf("set secret global macros: %w", err)
		}
	}
	if err := e.Refresh(ctx); err != nil {
		return err
	}
	if e.Config.DryRun && e.IsReplica() {
		e.enableDryRunVirtualState()
	}
	if e.IsMaster() {
		return e.ensureHostUUIDs(ctx)
	}
	return e.prepareReplica(ctx)
}

func (e *Engine) Refresh(ctx context.Context) error {
	if e.dryRunVirtual {
		e.rebuildIDReplace()
		return nil
	}
	local := map[string]map[string]*LocalItem{}
	for method, spec := range e.Params.Methods {
		result, err := e.API.Call(ctx, method+".get", spec.Options)
		if err != nil {
			return fmt.Errorf("%s.get: %w", method, err)
		}
		local[method] = map[string]*LocalItem{}
		if contains(e.Params.Global, method) {
			object, ok := result.(map[string]any)
			if !ok {
				return fmt.Errorf("%s.get returned %T", method, result)
			}
			index := 0
			keys := sortedKeys(object)
			for _, key := range keys {
				local[method][key] = &LocalItem{ID: fmt.Sprint(index), Name: key, Data: model.Object{key: object[key]}}
				index++
			}
			continue
		}
		values, ok := result.([]any)
		if !ok {
			return fmt.Errorf("%s.get returned %T", method, result)
		}
		for _, value := range values {
			object, ok := value.(map[string]any)
			if !ok {
				continue
			}
			name := model.String(object[spec.Name])
			id := model.String(object[spec.ID])
			delete(object, spec.ID)
			if name != "" {
				local[method][name] = &LocalItem{ID: id, Name: name, Data: object}
			}
		}
	}
	e.Local = local
	e.rebuildIDReplace()
	return nil
}

func (e *Engine) rebuildIDReplace() {
	e.IDReplace = map[string]map[string]any{}
	for method, items := range e.Local {
		e.IDReplace[method] = map[string]any{}
		for _, item := range items {
			if item.ID == "" || item.Name == "" {
				continue
			}
			e.IDReplace[method][item.ID] = item.Name
			e.IDReplace[method][item.Name] = item.ID
		}
	}
}

func (e *Engine) ensureHostUUIDs(ctx context.Context) error {
	hosts := e.Local["host"]
	total, set, exists := len(hosts), 0, 0
	for _, host := range hosts {
		tags := objects(host.Data["tags"])
		found := false
		for _, tag := range tags {
			if model.String(tag["tag"]) == uniqueTag {
				found = true
				break
			}
		}
		if found {
			exists++
			continue
		}
		tags = append(tags, model.Object{"tag": uniqueTag, "value": newUUID()})
		host.Data["tags"] = objectValues(tags)
		if _, err := e.API.Call(ctx, "host.update", model.Object{"hostid": host.ID, "tags": host.Data["tags"]}); err != nil {
			return fmt.Errorf("set UUID on host %s: %w", host.Name, err)
		}
		set++
		e.Log.Progress("\r    Set Host UUID: %d/%d (exist:%d/set:%d)", exists+set, total, exists, set)
	}
	if total > 0 {
		e.Log.Infof("Set Host UUID: %d/%d (exist:%d/set:%d)", exists+set, total, exists, set)
	}
	return nil
}

func (e *Engine) prepareReplica(ctx context.Context) error {
	version, err := store.Latest(e.Versions, e.Config.TargetVersion)
	if err != nil {
		return err
	}
	if e.Version.Float() < version.MasterVersion {
		return fmt.Errorf("target Zabbix %s is older than store data %.1f", e.Version.String(), version.MasterVersion)
	}
	e.NewVersion = version
	e.Log.Infof("Cloning Version: %s", version.VersionID)
	if e.Config.Initialize {
		return e.initializeReplica(ctx)
	}
	return nil
}

func (e *Engine) initializeReplica(ctx context.Context) error {
	for _, method := range []string{"correlation", "drule", "action", "script", "maintenance"} {
		spec, ok := e.Params.Methods[method]
		if !ok {
			continue
		}
		ids := make([]any, 0, len(e.Local[method]))
		for _, item := range e.Local[method] {
			ids = append(ids, item.ID)
		}
		if len(ids) > 0 {
			if _, err := e.API.Call(ctx, method+".delete", ids); err != nil {
				return fmt.Errorf("initialize %s: %w", method, err)
			}
		}
		if e.dryRunVirtual {
			e.Local[method] = map[string]*LocalItem{}
		}
		_ = spec
	}
	return e.Refresh(ctx)
}

func (e *Engine) ConvertIDs(value any, toNames bool) any {
	return e.convertIDsWithKey("", value, toNames)
}

func (e *Engine) convertIDsWithKey(key string, value any, toNames bool) any {
	switch current := value.(type) {
	case model.Object:
		out := model.Object{}
		for childKey, child := range current {
			if method := e.Params.IDMethod[childKey]; method != "" {
				out[childKey] = e.convertIdentifier(method, child, toNames)
				continue
			}
			out[childKey] = e.convertIDsWithKey(childKey, child, toNames)
		}
		return out
	case map[string]any:
		out := model.Object{}
		for childKey, child := range current {
			if method := e.Params.IDMethod[childKey]; method != "" {
				out[childKey] = e.convertIdentifier(method, child, toNames)
				continue
			}
			out[childKey] = e.convertIDsWithKey(childKey, child, toNames)
		}
		return out
	case []any:
		out := make([]any, len(current))
		for index, child := range current {
			out[index] = e.convertIDsWithKey(key, child, toNames)
		}
		return out
	default:
		return current
	}
}

func (e *Engine) convertIdentifier(method string, value any, toNames bool) any {
	switch values := value.(type) {
	case []any:
		out := make([]any, len(values))
		for index, item := range values {
			out[index] = e.Params.Replace(method, item, e.IDReplace)
		}
		return out
	default:
		return e.Params.Replace(method, values, e.IDReplace)
	}
}

func (e *Engine) SetVersionCode(ctx context.Context, initializing bool) error {
	value := e.NewVersion.VersionID
	if initializing {
		value = "__NOT_YET_CLONE__"
	}
	data := model.Object{"macro": versionMacro, "value": value}
	method := "usermacro.createglobal"
	if current := e.Local["usermacro"][versionMacro]; current != nil && !initializing {
		method = "usermacro.updateglobal"
		data["globalmacroid"] = current.ID
		delete(data, "macro")
	}
	_, err := e.API.Call(ctx, method, data)
	if err != nil {
		return err
	}
	e.virtualUpsert("usermacro", versionMacro, data)
	e.Log.Infof("Set VersionCode Globalmacro: Success")
	return nil
}

func marshalObject(value any) model.Object {
	data, _ := json.Marshal(value)
	var object model.Object
	_ = json.Unmarshal(data, &object)
	return object
}
func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
func objects(value any) []model.Object {
	values, _ := value.([]any)
	out := make([]model.Object, 0, len(values))
	for _, value := range values {
		switch object := value.(type) {
		case model.Object:
			out = append(out, object)
		case map[string]any:
			out = append(out, object)
		}
	}
	return out
}
func objectValues(values []model.Object) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}
func newUUID() string {
	var data [16]byte
	_, _ = rand.Read(data[:])
	data[6] = (data[6] & 0x0f) | 0x40
	data[8] = (data[8] & 0x3f) | 0x80
	text := hex.EncodeToString(data[:])
	return strings.Join([]string{text[:8], text[8:12], text[12:16], text[16:20], text[20:]}, "-")
}
