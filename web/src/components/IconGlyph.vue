<script setup lang="ts">
import {
  Activity,
  Box,
  Code2,
  Coffee,
  Cpu,
  Database,
  FileText,
  Globe,
  Layers,
  LayoutDashboard,
  ListChecks,
  Lock,
  Menu,
  Server,
  Settings,
  ShieldCheck,
  SlidersHorizontal,
  Terminal,
  Zap,
} from 'lucide-vue-next'
import { computed } from 'vue'

const props = defineProps<{ name: string; className?: string }>()

const iconMap: Record<string, unknown> = {
  dashboard: LayoutDashboard,
  globe: Globe,
  layers: Layers,
  'shield-check': ShieldCheck,
  server: Server,
  box: Box,
  code: Code2,
  coffee: Coffee,
  terminal: Terminal,
  cpu: Cpu,
  database: Database,
  zap: Zap,
  activity: Activity,
  'list-checks': ListChecks,
  'file-text': FileText,
  settings: Settings,
  sliders: SlidersHorizontal,
  menu: Menu,
  lock: Lock,
}

const component = computed(() => iconMap[props.name] || Layers)
</script>

<template>
  <component :is="component" :class="className || 'w-4 h-4 shrink-0'" />
</template>
