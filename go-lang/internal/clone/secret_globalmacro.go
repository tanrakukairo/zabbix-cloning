package clone

import (
	"context"
	"fmt"
	"strings"

	"github.com/tanrakukairo/zabbix-cloning/internal/model"
)

type secretGlobalMacro struct {
	Macro string
	Value string
}

type secretUserMacro struct {
	Host  string
	Macro string
	Value string
}

func (e *Engine) ApplySecretMacros(ctx context.Context) error {
	if e.IsMaster() {
		return nil
	}
	if err := e.applySecretGlobalMacros(ctx); err != nil {
		return fmt.Errorf("set secret global macros: %w", err)
	}
	if err := e.applySecretUserMacros(ctx); err != nil {
		return fmt.Errorf("set secret user macros: %w", err)
	}
	return nil
}

func (e *Engine) applySecretGlobalMacros(ctx context.Context) error {
	macros, err := parseSecretGlobalMacros(e.Config.Raw["secret_globalmacro"])
	if err != nil {
		return err
	}
	if len(macros) == 0 {
		return nil
	}

	existing, err := e.API.CallObjects(ctx, "usermacro.get", model.Object{
		"output":      []string{"globalmacroid", "macro", "type"},
		"globalmacro": true,
	})
	if err != nil {
		return fmt.Errorf("get global macros: %w", err)
	}
	macroIDs := map[string]string{}
	for _, item := range existing {
		macro := model.String(item["macro"])
		if macro != "" {
			macroIDs[macro] = model.String(item["globalmacroid"])
		}
	}

	created, updated := 0, 0
	for _, macro := range macros {
		method := "usermacro.createglobal"
		params := model.Object{"macro": macro.Macro, "value": macro.Value, "type": macroTypeSecret}
		if id := macroIDs[macro.Macro]; id != "" {
			method = "usermacro.updateglobal"
			params = model.Object{"globalmacroid": id, "value": macro.Value, "type": macroTypeSecret}
			updated++
		} else {
			created++
		}
		if _, err := e.API.Call(ctx, method, params); err != nil {
			return fmt.Errorf("%s %s: %w", method, macro.Macro, err)
		}
	}
	e.Log.Infof("Secret GlobalMacro: total:%d/create:%d/update:%d", len(macros), created, updated)
	return nil
}

func (e *Engine) applySecretUserMacros(ctx context.Context) error {
	macros, err := parseSecretUserMacros(e.Config.Raw["secret_usermacro"])
	if err != nil {
		return err
	}
	if len(macros) == 0 {
		return nil
	}

	targets := make([]secretUserMacro, 0, len(macros))
	hostIDs := make([]any, 0)
	seenHostIDs := map[string]bool{}
	for _, macro := range macros {
		host := e.Local["host"][macro.Host]
		if host == nil || host.ID == "" {
			continue
		}
		targets = append(targets, macro)
		if !isDryRunVirtualID(host.ID) && !seenHostIDs[host.ID] {
			hostIDs = append(hostIDs, host.ID)
			seenHostIDs[host.ID] = true
		}
	}
	if len(targets) == 0 {
		e.Log.Infof("Secret UserMacro: total:%d/create:0/update:0/skip:%d", len(macros), len(macros))
		return nil
	}

	existing := []map[string]any{}
	if len(hostIDs) > 0 {
		existing, err = e.API.CallObjects(ctx, "usermacro.get", model.Object{
			"output":  []string{"hostmacroid", "hostid", "macro", "type", "automatic"},
			"hostids": hostIDs,
		})
		if err != nil {
			return fmt.Errorf("get host macros: %w", err)
		}
	}
	macroIDs := map[string]string{}
	for _, item := range existing {
		hostID := model.String(item["hostid"])
		macro := model.String(item["macro"])
		if hostID != "" && macro != "" {
			macroIDs[secretUserMacroKey(hostID, macro)] = model.String(item["hostmacroid"])
		}
	}

	created, updated := 0, 0
	for _, macro := range targets {
		host := e.Local["host"][macro.Host]
		method := "usermacro.create"
		params := model.Object{
			"hostid": host.ID, "macro": macro.Macro, "value": macro.Value, "type": macroTypeSecret,
		}
		if id := macroIDs[secretUserMacroKey(host.ID, macro.Macro)]; id != "" {
			method = "usermacro.update"
			params = model.Object{"hostmacroid": id, "value": macro.Value, "type": macroTypeSecret}
			updated++
		} else {
			created++
		}
		if _, err := e.API.Call(ctx, method, params); err != nil {
			return fmt.Errorf("%s %s/%s: %w", method, macro.Host, macro.Macro, err)
		}
	}
	e.Log.Infof("Secret UserMacro: total:%d/create:%d/update:%d/skip:%d", len(macros), created, updated, len(macros)-len(targets))
	return nil
}

func secretUserMacroKey(hostID, macro string) string {
	return hostID + "\x00" + macro
}

func parseSecretGlobalMacros(value any) ([]secretGlobalMacro, error) {
	if value == nil {
		return nil, nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("secret_globalmacro must be an array")
	}
	macros := make([]secretGlobalMacro, 0, len(items))
	seen := map[string]bool{}
	for index, item := range items {
		object := objectMap(item)
		if len(object) == 0 {
			return nil, fmt.Errorf("secret_globalmacro[%d] must be an object", index)
		}
		macro := strings.TrimSpace(model.String(object["macro"]))
		if macro == "" {
			return nil, fmt.Errorf("secret_globalmacro[%d].macro must not be empty", index)
		}
		if seen[macro] {
			return nil, fmt.Errorf("secret_globalmacro contains duplicate macro %s", macro)
		}
		value, exists := object["value"]
		if !exists {
			return nil, fmt.Errorf("secret_globalmacro[%d].value is required", index)
		}
		seen[macro] = true
		macros = append(macros, secretGlobalMacro{Macro: macro, Value: model.String(value)})
	}
	return macros, nil
}

func parseSecretUserMacros(value any) ([]secretUserMacro, error) {
	if value == nil {
		return nil, nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("secret_usermacro must be an array")
	}
	macros := make([]secretUserMacro, 0, len(items))
	seen := map[string]bool{}
	for index, item := range items {
		object := objectMap(item)
		if len(object) == 0 {
			return nil, fmt.Errorf("secret_usermacro[%d] must be an object", index)
		}
		host := strings.TrimSpace(model.String(object["host"]))
		if host == "" {
			return nil, fmt.Errorf("secret_usermacro[%d].host must not be empty", index)
		}
		macro := strings.TrimSpace(model.String(object["macro"]))
		if macro == "" {
			return nil, fmt.Errorf("secret_usermacro[%d].macro must not be empty", index)
		}
		key := host + "\x00" + macro
		if seen[key] {
			return nil, fmt.Errorf("secret_usermacro contains duplicate macro %s for host %s", macro, host)
		}
		value, exists := object["value"]
		if !exists {
			return nil, fmt.Errorf("secret_usermacro[%d].value is required", index)
		}
		seen[key] = true
		macros = append(macros, secretUserMacro{Host: host, Macro: macro, Value: model.String(value)})
	}
	return macros, nil
}
