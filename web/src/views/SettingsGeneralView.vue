<script setup lang="ts">
import { Save } from 'lucide-vue-next'
import { onMounted, ref } from 'vue'
import { api, ApiError, type PanelSettings } from '../api/client'
import { loadSession } from '../stores/panel'

const form = ref<PanelSettings>({
  panel_name: 'Door 运维面板',
  listen_addr: ':12345',
  server_dir: '/door/server',
  database_dir: '/door/database',
  project_dir: '/door/project',
  nginx_dir: '/door/nginx',
  memory_limit_mb: 128,
  session_ttl_hours: 24,
  safe_entry: '',
})

const loading = ref(true)
const saving = ref(false)
const message = ref('')
const errorMsg = ref('')

onMounted(async () => {
  try {
    form.value = await api.getSettings()
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : '加载配置失败'
  } finally {
    loading.value = false
  }
})

async function save() {
  saving.value = true
  message.value = ''
  errorMsg.value = ''
  try {
    form.value = await api.updateSettings(form.value)
    await loadSession()
    message.value = '面板配置已保存（端口或目录变更将在重启面板服务后生效）'
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : '保存失败'
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="max-w-3xl space-y-5">
    <div>
      <h2 class="text-lg font-semibold text-slate-800">基础设置</h2>
      <p class="text-xs text-slate-500 mt-0.5">配置 Door 面板自身参数及核心隔离安装目录</p>
    </div>

    <div v-if="loading" class="bg-white rounded-xl border border-slate-200 p-6 text-sm text-slate-500">
      正在加载配置...
    </div>

    <form v-else @submit.prevent="save" class="bg-white rounded-xl border border-slate-200 p-6 space-y-5 shadow-xs">
      <div v-if="message" class="px-3 py-2 rounded-lg bg-emerald-50 border border-emerald-200 text-emerald-700 text-sm">
        {{ message }}
      </div>
      <div v-if="errorMsg" class="px-3 py-2 rounded-lg bg-red-50 border border-red-200 text-red-700 text-sm">
        {{ errorMsg }}
      </div>

      <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
        <div>
          <label class="block text-xs font-medium text-slate-700 mb-1">面板名称</label>
          <input
            v-model="form.panel_name"
            type="text"
            required
            class="w-full px-3 py-2 border border-slate-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-[#20a53a]"
          />
        </div>
        <div>
          <label class="block text-xs font-medium text-slate-700 mb-1">监听地址与端口（默认 :12345）</label>
          <input
            v-model="form.listen_addr"
            type="text"
            required
            class="w-full px-3 py-2 border border-slate-300 rounded-lg text-sm font-mono focus:outline-none focus:ring-2 focus:ring-[#20a53a]"
          />
        </div>
      </div>

      <div class="border-t border-slate-100 pt-4">
        <h3 class="text-sm font-semibold text-slate-800 mb-3">核心目录路径配置</h3>
        <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
          <div>
            <label class="block text-xs font-medium text-slate-700 mb-1">面板与运行时目录 (server_dir)</label>
            <input
              v-model="form.server_dir"
              type="text"
              required
              class="w-full px-3 py-2 border border-slate-300 rounded-lg text-sm font-mono focus:outline-none focus:ring-2 focus:ring-[#20a53a]"
            />
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-700 mb-1">数据库目录 (database_dir)</label>
            <input
              v-model="form.database_dir"
              type="text"
              required
              class="w-full px-3 py-2 border border-slate-300 rounded-lg text-sm font-mono focus:outline-none focus:ring-2 focus:ring-[#20a53a]"
            />
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-700 mb-1">项目部署目录 (project_dir)</label>
            <input
              v-model="form.project_dir"
              type="text"
              required
              class="w-full px-3 py-2 border border-slate-300 rounded-lg text-sm font-mono focus:outline-none focus:ring-2 focus:ring-[#20a53a]"
            />
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-700 mb-1">Nginx 目录 (nginx_dir)</label>
            <input
              v-model="form.nginx_dir"
              type="text"
              required
              class="w-full px-3 py-2 border border-slate-300 rounded-lg text-sm font-mono focus:outline-none focus:ring-2 focus:ring-[#20a53a]"
            />
          </div>
        </div>
      </div>

      <div class="border-t border-slate-100 pt-4 grid grid-cols-1 md:grid-cols-3 gap-4">
        <div>
          <label class="block text-xs font-medium text-slate-700 mb-1">Go 内存软上限 (MiB)</label>
          <input
            v-model.number="form.memory_limit_mb"
            type="number"
            min="0"
            max="65536"
            class="w-full px-3 py-2 border border-slate-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-[#20a53a]"
          />
        </div>
        <div>
          <label class="block text-xs font-medium text-slate-700 mb-1">Session 有效期 (小时)</label>
          <input
            v-model.number="form.session_ttl_hours"
            type="number"
            min="1"
            max="720"
            class="w-full px-3 py-2 border border-slate-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-[#20a53a]"
          />
        </div>
        <div>
          <label class="block text-xs font-medium text-slate-700 mb-1">安全入口前缀（可选）</label>
          <input
            v-model="form.safe_entry"
            type="text"
            placeholder="例如 /door-admin"
            class="w-full px-3 py-2 border border-slate-300 rounded-lg text-sm font-mono focus:outline-none focus:ring-2 focus:ring-[#20a53a]"
          />
        </div>
      </div>

      <div class="pt-2 flex justify-end">
        <button
          type="submit"
          :disabled="saving"
          class="inline-flex items-center gap-2 px-4 py-2 rounded-lg bg-[#20a53a] hover:bg-[#1a8e31] text-white text-sm font-medium cursor-pointer disabled:opacity-60"
        >
          <Save class="w-4 h-4" />
          {{ saving ? '保存中...' : '保存配置' }}
        </button>
      </div>
    </form>
  </div>
</template>
