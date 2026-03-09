// app.js - Main application entry point
import VideoPlayer from './components/VideoPlayer.js';
import StatusBar from './components/StatusBar.js';
import AntennaPanel from './components/AntennaPanel.js';
import StreamPanel from './components/StreamPanel.js';
import BitrateGraph from './components/BitrateGraph.js';
// import LinkQuality from './components/LinkQuality.js';  // TODO: no real latency measurement available
import AdaptiveLinkPanel from './components/AdaptiveLinkPanel.js';
import ConfigPanel from './components/ConfigPanel.js';

const { createApp, ref, provide, onMounted, onUnmounted } = Vue;

const App = {
    components: {
        VideoPlayer,
        StatusBar,
        AntennaPanel,
        StreamPanel,
        BitrateGraph,
        // LinkQuality,
        AdaptiveLinkPanel,
        ConfigPanel,
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

        onMounted(() => {
            connectStats();
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
            onVideoConnected,
            onVideoDisconnected,
            togglePanels,
        };
    },
    template: `
        <div class="container">
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
            />

            <div class="panels-toggle" @click="togglePanels">
                {{ showPanels ? 'Hide Details' : 'Show Details' }}
            </div>

            <div class="stats-panels" v-show="showPanels">
                <AdaptiveLinkPanel
                    :adaptiveLink="adaptiveLink"
                />
                <!-- LinkQuality disabled - no real latency measurement available
                <LinkQuality
                    :stats="stats"
                />
                -->
                <BitrateGraph
                    :bitrate="bitrateNum"
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
        </div>
    `
};

createApp(App).mount('#app');
