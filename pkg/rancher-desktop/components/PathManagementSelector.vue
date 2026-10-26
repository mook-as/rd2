<script setup lang="ts">
import { computed } from 'vue';
import { useStore } from 'vuex';

import RadioButton from '@pkg/components/form/RadioButton.vue';
import RadioGroup from '@pkg/components/form/RadioGroup.vue';

defineOptions({ name: 'path-management-selector' });

interface PathManagementOption {
  label:       string,
  value:       string,
  description: string
}

const {
  value = 'rcFiles',
  row = false,
  showLabel = true,
  isLocked = false,
} = defineProps<{
  value?:     string,
  row?:       boolean,
  showLabel?: boolean,
  isLocked?:  boolean
}>();

const emit = defineEmits({
  input: (value: string) => true,
});

const store = useStore();
const t = computed(() => store.getters['i18n/t']);

const options = computed<PathManagementOption[]>(() => [
  {
    label:       t.value('pathManagement.options.rcFiles.label'),
    value:       'rcFiles',
    description: t.value('pathManagement.options.rcFiles.description'),
  },
  {
    label:       t.value('pathManagement.options.manual.label'),
    value:       'manual',
    description: t.value('pathManagement.options.manual.description'),
  },
]);

const groupName = 'pathManagement';

const label = computed(() => showLabel ? t.value('pathManagement.label') : '');
const tooltip = computed(() => showLabel ? t.value('pathManagement.tooltip') : '');

function updateVal(value: string) {
  emit('input', value);
}
</script>

<template>
  <radio-group
    :name="groupName"
    :label="label"
    :tooltip="tooltip"
    :value="value"
    :options="options"
    :row="row"
    :disabled="isLocked"
    :class="{ 'locked-radio': isLocked }"
    class="path-management"
    @update:value="updateVal"
  >
    <template
      v-if="showLabel"
      #label
    >
      <slot name="label" />
    </template>
    <template #1="{ option, isDisabled, mode }">
      <radio-button
        v-bind="$attrs"
        :key="groupName + '-' + option.value"
        :name="groupName"
        :value="value"
        :label="option.label"
        :val="option.value"
        :disabled="isDisabled"
        :mode="mode"
        @update:value="updateVal(option.value)"
      >
        <template #description>
          <span v-html="option.description" />
        </template>
      </radio-button>
    </template>
  </radio-group>
</template>

<style lang="scss" scoped>
.path-management :deep(code) {
  user-select: text;
  cursor: text;
  padding: 2px;
}

.path-management :deep(label) {
  color: var(--input-label);
}
</style>
