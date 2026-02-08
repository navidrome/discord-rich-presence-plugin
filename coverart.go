package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/navidrome/navidrome/plugins/pdk/go/host"
	"github.com/navidrome/navidrome/plugins/pdk/go/pdk"
)

const cacheKeyFormat = "artwork.%s"

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

	cacheKey := fmt.Sprintf(cacheKeyFormat, trackID)
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

type subsonicGetSongResponse struct {
	Data struct {
		Song struct {
			AlbumID string `json:"albumId"`
		} `json:"song"`
	} `json:"subsonic-response"`
}

type subsonicGetAlbumResponse struct {
	Data struct {
		Album struct {
			MusicBrainzId string `json:"musicBrainzId,omitempty"`
		} `json:"album"`
	} `json:"subsonic-response"`
}

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
			Small    string `json:"small"` // deprecated; use 250
			Large    string `json:"large"` // deprecated; use 500
		} `json:"thumbnails"`
	} `json:"images"`
	ReleaseURL string `json:"release"`
}

func getAlbumIDFromTrackID(username, trackID string) (string, error) {
	data, err := host.SubsonicAPICall(fmt.Sprintf("getSong?u=%s&id=%s", username, trackID))
	if err != nil {
		return "", fmt.Errorf("getSong failed: %w", err)
	}

	var response subsonicGetSongResponse
	if err := json.Unmarshal([]byte(data), &response); err != nil {
		return "", fmt.Errorf("failed to parse getSong response: %w", err)
	}

	return response.Data.Song.AlbumID, nil
}

func getMusicBrainzIDFromAlbumID(username, albumID string) (string, error) {
	data, err := host.SubsonicAPICall(fmt.Sprintf("getAlbum?u=%s&id=%s", username, albumID))
	if err != nil {
		return "", fmt.Errorf("getAlbum failed: %w", err)
	}

	var response subsonicGetAlbumResponse
	if err := json.Unmarshal([]byte(data), &response); err != nil {
		return "", fmt.Errorf("failed to parse getAlbum response: %w", err)
	}

	return response.Data.Album.MusicBrainzId, nil
}

func fetchImageFromCAA(username, trackID string) (string, error) {
	albumID, err := getAlbumIDFromTrackID(username, trackID)
	if err != nil {
		return "", fmt.Errorf("failed to get album ID from track %s: %w", trackID, err)
	}
	if albumID == "" {
		pdk.Log(pdk.LogDebug, fmt.Sprintf("No Album ID for track %s", trackID))
		return "", nil
	}

	musicBrainzID, err := getMusicBrainzIDFromAlbumID(username, albumID)
	if err != nil {
		return "", fmt.Errorf("failed to get MusicBrainz ID from album %s: %w", trackID, err)
	}
	if musicBrainzID == "" {
		pdk.Log(pdk.LogDebug, fmt.Sprintf("No MusicBrainz ID for album %s", albumID))
		return "", nil
	}

	req := pdk.NewHTTPRequest(pdk.MethodGet, fmt.Sprintf("https://coverartarchive.org/release/%s", musicBrainzID))
	resp := req.Send()

	status := resp.Status()
	if status == 404 {
		pdk.Log(pdk.LogDebug, fmt.Sprintf("No cover art for MusicBrainz ID %s", musicBrainzID))
		return "", nil
	}
	if status >= 400 {
		return "", fmt.Errorf("Cover Art Archive request failed: HTTP %d", resp.Status())
	}

	var result caaResponse
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return "", fmt.Errorf("failed to parse Cover Art Archive response: %w", err)
	}

	for _, image := range result.Images {
		if image.Front {
			return image.ThumbnailImageURLs.Size250, nil
		}
	}

	pdk.Log(pdk.LogDebug, fmt.Sprintf("No front cover art for MusicBrainz ID %s (%d images)", musicBrainzID, len(result.Images)))
	return "", nil
}

func getImageViaCAA(username, trackID string) string {
	url, err := fetchImageFromCAA(username, trackID)
	if err != nil {
		pdk.Log(pdk.LogWarn, fmt.Sprintf("Failed to get image from Cover Art archive: %v", err))
		return ""
	}

	cacheKey := fmt.Sprintf(cacheKeyFormat, trackID)
	_ = host.CacheSetString(cacheKey, url, 86400)

	return url
}

// ============================================================================
// Image URL Resolution
// ============================================================================

const uguuEnabledKey = "uguuenabled"
const caaEnabledKey = "caaenabled"

func getImageURL(username, trackID string) string {
	cacheKey := fmt.Sprintf(cacheKeyFormat, trackID)
	cachedURL, exists, err := host.CacheGetString(cacheKey)
	if err == nil && exists {
		pdk.Log(pdk.LogDebug, fmt.Sprintf("Cache hit for artwork: %s", trackID))
		return cachedURL
	}

	caaEnabled, _ := pdk.GetConfig(caaEnabledKey)
	if caaEnabled == "true" {
		if url := getImageViaCAA(username, trackID); url != "" {
			return url
		}
	}

	uguuEnabled, _ := pdk.GetConfig(uguuEnabledKey)
	if uguuEnabled == "true" {
		return getImageViaUguu(username, trackID)
	}

	return getImageDirect(trackID)
}
