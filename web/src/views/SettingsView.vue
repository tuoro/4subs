<template>
  <section>
    <div class="page-header">
      <h1>Settings</h1>
      <Button label="Save" icon="pi pi-save" :loading="saving" @click="saveAll" />
    </div>

    <Card>
      <template #title>Language Priority</template>
      <template #subtitle>Default: bilingual > zh-cn > zh-tw</template>
      <template #content>
        <div class="priority-list">
          <div v-for="(lang, index) in languagePriority" :key="lang" class="priority-item">
            <span>{{ index + 1 }}. {{ labelForLanguage(lang) }}</span>
            <div class="row-actions">
              <Button icon="pi pi-arrow-up" text rounded size="small" :disabled="index === 0" @click="moveUp(index)" />
              <Button icon="pi pi-arrow-down" text rounded size="small" :disabled="index === languagePriority.length - 1" @click="moveDown(index)" />
            </div>
          </div>
        </div>

        <div class="mt-16">
          <ToggleSwitch v-model="autoReplace" inputId="replace" disabled />
          <label for="replace" class="ml-8">Auto replace existing subtitle (disabled by design)</label>
        </div>
      </template>
    </Card>

    <Card class="mt-16">
      <template #title>Provider Credentials</template>
      <template #content>
        <DataTable :value="providers" stripedRows>
          <Column field="display_name" header="Provider" />
          <Column field="configured" header="Configured">
            <template #body="slotProps">
              <Tag :value="slotProps.data.configured ? 'yes' : 'no'" :severity="slotProps.data.configured ? 'success' : 'warn'" />
            </template>
          </Column>
          <Column field="note" header="Note" />
        </DataTable>

        <div class="provider-form-grid mt-16">
          <div>
            <h3>ASSRT</h3>
            <InputText v-model="assrtToken" placeholder="ASSRT token" class="w-full" />
            <Button label="Save ASSRT" class="mt-8" @click="saveAssrt" />
          </div>
          <div>
            <h3>OpenSubtitles.com</h3>
            <InputText v-model="osApiKey" placeholder="API Key" class="w-full" />
            <InputText v-model="osUserAgent" placeholder="User Agent" class="w-full mt-8" />
            <InputText v-model="osUsername" placeholder="Username (optional)" class="w-full mt-8" />
            <Password v-model="osPassword" placeholder="Password (optional)" :feedback="false" toggleMask class="w-full mt-8" />
            <Button label="Save OpenSubtitles" class="mt-8" @click="saveOpenSubtitles" />
          </div>
        </div>
      </template>
    </Card>

    <Toast />
  </section>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { useToast } from 'primevue/usetoast'
import Button from 'primevue/button'
import Card from 'primevue/card'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import InputText from 'primevue/inputtext'
import Password from 'primevue/password'
import Tag from 'primevue/tag'
import ToggleSwitch from 'primevue/toggleswitch'
import Toast from 'primevue/toast'

import { getProviders, getSettings, saveProviderCredential, saveSettings } from '../api'

const toast = useToast()
const saving = ref(false)

const providers = ref([])
const languagePriority = ref(['bilingual', 'zh-cn', 'zh-tw'])
const autoReplace = ref(false)
const subtitleOutputPath = ref('/app/subtitles')

const assrtToken = ref('')
const osApiKey = ref('')
const osUserAgent = ref('4subs v0.1.0')
const osUsername = ref('')
const osPassword = ref('')

const labelForLanguage = (lang) => {
  if (lang === 'bilingual') return 'Bilingual'
  if (lang === 'zh-cn') return 'Simplified Chinese'
  if (lang === 'zh-tw') return 'Traditional Chinese'
  return lang
}

const moveUp = (index) => {
  if (index <= 0) return
  const arr = [...languagePriority.value]
  ;[arr[index - 1], arr[index]] = [arr[index], arr[index - 1]]
  languagePriority.value = arr
}

const moveDown = (index) => {
  if (index >= languagePriority.value.length - 1) return
  const arr = [...languagePriority.value]
  ;[arr[index], arr[index + 1]] = [arr[index + 1], arr[index]]
  languagePriority.value = arr
}

const load = async () => {
  const [settings, providerRows] = await Promise.all([getSettings(), getProviders()])
  languagePriority.value = settings.language_priority
  autoReplace.value = settings.auto_replace_existing
  subtitleOutputPath.value = settings.subtitle_output_path
  providers.value = providerRows
}

const notify = (severity, summary, detail) => {
  toast.add({ severity, summary, detail, life: 2500 })
}

const saveAll = async () => {
  saving.value = true
  try {
    await saveSettings({
      language_priority: languagePriority.value,
      auto_replace_existing: false,
      subtitle_output_path: subtitleOutputPath.value
    })
    notify('success', 'Saved', 'Settings updated')
  } catch (error) {
    notify('error', 'Save failed', error.message)
  } finally {
    saving.value = false
  }
}

const saveAssrt = async () => {
  if (!assrtToken.value) {
    notify('warn', 'Missing token', 'Please input ASSRT token')
    return
  }
  try {
    await saveProviderCredential('assrt', { token: assrtToken.value })
    await load()
    notify('success', 'Saved', 'ASSRT credential updated')
  } catch (error) {
    notify('error', 'Save failed', error.message)
  }
}

const saveOpenSubtitles = async () => {
  if (!osApiKey.value) {
    notify('warn', 'Missing key', 'Please input OpenSubtitles API key')
    return
  }

  try {
    await saveProviderCredential('opensubtitles', {
      api_key: osApiKey.value,
      user_agent: osUserAgent.value,
      username: osUsername.value,
      password: osPassword.value
    })
    await load()
    notify('success', 'Saved', 'OpenSubtitles credential updated')
  } catch (error) {
    notify('error', 'Save failed', error.message)
  }
}

onMounted(load)
</script>
