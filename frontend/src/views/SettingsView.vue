<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useAppStore } from '@/stores/app'
import { api } from '@/api/client'
import { useAppUpdater } from '@/composables/useAppUpdater'

const store = useAppStore()
const {
  appVersion,
  status: updaterStatus,
  availableVersion,
  errorMessage: updaterError,
  downloadPercent,
  isBusy: updaterBusy,
  isDesktop,
  initAppVersion,
  checkForUpdates,
  downloadAndInstallUpdate,
} = useAppUpdater()

const updaterStatusText = computed(() => {
  switch (updaterStatus.value) {
    case 'checking':
      return '正在检查更新…'
    case 'upToDate':
      return '已是最新版本'
    case 'available':
      return `发现新版本 ${availableVersion.value}`
    case 'downloading':
      return downloadPercent.value != null
        ? `正在下载 ${downloadPercent.value}%`
        : '正在下载更新…'
    case 'installing':
      return '正在安装并重启…'
    case 'error':
      return updaterError.value || '检查更新失败'
    default:
      return isDesktop() ? '可检查桌面版更新' : '自升级仅在桌面版可用'
  }
})

async function onCheckUpdate() {
  if (!isDesktop()) {
    store.note('自升级仅在桌面版可用')
    return
  }
  try {
    const found = await checkForUpdates()
    if (found) store.note(`发现新版本 ${availableVersion.value}`)
    else if (updaterStatus.value === 'upToDate') store.note('已是最新版本')
    else if (updaterStatus.value === 'error') store.fail(updaterError.value || '检查更新失败')
  } catch (e) {
    store.fail(e)
  }
}

async function onInstallUpdate() {
  try {
    store.note('正在下载更新…')
    await downloadAndInstallUpdate()
  } catch (e) {
    store.fail(e)
  }
}

const showForm = ref(false)
const editingId = ref('')
const form = ref<any>({ provider: 'openai', name: '', apiKey: '', baseUrl: '', defaultProtocolDialect: '', models: [] })
const manualModel = ref('')
const testing = ref<Record<string, boolean>>({})
const testResult = ref<Record<string, string>>({})
// 阶段建议：该阶段对模型的能力要求与推理等级建议。
const roles = [
  { id: 'plan', label: '立项', hint: '立项=总纲派生设定成套；重写设定携带依据；重新立项可保留精修文件。建议推理强的模型，effort medium–high。', effort: 'medium' },
  { id: 'outline', label: '细纲', hint: '单元合同的欲望/阻碍/选择与场面节拍，需要结构化输出与因果链。建议 effort medium。', effort: 'medium' },
  { id: 'write', label: '写作', hint: '正文 prose，文风与节奏优先；过长推理会稀释文气。建议文风好的模型，effort low–medium。', effort: 'low' },
  { id: 'review', label: '审校', hint: '找 OOC、连续性、写穿与 AI 味，需要严格比对与挑错能力。建议 effort medium–high。', effort: 'medium' },
  { id: 'local_edit', label: '局部编辑', hint: '选区润色/改写，改动小、要求贴原文。轻量模型即可，effort low。', effort: 'low' },
]
const EFFORTS = [
  { id: '', label: '默认' },
  { id: 'off', label: 'off（关闭推理）' },
  { id: 'low', label: 'low' },
  { id: 'medium', label: 'medium' },
  { id: 'high', label: 'high' },
  { id: 'xhigh', label: 'xhigh' },
]
// modelId 形如 provider/model[/effort]，拆分用于两级选择。
function splitModelId(id: string): { base: string; effort: string } {
  const parts = (id || '').split('/')
  if (parts.length >= 3) return { base: parts.slice(0, 2).join('/'), effort: parts[2] }
  return { base: id || '', effort: '' }
}
function assignmentBase(role: string) {
  return splitModelId(assignment(role).modelId).base
}
function assignmentEffort(role: string) {
  return splitModelId(assignment(role).modelId).effort
}
function setAssignment(role: string, base: string, effort: string) {
  assignment(role).modelId = base ? (effort ? `${base}/${effort}` : base) : ''
}
function effortsForModel(base: string) {
  const m = store.models.find((x) => x.id === base)
  const avail = m?.availableEfforts || []
  if (!avail.length) return EFFORTS
  return [{ id: '', label: '默认' }, ...EFFORTS.filter((e) => e.id && avail.includes(e.id))]
}

onMounted(async () => {
  await Promise.all([store.loadLLM(), initAppVersion()])
})

