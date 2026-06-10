package api

import "regexp"

// secretFieldRE matches JSON string fields whose name suggests a credential
// (apiKey, api_key, authToken, secret, password, authorization, …) and
// captures everything up to the value so the value alone can be replaced.
var secretFieldRE = regexp.MustCompile(
	`(?i)("[A-Za-z0-9_-]*(?:api[_-]?key|token|secret|password|authorization)[A-Za-z0-9_-]*"\s*:\s*)"(?:[^"\\]|\\.)*"`)

// redactConfigSecrets masks credential values in a JSON config blob with
// "[REDACTED]". Used before app-config content (e.g. connect diffs) is
// returned over the management API.
func redactConfigSecrets(b []byte) []byte {
	return secretFieldRE.ReplaceAll(b, []byte(`$1"[REDACTED]"`))
}
