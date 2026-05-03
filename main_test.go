package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/navidrome/navidrome/plugins/pdk/go/host"
	"github.com/navidrome/navidrome/plugins/pdk/go/pdk"
	"github.com/navidrome/navidrome/plugins/pdk/go/scheduler"
	"github.com/navidrome/navidrome/plugins/pdk/go/scrobbler"
	"github.com/stretchr/testify/mock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("discordPlugin", func() {
	var plugin discordPlugin

	BeforeEach(func() {
		plugin = discordPlugin{}
		pdk.ResetMock()
		host.CacheMock.ExpectedCalls = nil
		host.CacheMock.Calls = nil
		host.ConfigMock.ExpectedCalls = nil
		host.ConfigMock.Calls = nil
		host.WebSocketMock.ExpectedCalls = nil
		host.WebSocketMock.Calls = nil
		host.SchedulerMock.ExpectedCalls = nil
		host.SchedulerMock.Calls = nil
		host.ArtworkMock.ExpectedCalls = nil
		host.ArtworkMock.Calls = nil
		host.SubsonicAPIMock.ExpectedCalls = nil
		host.SubsonicAPIMock.Calls = nil
		host.HTTPMock.ExpectedCalls = nil
		host.HTTPMock.Calls = nil
	})

	Describe("getConfig", func() {
		It("returns config values when properly set", func() {
			pdk.PDKMock.On("GetConfig", clientIDKey).Return("test-client-id", true)
			pdk.PDKMock.On("GetConfig", usersKey).Return(`[{"username":"user1","token":"token1"},{"username":"user2","token":"token2"}]`, true)
			pdk.PDKMock.On("Log", mock.Anything, mock.Anything).Maybe()

			clientID, users, err := getConfig()
			Expect(err).ToNot(HaveOccurred())
			Expect(clientID).To(Equal("test-client-id"))
			Expect(users).To(HaveLen(2))
			Expect(users["user1"]).To(Equal("token1"))
			Expect(users["user2"]).To(Equal("token2"))
		})

		It("returns empty client ID when not set", func() {
			pdk.PDKMock.On("GetConfig", clientIDKey).Return("", false)
			pdk.PDKMock.On("Log", mock.Anything, mock.Anything).Maybe()

			clientID, users, err := getConfig()
			Expect(err).ToNot(HaveOccurred())
			Expect(clientID).To(BeEmpty())
			Expect(users).To(BeNil())
		})

		It("returns nil users when users not configured", func() {
			pdk.PDKMock.On("GetConfig", clientIDKey).Return("test-client-id", true)
			pdk.PDKMock.On("GetConfig", usersKey).Return("", false)
			pdk.PDKMock.On("Log", mock.Anything, mock.Anything).Maybe()

			clientID, users, err := getConfig()
			Expect(err).ToNot(HaveOccurred())
			Expect(clientID).To(Equal("test-client-id"))
			Expect(users).To(BeNil())
		})
	})

	Describe("IsAuthorized", func() {
		BeforeEach(func() {
			pdk.PDKMock.On("Log", mock.Anything, mock.Anything).Maybe()
		})

		It("returns true for authorized user", func() {
			pdk.PDKMock.On("GetConfig", clientIDKey).Return("test-client-id", true)
			pdk.PDKMock.On("GetConfig", usersKey).Return(`[{"username":"testuser","token":"token123"}]`, true)

			authorized, err := plugin.IsAuthorized(scrobbler.IsAuthorizedRequest{
				Username: "testuser",
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(authorized).To(BeTrue())
		})

		It("returns false for unauthorized user", func() {
			pdk.PDKMock.On("GetConfig", clientIDKey).Return("test-client-id", true)
			pdk.PDKMock.On("GetConfig", usersKey).Return(`[{"username":"otheruser","token":"token123"}]`, true)

			authorized, err := plugin.IsAuthorized(scrobbler.IsAuthorizedRequest{
				Username: "testuser",
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(authorized).To(BeFalse())
		})
	})

	Describe("PlaybackReport", func() {
		BeforeEach(func() {
			pdk.PDKMock.On("Log", mock.Anything, mock.Anything).Maybe()
		})

		baseRequest := func(state string) scrobbler.PlaybackReportRequest {
			return scrobbler.PlaybackReportRequest{
				Username:     "testuser",
				State:        state,
				PositionMs:   10000,
				PlaybackRate: 1.0,
				Timestamp:    1714600000,
				Track: scrobbler.TrackInfo{
					ID:       "track1",
					Title:    "Test Song",
					Artist:   "Test Artist",
					Album:    "Test Album",
					Duration: 180,
				},
			}
		}

		setupConnectMocks := func() {
			host.CacheMock.On("GetInt", "discord.seq.testuser").Return(int64(0), false, errors.New("not found"))
			gatewayResp := []byte(`{"url":"wss://gateway.discord.gg"}`)
			host.HTTPMock.On("Send", mock.MatchedBy(func(req host.HTTPRequest) bool {
				return req.Method == "GET" && req.URL == "https://discord.com/api/gateway"
			})).Return(&host.HTTPResponse{StatusCode: 200, Body: gatewayResp}, nil)
			host.WebSocketMock.On("Connect", mock.MatchedBy(func(url string) bool {
				return strings.Contains(url, "gateway.discord.gg")
			}), mock.Anything, "testuser").Return("testuser", nil)
			host.SchedulerMock.On("ScheduleRecurring", mock.Anything, payloadHeartbeat, "testuser").Return("testuser", nil)
		}

		setupConfigMocks := func() {
			pdk.PDKMock.On("GetConfig", clientIDKey).Return("test-client-id", true)
			pdk.PDKMock.On("GetConfig", usersKey).Return(`[{"username":"testuser","token":"test-token"}]`, true)
			pdk.PDKMock.On("GetConfig", uguuEnabledKey).Return("", false)
			pdk.PDKMock.On("GetConfig", caaEnabledKey).Return("", false)
			pdk.PDKMock.On("GetConfig", activityNameKey).Return("", false)
			pdk.PDKMock.On("GetConfig", spotifyLinksKey).Return("", false)
		}

		setupImageMocks := func() {
			host.CacheMock.On("GetString", discordImageKey).Return("", false, nil)
			host.CacheMock.On("SetString", discordImageKey, mock.Anything, mock.Anything).Return(nil)
			host.ArtworkMock.On("GetTrackUrl", "track1", int32(300)).Return("https://example.com/art.jpg", nil)
			host.HTTPMock.On("Send", externalAssetsReq).Return(&host.HTTPResponse{StatusCode: 200, Body: []byte(`[{"external_asset_path":"external/art"}]`)}, nil)
		}

		Context("starting state", func() {
			It("is a no-op", func() {
				err := plugin.PlaybackReport(baseRequest("starting"))
				Expect(err).ToNot(HaveOccurred())
			})
		})

		Context("unknown state", func() {
			It("is a no-op", func() {
				err := plugin.PlaybackReport(baseRequest("unknown"))
				Expect(err).ToNot(HaveOccurred())
			})
		})

		Context("playing state", func() {
			It("returns not authorized error when user not in config", func() {
				pdk.PDKMock.On("GetConfig", clientIDKey).Return("test-client-id", true)
				pdk.PDKMock.On("GetConfig", usersKey).Return(`[{"username":"otheruser","token":"token"}]`, true)

				err := plugin.PlaybackReport(baseRequest("playing"))
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("not authorized"))
			})

			It("sends activity with running timestamps and no small overlay", func() {
				setupConfigMocks()
				setupConnectMocks()
				setupImageMocks()

				var sentPayload string
				host.WebSocketMock.On("SendText", "testuser", mock.Anything).Run(func(args mock.Arguments) {
					sentPayload = args.Get(1).(string)
				}).Return(nil)

				err := plugin.PlaybackReport(baseRequest("playing"))
				Expect(err).ToNot(HaveOccurred())

				Expect(sentPayload).ToNot(ContainSubstring(`"small_image"`))
				Expect(sentPayload).To(ContainSubstring(`"start":`))
				Expect(sentPayload).To(ContainSubstring(`"end":`))
			})

			It("adjusts end time for non-1.0 playback rate", func() {
				setupConfigMocks()
				setupConnectMocks()
				setupImageMocks()

				var sentPayload string
				host.WebSocketMock.On("SendText", "testuser", mock.Anything).Run(func(args mock.Arguments) {
					sentPayload = args.Get(1).(string)
				}).Return(nil)

				req := baseRequest("playing")
				req.PlaybackRate = 2.0

				err := plugin.PlaybackReport(req)
				Expect(err).ToNot(HaveOccurred())

				// With 2x speed: position and duration are both scaled by rate
				// wallElapsed = 10000ms / 2.0 = 5000ms
				// startTime = 1714600000*1000 - 5000 = 1714599995000
				// wallDuration = 180*1000 / 2.0 = 90000ms
				// endTime = 1714599995000 + 90000 = 1714600085000
				Expect(sentPayload).To(ContainSubstring(`"start":1714599995000`))
				Expect(sentPayload).To(ContainSubstring(`"end":1714600085000`))
			})
		})

		Context("paused state", func() {
			It("sends activity with frozen timestamps and pause icon overlay", func() {
				setupConfigMocks()
				setupConnectMocks()
				setupImageMocks()

				var sentPayload string
				host.WebSocketMock.On("SendText", "testuser", mock.Anything).Run(func(args mock.Arguments) {
					sentPayload = args.Get(1).(string)
				}).Return(nil)

				err := plugin.PlaybackReport(baseRequest("paused"))
				Expect(err).ToNot(HaveOccurred())

				Expect(sentPayload).To(ContainSubstring(`"small_text":"Paused"`))
				Expect(sentPayload).ToNot(ContainSubstring(`"end":`))
				// Paused start = Timestamp * 1000 = 1714600000000
				Expect(sentPayload).To(ContainSubstring(`"start":1714600000000`))
			})
		})

		Context("stopped state", func() {
			It("clears activity and disconnects", func() {
				host.WebSocketMock.On("SendText", "testuser", mock.MatchedBy(func(msg string) bool {
					return strings.Contains(msg, `"op":3`) && strings.Contains(msg, `"activities":null`)
				})).Return(nil)
				host.SchedulerMock.On("CancelSchedule", "testuser").Return(nil)
				host.WebSocketMock.On("CloseConnection", "testuser", int32(1000), "Navidrome disconnect").Return(nil)

				err := plugin.PlaybackReport(baseRequest("stopped"))
				Expect(err).ToNot(HaveOccurred())
			})
		})

		Context("expired state", func() {
			It("clears activity and disconnects (same as stopped)", func() {
				host.WebSocketMock.On("SendText", "testuser", mock.MatchedBy(func(msg string) bool {
					return strings.Contains(msg, `"op":3`) && strings.Contains(msg, `"activities":null`)
				})).Return(nil)
				host.SchedulerMock.On("CancelSchedule", "testuser").Return(nil)
				host.WebSocketMock.On("CloseConnection", "testuser", int32(1000), "Navidrome disconnect").Return(nil)

				err := plugin.PlaybackReport(baseRequest("expired"))
				Expect(err).ToNot(HaveOccurred())
			})
		})

		DescribeTable("activity name configuration",
			func(configValue string, configExists bool, expectedName string, expectedDisplayType int) {
				pdk.PDKMock.On("GetConfig", clientIDKey).Return("test-client-id", true)
				pdk.PDKMock.On("GetConfig", usersKey).Return(`[{"username":"testuser","token":"test-token"}]`, true)
				pdk.PDKMock.On("GetConfig", uguuEnabledKey).Return("", false)
				pdk.PDKMock.On("GetConfig", caaEnabledKey).Return("", false)
				pdk.PDKMock.On("GetConfig", activityNameKey).Return(configValue, configExists)
				pdk.PDKMock.On("GetConfig", spotifyLinksKey).Return("", false)

				setupConnectMocks()
				setupImageMocks()

				var sentPayload string
				host.WebSocketMock.On("SendText", "testuser", mock.Anything).Run(func(args mock.Arguments) {
					sentPayload = args.Get(1).(string)
				}).Return(nil)

				err := plugin.PlaybackReport(baseRequest("playing"))
				Expect(err).ToNot(HaveOccurred())
				Expect(sentPayload).To(ContainSubstring(fmt.Sprintf(`"name":"%s"`, expectedName)))
				Expect(sentPayload).To(ContainSubstring(fmt.Sprintf(`"status_display_type":%d`, expectedDisplayType)))
			},
			Entry("defaults to Navidrome when not configured", "", false, "Navidrome", 2),
			Entry("defaults to Navidrome with explicit default value", "Default", true, "Navidrome", 2),
			Entry("uses track title when configured", "Track", true, "Test Song", 0),
			Entry("uses track album when configured", "Album", true, "Test Album", 0),
			Entry("uses track artist when configured", "Artist", true, "Test Artist", 0),
		)

		DescribeTable("custom activity name template",
			func(template string, templateExists bool, expectedName string) {
				pdk.PDKMock.On("GetConfig", clientIDKey).Return("test-client-id", true)
				pdk.PDKMock.On("GetConfig", usersKey).Return(`[{"username":"testuser","token":"test-token"}]`, true)
				pdk.PDKMock.On("GetConfig", uguuEnabledKey).Return("", false)
				pdk.PDKMock.On("GetConfig", caaEnabledKey).Return("", false)
				pdk.PDKMock.On("GetConfig", activityNameKey).Return("Custom", true)
				pdk.PDKMock.On("GetConfig", activityNameTemplateKey).Return(template, templateExists)
				pdk.PDKMock.On("GetConfig", spotifyLinksKey).Return("", false)

				setupConnectMocks()
				setupImageMocks()

				var sentPayload string
				host.WebSocketMock.On("SendText", "testuser", mock.Anything).Run(func(args mock.Arguments) {
					sentPayload = args.Get(1).(string)
				}).Return(nil)

				err := plugin.PlaybackReport(baseRequest("playing"))
				Expect(err).ToNot(HaveOccurred())
				Expect(sentPayload).To(ContainSubstring(fmt.Sprintf(`"name":"%s"`, expectedName)))
			},
			Entry("uses custom template with all placeholders", "{artist} - {track} ({album})", true, "Test Artist - Test Song (Test Album)"),
			Entry("uses custom template with only track", "{track}", true, "Test Song"),
			Entry("uses custom template with only artist", "{artist}", true, "Test Artist"),
			Entry("uses custom template with only album", "{album}", true, "Test Album"),
			Entry("uses custom template with plain text", "Now Playing", true, "Now Playing"),
			Entry("falls back to Navidrome when template is empty", "", false, "Navidrome"),
		)
	})

	Describe("OnCallback", func() {
		BeforeEach(func() {
			pdk.PDKMock.On("Log", mock.Anything, mock.Anything).Maybe()
		})

		It("handles heartbeat callback", func() {
			host.CacheMock.On("GetInt", "discord.seq.testuser").Return(int64(42), true, nil)
			host.WebSocketMock.On("SendText", "testuser", mock.Anything).Return(nil)

			err := plugin.OnCallback(scheduler.SchedulerCallbackRequest{
				ScheduleID:  "testuser",
				Payload:     payloadHeartbeat,
				IsRecurring: true,
			})
			Expect(err).ToNot(HaveOccurred())
		})

		It("logs warning for unknown payload", func() {
			err := plugin.OnCallback(scheduler.SchedulerCallbackRequest{
				ScheduleID: "testuser",
				Payload:    "unknown",
			})
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