function openNew(preset?: any) {
  editingId.value = ''
  if (preset) {
    form.value = { provider: preset.provider, name: preset.name, apiKey: '', baseUrl: preset.baseUrl || '', defaultProtocolDialect: preset.defaultProtocolDialect || '', models: [] }
  } else {
    form.value = { provider: 'openai', name: '', apiKey: '', baseUrl: '', defaultProtocolDialect: '', models: [] }
  }
  showForm.value = true
}
function openEdit(p: any) {
  editingId.value = p.id
  form.value = { provider: p.provider, name: p.name, apiKey: '', baseUrl: p.baseUrl || '', defaultProtocolDialect: p.defaultProtocolDialect || '', models: [...(p.models || [])] }
  showForm.value = true
}
function addModel() {
  const name = manualModel.value.trim()
  if (!name) return
  if (form.value.models.some((m: any) => m.name === name)) return
  form.value.models.push({ name, enabled: true, protocolDialect: '' })
  manualModel.value = ''
}
async function fetchModels() {
  try {
    const res = await api.post<any>('/api/llm/configs/fetch-models', form.value)
    const prev = new Map(form.value.models.map((m: any) => [m.name, m]))
    form.value.models = (res.models || []).map((m: any) => ({ ...m, protocolDialect: m.protocolDialect || (prev.get(m.name) as any)?.protocolDialect || '' }))
    store.note('模型列表已刷新')
  } catch (e) { store.fail(e) }
}
async function save() {
  try {
    if (editingId.value) await api.put(`/api/llm/configs/${editingId.value}`, form.value)
    else await api.post('/api/llm/configs', form.value)
    showForm.value = false
    await store.loadLLM()
    store.note('已保存提供商')
  } catch (e) { store.fail(e) }
}
async function remove(id: string) {
  await api.del(`/api/llm/configs/${id}`)
  await store.loadLLM()
}
async function test(modelId: string) {
  testing.value[modelId] = true
  try {
    const res = await api.post<any>('/api/llm/test', { modelId })
    testResult.value[modelId] = res.ok ? `ok · ${res.durationMs}ms` : (res.error || 'failed')
  } catch (e: any) { testResult.value[modelId] = e.message } finally { testing.value[modelId] = false }
}
function assignment(role: string) {
  let a = store.assignments.find((x) => x.role === role)
  if (!a) {
    a = { role, modelId: '' }
    store.assignments.push(a)
  }
  return a
}
async function saveAssignments() {
  await api.put('/api/settings/model-assignments', { assignments: store.assignments })
  store.note('阶段模型已保存')
}
</script>

<template>
  <section class="settings-page">
    <h1>模型设置</h1>

    <section class="panel block update-block">
      <header class="block-head">
        <h2>桌面版</h2>
        <span v-if="availableVersion" class="update-badge">有更新</span>
      </header>
      <p class="muted">当前版本 {{ appVersion || '…' }} · {{ updaterStatusText }}</p>
      <div class="row-actions" style="margin-top:10px">
        <button :disabled="!isDesktop() || updaterBusy" @click="onCheckUpdate">
          {{ updaterStatus === 'checking' ? '检查中…' : '检查更新' }}
        </button>
        <button
          v-if="updaterStatus === 'available'"
          class="primary"
          :disabled="updaterBusy"
          @click="onInstallUpdate"
        >
          安装并重启
        </button>
      </div>
    </section>

    <div class="settings-grid">
      <section class="panel block">
        <header class="block-head">
          <h2>提供商</h2>
          <button class="primary" @click="openNew()">添加</button>
        </header>
        <div class="preset-row">
          <button v-for="p in store.presets.slice(0, 8)" :key="p.id" @click="openNew(p)">{{ p.name }}</button>
        </div>
        <div v-for="p in store.providers" :key="p.id" class="provider-row">
          <div>
            <strong>{{ p.name }}</strong>
            <span class="muted"> · {{ p.provider }}</span>
            <div class="muted">{{ (p.models || []).filter(m => m.enabled).map(m => m.name).join(', ') || '无启用模型' }}</div>
          </div>
          <div class="row-actions">
            <button @click="openEdit(p)">编辑</button>
            <button @click="remove(p.id)">删除</button>
          </div>
        </div>
        <p v-if="!store.providers.length" class="muted">还没有提供商。点上方预设开始。</p>
      </section>

      <section class="panel block">
        <header class="block-head"><h2>创作阶段模型</h2><button class="primary" @click="saveAssignments">保存</button></header>
        <div v-for="r in roles" :key="r.id" class="assign-block">
          <div class="assign-row">
            <span class="assign-label">{{ r.label }}</span>
            <div class="assign-controls">
              <select :value="assignmentBase(r.id)" @change="setAssignment(r.id, ($event.target as HTMLSelectElement).value, assignmentEffort(r.id))">
                <option value="">未设置</option>
                <option v-for="m in store.models" :key="m.id" :value="m.id">{{ m.id }}</option>
              </select>
              <select class="effort-select" :value="assignmentEffort(r.id)" :disabled="!assignmentBase(r.id)"
                @change="setAssignment(r.id, assignmentBase(r.id), ($event.target as HTMLSelectElement).value)"
                :title="`推理等级：影响 ${r.label} 阶段的思考深度`">
                <option v-for="e in effortsForModel(assignmentBase(r.id))" :key="e.id" :value="e.id">{{ e.label }}</option>
              </select>
            </div>
          </div>
          <p class="assign-hint">{{ r.hint }}</p>
        </div>
        <h3 style="margin-top:18px">可用模型</h3>
        <div v-for="m in store.models" :key="m.id" class="model-row">
          <span>{{ m.id }}</span>
          <button @click="test(m.id)">{{ testing[m.id] ? '测试中' : '测试' }}</button>
          <span class="muted">{{ testResult[m.id] }}</span>
        </div>
      </section>
    </div>

    <div v-if="showForm" class="modal-mask" @click.self="showForm = false">
      <div class="modal panel">
        <h2>{{ editingId ? '编辑提供商' : '添加提供商' }}</h2>
        <label>协议
          <select v-model="form.provider" :disabled="!!editingId">
            <option value="openai">OpenAI Chat Completions</option>
            <option value="openai_responses">OpenAI Responses</option>
            <option value="anthropic">Anthropic Messages</option>
            <option value="mock">Mock</option>
          </select>
        </label>
        <label>名称<input v-model="form.name" :disabled="!!editingId" /></label>
        <label>API Key<input v-model="form.apiKey" type="password" placeholder="留空保留原密钥" /></label>
        <label>Base URL<input v-model="form.baseUrl" /></label>
        <label>默认方言
          <select v-model="form.defaultProtocolDialect">
            <option value="">自动</option>
            <option value="openai">openai</option><option value="deepseek">deepseek</option>
            <option value="qwen">qwen</option><option value="kimi">kimi</option>
            <option value="glm">glm</option><option value="minimax">minimax</option>
            <option value="gemini">gemini</option><option value="grok">grok</option>
          </select>
        </label>
        <div class="model-tools">
          <button @click="fetchModels">刷新模型列表</button>
          <input v-model="manualModel" placeholder="手工添加模型" @keydown.enter.prevent="addModel" />
          <button @click="addModel">添加模型</button>
        </div>
        <div v-for="m in form.models" :key="m.name" class="model-edit-row">
          <span>{{ m.name }}</span>
          <input v-model="m.protocolDialect" placeholder="dialect" />
          <input v-model="m.enabled" type="checkbox" />
        </div>
        <div class="modal-actions">
          <button @click="showForm = false">取消</button>
          <button class="primary" @click="save">保存</button>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.settings-page h1 { font-family: var(--font-prose); }
