package main

import (
	"github.com/navidrome/navidrome/plugins/pdk/go/pdk"
	"github.com/navidrome/navidrome/plugins/pdk/go/scrobbler"
	"github.com/stretchr/testify/mock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("isBlacklisted", func() {
	const usersConfig = `[{"username":"user1","token":"token1","blacklist":["9O3pkfh2MhOJM1OPYJSTsh",""]}]`

	BeforeEach(func() {
		pdk.ResetMock()
		pdk.PDKMock.On("Log", mock.Anything, mock.Anything).Maybe()
		pdk.PDKMock.On("GetConfig", usersKey).Return(usersConfig, true)
	})

	It("matches a blacklisted track by ID, ignoring stray spaces", func() {
		track := scrobbler.TrackInfo{ID: " 9O3pkfh2MhOJM1OPYJSTsh "}
		Expect(isBlacklisted("user1", track)).To(BeTrue())
	})

	It("does not match a track whose ID is not listed", func() {
		track := scrobbler.TrackInfo{ID: "aB7kd0Qz1Lmn4RtuVwx2Yz"}
		Expect(isBlacklisted("user1", track)).To(BeFalse())
	})

	It("applies the blacklist only to its own user", func() {
		track := scrobbler.TrackInfo{ID: "9O3pkfh2MhOJM1OPYJSTsh"}
		Expect(isBlacklisted("user2", track)).To(BeFalse())
	})

	It("does not blacklist a track with an empty ID", func() {
		track := scrobbler.TrackInfo{ID: "   "}
		Expect(isBlacklisted("user1", track)).To(BeFalse())
	})
})
