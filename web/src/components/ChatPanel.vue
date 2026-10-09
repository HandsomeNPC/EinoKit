<template>
  <div class="layout">
    <ThreadSidebar
        :active-thread-id="activeThreadId"
        :threads="threads"
        @create="newThread"
        @remove="removeThread"
        @select="selectThread"
    />

    <main class="panel">
      <header class="panel__head">
        <span class="panel__title">{{ activeThread?.title || '新会话' }}</span>
      </header>

      <div class="panel__body">
        <t-chat-list ref="listRef" :clear-history="false">
          <t-chat-message
              v-for="message in (messages as ChatMessagesData[])"
              :key="message.id"
              :datetime="formatTime(message.datetime)"
              :message="message"
              :placement="messageProps[message.role]?.placement"
              :variant="messageProps[message.role]?.variant"
              allow-content-segment-custom
          >
            <!--
              工具调用块交给 ToolCallRenderer，按 toolCallName 匹配已注册组件。
              content 这里要断言一次：它的联合类型横跨用户消息(可能带 attachment)
              和 AI 消息，而 isToolCallContent 只收 AIMessageContent。
              工具调用只会出现在 AI 消息上，断言安全。
            -->
            <template
                v-for="(item, index) in (message.content as AIMessageContent[])"
                :key="`content-${index}`"
                #[`${item.type}-${index}`]
            >
              <div v-if="isToolCallContent(item)">
                <ToolCallRenderer
                    :tool-call="item.data"
                    @respond="onToolRespond"
                />
              </div>
            </template>
          </t-chat-message>
        </t-chat-list>
      </div>

      <footer class="panel__foot">
        <t-chat-sender
            v-model="inputValue"
            :loading="senderLoading"
            :textarea-props="{ placeholder: '说点什么，比如：我想买两杯芋泥波波' }"
            @send="handleSend"
            @stop="handleStop"
        />
      </footer>
    </main>
  </div>
</template>

<script lang="ts" setup>
import {computed, onMounted, ref} from 'vue'
import {
  type AIMessageContent,
  type ChatMessagesData,
  type ChatRequestParams,
  type TdChatMessageConfig,
  type ToolCall,
  AGUIAdapter,
  isToolCallContent,
  ToolCallRenderer,
  useAgentToolcall,
  useChat,
} from '@tdesign-vue-next/chat'
import {MessagePlugin} from 'tdesign-vue-next'

import {
  abortThread,
  CHAT_ENDPOINT,
  createThread,
  deleteThread,
  listThreads,
  renderThread,
} from '@/api'
import type {ThreadInfo} from '@/api/types'
import {toolcallActions} from './tools/register'
import ThreadSidebar from './ThreadSidebar.vue'

const threads = ref<ThreadInfo[]>([])
const activeThreadId = ref('')
const inputValue = ref('')

useAgentToolcall(toolcallActions)

const {chatEngine, messages, status} = useChat({
  defaultMessages: [],
  chatServiceConfig: {
    endpoint: CHAT_ENDPOINT,
    protocol: 'agui',
    transport: 'sse',
    onRequest: (params: ChatRequestParams) => ({
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({
        ...params,
        threadId: activeThreadId.value,
      }),
    }),

    // 断开 SSE 不会让后端的 run 停下，必须显式调 /abort。
    onAbort: async () => {
      if (!activeThreadId.value) return
      try {
        await abortThread(activeThreadId.value)
      } catch (err) {
        console.warn('[chat] 通知后端终止失败:', err)
      }
    },

    onError: (err: Error | Response) => {
      MessagePlugin.error(
          err instanceof Error ? err.message : `对话请求失败(${err.status})`,
      )
    },
  },
})

const senderLoading = computed(
    () => status.value === 'pending' || status.value === 'streaming',
)

const messageProps: TdChatMessageConfig = {
  user: {variant: 'base', placement: 'right'},
  assistant: {placement: 'left'},
}

const activeThread = computed(() =>
    threads.value.find((t: ThreadInfo) => t.threadId === activeThreadId.value),
)

/**
 * 工具卡片的 respond 回调。
 */
