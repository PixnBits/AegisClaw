package main

import (
	"github.com/sirupsen/logrus"

	"AegisClaw/internal/channelid"
)

// invalidChannel is one channel.list entry whose id fails ValidateChannelID.
type invalidChannel struct {
	ID  string
	Err error
}

// invalidChannelIDs returns channels whose id cannot start an agent.
// Items that are not objects are skipped. The result keeps list order.
func invalidChannelIDs(list []interface{}) []invalidChannel {
	var out []invalidChannel
	for _, item := range list {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		id, _ := m["id"].(string)
		if err := channelid.ValidateChannelID(id); err != nil {
			out = append(out, invalidChannel{ID: id, Err: err})
		}
	}
	return out
}

// logInvalidChannelIDs warns once per stranded channel. It only reads list;
// callers must not add a hub round-trip to feed it.
func logInvalidChannelIDs(list []interface{}) {
	for _, bad := range invalidChannelIDs(list) {
		logrus.Warnf("channel %q has an invalid id (%v); agents cannot be started for it. Create a new channel with a valid id and move the work there.", bad.ID, bad.Err)
	}
}

// inspectStartupChannelList is the startup pass over channel.list: it warns
// about stranded invalid ids and reports whether "main" exists. It reuses the
// list setupDefaultMainChannelAndMembers already fetched (no extra hub call).
func inspectStartupChannelList(listResp interface{}) (hasMain bool) {
	arr, ok := listResp.([]interface{})
	if !ok {
		return false
	}
	logInvalidChannelIDs(arr)
	for _, c := range arr {
		if m, ok := c.(map[string]interface{}); ok {
			if id, ok := m["id"].(string); ok && id == "main" {
				return true
			}
		}
	}
	return false
}
