<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api } from '@/api/client'
import type { ContextPacket } from '@/types'

const props = defineProps<{
  spec: Record<string, any>
  title: string
  modelLabel: string
}>()
const emit = defineEmits<{ confirm: [payload: { instruction: string; knowledgeIds: string[] }]; cancel: [] }>()

const loading = ref(true)
const loadError = ref('')
const packet = ref<ContextPacket | null>(null)
const instruction = ref('')
const expandedBlock = ref('')
const knowledgeDocs = ref<{ id: string; title: string; summary: string; bytes: number }[]>([])
const selectedKnowledge = ref<Set<string>>(new Set())
const knowledgeOpen = ref(false)

const KIND_LABEL: Record<string, string> = {
  plan: '规划', outline: '细纲', write: '写正文', review: '审校',
  revise: '修订', finalize: '定稿', rewrite_entity: '重写优化', local_edit: '局部编辑',
}
const kindLabel = computed(() => KIND_LABEL[props.spec.kind] || props.spec.kind)
const totalBytes = computed(() => (packet.value?.blocks || []).reduce((s, b) => s + b.bytes, 0))

onMounted(async () => {
  try {
    packet.value = await api.post<ContextPacket>('/api/runs/preview', props.spec)
  } catch (e: any) {
    loadError.value = e?.message || '预览上下文失败'
  } finally {
    loading.value = false
  }
  try {
    knowledgeDocs.value = (await api.get<any[]>('/api/knowledge')) || []
  } catch { /* 知识库为空或不可读时忽略 */ }
})

function toggleKnowledge(id: string) {
  const s = new Set(selectedKnowledge.value)
  if (s.has(id)) s.delete(id)
  else s.add(id)
  selectedKnowledge.value = s
}

function confirm() {
  emit('confirm', { instruction: instruction.value.trim(), knowledgeIds: [...selectedKnowledge.value] })
}
</script>

<template>
  <div class="modal-mask" @click.self="emit('cancel')">
    <div class="run-dialog panel">
      <header class="dlg-head">
        <div>
          <small>{{ kindLabel }} · LLM 调用确认</small>
          <h3>{{ title }}</h3>
        </div>
        <button class="dlg-close" @click="emit('cancel')">×</button>
      </header>

      <div class="dlg-meta">
        <span class="meta-item"><small>模型</small><strong>{{ modelLabel || '按阶段分配' }}</strong></span>
        <span class="meta-item"><small>调用预算</small><strong>{{ packet?.policy.maxCalls ?? '…' }} 次</strong></span>
        <span class="meta-item"><small>上下文</small><strong>{{ packet ? `${packet.blocks.length} 块 · ${(totalBytes / 1024).toFixed(1)} KB` : '…' }}</strong></span>
      </div>

      <div class="dlg-context">
        <small class="ctx-label">将发送给模型的内容（字段级抽取，作者底牌不进入）</small>
        <p v-if="loading" class="ctx-loading">正在构建上下文预览…</p>
        <p v-else-if="loadError" class="ctx-error">{{ loadError }}</p>
        <template v-else-if="packet">
          <div class="ctx-system"><small>系统提示词</small><p>{{ packet.systemPrompt }}</p></div>
          <div class="ctx-blocks">
            <details v-for="b in packet.blocks" :key="b.id" class="ctx-block" :open="expandedBlock === b.id" @toggle="expandedBlock = ($event.target as HTMLDetailsElement).open ? b.id : ''">
              <summary><span class="ctx-block-label">{{ b.label }}</span><span class="ctx-block-meta">{{ b.source }} · {{ b.bytes }} B</span></summary>
              <pre>{{ b.content }}</pre>
            </details>
          </div>
        </template>
      </div>

      <div class="dlg-knowledge" v-if="knowledgeDocs.length">
        <button class="knowledge-toggle" @click="knowledgeOpen = !knowledgeOpen">
          <span>参考知识（可选，作者资料库）{{ selectedKnowledge.size ? ` · 已选 ${selectedKnowledge.size} 篇` : '' }}</span>
          <span>{{ knowledgeOpen ? '收起' : '展开' }}</span>
        </button>
        <div v-if="knowledgeOpen" class="knowledge-list">
          <p class="knowledge-note">勾选的文章会作为「方法/风格参考」附加到本次调用，不写入正文事实。</p>
          <label v-for="d in knowledgeDocs" :key="d.id" class="knowledge-item" :class="{ selected: selectedKnowledge.has(d.id) }">
            <input type="checkbox" :checked="selectedKnowledge.has(d.id)" @change="toggleKnowledge(d.id)" />
            <span class="knowledge-info"><strong>{{ d.title }}</strong><small>{{ d.summary }}</small></span>
            <span class="knowledge-bytes">{{ (d.bytes / 1024).toFixed(1) }}KB</span>
          </label>
        </div>
      </div>

      <div class="dlg-instruction">
        <label>追加指令（可选，会附加到本次调用的用户提示）</label>
        <textarea v-model="instruction" rows="3" placeholder="例如：保持既有事实，重点精简第 2 节；或：本卷新增人物 X，纳入索引…" spellcheck="false" />
      </div>

      <footer class="dlg-actions">
        <button @click="emit('cancel')">取消</button>
        <button class="primary" :disabled="loading || !!loadError" @click="confirm">确认调用</button>
      </footer>
    </div>
  </div>
