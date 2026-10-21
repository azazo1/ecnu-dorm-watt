<script setup lang="ts">
import { computed, ref } from 'vue'
import AppIcon from '@/components/AppIcon.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { useToast } from '@/composables/useToast'
import { useStatusStore } from '@/stores/status'
import { formatDateTime } from '@/utils/time'
import { useSettingsStore } from '@/stores/settings'

const status = useStatusStore()
const settings = useSettingsStore()
const toast = useToast()
const command = 'dormwatt client login'
const timezone = settings.timezone

const copied = ref(false)
const checking = ref(false)
const checkedMessage = ref('')

const health = status.health
const session = computed(() => status.remote.value?.session ?? null)
const sessionLabel = computed(() => {
  const state = session.value?.state
  if (state === 'ok') {
    return '会话有效'
  }
  if (state === 'invalid') {
    return '会话已失效'
  }
  if (state === 'missing') {
    return '尚未上传会话'
  }
  if (state === 'unknown') {
    return '会话待验证'
  }
  return '会话状态未知'
})

async function copyCommand(): Promise<void> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(command)
    } else {
      const input = document.createElement('textarea')
      input.value = command
      input.style.position = 'fixed'
      input.style.opacity = '0'
      document.body.appendChild(input)
      input.select()
      document.execCommand('copy')
      document.body.removeChild(input)
    }
    copied.value = true
    toast.success('命令已复制.')
    window.setTimeout(() => {
      copied.value = false
    }, 2500)
  } catch {
    toast.error('复制失败, 请手动选择命令文本.')
  }
}

async function checkLogin(): Promise<void> {
  if (checking.value) {
    return
  }
  checking.value = true
  checkedMessage.value = ''
  try {
    await status.refreshStatus()
    if (status.state.error) {
      checkedMessage.value = `刷新失败: ${status.state.error.detail}`
      toast.error('状态刷新失败, 请检查本地守护进程.')
      return
    }
    const state = status.remote.value?.session.state
    if (state === 'ok') {
      checkedMessage.value = '已检测到有效会话, 可以返回仪表盘查看电量.'
      toast.success('登录状态有效.')
    } else if (state === 'unknown') {
      checkedMessage.value = '会话已上传, 等待下一次采集验证.'
      toast.info('会话已上传, 等待下一次采集验证.')
    } else if (state === 'invalid') {
      checkedMessage.value = '会话仍然无效, 请重新完成学校 SSO.'
      toast.error('会话仍然无效.')
    } else if (state === 'missing') {
      checkedMessage.value = '尚未检测到会话, 请先运行登录命令.'
      toast.info('尚未检测到会话.')
    } else {
      checkedMessage.value = '暂时无法判断登录状态, 请稍后重试.'
    }
  } finally {
    checking.value = false
  }
}
</script>

