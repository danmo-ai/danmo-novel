<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import UnitContract from '@/shared/components/book/UnitContract.vue'
import UnitProse from '@/shared/components/book/UnitProse.vue'
import UnitFlowActions from '@/shared/components/book/UnitFlowActions.vue'
import ReviewWorkspace from '@/shared/components/book/ReviewWorkspace.vue'
import { useInjectedWorkspace } from '../workspace'

/**
 * 单元详情：桌面那五个平铺 Tab 在这里变成三段。
 *
 * 细纲 / 正文 / 审校 是同一个单元的不同阶段，不是三个并列的功能；手机上一次
 * 只显示一段（顶部三段控件切换），所以不会再出现「七个面板塞进一屏」。
 * 定稿不是页面，它是 UnitFlowActions 里的一个动作。
 */
const props = defineProps<{ unitId: string }>()
const router = useRouter()

const ws = useInjectedWorkspace()
const {
  store, view, selectedUnit, editorPath, editorContent, saving, busy,
  isProseUnit, editingProse, editingOutline,
  beginChapterEdit, cancelChapterEdit, saveChapterEdit, saveFile, runUnitAction,
  reviseFindings, retrySplitReview, focusUnit, rejectUnitFindings, accept, reject, statusText,
} = ws

type Segment = 'outline' | 'write' | 'review'
const segments: { key: Segment; label: string }[] = [
  { key: 'outline', label: '细纲' },
  { key: 'write', label: '正文' },
  { key: 'review', label: '审校' },
]
const segment = computed<Segment>(() => (view.value === 'review' ? 'review' : view.value === 'write' ? 'write' : 'outline'))

function pickSegment(next: Segment) {
  // 审校/正文 在 workspace 里会切到对应 Tab；细纲保持在大纲语义。
  view.value = next
}

function back() {
  // 原来是 router.push('/books/:id')，会让系统返回键变成「再进入这个单元」。
  // 有历史栈就退到列表，没有就替换到书根。
  if (window.history.state?.back) router.back()
  else router.replace(`/books/${store.currentBook?.summary.id}`)
}
</script>

<template>
  <section v-if="!selectedUnit" class="state panel">
    <p>没有找到这个单元。</p>
    <button type="button" @click="back">返回单元列表</button>
  </section>

  <template v-else>
    <header class="unit-head">
      <div>
        <small>{{ selectedUnit.unitId }} · {{ statusText[selectedUnit.status] || selectedUnit.status }}
          <template v-if="selectedUnit.chapterFrom"> · 第 {{ selectedUnit.chapterFrom }}–{{ selectedUnit.chapterTo }} 章</template>
        </small>
        <h2>{{ selectedUnit.title || '未完成单元合同' }}</h2>
      </div>
    </header>

    <div class="segments" role="tablist" aria-label="单元阶段">
      <button
        v-for="s in segments"
        :key="s.key"
        type="button"
        role="tab"
        :aria-selected="segment === s.key"
        :class="{ active: segment === s.key }"
        @click="pickSegment(s.key)"
      >{{ s.label }}</button>
    </div>

    <!-- 细纲 -->
    <section v-if="segment === 'outline'" class="segment-body">
      <div class="actions">
        <UnitFlowActions
          v-if="!editingOutline"
          :unit="selectedUnit"
          :busy="busy"
          :read-only="store.currentBook?.summary.readOnly ?? false"
          emphasize
          @run="runUnitAction"
        />
        <button v-if="!editingOutline" type="button" @click="editingOutline = true">手工编辑细纲</button>
      </div>

      <template v-if="!editingOutline">
        <UnitContract :unit="selectedUnit" />
        <details class="raw">
          <summary>查看细纲源文件</summary>
          <pre>{{ editorContent }}</pre>
        </details>
      </template>
      <div v-else class="editor">
        <div class="editor-bar">
          <span>{{ selectedUnit.outlinePath }}</span>
          <div>
            <button type="button" class="primary" :disabled="saving" @click="saveFile(); editingOutline = false">{{ saving ? '保存中' : '保存' }}</button>
            <button type="button" @click="editingOutline = false">取消</button>
          </div>
        </div>
        <textarea v-model="editorContent" class="prose-editor" spellcheck="false" />
      </div>
    </section>

    <!-- 正文 -->
    <section v-else-if="segment === 'write'" class="segment-body">
      <template v-if="isProseUnit">
        <UnitProse
          v-if="!editingProse"
          :unit="selectedUnit"
          :raw="editorContent"
          :can-write="!(store.currentBook?.summary.readOnly ?? false)"
          continuous
          @edit="beginChapterEdit"
        >
          <template #actions>
            <UnitFlowActions
              :unit="selectedUnit"
              :busy="busy"
              :read-only="store.currentBook?.summary.readOnly ?? false"
              emphasize
              @run="runUnitAction"
            />
          </template>
        </UnitProse>
        <div v-else class="editor">
          <div class="editor-bar">
            <span>{{ editorPath }}</span>
            <div>
              <button type="button" class="primary" :disabled="saving" @click="saveChapterEdit">{{ saving ? '保存中' : '保存' }}</button>
              <button type="button" @click="cancelChapterEdit">取消</button>
            </div>
          </div>
          <textarea v-model="editorContent" class="prose-editor" spellcheck="false" />
        </div>
      </template>

      <template v-else>
        <div class="actions">
          <UnitFlowActions
            :unit="selectedUnit"
            :busy="busy"
            :read-only="store.currentBook?.summary.readOnly ?? false"
            emphasize
            @run="runUnitAction"
          />
        </div>
        <p class="hint">这个单元还没有正文。先按上面的步骤生成候选稿；候选接受后，正文按章在这里阅读和修改。</p>
        <UnitContract :unit="selectedUnit" />
      </template>
    </section>

    <!-- 审校 -->
    <section v-else class="segment-body">
      <ReviewWorkspace
        :selected-unit="selectedUnit"
        :only-unit="selectedUnit.unitId"
        :busy="busy"
        :read-only="store.currentBook?.summary.readOnly ?? false"
        @revise="reviseFindings"
        @retry-split="retrySplitReview"
        @focus-unit="focusUnit"
        @reject-unit="rejectUnitFindings"
        @accept="accept"
        @reject-candidate="reject"
      />
    </section>
  </template>
