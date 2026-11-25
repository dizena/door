<script setup lang="ts">
import { ArrowUp, Eye, EyeOff, GripVertical, RotateCcw, Save } from 'lucide-vue-next'
import { onMounted, ref } from 'vue'
import { api, ApiError, type MenuNode, type MenuTreeInput } from '../api/client'
import IconGlyph from '../components/IconGlyph.vue'
import { refreshMenus } from '../stores/panel'

interface DragPayload {
  id: number
  fromParentId: number | null
}

const tree = ref<MenuNode[]>([])
const loading = ref(true)
const saving = ref(false)
const dirty = ref(false)
const msg = ref('')
const errMsg = ref('')
const dragging = ref<DragPayload | null>(null)
const dragOverTarget = ref<string | null>(null)

const iconChoices = [
  'dashboard',
  'globe',
  'layers',
  'shield-check',
  'server',
  'box',
  'code',
  'coffee',
  'terminal',
  'cpu',
  'database',
  'zap',
  'activity',
  'list-checks',
  'file-text',
  'settings',
  'sliders',
  'menu',
  'lock',
]

function cloneTree(items: MenuNode[]): MenuNode[] {
  return items.map((m) => ({
    ...m,
    children: (m.children || []).map((c) => ({ ...c, children: [] })),
  }))
}

async function load() {
  loading.value = true
  errMsg.value = ''
  try {
    const res = await api.getMenus()
    tree.value = cloneTree(res.items || [])
    dirty.value = false
  } catch (err) {
    errMsg.value = err instanceof ApiError ? err.message : '加载菜单失败'
  } finally {
    loading.value = false
  }
}

onMounted(load)

function startDrag(id: number, fromParentId: number | null, ev: DragEvent) {
  dragging.value = { id, fromParentId }
  if (ev.dataTransfer) {
    ev.dataTransfer.effectAllowed = 'move'
    ev.dataTransfer.setData('text/plain', String(id))
  }
}

function endDrag() {
  dragging.value = null
  dragOverTarget.value = null
}

function extractNode(id: number): MenuNode | null {
  const pIdx = tree.value.findIndex((m) => m.id === id)
  if (pIdx !== -1) {
    const [removed] = tree.value.splice(pIdx, 1)
    return removed
  }
  for (const parent of tree.value) {
    const cIdx = parent.children.findIndex((c) => c.id === id)
    if (cIdx !== -1) {
      const [removed] = parent.children.splice(cIdx, 1)
      return removed
    }
  }
  return null
}

function findNode(id: number): MenuNode | null {
  for (const p of tree.value) {
    if (p.id === id) return p
    for (const c of p.children) {
      if (c.id === id) return c
    }
  }
  return null
}

// Drop onto a Level-1 group header:
// - If dragging a Level-1 group -> reorder Level-1 groups
// - If dragging a Level-2 item (or childless Level-1) -> move into this group's children unless Shift/top-promote
function onDropOnParent(targetParentId: number) {
  const drag = dragging.value
  dragOverTarget.value = null
  if (!drag || drag.id === targetParentId) return

  const draggedNode = findNode(drag.id)
  if (!draggedNode) return

  const isDraggedLevel1 = drag.fromParentId === null
  const targetIdx = tree.value.findIndex((m) => m.id === targetParentId)
  if (targetIdx === -1) return

  if (isDraggedLevel1) {
    // Reorder Level-1 groups
    const extracted = extractNode(drag.id)
    if (!extracted) return
    const newTargetIdx = tree.value.findIndex((m) => m.id === targetParentId)
    tree.value.splice(newTargetIdx, 0, extracted)
    dirty.value = true
    return
  }

  // Dragging a Level-2 item onto a Level-1 group adds it as a child of that group
  const extracted = extractNode(drag.id)
  if (!extracted) return
  extracted.parent_id = targetParentId
  tree.value[targetIdx].children.push(extracted)
  dirty.value = true
}

