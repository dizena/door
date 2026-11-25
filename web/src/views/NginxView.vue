<script setup lang="ts">
import { CheckCircle2, FileCode2, Play, Plus, RotateCw, ShieldCheck, Square, Trash2 } from 'lucide-vue-next'
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { api, ApiError, type NginxInfo, type SiteItem, type SSLCertItem } from '../api/client'

const route = useRoute()

const tab = computed<'sites' | 'ssl' | 'service'>(() => {
  if (route.path === '/nginx/ssl') return 'ssl'
  if (route.path === '/nginx/service') return 'service'
  return 'sites'
})

const info = ref<NginxInfo | null>(null)
const sites = ref<SiteItem[]>([])
const certs = ref<SSLCertItem[]>([])
const mainConfDraft = ref('')

const msg = ref('')
const errMsg = ref('')

// Create site modal
const showSiteModal = ref(false)
const siteForm = ref({
  domain: '',
  extra_domains: '',
  listen_port: 80,
  site_type: 'proxy' as 'proxy' | 'static',
  proxy_pass: 'http://127.0.0.1:8080',
  ssl_enabled: false,
  ssl_cert_id: null as number | null,
})

// Site config editor modal
const editingSiteId = ref<number | null>(null)
const editingSiteConf = ref('')

// Issue SSL modal
const showSSLModal = ref(false)
const sslForm = ref({
  domain: '',
  provider: 'selfsigned' as 'selfsigned' | 'letsencrypt' | 'manual',
  email: '',
  cert_pem: '',
  key_pem: '',
})

async function load() {
  errMsg.value = ''
  try {
    if (tab.value === 'sites') {
      const [sRes, cRes] = await Promise.all([api.listSites(), api.listCerts()])
      sites.value = sRes.items || []
      certs.value = cRes.items || []
    } else if (tab.value === 'ssl') {
      const cRes = await api.listCerts()
      certs.value = cRes.items || []
    } else {
      info.value = await api.getNginxInfo()
      mainConfDraft.value = info.value.config_text || ''
    }
  } catch (err) {
    errMsg.value = err instanceof ApiError ? err.message : '加载失败'
  }
}

onMounted(load)
watch(tab, () => {
  msg.value = ''
  errMsg.value = ''
  load()
})

async function installNginx() {
  errMsg.value = ''
  try {
    const tsk = await api.installNginx()
    msg.value = `已提交 Nginx 部署任务 (${tsk.id})，安装目录 /door/nginx`
    setTimeout(load, 800)
  } catch (err) {
    errMsg.value = err instanceof ApiError ? err.message : '安装失败'
  }
}

async function doNginxAction(act: 'start' | 'stop' | 'restart' | 'reload' | 'test') {
  errMsg.value = ''
  msg.value = ''
  try {
    info.value = await api.actionNginx(act)
    msg.value = act === 'test' ? 'nginx -t 配置语法校验通过' : `Nginx ${act} 操作成功`
  } catch (err) {
    errMsg.value = err instanceof ApiError ? err.message : '操作失败'
  }
}

async function saveMainConfig() {
  errMsg.value = ''
  msg.value = ''
  try {
    await api.updateNginxConfig(mainConfDraft.value)
    msg.value = '主配置已校验并保存'
  } catch (err) {
    errMsg.value = err instanceof ApiError ? err.message : '保存失败（已自动回滚）'
  }
}

async function submitSite() {
  errMsg.value = ''
  msg.value = ''
  try {
    await api.createSite(siteForm.value)
    showSiteModal.value = false
    msg.value = `站点 ${siteForm.value.domain} 已创建并完成 nginx -t 校验`
    await load()
  } catch (err) {
    errMsg.value = err instanceof ApiError ? err.message : '创建站点失败'
  }
}

async function openSiteConf(s: SiteItem) {
  editingSiteId.value = s.id
  const res = await api.getSiteConfig(s.id)
  editingSiteConf.value = res.config_text
}

async function saveSiteConf() {
  if (!editingSiteId.value) return
  errMsg.value = ''
  try {
    await api.updateSiteConfig(editingSiteId.value, editingSiteConf.value)
    editingSiteId.value = null
    msg.value = '站点配置已通过 nginx -t 校验并生效'
  } catch (err) {
    errMsg.value = err instanceof ApiError ? err.message : '配置有误（已自动回滚）'
  }
}

async function removeSite(id: number) {
  await api.deleteSite(id)
  await load()
}

async function submitSSL() {
  errMsg.value = ''
  msg.value = ''
  try {
    await api.issueCert(sslForm.value)
    showSSLModal.value = false
    msg.value = `证书 ${sslForm.value.domain} 已签发并存入 /door/nginx/ssl`
    await load()
  } catch (err) {
    errMsg.value = err instanceof ApiError ? err.message : '证书签发失败'
  }
}

