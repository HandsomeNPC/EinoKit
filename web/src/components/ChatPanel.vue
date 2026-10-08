<script lang="ts" setup>
/**
 * EinoKit 对话面板。
 *
 * 结构对齐 TDesignChatStudy 的 demo：useChat 直接用，不再包中间层；
 * 工具调用(含审批)统一走 useAgentToolcall + ToolCallRenderer 渲染。
 *
 * 与 demo 的差异只有一处：后端的审批是 AG-UI 原生 interrupt
 * (RUN_FINISHED.outcome.interrupts)，不是 toolCallMessage，
 * 所以 respond 回调用 resumeRun 发 `{resume:[...]}`(见 onOrderRespond)。
 */
import {computed, onMounted, ref, watch} from 'vue'
import {
  type AgentToolcallConfig,
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

import {
  abortThread,
  chatEndpoint,
  createThread,
  deleteThread,
  listThreads,
  renderThread,
} from '@/api'
import type {AGUIInterrupt, ChatRequestBody, ThreadInfo} from '@/api/types'
import OrderApprovalCard from './OrderApprovalCard.vue'
import ThreadSidebar from './ThreadSidebar.vue'

const threads = ref<ThreadInfo[]>([])
const activeThreadId = ref('')
const errorMessage = ref('')
const inputValue = ref('')

/**
 * 当前待确认的中断。
 *
 * 后端用 AG-UI 的 interrupt 表达审批：RUN_FINISHED 带
 * `outcome:{type:'interrupt',interrupts:[...]}`。审批卡片本身由
 * order_milk_tea 的工具组件渲染，这里只存 interruptId 供 resume 时回填。
 */
const pendingInterrupt = ref<AGUIInterrupt | null>(null)

/**
 * 注册工具渲染组件。不配 handler = 交互式工具，ToolCallRenderer 不会自动执行，
 * 而是把 respond 交给组件，等用户操作。
 *
 * component 这里要断言一次：AgentToolcallConfig.component 的类型是
 * `DefineComponent<ToolcallComponentProps>`，它的 __defaults 要求 args 必填，
 * 而 `<script setup>` 推导出的组件类型里 __defaults 是 {}，两者无法直接对上
 * (库自身的类型不自洽，官方 demo 的 build 不跑 vue-tsc 所以没暴露)。
 * 组件内部的 props 仍是完整类型，断言只作用在注册这一处。
 */
const toolcallActions: AgentToolcallConfig[] = [
  {
    name: 'order_milk_tea',
    description: '下单购买奶茶，需要用户二次确认',
    parameters: [
      {name: 'flavor', type: 'string', required: true},
      {name: 'size', type: 'string', required: true},
      {name: 'sugar', type: 'string', required: true},
      {name: 'count', type: 'number', required: true},
    ],
    component: OrderApprovalCard as AgentToolcallConfig['component'],
  },
]

useAgentToolcall(toolcallActions)

const {chatEngine, messages, status} = useChat({
  defaultMessages: [],
  chatServiceConfig: {
    // endpoint 先留空：引擎只在 init 时求值一次配置，
    // 选中会话后用 updateEndpoint 注入。
    endpoint: '',
    protocol: 'agui',
    transport: 'sse',

    // SSE 分支下 onRequest 返回 undefined 会被兜成 `{}`，请求体就空了，
    // 所以必须显式返回 body，且要自己序列化。
    onRequest: (params: ChatRequestParams) => {
      const {resume} = params as ChatRequestParams<{
        resume?: ChatRequestBody['resume']
      }>
      const body: ChatRequestBody = resume?.length
          ? {resume}
          : {prompt: params.prompt ?? ''}

      return {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify(body),
      }
    },

    // 引擎把 RUN_FINISHED 原样(未经 zod 裁剪)透传到这里，
    // 所以 Go SDK 写的 outcome.interrupts 能完整读到。
    onComplete: (isAborted, _params, result) => {
      if (isAborted) return
      pendingInterrupt.value = extractInterrupt(result)
    },

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
      errorMessage.value =
          err instanceof Error ? err.message : `对话请求失败(${err.status})`
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

/** 从 RUN_FINISHED 的 outcome 里取第一个中断。 */
function extractInterrupt(result: unknown): AGUIInterrupt | null {
  if (!result || typeof result !== 'object') return null
  const outcome = (result as { outcome?: { type?: string; interrupts?: AGUIInterrupt[] } })
      .outcome
  if (outcome?.type !== 'interrupt') return null
  return outcome.interrupts?.find((it) => Boolean(it?.id)) ?? null
}

/**
 * 审批卡片的回调。
 *
 * 走 resumeRun 而不是 demo 里的 sendAIMessage：后端用 AG-UI interrupt 表达
 * 审批，恢复时要的是 `{resume:[{interruptId,status,payload}]}`，
 * 且不该再往列表里插一条用户气泡。
 */
async function onOrderRespond(_toolCall: ToolCall, approved: boolean) {
  const target = pendingInterrupt.value
  if (!target) {
    // 刷新页面后拿不到 interruptId(/render 只给布尔标记)，无法恢复。
    errorMessage.value = '审批信息已失效（页面刷新过），请重新提问。'
    return
  }

  pendingInterrupt.value = null
  errorMessage.value = ''
  try {
    await chatEngine.value?.resumeRun({
      resume: [
        approved
            ? {interruptId: target.id, status: 'resolved', payload: true}
            : {interruptId: target.id, status: 'cancelled'},
      ],
    } as ChatRequestParams)
  } catch (err) {
    pendingInterrupt.value = target
    errorMessage.value = toMessage(err, '恢复执行失败')
  }
}

async function refreshThreads() {
  try {
    threads.value = await listThreads()
  } catch (err) {
    errorMessage.value = toMessage(err, '加载会话列表失败')
  }
}

/** 切换会话：重指 endpoint + 用 /render 的历史替换消息列表。 */
async function selectThread(threadId: string) {
  if (!threadId) return
  activeThreadId.value = threadId
  pendingInterrupt.value = null
  errorMessage.value = ''
  chatEngine.value?.updateEndpoint(chatEndpoint(threadId))

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
      errorMessage.value = toMessage(err, '加载会话历史失败')
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
    errorMessage.value = toMessage(err, '创建会话失败')
  }
}

async function removeThread(threadId: string) {
  try {
    await deleteThread(threadId)
    threads.value = threads.value.filter(
        (t: ThreadInfo) => t.threadId !== threadId,
    )
    if (activeThreadId.value !== threadId) return

    activeThreadId.value = ''
    pendingInterrupt.value = null
    chatEngine.value?.clearMessages()
    const next = threads.value[0]
    if (next) await selectThread(next.threadId)
  } catch (err) {
    errorMessage.value = toMessage(err, '删除会话失败')
  }
}

async function handleSend(prompt: string) {
  const text = prompt.trim()
  if (!text) return

  // 有待审批时后端会回 409，先挡住并提示。
  if (pendingInterrupt.value) {
    errorMessage.value = '有待确认的操作，请先批准或取消后再提问。'
    return
  }
  if (!activeThreadId.value) {
    await newThread()
    if (!activeThreadId.value) return
  }

  inputValue.value = ''
  errorMessage.value = ''
  try {
    await chatEngine.value?.sendUserMessage({prompt: text})
  } catch (err) {
    errorMessage.value = toMessage(err, '发送失败')
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

/**
 * 工具调用块判定。
 *
 * message.content 的联合类型横跨用户消息(可能带 attachment)和 AI 消息，
 * 直接把元素喂给 isToolCallContent 会因入参类型太窄而报错，这里先放宽再转交。
 */
function isToolCall(item: unknown): boolean {
  return isToolCallContent(item as never)
}

function toolCallData(item: unknown): ToolCall {
  return (item as { data: ToolCall }).data
}

function toMessage(err: unknown, fallback: string): string {
  if (err instanceof Error && err.message) return err.message
  return fallback
}

/** 引擎在 onMounted 才建好，就绪后补一次 endpoint。 */
watch(chatEngine, (instance) => {
  if (instance && activeThreadId.value) {
    instance.updateEndpoint(chatEndpoint(activeThreadId.value))
  }
})

onMounted(async () => {
  await refreshThreads()
  const first = threads.value[0]
  if (first) await selectThread(first.threadId)
  else await newThread()
})
</script>

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
        <t-tag v-if="senderLoading" size="small" theme="primary" variant="light">
          生成中
        </t-tag>
      </header>

      <t-alert
          v-if="errorMessage"
          :message="errorMessage"
          class="panel__alert"
          close-btn
          theme="error"
          @close="errorMessage = ''"
      />

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
            <!-- 工具调用块交给 ToolCallRenderer，按 toolCallName 匹配已注册组件 -->
            <template
                v-for="(item, index) in message.content"
                :key="`content-${index}`"
                #[`${item.type}-${index}`]
            >
              <div v-if="isToolCall(item)">
                <ToolCallRenderer
                    :tool-call="toolCallData(item)"
                    @respond="onOrderRespond"
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

.panel__alert {
  margin: 12px 16px 0;
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
