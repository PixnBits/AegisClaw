package sanitize

import (
	"encoding/json"

	"AegisClaw/internal/channelid"
)

// validChannelID returns v when it is a string that passes
// channelid.ValidateChannelID (^[a-z][a-z0-9-]*$, 45 chars max), and ""
// otherwise. Such an id is a name the user chose and the SPA routes on it,
// so it is shown raw even when the credential pattern matches part of it.
func validChannelID(v interface{}) string {
	id, _ := v.(string)
	if channelid.ValidateChannelID(id) != nil {
		return ""
	}
	return id
}

// RestoreChannelIDField puts back the raw value of the top-level key in a
// sanitized JSON object when the raw value is a valid channel id. raw is
// the JSON before sanitizing and clean the JSON after. Everything else in
// clean is left as sanitized. When raw has no valid id under key, or either
// side is not a JSON object, clean is returned unchanged.
func RestoreChannelIDField(raw, clean []byte, key string) []byte {
	var before map[string]interface{}
	if json.Unmarshal(raw, &before) != nil {
		return clean
	}
	id := validChannelID(before[key])
	if id == "" {
		return clean
	}
	var after map[string]interface{}
	if json.Unmarshal(clean, &after) != nil {
		return clean
	}
	after[key] = id
	out, err := json.Marshal(after)
	if err != nil {
		return clean
	}
	return out
}
