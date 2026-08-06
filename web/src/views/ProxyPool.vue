<script setup lang="ts">
import {
  AlertCircleOutline,
  CheckmarkCircleOutline,
  CloudUploadOutline,
  CodeSlashOutline,
  DownloadOutline,
  FilterOutline,
  LayersOutline,
  PulseOutline,
  RefreshOutline,
  SearchOutline,
  ServerOutline,
  TimeOutline,
  TrashOutline,
} from "@vicons/ionicons5";
import { proxiesApi } from "@/api/proxies";
import type { ProxyCheckStatus, ProxyNode } from "@/types/models";
import {
  NButton,
  NCard,
  NIcon,
  NInput,
  NPagination,
  NPopconfirm,
  NSelect,
  NSpin,
  NTag,
} from "naive-ui";
import { computed, nextTick, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

type ProxyStatusFilter = "all" | ProxyCheckStatus;

const { t } = useI18n();
const proxies = ref<ProxyNode[]>([]);
const proxiesText = ref("");
const loading = ref(false);
const importing = ref(false);
const checking = ref(false);
const checkingIds = ref(new Set<number>());
const deletingId = ref<number | null>(null);
const deleting = ref(false);
const selectedProxyIds = ref(new Set<number>());
const loadFailed = ref(false);
const searchText = ref("");
const protocolFilter = ref("all");
const statusFilter = ref<ProxyStatusFilter>("all");
const filterMode = ref<"all" | "imported" | "pending">("all");
const pageSize = ref(8);
const currentPage = ref(1);

const protocolOptions = computed(() => [
  { label: t("proxyPool.allProtocols"), value: "all" },
  { label: "HTTP", value: "HTTP" },
  { label: "HTTPS", value: "HTTPS" },
  { label: "SOCKS5", value: "SOCKS5" },
]);

function parseProxyUrl(value: string) {
  const normalized = value.includes("://") ? value : `http://${value}`;

  try {
    const parsed = new URL(normalized);
    const protocol = parsed.protocol.replace(":", "").toUpperCase();
    const host = `${parsed.hostname}${parsed.port ? `:${parsed.port}` : ""}`;
    return {
      protocol,
      host,
      safeUrl: `${parsed.protocol}//${host}`,
    };
  } catch (_error) {
    return {
      protocol: "CUSTOM",
      host: value,
      safeUrl: value,
    };
  }
}

function formatCreatedAt(value: string, short = false) {
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: short ? "short" : "medium",
    timeStyle: short ? undefined : "short",
  }).format(new Date(value));
}

const protocolCounts = computed(() => {
  const counts = new Map<string, number>();
  for (const proxy of proxies.value) {
    const protocol = parseProxyUrl(proxy.url).protocol;
    counts.set(protocol, (counts.get(protocol) ?? 0) + 1);
  }
  return counts;
});

const protocolTypeCount = computed(() => protocolCounts.value.size);
const latestImportLabel = computed(() => {
  if (proxies.value.length === 0) {
    return t("proxyPool.notImported");
  }

  const latest = proxies.value.reduce((current, candidate) => {
    return new Date(candidate.created_at).getTime() > new Date(current.created_at).getTime()
      ? candidate
      : current;
  });
  return formatCreatedAt(latest.created_at, true);
});

const checkSummary = computed(() => {
  const checked = proxies.value.filter(proxy => proxy.check_status !== "unchecked").length;
  const healthy = proxies.value.filter(proxy => proxy.check_status === "up").length;
  const unhealthy = proxies.value.filter(proxy => proxy.check_status === "down").length;
  return { checked, healthy, unhealthy };
});

const checkOverallState = computed(() => {
  if (checkSummary.value.checked === 0) {
    return "pending";
  }
  return checkSummary.value.unhealthy > 0 ? "degraded" : "active";
});

function checkStatusLabel(status: ProxyCheckStatus) {
  return t(`proxyPool.checkStatus.${status}`);
}

const statusOptions = computed(() => [
  { label: t("proxyPool.allStatuses"), value: "all" as const },
  ...(["unchecked", "up", "down"] as ProxyCheckStatus[]).map(status => ({
    label: checkStatusLabel(status),
    value: status,
  })),
]);

const overviewStats = computed(() => [
  {
    key: "total",
    label: t("proxyPool.totalNodes"),
    value: proxies.value.length,
    hint: t("proxyPool.currentInventory"),
    icon: LayersOutline,
    tone: "primary",
  },
  {
    key: "bindable",
    label: t("proxyPool.bindableNodes"),
    value: proxies.value.length,
    hint: t("proxyPool.readyToBind"),
    icon: CheckmarkCircleOutline,
    tone: "success",
  },
  {
    key: "protocols",
    label: t("proxyPool.protocolTypes"),
    value: protocolTypeCount.value,
    hint: t("proxyPool.protocolCount"),
    icon: CodeSlashOutline,
    tone: "info",
  },
  {
    key: "latest",
    label: t("proxyPool.latestImport"),
    value: latestImportLabel.value,
    hint: t("proxyPool.currentInventory"),
    icon: TimeOutline,
    tone: "warning",
  },
]);

const filteredProxies = computed(() => {
  const query = searchText.value.trim().toLowerCase();

  return proxies.value.filter(proxy => {
    const parsed = parseProxyUrl(proxy.url);
    const matchesSearch =
      !query ||
      proxy.url.toLowerCase().includes(query) ||
      parsed.host.toLowerCase().includes(query);
    const matchesProtocol =
      protocolFilter.value === "all" || parsed.protocol === protocolFilter.value;
    const matchesStatus = statusFilter.value === "all" || proxy.check_status === statusFilter.value;

    const matchesMode =
      filterMode.value === "all" ||
      filterMode.value === "imported" ||
      (filterMode.value === "pending" && proxy.check_status === "unchecked");
    return matchesSearch && matchesProtocol && matchesStatus && matchesMode;
  });
});

