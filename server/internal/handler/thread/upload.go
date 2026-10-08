package thread

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/HandsomeNPC/EinoKit/internal/memory/thread"
)

// maxUploadBytes 单个上传文件的大小上限。
const maxUploadBytes = 20 << 20 // 20 MiB

// workspaceRoot 是会话上传文件的根目录，按 threadID 分子目录。
const workspaceRoot = "data/workspace"

// Upload 上传文档到会话工作区。POST /threads/docs
//
// multipart 表单：threadId 为会话 ID，file 为文件内容。
// 这里 threadId 走表单而非 JSON body——multipart 请求没法再带 JSON。
// 返回的 url 可直接填进下一次 /chat 的 attachments。
func Upload(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("threadId"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "threadId 不能为空"})
		return
	}
	if _, err := thread.SelectByID(id); err != nil {
		writeThreadLookupError(c, err)
		return
	}

	fh, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少上传文件（表单字段 file）"})
		return
	}
	if fh.Size > maxUploadBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{
			"error": fmt.Sprintf("文件超过 %d MiB 上限", maxUploadBytes>>20),
		})
		return
	}

	// 只取 basename 并换成新文件名，避免 ../ 穿越与同名覆盖。
	ext := filepath.Ext(filepath.Base(fh.Filename))
	stored := uuid.New().String() + ext
	dir := filepath.Join(workspaceRoot, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[thread] 创建工作区失败 thread=%s: %v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建工作区失败"})
		return
	}
	if err := c.SaveUploadedFile(fh, filepath.Join(dir, stored)); err != nil {
		log.Printf("[thread] 保存上传文件失败 thread=%s: %v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存上传文件失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"name":      fh.Filename,
		"size":      fh.Size,
		"extension": strings.TrimPrefix(ext, "."),
		// TODO: 当前没有任何路由会 GET 这个地址（改路由前就是如此），
		// 下载能力待补：挂一个静态文件路由或对象存储直链。
		"url":        "/threads/docs/" + id + "/" + stored,
		"uploadedAt": time.Now().UTC().Format(time.RFC3339),
	})
}
