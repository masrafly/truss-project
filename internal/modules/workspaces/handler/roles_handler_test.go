package handler_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"truss-project/internal/modules/workspaces/domain"
)

var memberPath = membersPath + "/" + memberID.String()

// ---------- PATCH /workspaces/:id/members/:userId ----------

func TestChangeRole_SuccessReturnsUpdatedMember(t *testing.T) {
	uc := &fakeMemberUC{changeFn: func(context.Context, string, string, string, string) (*domain.Member, error) {
		return sampleMember(domain.RoleAdmin), nil
	}}
	w := do(newMemberRouter(uc, nil), http.MethodPatch, memberPath, `{"role":"admin"}`, "Bearer valid-owner-1")

	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if uc.gotActor != "owner-1" || uc.gotWS != wsID.String() || uc.gotTarget != memberID.String() || uc.gotRole != "admin" {
		t.Errorf("argumen = %q %q %q %q", uc.gotActor, uc.gotWS, uc.gotTarget, uc.gotRole)
	}
	data := decode(t, w)["data"].(map[string]any)
	if data["role"] != "admin" || data["user_id"] != memberID.String() {
		t.Errorf("data = %v", data)
	}
}

func TestChangeRole_BadBodyIs400WithoutCallingUseCase(t *testing.T) {
	uc := &fakeMemberUC{}
	r := newMemberRouter(uc, nil)
	for _, body := range []string{``, `{}`, `{"role":""}`, `bukan json`} {
		if w := do(r, http.MethodPatch, memberPath, body, authAs); w.Code != http.StatusBadRequest {
			t.Errorf("body %q: status %d", body, w.Code)
		}
	}
	if uc.changeCalls != 0 {
		t.Error("use case tidak boleh dipanggil")
	}
	if w := do(r, http.MethodPatch, memberPath, `{"role":"admin"}`, ""); w.Code != http.StatusUnauthorized {
		t.Errorf("tanpa token: status %d", w.Code)
	}
}

func TestChangeRole_ErrorMapping(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want int
	}{
		"input tidak valid":   {domain.ErrInvalidInput, http.StatusBadRequest},
		"bukan owner":         {domain.ErrForbidden, http.StatusForbidden},
		"workspace tak ada":   {domain.ErrWorkspaceNotFound, http.StatusNotFound},
		"anggota tak ada":     {domain.ErrMemberNotFound, http.StatusNotFound},
		"target adalah owner": {domain.ErrCannotChangeOwnerRole, http.StatusConflict},
		"error tak terduga":   {errors.New("db mati"), http.StatusInternalServerError},
	} {
		t.Run(name, func(t *testing.T) {
			uc := &fakeMemberUC{changeFn: func(context.Context, string, string, string, string) (*domain.Member, error) {
				return nil, tc.err
			}}
			w := do(newMemberRouter(uc, nil), http.MethodPatch, memberPath, `{"role":"admin"}`, authAs)
			if w.Code != tc.want || strings.Contains(w.Body.String(), "db mati") {
				t.Errorf("status %d, harapannya %d: %s", w.Code, tc.want, w.Body)
			}
		})
	}
}

// ---------- POST /workspaces/:id/transfer-ownership ----------

func TestTransferOwnership_Success(t *testing.T) {
	uc := &fakeMemberUC{transferFn: func(context.Context, string, string, string) error { return nil }}
	w := do(newMemberRouter(uc, nil), http.MethodPost, "/workspaces/"+wsID.String()+"/transfer-ownership",
		`{"user_id":"`+memberID.String()+`"}`, "Bearer valid-owner-1")

	if w.Code != http.StatusOK || uc.gotActor != "owner-1" || uc.gotWS != wsID.String() || uc.gotTarget != memberID.String() {
		t.Fatalf("status %d actor=%q ws=%q target=%q", w.Code, uc.gotActor, uc.gotWS, uc.gotTarget)
	}
}