const pageCount = computed(() =>
  Math.max(1, Math.ceil(filteredProxies.value.length / pageSize.value))
);
const pageRangeStart = computed(() =>
  filteredProxies.value.length === 0 ? 0 : (currentPage.value - 1) * pageSize.value + 1
);
const pageRangeEnd = computed(() =>
  Math.min(currentPage.value * pageSize.value, filteredProxies.value.length)
);
const visibleRows = computed(() => {
  const start = (currentPage.value - 1) * pageSize.value;
  return filteredProxies.value.slice(start, start + pageSize.value).map(proxy => {
    const parsed = parseProxyUrl(proxy.url);
    return {
      ...proxy,
      ...parsed,
      createdLabel: formatCreatedAt(proxy.created_at),
    };
  });
});

const selectedCount = computed(() => selectedProxyIds.value.size);
const allVisibleSelected = computed(
  () =>
    visibleRows.value.length > 0 &&
    visibleRows.value.every(row => selectedProxyIds.value.has(row.id))
);
const someVisibleSelected = computed(
  () =>
    visibleRows.value.some(row => selectedProxyIds.value.has(row.id)) && !allVisibleSelected.value
);
const allFilteredSelected = computed(
  () =>
    filteredProxies.value.length > 0 &&
    filteredProxies.value.every(proxy => selectedProxyIds.value.has(proxy.id))
);

watch([searchText, protocolFilter, filterMode, statusFilter], () => {
  currentPage.value = 1;
  clearSelection();
});

watch(pageCount, count => {
  if (currentPage.value > count) {
    currentPage.value = count;
  }
});

async function loadProxies() {
  loading.value = true;
  loadFailed.value = false;
  try {
    proxies.value = await proxiesApi.list();
    clearSelection();
  } catch (_error) {
    loadFailed.value = true;
  } finally {
    loading.value = false;
  }
}

async function checkAll() {
  if (checking.value || proxies.value.length === 0) {
    return;
  }

  checking.value = true;
  try {
    const result = await proxiesApi.check();
    mergeCheckedNodes(result.nodes);
    window.$message.success(
      t("proxyPool.checkSuccess", {
        checked: result.checked_count,
        healthy: result.healthy_count,
        unhealthy: result.unhealthy_count,
      })
    );
  } catch (_error) {
    window.$message.error(t("proxyPool.checkFailed"));
  } finally {
    checking.value = false;
  }
}

async function checkProxy(proxy: ProxyNode) {
  if (checking.value || checkingIds.value.has(proxy.id)) {
    return;
  }

  const next = new Set(checkingIds.value);
  next.add(proxy.id);
  checkingIds.value = next;
  try {
    const result = await proxiesApi.check([proxy.id]);
    mergeCheckedNodes(result.nodes);
    window.$message.success(
      t("proxyPool.checkSuccess", {
        checked: result.checked_count,
        healthy: result.healthy_count,
        unhealthy: result.unhealthy_count,
      })
    );
  } catch (_error) {
    window.$message.error(t("proxyPool.checkFailed"));
  } finally {
    const remaining = new Set(checkingIds.value);
    remaining.delete(proxy.id);
    checkingIds.value = remaining;
  }
}

function mergeCheckedNodes(nodes: ProxyNode[]) {
  const checkedById = new Map(nodes.map(node => [node.id, node]));
  proxies.value = proxies.value.map(node => checkedById.get(node.id) ?? node);
}

function clearSelection() {
  selectedProxyIds.value = new Set<number>();
}

function toggleProxySelection(proxyId: number, checked: boolean) {
  const next = new Set(selectedProxyIds.value);
  if (checked) {
    next.add(proxyId);
  } else {
    next.delete(proxyId);
  }
  selectedProxyIds.value = next;
}

function togglePageSelection(checked: boolean) {
  const next = new Set(selectedProxyIds.value);
  for (const row of visibleRows.value) {
    if (checked) {
      next.add(row.id);
    } else {
      next.delete(row.id);
    }
  }
  selectedProxyIds.value = next;
}

function selectAllFiltered() {
  selectedProxyIds.value = new Set(filteredProxies.value.map(proxy => proxy.id));
}

function onPageSelectionChange(event: Event) {
  togglePageSelection((event.target as HTMLInputElement).checked);
}

function onProxySelectionChange(proxyId: number, event: Event) {
  toggleProxySelection(proxyId, (event.target as HTMLInputElement).checked);
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
  if (deleting.value) {
    return;
  }

  deleting.value = true;
  deletingId.value = proxy.id;
  try {
    const result = await proxiesApi.delete(proxy.id);
    proxies.value = proxies.value.filter(item => item.id !== proxy.id);
    toggleProxySelection(proxy.id, false);
    await loadProxies();
    window.$message.success(t("proxyPool.deleteSuccess", { count: result.unbound_key_count }));
  } finally {
    deletingId.value = null;
    deleting.value = false;
  }
}

async function deleteSelectedProxies() {
  const targetIds = Array.from(selectedProxyIds.value);
  if (targetIds.length === 0 || deleting.value) {
    return;
  }

  deleting.value = true;
  try {
    const result = await proxiesApi.deleteMany(targetIds);
    const targetSet = new Set(targetIds);
    proxies.value = proxies.value.filter(proxy => !targetSet.has(proxy.id));
    await loadProxies();
    window.$message.success(
      t("proxyPool.batchDeleteSuccess", {
        deleted: result.deleted_count,
        ignored: result.ignored_count,
        unbound: result.unbound_key_count,
      })
    );
  } catch (_error) {
    window.$message.error(t("proxyPool.batchDeleteFailed"));
  } finally {
    deleting.value = false;
  }
}

