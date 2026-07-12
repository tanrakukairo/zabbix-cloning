package clone

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/tanrakukairo/zabbix-cloning/internal/model"
)

type interfaceCounts struct{ Total, Create, Update, Delete, Skip, Failed int }
type interfacePlan struct {
	Function       string
	Update, Target model.Object
}

func (e *Engine) updateHostInterfaces(ctx context.Context, host hostPlan) error {
	updates := make([]model.Object, len(host.Interfaces))
	for i, value := range host.Interfaces {
		updates[i] = model.CloneObject(value)
	}
	selectMainInterface(updates)
	current, err := e.API.CallObjects(ctx, "hostinterface.get", model.Object{"output": "extend", "selectDetails": "extend", "hostids": host.ID})
	if err != nil {
		return err
	}
	existing := make([]model.Object, len(current))
	for i, value := range current {
		existing[i] = value
	}
	plans, deletes := buildInterfacePlans(existing, updates)
	counts := interfaceCounts{Total: len(plans) + len(deletes)}
	for _, plan := range plans {
		switch plan.Function {
		case "skip":
			counts.Skip++
		case "create":
			if err := e.createInterface(ctx, host.ID, plan.Update, existing); err != nil {
				e.Log.Debugf("hostinterface.create %s: %v", host.Name, err)
				counts.Failed++
			} else {
				counts.Create++
			}
		case "update":
			if err := e.updateInterface(ctx, plan.Update, plan.Target, existing); err != nil {
				e.Log.Debugf("hostinterface.update %s: %v", host.Name, err)
				counts.Failed++
			} else {
				counts.Update++
			}
		}
		e.interfaceProgress(counts)
	}
	for _, target := range deletes {
		if _, err := e.API.Call(ctx, "hostinterface.delete", []any{target["interfaceid"]}); err != nil {
			e.Log.Debugf("hostinterface.delete %s: %v", host.Name, err)
			counts.Failed++
		} else {
			counts.Delete++
		}
		e.interfaceProgress(counts)
	}
	e.Log.Infof("Host Interface Update: %s", formatInterfaceCounts(counts))
	return nil
}

func selectMainInterface(updates []model.Object) {
	for _, item := range updates {
		if model.Int(item["main"]) == 1 {
			return
		}
	}
	for _, kind := range []int{1, 2, 4, 3} {
		for _, item := range updates {
			if model.Int(item["type"]) == kind {
				item["main"] = 1
				return
			}
		}
	}
}

func buildInterfacePlans(existing, updates []model.Object) ([]interfacePlan, []model.Object) {
	matched := map[string]bool{}
	var plans []interfacePlan
	for _, update := range updates {
		target := findInterface(existing, update, matched)
		function := "create"
		if target != nil {
			matched[model.String(target["interfaceid"])] = true
			if interfaceChanged(update, target) {
				function = "update"
			} else {
				function = "skip"
			}
		}
		plans = append(plans, interfacePlan{Function: function, Update: update, Target: target})
	}
	keys := map[string]bool{}
	for _, update := range updates {
		keys[interfaceKey(update)] = true
	}
	var deletes []model.Object
	for _, item := range existing {
		if !matched[model.String(item["interfaceid"])] && !keys[interfaceKey(item)] {
			deletes = append(deletes, item)
		}
	}
	return plans, deletes
}

func findInterface(existing []model.Object, update model.Object, matched map[string]bool) model.Object {
	var candidates []model.Object
	for _, item := range existing {
		if !matched[model.String(item["interfaceid"])] && interfaceKey(item) == interfaceKey(update) {
			candidates = append(candidates, item)
		}
	}
	if len(candidates) == 1 {
		return candidates[0]
	}
	if len(candidates) > 1 {
		for _, item := range candidates {
			if model.Int(item["main"]) == model.Int(update["main"]) {
				return item
			}
		}
		return candidates[0]
	}
	var sameType []model.Object
	for _, item := range existing {
		if !matched[model.String(item["interfaceid"])] && model.Int(item["type"]) == model.Int(update["type"]) {
			sameType = append(sameType, item)
		}
	}
	if len(sameType) == 1 {
		return sameType[0]
	}
	for _, item := range sameType {
		if model.Int(item["main"]) == model.Int(update["main"]) {
			return item
		}
	}
	return nil
}