func TestTransferOwnership_BadBodyAndAuth(t *testing.T) {
	uc := &fakeMemberUC{}
	r := newMemberRouter(uc, nil)
	path := "/workspaces/" + wsID.String() + "/transfer-ownership"
	for _, body := range []string{``, `{}`, `{"user_id":""}`, `x`} {
		if w := do(r, http.MethodPost, path, body, authAs); w.Code != http.StatusBadRequest {
			t.Errorf("body %q: status %d", body, w.Code)
		}
	}
	if w := do(r, http.MethodPost, path, `{"user_id":"x"}`, ""); w.Code != http.StatusUnauthorized {
		t.Errorf("tanpa token: status %d", w.Code)
	}
	if uc.transferCalls != 0 {
		t.Error("use case tidak boleh dipanggil")
	}
}

func TestTransferOwnership_ErrorMapping(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want int
	}{
		"ke diri sendiri / input": {domain.ErrInvalidInput, http.StatusBadRequest},
		"bukan owner":             {domain.ErrForbidden, http.StatusForbidden},
		"target bukan anggota":    {domain.ErrMemberNotFound, http.StatusNotFound},
		"workspace tak ada":       {domain.ErrWorkspaceNotFound, http.StatusNotFound},
		"error tak terduga":       {errors.New("db mati"), http.StatusInternalServerError},
	} {
		t.Run(name, func(t *testing.T) {
			uc := &fakeMemberUC{transferFn: func(context.Context, string, string, string) error { return tc.err }}
			w := do(newMemberRouter(uc, nil), http.MethodPost, "/workspaces/"+wsID.String()+"/transfer-ownership",
				`{"user_id":"`+memberID.String()+`"}`, authAs)
			if w.Code != tc.want {
				t.Errorf("status %d, harapannya %d", w.Code, tc.want)
			}
		})
	}
}

// ---------- DELETE /workspaces/:id ----------

func TestDeleteWorkspace_NoContentAndPassesConfirmation(t *testing.T) {
	uc := &fakeUC{deleteFn: func(context.Context, string, string, string) error { return nil }}
	w := do(newRouter(uc, nil), http.MethodDelete, "/workspaces/"+wsID.String(), `{"confirm_name":"Tim Backend"}`, "Bearer valid-owner-1")

	if w.Code != http.StatusNoContent || w.Body.Len() != 0 {
		t.Fatalf("status %d body %q", w.Code, w.Body)
	}
	if uc.gotUserID != "owner-1" || uc.gotWSID != wsID.String() || uc.gotConfirm != "Tim Backend" {
		t.Errorf("argumen = %q %q %q", uc.gotUserID, uc.gotWSID, uc.gotConfirm)
	}
}

func TestDeleteWorkspace_ConfirmationRequired(t *testing.T) {
	uc := &fakeUC{}
	r := newRouter(uc, nil)
	for _, body := range []string{``, `{}`, `{"confirm_name":""}`, `bukan json`} {
		if w := do(r, http.MethodDelete, "/workspaces/"+wsID.String(), body, authAs); w.Code != http.StatusBadRequest {
			t.Errorf("body %q: status %d, harapannya 400", body, w.Code)
		}
	}
	if uc.deleteCalls != 0 {
		t.Error("use case tidak boleh dipanggil tanpa konfirmasi")
	}
	if w := do(r, http.MethodDelete, "/workspaces/"+wsID.String(), `{"confirm_name":"x"}`, ""); w.Code != http.StatusUnauthorized {
		t.Errorf("tanpa token: status %d", w.Code)
	}
}

func TestDeleteWorkspace_ErrorMapping(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want int
	}{
		"nama konfirmasi salah": {domain.ErrInvalidInput, http.StatusBadRequest},
		"bukan owner":           {domain.ErrForbidden, http.StatusForbidden},
		"bukan anggota":         {domain.ErrWorkspaceNotFound, http.StatusNotFound},
		"error tak terduga":     {errors.New("db mati"), http.StatusInternalServerError},
	} {
		t.Run(name, func(t *testing.T) {
			uc := &fakeUC{deleteFn: func(context.Context, string, string, string) error { return tc.err }}
			w := do(newRouter(uc, nil), http.MethodDelete, "/workspaces/"+wsID.String(), `{"confirm_name":"x"}`, authAs)
			if w.Code != tc.want {
				t.Errorf("status %d, harapannya %d", w.Code, tc.want)
			}
		})
	}
}
