package clone

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/t2-f/zabbix-cloning/internal/model"
	"github.com/t2-f/zabbix-cloning/internal/zabbix"
)

type MethodSpec struct {
	ID      string         `json:"id"`
	Name    string         `json:"name"`
	Options map[string]any `json:"options"`
}

type Parameters struct {
	Methods      map[string]MethodSpec
	Global       []string
	ConfigExport map[string]string
	Pre          []string
	Mid          []string
	Post         []string
	Account      []string
	Extend       []string
	ImportRules  map[string]any
	Discard      map[string]any
	IDMethod     map[string]string
}

type parameterData struct {
	Methods  map[string]MethodSpec `json:"methods"`
	Sections struct {
		Global       []string          `json:"GLOBAL"`
		ConfigExport map[string]string `json:"CONFIG_EXPORT"`
		Pre          []string          `json:"PRE"`
		Mid          []string          `json:"MID"`
		Post         []string          `json:"POST"`
		Account      []string          `json:"ACCOUNT"`
		Extend       []string          `json:"EXTEND"`
	} `json:"sections"`
	Rules   map[string]any `json:"rules"`
	Discard map[string]any `json:"discard"`
}

func NewParameters(version zabbix.Version) (*Parameters, error) {
	if version.Major < 6 {
		return nil, fmt.Errorf("Go implementation supports Zabbix 6.0 or newer; use prototype for %s", version.String())
	}
	var data parameterData
	decoder := json.NewDecoder(strings.NewReader(parameterJSON))
	decoder.UseNumber()
	if err := decoder.Decode(&data); err != nil {
		return nil, err
	}
	if version.Major < 7 {
		delete(data.Methods, "proxygroup")
		delete(data.Methods, "mfa")
		delete(data.Methods, "connector")
		data.Sections.Pre = removeStrings(data.Sections.Pre, "connector", "proxygroup")
		data.Sections.Post = removeStrings(data.Sections.Post, "mfa")
		if proxy, ok := data.Methods["proxy"]; ok {
			proxy.Name = "host"
			proxy.Options = map[string]any{"output": []any{"host", "status", "proxy_address", "tls_connect", "tls_accept", "tls_issuer", "tls_subject", "description"}, "selectInterface": []any{"useip", "ip", "dns", "port"}}
			data.Methods["proxy"] = proxy
		}
	}
	params := &Parameters{
		Methods: data.Methods, Global: data.Sections.Global, ConfigExport: data.Sections.ConfigExport,
		Pre: data.Sections.Pre, Mid: data.Sections.Mid, Post: data.Sections.Post, Account: data.Sections.Account,
		Extend: data.Sections.Extend, ImportRules: data.Rules, Discard: data.Discard, IDMethod: map[string]string{},
	}
	for method, spec := range params.Methods {
		if spec.ID != "" {
			params.IDMethod[spec.ID] = method
		}
	}
	params.IDMethod["groupid"] = "hostgroup"
	return params, nil
}

func (p *Parameters) Section(name string) []string {
	switch name {
	case "PRE":
		return p.Pre
	case "MID":
		return p.Mid
	case "POST":
		return p.Post
	case "ACCOUNT":
		return p.Account
	case "EXTEND":
		return p.Extend
	case "GLOBAL":
		return p.Global
	default:
		return nil
	}
}

func (p *Parameters) ImportSections(masterVersion float64) map[string]string {
	sections := map[string]string{"hostgroup": "groups", "template": "templates", "host": "hosts", "trigger": "triggers"}
	if masterVersion >= 4.4 {
		sections["mediatype"] = "mediaTypes"
	}
	if masterVersion >= 6.2 {
		sections["hostgroup"] = "host_groups"
		sections["templategroup"] = "template_groups"
	}
	return sections
}

func (p *Parameters) Replace(method string, target any, lookup map[string]map[string]any) any {
	key := model.String(target)
	switch method {
	case "mediatype":
		if key == "0" {
			return "__ALL_MEDIA__"
		}
		if key == "__ALL_MEDIA__" {
			return 0
		}
	case "host":
		if key == "0" {
			return "__CURRENT_HOST__"
		}
		if key == "__CURRENT_HOST__" {
			return 0
		}
	case "proxy":
		if key == "0" {
			return "__SERVER_DIRECT__"
		}
		if key == "__SERVER_DIRECT__" {
			return 0
		}
	case "proxygroup":
		if key == "0" {
			return "__NO_GROUP__"
		}
		if key == "__NO_GROUP__" {
			return 0
		}
	case "usergroup", "hostgroup", "templategroup":
		if key == "0" {
			return "__ALL_GROUP__"
		}
		if key == "__ALL_GROUP__" {
			return 0
		}
	}
	if values := lookup[method]; values != nil {
		if value, ok := values[key]; ok {
			return value
		}
	}
	return target
}

func removeStrings(values []string, targets ...string) []string {
	remove := map[string]bool{}
	for _, target := range targets {
		remove[target] = true
	}
	out := values[:0]
	for _, value := range values {
		if !remove[value] {
			out = append(out, value)
		}
	}
	return out
}

