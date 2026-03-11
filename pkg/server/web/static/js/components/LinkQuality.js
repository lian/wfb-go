// LinkQuality.js - Time-series link quality visualization
const { ref, computed, onMounted, onUnmounted, watch } = Vue;

export default {
    name: 'LinkQuality',
    props: {
        bitrate: {
            type: Number,
            default: 0
        },
        stats: {
            type: Object,
            default: () => ({})
        },
        maxPoints: {
            type: Number,
            default: 60  // 60 data points = 60 seconds (1 point per second)
        }
    },
    setup(props) {
        const canvas = ref(null);
        const history = ref([]);  // Array of { bitrate, rssi, quality }
        const maxBitrate = ref(10);  // Auto-scales

        let ctx = null;
        let lastUpdateTime = 0;
        const updateInterval = 1000;  // Update once per second for smoother movement

        // Track previous values to compute deltas for quality
        let prevPackets = 0;
        let prevFecRecovery = 0;
        let prevFecLost = 0;
        let prevDecErrors = 0;

        const currentBitrate = computed(() => {
            return props.bitrate?.toFixed(1) || '0.0';
        });

        const avgBitrate = computed(() => {
            if (history.value.length === 0) return '0.0';
            const sum = history.value.reduce((a, b) => a + b.bitrate, 0);
            return (sum / history.value.length).toFixed(1);
        });

        const peakBitrate = computed(() => {
            if (history.value.length === 0) return '0.0';
            return Math.max(...history.value.map(h => h.bitrate)).toFixed(1);
        });

        const avgRssi = computed(() => {
            if (history.value.length === 0) return '-100';
            const sum = history.value.reduce((a, b) => a + b.rssi, 0);
            return Math.round(sum / history.value.length);
        });

        const peakRssi = computed(() => {
            if (history.value.length === 0) return '-100';
            return Math.max(...history.value.map(h => h.rssi));
        });

        // Compute current quality status for display
        const currentQuality = computed(() => {
            if (history.value.length === 0) return 'ok';
            return history.value[history.value.length - 1].quality;
        });

        const currentRssi = computed(() => {
            if (history.value.length === 0) return '-100';
            return history.value[history.value.length - 1].rssi;
        });

        function getQuality(stats) {
            // Compute deltas since last update
            const deltaPackets = (stats.packets || 0) - prevPackets;
            const deltaRecovered = (stats.fecRecovery || 0) - prevFecRecovery;
            const deltaLost = (stats.fecLost || 0) - prevFecLost;
            const deltaErrors = (stats.decErrors || 0) - prevDecErrors;

            // Update previous values
            prevPackets = stats.packets || 0;
            prevFecRecovery = stats.fecRecovery || 0;
            prevFecLost = stats.fecLost || 0;
            prevDecErrors = stats.decErrors || 0;

            // Quality levels based on FEC activity as percentage of packets:
            // - no packets or lost/errors = bad (red)
            // - recovery rate > 3% = heavy FEC (orange)
            // - recovery rate > 0.5% = some FEC (yellow)
            // - recovery rate <= 0.5% = good (green)
            if (deltaLost > 0 || deltaErrors > 0) return 'lost';

            // No packets received = link down
            if (deltaPackets === 0 && prevPackets > 0) return 'lost';

            // Compute recovery rate as percentage
            if (deltaPackets > 0) {
                const recoveryRate = (deltaRecovered / deltaPackets) * 100;
                if (recoveryRate > 3.0) return 'fec-heavy';  // >3% recovery rate
                if (recoveryRate > 0.5) return 'fec';        // >0.5% recovery rate
            }
            return 'ok';
        }

        function qualityColor(quality) {
            switch (quality) {
                case 'lost': return '#ef4444';       // Red - packets lost
                case 'fec-heavy': return '#f97316';  // Orange - heavy FEC
                case 'fec': return '#fbbf24';        // Yellow - some FEC
                default: return '#4ade80';           // Green - all OK
            }
        }

        function addDataPoint(bitrate, rssi, stats) {
            const now = Date.now();
            // Only add a point once per second (but always add first point)
            if (history.value.length > 0 && now - lastUpdateTime < updateInterval) {
                return false;
            }
            lastUpdateTime = now;

            const quality = getQuality(stats);
            history.value.push({ bitrate: bitrate || 0, rssi: rssi || -100, quality });
            if (history.value.length > props.maxPoints) {
                history.value.shift();
            }
            // Auto-scale max with some headroom
            const currentMax = Math.max(...history.value.map(h => h.bitrate), 1);
            maxBitrate.value = Math.max(currentMax * 1.2, 5);
            return true;
        }

        function initCanvas() {
            if (!canvas.value) return false;
            ctx = canvas.value.getContext('2d');
            const rect = canvas.value.getBoundingClientRect();
            if (rect.width > 0 && rect.height > 0) {
                const dpr = window.devicePixelRatio || 1;
                const needsResize = canvas.value.width !== Math.floor(rect.width * dpr) ||
                                   canvas.value.height !== Math.floor(rect.height * dpr);
                if (needsResize) {
                    canvas.value.width = Math.floor(rect.width * dpr);
                    canvas.value.height = Math.floor(rect.height * dpr);
                }
                // Always reset transform and scale fresh
                ctx.setTransform(1, 0, 0, 1, 0, 0);
                ctx.scale(dpr, dpr);
                return true;
            }
            return false;
        }

        function draw() {
            if (!initCanvas()) return;

            const rect = canvas.value.getBoundingClientRect();
            const w = rect.width;
            const h = rect.height;
            const padding = { top: 8, right: 8, bottom: 8, left: 8 };
            const graphW = w - padding.left - padding.right;
            const graphH = h - padding.top - padding.bottom;

            // Clear
            ctx.fillStyle = '#1a1a2e';
            ctx.fillRect(0, 0, w, h);

            if (history.value.length < 1) return;

            const points = history.value;
            const stepX = graphW / (props.maxPoints - 1);
            const startIdx = props.maxPoints - points.length;
            const barWidth = Math.max(stepX - 1, 2);

            // === Draw quality bars (full height, background layer) ===
            for (let i = 0; i < points.length; i++) {
                const x = padding.left + (startIdx + i) * stepX;
                ctx.fillStyle = qualityColor(points[i].quality);
                ctx.fillRect(x, padding.top, barWidth, graphH);
            }

            // === Draw grid lines on top of bars ===
            ctx.strokeStyle = 'rgba(42, 42, 78, 0.5)';
            ctx.lineWidth = 1;
            for (let i = 0; i <= 4; i++) {
                const y = padding.top + (graphH * i / 4);
                ctx.beginPath();
                ctx.moveTo(padding.left, y);
                ctx.lineTo(w - padding.right, y);
                ctx.stroke();
            }

            if (history.value.length < 2) return;

            // === Draw bitrate line (overlay) ===
            // Fill area under curve with semi-transparent dark
            ctx.beginPath();
            ctx.moveTo(padding.left + startIdx * stepX, padding.top + graphH);
            for (let i = 0; i < points.length; i++) {
                const x = padding.left + (startIdx + i) * stepX;
                const y = padding.top + graphH - (points[i].bitrate / maxBitrate.value) * graphH;
                ctx.lineTo(x, y);
            }
            ctx.lineTo(padding.left + (startIdx + points.length - 1) * stepX, padding.top + graphH);
            ctx.closePath();
            ctx.fillStyle = 'rgba(0, 0, 0, 0.3)';
            ctx.fill();

            // Helper to draw a line
            function drawLine(points, getY, color) {
                ctx.beginPath();
                for (let i = 0; i < points.length; i++) {
                    const x = padding.left + (startIdx + i) * stepX;
                    const y = getY(points[i]);
                    if (i === 0) ctx.moveTo(x, y);
                    else ctx.lineTo(x, y);
                }
                ctx.strokeStyle = color;
                ctx.lineWidth = 2;
                ctx.stroke();
            }

            // Draw bitrate line (light gray)
            drawLine(points,
                p => padding.top + graphH - (p.bitrate / maxBitrate.value) * graphH,
                '#e0e0e0'
            );

            // Draw RSSI line (magenta/pink)
            const rssiMin = -90, rssiMax = -30;
            drawLine(points,
                p => {
                    const rssiNorm = (p.rssi - rssiMin) / (rssiMax - rssiMin);
                    return padding.top + graphH - Math.max(0, Math.min(1, rssiNorm)) * graphH;
                },
                '#e879f9'
            );

        }

        let resizeObserver = null;

        // Watch for bitrate or stats changes
        watch([() => props.bitrate, () => props.stats], ([newBitrate, newStats]) => {
            const rssi = newStats?.rssi || -100;
            if (addDataPoint(newBitrate, rssi, newStats || {})) {
                draw();
            }
        }, { deep: true, immediate: true });

        onMounted(() => {
            // Set up resize observer to handle container size changes
            if (canvas.value) {
                resizeObserver = new ResizeObserver(() => {
                    draw();
                });
                resizeObserver.observe(canvas.value.parentElement);
            }
            // Initial draw with delay to ensure DOM is ready
            setTimeout(() => draw(), 50);
        });

        onUnmounted(() => {
            if (resizeObserver) {
                resizeObserver.disconnect();
            }
        });

        return {
            canvas,
            currentBitrate,
            avgBitrate,
            peakBitrate,
            avgRssi,
            peakRssi,
            currentQuality,
            currentRssi,
        };
    },
    template: `
        <div class="panel link-quality-panel">
            <div class="panel-title">
                <span>Link Quality</span>
                <div class="bitrate-stats">
                    <span class="bitrate-current" :class="'quality-' + currentQuality">{{ currentBitrate }} Mbps</span>
                    <span class="rssi-current">{{ currentRssi }} dBm</span>
                </div>
            </div>
            <div class="link-quality-graph-container">
                <canvas ref="canvas" class="link-quality-canvas"></canvas>
            </div>
            <div class="quality-footer">
                <div class="quality-legend">
                    <span class="legend-item"><span class="line gray"></span>Bitrate</span>
                    <span class="legend-item"><span class="line magenta"></span>RSSI</span>
                    <span class="legend-sep">|</span>
                    <span class="legend-item"><span class="dot green"></span>OK</span>
                    <span class="legend-item"><span class="dot yellow"></span>FEC</span>
                    <span class="legend-item"><span class="dot red"></span>Lost</span>
                </div>
                <div class="quality-avgpeak">
                    <span>avg/peak: {{ avgBitrate }}/{{ peakBitrate }} Mbps, {{ avgRssi }}/{{ peakRssi }} dBm</span>
                </div>
            </div>
        </div>
    `
};
