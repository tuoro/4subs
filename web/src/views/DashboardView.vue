<template>
  <section>
    <div class="page-header">
      <h1>Dashboard</h1>
      <Button label="Run Scan" icon="pi pi-search" :loading="scanning" @click="runScan" />
    </div>

    <Card>
      <template #title>Service Status</template>
      <template #content>
        <div class="status-grid">
          <div>
            <div class="muted">Health</div>
            <Tag :value="health.status || 'unknown'" :severity="health.status === 'ok' ? 'success' : 'warn'" />
          </div>
          <div>
            <div class="muted">Version</div>
            <div>{{ health.version || '-' }}</div>
          </div>
          <div>
            <div class="muted">Mode</div>
            <div>{{ health.runtime_mode || '-' }}</div>
          </div>
          <div>
            <div class="muted">Missing subtitles</div>
            <div>{{ missingCount }}</div>
          </div>
        </div>
      </template>
    </Card>

    <Card class="mt-16">
      <template #title>Recent Jobs</template>
      <template #content>
        <DataTable :value="jobs" stripedRows>
          <Column field="id" header="ID" />
          <Column field="type" header="Type" />
          <Column field="status" header="Status">
            <template #body="slotProps">
              <Tag :value="slotProps.data.status" :severity="tagSeverity(slotProps.data.status)" />
            </template>
          </Column>
          <Column field="details" header="Details" />
          <Column field="updated_at" header="Updated" />
        </DataTable>
      </template>
    </Card>

    <Card class="mt-16">
      <template #title>Media Missing Subtitles</template>
      <template #content>
        <DataTable :value="missingMedia" stripedRows>
          <Column field="title" header="Title" />
          <Column field="media_type" header="Type" />
          <Column header="Season/Episode">
            <template #body="slotProps">
              {{ seasonEpisode(slotProps.data) }}
            </template>
          </Column>
          <Column field="file_path" header="Path" />
          <Column header="Actions">
            <template #body="slotProps">
              <Button
                size="small"
                label="Search"
                icon="pi pi-cloud-download"
                :loading="searchingMediaId === slotProps.data.id"
                @click="searchCandidates(slotProps.data)"
              />
            </template>
          </Column>
        </DataTable>
      </template>
    </Card>

    <Dialog v-model:visible="candidateDialogVisible" modal :style="{ width: '70rem' }" :header="candidateDialogTitle">
      <DataTable :value="currentCandidates" stripedRows>
        <Column field="provider_name" header="Provider" />
        <Column field="title" header="Title" />
        <Column field="release_name" header="Release" />
        <Column field="language_text" header="Language" />
        <Column field="score" header="Score" />
        <Column field="details" header="Details" />
      </DataTable>
      <div v-if="Object.keys(candidateErrors).length > 0" class="mt-16">
        <h4>Provider Errors</h4>
        <div v-for="(err, provider) in candidateErrors" :key="provider" class="muted">{{ provider }}: {{ err }}</div>
      </div>
    </Dialog>

    <Toast />
  </section>
</template>

<script setup>
import { onMounted, onBeforeUnmount, ref } from 'vue'
import { useToast } from 'primevue/usetoast'
import Button from 'primevue/button'
import Card from 'primevue/card'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'
import Dialog from 'primevue/dialog'
import Toast from 'primevue/toast'

import { getHealth, getJobs, getMedia, getMediaCandidates, searchMediaSubtitles, triggerScan } from '../api'

const toast = useToast()
const health = ref({})
const jobs = ref([])
const missingMedia = ref([])
const missingCount = ref(0)
const scanning = ref(false)
const searchingMediaId = ref(null)

const candidateDialogVisible = ref(false)
const candidateDialogTitle = ref('Subtitle Candidates')
const currentCandidates = ref([])
const candidateErrors = ref({})

let eventSource

const tagSeverity = (status) => {
  if (status === 'completed') return 'success'
  if (status === 'running') return 'info'
  if (status === 'failed') return 'danger'
  return 'secondary'
}

const seasonEpisode = (row) => {
  if (row.media_type !== 'episode') return '-'
  const season = row.season ?? '?'
  const episode = row.episode ?? '?'
  return `S${String(season).padStart(2, '0')}E${String(episode).padStart(2, '0')}`
}

const loadData = async () => {
  const [healthRes, jobsRes, mediaRes] = await Promise.all([
    getHealth(),
    getJobs(),
    getMedia({ missingOnly: true, limit: 500 })
  ])
  health.value = healthRes
  jobs.value = jobsRes
  missingMedia.value = mediaRes
  missingCount.value = mediaRes.length
}

const runScan = async () => {
  scanning.value = true
  try {
    await triggerScan()
    await loadData()
  } finally {
    scanning.value = false
  }
}

const searchCandidates = async (media) => {
  searchingMediaId.value = media.id
  candidateDialogTitle.value = `Candidates: ${media.title}`
  candidateErrors.value = {}
  try {
    const result = await searchMediaSubtitles(media.id)
    currentCandidates.value = await getMediaCandidates(media.id, 100)
    candidateErrors.value = result.errors || {}
    candidateDialogVisible.value = true
    toast.add({ severity: 'success', summary: 'Search completed', detail: `Found ${result.count || 0} candidates`, life: 2500 })
  } catch (error) {
    toast.add({ severity: 'error', summary: 'Search failed', detail: error.message, life: 3000 })
  } finally {
    searchingMediaId.value = null
  }
}

onMounted(async () => {
  await loadData()
  eventSource = new EventSource('/api/v1/events')
  eventSource.onmessage = async () => {
    await loadData()
  }
})

onBeforeUnmount(() => {
  if (eventSource) {
    eventSource.close()
  }
})
</script>