const parameterJSON = `{"methods":{"hostgroup":{"id":"groupid","name":"name","options":{"output":"extend"}},"host":{"id":"hostid","name":"host","options":{"output":["hostid","host"],"selectTags":["tag","value"]}},"template":{"id":"templateid","name":"name","options":{"output":["templateid","name"]}},"user":{"id":"userid","name":"username","options":{"output":["username","roleid","userdirectoryid"],"getAccess":true,"selectUsrgrps":["name"],"selectMedias":"extend"}},"usergroup":{"id":"usrgrpid","name":"name","options":{"output":"extend","selectTagFilters":"extend","selectHostGroupRights":"extend","selectTemplateGroupRights":"extend"}},"usermacro":{"id":"globalmacroid","name":"macro","options":{"output":["macro","value"],"globalmacro":true,"filter":{"type":[0,2]}}},"mediatype":{"id":"mediatypeid","name":"name","options":{"output":["name"]}},"action":{"id":"actionid","name":"name","options":{"output":"extend","selectOperations":"extend","selectRecoveryOperations":"extend","selectFilter":"extend","search":{"conditiontype":[2]},"selectUpdateOperations":"extend"}},"maintenance":{"id":"maintenanceid","name":"name","options":{"selectHosts":"extend","selectTimeperiods":"extend","selectTags":"extend","selectHostGroups":"extend"}},"script":{"id":"scriptid","name":"name","options":{}},"valuemap":{"id":"valuemapid","name":"name","options":{"output":"extend","selectMappings":"extend"}},"proxy":{"id":"proxyid","name":"name","options":{"output":"extend"}},"drule":{"id":"druleid","name":"name","options":{"output":"extend","selectDChecks":"extend"}},"correlation":{"id":"correlationid","name":"name","options":{"output":"extend","selectOperations":"extend","selectFilter":"extend"}},"autoregistration":{"id":"","name":"","options":{}},"role":{"id":"roleid","name":"name","options":{"output":"extend","selectRules":"extend"}},"authentication":{"id":"","name":"","options":{}},"regexp":{"id":"regexpid","name":"name","options":{"output":["regexpid","name"],"selectExpressions":["expression","expression_type","exp_delimiter","case_sensitive"]}},"settings":{"id":"","name":"","options":{"output":"extend"}},"sla":{"id":"slaid","name":"name","options":{"output":"extend","selectSchedule":"extend","selectExcludedDowntimes":"extend","selectServiceTags":"extend"}},"service":{"id":"serviceid","name":"name","options":{"output":"extend","selectParents":["name"],"selectChildren":["name"],"selectStatusRules":"extend","selectProblemTags":"extend","selectTags":"extend"}},"templategroup":{"id":"groupid","name":"name","options":{"output":"extend"}},"userdirectory":{"id":"userdirectoryid","name":"name","options":{"output":"extend","selectProvisionMedia":"extend","selectProvisionGroups":"extend"}},"proxygroup":{"id":"proxy_groupid","name":"name","options":{"output":["proxy_groupid","name","failover_delay","min_online","description"]}},"mfa":{"id":"mfaid","name":"name","options":{"output":"extend"}},"connector":{"id":"connectorid","name":"name","options":{"output":"extend","selectTags":"extend"}}},"sections":{"GLOBAL":["autoregistration","settings","authentication"],"CONFIG_EXPORT":{"hostgroup":"host_groups","template":"templates","host":"hosts","trigger":"triggers","mediatype":"mediaTypes","templategroup":"template_groups"},"PRE":["usermacro","regexp","connector","proxygroup"],"MID":["script","proxy"],"POST":["action","maintenance","drule","correlation","role","service","sla","userdirectory","mfa"],"ACCOUNT":["usergroup","user"],"EXTEND":[]},"rules":{"hosts":{"createMissing":true,"updateExisting":true},"templateLinkage":{"createMissing":true,"deleteMissing":true},"templates":{"createMissing":true,"updateExisting":true},"items":{"createMissing":true,"updateExisting":true,"deleteMissing":true},"discoveryRules":{"createMissing":true,"updateExisting":true,"deleteMissing":true},"triggers":{"createMissing":true,"updateExisting":true,"deleteMissing":true},"valueMaps":{"createMissing":true,"updateExisting":true},"images":{"createMissing":false,"updateExisting":false},"maps":{"createMissing":false,"updateExisting":false},"graphs":{"createMissing":false,"updateExisting":false,"deleteMissing":false},"httptests":{"createMissing":false,"updateExisting":false,"deleteMissing":false},"mediaTypes":{"createMissing":true,"updateExisting":true},"templateDashboards":{"createMissing":false,"updateExisting":false,"deleteMissing":false},"host_groups":{"createMissing":true},"template_groups":{"createMissing":true}},"discard":{"host":["items","triggers","discovery_rules"],"action":["actionid","operationid","opcommand_hstid","opcommand_grpid"],"proxy":["interface","lastaccess","version","compatibility","state","auto_compress"],"drule":["nextcheck"],"role":["readonly","services.actions"],"service":["status","uuid","created_at","readonly"],"settings":["ha_failover_delay"],"sla":["service_tags","schedule","excluded_downtimes"]}}`
