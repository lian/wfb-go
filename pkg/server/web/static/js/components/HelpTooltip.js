// HelpTooltip.js - Help icon with hover tooltip (fixed positioning to avoid clipping)
const { ref, onMounted, onUnmounted } = Vue;

export default {
    name: 'HelpTooltip',
    props: {
        text: { type: String, required: true },
        defaultValue: { type: String, default: '' },
        tuning: { type: String, default: '' }
    },
    setup(props) {
        const triggerRef = ref(null);
        const tooltipRef = ref(null);
        const isVisible = ref(false);
        const position = ref({ top: 0, left: 0 });

        function show() {
            if (!triggerRef.value || !tooltipRef.value) return;
            isVisible.value = true;

            // Wait for tooltip to be visible before calculating position
            requestAnimationFrame(() => {
                const trigger = triggerRef.value.getBoundingClientRect();
                const tooltip = tooltipRef.value.getBoundingClientRect();

                // Position above the trigger, centered
                let top = trigger.top - tooltip.height - 8;
                let left = trigger.left + trigger.width / 2 - tooltip.width / 2;

                // If it would go off the top, position below instead
                if (top < 10) {
                    top = trigger.bottom + 8;
                }

                // Keep within horizontal bounds
                if (left < 10) left = 10;
                if (left + tooltip.width > window.innerWidth - 10) {
                    left = window.innerWidth - tooltip.width - 10;
                }

                position.value = { top, left };
            });
        }

        function hide() {
            isVisible.value = false;
        }

        return { triggerRef, tooltipRef, isVisible, position, show, hide };
    },
    template: `
        <span class="help-tooltip-trigger" ref="triggerRef" @mouseenter="show" @mouseleave="hide">
            <span class="help-icon">?</span>
            <teleport to="body">
                <div
                    v-show="isVisible"
                    ref="tooltipRef"
                    class="help-tooltip-fixed"
                    :style="{ top: position.top + 'px', left: position.left + 'px' }"
                >
                    <div class="help-text">{{ text }}</div>
                    <div v-if="defaultValue" class="help-default">
                        <strong>Default:</strong> {{ defaultValue }}
                    </div>
                    <div v-if="tuning" class="help-tuning">
                        <strong>Tuning:</strong> {{ tuning }}
                    </div>
                </div>
            </teleport>
        </span>
    `
};
