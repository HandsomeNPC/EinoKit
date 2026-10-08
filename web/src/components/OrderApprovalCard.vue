<script lang="ts" setup>
/**
 * 下单审批卡片(Human-in-the-Loop)。
 *
 * 注册为 `order_milk_tea` 工具的渲染组件(见 demo.vue 的 useAgentToolcall)，
 * 由 ToolCallRenderer 按 toolCallName 匹配后渲染，props 也由它注入：
 *   - args   —— 工具入参(后端 TOOL_CALL_ARGS 的 JSON，已 parse)
 *   - status —— idle/executing/complete/error
 *   - result —— 工具结果(后端 TOOL_CALL_RESULT 的 JSON，已 parse)
 *   - respond—— 交互式工具的回调，把用户决定交回上层
 *
 * 不配 `handler` 即为交互式工具：ToolCallRenderer 不会自动执行，
 * 而是等 respond 被调用。
 */
import {computed} from 'vue'

/** order_milk_tea 的入参，对齐后端 orderMilkTeaArgs。 */
interface OrderArgs {
  flavor?: string
  size?: string
  sugar?: string
  count?: number
}

/** order_milk_tea 的返回值，对齐后端 orderMilkTeaResult。 */
interface OrderResult {
  cancelled?: boolean
  message?: string
  orderId?: string
  flavor?: string
  size?: string
  sugar?: string
  count?: number
  /** 金额，单位：分。 */
  amount?: number
  eta?: string
}

/**
 * props 由 ToolCallRenderer 注入。
 *
 * status / args 声明为必传(非可选)：AgentToolcallConfig.component 要求组件满足
 * ToolcallComponentProps，那里这两个是必传的，写成可选会类型不兼容。
 * 运行时 args 一定有值(渲染器 JSON.parse 失败也会兜成 {})，但字段本身可能缺，
 * 所以下面取值仍逐个兜默认。
 */
const props = defineProps<{
  status: 'idle' | 'executing' | 'complete' | 'error'
  args: OrderArgs
  /**
   * 两种来源：
   *   - boolean —— 用户点了批准/取消后，ToolCallRenderer 回存的决定值；
   *   - OrderResult —— 后端 TOOL_CALL_RESULT 的订单详情。
   */
  result?: boolean | OrderResult
  error?: Error
  respond?: (approved: boolean) => void
}>()

/** 后端金额以分为单位。 */
const amountText = computed(() => {
  const cents = (props.args?.count ?? 1) * 1800
  return (cents / 100).toFixed(2)
})

const summary = computed(() => {
  const a = props.args ?? {}
  return `${a.count ?? 1} 杯${a.flavor ?? '奶茶'}（${a.size ?? '中杯'} / ${a.sugar ?? '五分糖'}）`
})

/**
 * 用户已经做出选择。
 *
 * ToolCallRenderer 在 respond 被调用后，会把"回传值"原样存进 result——
 * 对本卡片而言就是 true/false 这个布尔决定，不是订单详情。
 */
const decided = computed(() => typeof props.result === 'boolean')
const approvedByUser = computed(() => props.result === true)

/**
 * 后端返回的订单详情。
 *
 * 只有拿到对象形态的 result 才算数。注意恢复审批是一个新的 run，引擎会在
 * RUN_STARTED 时 reset adapter(清掉 toolCallMap)，导致本次 TOOL_CALL_RESULT
 * 关联不回这个块——所以这里通常取不到值，订单详情由模型的复述文本给出，
 * 卡片只显示"已提交"。等引擎修复后此分支会自然生效。
 */
const order = computed(() =>
    props.result && typeof props.result === 'object' ? props.result : null,
)
</script>

<template>
  <t-card bordered class="order-card" size="small">
    <template v-if="props.error">
      <div class="order-card__error">下单失败：{{ props.error.message }}</div>
    </template>

    <!-- 后端已回订单详情 -->
    <template v-else-if="order">
      <template v-if="order.cancelled">
        <div class="order-card__title order-card__title--muted">已取消下单</div>
        <div class="order-card__line">{{ order.message || '订单未创建。' }}</div>
      </template>
      <template v-else>
        <div class="order-card__title order-card__title--ok">✓ 下单成功</div>
        <t-space direction="vertical" size="small">
          <div class="order-card__line">订单号：{{ order.orderId }}</div>
          <div class="order-card__line">
            金额：¥{{ ((order.amount ?? 0) / 100).toFixed(2) }}
          </div>
          <div class="order-card__line">预计送达：{{ order.eta }}</div>
        </t-space>
      </template>
    </template>

    <!-- 用户已选择，但订单详情还没回来(或关联不回本块)：只回执决定 -->
    <template v-else-if="decided">
      <div
          :class="approvedByUser ? 'order-card__title--ok' : 'order-card__title--muted'"
          class="order-card__title"
      >
        {{ approvedByUser ? '✓ 已确认下单' : '已取消下单' }}
      </div>
      <div class="order-card__line">{{ summary }}</div>
    </template>

    <!-- 待确认：等用户点批准/取消 -->
    <template v-else>
      <div class="order-card__title">请确认订单</div>
      <div class="order-card__line">{{ summary }}</div>
      <div class="order-card__amount">合计 ¥{{ amountText }}</div>
      <t-space size="small">
        <t-button theme="primary" @click="props.respond?.(true)">批准</t-button>
        <t-button theme="default" variant="outline" @click="props.respond?.(false)">
          取消
        </t-button>
      </t-space>
    </template>
  </t-card>
</template>

<style scoped>
.order-card {
  margin-top: 8px;
  max-width: 420px;
}

.order-card__title {
  font-size: 14px;
  font-weight: 600;
  margin-bottom: 8px;
}

.order-card__title--ok {
  color: var(--td-success-color);
}

.order-card__title--muted {
  color: var(--td-text-color-secondary);
}

.order-card__line {
  font-size: 12px;
  color: var(--td-text-color-secondary);
}

.order-card__amount {
  font-size: 13px;
  font-weight: 600;
  margin: 8px 0 12px;
}

.order-card__error {
  color: var(--td-error-color);
  font-size: 13px;
}
</style>
