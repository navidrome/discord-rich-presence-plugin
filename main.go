// Discord Rich Presence Plugin for Navidrome
//
// This plugin integrates Navidrome with Discord Rich Presence. It shows how a plugin can
// keep a real-time connection to an external service while remaining completely stateless.
//
// Capabilities: Scrobbler, SchedulerCallback, WebSocketCallback
//
// NOTE: This plugin is for demonstration purposes only. It relies on the user's Discord
// token being stored in the Navidrome configuration file, which is not secure and may be
// against Discord's terms of service. Use it at your own risk.
package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/navidrome/navidrome/plugins/pdk/go/pdk"
	"github.com/navidrome/navidrome/plugins/pdk/go/scheduler"
	"github.com/navidrome/navidrome/plugins/pdk/go/scrobbler"
	"github.com/navidrome/navidrome/plugins/pdk/go/websocket"
)

// Configuration keys
const (
	clientIDKey     = "clientid"
	usersKey        = "users"
	activityNameKey = "activityname"
	spotifyLinksKey = "spotifylinks"
	caaEnabledKey   = "caaenabled"
	uguuEnabledKey  = "uguuenabled"
)

const (
	navidromeWebsiteURL = "https://www.navidrome.org"

	// navidromeLogoURL is used as fallback large image when track artwork is unavailable.
	navidromeLogoURL = "https://raw.githubusercontent.com/navidrome/website/refs/heads/master/assets/icons/logo.webp"

	pauseIconURL = "https://raw.githubusercontent.com/navidrome/discord-rich-presence-plugin/refs/heads/main/assets/pause.png"
)

// Activity name display options
const (
	activityNameDefault = "Default"
	activityNameTrack   = "Track"
	activityNameArtist  = "Artist"
	activityNameAlbum   = "Album"
)

// userToken represents a user-token mapping from the config
type userToken struct {
	Username string `json:"username"`
	Token    string `json:"token"`
}

// discordPlugin implements the scrobbler and scheduler interfaces.
type discordPlugin struct{}

// rpc handles Discord gateway communication (via websockets).
var rpc = &discordRPC{}

// init registers the plugin capabilities
func init() {
	scrobbler.Register(&discordPlugin{})
	scheduler.Register(&discordPlugin{})
	websocket.Register(rpc)
}

// getConfig loads the plugin configuration.
func getConfig() (clientID string, users map[string]string, err error) {
	clientID, ok := pdk.GetConfig(clientIDKey)
	if !ok || clientID == "" {
		pdk.Log(pdk.LogWarn, "missing ClientID in configuration")
		return "", nil, nil
	}

	// Get the users array from config
	usersJSON, ok := pdk.GetConfig(usersKey)
	if !ok || usersJSON == "" {
		pdk.Log(pdk.LogWarn, "no users configured")
		return clientID, nil, nil
	}

	// Parse the JSON array
	var userTokens []userToken
	if err := json.Unmarshal([]byte(usersJSON), &userTokens); err != nil {
		pdk.Log(pdk.LogError, fmt.Sprintf("failed to parse users config: %v", err))
		return clientID, nil, nil
	}

	if len(userTokens) == 0 {
		pdk.Log(pdk.LogWarn, "no users configured")
		return clientID, nil, nil
	}

	// Build the users map
	users = make(map[string]string)
	for _, ut := range userTokens {
		if ut.Username != "" && ut.Token != "" {
			users[ut.Username] = ut.Token
		}
	}

	if len(users) == 0 {
		pdk.Log(pdk.LogWarn, "no valid users configured")
		return clientID, nil, nil
	}

	return clientID, users, nil
}

// ============================================================================
// Scrobbler Implementation
// ============================================================================

// IsAuthorized checks if a user is authorized for Discord Rich Presence.
func (p *discordPlugin) IsAuthorized(input scrobbler.IsAuthorizedRequest) (bool, error) {
	_, users, err := getConfig()
	if err != nil {
		return false, fmt.Errorf("failed to check user authorization: %w", err)
	}

	_, authorized := users[input.Username]
	pdk.Log(pdk.LogInfo, fmt.Sprintf("IsAuthorized for user %s: %v", input.Username, authorized))
	return authorized, nil
}

// NowPlaying is a no-op — playback state is handled by PlaybackReport.
func (p *discordPlugin) NowPlaying(_ scrobbler.NowPlayingRequest) error {
	return nil
}

// Scrobble handles scrobble requests (no-op for Discord).
func (p *discordPlugin) Scrobble(_ scrobbler.ScrobbleRequest) error {
	// Discord Rich Presence doesn't need scrobble events
	return nil
}

// PlaybackReport handles playback state reports from Navidrome.
func (p *discordPlugin) PlaybackReport(input scrobbler.PlaybackReportRequest) error {
	switch input.State {
	case "playing":
		return p.handlePlaying(input)
	case "paused":
		return p.handlePaused(input)
	case "stopped", "expired":
		return p.handleStopped(input)
	default:
		return nil
	}
}