// Drop into a Level-1 group's child zone (supports demoting childless Level-1 or moving Level-2 across groups)
function onDropIntoGroupChildren(targetParentId: number, insertIndex?: number) {
  const drag = dragging.value
  dragOverTarget.value = null
  if (!drag || drag.id === targetParentId) return

  const draggedNode = findNode(drag.id)
  if (!draggedNode) return

  // Prevent > 2 levels: a Level-1 group that already has children cannot become a Level-2 child
  if (draggedNode.children && draggedNode.children.length > 0) {
    errMsg.value = `「${draggedNode.title}」包含二级子菜单，不能直接降级为二级菜单（请先移出其子菜单）`
    return
  }

  errMsg.value = ''
  const extracted = extractNode(drag.id)
  if (!extracted) return
  extracted.parent_id = targetParentId
  extracted.children = []

  const parent = tree.value.find((m) => m.id === targetParentId)
  if (!parent) return
  if (insertIndex === undefined || insertIndex < 0 || insertIndex > parent.children.length) {
    parent.children.push(extracted)
  } else {
    parent.children.splice(insertIndex, 0, extracted)
  }
  dirty.value = true
}

// Drop onto top-level promotion zone: promotes Level-2 to Level-1 (or moves Level-1 to end)
function onDropPromoteToRoot() {
  const drag = dragging.value
  dragOverTarget.value = null
  if (!drag) return

  const extracted = extractNode(drag.id)
  if (!extracted) return
  extracted.parent_id = null
  if (!extracted.children) extracted.children = []
  tree.value.push(extracted)
  dirty.value = true
}

function promoteChild(parentId: number, childId: number) {
  const parent = tree.value.find((m) => m.id === parentId)
  if (!parent) return
  const idx = parent.children.findIndex((c) => c.id === childId)
  if (idx === -1) return
  const [child] = parent.children.splice(idx, 1)
  child.parent_id = null
  child.children = []
  const pIdx = tree.value.findIndex((m) => m.id === parentId)
  tree.value.splice(pIdx + 1, 0, child)
  dirty.value = true
}

function toggleVisible(node: MenuNode) {
  node.visible = !node.visible
  dirty.value = true
}

async function saveLayout() {
  saving.value = true
  msg.value = ''
  errMsg.value = ''
  try {
    const payload: MenuTreeInput[] = tree.value.map((p) => ({
      id: p.id,
      title: p.title,
      icon: p.icon,
      visible: p.visible,
      children: (p.children || []).map((c) => ({
        id: c.id,
        title: c.title,
        icon: c.icon,
        visible: c.visible,
      })),
    }))
    const res = await api.reorderMenus(payload)
    tree.value = cloneTree(res.items || [])
    dirty.value = false
    await refreshMenus()
    msg.value = '菜单排序与归属已保存并即时生效！'
  } catch (err) {
    errMsg.value = err instanceof ApiError ? err.message : '保存菜单布局失败'
  } finally {
    saving.value = false
  }
}

