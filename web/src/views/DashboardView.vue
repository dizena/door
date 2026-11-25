<script setup lang="ts">
import { Activity, Cpu, FolderOpen, HardDrive, MemoryStick, RefreshCw } from 'lucide-vue-next'
import { onMounted, onUnmounted, ref } from 'vue'
import { api, type SystemSnapshot } from '../api/client'

const snap = ref<SystemSnapshot | null>(null)
const loading = ref(false)
let timer: number | undefined

function formatBytes(bytes: number) {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  return `${(bytes / 1024 ** i).toFixed(1)} ${units[i]}`
}

async function fetchSnapshot() {
  loading.value = true
  try {
    snap.value = await api.snapshot()
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  fetchSnapshot()
  timer = window.setInterval(fetchSnapshot, 5000)
})

onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>

<template>
  <div class="space-y-5">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div>
        <h2 class="text-lg font-semibold text-slate-800">系统状态概览</h2>
        <p class="text-xs text-slate-500 mt-0.5" v-if="snap">
          主机：{{ snap.hostname }} · {{ snap.platform || snap.os }} ({{ snap.arch }}) · 面板自身内存占用
          <span class="font-semibold text-emerald-600">{{ formatBytes(snap.process_mem_bytes) }}</span>
        </p>
      </div>
      <button
        type="button"
        @click="fetchSnapshot"
        class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-white border border-slate-200 text-xs font-medium text-slate-700 hover:bg-slate-50 cursor-pointer"
      >
        <RefreshCw class="w-3.5 h-3.5" :class="loading ? 'animate-spin' : ''" />
        刷新指标
      </button>
    </div>

    <!-- Resource Cards -->
    <div v-if="snap" class="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-4 gap-4">
      <div class="bg-white rounded-xl border border-slate-200 p-4 shadow-xs">
        <div class="flex items-center justify-between text-slate-500 text-xs mb-2">
          <span>CPU 使用率 ({{ snap.cpu.cores }} 核)</span>
          <Cpu class="w-4 h-4 text-emerald-600" />
        </div>
        <div class="text-2xl font-bold text-slate-800">{{ snap.cpu.usage_percent.toFixed(1) }}%</div>
        <div class="w-full h-2 bg-slate-100 rounded-full mt-3 overflow-hidden">
          <div
            class="h-full bg-[#20a53a] transition-all"
            :style="{ width: Math.min(100, snap.cpu.usage_percent) + '%' }"
          />
        </div>
        <div class="text-[11px] text-slate-500 mt-2">
          负载: {{ snap.cpu.load1.toFixed(2) }} / {{ snap.cpu.load5.toFixed(2) }} / {{ snap.cpu.load15.toFixed(2) }}
        </div>
      </div>

      <div class="bg-white rounded-xl border border-slate-200 p-4 shadow-xs">
        <div class="flex items-center justify-between text-slate-500 text-xs mb-2">
          <span>内存使用率</span>
          <MemoryStick class="w-4 h-4 text-blue-600" />
        </div>
        <div class="text-2xl font-bold text-slate-800">{{ snap.memory.used_percent.toFixed(1) }}%</div>
        <div class="w-full h-2 bg-slate-100 rounded-full mt-3 overflow-hidden">
          <div
            class="h-full bg-blue-500 transition-all"
            :style="{ width: Math.min(100, snap.memory.used_percent) + '%' }"
          />
        </div>
        <div class="text-[11px] text-slate-500 mt-2">
          {{ formatBytes(snap.memory.used_bytes) }} / {{ formatBytes(snap.memory.total_bytes) }}
        </div>
      </div>

      <div class="bg-white rounded-xl border border-slate-200 p-4 shadow-xs">
        <div class="flex items-center justify-between text-slate-500 text-xs mb-2">
          <span>磁盘占用</span>
          <HardDrive class="w-4 h-4 text-amber-600" />
        </div>
        <div class="text-2xl font-bold text-slate-800">{{ snap.disk.used_percent.toFixed(1) }}%</div>
        <div class="w-full h-2 bg-slate-100 rounded-full mt-3 overflow-hidden">
          <div
            class="h-full bg-amber-500 transition-all"
            :style="{ width: Math.min(100, snap.disk.used_percent) + '%' }"
          />
        </div>
        <div class="text-[11px] text-slate-500 mt-2">
          可用 {{ formatBytes(snap.disk.free_bytes) }} / 共 {{ formatBytes(snap.disk.total_bytes) }}
        </div>
      </div>

      <div class="bg-white rounded-xl border border-slate-200 p-4 shadow-xs">
        <div class="flex items-center justify-between text-slate-500 text-xs mb-2">
          <span>累计网络流量</span>
          <Activity class="w-4 h-4 text-purple-600" />
        </div>
        <div class="text-sm font-semibold text-slate-800 mt-1">
          上行：{{ formatBytes(snap.network.bytes_sent) }}
        </div>
        <div class="text-sm font-semibold text-slate-800 mt-1">
          下行：{{ formatBytes(snap.network.bytes_recv) }}
        </div>
        <div class="text-[11px] text-slate-500 mt-3">Go Runtime: {{ snap.go_version }}</div>
      </div>
    </div>

    <!-- Core /door Directories -->
    <div v-if="snap" class="bg-white rounded-xl border border-slate-200 p-5 shadow-xs">
      <div class="flex items-center justify-between mb-4">
        <div>
          <h3 class="text-sm font-semibold text-slate-800">Door 核心目录架构</h3>
          <p class="text-xs text-slate-500 mt-0.5">
            隔离式原生目录布局（可在「面板设置 → 基础设置」中调整）
          </p>
        </div>
        <router-link
          to="/settings/general"
          class="text-xs text-[#20a53a] hover:underline font-medium"
        >
          修改目录配置 →
        </router-link>
      </div>

      <div class="grid grid-cols-1 md:grid-cols-2 gap-3">
        <div
          v-for="dir in snap.directories"
          :key="dir.key"
          class="flex items-center justify-between p-3 rounded-lg bg-slate-50 border border-slate-200/80"
        >
          <div class="flex items-center gap-3 min-w-0">
            <div class="w-8 h-8 rounded-lg bg-emerald-50 text-emerald-700 flex items-center justify-center shrink-0">
              <FolderOpen class="w-4 h-4" />
            </div>
            <div class="min-w-0">
              <div class="text-xs font-semibold text-slate-700">{{ dir.label }}</div>
              <div class="text-xs font-mono text-slate-500 truncate">{{ dir.path }}</div>
            </div>
          </div>
          <span
            class="px-2 py-0.5 rounded text-[11px] font-medium shrink-0"
            :class="dir.exists ? 'bg-emerald-100 text-emerald-700' : 'bg-amber-100 text-amber-700'"
          >
            {{ dir.exists ? '已就绪' : '未创建' }}
          </span>
        </div>
      </div>
    </div>
  </div>
</template>
