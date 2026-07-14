<script lang="ts" setup>

import RdCheckbox from '@pkg/components/form/RdCheckbox.vue';
import type { RecursiveLeafKeysOfType } from '@pkg/utils/typeUtils';

import type { IoRancherdesktopAppV1alpha1AppSpec as AppSpec } from '@rdd-client';
import type { PropType } from 'vue';

const { preference } = defineProps({
  preference: {
    type:     String as PropType<RecursiveLeafKeysOfType<AppSpec, boolean | undefined>>,
    required: true,
  },
});
</script>

<template>
  <div class="system-preferences">
    <div class="checkbox">
      <rd-checkbox
        :preference="preference"
        :immediate="true"
        label="Allow collection of anonymous statistics to help us improve Rancher Desktop"
      />
      <p class="fineprint">
        Send anonymized usage info, error reports, etc. to help improve Rancher Desktop. Your data will not be shared with anyone else, and no information about what specific resources or endpoints you are deploying is included.
      </p>
    </div>
  </div>
</template>

<style scoped>
p.fineprint {
  font-size: small;
  margin-top: 2px;
  color: var(--input-label);
}
</style>
