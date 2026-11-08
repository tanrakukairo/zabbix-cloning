package clone

import (
	"context"
	"fmt"

	"github.com/tanrakukairo/zabbix-cloning/internal/model"
)

// Zabbix permits one SAML directory and does not require a name for it.
const samlDirectoryName = "__ZC_SAML__"

var samlCertificateFields = []string{"idp_certificate", "sp_private_key", "sp_certificate"}

func isSAMLDirectory(data model.Object) bool {
	return model.Int(data["idp_type"]) == 2
}

func stripSAMLCertificates(data model.Object) {
	for _, field := range samlCertificateFields {
		delete(data, field)
		delete(data, field+"_hash")
	}
}

// Provisioning mappings can refer to user groups created during ACCOUNT.
// Apply the directory before the authentication settings can enable SAML.
func (e *Engine) ApplySAMLDirectory(ctx context.Context) error {
	for _, item := range e.Dataset["userdirectory"] {
		if !isSAMLDirectory(item.Data) {
			continue
		}
		data := marshalObject(e.ConvertIDs(model.CloneObject(item.Data), false))
		stripSAMLCertificates(data)
		delete(data, "name")
		target := e.Local["userdirectory"][samlDirectoryName]
		targetMedia := make(map[string]any)
		if target != nil {
			for _, mapping := range objects(target.Data["provision_media"]) {
				targetMedia[model.String(mapping["name"])] = mapping["userdirectory_mediaid"]
			}
		}
		for _, mapping := range objects(data["provision_media"]) {
			delete(mapping, "userdirectory_mediaid")
			if id := targetMedia[model.String(mapping["name"])]; id != nil {
				mapping["userdirectory_mediaid"] = id
			}
		}

		if raw := e.Config.Raw["saml_certificates"]; raw != nil && e.Version.AtLeast(8, 0) {
			certificates, err := settingsObject(raw, "saml_certificates")
			if err != nil {
				return err
			}
			for _, field := range samlCertificateFields {
				if value, present := certificates[field]; present {
					pem, ok := value.(string)
					if !ok {
						return fmt.Errorf("saml_certificates.%s must be a string", field)
					}
					data[field] = pem
				}
			}
		}

		method := "userdirectory.create"
		if target != nil {
			method = "userdirectory.update"
			data["userdirectoryid"] = target.ID
			delete(data, "idp_type")
		}
		if _, err := e.API.Call(ctx, method, data); err != nil {
			return fmt.Errorf("%s: %w", method, err)
		}
		stripSAMLCertificates(data)
		if e.dryRunVirtual {
			data["idp_type"] = 2
			e.virtualUpsert("userdirectory", samlDirectoryName, data)
		}
		return e.Refresh(ctx)
	}
	return nil
}
