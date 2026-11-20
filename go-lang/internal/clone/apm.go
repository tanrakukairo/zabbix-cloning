package clone

import (
	"context"
	"fmt"
	"strings"

	"github.com/tanrakukairo/zabbix-cloning/internal/model"
)

// ApplyAPMSettings runs after proxy identities and user-group permissions exist.
// Configure the shared data source before enabling telemetry collection.
func (e *Engine) ApplyAPMSettings(ctx context.Context) error {
	if !e.Version.AtLeast(8, 0) {
		return nil
	}
	changed := false
	if len(e.apmSettings) > 0 {
		data := model.CloneObject(e.apmSettings)
		if _, err := e.API.Call(ctx, "settings.update", data); err != nil {
			return fmt.Errorf("APM data source: %w", err)
		}
		e.virtualSetGlobal("settings", data)
		e.apmSettings = nil
		changed = true
		e.Log.Infof("APM Data Source: Success")
	}
	for _, item := range e.Dataset["proxy"] {
		value, exists := item.Data["apm"]
		if !exists {
			continue
		}
		local := e.Local["proxy"][item.Name]
		if local == nil {
			return fmt.Errorf("APM proxy %q does not exist on target", item.Name)
		}
		apm := model.CloneObject(objectMap(value))
		if model.Int(apm["data_collection_status"]) == 0 {
			delete(apm, "max_messages_per_second")
			delete(apm, "additional_resource_attributes")
		}
		data := model.Object{"proxyid": local.ID, "apm": apm}
		if _, err := e.API.Call(ctx, "proxy.update", data); err != nil {
			return fmt.Errorf("APM proxy %q: %w", item.Name, err)
		}
		e.virtualUpsert("proxy", item.Name, data)
		changed = true
		e.Log.Infof("APM Proxy[%s]: Success", item.Name)
	}
	if changed {
		return e.Refresh(ctx)
	}
	return nil
}

func normalizeAPMDataSource(db model.Object) {
	if status, present := db["status"]; present && model.Int(status) == 0 {
		for field := range db {
			if field != "status" {
				delete(db, field)
			}
		}
		return
	}
	if auth, present := db["authentication_type"]; present && model.Int(auth) == 1 {
		delete(db, "username")
		delete(db, "password")
	}
	if url, present := db["url"]; present && !strings.HasPrefix(strings.ToLower(model.String(url)), "https://") {
		delete(db, "ssl_verify_peer")
		delete(db, "ssl_verify_host")
	}
	if peer, present := db["ssl_verify_peer"]; present && model.Int(peer) == 0 {
		delete(db, "ssl_verify_host")
	}
}
