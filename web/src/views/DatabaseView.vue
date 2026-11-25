<script setup lang="ts">
import { Archive, Database, Play, Plus, RotateCw, Save, Square, Trash2 } from 'lucide-vue-next'
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { api, ApiError, type DBBackup, type DBInstance, type DBSchema } from '../api/client'

const route = useRoute()

const engine = computed(() => {
  if (route.path === '/databases/mongodb') return 'mongodb'
  if (route.path === '/databases/redis') return 'redis'
  if (route.path === '/databases/postgresql') return 'postgresql'
  return 'mysql'
})

const inst = ref<DBInstance | null>(null)
const schemas = ref<DBSchema[]>([])
const backups = ref<DBBackup[]>([])
const configText = ref('')
const activeTab = ref<'schemas' | 'config' | 'backups'>('schemas')

const installForm = ref({ version: '', port: 0, root_password: '' })
const schemaForm = ref({ db_name: '', db_user: '', charset: 'utf8mb4' })
const msg = ref('')
const errMsg = ref('')

async function load() {
  errMsg.value = ''
  try {
    const info = await api.getDB(engine.value)
    inst.value = info
    installForm.value = { version: info.version, port: info.port, root_password: '' }
    if (info.installed) {
      const [sRes, bRes, cRes] = await Promise.all([
        api.listDBSchemas(engine.value),
        api.listDBBackups(engine.value),
        api.getDBConfig(engine.value),
      ])
      schemas.value = sRes.items || []
      backups.value = bRes.items || []
      configText.value = cRes.config_text || ''
    } else {
      schemas.value = []
      backups.value = []
      configText.value = ''
    }
  } catch (err) {
    errMsg.value = err instanceof ApiError ? err.message : '加载失败'
  }
}

onMounted(load)
watch(engine, () => {
  msg.value = ''
  errMsg.value = ''
  load()
})

async function installEngine() {
  errMsg.value = ''
  try {
    const tsk = await api.installDB(engine.value, installForm.value)
    msg.value = `已提交 ${engine.value.toUpperCase()} 原生部署任务 (${tsk.id})`
    setTimeout(load, 800)
  } catch (err) {
    errMsg.value = err instanceof ApiError ? err.message : '部署失败'
  }
}

async function doAction(act: 'start' | 'stop' | 'restart') {
  errMsg.value = ''
  try {
    inst.value = await api.actionDB(engine.value, act)
  } catch (err) {
    errMsg.value = err instanceof ApiError ? err.message : '操作失败'
  }
}

async function saveConfig() {
  errMsg.value = ''
  try {
    await api.updateDBConfig(engine.value, configText.value)
    msg.value = '数据库配置已保存（重启服务后生效）'
  } catch (err) {
    errMsg.value = err instanceof ApiError ? err.message : '保存失败'
  }
}

async function addSchema() {
  errMsg.value = ''
  try {
    await api.createDBSchema(engine.value, schemaForm.value)
    schemaForm.value = { db_name: '', db_user: '', charset: 'utf8mb4' }
    await load()
  } catch (err) {
    errMsg.value = err instanceof ApiError ? err.message : '建库失败'
  }
}

async function removeSchema(id: number) {
  await api.deleteDBSchema(engine.value, id)
  await load()
}

async function triggerBackup(dbName: string) {
  errMsg.value = ''
  try {
    await api.createDBBackup(engine.value, dbName)
    msg.value = '备份归档已生成到 /door/server/backup/db'
    await load()
  } catch (err) {
    errMsg.value = err instanceof ApiError ? err.message : '备份失败'
  }
}
</script>

