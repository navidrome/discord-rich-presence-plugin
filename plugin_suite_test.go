package main

import (
	"strings"
	"testing"

	"github.com/navidrome/navidrome/plugins/pdk/go/host"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"
)

func TestDiscordPlugin(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Discord Plugin Main Suite")
}

// Shared matchers for tighter mock expectations across all test files.
var (
	discordImageKey   = mock.MatchedBy(func(key string) bool { return strings.HasPrefix(key, "discord.image.") })
	externalAssetsReq = mock.MatchedBy(func(req host.HTTPRequest) bool { return strings.Contains(req.URL, "external-assets") })
	spotifyURLKey     = mock.MatchedBy(func(key string) bool { return strings.HasPrefix(key, "spotify.url.") })
)
