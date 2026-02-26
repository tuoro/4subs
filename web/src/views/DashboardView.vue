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
  </section>
</template>

<script setup>
import { onMounted, onBeforeUnmount, ref } from 'vue'
import Button from 'primevue/button'
import Card from 'primevue/card'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'

import { getHealth, getJobs, triggerScan } from '../api'

const health = ref({})
const jobs = ref([])
const scanning = ref(false)
let eventSource

const tagSeverity = (status) => {
  if (status === 'completed') return 'success'
  if (status === 'running') return 'info'
  if (status === 'failed') return 'danger'
  return 'secondary'
}

const loadData = async () => {
  health.value = await getHealth()
  jobs.value = await getJobs()
}

const runScan = async () => {
  scanning.value = true
  try {
    await triggerScan()
    jobs.value = await getJobs()
  } finally {
    scanning.value = false
  }
}

onMounted(async () => {
  await loadData()
  eventSource = new EventSource('/api/v1/events')
  eventSource.onmessage = async () => {
    jobs.value = await getJobs()
  }
})

onBeforeUnmount(() => {
  if (eventSource) {
    eventSource.close()
  }
})
</script>
