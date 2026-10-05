# memory

Agent 中需要长期存储的内容放在这里。各子包定义自己的表结构与业务操作，
DB 连接统一从 `internal/database` 取（`database.GetInstance()`），不在此另设单例。

当前包含：

- `thread/`：会话与消息的持久化存储（对标 eino-examples `mem.Store`），封装 GORM 查询与错误归一。

启动时由 `main` 在 `database.Init` 之后调用 `memory.AutoMigrate` 一次性建表。
