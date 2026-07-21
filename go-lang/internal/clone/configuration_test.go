package clone

import (
	"regexp"
	"testing"

	"github.com/tanrakukairo/zabbix-cloning/internal/model"
)

type testVersion struct {
	major int
	minor int
}

func (v testVersion) AtLeast(major, minor int) bool {
	return v.major > major || v.major == major && v.minor >= minor
}

func TestNormalizeMediaTypeDisablesIncompleteSMTPAuthentication(t *testing.T) {
	mediaType := model.Object{
		"type":                "EMAIL",
		"smtp_authentication": "PASSWORD",
		"username":            "",
		"password":            "",
	}

	normalizeMediaType(mediaType, testVersion{major: 7})

	if got := model.String(mediaType["smtp_authentication"]); got != "NONE" {
		t.Fatalf("smtp_authentication = %q, want NONE", got)
	}
	if _, ok := mediaType["username"]; ok {
		t.Fatal("empty username was not removed")
	}
	if _, ok := mediaType["password"]; ok {
		t.Fatal("empty password was not removed")
	}
}

func TestNormalizeMediaTypePreservesConfiguredSMTPAuthentication(t *testing.T) {
	mediaType := model.Object{
		"type":                "EMAIL",
		"smtp_authentication": "PASSWORD",
		"username":            "alerts@example.com",
		"password":            "{$SMTP_PASSWORD}",
	}

	normalizeMediaType(mediaType, testVersion{major: 7})

	if got := model.String(mediaType["smtp_authentication"]); got != "PASSWORD" {
		t.Fatalf("smtp_authentication = %q, want PASSWORD", got)
	}
	if got := model.String(mediaType["username"]); got != "alerts@example.com" {
		t.Fatalf("username = %q, want alerts@example.com", got)
	}
}

func TestMasterMediaTypeExcludesMissingNormalPassword(t *testing.T) {
	for _, authentication := range []any{1, "1", "PASSWORD", "NORMAL_PASSWORD"} {
		data := model.Object{"smtp_authentication": authentication, "password": ""}
		if !mediaTypeHasEmptyNormalPassword(data) {
			t.Fatalf("missing Normal Password was not detected: %#v", data)
		}
	}
}

func TestMasterMediaTypeKeepsImportableSettings(t *testing.T) {
	tests := []model.Object{
		{"smtp_authentication": "PASSWORD", "password": "{$SMTP_PASSWORD}"},
		{"smtp_authentication": 0, "password": ""},
		{"smtp_authentication": "NONE"},
	}
	for _, data := range tests {
		if mediaTypeHasEmptyNormalPassword(data) {
			t.Fatalf("importable media type was excluded: %#v", data)
		}
	}
}

func TestPrepareMediaTypesExcludesEmptyNormalPassword(t *testing.T) {
	items := []model.StoreItem{
		{Name: "Gmail", Data: model.Object{"name": "Gmail", "type": "EMAIL", "smtp_authentication": "PASSWORD", "username": "gmail@example.com", "password": ""}},
		{Name: "Office365", Data: model.Object{"name": "Office365", "type": "EMAIL", "smtp_authentication": 1, "username": "office@example.com"}},
		{Name: "Configured SMTP", Data: model.Object{"name": "Configured SMTP", "type": "EMAIL", "smtp_authentication": "PASSWORD", "username": "alerts@example.com", "password": "{$SMTP_PASSWORD}"}},
		{Name: "No authentication", Data: model.Object{"name": "No authentication", "type": "EMAIL", "smtp_authentication": "NONE"}},
	}
	values := prepareMediaTypes(items, testVersion{major: 7})
	if len(values) != 2 {
		t.Fatalf("prepared media types = %d, want 2: %#v", len(values), values)
	}
	for _, value := range values {
		name := model.String(value.(model.Object)["name"])
		if name == "Gmail" || name == "Office365" {
			t.Fatalf("media type with empty Normal Password was retained: %s", name)
		}
	}
}

func TestPrepareMasterItemExcludesMediaTypeWithoutPassword(t *testing.T) {
	engine := &Engine{}
	if engine.prepareMasterItem("mediatype", model.Object{"smtp_authentication": 1, "password": ""}) {
		t.Fatal("media type without required password was included in master data")
	}
	if !engine.prepareMasterItem("mediatype", model.Object{"smtp_authentication": 1, "password": "secret"}) {
		t.Fatal("media type with password was excluded from master data")
	}
}

func TestRegenerateUUIDsIsDeterministicUUIDv4(t *testing.T) {
	original := "e1ca624566424496bff9d90c261ab37b"
	child := model.Object{"uuid": "79aa9ebb1ce64944a78a7c8d7603f53b"}
	template := model.Object{"uuid": original, "items": []any{child}}
	regenerateUUIDs(template)
	want := crossVersionUUID(original)
	if got := model.String(template["uuid"]); got != want {
		t.Fatalf("template UUID = %q, want %q", got, want)
	}
	pattern := regexp.MustCompile(`^[0-9a-f]{12}4[0-9a-f]{3}[89ab][0-9a-f]{15}$`)
	for _, value := range []string{model.String(template["uuid"]), model.String(child["uuid"])} {
		if !pattern.MatchString(value) {
			t.Fatalf("not a UUIDv4: %s", value)
		}
	}
}