<template>
  <div class="page">
    <header class="page-header">
      <div class="page-heading">
        <h1 class="page-title">登录帮助</h1>
        <p class="page-subtitle">重新获取 ECNU 会话, 让采集循环恢复工作.</p>
      </div>
      <div class="page-actions">
        <StatusBadge :health="health" :detail="status.state.error?.detail" />
      </div>
    </header>

    <section class="card">
      <div class="card-header">
        <div>
          <h2 class="card-title">当前登录状态</h2>
          <p class="card-subtitle">{{ sessionLabel }}</p>
        </div>
        <span v-if="session?.state === 'ok'" class="tag tag-ok">正常</span>
        <span v-else-if="session?.state === 'invalid'" class="tag tag-danger">需要重新登录</span>
        <span v-else class="tag">待确认</span>
      </div>

      <dl class="session-meta">
        <div>
          <dt>会话更新时间</dt>
          <dd>{{ formatDateTime(session?.updated_at, timezone) }}</dd>
        </div>
        <div>
          <dt>最近检查时间</dt>
          <dd>{{ formatDateTime(session?.last_checked_at, timezone) }}</dd>
        </div>
      </dl>

      <p v-if="checkedMessage" class="check-message" role="status">{{ checkedMessage }}</p>

      <div class="alert alert-info">
        <div class="alert-body">
          <p class="alert-title">页面不会内嵌学校登录页</p>
          <p class="alert-text">
            为了避免跨域与 X-Frame-Options 问题, 这里不使用 iframe. 登录在系统浏览器中完成,
            会话值由命令行工具发送到本地守护进程.
          </p>
        </div>
      </div>
    </section>

    <section class="card">
      <h2 class="card-title">操作步骤</h2>
      <ol class="steps">
        <li>
          <span class="step-number">1</span>
          <div>
            <p class="step-title">在终端执行登录命令</p>
            <p class="step-text">
              保持守护进程正在运行, 打开终端并执行下面的命令.
            </p>
            <div class="command-box">
              <code>{{ command }}</code>
              <button type="button" class="btn btn-sm" @click="copyCommand">
                <AppIcon :name="copied ? 'check' : 'copy'" :size="14" />
                {{ copied ? '已复制' : '复制命令' }}
              </button>
            </div>
          </div>
        </li>

        <li>
          <span class="step-number">2</span>
          <div>
            <p class="step-title">在打开的浏览器中完成学校 SSO</p>
            <p class="step-text">
              按命令行提示登录 ECNU 统一身份认证. 完成后命令会自动保存会话.
            </p>
          </div>
        </li>

        <li>
          <span class="step-number">3</span>
          <div>
            <p class="step-title">回到此页面确认</p>
            <p class="step-text">点击下面的按钮重新读取状态.</p>
            <button
              type="button"
              class="btn btn-primary"
              :disabled="checking"
              @click="checkLogin"
            >
              {{ checking ? '检查中...' : '我已登录' }}
            </button>
          </div>
        </li>

        <li>
          <span class="step-number">4</span>
          <div>
            <p class="step-title">无法打开浏览器时手动填写</p>
            <p class="step-text">
              从开发者工具复制 JSESSIONID, cookie 与 X-CSRF-TOKEN, 在设置页上传.
              三个值只发送到本地守护进程.
            </p>
            <RouterLink class="btn" to="/settings#login">
              <AppIcon name="login" :size="16" />
              前往手动登录表单
            </RouterLink>
          </div>
        </li>
      </ol>
    </section>

    <section class="card">
      <h2 class="card-title">安全提示</h2>
      <ul class="security-list">
        <li>会话值不在页面中回显, 不写入 URL, 不写入 localStorage.</li>
        <li>本地页面只访问同源 <code>/api/*</code>, 不直连远端服务端.</li>
        <li>如果怀疑会话泄露, 在设置页清除会话并重新登录.</li>
        <li>登录失败时可以先查看服务端返回的错误码, 再决定是否重试.</li>
      </ul>
      <div class="form-actions">
        <RouterLink class="btn" to="/settings">打开设置</RouterLink>
        <RouterLink class="btn btn-ghost" to="/">返回仪表盘</RouterLink>
      </div>
    </section>
  </div>
</template>

<style scoped>
.session-meta {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
  margin: 0 0 14px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface-soft);
}

.session-meta div {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.session-meta dt {
  color: var(--text-muted);
  font-size: 12px;
}

.session-meta dd {
  margin: 0;
  font-size: 13px;
  font-variant-numeric: tabular-nums;
}

.check-message {
  margin-bottom: 12px;
  padding: 9px 12px;
  border: 1px solid var(--accent);
  border-radius: var(--radius-sm);
  background: var(--accent-soft);
  color: var(--text);
  font-size: 13px;
}

.steps {
  display: flex;
  flex-direction: column;
  gap: 18px;
  margin: 18px 0 0;
  padding: 0;
  list-style: none;
}

.steps li {
  display: flex;
  align-items: flex-start;
  gap: 12px;
}

.step-number {
  display: inline-flex;
  width: 28px;
  height: 28px;
  flex: 0 0 auto;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  background: var(--accent-soft);
  color: var(--accent);
  font-size: 13px;
  font-weight: 700;
}

.step-title {
  font-weight: 650;
}

.step-text {
  margin: 2px 0 10px;
  color: var(--text-muted);
  font-size: 13px;
}

.command-box {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  flex-wrap: wrap;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface-soft);
}

.command-box code {
  font-size: 13px;
}

.security-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin: 14px 0 0;
  padding-left: 20px;
  color: var(--text-muted);
  font-size: 13px;
}

@media (max-width: 640px) {
  .session-meta {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