async function removeCert(id: number) {
  await api.deleteCert(id)
  await load()
}
</script>

<template>
  <div class="space-y-5">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div>
        <h2 class="text-lg font-semibold text-slate-800">
          {{ tab === 'sites' ? 'Nginx 站点与反向代理' : tab === 'ssl' ? 'SSL 证书管理' : 'Nginx 服务管理' }}
        </h2>
        <p class="text-xs text-slate-500 mt-0.5">隔离目录 /door/nginx · 每次修改配置均先执行 nginx -t 预检并支持自动回滚</p>
      </div>

      <button
        v-if="tab === 'sites'"
        type="button"
        @click="showSiteModal = true"
        class="inline-flex items-center gap-1.5 px-3.5 py-2 rounded-lg bg-[#20a53a] hover:bg-[#1a8e31] text-white text-xs font-medium cursor-pointer"
      >
        <Plus class="w-4 h-4" /> 添加站点
      </button>
      <button
        v-else-if="tab === 'ssl'"
        type="button"
        @click="showSSLModal = true"
        class="inline-flex items-center gap-1.5 px-3.5 py-2 rounded-lg bg-[#20a53a] hover:bg-[#1a8e31] text-white text-xs font-medium cursor-pointer"
      >
        <ShieldCheck class="w-4 h-4" /> 申请 / 上传证书
      </button>
    </div>

    <div v-if="msg" class="px-3 py-2 rounded-lg bg-emerald-50 border border-emerald-200 text-emerald-700 text-sm">{{ msg }}</div>
    <div v-if="errMsg" class="px-3 py-2 rounded-lg bg-red-50 border border-red-200 text-red-700 text-sm">{{ errMsg }}</div>

    <!-- Sites -->
    <div v-if="tab === 'sites'" class="bg-white rounded-xl border border-slate-200 overflow-hidden shadow-xs">
      <table class="w-full text-left text-xs">
        <thead class="bg-slate-50 text-slate-500">
          <tr>
            <th class="px-4 py-3 font-medium">域名</th>
            <th class="px-4 py-3 font-medium">类型</th>
            <th class="px-4 py-3 font-medium">目标 / 根目录</th>
            <th class="px-4 py-3 font-medium">HTTPS</th>
            <th class="px-4 py-3 font-medium text-right">操作</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-slate-100">
          <tr v-for="s in sites" :key="s.id">
            <td class="px-4 py-3 font-semibold text-slate-800">{{ s.domain }}</td>
            <td class="px-4 py-3">{{ s.site_type === 'proxy' ? '反向代理' : '静态站点' }}</td>
            <td class="px-4 py-3 font-mono text-slate-600">{{ s.proxy_pass || s.root_dir }}</td>
            <td class="px-4 py-3">
              <span class="px-2 py-0.5 rounded text-[11px]" :class="s.ssl_enabled ? 'bg-emerald-100 text-emerald-700' : 'bg-slate-100 text-slate-500'">
                {{ s.ssl_enabled ? '已开启' : '未开启' }}
              </span>
            </td>
            <td class="px-4 py-3 text-right space-x-2">
              <button @click="openSiteConf(s)" class="inline-flex items-center gap-1 text-blue-600 hover:underline cursor-pointer">
                <FileCode2 class="w-3.5 h-3.5" /> 配置
              </button>
              <button @click="removeSite(s.id)" class="inline-flex items-center gap-1 text-red-600 hover:underline cursor-pointer">
                <Trash2 class="w-3.5 h-3.5" /> 删除
              </button>
            </td>
          </tr>
          <tr v-if="sites.length === 0">
            <td colspan="5" class="px-4 py-10 text-center text-slate-400">暂无站点，点击右上角添加反代或静态站点</td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- SSL -->
    <div v-else-if="tab === 'ssl'" class="bg-white rounded-xl border border-slate-200 overflow-hidden shadow-xs">
      <table class="w-full text-left text-xs">
        <thead class="bg-slate-50 text-slate-500">
          <tr>
            <th class="px-4 py-3 font-medium">域名</th>
            <th class="px-4 py-3 font-medium">签发方式</th>
            <th class="px-4 py-3 font-medium">证书路径</th>
            <th class="px-4 py-3 font-medium">过期时间</th>
            <th class="px-4 py-3 font-medium text-right">操作</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-slate-100">
          <tr v-for="c in certs" :key="c.id">
            <td class="px-4 py-3 font-semibold text-slate-800">{{ c.domain }}</td>
            <td class="px-4 py-3 font-mono">{{ c.provider }}</td>
            <td class="px-4 py-3 font-mono text-slate-500">{{ c.cert_path }}</td>
            <td class="px-4 py-3 text-slate-600">{{ new Date(c.expires_at).toLocaleDateString() }}</td>
            <td class="px-4 py-3 text-right">
              <button @click="removeCert(c.id)" class="text-red-600 hover:underline cursor-pointer">删除</button>
            </td>
          </tr>
          <tr v-if="certs.length === 0">
            <td colspan="5" class="px-4 py-10 text-center text-slate-400">暂无 SSL 证书</td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Nginx Service -->
    <div v-else-if="info" class="space-y-4">
      <div class="bg-white rounded-xl border border-slate-200 p-5 flex flex-wrap items-center justify-between gap-4 shadow-xs">
        <div>
          <div class="text-sm font-semibold text-slate-800">
            Nginx 状态：
            <span class="ml-1 px-2 py-0.5 rounded text-xs" :class="info.active ? 'bg-emerald-100 text-emerald-700' : 'bg-slate-100 text-slate-600'">
              {{ info.installed ? info.status : '未部署' }}
            </span>
          </div>
          <div class="text-xs font-mono text-slate-500 mt-1">{{ info.binary_path }}</div>
        </div>
        <div class="flex flex-wrap gap-2">
          <button v-if="!info.installed" @click="installNginx" class="px-3.5 py-2 rounded-lg bg-[#20a53a] text-white text-xs font-medium cursor-pointer">
            一键部署 Nginx
          </button>
          <template v-else>
            <button @click="doNginxAction('start')" class="inline-flex items-center gap-1 px-3 py-1.5 rounded-lg bg-emerald-50 text-emerald-700 text-xs cursor-pointer"><Play class="w-3.5 h-3.5" /> 启动</button>
            <button @click="doNginxAction('stop')" class="inline-flex items-center gap-1 px-3 py-1.5 rounded-lg bg-slate-100 text-slate-700 text-xs cursor-pointer"><Square class="w-3.5 h-3.5" /> 停止</button>
            <button @click="doNginxAction('reload')" class="inline-flex items-center gap-1 px-3 py-1.5 rounded-lg bg-blue-50 text-blue-700 text-xs cursor-pointer"><RotateCw class="w-3.5 h-3.5" /> 平滑重载</button>
            <button @click="doNginxAction('test')" class="inline-flex items-center gap-1 px-3 py-1.5 rounded-lg bg-amber-50 text-amber-700 text-xs cursor-pointer"><CheckCircle2 class="w-3.5 h-3.5" /> nginx -t</button>
          </template>
        </div>
      </div>

      <div v-if="info.installed" class="bg-white rounded-xl border border-slate-200 p-5 space-y-3 shadow-xs">
        <div class="flex items-center justify-between">
          <h3 class="text-sm font-semibold text-slate-800">主配置文件 ({{ info.config_path }})</h3>
          <button @click="saveMainConfig" class="px-3 py-1.5 rounded-lg bg-[#20a53a] text-white text-xs cursor-pointer">校验并保存</button>
        </div>
        <textarea v-model="mainConfDraft" rows="14" class="w-full p-3 font-mono text-xs bg-slate-900 text-emerald-300 rounded-lg focus:outline-none" />
      </div>
    </div>

    <!-- Create Site Modal -->
    <div v-if="showSiteModal" class="fixed inset-0 z-50 bg-black/40 flex items-center justify-center p-4">
      <form @submit.prevent="submitSite" class="bg-white rounded-xl max-w-lg w-full p-6 space-y-4 shadow-xl">
        <h3 class="text-base font-semibold text-slate-800">添加 Nginx 站点</h3>
        <div class="grid grid-cols-2 gap-3">
          <div class="col-span-2">
            <label class="block text-xs font-medium text-slate-700 mb-1">主域名</label>
            <input v-model="siteForm.domain" required placeholder="api.example.com" class="w-full px-3 py-2 border border-slate-300 rounded-lg text-sm" />
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-700 mb-1">站点类型</label>
            <select v-model="siteForm.site_type" class="w-full px-3 py-2 border border-slate-300 rounded-lg text-sm">
              <option value="proxy">反向代理</option>
              <option value="static">静态站点</option>
            </select>
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-700 mb-1">HTTP 端口</label>
            <input v-model.number="siteForm.listen_port" type="number" class="w-full px-3 py-2 border border-slate-300 rounded-lg text-sm" />
          </div>
        </div>
        <div v-if="siteForm.site_type === 'proxy'">
          <label class="block text-xs font-medium text-slate-700 mb-1">反代目标地址 (proxy_pass)</label>
          <input v-model="siteForm.proxy_pass" required placeholder="http://127.0.0.1:8080" class="w-full px-3 py-2 border border-slate-300 rounded-lg text-sm font-mono" />
        </div>
        <div class="flex items-center gap-2">
          <input id="ssl-toggle" v-model="siteForm.ssl_enabled" type="checkbox" />
          <label for="ssl-toggle" class="text-xs text-slate-700">启用 HTTPS (443)</label>
        </div>
        <div v-if="siteForm.ssl_enabled">
          <label class="block text-xs font-medium text-slate-700 mb-1">选择 SSL 证书</label>
          <select v-model="siteForm.ssl_cert_id" required class="w-full px-3 py-2 border border-slate-300 rounded-lg text-sm">
            <option v-for="c in certs" :key="c.id" :value="c.id">{{ c.domain }} ({{ c.provider }})</option>
          </select>
        </div>
        <div class="flex justify-end gap-2 pt-2">
          <button type="button" @click="showSiteModal = false" class="px-4 py-2 rounded-lg border border-slate-300 text-xs cursor-pointer">取消</button>
          <button type="submit" class="px-4 py-2 rounded-lg bg-[#20a53a] text-white text-xs font-medium cursor-pointer">校验并创建</button>
        </div>
      </form>
    </div>

    <!-- Edit Site Config Modal -->
    <div v-if="editingSiteId !== null" class="fixed inset-0 z-50 bg-black/40 flex items-center justify-center p-4">
      <div class="bg-white rounded-xl max-w-2xl w-full p-5 space-y-3 shadow-2xl">
        <h3 class="text-sm font-semibold text-slate-800">编辑站点 vhost 配置</h3>
        <textarea v-model="editingSiteConf" rows="16" class="w-full p-3 font-mono text-xs bg-slate-900 text-emerald-300 rounded-lg" />
        <div class="flex justify-end gap-2">
          <button @click="editingSiteId = null" class="px-4 py-2 rounded-lg border border-slate-300 text-xs cursor-pointer">取消</button>
          <button @click="saveSiteConf" class="px-4 py-2 rounded-lg bg-[#20a53a] text-white text-xs font-medium cursor-pointer">nginx -t 校验并保存</button>
        </div>
      </div>
    </div>

    <!-- Issue SSL Modal -->
    <div v-if="showSSLModal" class="fixed inset-0 z-50 bg-black/40 flex items-center justify-center p-4">
      <form @submit.prevent="submitSSL" class="bg-white rounded-xl max-w-lg w-full p-6 space-y-4 shadow-xl">
        <h3 class="text-base font-semibold text-slate-800">签发或上传 SSL 证书</h3>
        <div>
          <label class="block text-xs font-medium text-slate-700 mb-1">域名</label>
          <input v-model="sslForm.domain" required placeholder="example.com" class="w-full px-3 py-2 border border-slate-300 rounded-lg text-sm" />
        </div>
        <div>
          <label class="block text-xs font-medium text-slate-700 mb-1">签发方式</label>
          <select v-model="sslForm.provider" class="w-full px-3 py-2 border border-slate-300 rounded-lg text-sm">
            <option value="selfsigned">自签名测试证书 (ECDSA)</option>
            <option value="letsencrypt">Let's Encrypt 免费证书 (HTTP-01)</option>
            <option value="manual">手动粘贴 PEM 证书与私钥</option>
          </select>
        </div>
        <div v-if="sslForm.provider === 'letsencrypt'">
          <label class="block text-xs font-medium text-slate-700 mb-1">ACME 通知邮箱</label>
          <input v-model="sslForm.email" type="email" required placeholder="admin@example.com" class="w-full px-3 py-2 border border-slate-300 rounded-lg text-sm" />
        </div>
        <template v-if="sslForm.provider === 'manual'">
          <div>
            <label class="block text-xs font-medium text-slate-700 mb-1">证书 PEM</label>
            <textarea v-model="sslForm.cert_pem" rows="4" required class="w-full p-2 border rounded-lg font-mono text-xs" />
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-700 mb-1">私钥 PEM</label>
            <textarea v-model="sslForm.key_pem" rows="4" required class="w-full p-2 border rounded-lg font-mono text-xs" />
          </div>
        </template>
        <div class="flex justify-end gap-2">
          <button type="button" @click="showSSLModal = false" class="px-4 py-2 rounded-lg border border-slate-300 text-xs cursor-pointer">取消</button>
          <button type="submit" class="px-4 py-2 rounded-lg bg-[#20a53a] text-white text-xs font-medium cursor-pointer">确认签发</button>
        </div>
      </form>
    </div>
  </div>
</template>