func (p *discordPlugin) handlePlaying(input scrobbler.PlaybackReportRequest) error {
	pdk.Log(pdk.LogInfo, fmt.Sprintf("Setting presence for user %s, track: %s", input.Username, input.Track.Title))

	clientID, users, err := getConfig()
	if err != nil {
		return fmt.Errorf("%w: failed to get config: %v", scrobbler.ScrobblerErrorRetryLater, err)
	}

	userToken, authorized := users[input.Username]
	if !authorized {
		return fmt.Errorf("%w: user '%s' not authorized", scrobbler.ScrobblerErrorNotAuthorized, input.Username)
	}

	if err := rpc.connect(input.Username, userToken); err != nil {
		return fmt.Errorf("%w: failed to connect to Discord: %v", scrobbler.ScrobblerErrorRetryLater, err)
	}

	startTime := input.Timestamp*1000 - input.PositionMs
	// Adjust end time for playback rate (e.g. 2x speed audiobooks finish in half the wall-clock time)
	effectiveDuration := int64(float64(int64(input.Track.Duration)*1000) / input.PlaybackRate)
	endTime := startTime + effectiveDuration

	activityName := "Navidrome"
	statusDisplayType := statusDisplayDetails
	activityNameOption, _ := pdk.GetConfig(activityNameKey)
	switch activityNameOption {
	case activityNameTrack:
		activityName = input.Track.Title
		statusDisplayType = statusDisplayName
	case activityNameAlbum:
		activityName = input.Track.Album
		statusDisplayType = statusDisplayName
	case activityNameArtist:
		activityName = input.Track.Artist
		statusDisplayType = statusDisplayName
	}

	var spotifyURL, artistSearchURL string
	spotifyLinksOption, _ := pdk.GetConfig(spotifyLinksKey)
	if spotifyLinksOption == "true" {
		spotifyURL = resolveSpotifyURL(input.Track)
		artistSearchURL = spotifySearchURL(input.Track.Artist)
	}

	return rpc.sendActivity(clientID, input.Username, userToken, activity{
		Application:       clientID,
		Name:              activityName,
		Type:              2,
		Details:           input.Track.Title,
		DetailsURL:        spotifyURL,
		State:             input.Track.Artist,
		StateURL:          artistSearchURL,
		StatusDisplayType: statusDisplayType,
		Timestamps: activityTimestamps{
			Start: startTime,
			End:   endTime,
		},
		Assets: activityAssets{
			LargeImage: getImageURL(input.Username, input.Track),
			LargeText:  input.Track.Album,
			LargeURL:   spotifyURL,
		},
	})
}

func (p *discordPlugin) handlePaused(input scrobbler.PlaybackReportRequest) error {
	pdk.Log(pdk.LogInfo, fmt.Sprintf("Pausing presence for user %s, track: %s", input.Username, input.Track.Title))

	clientID, users, err := getConfig()
	if err != nil {
		return fmt.Errorf("%w: failed to get config: %v", scrobbler.ScrobblerErrorRetryLater, err)
	}

	userToken, authorized := users[input.Username]
	if !authorized {
		return fmt.Errorf("%w: user '%s' not authorized", scrobbler.ScrobblerErrorNotAuthorized, input.Username)
	}

	if err := rpc.connect(input.Username, userToken); err != nil {
		return fmt.Errorf("%w: failed to connect to Discord: %v", scrobbler.ScrobblerErrorRetryLater, err)
	}

	startTime := time.Now().UnixMilli() - input.PositionMs

	activityName := "Navidrome"
	statusDisplayType := statusDisplayDetails
	activityNameOption, _ := pdk.GetConfig(activityNameKey)
	switch activityNameOption {
	case activityNameTrack:
		activityName = input.Track.Title
		statusDisplayType = statusDisplayName
	case activityNameAlbum:
		activityName = input.Track.Album
		statusDisplayType = statusDisplayName
	case activityNameArtist:
		activityName = input.Track.Artist
		statusDisplayType = statusDisplayName
	}

	var spotifyURL, artistSearchURL string
	spotifyLinksOption, _ := pdk.GetConfig(spotifyLinksKey)
	if spotifyLinksOption == "true" {
		spotifyURL = resolveSpotifyURL(input.Track)
		artistSearchURL = spotifySearchURL(input.Track.Artist)
	}

	return rpc.sendActivity(clientID, input.Username, userToken, activity{
		Application:       clientID,
		Name:              activityName,
		Type:              2,
		Details:           input.Track.Title,
		DetailsURL:        spotifyURL,
		State:             input.Track.Artist,
		StateURL:          artistSearchURL,
		StatusDisplayType: statusDisplayType,
		Timestamps: activityTimestamps{
			Start: startTime,
		},
		Assets: activityAssets{
			LargeImage: getImageURL(input.Username, input.Track),
			LargeText:  input.Track.Album,
			LargeURL:   spotifyURL,
			SmallImage: pauseIconURL,
			SmallText:  "Paused",
		},
	})
}

func (p *discordPlugin) handleStopped(input scrobbler.PlaybackReportRequest) error {
	pdk.Log(pdk.LogInfo, fmt.Sprintf("Clearing presence for user %s", input.Username))

	if err := rpc.clearActivity(input.Username); err != nil {
		return fmt.Errorf("failed to clear activity: %w", err)
	}

	if err := rpc.disconnect(input.Username); err != nil {
		return fmt.Errorf("failed to disconnect from Discord: %w", err)
	}
	return nil
}

// ============================================================================
// Scheduler Callback Implementation
// ============================================================================

// OnCallback handles scheduler callbacks.
func (p *discordPlugin) OnCallback(input scheduler.SchedulerCallbackRequest) error {
	pdk.Log(pdk.LogDebug, fmt.Sprintf("Scheduler callback: id=%s, payload=%s, recurring=%v", input.ScheduleID, input.Payload, input.IsRecurring))

	switch input.Payload {
	case payloadHeartbeat:
		if err := rpc.handleHeartbeatCallback(input.ScheduleID); err != nil {
			return err
		}
	default:
		pdk.Log(pdk.LogWarn, fmt.Sprintf("Unknown scheduler callback payload: %s", input.Payload))
	}

	return nil
}

func main() {}
