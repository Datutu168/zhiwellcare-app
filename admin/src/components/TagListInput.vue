<script setup lang="ts">
/**
 * 可增删的标签输入（课程 tags / 商品 specs 共用）。
 *
 * - v-model 绑定 string[]；回车或失焦提交，支持粘贴多行 / 逗号分隔文本自动按行拆分；
 * - 自动去重并忽略空白项，已存在的标签不会重复添加。
 */
import { nextTick, ref } from 'vue'

const props = withDefaults(
  defineProps<{
    modelValue: string[]
    placeholder?: string
    addText?: string
  }>(),
  {
    placeholder: '输入后回车添加（支持粘贴多行）',
    addText: '＋ 添加',
  },
)

const emit = defineEmits<{ (e: 'update:modelValue', value: string[]): void }>()

const draft = ref('')
const inputVisible = ref(false)

function commitRaw(value: string): void {
  const items = [...(props.modelValue ?? [])]
  value
    .split(/[\n,，;；]+/)
    .map((part) => part.trim())
    .filter((part) => part.length > 0)
    .forEach((part) => {
      if (!items.includes(part)) items.push(part)
    })
  emit('update:modelValue', items)
}

function commit(): void {
  const value = draft.value
  draft.value = ''
  inputVisible.value = false
  if (value.trim()) commitRaw(value)
}

async function showInput(): Promise<void> {
  inputVisible.value = true
  await nextTick()
}

function removeAt(index: number): void {
  const items = [...(props.modelValue ?? [])]
  items.splice(index, 1)
  emit('update:modelValue', items)
}
</script>

<template>
  <div class="tag-editor">
    <el-tag
      v-for="(item, index) in modelValue"
      :key="`${item}-${index}`"
      closable
      size="small"
      type="primary"
      effect="plain"
      class="tag-item"
      @close="removeAt(index)"
    >
      {{ item }}
    </el-tag>
    <el-input
      v-if="inputVisible"
      v-model="draft"
      class="tag-input"
      size="small"
      :placeholder="placeholder"
      @keyup.enter="commit"
      @blur="commit"
    />
    <el-button v-else size="small" class="tag-add" @click="showInput">{{ addText }}</el-button>
  </div>
</template>

<style scoped>
.tag-editor {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  width: 100%;
}

.tag-item {
  max-width: 100%;
}

.tag-input {
  width: 220px;
}
</style>
