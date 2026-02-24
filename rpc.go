// Discord Rich Presence Plugin - RPC Communication
//
// This file handles all Discord gateway communication including WebSocket connections,
// presence updates, and heartbeat management. The discordRPC struct implements WebSocket
// callback interfaces and encapsulates all Discord communication logic.
package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/navidrome/navidrome/plugins/pdk/go/host"
	"github.com/navidrome/navidrome/plugins/pdk/go/pdk"
	"github.com/navidrome/navidrome/plugins/pdk/go/websocket"
)

// Discord WebSocket Gateway constants
const (
	heartbeatOpCode = 1 // Heartbeat operation code
	gateOpCode      = 2 // Identify operation code
	presenceOpCode  = 3 // Presence update operation code
)

// Discord status_display_type values control how the activity name is shown.
// Type 0 renders the name as-is; type 2 renders the name with a "Listening to" prefix.
const (
	statusDisplayDefault   = 0 // Show activity name as-is (e.g. track title)
	statusDisplayListening = 2 // Show "Listening to <name>" (used for "Navidrome")
)

const heartbeatInterval = 41 // Heartbeat interval in seconds

// Image cache TTL constants
const (
	imageCacheTTL        int64 = 4 * 60 * 60  // 4 hours for track artwork
	defaultImageCacheTTL int64 = 48 * 60 * 60 // 48 hours for default Navidrome logo
)

// Scheduler callback payloads for routing
const (
	payloadHeartbeat     = "heartbeat"
	payloadClearActivity = "clear-activity"
)

// discordRPC handles Discord gateway communication and implements WebSocket callbacks.
type discordRPC struct{}

// ============================================================================
// WebSocket Callback Implementation
// ============================================================================

// OnTextMessage handles incoming WebSocket text messages.
func (r *discordRPC) OnTextMessage(input websocket.OnTextMessageRequest) error {
	return r.handleWebSocketMessage(input.ConnectionID, input.Message)
}

// OnBinaryMessage handles incoming WebSocket binary messages.
func (r *discordRPC) OnBinaryMessage(input websocket.OnBinaryMessageRequest) error {
	pdk.Log(pdk.LogDebug, fmt.Sprintf("Received unexpected binary message for connection '%s'", input.ConnectionID))
	return nil
}

// OnError handles WebSocket errors.
func (r *discordRPC) OnError(input websocket.OnErrorRequest) error {
	pdk.Log(pdk.LogWarn, fmt.Sprintf("WebSocket error for connection '%s': %s", input.ConnectionID, input.Error))
	return nil
}

// OnClose handles WebSocket connection closure.
func (r *discordRPC) OnClose(input websocket.OnCloseRequest) error {
	pdk.Log(pdk.LogInfo, fmt.Sprintf("WebSocket connection '%s' closed with code %d: %s", input.ConnectionID, input.Code, input.Reason))
	return nil
}

// activity represents a Discord activity sent via Gateway opcode 3.
type activity struct {
	Name              string             `json:"name"`
	Type              int                `json:"type"`
	Details           string             `json:"details"`
	DetailsURL        string             `json:"details_url,omitempty"`
	State             string             `json:"state"`
	StateURL          string             `json:"state_url,omitempty"`
	Application       string             `json:"application_id"`
	StatusDisplayType int                `json:"status_display_type"`
	Timestamps        activityTimestamps `json:"timestamps"`
	Assets            activityAssets     `json:"assets"`
}

type activityTimestamps struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}

type activityAssets struct {
	LargeImage string `json:"large_image"`
	LargeText  string `json:"large_text"`
	LargeURL   string `json:"large_url,omitempty"`
	SmallImage string `json:"small_image,omitempty"`
	SmallText  string `json:"small_text,omitempty"`
	SmallURL   string `json:"small_url,omitempty"`
}

// presencePayload represents a Discord presence update.
type presencePayload struct {
	Activities []activity `json:"activities"`
	Since      int64      `json:"since"`
	Status     string     `json:"status"`
	Afk        bool       `json:"afk"`
}

// identifyPayload represents a Discord identify payload.
type identifyPayload struct {
	Token      string             `json:"token"`
	Intents    int                `json:"intents"`
	Properties identifyProperties `json:"properties"`
}

