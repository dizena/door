<script setup lang="ts">
import { Lock, Server, User as UserIcon } from 'lucide-vue-next'
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { api, ApiError } from '../api/client'
import { loadSession } from '../stores/panel'

const router = useRouter()
const username = ref('admin')
const password = ref('')
const loading = ref(false)
const errorMsg = ref('')

async function submitLogin() {
  errorMsg.value = ''
  loading.value = true
  try {
    const res = await api.login(username.value, password.value)
    await loadSession()
    if (res.user.must_change_password) {
      await router.replace('/force-password')
    } else {
      await router.replace('/dashboard')
    }
  } catch (err) {
    if (err instanceof ApiError) {
      errorMsg.value = err.message
    } else {
      errorMsg.value = '登录失败，请检查网络或服务状态'
    }
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="min-h-screen bg-slate-900 flex items-center justify-center p-4">
    <div class="w-full max-w-md bg-white rounded-xl shadow-2xl overflow-hidden">
      <div class="bg-[#1b2430] px-6 py-5 flex items-center gap-3">
        <div class="w-10 h-10 rounded-lg bg-[#20a53a] flex items-center justify-center text-white">
          <Server class="w-5 h-5" />
        </div>
        <div>
          <h1 class="text-lg font-semibold text-white">Door 运维面板</h1>
          <p class="text-xs text-slate-400">轻量级原生 Linux 服务器管理平台</p>
        </div>
      </div>

      <form @submit.prevent="submitLogin" class="p-6 space-y-4">
        <div
          v-if="errorMsg"
          class="px-3 py-2 rounded-lg bg-red-50 border border-red-200 text-red-700 text-sm"
        >
          {{ errorMsg }}
        </div>

        <div>
          <label class="block text-sm font-medium text-slate-700 mb-1">管理员账号</label>
          <div class="relative">
            <UserIcon class="w-4 h-4 text-slate-400 absolute left-3 top-3" />
            <input
              v-model="username"
              type="text"
              autocomplete="username"
              required
              class="w-full pl-9 pr-3 py-2 border border-slate-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-[#20a53a]"
              placeholder="admin"
            />
          </div>
        </div>

        <div>
          <label class="block text-sm font-medium text-slate-700 mb-1">密码</label>
          <div class="relative">
            <Lock class="w-4 h-4 text-slate-400 absolute left-3 top-3" />
            <input
              v-model="password"
              type="password"
              autocomplete="current-password"
              required
              class="w-full pl-9 pr-3 py-2 border border-slate-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-[#20a53a]"
              placeholder="请输入密码（默认 12345678）"
            />
          </div>
        </div>

        <button
          type="submit"
          :disabled="loading"
          class="w-full py-2.5 bg-[#20a53a] hover:bg-[#1a8e31] disabled:opacity-60 text-white font-medium rounded-lg text-sm transition-colors cursor-pointer"
        >
          {{ loading ? '登录中...' : '立即登录' }}
        </button>

        <p class="text-xs text-slate-500 text-center">
          首次登录默认账号 <code class="px-1 py-0.5 bg-slate-100 rounded">admin</code> /
          <code class="px-1 py-0.5 bg-slate-100 rounded">12345678</code>，登录后需立即修改密码。
        </p>
      </form>
    </div>
  </div>
</template>
