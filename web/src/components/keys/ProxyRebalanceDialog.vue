<script setup lang="ts">
import { proxiesApi } from "@/api/proxies";
import type { ProxyNode } from "@/types/models";
import { NButton, NCard, NCheckbox, NCheckboxGroup, NEmpty, NModal, NSpin } from "naive-ui";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

interface Props {
  show: boolean;
  groupId: number;
  groupName?: string;
}

interface Emits {
  (e: "update:show", value: boolean): void;
  (e: "success"): void;
}

const props = defineProps<Props>();
const emit = defineEmits<Emits>();
const { t } = useI18n();

const proxies = ref<ProxyNode[]>([]);
const selectedProxyIds = ref<number[]>([]);
const loading = ref(false);
const submitting = ref(false);
const loadFailed = ref(false);
let requestGeneration = 0;

const canSubmit = computed(
  () =>
    props.groupId > 0 && selectedProxyIds.value.length > 0 && !loading.value && !loadFailed.value
);

watch(
  () => props.show,
  show => {
    if (show) {
      void loadProxies();
    } else {
      requestGeneration++;
    }
  },
  { immediate: true }
);

async function loadProxies() {
  const generation = ++requestGeneration;
  loading.value = true;
  loadFailed.value = false;

  try {
    const nodes = await proxiesApi.list();
    if (generation !== requestGeneration || !props.show) {
      return;
    }
    proxies.value = nodes;
    selectedProxyIds.value = nodes.map(node => node.id);
  } catch (_error) {
    if (generation !== requestGeneration || !props.show) {
      return;
    }
    loadFailed.value = true;
    proxies.value = [];
    selectedProxyIds.value = [];
  } finally {
    if (generation === requestGeneration && props.show) {
      loading.value = false;
    }
  }
}

function close() {
  if (!submitting.value) {
    emit("update:show", false);
  }
}

async function submit() {
  if (!canSubmit.value || submitting.value) {
    return;
  }

  submitting.value = true;
  try {
    const result = await proxiesApi.rebalance(props.groupId, selectedProxyIds.value);
    window.$message.success(t("proxyPool.rebalanceSuccess", { count: result.bound_key_count }));
    emit("success");
    emit("update:show", false);
  } finally {
    submitting.value = false;
  }
}
</script>

<template>
  <n-modal
    :show="show"
    :mask-closable="!submitting"
    :close-on-esc="!submitting"
    @update:show="next => !next && close()"
  >
    <n-card
      class="rebalance-dialog"
      :title="t('proxyPool.rebalanceTitle')"
      :bordered="false"
      role="dialog"
      aria-modal="true"
      :aria-label="t('proxyPool.rebalanceTitle')"
    >
      <p class="dialog-description">
        {{ t("proxyPool.rebalanceDescription", { group: groupName || groupId }) }}
      </p>

      <n-spin :show="loading">
        <div v-if="loadFailed" class="dialog-state" role="alert">
          {{ t("proxyPool.loadFailed") }}
          <n-button text type="primary" @click="loadProxies">{{ t("common.refresh") }}</n-button>
        </div>
        <n-empty
          v-else-if="!loading && proxies.length === 0"
          :description="t('proxyPool.emptyForRebalance')"
        />
        <n-checkbox-group
          v-else-if="!loading"
          v-model:value="selectedProxyIds"
          class="proxy-options"
        >
          <n-checkbox
            v-for="proxy in proxies"
            :key="proxy.id"
            :value="proxy.id"
            class="proxy-option"
          >
            <code>{{ proxy.url }}</code>
          </n-checkbox>
        </n-checkbox-group>
      </n-spin>

      <template #footer>
        <div class="dialog-footer">
          <span class="selection-count">
            {{ t("proxyPool.selectedNodes", { count: selectedProxyIds.length }) }}
          </span>
          <div class="dialog-actions">
            <n-button :disabled="submitting" @click="close">{{ t("common.cancel") }}</n-button>
            <n-button type="primary" :loading="submitting" :disabled="!canSubmit" @click="submit">
              {{ t("proxyPool.assignEvenly") }}
            </n-button>
          </div>
        </div>
      </template>
    </n-card>
  </n-modal>
</template>

<style scoped>
.rebalance-dialog {
  width: min(560px, calc(100vw - 32px));
}

.dialog-description {
  margin: 0 0 16px;
  color: var(--text-color-2);
  line-height: 1.6;
}

.proxy-options {
  display: grid;
  gap: 8px;
  max-height: min(48vh, 360px);
  overflow-y: auto;
  padding: 4px;
}

.proxy-option {
  display: flex;
  align-items: center;
  min-height: 38px;
  padding: 8px 10px;
  border: 1px solid var(--border-color);
  border-radius: 8px;
  background: var(--card-bg-solid);
}

.proxy-option code {
  overflow-wrap: anywhere;
}

.dialog-state {
  display: grid;
  justify-items: start;
  gap: 8px;
  padding: 20px 0;
}

.dialog-footer,
.dialog-actions {
  display: flex;
  align-items: center;
  gap: 12px;
}

.dialog-footer {
  justify-content: space-between;
}

.selection-count {
  color: var(--text-color-3);
  font-size: 13px;
}

@media (max-width: 640px) {
  .dialog-footer {
    align-items: flex-start;
    flex-direction: column;
  }

  .dialog-actions {
    width: 100%;
  }

  .dialog-actions :deep(.n-button) {
    flex: 1;
  }
}
</style>
