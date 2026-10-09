<template>
  <aside class="sidebar">
    <div class="sidebar__head">
      <span class="sidebar__brand">EinoKit</span>
      <t-button
          size="small"
          theme="primary"
          variant="outline"
          @click="emit('create')"
      >
        新会话
      </t-button>
    </div>

    <div class="sidebar__list">
      <div
          v-for="t in props.threads"
          :key="t.threadId"
          :class="{ 'thread--active': t.threadId === props.activeThreadId }"
          class="thread"
          @click="emit('select', t.threadId)"
      >
        <span class="thread__title">{{ t.title || '新会话' }}</span>
        <!-- 阻止冒泡：删除不该顺带把这条会话选中 -->
        <t-button
            class="thread__del"
            shape="square"
            size="small"
            theme="danger"
            variant="text"
            @click.stop="emit('remove', t.threadId)"
        >
          <t-icon name="delete"/>
        </t-button>
      </div>

      <div v-if="props.threads.length === 0" class="sidebar__empty">
        暂无会话
      </div>
    </div>
  </aside>
</template>

<script lang="ts" setup>
/** 会话侧边栏：新建、切换、删除。 */
import type {ThreadInfo} from '@/api/types'

const props = defineProps<{
  threads: ThreadInfo[]
  activeThreadId: string
}>()

const emit = defineEmits<{
  (e: 'select', threadId: string): void
  (e: 'remove', threadId: string): void
  (e: 'create'): void
}>()
</script>

<style scoped>
.sidebar {
  width: 260px;
  flex: 0 0 260px;
  display: flex;
  flex-direction: column;
  border-right: 1px solid var(--td-component-stroke);
  background: var(--td-bg-color-container);
}

.sidebar__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 16px;
  border-bottom: 1px solid var(--td-component-stroke);
}

.sidebar__brand {
  font-size: 16px;
  font-weight: 600;
}

.sidebar__list {
  flex: 1;
  overflow-y: auto;
  padding: 8px;
}

.thread {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 10px 12px;
  border-radius: var(--td-radius-default);
  cursor: pointer;
}

.thread:hover {
  background: var(--td-bg-color-container-hover);
}

.thread--active {
  background: var(--td-brand-color-light);
  color: var(--td-brand-color);
}

.thread__title {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 14px;
}

.thread__del {
  opacity: 0;
}

.thread:hover .thread__del {
  opacity: 1;
}

.sidebar__empty {
  padding: 24px 12px;
  text-align: center;
  color: var(--td-text-color-placeholder);
  font-size: 13px;
}
</style>
