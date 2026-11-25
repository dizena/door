<script setup lang="ts">
import { FitAddon } from '@xterm/addon-fit'
import { Terminal } from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'
import { RotateCw } from 'lucide-vue-next'
import { onMounted, onUnmounted, ref } from 'vue'

const termEl = ref<HTMLElement | null>(null)
const statusText = ref('未连接')

let term: Terminal | null = null
let fitAddon: FitAddon | null = null
let ws: WebSocket | null = null
let resizeObserver: ResizeObserver | null = null

function connect() {
  if (ws) {
    ws.close()
    ws = null
  }
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const url = `${proto}//${window.location.host}/api/v1/terminal/ws`
  statusText.value = '连接中...'
  ws = new WebSocket(url)

  ws.onopen = () => {
    statusText.value = '已连接'
    fitAddon?.fit()
    if (term && ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }))
    }
    term?.focus()
  }

  ws.onmessage = (ev) => {
    if (typeof ev.data === 'string') {
      term?.write(ev.data)
    }
  }

  ws.onclose = () => {
    statusText.value = '已断开'
    term?.write('\r\n\x1b[33m[终端会话已断开]\x1b[0m\r\n')
  }
}

onMounted(() => {
  if (!termEl.value) return
  term = new Terminal({
    cursorBlink: true,
    fontSize: 13,
    fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace',
    theme: {
      background: '#0f172a',
      foreground: '#e2e8f0',
    },
  })
  fitAddon = new FitAddon()
  term.loadAddon(fitAddon)
  term.open(termEl.value)
  fitAddon.fit()

  term.onData((data) => {
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ type: 'input', data }))
    }
  })

  resizeObserver = new ResizeObserver(() => {
    fitAddon?.fit()
    if (term && ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }))
    }
  })
  resizeObserver.observe(termEl.value)

  connect()
})

onUnmounted(() => {
  resizeObserver?.disconnect()
  ws?.close()
  term?.dispose()
})
</script>

<template>
  <div class="h-full flex flex-col space-y-3">
    <div class="flex items-center justify-between">
      <div>
        <h2 class="text-lg font-semibold text-slate-800">Web SSH 终端</h2>
        <p class="text-xs text-slate-500 mt-0.5">基于 PTY + WebSocket 实时交互会话 · 状态：{{ statusText }}</p>
      </div>
      <button
        type="button"
        @click="connect"
        class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-white border border-slate-200 text-xs font-medium text-slate-700 hover:bg-slate-50 cursor-pointer"
      >
        <RotateCw class="w-3.5 h-3.5" />
        重新连接
      </button>
    </div>

    <div
      ref="termEl"
      class="flex-1 min-h-[460px] rounded-xl overflow-hidden bg-[#0f172a] p-2 border border-slate-800 shadow-lg"
    />
  </div>
</template>
