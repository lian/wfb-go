// LinkQuality.js - Link quality indicator with visual gauge
const { computed } = Vue;

export default {
    name: 'LinkQuality',
    props: {
        stats: {
            type: Object,
            default: () => ({})
        },
        latency: {
            type: Number,
            default: 0
        }
    },
    setup(props) {
        // Calculate link quality score (0-100)
        const qualityScore = computed(() => {
            const s = props.stats;
            if (!s || !s.rssi) return 0;

            let score = 100;

            // RSSI contribution (0-40 points)
            // -30 dBm = excellent, -80 dBm = poor
            const rssi = s.rssi || -80;
            const rssiScore = Math.max(0, Math.min(40, (rssi + 80) * 0.8));
            score = rssiScore;

            // SNR contribution (0-30 points)
            // 30+ dB = excellent, 10 dB = poor
            const snr = s.snr || 0;
            const snrScore = Math.max(0, Math.min(30, snr));
            score += snrScore;

            // FEC penalty (up to -20 points)
            const fecLost = s.fecLost || 0;
            const fecPenalty = Math.min(20, fecLost * 2);
            score -= fecPenalty;

            // Decode errors penalty (up to -10 points)
            const decErrors = s.decErrors || 0;
            const decPenalty = Math.min(10, decErrors * 2);
            score -= decPenalty;

            return Math.max(0, Math.min(100, Math.round(score)));
        });

        const qualityLabel = computed(() => {
            const q = qualityScore.value;
            if (q >= 80) return 'Excellent';
            if (q >= 60) return 'Good';
            if (q >= 40) return 'Fair';
            if (q >= 20) return 'Poor';
            return 'Critical';
        });

        const qualityClass = computed(() => {
            const q = qualityScore.value;
            if (q >= 80) return 'quality-excellent';
            if (q >= 60) return 'quality-good';
            if (q >= 40) return 'quality-fair';
            if (q >= 20) return 'quality-poor';
            return 'quality-critical';
        });

        const qualityColor = computed(() => {
            const q = qualityScore.value;
            if (q >= 80) return '#4ade80';  // green
            if (q >= 60) return '#a3e635';  // lime
            if (q >= 40) return '#fbbf24';  // amber
            if (q >= 20) return '#f97316';  // orange
            return '#ef4444';  // red
        });

        // Generate bars for the quality meter
        const bars = computed(() => {
            const total = 10;
            const filled = Math.round(qualityScore.value / 10);
            return Array.from({ length: total }, (_, i) => ({
                filled: i < filled,
                index: i
            }));
        });

        const latencyDisplay = computed(() => {
            if (!props.latency) return '--';
            return props.latency.toFixed(1);
        });

        const latencyClass = computed(() => {
            const lat = props.latency;
            if (!lat) return '';
            if (lat < 50) return 'good';
            if (lat < 100) return 'warn';
            return 'bad';
        });

        return {
            qualityScore,
            qualityLabel,
            qualityClass,
            qualityColor,
            bars,
            latencyDisplay,
            latencyClass,
        };
    },
    template: `
        <div class="panel link-quality-panel">
            <div class="panel-title">
                <span>Link Quality</span>
            </div>
            <div class="link-quality-content">
                <div class="quality-gauge">
                    <div class="quality-bars">
                        <div
                            v-for="bar in bars"
                            :key="bar.index"
                            class="quality-bar"
                            :class="{ filled: bar.filled }"
                            :style="{ backgroundColor: bar.filled ? qualityColor : '' }"
                        ></div>
                    </div>
                    <div class="quality-info">
                        <span class="quality-score" :style="{ color: qualityColor }">{{ qualityScore }}%</span>
                        <span class="quality-label" :class="qualityClass">{{ qualityLabel }}</span>
                    </div>
                </div>
                <div class="latency-display">
                    <span class="latency-label">Latency</span>
                    <span class="latency-value" :class="latencyClass">{{ latencyDisplay }} ms</span>
                </div>
            </div>
        </div>
    `
};
