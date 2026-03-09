// StreamPanel.js - Per-stream statistics display
const { computed } = Vue;

export default {
    name: 'StreamPanel',
    props: {
        streams: {
            type: Array,
            default: () => []
        }
    },
    setup(props) {
        const hasStreams = computed(() => {
            return props.streams && props.streams.length > 0;
        });

        function formatRate(bytesPerSec) {
            if (!bytesPerSec) return '0';
            const mbps = (bytesPerSec * 8) / 1000000;
            return mbps.toFixed(1);
        }

        function streamTypeClass(type) {
            switch(type) {
                case 'rx': return 'stream-rx';
                case 'tx': return 'stream-tx';
                default: return '';
            }
        }

        function fecLabel(stream) {
            if (stream.fec_k && stream.fec_n) {
                return `${stream.fec_k}/${stream.fec_n}`;
            }
            return '--';
        }

        return {
            hasStreams,
            formatRate,
            streamTypeClass,
            fecLabel,
        };
    },
    template: `
        <div class="panel" v-if="hasStreams">
            <div class="panel-title">
                <span>Streams ({{ streams.length }})</span>
            </div>
            <div class="stream-list">
                <div class="stream-item" v-for="stream in streams" :key="stream.name">
                    <div class="stream-info">
                        <span class="stream-name">{{ stream.name }}</span>
                        <span class="stream-type" :class="streamTypeClass(stream.type)">{{ stream.type?.toUpperCase() }}</span>
                    </div>
                    <div class="stream-stats">
                        <div class="stream-stat">
                            <span class="stream-stat-label">Rate</span>
                            <span class="stream-stat-value">{{ formatRate(stream.byte_rate) }} Mbps</span>
                        </div>
                        <div class="stream-stat">
                            <span class="stream-stat-label">Packets</span>
                            <span class="stream-stat-value">{{ stream.packets?.toLocaleString() || 0 }}</span>
                        </div>
                        <div class="stream-stat" v-if="stream.fec_k && stream.fec_n">
                            <span class="stream-stat-label">FEC</span>
                            <span class="stream-stat-value">{{ fecLabel(stream) }}</span>
                        </div>
                        <div class="stream-stat" v-if="stream.fec_recovery !== undefined">
                            <span class="stream-stat-label">Recovered</span>
                            <span class="stream-stat-value good">{{ stream.fec_recovery || 0 }}</span>
                        </div>
                        <div class="stream-stat" v-if="stream.fec_lost !== undefined">
                            <span class="stream-stat-label">Lost</span>
                            <span class="stream-stat-value" :class="stream.fec_lost > 0 ? 'bad' : ''">{{ stream.fec_lost || 0 }}</span>
                        </div>
                        <div class="stream-stat" v-if="stream.dropped !== undefined">
                            <span class="stream-stat-label">Dropped</span>
                            <span class="stream-stat-value" :class="stream.dropped > 0 ? 'warn' : ''">{{ stream.dropped || 0 }}</span>
                        </div>
                    </div>
                </div>
            </div>
        </div>
    `
};
