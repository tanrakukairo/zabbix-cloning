package clone

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/t2-f/zabbix-cloning/internal/model"
)

func (e *Engine) ChangePassword(ctx context.Context) error {
	if !e.Config.UpdatePassword {
		return nil
	}
	admin := e.Local["user"][e.Config.User]
	if admin == nil {
		return fmt.Errorf("user %s does not exist", e.Config.User)
	}
	data := model.Object{"userid": admin.ID, "passwd": e.Config.Password}
	if e.Version.AtLeast(6, 4) {
		data["current_passwd"] = "zabbix"
		if current := model.String(e.Config.Raw["platform_password"]); current != "" {
			data["current_passwd"] = current
		}
	}
	_, err := e.API.Call(ctx, "user.update", data)
	return err
}

func (e *Engine) ApplyAlertMedia(ctx context.Context) error {
	if e.IsReplica() {
		e.Log.Infof("Alert Media: SKIP, replica node.")
		return nil
	}
	settings := objectMap(e.Config.Raw["media_settings"])
	if len(settings) == 0 {
		return nil
	}
	weekdays := map[string]int{"MON": 1, "TUE": 2, "WED": 3, "THU": 4, "FRI": 5, "SAT": 6, "SUN": 7}
	users := map[string][]any{}
	for mediaName, rawUsers := range settings {
		mediaID := e.IDReplace["mediatype"][mediaName]
		if mediaID == nil {
			continue
		}
		for userName, raw := range objectMap(rawUsers) {
			userID := e.IDReplace["user"][userName]
			if userID == nil {
				continue
			}
			value := objectMap(raw)
			addresses := toAnyList(value["to"])
			if len(addresses) == 0 {
				continue
			}
			severity := 0
			for level := 0; level < 6; level++ {
				if model.Bool(objectMap(value["severity"])[fmt.Sprint(level)], false) {
					severity += 1 << level
				}
			}
			var periods []string
			for day, hours := range objectMap(value["work_time"]) {
				if model.String(hours) != "" {
					periods = append(periods, fmt.Sprintf("%d,%s", weekdays[strings.ToUpper(day)], model.String(hours)))
				}
			}
			users[model.String(userID)] = append(users[model.String(userID)], model.Object{"mediatypeid": mediaID, "sendto": addresses, "active": 0, "severity": severity, "period": strings.Join(periods, ";")})
		}
	}
	for userID, medias := range users {
		if _, err := e.API.Call(ctx, "user.update", model.Object{"userid": userID, "medias": medias}); err != nil {
			return err
		}
		if e.dryRunVirtual {
			for _, user := range e.Local["user"] {
				if user.ID == userID {
					user.Data["medias"] = medias
					break
				}
			}
		}
	}
	return nil
}

func toAnyList(value any) []any {
	switch values := value.(type) {
	case []any:
		return values
	case string:
		if values != "" {
			return []any{values}
		}
	}
	return nil
}

func (e *Engine) SetAlertStop(ctx context.Context) error {
	var ids []any
	for name, item := range e.Local["maintenance"] {
		if name == "__ZC_UPDATE__" {
			ids = append(ids, item.ID)
		}
	}
	if len(ids) > 0 {
		if _, err := e.API.Call(ctx, "maintenance.delete", ids); err != nil {
			return err
		}
		if e.dryRunVirtual {
			e.virtualDelete("maintenance", "__ZC_UPDATE__")
		}
	}
	groups := make([]any, 0, len(e.Local["hostgroup"]))
	for _, item := range e.Local["hostgroup"] {
		groups = append(groups, model.Object{"groupid": item.ID})
	}
	now := model.NowUnix()
	data := model.Object{"name": "__ZC_UPDATE__", "active_since": now, "active_till": now + 600, "maintenance_type": 0, "timeperiods": []any{model.Object{"timeperiod_type": 0, "start_date": now, "period": 600}}, "groups": groups}
	if _, err := e.API.Call(ctx, "maintenance.create", data); err != nil {
		return err
	}
	e.virtualUpsert("maintenance", "__ZC_UPDATE__", data)
	e.Log.Infof("Set AlertStop in Update: Start from NOW to 600s after.")
	return e.Refresh(ctx)
}

