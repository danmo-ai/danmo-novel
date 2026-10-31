<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useAppStore } from '@/stores/app'
import { api } from '@/api/client'
import { useAppUpdater } from '@/composables/useAppUpdater'
import type { ModelConfig } from '@/types'

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

// "dev" / "dirty" build tags are placeholders, not shippable versions.
const versionLabel = computed(() => {
  const v = appVersion.value.trim()
  return v === '' || v === 'dev' || v.includes('dirty') ? '开发版' : v
})

const updaterStatusText = computed(() => {
  switch (updaterStatus.value) {
    case 'checking':
      return '正在检查更新…'
    case 'upToDate':
      return `已是最新版本（${versionLabel.value}）`
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
      return isDesktop() ? `当前版本 ${versionLabel.value}，可检查桌面版更新` : `当前版本 ${versionLabel.value}；自升级仅在桌面版可用`
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

type Tab = 'version' | 'providers' | 'stages' | 'modelConfig'
const activeTab = ref<Tab>('providers')
const showForm = ref(false)
const editingId = ref('')
const form = ref<any>({ provider: 'openai', name: '', apiKey: '', baseUrl: '', defaultProtocolDialect: '', models: [] })
const manualModel = ref('')
const testing = ref<Record<string, boolean>>({})
const testResult = ref<Record<string, string>>({})

// ---- Model Config ----
const modelConfigForm = ref<ModelConfig[]>([])
const selectedModel = ref('__default__')
const editingModelIdx = ref<number | null>(null)
const defaultForm = ref({ context_window: 128000, max_output: 8192, temperature: 0.7 })
const mcSaving = ref(false)
const showAddModelDialog = ref(false)
const newModelName = ref('')
const newContextWindow = ref<number | undefined>(undefined)
const newMaxOutput = ref<number | undefined>(undefined)
const newTemperature = ref<number | undefined>(undefined)
const newAvailableEfforts = ref('')

const editingModel = computed(() => {
  if (editingModelIdx.value === null) return null
  return modelConfigForm.value[editingModelIdx.value] ?? null
})

watch(selectedModel, (model) => {
  if (!model || model === '__default__') { editingModelIdx.value = null; return }
  editingModelIdx.value = modelConfigForm.value.findIndex((m) => m.model === model)
})

async function loadModelConfig() {
  try {
    const res = await api.get<{ models: ModelConfig[] }>('/api/model-configs')
    modelConfigForm.value = res.models || []
  } catch { modelConfigForm.value = [] }
}

async function saveModelConfig() {
  mcSaving.value = true
  try {
    const cleaned = modelConfigForm.value.filter((m) => m.model.trim() !== '')
    const res = await api.put<{ models: ModelConfig[] }>('/api/model-configs', cleaned)
    modelConfigForm.value = res.models || cleaned
    store.note('模型参数已保存')
    await store.loadLLM()
  } catch (e) { store.fail(e) } finally { mcSaving.value = false }
}

async function refreshModelConfig() {
  mcSaving.value = true
  try {
    const res = await api.post<{ models: ModelConfig[] }>('/api/model-configs/refresh')
    modelConfigForm.value = res.models || []
    store.note('已重置为内置目录')
    await store.loadLLM()
  } catch (e) { store.fail(e) } finally { mcSaving.value = false }
}

function addModelEntry() {
  showAddModelDialog.value = true
}

function confirmAddModel() {
  const name = newModelName.value.trim()
  if (!name) return
  if (modelConfigForm.value.find((m) => m.model === name)) return
  const efforts = newAvailableEfforts.value.trim()
  modelConfigForm.value.push({
    model: name,
    context_window: newContextWindow.value || undefined,
    max_output: newMaxOutput.value || undefined,
    temperature: newTemperature.value || undefined,
    available_efforts: efforts ? efforts.split(',').map((s) => s.trim()).filter(Boolean) : undefined,
  })
  showAddModelDialog.value = false
  newModelName.value = ''
  newContextWindow.value = undefined
  newMaxOutput.value = undefined
  newTemperature.value = undefined
  newAvailableEfforts.value = ''
  editingModelIdx.value = modelConfigForm.value.length - 1
  selectedModel.value = name
}

function removeModelEntry() {
  if (editingModelIdx.value === null) return
  modelConfigForm.value.splice(editingModelIdx.value, 1)
  editingModelIdx.value = null
  selectedModel.value = '__default__'
}
// 阶段建议：该阶段对模型的能力要求与推理等级建议。
const roles = [
  { id: 'plan', label: '立项', hint: '立项=总纲派生设定成套；重写设定携带依据；重新立项可保留精修文件。建议推理强的模型，effort medium–high。', effort: 'medium' },
  { id: 'outline', label: '细纲', hint: '单元合同的欲望/阻碍/选择与场面节拍，需要结构化输出与因果链。建议 effort medium。', effort: 'medium' },
  { id: 'write', label: '写作', hint: '正文 prose，文风与节奏优先；过长推理会稀释文气。建议文风好的模型，effort low–medium。', effort: 'low' },
  { id: 'review', label: '审校', hint: '找 OOC、连续性、写穿与 AI 味，需要严格比对与挑错能力。建议 effort medium–high。', effort: 'medium' },
  { id: 'local_edit', label: '局部编辑', hint: '选区润色/改写，改动小、要求贴原文。轻量模型即可，effort low。', effort: 'low' },
]
// Reasoning effort labels. Keep in sync with the backend effort vocabulary
// (off/none/minimal/low/medium/high/xhigh/max/ultra); unknown ids fall back to
// the raw id so new vendor levels never get silently dropped.
const EFFORT_LABELS: Record<string, string> = {
  '': '默认',
  off: 'off（关闭推理）',
  none: 'none',
  minimal: 'minimal',
  low: 'low',
  medium: 'medium',
  high: 'high',
  xhigh: 'xhigh',
  max: 'max',
  ultra: 'ultra',
}
const FALLBACK_EFFORTS = ['off', 'low', 'medium', 'high', 'xhigh']
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
  // Drive the options straight from the model catalog so vendor-specific levels
  // (max / none / minimal) are not filtered out by a hardcoded whitelist.
  const ids = avail.length ? avail : (base ? FALLBACK_EFFORTS : [])
  return ['', ...ids].map((id) => ({ id, label: EFFORT_LABELS[id] ?? id }))
}

onMounted(async () => {
  await Promise.all([store.loadLLM(), initAppVersion(), loadModelConfig()])
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
    <header class="settings-hero">
      <p class="eyebrow">Settings</p>
      <h1>配置你的写作引擎。</h1>
      <p class="sub">版本 · 提供商 · 创作阶段 · 模型参数</p>
    </header>

    <nav class="tab-bar">
      <button :class="{ active: activeTab === 'version' }" @click="activeTab = 'version'">版本</button>
      <button :class="{ active: activeTab === 'providers' }" @click="activeTab = 'providers'">提供商</button>
      <button :class="{ active: activeTab === 'stages' }" @click="activeTab = 'stages'">创作阶段</button>
      <button :class="{ active: activeTab === 'modelConfig' }" @click="activeTab = 'modelConfig'">模型参数</button>
    </nav>

    <!-- ===== Version Tab ===== -->
    <div v-if="activeTab === 'version'" class="tab-content">
      <section class="panel block">
        <header class="block-head">
          <h2>桌面版</h2>
          <span v-if="availableVersion" class="update-badge">有更新</span>
        </header>
        <p class="muted">{{ updaterStatusText }}</p>
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
    </div>

    <!-- ===== Providers Tab ===== -->
    <div v-else-if="activeTab === 'providers'" class="tab-content">
      <section class="panel block">
        <header class="block-head">
          <h2>提供商</h2>
          <button class="primary" @click="openNew()">添加</button>
        </header>
        <div class="preset-row">
          <button v-for="p in store.presets" :key="p.id" class="preset-chip" @click="openNew(p)">{{ p.name }}</button>
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
        <header class="block-head"><h2>可用模型</h2></header>
        <div v-for="m in store.models" :key="m.id" class="model-row">
          <span>{{ m.id }}</span>
          <button @click="test(m.id)">{{ testing[m.id] ? '测试中' : '测试' }}</button>
          <span class="muted">{{ testResult[m.id] }}</span>
        </div>
        <p v-if="!store.models.length" class="muted">还没有可用模型。先添加提供商。</p>
      </section>
    </div>

    <!-- ===== Stages Tab ===== -->
    <div v-else-if="activeTab === 'stages'" class="tab-content">
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
      </section>
    </div>

    <!-- ===== Model Config Tab ===== -->
    <div v-else-if="activeTab === 'modelConfig'" class="tab-content">
      <section class="panel block">
        <header class="block-head">
          <h2>模型参数</h2>
          <div class="row-actions">
            <button @click="refreshModelConfig" :disabled="mcSaving">重置内置</button>
            <button class="primary" @click="saveModelConfig" :disabled="mcSaving">{{ mcSaving ? '保存中…' : '保存' }}</button>
          </div>
        </header>
        <p class="muted" style="margin-bottom:14px">配置每个模型的容量、采样与推理参数。留空表示使用默认值。</p>

        <div class="mc-form">
          <label class="mc-field">
            <span class="mc-label">模型</span>
            <select v-model="selectedModel">
              <option value="__default__">默认 (128K / 8K / 0.7)</option>
              <option v-for="item in modelConfigForm" :key="item.model" :value="item.model">{{ item.model }}</option>
            </select>
          </label>

          <template v-if="selectedModel === '__default__'">
            <label class="mc-field"><span class="mc-label">Context Window</span><input v-model.number="defaultForm.context_window" type="number" /></label>
            <label class="mc-field"><span class="mc-label">Max Output</span><input v-model.number="defaultForm.max_output" type="number" /></label>
            <label class="mc-field"><span class="mc-label">Temperature</span><input v-model.number="defaultForm.temperature" type="number" step="0.1" /></label>
          </template>

          <template v-else-if="editingModel">
            <div class="mc-group">
              <h3>容量</h3>
              <label class="mc-field"><span class="mc-label">Context Window</span><input v-model.number="editingModel.context_window" type="number" placeholder="留空使用默认" /></label>
              <label class="mc-field"><span class="mc-label">Max Output</span><input v-model.number="editingModel.max_output" type="number" placeholder="留空使用默认" /></label>
              <label class="mc-field mc-switch">
                <span class="mc-label">Vision</span>
                <input v-model="editingModel.vision" type="checkbox" />
                <span class="muted">支持图片输入</span>
              </label>
            </div>
            <div class="mc-group">
              <h3>采样</h3>
              <label class="mc-field"><span class="mc-label">Temperature</span><input v-model.number="editingModel.temperature" type="number" step="0.1" placeholder="留空使用默认" /></label>
              <label class="mc-field"><span class="mc-label">Top P</span><input v-model.number="editingModel.top_p" type="number" step="0.05" placeholder="留空使用默认" /></label>
              <label class="mc-field"><span class="mc-label">Frequency Penalty</span><input v-model.number="editingModel.frequency_penalty" type="number" step="0.1" placeholder="留空使用默认" /></label>
              <label class="mc-field"><span class="mc-label">Presence Penalty</span><input v-model.number="editingModel.presence_penalty" type="number" step="0.1" placeholder="留空使用默认" /></label>
              <label class="mc-field"><span class="mc-label">Stop Sequences</span><input :value="(editingModel.stop ?? []).join(', ')" @input="editingModel.stop = ($event.target as HTMLInputElement).value.split(',').map(s => s.trim()).filter(Boolean)" placeholder="逗号分隔" /></label>
            </div>
            <div class="mc-group">
              <h3>推理</h3>
              <label class="mc-field"><span class="mc-label">Available Efforts</span><input :value="(editingModel.available_efforts ?? []).join(', ')" @input="editingModel.available_efforts = ($event.target as HTMLInputElement).value.split(',').map(s => s.trim()).filter(Boolean)" placeholder="off, low, medium, high, xhigh" /></label>
            </div>
            <button class="mc-delete" @click="removeModelEntry">删除此模型配置</button>
          </template>
          <p v-else class="muted">请选择或添加一个模型</p>
        </div>

        <div class="row-actions" style="margin-top:16px">
          <button @click="addModelEntry">添加模型配置</button>
        </div>
      </section>

      <div v-if="showAddModelDialog" class="modal-mask" @click.self="showAddModelDialog = false">
        <div class="modal panel">
          <h2>添加模型配置</h2>
          <label>模型名<input v-model="newModelName" placeholder="如 gpt-5.6" /></label>
          <label>Context Window<input v-model.number="newContextWindow" type="number" placeholder="可选" /></label>
          <label>Max Output<input v-model.number="newMaxOutput" type="number" placeholder="可选" /></label>
          <label>Temperature<input v-model.number="newTemperature" type="number" step="0.1" placeholder="可选" /></label>
          <label>Available Efforts<input v-model="newAvailableEfforts" placeholder="off, low, medium, high, xhigh" /></label>
          <div class="modal-actions">
            <button @click="showAddModelDialog = false">取消</button>
            <button class="primary" @click="confirmAddModel">添加</button>
          </div>
        </div>
      </div>
    </div>

    <!-- ===== Provider Form Modal (shared) ===== -->
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
            <option value="kimi_code">kimi_code</option><option value="kimi_k3">kimi_k3</option>
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
          <select v-model="m.protocolDialect">
            <option value="">自动</option>
            <option value="openai">openai</option><option value="deepseek">deepseek</option>
            <option value="qwen">qwen</option><option value="kimi">kimi</option>
            <option value="kimi_code">kimi_code</option><option value="kimi_k3">kimi_k3</option>
            <option value="glm">glm</option><option value="minimax">minimax</option>
            <option value="gemini">gemini</option><option value="grok">grok</option>
          </select>
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
.settings-hero { padding: 32px 4px 24px; }
.eyebrow { color: var(--accent); letter-spacing: 0.16em; text-transform: uppercase; font-size: 12px; margin: 0; }
.settings-hero h1 { font-family: var(--font-prose); font-size: clamp(28px, 3.5vw, 44px); line-height: 1.05; margin: 8px 0 8px; }
.sub { color: var(--ink-soft); font-size: 13px; line-height: 1.7; margin: 0; }
.tab-bar { display: flex; gap: 0; border-bottom: 1px solid var(--line); margin-bottom: 22px; }
.tab-bar button { padding: 10px 22px; border: none; background: none; cursor: pointer; font-size: 14px; color: var(--ink-soft); border-bottom: 2px solid transparent; margin-bottom: -1px; border-radius: 0; }
.tab-bar button:hover:not(:disabled) { color: var(--accent); background: none; border-color: transparent; }
.tab-bar button.active { color: var(--accent); border-bottom-color: var(--accent); font-weight: 600; }
.tab-content { display: grid; gap: 18px; align-items: start; }
.block { padding: 20px; }
.block-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 14px; }
.block-head h2 { margin: 0; font-size: 17px; }
.update-block { margin-bottom: 0; }
.update-badge {
  font-size: 12px; font-weight: 700; color: var(--accent);
  border: 1px solid var(--accent); border-radius: 999px; padding: 2px 10px;
}
.preset-row { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 16px; }
.preset-chip { font-size: 12px; padding: 6px 12px; }
.provider-row { display: flex; justify-content: space-between; gap: 10px; padding: 12px 0; border-top: 1px solid var(--line); }
.muted { color: var(--ink-soft); font-size: 12px; }
.row-actions { display: flex; gap: 6px; }
.assign-block { padding: 12px 0; border-top: 1px solid var(--line); }
.assign-block:first-of-type { border-top: none; }
.assign-row { display: grid; grid-template-columns: 64px 1fr; gap: 12px; align-items: center; }
.assign-label { font-weight: 600; font-size: 14px; }
.assign-controls { display: grid; grid-template-columns: 1fr 150px; gap: 8px; }
.assign-row select, .modal input, .modal select { padding: 10px 12px; border-radius: 10px; border: 1px solid var(--line); width: 100%; }
.effort-select { color: var(--accent); }
.assign-hint { margin: 8px 0 0 76px; font-size: 11px; color: var(--ink-soft); line-height: 1.65; }
.model-row { display: flex; gap: 8px; align-items: center; padding: 8px 0; border-top: 1px solid var(--line); font-size: 13px; }
.modal-mask { position: fixed; inset: 0; background: rgba(20,35,31,0.3); display: grid; place-items: center; z-index: 40; }
.modal { width: min(520px, 94vw); max-height: 90vh; overflow: auto; padding: 24px; display: grid; gap: 14px; }
.modal label { display: grid; gap: 6px; font-size: 13px; color: var(--ink-soft); }
.model-tools { display: flex; gap: 8px; flex-wrap: wrap; }
.model-tools input { flex: 1; }
.model-edit-row { display: grid; grid-template-columns: 1fr 130px 30px; gap: 8px; align-items: center; font-size: 13px; }
.modal-actions { display: flex; justify-content: flex-end; gap: 10px; }
.mc-form { display: grid; gap: 14px; }
.mc-group { display: grid; gap: 10px; padding: 14px; background: rgba(20,35,31,.04); border-radius: 12px; }
.mc-group h3 { margin: 0; font-size: 13px; font-weight: 600; color: var(--ink-soft); text-transform: uppercase; letter-spacing: 0.5px; }
.mc-field { display: grid; gap: 6px; font-size: 13px; }
.mc-field .mc-label { color: var(--ink-soft); font-size: 12px; }
.mc-field input, .mc-field select { padding: 10px 12px; border-radius: 10px; border: 1px solid var(--line); width: 100%; }
.mc-switch { display: flex; align-items: center; gap: 8px; }
.mc-switch input { width: auto; }
.mc-delete { margin-top: 8px; color: var(--bad); background: none; border: 1px solid var(--bad); border-radius: 10px; padding: 6px 14px; cursor: pointer; font-size: 13px; }
</style>