</template>

<style scoped>
.state { padding: 28px 20px; text-align: center; color: var(--ink-soft); font-size: 13px; }
.state p { margin: 0 0 12px; }
.unit-head { padding: 0 2px 10px; }
.unit-head small { color: var(--accent); font-size: 11px; font-weight: 600; }
.unit-head h2 { margin: 4px 0 0; font-family: var(--font-prose); font-size: 22px; line-height: 1.3; overflow-wrap: anywhere; }
.segments { display: grid; grid-template-columns: repeat(3, 1fr); gap: 4px; padding: 4px; border-radius: 12px; background: rgba(20,35,31,.05); margin-bottom: 14px; }
.segments button { border: 0; background: transparent; color: var(--ink-soft); font-weight: 600; border-radius: 9px; }
.segments button.active { background: white; color: var(--accent); box-shadow: 0 2px 8px rgba(20,35,31,.1); }
.segment-body { display: grid; gap: 12px; }
.actions { display: grid; gap: 8px; }
.hint { margin: 0; color: var(--ink-soft); font-size: 12px; line-height: 1.7; }
.raw summary { padding: 8px 0; cursor: pointer; color: var(--ink-soft); font-size: 12px; }
.raw pre { white-space: pre-wrap; font: 12px/1.7 ui-monospace, monospace; color: var(--ink-soft); background: rgba(255,255,255,.55); padding: 12px; border-radius: 10px; max-height: 50dvh; overflow: auto; }
.editor { display: grid; gap: 0; border-radius: 14px; overflow: hidden; background: var(--panel); border: 1px solid var(--line); }
.editor-bar { display: flex; align-items: center; justify-content: space-between; gap: 10px; padding: 10px 12px; border-bottom: 1px solid var(--line); }
.editor-bar > span { font-size: 11px; color: var(--ink-soft); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.editor-bar > div { display: flex; gap: 8px; flex-shrink: 0; }
.prose-editor {
  width: 100%; border: none; padding: 16px 14px;
  font-family: var(--font-prose); font-size: 17px; line-height: 1.85; color: var(--ink);
  background: transparent; outline: none;
  /* 全屏编辑：高度跟可视区走，键盘弹出时仍能看到光标所在行。 */
  min-height: calc(var(--vvh, 100dvh) - 230px);
}
</style>
