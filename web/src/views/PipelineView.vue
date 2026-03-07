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
        <p class="card-subtle">这个页面展示新架构下的标准处理链路，便于你继续推进识别、翻译和渲染模块。</p>
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
            <h3>字幕输出目录</h3>
            <p>{{ runtime.subtitle_output_dir || '未设置' }}</p>
          </div>
        </div>
      </template>
    </Card>

    <Card class="span-6">
      <template #title><h2>下一步开发建议</h2></template>
      <template #content>
        <div class="tip-list">
          <div class="tip-item">
            <h3>先做任务执行器</h3>
            <p>把当前“仅创建任务”的骨架扩展成可串行执行音频提取、识别、翻译和导出的后台任务。</p>
          </div>
          <div class="tip-item">
            <h3>再接 ASR 适配层</h3>
            <p>先统一输出字幕块结构，保证时间轴与文本切分稳定，再交给翻译层处理。</p>
          </div>
          <div class="tip-item">
            <h3>最后加人工校对</h3>
            <p>加字幕预览与人工修正页面，可以显著提升成品质量和可用性。</p>
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

