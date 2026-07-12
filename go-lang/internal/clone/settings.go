package clone

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/tanrakukairo/zabbix-cloning/internal/model"
	"github.com/tanrakukairo/zabbix-cloning/internal/zabbix"
)

var settings70Properties = makeStringSet(`
	default_lang default_timezone default_theme search_limit max_overview_table_size max_in_table
	server_check_interval work_period show_technical_errors history_period period_default max_period
	severity_color_0 severity_color_1 severity_color_2 severity_color_3 severity_color_4 severity_color_5
	severity_name_0 severity_name_1 severity_name_2 severity_name_3 severity_name_4 severity_name_5
	custom_color ok_period blink_period problem_unack_color problem_ack_color ok_unack_color ok_ack_color
	problem_unack_style problem_ack_style ok_unack_style ok_ack_style url discovery_groupid
	default_inventory_mode alert_usrgrpid snmptrap_logging login_attempts login_block validate_uri_schemes
	uri_valid_schemes x_frame_options iframe_sandboxing_enabled iframe_sandboxing_exceptions connect_timeout
	socket_timeout media_type_test_timeout item_test_timeout script_timeout report_test_timeout auditlog_enabled
	auditlog_mode ha_failover_delay geomaps_tile_provider geomaps_tile_url geomaps_max_zoom geomaps_attribution
	vault_provider timeout_zabbix_agent timeout_simple_check timeout_snmp_agent timeout_external_check
	timeout_db_monitor timeout_http_agent timeout_ssh_agent timeout_telnet_agent timeout_script timeout_browser
`)

var settingsTimeoutTargets = makeStringSet(`
	zabbix_agent simple_check snmp_agent external_check db_monitor http_agent ssh_agent telnet_agent script browser
`)

var settingsColor = regexp.MustCompile(`^[0-9A-Fa-f]{6}$`)

func (e *Engine) prepareSettingsUpdate(data model.Object) (model.Object, error) {
	e.replaceSettingsIDs(data, false)
	overrides, err := normalizeSettingsConfig(e.Config.Raw["settings"], e.Version)
	if err != nil {
		return nil, err
	}
	for key, value := range overrides {
		data[key] = value
	}
	delete(data, "ha_failover_delay")
	return data, nil
}

func (e *Engine) replaceSettingsIDs(data model.Object, toNames bool) {
	for key, method := range map[string]string{
		"discovery_groupid": "hostgroup",
		"alert_usrgrpid":    "usergroup",
	} {
		value := model.String(data[key])
		if value == "" || value == "0" {
			continue
		}
		if replacement := e.IDReplace[method][value]; replacement != nil {
			data[key] = replacement
		} else if !toNames {
			data[key] = value
		}
	}
}

func normalizeSettingsConfig(value any, version zabbix.Version) (model.Object, error) {
	if value == nil {
		return model.Object{}, nil
	}
	settings, err := settingsObject(value, "settings")
	if err != nil {
		return nil, err
	}
	if len(settings) == 0 {
		return model.Object{}, nil
	}
	if !version.AtLeast(7, 0) {
		return nil, fmt.Errorf("settings configuration requires Zabbix 7.0 or later")
	}

	result := model.Object{}
	if err := expandSeveritySettings(result, settings["severity"]); err != nil {
		return nil, err
	}
	if err := expandTimeoutSettings(result, settings["timeout"]); err != nil {
		return nil, err
	}
	for _, key := range sortedKeys(settings) {
		if key == "severity" || key == "timeout" {
			continue
		}
		if !settings70Properties[key] {
			return nil, fmt.Errorf("settings.%s is not a Zabbix 7.0 settings property", key)
		}
		if key == "ha_failover_delay" {
			return nil, fmt.Errorf("settings.ha_failover_delay is read-only")
		}
		result[key] = settings[key]
	}
	return result, nil
}

func expandSeveritySettings(result model.Object, value any) error {
	if value == nil {
		return nil
	}
	severity, err := settingsObject(value, "settings.severity")
	if err != nil {
		return err
	}
	for _, level := range sortedKeys(severity) {
		if level < "0" || level > "5" || len(level) != 1 {
			return fmt.Errorf("settings.severity.%s must be between 0 and 5", level)
		}
		entry, err := settingsObject(severity[level], "settings.severity."+level)
		if err != nil {
			return err
		}
		if name := model.String(entry["name"]); name != "" {
			result["severity_name_"+level] = name
		}
		if color := model.String(entry["color"]); color != "" {
			if !settingsColor.MatchString(color) {
				return fmt.Errorf("settings.severity.%s.color must be a 6-digit hexadecimal color", level)
			}
			result["severity_color_"+level] = strings.ToUpper(color)
		}
	}
	return nil
}

func expandTimeoutSettings(result model.Object, value any) error {
	if value == nil {
		return nil
	}
	timeouts, err := settingsObject(value, "settings.timeout")
	if err != nil {
		return err
	}
	for _, rawTarget := range sortedKeys(timeouts) {
		target := strings.TrimPrefix(rawTarget, "timeout_")
		if !settingsTimeoutTargets[target] {
			return fmt.Errorf("settings.timeout.%s is not a Zabbix 7.0 timeout target", rawTarget)
		}
		setting := model.String(timeouts[rawTarget])
		if setting == "" {
			return fmt.Errorf("settings.timeout.%s must not be empty", rawTarget)
		}
		result["timeout_"+target] = setting
	}
	return nil
}

func settingsObject(value any, path string) (map[string]any, error) {
	switch object := value.(type) {
	case model.Object:
		return object, nil
	case map[string]any:
		return object, nil
	default:
		return nil, fmt.Errorf("%s must be an object", path)
	}
}

func makeStringSet(value string) map[string]bool {
	result := map[string]bool{}
	for _, item := range strings.Fields(value) {
		result[item] = true
	}
	return result
}
