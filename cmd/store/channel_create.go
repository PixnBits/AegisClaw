package main

import (
	"AegisClaw/internal/channeldata"
	"AegisClaw/internal/channelid"
)

// handleChannelCreate is the channel.create case. The id is the caller's "id"
// field only: a name is not copied or slugified into the id. Invalid ids are
// refused before the channel map or channels.json is touched.
//
// members: a missing key and JSON null both mean "no members" and install the
// default project-manager member. An explicit empty array does too (the
// historical len == 0 path). A string, number, or object is an error and
// stores nothing.
func handleChannelCreate(payload interface{}, channels map[string]interface{}, ts string) Message {
	resp := Message{Timestamp: ts}
	obj, err := payloadObject(payload)
	if err != nil {
		resp.Command = "error"
		resp.Payload = invalidPayload("channel.create", err)
		return resp
	}
	id, err := optString(obj, "id")
	if err != nil {
		resp.Command = "error"
		resp.Payload = invalidPayload("channel.create", err)
		return resp
	}
	if err := channelid.ValidateChannelID(id); err != nil {
		resp.Command = "error"
		resp.Payload = err.Error()
		return resp
	}
	members, present, err := optArray(obj, "members")
	if err != nil {
		resp.Command = "error"
		resp.Payload = invalidPayload("channel.create", err)
		return resp
	}
	if _, ok := obj["created_at"]; !ok {
		obj["created_at"] = ts
	}
	// Missing, null, and empty array: default project-manager member.
	if !present || len(members) == 0 {
		pmMember := map[string]interface{}{"role": "project-manager", "added_at": ts}
		channeldata.EnsureMemberDefaults(pmMember)
		obj["members"] = []interface{}{pmMember}
	} else {
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
