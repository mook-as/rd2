<script lang="ts" setup>
import RdCheckbox from '@pkg/components/form/RdCheckbox.vue';
import RdFieldset from '@pkg/components/form/RdFieldset.vue';

defineOptions({ name: 'preferences-body-kubernetes' });
</script>

<template>
  <div class="preferences-body">
    <rd-fieldset
      data-test="kubernetesToggle"
      legend-text="Kubernetes"
    >
      <rd-checkbox
        label="Enable Kubernetes"
        preference="kubernetes.enabled"
      />
    </rd-fieldset>
    <!--
    <rd-fieldset
      data-test="kubernetesVersion"
      class="width-xs"
      :legend-text="kubernetesVersionLabel"
    >
      <rd-select
        class="select-k8s-version"
        :model-value="kubernetesVersion"
        :disabled="isKubernetesDisabled"
        :is-locked="isPreferenceLocked('kubernetes.version')"
        @change="onChange('kubernetes.version', $event.target.value)"
      >
    -->
    <!--
            - On macOS Chrome / Electron can't style the <option> elements.
            - We do the best we can by instead using <optgroup> for a recommended section.
            -->
    <!--
        <optgroup
          v-if="recommendedVersions.length > 0"
          label="Recommended Versions"
        >
          <option
            v-for="item in recommendedVersions"
            :key="item.version"
            :value="item.version"
            :selected="item.version === defaultVersion.version"
          >
            {{ versionName(item) }}
          </option>
        </optgroup>
        <optgroup
          v-if="nonRecommendedVersions.length > 0"
          label="Other Versions"
        >
          <option
            v-for="item in nonRecommendedVersions"
            :key="item.version"
            :value="item.version"
            :selected="item.version === defaultVersion.version"
          >
            v{{ item.version }}
          </option>
        </optgroup>
      </rd-select>
    </rd-fieldset>
    <rd-fieldset
      data-test="kubernetesPort"
      class="width-xs"
      legend-text="Kubernetes Port"
    >
      <rd-input
        type="number"
        :disabled="isKubernetesDisabled"
        :value="preferences.kubernetes.port"
        :is-locked="isPreferenceLocked('kubernetes.port')"
        @input="onChange('kubernetes.port', castToNumber($event.target.value))"
      />
    </rd-fieldset>
    <rd-fieldset
      data-test="kubernetesOptions"
      legend-text="Options"
    >
      <rd-checkbox
        label="Enable Traefik"
        :disabled="isKubernetesDisabled"
        :value="preferences.kubernetes.options.traefik"
        :is-locked="isPreferenceLocked('kubernetes.options.traefik')"
        @update:value="onChange('kubernetes.options.traefik', $event)"
      />
    -->
    <!-- Don't disable Spinkube option when Wasm is disabled; let validation deal with it  -->
    <!--
      <rd-checkbox
        label="Install Spin Operator"
        :disabled="isKubernetesDisabled"
        :value="preferences.experimental.kubernetes.options.spinkube"
        :is-locked="isPreferenceLocked('experimental.kubernetes.options.spinkube')"
        :is-experimental="true"
        @update:value="onChange('experimental.kubernetes.options.spinkube', $event)"
      >
        <template
          v-if="spinOperatorIncompatible"
          #below
        >
          <banner color="warning">
            Spin operator requires
            <a
              href="#"
              @click.prevent="($root as any).navigate('Container Engine', 'general')"
            >WebAssembly</a>
            to be enabled.
          </banner>
        </template>
      </rd-checkbox>
    </rd-fieldset>
    -->
  </div>
</template>

<style lang="scss" scoped>
  .checkbox-title {
    font-size: 1rem;
    line-height: 1.5rem;
    padding-bottom: 0.5rem;
  }

  .preferences-body {
    padding: var(--preferences-content-padding);
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  .width-xs {
    max-width: 20rem;
    min-width: 20rem;
  }
</style>
