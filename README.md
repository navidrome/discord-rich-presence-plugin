# Discord Rich Presence Plugin for Navidrome

[![Build](https://github.com/navidrome/discord-rich-presence-plugin/actions/workflows/build.yml/badge.svg)](https://github.com/navidrome/discord-rich-presence-plugin/actions/workflows/build.yml)
[![Latest](https://img.shields.io/github/v/release/navidrome/discord-rich-presence-plugin)](https://github.com/navidrome/discord-rich-presence-plugin/releases/latest/download/discord-rich-presence.ndp)

**Attention: This plugin requires Navidrome 0.60.2 or later.**

This plugin integrates Navidrome with Discord Rich Presence, displaying your currently playing track in your Discord status. 
The goal is to demonstrate the capabilities of Navidrome's plugin system by implementing a real-time presence feature using Discord's Gateway API.
It demonstrates how a Navidrome plugin can maintain real-time connections to external services while remaining completely stateless. 

Based on the [Navicord](https://github.com/logixism/navicord) project.

**⚠️ WARNING: This plugin requires storing Discord user tokens, which may violate Discord's Terms of Service. Use at your own risk.**

## Features

- Shows currently playing track with title, artist, and album art
- Customizable activity name: "Navidrome" is default, but can be configured to display track title, artist, or album
- Displays playback progress with start/end timestamps
- Automatic presence clearing when track finishes
- Multi-user support with individual Discord tokens
- Optional image hosting via [uguu.se](https://uguu.se) for non-public Navidrome instances

<img height="550" alt="Discord Rich Presence showing currently playing track with album art, artist, and playback progress" src="https://raw.githubusercontent.com/navidrome/discord-rich-presence-plugin/master/.github/screenshot.png">


## Installation

### Step 1: Download and Install the Plugin
1. Download the `discord-rich-presence.ndp` file from the [releases page](https://github.com/navidrome/discord-rich-presence-plugin/releases)
2. Copy it to your Navidrome plugins folder. Default location: `<navidrome-data-directory>/plugins/`

### Step 2: Create a Discord Application
1. Go to the [Discord Developer Portal](https://discord.com/developers/applications)
2. Click "New Application" and give it a name (e.g., "My Navidrome")
3. Note down the **Application ID** (Client ID) - you'll need this for configuration

### Step 3: Get Your Discord User Token
⚠️ **WARNING**: This step involves using your Discord user token, which may violate Discord's Terms of Service. Proceed at your own risk.

We don't provide instructions for obtaining the token as it may violate Discord's policies. You can find guides online by searching for "how to get Discord user token".

### Step 4: Configure the Plugin
1. Open Navidrome and go to **Settings > Plugins > Discord Rich Presence**
2. Fill in the configuration:
   - **Client ID**: Your Discord Application ID from Step 2
    - **Activity Name Display**: Choose what to show as the activity name (Default, Track, Album, Artist)
      - "Default" is recommended to help spread awareness of your favorite music server 😉, but feel free to choose the option that best suits your preferences
   - **Upload to uguu.se**: Enable this if your Navidrome isn't publicly accessible (see Album Art section below)
   - **Users**: Add your Navidrome username and Discord token from Step 3

### Step 5: Enable Discord Activity Sharing
In Discord, ensure your activity is visible to others:
1. Go to **User Settings** (gear icon)
2. Navigate to **Activity Privacy**
3. Enable **"Display current activity as a status message"**

### Step 6: Enable the Plugin
1. In Navidrome's plugin settings, toggle the plugin to **Enabled**
2. No restart required - check Navidrome logs for any initialization errors

## Album Art Display

For album artwork to display in Discord, Discord needs to be able to access the image. Choose one of these options:

### Option 1: Public Navidrome Instance
**Use this if**: Your Navidrome server can be reached from the internet

**Setup**:
1. Set the `ND_BASEURL` environment variable to your public URL:
   ```bash
   # Example for Docker or Docker Compose
   ND_BASEURL=https://music.yourdomain.com
   
   # Example for navidrome.toml
   BaseURL = "https://music.yourdomain.com"
   ```
2. **Restart Navidrome** (required for ND_BASEURL changes)
3. In plugin settings: **Disable** "Upload to uguu.se"

### Option 2: Private Instance with uguu.se Upload
**Use this if**: Your Navidrome is only accessible locally (home network, behind VPN, etc.)

**Setup**:
1. In plugin settings: **Enable** "Upload to uguu.se"
2. No other configuration needed

**How it works**: Album art is automatically uploaded to uguu.se (temporary, anonymous hosting service) so Discord can access it. Files are deleted after 3 hours.

### Backup: Cover Art Archive
**Use this if**: You have your music tagged with MusicBrainz

**Setup**:
1. In plugin settings: **Enable** "Use artwork from Cover Art Archive"
2. No other configuration needed

**How it works**: Cover art is linked directly from MusicBrainz via Release ID using the [Cover Art Archive API](https://musicbrainz.org/doc/Cover_Art_Archive/API). Will fall back to other methods if no artwork is found.

### Troubleshooting Album Art
- **No album art showing**: Check Navidrome logs for errors
- **Using public instance**: Verify ND_BASEURL is correct and Navidrome was restarted
- **Using uguu.se**: Check that the option is enabled and your server has internet access

## Configuration

Access the plugin configuration in Navidrome: **Settings > Plugins > Discord Rich Presence**

### Configuration Fields

#### Client ID
- **What it is**: Your Discord Application ID
- **How to get it**: 
  1. Go to [Discord Developer Portal](https://discord.com/developers/applications)
  2. Create a new application or select an existing one
  3. Copy the "Application ID" from the General Information page
- **Example**: `1234567890123456789`

#### Activity Name Display
- **What it is**: Choose what information to display as the activity name in Discord Rich Presence
- **Options**:
  - **Default**: Shows "Navidrome" (static app name)
  - **Track**: Shows the currently playing track title
  - **Album**: Shows the currently playing track's album name
  - **Artist**: Shows the currently playing track's artist name

#### Use artwork from Cover Art Archive
- **When to enable**: Your Navidrome instance is NOT publicly accessible from the internet, or you don't feel comfortable with directly linking
- **What it does**: Attempts to find and link album artwork with MusicBrainz before using other methods

#### Upload to uguu.se
- **When to enable**: Your Navidrome instance is NOT publicly accessible from the internet
- **What it does**: Automatically uploads album artwork to uguu.se (temporary hosting) so Discord can display it
- **When to disable**: Your Navidrome is publicly accessible and you've set `ND_BASEURL`

#### Users
Add each Navidrome user who wants Discord Rich Presence. For each user, provide:
- **Username**: The Navidrome login username (case-sensitive)
- **Token**: The Discord user token (see Step 3 in Installation for how to obtain this)

## How It Works

### Plugin Capabilities

The plugin implements three Navidrome capabilities:

| Capability            | Purpose                                                                      |
|-----------------------|------------------------------------------------------------------------------|
| **Scrobbler**         | Receives `NowPlaying` events when users start playing tracks                 |
| **WebSocketCallback** | Handles incoming Discord gateway messages (heartbeat ACKs, sequence numbers) |
| **SchedulerCallback** | Processes scheduled events for heartbeats and presence clearing              |

### Host Services

| Service         | Usage                                                               |
|-----------------|---------------------------------------------------------------------|
| **HTTP**        | Discord API calls (gateway discovery, external assets registration) |
| **WebSocket**   | Persistent connection to Discord gateway                            |
| **Cache**       | Sequence numbers, processed image URLs                              |
| **Scheduler**   | Recurring heartbeats, one-time presence clearing                    |
| **Artwork**     | Track artwork public URL resolution                                 |
| **SubsonicAPI** | Fetches track artwork data for image hosting upload                 |

### Flow

1. **Track starts playing** - Navidrome calls `NowPlaying`
2. **Plugin connects** - If not already connected, establishes WebSocket to Discord gateway
3. **Authentication** - Sends identify payload with user's Discord token
4. **Presence update** - Sends activity with track info and processed artwork URL
5. **Heartbeat loop** - Recurring scheduler sends heartbeats every 41 seconds to keep connection alive
6. **Track ends** - One-time scheduler callback clears presence and disconnects

### Stateless Design

Navidrome plugins are stateless - each call creates a fresh instance. This plugin handles that by:

- **WebSocket connections**: Managed by host, keyed by username
- **Sequence numbers**: Stored in cache for heartbeat messages
- **Configuration**: Reloaded on every method call
- **Artwork URLs**: Cached after processing through Discord's external assets API

### Image Processing

Discord requires images to be registered via their external assets API. The plugin:
1. Fetches track artwork URL from Navidrome
2. Registers it with Discord's API to get an `mp:` prefixed URL
3. Caches the result (4 hours for track art, 48 hours for default image)
4. Falls back to a default image if artwork is unavailable

**For non-public Navidrome instances**: If your server isn't publicly accessible (e.g., behind a VPN or firewall), enable the "Upload to uguu.se" option. This uploads artwork to a temporary file host so Discord can display it.

### Files

| File                           | Description                                                            |
|--------------------------------|------------------------------------------------------------------------|
| [main.go](main.go)             | Plugin entry point, scrobbler and scheduler implementations            |
| [rpc.go](rpc.go)               | Discord gateway communication, WebSocket handling, activity management |
| [coverart.go](coverart.go)     | Artwork URL handling and optional uguu.se image hosting                |
| [manifest.json](manifest.json) | Plugin metadata and permission declarations                            |
| [Makefile](Makefile)           | Build automation                                                       |

## Building

### Prerequisites
- **Recommended**: [TinyGo](https://tinygo.org/getting-started/install/) (produces smaller binary size)
- **Alternative**: Standard Go 1.19+ (larger binary but easier setup)

### Quick Build (Using Makefile)
```sh
# Run tests
make test

# Build plugin.wasm
make build

# Create distributable plugin package
make package
```

The `make package` command creates `discord-rich-presence.ndp` containing the compiled WebAssembly module and manifest.

### Manual Build Options

#### Using TinyGo (Recommended)
```sh
# Install TinyGo first: https://tinygo.org/getting-started/install/
tinygo build -target wasip1 -buildmode=c-shared -o plugin.wasm -scheduler=none .
zip discord-rich-presence.ndp plugin.wasm manifest.json
```

#### Using Standard Go
```sh
GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o plugin.wasm .
zip discord-rich-presence.ndp plugin.wasm manifest.json
```

### Output
- `plugin.wasm`: The compiled WebAssembly module
- `discord-rich-presence.ndp`: The complete plugin package ready for installation

