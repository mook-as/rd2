<script setup lang="ts">
import { onMounted } from 'vue';
import { useStore } from 'vuex';

import { ipcRenderer } from '@pkg/utils/ipcRenderer';

const store = useStore();

defineOptions({ name: 'preferences-layout' });

onMounted(() => {
  store.dispatch('i18n/init').catch(ex => console.error(ex));
  ipcRenderer.send('preferences/load');
});
</script>

<template>
  <div class="wrapper">
    <RouterView />
  </div>
</template>

<style lang="scss" src="@pkg/assets/styles/app.scss"></style>
<style lang="scss" scoped>
.wrapper {
  background-color: var(--body-bg);
}
</style>
