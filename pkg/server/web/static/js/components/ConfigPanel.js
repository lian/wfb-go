// ConfigPanel.js - Ground station configuration panel (modal overlay)
import HelpTooltip from './HelpTooltip.js';
import { HELP } from './configHelp.js';
import GeneralTab from './tabs/GeneralTab.js';
import StreamsTab from './tabs/StreamsTab.js';
import CameraTab from './tabs/CameraTab.js';
import AdaptiveTab from './tabs/AdaptiveTab.js';
import AdvancedTab from './tabs/AdvancedTab.js';

const { ref, computed } = Vue;

export default {
    name: 'ConfigPanel',
    components: {
        HelpTooltip,
        GeneralTab,
        StreamsTab,
        CameraTab,
        AdaptiveTab,
        AdvancedTab
    },
    setup() {
        const config = ref(null);
        const loading = ref(true);
        const saving = ref(false);
        const error = ref(null);
        const success = ref(null);
        const isOpen = ref(false);
        const configAvailable = ref(true);
        const activeTab = ref('general');

        // Device selector: 'gs' or 'drone'
        const activeDevice = ref('gs');

        // Dynamic tabs based on device (Camera tab only for drone)
        const tabs = computed(() => {
            const baseTabs = [
                { id: 'general', label: 'General' },
                { id: 'streams', label: 'Streams' }
            ];
            if (activeDevice.value === 'drone') {
                baseTabs.push({ id: 'camera', label: 'Camera' });
            }
            baseTabs.push({ id: 'adaptive', label: 'Adaptive' });
            baseTabs.push({ id: 'advanced', label: 'Advanced' });
            return baseTabs;
        });

        // Drone remote config state
        const droneAddr = ref(localStorage.getItem('wfb_droneAddr') || '10.5.0.10');
        const droneMode = ref(localStorage.getItem('wfb_droneMode') || 'ssh'); // 'ssh' or 'http'
        const droneConfig = ref(null);
        const droneLoading = ref(false);
        const droneSaving = ref(false);
        const droneError = ref(null);
        const droneSuccess = ref(null);
        const droneConnected = ref(false);

        // Active config based on device selection
        const activeConfig = computed(() => {
            return activeDevice.value === 'drone' ? droneConfig.value : config.value;
        });

        const hasConfig = computed(() => config.value !== null);

        const hasActiveConfig = computed(() => {
            if (activeDevice.value === 'drone') {
                return droneConfig.value !== null;
            }
            return config.value !== null;
        });

        const isActiveLoading = computed(() => {
            return activeDevice.value === 'drone' ? droneLoading.value : loading.value;
        });

        const activeError = computed(() => {
            return activeDevice.value === 'drone' ? droneError.value : error.value;
        });

        const activeSuccess = computed(() => {
            return activeDevice.value === 'drone' ? droneSuccess.value : success.value;
        });

        const isActiveSaving = computed(() => {
            return activeDevice.value === 'drone' ? droneSaving.value : saving.value;
        });

        // GS config functions
        async function fetchConfig() {
            loading.value = true;
            error.value = null;
            try {
                const resp = await fetch('/api/config');
                if (resp.status === 503) {
                    configAvailable.value = false;
                    error.value = 'Config API not available (standalone mode)';
                    return;
                }
                if (!resp.ok) {
                    throw new Error(`HTTP ${resp.status}`);
                }
                config.value = await resp.json();
            } catch (e) {
                error.value = 'Failed to load config: ' + e.message;
            } finally {
                loading.value = false;
            }
        }

        async function saveConfig() {
            saving.value = true;
            error.value = null;
            success.value = null;
            try {
                const resp = await fetch('/api/config', {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(config.value)
                });
                const result = await resp.json();
                if (!resp.ok) {
                    throw new Error(result.error || `HTTP ${resp.status}`);
                }
                success.value = result.message || 'Config saved';
                setTimeout(() => { success.value = null; }, 5000);
            } catch (e) {
                error.value = 'Failed to save: ' + e.message;
            } finally {
                saving.value = false;
            }
        }

        // Drone config functions
        async function fetchDroneConfig() {
            droneLoading.value = true;
            droneError.value = null;
            droneConnected.value = false;
            localStorage.setItem('wfb_droneAddr', droneAddr.value);
            localStorage.setItem('wfb_droneMode', droneMode.value);
            try {
                const url = '/api/drone/config?addr=' + encodeURIComponent(droneAddr.value) + '&mode=' + droneMode.value;
                const resp = await fetch(url);
                if (!resp.ok) {
                    const result = await resp.json().catch(() => ({}));
                    throw new Error(result.error || `HTTP ${resp.status}`);
                }
                droneConfig.value = await resp.json();
                droneConnected.value = true;
            } catch (e) {
                droneError.value = 'Failed to connect: ' + e.message;
                droneConfig.value = null;
            } finally {
                droneLoading.value = false;
            }
        }

        async function saveDroneConfig() {
            droneSaving.value = true;
            droneError.value = null;
            droneSuccess.value = null;
            try {
                const resp = await fetch('/api/drone/config?addr=' + encodeURIComponent(droneAddr.value) + '&mode=' + droneMode.value, {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(droneConfig.value)
                });
                const result = await resp.json();
                if (!resp.ok) {
                    throw new Error(result.error || `HTTP ${resp.status}`);
                }
                droneSuccess.value = result.message || 'Config sent to drone';
                setTimeout(() => { droneSuccess.value = null; }, 5000);
            } catch (e) {
                droneError.value = 'Failed to send: ' + e.message;
            } finally {
                droneSaving.value = false;
            }
        }

        // Save/reload for active device
        async function saveActiveConfig() {
            if (activeDevice.value === 'drone') {
                await saveDroneConfig();
            } else {
                await saveConfig();
            }
        }

        async function reloadActiveConfig() {
            if (activeDevice.value === 'drone') {
                await fetchDroneConfig();
            } else {
                await fetchConfig();
            }
        }

        function open() {
            isOpen.value = true;
            fetchConfig();
        }

        function close() {
            isOpen.value = false;
        }

        // Duration helpers
        function formatDuration(ns) {
            if (!ns) return '0';
            if (ns >= 1e9) return (ns / 1e9) + 's';
            if (ns >= 1e6) return (ns / 1e6) + 'ms';
            if (ns >= 1e3) return (ns / 1e3) + 'µs';
            return ns + 'ns';
        }

        function parseDuration(str) {
            if (!str) return 0;
            str = str.toString().trim().toLowerCase();
            const match = str.match(/^([\d.]+)\s*(s|ms|µs|us|ns)?$/);
            if (!match) return parseInt(str) || 0;
            const val = parseFloat(match[1]);
            const unit = match[2] || 'ns';
            switch (unit) {
                case 's': return val * 1e9;
                case 'ms': return val * 1e6;
                case 'µs':
                case 'us': return val * 1e3;
                default: return val;
            }
        }

        // Profile management (works on activeConfig)
        function addProfile() {
            const cfg = activeConfig.value;
            if (!cfg?.adaptive) return;
            if (!cfg.adaptive.profiles) {
                cfg.adaptive.profiles = [];
            }
            const profiles = cfg.adaptive.profiles;
            let minScore = 1000;
            if (profiles.length > 0) {
                minScore = profiles[profiles.length - 1].range[1] + 1;
            }
            cfg.adaptive.profiles.push({
                range: [minScore, Math.min(minScore + 200, 2000)],
                mcs: 2,
                fec: [8, 12],
                bitrate: 8000,
                power: 25,
                bandwidth: 20,
                short_gi: false,
                gop: 2,
                qp_delta: -12,
                roi_qp: ''
            });
        }

        function removeProfile(index) {
            activeConfig.value?.adaptive?.profiles?.splice(index, 1);
        }

        function loadPresetProfiles() {
            const cfg = activeConfig.value;
            if (!cfg?.adaptive) return;
            cfg.adaptive.profiles = [
                { range: [999, 999], mcs: 0, fec: [2, 6], bitrate: 1000, power: 30, bandwidth: 20, short_gi: false, gop: 0.5, qp_delta: 0, roi_qp: '' },
                { range: [1000, 1200], mcs: 1, fec: [4, 8], bitrate: 4000, power: 30, bandwidth: 20, short_gi: false, gop: 1, qp_delta: -12, roi_qp: '' },
                { range: [1201, 1500], mcs: 2, fec: [8, 12], bitrate: 8000, power: 25, bandwidth: 20, short_gi: false, gop: 2, qp_delta: -12, roi_qp: '' },
                { range: [1501, 1800], mcs: 3, fec: [8, 12], bitrate: 12000, power: 20, bandwidth: 20, short_gi: false, gop: 2, qp_delta: -12, roi_qp: '' },
                { range: [1801, 2000], mcs: 4, fec: [10, 12], bitrate: 18000, power: 20, bandwidth: 20, short_gi: true, gop: 3, qp_delta: -12, roi_qp: '' },
            ];
        }

        return {
            // State
            config,
            loading,
            saving,
            error,
            success,
            isOpen,
            hasConfig,
            configAvailable,
            activeTab,
            tabs,
            // Device selector
            activeDevice,
            activeConfig,
            hasActiveConfig,
            isActiveLoading,
            activeError,
            activeSuccess,
            isActiveSaving,
            // Drone state
            droneAddr,
            droneMode,
            droneConfig,
            droneLoading,
            droneSaving,
            droneError,
            droneSuccess,
            droneConnected,
            // Functions
            fetchConfig,
            saveConfig,
            fetchDroneConfig,
            saveDroneConfig,
            saveActiveConfig,
            reloadActiveConfig,
            open,
            close,
            formatDuration,
            parseDuration,
            addProfile,
            removeProfile,
            loadPresetProfiles,
            // Constants
            HELP
        };
    },
    template: `
        <!-- Settings Button -->
        <button class="config-toggle-btn" @click="open" title="Settings">
            <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <circle cx="12" cy="12" r="3"></circle>
                <path d="M12 1v4M12 19v4M4.22 4.22l2.83 2.83M16.95 16.95l2.83 2.83M1 12h4M19 12h4M4.22 19.78l2.83-2.83M16.95 7.05l2.83-2.83"></path>
            </svg>
        </button>

        <!-- Modal Overlay -->
        <div v-if="isOpen" class="config-overlay" @click.self="close">
            <div class="config-modal">
                <div class="config-header">
                    <h2>Configuration</h2>
                    <div class="device-selector">
                        <button :class="['device-btn', { active: activeDevice === 'gs' }]" @click="activeDevice = 'gs'">
                            Ground Station
                        </button>
                        <button :class="['device-btn', { active: activeDevice === 'drone' }]" @click="activeDevice = 'drone'">
                            Drone
                        </button>
                    </div>
                    <button class="config-close-btn" @click="close">&times;</button>
                </div>

                <div class="config-body">
                    <!-- Drone connection UI -->
                    <div v-if="activeDevice === 'drone'" class="drone-connection-bar">
                        <div class="drone-addr-row">
                            <label>Drone:</label>
                            <input type="text" v-model="droneAddr" placeholder="10.5.0.10" class="drone-addr-input">
                            <select v-model="droneMode" class="drone-mode-select">
                                <option value="ssh">SSH (wfb-ng)</option>
                                <option value="http">HTTP (wfb-go)</option>
                            </select>
                            <button class="drone-fetch-btn" @click="fetchDroneConfig" :disabled="droneLoading">
                                {{ droneLoading ? 'Connecting...' : 'Connect' }}
                            </button>
                            <span class="drone-status-indicator" :class="{ connected: droneConnected }">
                                {{ droneConnected ? '● Connected' : '○ Not connected' }}
                            </span>
                        </div>
                    </div>

                    <!-- GS loading state -->
                    <div v-if="activeDevice === 'gs' && loading" class="config-loading">Loading...</div>

                    <!-- GS config unavailable (standalone mode) -->
                    <div v-else-if="activeDevice === 'gs' && !configAvailable" class="config-unavailable">
                        <div class="unavailable-icon">⚙️</div>
                        <div class="unavailable-title">Configuration Not Available</div>
                        <div class="unavailable-message">
                            Config editing is only available when running <code>wfb_server</code>.<br>
                            The standalone <code>wfb_web</code> does not have access to server configuration.
                        </div>
                    </div>

                    <!-- GS error state -->
                    <div v-else-if="activeDevice === 'gs' && error && !hasConfig" class="config-error">{{ error }}</div>

                    <!-- Drone not connected message -->
                    <div v-else-if="activeDevice === 'drone' && !droneConnected && !droneLoading" class="config-info drone-connect-msg">
                        <div v-if="droneError" class="config-error">{{ droneError }}</div>
                        <div v-else>Enter the drone address and click Connect to load its configuration.</div>
                    </div>

                    <!-- Main config content (either GS config or drone config is loaded) -->
                    <div v-else-if="hasActiveConfig" class="config-content">
                        <!-- Tab Navigation -->
                        <div class="config-tabs">
                            <button
                                v-for="tab in tabs"
                                :key="tab.id"
                                :class="['config-tab', { active: activeTab === tab.id }]"
                                @click="activeTab = tab.id"
                            >{{ tab.label }}</button>
                        </div>

                        <div class="config-sections">
                            <GeneralTab
                                v-show="activeTab === 'general'"
                                :config="activeConfig"
                                :help="HELP"
                            />
                            <StreamsTab
                                v-show="activeTab === 'streams'"
                                :config="activeConfig"
                                :active-device="activeDevice"
                                :drone-addr="droneAddr"
                                :drone-connected="droneConnected"
                                :help="HELP"
                            />
                            <CameraTab
                                v-if="activeDevice === 'drone'"
                                v-show="activeTab === 'camera'"
                                :config="activeConfig"
                                :help="HELP"
                            />
                            <AdaptiveTab
                                v-show="activeTab === 'adaptive'"
                                :config="activeConfig"
                                :active-device="activeDevice"
                                :help="HELP"
                                :format-duration="formatDuration"
                                :parse-duration="parseDuration"
                                @add-profile="addProfile"
                                @remove-profile="removeProfile"
                                @load-preset="loadPresetProfiles"
                            />
                            <AdvancedTab
                                v-show="activeTab === 'advanced'"
                                :config="activeConfig"
                                :help="HELP"
                            />
                        </div>
                    </div>
                </div>

                <div class="config-footer" v-if="hasActiveConfig">
                    <div v-if="activeSuccess" class="config-success">{{ activeSuccess }}</div>
                    <div v-if="activeError && hasActiveConfig" class="config-error">{{ activeError }}</div>
                    <div class="config-actions">
                        <button class="reload-btn" @click="reloadActiveConfig" :disabled="isActiveLoading">
                            Reload
                        </button>
                        <button class="save-btn" @click="saveActiveConfig" :disabled="isActiveSaving || !hasActiveConfig">
                            {{ isActiveSaving ? 'Saving...' : (activeDevice === 'drone' ? 'Send to Drone' : 'Save Config') }}
                        </button>
                    </div>
                </div>
            </div>
        </div>
    `
};
