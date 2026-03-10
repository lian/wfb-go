// AntennaPanel.js - Per-antenna statistics display
const { computed } = Vue;

export default {
    name: 'AntennaPanel',
    props: {
        antennas: {
            type: Array,
            default: () => []
        },
        txWlan: {
            type: Number,
            default: null
        }
    },
    setup(props) {
        const hasAntennas = computed(() => {
            return props.antennas && props.antennas.length > 0;
        });

        // Sort antennas consistently by wlan_idx then antenna
        const sortedAntennas = computed(() => {
            if (!props.antennas) return [];
            return [...props.antennas].sort((a, b) => {
                if (a.wlan_idx !== b.wlan_idx) return a.wlan_idx - b.wlan_idx;
                return a.antenna - b.antenna;
            });
        });

        // Get shared info from first antenna (freq/bw are same for all)
        const sharedInfo = computed(() => {
            if (!props.antennas || props.antennas.length === 0) return null;
            const first = props.antennas[0];
            return {
                freq: formatFreq(first.freq),
                bw: first.bandwidth ? `${first.bandwidth}MHz` : '--'
            };
        });

        function antennaLabel(ant) {
            const name = ant.wlan_name || `wlan${ant.wlan_idx}`;
            return `${name}:${ant.antenna}`;
        }

        function rssiClass(rssi) {
            if (!rssi && rssi !== 0) return '';
            return rssi > -50 ? 'good' : rssi > -70 ? 'warn' : 'bad';
        }

        function snrClass(snr) {
            if (!snr && snr !== 0) return '';
            return snr > 25 ? 'good' : snr > 15 ? 'warn' : 'bad';
        }

        function formatFreq(freq) {
            if (!freq) return '--';
            return freq >= 5000 ? `${(freq/1000).toFixed(1)}G` : `${freq}`;
        }

        return {
            hasAntennas,
            sortedAntennas,
            sharedInfo,
            antennaLabel,
            rssiClass,
            snrClass,
        };
    },
    template: `
        <div class="panel" v-if="hasAntennas">
            <div class="panel-title">
                <span>Antennas ({{ sortedAntennas.length }})</span>
                <span class="panel-subtitle" v-if="sharedInfo">{{ sharedInfo.freq }} / {{ sharedInfo.bw }}</span>
            </div>
            <div class="antenna-grid">
                <div class="antenna-card" v-for="ant in sortedAntennas" :key="ant.wlan_idx + '-' + ant.antenna">
                    <div class="antenna-header">
                        <span>{{ antennaLabel(ant) }}</span>
                        <span class="tx-badge" v-if="txWlan === ant.wlan_idx">TX</span>
                    </div>
                    <div class="antenna-stat">
                        <span>RSSI</span>
                        <span :class="rssiClass(ant.rssi_avg)">
                            {{ ant.rssi_avg ?? '--' }}
                            <template v-if="ant.rssi_min !== ant.rssi_max">
                                ({{ ant.rssi_min }}/{{ ant.rssi_max }})
                            </template>
                        </span>
                    </div>
                    <div class="antenna-stat">
                        <span>SNR</span>
                        <span :class="snrClass(ant.snr_avg)">
                            {{ ant.snr_avg ?? '--' }}
                            <template v-if="ant.snr_min !== ant.snr_max">
                                ({{ ant.snr_min }}/{{ ant.snr_max }})
                            </template>
                        </span>
                    </div>
                    <div class="antenna-stat">
                        <span>Packets</span>
                        <span>{{ ant.packets?.toLocaleString() || 0 }}</span>
                    </div>
                </div>
            </div>
        </div>
    `
};
