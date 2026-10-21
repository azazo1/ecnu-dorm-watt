<script setup lang="ts">
import { ref } from 'vue'
import { api } from '@/api/client'
import { useToast } from '@/composables/useToast'
import { toUiError, type UiError } from '@/utils/errors'

const emit = defineEmits<{ success: [] }>()
const toast = useToast()

const jsessionId = ref('')
const cookie = ref('')
const csrfToken = ref('')
const submitting = ref(false)
const error = ref<UiError | null>(null)

function validate(): string {
  const values = [jsessionId.value, cookie.value, csrfToken.value]
  if (values.some((value) => !value)) {
    return '三个字段都必须填写.'
  }
  if (values.some((value) => /\s/.test(value))) {
    return '字段值不能包含空白字符.'
  }
  if (values.some((value) => /[\u0000-\u001f\u007f]/.test(value))) {
    return '字段值不能包含控制字符.'
  }
  return ''
}

async function submit(): Promise<void> {
  if (submitting.value) {
    return
  }
  error.value = null
  const message = validate()
  if (message) {
    error.value = {
      code: 'validation_failed',
      title: '表单校验失败',
      detail: message,
      advice: '从浏览器开发者工具复制完整的会话值后重试.',
      status: 0,
      requestId: null,
    }
    return
  }
  submitting.value = true
  try {
    await api.updateSession({
      jsession_id: jsessionId.value,
      cookie: cookie.value,
      x_csrf_token: csrfToken.value,
    })
    jsessionId.value = ''
    cookie.value = ''
    csrfToken.value = ''
    toast.success('会话已上传, 下一次采集会验证登录状态.')
    emit('success')
  } catch (caught) {
    error.value = toUiError(caught)
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <form class="login-form" @submit.prevent="submit">
    <div class="alert alert-info">
      <div class="alert-body">
        <p class="alert-title">推荐使用命令行登录</p>
        <p class="alert-text">
          在终端运行 <code>dormwatt client login</code> 打开学校 SSO, 完成后回到此页面.
          下面的表单用于无法打开浏览器时的兜底.
        </p>
      </div>
    </div>

    <div class="form-grid">
      <div class="field">
        <label for="login-jsession">JSESSIONID</label>
        <input
          id="login-jsession"
          v-model="jsessionId"
          class="input"
          type="password"
          autocomplete="off"
          autocapitalize="none"
          spellcheck="false"
          :disabled="submitting"
        />
      </div>
      <div class="field">
        <label for="login-cookie">cookie</label>
        <input
          id="login-cookie"
          v-model="cookie"
          class="input"
          type="password"
          autocomplete="off"
          autocapitalize="none"
          spellcheck="false"
          :disabled="submitting"
        />
      </div>
      <div class="field">
        <label for="login-csrf">X-CSRF-TOKEN</label>
        <input
          id="login-csrf"
          v-model="csrfToken"
          class="input"
          type="password"
          autocomplete="off"
          autocapitalize="none"
          spellcheck="false"
          :disabled="submitting"
        />
      </div>
    </div>

    <p class="field-hint">
      这三个值只会发送到本地守护进程, 不会写入页面 URL 或 localStorage. 提交成功后输入框会清空.
    </p>

    <div v-if="error" class="form-error" role="alert">
      <strong>{{ error.title }}</strong>
      <span>{{ error.detail }}</span>
      <span>{{ error.advice }}</span>
      <span class="small">错误码: <code>{{ error.code }}</code></span>
    </div>

    <div class="form-actions">
      <button type="submit" class="btn btn-primary" :disabled="submitting">
        {{ submitting ? '上传中...' : '上传会话' }}
      </button>
      <RouterLink class="btn btn-ghost" to="/login-help">登录帮助</RouterLink>
    </div>
  </form>
</template>

<style scoped>
.login-form {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
</style>
