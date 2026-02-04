package main

import (
	"errors"

	"github.com/navidrome/navidrome/plugins/pdk/go/host"
	"github.com/navidrome/navidrome/plugins/pdk/go/pdk"
	"github.com/stretchr/testify/mock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("getImageURL", func() {
	BeforeEach(func() {
		pdk.ResetMock()
		host.CacheMock.ExpectedCalls = nil
		host.CacheMock.Calls = nil
		host.ArtworkMock.ExpectedCalls = nil
		host.ArtworkMock.Calls = nil
		host.SubsonicAPIMock.ExpectedCalls = nil
		host.SubsonicAPIMock.Calls = nil
		pdk.PDKMock.On("Log", mock.Anything, mock.Anything).Maybe()
	})

	Describe("uguu disabled (default)", func() {
		BeforeEach(func() {
			pdk.PDKMock.On("GetConfig", uguuEnabledKey).Return("", false)
		})

		It("returns artwork URL directly", func() {
			host.ArtworkMock.On("GetTrackUrl", "track1", int32(300)).Return("https://example.com/art.jpg", nil)

			url := getImageURL("testuser", "track1")
			Expect(url).To(Equal("https://example.com/art.jpg"))
		})

		It("returns empty for localhost URL", func() {
			host.ArtworkMock.On("GetTrackUrl", "track1", int32(300)).Return("http://localhost:4533/art.jpg", nil)

			url := getImageURL("testuser", "track1")
			Expect(url).To(BeEmpty())
		})

		It("returns empty when artwork fetch fails", func() {
			host.ArtworkMock.On("GetTrackUrl", "track1", int32(300)).Return("", errors.New("not found"))

			url := getImageURL("testuser", "track1")
			Expect(url).To(BeEmpty())
		})
	})

	Describe("uguu enabled", func() {
		BeforeEach(func() {
			pdk.PDKMock.On("GetConfig", uguuEnabledKey).Return("true", true)
		})

		It("returns cached URL when available", func() {
			host.CacheMock.On("GetString", "uguu.artwork.track1").Return("https://a.uguu.se/cached.jpg", true, nil)

			url := getImageURL("testuser", "track1")
			Expect(url).To(Equal("https://a.uguu.se/cached.jpg"))
		})

		It("uploads artwork and caches the result", func() {
			host.CacheMock.On("GetString", "uguu.artwork.track1").Return("", false, nil)

			// Mock SubsonicAPICallRaw
			imageData := []byte("fake-image-data")
			host.SubsonicAPIMock.On("CallRaw", "/getCoverArt?u=testuser&id=track1&size=300").
				Return("image/jpeg", imageData, nil)

			// Mock uguu.se HTTP upload
			uguuReq := &pdk.HTTPRequest{}
			pdk.PDKMock.On("NewHTTPRequest", pdk.MethodPost, "https://uguu.se/upload").Return(uguuReq)
			pdk.PDKMock.On("Send", uguuReq).Return(pdk.NewStubHTTPResponse(200, nil,
				[]byte(`{"success":true,"files":[{"url":"https://a.uguu.se/uploaded.jpg"}]}`)))

			// Mock cache set
			host.CacheMock.On("SetString", "uguu.artwork.track1", "https://a.uguu.se/uploaded.jpg", int64(9000)).Return(nil)

			url := getImageURL("testuser", "track1")
			Expect(url).To(Equal("https://a.uguu.se/uploaded.jpg"))
			host.CacheMock.AssertCalled(GinkgoT(), "SetString", "uguu.artwork.track1", "https://a.uguu.se/uploaded.jpg", int64(9000))
		})

		It("returns empty when artwork data fetch fails", func() {
			host.CacheMock.On("GetString", "uguu.artwork.track1").Return("", false, nil)
			host.SubsonicAPIMock.On("CallRaw", "/getCoverArt?u=testuser&id=track1&size=300").
				Return("", []byte(nil), errors.New("fetch failed"))

			url := getImageURL("testuser", "track1")
			Expect(url).To(BeEmpty())
		})

		It("returns empty when uguu.se upload fails", func() {
			host.CacheMock.On("GetString", "uguu.artwork.track1").Return("", false, nil)
			host.SubsonicAPIMock.On("CallRaw", "/getCoverArt?u=testuser&id=track1&size=300").
				Return("image/jpeg", []byte("fake-image-data"), nil)

			uguuReq := &pdk.HTTPRequest{}
			pdk.PDKMock.On("NewHTTPRequest", pdk.MethodPost, "https://uguu.se/upload").Return(uguuReq)
			pdk.PDKMock.On("Send", uguuReq).Return(pdk.NewStubHTTPResponse(500, nil, []byte(`{"success":false}`)))

			url := getImageURL("testuser", "track1")
			Expect(url).To(BeEmpty())
		})
	})
})
