// app.js - Main application entry point
import VideoPlayer from './components/VideoPlayer.js';
import StatusBar from './components/StatusBar.js';
import AntennaPanel from './components/AntennaPanel.js';
import StreamPanel from './components/StreamPanel.js';
import LinkQuality from './components/LinkQuality.js';
import AdaptiveLinkPanel from './components/AdaptiveLinkPanel.js';
import ConfigPanel from './components/ConfigPanel.js';
import ChannelScannerPanel from './components/ChannelScannerPanel.js';

const { createApp, ref, provide, onMounted, onUnmounted } = Vue;

const App = {
    components: {
        VideoPlayer,
        StatusBar,
        AntennaPanel,
        StreamPanel,
        LinkQuality,
        AdaptiveLinkPanel,
        ConfigPanel,
        ChannelScannerPanel,
    },
    setup() {
        const connected = ref(false);
        const stats = ref({});
        const bitrate = ref(0);
        const bitrateNum = ref(0);  // Numeric for graph
        const nalCount = ref(0);
        const antennas = ref([]);
        const streams = ref([]);
        const txWlan = ref(null);
        const adaptiveLink = ref(null);
        const showPanels = ref(true);
        const rawStats = ref(null);

        // Pit mode state
        const pitMode = ref({ enabled: false, loading: false, error: null });

        // Scanner modal state
        const scannerOpen = ref(false);

        // Provide rawStats for child components (e.g., StreamsTab needs streams_running)
        provide('stats', rawStats);

        let statsWs = null;

        function connectStats() {
            statsWs = new WebSocket(`ws://${location.host}/ws/stats`);

            statsWs.onmessage = (e) => {
                try {
                    const s = JSON.parse(e.data);
                    rawStats.value = s;
                    stats.value = {
                        rssi: s.rssi,
                        snr: s.snr,
                        packets: s.packets,
                        fecRecovery: s.fec_recovery,
                        fecLost: s.fec_lost,
                        decErrors: s.dec_errors,
                        txInjected: s.tx_injected,
                        txDropped: s.tx_dropped,
                    };

                    if (s.bitrate_mbps !== undefined) {
                        bitrate.value = s.bitrate_mbps.toFixed(1);
                        bitrateNum.value = s.bitrate_mbps;
                    }

                    // Per-antenna stats
                    if (s.antennas) {
                        antennas.value = s.antennas;
                    }

                    // Per-stream stats
                    if (s.streams) {
                        streams.value = s.streams;
                    }

                    // TX wlan selection
                    if (s.tx_wlan !== undefined && s.tx_wlan !== null) {
                        txWlan.value = s.tx_wlan;
                    }

                    // Adaptive link stats
                    adaptiveLink.value = s.adaptive_link || null;
                } catch (e) {
                    console.error('Stats parse error:', e);
                }
            };

            statsWs.onclose = () => {
                setTimeout(connectStats, 2000);
            };
        }

        function onVideoConnected() {
            connected.value = true;
        }

        function onVideoDisconnected() {
            connected.value = false;
        }

        function togglePanels() {
            showPanels.value = !showPanels.value;
        }

        // Fetch pit mode state
        async function fetchPitMode() {
            try {
                const res = await fetch('/api/pitmode');
                if (res.ok) {
                    const data = await res.json();
                    pitMode.value.enabled = data.enabled;
                    pitMode.value.error = data.error || null;
                }
            } catch (e) {
                console.error('Failed to fetch pit mode:', e);
            }
        }

        // Scanner modal
        function openScanner() {
            scannerOpen.value = true;
        }

        function closeScanner() {
            scannerOpen.value = false;
        }

        // Toggle pit mode
        async function togglePitMode() {
            if (pitMode.value.loading) return;

            const newState = !pitMode.value.enabled;
            const confirmMsg = newState
                ? 'Enable pit mode? This will reduce TX power to minimum on GS and drone.'
                : 'Disable pit mode? This will restore normal TX power.';

            if (!confirm(confirmMsg)) return;

            pitMode.value.loading = true;
            pitMode.value.error = null;

            try {
                const res = await fetch('/api/pitmode', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ enabled: newState })
                });
                const data = await res.json();
                pitMode.value.enabled = data.enabled;
                pitMode.value.error = data.error || null;
            } catch (e) {
                pitMode.value.error = e.message;
            } finally {
                pitMode.value.loading = false;
            }
        }

        onMounted(() => {
            connectStats();
            fetchPitMode();
        });

        onUnmounted(() => {
            if (statsWs) statsWs.close();
        });

        return {
            connected,
            stats,
            bitrate,
            bitrateNum,
            nalCount,
            antennas,
            streams,
            txWlan,
            adaptiveLink,
            showPanels,
            pitMode,
            scannerOpen,
            onVideoConnected,
            onVideoDisconnected,
            togglePanels,
            togglePitMode,
            openScanner,
            closeScanner,
        };
    },
    template: `
        <div class="container">
            <!-- Pit Mode Warning Banner -->
            <div class="pit-mode-banner" v-if="pitMode.enabled">
                <span class="pit-mode-icon">&#9888;</span>
                <span>PIT MODE ACTIVE - TX power reduced to minimum</span>
                <button class="pit-mode-exit" @click="togglePitMode" :disabled="pitMode.loading">
                    {{ pitMode.loading ? 'Restoring...' : 'Exit Pit Mode' }}
                </button>
            </div>

            <VideoPlayer
                :stats="stats"
                @connected="onVideoConnected"
                @disconnected="onVideoDisconnected"
            />

            <StatusBar
                :connected="connected"
                :stats="stats"
                :bitrate="bitrate"
                :nalCount="nalCount"
                :pitMode="pitMode"
                @toggle-pit-mode="togglePitMode"
                @open-scanner="openScanner"
            />

            <div class="panels-toggle" @click="togglePanels">
                {{ showPanels ? 'Hide Details' : 'Show Details' }}
            </div>

            <div class="stats-panels" v-show="showPanels">
                <AdaptiveLinkPanel
                    :adaptiveLink="adaptiveLink"
                />
                <LinkQuality
                    :bitrate="bitrateNum"
                    :stats="stats"
                />
                <AntennaPanel
                    :antennas="antennas"
                    :txWlan="txWlan"
                />
                <StreamPanel
                    :streams="streams"
                />
            </div>

            <ConfigPanel />

            <ChannelScannerPanel
                :isOpen="scannerOpen"
                @close="closeScanner"
            />
        </div>
    `
};

createApp(App).mount('#app');
