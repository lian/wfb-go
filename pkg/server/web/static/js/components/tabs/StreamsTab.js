// StreamsTab.js - Streams management tab (editable)
import HelpTooltip from '../HelpTooltip.js';

const { ref, computed, inject } = Vue;

export default {
    name: 'StreamsTab',
    components: { HelpTooltip },
    props: {
        config: { type: Object, required: true },
        activeDevice: { type: String, default: 'gs' },
        droneAddr: { type: String, default: '' },
        droneConnected: { type: Boolean, default: false },
        help: { type: Object, required: true }
    },
    setup(props) {
        const actionLoading = ref({});
        const error = ref(null);

        // Get stats from app (includes streams_running)
        const stats = inject('stats', ref(null));

        // Compute streams from config with running status from stats
        const streams = computed(() => {
            if (!props.config.streams) return [];

            const running = stats.value?.streams_running || {};

            return Object.entries(props.config.streams)
                .map(([name, cfg]) => {
                    // Fill in defaults for TX params if not set
                    if (cfg.stream_tx !== undefined && cfg.stream_tx !== null) {
                        if (cfg.stbc === undefined) cfg.stbc = 1;
                        if (cfg.ldpc === undefined) cfg.ldpc = 1;
                        if (cfg.short_gi === undefined) cfg.short_gi = false;
                    }
                    // Fill in defaults for tunnel params
                    if (cfg.tunnel) {
                        if (cfg.tunnel.default_route === undefined) cfg.tunnel.default_route = false;
                    }
                    return {
                        name,
                        running: running[name] || false,
                        ...cfg
                    };
                })
                .sort((a, b) => a.name.localeCompare(b.name));
        });

        function getBaseUrl() {
            if (props.activeDevice === 'drone') {
                return '/api/drone/streams';
            }
            return '/api/streams';
        }

        function getAddrParam() {
            if (props.activeDevice === 'drone' && props.droneAddr) {
                return '?addr=' + encodeURIComponent(props.droneAddr);
            }
            return '';
        }

        async function startStream(name) {
            actionLoading.value[name] = true;
            error.value = null;
            try {
                const url = `${getBaseUrl()}/${encodeURIComponent(name)}/start${getAddrParam()}`;
                const resp = await fetch(url, { method: 'POST' });
                const result = await resp.json();
                if (!resp.ok) {
                    throw new Error(result.error || `HTTP ${resp.status}`);
                }
            } catch (e) {
                error.value = 'Failed to start stream: ' + e.message;
            } finally {
                actionLoading.value[name] = false;
            }
        }

        async function stopStream(name) {
            actionLoading.value[name] = true;
            error.value = null;
            try {
                const url = `${getBaseUrl()}/${encodeURIComponent(name)}/stop${getAddrParam()}`;
                const resp = await fetch(url, { method: 'POST' });
                const result = await resp.json();
                if (!resp.ok) {
                    throw new Error(result.error || `HTTP ${resp.status}`);
                }
            } catch (e) {
                error.value = 'Failed to stop stream: ' + e.message;
            } finally {
                actionLoading.value[name] = false;
            }
        }

        return {
            streams,
            error,
            actionLoading,
            startStream,
            stopStream
        };
    },
    template: `
        <div class="config-section">
            <div v-if="error" class="config-error" style="margin-bottom: 10px;">{{ error }}</div>

            <!-- Drone not connected message -->
            <div v-if="activeDevice === 'drone' && !droneConnected" class="config-info">
                Connect to drone to view and manage streams.
            </div>

            <div v-else-if="streams.length > 0" class="stream-list-config">
                <div v-for="stream in streams" :key="stream.name" class="stream-config-item">
                    <div class="stream-config-header">
                        <span class="stream-name">{{ stream.name }}</span>
                        <span class="stream-type-badge">{{ stream.service_type }}</span>
                        <span :class="['stream-status-badge', stream.running ? 'running' : 'stopped']">
                            {{ stream.running ? 'Running' : 'Stopped' }}
                        </span>
                        <div class="stream-actions">
                            <button
                                v-if="!stream.running"
                                class="stream-action-btn start"
                                @click="startStream(stream.name)"
                                :disabled="actionLoading[stream.name]"
                            >
                                {{ actionLoading[stream.name] ? '...' : 'Start' }}
                            </button>
                            <button
                                v-else
                                class="stream-action-btn stop"
                                @click="stopStream(stream.name)"
                                :disabled="actionLoading[stream.name]"
                            >
                                {{ actionLoading[stream.name] ? '...' : 'Stop' }}
                            </button>
                        </div>
                    </div>
                    <div class="stream-config-grid">
                        <div class="config-field" v-if="stream.stream_rx !== undefined && stream.stream_rx !== null">
                            <label>
                                RX Stream ID
                                <HelpTooltip :text="help.stream_rx.text" :default-value="help.stream_rx.default" :tuning="help.stream_rx.tuning" />
                            </label>
                            <input type="number" v-model.number="config.streams[stream.name].stream_rx" min="0" max="255">
                        </div>
                        <div class="config-field" v-if="stream.stream_tx !== undefined && stream.stream_tx !== null">
                            <label>
                                TX Stream ID
                                <HelpTooltip :text="help.stream_tx.text" :default-value="help.stream_tx.default" :tuning="help.stream_tx.tuning" />
                            </label>
                            <input type="number" v-model.number="config.streams[stream.name].stream_tx" min="0" max="255">
                        </div>
                        <div class="config-field wide" v-if="stream.peer !== undefined && stream.service_type !== 'tunnel'">
                            <label>
                                Peer
                                <HelpTooltip :text="help.peer.text" :default-value="help.peer.default" :tuning="help.peer.tuning" />
                            </label>
                            <input type="text" v-model="config.streams[stream.name].peer">
                        </div>
                        <!-- Tunnel-specific fields -->
                        <div class="config-field" v-if="stream.tunnel">
                            <label>
                                Interface Name
                                <HelpTooltip :text="help.tunnel_ifname.text" :default-value="help.tunnel_ifname.default" :tuning="help.tunnel_ifname.tuning" />
                            </label>
                            <input type="text" v-model="config.streams[stream.name].tunnel.ifname" placeholder="wfb-tunnel">
                        </div>
                        <div class="config-field" v-if="stream.tunnel">
                            <label>
                                Interface Address
                                <HelpTooltip :text="help.tunnel_ifaddr.text" :default-value="help.tunnel_ifaddr.default" :tuning="help.tunnel_ifaddr.tuning" />
                            </label>
                            <input type="text" v-model="config.streams[stream.name].tunnel.ifaddr" placeholder="10.5.0.1/24">
                        </div>
                        <div class="config-field" v-if="stream.tunnel">
                            <label>
                                Default Route
                                <HelpTooltip :text="help.tunnel_default_route.text" :default-value="help.tunnel_default_route.default" :tuning="help.tunnel_default_route.tuning" />
                            </label>
                            <select v-model="config.streams[stream.name].tunnel.default_route">
                                <option :value="false">No</option>
                                <option :value="true">Yes</option>
                            </select>
                        </div>
                    </div>
                    <!-- TX parameters (only show for services with TX capability) -->
                    <div v-if="stream.stream_tx !== undefined && stream.stream_tx !== null" class="stream-tx-params">
                        <div class="stream-tx-label">TX Radio Parameters</div>
                        <div class="stream-config-grid">
                            <div class="config-field" v-if="stream.fec">
                                <label>
                                    FEC (k/n)
                                    <HelpTooltip :text="help.fec.text" :default-value="help.fec.default" :tuning="help.fec.tuning" />
                                </label>
                                <div class="fec-inputs">
                                    <input type="number" v-model.number="config.streams[stream.name].fec[0]" min="1" max="255" class="fec-input">
                                    <span>/</span>
                                    <input type="number" v-model.number="config.streams[stream.name].fec[1]" min="1" max="255" class="fec-input">
                                </div>
                            </div>
                            <div class="config-field">
                                <label>
                                    MCS
                                    <HelpTooltip :text="help.mcs.text" :default-value="help.mcs.default" :tuning="help.mcs.tuning" />
                                </label>
                                <input type="number" v-model.number="config.streams[stream.name].mcs" min="0" max="9" placeholder="1">
                            </div>
                            <div class="config-field">
                                <label>
                                    STBC
                                    <HelpTooltip :text="help.stbc.text" :default-value="help.stbc.default" :tuning="help.stbc.tuning" />
                                </label>
                                <select v-model.number="config.streams[stream.name].stbc">
                                    <option :value="1">On</option>
                                    <option :value="0">Off</option>
                                </select>
                            </div>
                            <div class="config-field">
                                <label>
                                    LDPC
                                    <HelpTooltip :text="help.ldpc.text" :default-value="help.ldpc.default" :tuning="help.ldpc.tuning" />
                                </label>
                                <select v-model.number="config.streams[stream.name].ldpc">
                                    <option :value="1">On</option>
                                    <option :value="0">Off</option>
                                </select>
                            </div>
                            <div class="config-field">
                                <label>
                                    Short GI
                                    <HelpTooltip :text="help.short_gi.text" :default-value="help.short_gi.default" :tuning="help.short_gi.tuning" />
                                </label>
                                <select v-model="config.streams[stream.name].short_gi">
                                    <option :value="true">On</option>
                                    <option :value="false">Off</option>
                                </select>
                            </div>
                        </div>
                    </div>
                </div>
            </div>

            <div v-else class="config-info">
                No streams configured.
            </div>
        </div>
    `
};
