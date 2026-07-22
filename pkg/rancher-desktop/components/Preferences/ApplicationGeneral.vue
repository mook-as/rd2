<script lang="ts" setup>
import RdCheckbox from '@pkg/components/form/RdCheckbox.vue';
import RdFieldset from '@pkg/components/form/RdFieldset.vue';

defineOptions({ name: 'preferences-application-general' });

// TODO: Move this to i18n.
const sudoAllowedTooltip = `
  If checked, Rancher Desktop will attempt to acquire administrative
  credentials ("sudo access") when starting for some operations.  This
  allows for enhanced functionality, including bridged networking and
  default docker socket support.  Changes will only be applied next time
  Rancher Desktop starts.
`;
</script>

<template>
  <div class="application-general">
    <!-- We don't have sudo access at this point
    <rd-fieldset
      v-if="platform !== 'win32'"
      data-test="administrativeAccess"
      legend-text="Administrative Access"
      :legend-tooltip="sudoAllowedTooltip"
    >
      <rd-checkbox
        label="Allow to acquire administrative credentials (sudo access)"
        :value="isSudoAllowed"
        :is-locked="isPreferenceLocked('application.adminAccess')"
        @update:value="onChange('application.adminAccess', $event)"
      />
    </rd-fieldset>
  -->
    <rd-fieldset
      data-test="automaticUpdates"
      legend-text="Automatic Updates"
    >
      <rd-checkbox
        preference="application.updates.enabled"
        data-test="automaticUpdatesCheckbox"
        label="Check for updates automatically"
      />
    </rd-fieldset>
    <!--
    <rd-fieldset
      data-test="statistics"
      legend-text="Statistics"
    >
      <rd-checkbox
        preference="application.telemetry.enabled"
        label="Allow collection of anonymous statistics to help us improve Rancher Desktop"
      />
    </rd-fieldset>
    -->
  </div>
</template>

<style lang="scss" scoped>
  .application-general {
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }
</style>
