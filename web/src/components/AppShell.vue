<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, watch } from 'vue'
import { useNotifications } from '@/composables/useNotifications'
import { useSettingsStore } from '@/stores/settings'
import { useStatusStore } from '@/stores/status'
import { useTheme } from '@/stores/theme'
import AppIcon from '@/components/AppIcon.vue'
import OfflineBanner from '@/components/OfflineBanner.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import Toast from '@/components/Toast.vue'
import { formatNumber } from '@/utils/time'

const settings = useSettingsStore()
const status = useStatusStore()
const theme = useTheme()
useNotifications()

const navigation = [
  { to: '/', label: '仪表盘', icon: 'dashboard' as const },
  { to: '/records', label: '记录', icon: 'records' as const },
  { to: '/archives', label: '归档', icon: 'archives' as const },
  { to: '/settings', label: '设置', icon: 'settings' as const },
]

const version = __APP_VERSION__
const degree = computed(() => status.remote.value?.monitor.last_degree ?? null)
const themeIcon = computed(() => {
  if (theme.mode.value === 'auto') {
    return 'auto' as const
  }
  return theme.mode.value === 'light' ? ('sun' as const) : ('moon' as const)
})
const themeTitle = computed(() => `切换主题, 当前: ${theme.label.value}`)
const offlineDetail = computed(
  () => status.state.error?.detail ?? status.local.value?.health_detail ?? null,
)
const clientVersion = computed(() => status.local.value?.version ?? version)

async function refresh(): Promise<void> {
  await status.manualRefresh()
}

onMounted(async () => {
  await settings.loadSettings()
  status.startStatusPolling(settings.refreshIntervalMs.value)
})

watch(
  () => settings.refreshIntervalMs.value,
  (value) => {
    status.setStatusInterval(value)
  },
)

onBeforeUnmount(() => {
  status.stopStatusPolling()
})
</script>

<template>
  <div class="app-shell">
    <a class="skip-link" href="#main-content">跳到主要内容</a>

    <aside class="sidebar">
      <RouterLink class="brand" to="/" aria-label="DormWatt 首页">
        <span class="brand-mark">DW</span>
        <span class="brand-text">DormWatt</span>
      </RouterLink>

      <nav class="side-nav" aria-label="主导航">
        <RouterLink
          v-for="item in navigation"
          :key="item.to"
          :to="item.to"
          class="side-link"
          :title="item.label"
          :aria-label="item.label"
        >
          <AppIcon :name="item.icon" :size="19" />
          <span class="side-label">{{ item.label }}</span>
        </RouterLink>
      </nav>

      <div class="sidebar-footer">
        <span class="side-version">版本 {{ version }}</span>
        <span class="side-client">客户端 {{ clientVersion }}</span>
      </div>
    </aside>

    <div class="app-main">
      <header class="topbar">
        <RouterLink class="topbar-brand" to="/" aria-label="DormWatt 首页">
          <span class="brand-mark">DW</span>
          <span>DormWatt</span>
        </RouterLink>

        <div class="topbar-status">
          <StatusBadge
            :health="status.health.value"
            :detail="status.state.error?.detail ?? status.local.value?.health_detail"
          />
          <span class="topbar-degree tabular" aria-live="polite">
            <AppIcon name="battery" :size="16" />
            <template v-if="degree !== null">
              {{ formatNumber(degree) }} <span class="degree-unit">度</span>
            </template>
            <template v-else>-- 度</template>
          </span>
        </div>

        <div class="topbar-actions">
          <button
            type="button"
            class="btn btn-icon"
            :disabled="status.state.loading"
            aria-label="刷新"
            title="刷新"
            @click="refresh"
          >
            <AppIcon name="refresh" :size="18" :class="{ spin: status.state.loading }" />
          </button>
          <button
            type="button"
            class="btn btn-icon"
            :aria-label="themeTitle"
            :title="themeTitle"
            @click="theme.cycleTheme"
          >
            <AppIcon :name="themeIcon" :size="18" />
          </button>
          <RouterLink
            class="btn btn-icon"
            to="/settings"
            aria-label="设置"
            title="设置"
          >
            <AppIcon name="settings" :size="18" />
          </RouterLink>
        </div>
      </header>

      <OfflineBanner
        :health="status.health.value"
        :error="status.state.error"
        :interval-ms="settings.refreshIntervalMs.value"
        :updated-at="status.state.updatedAt"
        :detail="offlineDetail"
        @retry="refresh"
      />

      <main id="main-content" class="app-content">
        <RouterView />
      </main>
    </div>

    <nav class="mobile-nav" aria-label="移动端主导航">
      <RouterLink
        v-for="item in navigation"
        :key="item.to"
        :to="item.to"
        class="mobile-link"
        :aria-label="item.label"
      >
        <AppIcon :name="item.icon" :size="20" />
        <span>{{ item.label }}</span>
      </RouterLink>
    </nav>

    <Toast />
  </div>