type identifyProperties struct {
	OS      string `json:"os"`
	Browser string `json:"browser"`
	Device  string `json:"device"`
}

// ============================================================================
// Image Processing
// ============================================================================

// processImage processes an image URL for Discord. Returns the processed image
// string (mp:prefixed) or an error. No fallback logic — the caller handles retries.
func (r *discordRPC) processImage(imageURL, clientID, token string, ttl int64) (string, error) {
	if imageURL == "" {
		return "", fmt.Errorf("image URL is empty")
	}

	if strings.HasPrefix(imageURL, "mp:") {
		return imageURL, nil
	}

	// Check cache first
	cacheKey := "discord.image." + hashKey(imageURL)
	cachedValue, exists, err := host.CacheGetString(cacheKey)
	if err == nil && exists {
		pdk.Log(pdk.LogDebug, fmt.Sprintf("Cache hit for image URL: %s", imageURL))
		return cachedValue, nil
	}

	// Process via Discord API
	body := fmt.Sprintf(`{"urls":[%q]}`, imageURL)
	resp, err := host.HTTPSend(host.HTTPRequest{
		Method:  "POST",
		URL:     fmt.Sprintf("https://discord.com/api/v9/applications/%s/external-assets", clientID),
		Headers: map[string]string{"Authorization": token, "Content-Type": "application/json"},
		Body:    []byte(body),
	})
	if err != nil {
		pdk.Log(pdk.LogWarn, fmt.Sprintf("HTTP request failed for image processing: %v", err))
		return "", fmt.Errorf("failed to process image: %w", err)
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("failed to process image: HTTP %d", resp.StatusCode)
	}

	var data []map[string]string
	if err := json.Unmarshal(resp.Body, &data); err != nil {
		return "", fmt.Errorf("failed to unmarshal image response: %w", err)
	}

	if len(data) == 0 {
		return "", fmt.Errorf("no data returned for image")
	}

	image := data[0]["external_asset_path"]
	if image == "" {
		return "", fmt.Errorf("empty external_asset_path for image")
	}

	processedImage := fmt.Sprintf("mp:%s", image)

	_ = host.CacheSetString(cacheKey, processedImage, ttl)
	pdk.Log(pdk.LogDebug, fmt.Sprintf("Cached processed image URL for %s (TTL: %ds)", imageURL, ttl))

	return processedImage, nil
}

// ============================================================================
// Activity Management
// ============================================================================

// sendActivity sends an activity update to Discord.
func (r *discordRPC) sendActivity(clientID, username, token string, data activity) error {
	pdk.Log(pdk.LogInfo, fmt.Sprintf("Sending activity for user %s: %s - %s", username, data.Details, data.State))

	// Try track artwork first, fall back to Navidrome logo
	usingDefaultImage := false
	processedImage, err := r.processImage(data.Assets.LargeImage, clientID, token, imageCacheTTL)
	if err != nil {
		pdk.Log(pdk.LogWarn, fmt.Sprintf("Failed to process track image for user %s: %v, falling back to default", username, err))
		processedImage, err = r.processImage(navidromeLogoURL, clientID, token, defaultImageCacheTTL)
		if err != nil {
			pdk.Log(pdk.LogWarn, fmt.Sprintf("Failed to process default image for user %s: %v, continuing without image", username, err))
			data.Assets.LargeImage = ""
		} else {
			data.Assets.LargeImage = processedImage
			usingDefaultImage = true
		}
	} else {
		data.Assets.LargeImage = processedImage
	}

	// Only show SmallImage (Navidrome logo overlay) when LargeImage is actual track artwork
	if usingDefaultImage || data.Assets.LargeImage == "" {
		data.Assets.SmallImage = ""
		data.Assets.SmallText = ""
	} else if data.Assets.SmallImage != "" {
		processedSmall, err := r.processImage(data.Assets.SmallImage, clientID, token, defaultImageCacheTTL)
		if err != nil {
			pdk.Log(pdk.LogWarn, fmt.Sprintf("Failed to process small image for user %s: %v", username, err))
			data.Assets.SmallImage = ""
			data.Assets.SmallText = ""
		} else {
			data.Assets.SmallImage = processedSmall
		}
	}

	presence := presencePayload{
		Activities: []activity{data},
		Status:     "dnd",
		Afk:        false,
	}
	return r.sendMessage(username, presenceOpCode, presence)
}

