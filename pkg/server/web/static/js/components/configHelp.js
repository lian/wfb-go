// configHelp.js - Help text definitions for config fields

export const HELP = {
    // Hardware
    channel: {
        text: 'WiFi channel number. 5GHz channels (36-165) recommended. Higher channels often have less interference.',
        default: '165',
        tuning: 'Use 149-165 for least interference. Match drone and GS channels.'
    },
    bandwidth: {
        text: 'Channel width in MHz. 20 MHz is more reliable at range; 40 MHz doubles throughput but reduces range.',
        default: '20',
        tuning: 'Use 20 MHz for long range, 40 MHz only with strong signal.'
    },
    tx_power: {
        text: 'Transmit power in dBm. Higher = more range but more power consumption and heat.',
        default: 'Driver default (usually max)',
        tuning: 'Start low (15-20 dBm), increase if needed. Check adapter limits.'
    },
    mcs: {
        text: 'Modulation and Coding Scheme (0-9). Lower values = more robust but slower. Higher = faster but less reliable.',
        default: '1',
        tuning: 'MCS 1-3 for video. Use adaptive link for auto-adjustment.'
    },
    region: {
        text: 'Regulatory region code. Affects available channels and TX power limits.',
        default: 'BO',
        tuning: 'Use "BO" (Bolivia) for maximum flexibility. May not be legal everywhere.'
    },
    stbc: {
        text: 'Space-Time Block Coding. Improves reliability with multiple TX antennas by sending redundant copies.',
        default: '1 (Enabled)',
        tuning: 'Enable if adapter supports it. Helps with multipath interference.'
    },
    ldpc: {
        text: 'Low-Density Parity-Check coding. Better error correction than standard BCC coding.',
        default: '1 (Enabled)',
        tuning: 'Enable if adapter supports it. Slight throughput improvement.'
    },
    short_gi: {
        text: 'Short Guard Interval. Reduces gap between symbols for ~10% more throughput, but less robust.',
        default: 'Off',
        tuning: 'Only enable with very good signal conditions.'
    },

    // Link
    domain: {
        text: 'Link domain string, hashed to create link_id. All nodes must use the same domain.',
        default: 'default',
        tuning: 'Use a unique domain for each link to avoid interference.'
    },
    link_id: {
        text: 'Numeric link ID (24-bit). Overrides domain if set non-zero. Use for explicit control.',
        default: '0 (use domain hash)',
        tuning: 'Usually leave at 0 to use domain-based ID.'
    },
    key_base64: {
        text: 'Encryption key for the link in Base64 format. All nodes must use the same key.',
        default: 'None (unencrypted)',
        tuning: 'Generate with wfb-keygen. 64 bytes: 32-byte secret + 32-byte peer public key.'
    },

    // Web Server
    web_enabled: {
        text: 'Enable the web UI and video streaming server.',
        default: 'Yes',
        tuning: 'Disable if running headless or using external monitoring.'
    },
    web_port: {
        text: 'HTTP port for web UI and WebSocket connections.',
        default: '8080',
        tuning: 'Change if port conflicts with other services.'
    },
    video_stream: {
        text: 'Name of the stream to display in the web video player.',
        default: 'video',
        tuning: 'Must match a configured stream name with video data.'
    },

    // Stats API
    api_enabled: {
        text: 'Enable external stats API servers (JSON and/or MsgPack).',
        default: 'No',
        tuning: 'Enable for external monitoring tools like QGroundControl.'
    },
    json_port: {
        text: 'TCP port for JSON stats API. Provides human-readable stats output.',
        default: '0 (disabled)',
        tuning: 'Enable for external tools. Common ports: 8001, 8080.'
    },
    stats_port: {
        text: 'TCP port for MsgPack stats API. Compatible with wfb-ng ecosystem.',
        default: '0 (disabled)',
        tuning: 'Use 8003 for wfb-ng compatibility.'
    },
    log_interval: {
        text: 'Interval between stats log messages in milliseconds.',
        default: '1000',
        tuning: 'Lower = more frequent updates. 500-2000ms typical.'
    },

    // Common/Advanced
    debug: {
        text: 'Enable verbose debug logging. Outputs detailed packet and state information.',
        default: 'Off',
        tuning: 'Enable for troubleshooting. Increases log output significantly.'
    },
    radio_mtu: {
        text: 'Maximum transmission unit for radio packets. Larger = more efficient but higher latency.',
        default: '1445',
        tuning: 'Usually no need to change. Must match all nodes in the link.'
    },
    tx_sel_rssi_delta: {
        text: 'RSSI difference threshold for TX antenna selection. Higher = less switching between antennas.',
        default: '3',
        tuning: 'Lower = more aggressive switching. 2-5 dB typical.'
    },
    tx_rcv_buf_size: {
        text: 'Socket receive buffer size for TX path. Larger buffers handle bursts better.',
        default: '65536',
        tuning: 'Increase for high bitrate video. 262144 for 4K.'
    },
    rx_snd_buf_size: {
        text: 'Socket send buffer size for RX path. Larger buffers handle bursts better.',
        default: '65536',
        tuning: 'Increase if seeing dropped packets on output.'
    },
    tunnel_agg_timeout: {
        text: 'Time to wait for more tunnel packets before sending. Lower = less latency, higher = more efficient.',
        default: '0.005',
        tuning: 'For telemetry: 0.001-0.005s. Increase for throughput.'
    },
    mavlink_agg_timeout: {
        text: 'Time to wait for more MAVLink packets before sending. Aggregates small messages.',
        default: '0.03',
        tuning: 'Lower for responsive telemetry. 0.01-0.05s typical.'
    },

    // Streams
    stream_rx: {
        text: 'Radio stream ID for receiving data. Must match the TX stream ID on the sender.',
        default: 'Depends on service',
        tuning: 'Video typically uses 0, telemetry uses 1-2.'
    },
    tunnel_ifname: {
        text: 'TUN interface name for the tunnel. Creates a virtual network interface.',
        default: 'wfb-tunnel',
        tuning: 'Use unique names if running multiple tunnels.'
    },
    tunnel_ifaddr: {
        text: 'IP address and subnet for the tunnel interface in CIDR notation.',
        default: '10.5.0.1/24',
        tuning: 'GS typically .1, drone typically .10. Use /24 for point-to-point.'
    },
    tunnel_default_route: {
        text: 'Add default route through the tunnel. Routes all traffic through the link.',
        default: 'No',
        tuning: 'Enable on drone to route internet through GS. Usually off on GS.'
    },
    stream_tx: {
        text: 'Radio stream ID for transmitting data. Must match the RX stream ID on the receiver.',
        default: 'Depends on service',
        tuning: 'Match with receiving end configuration.'
    },
    peer: {
        text: 'Address to connect to or listen on. Format depends on service type.',
        default: 'Service dependent',
        tuning: 'UDP: "ip:port" or ":port" for listen. TCP: "ip:port".'
    },
    fec: {
        text: 'Forward Error Correction ratio (k/n). k = data packets, n = total packets. More FEC = more recovery but lower throughput.',
        default: '8/12',
        tuning: 'Video: 8/12 to 4/12. Telemetry: 1/2. Lower k/n ratio = more protection.'
    },

    // Adaptive Link - Basic
    adaptive_enabled: {
        text: 'Enable adaptive link control. Automatically adjusts TX parameters based on link quality.',
        default: 'Off',
        tuning: 'Enable for dynamic MCS/FEC/bitrate adjustment based on conditions.'
    },
    send_addr: {
        text: 'Address to send adaptive link messages to (GS mode). Usually drone\'s tunnel IP.',
        default: 'None',
        tuning: 'Format: "10.5.0.10:9999". Use tunnel or direct IP.'
    },
    listen_port: {
        text: 'UDP port to receive adaptive link messages on (drone mode).',
        default: '9999',
        tuning: 'Must match send_addr port on GS.'
    },

    // Adaptive Link - Score
    snr_weight: {
        text: 'Weight of SNR in link quality score calculation. Higher = SNR matters more.',
        default: '0.7',
        tuning: 'SNR is usually more reliable than RSSI. 0.6-0.8 typical.'
    },
    rssi_weight: {
        text: 'Weight of RSSI in link quality score calculation. Higher = RSSI matters more.',
        default: '0.3',
        tuning: 'RSSI can be noisy. 0.2-0.4 typical.'
    },
    snr_min: {
        text: 'SNR value mapped to score 0. Below this = worst quality.',
        default: '5',
        tuning: 'Set to the lowest usable SNR for your setup.'
    },
    snr_max: {
        text: 'SNR value mapped to score 100. Above this = best quality.',
        default: '35',
        tuning: 'Set to a typical good SNR value for your setup.'
    },
    rssi_min: {
        text: 'RSSI value mapped to score 0 (usually negative dBm). Below this = worst quality.',
        default: '-85',
        tuning: 'Set to the lowest usable RSSI for your setup.'
    },
    rssi_max: {
        text: 'RSSI value mapped to score 100 (usually negative dBm). Above this = best quality.',
        default: '-30',
        tuning: 'Set to a typical good RSSI value for your setup.'
    },

    // Adaptive Link - Kalman
    kalman_estimate: {
        text: 'Initial estimate for Kalman filter. Starting point for score smoothing.',
        default: '0.005',
        tuning: 'Usually leave at default. Low value = conservative start.'
    },
    kalman_error: {
        text: 'Initial error estimate for Kalman filter. Uncertainty in starting estimate.',
        default: '0.1',
        tuning: 'Higher = adapts faster initially.'
    },
    kalman_process: {
        text: 'Process variance for Kalman filter. How much the true score can change between measurements.',
        default: '1e-5',
        tuning: 'Lower = smoother but slower response. 1e-6 to 1e-4.'
    },
    kalman_measurement: {
        text: 'Measurement variance for Kalman filter. How noisy the measurements are.',
        default: '0.01',
        tuning: 'Higher = trusts measurements less. 0.001 to 0.1.'
    },

    // Adaptive Link - Timing
    fallback_timeout: {
        text: 'Time without GS stats before entering fallback mode (lowest MCS, highest FEC).',
        default: '1s',
        tuning: 'Shorter = faster recovery but more sensitive. 500ms-2s typical.'
    },
    fallback_hold: {
        text: 'Minimum time to stay in fallback mode before returning to normal operation.',
        default: '1s',
        tuning: 'Prevents rapid oscillation. 1-3s typical.'
    },
    hold_up: {
        text: 'Minimum time to hold at a profile before allowing upgrade to better profile.',
        default: '3s',
        tuning: 'Longer = more stable but slower adaptation. 2-5s typical.'
    },
    min_between_changes: {
        text: 'Minimum time between any profile changes. Prevents rapid switching.',
        default: '200ms',
        tuning: 'Lower = more responsive. 100-500ms typical.'
    },

    // Adaptive Link - Smoothing
    smoothing: {
        text: 'Score smoothing factor for upward changes (0-1). Lower = more responsive to improvements.',
        default: '0.1',
        tuning: 'Lower values react faster. 0.05-0.2 typical.'
    },
    smoothing_down: {
        text: 'Score smoothing factor for downward changes (0-1). Lower = faster response to degradation.',
        default: '0.3',
        tuning: 'Usually higher than smoothing to react faster to problems.'
    },
    hysteresis: {
        text: 'Score difference required to upgrade profile. Prevents rapid oscillation.',
        default: '5',
        tuning: 'Higher = more stable but slower to improve. 3-10 typical.'
    },
    hysteresis_down: {
        text: 'Score difference required to downgrade profile. Usually lower than hysteresis.',
        default: '3',
        tuning: 'Lower = faster response to degradation. 2-5 typical.'
    },

    // Adaptive Link - Keyframe
    allow_keyframe: {
        text: 'Allow sending keyframe requests to encoder when link quality changes.',
        default: 'Yes',
        tuning: 'Enable for faster video recovery after profile changes.'
    },
    keyframe_interval: {
        text: 'Minimum interval between keyframe requests.',
        default: '1112ms',
        tuning: 'Lower = faster recovery but more bandwidth. 500-2000ms.'
    },
    idr_on_change: {
        text: 'Request IDR frame immediately when profile changes.',
        default: 'Yes',
        tuning: 'Enable for immediate video update on profile change.'
    },

    // Adaptive Link - TX Drop
    tx_drop_keyframe: {
        text: 'Request keyframe when TX drops are detected. Helps recover video faster.',
        default: 'Yes',
        tuning: 'Enable unless bandwidth is extremely limited.'
    },
    tx_drop_reduce_bitrate: {
        text: 'Temporarily reduce bitrate when TX drops detected. Prevents congestion spiral.',
        default: 'Yes',
        tuning: 'Enable for congestion-prone links.'
    },
    tx_drop_check_interval: {
        text: 'Interval to check for TX drops.',
        default: '2250ms',
        tuning: 'Lower = faster response but more CPU. 1-5s typical.'
    },
    tx_drop_bitrate_factor: {
        text: 'Factor to multiply bitrate by when drops detected (0-1). Lower = more aggressive reduction.',
        default: '0.8',
        tuning: '0.7-0.9 typical. Lower for congested links.'
    },

    // Adaptive Link - Dynamic FEC
    dynamic_fec: {
        text: 'Dynamically adjust FEC based on packet loss. Adds protection when needed.',
        default: 'No',
        tuning: 'Enable for varying link conditions. May increase latency.'
    },
    fec_k_adjust: {
        text: 'Allow adjusting FEC K (data packets) in addition to N. More fine-grained control.',
        default: 'No',
        tuning: 'Enable for more adaptation options.'
    },
    spike_fix: {
        text: 'Disable dynamic FEC when bitrate is low (≤4000 kbps). Prevents FEC overhead from consuming too much of limited bandwidth.',
        default: 'No',
        tuning: 'Enable if using low bitrate profiles to ensure bandwidth is used for video data.'
    },
    allow_spike_fps: {
        text: 'Allow reducing FPS during bitrate spikes to help the encoder cope. Temporarily lowers frame rate on high resolution streams.',
        default: 'No',
        tuning: 'Enable for high-resolution streams (1080p+) where encoder may struggle with bitrate limits.'
    },

    // Adaptive Link - Power Control
    allow_set_power: {
        text: 'Enable adaptive TX power control. Allows the system to adjust transmit power based on link conditions and profile settings.',
        default: 'Yes',
        tuning: 'Enable for power-efficient operation. Disable if you want fixed TX power.'
    },
    use_04_txpower: {
        text: 'Use card-specific power tables for accurate TX power control. Loads power calibration data from wfb.yaml based on your WiFi card model.',
        default: 'Yes',
        tuning: 'Enable for cards with power tables defined. Uses 0-4 power scale that maps to actual driver values per MCS rate.'
    },
    power_level_04: {
        text: 'TX power level on 0-4 scale. 0 = pit mode (minimum power for bench testing), 4 = maximum power. The actual driver power depends on MCS rate and card-specific tables.',
        default: '0',
        tuning: '0 for bench/pit, 2-3 for normal flying, 4 for long range. Power scales with MCS for optimal efficiency.'
    },

    // Adaptive Link - Video Quality
    roi_focus_mode: {
        text: 'Region of Interest focus mode. Allocates more bitrate to the center of the image for higher quality where pilots typically look.',
        default: 'No',
        tuning: 'Enable for FPV flying. The center gets higher quality while edges are slightly compressed.'
    },
    osd_level: {
        text: 'On-Screen Display verbosity level. Controls how much adaptive link information is shown on the video OSD.',
        default: '0 (Off)',
        tuning: '0=off, 1-3=increasing detail, 4=all on one line, 5-6=extended multi-line display.'
    },

    // Adaptive Link - Profiles
    profiles: {
        text: 'TX profiles define radio parameters for different link quality ranges. System selects profile matching current score.',
        default: 'Empty (use hardware defaults)',
        tuning: 'Create from worst (low score, robust) to best (high score, fast). Ranges should not overlap.'
    },

    // Camera Settings (majestic)
    camera_bitrate: {
        text: 'Video encoder bitrate in kilobits per second. Higher values improve quality but require more bandwidth.',
        default: '8000',
        tuning: 'Match to your link capacity. Start with 4000-8000 for typical FPV setups.'
    },
    camera_gop: {
        text: 'Group of Pictures interval in seconds. Lower values recover faster from packet loss but use more bandwidth.',
        default: '1',
        tuning: 'Use 0.5-1s for FPV. Lower (0.25-0.5) for unreliable links, higher (1-2) for stable links.'
    },
    camera_fps: {
        text: 'Frames per second. Higher FPS is smoother but requires more bitrate.',
        default: '60',
        tuning: '60 FPS is standard for FPV. 90/120 for racing if bandwidth allows.'
    },
    camera_codec: {
        text: 'Video codec. H.265 is more efficient but requires more processing power.',
        default: 'h265',
        tuning: 'Use H.265 for better compression. Use H.264 for compatibility or lower latency.'
    },
    camera_size: {
        text: 'Video resolution. Higher resolution requires more bitrate.',
        default: '1920x1080',
        tuning: '1080p is standard. Lower for bandwidth-limited setups.'
    },
    camera_rc_mode: {
        text: 'Rate control mode. CBR maintains constant bitrate, VBR varies based on scene complexity.',
        default: 'cbr',
        tuning: 'Use CBR for predictable bandwidth usage. VBR may improve quality for static scenes.'
    },
    camera_qp_delta: {
        text: 'Quantization parameter delta. Negative values improve quality, positive values reduce bitrate.',
        default: '0',
        tuning: 'Use -5 to -15 for better quality at same bitrate. Trade-off is encoder load.'
    },
    camera_mirror: {
        text: 'Mirror the video horizontally.',
        default: 'Off',
        tuning: 'Enable if camera is mounted inverted horizontally.'
    },
    camera_flip: {
        text: 'Flip the video vertically.',
        default: 'Off',
        tuning: 'Enable if camera is mounted upside down.'
    }
};
