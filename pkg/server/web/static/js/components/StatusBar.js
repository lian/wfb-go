// StatusBar.js - Main status bar with link status and key metrics
const { computed, ref } = Vue;

export default {
    name: 'StatusBar',
    props: {
        connected: Boolean,
        stats: Object,
        bitrate: [Number, String],
        nalCount: Number,
        pitMode: Object,
    },
    emits: ['toggle-pit-mode', 'open-scanner'],
    setup(props, { emit }) {
        const dropdownOpen = ref(false);

        const rssiClass = computed(() => {
            const rssi = props.stats?.rssi;
            if (!rssi) return '';
            return rssi > -50 ? 'good' : rssi > -70 ? 'warn' : 'bad';
        });

        const fecLostClass = computed(() => {
            const lost = props.stats?.fecLost || 0;
            return lost === 0 ? 'good' : lost < 10 ? 'warn' : 'bad';
        });

        const decErrorsClass = computed(() => {
            const errors = props.stats?.decErrors || 0;
            return errors === 0 ? 'good' : errors < 5 ? 'warn' : 'bad';
        });

        function toggleDropdown() {
            dropdownOpen.value = !dropdownOpen.value;
        }

        function closeDropdown() {
            dropdownOpen.value = false;
        }

        function onTogglePitMode() {
            closeDropdown();
            emit('toggle-pit-mode');
        }

        function onOpenScanner() {
            closeDropdown();
            emit('open-scanner');
        }

        return {
            dropdownOpen,
            rssiClass,
            fecLostClass,
            decErrorsClass,
            toggleDropdown,
            closeDropdown,
            onTogglePitMode,
            onOpenScanner,
        };
    },
    template: `
        <div class="status-bar">
            <div class="stat">
                <span class="stat-label">Link</span>
                <span class="stat-value" :class="connected ? 'good' : 'bad'">
                    {{ connected ? 'Connected' : 'Disconnected' }}
                </span>
            </div>
            <div class="stat">
                <span class="stat-label">RSSI</span>
                <span class="stat-value" :class="rssiClass">{{ stats?.rssi || '--' }} dBm</span>
            </div>
            <div class="stat">
                <span class="stat-label">SNR</span>
                <span class="stat-value">{{ stats?.snr || '--' }} dB</span>
            </div>
            <div class="stat">
                <span class="stat-label">Packets</span>
                <span class="stat-value">{{ stats?.packets?.toLocaleString() || 0 }}</span>
            </div>
            <div class="stat">
                <span class="stat-label">FEC Recovered</span>
                <span class="stat-value good">{{ stats?.fecRecovery || 0 }}</span>
            </div>
            <div class="stat">
                <span class="stat-label">FEC Lost</span>
                <span class="stat-value" :class="fecLostClass">{{ stats?.fecLost || 0 }}</span>
            </div>
            <div class="stat">
                <span class="stat-label">Dec Errors</span>
                <span class="stat-value" :class="decErrorsClass">{{ stats?.decErrors || 0 }}</span>
            </div>
            <div class="stat">
                <span class="stat-label">Bitrate</span>
                <span class="stat-value">{{ bitrate }} Mbps</span>
            </div>
            <div class="stat" v-if="stats?.txInjected !== undefined">
                <span class="stat-label">TX Injected</span>
                <span class="stat-value">{{ stats?.txInjected?.toLocaleString() || 0 }}</span>
            </div>
            <div class="stat" v-if="stats?.txDropped !== undefined">
                <span class="stat-label">TX Dropped</span>
                <span class="stat-value" :class="stats?.txDropped > 0 ? 'warn' : ''">{{ stats?.txDropped || 0 }}</span>
            </div>

            <!-- Actions Dropdown -->
            <div class="actions-dropdown" @mouseleave="closeDropdown">
                <button class="actions-btn" :class="{ active: pitMode?.enabled }" @click="toggleDropdown">
                    Actions <span class="dropdown-arrow">{{ dropdownOpen ? '▲' : '▼' }}</span>
                </button>
                <div class="dropdown-menu" v-show="dropdownOpen">
                    <button class="dropdown-item" @click="onOpenScanner">📡 Channel Scanner</button>
                    <button class="dropdown-item" :class="{ 'pit-active': pitMode?.enabled }"
                        @click="onTogglePitMode" :disabled="pitMode?.loading">
                        {{ pitMode?.enabled ? '🔴' : '⚡' }}
                        {{ pitMode?.loading ? 'Loading...' : (pitMode?.enabled ? 'Disable Pit Mode' : 'Enable Pit Mode') }}
                    </button>
                </div>
            </div>
        </div>
    `
};