// clearActivity clears the Discord activity for a user.
func (r *discordRPC) clearActivity(username string) error {
	pdk.Log(pdk.LogInfo, fmt.Sprintf("Clearing activity for user %s", username))
	return r.sendMessage(username, presenceOpCode, presencePayload{})
}

// ============================================================================
// Low-level Communication
// ============================================================================

// sendMessage sends a message over the WebSocket connection.
func (r *discordRPC) sendMessage(username string, opCode int, payload any) error {
	message := map[string]any{
		"op": opCode,
		"d":  payload,
	}
	b, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	err = host.WebSocketSendText(username, string(b))
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	return nil
}

// getDiscordGateway retrieves the Discord gateway URL.
func (r *discordRPC) getDiscordGateway() (string, error) {
	resp, err := host.HTTPSend(host.HTTPRequest{
		Method: "GET",
		URL:    "https://discord.com/api/gateway",
	})
	if err != nil {
		pdk.Log(pdk.LogWarn, fmt.Sprintf("HTTP request failed for Discord gateway: %v", err))
		return "", fmt.Errorf("failed to get Discord gateway: %w", err)
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("failed to get Discord gateway: HTTP %d", resp.StatusCode)
	}

	var result map[string]string
	if err := json.Unmarshal(resp.Body, &result); err != nil {
		return "", fmt.Errorf("failed to parse Discord gateway response: %w", err)
	}
	return result["url"], nil
}

// sendHeartbeat sends a heartbeat to Discord.
func (r *discordRPC) sendHeartbeat(username string) error {
	seqNum, _, err := host.CacheGetInt(fmt.Sprintf("discord.seq.%s", username))
	if err != nil {
		return fmt.Errorf("failed to get sequence number: %w", err)
	}

	pdk.Log(pdk.LogDebug, fmt.Sprintf("Sending heartbeat for user %s: %d", username, seqNum))
	return r.sendMessage(username, heartbeatOpCode, seqNum)
}

// cleanupFailedConnection cleans up a failed Discord connection.
func (r *discordRPC) cleanupFailedConnection(username string) {
	pdk.Log(pdk.LogInfo, fmt.Sprintf("Cleaning up failed connection for user %s", username))

	// Cancel the heartbeat schedule
	if err := host.SchedulerCancelSchedule(username); err != nil {
		pdk.Log(pdk.LogWarn, fmt.Sprintf("Failed to cancel heartbeat schedule for user %s: %v", username, err))
	}

	// Close the WebSocket connection
	if err := host.WebSocketCloseConnection(username, 1000, "Connection lost"); err != nil {
		pdk.Log(pdk.LogWarn, fmt.Sprintf("Failed to close WebSocket connection for user %s: %v", username, err))
	}

	// Clean up cache entries
	_ = host.CacheRemove(fmt.Sprintf("discord.seq.%s", username))

	pdk.Log(pdk.LogInfo, fmt.Sprintf("Cleaned up connection for user %s", username))
}

// isConnected checks if a user is connected to Discord by testing the heartbeat.
func (r *discordRPC) isConnected(username string) bool {
	err := r.sendHeartbeat(username)
	if err != nil {
		pdk.Log(pdk.LogDebug, fmt.Sprintf("Heartbeat test failed for user %s: %v", username, err))
		return false
	}
	return true
}

