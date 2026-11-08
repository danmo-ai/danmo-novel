<script setup lang="ts">
/**
 * 桌面审校面板 = 共享视图模型 + 桌面布局。
 * 分组/勾选/筛选/归档都在 useReview（移动端同一份）。
 */
import { toRef } from 'vue'
import { isProseFixable, isUnfinishedFinding, VERDICT_LABEL } from '@/utils/reviewState'
import { useReview } from '@/composables/useReview'
import type { Candidate, UnitInfo } from '@/types'
import ChapterDiff from './ChapterDiff.vue'

const props = defineProps<{ selectedUnit: UnitInfo | null; busy?: boolean; readOnly?: boolean; onlyUnit?: string }>()
const emit = defineEmits<{
  revise: [unitId: string, issueIds: string[]]
  retrySplit: [unitId?: string]
  focusUnit: [unitId: string]
  rejectUnit: [unitId: string, candidateIds: string[]]
  /** 修订候选落盘：候选写正文、摘掉本次修掉的问题。 */
  accept: [c: Candidate]
  /** 丢弃这次修复：正文不变，清单原样留下。 */
  rejectCandidate: [c: Candidate]
}>()

const {
  store, groups, totalBlocking, totalFindings, reviewRuns, latestRun, rejectedRevises,
  picksOf, isPicked, toggle, issueIdsOf,
  chapterFilter, filterOf, visibleRows, canPick, selectableRows, allSelected, toggleAll, allLabel,
  pickTitle, copyFinding, archivedByCandidate,
  rulesSource, rulesSourceLabel, selectableCount, RULES_DOC_ID,
  archiveLabel, runStatusLabel, archivedAt, reviseSummary, keyOf,
} = useReview({
  selectedUnit: toRef(props, 'selectedUnit'),
  onlyUnit: toRef(props, 'onlyUnit'),
  busy: toRef(props, 'busy'),
  readOnly: toRef(props, 'readOnly'),
  onRevise: (unitId, ids) => emit('revise', unitId, ids),
  onRetrySplit: (unitId) => emit('retrySplit', unitId),
  onFocusUnit: (unitId) => emit('focusUnit', unitId),
  onRejectUnit: (unitId, ids) => emit('rejectUnit', unitId, ids),
  onAccept: (c) => emit('accept', c),
  onRejectCandidate: (c) => emit('rejectCandidate', c),
})
</script>