<template>
  <div class="space-y-5" v-if="inst">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div>
        <h2 class="text-lg font-semibold text-slate-800">{{ engine.toUpperCase() }} 原生数据库管理</h2>
        <p class="text-xs text-slate-500 mt-0.5">
          隔离目录 <code class="px-1 py-0.5 bg-slate-200/70 rounded">{{ inst.install_dir }}</code>
        </p>
      </div>
    </div>

    <div v-if="msg" class="px-3 py-2 rounded-lg bg-emerald-50 border border-emerald-200 text-emerald-700 text-sm">{{ msg }}</div>
    <div v-if="errMsg" class="px-3 py-2 rounded-lg bg-red-50 border border-red-200 text-red-700 text-sm">{{ errMsg }}</div>

    <!-- Not installed card -->
    <form v-if="!inst.installed" @submit.prevent="installEngine" class="bg-white rounded-xl border border-slate-200 p-6 max-w-xl space-y-4 shadow-xs">
      <div class="flex items-center gap-3">
        <div class="w-10 h-10 rounded-lg bg-emerald-50 text-emerald-700 flex items-center justify-center">
          <Database class="w-5 h-5" />
        </div>
        <div>
          <h3 class="text-sm font-semibold text-slate-800">一键原生部署 {{ engine.toUpperCase() }}</h3>
          <p class="text-xs text-slate-500">自动创建降权系统用户、数据目录、配置文件与 Systemd 托管</p>
        </div>
      </div>

      <div class="grid grid-cols-2 gap-3">
        <div>
          <label class="block text-xs font-medium text-slate-700 mb-1">版本</label>
          <input v-model="installForm.version" required class="w-full px-3 py-2 border border-slate-300 rounded-lg text-sm font-mono" />
        </div>
        <div>
          <label class="block text-xs font-medium text-slate-700 mb-1">端口</label>
          <input v-model.number="installForm.port" type="number" required class="w-full px-3 py-2 border border-slate-300 rounded-lg text-sm font-mono" />
        </div>
        <div class="col-span-2">
          <label class="block text-xs font-medium text-slate-700 mb-1">Root / 管理密码（留空自动生成强随机密码）</label>
          <input v-model="installForm.root_password" type="password" placeholder="留空自动生成" class="w-full px-3 py-2 border border-slate-300 rounded-lg text-sm" />
        </div>
      </div>

      <button type="submit" class="px-4 py-2 rounded-lg bg-[#20a53a] hover:bg-[#1a8e31] text-white text-xs font-medium cursor-pointer">
        立即部署到 {{ inst.install_dir }}
      </button>
    </form>

    <!-- Installed management workspace -->
    <template v-else>
      <div class="bg-white rounded-xl border border-slate-200 p-5 flex flex-wrap items-center justify-between gap-4 shadow-xs">
        <div class="space-y-1">
          <div class="flex items-center gap-2 text-sm font-semibold text-slate-800">
            <span>{{ engine.toUpperCase() }} {{ inst.version }}</span>
            <span class="px-2 py-0.5 rounded text-xs" :class="inst.status === 'active' ? 'bg-emerald-100 text-emerald-700' : 'bg-slate-100 text-slate-600'">
              {{ inst.status }}
            </span>
          </div>
          <div class="text-xs text-slate-500 font-mono">
            端口: {{ inst.port }} · 管理账号: {{ inst.root_user }} · 密码: {{ inst.root_password }}
          </div>
        </div>

        <div class="flex gap-2">
          <button @click="doAction('start')" class="inline-flex items-center gap-1 px-3 py-1.5 rounded-lg bg-emerald-50 text-emerald-700 text-xs cursor-pointer"><Play class="w-3.5 h-3.5" /> 启动</button>
          <button @click="doAction('stop')" class="inline-flex items-center gap-1 px-3 py-1.5 rounded-lg bg-slate-100 text-slate-700 text-xs cursor-pointer"><Square class="w-3.5 h-3.5" /> 停止</button>
          <button @click="doAction('restart')" class="inline-flex items-center gap-1 px-3 py-1.5 rounded-lg bg-blue-50 text-blue-700 text-xs cursor-pointer"><RotateCw class="w-3.5 h-3.5" /> 重启</button>
        </div>
      </div>

      <div class="flex gap-2 border-b border-slate-200">
        <button @click="activeTab = 'schemas'" class="px-4 py-2 text-xs font-medium border-b-2 cursor-pointer" :class="activeTab === 'schemas' ? 'border-[#20a53a] text-[#20a53a]' : 'border-transparent text-slate-500'">数据库与用户</button>
        <button @click="activeTab = 'config'" class="px-4 py-2 text-xs font-medium border-b-2 cursor-pointer" :class="activeTab === 'config' ? 'border-[#20a53a] text-[#20a53a]' : 'border-transparent text-slate-500'">配置文件</button>
        <button @click="activeTab = 'backups'" class="px-4 py-2 text-xs font-medium border-b-2 cursor-pointer" :class="activeTab === 'backups' ? 'border-[#20a53a] text-[#20a53a]' : 'border-transparent text-slate-500'">备份管理</button>
      </div>

      <!-- Schemas -->
      <div v-if="activeTab === 'schemas'" class="space-y-4">
        <form @submit.prevent="addSchema" class="bg-white rounded-xl border border-slate-200 p-4 flex flex-wrap items-end gap-3 shadow-xs">
          <div>
            <label class="block text-xs font-medium text-slate-700 mb-1">数据库名</label>
            <input v-model="schemaForm.db_name" required placeholder="app_prod" class="px-3 py-1.5 border border-slate-300 rounded-lg text-xs font-mono" />
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-700 mb-1">用户名</label>
            <input v-model="schemaForm.db_user" placeholder="app_user" class="px-3 py-1.5 border border-slate-300 rounded-lg text-xs font-mono" />
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-700 mb-1">字符集</label>
            <input v-model="schemaForm.charset" class="px-3 py-1.5 border border-slate-300 rounded-lg text-xs font-mono w-28" />
          </div>
          <button type="submit" class="inline-flex items-center gap-1 px-3.5 py-1.5 rounded-lg bg-[#20a53a] text-white text-xs font-medium cursor-pointer">
            <Plus class="w-3.5 h-3.5" /> 添加数据库
          </button>
        </form>

        <div class="bg-white rounded-xl border border-slate-200 overflow-hidden shadow-xs">
          <table class="w-full text-left text-xs">
            <thead class="bg-slate-50 text-slate-500">
              <tr>
                <th class="px-4 py-2.5 font-medium">库名</th>
                <th class="px-4 py-2.5 font-medium">用户</th>
                <th class="px-4 py-2.5 font-medium">字符集</th>
                <th class="px-4 py-2.5 font-medium text-right">操作</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-slate-100">
              <tr v-for="s in schemas" :key="s.id">
                <td class="px-4 py-2.5 font-mono font-semibold">{{ s.db_name }}</td>
                <td class="px-4 py-2.5 font-mono">{{ s.db_user || '-' }}</td>
                <td class="px-4 py-2.5 font-mono">{{ s.charset }}</td>
                <td class="px-4 py-2.5 text-right space-x-2">
                  <button @click="triggerBackup(s.db_name)" class="inline-flex items-center gap-1 text-emerald-700 hover:underline cursor-pointer"><Archive class="w-3 h-3" /> 备份</button>
                  <button @click="removeSchema(s.id)" class="inline-flex items-center gap-1 text-red-600 hover:underline cursor-pointer"><Trash2 class="w-3 h-3" /> 删除</button>
                </td>
              </tr>
              <tr v-if="schemas.length === 0">
                <td colspan="4" class="px-4 py-8 text-center text-slate-400">暂无数据库记录</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <!-- Config -->
      <div v-else-if="activeTab === 'config'" class="bg-white rounded-xl border border-slate-200 p-5 space-y-3 shadow-xs">
        <div class="flex items-center justify-between">
          <span class="text-xs font-mono text-slate-500">{{ inst.config_path }}</span>
          <button @click="saveConfig" class="inline-flex items-center gap-1 px-3.5 py-1.5 rounded-lg bg-[#20a53a] text-white text-xs cursor-pointer"><Save class="w-3.5 h-3.5" /> 保存配置</button>
        </div>
        <textarea v-model="configText" rows="14" class="w-full p-3 font-mono text-xs bg-slate-900 text-emerald-300 rounded-lg" />
      </div>

      <!-- Backups -->
      <div v-else class="bg-white rounded-xl border border-slate-200 overflow-hidden shadow-xs">
        <div class="px-4 py-3 border-b border-slate-100 flex justify-between items-center">
          <span class="text-xs font-semibold text-slate-700">备份记录 (/door/server/backup/db)</span>
          <button @click="triggerBackup('all')" class="px-3 py-1.5 rounded-lg bg-[#20a53a] text-white text-xs cursor-pointer">全库备份</button>
        </div>
        <table class="w-full text-left text-xs">
          <thead class="bg-slate-50 text-slate-500">
            <tr>
              <th class="px-4 py-2.5 font-medium">库名</th>
              <th class="px-4 py-2.5 font-medium">归档路径</th>
              <th class="px-4 py-2.5 font-medium">大小</th>
              <th class="px-4 py-2.5 font-medium">时间</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-slate-100">
            <tr v-for="b in backups" :key="b.id">
              <td class="px-4 py-2.5 font-mono font-semibold">{{ b.db_name }}</td>
              <td class="px-4 py-2.5 font-mono text-slate-500">{{ b.file_path }}</td>
              <td class="px-4 py-2.5">{{ b.size_bytes }} B</td>
              <td class="px-4 py-2.5 text-slate-500">{{ new Date(b.created_at).toLocaleString() }}</td>
            </tr>
            <tr v-if="backups.length === 0">
              <td colspan="4" class="px-4 py-8 text-center text-slate-400">暂无备份记录</td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>
  </div>
</template>
