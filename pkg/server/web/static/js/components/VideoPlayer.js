// VideoPlayer.js - H.265/HEVC video player using WebCodecs
const { ref, computed, onMounted, onBeforeUnmount } = Vue;

// H.265 NAL unit types
const NAL = {
    TRAIL_N: 0, TRAIL_R: 1,
    TSA_N: 2, TSA_R: 3,
    STSA_N: 4, STSA_R: 5,
    RADL_N: 6, RADL_R: 7,
    RASL_N: 8, RASL_R: 9,
    BLA_W_LP: 16, BLA_W_RADL: 17, BLA_N_LP: 18,
    IDR_W_RADL: 19, IDR_N_LP: 20,
    CRA: 21,
    VPS: 32, SPS: 33, PPS: 34,
    AUD: 35, EOS: 36, EOB: 37, FD: 38,
    PREFIX_SEI: 39, SUFFIX_SEI: 40,
};

export default {
    name: 'VideoPlayer',
    props: {
        stats: Object,
    },
    emits: ['connected', 'disconnected'],
    setup(props, { emit }) {
        // Template refs
        const videoCanvas = ref(null);

        // Reactive state
        const connected = ref(false);
        const hasVideo = ref(false);
        const codecSupported = ref(false);
        const codecStatus = ref('Checking...');
        const statusMessage = ref('Initializing...');
        const debugLog = ref([]);
        const fps = ref(0);
        const avgDecodeTime = ref(0);
        const decodeQueue = ref(0);
        const nalCount = ref(0);

        // Non-reactive internal state
        let decoder = null;
        let ctx = null;
        let videoWs = null;
        let vps = null;
        let sps = null;
        let pps = null;
        let decoderConfigured = false;
        let waitingForKeyframe = true;
        let frameCount = 0;
        let lastFpsTime = 0;
        let decodeTimeSum = 0;
        let decodeTimeCount = 0;
        let errorCount = 0;
        let lastErrorTime = 0;
        let resetPending = false;

        // Computed
        const rssiClass = computed(() => {
            const rssi = props.stats?.rssi;
            if (!rssi) return '';
            return rssi > -50 ? 'good' : rssi > -70 ? 'warn' : 'bad';
        });

        // Methods
        function log(msg) {
            console.log(msg);
            const time = new Date().toLocaleTimeString();
            debugLog.value.push(`${time}: ${msg}`);
            if (debugLog.value.length > 20) debugLog.value.shift();
        }

        function getNalType(nalu) {
            if (nalu.length < 2) return -1;
            return (nalu[0] >> 1) & 0x3F;
        }

        function isIdrFrame(nalType) {
            return nalType >= NAL.BLA_W_LP && nalType <= NAL.CRA;
        }

        function isVclNalu(nalType) {
            return nalType <= NAL.RASL_R || (nalType >= NAL.BLA_W_LP && nalType <= NAL.CRA);
        }

        function buildHvccDescription(vpsData, spsData, ppsData) {
            const arrays = [
                { type: 32, nalus: [vpsData] },
                { type: 33, nalus: [spsData] },
                { type: 34, nalus: [ppsData] },
            ];

            let size = 23;
            for (const arr of arrays) {
                size += 3;
                for (const nalu of arr.nalus) {
                    size += 2 + nalu.length;
                }
            }

            const buf = new Uint8Array(size);
            let offset = 0;

            buf[offset++] = 1;
            buf[offset++] = 0x01;
            buf[offset++] = 0x60;
            buf[offset++] = 0x00;
            buf[offset++] = 0x00;
            buf[offset++] = 0x00;
            buf[offset++] = 0x90;
            buf[offset++] = 0x00;
            buf[offset++] = 0x00;
            buf[offset++] = 0x00;
            buf[offset++] = 0x00;
            buf[offset++] = 0x00;
            buf[offset++] = 93;
            buf[offset++] = 0xF0;
            buf[offset++] = 0x00;
            buf[offset++] = 0xFC;
            buf[offset++] = 0xFD;
            buf[offset++] = 0xF8;
            buf[offset++] = 0xF8;
            buf[offset++] = 0x00;
            buf[offset++] = 0x00;
            buf[offset++] = 0x03;
            buf[offset++] = arrays.length;

            for (const arr of arrays) {
                buf[offset++] = 0x80 | arr.type;
                buf[offset++] = (arr.nalus.length >> 8) & 0xFF;
                buf[offset++] = arr.nalus.length & 0xFF;

                for (const nalu of arr.nalus) {
                    buf[offset++] = (nalu.length >> 8) & 0xFF;
                    buf[offset++] = nalu.length & 0xFF;
                    buf.set(nalu, offset);
                    offset += nalu.length;
                }
            }

            return buf.slice(0, offset);
        }

        async function configureDecoder() {
            if (!vps || !sps || !pps) {
                return false;
            }

            try {
                const description = buildHvccDescription(vps, sps, pps);

                if (decoder && decoder.state !== 'closed') {
                    decoder.close();
                }

                decoder = new VideoDecoder({
                    output: (frame) => {
                        const decodeTime = performance.now() - (frame.timestamp / 1000);
                        decodeTimeSum += decodeTime;
                        decodeTimeCount++;

                        const canvas = videoCanvas.value;
                        if (canvas) {
                            if (canvas.width !== frame.displayWidth) {
                                canvas.width = frame.displayWidth;
                                canvas.height = frame.displayHeight;
                            }
                            ctx.drawImage(frame, 0, 0);
                        }
                        frame.close();

                        hasVideo.value = true;
                        frameCount++;
                        // Reset error count on successful decode
                        errorCount = 0;

                        const now = performance.now();
                        if (now - lastFpsTime >= 1000) {
                            fps.value = frameCount;
                            frameCount = 0;
                            lastFpsTime = now;
                            if (decodeTimeCount > 0) {
                                avgDecodeTime.value = (decodeTimeSum / decodeTimeCount).toFixed(1);
                                decodeTimeSum = 0;
                                decodeTimeCount = 0;
                            }
                        }
                        decodeQueue.value = decoder ? decoder.decodeQueueSize : 0;
                    },
                    error: (e) => {
                        log(`Decode error: ${e.message.substring(0, 30)}`);
                        handleDecodeError();
                    }
                });

                await decoder.configure({
                    codec: 'hvc1.1.6.L93.B0',
                    codedWidth: 1920,
                    codedHeight: 1080,
                    description: description,
                    hardwareAcceleration: 'prefer-hardware',
                    optimizeForLatency: true,
                });

                decoderConfigured = true;
                waitingForKeyframe = true;
                errorCount = 0;
                log('Decoder ready');
                return true;
            } catch (e) {
                log(`Config error: ${e.message}`);
                return false;
            }
        }

        function handleDecodeError() {
            errorCount++;
            waitingForKeyframe = true;

            // Check decoder state - if closed, need full recreate
            if (!decoder || decoder.state === 'closed') {
                log(`DecErr #${errorCount}: recreating`);
                decoder = null;
                decoderConfigured = false;
                errorCount = 0;
                // Recreate immediately (setTimeout 0 to not block callback)
                if (vps && sps && pps && !resetPending) {
                    resetPending = true;
                    setTimeout(() => {
                        resetPending = false;
                        configureDecoder();
                    }, 0);
                }
                return;
            }

            log(`DecErr #${errorCount}: state=${decoder.state}`);

            // Decoder still alive, try reset
            if (!resetPending) {
                resetPending = true;
                setTimeout(resetDecoder, 0);
            }
        }

        function resetDecoder() {
            resetPending = false;
            if (!decoder || decoder.state === 'closed') {
                decoderConfigured = false;
                return;
            }

            try {
                decoder.reset();
                // Reconfigure immediately
                if (vps && sps && pps) {
                    const description = buildHvccDescription(vps, sps, pps);
                    decoder.configure({
                        codec: 'hvc1.1.6.L93.B0',
                        codedWidth: 1920,
                        codedHeight: 1080,
                        description: description,
                        hardwareAcceleration: 'prefer-hardware',
                        optimizeForLatency: true,
                    });
                    errorCount = 0;
                    log('Decoder reset OK');
                }
            } catch (e) {
                log(`Reset failed: ${e.message}, recreating`);
                try { decoder.close(); } catch (e2) {}
                decoder = null;
                decoderConfigured = false;
                errorCount = 0;
                // Reconfigure will happen when next VPS/SPS/PPS or keyframe arrives
                if (vps && sps && pps) {
                    configureDecoder();
                }
            }
        }

        function processNalu(nalu) {
            nalCount.value++;
            const nalType = getNalType(nalu);

            if (nalType === NAL.VPS) {
                if (!vps) {
                    vps = nalu;
                    log(`VPS (${nalu.length}B)`);
                    if (sps && pps && !decoderConfigured) configureDecoder();
                }
                return;
            }
            if (nalType === NAL.SPS) {
                if (!sps) {
                    sps = nalu;
                    log(`SPS (${nalu.length}B)`);
                    if (vps && pps && !decoderConfigured) configureDecoder();
                }
                return;
            }
            if (nalType === NAL.PPS) {
                if (!pps) {
                    pps = nalu;
                    log(`PPS (${nalu.length}B)`);
                    if (vps && sps && !decoderConfigured) configureDecoder();
                }
                return;
            }

            if (!isVclNalu(nalType)) {
                return;
            }

            if (!decoderConfigured || !decoder || decoder.state !== 'configured') {
                return;
            }

            const isKeyframe = isIdrFrame(nalType);

            if (waitingForKeyframe && !isKeyframe) {
                return;
            }
            waitingForKeyframe = false;

            // Skip non-keyframes if queue is backing up
            if (decoder.decodeQueueSize > 2) {
                if (!isKeyframe) return;
                // Queue backed up even for keyframe - decoder might be stuck
                if (decoder.decodeQueueSize > 5) {
                    log(`Queue stuck (${decoder.decodeQueueSize}), resetting`);
                    resetPending = true;
                    setTimeout(resetDecoder, 0);
                    return;
                }
            }

            try {
                const hvccNalu = new Uint8Array(4 + nalu.length);
                hvccNalu[0] = (nalu.length >> 24) & 0xFF;
                hvccNalu[1] = (nalu.length >> 16) & 0xFF;
                hvccNalu[2] = (nalu.length >> 8) & 0xFF;
                hvccNalu[3] = nalu.length & 0xFF;
                hvccNalu.set(nalu, 4);

                if (isKeyframe) {
                    log(`IDR NAL${nalType} (${nalu.length}B) queue=${decoder.decodeQueueSize}`);
                }

                decoder.decode(new EncodedVideoChunk({
                    type: isKeyframe ? 'key' : 'delta',
                    timestamp: performance.now() * 1000,
                    data: hvccNalu,
                }));
            } catch (e) {
                log(`Err NAL${nalType}: ${e.message.substring(0, 40)}`);
                handleDecodeError();
            }
        }

        async function checkCodecSupport() {
            if (!('VideoDecoder' in window)) {
                codecStatus.value = 'WebCodecs N/A';
                statusMessage.value = 'WebCodecs not supported. Try Chrome/Safari.';
                return false;
            }

            const codecs = ['hvc1.1.6.L93.B0', 'hvc1.1.6.L120.B0', 'hev1.1.6.L93.B0'];

            for (const codec of codecs) {
                try {
                    const support = await VideoDecoder.isConfigSupported({
                        codec,
                        codedWidth: 1920,
                        codedHeight: 1080,
                        hardwareAcceleration: 'prefer-hardware',
                    });
                    if (support.supported) {
                        codecSupported.value = true;
                        codecStatus.value = 'HEVC OK';
                        log(`Codec: ${codec}`);
                        return true;
                    }
                } catch (e) {}
            }

            codecStatus.value = 'HEVC N/A';
            statusMessage.value = 'HEVC not supported. Try Safari on macOS.';
            return false;
        }

        function connectVideo() {
            videoWs = new WebSocket(`ws://${location.host}/ws/video`);
            videoWs.binaryType = 'arraybuffer';

            videoWs.onopen = () => {
                connected.value = true;
                statusMessage.value = 'Waiting for video...';
                log('Connected');
                emit('connected');
            };

            videoWs.onclose = () => {
                connected.value = false;
                emit('disconnected');
                setTimeout(connectVideo, 2000);
            };

            videoWs.onmessage = (e) => {
                const data = new Uint8Array(e.data);
                if (data.length < 4) return;
                const len = (data[0] << 24) | (data[1] << 16) | (data[2] << 8) | data[3];
                if (data.length >= 4 + len) {
                    processNalu(data.slice(4, 4 + len));
                }
            };
        }

        async function initDecoder() {
            vps = null;
            sps = null;
            pps = null;
            decoderConfigured = false;
            waitingForKeyframe = true;
            frameCount = 0;
            lastFpsTime = 0;
            decodeTimeSum = 0;
            decodeTimeCount = 0;
            errorCount = 0;
            lastErrorTime = 0;
            resetPending = false;

            const supported = await checkCodecSupport();
            if (supported) {
                connectVideo();
            }
        }

        // Lifecycle
        onMounted(() => {
            ctx = videoCanvas.value.getContext('2d');
            initDecoder();
        });

        onBeforeUnmount(() => {
            if (decoder) decoder.close();
            if (videoWs) videoWs.close();
        });

        return {
            videoCanvas,
            connected,
            hasVideo,
            codecSupported,
            codecStatus,
            statusMessage,
            debugLog,
            fps,
            avgDecodeTime,
            decodeQueue,
            nalCount,
            rssiClass,
        };
    },
    template: `
        <div class="video-container">
            <canvas ref="videoCanvas" width="1920" height="1080"></canvas>
            <div v-if="!hasVideo" class="no-video">{{ statusMessage }}</div>

            <div class="overlay overlay-tl">
                <div>RSSI: <span :class="rssiClass">{{ stats?.rssi || '--' }}</span> dBm</div>
                <div>SNR: {{ stats?.snr || '--' }} dB</div>
            </div>

            <div class="overlay overlay-tr">
                <div>FPS: {{ fps }}</div>
                <div>Decode: {{ avgDecodeTime }}ms</div>
                <div>Queue: {{ decodeQueue }}</div>
            </div>

            <div class="overlay overlay-bl">
                <span class="codec-badge" :class="{ supported: codecSupported, unsupported: !codecSupported }">
                    {{ codecStatus }}
                </span>
            </div>

            <div class="overlay overlay-br">
                <div v-for="(line, i) in debugLog" :key="i">{{ line }}</div>
            </div>
        </div>
    `
};
