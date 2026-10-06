package main

import (
	"AegisClaw/internal/channeldata"
	"AegisClaw/internal/channelid"
)

// handleChannelCreate is the channel.create case. The id is the caller's "id"
// field only: a name is not copied or slugified into the id. Invalid ids are
// refused before the channel map or channels.json is touched.
func handleChannelCreate(payload interface{}, channels map[string]interface{}, ts string) Message {
	resp := Message{Timestamp: ts}
	obj, ok := payload.(map[string]interface{})
	if !ok || obj == nil {
		resp.Command = "error"
		resp.Payload = channelid.ValidateChannelID("").Error()
		return resp
	}
	id, _ := obj["id"].(string)
	if err := channelid.ValidateChannelID(id); err != nil {
		resp.Command = "error"
		resp.Payload = err.Error()
		return resp
	}
	if _, ok := obj["created_at"]; !ok {
		obj["created_at"] = ts
	}
	if _, ok := obj["members"]; !ok || len(obj["members"].([]interface{})) == 0 {
		pmMember := map[string]interface{}{"role": "project-manager", "added_at": ts}
		channeldata.EnsureMemberDefaults(pmMember)
		obj["members"] = []interface{}{pmMember}
	} else if members, ok := obj["members"].([]interface{}); ok {
		for _, item := range members {
			if m, ok := item.(map[string]interface{}); ok {
				channeldata.EnsureMemberDefaults(m)
			}
		}
	}
	if _, ok := obj["messages"]; !ok {
		obj["messages"] = []interface{}{}
	}
	obj["next_seq"] = 1
	channels[id] = obj
	saveToFile("channels.json", channels)
	resp.Command = "channel.created"
	resp.Payload = map[string]interface{}{"id": id}
	return resp
}
