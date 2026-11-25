<script setup lang="ts">
import { KeyRound } from 'lucide-vue-next'
import { onMounted, ref } from 'vue'
import { api, ApiError, type AuditLogItem } from '../api/client'

const oldPassword = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
const saving = ref(false)
const msg = ref('')
const errMsg = ref('')

const logs = ref<AuditLogItem[]>([])

async function loadLogs() {
  try {
    const res = await api.listAuditLogs()
    logs.value = res.items || []
  } catch {
    logs.value = []
  }
}

onMounted(loadLogs)

async function submitPassword() {
  msg.value = ''
  errMsg.value = ''
  if (newPassword.value.length < 8) {
    errMsg.value = '新密码至少 8 位'
    return
  }
  if (newPassword.value !== confirmPassword.value) {
    errMsg.value = '两次输入的新密码不一致'
    return
  }

  saving.value = true
  try {
    await api.changePassword(oldPassword.value, newPassword.value)
    oldPassword.value = ''
    newPassword.value = ''
    confirmPassword.value = ''
    msg.value = '管理员密码已更新'
    await loadLogs()
  } catch (err) {
    errMsg.value = err instanceof ApiError ? err.message : '修改密码失败'
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="max-w-4xl space-y-6">
    <div>
      <h2 class="text-lg font-semibold text-slate-800">安全设置</h2>
      <p class="text-xs text-slate-500 mt-0.5">管理员密码轮换与操作审计日志</p>
    </div>

    <form @submit.prevent="submitPassword" class="bg-white rounded-xl border border-slate-200 p-6 space-y-4 shadow-xs">
      <h3 class="text-sm font-semibold text-slate-800">修改管理员密码</h3>

      <div v-if="msg" class="px-3 py-2 rounded-lg bg-emerald-50 border border-emerald-200 text-emerald-700 text-sm">
        {{ msg }}
      </div>
      <div v-if="errMsg" class="px-3 py-2 rounded-lg bg-red-50 border border-red-200 text-red-700 text-sm">
        {{ errMsg }}
      </div>

      <div class="grid grid-cols-1 md:grid-cols-3 gap-4">
        <div>
          <label class="block text-xs font-medium text-slate-700 mb-1">原密码</label>
          <input
            v-model="oldPassword"
            type="password"
            required
            class="w-full px-3 py-2 border border-slate-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-[#20a53a]"
          />
        </div>
        <div>
          <label class="block text-xs font-medium text-slate-700 mb-1">新密码（≥ 8 位）</label>
          <input
            v-model="newPassword"
            type="password"
            required
            minlength="8"
            class="w-full px-3 py-2 border border-slate-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-[#20a53a]"
          />
        </div>
        <div>
          <label class="block text-xs font-medium text-slate-700 mb-1">确认新密码</label>
          <input
            v-model="confirmPassword"
            type="password"
            required
            minlength="8"
            class="w-full px-3 py-2 border border-slate-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-[#20a53a]"
          />
        </div>
      </div>

      <div class="flex justify-end">
        <button
          type="submit"
          :disabled="saving"
          class="inline-flex items-center gap-1.5 px-4 py-2 rounded-lg bg-[#20a53a] hover:bg-[#1a8e31] text-white text-xs font-medium cursor-pointer disabled:opacity-60"
        >
          <KeyRound class="w-3.5 h-3.5" />
          {{ saving ? '提交中...' : '更新密码' }}
        </button>
      </div>
    </form>

    <div class="bg-white rounded-xl border border-slate-200 overflow-hidden shadow-xs">
      <div class="px-5 py-3.5 border-b border-slate-100 flex items-center justify-between">
        <h3 class="text-sm font-semibold text-slate-800">最近审计日志</h3>
        <button @click="loadLogs" class="text-xs text-[#20a53a] hover:underline cursor-pointer">刷新</button>
      </div>
      <div class="overflow-x-auto">
        <table class="w-full text-left text-xs">
          <thead class="bg-slate-50 text-slate-500">
            <tr>
              <th class="px-4 py-2.5 font-medium">时间</th>
              <th class="px-4 py-2.5 font-medium">操作人</th>
              <th class="px-4 py-2.5 font-medium">动作</th>
              <th class="px-4 py-2.5 font-medium">目标</th>
              <th class="px-4 py-2.5 font-medium">详情</th>
              <th class="px-4 py-2.5 font-medium">来源 IP</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-slate-100 text-slate-700">
            <tr v-for="log in logs" :key="log.id">
              <td class="px-4 py-2.5 whitespace-nowrap font-mono text-slate-500">
                {{ new Date(log.created_at).toLocaleString() }}
              </td>
              <td class="px-4 py-2.5 font-medium">{{ log.username }}</td>
              <td class="px-4 py-2.5">
                <span class="px-1.5 py-0.5 rounded bg-slate-100 font-mono">{{ log.action }}</span>
              </td>
              <td class="px-4 py-2.5 font-mono">{{ log.target }}</td>
              <td class="px-4 py-2.5 text-slate-500">{{ log.detail }}</td>
              <td class="px-4 py-2.5 font-mono text-slate-500">{{ log.ip }}</td>
            </tr>
            <tr v-if="logs.length === 0">
              <td colspan="6" class="px-4 py-6 text-center text-slate-400">暂无审计记录</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>
