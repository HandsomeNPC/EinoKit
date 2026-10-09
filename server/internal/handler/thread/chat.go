package thread

import (
	"github.com/gin-gonic/gin"
)

// Chat 会话对话。POST /threads/chat
func Chat(c *gin.Context) {
	//TODO 获取ThreadID
	//TODO 根据ThreadID查询ThreadInfo是否存在。不存在则报错
	//TODO 根据ChatRequest的Resume判断是否为中断回复
	//TODO 创建一个Emitter,并转换成ThreadContext
	//TODO 把请求转换成ChatItem
	//TODO 设置请求头
	//TODO 如果是中断回复,则从TurnLoopHolder获取TurnLoop继续回复,获取不到则新建一个
	//TODO 如果是普通对话,则从TurnLoopHolder获取TurnLoop,获取到了先调用Preempt，Preempt调用失败则调用普通的对话,如果获取不到TurnLoop则新建一个再对话
}
