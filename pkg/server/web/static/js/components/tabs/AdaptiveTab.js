// AdaptiveTab.js - Adaptive link settings tab
import HelpTooltip from '../HelpTooltip.js';

export default {
    name: 'AdaptiveTab',
    components: { HelpTooltip },
    props: {
        config: { type: Object, required: true },
        activeDevice: { type: String, required: true },
        help: { type: Object, required: true },
        formatDuration: { type: Function, required: true },
        parseDuration: { type: Function, required: true }
    },
    emits: ['add-profile', 'remove-profile', 'load-preset'],
    template: `
        <div class="config-section" v-if="config.adaptive">
            <!-- Basic Settings -->
            <div class="config-subsection">
                <div class="config-subsection-title">Basic Settings</div>
                <div class="config-grid">
                    <div class="config-field">
                        <label>
                            Enabled
                            <HelpTooltip :text="help.adaptive_enabled.text" :default-value="help.adaptive_enabled.default" :tuning="help.adaptive_enabled.tuning" />
                        </label>
                        <select v-model="config.adaptive.enabled">
                            <option :value="false">No</option>
                            <option :value="true">Yes</option>
                        </select>
                    </div>
                    <!-- GS: Send Address -->
                    <div class="config-field" v-if="activeDevice === 'gs'">
                        <label>
                            Send Address
                            <HelpTooltip :text="help.send_addr.text" :default-value="help.send_addr.default" :tuning="help.send_addr.tuning" />
                        </label>
                        <input type="text" v-model="config.adaptive.send_addr" placeholder="10.5.0.10:9999">
                    </div>
                    <!-- Drone: Listen Port -->
                    <div class="config-field" v-if="activeDevice === 'drone'">
                        <label>
                            Listen Port
                            <HelpTooltip :text="help.listen_port.text" :default-value="help.listen_port.default" :tuning="help.listen_port.tuning" />
                        </label>
                        <input type="number" v-model.number="config.adaptive.listen_port" min="1" max="65535">
                    </div>
                </div>
            </div>

            <!-- GS: Score Calculation Settings -->
            <template v-if="activeDevice === 'gs' && config.adaptive.enabled">
                <div class="config-subsection" v-if="config.adaptive.score_weights">
                    <div class="config-subsection-title">Score Weights</div>
                    <div class="config-grid">
                        <div class="config-field">
                            <label>
                                SNR Weight
                                <HelpTooltip :text="help.snr_weight.text" :default-value="help.snr_weight.default" :tuning="help.snr_weight.tuning" />
                            </label>
                            <input type="number" v-model.number="config.adaptive.score_weights.snr" min="0" max="1" step="0.1">
                        </div>
                        <div class="config-field">
                            <label>
                                RSSI Weight
                                <HelpTooltip :text="help.rssi_weight.text" :default-value="help.rssi_weight.default" :tuning="help.rssi_weight.tuning" />
                            </label>
                            <input type="number" v-model.number="config.adaptive.score_weights.rssi" min="0" max="1" step="0.1">
                        </div>
                    </div>
                </div>

                <div class="config-subsection" v-if="config.adaptive.score_ranges">
                    <div class="config-subsection-title">Score Ranges</div>
                    <div class="config-grid">
                        <div class="config-field">
                            <label>
                                SNR Min
                                <HelpTooltip :text="help.snr_min.text" :default-value="help.snr_min.default" :tuning="help.snr_min.tuning" />
                            </label>
                            <input type="number" v-model.number="config.adaptive.score_ranges.snr_min">
                        </div>
                        <div class="config-field">
                            <label>
                                SNR Max
                                <HelpTooltip :text="help.snr_max.text" :default-value="help.snr_max.default" :tuning="help.snr_max.tuning" />
                            </label>
                            <input type="number" v-model.number="config.adaptive.score_ranges.snr_max">
                        </div>
                        <div class="config-field">
                            <label>
                                RSSI Min
                                <HelpTooltip :text="help.rssi_min.text" :default-value="help.rssi_min.default" :tuning="help.rssi_min.tuning" />
                            </label>
                            <input type="number" v-model.number="config.adaptive.score_ranges.rssi_min">
                        </div>
                        <div class="config-field">
                            <label>
                                RSSI Max
                                <HelpTooltip :text="help.rssi_max.text" :default-value="help.rssi_max.default" :tuning="help.rssi_max.tuning" />
                            </label>
                            <input type="number" v-model.number="config.adaptive.score_ranges.rssi_max">
                        </div>
                    </div>
                </div>

                <div class="config-subsection" v-if="config.adaptive.kalman">
                    <div class="config-subsection-title">Kalman Filter</div>
                    <div class="config-grid">
                        <div class="config-field">
                            <label>
                                Initial Estimate
                                <HelpTooltip :text="help.kalman_estimate.text" :default-value="help.kalman_estimate.default" :tuning="help.kalman_estimate.tuning" />
                            </label>
                            <input type="number" v-model.number="config.adaptive.kalman.estimate" min="0" max="1" step="0.001">
                        </div>
                        <div class="config-field">
                            <label>
                                Error Estimate
                                <HelpTooltip :text="help.kalman_error.text" :default-value="help.kalman_error.default" :tuning="help.kalman_error.tuning" />
                            </label>
                            <input type="number" v-model.number="config.adaptive.kalman.error_estimate" min="0" max="1" step="0.01">
                        </div>
                        <div class="config-field">
                            <label>
                                Process Variance
                                <HelpTooltip :text="help.kalman_process.text" :default-value="help.kalman_process.default" :tuning="help.kalman_process.tuning" />
                            </label>
                            <input type="number" v-model.number="config.adaptive.kalman.process_variance" min="0" max="0.01" step="0.00001">
                        </div>
                        <div class="config-field">
                            <label>
                                Measurement Variance
                                <HelpTooltip :text="help.kalman_measurement.text" :default-value="help.kalman_measurement.default" :tuning="help.kalman_measurement.tuning" />
                            </label>
                            <input type="number" v-model.number="config.adaptive.kalman.measurement_variance" min="0" max="1" step="0.001">
                        </div>
                    </div>
                </div>
            </template>

            <!-- Drone: TX Settings -->
            <template v-if="activeDevice === 'drone' && config.adaptive.enabled">
                <div class="config-subsection">
                    <div class="config-subsection-title">Timing</div>
                    <div class="config-grid">
                        <div class="config-field">
                            <label>
                                Fallback Timeout
                                <HelpTooltip :text="help.fallback_timeout.text" :default-value="help.fallback_timeout.default" :tuning="help.fallback_timeout.tuning" />
                            </label>
                            <input type="text"
                                   :value="formatDuration(config.adaptive.fallback_timeout)"
                                   @change="config.adaptive.fallback_timeout = parseDuration($event.target.value)"
                                   placeholder="1s">
                        </div>
                        <div class="config-field">
                            <label>
                                Fallback Hold
                                <HelpTooltip :text="help.fallback_hold.text" :default-value="help.fallback_hold.default" :tuning="help.fallback_hold.tuning" />
                            </label>
                            <input type="text"
                                   :value="formatDuration(config.adaptive.fallback_hold)"
                                   @change="config.adaptive.fallback_hold = parseDuration($event.target.value)"
                                   placeholder="1s">
                        </div>
                        <div class="config-field">
                            <label>
                                Hold Up
                                <HelpTooltip :text="help.hold_up.text" :default-value="help.hold_up.default" :tuning="help.hold_up.tuning" />
                            </label>
                            <input type="text"
                                   :value="formatDuration(config.adaptive.hold_up)"
                                   @change="config.adaptive.hold_up = parseDuration($event.target.value)"
                                   placeholder="3s">
                        </div>
                        <div class="config-field">
                            <label>
                                Min Between Changes
                                <HelpTooltip :text="help.min_between_changes.text" :default-value="help.min_between_changes.default" :tuning="help.min_between_changes.tuning" />
                            </label>
                            <input type="text"
                                   :value="formatDuration(config.adaptive.min_between_changes)"
                                   @change="config.adaptive.min_between_changes = parseDuration($event.target.value)"
                                   placeholder="200ms">
                        </div>
                    </div>
                </div>

                <div class="config-subsection">
                    <div class="config-subsection-title">Smoothing & Hysteresis</div>
                    <div class="config-grid">
                        <div class="config-field">
                            <label>
                                Smoothing
                                <HelpTooltip :text="help.smoothing.text" :default-value="help.smoothing.default" :tuning="help.smoothing.tuning" />
                            </label>
                            <input type="number" v-model.number="config.adaptive.smoothing" min="0" max="1" step="0.05">
                        </div>
                        <div class="config-field">
                            <label>
                                Smoothing Down
                                <HelpTooltip :text="help.smoothing_down.text" :default-value="help.smoothing_down.default" :tuning="help.smoothing_down.tuning" />
                            </label>
                            <input type="number" v-model.number="config.adaptive.smoothing_down" min="0" max="1" step="0.05">
                        </div>
                        <div class="config-field">
                            <label>
                                Hysteresis
                                <HelpTooltip :text="help.hysteresis.text" :default-value="help.hysteresis.default" :tuning="help.hysteresis.tuning" />
                            </label>
                            <input type="number" v-model.number="config.adaptive.hysteresis" min="0" max="50">
                        </div>
                        <div class="config-field">
                            <label>
                                Hysteresis Down
                                <HelpTooltip :text="help.hysteresis_down.text" :default-value="help.hysteresis_down.default" :tuning="help.hysteresis_down.tuning" />
                            </label>
                            <input type="number" v-model.number="config.adaptive.hysteresis_down" min="0" max="50">
                        </div>
                    </div>
                </div>

                <div class="config-subsection">
                    <div class="config-subsection-title">Keyframe Control</div>
                    <div class="config-grid">
                        <div class="config-field">
                            <label>
                                Allow Keyframe Requests
                                <HelpTooltip :text="help.allow_keyframe.text" :default-value="help.allow_keyframe.default" :tuning="help.allow_keyframe.tuning" />
                            </label>
                            <select v-model="config.adaptive.allow_keyframe">
                                <option :value="false">No</option>
                                <option :value="true">Yes</option>
                            </select>
                        </div>
                        <div class="config-field">
                            <label>
                                Keyframe Interval
                                <HelpTooltip :text="help.keyframe_interval.text" :default-value="help.keyframe_interval.default" :tuning="help.keyframe_interval.tuning" />
                            </label>
                            <input type="text"
                                   :value="formatDuration(config.adaptive.keyframe_interval)"
                                   @change="config.adaptive.keyframe_interval = parseDuration($event.target.value)"
                                   placeholder="1112ms">
                        </div>
                        <div class="config-field">
                            <label>
                                IDR on Profile Change
                                <HelpTooltip :text="help.idr_on_change.text" :default-value="help.idr_on_change.default" :tuning="help.idr_on_change.tuning" />
                            </label>
                            <select v-model="config.adaptive.idr_on_change">
                                <option :value="false">No</option>
                                <option :value="true">Yes</option>
                            </select>
                        </div>
                    </div>
                </div>

                <div class="config-subsection">
                    <div class="config-subsection-title">TX Drop Recovery</div>
                    <div class="config-grid">
                        <div class="config-field">
                            <label>
                                Request Keyframe on Drop
                                <HelpTooltip :text="help.tx_drop_keyframe.text" :default-value="help.tx_drop_keyframe.default" :tuning="help.tx_drop_keyframe.tuning" />
                            </label>
                            <select v-model="config.adaptive.tx_drop_keyframe">
                                <option :value="false">No</option>
                                <option :value="true">Yes</option>
                            </select>
                        </div>
                        <div class="config-field">
                            <label>
                                Reduce Bitrate on Drop
                                <HelpTooltip :text="help.tx_drop_reduce_bitrate.text" :default-value="help.tx_drop_reduce_bitrate.default" :tuning="help.tx_drop_reduce_bitrate.tuning" />
                            </label>
                            <select v-model="config.adaptive.tx_drop_reduce_bitrate">
                                <option :value="false">No</option>
                                <option :value="true">Yes</option>
                            </select>
                        </div>
                        <div class="config-field">
                            <label>
                                Drop Check Interval
                                <HelpTooltip :text="help.tx_drop_check_interval.text" :default-value="help.tx_drop_check_interval.default" :tuning="help.tx_drop_check_interval.tuning" />
                            </label>
                            <input type="text"
                                   :value="formatDuration(config.adaptive.tx_drop_check_interval)"
                                   @change="config.adaptive.tx_drop_check_interval = parseDuration($event.target.value)"
                                   placeholder="2250ms">
                        </div>
                        <div class="config-field">
                            <label>
                                Bitrate Reduction Factor
                                <HelpTooltip :text="help.tx_drop_bitrate_factor.text" :default-value="help.tx_drop_bitrate_factor.default" :tuning="help.tx_drop_bitrate_factor.tuning" />
                            </label>
                            <input type="number" v-model.number="config.adaptive.tx_drop_bitrate_factor" min="0" max="1" step="0.05">
                        </div>
                    </div>
                </div>

                <div class="config-subsection">
                    <div class="config-subsection-title">Dynamic FEC</div>
                    <div class="config-grid">
                        <div class="config-field">
                            <label>
                                Dynamic FEC
                                <HelpTooltip :text="help.dynamic_fec.text" :default-value="help.dynamic_fec.default" :tuning="help.dynamic_fec.tuning" />
                            </label>
                            <select v-model="config.adaptive.dynamic_fec">
                                <option :value="false">No</option>
                                <option :value="true">Yes</option>
                            </select>
                        </div>
                        <div class="config-field">
                            <label>
                                FEC K Adjust
                                <HelpTooltip :text="help.fec_k_adjust.text" :default-value="help.fec_k_adjust.default" :tuning="help.fec_k_adjust.tuning" />
                            </label>
                            <select v-model="config.adaptive.fec_k_adjust">
                                <option :value="false">No</option>
                                <option :value="true">Yes</option>
                            </select>
                        </div>
                    </div>
                </div>

                <!-- TX Profiles -->
                <div class="config-subsection">
                    <div class="config-subsection-title">
                        TX Profiles
                        <HelpTooltip :text="help.profiles.text" :default-value="help.profiles.default" :tuning="help.profiles.tuning" />
                    </div>

                    <div v-if="config.adaptive.profiles && config.adaptive.profiles.length > 0" class="profile-table-wrapper">
                        <table class="profile-table">
                            <thead>
                                <tr>
                                    <th>Score Range</th>
                                    <th>MCS</th>
                                    <th>FEC (k/n)</th>
                                    <th>Bitrate</th>
                                    <th>GOP</th>
                                    <th>Power</th>
                                    <th>BW</th>
                                    <th>GI</th>
                                    <th>QP</th>
                                    <th></th>
                                </tr>
                            </thead>
                            <tbody>
                                <tr v-for="(profile, index) in config.adaptive.profiles" :key="index">
                                    <td class="profile-range">
                                        <input type="number" v-model.number="profile.range[0]" min="0" max="2000" class="range-input">
                                        <span>-</span>
                                        <input type="number" v-model.number="profile.range[1]" min="0" max="2000" class="range-input">
                                    </td>
                                    <td>
                                        <select v-model.number="profile.mcs" class="profile-select">
                                            <option v-for="m in 8" :key="m-1" :value="m-1">{{ m-1 }}</option>
                                        </select>
                                    </td>
                                    <td class="profile-fec">
                                        <input type="number" v-model.number="profile.fec[0]" min="1" max="12" class="fec-input">
                                        <span>/</span>
                                        <input type="number" v-model.number="profile.fec[1]" min="1" max="16" class="fec-input">
                                    </td>
                                    <td>
                                        <input type="number" v-model.number="profile.bitrate" min="0" max="50000" step="100" class="bitrate-input" placeholder="kbps">
                                    </td>
                                    <td>
                                        <input type="number" v-model.number="profile.gop" min="0" max="10" step="0.5" class="gop-input" placeholder="s">
                                    </td>
                                    <td>
                                        <input type="number" v-model.number="profile.power" min="0" max="30" class="power-input" placeholder="dBm">
                                    </td>
                                    <td>
                                        <select v-model.number="profile.bandwidth" class="profile-select-sm">
                                            <option :value="0">-</option>
                                            <option :value="20">20</option>
                                            <option :value="40">40</option>
                                        </select>
                                    </td>
                                    <td>
                                        <select v-model="profile.short_gi" class="profile-select-sm">
                                            <option :value="false">Off</option>
                                            <option :value="true">On</option>
                                        </select>
                                    </td>
                                    <td>
                                        <input type="number" v-model.number="profile.qp_delta" min="-30" max="30" class="qp-input">
                                    </td>
                                    <td>
                                        <button class="profile-delete-btn" @click="$emit('remove-profile', index)" title="Delete profile">&times;</button>
                                    </td>
                                </tr>
                            </tbody>
                        </table>
                    </div>

                    <div v-else class="config-info">
                        No profiles configured. Add profiles to enable automatic parameter adjustment.
                    </div>

                    <div class="profile-actions">
                        <button class="profile-add-btn" @click="$emit('add-profile')">+ Add Profile</button>
                        <button class="profile-preset-btn" @click="$emit('load-preset')">Load Preset</button>
                    </div>
                </div>
            </template>
        </div>
    `
};