function resetFilters() {
  searchText.value = "";
  protocolFilter.value = "all";
  statusFilter.value = "all";
  filterMode.value = "all";
}

function fillTemplate() {
  proxiesText.value = [
    "# one proxy node per line",
    "socks5://host:1080",
    "http://host:3128",
    "https://host:8443",
  ].join("\n");
}

function openQuickImport() {
  fillTemplate();
  void nextTick(() => {
    const textarea = document.querySelector<HTMLTextAreaElement>(".quick-import-panel textarea");
    textarea?.scrollIntoView({ behavior: "smooth", block: "center" });
    textarea?.focus();
  });
}

function downloadTemplate() {
  const content = `${proxiesText.value || "# one proxy node per line\nhttp://host:3128\nsocks5://host:1080"}\n`;
  const blob = new Blob([content], { type: "text/plain;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = "gpt-load-proxy-template.txt";
  link.click();
  URL.revokeObjectURL(url);
}

onMounted(() => {
  void loadProxies();
});
</script>

<template>
  <section class="proxy-pool-page" :aria-label="t('proxyPool.title')">
    <header class="page-header">
      <div class="page-heading">
        <div class="page-title-group">
          <span class="proxy-page-logo" role="img" :aria-label="t('proxyPool.logoLabel')">
            <span class="proxy-logo-mark" aria-hidden="true">
              <i />
              <i />
              <i />
            </span>
            <span class="proxy-logo-word">{{ t("proxyPool.logoWordmark") }}</span>
          </span>
          <div class="page-heading-copy">
            <p class="eyebrow">{{ t("proxyPool.eyebrow") }}</p>
            <h2>{{ t("proxyPool.title") }}</h2>
          </div>
        </div>
        <p>{{ t("proxyPool.description") }}</p>
      </div>
      <div class="page-actions">
        <n-button secondary :loading="loading" @click="loadProxies">
          <template #icon>
            <n-icon><refresh-outline /></n-icon>
          </template>
          {{ t("common.refresh") }}
        </n-button>
        <n-button type="primary" @click="openQuickImport">
          <template #icon>
            <n-icon><cloud-upload-outline /></n-icon>
          </template>
          {{ t("proxyPool.quickImportAction") }}
        </n-button>
      </div>
    </header>

    <section class="proxy-overview" :aria-label="t('proxyPool.overviewTitle')">
      <n-card
        v-for="stat in overviewStats"
        :key="stat.key"
        class="proxy-stat-card"
        :class="stat.tone"
        :bordered="false"
      >
        <div class="stat-topline">
          <span class="stat-icon">
            <n-icon :size="19"><component :is="stat.icon" /></n-icon>
          </span>
          <span class="stat-kicker">{{ stat.hint }}</span>
        </div>
        <strong class="stat-value" :class="{ 'stat-value-date': stat.key === 'latest' }">
          {{ stat.value }}
        </strong>
        <span class="stat-label">{{ stat.label }}</span>
        <span class="stat-progress" aria-hidden="true"><i /></span>
      </n-card>
    </section>

    <section class="proxy-workspace">
      <n-card class="proxy-list-panel" :bordered="false">
        <div class="panel-heading">
          <div>
            <div class="heading-line">
              <h3>{{ t("proxyPool.nodesTitle") }}</h3>
              <n-tag size="small" type="info" :bordered="false">{{ proxies.length }}</n-tag>
            </div>
            <p>{{ t("proxyPool.nodesHint") }}</p>
          </div>
          <span class="panel-note">{{ t("proxyPool.nodesCardHint") }}</span>
        </div>

        <div class="list-tabs" role="tablist" :aria-label="t('proxyPool.nodesTitle')">
          <button
            v-for="tab in [
              { key: 'all', label: t('proxyPool.allTab') },
              { key: 'imported', label: t('proxyPool.importedTab') },
              { key: 'pending', label: t('proxyPool.pendingTab') },
            ]"
            :key="tab.key"
            class="list-tab"
            :class="{ active: filterMode === tab.key }"
            type="button"
            role="tab"
            :aria-selected="filterMode === tab.key"
            @click="filterMode = tab.key as 'all' | 'imported' | 'pending'"
          >
            {{ tab.label }}
          </button>
        </div>

        <div class="filter-toolbar">
          <n-input
            v-model:value="searchText"
            clearable
            :placeholder="t('proxyPool.searchPlaceholder')"
            class="search-input"
          >
            <template #prefix>
              <n-icon><search-outline /></n-icon>
            </template>
          </n-input>
          <n-select
            v-model:value="protocolFilter"
            :options="protocolOptions"
            class="protocol-select"
          />
          <n-select
            v-model:value="statusFilter"
            :options="statusOptions"
            :aria-label="t('proxyPool.nodeStatus')"
            class="status-select"
          />
          <n-button quaternary @click="resetFilters">
            <template #icon>
              <n-icon><filter-outline /></n-icon>
            </template>
            {{ t("proxyPool.resetFilters") }}
          </n-button>
        </div>

        <div v-if="filteredProxies.length > 0" class="selection-toolbar">
          <div class="selection-leading">
            <label class="selection-label">
              <input
                type="checkbox"
                :checked="allVisibleSelected"
                :indeterminate="someVisibleSelected"
                :aria-label="t('proxyPool.selectPage')"
                @change="onPageSelectionChange"
              />
              <span>{{ t("proxyPool.selectPage") }}</span>
            </label>
            <span v-if="selectedCount > 0" class="selection-count">
              {{ t("proxyPool.selectedCount", { count: selectedCount }) }}
            </span>
          </div>
          <div class="selection-actions">
            <n-button
              v-if="!allFilteredSelected"
              text
              type="primary"
              size="small"
              :disabled="deleting || checking"
              @click="selectAllFiltered"
            >
              {{ t("proxyPool.selectAllFiltered", { count: filteredProxies.length }) }}
            </n-button>
            <template v-if="selectedCount > 0">
              <n-button text size="small" :disabled="deleting" @click="clearSelection">
                {{ t("proxyPool.clearSelection") }}
              </n-button>
              <n-popconfirm
                placement="top-end"
                :width="340"
                content-class="proxy-batch-delete-popconfirm"
                content-style="max-width: calc(100vw - 24px); white-space: normal;"
                :positive-text="t('proxyPool.batchDelete')"
                :negative-text="t('common.cancel')"
                :disabled="deleting"
                @positive-click="deleteSelectedProxies"
              >
                <template #trigger>
                  <n-button type="error" size="small" :loading="deleting" :disabled="checking">
                    <template #icon>
                      <n-icon><trash-outline /></n-icon>
                    </template>
                    {{ t("proxyPool.batchDelete") }}
                  </n-button>
                </template>
                {{ t("proxyPool.batchDeleteConfirm", { count: selectedCount }) }}
              </n-popconfirm>
            </template>
          </div>
        </div>

        <n-spin :show="loading">
          <div v-if="loadFailed" class="state-block" role="alert">
            <n-icon :size="28"><alert-circle-outline /></n-icon>
            <p>{{ t("proxyPool.loadFailed") }}</p>
            <n-button size="small" @click="loadProxies">
              {{ t("common.refresh") }}
            </n-button>
          </div>

          <div v-else-if="!loading && proxies.length === 0" class="empty-state">
            <span class="empty-illustration">
              <n-icon :size="28"><server-outline /></n-icon>
            </span>
            <h4>{{ t("proxyPool.empty") }}</h4>
            <p>{{ t("proxyPool.emptyHint") }}</p>
            <n-button type="primary" size="small" @click="fillTemplate">
              {{ t("proxyPool.importAction") }}
            </n-button>
          </div>

          <div v-else-if="filteredProxies.length === 0" class="empty-state compact-empty">
            <span class="empty-illustration">
              <n-icon :size="24"><search-outline /></n-icon>
            </span>
            <h4>{{ t("proxyPool.noFilteredNodes") }}</h4>
            <n-button quaternary size="small" @click="resetFilters">
              {{ t("proxyPool.resetFilters") }}
            </n-button>
          </div>

          <div v-else class="proxy-table-section">
            <div class="proxy-table-viewport">
              <div class="proxy-table-wrap">
                <table class="proxy-table">
                  <thead>
                    <tr>
                      <th class="selection-column">
                        <input
                          type="checkbox"
                          :checked="allVisibleSelected"
                          :indeterminate="someVisibleSelected"
                          :aria-label="t('proxyPool.selectPage')"
                          @change="onPageSelectionChange"
                        />
                      </th>
                      <th>{{ t("proxyPool.endpoint") }}</th>
                      <th>{{ t("proxyPool.protocol") }}</th>
                      <th>{{ t("proxyPool.createdAt") }}</th>
                      <th>{{ t("proxyPool.state") }}</th>
                      <th>{{ t("proxyPool.binding") }}</th>
                      <th class="actions-column">{{ t("proxyPool.actions") }}</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="row in visibleRows" :key="row.id">
                      <td class="selection-column">
                        <input
                          type="checkbox"
                          :checked="selectedProxyIds.has(row.id)"
                          :aria-label="row.host"
                          @change="onProxySelectionChange(row.id, $event)"
                        />
                      </td>
                      <td>
                        <div class="endpoint-cell">
                          <span class="endpoint-icon">
                            <n-icon><server-outline /></n-icon>
                          </span>
                          <div class="endpoint-copy">
                            <strong>{{ row.host }}</strong>
                            <code>{{ row.safeUrl }}</code>
                          </div>
                        </div>
                      </td>
                      <td>
                        <span class="protocol-badge" :class="row.protocol.toLowerCase()">
                          {{ row.protocol }}
                        </span>
                      </td>
                      <td>
                        <span class="muted-cell">{{ row.createdLabel }}</span>
                      </td>
                      <td>
                        <span class="status-cell" :class="row.check_status">
                          <i class="status-dot" />
                          <span>{{ checkStatusLabel(row.check_status) }}</span>
                          <small v-if="row.check_status === 'up'">
                            {{ row.check_latency_ms }} ms
                          </small>
                          <small v-else-if="row.check_status === 'down'">
                            {{ row.check_http_status || row.check_error }}
                          </small>
                        </span>
                      </td>
                      <td>
                        <span class="binding-cell">
                          <n-icon><checkmark-circle-outline /></n-icon>
                          {{ t("proxyPool.bindingReady") }}
                        </span>
                      </td>
                      <td class="actions-column">
                        <n-button
                          circle
                          quaternary
                          type="primary"
                          :disabled="checking"
                          :loading="checkingIds.has(row.id)"
                          :aria-label="t('proxyPool.checkNode')"
                          :title="t('proxyPool.checkNode')"
                          @click="checkProxy(row)"
                        >
                          <template #icon>
                            <n-icon><pulse-outline /></n-icon>
                          </template>
                        </n-button>
                        <n-popconfirm
                          :positive-text="t('common.delete')"
                          :negative-text="t('common.cancel')"
                          @positive-click="deleteProxy(row)"
                        >
                          <template #trigger>
                            <n-button
                              circle
                              quaternary
                              type="error"
                              :loading="deletingId === row.id"
                              :aria-label="t('common.delete')"
                            >
                              <template #icon>
                                <n-icon><trash-outline /></n-icon>
                              </template>
                            </n-button>
                          </template>
                          {{ t("proxyPool.deleteConfirm") }}
                        </n-popconfirm>
                      </td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </div>
            <div class="table-footer">
              <span class="table-range">
                {{ pageRangeStart }}–{{ pageRangeEnd }} / {{ filteredProxies.length }}
              </span>
              <n-pagination
                v-model:page="currentPage"
                :page-count="pageCount"
                :page-size="pageSize"
                size="small"
              />
            </div>
          </div>
        </n-spin>
      </n-card>

      <aside class="proxy-side-column">
        <n-card class="quick-import-panel" :bordered="false">
          <div class="side-card-heading">
            <div>
              <div class="heading-line">
                <h3>{{ t("proxyPool.importTitle") }}</h3>
                <span class="soft-badge">{{ t("proxyPool.importBadge") }}</span>
              </div>
              <p>{{ t("proxyPool.importHint") }}</p>
            </div>
          </div>
          <n-input
            v-model:value="proxiesText"
            type="textarea"
            :placeholder="t('proxyPool.importPlaceholder')"
            :rows="7"
            class="import-input"
            aria-label="Proxy nodes"
          />
          <p class="format-hint">{{ t("proxyPool.importFormat") }}</p>
          <div class="import-actions">
            <n-button text type="primary" @click="downloadTemplate">
              <template #icon>
                <n-icon><download-outline /></n-icon>
              </template>
              {{ t("proxyPool.downloadTemplate") }}
            </n-button>
            <n-button text :disabled="!proxiesText" @click="proxiesText = ''">
              {{ t("proxyPool.clearImport") }}
            </n-button>
          </div>
          <n-button
            type="primary"
            block
            :loading="importing"
            :disabled="!proxiesText.trim()"
            @click="importProxies"
          >
            <template #icon>
              <n-icon><cloud-upload-outline /></n-icon>
            </template>
            {{ t("proxyPool.importAction") }}
          </n-button>
        </n-card>

        <n-card class="health-queue-panel" :bordered="false">
          <div class="side-card-heading compact-heading">
            <div>
              <div class="heading-line">
                <h3>{{ t("proxyPool.healthTitle") }}</h3>
                <span class="health-state-dot" :class="checkOverallState" />
              </div>
              <p>{{ t("proxyPool.healthHint") }}</p>
            </div>
            <n-button
              size="small"
              type="primary"
              :loading="checking"
              :disabled="proxies.length === 0"
              @click="checkAll"
            >
              <template #icon>
                <n-icon><pulse-outline /></n-icon>
              </template>
              {{ t("proxyPool.checkAll") }}
            </n-button>
          </div>
          <div class="health-summary">
            <div class="health-summary-main">
              <strong>{{ checkSummary.checked }} / {{ proxies.length }}</strong>
              <span>{{ t("proxyPool.checkedNodes") }}</span>
            </div>
            <div class="health-summary-stats">
              <span class="health-summary-stat up">
                <i class="status-dot" />
                {{ checkSummary.healthy }} {{ t("proxyPool.healthyNodes") }}
              </span>
              <span class="health-summary-stat down">
                <i class="status-dot" />
                {{ checkSummary.unhealthy }} {{ t("proxyPool.unhealthyNodes") }}
              </span>
            </div>
            <p class="health-target">
              <n-icon><pulse-outline /></n-icon>
              {{ t("proxyPool.checkTarget") }}
            </p>
          </div>
          <div class="health-footnote">
            <n-icon><checkmark-circle-outline /></n-icon>
            <span>{{ t("proxyPool.checkPersisted") }}</span>
          </div>
        </n-card>
      </aside>
    </section>
  </section>
</template>

<style scoped>
.proxy-pool-page {
  display: grid;
  gap: 20px;
  max-width: 1160px;
  min-width: 0;
  width: 100%;
  margin: 0 auto;
  overflow-x: hidden;
  padding-bottom: 8px;
}

.page-header,
.panel-heading,
.side-card-heading,
.heading-line,
.page-actions,
.import-actions {
  display: flex;
  align-items: center;
}

.page-header,
.panel-heading,
.side-card-heading {
  justify-content: space-between;
  gap: 18px;
}

.page-heading {
  min-width: 0;
}

.page-title-group {
  align-items: flex-start;
  display: flex;
  gap: 14px;
  min-width: 0;
}

.page-heading-copy {
  min-width: 0;
}

.proxy-page-logo {
  align-items: center;
  background: var(--primary-gradient);
  border: 1px solid rgba(255, 255, 255, 0.46);
  border-radius: 16px;
  box-shadow: var(--shadow-sm);
  color: #fff;
  display: inline-flex;
  flex: 0 0 auto;
  gap: 8px;
  height: 52px;
  justify-content: center;
  margin-top: 2px;
  padding: 0 12px 0 10px;
}

.proxy-logo-mark {
  display: grid;
  gap: 3px;
  width: 22px;
}

.proxy-logo-mark i {
  background: currentColor;
  border-radius: 2px;
  display: block;
  height: 4px;
  transform: skewX(-22deg);
}

.proxy-logo-mark i:nth-child(2) {
  opacity: 0.78;
  transform: translateX(3px) skewX(-22deg);
}

.proxy-logo-mark i:nth-child(3) {
  opacity: 0.58;
  transform: translateX(6px) skewX(-22deg);
}

.proxy-logo-word {
  font-size: 12px;
  font-weight: 800;
  letter-spacing: 0.14em;
  line-height: 1;
}

.page-heading h2,
.page-heading p,
.panel-heading h3,
.panel-heading p,
.side-card-heading h3,
.side-card-heading p,
.empty-state h4,
.empty-state p,
.state-block p,
.format-hint {
  margin: 0;
}

.page-heading h2 {
  color: var(--text-color-1, var(--text-primary));
  font-size: clamp(26px, 3vw, 34px);
  font-weight: 700;
  letter-spacing: -0.04em;
  line-height: 1.15;
}

.page-heading > p:last-child,
.panel-heading p,
.side-card-heading p,
.format-hint {
  color: var(--text-color-3, var(--text-tertiary));
  line-height: 1.55;
  margin-top: 7px;
}

.eyebrow {
  color: var(--primary-color);
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.12em;
  margin-bottom: 8px !important;
  text-transform: uppercase;
}

.page-actions {
  flex-shrink: 0;
  gap: 10px;
}

.proxy-overview {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 14px;
}

.proxy-stat-card,
.proxy-list-panel,
.quick-import-panel,
.health-queue-panel {
  background: var(--card-bg, rgba(255, 255, 255, 0.95));
  border: 1px solid var(--border-color-light);
  border-radius: var(--border-radius-lg);
  box-shadow: var(--shadow-sm);
}

.proxy-stat-card {
  min-width: 0;
  overflow: hidden;
  position: relative;
  transition:
    transform 0.2s ease,
    box-shadow 0.2s ease;
}

.proxy-stat-card:hover {
  box-shadow: var(--shadow-md);
  transform: translateY(-2px);
}

.proxy-stat-card :deep(.n-card__content) {
  display: grid;
  gap: 8px;
  min-height: 138px;
  padding: 17px 18px 14px;
}

.stat-topline {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.stat-icon,
.endpoint-icon,
.empty-illustration,
.health-placeholder-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
}

.stat-icon {
  width: 36px;
  height: 36px;
  border-radius: var(--border-radius-md);
  color: white;
  box-shadow: var(--shadow-sm);
}

.proxy-stat-card.primary .stat-icon {
  background: var(--primary-gradient);
}

.proxy-stat-card.success .stat-icon {
  background: var(--warning-gradient);
}

.proxy-stat-card.info .stat-icon {
  background: var(--success-gradient);
}

.proxy-stat-card.warning .stat-icon {
  background: var(--secondary-gradient);
}

.stat-kicker,
.stat-label,
.muted-cell,
.panel-note {
  color: var(--text-color-3, var(--text-tertiary));
}

.stat-kicker {
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.stat-value {
  color: var(--text-color-1, var(--text-primary));
  font-size: 29px;
  font-variant-numeric: tabular-nums;
  line-height: 1.1;
}

.stat-value-date {
  font-size: 19px;
  padding-top: 5px;
}

.stat-label {
  font-size: 13px;
  font-weight: 600;
}

.stat-progress {
  align-self: end;
  background: var(--border-color);
  border-radius: 99px;
  height: 4px;
  margin-top: 3px;
  overflow: hidden;
}

.stat-progress i {
  display: block;
  height: 100%;
  width: 68%;
}

.proxy-stat-card.primary .stat-progress i {
  background: var(--primary-gradient);
}

.proxy-stat-card.success .stat-progress i {
  background: var(--warning-gradient);
}

.proxy-stat-card.info .stat-progress i {
  background: var(--success-gradient);
}

.proxy-stat-card.warning .stat-progress i {
  background: var(--secondary-gradient);
}

.proxy-workspace {
  display: grid;
  align-items: stretch;
  grid-template-columns: minmax(0, 1fr) 326px;
  gap: 16px;
}

.proxy-list-panel,
.quick-import-panel,
.health-queue-panel {
  min-width: 0;
}

.proxy-list-panel :deep(.n-card__content),
.quick-import-panel :deep(.n-card__content),
.health-queue-panel :deep(.n-card__content) {
  display: grid;
  gap: 16px;
}

.panel-heading {
  align-items: flex-start;
}

.panel-heading h3,
.side-card-heading h3 {
  color: var(--text-color-1, var(--text-primary));
  font-size: 17px;
  line-height: 1.3;
}

.heading-line {
  gap: 9px;
}

.panel-note {
  flex-shrink: 0;
  font-size: 12px;
  max-width: 240px;
  text-align: right;
}

.list-tabs {
  border-bottom: 1px solid var(--border-color);
  display: flex;
  gap: 22px;
}

.list-tab {
  background: transparent;
  border: 0;
  border-bottom: 2px solid transparent;
  color: var(--text-color-3, var(--text-tertiary));
  cursor: pointer;
  font: inherit;
  font-size: 13px;
  margin-bottom: -1px;
  padding: 7px 2px 11px;
  transition:
    color 0.2s ease,
    border-color 0.2s ease;
}

.list-tab:hover,
.list-tab.active {
  color: var(--primary-color);
}

.list-tab.active {
  border-color: var(--primary-color);
  font-weight: 650;
}

.filter-toolbar {
  display: flex;
  align-items: center;
  gap: 9px;
}

.selection-toolbar {
  align-items: center;
  background: var(--primary-color-suppl);
  border: 1px solid rgba(102, 126, 234, 0.15);
  border-radius: var(--border-radius-md);
  display: flex;
  gap: 12px;
  justify-content: space-between;
  padding: 8px 10px;
}

.selection-leading,
.selection-actions,
.selection-label {
  align-items: center;
  display: flex;
  gap: 8px;
}

.selection-leading,
.selection-actions {
  min-width: 0;
}

.selection-label {
  color: var(--text-color-2, var(--text-secondary));
  cursor: pointer;
  font-size: 12px;
  white-space: nowrap;
}

.selection-label input,
.selection-column input {
  accent-color: var(--primary-color);
  cursor: pointer;
  height: 15px;
  margin: 0;
  width: 15px;
}

.selection-count {
  border-left: 1px solid rgba(102, 126, 234, 0.2);
  color: var(--primary-color);
  font-size: 12px;
  padding-left: 10px;
  white-space: nowrap;
}

.search-input {
  flex: 1 1 260px;
  min-width: 170px;
}

.protocol-select,
.status-select {
  flex: 0 0 150px;
}

.proxy-table-section {
  display: grid;
  gap: 12px;
  min-width: 0;
}

.proxy-table-viewport {
  height: clamp(280px, 42vh, 430px);
  min-width: 0;
  overflow: auto;
  overscroll-behavior: contain;
  scrollbar-gutter: stable;
}

.proxy-table-wrap {
  min-width: 720px;
}

.proxy-table {
  border-collapse: collapse;
  min-width: 720px;
  width: 100%;
}

.table-footer {
  align-items: center;
  display: flex;
  gap: 12px;
  justify-content: space-between;
  min-width: 0;
}

.table-range {
  color: var(--text-color-3, var(--text-tertiary));
  flex: 0 0 auto;
  font-size: 11px;
  white-space: nowrap;
}

.table-footer :deep(.n-pagination) {
  margin-left: auto;
}

.proxy-table th {
  color: var(--text-color-3, var(--text-tertiary));
  font-size: 11px;
  font-weight: 650;
  letter-spacing: 0.02em;
  padding: 0 9px 10px;
  text-align: left;
  white-space: nowrap;
}

.selection-column {
  padding-left: 4px !important;
  padding-right: 4px !important;
  text-align: center !important;
  width: 36px;
}

.proxy-table td {
  border-top: 1px solid var(--border-color-light);
  color: var(--text-color-2, var(--text-secondary));
  font-size: 12px;
  padding: 12px 9px;
  vertical-align: middle;
}

.proxy-table tbody tr {
  transition: background 0.18s ease;
}

.proxy-table tbody tr:hover {
  background: var(--hover-bg);
}

.endpoint-cell,
.binding-cell,
.status-cell {
  display: flex;
  align-items: center;
  gap: 9px;
}

.endpoint-icon {
  background: var(--primary-color-suppl);
  border-radius: 9px;
  color: var(--primary-color);
  flex: 0 0 auto;
  height: 32px;
  width: 32px;
}

.endpoint-copy {
  display: grid;
  gap: 4px;
  min-width: 0;
}

.endpoint-copy strong {
  color: var(--text-color-1, var(--text-primary));
  font-size: 13px;
  font-weight: 600;
  max-width: 230px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.endpoint-copy code {
  color: var(--text-color-3, var(--text-tertiary));
  font-size: 11px;
  max-width: 230px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.protocol-badge,
.soft-badge {
  border-radius: 6px;
  font-size: 11px;
  font-weight: 650;
  padding: 4px 7px;
}

.protocol-badge {
  align-items: center;
  background: var(--primary-color-suppl);
  color: var(--primary-color);
  display: inline-flex;
  justify-content: center;
  min-width: 56px;
  white-space: nowrap;
}

.protocol-badge.socks5 {
  background: rgba(24, 160, 88, 0.1);
  color: var(--success-color);
}

.protocol-badge.https {
  background: rgba(240, 147, 251, 0.13);
  color: #b14ac4;
}

.status-cell {
  white-space: nowrap;
}

.status-dot,
.health-state-dot {
  border-radius: 50%;
  display: inline-block;
  flex: 0 0 auto;
  height: 7px;
  width: 7px;
}

.status-cell.unchecked {
  color: #c27803;
}

.status-cell.unchecked .status-dot,
.health-state-dot {
  background: #f0a020;
  box-shadow: 0 0 0 3px rgba(240, 160, 32, 0.14);
}

.status-cell.up {
  color: var(--success-color);
}

.status-cell.up .status-dot,
.health-state-dot.active,
.health-summary-stat.up .status-dot {
  background: var(--success-color);
  box-shadow: 0 0 0 3px rgba(24, 160, 88, 0.14);
}

.status-cell.down {
  color: var(--error-color);
}

.status-cell.down .status-dot,
.health-state-dot.degraded,
.health-summary-stat.down .status-dot {
  background: var(--error-color);
  box-shadow: 0 0 0 3px rgba(208, 48, 80, 0.14);
}

.status-cell small {
  color: var(--text-color-3, var(--text-tertiary));
  font-size: 10px;
}

.binding-cell {
  color: var(--success-color);
  white-space: nowrap;
}

.actions-column {
  text-align: right !important;
  width: 92px;
}

.proxy-side-column {
  align-self: start;
  border: 1px solid var(--border-color);
  border-radius: var(--border-radius-lg);
  display: flex;
  flex-direction: column;
  gap: 0;
  min-height: 0;
  overflow: visible;
}

.proxy-side-column > .quick-import-panel,
.proxy-side-column > .health-queue-panel {
  border: 0;
  border-radius: 0;
  box-shadow: none;
}

.proxy-side-column > .quick-import-panel {
  border-radius: var(--border-radius-lg) var(--border-radius-lg) 0 0;
}

.proxy-side-column > .health-queue-panel {
  border-radius: 0 0 var(--border-radius-lg) var(--border-radius-lg);
  border-top: 1px solid var(--border-color);
  flex: 0 0 auto;
  min-height: auto;
}

.health-queue-panel :deep(.n-card__content) {
  align-content: normal;
  min-height: 0;
}

.health-summary {
  background: var(--primary-color-suppl);
  border: 1px solid rgba(102, 126, 234, 0.14);
  border-radius: var(--border-radius-md);
  display: grid;
  gap: 12px;
  padding: 15px;
}

.health-summary-main {
  align-items: baseline;
  display: flex;
  gap: 9px;
}

.health-summary-main strong {
  color: var(--text-color-1, var(--text-primary));
  font-size: 24px;
  letter-spacing: -0.04em;
}

.health-summary-main span,
.health-summary-stat,
.health-target {
  color: var(--text-color-3, var(--text-tertiary));
  font-size: 11px;
}

.health-summary-stats {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
}

.health-summary-stat {
  align-items: center;
  display: inline-flex;
  gap: 6px;
}

.health-summary-stat.up {
  color: var(--success-color);
}

.health-summary-stat.down {
  color: var(--error-color);
}

.health-target {
  align-items: flex-start;
  display: flex;
  gap: 7px;
  line-height: 1.45;
  margin: 0;
}

.health-target .n-icon {
  color: var(--primary-color);
  flex: 0 0 auto;
  margin-top: 1px;
}

.side-card-heading {
  align-items: flex-start;
}

.soft-badge {
  background: var(--primary-color-suppl);
  color: var(--primary-color);
  white-space: nowrap;
}

.import-input :deep(.n-input__textarea),
.import-input :deep(textarea) {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
  line-height: 1.65;
  max-height: 220px;
  overflow-y: auto;
  resize: vertical;
}

.format-hint {
  font-size: 11px;
  margin-top: -8px;
}

.import-actions {
  justify-content: space-between;
  gap: 8px;
}

.health-icon {
  color: var(--primary-color);
  font-size: 21px;
}

.health-placeholder {
  background: var(--primary-color-suppl);
  border: 1px solid rgba(102, 126, 234, 0.14);
  border-radius: var(--border-radius-md);
  display: grid;
  gap: 8px;
  padding: 15px;
}

.health-placeholder-icon {
  background: var(--primary-gradient);
  border-radius: 10px;
  color: white;
  height: 36px;
  width: 36px;
}

.health-placeholder strong {
  color: var(--text-color-1, var(--text-primary));
  font-size: 13px;
}

.health-placeholder p {
  color: var(--text-color-3, var(--text-tertiary));
  font-size: 12px;
  line-height: 1.55;
  margin: 0;
}

.health-footnote {
  align-items: flex-start;
  color: var(--text-color-3, var(--text-tertiary));
  display: flex;
  font-size: 11px;
  gap: 7px;
  line-height: 1.5;
}

.health-footnote .n-icon {
  color: var(--success-color);
  flex: 0 0 auto;
  margin-top: 2px;
}

.state-block,
.empty-state {
  align-items: center;
  display: grid;
  gap: 9px;
  justify-items: center;
  min-height: 230px;
  padding: 24px;
  text-align: center;
}

.state-block {
  color: var(--error-color);
}

.state-block p,
.empty-state p {
  color: var(--text-color-3, var(--text-tertiary));
  font-size: 13px;
  line-height: 1.55;
  max-width: 360px;
}

.empty-illustration {
  background: var(--primary-color-suppl);
  border: 1px solid rgba(102, 126, 234, 0.16);
  border-radius: 18px;
  color: var(--primary-color);
  height: 58px;
  width: 58px;
}

.empty-state h4 {
  color: var(--text-color-1, var(--text-primary));
  font-size: 15px;
}

.compact-empty {
  min-height: 160px;
}

:global(.proxy-batch-delete-popconfirm) {
  max-width: calc(100vw - 24px);
}

:global(.proxy-batch-delete-popconfirm .n-popconfirm__body) {
  min-width: 0;
  overflow-wrap: anywhere;
  white-space: normal;
}

:global(.proxy-batch-delete-popconfirm .n-popconfirm__action) {
  flex-wrap: wrap;
}

@media (max-width: 980px) {
  .proxy-overview {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .proxy-workspace {
    grid-template-columns: 1fr;
  }

  .proxy-side-column {
    align-self: auto;
    border: 0;
    border-radius: 0;
    display: grid;
    gap: 16px;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    grid-template-rows: none;
    overflow: visible;
  }

  .proxy-side-column > .quick-import-panel,
  .proxy-side-column > .health-queue-panel {
    border: 1px solid var(--border-color);
    border-radius: var(--border-radius-lg);
    box-shadow: var(--card-shadow);
  }

  .proxy-side-column > .health-queue-panel {
    flex: initial;
    min-height: auto;
  }

  .health-queue-panel :deep(.n-card__content) {
    align-content: normal;
    min-height: 0;
  }
}

@media (max-width: 640px) {
  .proxy-pool-page {
    gap: 14px;
  }

  .page-header,
  .panel-heading,
  .side-card-heading {
    align-items: flex-start;
    flex-direction: column;
  }

  .page-actions,
  .page-actions > :deep(.n-button) {
    width: 100%;
  }

  .page-actions {
    align-items: stretch;
    flex-direction: column;
  }

  .proxy-overview {
    gap: 10px;
    grid-template-columns: minmax(0, 1fr);
  }

  .proxy-stat-card :deep(.n-card__content) {
    min-height: 124px;
    padding: 14px;
  }

  .stat-value {
    font-size: 24px;
  }

  .stat-value-date {
    font-size: 16px;
  }

  .panel-note {
    max-width: none;
    text-align: left;
  }

  .filter-toolbar,
  .proxy-side-column {
    align-items: stretch;
    display: grid;
    grid-template-columns: 1fr;
  }

  .selection-toolbar {
    align-items: flex-start;
    flex-direction: column;
  }

  .selection-actions {
    flex-wrap: wrap;
    width: 100%;
  }

  .table-footer {
    align-items: flex-start;
    flex-direction: column;
  }

  .table-footer :deep(.n-pagination) {
    margin-left: 0;
  }

  .search-input,
  .protocol-select,
  .status-select {
    width: 100%;
  }

  .filter-toolbar > .n-button {
    justify-content: flex-start;
  }
}
</style>
