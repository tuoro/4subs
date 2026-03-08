<template>
  <section class="page-grid">
    <div class="span-12">
      <Message v-if="errorMessage" severity="error" :closable="false">{{ errorMessage }}</Message>
      <Message v-else severity="info" :closable="false">
        当前版本已支持“文本字幕优先直译 → 找不到字幕时回退远程 OCR → OCR 仍失败再回退远程 ASR”，并输出双语 SRT / ASS，可继续人工校对。
      </Message>
    </div>

    <div class="span-12">
      <div class="stat-grid stat-grid-4">
        <div class="stat-card">
          <div class="label">媒体素材数</div>
          <div class="value">{{ overview?.media_asset_count ?? 0 }}</div>
        </div>
        <div class="stat-card">
          <div class="label">待处理任务</div>
          <div class="value">{{ overview?.pending_job_count ?? 0 }}</div>
        </div>
        <div class="stat-card">
          <div class="label">翻译 / OCR / ASR</div>
          <div class="value">{{ statusSummary }}</div>
        </div>
        <div class="stat-card">
          <div class="label">并发数</div>
          <div class="value">{{ concurrencySummary }}</div>
        </div>
      </div>
    </div>

    <Card class="span-8">
      <template #title>
        <div class="card-title-row">
          <h2>媒体库</h2>
          <div class="action-row">
            <Button label="刷新总览" icon="pi pi-refresh" severity="secondary" @click="loadAll" />
            <Button label="扫描媒体目录" icon="pi pi-search" @click="handleScan" :loading="scanning" />
          </div>
        </div>
      </template>
      <template #content>
        <p class="card-subtle">系统会优先提取外挂或内嵌文本字幕；没有文本字幕时先尝试远程 OCR 识别硬字幕，再回退到远程 ASR 音频转写。</p>
        <DataTable :value="mediaItems" stripedRows paginator :rows="6">
          <Column field="title" header="标题" />
          <Column field="relative_path" header="相对路径" />
          <Column field="file_size" header="大小">
            <template #body="slotProps">{{ formatSize(slotProps.data.file_size) }}</template>
          </Column>
          <Column field="status" header="状态">
            <template #body="slotProps">
              <Tag :value="slotProps.data.status" severity="success" />
            </template>
          </Column>
          <Column header="操作">
            <template #body="slotProps">
              <Button label="开始翻译" size="small" @click="handleCreateJob(slotProps.data)" />
            </template>
          </Column>
        </DataTable>
        <div class="table-note">如果 OCR 和 ASR 都未配置，那么没有外挂字幕或内嵌字幕轨的视频仍然会失败。</div>
      </template>
    </Card>

    <Card class="span-4">
      <template #title>
        <div class="card-title-row">
          <h2>处理流水线</h2>
          <RouterLink to="/pipeline" class="nav-link">查看详情</RouterLink>
        </div>
      </template>
      <template #content>
        <div class="pipeline-list">
          <div v-for="step in overview?.pipeline || []" :key="step.key" class="pipeline-item">
            <div class="card-title-row">
              <h3>{{ step.title }}</h3>
              <Tag :value="step.owner" severity="contrast" />
            </div>
            <p>{{ step.description }}</p>
          </div>
        </div>
      </template>
    </Card>

    <Card class="span-12">
      <template #title>
        <div class="card-title-row">
          <h2>最近任务</h2>
          <Button label="自动刷新" icon="pi pi-clock" severity="secondary" @click="loadJobsOnly" />
        </div>
      </template>
      <template #content>
        <DataTable :value="jobs" stripedRows paginator :rows="8">
          <Column field="file_name" header="文件名" />
          <Column field="status" header="状态">
            <template #body="slotProps">
              <Tag :value="slotProps.data.status" :severity="statusSeverity(slotProps.data.status)" />
            </template>
          </Column>
          <Column field="current_stage" header="当前阶段" />
          <Column field="progress" header="进度">
            <template #body="slotProps">{{ slotProps.data.progress }}%</template>
          </Column>
          <Column field="details" header="说明" />
          <Column field="error_message" header="错误" />
          <Column header="结果">
            <template #body="slotProps">
              <div class="action-row">
                <RouterLink :to="`/jobs/${slotProps.data.id}`" class="nav-link">详情 / 校对</RouterLink>
                <a v-if="slotProps.data.output_srt_path" :href="getJobDownloadURL(slotProps.data.id, 'srt')" class="nav-link">SRT</a>
                <a v-if="slotProps.data.output_ass_path" :href="getJobDownloadURL(slotProps.data.id, 'ass')" class="nav-link">ASS</a>
                <Button v-if="canCancel(slotProps.data.status)" label="取消" size="small" severity="contrast" @click="handleCancel(slotProps.data.id)" />
                <Button v-else-if="slotProps.data.status === 'failed' || slotProps.data.status === 'cancelled'" label="重试" size="small" severity="danger" @click="handleRetry(slotProps.data.id)" />
              </div>
            </template>
          </Column>
        </DataTable>
      </template>
    </Card>
  </section>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import Button from 'primevue/button'
