// AdaptiveLinkPanel.js - Adaptive link status display
const { computed } = Vue;

export default {
    name: 'AdaptiveLinkPanel',
    props: {
        adaptiveLink: {
            type: Object,
            default: null
        }
    },
    setup(props) {
        const isEnabled = computed(() => props.adaptiveLink != null);

        const mode = computed(() => props.adaptiveLink?.mode || 'unknown');

        const modeLabel = computed(() => {
            switch (props.adaptiveLink?.mode) {
                case 'gs': return 'Ground Station';
                case 'drone': return 'Drone';
                default: return 'Unknown';
            }
        });

        const score = computed(() => {
            const s = props.adaptiveLink?.score;
            if (!s && s !== 0) return null;
            return Math.round(s);
        });

        const scorePercent = computed(() => {
            // Score range is 1000-2000
            const s = props.adaptiveLink?.score || 1000;
            return Math.max(0, Math.min(100, ((s - 1000) / 1000) * 100));
        });

        const scoreColor = computed(() => {
            const pct = scorePercent.value;
            if (pct >= 70) return '#4ade80';
            if (pct >= 40) return '#fbbf24';
            return '#f87171';
        });

        const scoreClass = computed(() => {
            const pct = scorePercent.value;
            if (pct >= 70) return 'good';
            if (pct >= 40) return 'warn';
            return 'bad';
        });

        const statusLabel = computed(() => {
            if (props.adaptiveLink?.paused) return 'Paused';
            if (props.adaptiveLink?.in_fallback) return 'Fallback';
            return 'Active';
        });

        const statusClass = computed(() => {
            if (props.adaptiveLink?.paused) return 'status-paused';
            if (props.adaptiveLink?.in_fallback) return 'status-fallback';
            return 'status-active';
        });

        const hasProfile = computed(() => {
            return props.adaptiveLink?.profile_mcs !== undefined;
        });

        const profileFec = computed(() => {
            const k = props.adaptiveLink?.profile_fec_k;
            const n = props.adaptiveLink?.profile_fec_n;
            if (k && n) return `${k}/${n}`;
            return '--';
        });

        const profileBitrate = computed(() => {
            const b = props.adaptiveLink?.profile_bitrate;
            if (!b) return '--';
            return (b / 1000).toFixed(1);
        });

        const profileRange = computed(() => {
            const min = props.adaptiveLink?.profile_range_min;
            const max = props.adaptiveLink?.profile_range_max;
            if (min && max) return `${min}-${max}`;
            return '--';
        });

        const noisePercent = computed(() => {
            // Noise typically 0-0.1, scale to 0-100
            const n = props.adaptiveLink?.noise || 0;
            return Math.min(100, n * 1000);
        });

        const noiseDisplay = computed(() => {
            const n = props.adaptiveLink?.noise;
            if (!n && n !== 0) return '--';
            return (n * 100).toFixed(2) + '%';
        });

        return {
            isEnabled,
            mode,
            modeLabel,
            score,
            scorePercent,
            scoreColor,
            scoreClass,
            statusLabel,
            statusClass,
            hasProfile,
            profileFec,
            profileBitrate,
            profileRange,
            noisePercent,
            noiseDisplay,
        };
    },
    template: `
        <div class="panel adaptive-panel" v-if="isEnabled">
            <div class="panel-title">
                <span>Adaptive Link</span>
                <span class="adaptive-mode-badge">{{ modeLabel }}</span>
            </div>

            <div class="adaptive-content">
                <!-- Score gauge (both modes) -->
                <div class="adaptive-score-section">
                    <div class="score-row">
                        <span class="score-label">Score</span>
                        <span class="score-value" :class="scoreClass">{{ score ?? '--' }}</span>
                        <span class="score-range-label">(1000-2000)</span>
                    </div>
                    <div class="score-bar-container">
                        <div class="score-bar-bg">
                            <div
                                class="score-bar-fill"
                                :style="{ width: scorePercent + '%', backgroundColor: scoreColor }"
                            ></div>
                        </div>
                    </div>
                </div>

                <!-- GS mode: show RSSI/SNR/noise used in calculation -->
                <div class="adaptive-gs-stats" v-if="mode === 'gs'">
                    <div class="gs-stat">
                        <span class="gs-stat-label">Best RSSI</span>
                        <span class="gs-stat-value">{{ adaptiveLink?.best_rssi ?? '--' }} dBm</span>
                    </div>
                    <div class="gs-stat">
                        <span class="gs-stat-label">Best SNR</span>
                        <span class="gs-stat-value">{{ adaptiveLink?.best_snr ?? '--' }} dB</span>
                    </div>
                    <div class="gs-stat">
                        <span class="gs-stat-label">Noise</span>
                        <span class="gs-stat-value">{{ noiseDisplay }}</span>
                    </div>
                </div>

                <!-- Drone mode: show status and profile -->
                <div class="adaptive-drone-stats" v-if="mode === 'drone'">
                    <div class="drone-status-row">
                        <span class="drone-status" :class="statusClass">{{ statusLabel }}</span>
                    </div>

                    <div class="profile-grid" v-if="hasProfile">
                        <div class="profile-item">
                            <span class="profile-label">MCS</span>
                            <span class="profile-value">{{ adaptiveLink?.profile_mcs ?? '--' }}</span>
                        </div>
                        <div class="profile-item">
                            <span class="profile-label">FEC</span>
                            <span class="profile-value">{{ profileFec }}</span>
                        </div>
                        <div class="profile-item">
                            <span class="profile-label">Bitrate</span>
                            <span class="profile-value">{{ profileBitrate }} Mbps</span>
                        </div>
                        <div class="profile-item">
                            <span class="profile-label">TX Power</span>
                            <span class="profile-value">{{ adaptiveLink?.profile_tx_power ?? '--' }} dBm</span>
                        </div>
                        <div class="profile-item profile-range">
                            <span class="profile-label">Range</span>
                            <span class="profile-value">{{ profileRange }}</span>
                        </div>
                    </div>
                </div>
            </div>
        </div>
    `
};
