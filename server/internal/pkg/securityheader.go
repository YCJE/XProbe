package pkg

import "github.com/gin-gonic/gin"

// SecurityHeaders 为全部响应附加安全响应头(设计文档 7.7)。
// CSP: script 无 inline(打包产物); style 保留 'unsafe-inline'(Tailwind/ECharts 运行时样式);
// img 允许 https(分享页 logo_url 白名单已在输入侧校验)。
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		h.Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' https: data:; style-src 'self' 'unsafe-inline'; "+
				"script-src 'self'; connect-src 'self'; frame-ancestors 'none'")
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Next()
	}
}
