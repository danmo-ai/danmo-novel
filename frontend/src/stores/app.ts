import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api } from '@/api/client'
import type { BookSnapshot, BookSummary, Candidate, LLMModel, ProviderPublic, Run } from '@/types'

export const useAppStore = defineStore('app', () => {
  const books = ref<BookSummary[]>([])
  const currentBook = ref<BookSnapshot | null>(null)
  const primary = ref<{ kind: string; unitId?: string; volume: number } | null>(null)
  const candidates = ref<Candidate[]>([])
  const runs = ref<Run[]>([])
  const activeRun = ref<Run | null>(null)
  const providers = ref<ProviderPublic[]>([])
  const models = ref<LLMModel[]>([])
  const presets = ref<any[]>([])
  const assignments = ref<{ role: string; modelId: string }[]>([])
  const toast = ref('')
  const error = ref('')

  function note(msg: string) { toast.value = msg; setTimeout(() => { if (toast.value === msg) toast.value = '' }, 4000) }
  function fail(e: any) { error.value = e?.message || String(e); setTimeout(() => { if (error.value) error.value = '' }, 8000) }

  async function loadBooks() { books.value = (await api.get<BookSummary[] | null>('/api/books')) ?? [] }
  async function loadBook(id: string) {
    const res = await api.get<{ book: BookSnapshot; primary: any }>(`/api/books/${id}`)
    currentBook.value = res.book
    primary.value = res.primary
    await Promise.all([loadCandidates(id), loadRuns(id)])
  }
  async function loadCandidates(id: string) { candidates.value = (await api.get<Candidate[] | null>(`/api/books/${id}/candidates`)) ?? [] }
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
  async function acceptCandidate(id: string, baseRevision: string) {
    const c = await api.post<Candidate>(`/api/candidates/${id}/accept`, { baseRevision })
    note('已接受候选')
    return c
  }
  async function rejectCandidate(id: string, reason = '') { await api.post(`/api/candidates/${id}/reject`, { reason }) }

  return {
    books, currentBook, primary, candidates, runs, activeRun,
    providers, models, presets, assignments, toast, error,
    note, fail, loadBooks, loadBook, loadCandidates, loadRuns,
    loadLLM, startRun, pollRun, clearActiveRun, acceptCandidate, rejectCandidate,
  }
})
