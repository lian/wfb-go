# wfb_web - Standalone Web Video Player

Browser-based video player for WFB streams. Receives video via UDP and serves a web interface.

**Note:** The web player has higher latency than native decoders like GStreamer or ffplay. Use this for convenience when latency is not critical.

## Usage

```
wfb_web [options]
```

## Options

| Flag | Description | Default |
|------|-------------|---------|
| `--http` | HTTP server address | `:8080` |
| `--video` | UDP address to receive video | `:5600` |
| `--stats` | TCP address for wfb_server JSON API | (disabled) |

## Example

```bash
# Start web server, receive video on UDP 5600
wfb_web --http :8080 --video :5600

# With stats from wfb_server (shows RSSI, SNR, FEC, etc.)
wfb_web --http :8080 --video :5600 --stats localhost:8103

# Then open http://localhost:8080 in Safari
```

## How It Works

1. Receives H.264/H.265 video via UDP (same format as wfb_rx output)
2. Parses Annex B NAL units from the stream
3. Sends NAL units to connected browsers via WebSocket
4. Browser uses MSE (Media Source Extensions) to decode and display

## Browser Support

| Browser | H.264 | H.265/HEVC |
|---------|-------|------------|
| Safari | Yes | Yes (recommended) |
| Chrome | Yes | Limited |
| Firefox | Yes | No |

**Recommended:** Safari on macOS or iOS for HEVC video.

## Integration with wfb_rx

```bash
# Terminal 1: Start receiver, output to UDP 5600
wfb_rx -K gs.key -c 127.0.0.1 -u 5600 wlan0

# Terminal 2: Start web player
wfb_web --http :8080 --video :5600

# Open browser
open http://localhost:8080
```

## Integration with wfb_server

For wfb_server, use the built-in web UI instead (no UDP needed):

```yaml
# In your config.yaml
web:
  enabled: true
  port: 8080
  video_stream: video
```

This sends video directly from the RX service to the web server without UDP.

## See Also

- [pkg/server/web](../../pkg/server/web/README.md) - Web package documentation
- [pkg/server](../../pkg/server/README.md) - Server integration details