</template>

<style scoped>
.modal-mask { position: fixed; inset: 0; background: rgba(20,35,31,.35); display: grid; place-items: center; z-index: 60; padding: 24px; }
.run-dialog { width: min(720px, 96vw); max-height: 90vh; display: flex; flex-direction: column; padding: 0; overflow: hidden; }
.dlg-head { display: flex; justify-content: space-between; align-items: flex-start; padding: 22px 24px 14px; }
.dlg-head small { color: var(--accent); font-size: 11px; font-weight: 700; letter-spacing: .05em; }
.dlg-head h3 { margin: 6px 0 0; font-family: var(--font-prose); font-size: 22px; }
.dlg-close { border: none; background: transparent; font-size: 22px; color: var(--ink-soft); padding: 0 4px; }
.dlg-meta { display: flex; gap: 24px; padding: 0 24px 16px; border-bottom: 1px solid var(--line); flex-wrap: wrap; }
.meta-item { display: grid; gap: 2px; }
.meta-item small { color: var(--ink-soft); font-size: 10px; }
.meta-item strong { font-size: 13px; }
.dlg-context { flex: 1; overflow-y: auto; padding: 16px 24px; min-height: 0; }
.ctx-label { color: var(--ink-soft); font-size: 11px; font-weight: 600; display: block; margin-bottom: 10px; }
.ctx-loading, .ctx-error { color: var(--ink-soft); font-size: 13px; }
.ctx-error { color: var(--bad); }
.ctx-system { margin-bottom: 12px; padding: 12px 14px; background: rgba(15,107,92,.05); border-radius: 10px; }
.ctx-system small { color: var(--accent); font-size: 10px; font-weight: 700; }
.ctx-system p { margin: 6px 0 0; font-size: 12px; line-height: 1.7; color: var(--ink-soft); white-space: pre-wrap; }
.ctx-blocks { display: grid; gap: 6px; }
.ctx-block { border: 1px solid var(--line); border-radius: 10px; overflow: hidden; }
.ctx-block summary { display: flex; justify-content: space-between; align-items: center; gap: 10px; padding: 10px 12px; cursor: pointer; background: rgba(255,255,255,.5); }
.ctx-block-label { font-size: 12px; font-weight: 600; }
.ctx-block-meta { font-size: 10px; color: var(--ink-soft); }
.ctx-block pre { margin: 0; padding: 12px; font: 11px/1.7 ui-monospace, monospace; white-space: pre-wrap; color: var(--ink-soft); max-height: 260px; overflow: auto; border-top: 1px solid var(--line); }
.dlg-knowledge { border-top: 1px solid var(--line); }
.knowledge-toggle { display: flex; justify-content: space-between; align-items: center; width: 100%; border: none; background: transparent; padding: 14px 24px; text-align: left; font-size: 13px; color: var(--ink); }
.knowledge-toggle span:last-child { color: var(--accent); font-size: 12px; }
.knowledge-list { padding: 0 24px 14px; }
.knowledge-note { margin: 0 0 10px; font-size: 11px; color: var(--ink-soft); line-height: 1.6; }
.knowledge-item { display: flex; align-items: flex-start; gap: 10px; padding: 10px 12px; border: 1px solid var(--line); border-radius: 10px; margin-bottom: 6px; cursor: pointer; }
.knowledge-item.selected { border-color: var(--accent); background: rgba(15,107,92,.05); }
.knowledge-item input { margin-top: 3px; }
.knowledge-info { display: grid; gap: 2px; flex: 1; min-width: 0; }
.knowledge-info strong { font-size: 13px; }
.knowledge-info small { font-size: 11px; color: var(--ink-soft); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.knowledge-bytes { font-size: 10px; color: var(--ink-soft); flex-shrink: 0; }
.dlg-instruction { padding: 14px 24px; border-top: 1px solid var(--line); }
.dlg-instruction label { display: block; font-size: 12px; color: var(--ink-soft); margin-bottom: 8px; }
.dlg-instruction textarea { width: 100%; padding: 10px 12px; border: 1px solid var(--line); border-radius: 10px; font-size: 13px; resize: vertical; background: rgba(255,255,255,.85); }
.dlg-actions { display: flex; justify-content: flex-end; gap: 10px; padding: 14px 24px 20px; }
</style>
