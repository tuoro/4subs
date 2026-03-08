<template>
  <section class="page-grid">
    <Card class="span-12">
      <template #title>
        <div class="card-title-row">
          <div>
            <h2>任务详情</h2>
            <p class="card-subtle">{{ job?.file_name || '正在加载任务信息...' }}</p>
          </div>
          <div class="action-row">
            <RouterLink to="/" class="nav-link">返回总览</RouterLink>
            <Button label="刷新" icon="pi pi-refresh" severity="secondary" @click="loadAll" :loading="loading" />
            <Button v-if="canCancel(job?.status)" label="取消任务" severity="contrast" @click="handleCancel" />
            <Button v-else-if="job?.status === 'failed' || job?.status === 'cancelled'" label="重试任务" severity="danger" @click="handleRetry" />
            <a v-if="job?.output_srt_path" :href="getJobDownloadURL(job.id, 'srt')" class="nav-link">下载 SRT</a>
            <a v-if="job?.output_ass_path" :href="getJobDownloadURL(job.id, 'ass')" class="nav-link">下载 ASS</a>
            <Button v-if="activeOutputPreview.editable && activeOutputPreview.exists" label="保存修改" icon="pi pi-save" @click="handleSave" :loading="saving" />
          </div>
        </div>
      </template>
      <template #content>
        <Message v-if="message" severity="success" :closable="false">{{ message }}</Message>
        <Message v-if="errorMessage" severity="error" :closable="false">{{ errorMessage }}</Message>

        <div class="stat-grid stat-grid-4">
          <div class="stat-card">
            <div class="label">状态</div>
            <div class="value small">{{ job?.status || '未知' }}</div>
          </div>
          <div class="stat-card">
            <div class="label">阶段</div>
            <div class="value small">{{ job?.current_stage || '未知' }}</div>
          </div>
          <div class="stat-card">
            <div class="label">进度</div>
            <div class="value small">{{ job?.progress ?? 0 }}%</div>
          </div>
          <div class="stat-card">
            <div class="label">输出格式</div>
            <div class="value small">{{ (job?.output_formats || []).join(' / ') || '未知' }}</div>
          </div>
        </div>

        <div class="page-grid review-grid">
          <Card class="span-6 review-card">
            <template #title>
              <div class="card-title-row">
                <h3>源字幕预览</h3>
                <Tag :value="sourcePreview.exists ? '已生成' : '未生成'" :severity="sourcePreview.exists ? 'success' : 'warn'" />
              </div>
            </template>
            <template #content>
              <textarea class="field-textarea preview-textarea" :value="sourcePreview.content" readonly placeholder="源字幕尚未生成"></textarea>
            </template>
          </Card>

          <Card class="span-6 review-card">
            <template #title>
              <div class="card-title-row">
                <h3>输出字幕校对</h3>
                <div class="action-row">
                  <Button label="SRT" size="small" :severity="activePreviewKind === 'srt' ? 'primary' : 'secondary'" @click="switchPreview('srt')" />
                  <Button label="ASS" size="small" :severity="activePreviewKind === 'ass' ? 'primary' : 'secondary'" @click="switchPreview('ass')" />
                </div>
              </div>
            </template>
            <template #content>
              <textarea v-model="editableOutput" class="field-textarea preview-textarea" :readonly="!activeOutputPreview.editable || !activeOutputPreview.exists" :placeholder="activePreviewKind === 'ass' ? 'ASS 字幕生成后可在这里人工修订' : 'SRT 字幕生成后可在这里人工修订'"></textarea>
            </template>
          </Card>
        </div>

        <div class="tip-list">
          <div class="tip-item">
            <h3>任务说明</h3>
            <p>{{ job?.details || '暂无说明' }}</p>
          </div>
          <div v-if="job?.error_message" class="tip-item">
            <h3>错误信息</h3>
            <p>{{ job.error_message }}</p>
          </div>
          <div class="tip-item">
            <h3>文件路径</h3>
            <p>源字幕：{{ sourcePreview.path || '暂无' }}</p>
            <p>SRT 输出：{{ srtPreview.path || '暂无' }}</p>
            <p>ASS 输出：{{ assPreview.path || '暂无' }}</p>
          </div>
        </div>
      </template>
    </Card>
  </section>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import Button from 'primevue/button'
import Card from 'primevue/card'
import Message from 'primevue/message'
import Tag from 'primevue/tag'
import { cancelJob, getJob, getJobDownloadURL, getJobPreview, retryJob, saveJobPreview } from '../api'

const route = useRoute()
const job = ref(null)
const sourcePreview = ref({ exists: false, content: '', path: '', editable: false })
const srtPreview = ref({ exists: false, content: '', path: '', editable: true })
const assPreview = ref({ exists: false, content: '', path: '', editable: true })
const activePreviewKind = ref('srt')
const editableOutput = ref('')
const loading = ref(false)
const saving = ref(false)
const errorMessage = ref('')
const message = ref('')
let timer = null

const activeOutputPreview = computed(() => (activePreviewKind.value === 'ass' ? assPreview.value : srtPreview.value))

async function loadAll() {
  try {
    loading.value = true
    errorMessage.value = ''
    message.value = ''
    const jobId = route.params.id
    job.value = await getJob(jobId)
    sourcePreview.value = await getJobPreview(jobId, 'source')
    srtPreview.value = await getJobPreview(jobId, 'srt')
    assPreview.value = await getJobPreview(jobId, 'ass')
    if (!srtPreview.value.exists && assPreview.value.exists) {
      activePreviewKind.value = 'ass'
    }
    syncEditableOutput()
  } catch (error) {
    errorMessage.value = error.message
  } finally {
    loading.value = false
  }
}

function syncEditableOutput() {
  editableOutput.value = activeOutputPreview.value.content || ''
}

function switchPreview(kind) {
  activePreviewKind.value = kind
  syncEditableOutput()
}

function canCancel(status) {
  return status === 'queued' || status === 'running' || status === 'cancelling'
}

async function handleRetry() {
  try {
    errorMessage.value = ''
    message.value = ''
    await retryJob(route.params.id)
    message.value = '任务已重新排队'
    await loadAll()
  } catch (error) {
    errorMessage.value = error.message
  }
}

async function handleCancel() {
  try {
    errorMessage.value = ''
    message.value = ''
    await cancelJob(route.params.id)
    message.value = '任务取消请求已发送'
    await loadAll()
  } catch (error) {
    errorMessage.value = error.message
  }
}

async function handleSave() {
  try {
    saving.value = true
    errorMessage.value = ''
    message.value = ''
    const saved = await saveJobPreview(route.params.id, activePreviewKind.value, editableOutput.value)
    if (activePreviewKind.value === 'ass') {
      assPreview.value = saved
    } else {
      srtPreview.value = saved
    }
    syncEditableOutput()
    job.value = await getJob(route.params.id)
    message.value = `${activePreviewKind.value.toUpperCase()} 字幕修改已保存`
  } catch (error) {
    errorMessage.value = error.message
  } finally {
    saving.value = false
  }
}

onMounted(async () => {
  await loadAll()
  timer = window.setInterval(loadAll, 5000)
})

onUnmounted(() => {
  if (timer) {
    window.clearInterval(timer)
  }
})
</script>
