<script setup lang="ts">
import { LogOut, UserCheck } from 'lucide-vue-next'
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import SidebarNav from './components/SidebarNav.vue'
import { logoutSession, panelState } from './stores/panel'

const route = useRoute()
const router = useRouter()

const showShell = computed(() => !route.meta.public && !route.meta.allowForcedPw && !!panelState.user)

async function handleLogout() {
  await logoutSession()
  await router.replace('/login')
}
</script>

<template>
  <div v-if="showShell" class="min-h-screen flex bg-slate-100">
    <SidebarNav />

    <div class="flex-1 min-w-0 flex flex-col">
      <!-- Top Header -->
      <header class="h-14 bg-white border-b border-slate-200 px-6 flex items-center justify-between shrink-0">
        <div class="flex items-center gap-2 text-xs text-slate-500">
          <span class="font-medium text-slate-700">{{ panelState.panelName }}</span>
          <span>/</span>
          <span class="font-mono text-slate-500">{{ route.path }}</span>
        </div>

        <div class="flex items-center gap-3">
          <div class="inline-flex items-center gap-1.5 text-xs text-slate-600 bg-slate-100 px-2.5 py-1 rounded-full">
            <UserCheck class="w-3.5 h-3.5 text-[#20a53a]" />
            <span>{{ panelState.user?.username }}</span>
          </div>
          <button
            type="button"
            @click="handleLogout"
            class="inline-flex items-center gap-1 text-xs text-slate-500 hover:text-red-600 cursor-pointer"
          >
            <LogOut class="w-3.5 h-3.5" />
            退出
          </button>
        </div>
      </header>

      <!-- Main Workspace -->
      <main class="flex-1 p-6 overflow-y-auto">
        <router-view />
      </main>
    </div>
  </div>

  <router-view v-else />
</template>
