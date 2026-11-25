<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { api, type AuditLogItem, type PanelSettings, type TaskItem } from '../api/client'
import { panelState } from '../stores/panel'

const route = useRoute()
const settings = ref<PanelSettings | null>(null)
const tasks = ref<TaskItem[]>([])
const audits = ref<AuditLogItem[]>([])
const selectedTaskLogs = ref<string | null>(null)

const currentTitle = computed(() => {
  for (const p of panelState.menus) {
    if (p.route === route.path) return p.title
    for (const c of p.children || []) {
      if (c.route === route.path) return c.title
    }
  }
  return '系统模块'
})

async function loadData() {
  try {
    settings.value = await api.getSettings()
  } catch {
    settings.value = null
  }
  if (route.path === '/system/tasks') {
    const res = await api.listTasks()
    tasks.value = res.items || []
  } else if (route.path === '/system/audit') {
    const res = await api.listAuditLogs()
    audits.value = res.items || []
  }
}

async function viewTaskLogs(id: string) {
  const res = await api.getTaskLogs(id)
  selectedTaskLogs.value = res.logs || '(暂无日志输出)'
}

onMounted(loadData)
watch(() => route.path, loadData)
</script>

<template>
  <div class="space-y-5">
    <div>
      <h2 class="text-lg font-semibold text-slate-800">{{ currentTitle }}</h2>
      <p class="text-xs text-slate-500 mt-0.5">路由 {{ route.path }}</p>
    </div>

    <!-- Tasks view -->
    <div v-if="route.path === '/system/tasks'" class="bg-white rounded-xl border border-slate-200 overflow-hidden shadow-xs">
      <div class="px-5 py-3.5 border-b border-slate-100 flex items-center justify-between">
        <h3 class="text-sm font-semibold text-slate-800">异步任务队列</h3>
        <button @click="loadData" class="text-xs text-[#20a53a] hover:underline cursor-pointer">刷新</button>
      </div>
      <table class="w-full text-left text-xs">
        <thead class="bg-slate-50 text-slate-500">
          <tr>
            <th class="px-4 py-2.5 font-medium">任务 ID</th>
            <th class="px-4 py-2.5 font-medium">名称</th>
            <th class="px-4 py-2.5 font-medium">状态</th>
            <th class="px-4 py-2.5 font-medium">进度</th>
            <th class="px-4 py-2.5 font-medium">创建时间</th>
            <th class="px-4 py-2.5 font-medium text-right">日志</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-slate-100">
          <tr v-for="t in tasks" :key="t.id">
            <td class="px-4 py-2.5 font-mono text-slate-500">{{ t.id }}</td>
            <td class="px-4 py-2.5 font-medium text-slate-800">{{ t.title }}</td>
            <td class="px-4 py-2.5">{{ t.status }}</td>
            <td class="px-4 py-2.5">{{ t.progress }}%</td>
            <td class="px-4 py-2.5 text-slate-500">{{ new Date(t.created_at).toLocaleString() }}</td>
            <td class="px-4 py-2.5 text-right">
              <button @click="viewTaskLogs(t.id)" class="text-[#20a53a] hover:underline cursor-pointer">查看日志</button>
            </td>
          </tr>
          <tr v-if="tasks.length === 0">
            <td colspan="6" class="px-4 py-8 text-center text-slate-400">暂无后台任务</td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Audit view -->
    <div v-else-if="route.path === '/system/audit'" class="bg-white rounded-xl border border-slate-200 overflow-hidden shadow-xs">
      <div class="px-5 py-3.5 border-b border-slate-100 flex items-center justify-between">
        <h3 class="text-sm font-semibold text-slate-800">操作审计日志</h3>
        <button @click="loadData" class="text-xs text-[#20a53a] hover:underline cursor-pointer">刷新</button>
      </div>
      <table class="w-full text-left text-xs">
        <thead class="bg-slate-50 text-slate-500">
          <tr>
            <th class="px-4 py-2.5 font-medium">时间</th>
            <th class="px-4 py-2.5 font-medium">操作人</th>
            <th class="px-4 py-2.5 font-medium">动作</th>
            <th class="px-4 py-2.5 font-medium">目标</th>
            <th class="px-4 py-2.5 font-medium">详情</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-slate-100">
          <tr v-for="a in audits" :key="a.id">
            <td class="px-4 py-2.5 font-mono text-slate-500">{{ new Date(a.created_at).toLocaleString() }}</td>
            <td class="px-4 py-2.5 font-medium">{{ a.username }}</td>
            <td class="px-4 py-2.5 font-mono">{{ a.action }}</td>
            <td class="px-4 py-2.5 font-mono">{{ a.target }}</td>
            <td class="px-4 py-2.5 text-slate-500">{{ a.detail }}</td>
          </tr>
        </tbody>
      </table>
    </div>

    <div v-if="selectedTaskLogs !== null" class="fixed inset-0 z-50 bg-black/40 flex items-center justify-center p-4">
      <div class="bg-slate-900 text-slate-100 rounded-xl max-w-2xl w-full p-5 space-y-3 shadow-2xl">
        <div class="flex items-center justify-between">
          <h3 class="text-sm font-semibold">任务执行日志</h3>
          <button @click="selectedTaskLogs = null" class="text-xs text-slate-400 hover:text-white cursor-pointer">关闭</button>
        </div>
        <pre class="bg-black/60 p-3 rounded-lg text-xs font-mono max-h-80 overflow-y-auto whitespace-pre-wrap">{{ selectedTaskLogs }}</pre>
      </div>
    </div>
  </div>
</template>
