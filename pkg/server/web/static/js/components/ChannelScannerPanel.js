// ChannelScannerPanel.js - WiFi channel scanner modal for finding clean 5GHz channels
const { ref, computed, onMounted, watch } = Vue;

export default {
    name: 'ChannelScannerPanel',
    props: {
        isOpen: {
            type: Boolean,
            default: false
        }
    },
    emits: ['close'],
    setup(props, { emit }) {
        const scanning = ref(false);
        const results = ref(null);
        const error = ref(null);
        const interfaces = ref([]);
        const selectedInterface = ref('');

        // Load interfaces when modal opens
        async function loadInterfaces() {
            try {
                const res = await fetch('/api/scanner');
                const data = await res.json();
                interfaces.value = data.interfaces || [];
                // Auto-select first managed interface, or first interface
                if (interfaces.value.length > 0 && !selectedInterface.value) {
                    const managed = interfaces.value.find(i => i.type === 'managed');
                    selectedInterface.value = managed ? managed.name : interfaces.value[0].name;
                }
            } catch (e) {
                console.error('Failed to load interfaces:', e);
            }
        }

        // Watch for modal open
        watch(() => props.isOpen, (isOpen) => {
            if (isOpen) {
                loadInterfaces();
            }
        });

        onMounted(() => {
            if (props.isOpen) {
                loadInterfaces();
            }
        });

        // Separate 2.4GHz and 5GHz channels
        const channels5g = computed(() => {
            if (!results.value || !results.value.channels) return [];
            return results.value.channels.filter(ch => ch.channel >= 36);
        });

        const channels24g = computed(() => {
            if (!results.value || !results.value.channels) return [];
            return results.value.channels.filter(ch => ch.channel >= 1 && ch.channel <= 14);
        });

        // Use 5GHz for main display
        const channels = channels5g;

        // Calculate bar height in pixels (absolute scale based on AP count)
        function barHeight(ch) {
            if (ch.count === 0) return '16px';   // Empty: small
            if (ch.count === 1) return '35px';   // 1 AP: medium-low
            if (ch.count === 2) return '50px';   // 2 APs: medium
            if (ch.count === 3) return '60px';   // 3 APs: medium-high
            return '75px';                        // 4+ APs: tall
        }

        // Get congestion class based on network count and signal strength
        function congestionClass(ch) {
            if (ch.count === 0) return 'clear';
            if (ch.count >= 3 || (ch.max_signal && ch.max_signal > -50)) return 'crowded';
            if (ch.count >= 1) return 'moderate';
            return 'clear';
        }

        // Get interface display info
        function interfaceLabel(iface) {
            let label = iface.name;
            if (iface.type) label += ` (${iface.type})`;
            if (iface.channel > 0) label += ` ch${iface.channel}`;
            if (!iface.is_up) label += ' [down]';
            return label;
        }

        async function runScan() {
            if (!selectedInterface.value) {
                error.value = 'Please select an interface';
                return;
            }

            scanning.value = true;
            error.value = null;

            try {
                const res = await fetch('/api/scanner', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ interface: selectedInterface.value })
                });
                const data = await res.json();

                if (data.error) {
                    error.value = data.error;
                } else {
                    results.value = data;
                    console.log('Scan results:', JSON.stringify(data, null, 2));
                }
            } catch (e) {
                error.value = e.message;
            } finally {
                scanning.value = false;
                // Reload interfaces to get updated state
                loadInterfaces();
            }
        }

        function close() {
            emit('close');
        }

        // Count networks per band
        const networks5gCount = computed(() => {
            return channels5g.value.reduce((sum, ch) => sum + ch.count, 0);
        });

        const networks24gCount = computed(() => {
            return channels24g.value.reduce((sum, ch) => sum + ch.count, 0);
        });

        return {
            scanning,
            results,
            error,
            interfaces,
            selectedInterface,
            channels,
            channels24g,
            networks5gCount,
            networks24gCount,
            barHeight,
            congestionClass,
            interfaceLabel,
            runScan,
            close,
        };
    },
    template: `
        <div class="scanner-overlay" v-if="isOpen" @click.self="close">
            <div class="scanner-modal">
                <div class="scanner-header">
                    <h2>Channel Scanner</h2>
                    <button class="scanner-close-btn" @click="close">&times;</button>
                </div>

                <div class="scanner-body">
                    <div class="scan-controls">
                        <select v-model="selectedInterface" class="interface-select" :disabled="scanning">
                            <option value="" disabled>Select interface...</option>
                            <option v-for="iface in interfaces" :key="iface.name" :value="iface.name">
                                {{ interfaceLabel(iface) }}
                            </option>
                        </select>
                        <button @click="runScan" :disabled="scanning || !selectedInterface" class="scan-btn">
                            {{ scanning ? 'Scanning...' : 'Scan' }}
                        </button>
                    </div>

                    <div class="scan-note" v-if="!results && !scanning">
                        Select a WiFi interface and click Scan. If the interface is in monitor mode, it will be
                        temporarily switched to managed mode and restored after scanning.
                    </div>

                    <div class="scan-error" v-if="error">{{ error }}</div>

                    <div class="scan-status" v-if="scanning">
                        <div class="scan-spinner"></div>
                        <span>Scanning nearby networks on {{ selectedInterface }}...</span>
                    </div>

                    <div class="scan-results" v-if="results && !scanning">
                        <!-- Band summary -->
                        <div class="band-summary">
                            <span class="band-count">5 GHz: {{ networks5gCount }} networks</span>
                            <span class="band-count">2.4 GHz: {{ networks24gCount }} networks</span>
                            <span class="scan-info" v-if="results.wlan">| Scanned: {{ results.wlan }}</span>
                        </div>

                        <!-- 5GHz channels -->
                        <div class="band-section">
                            <div class="band-title">5 GHz Channels (for WFB)</div>
                            <div class="channel-chart">
                                <div v-for="ch in channels"
                                     :key="ch.channel"
                                     class="channel-bar"
                                     :class="{ recommended: ch.recommended }"
                                     :title="ch.count + ' network(s)' + (ch.max_signal ? ', max ' + ch.max_signal + ' dBm' : '')">
                                    <div class="bar-fill"
                                         :style="{ height: barHeight(ch) }"
                                         :class="congestionClass(ch)">
                                    </div>
                                    <span class="channel-label">{{ ch.channel }}</span>
                                </div>
                            </div>
                        </div>

                        <!-- 2.4GHz channels (for reference) -->
                        <div class="band-section" v-if="channels24g.length > 0">
                            <div class="band-title">2.4 GHz Channels (reference only)</div>
                            <div class="channel-chart channel-chart-24g">
                                <div v-for="ch in channels24g"
                                     :key="ch.channel"
                                     class="channel-bar"
                                     :title="ch.count + ' network(s)' + (ch.max_signal ? ', max ' + ch.max_signal + ' dBm' : '')">
                                    <div class="bar-fill"
                                         :style="{ height: barHeight(ch) }"
                                         :class="congestionClass(ch)">
                                    </div>
                                    <span class="channel-label">{{ ch.channel }}</span>
                                </div>
                            </div>
                        </div>

                        <div class="scan-legend">
                            <span class="legend-item"><span class="bar-sample clear"></span>Clear</span>
                            <span class="legend-item"><span class="bar-sample moderate"></span>Some</span>
                            <span class="legend-item"><span class="bar-sample crowded"></span>Busy</span>
                        </div>

                    </div>
                </div>
            </div>
        </div>
    `
};
