<template>
  <section class="page-grid">
    <Card class="span-12">
      <template #title>
        <div class="card-title-row">
          <h2>双语字幕流水线设计</h2>
          <Button label="刷新" icon="pi pi-refresh" severity="secondary" @click="loadPipeline" :loading="loading" />
        </div>
      </template>
      <template #content>
        <p class="card-subtle">当前已跑通的链路是：扫描媒体 -> 先找字幕 -> 找不到则提音频做 ASR -> DeepSeek 翻译 -> 双语 SRT 输出。</p>
        <div class="pipeline-list">
          <div v-for="step in steps" :key="step.key" class="pipeline-item">
            <div class="card-title-row">
              <h3>{{ step.title }}</h3>
              <Tag :value="step.owner" severity="contrast" />
            </div>
            <p>{{ step.description }}</p>
          </div>
        </div>
      </template>
    </Card>

    <Card class="span-6">
      <template #title><h2>运行时状态</h2></template>
      <template #content>
        <div class="tip-list">
          <div class="tip-item">
            <h3>FFmpeg</h3>
            <p>{{ runtime.ffmpeg_bin || '未设置' }}</p>
          </div>
          <div class="tip-item">
            <h3>工作目录</h3>
            <p>{{ runtime.work_dir || '未设置' }}</p>
          </div>
          <div class="tip-item">
            <h3>输出目录</h3>
            <p>{{ runtime.subtitle_output_dir || '未设置' }}</p>
          </div>
          <div class="tip-item">
            <h3>翻译提供方</h3>
            <p>{{ runtime.translation_provider || '未设置' }}</p>
          </div>
          <div class="tip-item">
            <h3>ASR</h3>
            <p>{{ runtime.asr_provider || '未设置' }} / {{ runtime.asr_model || '未设置' }}</p>
          </div>
        </div>
      </template>
    </Card>

    <Card class="span-6">
      <template #title><h2>当前限制</h2></template>
      <template #content>
        <div class="tip-list">
          <div class="tip-item">
            <h3>已支持</h3>
            <p>同名外挂字幕、内嵌文本字幕轨、以及找不到字幕时的远程 ASR 转写。</p>
          </div>
          <div class="tip-item">
            <h3>暂未支持</h3>
            <p>图片字幕 OCR、ASS 样式导出、人工逐条校对和任务取消。</p>
          </div>
          <div class="tip-item">
            <h3>下一步</h3>
            <p>后续最值得补的是预览校对和批量任务并发控制，这样生产可用性会明显提高。</p>
          </div>
        </div>
      </template>
    </Card>
  </section>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import Button from 'primevue/button'
import Card from 'primevue/card'
import Tag from 'primevue/tag'
import { getPipeline } from '../api'

const steps = ref([])
const runtime = ref({})
const loading = ref(false)

async function loadPipeline() {
  try {
    loading.value = true
    const payload = await getPipeline()
    steps.value = payload.steps || []
    runtime.value = payload.runtime || {}
  } finally {
    loading.value = false
  }
}

onMounted(loadPipeline)
</script>
