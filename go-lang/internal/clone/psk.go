package clone

import (
	"context"
	"fmt"
	"sort"

	"github.com/t2-f/zabbix-cloning/internal/model"
)

type pskCredential struct {
	Identity string
	Key      string
}

type pskConfiguration struct {
	Proxy            map[string]pskCredential
	Host             map[string]pskCredential
	Autoregistration *pskCredential
}

type pskCounts struct {
	Total   int
	Updated int
	Missing int
	Skipped int
}

func (e *Engine) ApplyPSK(ctx context.Context) (bool, error) {
	if e.IsMaster() {
		return false, nil
	}
	configuration, err := parsePSKConfiguration(e.Config.Raw["psk"])
	if err != nil {
		return false, err
	}
	counts := pskCounts{}
	if err := e.applyNamedPSK(ctx, "proxy", configuration.Proxy, &counts); err != nil {
		return false, err
	}
	if err := e.applyNamedPSK(ctx, "host", configuration.Host, &counts); err != nil {
		return false, err
	}
	if err := e.applyAutoregistrationPSK(ctx, configuration.Autoregistration, &counts); err != nil {
		return false, err
	}
	if counts.Total > 0 {
		e.Log.Infof("PSK: total:%d/update:%d/missing:%d/skip:%d", counts.Total, counts.Updated, counts.Missing, counts.Skipped)
	}
	return counts.Updated > 0, nil
}

func (e *Engine) applyNamedPSK(ctx context.Context, method string, configured map[string]pskCredential, counts *pskCounts) error {
	if len(configured) == 0 {
		return nil
	}
	counts.Total += len(configured)
	spec, ok := e.Params.Methods[method]
	if !ok {
		return fmt.Errorf("PSK target method %s is not available", method)
	}
	names := sortedPSKNames(configured)
	items, err := e.API.CallObjects(ctx, method+".get", model.Object{
		"output": []string{spec.ID, spec.Name, "tls_accept"},
		"filter": model.Object{spec.Name: names},
	})
	if err != nil {
		return fmt.Errorf("get PSK targets for %s: %w", method, err)
	}
	existing := map[string]map[string]any{}
	for _, item := range items {
		existing[model.String(item[spec.Name])] = item
	}
	if e.dryRunVirtual {
		for _, name := range names {
			local := e.Local[method][name]
			if local == nil {
				delete(existing, name)
				continue
			}
			item := existing[name]
			if item == nil {
				item = map[string]any{}
				existing[name] = item
			}
			item[spec.ID] = local.ID
			item[spec.Name] = name
			for key, value := range local.Data {
				item[key] = value
			}
		}
	}
	for _, name := range names {
		item := existing[name]
		if item == nil {
			counts.Missing++
			continue
		}
		params, update := pskUpdateParameters(spec.ID, item[spec.ID], item["tls_accept"], configured[name])
		if !update {
			counts.Skipped++
			continue
		}
		if _, err := e.API.Call(ctx, method+".update", params); err != nil {
			return fmt.Errorf("%s.update PSK for %s: %w", method, name, err)
		}
		e.virtualApplyPSK(method, name, params)
		counts.Updated++
	}
	return nil
}

func (e *Engine) applyAutoregistrationPSK(ctx context.Context, credential *pskCredential, counts *pskCounts) error {
	if credential == nil {
		return nil
	}
	counts.Total++
	result, err := e.API.Call(ctx, "autoregistration.get", model.Object{})
	if err != nil {
		return fmt.Errorf("autoregistration.get for PSK: %w", err)
	}
	settings, ok := result.(map[string]any)
	if !ok {
		return fmt.Errorf("autoregistration.get returned %T", result)
	}
	if e.dryRunVirtual {
		for key, item := range e.Local["autoregistration"] {
			settings[key] = item.Data[key]
		}
	}
	params, update := pskUpdateParameters("", nil, settings["tls_accept"], *credential)
	if !update {
		counts.Skipped++
		return nil
	}
	if _, err := e.API.Call(ctx, "autoregistration.update", params); err != nil {
		return fmt.Errorf("autoregistration.update PSK: %w", err)
	}
	e.virtualApplyPSK("autoregistration", "", params)
	counts.Updated++
	return nil
}

func pskUpdateParameters(idKey string, id, tlsAccept any, credential pskCredential) (model.Object, bool) {
	accept := model.Int(tlsAccept)
	if accept >= 4 {
		return nil, false
	}
	params := model.Object{
		"tls_psk_identity": credential.Identity,
		"tls_psk":          credential.Key,
	}
	if idKey != "" {
		params[idKey] = id
	}
	if accept == 1 {
		params["tls_accept"] = 2
	}
	return params, true
}

func parsePSKConfiguration(value any) (pskConfiguration, error) {
	configuration := pskConfiguration{Proxy: map[string]pskCredential{}, Host: map[string]pskCredential{}}
	if value == nil {
		return configuration, nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return configuration, fmt.Errorf("psk must be an object")
	}
	for key := range object {
		if key != "proxy" && key != "host" && key != "autoregistration" {
			return configuration, fmt.Errorf("psk.%s is not supported", key)
		}
	}
	var err error
	configuration.Proxy, err = parseNamedPSK(object["proxy"], "psk.proxy")
	if err != nil {
		return configuration, err
	}
	configuration.Host, err = parseNamedPSK(object["host"], "psk.host")
	if err != nil {
		return configuration, err
	}
	if object["autoregistration"] != nil {
		credential, err := parsePSKCredential(object["autoregistration"], "psk.autoregistration")
		if err != nil {
			return configuration, err
		}
		configuration.Autoregistration = &credential
	}
	return configuration, nil
}

func parseNamedPSK(value any, path string) (map[string]pskCredential, error) {
	result := map[string]pskCredential{}
	if value == nil {
		return result, nil
	}
	items, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an object", path)
	}
	for name, value := range items {
		if name == "" {
			return nil, fmt.Errorf("%s target name must not be empty", path)
		}
		credential, err := parsePSKCredential(value, path+"."+name)
		if err != nil {
			return nil, err
		}
		result[name] = credential
	}
	return result, nil
}

func parsePSKCredential(value any, path string) (pskCredential, error) {
	values, ok := value.([]any)
	if !ok || len(values) != 2 {
		return pskCredential{}, fmt.Errorf("%s must be [identity, key]", path)
	}
	credential := pskCredential{Identity: model.String(values[0]), Key: model.String(values[1])}
	if credential.Identity == "" || credential.Key == "" {
		return pskCredential{}, fmt.Errorf("%s identity and key must not be empty", path)
	}
	return credential, nil
}

func sortedPSKNames(values map[string]pskCredential) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
