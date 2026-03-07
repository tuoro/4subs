<template>
  <section class="page-grid">
    <div class="span-12">
      <Message v-if="errorMessage" severity="error" :closable="false">{{ errorMessage }}</Message>
      <Message v-else severity="info" :closable="false">
        当前版本已经完成旧模块清理，正在围绕“本地媒体 + DeepSeek 翻译 + 双语字幕导出”重建产品骨架。
      </Message>
    </div>

    <div class="span-12">
      <div class="stat-grid">
        <div class="stat-card">
          <div class="label">媒体素材数</div>
          <div class="value">{{ overview?.media_asset_count ?? 0 }}</div>
        </div>
        <div class="stat-card">
          <div class="label">待处理任务</div>
          <div class="value">{{ overview?.pending_job_count ?? 0 }}</div>
        </div>
        <div class="stat-card">
          <div class="label">翻译能力状态</div>
          <div class="value">{{ overview?.translation_ready ? '已配置' : '待配置' }}</div>
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
        <p class="card-subtle">直接扫描你挂载到容器的本地视频目录，后续将在这里为每个素材创建字幕任务。</p>
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
              <Button label="创建任务" size="small" @click="handleCreateJob(slotProps.data)" />
            </template>
          </Column>
        </DataTable>
        <div class="table-note">如果媒体表为空，请先到设置页确认媒体目录，再回来执行扫描。</div>
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
        </div>
      </template>
      <template #content>
        <DataTable :value="jobs" stripedRows paginator :rows="6">
          <Column field="file_name" header="文件名" />
          <Column field="status" header="状态">
            <template #body="slotProps">
              <Tag :value="slotProps.data.status" :severity="slotProps.data.status === 'queued' ? 'warn' : 'info'" />
            </template>
          </Column>
          <Column field="current_stage" header="当前阶段" />
          <Column field="provider" header="翻译提供方" />
          <Column field="target_language" header="目标语言" />
          <Column field="details" header="说明" />
        </DataTable>
      </template>
    </Card>
  </section>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import Button from 'primevue/button'
import Card from 'primevue/card'
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Message from 'primevue/message'
import Tag from 'primevue/tag'
import { createJob, getOverview, listJobs, listMedia, scanMedia } from '../api'

const overview = ref(null)
const mediaItems = ref([])
const jobs = ref([])
const errorMessage = ref('')
const scanning = ref(false)

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
    await createJob({
      media_asset_id: item.id,
      media_path: item.file_path,
      file_name: item.relative_path,
      output_formats: ['srt']
    })
    await loadAll()
  } catch (error) {
    errorMessage.value = error.message
  }
}

function formatSize(size) {
  if (!size) {
    return '0 B'
  }
  const units = ['B', 'KB', 'MB', 'GB']
  let value = size
  let index = 0
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024
    index += 1
  }
  return `${value.toFixed(index === 0 ? 0 : 1)} ${units[index]}`
}

onMounted(loadAll)
</script>