<template>
  <section class="review-workspace">
    <div class="view-title">
      <div>
        <p class="eyebrow">Quality</p>
        <h2>审校与定稿</h2>
        <p>审校只出问题清单，出口只有两个：勾选后「自动修复」——diff 就地展开在该单元里，确认落盘才写正文（落盘即完成这一轮修订，清单里没勾的仍留着可继续修）；或者「拒绝」——不同意这些结论，丢弃清单，之后需要时自己重新发起审校。定稿是写作页的独立步骤，不在审校面板上。</p>
      </div>
      <div class="review-totals">
        <span><strong>{{ totalBlocking }}</strong> 阻断</span>
        <span><strong>{{ totalFindings }}</strong> 总问题</span>
      </div>
    </div>

    <div class="rules-bar panel">
      <div>
        <h3>审校规则</h3>
        <ol class="rules-kinds">
          <li><strong>默认注入</strong>：{{ rulesSourceLabel }}——每次审校自动带上，不需勾选</li>
          <li><strong>本地硬门</strong>：程序正则表（不在知识库，永不进提示词），结果直接出现在下面</li>
          <li><strong>题材/技法参考</strong>：{{ selectableCount }} 篇可选，在写作页点审校时的确认窗口勾选</li>
        </ol>
        <p class="continuity-line">
          <template v-if="store.continuity?.units">
            连续性状态：<strong>{{ store.continuity.lastUnit }}</strong> 之后
            <span v-if="store.continuity.afterCh">· 已定稿到第 {{ store.continuity.afterCh }} 章</span>
            · {{ store.continuity.cast }} 人 · {{ store.continuity.openLoops }} 条未回收伏笔
          </template>
          <template v-else>
            连续性状态未建立：定稿第一个单元后自动生成，写作与审校都会读它。
          </template>
        </p>
      </div>
      <div class="rules-actions">
        <RouterLink class="ghost-link" :to="{ path: '/knowledge', query: { id: RULES_DOC_ID } }">
          编辑默认规则
        </RouterLink>
      </div>
    </div>

    <div v-if="latestRun?.status === 'failed'" class="run-error panel">
      <strong>最近一次审校失败</strong>
      <p>{{ latestRun.error }}</p>
      <small v-if="latestRun.error?.includes('截断')">输出被截断。改用分章审校：每章一次调用，不会撞输出预算。</small>
      <small v-else>已按调用预算停止，可修复后重试；正文未改变。</small>
      <button class="primary" @click="emit('retrySplit', latestRun.spec?.unitId)">
        {{ latestRun.error?.includes('截断') ? '重新审校（按章）' : '重新审校' }}
      </button>
    </div>

    <div v-if="!groups.length" class="empty-state panel">
      <h3>全书没有待处理审校问题</h3>
      <p>到写作页对某个单元点「审校」；结果会按单元分组出现在这里。清单处理完（全部修复或拒绝）后，回写作页点「定稿」回写账本。</p>
      <p v-if="selectedUnit?.hasProse" class="muted">没有待处理清单时，可重新审校一遍，或直接去写作页定稿。</p>
      <div v-if="selectedUnit?.hasProse" class="empty-actions">
        <button :disabled="props.busy || props.readOnly" @click="emit('retrySplit', selectedUnit.unitId)">
          重新审校 {{ selectedUnit.unitId }}
        </button>
      </div>
      <p v-if="archivedByCandidate.length" class="muted">已有 {{ archivedByCandidate.length }} 份历史清单，见页面底部。</p>
    </div>

    <section v-for="g in groups" :key="g.unitId" class="unit-group panel">
      <header class="unit-head">
        <div class="unit-title">
          <button class="unit-link" :title="`切换到 ${g.unitId}`" @click="emit('focusUnit', g.unitId)">{{ g.unitId }}</button>
          <span class="unit-badge" :data-severity="g.verdict === 'clean' ? 'advisory' : g.verdict">
            {{ g.verdict === 'blocking' ? `${g.blocking} 阻断` : VERDICT_LABEL[g.verdict] }}
          </span>
          <span class="muted">{{ g.rows.length }} 条 · {{ g.chapters.length }} 章 · {{ g.candidateIds.length }} 份清单</span>
        </div>
        <div class="unit-actions">
          <!-- 修复生成中/待落盘：这一组只处理当前这一步，勾选与「自动修复」先收起。 -->
          <span v-if="g.revise || g.running" class="muted">
            {{ g.running ? '正在生成修复…' : '修复待确认落盘' }}
          </span>
          <template v-else>
            <select v-if="g.chapters.length > 1" :value="filterOf(g.unitId)"
              @change="chapterFilter[g.unitId] = ($event.target as HTMLSelectElement).value === 'all' ? 'all' : Number(($event.target as HTMLSelectElement).value)">
              <option value="all">全部章节</option>
              <option v-for="ch in g.chapters" :key="ch" :value="ch">第 {{ ch }} 章</option>
            </select>
            <span class="muted">已选 {{ issueIdsOf(g).length }}/{{ selectableRows(g).length }}</span>
            <button class="ghost" :disabled="props.busy || props.readOnly"
              title="不同意这些审校结论：丢弃整份清单，正文不变。之后需要时自己重新发起审校"
              @click="emit('rejectUnit', g.unitId, g.candidateIds)">
              拒绝
            </button>
            <button class="primary" :disabled="props.busy || props.readOnly || !issueIdsOf(g).length"
              :title="issueIdsOf(g).length ? '按勾选问题自动修复，产出可 diff 的候选等你确认' : '先勾选要修的问题'"
              @click="emit('revise', g.unitId, issueIdsOf(g))">
              {{ props.busy ? '运行中…' : '自动修复' }}
            </button>
          </template>
        </div>
      </header>

      <!-- 待落盘的修订：清单区改成这一轮的 diff，不再和问题清单并排出现。 -->
      <section v-if="g.revise" class="revise-pending">
        <header class="revise-head">
          <div class="revise-title">
            <strong>本次修复 · 待确认落盘</strong>
            <span class="muted">{{ reviseSummary(g) }}</span>
          </div>
          <div class="revise-actions">
            <button class="ghost" :disabled="props.busy || props.readOnly"
              title="丢弃这次修复：正文不变，清单原样留下"
              @click="emit('rejectCandidate', g.revise)">拒绝这次修复</button>
            <button class="primary" :disabled="props.busy || props.readOnly"
              @click="emit('accept', g.revise)">确认落盘</button>
          </div>
        </header>
        <p class="revise-note">下面是相对现有正文的改动。确认无误再落盘：正文更新后这份清单归档到历史（未勾选的也不再生效），需要时按新正文重新审校。不认可这次改动就拒绝，正文不变。</p>
        <ChapterDiff :base="g.revise.baseChapters" :revised="g.revise.chapters || []" />
        <details v-if="g.addressedRows.length" class="revise-issues">
          <summary>本次针对的 {{ g.addressedRows.length }} 条问题</summary>
          <ul>
            <li v-for="f in g.addressedRows" :key="f.id">
              <span class="sev">{{ f.severity === 'blocking' ? '阻断' : '建议' }}</span>
              {{ f.title }}<span v-if="f.chapter" class="muted"> · 第 {{ f.chapter }} 章</span>
            </li>
          </ul>
        </details>
      </section>
      <section v-else-if="g.running" class="revise-pending running">
        <strong>正在按勾选生成修复…</strong>
        <p class="muted">生成期间这一组只显示这一步。完成后这里直接变成 diff，确认落盘才写正文。</p>
      </section>

      <div v-else class="finding-list">
        <!-- 全选按"当前筛选 + 可修"生效：筛到第 2 章只选第 2 章，截断标记不参与。 -->
        <div class="list-tools">
          <button class="ghost" :disabled="props.busy || props.readOnly || !selectableRows(g).length"
            :title="selectableRows(g).length ? '勾选当前筛选下所有可修问题' : '当前筛选下没有可修的问题'"
            @click="toggleAll(g)">
            {{ allLabel(g) }}
          </button>
          <span class="muted">可修 {{ selectableRows(g).length }} 条 · 已选 {{ issueIdsOf(g).length }}</span>
          <span v-if="g.unfinished" class="muted">· {{ g.unfinished }} 条审校不完整，不参与勾选</span>
          <span v-if="g.unitLevel" class="muted">· {{ g.unitLevel }} 条不属于正文，需回规划/状态层处理</span>
        </div>
        <!-- 没有结论 ≠ 没有问题：截断/分章失败的清单必须先补审，不能当成"审完了"。 -->
        <div v-if="g.verdict === 'unfinished'" class="unfinished-note">
          <strong>审校不完整</strong>
          <span class="muted">有章节的输出被预算截断或调用失败，剩下的结论覆盖不全。先重新审校该单元，再决定修还是拒绝。</span>
          <button class="ghost" :disabled="props.busy || props.readOnly" @click="emit('retrySplit', g.unitId)">
            重新审校
          </button>
        </div>
        <div v-if="rejectedRevises.get(g.unitId)" class="rejected-note">
          <strong>上一次自动修复已丢弃</strong>
          <span class="muted">{{ rejectedRevises.get(g.unitId)!.reason }}</span>
          <small>正文未改变。调整下面的勾选（比如只留最关键的一条）后再点「自动修复」。</small>
        </div>
        <article v-for="f in visibleRows(g)" :key="keyOf(f)" class="finding" :class="f.severity === 'blocking' ? 'blocking' : 'advisory'">
          <div class="finding-head">
            <label class="pick" :title="pickTitle(f)">
              <input type="checkbox" :disabled="!canPick(f)"
                :checked="isPicked(f, g.unitId)" @change="toggle(f, g.unitId)" /><span>{{ canPick(f) ? '选中' : '不勾选' }}</span>
            </label>
            <button class="copy-btn" title="复制引用" @click="copyFinding(f)">复制</button>
          </div>
          <div class="finding-meta">
            <span>{{ f.source === 'local_gate' ? '本地硬门' : 'AI 审校' }}</span>
            <span v-if="f.chapter">第 {{ f.chapter }} 章</span>
            <span v-else>整单元</span>
            <span v-if="f.lens">{{ f.lens }}</span>
            <!-- 多轮并集：命中次数越高越可能是可复现的硬伤，单轮命中的更可能是注意力偏好。 -->
            <span v-if="(f.roundsHit || 0) > 1" class="hits" :title="`多轮审校中有 ${f.roundsHit} 轮独立检出同一处，可复现性高`">×{{ f.roundsHit }}</span>
            <span class="sev" :data-severity="isUnfinishedFinding(f) ? 'unfinished' : f.severity">
              {{ isUnfinishedFinding(f) ? '不完整' : (f.severity === 'blocking' ? '阻断' : '建议') }}
            </span>
          </div>
          <h4>{{ f.title }}</h4>
          <p v-if="f.evidence">{{ f.evidence }}</p>
          <p v-if="f.suggestion" class="suggestion">建议：{{ f.suggestion }}</p>
          <!-- 没有章号的整单元结论：说清它不走「自动修复」，否则作者会以为漏了按钮。 -->
          <p v-if="!isProseFixable(f) && !isUnfinishedFinding(f)" class="suggestion">
            这条不属于任何一章正文，逐章修订改不了它：请按上面的建议回规划/状态层处理（人物状态过期 → 在该单元细纲的 state_deltas 里写显式终值）。
          </p>
        </article>
      </div>
    </section>

    <section v-if="reviewRuns.length" class="history panel">
      <h3>审校记录</h3>
      <div v-for="r in reviewRuns.slice(0, 10)" :key="r.id" class="history-row">
        <span>{{ r.kind }} · {{ r.spec?.unitId || '—' }}</span>
        <span>{{ runStatusLabel(r.status) }}</span>
        <span>调用 {{ r.callsUsed }}/{{ r.policy?.maxCalls || '—' }}</span>
      </div>
    </section>

    <details v-if="archivedByCandidate.length" class="archive panel">
      <summary><strong>历史审校清单</strong><span class="muted"> · {{ archivedByCandidate.length }} 份，只读</span></summary>
      <p class="archive-note">清单只有两个出口：勾选后自动修复，或拒绝丢弃。历史里的「已归档」= 这次修订已把它勾选的问题改掉（没勾的仍留在待处理清单里）。</p>
      <article v-for="c in archivedByCandidate" :key="c.id" class="archive-row">
        <div class="archive-head">
          <span class="archive-status" :data-status="c.status">{{ archiveLabel[c.status] || c.status }}</span>
          <span class="muted">{{ c.unitId || '—' }} · {{ archivedAt(c) }}</span>
          <span class="muted">· {{ (c.findings || []).length }} 条（阻断 {{ (c.findings || []).filter((f) => f.severity === 'blocking').length }}）</span>
        </div>
        <ul class="archive-findings">
          <li v-for="f in c.findings || []" :key="f.id" :class="{ blocking: f.severity === 'blocking' }">
            <span class="sev">{{ f.severity === 'blocking' ? '阻断' : '建议' }}</span>
            {{ f.title }}<span v-if="f.chapter" class="muted"> · 第 {{ f.chapter }} 章</span>
          </li>
        </ul>
      </article>
    </details>
  </section>