func (e *Engine) ExecuteCheckNow(ctx context.Context) error {
	if !e.Config.CheckNowExecute {
		return nil
	}
	intervals := normalizeIntervals(e.Config.CheckNowInterval)
	var hostIDs []any
	virtualHosts := 0
	for _, host := range e.Local["host"] {
		if isDryRunVirtualID(host.ID) {
			virtualHosts++
			continue
		}
		hostIDs = append(hostIDs, host.ID)
	}
	if virtualHosts > 0 {
		e.Log.Infof("DRY RUN: CheckNow item discovery excludes %d virtually created hosts", virtualHosts)
	}
	if len(hostIDs) == 0 {
		e.Log.Infof("DRY RUN: CheckNow has no existing hosts to inspect")
		return nil
	}
	output := []any{"itemid", "type", "master_itemid"}
	lld, err := e.API.CallObjects(ctx, "discoveryrule.get", model.Object{"output": output, "hostids": hostIDs})
	if err != nil {
		return err
	}
	targets, skipped, err := e.checkNowTargets(ctx, lld)
	if err != nil {
		return err
	}
	if err := e.runCheckNow(ctx, "LLDs", targets, skipped); err != nil {
		return err
	}
	items, err := e.API.CallObjects(ctx, "item.get", model.Object{"output": output, "hostids": hostIDs, "filter": model.Object{"delay": intervals}})
	if err != nil {
		return err
	}
	targets, skipped, err = e.checkNowTargets(ctx, items)
	if err != nil {
		return err
	}
	return e.runCheckNow(ctx, "TargetInterval["+strings.Join(intervals, "/")+"]", targets, skipped)
}

func (e *Engine) checkNowTargets(ctx context.Context, items []map[string]any) ([]any, int, error) {
	var masterIDs []any
	for _, item := range items {
		if model.Int(item["master_itemid"]) != 0 {
			masterIDs = append(masterIDs, item["master_itemid"])
		}
	}
	masterTypes := map[string]string{}
	if len(masterIDs) > 0 {
		masters, err := e.API.CallObjects(ctx, "item.get", model.Object{"output": []any{"itemid", "type"}, "itemids": masterIDs})
		if err != nil {
			return nil, 0, err
		}
		for _, item := range masters {
			masterTypes[model.String(item["itemid"])] = model.String(item["type"])
		}
	}
	seen := map[string]bool{}
	var targets []any
	skipped := 0
	for _, item := range items {
		target := item["itemid"]
		kind := model.String(item["type"])
		if model.Int(item["master_itemid"]) != 0 {
			target = item["master_itemid"]
			kind = masterTypes[model.String(target)]
		}
		if kind == "7" {
			skipped++
			continue
		}
		key := model.String(target)
		if !seen[key] {
			seen[key] = true
			targets = append(targets, target)
		}
	}
	return targets, skipped, nil
}

func (e *Engine) runCheckNow(ctx context.Context, name string, targets []any, skipped int) error {
	if len(targets) == 0 {
		e.Log.Infof("%s 0 items (skip:%d): No targets.", name, skipped)
		return nil
	}
	if !e.Config.DryRun {
		time.Sleep(time.Duration(e.Config.CheckNowWait) * time.Second)
	}
	params := make([]any, len(targets))
	for i, target := range targets {
		params[i] = model.Object{"type": "6", "request": model.Object{"itemid": target}}
	}
	if _, err := e.API.Call(ctx, "task.create", params); err != nil {
		return err
	}
	e.Log.Infof("%s %d items (skip:%d): Success.", name, len(targets), skipped)
	return nil
}

func normalizeIntervals(values []string) []string {
	pattern := regexp.MustCompile(`^([0-9]+)([mhd]?)$`)
	set := map[string]bool{}
	for _, value := range values {
		match := pattern.FindStringSubmatch(value)
		if len(match) != 3 {
			continue
		}
		number, _ := strconv.Atoi(match[1])
		switch match[2] {
		case "m":
			number *= 60
		case "h":
			number *= 3600
		case "d":
			number *= 86400
		}
		set[fmt.Sprint(number)] = true
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	return result
}