import Card from 'primevue/card'
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Message from 'primevue/message'
import Tag from 'primevue/tag'
import { cancelJob, createJob, getJobDownloadURL, getOverview, listJobs, listMedia, retryJob, scanMedia } from '../api'

const overview = ref(null)
const mediaItems = ref([])
const jobs = ref([])
const errorMessage = ref('')
const scanning = ref(false)
let timer = null

const statusSummary = computed(() => {
  if (!overview.value) {
    return '加载中'
  }
  const translation = overview.value.translation_ready ? '翻译已就绪' : '翻译待配置'
  const ocr = overview.value.ocr_ready ? 'OCR 已就绪' : 'OCR 待配置'
  const asr = overview.value.asr_ready ? 'ASR 已就绪' : 'ASR 待配置'
  return `${translation} / ${ocr} / ${asr}`
})

const concurrencySummary = computed(() => {
  if (!overview.value) {
    return '加载中'
  }
  const count = Number(overview.value.worker_concurrency || 1)
  return `${count} 路并发`
})

async function loadAll() {
  try {
    errorMessage.value = ''
    overview.value = await getOverview()
    mediaItems.value = (await listMedia()).items || []
    jobs.value = (await listJobs()).items || []
  } catch (error) {
    errorMessage.value = error.message
  }
}

async function loadJobsOnly() {
  try {
    jobs.value = (await listJobs()).items || []
    overview.value = await getOverview()
  } catch (error) {
    errorMessage.value = error.message
  }
}

async function handleScan() {
  try {
    scanning.value = true
    await scanMedia()
    await loadAll()
  } catch (error) {
    errorMessage.value = error.message
  } finally {
    scanning.value = false
  }
}

async function handleCreateJob(item) {
  try {
    errorMessage.value = ''
    await createJob({
      media_asset_id: item.id,
      media_path: item.file_path,
      file_name: item.relative_path,
      output_formats: ['srt', 'ass']
    })
    await loadJobsOnly()
  } catch (error) {
    errorMessage.value = error.message
  }
}

async function handleRetry(jobId) {
  try {
    errorMessage.value = ''
    await retryJob(jobId)
    await loadJobsOnly()
  } catch (error) {
    errorMessage.value = error.message
  }
}

async function handleCancel(jobId) {
  try {
    errorMessage.value = ''
    await cancelJob(jobId)
    await loadJobsOnly()
  } catch (error) {
    errorMessage.value = error.message
  }
}

function canCancel(status) {
  return status === 'queued' || status === 'running' || status === 'cancelling'
}

function statusSeverity(status) {
  if (status === 'completed') return 'success'
  if (status === 'failed') return 'danger'
  if (status === 'queued') return 'warn'
  if (status === 'cancelled') return 'secondary'
  if (status === 'cancelling') return 'contrast'
  return 'info'
}

function formatSize(size) {
  if (!size) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  let value = size
  let index = 0
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024
    index += 1
  }
  return `${value.toFixed(index === 0 ? 0 : 1)} ${units[index]}`
}

onMounted(async () => {
  await loadAll()
  timer = window.setInterval(loadJobsOnly, 5000)
})

onUnmounted(() => {
  if (timer) window.clearInterval(timer)
})
</script>