</template>

<style scoped>
.review-workspace { display: grid; gap: 18px; }
.view-title { display: flex; justify-content: space-between; gap: 20px; align-items: flex-end; }
.view-title h2 { margin: 4px 0 6px; font-family: var(--font-prose); font-size: 28px; }
.view-title p { margin: 0; color: var(--ink-soft); font-size: 13px; }
.eyebrow { color: var(--accent) !important; font-size: 11px !important; letter-spacing: .12em; text-transform: uppercase; }
.review-totals { display: flex; gap: 18px; flex-shrink: 0; font-size: 12px; color: var(--ink-soft); }
.review-totals strong { font-size: 20px; font-family: var(--font-prose); color: var(--ink); }
.unit-group { padding: 0; overflow: hidden; }
.unit-head { display: flex; justify-content: space-between; align-items: center; gap: 16px; padding: 14px 18px; border-bottom: 1px solid var(--line); background: rgba(15,107,92,.03); flex-wrap: wrap; }
.unit-title { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.unit-link { border: none; background: transparent; padding: 0; font: inherit; font-size: 15px; font-weight: 700; color: var(--accent); cursor: pointer; }
.unit-badge { font-size: 11px; padding: 2px 8px; border-radius: 999px; font-weight: 600; }
.unit-badge[data-severity='blocking'] { background: rgba(163,59,50,.12); color: var(--bad); }
.unit-badge[data-severity='advisory'] { background: rgba(15,107,92,.1); color: var(--accent); }
.unit-badge[data-severity='unfinished'] { background: rgba(176,124,28,.14); color: var(--warn, #8a5a00); }

.unit-actions { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.unit-actions select { padding: 6px 10px; border: 1px solid var(--line); border-radius: 8px; font-size: 12px; }
.unit-actions .muted { font-size: 12px; color: var(--ink-soft); }
.unit-actions .ghost { background: transparent; border: 1px solid var(--line); color: var(--ink-soft); }
.unit-actions .ghost:hover:not(:disabled) { color: var(--bad); border-color: rgba(163,59,50,.35); }
.empty-actions { display: flex; gap: 10px; justify-content: center; flex-wrap: wrap; }
.unfinished-note { display: grid; gap: 4px; margin: 12px 0 4px; padding: 10px 12px; border-radius: 10px; background: rgba(176,124,28,.08); border: 1px solid rgba(176,124,28,.25); font-size: 12px; justify-items: start; }
.unfinished-note strong { color: var(--warn, #8a5a00); font-size: 13px; }
.unfinished-note .muted { color: var(--ink-soft); line-height: 1.6; }
.unfinished-note button { margin-top: 4px; }
.rules-bar { padding: 14px 18px; display: flex; justify-content: space-between; gap: 16px; align-items: flex-start; }
.rules-bar h3 { margin: 0 0 6px; font-size: 14px; }
.rules-kinds { margin: 0; padding-left: 18px; color: var(--ink-soft); font-size: 12px; line-height: 1.75; }
.rules-kinds strong { color: var(--ink); }
.continuity-line { margin: 8px 0 0; color: var(--ink-soft); font-size: 12px; }
.rules-actions { flex-shrink: 0; display: flex; gap: 8px; flex-wrap: wrap; }
.ghost-link { font-size: 12px; color: var(--accent); text-decoration: none; padding: 6px 12px; border: 1px solid var(--line); border-radius: 8px; white-space: nowrap; }
.ghost-link:hover { border-color: var(--accent); }
/* 待落盘的修复：这一组此刻只显示这一步，diff 和落盘按钮同屏，不再和清单并排。 */
.revise-pending { display: grid; gap: 10px; padding: 14px 18px 16px; }
.revise-head { display: flex; justify-content: space-between; align-items: flex-start; gap: 12px; flex-wrap: wrap; }
.revise-title { display: grid; gap: 3px; }
.revise-title strong { font-size: 14px; color: var(--accent); }
.revise-note { margin: 0; color: var(--ink-soft); font-size: 12px; line-height: 1.7; }
.revise-actions { display: flex; gap: 8px; flex-wrap: wrap; flex-shrink: 0; }
.revise-actions .ghost { background: transparent; border: 1px solid var(--line); color: var(--ink-soft); }
.revise-actions .ghost:hover:not(:disabled) { color: var(--bad); border-color: rgba(163,59,50,.35); }
.revise-issues { border: 1px solid var(--line); border-radius: 10px; padding: 8px 12px; background: rgba(255,255,255,.5); }
.revise-issues summary { cursor: pointer; font-size: 12px; color: var(--ink-soft); }
.revise-issues ul { margin: 8px 0 0; padding-left: 18px; font-size: 12px; line-height: 1.8; color: var(--ink-soft); }
.revise-issues .sev { font-weight: 600; margin-right: 4px; }
.revise-pending.running { padding: 18px; }
.revise-pending.running strong { color: var(--accent); font-size: 13px; }
.revise-pending.running p { margin: 4px 0 0; }
.finding-list { padding: 4px 18px 12px; }
.list-tools { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; padding: 10px 0 8px; border-bottom: 1px solid var(--line); }
.list-tools .ghost { background: transparent; border: 1px solid var(--line); color: var(--ink-soft); padding: 4px 10px; font-size: 12px; }
.list-tools .ghost:hover:not(:disabled) { color: var(--accent); border-color: var(--accent); }
.list-tools .muted { font-size: 12px; color: var(--ink-soft); }
.rejected-note { display: grid; gap: 3px; margin: 12px 0 4px; padding: 10px 12px; border-radius: 10px; background: rgba(163,59,50,.06); border: 1px solid rgba(163,59,50,.18); font-size: 12px; }
.rejected-note strong { color: var(--bad); font-size: 13px; }
.rejected-note small { color: var(--ink-soft); line-height: 1.6; }
.finding { border-top: 1px solid var(--line); padding: 12px 0; }
.finding:first-child { border-top: none; }
.finding-head { display: flex; justify-content: space-between; align-items: center; gap: 10px; }
.pick { display: inline-flex; align-items: center; gap: 6px; font-size: 11px; color: var(--ink-soft); cursor: pointer; }
.copy-btn { border: none; background: transparent; color: var(--accent); font-size: 11px; cursor: pointer; padding: 0; }
.finding-meta { display: flex; gap: 8px; color: var(--ink-soft); font-size: 11px; margin-top: 4px; flex-wrap: wrap; }
.finding-meta .sev { font-weight: 600; }
.finding-meta .sev[data-severity='blocking'] { color: var(--bad); }
.finding-meta .sev[data-severity='unfinished'] { color: var(--warn, #8a5a00); }
/* 多轮并集命中次数：越高越可复现，所以给一点视觉权重。 */
.finding-meta .hits { padding: 0 6px; border-radius: 999px; background: rgba(163,59,50,.1); color: var(--bad); font-weight: 600; }
.finding h4 { margin: 6px 0; font-size: 14px; }
.finding.blocking h4 { color: var(--bad); }
.finding p { margin: 4px 0; color: var(--ink-soft); font-size: 13px; line-height: 1.7; }
.suggestion { color: var(--accent) !important; }
.empty-state { padding: 32px; text-align: center; }
.run-error { padding: 16px; border-color: rgba(163,59,50,.3); display: grid; gap: 8px; justify-items: start; }
.run-error strong { color: var(--bad); }
.run-error p { color: var(--ink-soft); font-size: 13px; margin: 0; }
.run-error small { color: var(--ink-soft); }
.history { padding: 18px; }
.history h3 { margin: 0 0 10px; font-size: 15px; }
.history-row { display: flex; justify-content: space-between; gap: 10px; border-top: 1px solid var(--line); padding: 10px 0; font-size: 12px; color: var(--ink-soft); }
.archive { padding: 18px; }
.archive summary { cursor: pointer; font-size: 15px; }
.archive-note { margin: 10px 0 0; color: var(--ink-soft); font-size: 12px; }
.archive-row { border-top: 1px solid var(--line); padding: 12px 0 4px; }
.archive-head { display: flex; align-items: center; gap: 8px; font-size: 12px; flex-wrap: wrap; }
.archive-status { font-size: 11px; padding: 2px 8px; border-radius: 999px; background: rgba(15,107,92,.1); color: var(--accent); font-weight: 600; }
.archive-status[data-status='rejected'] { background: rgba(163,59,50,.1); color: var(--bad); }
.archive-status[data-status='expired'] { background: rgba(0,0,0,.06); color: var(--ink-soft); }
.archive-findings { margin: 8px 0 0; padding-left: 18px; color: var(--ink-soft); font-size: 12px; line-height: 1.8; }
.archive-findings li.blocking .sev { color: var(--bad); }
.archive-findings .sev { font-weight: 600; margin-right: 4px; }
html.shell-mobile .view-title { flex-direction: column; align-items: flex-start; }
html.shell-mobile .review-totals { width: 100%; justify-content: space-between; gap: 12px; }
html.shell-mobile .unit-head { flex-direction: column; align-items: flex-start; padding: 14px; }
html.shell-mobile .unit-actions { width: 100%; }
html.shell-mobile .unit-actions button, html.shell-mobile .unit-actions select { flex: 1; min-height: 40px; }
html.shell-mobile .rules-bar { flex-direction: column; padding: 14px; }
html.shell-mobile .rules-actions { width: 100%; }
html.shell-mobile .rules-actions > * { flex: 1; }
html.shell-mobile .finding-head { align-items: flex-start; }
html.shell-mobile .finding p { overflow-wrap: anywhere; }
html.shell-mobile .finding h4 { overflow-wrap: anywhere; }
html.shell-mobile .revise-head { flex-direction: column; }
html.shell-mobile .revise-actions { width: 100%; }
html.shell-mobile .revise-actions button { flex: 1; }
html.shell-mobile .history-row { flex-direction: column; }
html.shell-mobile .archive-head { align-items: flex-start; }

</style>
