package handler_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"truss-project/internal/modules/projects/domain"
)

var memberRolePath = membersPath + "/" + memberUserID.String()
var transferPath = projectPath + "/transfer-ownership"

// ---------- PATCH /projects/:id/members/:userId ----------

func TestMemberChangeRole_Success(t *testing.T) {
	uc := &fakeMemberUC{changeFn: func() (*domain.Member, error) { return sampleMember(domain.ProjectAdmin), nil }}
	w := do(newMemberRouter(uc, nil), "PATCH", memberRolePath, `{"role":"admin"}`, authAs)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if uc.gotProject != projectID.String() || uc.gotTarget != memberUserID.String() || uc.gotRole != "admin" {
		t.Errorf("argumen = %q %q %q", uc.gotProject, uc.gotTarget, uc.gotRole)
	}
	if data := decode(t, w)["data"].(map[string]any); data["role"] != "admin" || data["user_id"] != memberUserID.String() {
		t.Errorf("data = %v", data)
	}
}

func TestMemberChangeRole_BadBodyAndAuth(t *testing.T) {
	uc := &fakeMemberUC{}
	r := newMemberRouter(uc, nil)
	for _, body := range []string{``, `{}`, `{"role":""}`, `bukan json`} {
		if w := do(r, "PATCH", memberRolePath, body, authAs); w.Code != http.StatusBadRequest {
			t.Errorf("body %q: status %d", body, w.Code)
		}
	}
	if w := do(r, "PATCH", memberRolePath, `{"role":"admin"}`, ""); w.Code != http.StatusUnauthorized {
		t.Errorf("tanpa token: status %d", w.Code)
	}
	if uc.calls != 0 {
		t.Error("use case tidak boleh dipanggil")
	}
}

func TestMemberChangeRole_ErrorMapping(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want int
	}{
		"input tidak valid":      {domain.ErrInvalidInput, http.StatusBadRequest},
		"peran tidak cukup":      {domain.ErrForbidden, http.StatusForbidden},
		"project tidak terlihat": {domain.ErrProjectNotFound, http.StatusNotFound},
		"bukan anggota":          {domain.ErrMemberNotFound, http.StatusNotFound},
		"target owner":           {domain.ErrCannotChangeOwnerRole, http.StatusConflict},
		"tak terduga":            {errors.New("db mati"), http.StatusInternalServerError},
	} {
		t.Run(name, func(t *testing.T) {
			uc := &fakeMemberUC{changeFn: func() (*domain.Member, error) { return nil, tc.err }}
			w := do(newMemberRouter(uc, nil), "PATCH", memberRolePath, `{"role":"admin"}`, authAs)
			if w.Code != tc.want || strings.Contains(w.Body.String(), "db mati") {
				t.Errorf("status %d, harapannya %d: %s", w.Code, tc.want, w.Body)
			}
		})
	}
}

// ---------- POST /projects/:id/transfer-ownership ----------

func TestProjectTransferOwnership_Success(t *testing.T) {
	uc := &fakeMemberUC{transferFn: func() error { return nil }}
	w := do(newMemberRouter(uc, nil), "POST", transferPath, `{"user_id":"`+memberUserID.String()+`"}`, authAs)
	if w.Code != http.StatusOK || uc.gotProject != projectID.String() || uc.gotTarget != memberUserID.String() {
		t.Fatalf("status %d project=%q target=%q", w.Code, uc.gotProject, uc.gotTarget)
	}
}

func TestProjectTransferOwnership_BadBodyAndAuth(t *testing.T) {
	uc := &fakeMemberUC{}
	r := newMemberRouter(uc, nil)
	for _, body := range []string{``, `{}`, `{"user_id":""}`, `x`} {
		if w := do(r, "POST", transferPath, body, authAs); w.Code != http.StatusBadRequest {
			t.Errorf("body %q: status %d", body, w.Code)
		}
	}
	if w := do(r, "POST", transferPath, `{"user_id":"x"}`, ""); w.Code != http.StatusUnauthorized {
		t.Errorf("tanpa token: status %d", w.Code)
	}
	if uc.calls != 0 {
		t.Error("use case tidak boleh dipanggil")
	}
}

func TestProjectTransferOwnership_ErrorMapping(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want int
	}{
		"target sudah owner / input": {domain.ErrInvalidInput, http.StatusBadRequest},
		"peran tidak cukup":          {domain.ErrForbidden, http.StatusForbidden},
		"project tidak terlihat":     {domain.ErrProjectNotFound, http.StatusNotFound},
		"target bukan anggota":       {domain.ErrMemberNotFound, http.StatusNotFound},
		"tak terduga":                {errors.New("db mati"), http.StatusInternalServerError},
	} {
		t.Run(name, func(t *testing.T) {
			uc := &fakeMemberUC{transferFn: func() error { return tc.err }}
			w := do(newMemberRouter(uc, nil), "POST", transferPath, `{"user_id":"`+memberUserID.String()+`"}`, authAs)
			if w.Code != tc.want {
				t.Errorf("status %d, harapannya %d", w.Code, tc.want)
			}
		})
	}
}
