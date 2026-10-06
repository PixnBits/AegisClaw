package main

import "fmt"

// payloadObject returns a peer payload when it is a JSON object.
// JSON null, arrays, strings, and numbers are rejected. A typed nil map
// is rejected too: indexing it would panic.
func payloadObject(payload interface{}) (map[string]interface{}, error) {
	obj, ok := payload.(map[string]interface{})
	if !ok || obj == nil {
		return nil, fmt.Errorf("payload must be an object")
	}
	return obj, nil
}

// reqString reads a required string field.
// Missing and JSON null are "<key> is required". A non-string is
// "<key> must be a string". An empty string is returned: a bare assertion
// used to succeed for "".
func reqString(obj map[string]interface{}, key string) (string, error) {
	v, ok := obj[key]
	if !ok || v == nil {
		return "", fmt.Errorf("%s is required", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", key)
	}
	return s, nil
}

// optString reads an optional string.
// A missing key and JSON null return "" and no error. Any other non-string
// is an error.
func optString(obj map[string]interface{}, key string) (string, error) {
	v, ok := obj[key]
	if !ok || v == nil {
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", key)
	}
	return s, nil
}

// optArray reads an optional JSON array.
// A missing key and JSON null are not present (present=false) so callers
// can apply the same default. channel.create treats both as "no members".
// An explicit array, including an empty one, is present. Any other type
// (string, number, object) is an error.
func optArray(obj map[string]interface{}, key string) ([]interface{}, bool, error) {
	v, ok := obj[key]
	if !ok || v == nil {
		return nil, false, nil
	}
	arr, ok := v.([]interface{})
	if !ok {
		return nil, false, fmt.Errorf("%s must be an array", key)
	}
	return arr, true, nil
}

// reqObject reads a required JSON object field.
func reqObject(obj map[string]interface{}, key string) (map[string]interface{}, error) {
	v, ok := obj[key]
	if !ok || v == nil {
		return nil, fmt.Errorf("%s is required", key)
	}
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("%s must be an object", key)
	}
	return m, nil
}

// optObject reads an optional JSON object.
// Missing and JSON null are not present. Any other type is an error.
func optObject(obj map[string]interface{}, key string) (map[string]interface{}, bool, error) {
	v, ok := obj[key]
	if !ok || v == nil {
		return nil, false, nil
	}
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil, false, fmt.Errorf("%s must be an object", key)
	}
	return m, true, nil
}

// storedArray reads an array persisted on a channel record.
// Missing and JSON null are an empty array. A string, number, or object
// left in channels.json is an error so a later write cannot replace it.
func storedArray(ch map[string]interface{}, key string) ([]interface{}, error) {
	v, ok := ch[key]
	if !ok || v == nil {
		return []interface{}{}, nil
	}
	arr, ok := v.([]interface{})
	if !ok {
		return nil, fmt.Errorf("%s must be an array", key)
	}
	return arr, nil
}

func invalidPayload(command string, err error) string {
	return "invalid " + command + " payload: " + err.Error()
}

// mustPayload decodes msg.Payload into an object or sets an error reply.
func mustPayload(msg Message, response *Message) (map[string]interface{}, bool) {
	obj, err := payloadObject(msg.Payload)
	if err != nil {
		response.Command = "error"
		response.Payload = invalidPayload(msg.Command, err)
		return nil, false
	}
	return obj, true
}

func mustString(msg Message, response *Message, obj map[string]interface{}, key string) (string, bool) {
	s, err := reqString(obj, key)
	if err != nil {
		response.Command = "error"
		response.Payload = invalidPayload(msg.Command, err)
		return "", false
	}
	return s, true
}

func mustOptString(msg Message, response *Message, obj map[string]interface{}, key string) (string, bool) {
	s, err := optString(obj, key)
	if err != nil {
		response.Command = "error"
		response.Payload = invalidPayload(msg.Command, err)
		return "", false
	}
	return s, true
}

func mustChannelID(msg Message, response *Message, payload map[string]interface{}) (string, bool) {
	id, err := channelIDFromPayload(payload)
	if err != nil {
		response.Command = "error"
		response.Payload = invalidPayload(msg.Command, err)
		return "", false
	}
	return id, true
}

func mustStoredArray(response *Message, ch map[string]interface{}, key string) ([]interface{}, bool) {
	arr, err := storedArray(ch, key)
	if err != nil {
		response.Command = "error"
		response.Payload = "invalid stored channel: " + err.Error()
		return nil, false
	}
	return arr, true
}
