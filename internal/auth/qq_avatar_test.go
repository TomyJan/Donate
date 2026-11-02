package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

// Removing the comparison measurements must fail this test: a mismatch alone
// cannot distinguish provider image variants from resizing/threshold failures.
func TestQQAvatarComparisonReportsMeasurementsWithoutIdentity(t *testing.T) {
	for _, match := range []bool{true, false} {
		t.Run(map[bool]string{true: "matching", false: "mismatching"}[match], func(t *testing.T) {
			var logs bytes.Buffer
			s := &Service{Log: slog.New(slog.NewJSONHandler(&logs, nil))}
			s.HTTP = &http.Client{Transport: qqTransport(func(r *http.Request) (*http.Response, error) {
				size, shade := 100, uint8(255)
				if r.URL.Host == "thirdqq.qlogo.cn" {
					size = 40
				} else if !match {
					shade = 0
				}
				im := image.NewRGBA(image.Rect(0, 0, size, size))
				for y := range size {
					for x := range size {
						im.SetRGBA(x, y, color.RGBA{shade, shade, shade, 255})
					}
				}
				var body bytes.Buffer
				if err := png.Encode(&body, im); err != nil {
					t.Fatal(err)
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(&body)}, nil
			})}
			err := s.checkQQAvatar(context.Background(), "https://thirdqq.qlogo.cn/g?b=oidb&k=private-avatar-key&s=40", "123456789")
			if match && err != nil || !match && !errors.Is(err, ErrInvalid) {
				t.Fatalf("match=%v: %v", match, err)
			}
			var entry map[string]any
			if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &entry); err != nil {
				t.Fatalf("comparison measurement log missing: %v", err)
			}
			wantDistance := float64(1)
			if match {
				wantDistance = 0
			}
			for key, want := range map[string]any{
				"stage": "qq_avatar_compare", "result": "compared", "image_match": match,
				"image_distance": wantDistance, "image_threshold": float64(.05),
				"oauth_avatar_width": float64(40), "oauth_avatar_height": float64(40),
				"number_avatar_width": float64(100), "number_avatar_height": float64(100),
			} {
				if entry[key] != want {
					t.Errorf("%s=%v, want %v", key, entry[key], want)
				}
			}
			for _, private := range []string{"123456789", "private-avatar-key", "https://", "qlogo.cn", "oidb"} {
				if strings.Contains(logs.String(), private) {
					t.Errorf("comparison diagnostics exposed %q", private)
				}
			}
		})
	}
}
