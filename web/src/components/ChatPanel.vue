<script lang="ts" setup>
/**
 * EinoKit 对话面板。
 *
 * 工具调用(含需要审批的)统一走 useAgentToolcall + ToolCallRenderer 渲染：
 * 注册时不配 handler 即为交互式工具，渲染器把 respond 交给组件等用户操作。
 *
 * 与 TDesign demo 的唯一差异在 respond 的回传路径：demo 用 toolCallMessage
 * 走 sendAIMessage，而本项目后端是 AG-UI 原生 interrupt，恢复时要的是
 * `{resume:[{status,payload}]}`，所以走 resumeRun(见 onToolRespond)。
 * 不带 interruptId —— 它由后端从自己落库的中断快照回填。
 */
import {computed, onMounted, ref} from 'vue'
import {
  type AgentToolcallConfig,
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

import {
  abortThread,
  CHAT_ENDPOINT,
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
 * 待确认的中断，按 toolCallId 索引。
 *
 * 后端在 RUN_FINISHED 的 `outcome.interrupts[]` 里带上触发中断的 toolCallId
 * (从 ADK 的中断地址解析)，并把同样的快照落库，所以两个来源都能填这张表：
 * 本轮对话走 onComplete，切会话/刷新页面走 /render(见 selectThread)。
 *
 * 这里只用于"卡片该不该可点"的本地判断——真正的 interruptId 由后端持有，
 * resume 时不需要前端回传。整条链路与具体是哪个工具无关。
 */
const pendingInterrupts = ref(new Map<string, AGUIInterrupt>())

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
    name: 'Demo',
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
    // endpoint 是常量：threadId 走请求体，切会话不必 updateEndpoint。
    endpoint: CHAT_ENDPOINT,
    protocol: 'agui',
    transport: 'sse',

    // SSE 分支下 onRequest 返回 undefined 会被兜成 `{}`，请求体就空了，
    // 所以必须显式返回 body，且要自己序列化。
    onRequest: (params: ChatRequestParams) => {
      const {resume} = params as ChatRequestParams<{
        resume?: ChatRequestBody['resume']
      }>
      // 在发请求这一刻读 activeThreadId，所以切会话天然生效。
      const body: ChatRequestBody = resume?.length
          ? {threadId: activeThreadId.value, resume}
          : {threadId: activeThreadId.value, prompt: params.prompt ?? ''}

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
      rememberInterrupts(extractInterrupts(result))
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

/** 从 RUN_FINISHED 的 outcome 里取出全部中断。 */
function extractInterrupts(result: unknown): AGUIInterrupt[] {
  if (!result || typeof result !== 'object') return []
  const outcome = (result as {
    outcome?: { type?: string; interrupts?: AGUIInterrupt[] }
  }).outcome
  if (outcome?.type !== 'interrupt') return []
  return outcome.interrupts?.filter((it) => Boolean(it?.id)) ?? []
}

/**
 * 工具卡片的 respond 回调。
 *
 * 对任意工具通用：用 toolCallId 确认该中断仍待处理，然后把用户的回传值作为
 * resume payload 发回后端。约定 `response === false` 表示取消（审批类卡片回传
 * 布尔值），其余一律视为已解决，response 原样作为 payload，这样需要结构化回执
 * 的工具（如表单）不必改动这里。
 *
 * 不传 interruptId —— 后端用自己记录的中断快照回填（见 buildChatItem）。
 */
async function onToolRespond(toolCall: ToolCall, response: unknown) {
  const target = pendingInterrupts.value.get(toolCall.toolCallId)
  if (!target) {
    errorMessage.value = '该操作已处理或已失效，请重新提问。'
    return
  }

  pendingInterrupts.value.delete(toolCall.toolCallId)
  errorMessage.value = ''
  try {
    await chatEngine.value?.resumeRun({
      resume: [
        response === false
            ? {status: 'cancelled'}
            : {status: 'resolved', payload: response},
      ],
    } as ChatRequestParams)
  } catch (err) {
    pendingInterrupts.value.set(toolCall.toolCallId, target)
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

/** 切换会话：用 /render 的历史替换消息列表，并恢复待审批状态。 */
async function selectThread(threadId: string) {
  if (!threadId) return
  activeThreadId.value = threadId
  pendingInterrupts.value.clear()
  errorMessage.value = ''

  try {
    const data = await renderThread(threadId)
    // 切会话期间用户可能又点了别的会话，过期响应直接丢掉。
    if (activeThreadId.value !== threadId) return
    chatEngine.value?.setMessages(
        AGUIAdapter.convertHistoryMessages(data.messages as never),
        'replace',
    )
    // 恢复待审批状态：后端只在 checkpoint 仍有效时才返回 interrupts，
    // 所以这里恢复出来的卡片点下去一定能 resume（刷新页面也不例外）。
    rememberInterrupts(data.interrupts)
  } catch (err) {
    if (activeThreadId.value === threadId) {
      errorMessage.value = toMessage(err, '加载会话历史失败')
    }
  }
}

/** 按 toolCallId 收录中断，供 respond 时核对。无 toolCallId 的无法关联卡片，跳过。 */
function rememberInterrupts(interrupts: AGUIInterrupt[] | undefined) {
  for (const it of interrupts ?? []) {
    if (it?.toolCallId) pendingInterrupts.value.set(it.toolCallId, it)
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
    pendingInterrupts.value.clear()
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
  if (pendingInterrupts.value.size > 0) {
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

function toMessage(err: unknown, fallback: string): string {
  if (err instanceof Error && err.message) return err.message
  return fallback
}

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
