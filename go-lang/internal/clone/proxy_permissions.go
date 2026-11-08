package clone

import (
	"fmt"

	"github.com/tanrakukairo/zabbix-cloning/internal/model"
)

var proxyPermissionRelations = []struct {
	field  string
	method string
	id     string
}{
	{"proxies", "proxy", "proxyid"},
	{"proxy_groups", "proxygroup", "proxy_groupid"},
}

func (e *Engine) storeProxyPermissions(data model.Object) error {
	for _, relation := range proxyPermissionRelations {
		value, present := data[relation.field]
		if !present {
			continue
		}
		raw, ok := value.([]any)
		entries := objects(value)
		if !ok || len(raw) != len(entries) {
			return fmt.Errorf("%s must be an array of source references", relation.field)
		}
		names := make([]any, 0)
		for _, reference := range entries {
			id := model.String(reference[relation.id])
			name := model.String(e.IDReplace[relation.method][id])
			item := e.Local[relation.method][name]
			if item == nil || item.ID != id {
				return fmt.Errorf("%s references unknown source ID %q", relation.field, id)
			}
			names = append(names, name)
		}
		data[relation.field] = names
	}
	return nil
}

func (e *Engine) resolveProxyPermissions(data model.Object) error {
	for _, relation := range proxyPermissionRelations {
		value, present := data[relation.field]
		if !present {
			continue
		}
		names, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s must be an array of target names", relation.field)
		}
		references := make([]any, 0)
		for _, rawName := range names {
			name := model.String(rawName)
			item := e.Local[relation.method][name]
			if item == nil {
				return fmt.Errorf("%s references unknown target %q", relation.field, name)
			}
			references = append(references, model.Object{relation.id: item.ID})
		}
		data[relation.field] = references
	}
	return nil
}