func interfaceKey(item model.Object) string {
	details := objectMap(item["details"])
	keys := sortedKeys(details)
	ordered := make([][2]string, 0, len(keys))
	for _, key := range keys {
		ordered = append(ordered, [2]string{key, model.String(details[key])})
	}
	data, _ := json.Marshal([]any{model.Int(item["type"]), model.Int(item["useip"]), model.String(item["ip"]), model.String(item["dns"]), model.String(item["port"]), ordered})
	return string(data)
}
func interfaceChanged(update, target model.Object) bool {
	for key, value := range update {
		if key == "details" {
			for detail, detailValue := range objectMap(value) {
				if model.String(objectMap(target["details"])[detail]) != model.String(detailValue) {
					return true
				}
			}
		} else if model.String(target[key]) != model.String(value) {
			return true
		}
	}
	return false
}

func (e *Engine) createInterface(ctx context.Context, hostID string, update model.Object, existing []model.Object) error {
	data := model.CloneObject(update)
	data["hostid"] = hostID
	old := currentMain(existing, update, nil)
	if _, err := e.API.Call(ctx, "hostinterface.create", data); err == nil {
		_ = e.unsetMain(ctx, old, false)
		return nil
	} else if old == nil {
		return err
	}
	if err := e.unsetMain(ctx, old, true); err != nil {
		return err
	}
	_, err := e.API.Call(ctx, "hostinterface.create", data)
	return err
}
func (e *Engine) updateInterface(ctx context.Context, update, target model.Object, existing []model.Object) error {
	data := model.CloneObject(update)
	data["interfaceid"] = target["interfaceid"]
	old := currentMain(existing, update, target)
	if _, err := e.API.Call(ctx, "hostinterface.update", data); err == nil {
		applyInterface(target, data)
		_ = e.unsetMain(ctx, old, false)
		return nil
	} else if old == nil {
		return err
	}
	if err := e.unsetMain(ctx, old, true); err != nil {
		return err
	}
	_, err := e.API.Call(ctx, "hostinterface.update", data)
	if err == nil {
		applyInterface(target, data)
	}
	return err
}

func currentMain(existing []model.Object, update, target model.Object) model.Object {
	if model.Int(update["main"]) != 1 || target != nil && model.Int(target["main"]) == 1 {
		return nil
	}
	var current []model.Object
	for _, item := range existing {
		if model.Int(item["type"]) == model.Int(update["type"]) && model.Int(item["main"]) == 1 {
			current = append(current, item)
		}
	}
	if len(current) != 1 {
		return nil
	}
	if target != nil && model.String(current[0]["interfaceid"]) == model.String(target["interfaceid"]) {
		return nil
	}
	return current[0]
}
func (e *Engine) unsetMain(ctx context.Context, item model.Object, force bool) error {
	if item == nil || !force && model.Int(item["main"]) == 0 {
		return nil
	}
	_, err := e.API.Call(ctx, "hostinterface.update", model.Object{"interfaceid": item["interfaceid"], "main": 0})
	if err == nil {
		item["main"] = "0"
	}
	return err
}
func applyInterface(target, data model.Object) {
	for key, value := range data {
		if key != "interfaceid" {
			target[key] = value
		}
	}
}
func (e *Engine) interfaceProgress(counts interfaceCounts) {
	e.Log.Progress("\r    Host Interface Update: %s", formatInterfaceCounts(counts))
}
func formatInterfaceCounts(c interfaceCounts) string {
	return fmt.Sprintf("%d/%d (create:%d/update:%d/delete:%d/skip:%d/failed:%d)", c.Create+c.Update+c.Delete+c.Skip+c.Failed, c.Total, c.Create, c.Update, c.Delete, c.Skip, c.Failed)
}
