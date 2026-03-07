<template>
  <section class="page-grid">
    <Card class="span-12">
      <template #title>
        <div class="card-title-row">
          <h2>项目设置</h2>
          <Button label="保存设置" icon="pi pi-save" @click="handleSave" :loading="saving" />
        </div>
      </template>
      <template #content>
        <Message v-if="message" severity="success" :closable="false">{{ message }}</Message>
        <Message v-if="errorMessage" severity="error" :closable="false">{{ errorMessage }}</Message>

        <div class="form-grid">
          <div class="field-group full">
            <label class="field-label">媒体目录</label>
            <textarea v-model="mediaPathsText" class="field-textarea" placeholder="每行一个目录，例如&#10;/media/movies&#10;/media/tv"></textarea>
          </div>

          <div class="field-group">
            <label class="field-label">源语言</label>
            <input v-model="form.source_language" class="field-input" placeholder="auto" />
          </div>

          <div class="field-group">
            <label class="field-label">目标语言</label>
            <input v-model="form.target_language" class="field-input" placeholder="zh-CN" />
          </div>

          <div class="field-group">
            <label class="field-label">双语布局</label>
            <input v-model="form.bilingual_layout" class="field-input" placeholder="origin_above" />
          </div>

          <div class="field-group">
            <label class="field-label">输出格式</label>
            <input v-model="outputFormatsText" class="field-input" placeholder="当前仅支持 srt" />
          </div>

          <div class="field-group">
            <label class="field-label">翻译提供方</label>
            <input v-model="form.translation_provider" class="field-input" placeholder="deepseek" />
          </div>

          <div class="field-group">
            <label class="field-label">翻译模型</label>
            <input v-model="form.translation_model" class="field-input" placeholder="deepseek-chat" />
          </div>

          <div class="field-group full">
            <label class="field-label">翻译提示词</label>
            <textarea v-model="form.translation_prompt" class="field-textarea" placeholder="请输入翻译提示词"></textarea>
          </div>

          <div class="field-group">
            <label class="field-label">单批次字幕条数</label>
            <input v-model.number="form.max_subtitle_per_batch" type="number" class="field-input" min="1" />
          </div>
        </div>
      </template>
    </Card>
  </section>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import Button from 'primevue/button'
import Card from 'primevue/card'
import Message from 'primevue/message'
import { getSettings, saveSettings } from '../api'

const form = reactive({
  source_language: 'auto',
  target_language: 'zh-CN',
  bilingual_layout: 'origin_above',
  translation_provider: 'deepseek',
  translation_model: 'deepseek-chat',
  translation_prompt: '',
  max_subtitle_per_batch: 20
})

const mediaPathsText = ref('')
const outputFormatsText = ref('srt')
const saving = ref(false)
const message = ref('')
const errorMessage = ref('')

async function loadSettings() {
  try {
    errorMessage.value = ''
    const payload = await getSettings()
    mediaPathsText.value = (payload.media_paths || []).join('\n')
    outputFormatsText.value = (payload.output_formats || []).join(',')
    Object.assign(form, payload)
  } catch (error) {
    errorMessage.value = error.message
  }
}

async function handleSave() {
  try {
    saving.value = true
    message.value = ''
    errorMessage.value = ''
    const payload = {
      ...form,
      media_paths: mediaPathsText.value.split(/\r?\n/).map((item) => item.trim()).filter(Boolean),
      output_formats: outputFormatsText.value.split(',').map((item) => item.trim()).filter((item) => item === 'srt')
    }
    const saved = await saveSettings(payload)
    mediaPathsText.value = (saved.media_paths || []).join('\n')
    outputFormatsText.value = (saved.output_formats || []).join(',')
    Object.assign(form, saved)
    message.value = '设置已保存'
  } catch (error) {
    errorMessage.value = error.message
  } finally {
    saving.value = false
  }
}

onMounted(loadSettings)
</script>
