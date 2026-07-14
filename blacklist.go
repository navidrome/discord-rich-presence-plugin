package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/navidrome/navidrome/plugins/pdk/go/pdk"
	"github.com/navidrome/navidrome/plugins/pdk/go/scrobbler"
)

// userBlacklists loads the per-user track blacklists from the users config.
// It reads the same config value as getConfig, so the two can never drift.
func userBlacklists() map[string][]string {
	usersJSON, ok := pdk.GetConfig(usersKey)
	if !ok || usersJSON == "" {
		return nil
	}

	var userTokens []userToken
	if err := json.Unmarshal([]byte(usersJSON), &userTokens); err != nil {
		pdk.Log(pdk.LogError, fmt.Sprintf("failed to parse users config: %v", err))
		return nil
	}

	lists := make(map[string][]string)
	for _, ut := range userTokens {
		if len(ut.Blacklist) > 0 {
			lists[ut.Username] = ut.Blacklist
		}
	}
	return lists
}

// isBlacklisted reports whether a user has hidden the given track. Matching is
// done on the Navidrome track ID, which is unique to every track.
func isBlacklisted(username string, track scrobbler.TrackInfo) bool {
	id := strings.TrimSpace(track.ID)
	if id == "" {
		return false
	}
	for _, item := range userBlacklists()[username] {
		if strings.TrimSpace(item) == id {
			return true
		}
	}
	return false
}
