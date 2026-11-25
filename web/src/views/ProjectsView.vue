<script setup lang="ts">
import { Play, Plus, RotateCw, Square, Terminal, Trash2 } from 'lucide-vue-next'
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { api, ApiError, type ProjectItem, type RuntimeItem } from '../api/client'

const route = useRoute()

const activeMode = computed<'go' | 'java' | 'python' | 'runtimes'>(() => {
  if (route.path === '/projects/java') return 'java'
  if (route.path === '/projects/python') return 'python'
  if (route.path === '/projects/runtimes') return 'runtimes'
  return 'go'
})

const projects = ref<ProjectItem[]>([])
const runtimes = ref<RuntimeItem[]>([])
const loading = ref(false)
const msg = ref('')
const errMsg = ref('')

// New project modal
const showCreateModal = ref(false)
const createForm = ref({
  name: '',
  lang: 'go' as 'go' | 'java' | 'python',
  entry: '',
  args: '',
  runtime_id: null as number | null,
  port: 8080,
  run_user: 'www',
})

// Logs modal
const logModalTitle = ref('')
const logContent = ref('')
const showLogModal = ref(false)

// Runtime install presets
const runtimePresets = [
  { kind: 'java', version: '21', label: 'OpenJDK 21 LTS' },
  { kind: 'java', version: '17', label: 'OpenJDK 17 LTS' },
  { kind: 'python', version: '3.12', label: 'Python 3.12' },
  { kind: 'python', version: '3.11', label: 'Python 3.11' },
  { kind: 'go', version: '1.26', label: 'Go 1.26' },
]

const filteredRuntimes = computed(() =>
  runtimes.value.filter((r) => r.kind === createForm.value.lang),
)

async function load() {
  loading.value = true
  errMsg.value = ''
  try {
    const rtRes = await api.listRuntimes()
    runtimes.value = rtRes.items || []
    if (activeMode.value !== 'runtimes') {
      const projRes = await api.listProjects(activeMode.value)
      projects.value = projRes.items || []
    }
  } catch (err) {
    errMsg.value = err instanceof ApiError ? err.message : '加载失败'
  } finally {
    loading.value = false
  }
}

onMounted(load)
watch(activeMode, () => {
  msg.value = ''
  errMsg.value = ''
  load()
})

function openCreateModal() {
  const lang = activeMode.value === 'runtimes' ? 'go' : activeMode.value
  createForm.value = {
    name: '',
    lang,
    entry: lang === 'java' ? 'app.jar' : lang === 'python' ? 'app.py' : 'server',
    args: '',
    runtime_id: null,
    port: 8080,
    run_user: 'www',
  }
  showCreateModal.value = true
}

async function submitProject() {
  errMsg.value = ''
  msg.value = ''
  try {
    await api.createProject({ ...createForm.value, env: {} })
    showCreateModal.value = false
    msg.value = `项目 ${createForm.value.name} 已创建并生成 Systemd 托管配置`
    await load()
  } catch (err) {
    errMsg.value = err instanceof ApiError ? err.message : '创建项目失败'
  }
}

async function doProjectAction(p: ProjectItem, action: 'start' | 'stop' | 'restart') {
  errMsg.value = ''
  try {
    await api.actionProject(p.id, action)
    await load()
  } catch (err) {
    errMsg.value = err instanceof ApiError ? err.message : '操作失败'
  }
}

async function openLogs(p: ProjectItem) {
  logModalTitle.value = `${p.name} 运行日志`
  showLogModal.value = true
  try {
    const res = await api.getProjectLogs(p.id)
    logContent.value = res.logs || '(暂无输出)'
  } catch (err) {
    logContent.value = err instanceof ApiError ? err.message : '读取日志失败'
  }
}

async function removeProject(p: ProjectItem) {
  try {
    await api.deleteProject(p.id)
    msg.value = `项目 ${p.name} 已移除`
    await load()
  } catch (err) {
    errMsg.value = err instanceof ApiError ? err.message : '删除失败'
  }
}

async function installPreset(kind: string, version: string) {
  errMsg.value = ''
  msg.value = ''
  try {
    const tsk = await api.installRuntime(kind, version)
    msg.value = `已提交后台安装任务 ${tsk.id}，正在部署至 /door/server/${kind}`
    setTimeout(load, 800)
  } catch (err) {
    errMsg.value = err instanceof ApiError ? err.message : '安装任务提交失败'
  }
}

async function removeRuntime(id: number) {
  try {
    await api.deleteRuntime(id)
    await load()
  } catch (err) {
    errMsg.value = err instanceof ApiError ? err.message : '卸载失败'
  }
}
</script>