async function resetDefaults() {
  saving.value = true
  msg.value = ''
  errMsg.value = ''
  try {
    const res = await api.resetMenus()
    tree.value = cloneTree(res.items || [])
    dirty.value = false
    await refreshMenus()
    msg.value = '已恢复默认菜单布局'
  } catch (err) {
    errMsg.value = err instanceof ApiError ? err.message : '恢复默认失败'
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="max-w-4xl space-y-5">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div>
        <h2 class="text-lg font-semibold text-slate-800">菜单配置（拖拽排序与归属管理）</h2>
        <p class="text-xs text-slate-500 mt-0.5">
          支持拖拽调整一级菜单顺序、跨一级菜单移动二级子菜单、二级升一级或无子菜单的一级降为二级。
        </p>
      </div>
      <div class="flex items-center gap-2">
        <button
          type="button"
          @click="resetDefaults"
          :disabled="saving"
          class="inline-flex items-center gap-1.5 px-3 py-2 rounded-lg border border-slate-300 bg-white text-xs font-medium text-slate-700 hover:bg-slate-50 cursor-pointer"
        >
          <RotateCcw class="w-3.5 h-3.5" />
          恢复默认菜单
        </button>
        <button
          type="button"
          @click="saveLayout"
          :disabled="saving"
          class="inline-flex items-center gap-1.5 px-4 py-2 rounded-lg bg-[#20a53a] hover:bg-[#1a8e31] text-white text-xs font-medium cursor-pointer disabled:opacity-60"
        >
          <Save class="w-3.5 h-3.5" />
          {{ saving ? '保存中...' : dirty ? '保存菜单布局 *' : '保存菜单布局' }}
        </button>
      </div>
    </div>

    <div v-if="msg" class="px-3 py-2 rounded-lg bg-emerald-50 border border-emerald-200 text-emerald-700 text-sm">
      {{ msg }}
    </div>
    <div v-if="errMsg" class="px-3 py-2 rounded-lg bg-red-50 border border-red-200 text-red-700 text-sm">
      {{ errMsg }}
    </div>

    <!-- Top-level promotion drop zone -->
    <div
      @dragover.prevent="dragOverTarget = 'root-promote'"
      @dragleave="dragOverTarget = null"
      @drop.prevent="onDropPromoteToRoot"
      class="rounded-xl border-2 border-dashed px-4 py-3 text-center text-xs transition-colors"
      :class="
        dragOverTarget === 'root-promote'
          ? 'border-[#20a53a] bg-emerald-50 text-emerald-700 font-medium'
          : 'border-slate-300 bg-slate-50 text-slate-500'
      "
    >
      将任意二级菜单拖拽到此处，可立即升级为「一级菜单」（或拖拽一级菜单到此处移至末尾）
    </div>

    <div v-if="loading" class="bg-white rounded-xl border border-slate-200 p-6 text-sm text-slate-500">
      正在加载菜单树...
    </div>

    <div v-else class="space-y-3">
      <div
        v-for="parent in tree"
        :key="parent.id"
        class="bg-white rounded-xl border border-slate-200 shadow-xs overflow-hidden"
      >
        <!-- Level-1 row -->
        <div
          draggable="true"
          @dragstart="startDrag(parent.id, null, $event)"
          @dragend="endDrag"
          @dragover.prevent="dragOverTarget = 'p-' + parent.id"
          @dragleave="dragOverTarget = null"
          @drop.prevent="onDropOnParent(parent.id)"
          class="flex flex-wrap items-center justify-between gap-3 px-4 py-3 bg-slate-50/80 border-b border-slate-100 transition-colors"
          :class="dragOverTarget === 'p-' + parent.id ? 'bg-emerald-50 ring-2 ring-inset ring-[#20a53a]' : ''"
        >
          <div class="flex items-center gap-2.5 flex-1 min-w-[220px]">
            <GripVertical class="w-4 h-4 text-slate-400 cursor-grab shrink-0" />
            <span class="px-1.5 py-0.5 rounded bg-slate-200 text-slate-700 text-[10px] font-semibold">一级</span>
            <IconGlyph :name="parent.icon" class-name="w-4 h-4 text-slate-600" />
            <input
              v-model="parent.title"
              @input="dirty = true"
              type="text"
              class="px-2 py-1 text-sm font-medium text-slate-800 bg-white border border-slate-200 rounded focus:outline-none focus:ring-2 focus:ring-[#20a53a] w-44"
            />
            <select
              v-model="parent.icon"
              @change="dirty = true"
              class="px-2 py-1 text-xs bg-white border border-slate-200 rounded text-slate-600"
            >
              <option v-for="ic in iconChoices" :key="ic" :value="ic">{{ ic }}</option>
            </select>
            <span class="text-xs font-mono text-slate-400 hidden sm:inline">{{ parent.route }}</span>
          </div>

          <button
            type="button"
            @click="toggleVisible(parent)"
            class="inline-flex items-center gap-1 px-2.5 py-1 rounded text-xs font-medium cursor-pointer"
            :class="parent.visible ? 'bg-emerald-50 text-emerald-700' : 'bg-slate-200 text-slate-500'"
          >
            <Eye v-if="parent.visible" class="w-3.5 h-3.5" />
            <EyeOff v-else class="w-3.5 h-3.5" />
            {{ parent.visible ? '显示' : '隐藏' }}
          </button>
        </div>

        <!-- Level-2 drop container -->
        <div
          @dragover.prevent="dragOverTarget = 'c-' + parent.id"
          @dragleave="dragOverTarget = null"
          @drop.self.prevent="onDropIntoGroupChildren(parent.id)"
          class="p-3 pl-8 space-y-2 min-h-[48px] transition-colors"
          :class="dragOverTarget === 'c-' + parent.id ? 'bg-emerald-50/50' : 'bg-white'"
        >
          <div
            v-if="parent.children.length === 0"
            @dragover.prevent="dragOverTarget = 'c-' + parent.id"
            @drop.prevent="onDropIntoGroupChildren(parent.id)"
            class="text-xs text-slate-400 italic py-1.5 border border-dashed border-slate-200 rounded-lg text-center"
          >
            暂无二级菜单（可将其它菜单拖入此处成为二级子菜单）
          </div>

          <div
            v-for="(child, cIdx) in parent.children"
            :key="child.id"
            draggable="true"
            @dragstart.stop="startDrag(child.id, parent.id, $event)"
            @dragend="endDrag"
            @dragover.prevent.stop="dragOverTarget = 'item-' + child.id"
            @dragleave="dragOverTarget = null"
            @drop.prevent.stop="onDropIntoGroupChildren(parent.id, cIdx)"
            class="flex flex-wrap items-center justify-between gap-2 px-3 py-2 rounded-lg border border-slate-200 bg-slate-50/50 hover:bg-slate-50 transition-colors"
            :class="dragOverTarget === 'item-' + child.id ? 'ring-2 ring-[#20a53a] bg-emerald-50/40' : ''"
          >
            <div class="flex items-center gap-2.5 flex-1 min-w-[200px]">
              <GripVertical class="w-4 h-4 text-slate-400 cursor-grab shrink-0" />
              <span class="px-1.5 py-0.5 rounded bg-emerald-100 text-emerald-700 text-[10px] font-semibold">二级</span>
              <IconGlyph :name="child.icon" class-name="w-3.5 h-3.5 text-slate-600" />
              <input
                v-model="child.title"
                @input="dirty = true"
                type="text"
                class="px-2 py-1 text-xs font-medium text-slate-800 bg-white border border-slate-200 rounded focus:outline-none focus:ring-2 focus:ring-[#20a53a] w-36"
              />
              <select
                v-model="child.icon"
                @change="dirty = true"
                class="px-2 py-1 text-xs bg-white border border-slate-200 rounded text-slate-600"
              >
                <option v-for="ic in iconChoices" :key="ic" :value="ic">{{ ic }}</option>
              </select>
              <span class="text-[11px] font-mono text-slate-400 hidden sm:inline">{{ child.route }}</span>
            </div>

            <div class="flex items-center gap-1.5">
              <button
                type="button"
                @click="promoteChild(parent.id, child.id)"
                title="升为一级菜单"
                class="inline-flex items-center gap-1 px-2 py-1 rounded text-[11px] bg-white border border-slate-200 text-slate-600 hover:bg-slate-100 cursor-pointer"
              >
                <ArrowUp class="w-3 h-3" />
                升为一级
              </button>
              <button
                type="button"
                @click="toggleVisible(child)"
                class="inline-flex items-center gap-1 px-2 py-1 rounded text-[11px] font-medium cursor-pointer"
                :class="child.visible ? 'bg-emerald-50 text-emerald-700' : 'bg-slate-200 text-slate-500'"
              >
                <Eye v-if="child.visible" class="w-3 h-3" />
                <EyeOff v-else class="w-3 h-3" />
                {{ child.visible ? '显示' : '隐藏' }}
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
