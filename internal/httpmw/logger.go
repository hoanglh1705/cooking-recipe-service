// Package httpmw — middleware HTTP dùng chung cho các Echo server.
package httpmw

import (
	"log/slog"
	"time"

	"github.com/labstack/echo/v4"
)

// Logger trả về middleware ghi log request/response bằng `log/slog`.
//
// Mỗi request thoát ra → 1 dòng JSON (theo handler đã set ở main):
//   method, path, status, latency, ip, ua, request_id, bytes_out, query, err
//
// Level chọn theo status code:
//   2xx/3xx → INFO   ·   4xx → WARN   ·   5xx → ERROR
//
// Để middleware ghi đúng status sau khi panic được Recover xử lý, hãy
// đăng ký middleware này TRƯỚC `middleware.Recover()` ở echo:
//
//   e.Use(middleware.RequestID())
//   e.Use(httpmw.Logger(logger))
//   e.Use(middleware.Recover())
func Logger(logger *slog.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()
			req := c.Request()
			res := c.Response()

			err := next(c)
			if err != nil {
				// Force Echo's error handler to set the response status now,
				// trước khi log — nếu không, status sẽ là 200 mặc định.
				c.Error(err)
			}

			latency := time.Since(start)
			status := res.Status

			attrs := []slog.Attr{
				slog.String("method", req.Method),
				slog.String("path", req.URL.Path),
				slog.Int("status", status),
				slog.Duration("latency", latency),
				slog.Int64("bytes_out", res.Size),
				slog.String("ip", c.RealIP()),
				slog.String("request_id", res.Header().Get(echo.HeaderXRequestID)),
			}
			if q := req.URL.RawQuery; q != "" {
				attrs = append(attrs, slog.String("query", q))
			}
			if ua := req.UserAgent(); ua != "" {
				attrs = append(attrs, slog.String("ua", ua))
			}
			if err != nil {
				attrs = append(attrs, slog.String("err", err.Error()))
			}

			level := levelFor(status)
			logger.LogAttrs(req.Context(), level, "http", attrs...)

			// Đã gọi c.Error ở trên — không trả lại err để Echo không xử lý lần 2.
			return nil
		}
	}
}

func levelFor(status int) slog.Level {
	switch {
	case status >= 500:
		return slog.LevelError
	case status >= 400:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}
