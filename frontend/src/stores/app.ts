import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api } from '@/api/client'
import type { BookSnapshot, BookSummary, Candidate, LLMModel, ProviderPublic, Run } from '@/types'

export const useAppStore = defineStore('app', () => {
  const books = ref<BookSummary[]>([])
  const currentBook = ref<BookSnapshot | null>(null)
  const primary = ref<{ kind: string; unitId?: string; volume: number } | null>(null)
  const candidates = ref<Candidate[]>([])
  /** 已终结候选（accepted/rejected/expired）：接受归档后的审校清单靠它回看。 */
  const candidateHistory = ref<Candidate[]>([])
  const runs = ref<Run[]>([])
  const activeRun = ref<Run | null>(null)
  const providers = ref<ProviderPublic[]>([])
  const models = ref<LLMModel[]>([])
  const presets = ref<any[]>([])
  const assignments = ref<{ role: string; modelId: string }[]>([])
  const toast = ref('')
  const error = ref('')
  /** 连续性状态摘要：已建到哪个单元、多少人物状态、多少未回收伏笔。 */
  const continuity = ref<{ units: number; lastUnit?: string; afterCh?: number; cast: number; openLoops: number } | null>(null)

  function note(msg: string) { toast.value = msg; setTimeout(() => { if (toast.value === msg) toast.value = '' }, 4000) }
  function fail(e: any) { error.value = e?.message || String(e); setTimeout(() => { if (error.value) error.value = '' }, 8000) }

  async function loadBooks() { books.value = (await api.get<BookSummary[] | null>('/api/books')) ?? [] }
  async function loadBook(id: string) {
    type StateSummary = { units: number; lastUnit?: string; afterCh?: number; cast: number; openLoops: number }
    const res = await api.get<{ book: BookSnapshot; primary: any; continuity?: StateSummary }>(`/api/books/${id}`)
    currentBook.value = res.book
    primary.value = res.primary
    continuity.value = res.continuity ?? null
    await Promise.all([loadCandidates(id), loadRuns(id), loadCandidateHistory(id)])
  }
  async function loadCandidates(id: string) { candidates.value = (await api.get<Candidate[] | null>(`/api/books/${id}/candidates`)) ?? [] }
  async function loadCandidateHistory(id: string) {
    candidateHistory.value = (await api.get<Candidate[] | null>(`/api/books/${id}/candidates/history`)) ?? []
  }
  async function loadRuns(id: string) { runs.value = (await api.get<Run[] | null>(`/api/books/${id}/runs`)) ?? [] }
  async function loadLLM() {
    const [p, m, pr, s] = await Promise.all([
      api.get<ProviderPublic[]>('/api/llm/configs'),
      api.get<LLMModel[]>('/api/llm/models'),
      api.get<any[]>('/api/llm/presets'),
      api.get<any>('/api/settings'),
    ])
    providers.value = p || []
    models.value = m || []
    presets.value = pr || []
    assignments.value = s.modelAssignments || []
  }
  async function startRun(spec: any) {
    const run = await api.post<Run>('/api/runs', spec)
    activeRun.value = run
    note('任务已开始：' + spec.kind)
    return run
  }
  async function pollRun(id: string) {
    const run = await api.get<Run>(`/api/runs/${id}`)
    activeRun.value = run
    return run
  }
  function clearActiveRun() { activeRun.value = null }
  async function acceptCandidate(
    id: string,
    baseRevision: string,
    opts: { forceRiskAccept?: boolean } = {},
  ) {
    const c = await api.post<Candidate>(`/api/candidates/${id}/accept`, {
      baseRevision,
      forceRiskAccept: opts.forceRiskAccept === true,
    })
    note('已接受候选')
    return c
  }
  async function rejectCandidate(id: string, reason = '') { await api.post(`/api/candidates/${id}/reject`, { reason }) }

  return {
    books, currentBook, primary, continuity, candidates, candidateHistory, runs, activeRun,
    providers, models, presets, assignments, toast, error,
    note, fail, loadBooks, loadBook, loadCandidates, loadCandidateHistory, loadRuns,
    loadLLM, startRun, pollRun, clearActiveRun, acceptCandidate, rejectCandidate,
  }
})
