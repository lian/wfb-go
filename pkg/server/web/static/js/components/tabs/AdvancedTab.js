// AdvancedTab.js - Advanced/common settings tab
import HelpTooltip from '../HelpTooltip.js';

export default {
    name: 'AdvancedTab',
    components: { HelpTooltip },
    props: {
        config: { type: Object, required: true },
        help: { type: Object, required: true }
    },
    template: `
        <div class="config-section">
            <div class="config-subsection" v-if="config.common">
                <div class="config-subsection-title">Common Settings</div>
                <div class="config-grid">
                    <div class="config-field">
                        <label>
                            Debug Logging
                            <HelpTooltip :text="help.debug.text" :default-value="help.debug.default" :tuning="help.debug.tuning" />
                        </label>
                        <select v-model="config.common.debug">
                            <option :value="false">No</option>
                            <option :value="true">Yes</option>
                        </select>
                    </div>
                    <div class="config-field">
                        <label>
                            Radio MTU
                            <HelpTooltip :text="help.radio_mtu.text" :default-value="help.radio_mtu.default" :tuning="help.radio_mtu.tuning" />
                        </label>
                        <input type="number" v-model.number="config.common.radio_mtu" min="500" max="2000">
                    </div>
                    <div class="config-field">
                        <label>
                            Log Interval (ms)
                            <HelpTooltip :text="help.log_interval.text" :default-value="help.log_interval.default" :tuning="help.log_interval.tuning" />
                        </label>
                        <input type="number" v-model.number="config.common.log_interval" min="100" max="10000">
                    </div>
                    <div class="config-field">
                        <label>
                            TX Selection RSSI Delta
                            <HelpTooltip :text="help.tx_sel_rssi_delta.text" :default-value="help.tx_sel_rssi_delta.default" :tuning="help.tx_sel_rssi_delta.tuning" />
                        </label>
                        <input type="number" v-model.number="config.common.tx_sel_rssi_delta" min="0" max="20">
                    </div>
                </div>
            </div>

            <div class="config-subsection" v-if="config.common">
                <div class="config-subsection-title">Buffer Sizes</div>
                <div class="config-grid">
                    <div class="config-field">
                        <label>
                            TX Receive Buffer
                            <HelpTooltip :text="help.tx_rcv_buf_size.text" :default-value="help.tx_rcv_buf_size.default" :tuning="help.tx_rcv_buf_size.tuning" />
                        </label>
                        <input type="number" v-model.number="config.common.tx_rcv_buf_size" min="65536">
                    </div>
                    <div class="config-field">
                        <label>
                            RX Send Buffer
                            <HelpTooltip :text="help.rx_snd_buf_size.text" :default-value="help.rx_snd_buf_size.default" :tuning="help.rx_snd_buf_size.tuning" />
                        </label>
                        <input type="number" v-model.number="config.common.rx_snd_buf_size" min="65536">
                    </div>
                </div>
            </div>

            <div class="config-subsection" v-if="config.common">
                <div class="config-subsection-title">Aggregation Timeouts</div>
                <div class="config-grid">
                    <div class="config-field">
                        <label>
                            Tunnel Aggregation (s)
                            <HelpTooltip :text="help.tunnel_agg_timeout.text" :default-value="help.tunnel_agg_timeout.default" :tuning="help.tunnel_agg_timeout.tuning" />
                        </label>
                        <input type="number" v-model.number="config.common.tunnel_agg_timeout" min="0" max="1" step="0.001">
                    </div>
                    <div class="config-field">
                        <label>
                            MAVLink Aggregation (s)
                            <HelpTooltip :text="help.mavlink_agg_timeout.text" :default-value="help.mavlink_agg_timeout.default" :tuning="help.mavlink_agg_timeout.tuning" />
                        </label>
                        <input type="number" v-model.number="config.common.mavlink_agg_timeout" min="0" max="1" step="0.01">
                    </div>
                </div>
            </div>
        </div>
    `
};