<template>
  <div class="space-y-5">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div>
        <h2 class="text-lg font-semibold text-slate-800">
          {{
            activeMode === 'runtimes'
              ? '多语言运行环境管理'
              : `${activeMode.toUpperCase()} 项目部署`
          }}
        </h2>
        <p class="text-xs text-slate-500 mt-0.5">
          {{
            activeMode === 'runtimes'
              ? '隔离安装至 /door/server/{java,python,go}，支持多版本共存'
              : '项目目录托管于 /door/project/<项目名>，由 Systemd 降权守护进程'
          }}
        </p>
      </div>

      <button
        v-if="activeMode !== 'runtimes'"
        type="button"
        @click="openCreateModal"
        class="inline-flex items-center gap-1.5 px-3.5 py-2 rounded-lg bg-[#20a53a] hover:bg-[#1a8e31] text-white text-xs font-medium cursor-pointer"
      >
        <Plus class="w-4 h-4" />
        添加 {{ activeMode.toUpperCase() }} 项目
      </button>
    </div>

    <div v-if="msg" class="px-3 py-2 rounded-lg bg-emerald-50 border border-emerald-200 text-emerald-700 text-sm">
      {{ msg }}
    </div>
    <div v-if="errMsg" class="px-3 py-2 rounded-lg bg-red-50 border border-red-200 text-red-700 text-sm">
      {{ errMsg }}
    </div>

    <!-- Runtimes Management -->
    <template v-if="activeMode === 'runtimes'">
      <div class="grid grid-cols-1 md:grid-cols-3 gap-3">
        <div
          v-for="preset in runtimePresets"
          :key="preset.kind + preset.version"
          class="bg-white rounded-xl border border-slate-200 p-4 flex items-center justify-between shadow-xs"
        >
          <div>
            <div class="text-sm font-semibold text-slate-800">{{ preset.label }}</div>
            <div class="text-xs font-mono text-slate-400 mt-0.5">/door/server/{{ preset.kind }}/{{ preset.kind }}-{{ preset.version }}</div>
          </div>
          <button
            type="button"
            @click="installPreset(preset.kind, preset.version)"
            class="px-3 py-1.5 rounded-lg bg-emerald-50 text-emerald-700 hover:bg-emerald-100 text-xs font-medium cursor-pointer"
          >
            一键部署
          </button>
        </div>
      </div>

      <div class="bg-white rounded-xl border border-slate-200 overflow-hidden shadow-xs">
        <div class="px-5 py-3.5 border-b border-slate-100 text-sm font-semibold text-slate-800">
          已安装运行环境
        </div>
        <table class="w-full text-left text-xs">
          <thead class="bg-slate-50 text-slate-500">
            <tr>
              <th class="px-4 py-2.5 font-medium">类型</th>
              <th class="px-4 py-2.5 font-medium">版本</th>
              <th class="px-4 py-2.5 font-medium">二进制路径</th>
              <th class="px-4 py-2.5 font-medium">状态</th>
              <th class="px-4 py-2.5 font-medium text-right">操作</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-slate-100">
            <tr v-for="rt in runtimes" :key="rt.id">
              <td class="px-4 py-2.5 uppercase font-semibold">{{ rt.kind }}</td>
              <td class="px-4 py-2.5 font-mono">{{ rt.version }}</td>
              <td class="px-4 py-2.5 font-mono text-slate-500">{{ rt.binary_path }}</td>
              <td class="px-4 py-2.5">
                <span class="px-2 py-0.5 rounded bg-emerald-100 text-emerald-700">{{ rt.status }}</span>
              </td>
              <td class="px-4 py-2.5 text-right">
                <button
                  type="button"
                  @click="removeRuntime(rt.id)"
                  class="text-red-600 hover:underline cursor-pointer"
                >
                  卸载
                </button>
              </td>
            </tr>
            <tr v-if="runtimes.length === 0">
              <td colspan="5" class="px-4 py-8 text-center text-slate-400">尚未安装托管运行环境</td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>

    <!-- Projects Table -->
    <div v-else class="bg-white rounded-xl border border-slate-200 overflow-hidden shadow-xs">
      <table class="w-full text-left text-xs">
        <thead class="bg-slate-50 text-slate-500">
          <tr>
            <th class="px-4 py-3 font-medium">项目名称</th>
            <th class="px-4 py-3 font-medium">端口</th>
            <th class="px-4 py-3 font-medium">目录与入口</th>
            <th class="px-4 py-3 font-medium">运行用户</th>
            <th class="px-4 py-3 font-medium">状态</th>
            <th class="px-4 py-3 font-medium text-right">操作</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-slate-100 text-slate-700">
          <tr v-for="p in projects" :key="p.id">
            <td class="px-4 py-3 font-semibold text-slate-800">{{ p.name }}</td>
            <td class="px-4 py-3 font-mono">{{ p.port || '-' }}</td>
            <td class="px-4 py-3">
              <div class="font-mono text-slate-700">{{ p.entry }} {{ p.args }}</div>
              <div class="font-mono text-[11px] text-slate-400">{{ p.root_dir }}</div>
            </td>
            <td class="px-4 py-3 font-mono">{{ p.run_user }}</td>
            <td class="px-4 py-3">
              <span
                class="px-2 py-0.5 rounded text-[11px] font-medium"
                :class="p.status === 'active' ? 'bg-emerald-100 text-emerald-700' : 'bg-slate-100 text-slate-600'"
              >
                {{ p.status }}
              </span>
            </td>
            <td class="px-4 py-3 text-right space-x-1.5 whitespace-nowrap">
              <button
                type="button"
                @click="doProjectAction(p, 'start')"
                class="inline-flex items-center gap-1 px-2 py-1 rounded bg-emerald-50 text-emerald-700 hover:bg-emerald-100 cursor-pointer"
              >
                <Play class="w-3 h-3" /> 启动
              </button>
              <button
                type="button"
                @click="doProjectAction(p, 'stop')"
                class="inline-flex items-center gap-1 px-2 py-1 rounded bg-slate-100 text-slate-700 hover:bg-slate-200 cursor-pointer"
              >
                <Square class="w-3 h-3" /> 停止
              </button>
              <button
                type="button"
                @click="doProjectAction(p, 'restart')"
                class="inline-flex items-center gap-1 px-2 py-1 rounded bg-blue-50 text-blue-700 hover:bg-blue-100 cursor-pointer"
              >
                <RotateCw class="w-3 h-3" /> 重启
              </button>
              <button
                type="button"
                @click="openLogs(p)"
                class="inline-flex items-center gap-1 px-2 py-1 rounded bg-slate-100 text-slate-700 hover:bg-slate-200 cursor-pointer"
              >
                <Terminal class="w-3 h-3" /> 日志
              </button>
              <button
                type="button"
                @click="removeProject(p)"
                class="inline-flex items-center gap-1 px-2 py-1 rounded bg-red-50 text-red-600 hover:bg-red-100 cursor-pointer"
              >
                <Trash2 class="w-3 h-3" /> 删除
              </button>
            </td>
          </tr>
          <tr v-if="!loading && projects.length === 0">
            <td colspan="6" class="px-4 py-10 text-center text-slate-400">
              暂无 {{ activeMode.toUpperCase() }} 项目，点击右上角添加
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Create Modal -->
    <div v-if="showCreateModal" class="fixed inset-0 z-50 bg-black/40 flex items-center justify-center p-4">
      <form @submit.prevent="submitProject" class="bg-white rounded-xl max-w-lg w-full p-6 space-y-4 shadow-xl">
        <h3 class="text-base font-semibold text-slate-800">新建 {{ createForm.lang.toUpperCase() }} 托管项目</h3>
        <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <div>
            <label class="block text-xs font-medium text-slate-700 mb-1">项目名称</label>
            <input v-model="createForm.name" required placeholder="my-api" class="w-full px-3 py-2 border border-slate-300 rounded-lg text-sm" />
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-700 mb-1">监听端口</label>
            <input v-model.number="createForm.port" type="number" class="w-full px-3 py-2 border border-slate-300 rounded-lg text-sm" />
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-700 mb-1">入口文件</label>
            <input v-model="createForm.entry" required class="w-full px-3 py-2 border border-slate-300 rounded-lg text-sm font-mono" />
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-700 mb-1">运行用户</label>
            <input v-model="createForm.run_user" class="w-full px-3 py-2 border border-slate-300 rounded-lg text-sm font-mono" />
          </div>
        </div>
        <div v-if="filteredRuntimes.length > 0">
          <label class="block text-xs font-medium text-slate-700 mb-1">绑定运行环境</label>
          <select v-model="createForm.runtime_id" class="w-full px-3 py-2 border border-slate-300 rounded-lg text-sm">
            <option :value="null">默认系统运行时</option>
            <option v-for="rt in filteredRuntimes" :key="rt.id" :value="rt.id">
              {{ rt.kind }} {{ rt.version }} ({{ rt.binary_path }})
            </option>
          </select>
        </div>
        <div>
          <label class="block text-xs font-medium text-slate-700 mb-1">启动参数（可选）</label>
          <input v-model="createForm.args" placeholder="--port 8080" class="w-full px-3 py-2 border border-slate-300 rounded-lg text-sm font-mono" />
        </div>
        <div class="flex justify-end gap-2 pt-2">
          <button type="button" @click="showCreateModal = false" class="px-4 py-2 rounded-lg border border-slate-300 text-xs cursor-pointer">取消</button>
          <button type="submit" class="px-4 py-2 rounded-lg bg-[#20a53a] text-white text-xs font-medium cursor-pointer">创建并生成服务</button>
        </div>
      </form>
    </div>

    <!-- Logs Modal -->
    <div v-if="showLogModal" class="fixed inset-0 z-50 bg-black/40 flex items-center justify-center p-4">
      <div class="bg-slate-900 text-slate-100 rounded-xl max-w-2xl w-full p-5 space-y-3 shadow-2xl">
        <div class="flex items-center justify-between">
          <h3 class="text-sm font-semibold">{{ logModalTitle }}</h3>
          <button type="button" @click="showLogModal = false" class="text-xs text-slate-400 hover:text-white cursor-pointer">关闭</button>
        </div>
        <pre class="bg-black/60 p-3 rounded-lg text-xs font-mono max-h-80 overflow-y-auto whitespace-pre-wrap">{{ logContent }}</pre>
      </div>
    </div>
  </div>
</template>
