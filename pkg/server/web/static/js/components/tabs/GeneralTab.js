// GeneralTab.js - Combined Hardware, Link, and Server settings tab
import HelpTooltip from '../HelpTooltip.js';

const { ref } = Vue;

export default {
    name: 'GeneralTab',
    components: { HelpTooltip },
    props: {
        config: { type: Object, required: true },
        help: { type: Object, required: true }
    },
    setup() {
        const showKey = ref(false);
        return { showKey };
    },
    template: `
        <div class="config-section">
            <!-- Hardware Settings -->
            <div class="config-subsection" v-if="config.hardware">
                <div class="config-subsection-title">Hardware</div>
                <div class="config-grid">
                    <div class="config-field">
                        <label>
                            Channel
                            <HelpTooltip :text="help.channel.text" :default-value="help.channel.default" :tuning="help.channel.tuning" />
                        </label>
                        <select v-model.number="config.hardware.channel">
                            <optgroup label="5GHz Low">
                                <option :value="36">36 (5180 MHz)</option>
                                <option :value="40">40 (5200 MHz)</option>
                                <option :value="44">44 (5220 MHz)</option>
                                <option :value="48">48 (5240 MHz)</option>
                            </optgroup>
                            <optgroup label="5GHz Mid">
                                <option :value="52">52 (5260 MHz)</option>
                                <option :value="56">56 (5280 MHz)</option>
                                <option :value="60">60 (5300 MHz)</option>
                                <option :value="64">64 (5320 MHz)</option>
                            </optgroup>
                            <optgroup label="5GHz High (Recommended)">
                                <option :value="100">100 (5500 MHz)</option>
                                <option :value="104">104 (5520 MHz)</option>
                                <option :value="108">108 (5540 MHz)</option>
                                <option :value="112">112 (5560 MHz)</option>
                                <option :value="116">116 (5580 MHz)</option>
                                <option :value="120">120 (5600 MHz)</option>
                                <option :value="124">124 (5620 MHz)</option>
                                <option :value="128">128 (5640 MHz)</option>
                                <option :value="132">132 (5660 MHz)</option>
                                <option :value="136">136 (5680 MHz)</option>
                                <option :value="140">140 (5700 MHz)</option>
                                <option :value="144">144 (5720 MHz)</option>
                                <option :value="149">149 (5745 MHz)</option>
                                <option :value="153">153 (5765 MHz)</option>
                                <option :value="157">157 (5785 MHz)</option>
                                <option :value="161">161 (5805 MHz)</option>
                                <option :value="165">165 (5825 MHz)</option>
                            </optgroup>
                        </select>
                    </div>
                    <div class="config-field">
                        <label>
                            Bandwidth
                            <HelpTooltip :text="help.bandwidth.text" :default-value="help.bandwidth.default" :tuning="help.bandwidth.tuning" />
                        </label>
                        <select v-model.number="config.hardware.bandwidth">
                            <option :value="20">20 MHz</option>
                            <option :value="40">40 MHz</option>
                        </select>
                    </div>
                    <div class="config-field">
                        <label>
                            TX Power (dBm)
                            <HelpTooltip :text="help.tx_power.text" :default-value="help.tx_power.default" :tuning="help.tx_power.tuning" />
                        </label>
                        <input type="number" v-model.number="config.hardware.tx_power" min="0" max="30" placeholder="Auto">
                    </div>
                    <div class="config-field">
                        <label>
                            Region
                            <HelpTooltip :text="help.region.text" :default-value="help.region.default" :tuning="help.region.tuning" />
                        </label>
                        <select v-model="config.hardware.region">
                            <option value="BO">BO - Bolivia (Max channels)</option>
                            <option value="US">US - United States</option>
                            <option value="EU">EU - Europe</option>
                            <option value="JP">JP - Japan</option>
                            <option value="CN">CN - China</option>
                            <option value="00">00 - World</option>
                        </select>
                    </div>
                </div>
            </div>
            <div v-else class="config-info">Hardware settings not available.</div>

            <!-- Link Settings -->
            <div class="config-subsection" v-if="config.link">
                <div class="config-subsection-title">Link</div>
                <div class="config-grid">
                    <div class="config-field wide">
                        <label>
                            Domain
                            <HelpTooltip :text="help.domain.text" :default-value="help.domain.default" :tuning="help.domain.tuning" />
                        </label>
                        <input type="text" v-model="config.link.domain" placeholder="default">
                    </div>
                    <div class="config-field">
                        <label>
                            Link ID (hex)
                            <HelpTooltip :text="help.link_id.text" :default-value="help.link_id.default" :tuning="help.link_id.tuning" />
                        </label>
                        <input type="text" :value="config.link.id ? '0x' + config.link.id.toString(16) : ''"
                               @input="config.link.id = parseInt($event.target.value, 16) || 0"
                               placeholder="0x0 (auto)">
                    </div>
                    <div class="config-field wide">
                        <label>
                            Key (Base64)
                            <HelpTooltip :text="help.key_base64.text" :default-value="help.key_base64.default" :tuning="help.key_base64.tuning" />
                        </label>
                        <div class="key-input-wrapper">
                            <input
                                :type="showKey ? 'text' : 'password'"
                                v-model="config.link.key_base64"
                                placeholder="No key configured"
                                class="key-input"
                                spellcheck="false"
                                autocomplete="off"
                            >
                            <button type="button" class="key-toggle-btn" @click="showKey = !showKey" :title="showKey ? 'Hide key' : 'Show key'">
                                <svg v-if="showKey" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                                    <path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"></path>
                                    <line x1="1" y1="1" x2="23" y2="23"></line>
                                </svg>
                                <svg v-else width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                                    <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"></path>
                                    <circle cx="12" cy="12" r="3"></circle>
                                </svg>
                            </button>
                        </div>
                    </div>
                </div>
            </div>
            <div v-else class="config-info">Link settings not available.</div>

            <!-- Web Server Settings -->
            <div class="config-subsection" v-if="config.web">
                <div class="config-subsection-title">Web Server</div>
                <div class="config-grid">
                    <div class="config-field">
                        <label>
                            Enabled
                            <HelpTooltip :text="help.web_enabled.text" :default-value="help.web_enabled.default" :tuning="help.web_enabled.tuning" />
                        </label>
                        <select v-model="config.web.enabled">
                            <option :value="false">No</option>
                            <option :value="true">Yes</option>
                        </select>
                    </div>
                    <div class="config-field">
                        <label>
                            Port
                            <HelpTooltip :text="help.web_port.text" :default-value="help.web_port.default" :tuning="help.web_port.tuning" />
                        </label>
                        <input type="number" v-model.number="config.web.port" min="1" max="65535">
                    </div>
                    <div class="config-field" v-if="config.web.video_stream !== undefined">
                        <label>
                            Video Stream
                            <HelpTooltip :text="help.video_stream.text" :default-value="help.video_stream.default" :tuning="help.video_stream.tuning" />
                        </label>
                        <input type="text" v-model="config.web.video_stream" placeholder="video">
                    </div>
                </div>
            </div>

            <!-- Stats API Settings -->
            <div class="config-subsection" v-if="config.api">
                <div class="config-subsection-title">Stats API</div>
                <div class="config-grid">
                    <div class="config-field">
                        <label>
                            Enabled
                            <HelpTooltip :text="help.api_enabled.text" :default-value="help.api_enabled.default" :tuning="help.api_enabled.tuning" />
                        </label>
                        <select v-model="config.api.enabled">
                            <option :value="false">No</option>
                            <option :value="true">Yes</option>
                        </select>
                    </div>
                    <div class="config-field">
                        <label>
                            JSON Port
                            <HelpTooltip :text="help.json_port.text" :default-value="help.json_port.default" :tuning="help.json_port.tuning" />
                        </label>
                        <input type="number" v-model.number="config.api.json_port" min="0" max="65535" placeholder="0 = disabled">
                    </div>
                    <div class="config-field">
                        <label>
                            MsgPack Port
                            <HelpTooltip :text="help.stats_port.text" :default-value="help.stats_port.default" :tuning="help.stats_port.tuning" />
                        </label>
                        <input type="number" v-model.number="config.api.stats_port" min="0" max="65535" placeholder="0 = disabled">
                    </div>
                </div>
            </div>
        </div>
    `
};
