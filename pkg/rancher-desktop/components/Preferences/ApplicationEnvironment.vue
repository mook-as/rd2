<script setup lang="ts">
import { computed, onMounted, onUnmounted } from 'vue';
import { useStore } from 'vuex';

import PathManagementSelector from '@pkg/components/PathManagementSelector.vue';
import RdFieldset from '@pkg/components/form/RdFieldset.vue';
import { Settings } from '@pkg/config/settings';
import { ipcRenderer } from '@pkg/utils/ipcRenderer';
import { RecursiveTypes } from '@pkg/utils/typeUtils';

defineOptions({ name: 'preferences-application-environment' });

const { preferences } = defineProps<{
  preferences: Settings
}>();

const store = useStore();
const t = computed(() => store.getters['i18n/t']);
const isPreferenceLocked = computed(() => store.getters['preferences/isPreferenceLocked']);

function onSettingsRead(_: unknown, currentSettings: Settings) {
  store.dispatch('preferences/updatePreferencesData', {
    property: 'application.pathManagementStrategy',
    value:    currentSettings.application.pathManagementStrategy,
  });
}

onMounted(() => {
  ipcRenderer.on('settings-read', onSettingsRead);
  ipcRenderer.send('settings-read');
});

onUnmounted(() => {
  ipcRenderer.removeListener('settings-read', onSettingsRead);
});

function onChange<P extends keyof RecursiveTypes<Settings>>(
  property: P,
  value: RecursiveTypes<Settings>[P],
) {
  store.dispatch('preferences/updatePreferencesData', { property, value });
}
</script>

<template>
  <rd-fieldset
    data-testid="pathManagement"
    :legend-text="t('pathManagement.label')"
    :legend-tooltip="t('pathManagement.tooltip')"
    :is-locked="isPreferenceLocked('application.addPath')"
  >
    <template #default="{ isLocked }">
      <path-management-selector
        :show-label="false"
        :value="preferences.application.pathManagementStrategy"
        :is-locked="isLocked"
        @input="onChange('application.pathManagementStrategy', $event)"
      />
    </template>
  </rd-fieldset>
</template>
