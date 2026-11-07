<script setup lang="ts">
import SettingsView from '@/views/SettingsView.vue'

/**
 * 手机设置页复用桌面设置。
 *
 * 设置是表单集合，没有「桌面并排 / 手机分层」的取舍：提供商、阶段、模型参数
 * 都是一列到底，SettingsView 本身也已经带了窄屏规则。版本页在移动端会显示
 * 「自升级仅在桌面版可用」并且按钮禁用——这正是我们要的行为，所以不需要
 * 再写一份。真要分裂（比如加移动端专属项）再拆也不迟。
 */
</script>

<template>
  <SettingsView />
</template>
