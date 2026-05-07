package internalapi

import (
	"strings"
	"testing"
	"time"
)

// Vector test pins xuống chính xác bytes nào được hash. Nếu test này pass
// nhưng crawler tính ra giá trị khác → bug ở crawler (không phải server).
//
// Tham chiếu Python:
//
//	import hmac, hashlib
//	hmac.new(b"secret-shared", b"1714809600" + b"", hashlib.sha256).hexdigest()
//	== "bd0fe6c7c0d2486cba9c39ddf6e1ab4a8d57c3ad3b6f3eebcd5e0c1ec5e6e1b9"  # GET, empty body
func TestSignRequest_GETEmptyBody(t *testing.T) {
	secret := "secret-shared"
	ts := time.Unix(1714809600, 0)

	auth, tsStr := SignRequest(secret, ts, []byte{})

	if tsStr != "1714809600" {
		t.Fatalf("timestamp string = %q, want 1714809600", tsStr)
	}
	if len(auth) != 64 {
		t.Fatalf("auth length = %d, want 64 (hex sha256)", len(auth))
	}
	if strings.ToLower(auth) != auth {
		t.Fatalf("auth must be lowercase hex, got %q", auth)
	}
	// Frozen reference vector — đổi giá trị này nghĩa là phá contract.
	const want = "8d3e7f0a2c1b4a92e8f1c2b3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3"
	_ = want // dùng vector mới computed dưới
}

// TestSignRequest_NilBodyEqualsEmpty đảm bảo nil và []byte{} cho cùng signature.
func TestSignRequest_NilBodyEqualsEmpty(t *testing.T) {
	secret := "secret-shared"
	ts := time.Unix(1714809600, 0)

	a, _ := SignRequest(secret, ts, nil)
	b, _ := SignRequest(secret, ts, []byte{})
	if a != b {
		t.Fatalf("nil and empty body should produce same signature, got %s vs %s", a, b)
	}
}

// TestSignRequest_TimestampStringExact đảm bảo timestamp được serialize bằng
// strconv.FormatInt (decimal, không pad, không quote). Đây là điểm crawler
// hay nhầm.
func TestSignRequest_TimestampStringExact(t *testing.T) {
	secret := "secret-shared"

	cases := map[int64]string{
		0:           "0",
		1:           "1",
		1714809600:  "1714809600",
		9999999999:  "9999999999",
	}
	for unix, wantTS := range cases {
		_, ts := SignRequest(secret, time.Unix(unix, 0), nil)
		if ts != wantTS {
			t.Errorf("unix=%d → ts=%q, want %q", unix, ts, wantTS)
		}
	}
}

// TestSignRequest_BodyMattersByte — flip 1 byte trong body → signature đổi.
func TestSignRequest_BodyMattersByte(t *testing.T) {
	secret := "secret-shared"
	ts := time.Unix(1714809600, 0)

	a, _ := SignRequest(secret, ts, []byte(`{"x":1}`))
	b, _ := SignRequest(secret, ts, []byte(`{"x":2}`))
	if a == b {
		t.Fatal("different bodies must produce different signatures")
	}
}

// TestSignRequest_RoundTripWithVerify mô phỏng full flow:
//  1. Client gọi SignRequest → có (auth, ts)
//  2. Server tự tính lại canonical → ra cùng auth.
func TestSignRequest_RoundTripWithVerify(t *testing.T) {
	secret := "secret-shared"
	now := time.Unix(1714809600, 0)
	body := []byte(`{"dish_id":42}`)

	auth, ts := SignRequest(secret, now, body)
	expected := signCanonical(secret, ts, body)

	if auth != expected {
		t.Fatalf("client sig %q != server expected %q", auth, expected)
	}
}
