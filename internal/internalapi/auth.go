// Package internalapi expose namespace `/api/internal/*` cho crawler service
// (xem docs/crawler_architech.md §6). Mọi endpoint xác thực bằng HMAC-SHA256
// trên header X-Crawler-Auth.
package internalapi

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// Header names dùng cho HMAC auth.
const (
	HeaderAuth      = "X-Crawler-Auth"
	HeaderTimestamp = "X-Crawler-Timestamp"
	HeaderWorker    = "X-Crawler-Worker"

	// timestampSkew: chấp nhận lệch ±5 phút giữa crawler và API service.
	timestampSkew = 5 * time.Minute

	// ctxKeyWorkerID: lưu worker_id sau khi auth pass để handler đọc lại.
	ctxKeyWorkerID = "crawlerWorkerID"
)

// HMACAuth verify chữ ký HMAC-SHA256.
//
// Canonical message (chính xác bytes nào được hash):
//
//	canonical = ASCII(trim(X-Crawler-Timestamp))   ||   raw_request_body_bytes
//	signature = lowercase_hex(HMAC_SHA256(secret, canonical))
//
// Với GET hoặc bất kỳ request không body nào, `raw_request_body_bytes` là
// EMPTY (`b""`). Không có dấu cách, dấu chấm, newline giữa timestamp và body.
//
// Ví dụ Python:
//
//	import hmac, hashlib
//	ts  = "1714809600"
//	msg = ts.encode("ascii") + body_bytes
//	sig = hmac.new(secret.encode("utf-8"), msg, hashlib.sha256).hexdigest()
func HMACAuth(secret string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if secret == "" {
				return errorJSON(c, http.StatusServiceUnavailable, "INTERNAL_AUTH_DISABLED",
					"crawler shared secret not configured", nil)
			}

			tsRaw := c.Request().Header.Get(HeaderTimestamp)
			sigRaw := c.Request().Header.Get(HeaderAuth)
			if tsRaw == "" || sigRaw == "" {
				return errorJSON(c, http.StatusUnauthorized, "MISSING_AUTH_HEADERS",
					"X-Crawler-Auth and X-Crawler-Timestamp are required", nil)
			}

			// 1. Normalize: trim whitespace once, dùng cùng giá trị cho parse + HMAC.
			tsStr := strings.TrimSpace(tsRaw)
			sig := strings.TrimSpace(sigRaw)

			// 2. Validate timestamp.
			ts, err := strconv.ParseInt(tsStr, 10, 64)
			if err != nil {
				return errorJSON(c, http.StatusUnauthorized, "INVALID_TIMESTAMP",
					"X-Crawler-Timestamp must be a unix epoch second", nil)
			}
			if drift := time.Since(time.Unix(ts, 0)); drift > timestampSkew || drift < -timestampSkew {
				return errorJSON(c, http.StatusUnauthorized, "TIMESTAMP_DRIFT",
					"timestamp differs from server clock by more than 5 minutes", nil)
			}

			// 3. Read + restore body để handler kế tiếp đọc lại bình thường.
			body, err := readAndRestoreBody(c)
			if err != nil {
				return errorJSON(c, http.StatusBadRequest, "BODY_READ_FAILED", err.Error(), nil)
			}

			// 4. Compute expected signature theo cùng quy ước với SignRequest().
			expected := signCanonical(secret, tsStr, body)

			if !hmac.Equal([]byte(expected), []byte(sig)) {
				// Log debug để dev có thể so chính xác canonical bytes mà server hash
				// so với những gì crawler hash. KHÔNG log secret và KHÔNG log body trừ
				// khi level = Debug — đề phòng leak PII trong production.
				slog.Default().Debug("crawler hmac mismatch",
					slog.String("method", c.Request().Method),
					slog.String("path", c.Request().URL.Path),
					slog.String("ts_received", tsStr),
					slog.Int("body_len", len(body)),
					slog.String("sha256_body", hex.EncodeToString(sha256Sum(body))),
					slog.String("expected_sig", expected),
					slog.String("received_sig", sig),
				)
				return errorJSON(c, http.StatusUnauthorized, "HMAC_MISMATCH",
					"signature does not match expected HMAC", nil)
			}

			c.Set(ctxKeyWorkerID, c.Request().Header.Get(HeaderWorker))
			return next(c)
		}
	}
}

// signCanonical là single-source-of-truth cho cách hash request.
// Mọi caller (middleware verify, helper SignRequest) đi qua hàm này để tránh
// drift giữa "verify path" và "sign path".
func signCanonical(secret, timestampString string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestampString))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func sha256Sum(b []byte) []byte {
	s := sha256.Sum256(b)
	return s[:]
}

// WorkerIDOf trả worker_id đã được auth middleware lưu vào context.
func WorkerIDOf(c echo.Context) string {
	if v, ok := c.Get(ctxKeyWorkerID).(string); ok {
		return v
	}
	return ""
}

// readAndRestoreBody đọc body, đóng nó, rồi gắn lại 1 reader mới
// để handler tiếp theo bind/json-decode bình thường.
//
// Cho GET request không body, Go trả về `r.Body` không-nil nhưng đọc xong là
// EMPTY ([]byte{}) — đúng giá trị crawler phải sign.
func readAndRestoreBody(c echo.Context) ([]byte, error) {
	if c.Request().Body == nil {
		return []byte{}, nil
	}
	b, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return nil, err
	}
	_ = c.Request().Body.Close()
	c.Request().Body = io.NopCloser(bytes.NewReader(b))
	if b == nil {
		b = []byte{}
	}
	return b, nil
}

// SignRequest là helper được crawler/test có thể tham chiếu để biết server
// expect gì. Trả về cặp header cần đính lên request:
//
//	auth, ts := SignRequest(secret, time.Now(), body)
//	req.Header.Set(HeaderAuth, auth)
//	req.Header.Set(HeaderTimestamp, ts)
//
// Quan trọng: `body` phải LÀ ĐÚNG bytes mà client thực sự gửi. Nếu client
// re-serialize JSON sau khi sign, hash sẽ lệch.
func SignRequest(secret string, t time.Time, body []byte) (auth, timestamp string) {
	timestamp = strconv.FormatInt(t.Unix(), 10)
	auth = signCanonical(secret, timestamp, body)
	return auth, timestamp
}

// SignBody — alias backward-compat cho dùng cũ. Mới gọi SignRequest đi cho rõ.
//
// Deprecated: use SignRequest.
func SignBody(secret string, timestamp int64, body []byte) string {
	return signCanonical(secret, strconv.FormatInt(timestamp, 10), body)
}

// errorJSON là helper trả error theo schema thống nhất ở §6.3 của doc.
func errorJSON(c echo.Context, status int, code, message string, retryAfter *int) error {
	body := echo.Map{"code": code, "message": message}
	if retryAfter != nil {
		body["retry_after_seconds"] = *retryAfter
	}
	return c.JSON(status, echo.Map{"error": body})
}

// Sentinel-style errors mà handler có thể trả lại từ DB layer.
var (
	errBadRequest = errors.New("bad request")
)
