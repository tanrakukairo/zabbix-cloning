package clone

import (
	"fmt"
	"strings"

	"github.com/tanrakukairo/zabbix-cloning/internal/model"
)

func (e *Engine) enableDryRunVirtualState() {
	virtual := make(map[string]map[string]*LocalItem, len(e.Local))
	for method, items := range e.Local {
		virtual[method] = make(map[string]*LocalItem, len(items))
		for name, item := range items {
			virtual[method][name] = &LocalItem{
				ID: item.ID, Name: item.Name, Data: model.CloneObject(item.Data),
			}
		}
	}
	e.Local = virtual
	e.dryRunVirtual = true
	e.rebuildIDReplace()
}

func isDryRunVirtualID(value any) bool {
	return strings.HasPrefix(model.String(value), "__DRY_RUN_")
}

func (e *Engine) virtualApplyPSK(method, name string, data model.Object) {
	if !e.dryRunVirtual {
		return
	}
	if method == "autoregistration" {
		current := model.Object{}
		for key, item := range e.Local[method] {
			current[key] = item.Data[key]
		}
		for key, value := range data {
			current[key] = value
		}
		e.virtualSetGlobal(method, current)
		return
	}
	item := e.Local[method][name]
	if item == nil {
		return
	}
	for key, value := range data {
		if key != "hostid" && key != "proxyid" {
			item.Data[key] = value
		}
	}
}

func (e *Engine) virtualUpsert(method, name string, data model.Object) {
	if !e.dryRunVirtual || name == "" {
		return
	}
	if e.Local[method] == nil {
		e.Local[method] = map[string]*LocalItem{}
	}
	current := e.Local[method][name]
	if current == nil {
		e.dryRunSequence++
		current = &LocalItem{ID: fmt.Sprintf("__DRY_RUN_%s_%d__", method, e.dryRunSequence), Name: name, Data: model.Object{}}
		e.Local[method][name] = current
	}
	for key, value := range model.CloneObject(data) {
		if spec, ok := e.Params.Methods[method]; ok && key == spec.ID {
			continue
		}
		current.Data[key] = value
	}
	e.rebuildIDReplace()
}

func (e *Engine) virtualDelete(method, name string) {
	if !e.dryRunVirtual || e.Local[method] == nil {
		return
	}
	delete(e.Local[method], name)
	e.rebuildIDReplace()
}

func (e *Engine) virtualSetGlobal(method string, data model.Object) {
	if !e.dryRunVirtual {
		return
	}
	e.Local[method] = map[string]*LocalItem{}
	index := 0
	for _, key := range sortedKeys(data) {
		e.Local[method][key] = &LocalItem{
			ID: fmt.Sprint(index), Name: key, Data: model.Object{key: data[key]},
		}
		index++
	}
	e.rebuildIDReplace()
}

func (e *Engine) virtualApplyConfiguration() {
	if !e.dryRunVirtual {
		return
	}
	for method := range e.Params.ConfigExport {
		if method == "host" || method == "trigger" || method == "template" && e.Config.SkipTemplate {
			continue
		}
		for _, item := range e.Dataset[method] {
			e.virtualUpsert(method, item.Name, item.Data)
		}
	}
}

func (e *Engine) virtualApplyHostPlan(plan hostPlan) {
	if !e.dryRunVirtual {
		return
	}
	data := model.CloneObject(plan.Data)
	if len(plan.Interfaces) > 0 {
		data["interfaces"] = objectValues(plan.Interfaces)
	}
	if plan.Function == "create" {
		e.virtualUpsert("host", plan.Name, data)
		return
	}
	for name, item := range e.Local["host"] {
		if item.ID == plan.ID {
			for key, value := range data {
				if key != "hostid" {
					item.Data[key] = value
				}
			}
			if name != plan.Name && model.String(data["host"]) == plan.Name {
				delete(e.Local["host"], name)
				item.Name = plan.Name
				e.Local["host"][plan.Name] = item
			}
			break
		}
	}
	e.rebuildIDReplace()
}
