<script setup lang="ts">
import { AlertTriangle, KeyRound, LogOut } from 'lucide-vue-next'
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { api, ApiError } from '../api/client'
import { loadSession, logoutSession } from '../stores/panel'

const router = useRouter()
const oldPassword = ref('12345678')
const newPassword = ref('')
const confirmPassword = ref('')
const loading = ref(false)
const errorMsg = ref('')

async function submitChange() {
  errorMsg.value = ''
  if (newPassword.value.length < 8) {
    errorMsg.value = '新密码长度不能少于 8 位'
    return
  }
  if (newPassword.value === '12345678') {
    errorMsg.value = '新密码不能继续使用默认密码 12345678'
    return
  }
  if (newPassword.value !== confirmPassword.value) {
    errorMsg.value = '两次输入的新密码不一致'
    return
  }

  loading.value = true
  try {
    await api.changePassword(oldPassword.value, newPassword.value)
    await loadSession()
    await router.replace('/dashboard')
  } catch (err) {
    if (err instanceof ApiError) {
      errorMsg.value = err.message
    } else {
      errorMsg.value = '修改密码失败'
    }
  } finally {
    loading.value = false
  }
}

async function handleLogout() {
  await logoutSession()
  await router.replace('/login')
}
</script>

<template>
  <div class="min-h-screen bg-slate-900 flex items-center justify-center p-4">
    <div class="w-full max-w-md bg-white rounded-xl shadow-2xl overflow-hidden">
      <div class="bg-amber-500 px-6 py-4 flex items-center justify-between text-white">
        <div class="flex items-center gap-2.5">
          <AlertTriangle class="w-5 h-5" />
          <div>
            <h1 class="text-base font-semibold">首次登录安全提示</h1>
            <p class="text-xs text-amber-50">必须修改初始密码后才能进入运维面板</p>
          </div>
        </div>
        <button
          type="button"
          @click="handleLogout"
          class="text-xs flex items-center gap-1 bg-black/15 hover:bg-black/25 px-2.5 py-1.5 rounded cursor-pointer"
        >
          <LogOut class="w-3.5 h-3.5" />
          退出
        </button>
      </div>

      <form @submit.prevent="submitChange" class="p-6 space-y-4">
        <div
          v-if="errorMsg"
          class="px-3 py-2 rounded-lg bg-red-50 border border-red-200 text-red-700 text-sm"
        >
          {{ errorMsg }}
        </div>

        <div>
          <label class="block text-sm font-medium text-slate-700 mb-1">原密码</label>
          <input
            v-model="oldPassword"
            type="password"
            required
            class="w-full px-3 py-2 border border-slate-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-[#20a53a]"
          />
        </div>

        <div>
          <label class="block text-sm font-medium text-slate-700 mb-1">新密码（至少 8 位）</label>
          <input
            v-model="newPassword"
            type="password"
            required
            minlength="8"
            class="w-full px-3 py-2 border border-slate-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-[#20a53a]"
            placeholder="请设置高强度管理员密码"
          />
        </div>

        <div>
          <label class="block text-sm font-medium text-slate-700 mb-1">确认新密码</label>
          <input
            v-model="confirmPassword"
            type="password"
            required
            minlength="8"
            class="w-full px-3 py-2 border border-slate-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-[#20a53a]"
            placeholder="再次输入新密码"
          />
        </div>

        <button
          type="submit"
          :disabled="loading"
          class="w-full py-2.5 bg-[#20a53a] hover:bg-[#1a8e31] disabled:opacity-60 text-white font-medium rounded-lg text-sm flex items-center justify-center gap-2 cursor-pointer"
        >
          <KeyRound class="w-4 h-4" />
          {{ loading ? '提交中...' : '确认修改并进入面板' }}
        </button>
      </form>
    </div>
  </div>
</template>
