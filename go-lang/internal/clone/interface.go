package clone

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/tanrakukairo/zabbix-cloning/internal/model"
)

type interfacePlan struct {
	Function       string
	Update, Target model.Object
}
type hostInterfaceWork struct {
	Host              hostPlan
	Existing          []model.Object
	Plans             []interfacePlan
	Deletes           []model.Object
	PreparationFailed error
}

func (e *Engine) applyHostInterfaces(ctx context.Context, hosts []hostPlan, failedHosts map[string]bool) {
	var targets []hostPlan
	for _, host := range hosts {
		if host.Function != "update" || len(host.Interfaces) == 0 || failedHosts[host.Name] {
			continue
		}
		targets = append(targets, host)
	}
	if len(targets) == 0 {
		return
	}
	works := make([]hostInterfaceWork, len(targets))
	e.runHostApplyWorkers(len(targets), func(index int) {
		works[index] = e.prepareHostInterfaceWork(ctx, targets[index])
	})
	total := 0
	for _, work := range works {
		if work.PreparationFailed != nil {
			total++
		} else {
			total += len(work.Plans) + len(work.Deletes)
		}
	}
	if total == 0 {
		return
	}
	progress := newApplyProgress(e.Log, e.Config.Quiet, "Host Interface Update", total, "create", "update", "delete", "skip")
	var progressMutex sync.Mutex
	e.runHostApplyWorkers(len(works), func(index int) {
		e.applyHostInterfaceWork(ctx, works[index], progress, &progressMutex)
	})
	progress.finish()
}

func (e *Engine) runHostApplyWorkers(count int, process func(int)) {
	jobs := make(chan int)
	var wait sync.WaitGroup
	workers := e.hostApplyWorkers()
	if workers > count {
		workers = count
	}
	for i := 0; i < workers; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for index := range jobs {
				process(index)
			}
		}()
	}
	for index := 0; index < count; index++ {
		jobs <- index
	}
	close(jobs)
	wait.Wait()
}

func (e *Engine) prepareHostInterfaceWork(ctx context.Context, host hostPlan) hostInterfaceWork {
	updates := make([]model.Object, len(host.Interfaces))
	for i, value := range host.Interfaces {
		updates[i] = model.CloneObject(value)
	}
	selectMainInterface(updates)
	current, err := e.API.CallObjects(ctx, "hostinterface.get", model.Object{"output": "extend", "selectDetails": "extend", "hostids": host.ID})
	if err != nil {
		return hostInterfaceWork{Host: host, PreparationFailed: err}
	}
	existing := make([]model.Object, len(current))
	for i, value := range current {
		existing[i] = value
	}
	plans, deletes := buildInterfacePlans(existing, updates)
	return hostInterfaceWork{Host: host, Existing: existing, Plans: plans, Deletes: deletes}
}

func (e *Engine) applyHostInterfaceWork(ctx context.Context, work hostInterfaceWork, progress *applyProgress, mutex *sync.Mutex) {
	record := func(action, target string, err error) {
		mutex.Lock()
		defer mutex.Unlock()
		if err != nil {
			progress.fail(target, err)
		} else {
			progress.record(action)
		}
	}
	if work.PreparationFailed != nil {
		record("", work.Host.Name, fmt.Errorf("hostinterface.get: %w", work.PreparationFailed))
		return
	}
	for _, plan := range work.Plans {
		target := interfaceTarget(work.Host.Name, plan.Function, plan.Update)
		switch plan.Function {
		case "skip":
			record("skip", target, nil)
		case "create":
			record("create", target, e.createInterface(ctx, work.Host.ID, plan.Update, work.Existing))
		case "update":
			record("update", target, e.updateInterface(ctx, plan.Update, plan.Target, work.Existing))
		}
	}
	for _, target := range work.Deletes {
		name := interfaceTarget(work.Host.Name, "delete", target)
		_, err := e.API.Call(ctx, "hostinterface.delete", []any{target["interfaceid"]})
		record("delete", name, err)
	}
}

func interfaceTarget(host, action string, item model.Object) string {
	address := model.String(item["ip"])
	if model.Int(item["useip"]) == 0 || address == "" {
		address = model.String(item["dns"])
	}
	return fmt.Sprintf("%s %s type=%s address=%s port=%s", host, action, model.String(item["type"]), address, model.String(item["port"]))
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
