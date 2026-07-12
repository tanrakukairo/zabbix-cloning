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
		params := model.Object{"macro": macro.Macro, "value": macro.Value, "type": 1}
		if id := macroIDs[macro.Macro]; id != "" {
			method = "usermacro.updateglobal"
			params = model.Object{"globalmacroid": id, "value": macro.Value, "type": 1}
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