async function onToolRespond(_toolCall: ToolCall, response: unknown) {
  try {
    await chatEngine.value?.resumeRun({
      resume: [
        response === false
            ? {status: 'cancelled'}
            : {status: 'resolved', payload: response},
      ],
    } as ChatRequestParams)
  } catch (err) {
    MessagePlugin.error(err instanceof Error && err.message ? err.message : '恢复执行失败')
  }
}

async function refreshThreads() {
  try {
    threads.value = await listThreads()
  } catch (err) {
    MessagePlugin.error(err instanceof Error && err.message ? err.message : '加载会话列表失败')
  }
}

/** 切换会话：用 /render 的历史替换消息列表。 */
async function selectThread(threadId: string) {
  if (!threadId) return
  activeThreadId.value = threadId

  try {
    const data = await renderThread(threadId)
    // 切会话期间用户可能又点了别的会话，过期响应直接丢掉。
    if (activeThreadId.value !== threadId) return
    chatEngine.value?.setMessages(
        AGUIAdapter.convertHistoryMessages(data.messages as never),
        'replace',
    )
  } catch (err) {
    if (activeThreadId.value === threadId) {
      MessagePlugin.error(err instanceof Error && err.message ? err.message : '加载会话历史失败')
    }
  }
}

async function newThread() {
  try {
    const info = await createThread()
    threads.value = [info, ...threads.value]
    chatEngine.value?.clearMessages()
    await selectThread(info.threadId)
  } catch (err) {
    MessagePlugin.error(err instanceof Error && err.message ? err.message : '创建会话失败')
  }
}

async function removeThread(threadId: string) {
  try {
    await deleteThread(threadId)
    threads.value = threads.value.filter(
        (t: ThreadInfo) => t.threadId === threadId,
    )
    if (activeThreadId.value !== threadId) return

    activeThreadId.value = ''
    chatEngine.value?.clearMessages()
    const next = threads.value[0]
    if (next) await selectThread(next.threadId)
  } catch (err) {
    MessagePlugin.error(err instanceof Error && err.message ? err.message : '删除会话失败')
  }
}

async function handleSend(prompt: string) {
  const text = prompt.trim()
  if (!text) return

  if (!activeThreadId.value) {
    await newThread()
    if (!activeThreadId.value) return
  }

  inputValue.value = ''
  try {
    await chatEngine.value?.sendUserMessage({prompt: text})
  } catch (err) {
    MessagePlugin.error(err instanceof Error && err.message ? err.message : '发送失败')
  }
}

function handleStop() {
  chatEngine.value?.abortChat()
}

/**
 * 格式化气泡时间。引擎给的 datetime 两种形态都不能直接显示：
 * 用户消息是毫秒时间戳字符串，AI 消息是 ISO 串。
 */
function formatTime(datetime?: string): string {
  if (!datetime) return ''
  const ms = /^\d+$/.test(datetime) ? Number(datetime) : Date.parse(datetime)
  if (Number.isNaN(ms)) return ''
  return new Date(ms).toLocaleTimeString('zh-CN', {
    hour: '2-digit',
    minute: '2-digit',
  })
}

onMounted(async () => {
  await refreshThreads()
  const first = threads.value[0]
  if (first) await selectThread(first.threadId)
  else await newThread()
})
</script>

<style scoped>
.layout {
  display: flex;
  height: 100vh;
  overflow: hidden;
}

.panel {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
  background: var(--td-bg-color-page);
}

.panel__head {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 16px 24px;
  border-bottom: 1px solid var(--td-component-stroke);
  background: var(--td-bg-color-container);
}

.panel__title {
  font-size: 15px;
  font-weight: 600;
}

/* min-height:0 —— 否则 flex 子项不收缩，长列表会把输入框顶出视口。 */
.panel__body {
  flex: 1;
  min-height: 0;
  overflow: hidden;
  padding: 16px;
}

.panel__foot {
  padding: 12px 16px 16px;
  border-top: 1px solid var(--td-component-stroke);
  background: var(--td-bg-color-container);
}
</style>
