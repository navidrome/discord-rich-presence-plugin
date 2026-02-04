package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/navidrome/navidrome/plugins/pdk/go/host"
	"github.com/navidrome/navidrome/plugins/pdk/go/pdk"
)

// Configuration keys for image hosting
const (
	imageHostKey   = "imagehost"
	imgbbApiKeyKey = "imgbbapikey"
)

// imgbb API response
type imgbbResponse struct {
	Data struct {
		DisplayURL string `json:"display_url"`
	} `json:"data"`
	Success bool `json:"success"`
}

// uguu.se API response
type uguuResponse struct {
	Success bool `json:"success"`
	Files   []struct {
		URL string `json:"url"`
	} `json:"files"`
}

// getImageURL retrieves the track artwork URL, optionally uploading to a public image host.
func getImageURL(username, trackID string) string {
	imageHost, _ := pdk.GetConfig(imageHostKey)

	switch imageHost {
	case "imgbb":
		return getImageViaImgbb(username, trackID)
	case "uguu":
		return getImageViaUguu(username, trackID)
	default:
		return getImageDirect(trackID)
	}
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

// uploadFunc uploads image data to an external host and returns the public URL.
type uploadFunc func(contentType string, data []byte) (string, error)

// getImageViaHost fetches artwork and uploads it using the provided upload function.
func getImageViaHost(provider, username, trackID string, cacheTTL int64, upload uploadFunc) string {
	// Check cache first
	cacheKey := fmt.Sprintf("%s.artwork.%s", provider, trackID)
	cachedURL, exists, err := host.CacheGetString(cacheKey)
	if err == nil && exists {
		pdk.Log(pdk.LogDebug, fmt.Sprintf("Cache hit for %s artwork: %s", provider, trackID))
		return cachedURL
	}

	// Fetch artwork data from Navidrome
	contentType, data, err := host.SubsonicAPICallRaw(fmt.Sprintf("/getCoverArt?u=%s&id=%s&size=300", username, trackID))
	if err != nil {
		pdk.Log(pdk.LogWarn, fmt.Sprintf("Failed to fetch artwork data: %v", err))
		return ""
	}

	// Upload to external host
	url, err := upload(contentType, data)
	if err != nil {
		pdk.Log(pdk.LogWarn, fmt.Sprintf("Failed to upload to %s: %v", provider, err))
		return ""
	}

	_ = host.CacheSetString(cacheKey, url, cacheTTL)
	return url
}

// getImageViaImgbb fetches artwork and uploads it to imgbb.
func getImageViaImgbb(username, trackID string) string {
	apiKey, ok := pdk.GetConfig(imgbbApiKeyKey)
	if !ok || apiKey == "" {
		pdk.Log(pdk.LogWarn, "imgbb image host selected but no API key configured")
		return ""
	}

	return getImageViaHost("imgbb", username, trackID, 82800, func(_ string, data []byte) (string, error) {
		return uploadToImgbb(apiKey, data)
	})
}

// getImageViaUguu fetches artwork and uploads it to uguu.se.
func getImageViaUguu(username, trackID string) string {
	return getImageViaHost("uguu", username, trackID, 9000, func(contentType string, data []byte) (string, error) {
		return uploadToUguu(data, contentType)
	})
}

// uploadToImgbb uploads image data to imgbb and returns the display URL.
func uploadToImgbb(apiKey string, imageData []byte) (string, error) {
	encoded := base64.StdEncoding.EncodeToString(imageData)
	body := fmt.Sprintf("key=%s&image=%s&expiration=86400", url.QueryEscape(apiKey), url.QueryEscape(encoded))

	req := pdk.NewHTTPRequest(pdk.MethodPost, "https://api.imgbb.com/1/upload")
	req.SetHeader("Content-Type", "application/x-www-form-urlencoded")
	req.SetBody([]byte(body))

	resp := req.Send()
	if resp.Status() >= 400 {
		return "", fmt.Errorf("imgbb upload failed: HTTP %d", resp.Status())
	}

	var result imgbbResponse
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return "", fmt.Errorf("failed to parse imgbb response: %w", err)
	}

	if !result.Success {
		return "", fmt.Errorf("imgbb upload was not successful")
	}

	if result.Data.DisplayURL == "" {
		return "", fmt.Errorf("imgbb returned empty display URL")
	}

	return result.Data.DisplayURL, nil
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
