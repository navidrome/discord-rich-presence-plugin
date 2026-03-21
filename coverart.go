package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/navidrome/navidrome/plugins/pdk/go/host"
	"github.com/navidrome/navidrome/plugins/pdk/go/pdk"
	"github.com/navidrome/navidrome/plugins/pdk/go/scrobbler"
)

// headCoverArt sends a HEAD request to the given CAA URL without following redirects.
// Returns the Location header value on 307 (image exists), or "" otherwise.
func headCoverArt(url string) string {
	resp, err := host.HTTPSend(host.HTTPRequest{
		Method:            "HEAD",
		URL:               url,
		NoFollowRedirects: true,
		TimeoutMs:         5000, // 5s timeout to avoid blocking NowPlaying
	})
	if err != nil {
		pdk.Log(pdk.LogDebug, fmt.Sprintf("CAA HEAD request failed for %s: %v", url, err))
		return ""
	}
	if resp.StatusCode != 307 {
		return ""
	}
	location := resp.Headers["Location"]
	if location == "" {
		pdk.Log(pdk.LogWarn, fmt.Sprintf("CAA returned 307 but no Location header for %s", url))
	}
	return location
}

// Cache TTLs for cover art lookups
const (
	caaCacheTTLHit  int64 = 24 * 60 * 60 // 24 hours for resolved CAA artwork
	caaCacheTTLMiss int64 = 4 * 60 * 60  // 4 hours for CAA misses
	uguuCacheTTL    int64 = 150 * 60     // 2.5 hours for uguu.se uploads
)

// getImageViaCoverArt checks the Cover Art Archive for album artwork.
// Tries the release first, then falls back to the release group.
// Returns the archive.org image URL on success, "" on failure.
func getImageViaCoverArt(mbzAlbumID, mbzReleaseGroupID string) string {
	if mbzAlbumID == "" && mbzReleaseGroupID == "" {
		return ""
	}

	// Determine cache key: use album ID when available, otherwise release group ID
	cacheKey := "caa.artwork." + mbzAlbumID
	if mbzAlbumID == "" {
		cacheKey = "caa.artwork.rg." + mbzReleaseGroupID
	}

	// Check cache
	cachedURL, exists, err := host.CacheGetString(cacheKey)
	if err == nil && exists {
		pdk.Log(pdk.LogDebug, fmt.Sprintf("CAA cache hit for %s", cacheKey))
		return cachedURL
	}

	// Try release first
	var imageURL string
	if mbzAlbumID != "" {
		imageURL = headCoverArt(fmt.Sprintf("https://coverartarchive.org/release/%s/front-500", mbzAlbumID))
	}

	// Fall back to release group
	if imageURL == "" && mbzReleaseGroupID != "" {
		imageURL = headCoverArt(fmt.Sprintf("https://coverartarchive.org/release-group/%s/front-500", mbzReleaseGroupID))
	}

	// Cache the result (hit or miss)
	ttl := caaCacheTTLHit
	if imageURL == "" {
		ttl = caaCacheTTLMiss
	}
	_ = host.CacheSetString(cacheKey, imageURL, ttl)

	if imageURL != "" {
		pdk.Log(pdk.LogDebug, fmt.Sprintf("CAA resolved artwork for %s: %s", cacheKey, imageURL))
	}

	return imageURL
}

// uguu.se API response
type uguuResponse struct {
	Success bool `json:"success"`
	Files   []struct {
		URL string `json:"url"`
	} `json:"files"`
}

// getImageURL retrieves the track artwork URL, checking CAA first if enabled,
// then uguu.se, then direct Navidrome URL.
func getImageURL(username string, track scrobbler.TrackInfo) string {
	caaEnabled, _ := pdk.GetConfig(caaEnabledKey)
	if caaEnabled == "true" {
		if url := getImageViaCoverArt(track.MBZAlbumID, track.MBZReleaseGroupID); url != "" {
			return url
		}
	}

	uguuEnabled, _ := pdk.GetConfig(uguuEnabledKey)
	if uguuEnabled == "true" {
		return getImageViaUguu(username, track.ID)
	}

	return getImageDirect(track.ID)
}

// getImageDirect returns the artwork URL directly from Navidrome (current behavior).
func getImageDirect(trackID string) string {
	artworkURL, err := host.ArtworkGetTrackUrl(trackID, 300)
	if err != nil {
		pdk.Log(pdk.LogWarn, fmt.Sprintf("Failed to get artwork URL: %v", err))
		return ""
	}

	// Don't use localhost URLs
	if strings.HasPrefix(artworkURL, "http://localhost") {
		return ""
	}
	return artworkURL
}

// getImageViaUguu fetches artwork and uploads it to uguu.se.
func getImageViaUguu(username, trackID string) string {
	// Check cache first
	cacheKey := fmt.Sprintf("uguu.artwork.%s", trackID)
	cachedURL, exists, err := host.CacheGetString(cacheKey)
	if err == nil && exists {
		pdk.Log(pdk.LogDebug, fmt.Sprintf("Cache hit for uguu artwork: %s", trackID))
		return cachedURL
	}

	// Fetch artwork data from Navidrome
	contentType, data, err := host.SubsonicAPICallRaw(fmt.Sprintf("/getCoverArt?u=%s&id=%s&size=300", username, trackID))
	if err != nil {
		pdk.Log(pdk.LogWarn, fmt.Sprintf("Failed to fetch artwork data: %v", err))
		return ""
	}

	// Upload to uguu.se
	url, err := uploadToUguu(data, contentType)
	if err != nil {
		pdk.Log(pdk.LogWarn, fmt.Sprintf("Failed to upload to uguu.se: %v", err))
		return ""
	}

	_ = host.CacheSetString(cacheKey, url, uguuCacheTTL)
	return url
}

// uploadToUguu uploads image data to uguu.se and returns the file URL.
func uploadToUguu(imageData []byte, contentType string) (string, error) {
	// Build multipart/form-data body manually (TinyGo-compatible)
	boundary := "----NavidromeCoverArt"
	var body []byte
	body = append(body, []byte(fmt.Sprintf("--%s\r\n", boundary))...)
	body = append(body, []byte(fmt.Sprintf("Content-Disposition: form-data; name=\"files[]\"; filename=\"cover.jpg\"\r\n"))...)
	body = append(body, []byte(fmt.Sprintf("Content-Type: %s\r\n", contentType))...)
	body = append(body, []byte("\r\n")...)
	body = append(body, imageData...)
	body = append(body, []byte(fmt.Sprintf("\r\n--%s--\r\n", boundary))...)

	resp, err := host.HTTPSend(host.HTTPRequest{
		Method:  "POST",
		URL:     "https://uguu.se/upload",
		Headers: map[string]string{"Content-Type": fmt.Sprintf("multipart/form-data; boundary=%s", boundary)},
		Body:    body,
	})
	if err != nil {
		return "", fmt.Errorf("uguu.se upload failed: %w", err)
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("uguu.se upload failed: HTTP %d", resp.StatusCode)
	}

	var result uguuResponse
	if err := json.Unmarshal(resp.Body, &result); err != nil {
		return "", fmt.Errorf("failed to parse uguu.se response: %w", err)
	}

	if !result.Success || len(result.Files) == 0 {
		return "", fmt.Errorf("uguu.se upload was not successful")
	}

	if result.Files[0].URL == "" {
		return "", fmt.Errorf("uguu.se returned empty URL")
	}

	return result.Files[0].URL, nil
}
