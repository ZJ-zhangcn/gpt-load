<script setup lang="ts">
import { proxiesApi } from "@/api/proxies";
import type { ProxyNode } from "@/types/models";
import { NButton, NCard, NEmpty, NInput, NPopconfirm, NSpin, NTag } from "naive-ui";
import { onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";

const { t } = useI18n();
const proxies = ref<ProxyNode[]>([]);
const proxiesText = ref("");
const loading = ref(false);
const importing = ref(false);
const deletingId = ref<number | null>(null);
const loadFailed = ref(false);

async function loadProxies() {
  loading.value = true;
  loadFailed.value = false;
  try {
    proxies.value = await proxiesApi.list();
  } catch (_error) {
    loadFailed.value = true;
  } finally {
    loading.value = false;
  }
}

async function importProxies() {
  if (!proxiesText.value.trim() || importing.value) {
    return;
  }

  importing.value = true;
  try {
    const result = await proxiesApi.import(proxiesText.value);
    proxiesText.value = "";
    await loadProxies();
    window.$message.success(
      t("proxyPool.importSuccess", { added: result.added_count, ignored: result.ignored_count })
    );
  } finally {
    importing.value = false;
  }
}

async function deleteProxy(proxy: ProxyNode) {
  if (deletingId.value !== null) {
    return;
  }

  deletingId.value = proxy.id;
  try {
    const result = await proxiesApi.delete(proxy.id);
    proxies.value = proxies.value.filter(item => item.id !== proxy.id);
    window.$message.success(t("proxyPool.deleteSuccess", { count: result.unbound_key_count }));
  } finally {
    deletingId.value = null;
  }
}

function formatCreatedAt(value: string) {
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
}

onMounted(() => {
  void loadProxies();
});
</script>

<template>
  <section class="proxy-pool-page" :aria-label="t('proxyPool.title')">
    <header class="page-header">
      <div>
        <p class="eyebrow">{{ t("proxyPool.eyebrow") }}</p>
        <h2>{{ t("proxyPool.title") }}</h2>
        <p>{{ t("proxyPool.description") }}</p>
      </div>
      <n-button secondary :loading="loading" @click="loadProxies">
        {{ t("common.refresh") }}
      </n-button>
    </header>

    <n-card class="nodes-card" :bordered="false">
      <div class="section-heading">
        <div>
          <h3>{{ t("proxyPool.nodesTitle") }}</h3>
          <p>{{ t("proxyPool.nodesCount", { count: proxies.length }) }}</p>
        </div>
        <n-tag type="success" :bordered="false">{{ t("proxyPool.ready") }}</n-tag>
      </div>

      <section class="proxy-import" aria-label="Proxy node import">
        <div class="proxy-import-heading">
          <div>
            <h3>{{ t("proxyPool.importTitle") }}</h3>
            <p>{{ t("proxyPool.importHint") }}</p>
          </div>
          <n-button
            type="primary"
            :loading="importing"
            :disabled="!proxiesText.trim()"
            @click="importProxies"
          >
            {{ t("common.import") }}
          </n-button>
        </div>
        <n-input
          v-model:value="proxiesText"
          type="textarea"
          :placeholder="t('proxyPool.importPlaceholder')"
          :rows="6"
          aria-label="Proxy nodes"
        />
      </section>

      <n-spin :show="loading">
        <div v-if="loadFailed" class="state-block" role="alert">
          <p>{{ t("proxyPool.loadFailed") }}</p>
          <n-button size="small" @click="loadProxies">{{ t("common.refresh") }}</n-button>
        </div>
        <n-empty v-else-if="!loading && proxies.length === 0" :description="t('proxyPool.empty')" />
        <div v-else class="proxy-list">
          <article v-for="proxy in proxies" :key="proxy.id" class="proxy-row">
            <div class="proxy-identity">
              <span class="probe-dot" aria-hidden="true" />
              <div>
                <code>{{ proxy.url }}</code>
                <small>{{ formatCreatedAt(proxy.created_at) }}</small>
              </div>
            </div>
            <n-popconfirm
              :positive-text="t('common.delete')"
              :negative-text="t('common.cancel')"
              @positive-click="deleteProxy(proxy)"
            >
              <template #trigger>
                <n-button size="small" type="error" tertiary :loading="deletingId === proxy.id">
                  {{ t("common.delete") }}
                </n-button>
              </template>
              {{ t("proxyPool.deleteConfirm") }}
            </n-popconfirm>
          </article>
        </div>
      </n-spin>
    </n-card>
  </section>
</template>

<style scoped>
.proxy-pool-page {
  display: grid;
  gap: 18px;
  margin: 0 auto;
  max-width: 1080px;
}

.page-header,
.section-heading,
.proxy-row,
.proxy-identity {
  display: flex;
  align-items: center;
}

.page-header,
.section-heading,
.proxy-row {
  justify-content: space-between;
  gap: 16px;
}

.page-header h2,
.section-heading h3,
.page-header p,
.section-heading p {
  margin: 0;
}

.page-header h2 {
  color: var(--text-color-1);
  font-size: clamp(24px, 3vw, 30px);
  letter-spacing: -0.03em;
}

.page-header > div > p:last-child,
.section-heading p {
  color: var(--text-color-3);
  line-height: 1.55;
  margin-top: 6px;
}

.eyebrow {
  color: var(--primary-color);
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.1em;
  text-transform: uppercase;
}

.nodes-card {
  border: 1px solid var(--border-color);
  box-shadow: none;
}

.nodes-card :deep(.n-card__content) {
  display: grid;
  gap: 18px;
}

.proxy-import {
  display: grid;
  gap: 14px;
  padding: 2px 0 20px;
  border-bottom: 1px solid var(--border-color);
}

.proxy-import-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}

.proxy-import-heading h3,
.proxy-import-heading p {
  margin: 0;
}

.proxy-import-heading p {
  color: var(--text-color-3);
  line-height: 1.55;
  margin-top: 6px;
}

.proxy-list {
  display: grid;
  gap: 8px;
}

.proxy-row {
  min-height: 62px;
  padding: 10px 12px;
  border: 1px solid var(--border-color);
  border-radius: 10px;
  background: var(--card-bg-solid);
}

.proxy-identity {
  min-width: 0;
  gap: 12px;
}

.proxy-identity code {
  display: block;
  overflow-wrap: anywhere;
  color: var(--text-color-1);
}

.proxy-identity small {
  color: var(--text-color-3);
  font-size: 12px;
}

.probe-dot {
  flex: 0 0 auto;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--success-color);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--success-color) 16%, transparent);
}

.state-block {
  display: grid;
  justify-items: start;
  gap: 8px;
  padding: 24px 0;
}

.state-block p {
  margin: 0;
}

@media (max-width: 640px) {
  .page-header,
  .section-heading,
  .proxy-import-heading,
  .proxy-row {
    align-items: flex-start;
    flex-direction: column;
  }

  .page-header > .n-button,
  .section-heading > .n-button,
  .proxy-import-heading > .n-button,
  .proxy-row :deep(.n-button) {
    width: 100%;
  }
}
</style>
