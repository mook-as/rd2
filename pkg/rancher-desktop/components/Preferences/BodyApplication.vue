<script lang="ts" setup>

import { Component, computed, ComputedRef } from 'vue';
import { useStore } from 'vuex';

import PreferencesApplicationBehavior from '@pkg/components/Preferences/ApplicationBehavior.vue';
import PreferencesApplicationEnvironment from '@pkg/components/Preferences/ApplicationEnvironment.vue';
import PreferencesApplicationGeneral from '@pkg/components/Preferences/ApplicationGeneral.vue';
import RdTabbed from '@pkg/components/Tabbed/RdTabbed.vue';
import Tab from '@pkg/components/Tabbed/Tab.vue';

defineOptions({ name: 'preferences-body-application' });

const store = useStore();
const { navigation } = store.state['transient-preferences'];
const activeTab = computed(() => navigation.preferences.application || 'general');

const componentFromTab: ComputedRef<Component> = computed(() => {
  return ({
    general:     PreferencesApplicationGeneral,
    behavior:    PreferencesApplicationBehavior,
    environment: PreferencesApplicationEnvironment,
  } as const)[activeTab.value];
});

function tabSelected({ tab }: { tab: Component }) {
  const newTab = tab.name as typeof activeTab.value;
  if (activeTab.value !== newTab) {
    store.dispatch('transient-preferences/navigate', { 'preferences.application': newTab });
  }
}

</script>

<template>
  <rd-tabbed
    v-bind="$attrs"
    class="action-tabs"
    :no-content="true"
    :default-tab="activeTab"
    @changed="tabSelected"
  >
    <template #tabs>
      <!--
      Environment has nothing yet
      <tab
        v-if="!isPlatformWindows"
        label="Environment"
        name="environment"
        :weight="1"
      />
      -->
      <!--
      <tab
        label="Behavior"
        name="behavior"
        :weight="2"
      />
      -->
      <tab
        label="General"
        name="general"
        :weight="3"
      />
    </template>
    <div class="application-content">
      <component
        v-bind="$attrs"
        :is="componentFromTab"
      />
    </div>
  </rd-tabbed>
</template>

<style lang="scss" scoped>
  .application-content {
    padding: var(--preferences-content-padding);
  }
</style>
