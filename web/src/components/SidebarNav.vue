<script setup lang="ts">
import { ChevronDown, ChevronRight, Server } from 'lucide-vue-next'
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { MenuNode } from '../api/client'
import { panelState } from '../stores/panel'
import IconGlyph from './IconGlyph.vue'

const route = useRoute()
const router = useRouter()

const visibleMenus = computed(() =>
  panelState.menus
    .filter((m) => m.visible)
    .map((m) => ({
      ...m,
      children: (m.children || []).filter((c) => c.visible),
    })),
)

const expanded = ref<Record<number, boolean>>({})

watch(
  () => [route.path, panelState.menus] as const,
  () => {
    for (const parent of panelState.menus) {
      if (expanded.value[parent.id] === undefined) {
        expanded.value[parent.id] = true
      }
      if (parent.children?.some((c) => c.route === route.path)) {
        expanded.value[parent.id] = true
      }
    }
  },
  { immediate: true },
)

function handleParentClick(item: MenuNode) {
  if (item.children && item.children.length > 0) {
    expanded.value[item.id] = !expanded.value[item.id]
    return
  }
  if (item.route) {
    router.push(item.route)
  }
}

function isActive(targetRoute: string) {
  return route.path === targetRoute
}
</script>

<template>
  <aside class="w-60 shrink-0 bg-[#1b2430] text-slate-200 flex flex-col border-r border-slate-800 select-none">
    <!-- Brand -->
    <div class="h-14 px-4 flex items-center gap-2.5 border-b border-slate-800/80">
      <div class="w-8 h-8 rounded-lg bg-[#20a53a] flex items-center justify-center text-white shadow-sm">
        <Server class="w-4 h-4" />
      </div>
      <div class="min-w-0">
        <div class="text-sm font-semibold text-white truncate">{{ panelState.panelName }}</div>
        <div class="text-[11px] text-emerald-400/90 truncate">Linux 轻量运维面板</div>
      </div>
    </div>

    <!-- Dynamic 2-level menu tree -->
    <nav class="flex-1 overflow-y-auto py-3 px-2.5 space-y-1">
      <div v-for="group in visibleMenus" :key="group.id">
        <button
          type="button"
          @click="handleParentClick(group)"
          class="w-full flex items-center justify-between px-3 py-2 rounded-md text-sm transition-colors"
          :class="
            group.children.length === 0 && isActive(group.route)
              ? 'bg-[#20a53a] text-white font-medium shadow-sm'
              : 'text-slate-300 hover:bg-slate-800/80 hover:text-white'
          "
        >
          <span class="flex items-center gap-2.5 min-w-0">
            <IconGlyph :name="group.icon" class-name="w-4 h-4 shrink-0" />
            <span class="truncate">{{ group.title }}</span>
          </span>
          <template v-if="group.children.length > 0">
            <ChevronDown v-if="expanded[group.id]" class="w-3.5 h-3.5 text-slate-400 shrink-0" />
            <ChevronRight v-else class="w-3.5 h-3.5 text-slate-400 shrink-0" />
          </template>
        </button>

        <div
          v-if="group.children.length > 0 && expanded[group.id]"
          class="mt-1 ml-3 pl-3 border-l border-slate-700/70 space-y-0.5"
        >
          <router-link
            v-for="child in group.children"
            :key="child.id"
            :to="child.route || '/dashboard'"
            class="flex items-center gap-2.5 px-2.5 py-1.5 rounded-md text-[13px] transition-colors"
            :class="
              isActive(child.route)
                ? 'bg-[#20a53a] text-white font-medium'
                : 'text-slate-400 hover:text-white hover:bg-slate-800/60'
            "
          >
            <IconGlyph :name="child.icon" class-name="w-3.5 h-3.5 shrink-0" />
            <span class="truncate">{{ child.title }}</span>
          </router-link>
        </div>
      </div>
    </nav>

    <!-- Footer status -->
    <div class="px-4 py-3 border-t border-slate-800 text-[11px] text-slate-400 flex items-center justify-between">
      <span>驱动模式</span>
      <span class="px-1.5 py-0.5 rounded bg-slate-800 text-emerald-400 font-mono">
        {{ panelState.driverMode }}
      </span>
    </div>
  </aside>
</template>