.update-block { margin-bottom: 18px; padding: 18px; }
.update-badge {
  font-size: 12px; font-weight: 700; color: var(--accent);
  border: 1px solid var(--accent); border-radius: 999px; padding: 2px 10px;
}
.settings-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 18px; align-items: start; }
.block { padding: 18px; }
.block-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; }
.block-head h2 { margin: 0; font-size: 18px; }
.preset-row { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 16px; }
.provider-row { display: flex; justify-content: space-between; gap: 10px; padding: 12px 0; border-top: 1px solid var(--line); }
.muted { color: var(--ink-soft); font-size: 12px; }
.row-actions { display: flex; gap: 6px; }
.assign-block { padding: 12px 0; border-top: 1px solid var(--line); }
.assign-block:first-of-type { border-top: none; }
.assign-row { display: grid; grid-template-columns: 64px 1fr; gap: 12px; align-items: center; }
.assign-label { font-weight: 600; font-size: 14px; }
.assign-controls { display: grid; grid-template-columns: 1fr 150px; gap: 8px; }
.assign-row select, .modal input, .modal select { padding: 8px 10px; border-radius: 8px; border: 1px solid var(--line); width: 100%; }
.effort-select { color: var(--accent); }
.assign-hint { margin: 8px 0 0 76px; font-size: 11px; color: var(--ink-soft); line-height: 1.65; }
.model-row { display: flex; gap: 8px; align-items: center; padding: 8px 0; border-top: 1px solid var(--line); font-size: 13px; }
.modal-mask { position: fixed; inset: 0; background: rgba(20,35,31,0.3); display: grid; place-items: center; z-index: 40; }
.modal { width: min(520px, 94vw); max-height: 90vh; overflow: auto; padding: 22px; display: grid; gap: 12px; }
.modal label { display: grid; gap: 6px; font-size: 13px; color: var(--ink-soft); }
.model-tools { display: flex; gap: 8px; flex-wrap: wrap; }
.model-tools input { flex: 1; }
.model-edit-row { display: grid; grid-template-columns: 1fr 100px 30px; gap: 8px; align-items: center; font-size: 13px; }
.modal-actions { display: flex; justify-content: flex-end; gap: 10px; }
@media (max-width: 900px) { .settings-grid { grid-template-columns: 1fr; } }
</style>
