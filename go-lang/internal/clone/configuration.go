package clone

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/tanrakukairo/zabbix-cloning/internal/model"
)

func (e *Engine) ApplyConfiguration(ctx context.Context) error {
	sections := e.Params.ImportSections(e.NewVersion.MasterVersion)
	base := model.Object{}
	var templates []model.Object
	var triggers []model.Object
	for method, section := range sections {
		items := e.Dataset[method]
		if len(items) == 0 {
			continue
		}
		switch method {
		case "host":
			base[section] = []any{}
		case "template":
			for _, item := range items {
				template := model.CloneObject(item.Data)
				normalizeTemplate(template, e.Version)
				templates = append(templates, template)
			}
		case "trigger":
			for _, item := range items {
				triggers = append(triggers, model.CloneObject(item.Data))
			}
		case "mediatype":
			values := make([]any, 0, len(items))
			for _, item := range items {
				data := model.CloneObject(item.Data)
				normalizeMediaType(data, e.Version)
				values = append(values, data)
			}
			base["media_types"] = values
		default:
			values := make([]any, 0, len(items))
			for _, item := range items {
				values = append(values, model.CloneObject(item.Data))
			}
			base[section] = values
		}
	}
	if err := e.importConfiguration(ctx, base, "base"); err != nil {
		return err
	}
	groups, err := groupTemplates(templates)
	if err != nil {
		return err
	}
	progress := newApplyProgress(e.Log, e.Config.Quiet, "Template Import", len(templates), "success")
	for _, group := range groups {
		for _, template := range group {
			name := model.String(template["name"])
			document := model.Object{"templates": []any{template}}
			needle := "/" + name + "/"
			var templateTriggers []any
			for _, trigger := range triggers {
				if strings.Contains(model.String(trigger["expression"]), needle) {
					templateTriggers = append(templateTriggers, trigger)
				}
			}
			if len(templateTriggers) > 0 {
				document["triggers"] = templateTriggers
			}
			if e.Config.SkipTemplate {
				continue
			}
			if err := e.importConfiguration(ctx, document, name); err != nil {
				progress.fail(name, err)
				continue
			}
			progress.record("success")
		}
	}
	if e.Config.SkipTemplate {
		e.Log.Infof("Template Import: SKIP.")
	} else {
		progress.finish()
	}
	e.virtualApplyConfiguration()
	if err := e.Refresh(ctx); err != nil {
		return err
	}
	return nil
}

func (e *Engine) importConfiguration(ctx context.Context, data model.Object, target string) error {
	if len(data) == 0 {
		return nil
	}
	data["version"] = fmt.Sprintf("%.1f", e.NewVersion.MasterVersion)
	if e.NewVersion.MasterVersion < 7 {
		data["date"] = model.ZabbixTime()
	}
	document := model.Object{"zabbix_export": data}
	source, err := json.Marshal(document)
	if err != nil {
		return err
	}
	keys := sortedKeys(data)
	e.Log.Debugf("Template Import: Execute Import target=%s sections=%v", target, keys)
	_, err = e.API.Call(ctx, "configuration.import", model.Object{"format": "json", "rules": e.Params.ImportRules, "source": string(source)})
	return err
}

func normalizeTemplate(template model.Object, version interface{ AtLeast(int, int) bool }) {
	if !version.AtLeast(6, 4) {
		return
	}
	for _, item := range objects(template["items"]) {
		if model.String(item["type"]) != "HTTP_AGENT" {
			delete(item, "request_method")
		}
	}
	for _, rule := range objects(template["discovery_rules"]) {
		if model.String(rule["type"]) != "HTTP_AGENT" {
			delete(rule, "request_method")
		}
		for _, item := range objects(rule["item_prototypes"]) {
			if model.String(item["type"]) != "HTTP_AGENT" {
				delete(item, "request_method")
			}
		}
	}
}

func normalizeMediaType(data model.Object, version interface{ AtLeast(int, int) bool }) {
	if version.AtLeast(6, 0) && model.String(data["type"]) == "SCRIPT" {
		delete(data, "content_type")
	}
	if version.AtLeast(7, 0) {
		delete(data, "content_type")
	}
}

func groupTemplates(templates []model.Object) ([][]model.Object, error) {
	remaining := append([]model.Object(nil), templates...)
	sort.Slice(remaining, func(i, j int) bool { return model.String(remaining[i]["name"]) < model.String(remaining[j]["name"]) })
	processed := map[string]bool{}
	var groups [][]model.Object
	for len(remaining) > 0 {
		var group, next []model.Object
		for _, template := range remaining {
			dependencies := templateDependencies(template)
			ready := true
			for _, dependency := range dependencies {
				if !processed[dependency] {
					ready = false
					break
				}
			}
			if ready {
				group = append(group, template)
			} else {
				next = append(next, template)
			}
		}
		if len(group) == 0 {
			return nil, fmt.Errorf("template dependency cycle or missing dependency: %v", templateNames(next))
		}
		for _, template := range group {
			processed[model.String(template["name"])] = true
		}
		groups = append(groups, group)
		remaining = next
	}
	return groups, nil
}
func templateDependencies(template model.Object) []string {
	set := map[string]bool{}
	for _, link := range objects(template["templates"]) {
		if name := model.String(link["name"]); name != "" {
			set[name] = true
		}
	}
	for _, rule := range objects(template["discovery_rules"]) {
		for _, prototype := range objects(rule["host_prototypes"]) {
			for _, link := range objects(prototype["templates"]) {
				if name := model.String(link["name"]); name != "" {
					set[name] = true
				}
			}
		}
	}
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
func templateNames(templates []model.Object) []string {
	names := make([]string, 0, len(templates))
	for _, template := range templates {
		names = append(names, model.String(template["name"]))
	}
	return names
}
