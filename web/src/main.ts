import { createApp } from 'vue'
import TDesign from 'tdesign-vue-next'
import TDesignChat from '@tdesign-vue-next/chat'
import 'tdesign-vue-next/es/style/index.css'

import './style.css'
import App from './App.vue'

// 装上 TDesignChat 才能在模板里用 t-chat-list / t-chat-message / t-chat-sender
// 这些标签(与 TDesignChatStudy demo 的 main.js 一致)。
// chat 包自带样式副作用(es/index.mjs 内部 import 了 style)，无需额外引样式。
createApp(App).use(TDesign).use(TDesignChat).mount('#app')
