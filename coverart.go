package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/navidrome/navidrome/plugins/pdk/go/host"
	"github.com/navidrome/navidrome/plugins/pdk/go/pdk"
	"github.com/navidrome/navidrome/plugins/pdk/go/scrobbler"
)

// ============================================================================
// Direct
// ============================================================================

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

// ============================================================================
// uguu.se
// ============================================================================

// uguu.se API response
type uguuResponse struct {
	Success bool `json:"success"`
	Files   []struct {
		URL string `json:"url"`
	} `json:"files"`
}

// getImageViaUguu fetches artwork and uploads it to uguu.se.
func getImageViaUguu(username, trackID string) string {
	// Check cache first
	cacheKey := fmt.Sprintf("uguu.artwork.%s", trackID)
	cachedURL, exists, err := host.CacheGetString(cacheKey)
	if err == nil && exists {
		pdk.Log(pdk.LogDebug, fmt.Sprintf("Cache hit for uguu.se artwork: %s", trackID))
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

	_ = host.CacheSetString(cacheKey, url, 9000)
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

	req := pdk.NewHTTPRequest(pdk.MethodPost, "https://uguu.se/upload")
	req.SetHeader("Content-Type", fmt.Sprintf("multipart/form-data; boundary=%s", boundary))
	req.SetBody(body)

	resp := req.Send()
	if resp.Status() >= 400 {
		return "", fmt.Errorf("uguu.se upload failed: HTTP %d", resp.Status())
	}

	var result uguuResponse
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
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

// ============================================================================
// Cover Art Archive
// ============================================================================

const CAA_TIMEOUT = 5 * time.Second

// caaResponse only includes relevant parameters; see API for full response
// https://musicbrainz.org/doc/Cover_Art_Archive/API
type caaResponse struct {
	Images []struct {
		Front              bool   `json:"front"`
		Back               bool   `json:"back"`
		ImageURL           string `json:"image"`
		ThumbnailImageURLs struct {
			Size250  string `json:"250"`
			Size500  string `json:"500"`
			Size1200 string `json:"1200"`
		} `json:"thumbnails"`
	} `json:"images"`
}

func getThumbnailForMBZAlbumID(mbzAlbumID string) (string, error) {
	req := pdk.NewHTTPRequest(pdk.MethodGet, fmt.Sprintf("https://coverartarchive.org/release/%s", mbzAlbumID))

	respChan := make(chan pdk.HTTPResponse, 1)
	go func() { respChan <- req.Send() }()

	var result caaResponse

	select {
	case resp := <-respChan:
		if status := resp.Status(); status == 404 {
			pdk.Log(pdk.LogDebug, fmt.Sprintf("No cover art for MusicBrainz Album ID: %s", mbzAlbumID))
			return "", nil
		} else if status >= 400 {
			return "", fmt.Errorf("HTTP %d", resp.Status())
		}

		if err := json.Unmarshal(resp.Body(), &result); err != nil {
			return "", fmt.Errorf("failed to parse: %w", err)
		}
	case <-time.After(CAA_TIMEOUT):
		return "", fmt.Errorf("Timed out")
	}

	for _, image := range result.Images {
		if image.Front {
			return image.ThumbnailImageURLs.Size250, nil
		}
	}

	pdk.Log(pdk.LogDebug, fmt.Sprintf("No front cover art for MusicBrainz Album ID: %s (%d images)", mbzAlbumID, len(result.Images)))
	return "", nil
}

func getImageViaCAA(mbzAlbumID string) string {
	cacheKey := fmt.Sprintf("caa.artwork.%s", mbzAlbumID)
	cachedURL, exists, err := host.CacheGetString(cacheKey)
	if err == nil && exists {
		pdk.Log(pdk.LogDebug, fmt.Sprintf("Cache hit for Cover Art Archive artwork: %s", mbzAlbumID))
		return cachedURL
	}

	url, err := getThumbnailForMBZAlbumID(mbzAlbumID)
	if err != nil {
		pdk.Log(pdk.LogWarn, fmt.Sprintf("Cover Art Archive request failed for %s: %v", mbzAlbumID, err))
		return ""
	}

	_ = host.CacheSetString(cacheKey, url, 86400)
	return url
}

// ============================================================================
// Image URL Resolution
// ============================================================================

const uguuEnabledKey = "uguuenabled"
const caaEnabledKey = "caaenabled"

func getImageURL(username string, track scrobbler.TrackInfo) string {
	caaEnabled, _ := pdk.GetConfig(caaEnabledKey)
	if caaEnabled == "true" && track.MBZAlbumID != "" {
		if url := getImageViaCAA(track.MBZAlbumID); url != "" {
			return url
		}
	}

	uguuEnabled, _ := pdk.GetConfig(uguuEnabledKey)
	if uguuEnabled == "true" {
		return getImageViaUguu(username, track.ID)
	}

	return getImageDirect(track.ID)
}