</template>

<style scoped>
.skip-link {
  position: fixed;
  z-index: 1100;
  top: 8px;
  left: 8px;
  padding: 8px 12px;
  border-radius: 6px;
  background: var(--accent);
  color: #ffffff;
  transform: translateY(-160%);
}

.skip-link:focus {
  transform: translateY(0);
}

.sidebar {
  position: fixed;
  z-index: 30;
  inset: 0 auto 0 0;
  display: flex;
  width: var(--sidebar-width);
  flex-direction: column;
  border-right: 1px solid var(--border);
  background: var(--surface);
}

.brand {
  display: flex;
  height: var(--topbar-height);
  align-items: center;
  gap: 10px;
  padding: 0 16px;
  border-bottom: 1px solid var(--border);
  color: var(--text);
}

.brand:hover {
  color: var(--text);
}

.brand-mark {
  display: inline-flex;
  width: 30px;
  height: 30px;
  flex: 0 0 auto;
  align-items: center;
  justify-content: center;
  border-radius: 7px;
  background: var(--accent);
  color: #ffffff;
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.04em;
}

.brand-text {
  font-size: 15px;
  font-weight: 700;
}

.side-nav {
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: 4px;
  padding: 14px 10px;
}

.side-link {
  display: flex;
  align-items: center;
  gap: 10px;
  min-height: 40px;
  padding: 8px 10px;
  border-radius: var(--radius-sm);
  color: var(--text-muted);
  font-size: 14px;
}

.side-link:hover {
  background: var(--surface-soft);
  color: var(--text);
}

.side-link.router-link-active {
  background: var(--accent-soft);
  color: var(--accent);
  font-weight: 600;
}

.sidebar-footer {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 14px 18px;
  border-top: 1px solid var(--border);
  color: var(--text-muted);
  font-size: 11px;
}

.topbar {
  position: sticky;
  z-index: 20;
  top: 0;
  display: flex;
  height: var(--topbar-height);
  align-items: center;
  gap: 12px;
  padding: 0 20px;
  border-bottom: 1px solid var(--border);
  background: color-mix(in srgb, var(--surface) 88%, transparent);
  backdrop-filter: blur(10px);
}

.topbar-brand {
  display: none;
  align-items: center;
  gap: 8px;
  color: var(--text);
  font-weight: 700;
}

.topbar-status {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 12px;
}

.topbar-degree {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  color: var(--text);
  font-size: 15px;
  font-weight: 650;
  white-space: nowrap;
}

.degree-unit {
  color: var(--text-muted);
  font-size: 12px;
  font-weight: 500;
}

.topbar-actions {
  display: flex;
  align-items: center;
  gap: 4px;
  margin-left: auto;
}

.mobile-nav {
  display: none;
}

@media (max-width: 1024px) {
  .sidebar {
    width: 72px;
  }

  .brand {
    justify-content: center;
    padding: 0;
  }

  .brand-text,
  .side-label,
  .sidebar-footer {
    display: none;
  }

  .side-nav {
    align-items: center;
    padding: 14px 8px;
  }

  .side-link {
    width: 44px;
    justify-content: center;
    padding: 8px;
  }

  .app-main {
    margin-left: 72px;
  }
}

@media (max-width: 768px) {
  .sidebar {
    display: none;
  }

  .app-main {
    margin-left: 0;
  }

  .topbar {
    padding: 0 14px;
  }

  .topbar-brand {
    display: inline-flex;
  }

  .topbar-brand .brand-mark {
    width: 26px;
    height: 26px;
  }

  .topbar-brand > span:last-child {
    display: none;
  }

  .topbar-status {
    gap: 8px;
  }

  .topbar-status :deep(.status-badge) {
    padding: 2px 8px;
  }

  .topbar-degree {
    font-size: 14px;
  }

  .mobile-nav {
    position: fixed;
    z-index: 40;
    right: 0;
    bottom: 0;
    left: 0;
    display: flex;
    padding: 6px 8px calc(6px + env(safe-area-inset-bottom));
    border-top: 1px solid var(--border);
    background: var(--surface);
  }

  .mobile-link {
    display: flex;
    flex: 1;
    min-height: 46px;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 1px;
    border-radius: var(--radius-sm);
    color: var(--text-muted);
    font-size: 10px;
  }

  .mobile-link.router-link-active {
    background: var(--accent-soft);
    color: var(--accent);
    font-weight: 600;
  }
}

@media (max-width: 400px) {
  .topbar-degree .degree-unit {
    display: none;
  }
}
</style>