// connect establishes a connection to Discord for a user.
func (r *discordRPC) connect(username, token string) error {
	if r.isConnected(username) {
		pdk.Log(pdk.LogInfo, fmt.Sprintf("Reusing existing connection for user %s", username))
		return nil
	}
	pdk.Log(pdk.LogInfo, fmt.Sprintf("Creating new connection for user %s", username))

	// Get Discord Gateway URL
	gateway, err := r.getDiscordGateway()
	if err != nil {
		return fmt.Errorf("failed to get Discord gateway: %w", err)
	}
	pdk.Log(pdk.LogDebug, fmt.Sprintf("Using gateway: %s", gateway))

	// Connect to Discord Gateway
	_, err = host.WebSocketConnect(gateway, nil, username)
	if err != nil {
		return fmt.Errorf("failed to connect to WebSocket: %w", err)
	}

	// Send identify payload
	payload := identifyPayload{
		Token:   token,
		Intents: 0,
		Properties: identifyProperties{
			OS:      "Windows 10",
			Browser: "Discord Client",
			Device:  "Discord Client",
		},
	}
	if err := r.sendMessage(username, gateOpCode, payload); err != nil {
		return fmt.Errorf("failed to send identify payload: %w", err)
	}

	// Schedule heartbeats for this user/connection
	cronExpr := fmt.Sprintf("@every %ds", heartbeatInterval)
	scheduleID, err := host.SchedulerScheduleRecurring(cronExpr, payloadHeartbeat, username)
	if err != nil {
		return fmt.Errorf("failed to schedule heartbeat: %w", err)
	}
	pdk.Log(pdk.LogInfo, fmt.Sprintf("Scheduled heartbeat for user %s with ID %s", username, scheduleID))

	pdk.Log(pdk.LogInfo, fmt.Sprintf("Successfully authenticated user %s", username))
	return nil
}

// disconnect closes the Discord connection for a user.
func (r *discordRPC) disconnect(username string) error {
	if err := host.SchedulerCancelSchedule(username); err != nil {
		return fmt.Errorf("failed to cancel schedule: %w", err)
	}

	if err := host.WebSocketCloseConnection(username, 1000, "Navidrome disconnect"); err != nil {
		return fmt.Errorf("failed to close WebSocket connection: %w", err)
	}
	return nil
}

// handleWebSocketMessage processes incoming WebSocket messages from Discord.
func (r *discordRPC) handleWebSocketMessage(connectionID, message string) error {
	if len(message) < 1024 {
		pdk.Log(pdk.LogTrace, fmt.Sprintf("Received WebSocket message for connection '%s': %s", connectionID, message))
	} else {
		pdk.Log(pdk.LogTrace, fmt.Sprintf("Received WebSocket message for connection '%s' (truncated): %s...", connectionID, message[:1021]))
	}

	// Parse the message
	var msg map[string]any
	if err := json.Unmarshal([]byte(message), &msg); err != nil {
		return fmt.Errorf("failed to parse WebSocket message: %w", err)
	}

	// Store sequence number if present
	if v := msg["s"]; v != nil {
		seq := int64(v.(float64))
		pdk.Log(pdk.LogTrace, fmt.Sprintf("Received sequence number for connection '%s': %d", connectionID, seq))
		if err := host.CacheSetInt(fmt.Sprintf("discord.seq.%s", connectionID), seq, int64(heartbeatInterval*2)); err != nil {
			return fmt.Errorf("failed to store sequence number for user %s: %w", connectionID, err)
		}
	}
	return nil
}

// handleHeartbeatCallback processes heartbeat scheduler callbacks.
func (r *discordRPC) handleHeartbeatCallback(username string) error {
	if err := r.sendHeartbeat(username); err != nil {
		// On first heartbeat failure, immediately clean up the connection
		pdk.Log(pdk.LogWarn, fmt.Sprintf("Heartbeat failed for user %s, cleaning up connection: %v", username, err))
		r.cleanupFailedConnection(username)
		return fmt.Errorf("heartbeat failed, connection cleaned up: %w", err)
	}
	return nil
}

// handleClearActivityCallback processes clear activity scheduler callbacks.
func (r *discordRPC) handleClearActivityCallback(username string) error {
	pdk.Log(pdk.LogInfo, fmt.Sprintf("Removing presence for user %s", username))
	if err := r.clearActivity(username); err != nil {
		return fmt.Errorf("failed to clear activity: %w", err)
	}

	pdk.Log(pdk.LogInfo, fmt.Sprintf("Disconnecting user %s", username))
	if err := r.disconnect(username); err != nil {
		return fmt.Errorf("failed to disconnect from Discord: %w", err)
	}
	return nil
}
