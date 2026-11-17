package repository

// Test skema GORM TANPA database: memastikan setiap kolom yang di-SELECT benar-benar
// dipetakan ke field struct.
//
// Latar belakang: GORM hanya membaca field yang diekspor. taskRow dulu menyematkan tipe
// TIDAK diekspor (taskRecord), sehingga semua kolom task diabaikan secara diam-diam dan
// setiap task kembali dengan ID dan semua field kosong, tanpa error apa pun. Unit test
// dengan fake repository tidak bisa menangkapnya; test ini bisa, dan berjalan di lokal.

import (
	"reflect"
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

func parse(t *testing.T, model any) *schema.Schema {
	t.Helper()
	s, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("schema.Parse(%T): %v", model, err)
	}
	return s
}

func assertColumns(t *testing.T, model any, want []string) {
	t.Helper()
	s := parse(t, model)
	have := map[string]bool{}
	for _, f := range s.Fields {
		if f.DBName != "" {
			have[f.DBName] = true
		}
	}
	for _, col := range want {
		if !have[col] {
			t.Errorf("%T: kolom %q tidak dipetakan ke field mana pun (akan terbaca kosong tanpa error)", model, col)
		}
	}
}

// Kolom persis seperti di viewSelect.
func TestTaskRow_MapsEverySelectedColumn(t *testing.T) {
	assertColumns(t, &taskRow{}, []string{
		"internal_id", "project_id", "title", "description", "status", "priority",
		"assignee_id", "created_by", "due_date", "created_at", "updated_at", "assignee_name",
	})
}

func TestTaskRecord_MapsEveryColumn(t *testing.T) {
	assertColumns(t, &taskRecord{}, []string{
		"internal_id", "project_id", "title", "description", "status", "priority",
		"assignee_id", "created_by", "due_date", "created_at", "updated_at",
	})
}

func TestCommentRows_MapEverySelectedColumn(t *testing.T) {
	assertColumns(t, &commentRow{}, []string{
		"internal_id", "task_id", "user_id", "author_name", "content", "created_at", "updated_at",
	})
	assertColumns(t, &commentRecord{}, []string{
		"internal_id", "task_id", "user_id", "content", "created_at", "updated_at",
	})
}

func TestOtherRows_MapEverySelectedColumn(t *testing.T) {
	assertColumns(t, &accessRow{}, []string{"internal_id", "workspace_id", "status", "workspace_role", "project_role"})
	assertColumns(t, &activityRow{}, []string{
		"internal_id", "task_id", "actor_id", "actor_name", "activity_type", "old_value", "new_value", "created_at",
	})
	assertColumns(t, &summaryRow{}, []string{
		"total", "todo", "in_progress", "done", "low", "medium", "high", "urgent", "unassigned", "overdue",
	})
}

// Pagar umum: tidak boleh ada struct yang menyematkan tipe tidak diekspor, karena
// GORM akan mengabaikannya (lihat catatan di atas).
func TestRowTypes_DoNotEmbedUnexportedStructs(t *testing.T) {
	for _, model := range []any{
		taskRow{}, taskRecord{}, commentRow{}, commentRecord{}, accessRow{}, activityRow{}, summaryRow{},
	} {
		rt := reflect.TypeOf(model)
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			if f.Anonymous && !f.IsExported() {
				t.Errorf("%s menyematkan tipe tidak diekspor %s: GORM akan mengabaikan semua kolomnya", rt.Name(), f.Name)
			}
		}
	}
}
