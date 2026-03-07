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
            <Button v-if="job?.status === 'failed'" label="重试任务" severity="danger" @click="handleRetry" />
            <a v-if="job?.output_subtitle_path" :href="getJobDownloadURL(job.id)" class="nav-link">下载 SRT</a>
            <Button v-if="outputPreview.editable && outputPreview.exists" label="保存修改" icon="pi pi-save" @click="handleSave" :loading="saving" />
          </div>
        </div>
      </template>
      <template #content>
        <Message v-if="message" severity="success" :closable="false">{{ message }}</Message>
        <Message v-if="errorMessage" severity="error" :closable="false">{{ errorMessage }}</Message>

        <div class="stat-grid">
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
                <h3>双语字幕校对</h3>
                <Tag :value="outputPreview.exists ? '可编辑' : '待生成'" :severity="outputPreview.exists ? 'info' : 'warn'" />
              </div>
            </template>
            <template #content>
              <textarea v-model="editableOutput" class="field-textarea preview-textarea" :readonly="!outputPreview.editable || !outputPreview.exists" placeholder="双语字幕生成后可在这里人工修订"></textarea>
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
            <p>输出字幕：{{ outputPreview.path || '暂无' }}</p>
          </div>
        </div>
      </template>
    </Card>
  </section>
</template>

<script setup>
import { onMounted, onUnmounted, ref } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import Button from 'primevue/button'
import Card from 'primevue/card'
import Message from 'primevue/message'
import Tag from 'primevue/tag'
import { getJob, getJobDownloadURL, getJobPreview, retryJob, saveJobPreview } from '../api'

const route = useRoute()
const job = ref(null)
const sourcePreview = ref({ exists: false, content: '', path: '', editable: false })
const outputPreview = ref({ exists: false, content: '', path: '', editable: false })
const editableOutput = ref('')
const loading = ref(false)
const saving = ref(false)
const errorMessage = ref('')
const message = ref('')
let timer = null

async function loadAll() {
  try {
    loading.value = true
    errorMessage.value = ''
    message.value = ''
    const jobId = route.params.id
    job.value = await getJob(jobId)
    sourcePreview.value = await getJobPreview(jobId, 'source')
    outputPreview.value = await getJobPreview(jobId, 'output')
    editableOutput.value = outputPreview.value.content || ''
  } catch (error) {
    errorMessage.value = error.message
  } finally {
    loading.value = false
  }
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

async function handleSave() {
  try {
    saving.value = true
    errorMessage.value = ''
    message.value = ''
    outputPreview.value = await saveJobPreview(route.params.id, editableOutput.value)
    editableOutput.value = outputPreview.value.content || ''
    job.value = await getJob(route.params.id)
    message.value = '字幕修改已保存'
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
