package pagination

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestNew(t *testing.T) {
	cases := []struct {
		name        string
		page, limit int
		want        Params
		wantErr     bool
	}{
		{"default", 0, 0, Params{1, 20, 0}, false},
		{"halaman kedua", 2, 10, Params{2, 10, 10}, false},
		{"limit tepat di batas", 1, 100, Params{1, 100, 0}, false},
		{"limit melebihi batas", 1, 101, Params{}, true},
		{"page negatif", -1, 10, Params{}, true},
		{"limit negatif", 1, -5, Params{}, true},
		{"page absurd besar", 1 << 40, 100, Params{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := New(tc.page, tc.limit, 20, 100)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalid) {
					t.Fatalf("err = %v, ingin ErrInvalid", err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got %+v, %v; ingin %+v", got, err, tc.want)
			}
		})
	}
}

func TestNewResult_NilItemsBecomeEmpty(t *testing.T) {
	r := NewResult[int](nil, 0, Params{Page: 1, Limit: 20})
	if r.Items == nil || len(r.Items) != 0 {
		t.Fatalf("items harus slice kosong non-nil, dapat %#v", r.Items)
	}
}

func TestFromQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		url         string
		page, limit int
		ok          bool
	}{
		{"/x", 0, 0, true},
		{"/x?page=3&limit=7", 3, 7, true},
		{"/x?page=abc", 0, 0, false},
		{"/x?limit=1.5", 0, 0, false},
	}
	for _, tc := range cases {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", tc.url, nil)
		page, limit, ok := FromQuery(c)
		if ok != tc.ok || (ok && (page != tc.page || limit != tc.limit)) {
			t.Errorf("%s: got %d,%d,%v", tc.url, page, limit, ok)
		}
	}
}
