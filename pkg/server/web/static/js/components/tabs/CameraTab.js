// CameraTab.js - Camera (majestic) settings tab for drone config
import HelpTooltip from '../HelpTooltip.js';

export default {
    name: 'CameraTab',
    components: { HelpTooltip },
    props: {
        config: { type: Object, required: true },
        help: { type: Object, required: true }
    },
    template: `
        <div class="config-section" v-if="config.camera">
            <!-- Video Settings -->
            <div class="config-subsection">
                <div class="config-subsection-title">Video Settings</div>
                <div class="config-grid">
                    <div class="config-field">
                        <label>
                            Bitrate (kbps)
                            <HelpTooltip :text="help.camera_bitrate.text" :default-value="help.camera_bitrate.default" :tuning="help.camera_bitrate.tuning" />
                        </label>
                        <input type="number" v-model.number="config.camera.bitrate" min="500" max="50000" step="100">
                    </div>
                    <div class="config-field">
                        <label>
                            GOP (seconds)
                            <HelpTooltip :text="help.camera_gop.text" :default-value="help.camera_gop.default" :tuning="help.camera_gop.tuning" />
                        </label>
                        <input type="number" v-model.number="config.camera.gop" min="0.1" max="10" step="0.1">
                    </div>
                    <div class="config-field">
                        <label>
                            FPS
                            <HelpTooltip :text="help.camera_fps.text" :default-value="help.camera_fps.default" :tuning="help.camera_fps.tuning" />
                        </label>
                        <select v-model.number="config.camera.fps">
                            <option :value="30">30</option>
                            <option :value="60">60</option>
                            <option :value="90">90</option>
                            <option :value="120">120</option>
                        </select>
                    </div>
                    <div class="config-field">
                        <label>
                            Codec
                            <HelpTooltip :text="help.camera_codec.text" :default-value="help.camera_codec.default" :tuning="help.camera_codec.tuning" />
                        </label>
                        <select v-model="config.camera.codec">
                            <option value="h264">H.264</option>
                            <option value="h265">H.265</option>
                        </select>
                    </div>
                    <div class="config-field">
                        <label>
                            Resolution
                            <HelpTooltip :text="help.camera_size.text" :default-value="help.camera_size.default" :tuning="help.camera_size.tuning" />
                        </label>
                        <select v-model="config.camera.size">
                            <option value="1920x1080">1920x1080 (1080p)</option>
                            <option value="1280x720">1280x720 (720p)</option>
                            <option value="1440x1080">1440x1080 (4:3)</option>
                            <option value="2560x1440">2560x1440 (1440p)</option>
                            <option value="3840x2160">3840x2160 (4K)</option>
                        </select>
                    </div>
                    <div class="config-field">
                        <label>
                            Rate Control
                            <HelpTooltip :text="help.camera_rc_mode.text" :default-value="help.camera_rc_mode.default" :tuning="help.camera_rc_mode.tuning" />
                        </label>
                        <select v-model="config.camera.rc_mode">
                            <option value="cbr">CBR (Constant)</option>
                            <option value="vbr">VBR (Variable)</option>
                            <option value="avbr">AVBR (Adaptive)</option>
                        </select>
                    </div>
                </div>
            </div>

            <!-- Quality Settings -->
            <div class="config-subsection">
                <div class="config-subsection-title">Quality Settings</div>
                <div class="config-grid">
                    <div class="config-field">
                        <label>
                            QP Delta
                            <HelpTooltip :text="help.camera_qp_delta.text" :default-value="help.camera_qp_delta.default" :tuning="help.camera_qp_delta.tuning" />
                        </label>
                        <input type="number" v-model.number="config.camera.qp_delta" min="-30" max="30">
                    </div>
                </div>
            </div>

            <!-- Image Settings -->
            <div class="config-subsection">
                <div class="config-subsection-title">Image Settings</div>
                <div class="config-grid">
                    <div class="config-field">
                        <label>
                            Mirror
                            <HelpTooltip :text="help.camera_mirror.text" :default-value="help.camera_mirror.default" :tuning="help.camera_mirror.tuning" />
                        </label>
                        <select v-model="config.camera.mirror">
                            <option :value="false">Off</option>
                            <option :value="true">On</option>
                        </select>
                    </div>
                    <div class="config-field">
                        <label>
                            Flip
                            <HelpTooltip :text="help.camera_flip.text" :default-value="help.camera_flip.default" :tuning="help.camera_flip.tuning" />
                        </label>
                        <select v-model="config.camera.flip">
                            <option :value="false">Off</option>
                            <option :value="true">On</option>
                        </select>
                    </div>
                </div>
            </div>
        </div>

        <div v-else class="config-info">
            No camera configuration available from this drone.
        </div>
    `
};
