package main

import (
	"errors"
	"strings"

	"github.com/navidrome/navidrome/plugins/pdk/go/host"
	"github.com/navidrome/navidrome/plugins/pdk/go/pdk"
	"github.com/navidrome/navidrome/plugins/pdk/go/websocket"
	"github.com/stretchr/testify/mock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("discordRPC", func() {
	var r *discordRPC

	BeforeEach(func() {
		r = &discordRPC{}
		pdk.ResetMock()
		host.CacheMock.ExpectedCalls = nil
		host.CacheMock.Calls = nil
		host.WebSocketMock.ExpectedCalls = nil
		host.WebSocketMock.Calls = nil
		host.SchedulerMock.ExpectedCalls = nil
		host.SchedulerMock.Calls = nil
		host.HTTPMock.ExpectedCalls = nil
		host.HTTPMock.Calls = nil
	})

	Describe("sendMessage", func() {
		It("sends JSON message over WebSocket", func() {
			pdk.PDKMock.On("Log", mock.Anything, mock.Anything).Maybe()
			host.WebSocketMock.On("SendText", "testuser", mock.MatchedBy(func(msg string) bool {
				return strings.Contains(msg, `"op":3`)
			})).Return(nil)

			err := r.sendMessage("testuser", presenceOpCode, map[string]string{"status": "online"})
			Expect(err).ToNot(HaveOccurred())
			host.WebSocketMock.AssertExpectations(GinkgoT())
		})

		It("returns error when WebSocket send fails", func() {
			pdk.PDKMock.On("Log", mock.Anything, mock.Anything).Maybe()
			host.WebSocketMock.On("SendText", mock.Anything, mock.Anything).
				Return(errors.New("connection closed"))

			err := r.sendMessage("testuser", presenceOpCode, map[string]string{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("connection closed"))
		})
	})

	Describe("sendHeartbeat", func() {
		It("retrieves sequence number from cache and sends heartbeat", func() {
			pdk.PDKMock.On("Log", mock.Anything, mock.Anything).Maybe()
			host.CacheMock.On("GetInt", "discord.seq.testuser").Return(int64(123), true, nil)
			host.WebSocketMock.On("SendText", "testuser", mock.MatchedBy(func(msg string) bool {
				return strings.Contains(msg, `"op":1`) && strings.Contains(msg, "123")
			})).Return(nil)

			err := r.sendHeartbeat("testuser")
			Expect(err).ToNot(HaveOccurred())
			host.CacheMock.AssertExpectations(GinkgoT())
			host.WebSocketMock.AssertExpectations(GinkgoT())
		})

		It("returns error when cache get fails", func() {
			pdk.PDKMock.On("Log", mock.Anything, mock.Anything).Maybe()
			host.CacheMock.On("GetInt", "discord.seq.testuser").Return(int64(0), false, errors.New("cache error"))

			err := r.sendHeartbeat("testuser")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cache error"))
		})
	})

	Describe("connect", func() {
		It("establishes WebSocket connection and sends identify payload", func() {
			pdk.PDKMock.On("Log", mock.Anything, mock.Anything).Maybe()
			host.CacheMock.On("GetInt", "discord.seq.testuser").Return(int64(0), false, errors.New("not found"))

			// Mock HTTP GET request for gateway discovery
			gatewayResp := []byte(`{"url":"wss://gateway.discord.gg"}`)
			host.HTTPMock.On("Send", mock.MatchedBy(func(req host.HTTPRequest) bool {
				return req.Method == "GET" && req.URL == "https://discord.com/api/gateway"
			})).Return(&host.HTTPResponse{StatusCode: 200, Body: gatewayResp}, nil)

			// Mock WebSocket connection
			host.WebSocketMock.On("Connect", mock.MatchedBy(func(url string) bool {
				return strings.Contains(url, "gateway.discord.gg")
			}), mock.Anything, "testuser").Return("testuser", nil)
			host.WebSocketMock.On("SendText", "testuser", mock.MatchedBy(func(msg string) bool {
				return strings.Contains(msg, `"op":2`) && strings.Contains(msg, "test-token")
			})).Return(nil)
			host.SchedulerMock.On("ScheduleRecurring", "@every 41s", payloadHeartbeat, "testuser").
				Return("testuser", nil)

			err := r.connect("testuser", "test-token")
			Expect(err).ToNot(HaveOccurred())
		})

		It("reuses existing connection if connected", func() {
			pdk.PDKMock.On("Log", mock.Anything, mock.Anything).Maybe()
			host.CacheMock.On("GetInt", "discord.seq.testuser").Return(int64(42), true, nil)
			host.WebSocketMock.On("SendText", "testuser", mock.Anything).Return(nil)

			err := r.connect("testuser", "test-token")
			Expect(err).ToNot(HaveOccurred())
			host.WebSocketMock.AssertNotCalled(GinkgoT(), "Connect", mock.Anything, mock.Anything, mock.Anything)
		})
	})

	Describe("disconnect", func() {
		It("cancels schedule and closes WebSocket connection", func() {
			pdk.PDKMock.On("Log", mock.Anything, mock.Anything).Maybe()
			host.SchedulerMock.On("CancelSchedule", "testuser").Return(nil)
			host.WebSocketMock.On("CloseConnection", "testuser", int32(1000), "Navidrome disconnect").Return(nil)

			err := r.disconnect("testuser")
			Expect(err).ToNot(HaveOccurred())
			host.SchedulerMock.AssertExpectations(GinkgoT())
			host.WebSocketMock.AssertExpectations(GinkgoT())
		})
	})

	Describe("cleanupFailedConnection", func() {
		It("cancels schedule, closes WebSocket, and clears cache", func() {
			pdk.PDKMock.On("Log", mock.Anything, mock.Anything).Maybe()
			host.SchedulerMock.On("CancelSchedule", "testuser").Return(nil)
			host.WebSocketMock.On("CloseConnection", "testuser", int32(1000), "Connection lost").Return(nil)
			host.CacheMock.On("Remove", "discord.seq.testuser").Return(nil)

			r.cleanupFailedConnection("testuser")

			host.SchedulerMock.AssertExpectations(GinkgoT())
			host.WebSocketMock.AssertExpectations(GinkgoT())
		})
	})

	Describe("handleHeartbeatCallback", func() {
		It("sends heartbeat successfully", func() {
			pdk.PDKMock.On("Log", mock.Anything, mock.Anything).Maybe()
			host.CacheMock.On("GetInt", "discord.seq.testuser").Return(int64(42), true, nil)
			host.WebSocketMock.On("SendText", "testuser", mock.Anything).Return(nil)

			err := r.handleHeartbeatCallback("testuser")
			Expect(err).ToNot(HaveOccurred())
		})

		It("cleans up connection on heartbeat failure", func() {
			pdk.PDKMock.On("Log", mock.Anything, mock.Anything).Maybe()
			host.CacheMock.On("GetInt", "discord.seq.testuser").Return(int64(0), false, errors.New("cache miss"))
			host.SchedulerMock.On("CancelSchedule", "testuser").Return(nil)
			host.WebSocketMock.On("CloseConnection", "testuser", int32(1000), "Connection lost").Return(nil)
			host.CacheMock.On("Remove", "discord.seq.testuser").Return(nil)

			err := r.handleHeartbeatCallback("testuser")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("connection cleaned up"))
		})
	})

	Describe("handleClearActivityCallback", func() {
		It("clears activity and disconnects", func() {
			pdk.PDKMock.On("Log", mock.Anything, mock.Anything).Maybe()
			host.WebSocketMock.On("SendText", "testuser", mock.MatchedBy(func(msg string) bool {
				return strings.Contains(msg, `"op":3`) && strings.Contains(msg, `"activities":null`)
			})).Return(nil)
			host.SchedulerMock.On("CancelSchedule", "testuser").Return(nil)
			host.WebSocketMock.On("CloseConnection", "testuser", int32(1000), "Navidrome disconnect").Return(nil)

			err := r.handleClearActivityCallback("testuser")
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Describe("WebSocket callbacks", func() {
		Describe("OnTextMessage", func() {
			It("handles valid JSON message", func() {
				pdk.PDKMock.On("Log", mock.Anything, mock.Anything).Maybe()
				host.CacheMock.On("SetInt", mock.Anything, mock.Anything, mock.Anything).Return(nil)

				err := r.OnTextMessage(websocket.OnTextMessageRequest{
					ConnectionID: "testuser",
					Message:      `{"s":42}`,
				})
				Expect(err).ToNot(HaveOccurred())
			})

			It("returns error for invalid JSON", func() {
				pdk.PDKMock.On("Log", mock.Anything, mock.Anything).Maybe()
				err := r.OnTextMessage(websocket.OnTextMessageRequest{
					ConnectionID: "testuser",
					Message:      `not json`,
				})
				Expect(err).To(HaveOccurred())
			})
		})

		Describe("OnBinaryMessage", func() {
			It("handles binary message without error", func() {
				pdk.PDKMock.On("Log", mock.Anything, mock.Anything).Maybe()
				err := r.OnBinaryMessage(websocket.OnBinaryMessageRequest{
					ConnectionID: "testuser",
					Data:         "AQID", // base64 encoded [0x01, 0x02, 0x03]
				})
				Expect(err).ToNot(HaveOccurred())
			})
		})

		Describe("OnError", func() {
			It("handles error without returning error", func() {
				pdk.PDKMock.On("Log", mock.Anything, mock.Anything).Maybe()
				err := r.OnError(websocket.OnErrorRequest{
					ConnectionID: "testuser",
					Error:        "test error",
				})
				Expect(err).ToNot(HaveOccurred())
			})
		})

		Describe("OnClose", func() {
			It("handles close without returning error", func() {
				pdk.PDKMock.On("Log", mock.Anything, mock.Anything).Maybe()
				err := r.OnClose(websocket.OnCloseRequest{
					ConnectionID: "testuser",
					Code:         1000,
					Reason:       "normal close",
				})
				Expect(err).ToNot(HaveOccurred())
			})
		})
	})

	Describe("processImage", func() {
		BeforeEach(func() {
			pdk.PDKMock.On("Log", mock.Anything, mock.Anything).Maybe()
		})

		It("returns error for empty URL", func() {
			_, err := r.processImage("", "client123", "token123", imageCacheTTL)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("image URL is empty"))
		})

		It("returns mp: prefixed URL as-is", func() {
			result, err := r.processImage("mp:external/abc123", "client123", "token123", imageCacheTTL)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal("mp:external/abc123"))
		})

		It("returns cached value on cache hit", func() {
			host.CacheMock.On("GetString", mock.MatchedBy(func(key string) bool {
				return strings.HasPrefix(key, "discord.image.")
			})).Return("mp:cached/image", true, nil)

			result, err := r.processImage("https://example.com/art.jpg", "client123", "token123", imageCacheTTL)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal("mp:cached/image"))
		})

		It("processes image via Discord API and caches result", func() {
			host.CacheMock.On("GetString", discordImageKey).Return("", false, nil)
			host.CacheMock.On("SetString", discordImageKey, mock.MatchedBy(func(val string) bool {
				return val == "mp:external/new-asset"
			}), int64(imageCacheTTL)).Return(nil)

			host.HTTPMock.On("Send", externalAssetsReq).Return(&host.HTTPResponse{StatusCode: 200, Body: []byte(`[{"external_asset_path":"external/new-asset"}]`)}, nil)

			result, err := r.processImage("https://example.com/art.jpg", "client123", "token123", imageCacheTTL)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal("mp:external/new-asset"))
		})

		It("returns error on HTTP failure", func() {
			host.CacheMock.On("GetString", discordImageKey).Return("", false, nil)

			host.HTTPMock.On("Send", externalAssetsReq).Return(&host.HTTPResponse{StatusCode: 500, Body: []byte(`error`)}, nil)

			_, err := r.processImage("https://example.com/art.jpg", "client123", "token123", imageCacheTTL)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("HTTP 500"))
		})

		It("returns error on unmarshal failure", func() {
			host.CacheMock.On("GetString", discordImageKey).Return("", false, nil)

			host.HTTPMock.On("Send", externalAssetsReq).Return(&host.HTTPResponse{StatusCode: 200, Body: []byte(`{"not":"an-array"}`)}, nil)

			_, err := r.processImage("https://example.com/art.jpg", "client123", "token123", imageCacheTTL)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to unmarshal"))
		})

		It("returns error on empty response array", func() {
			host.CacheMock.On("GetString", discordImageKey).Return("", false, nil)

			host.HTTPMock.On("Send", externalAssetsReq).Return(&host.HTTPResponse{StatusCode: 200, Body: []byte(`[]`)}, nil)

			_, err := r.processImage("https://example.com/art.jpg", "client123", "token123", imageCacheTTL)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no data returned"))
		})

		It("returns error on empty external_asset_path", func() {
			host.CacheMock.On("GetString", discordImageKey).Return("", false, nil)

			host.HTTPMock.On("Send", externalAssetsReq).Return(&host.HTTPResponse{StatusCode: 200, Body: []byte(`[{"external_asset_path":""}]`)}, nil)

			_, err := r.processImage("https://example.com/art.jpg", "client123", "token123", imageCacheTTL)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("empty external_asset_path"))
		})
	})

	Describe("sendActivity", func() {
		BeforeEach(func() {
			pdk.PDKMock.On("Log", mock.Anything, mock.Anything).Maybe()
		})

		It("sends activity with track artwork and SmallImage overlay", func() {
			host.CacheMock.On("GetString", discordImageKey).Return("", false, nil)
			host.CacheMock.On("SetString", discordImageKey, mock.Anything, mock.Anything).Return(nil)

			host.HTTPMock.On("Send", externalAssetsReq).Return(&host.HTTPResponse{StatusCode: 200, Body: []byte(`[{"external_asset_path":"external/art"}]`)}, nil)

			host.WebSocketMock.On("SendText", "testuser", mock.MatchedBy(func(msg string) bool {
				return strings.Contains(msg, `"op":3`) &&
					strings.Contains(msg, `"large_image":"mp:external/art"`) &&
					strings.Contains(msg, `"small_image":"mp:external/art"`) &&
					strings.Contains(msg, `"small_text":"Navidrome"`)
			})).Return(nil)

			err := r.sendActivity("client123", "testuser", "token123", activity{
				Application: "client123",
				Name:        "Test Song",
				Type:        2,
				State:       "Test Artist",
				Details:     "Test Album",
				Assets: activityAssets{
					LargeImage: "https://example.com/art.jpg",
					LargeText:  "Test Album",
					SmallImage: navidromeLogoURL,
					SmallText:  "Navidrome",
				},
			})
			Expect(err).ToNot(HaveOccurred())
		})

		It("falls back to default image and clears SmallImage", func() {
			// Track art fails (HTTP error), default image succeeds
			host.CacheMock.On("GetString", discordImageKey).Return("", false, nil)
			host.CacheMock.On("SetString", discordImageKey, mock.Anything, mock.Anything).Return(nil)

			// First call (track art) returns 500, second call (default) succeeds
			host.HTTPMock.On("Send", externalAssetsReq).Return(&host.HTTPResponse{StatusCode: 500, Body: []byte(`error`)}, nil).Once()
			host.HTTPMock.On("Send", externalAssetsReq).Return(&host.HTTPResponse{StatusCode: 200, Body: []byte(`[{"external_asset_path":"external/logo"}]`)}, nil).Once()

			host.WebSocketMock.On("SendText", "testuser", mock.MatchedBy(func(msg string) bool {
				return strings.Contains(msg, `"op":3`) &&
					strings.Contains(msg, `"large_image":"mp:external/logo"`) &&
					!strings.Contains(msg, `"small_image":"mp:`) &&
					!strings.Contains(msg, `"small_text":"Navidrome"`)
			})).Return(nil)

			err := r.sendActivity("client123", "testuser", "token123", activity{
				Application: "client123",
				Name:        "Test Song",
				Type:        2,
				State:       "Test Artist",
				Details:     "Test Album",
				Assets: activityAssets{
					LargeImage: "https://example.com/art.jpg",
					LargeText:  "Test Album",
					SmallImage: navidromeLogoURL,
					SmallText:  "Navidrome",
				},
			})
			Expect(err).ToNot(HaveOccurred())
		})

		It("clears all images when both track art and default fail", func() {
			host.CacheMock.On("GetString", discordImageKey).Return("", false, nil)

			host.HTTPMock.On("Send", externalAssetsReq).Return(&host.HTTPResponse{StatusCode: 200, Body: []byte(`{"not":"array"}`)}, nil)

			host.WebSocketMock.On("SendText", "testuser", mock.MatchedBy(func(msg string) bool {
				return strings.Contains(msg, `"op":3`) &&
					strings.Contains(msg, `"large_image":""`) &&
					!strings.Contains(msg, `"small_image":"mp:`)
			})).Return(nil)

			err := r.sendActivity("client123", "testuser", "token123", activity{
				Application: "client123",
				Name:        "Test Song",
				Type:        2,
				State:       "Test Artist",
				Details:     "Test Album",
				Assets: activityAssets{
					LargeImage: "https://example.com/art.jpg",
					LargeText:  "Test Album",
					SmallImage: navidromeLogoURL,
					SmallText:  "Navidrome",
				},
			})
			Expect(err).ToNot(HaveOccurred())
		})

		It("handles SmallImage processing failure gracefully", func() {
			// LargeImage from cache (succeeds), SmallImage API fails
			host.CacheMock.On("GetString", discordImageKey).Return("mp:cached/large", true, nil).Once()
			host.CacheMock.On("GetString", discordImageKey).Return("", false, nil).Once()

			host.HTTPMock.On("Send", externalAssetsReq).Return(&host.HTTPResponse{StatusCode: 500, Body: []byte(`error`)}, nil)

			host.WebSocketMock.On("SendText", "testuser", mock.MatchedBy(func(msg string) bool {
				return strings.Contains(msg, `"large_image":"mp:cached/large"`) &&
					!strings.Contains(msg, `"small_image":"mp:`)
			})).Return(nil)

			err := r.sendActivity("client123", "testuser", "token123", activity{
				Application: "client123",
				Name:        "Test Song",
				Type:        2,
				State:       "Test Artist",
				Details:     "Test Album",
				Assets: activityAssets{
					LargeImage: "https://example.com/art.jpg",
					LargeText:  "Test Album",
					SmallImage: navidromeLogoURL,
					SmallText:  "Navidrome",
				},
			})
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Describe("clearActivity", func() {
		It("sends presence update with nil activities", func() {
			pdk.PDKMock.On("Log", mock.Anything, mock.Anything).Maybe()
			host.WebSocketMock.On("SendText", "testuser", mock.MatchedBy(func(msg string) bool {
				return strings.Contains(msg, `"op":3`) && strings.Contains(msg, `"activities":null`)
			})).Return(nil)

			err := r.clearActivity("testuser")
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
