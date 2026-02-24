package main

import (
	"encoding/json"
	"fmt"

	"github.com/navidrome/navidrome/plugins/pdk/go/host"
	"github.com/navidrome/navidrome/plugins/pdk/go/pdk"
	"github.com/navidrome/navidrome/plugins/pdk/go/scrobbler"
	"github.com/stretchr/testify/mock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Spotify", func() {
	Describe("spotifySearchURL", func() {
		DescribeTable("constructs Spotify search URL",
			func(expectedURL string, terms ...string) {
				Expect(spotifySearchURL(terms...)).To(Equal(expectedURL))
			},
			Entry("artist and title", "https://open.spotify.com/search/Rick%20Astley%20Never%20Gonna%20Give%20You%20Up", "Rick Astley", "Never Gonna Give You Up"),
			Entry("single term", "https://open.spotify.com/search/Radiohead", "Radiohead"),
			Entry("empty terms", "", "", ""),
			Entry("one empty term", "https://open.spotify.com/search/Solo%20Artist", "Solo Artist", ""),
		)
	})

	Describe("spotifyCacheKey", func() {
		It("produces identical keys for identical inputs", func() {
			key1 := spotifyCacheKey("Radiohead", "Karma Police", "OK Computer")
			key2 := spotifyCacheKey("Radiohead", "Karma Police", "OK Computer")
			Expect(key1).To(Equal(key2))
		})

		It("produces different keys for different albums", func() {
			key1 := spotifyCacheKey("Radiohead", "Karma Police", "OK Computer")
			key2 := spotifyCacheKey("Radiohead", "Karma Police", "The Bends")
			Expect(key1).ToNot(Equal(key2))
		})

		It("uses the correct prefix", func() {
			key := spotifyCacheKey("Radiohead", "Karma Police", "OK Computer")
			Expect(key).To(HavePrefix("spotify.url."))
		})

		It("is case-insensitive", func() {
			keyUpper := spotifyCacheKey("Radiohead", "Karma Police", "OK Computer")
			keyLower := spotifyCacheKey("radiohead", "karma police", "ok computer")
			Expect(keyUpper).To(Equal(keyLower))
		})
	})

	Describe("parseSpotifyID", func() {
		DescribeTable("extracts first Spotify track ID from ListenBrainz response",
			func(body, expectedID string) {
				Expect(parseSpotifyID([]byte(body))).To(Equal(expectedID))
			},
			Entry("valid single result",
				`[{"spotify_track_ids":["4tIGK5G9hNDA50ZdGioZRG"]}]`, "4tIGK5G9hNDA50ZdGioZRG"),
			Entry("multiple IDs picks first",
				`[{"artist_name":"Lil Baby & Drake","track_name":"Yes Indeed","spotify_track_ids":["6vN77lE9LK6HP2DewaN6HZ","4wlLbLeDWbA6TzwZFp1UaK"]}]`, "6vN77lE9LK6HP2DewaN6HZ"),
			Entry("valid result with extra fields",
				`[{"artist_name":"Radiohead","track_name":"Karma Police","spotify_track_ids":["63OQupATfueTdZMWIV7nzz"],"release_name":"OK Computer"}]`, "63OQupATfueTdZMWIV7nzz"),
			Entry("empty spotify_track_ids array",
				`[{"spotify_track_ids":[]}]`, ""),
			Entry("no spotify_track_ids field",
				`[{"artist_name":"Unknown"}]`, ""),
			Entry("empty array",
				`[]`, ""),
			Entry("invalid JSON",
				`not json`, ""),
			Entry("null first result falls through to second",
				`[{"spotify_track_ids":[]},{"spotify_track_ids":["6vN77lE9LK6HP2DewaN6HZ"]}]`, "6vN77lE9LK6HP2DewaN6HZ"),
			Entry("skips invalid ID with special characters",
				`[{"spotify_track_ids":["abc!@#$%^&*()_+=-12345"]}]`, ""),
		)
	})

	Describe("isValidSpotifyID", func() {
		DescribeTable("validates Spotify track IDs",
			func(id string, expected bool) {
				Expect(isValidSpotifyID(id)).To(Equal(expected))
			},
			Entry("valid 22-char ID", "6vN77lE9LK6HP2DewaN6HZ", true),
			Entry("another valid ID", "4tIGK5G9hNDA50ZdGioZRG", true),
			Entry("short valid ID", "abc123", true),
			Entry("special characters", "6vN77lE9!K6HP2DewaN6HZ", false),
			Entry("spaces", "6vN77 E9LK6HP2DewaN6HZ", false),
			Entry("empty string", "", false),
		)
	})

	Describe("ListenBrainz request payloads", func() {
		It("builds valid JSON for MBID requests", func() {
			mbid := "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
			body := []byte(fmt.Sprintf(`[{"recording_mbid":%q}]`, mbid))
			var parsed []map[string]string
			Expect(json.Unmarshal(body, &parsed)).To(Succeed())
			Expect(parsed[0]["recording_mbid"]).To(Equal(mbid))
		})

		It("builds valid JSON for metadata requests with special characters", func() {
			artist := `Guns N' Roses`
			title := `Sweet Child O' Mine`
			album := `Appetite for Destruction`
			payload := fmt.Sprintf(`[{"artist_name":%q,"track_name":%q,"release_name":%q}]`, artist, title, album)
			var parsed []map[string]string
			Expect(json.Unmarshal([]byte(payload), &parsed)).To(Succeed())
			Expect(parsed[0]["artist_name"]).To(Equal(artist))
			Expect(parsed[0]["track_name"]).To(Equal(title))
			Expect(parsed[0]["release_name"]).To(Equal(album))
		})
	})

	Describe("resolveSpotifyURL", func() {
		BeforeEach(func() {
			pdk.ResetMock()
			host.CacheMock.ExpectedCalls = nil
			host.CacheMock.Calls = nil
			host.HTTPMock.ExpectedCalls = nil
			host.HTTPMock.Calls = nil
			pdk.PDKMock.On("Log", mock.Anything, mock.Anything).Maybe()
		})

		It("returns cached URL on cache hit", func() {
			host.CacheMock.On("GetString", spotifyURLKey).Return("https://open.spotify.com/track/cached123", true, nil)

			url := resolveSpotifyURL(scrobbler.TrackInfo{
				Title:   "Karma Police",
				Artist:  "Radiohead",
				Artists: []scrobbler.ArtistRef{{Name: "Radiohead"}},
				Album:   "OK Computer",
			})
			Expect(url).To(Equal("https://open.spotify.com/track/cached123"))
		})

		It("resolves via MBID when available", func() {
			host.CacheMock.On("GetString", spotifyURLKey).Return("", false, nil)
			host.CacheMock.On("SetString", spotifyURLKey, mock.Anything, mock.Anything).Return(nil)

			// Mock the MBID HTTP request
			host.HTTPMock.On("Send", mock.MatchedBy(func(req host.HTTPRequest) bool {
				return req.URL == "https://labs.api.listenbrainz.org/spotify-id-from-mbid/json"
			})).Return(&host.HTTPResponse{StatusCode: 200, Body: []byte(`[{"spotify_track_ids":["63OQupATfueTdZMWIV7nzz"]}]`)}, nil)

			url := resolveSpotifyURL(scrobbler.TrackInfo{
				Title:          "Karma Police",
				Artist:         "Radiohead",
				Artists:        []scrobbler.ArtistRef{{Name: "Radiohead"}},
				Album:          "OK Computer",
				MBZRecordingID: "mbid-123",
			})
			Expect(url).To(Equal("https://open.spotify.com/track/63OQupATfueTdZMWIV7nzz"))
			host.CacheMock.AssertCalled(GinkgoT(), "SetString", spotifyURLKey, "https://open.spotify.com/track/63OQupATfueTdZMWIV7nzz", spotifyCacheTTLHit)
		})

		It("falls back to metadata lookup when MBID fails", func() {
			host.CacheMock.On("GetString", spotifyURLKey).Return("", false, nil)
			host.CacheMock.On("SetString", spotifyURLKey, mock.Anything, mock.Anything).Return(nil)

			// MBID request fails
			host.HTTPMock.On("Send", mock.MatchedBy(func(req host.HTTPRequest) bool {
				return req.URL == "https://labs.api.listenbrainz.org/spotify-id-from-mbid/json"
			})).Return(&host.HTTPResponse{StatusCode: 404, Body: []byte(`[]`)}, nil)

			// Metadata request succeeds
			host.HTTPMock.On("Send", mock.MatchedBy(func(req host.HTTPRequest) bool {
				return req.URL == "https://labs.api.listenbrainz.org/spotify-id-from-metadata/json"
			})).Return(&host.HTTPResponse{StatusCode: 200, Body: []byte(`[{"spotify_track_ids":["4wlLbLeDWbA6TzwZFp1UaK"]}]`)}, nil)

			url := resolveSpotifyURL(scrobbler.TrackInfo{
				Title:          "Karma Police",
				Artist:         "Radiohead",
				Artists:        []scrobbler.ArtistRef{{Name: "Radiohead"}},
				Album:          "OK Computer",
				MBZRecordingID: "mbid-123",
			})
			Expect(url).To(Equal("https://open.spotify.com/track/4wlLbLeDWbA6TzwZFp1UaK"))
		})

		It("falls back to search URL when both lookups fail", func() {
			host.CacheMock.On("GetString", spotifyURLKey).Return("", false, nil)
			host.CacheMock.On("SetString", spotifyURLKey, mock.Anything, mock.Anything).Return(nil)

			// No MBID, metadata request fails
			host.HTTPMock.On("Send", mock.MatchedBy(func(req host.HTTPRequest) bool {
				return req.URL == "https://labs.api.listenbrainz.org/spotify-id-from-metadata/json"
			})).Return(&host.HTTPResponse{StatusCode: 500, Body: []byte(`error`)}, nil)

			url := resolveSpotifyURL(scrobbler.TrackInfo{
				Title:   "Karma Police",
				Artist:  "Radiohead",
				Artists: []scrobbler.ArtistRef{{Name: "Radiohead"}},
				Album:   "OK Computer",
			})
			Expect(url).To(HavePrefix("https://open.spotify.com/search/"))
			Expect(url).To(ContainSubstring("Radiohead"))
			host.CacheMock.AssertCalled(GinkgoT(), "SetString", spotifyURLKey, mock.Anything, spotifyCacheTTLMiss)
		})

		It("uses Artists[0] for primary artist", func() {
			host.CacheMock.On("GetString", spotifyURLKey).Return("", false, nil)
			host.CacheMock.On("SetString", spotifyURLKey, mock.Anything, mock.Anything).Return(nil)

			host.HTTPMock.On("Send", mock.MatchedBy(func(req host.HTTPRequest) bool {
				return req.URL == "https://labs.api.listenbrainz.org/spotify-id-from-metadata/json"
			})).Return(&host.HTTPResponse{StatusCode: 200, Body: []byte(`[{"spotify_track_ids":["4tIGK5G9hNDA50ZdGioZRG"]}]`)}, nil)

			url := resolveSpotifyURL(scrobbler.TrackInfo{
				Title:   "Some Song",
				Artist:  "",
				Album:   "Some Album",
				Artists: []scrobbler.ArtistRef{{Name: "Fallback Artist"}},
			})
			Expect(url).To(Equal("https://open.spotify.com/track/4tIGK5G9hNDA50ZdGioZRG"))
		})
	})
})
