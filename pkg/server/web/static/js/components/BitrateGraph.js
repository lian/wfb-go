// BitrateGraph.js - Time-series bitrate visualization
const { ref, computed, onMounted, onBeforeUnmount, watch } = Vue;

export default {
    name: 'BitrateGraph',
    props: {
        bitrate: {
            type: Number,
            default: 0
        },
        maxPoints: {
            type: Number,
            default: 60  // 60 seconds of history
        }
    },
    setup(props) {
        const canvas = ref(null);
        const history = ref([]);
        const maxBitrate = ref(10);  // Auto-scales

        let ctx = null;
        let animationFrame = null;

        const currentBitrate = computed(() => {
            return props.bitrate?.toFixed(1) || '0.0';
        });

        const avgBitrate = computed(() => {
            if (history.value.length === 0) return '0.0';
            const sum = history.value.reduce((a, b) => a + b, 0);
            return (sum / history.value.length).toFixed(1);
        });

        const peakBitrate = computed(() => {
            if (history.value.length === 0) return '0.0';
            return Math.max(...history.value).toFixed(1);
        });

        function addDataPoint(value) {
            history.value.push(value || 0);
            if (history.value.length > props.maxPoints) {
                history.value.shift();
            }
            // Auto-scale max with some headroom
            const currentMax = Math.max(...history.value, 1);
            maxBitrate.value = Math.max(currentMax * 1.2, 5);
        }

        function draw() {
            if (!ctx || !canvas.value) return;

            const w = canvas.value.width;
            const h = canvas.value.height;
            const padding = { top: 5, right: 5, bottom: 5, left: 5 };
            const graphW = w - padding.left - padding.right;
            const graphH = h - padding.top - padding.bottom;

            // Clear
            ctx.fillStyle = '#1a1a2e';
            ctx.fillRect(0, 0, w, h);

            // Draw grid lines
            ctx.strokeStyle = '#2a2a4e';
            ctx.lineWidth = 1;
            for (let i = 0; i <= 4; i++) {
                const y = padding.top + (graphH * i / 4);
                ctx.beginPath();
                ctx.moveTo(padding.left, y);
                ctx.lineTo(w - padding.right, y);
                ctx.stroke();
            }

            // Draw data
            if (history.value.length < 2) return;

            const points = history.value;
            const stepX = graphW / (props.maxPoints - 1);

            // Fill area under curve
            ctx.beginPath();
            ctx.moveTo(padding.left + (props.maxPoints - points.length) * stepX, h - padding.bottom);

            for (let i = 0; i < points.length; i++) {
                const x = padding.left + (props.maxPoints - points.length + i) * stepX;
                const y = padding.top + graphH - (points[i] / maxBitrate.value) * graphH;
                if (i === 0) {
                    ctx.lineTo(x, y);
                } else {
                    ctx.lineTo(x, y);
                }
            }

            ctx.lineTo(padding.left + (props.maxPoints - 1) * stepX, h - padding.bottom);
            ctx.closePath();
            ctx.fillStyle = 'rgba(74, 222, 128, 0.2)';
            ctx.fill();

            // Draw line
            ctx.beginPath();
            for (let i = 0; i < points.length; i++) {
                const x = padding.left + (props.maxPoints - points.length + i) * stepX;
                const y = padding.top + graphH - (points[i] / maxBitrate.value) * graphH;
                if (i === 0) {
                    ctx.moveTo(x, y);
                } else {
                    ctx.lineTo(x, y);
                }
            }
            ctx.strokeStyle = '#4ade80';
            ctx.lineWidth = 2;
            ctx.stroke();

            // Draw current value dot
            if (points.length > 0) {
                const lastX = padding.left + (props.maxPoints - 1) * stepX;
                const lastY = padding.top + graphH - (points[points.length - 1] / maxBitrate.value) * graphH;
                ctx.beginPath();
                ctx.arc(lastX, lastY, 4, 0, Math.PI * 2);
                ctx.fillStyle = '#4ade80';
                ctx.fill();
            }
        }

        // Watch for bitrate changes
        watch(() => props.bitrate, (newVal) => {
            addDataPoint(newVal);
            draw();
        });

        onMounted(() => {
            if (canvas.value) {
                ctx = canvas.value.getContext('2d');
                // Set canvas size based on container
                const rect = canvas.value.getBoundingClientRect();
                canvas.value.width = rect.width * window.devicePixelRatio;
                canvas.value.height = rect.height * window.devicePixelRatio;
                ctx.scale(window.devicePixelRatio, window.devicePixelRatio);
                draw();
            }
        });

        onBeforeUnmount(() => {
            if (animationFrame) {
                cancelAnimationFrame(animationFrame);
            }
        });

        return {
            canvas,
            currentBitrate,
            avgBitrate,
            peakBitrate,
        };
    },
    template: `
        <div class="panel bitrate-panel">
            <div class="panel-title">
                <span>Bitrate</span>
                <div class="bitrate-stats">
                    <span class="bitrate-current">{{ currentBitrate }} Mbps</span>
                    <span class="bitrate-meta">avg: {{ avgBitrate }} / peak: {{ peakBitrate }}</span>
                </div>
            </div>
            <div class="bitrate-graph-container">
                <canvas ref="canvas" class="bitrate-canvas"></canvas>
            </div>
        </div>
    `
};
