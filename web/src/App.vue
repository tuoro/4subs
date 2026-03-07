<template>
  <div class="app-shell">
    <header class="topbar">
      <div>
        <h1>4subs</h1>
        <p>本地媒体双语字幕生成工作台</p>
      </div>
      <nav class="nav-links">
        <RouterLink to="/" class="nav-link">总览</RouterLink>
        <RouterLink to="/pipeline" class="nav-link">流水线</RouterLink>
        <RouterLink to="/settings" class="nav-link">设置</RouterLink>
      </nav>
    </header>

    <main class="content-shell">
      <RouterView />
    </main>
  </div>
</template>

<script setup>
import { RouterLink, RouterView } from 'vue-router'
</script>

